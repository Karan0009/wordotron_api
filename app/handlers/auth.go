package handlers

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/Karan0009/wordotron_api/app/config"
	"github.com/Karan0009/wordotron_api/app/lib"
	"github.com/Karan0009/wordotron_api/app/lib/validation"
	"github.com/Karan0009/wordotron_api/app/middleware"
	"github.com/Karan0009/wordotron_api/app/serializers"
	"github.com/Karan0009/wordotron_api/app/services"
)

// refreshCookieName is the cookie used when REFRESH_COOKIE_ENABLED is on.
const refreshCookieName = "refresh_token"

// AuthHandler exposes the authentication endpoints.
type AuthHandler struct {
	base
	auth services.AuthService
	cfg  *config.Config
}

// NewAuthHandler builds the authentication handler.
func NewAuthHandler(authService services.AuthService, validator *validation.Validator, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		base: base{validator: validator},
		auth: authService,
		cfg:  cfg,
	}
}

// Register creates an account and emails a verification link. No session is
// started: the account cannot log in until it is verified.
//
//	@Summary		Register a new account
//	@Description	Creates a user with the "user" role and sends a verification email. No session is issued; the account must be verified before /auth/login succeeds.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		serializers.RegisterRequest	true	"Registration details"
//	@Success		201		{object}	lib.SuccessEnvelope{data=serializers.UserResponse}
//	@Failure		409		{object}	lib.ErrorEnvelope
//	@Failure		422		{object}	lib.ErrorEnvelope
//	@Router			/auth/register [post]
func (h *AuthHandler) Register(c fiber.Ctx) error {
	var req serializers.RegisterRequest
	if err := h.bind(c, &req); err != nil {
		return err
	}

	user, err := h.auth.Register(c.Context(), services.RegisterInput{
		Email:    req.Email,
		Password: req.Password,
		FullName: req.FullName,
	})
	if err != nil {
		return err
	}

	return lib.Created(c, serializers.ToUserResponse(user))
}

// Login exchanges credentials for tokens.
//
//	@Summary		Sign in
//	@Description	Verifies credentials and returns an access/refresh token pair.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		serializers.LoginRequest	true	"Credentials"
//	@Success		200		{object}	lib.SuccessEnvelope{data=AuthResponse}
//	@Failure		401		{object}	lib.ErrorEnvelope
//	@Router			/auth/login [post]
func (h *AuthHandler) Login(c fiber.Ctx) error {
	var req serializers.LoginRequest
	if err := h.bind(c, &req); err != nil {
		return err
	}

	result, err := h.auth.Login(c.Context(), services.LoginInput{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		return err
	}

	h.setRefreshCookie(c, result)
	return lib.OK(c, serializers.ToAuthResponse(result))
}

// Refresh rotates a refresh token into a new pair.
//
//	@Summary		Refresh the session
//	@Description	Rotates the refresh token. The presented token is invalidated; reuse revokes every session.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		serializers.RefreshRequest	false	"Refresh token (omit when using the cookie)"
//	@Success		200		{object}	lib.SuccessEnvelope{data=AuthResponse}
//	@Failure		401		{object}	lib.ErrorEnvelope
//	@Router			/auth/refresh [post]
func (h *AuthHandler) Refresh(c fiber.Ctx) error {
	var req serializers.RefreshRequest
	// The body is optional when the token travels in a cookie, so a decode
	// failure is only fatal if no cookie is present either.
	_ = c.Bind().Body(&req)

	token := req.RefreshToken
	if token == "" {
		token = c.Cookies(refreshCookieName)
	}
	if token == "" {
		return lib.Unauthorized("Refresh token is missing").
			WithFields(lib.FieldError{Field: "refresh_token", Message: "This field is required"})
	}

	result, err := h.auth.Refresh(c.Context(), token)
	if err != nil {
		h.clearRefreshCookie(c)
		return err
	}

	h.setRefreshCookie(c, result)
	return lib.OK(c, serializers.ToAuthResponse(result))
}

// Logout revokes the current session.
//
//	@Summary		Sign out
//	@Description	Blacklists the current access token and revokes the supplied refresh token.
//	@Tags			auth
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		serializers.LogoutRequest	false	"Refresh token to revoke"
//	@Success		200		{object}	lib.SuccessEnvelope
//	@Failure		401		{object}	lib.ErrorEnvelope
//	@Router			/auth/logout [post]
func (h *AuthHandler) Logout(c fiber.Ctx) error {
	claims, ok := middleware.ClaimsFrom(c)
	if !ok {
		return lib.Unauthorized("")
	}
	userID, err := claims.UserID()
	if err != nil {
		return lib.Unauthorized("").Wrap(err)
	}

	var req serializers.LogoutRequest
	_ = c.Bind().Body(&req)

	token := req.RefreshToken
	if token == "" {
		token = c.Cookies(refreshCookieName)
	}

	var expiresAt time.Time
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time
	}

	if err := h.auth.Logout(c.Context(), services.LogoutInput{
		UserID:          userID,
		AccessJTI:       claims.ID,
		AccessExpiresAt: expiresAt,
		RefreshToken:    token,
	}); err != nil {
		return err
	}

	h.clearRefreshCookie(c)
	return lib.Message(c, fiber.StatusOK, "Signed out")
}

