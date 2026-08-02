// Package lib owns the lifecycle of external data stores plus the app's
// small cross-cutting infra: db clients, errors, logging, HTTP helpers.
package lib

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Karan0009/wordotron_api/app/config"
)

// PostgresErrorCodes we translate into domain errors upstream.
const (
	PgUniqueViolation     = "23505"
	PgForeignKeyViolation = "23503"
	PgCheckViolation      = "23514"
)

// NewPostgres opens a pgx pool and verifies connectivity before returning.
func NewPostgres(ctx context.Context, cfg config.Database, log *slog.Logger) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolCfg.HealthCheckPeriod = time.Minute

	// Statement cache mode "describe" plays well with connection poolers such
	// as PgBouncer in transaction mode.
	poolCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheStatement

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	log.Info("connected to postgres",
		slog.Int("max_conns", int(poolCfg.MaxConns)),
		slog.Int("min_conns", int(poolCfg.MinConns)),
	)
	return pool, nil
}

// InTx runs fn inside a transaction, rolling back on error or panic. The
// rollback uses a fresh context so cancellation of the request does not leave
// the transaction dangling.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			rollback(tx)
			panic(p)
		}
		if err != nil {
			rollback(tx)
		}
	}()

	if err = fn(tx); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

// IsPgError reports whether err is a Postgres server error with the given code.
func IsPgError(err error, code string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == code
	}
	return false
}

// IsNoRows reports whether err represents an empty result set.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
