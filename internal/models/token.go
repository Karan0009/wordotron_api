package models

import (
	"time"

	"github.com/google/uuid"
)

// PasswordResetToken is the domain view of a single-use reset token. Only the
// hash is ever persisted; the plaintext exists just long enough to be emailed.
type PasswordResetToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// IsUsable reports whether the token is unused and unexpired at now.
func (t *PasswordResetToken) IsUsable(now time.Time) bool {
	return t.UsedAt == nil && now.Before(t.ExpiresAt)
}
