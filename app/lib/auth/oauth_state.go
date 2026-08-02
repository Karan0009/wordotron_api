package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ErrStateNotFound means the state or hand-off code was unknown, already used,
// or expired. All three are the same thing to a caller: start again.
var ErrStateNotFound = errors.New("auth: oauth state not found")

const (
	oauthStatePrefix    = "oauth:state:"
	oauthHandoffPrefix  = "oauth:handoff:"
	oauthStateTTL       = 10 * time.Minute
	oauthHandoffTTL     = 60 * time.Second
	maxReturnPathLength = 512
)

// OAuthState is what is remembered between sending the browser to the provider
// and it coming back.
type OAuthState struct {
	// CodeVerifier is the PKCE secret. It stays server-side for the whole flow.
	CodeVerifier string `json:"code_verifier"`
	// ReturnTo is the in-app path to land on afterwards.
	ReturnTo  string    `json:"return_to"`
	CreatedAt time.Time `json:"created_at"`
}

// OAuthStateStore holds the short-lived secrets of an in-flight sign-in.
//
// Redis rather than a cookie: the verifier must not be readable by the browser,
// and the one-time hand-off code has to be consumable exactly once across
// however many instances are running.
type OAuthStateStore interface {
	SaveState(ctx context.Context, state string, data OAuthState) error
	// ConsumeState returns the state and deletes it, so a replayed callback
	// cannot be exchanged twice.
	ConsumeState(ctx context.Context, state string) (*OAuthState, error)

	SaveHandoff(ctx context.Context, code string, userID uuid.UUID) error
	ConsumeHandoff(ctx context.Context, code string) (uuid.UUID, error)
}

// RedisOAuthStateStore is the Redis implementation.
type RedisOAuthStateStore struct {
	client redis.UniversalClient
	now    func() time.Time
}

var _ OAuthStateStore = (*RedisOAuthStateStore)(nil)

// NewRedisOAuthStateStore builds the store.
func NewRedisOAuthStateStore(client redis.UniversalClient) *RedisOAuthStateStore {
	return &RedisOAuthStateStore{client: client, now: time.Now}
}

func (s *RedisOAuthStateStore) SaveState(ctx context.Context, state string, data OAuthState) error {
	if len(data.ReturnTo) > maxReturnPathLength {
		data.ReturnTo = ""
	}
	data.CreatedAt = s.now().UTC()

	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode oauth state: %w", err)
	}

	if err := s.client.Set(ctx, oauthStatePrefix+state, encoded, oauthStateTTL).Err(); err != nil {
		return fmt.Errorf("save oauth state: %w", err)
	}
	return nil
}

func (s *RedisOAuthStateStore) ConsumeState(ctx context.Context, state string) (*OAuthState, error) {
	// GETDEL makes consumption atomic: two concurrent callbacks carrying the
	// same state cannot both succeed.
	raw, err := s.client.GetDel(ctx, oauthStatePrefix+state).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load oauth state: %w", err)
	}

	var data OAuthState
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("decode oauth state: %w", err)
	}
	return &data, nil
}

func (s *RedisOAuthStateStore) SaveHandoff(ctx context.Context, code string, userID uuid.UUID) error {
	if err := s.client.Set(ctx, oauthHandoffPrefix+code, userID.String(), oauthHandoffTTL).Err(); err != nil {
		return fmt.Errorf("save oauth handoff: %w", err)
	}
	return nil
}

func (s *RedisOAuthStateStore) ConsumeHandoff(ctx context.Context, code string) (uuid.UUID, error) {
	raw, err := s.client.GetDel(ctx, oauthHandoffPrefix+code).Result()
	if errors.Is(err, redis.Nil) {
		return uuid.Nil, ErrStateNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("load oauth handoff: %w", err)
	}

	userID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("decode oauth handoff: %w", err)
	}
	return userID, nil
}
