package models

import (
	"time"

	"github.com/google/uuid"
)

// Provider identifies an external identity provider.
type Provider string

const (
	ProviderGoogle Provider = "google"
)

// Valid reports whether the provider is one this build supports.
func (p Provider) Valid() bool {
	return p == ProviderGoogle
}

// OAuthAccount links a local user to an identity at an external provider.
type OAuthAccount struct {
	ID                uuid.UUID `json:"id"`
	UserID            uuid.UUID `json:"user_id"`
	Provider          Provider  `json:"provider"`
	ProviderAccountID string    `json:"-"` // never leaves the server
	Email             *string   `json:"email,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// ExternalIdentity is the normalised profile a provider hands back after a
// successful exchange. Every provider is mapped into this shape so the service
// layer never learns provider-specific field names.
type ExternalIdentity struct {
	Provider      Provider
	Subject       string // stable id at the provider
	Email         string
	EmailVerified bool
	FullName      string
	AvatarURL     string
}
