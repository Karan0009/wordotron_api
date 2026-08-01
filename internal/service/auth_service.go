// Package service holds the business rules. It depends on repository and auth
// interfaces only, so every rule here is unit-testable without a database and
// reusable from a future worker binary.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/internal/auth"
	"github.com/Karan0009/wordotron_api/internal/config"
	"github.com/Karan0009/wordotron_api/internal/mailer"
	"github.com/Karan0009/wordotron_api/internal/models"
	"github.com/Karan0009/wordotron_api/internal/repository"
	"github.com/Karan0009/wordotron_api/pkg/apperror"
)

// AuthResult is returned by every operation that authenticates a user.
type AuthResult struct {
	User   *models.User
	Tokens auth.TokenPair
}

// RegisterInput is the validated payload for account creation.
type RegisterInput struct {
	Email    string
	Password string
	FullName string
}

// LoginInput is the validated payload for a login attempt.
type LoginInput struct {
	Email    string
	Password string
}

// LogoutInput carries everything needed to revoke the current session.
type LogoutInput struct {
	UserID          uuid.UUID
	AccessJTI       string
	AccessExpiresAt time.Time
	RefreshToken    string
}

// AuthService is the authentication use-case boundary.
type AuthService interface {
	Register(ctx context.Context, in RegisterInput) (*AuthResult, error)
	Login(ctx context.Context, in LoginInput) (*AuthResult, error)
	Refresh(ctx context.Context, refreshToken string) (*AuthResult, error)
	Logout(ctx context.Context, in LogoutInput) error
	LogoutAll(ctx context.Context, userID uuid.UUID) error
	ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) (*AuthResult, error)
	ForgotPassword(ctx context.Context, email string) error
	ResetPassword(ctx context.Context, token, newPassword string) error
}

type authService struct {
	store    repository.Store
	hasher   auth.Hasher
	tokens   auth.TokenManager
	sessions auth.SessionStore
	mail     mailer.Mailer
	cfg      *config.Config
	log      *slog.Logger

	now func() time.Time
}

var _ AuthService = (*authService)(nil)

// NewAuthService wires the authentication use cases.
func NewAuthService(
	store repository.Store,
	hasher auth.Hasher,
	tokens auth.TokenManager,
	sessions auth.SessionStore,
	mail mailer.Mailer,
	cfg *config.Config,
	log *slog.Logger,
) AuthService {
	return &authService{
		store:    store,
		hasher:   hasher,
		tokens:   tokens,
		sessions: sessions,
		mail:     mail,
		cfg:      cfg,
		log:      log.With(slog.String("component", "auth_service")),
		now:      time.Now,
	}
}

