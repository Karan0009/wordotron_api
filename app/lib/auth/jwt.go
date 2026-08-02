package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/app/config"
	"github.com/Karan0009/wordotron_api/app/models"
)

// Token-related errors surfaced to the service layer.
var (
	ErrInvalidToken = errors.New("auth: invalid token")
	ErrExpiredToken = errors.New("auth: token expired")
	ErrWrongType    = errors.New("auth: wrong token type")
)

// TokenType distinguishes access tokens from refresh tokens so a refresh token
// can never be replayed as a bearer credential.
type TokenType string

const (
	TokenTypeAccess  TokenType = "access"
	TokenTypeRefresh TokenType = "refresh"
)

// Claims is the JWT payload. Subject holds the user ID and ID holds the JTI
// used for revocation lookups.
type Claims struct {
	jwt.RegisteredClaims
	Role      models.Role `json:"role"`
	TokenType TokenType   `json:"typ"`
}

// UserID parses the subject claim.
func (c *Claims) UserID() (uuid.UUID, error) {
	id, err := uuid.Parse(c.Subject)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: subject is not a uuid", ErrInvalidToken)
	}
	return id, nil
}

// IssuedAtTime returns the iat claim as a time.Time.
func (c *Claims) IssuedAtTime() time.Time {
	if c.IssuedAt == nil {
		return time.Time{}
	}
	return c.IssuedAt.Time
}

// TokenPair is what the API hands back after a successful authentication. The
// JTI and expiry fields are needed by the session store but never serialised.
type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int64     `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`

	AccessJTI        string    `json:"-"`
	RefreshJTI       string    `json:"-"`
	RefreshExpiresAt time.Time `json:"-"`
}

// TokenManager issues and verifies JWTs.
type TokenManager interface {
	GenerateTokenPair(userID uuid.UUID, role models.Role) (TokenPair, error)
	ParseAccessToken(token string) (*Claims, error)
	ParseRefreshToken(token string) (*Claims, error)
	AccessTTL() time.Duration
	RefreshTTL() time.Duration
}

// JWTManager is the HMAC-SHA256 implementation of TokenManager. Access and
// refresh tokens are signed with separate secrets so leaking one does not
// compromise the other.
type JWTManager struct {
	accessSecret  []byte
	refreshSecret []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
	issuer        string

	// now is injectable for deterministic tests.
	now func() time.Time
}

var _ TokenManager = (*JWTManager)(nil)

// NewJWTManager builds a manager from the auth configuration.
func NewJWTManager(cfg config.Auth) *JWTManager {
	return &JWTManager{
		accessSecret:  []byte(cfg.JWTSecret),
		refreshSecret: []byte(cfg.RefreshSecret),
		accessTTL:     cfg.JWTExpiry,
		refreshTTL:    cfg.RefreshExpiry,
		issuer:        cfg.Issuer,
		now:           time.Now,
	}
}

func (m *JWTManager) AccessTTL() time.Duration  { return m.accessTTL }
func (m *JWTManager) RefreshTTL() time.Duration { return m.refreshTTL }

// GenerateTokenPair mints a fresh access/refresh pair for a user.
func (m *JWTManager) GenerateTokenPair(userID uuid.UUID, role models.Role) (TokenPair, error) {
	issuedAt := m.now().UTC()
	accessExpiry := issuedAt.Add(m.accessTTL)
	refreshExpiry := issuedAt.Add(m.refreshTTL)

	accessJTI := uuid.NewString()
	refreshJTI := uuid.NewString()

	accessToken, err := m.sign(m.accessSecret, Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        accessJTI,
			Subject:   userID.String(),
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			NotBefore: jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(accessExpiry),
		},
		Role:      role,
		TokenType: TokenTypeAccess,
	})
	if err != nil {
		return TokenPair{}, err
	}

	refreshToken, err := m.sign(m.refreshSecret, Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        refreshJTI,
			Subject:   userID.String(),
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			NotBefore: jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(refreshExpiry),
		},
		Role:      role,
		TokenType: TokenTypeRefresh,
	})
	if err != nil {
		return TokenPair{}, err
	}

	return TokenPair{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		TokenType:        "Bearer",
		ExpiresIn:        int64(m.accessTTL.Seconds()),
		ExpiresAt:        accessExpiry,
		AccessJTI:        accessJTI,
		RefreshJTI:       refreshJTI,
		RefreshExpiresAt: refreshExpiry,
	}, nil
}

// ParseAccessToken verifies an access token and returns its claims.
func (m *JWTManager) ParseAccessToken(token string) (*Claims, error) {
	return m.parse(token, m.accessSecret, TokenTypeAccess)
}

// ParseRefreshToken verifies a refresh token and returns its claims.
func (m *JWTManager) ParseRefreshToken(token string) (*Claims, error) {
	return m.parse(token, m.refreshSecret, TokenTypeRefresh)
}

func (m *JWTManager) sign(secret []byte, claims Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("auth: sign token: %w", err)
	}
	return signed, nil
}

func (m *JWTManager) parse(raw string, secret []byte, want TokenType) (*Claims, error) {
	claims := &Claims{}

	_, err := jwt.ParseWithClaims(raw, claims,
		func(t *jwt.Token) (any, error) {
			// Pin the algorithm: without this check a token signed with "none"
			// or an asymmetric key confusion trick could be accepted.
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("%w: unexpected signing method %v", ErrInvalidToken, t.Header["alg"])
			}
			return secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return m.now() }),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, fmt.Errorf("%w: %s", ErrInvalidToken, err.Error())
	}

	if claims.TokenType != want {
		return nil, ErrWrongType
	}
	if claims.ID == "" {
		return nil, fmt.Errorf("%w: missing jti", ErrInvalidToken)
	}
	if _, err := claims.UserID(); err != nil {
		return nil, err
	}
	if !claims.Role.Valid() {
		return nil, fmt.Errorf("%w: unknown role", ErrInvalidToken)
	}

	return claims, nil
}
