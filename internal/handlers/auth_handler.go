package handlers

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/Karan0009/wordotron_api/internal/config"
	"github.com/Karan0009/wordotron_api/internal/middleware"
	"github.com/Karan0009/wordotron_api/internal/service"
	"github.com/Karan0009/wordotron_api/internal/utils"
	"github.com/Karan0009/wordotron_api/internal/validation"
	"github.com/Karan0009/wordotron_api/pkg/apperror"
)

// refreshCookieName is the cookie used when REFRESH_COOKIE_ENABLED is on.
const refreshCookieName = "refresh_token"

// AuthHandler exposes the authentication endpoints.
type AuthHandler struct {
	base
	auth service.AuthService
	cfg  *config.Config
}

// NewAuthHandler builds the authentication handler.
func NewAuthHandler(authService service.AuthService, validator *validation.Validator, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		base: base{validator: validator},
		auth: authService,
		cfg:  cfg,
	}
}

// Register creates an account and starts a session.
//
//	@Summary		Register a new account
//	@Description	Creates a user with the "user" role and returns an access/refresh token pair.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		RegisterRequest	true	"Registration details"
//	@Success		201		{object}	utils.SuccessEnvelope{data=AuthResponse}
//	@Failure		409		{object}	utils.ErrorEnvelope
//	@Failure		422		{object}	utils.ErrorEnvelope
//	@Router			/auth/register [post]
func (h *AuthHandler) Register(c fiber.Ctx) error {
	var req RegisterRequest
	if err := h.bind(c, &req); err != nil {
		return err
	}

	result, err := h.auth.Register(c.Context(), service.RegisterInput{
		Email:    req.Email,
		Password: req.Password,
		FullName: req.FullName,
	})
	if err != nil {
		return err
	}

	h.setRefreshCookie(c, result)
	return utils.Created(c, toAuthResponse(result))
}

// Login exchanges credentials for tokens.
//
//	@Summary		Sign in
//	@Description	Verifies credentials and returns an access/refresh token pair.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		LoginRequest	true	"Credentials"
//	@Success		200		{object}	utils.SuccessEnvelope{data=AuthResponse}
//	@Failure		401		{object}	utils.ErrorEnvelope
//	@Router			/auth/login [post]
func (h *AuthHandler) Login(c fiber.Ctx) error {
	var req LoginRequest
	if err := h.bind(c, &req); err != nil {
		return err
	}

	result, err := h.auth.Login(c.Context(), service.LoginInput{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		return err
	}

	h.setRefreshCookie(c, result)
	return utils.OK(c, toAuthResponse(result))
}

// Refresh rotates a refresh token into a new pair.
//
//	@Summary		Refresh the session
//	@Description	Rotates the refresh token. The presented token is invalidated; reuse revokes every session.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		RefreshRequest	false	"Refresh token (omit when using the cookie)"
//	@Success		200		{object}	utils.SuccessEnvelope{data=AuthResponse}
//	@Failure		401		{object}	utils.ErrorEnvelope
//	@Router			/auth/refresh [post]
func (h *AuthHandler) Refresh(c fiber.Ctx) error {
	var req RefreshRequest
	// The body is optional when the token travels in a cookie, so a decode
	// failure is only fatal if no cookie is present either.
	_ = c.Bind().Body(&req)

	token := req.RefreshToken
	if token == "" {
		token = c.Cookies(refreshCookieName)
	}
	if token == "" {
		return apperror.Unauthorized("Refresh token is missing").
			WithFields(apperror.FieldError{Field: "refresh_token", Message: "This field is required"})
	}

	result, err := h.auth.Refresh(c.Context(), token)
	if err != nil {
		h.clearRefreshCookie(c)
		return err
	}

	h.setRefreshCookie(c, result)
	return utils.OK(c, toAuthResponse(result))
}

// Logout revokes the current session.
//
//	@Summary		Sign out
//	@Description	Blacklists the current access token and revokes the supplied refresh token.
//	@Tags			auth
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		LogoutRequest	false	"Refresh token to revoke"
//	@Success		200		{object}	utils.SuccessEnvelope
//	@Failure		401		{object}	utils.ErrorEnvelope
//	@Router			/auth/logout [post]
func (h *AuthHandler) Logout(c fiber.Ctx) error {
	claims, ok := middleware.ClaimsFrom(c)
	if !ok {
		return apperror.Unauthorized("")
	}
	userID, err := claims.UserID()
	if err != nil {
		return apperror.Unauthorized("").Wrap(err)
	}

	var req LogoutRequest
	_ = c.Bind().Body(&req)

	token := req.RefreshToken
	if token == "" {
		token = c.Cookies(refreshCookieName)
	}

	var expiresAt time.Time
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time
	}

	if err := h.auth.Logout(c.Context(), service.LogoutInput{
		UserID:          userID,
		AccessJTI:       claims.ID,
		AccessExpiresAt: expiresAt,
		RefreshToken:    token,
	}); err != nil {
		return err
	}

	h.clearRefreshCookie(c)
	return utils.Message(c, fiber.StatusOK, "Signed out")
}

