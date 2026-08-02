package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/app/lib"
	"github.com/Karan0009/wordotron_api/app/models"
	"github.com/Karan0009/wordotron_api/app/repository/db"
)

// PasswordResetRepository persists single-use password reset tokens.
type PasswordResetRepository interface {
	Create(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) (*models.PasswordResetToken, error)
	GetByTokenHash(ctx context.Context, tokenHash string) (*models.PasswordResetToken, error)
	MarkUsed(ctx context.Context, id uuid.UUID) error
	InvalidateForUser(ctx context.Context, userID uuid.UUID) error
	DeleteExpired(ctx context.Context) (int64, error)
}

type passwordResetRepository struct {
	q db.Querier
}

var _ PasswordResetRepository = (*passwordResetRepository)(nil)

func (r *passwordResetRepository) Create(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) (*models.PasswordResetToken, error) {
	row, err := r.q.CreatePasswordResetToken(ctx, db.CreatePasswordResetTokenParams{
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, lib.Internal(fmt.Errorf("create password reset token: %w", err))
	}
	return toDomainResetToken(row), nil
}

func (r *passwordResetRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*models.PasswordResetToken, error) {
	row, err := r.q.GetPasswordResetToken(ctx, tokenHash)
	if err != nil {
		if lib.IsNoRows(err) {
			return nil, lib.NotFound("Reset token").Wrap(err)
		}
		return nil, lib.Internal(fmt.Errorf("get password reset token: %w", err))
	}
	return toDomainResetToken(row), nil
}

// MarkUsed consumes a token. A zero row count means it was already used, which
// is reported as a conflict so a replayed link cannot reset a password twice.
func (r *passwordResetRepository) MarkUsed(ctx context.Context, id uuid.UUID) error {
	affected, err := r.q.MarkPasswordResetTokenUsed(ctx, id)
	if err != nil {
		return lib.Internal(fmt.Errorf("mark reset token used: %w", err))
	}
	if affected == 0 {
		return lib.Conflict("This reset link has already been used")
	}
	return nil
}

func (r *passwordResetRepository) InvalidateForUser(ctx context.Context, userID uuid.UUID) error {
	if err := r.q.InvalidateUserPasswordResetTokens(ctx, userID); err != nil {
		return lib.Internal(fmt.Errorf("invalidate reset tokens: %w", err))
	}
	return nil
}

func (r *passwordResetRepository) DeleteExpired(ctx context.Context) (int64, error) {
	deleted, err := r.q.DeleteExpiredPasswordResetTokens(ctx)
	if err != nil {
		return 0, lib.Internal(fmt.Errorf("delete expired reset tokens: %w", err))
	}
	return deleted, nil
}

func toDomainResetToken(row db.PasswordResetToken) *models.PasswordResetToken {
	return &models.PasswordResetToken{
		ID:        row.ID,
		UserID:    row.UserID,
		TokenHash: row.TokenHash,
		ExpiresAt: row.ExpiresAt,
		UsedAt:    row.UsedAt,
		CreatedAt: row.CreatedAt,
	}
}
