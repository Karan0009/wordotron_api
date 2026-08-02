// Package models holds the domain types shared by the service, handler and
// repository layers. They are storage-agnostic: nothing here knows about SQL
// column types or HTTP.
package models

import (
	"time"

	"github.com/google/uuid"
)

// Role enumerates the authorisation levels understood by the API.
type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

// Valid reports whether r is a role the system recognises.
func (r Role) Valid() bool {
	switch r {
	case RoleUser, RoleAdmin:
		return true
	default:
		return false
	}
}

func (r Role) String() string { return string(r) }

// User is the domain representation of an account. PasswordHash is unexported
// from JSON so it cannot leak through a handler that forgets to map to a DTO.
type User struct {
	ID              uuid.UUID  `json:"id"`
	Email           string     `json:"email"`
	FullName        string     `json:"full_name"`
	Role            Role       `json:"role"`
	AvatarURL       *string    `json:"avatar_url"`
	IsActive        bool       `json:"is_active"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`

	PasswordHash string `json:"-"`
}

// IsAdmin reports whether the user holds the admin role.
func (u *User) IsAdmin() bool { return u.Role == RoleAdmin }

// CreateUserInput is the repository-facing payload for account creation.
type CreateUserInput struct {
	Email        string
	PasswordHash string
	FullName     string
	Role         Role
}

// UpdateUserInput applies partial updates; nil fields are left untouched.
type UpdateUserInput struct {
	FullName  *string
	AvatarURL *string
	Role      *Role
	IsActive  *bool
}

// ListUsersFilter combines pagination with the user-specific filters.
type ListUsersFilter struct {
	Page     PageParams
	Role     *Role
	IsActive *bool
}
