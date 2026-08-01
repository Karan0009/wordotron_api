package handlers

import (
	"time"

	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/internal/auth"
	"github.com/Karan0009/wordotron_api/internal/models"
	"github.com/Karan0009/wordotron_api/internal/service"
)

// ---------------------------------------------------------------------------
// Requests
// ---------------------------------------------------------------------------

// RegisterRequest is the body of POST /api/v1/auth/register.
type RegisterRequest struct {
	Email    string `json:"email"    validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,min=12,max=72,strongpassword"`
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

// CreateUserRequest is the admin-only body of POST /api/v1/users.
type CreateUserRequest struct {
	Email    string `json:"email"     validate:"required,email,max=254"`
	Password string `json:"password"  validate:"required,min=12,max=72,strongpassword"`
	FullName string `json:"full_name" validate:"required,min=2,max=120"`
	Role     string `json:"role"      validate:"omitempty,role"`
}

// UpdateUserRequest is a partial update. Pointers distinguish "absent" from
// "explicitly set to the zero value".
type UpdateUserRequest struct {
	FullName *string `json:"full_name" validate:"omitempty,min=2,max=120"`
	Role     *string `json:"role"      validate:"omitempty,role"`
	IsActive *bool   `json:"is_active"`
}

// ---------------------------------------------------------------------------
// Responses
// ---------------------------------------------------------------------------

// UserResponse is the public projection of a user. It exists so that adding a
// column to the table never silently widens the API surface.
type UserResponse struct {
	ID              uuid.UUID  `json:"id"`
	Email           string     `json:"email"`
	FullName        string     `json:"full_name"`
	Role            string     `json:"role"`
	AvatarURL       *string    `json:"avatar_url"`
	IsActive        bool       `json:"is_active"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

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

func toUserResponse(u *models.User) UserResponse {
	return UserResponse{
		ID:              u.ID,
		Email:           u.Email,
		FullName:        u.FullName,
		Role:            u.Role.String(),
		AvatarURL:       u.AvatarURL,
		IsActive:        u.IsActive,
		EmailVerifiedAt: u.EmailVerifiedAt,
		LastLoginAt:     u.LastLoginAt,
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

func toUserResponses(users []models.User) []UserResponse {
	out := make([]UserResponse, 0, len(users))
	for i := range users {
		out = append(out, toUserResponse(&users[i]))
	}
	return out
}

func toTokensResponse(pair auth.TokenPair) TokensResponse {
	return TokensResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		TokenType:    pair.TokenType,
		ExpiresIn:    pair.ExpiresIn,
		ExpiresAt:    pair.ExpiresAt,
	}
}

func toAuthResponse(result *service.AuthResult) AuthResponse {
	return AuthResponse{
		User:   toUserResponse(result.User),
		Tokens: toTokensResponse(result.Tokens),
	}
}