// LogoutAll revokes every session for the caller.
//
//	@Summary		Sign out everywhere
//	@Description	Revokes every refresh token and invalidates outstanding access tokens for the account.
//	@Tags			auth
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	lib.SuccessEnvelope
//	@Failure		401	{object}	lib.ErrorEnvelope
//	@Router			/auth/logout-all [post]
func (h *AuthHandler) LogoutAll(c fiber.Ctx) error {
	userID, err := middleware.UserID(c)
	if err != nil {
		return err
	}

	if err := h.auth.LogoutAll(c.Context(), userID); err != nil {
		return err
	}

	h.clearRefreshCookie(c)
	return lib.Message(c, fiber.StatusOK, "Signed out of all sessions")
}

// ChangePassword replaces the caller's password.
//
//	@Summary		Change password
//	@Description	Requires the current password. All other sessions are revoked and a fresh token pair is returned.
//	@Tags			auth
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		serializers.ChangePasswordRequest	true	"Current and new password"
//	@Success		200		{object}	lib.SuccessEnvelope{data=AuthResponse}
//	@Failure		401		{object}	lib.ErrorEnvelope
//	@Router			/auth/change-password [post]
func (h *AuthHandler) ChangePassword(c fiber.Ctx) error {
	userID, err := middleware.UserID(c)
	if err != nil {
		return err
	}

	var req serializers.ChangePasswordRequest
	if err := h.bind(c, &req); err != nil {
		return err
	}

	result, err := h.auth.ChangePassword(c.Context(), userID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		return err
	}

	h.setRefreshCookie(c, result)
	return lib.OK(c, serializers.ToAuthResponse(result))
}

// ForgotPassword emails a reset link.
//
//	@Summary		Request a password reset
//	@Description	Always responds 200 so the endpoint cannot be used to discover registered addresses.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		serializers.ForgotPasswordRequest	true	"Account email"
//	@Success		200		{object}	lib.SuccessEnvelope
//	@Router			/auth/forgot-password [post]
func (h *AuthHandler) ForgotPassword(c fiber.Ctx) error {
	var req serializers.ForgotPasswordRequest
	if err := h.bind(c, &req); err != nil {
		return err
	}

	if err := h.auth.ForgotPassword(c.Context(), req.Email); err != nil {
		return err
	}

	return lib.Message(c, fiber.StatusOK,
		"If an account exists for that address, a reset link has been sent")
}

// ResetPassword consumes a reset token.
//
//	@Summary		Reset a password
//	@Description	Consumes a single-use reset token and revokes every existing session.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		serializers.ResetPasswordRequest	true	"Reset token and new password"
//	@Success		200		{object}	lib.SuccessEnvelope
//	@Failure		400		{object}	lib.ErrorEnvelope
//	@Router			/auth/reset-password [post]
func (h *AuthHandler) ResetPassword(c fiber.Ctx) error {
	var req serializers.ResetPasswordRequest
	if err := h.bind(c, &req); err != nil {
		return err
	}

	if err := h.auth.ResetPassword(c.Context(), req.Token, req.NewPassword); err != nil {
		return err
	}

	h.clearRefreshCookie(c)
	return lib.Message(c, fiber.StatusOK, "Password updated, please sign in again")
}

// VerifyEmail consumes a verification token and unblocks login.
//
//	@Summary		Verify an email address
//	@Description	Consumes a single-use verification token sent at registration.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		serializers.VerifyEmailRequest	true	"Verification token"
//	@Success		200		{object}	lib.SuccessEnvelope
//	@Failure		400		{object}	lib.ErrorEnvelope
//	@Router			/auth/verify-email [post]
func (h *AuthHandler) VerifyEmail(c fiber.Ctx) error {
	var req serializers.VerifyEmailRequest
	if err := h.bind(c, &req); err != nil {
		return err
	}

	if err := h.auth.VerifyEmail(c.Context(), req.Token); err != nil {
		return err
	}

	return lib.Message(c, fiber.StatusOK, "Email verified, you can now sign in")
}

// setRefreshCookie mirrors the refresh token into an HttpOnly cookie when the
// feature is enabled.
func (h *AuthHandler) setRefreshCookie(c fiber.Ctx, result *services.AuthResult) {
	if !h.cfg.Auth.RefreshCookie {
		return
	}

	c.Cookie(&fiber.Cookie{
		Name:     refreshCookieName,
		Value:    result.Tokens.RefreshToken,
		Path:     "/api/v1/auth",
		Domain:   h.cfg.Auth.CookieDomain,
		Expires:  result.Tokens.RefreshExpiresAt,
		Secure:   h.cfg.Auth.CookieSecure,
		HTTPOnly: true,
		SameSite: h.cfg.Auth.CookieSameSite,
	})
}

func (h *AuthHandler) clearRefreshCookie(c fiber.Ctx) {
	if !h.cfg.Auth.RefreshCookie {
		return
	}

	c.Cookie(&fiber.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/api/v1/auth",
		Domain:   h.cfg.Auth.CookieDomain,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		Secure:   h.cfg.Auth.CookieSecure,
		HTTPOnly: true,
		SameSite: h.cfg.Auth.CookieSameSite,
	})
}
