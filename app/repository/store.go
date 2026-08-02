// Package repository is the persistence boundary. Services depend on the
// interfaces declared here, never on sqlc-generated types, so the storage
// engine can change without rippling through business logic.
package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Karan0009/wordotron_api/app/lib"
	"github.com/Karan0009/wordotron_api/app/repository/db"
)

// Store aggregates the repositories and exposes transactional composition.
type Store interface {
	Users() UserRepository
	PasswordResets() PasswordResetRepository
	EmailVerifications() EmailVerificationRepository

	// WithTx runs fn against a Store bound to a single transaction. Returning
	// a non-nil error rolls back; returning nil commits.
	WithTx(ctx context.Context, fn func(Store) error) error
}

type pgStore struct {
	pool    *pgxpool.Pool
	queries *db.Queries

	users              UserRepository
	passwordResets     PasswordResetRepository
	emailVerifications EmailVerificationRepository
}

var _ Store = (*pgStore)(nil)

// NewStore wires the repositories on top of a pgx pool.
func NewStore(pool *pgxpool.Pool) Store {
	return newStore(pool, db.New(pool))
}

func newStore(pool *pgxpool.Pool, queries *db.Queries) *pgStore {
	return &pgStore{
		pool:               pool,
		queries:            queries,
		users:              &userRepository{q: queries},
		passwordResets:     &passwordResetRepository{q: queries},
		emailVerifications: &emailVerificationRepository{q: queries},
	}
}

func (s *pgStore) Users() UserRepository                           { return s.users }
func (s *pgStore) PasswordResets() PasswordResetRepository         { return s.passwordResets }
func (s *pgStore) EmailVerifications() EmailVerificationRepository { return s.emailVerifications }

func (s *pgStore) WithTx(ctx context.Context, fn func(Store) error) error {
	if s.pool == nil {
		return fmt.Errorf("repository: nested transactions are not supported")
	}

	return lib.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		// pool is nil inside the closure so an accidental nested WithTx fails
		// loudly instead of silently opening a second connection.
		txStore := newStore(nil, s.queries.WithTx(tx))
		return fn(txStore)
	})
}
