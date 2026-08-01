package service

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/internal/auth"
	"github.com/Karan0009/wordotron_api/internal/config"
	"github.com/Karan0009/wordotron_api/internal/models"
	"github.com/Karan0009/wordotron_api/internal/repository"
	"github.com/Karan0009/wordotron_api/internal/storage"
	"github.com/Karan0009/wordotron_api/pkg/apperror"
)

// Actor is the authenticated caller. Authorisation decisions live in the
// service rather than the handler so they hold for every transport.
type Actor struct {
	ID   uuid.UUID
	Role models.Role
}

// IsAdmin reports whether the actor holds the admin role.
func (a Actor) IsAdmin() bool { return a.Role == models.RoleAdmin }

// CreateUserInput is the admin-only account creation payload.
type CreateUserInput struct {
	Email    string
	Password string
	FullName string
	Role     models.Role
}

// UpdateUserInput is a partial update; nil fields are left unchanged.
type UpdateUserInput struct {
	FullName *string
	Role     *models.Role
	IsActive *bool
}

// AvatarInput describes an uploaded avatar.
type AvatarInput struct {
	Filename    string
	ContentType string
	Size        int64
	Content     io.Reader
}

// UserService is the user management use-case boundary.
type UserService interface {
	Get(ctx context.Context, actor Actor, id uuid.UUID) (*models.User, error)
	List(ctx context.Context, filter models.ListUsersFilter) (*models.Page[models.User], error)
	Create(ctx context.Context, in CreateUserInput) (*models.User, error)
	Update(ctx context.Context, actor Actor, id uuid.UUID, in UpdateUserInput) (*models.User, error)
	Delete(ctx context.Context, actor Actor, id uuid.UUID) error
	UploadAvatar(ctx context.Context, actor Actor, id uuid.UUID, in AvatarInput) (*models.User, error)
}

type userService struct {
	store    repository.Store
	hasher   auth.Hasher
	sessions auth.SessionStore
	files    storage.Storage
	cfg      *config.Config
	log      *slog.Logger
}

var _ UserService = (*userService)(nil)

// NewUserService wires the user management use cases.
func NewUserService(
	store repository.Store,
	hasher auth.Hasher,
	sessions auth.SessionStore,
	files storage.Storage,
	cfg *config.Config,
	log *slog.Logger,
) UserService {
	return &userService{
		store:    store,
		hasher:   hasher,
		sessions: sessions,
		files:    files,
		cfg:      cfg,
		log:      log.With(slog.String("component", "user_service")),
	}
}

// allowedAvatarTypes is an allow-list: never trust a client-supplied MIME type
// beyond checking it against a known-safe set.
var allowedAvatarTypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
	"image/gif":  {},
}

func (s *userService) Get(ctx context.Context, actor Actor, id uuid.UUID) (*models.User, error) {
	if err := s.authorize(actor, id); err != nil {
		return nil, err
	}
	return s.store.Users().GetByID(ctx, id)
}

func (s *userService) List(ctx context.Context, filter models.ListUsersFilter) (*models.Page[models.User], error) {
	users, total, err := s.store.Users().List(ctx, filter)
	if err != nil {
		return nil, err
	}
	return &models.Page[models.User]{
		Items: users,
		Meta:  models.NewPageMeta(filter.Page, total),
	}, nil
}

func (s *userService) Create(ctx context.Context, in CreateUserInput) (*models.User, error) {
	email := normalizeEmail(in.Email)

	role := in.Role
	if role == "" {
		role = models.RoleUser
	}
	if !role.Valid() {
		return nil, apperror.BadRequest("Invalid role").
			WithFields(apperror.FieldError{Field: "role", Message: "Must be one of: user, admin"})
	}

	exists, err := s.store.Users().EmailExists(ctx, email)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, apperror.Conflict("An account with this email already exists")
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	return s.store.Users().Create(ctx, models.CreateUserInput{
		Email:        email,
		PasswordHash: hash,
		FullName:     strings.TrimSpace(in.FullName),
		Role:         role,
	})
}

