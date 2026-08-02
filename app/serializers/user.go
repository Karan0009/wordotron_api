package serializers

import (
	"time"

	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/app/models"
)

// ---------------------------------------------------------------------------
// Requests
// ---------------------------------------------------------------------------

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

func ToUserResponse(u *models.User) UserResponse {
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

func ToUserResponses(users []models.User) []UserResponse {
	out := make([]UserResponse, 0, len(users))
	for i := range users {
		out = append(out, ToUserResponse(&users[i]))
	}
	return out
}
