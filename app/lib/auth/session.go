package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Redis key layout:
//
//	auth:sessions:<user_id>   HASH   jti -> refresh expiry (unix seconds)
//	auth:revoked:<user_id>    STRING revocation epoch (unix seconds)
//	auth:blacklist:<jti>      STRING single access token revoked before expiry
//
// Sessions are stored per user rather than per token so "log out everywhere"
// is a single DEL instead of a scan.
const (
	sessionsKeyPrefix  = "auth:sessions:"
	revokedKeyPrefix   = "auth:revoked:"
	blacklistKeyPrefix = "auth:blacklist:"
)

// SessionStore tracks which refresh tokens are still usable and which access
// tokens have been revoked before their natural expiry.
type SessionStore interface {
	// StoreRefreshToken records a newly issued refresh token.
	StoreRefreshToken(ctx context.Context, userID uuid.UUID, jti string, expiresAt time.Time) error
	// IsRefreshTokenActive reports whether the token is still registered.
	IsRefreshTokenActive(ctx context.Context, userID uuid.UUID, jti string) (bool, error)
	// RevokeRefreshToken removes a single refresh token (rotation, logout).
	RevokeRefreshToken(ctx context.Context, userID uuid.UUID, jti string) error
	// RevokeAllSessions invalidates every refresh token for the user and marks
	// a revocation epoch so outstanding access tokens stop working too.
	RevokeAllSessions(ctx context.Context, userID uuid.UUID) error
	// BlacklistAccessToken revokes one access token until it expires anyway.
	BlacklistAccessToken(ctx context.Context, jti string, ttl time.Duration) error
	// IsAccessTokenValid checks the blacklist and the revocation epoch in a
	// single round trip.
	IsAccessTokenValid(ctx context.Context, userID uuid.UUID, jti string, issuedAt time.Time) (bool, error)
	// ActiveSessionCount returns how many refresh tokens the user holds.
	ActiveSessionCount(ctx context.Context, userID uuid.UUID) (int64, error)
}

// RedisSessionStore is the Redis-backed SessionStore.
type RedisSessionStore struct {
	client     redis.UniversalClient
	refreshTTL time.Duration
	now        func() time.Time
}

var _ SessionStore = (*RedisSessionStore)(nil)

// NewRedisSessionStore builds a store. refreshTTL bounds how long per-user
// keys are kept alive.
func NewRedisSessionStore(client redis.UniversalClient, refreshTTL time.Duration) *RedisSessionStore {
	return &RedisSessionStore{client: client, refreshTTL: refreshTTL, now: time.Now}
}

func sessionsKey(userID uuid.UUID) string { return sessionsKeyPrefix + userID.String() }
func revokedKey(userID uuid.UUID) string  { return revokedKeyPrefix + userID.String() }
func blacklistKey(jti string) string      { return blacklistKeyPrefix + jti }

func (s *RedisSessionStore) StoreRefreshToken(ctx context.Context, userID uuid.UUID, jti string, expiresAt time.Time) error {
	key := sessionsKey(userID)

	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, key, jti, strconv.FormatInt(expiresAt.Unix(), 10))
	// Refresh the key TTL on every write so an active user's session hash
	// never expires out from under them.
	pipe.Expire(ctx, key, s.refreshTTL+time.Hour)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("auth: store refresh token: %w", err)
	}
	return nil
}

func (s *RedisSessionStore) IsRefreshTokenActive(ctx context.Context, userID uuid.UUID, jti string) (bool, error) {
	raw, err := s.client.HGet(ctx, sessionsKey(userID), jti).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("auth: read refresh token: %w", err)
	}

	expiresAt, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		// Corrupt entry: treat as revoked and clean it up.
		_ = s.RevokeRefreshToken(ctx, userID, jti)
		return false, nil
	}

	if s.now().Unix() >= expiresAt {
		_ = s.RevokeRefreshToken(ctx, userID, jti)
		return false, nil
	}
	return true, nil
}

func (s *RedisSessionStore) RevokeRefreshToken(ctx context.Context, userID uuid.UUID, jti string) error {
	if err := s.client.HDel(ctx, sessionsKey(userID), jti).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("auth: revoke refresh token: %w", err)
	}
	return nil
}

func (s *RedisSessionStore) RevokeAllSessions(ctx context.Context, userID uuid.UUID) error {
	// One second of slack: JWT iat has second granularity, so without it a
	// replacement token minted in the same second as the revocation would be
	// rejected by its own epoch check.
	now := s.now().UTC().Add(-time.Second)

	pipe := s.client.TxPipeline()
	pipe.Del(ctx, sessionsKey(userID))
	pipe.Set(ctx, revokedKey(userID), strconv.FormatInt(now.Unix(), 10), s.refreshTTL+time.Hour)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("auth: revoke all sessions: %w", err)
	}
	return nil
}

func (s *RedisSessionStore) BlacklistAccessToken(ctx context.Context, jti string, ttl time.Duration) error {
	if ttl <= 0 {
		// Already expired; nothing to revoke.
		return nil
	}
	if err := s.client.Set(ctx, blacklistKey(jti), "1", ttl).Err(); err != nil {
		return fmt.Errorf("auth: blacklist access token: %w", err)
	}
	return nil
}

func (s *RedisSessionStore) IsAccessTokenValid(ctx context.Context, userID uuid.UUID, jti string, issuedAt time.Time) (bool, error) {
	pipe := s.client.Pipeline()
	blacklisted := pipe.Exists(ctx, blacklistKey(jti))
	revokedAt := pipe.Get(ctx, revokedKey(userID))

	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return false, fmt.Errorf("auth: validate access token: %w", err)
	}

	if blacklisted.Val() > 0 {
		return false, nil
	}

	raw, err := revokedAt.Result()
	if errors.Is(err, redis.Nil) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("auth: read revocation epoch: %w", err)
	}

	epoch, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return true, nil
	}

	// Tokens minted at or before the epoch second are rejected; the token was
	// issued in the same second the user revoked everything, so err safe.
	return issuedAt.Unix() > epoch, nil
}

func (s *RedisSessionStore) ActiveSessionCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	count, err := s.client.HLen(ctx, sessionsKey(userID)).Result()
	if err != nil {
		return 0, fmt.Errorf("auth: count sessions: %w", err)
	}
	return count, nil
}