func (s *authService) Register(ctx context.Context, in RegisterInput) (*AuthResult, error) {
	email := normalizeEmail(in.Email)

	exists, err := s.store.Users().EmailExists(ctx, email)
	if err != nil {
		return nil, err
	}
	if exists {
		// The email is already discoverable through this endpoint by design:
		// a signup form has to tell the user their address is taken.
		return nil, apperror.Conflict("An account with this email already exists")
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	user, err := s.store.Users().Create(ctx, models.CreateUserInput{
		Email:        email,
		PasswordHash: hash,
		FullName:     strings.TrimSpace(in.FullName),
		Role:         models.RoleUser,
	})
	if err != nil {
		return nil, err
	}

	if err := s.mail.SendWelcome(ctx, user.Email, user.FullName); err != nil {
		// A failed welcome email must not fail the registration.
		s.log.WarnContext(ctx, "welcome email failed", slog.String("error", err.Error()))
	}

	s.log.InfoContext(ctx, "user registered", slog.String("user_id", user.ID.String()))
	return s.issueSession(ctx, user)
}

func (s *authService) Login(ctx context.Context, in LoginInput) (*AuthResult, error) {
	email := normalizeEmail(in.Email)
	invalidCredentials := apperror.Unauthorized("Invalid email or password")

	user, err := s.store.Users().GetByEmail(ctx, email)
	if err != nil {
		if appErr, ok := apperror.As(err); ok && appErr.Code == apperror.CodeNotFound {
			// Spend the same CPU as a real verification so response timing
			// does not reveal whether the account exists.
			s.hasher.DummyVerify()
			return nil, invalidCredentials
		}
		return nil, err
	}

	if err := s.hasher.Verify(user.PasswordHash, in.Password); err != nil {
		if errors.Is(err, auth.ErrPasswordMismatch) {
			s.log.InfoContext(ctx, "failed login", slog.String("user_id", user.ID.String()))
			return nil, invalidCredentials
		}
		return nil, apperror.Internal(err)
	}

	if !user.IsActive {
		return nil, apperror.Forbidden("This account has been deactivated")
	}

	if err := s.store.Users().TouchLastLogin(ctx, user.ID); err != nil {
		// Non-fatal: a login that works is more important than the timestamp.
		s.log.WarnContext(ctx, "could not update last login", slog.String("error", err.Error()))
	}

	return s.issueSession(ctx, user)
}

// Refresh rotates the refresh token: the presented token is revoked and a new
// pair is issued. A token that parses but is no longer registered indicates
// replay of a stolen credential, so every session for that user is dropped.
func (s *authService) Refresh(ctx context.Context, refreshToken string) (*AuthResult, error) {
	claims, err := s.tokens.ParseRefreshToken(refreshToken)
	if err != nil {
		return nil, apperror.Unauthorized("Invalid or expired refresh token").Wrap(err)
	}

	userID, err := claims.UserID()
	if err != nil {
		return nil, apperror.Unauthorized("Invalid refresh token").Wrap(err)
	}

	active, err := s.sessions.IsRefreshTokenActive(ctx, userID, claims.ID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	if !active {
		s.log.WarnContext(ctx, "refresh token reuse detected, revoking all sessions",
			slog.String("user_id", userID.String()),
			slog.String("jti", claims.ID),
		)
		if err := s.sessions.RevokeAllSessions(ctx, userID); err != nil {
			s.log.ErrorContext(ctx, "revoke all sessions failed", slog.String("error", err.Error()))
		}
		return nil, apperror.Unauthorized("Session expired, please sign in again")
	}

	user, err := s.store.Users().GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !user.IsActive {
		_ = s.sessions.RevokeAllSessions(ctx, userID)
		return nil, apperror.Forbidden("This account has been deactivated")
	}

	if err := s.sessions.RevokeRefreshToken(ctx, userID, claims.ID); err != nil {
		return nil, apperror.Internal(err)
	}

	return s.issueSession(ctx, user)
}

func (s *authService) Logout(ctx context.Context, in LogoutInput) error {
	// Best effort on the refresh token: a malformed one still logs the caller
	// out of the current access token.
	if in.RefreshToken != "" {
		if claims, err := s.tokens.ParseRefreshToken(in.RefreshToken); err == nil {
			if userID, err := claims.UserID(); err == nil && userID == in.UserID {
				if err := s.sessions.RevokeRefreshToken(ctx, in.UserID, claims.ID); err != nil {
					return apperror.Internal(err)
				}
			}
		}
	}

	if in.AccessJTI != "" {
		ttl := time.Until(in.AccessExpiresAt)
		if err := s.sessions.BlacklistAccessToken(ctx, in.AccessJTI, ttl); err != nil {
			return apperror.Internal(err)
		}
	}

	s.log.InfoContext(ctx, "user logged out", slog.String("user_id", in.UserID.String()))
	return nil
}

func (s *authService) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	if err := s.sessions.RevokeAllSessions(ctx, userID); err != nil {
		return apperror.Internal(err)
	}
	s.log.InfoContext(ctx, "all sessions revoked", slog.String("user_id", userID.String()))
	return nil
}

// ChangePassword verifies the current password, replaces it, drops every other
// session and hands back a fresh token pair so the caller stays signed in.
func (s *authService) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) (*AuthResult, error) {
	user, err := s.store.Users().GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if err := s.hasher.Verify(user.PasswordHash, currentPassword); err != nil {
		if errors.Is(err, auth.ErrPasswordMismatch) {
			return nil, apperror.Unauthorized("Current password is incorrect").
				WithFields(apperror.FieldError{Field: "current_password", Message: "Incorrect password"})
		}
		return nil, apperror.Internal(err)
	}

	if currentPassword == newPassword {
		return nil, apperror.BadRequest("New password must differ from the current one").
			WithFields(apperror.FieldError{Field: "new_password", Message: "Must differ from the current password"})
	}

	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if err := s.store.Users().UpdatePassword(ctx, user.ID, hash); err != nil {
		return nil, err
	}

	if err := s.sessions.RevokeAllSessions(ctx, user.ID); err != nil {
		return nil, apperror.Internal(err)
	}

	s.log.InfoContext(ctx, "password changed", slog.String("user_id", user.ID.String()))
	return s.issueSession(ctx, user)
}