// LogoutAll revokes every session for the caller.
//
//	@Summary		Sign out everywhere
//	@Description	Revokes every refresh token and invalidates outstanding access tokens for the account.
//	@Tags			auth
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	utils.SuccessEnvelope
//	@Failure		401	{object}	utils.ErrorEnvelope
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
	return utils.Message(c, fiber.StatusOK, "Signed out of all sessions")
}

// ChangePassword replaces the caller's password.
//
//	@Summary		Change password
//	@Description	Requires the current password. All other sessions are revoked and a fresh token pair is returned.
//	@Tags			auth
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		ChangePasswordRequest	true	"Current and new password"
//	@Success		200		{object}	utils.SuccessEnvelope{data=AuthResponse}
//	@Failure		401		{object}	utils.ErrorEnvelope
//	@Router			/auth/change-password [post]
func (h *AuthHandler) ChangePassword(c fiber.Ctx) error {
	userID, err := middleware.UserID(c)
	if err != nil {
		return err
	}

	var req ChangePasswordRequest
	if err := h.bind(c, &req); err != nil {
		return err
	}

	result, err := h.auth.ChangePassword(c.Context(), userID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		return err
	}

	h.setRefreshCookie(c, result)
	return utils.OK(c, toAuthResponse(result))
}

// ForgotPassword emails a reset link.
//
//	@Summary		Request a password reset
//	@Description	Always responds 200 so the endpoint cannot be used to discover registered addresses.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		ForgotPasswordRequest	true	"Account email"
//	@Success		200		{object}	utils.SuccessEnvelope
//	@Router			/auth/forgot-password [post]
func (h *AuthHandler) ForgotPassword(c fiber.Ctx) error {
	var req ForgotPasswordRequest
	if err := h.bind(c, &req); err != nil {
		return err
	}

	if err := h.auth.ForgotPassword(c.Context(), req.Email); err != nil {
		return err
	}

	return utils.Message(c, fiber.StatusOK,
		"If an account exists for that address, a reset link has been sent")
}

// ResetPassword consumes a reset token.
//
//	@Summary		Reset a password
//	@Description	Consumes a single-use reset token and revokes every existing session.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		ResetPasswordRequest	true	"Reset token and new password"
//	@Success		200		{object}	utils.SuccessEnvelope
//	@Failure		400		{object}	utils.ErrorEnvelope
//	@Router			/auth/reset-password [post]
func (h *AuthHandler) ResetPassword(c fiber.Ctx) error {
	var req ResetPasswordRequest
	if err := h.bind(c, &req); err != nil {
		return err
	}

	if err := h.auth.ResetPassword(c.Context(), req.Token, req.NewPassword); err != nil {
		return err
	}

	h.clearRefreshCookie(c)
	return utils.Message(c, fiber.StatusOK, "Password updated, please sign in again")
}

// setRefreshCookie mirrors the refresh token into an HttpOnly cookie when the
// feature is enabled.
func (h *AuthHandler) setRefreshCookie(c fiber.Ctx, result *service.AuthResult) {
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
