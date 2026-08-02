package serializers

import (
	"time"

	"github.com/Karan0009/wordotron_api/app/lib/auth"
	"github.com/Karan0009/wordotron_api/app/services"
)

// ---------------------------------------------------------------------------
// Requests
// ---------------------------------------------------------------------------

// RegisterRequest is the body of POST /api/v1/auth/register.
type RegisterRequest struct {
	Email    string `json:"email"    validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,min=6,max=15,strongpassword"`
	FullName string `json:"full_name" validate:"required,min=2,max=120"`
}

// LoginRequest is the body of POST /api/v1/auth/login.
type LoginRequest struct {
	Email    string `json:"email"    validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,max=72"`
}

// RefreshRequest is the body of POST /api/v1/auth/refresh. The token may also
// arrive in the refresh cookie, in which case the field is optional.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// LogoutRequest is the body of POST /api/v1/auth/logout.
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// ChangePasswordRequest is the body of POST /api/v1/auth/change-password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" validate:"required,max=72"`
	NewPassword     string `json:"new_password"     validate:"required,min=12,max=72,strongpassword"`
}

// ForgotPasswordRequest is the body of POST /api/v1/auth/forgot-password.
type ForgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email,max=254"`
}

// ResetPasswordRequest is the body of POST /api/v1/auth/reset-password.
type ResetPasswordRequest struct {
	Token       string `json:"token"        validate:"required,min=16,max=256"`
	NewPassword string `json:"new_password" validate:"required,min=12,max=72,strongpassword"`
}

// VerifyEmailRequest is the body of POST /api/v1/auth/verify-email.
type VerifyEmailRequest struct {
	Token string `json:"token" validate:"required,min=16,max=256"`
}

// ---------------------------------------------------------------------------
// Responses
// ---------------------------------------------------------------------------

// TokensResponse is the OAuth2-shaped token payload.
type TokensResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int64     `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// AuthResponse is returned by every endpoint that establishes a session.
type AuthResponse struct {
	User   UserResponse   `json:"user"`
	Tokens TokensResponse `json:"tokens"`
}

func ToTokensResponse(pair auth.TokenPair) TokensResponse {
	return TokensResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		TokenType:    pair.TokenType,
		ExpiresIn:    pair.ExpiresIn,
		ExpiresAt:    pair.ExpiresAt,
	}
}

func ToAuthResponse(result *services.AuthResult) AuthResponse {
	return AuthResponse{
		User:   ToUserResponse(result.User),
		Tokens: ToTokensResponse(result.Tokens),
	}
}