// ForgotPassword always reports success: telling an anonymous caller whether an
// address is registered is an account-enumeration oracle.
func (s *authService) ForgotPassword(ctx context.Context, email string) error {
	normalized := normalizeEmail(email)

	user, err := s.store.Users().GetByEmail(ctx, normalized)
	if err != nil {
		if appErr, ok := apperror.As(err); ok && appErr.Code == apperror.CodeNotFound {
			s.log.InfoContext(ctx, "password reset requested for unknown email")
			return nil
		}
		return err
	}
	if !user.IsActive {
		return nil
	}

	token, hash, err := generateResetToken()
	if err != nil {
		return apperror.Internal(err)
	}

	expiresAt := s.now().UTC().Add(s.cfg.Auth.PasswordResetExpiry)

	// Invalidating the previous tokens and issuing the new one must be atomic,
	// otherwise a crash between the two leaves the account with no valid link
	// and no way to request another until the old ones expire.
	err = s.store.WithTx(ctx, func(tx repository.Store) error {
		if err := tx.PasswordResets().InvalidateForUser(ctx, user.ID); err != nil {
			return err
		}
		_, err := tx.PasswordResets().Create(ctx, user.ID, hash, expiresAt)
		return err
	})
	if err != nil {
		return err
	}

	resetURL := s.buildResetURL(token)
	if err := s.mail.SendPasswordReset(ctx, user.Email, resetURL); err != nil {
		return apperror.Internal(fmt.Errorf("send password reset email: %w", err))
	}

	s.log.InfoContext(ctx, "password reset requested", slog.String("user_id", user.ID.String()))
	return nil
}

func (s *authService) ResetPassword(ctx context.Context, token, newPassword string) error {
	invalid := apperror.BadRequest("This reset link is invalid or has expired")

	record, err := s.store.PasswordResets().GetByTokenHash(ctx, hashResetToken(token))
	if err != nil {
		if appErr, ok := apperror.As(err); ok && appErr.Code == apperror.CodeNotFound {
			return invalid
		}
		return err
	}

	if !record.IsUsable(s.now().UTC()) {
		return invalid
	}

	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return apperror.Internal(err)
	}

	// Consuming the token and setting the password share a transaction so a
	// partial failure cannot leave a reusable link behind.
	if err := s.store.WithTx(ctx, func(tx repository.Store) error {
		if err := tx.PasswordResets().MarkUsed(ctx, record.ID); err != nil {
			return err
		}
		return tx.Users().UpdatePassword(ctx, record.UserID, hash)
	}); err != nil {
		return err
	}

	// Anyone holding a session obtained with the old password is logged out.
	if err := s.sessions.RevokeAllSessions(ctx, record.UserID); err != nil {
		s.log.ErrorContext(ctx, "revoke sessions after reset failed", slog.String("error", err.Error()))
	}

	s.log.InfoContext(ctx, "password reset completed", slog.String("user_id", record.UserID.String()))
	return nil
}

// issueSession mints a token pair and registers the refresh token.
func (s *authService) issueSession(ctx context.Context, user *models.User) (*AuthResult, error) {
	pair, err := s.tokens.GenerateTokenPair(user.ID, user.Role)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if err := s.sessions.StoreRefreshToken(ctx, user.ID, pair.RefreshJTI, pair.RefreshExpiresAt); err != nil {
		return nil, apperror.Internal(err)
	}

	return &AuthResult{User: user, Tokens: pair}, nil
}

func (s *authService) buildResetURL(token string) string {
	base := strings.TrimRight(s.cfg.App.FrontendURL, "/")
	path := "/" + strings.TrimLeft(s.cfg.App.ResetPasswordPath, "/")
	return base + path + "?token=" + url.QueryEscape(token)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// generateResetToken returns the plaintext token (emailed to the user) and the
// SHA-256 hash that is stored. A database leak therefore yields no usable
// links. SHA-256 without a salt is appropriate here: the token is 256 bits of
// entropy, so it is not brute-forceable.
func generateResetToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate reset token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashResetToken(token), nil
}

func hashResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
