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

// EmailVerificationRepository persists single-use email verification tokens.
type EmailVerificationRepository interface {
	Create(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) (*models.EmailVerificationToken, error)
	GetByTokenHash(ctx context.Context, tokenHash string) (*models.EmailVerificationToken, error)
	MarkUsed(ctx context.Context, id uuid.UUID) error
	InvalidateForUser(ctx context.Context, userID uuid.UUID) error
	DeleteExpired(ctx context.Context) (int64, error)
}

type emailVerificationRepository struct {
	q db.Querier
}

var _ EmailVerificationRepository = (*emailVerificationRepository)(nil)

func (r *emailVerificationRepository) Create(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) (*models.EmailVerificationToken, error) {
	row, err := r.q.CreateEmailVerificationToken(ctx, db.CreateEmailVerificationTokenParams{
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, lib.Internal(fmt.Errorf("create email verification token: %w", err))
	}
	return toDomainVerificationToken(row), nil
}

func (r *emailVerificationRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*models.EmailVerificationToken, error) {
	row, err := r.q.GetEmailVerificationToken(ctx, tokenHash)
	if err != nil {
		if lib.IsNoRows(err) {
			return nil, lib.NotFound("Verification token").Wrap(err)
		}
		return nil, lib.Internal(fmt.Errorf("get email verification token: %w", err))
	}
	return toDomainVerificationToken(row), nil
}

// MarkUsed consumes a token. A zero row count means it was already used, which
// is reported as a conflict so a replayed link cannot verify twice.
func (r *emailVerificationRepository) MarkUsed(ctx context.Context, id uuid.UUID) error {
	affected, err := r.q.MarkEmailVerificationTokenUsed(ctx, id)
	if err != nil {
		return lib.Internal(fmt.Errorf("mark verification token used: %w", err))
	}
	if affected == 0 {
		return lib.Conflict("This verification link has already been used")
	}
	return nil
}

func (r *emailVerificationRepository) InvalidateForUser(ctx context.Context, userID uuid.UUID) error {
	if err := r.q.InvalidateUserEmailVerificationTokens(ctx, userID); err != nil {
		return lib.Internal(fmt.Errorf("invalidate verification tokens: %w", err))
	}
	return nil
}

func (r *emailVerificationRepository) DeleteExpired(ctx context.Context) (int64, error) {
	deleted, err := r.q.DeleteExpiredEmailVerificationTokens(ctx)
	if err != nil {
		return 0, lib.Internal(fmt.Errorf("delete expired verification tokens: %w", err))
	}
	return deleted, nil
}

func toDomainVerificationToken(row db.EmailVerificationToken) *models.EmailVerificationToken {
	return &models.EmailVerificationToken{
		ID:        row.ID,
		UserID:    row.UserID,
		TokenHash: row.TokenHash,
		ExpiresAt: row.ExpiresAt,
		UsedAt:    row.UsedAt,
		CreatedAt: row.CreatedAt,
	}
}