func (s *userService) Update(ctx context.Context, actor Actor, id uuid.UUID, in UpdateUserInput) (*models.User, error) {
	if err := s.authorize(actor, id); err != nil {
		return nil, err
	}

	// Role and activation are privileged: a user must not be able to promote
	// themselves by adding a field to the request body.
	if !actor.IsAdmin() && (in.Role != nil || in.IsActive != nil) {
		return nil, apperror.Forbidden("Only administrators can change role or activation status")
	}
	if in.Role != nil && !in.Role.Valid() {
		return nil, apperror.BadRequest("Invalid role").
			WithFields(apperror.FieldError{Field: "role", Message: "Must be one of: user, admin"})
	}
	// Locking yourself out is almost always a mistake, so it is rejected.
	if actor.ID == id && in.IsActive != nil && !*in.IsActive {
		return nil, apperror.BadRequest("You cannot deactivate your own account")
	}

	var fullName *string
	if in.FullName != nil {
		trimmed := strings.TrimSpace(*in.FullName)
		fullName = &trimmed
	}

	user, err := s.store.Users().Update(ctx, id, models.UpdateUserInput{
		FullName: fullName,
		Role:     in.Role,
		IsActive: in.IsActive,
	})
	if err != nil {
		return nil, err
	}

	// A deactivated or demoted account must not keep using tokens minted under
	// the old privileges.
	if (in.IsActive != nil && !*in.IsActive) || in.Role != nil {
		if err := s.sessions.RevokeAllSessions(ctx, id); err != nil {
			s.log.ErrorContext(ctx, "revoke sessions after update failed", slog.String("error", err.Error()))
		}
	}

	return user, nil
}

func (s *userService) Delete(ctx context.Context, actor Actor, id uuid.UUID) error {
	if !actor.IsAdmin() && actor.ID != id {
		return apperror.Forbidden("")
	}
	if actor.IsAdmin() && actor.ID == id {
		return apperror.BadRequest("Administrators cannot delete their own account")
	}

	user, err := s.store.Users().GetByID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.store.Users().Delete(ctx, id); err != nil {
		return err
	}

	if err := s.sessions.RevokeAllSessions(ctx, id); err != nil {
		s.log.ErrorContext(ctx, "revoke sessions after delete failed", slog.String("error", err.Error()))
	}

	// Orphaned avatars are cleaned up on a best-effort basis: the account row
	// is already gone, so a storage hiccup must not fail the request.
	if user.AvatarURL != nil {
		if key := s.keyFromURL(*user.AvatarURL); key != "" {
			if err := s.files.Delete(ctx, key); err != nil {
				s.log.WarnContext(ctx, "delete avatar failed", slog.String("error", err.Error()))
			}
		}
	}

	s.log.InfoContext(ctx, "user deleted", slog.String("user_id", id.String()))
	return nil
}

func (s *userService) UploadAvatar(ctx context.Context, actor Actor, id uuid.UUID, in AvatarInput) (*models.User, error) {
	if err := s.authorize(actor, id); err != nil {
		return nil, err
	}

	contentType := strings.ToLower(strings.TrimSpace(strings.SplitN(in.ContentType, ";", 2)[0]))
	if _, ok := allowedAvatarTypes[contentType]; !ok {
		return nil, apperror.UnsupportedMediaType("Avatar must be a JPEG, PNG, WebP or GIF image")
	}
	if in.Size > s.cfg.Storage.MaxUploadBytes {
		return nil, apperror.PayloadTooLarge("Avatar exceeds the maximum upload size")
	}

	existing, err := s.store.Users().GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	key := storage.BuildKey("avatars", in.Filename)
	object, err := s.files.Put(ctx, key, in.Content, in.Size, contentType)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	user, err := s.store.Users().Update(ctx, id, models.UpdateUserInput{AvatarURL: &object.URL})
	if err != nil {
		// Roll back the orphaned upload before returning.
		if delErr := s.files.Delete(ctx, key); delErr != nil {
			s.log.WarnContext(ctx, "orphaned avatar cleanup failed", slog.String("error", delErr.Error()))
		}
		return nil, err
	}

	if existing.AvatarURL != nil {
		if oldKey := s.keyFromURL(*existing.AvatarURL); oldKey != "" && oldKey != key {
			if err := s.files.Delete(ctx, oldKey); err != nil {
				s.log.WarnContext(ctx, "previous avatar cleanup failed", slog.String("error", err.Error()))
			}
		}
	}

	return user, nil
}

// authorize allows admins everything and users only their own record.
func (s *userService) authorize(actor Actor, targetID uuid.UUID) error {
	if actor.IsAdmin() || actor.ID == targetID {
		return nil
	}
	return apperror.Forbidden("")
}

// keyFromURL recovers the storage key from a stored public URL.
func (s *userService) keyFromURL(rawURL string) string {
	base := strings.TrimRight(s.cfg.Storage.PublicBaseURL, "/") + "/"
	if !strings.HasPrefix(rawURL, base) {
		return ""
	}
	return strings.TrimPrefix(rawURL, base)
}
