package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Karan0009/wordotron_api/internal/auth"
	"github.com/Karan0009/wordotron_api/internal/config"
	"github.com/Karan0009/wordotron_api/internal/models"
)

func testAuthConfig() config.Auth {
	return config.Auth{
		JWTSecret:     "test-access-secret-that-is-long-enough-32",
		RefreshSecret: "test-refresh-secret-that-is-long-enough-32",
		JWTExpiry:     15 * time.Minute,
		RefreshExpiry: 24 * time.Hour,
		Issuer:        "test-issuer",
	}
}

func TestJWTManager_GenerateAndParse(t *testing.T) {
	t.Parallel()

	manager := auth.NewJWTManager(testAuthConfig())
	userID := uuid.New()

	pair, err := manager.GenerateTokenPair(userID, models.RoleAdmin)
	require.NoError(t, err)
	require.NotEmpty(t, pair.AccessToken)
	require.NotEmpty(t, pair.RefreshToken)
	require.NotEqual(t, pair.AccessToken, pair.RefreshToken)
	require.Equal(t, "Bearer", pair.TokenType)
	require.Equal(t, int64(900), pair.ExpiresIn)

	claims, err := manager.ParseAccessToken(pair.AccessToken)
	require.NoError(t, err)

	parsedID, err := claims.UserID()
	require.NoError(t, err)
	require.Equal(t, userID, parsedID)
	require.Equal(t, models.RoleAdmin, claims.Role)
	require.Equal(t, auth.TokenTypeAccess, claims.TokenType)
	require.Equal(t, pair.AccessJTI, claims.ID)
}

func TestJWTManager_RefreshTokenIsNotAcceptedAsAccessToken(t *testing.T) {
	t.Parallel()

	manager := auth.NewJWTManager(testAuthConfig())

	pair, err := manager.GenerateTokenPair(uuid.New(), models.RoleUser)
	require.NoError(t, err)

	// Separate secrets plus the typ claim make this a hard failure.
	_, err = manager.ParseAccessToken(pair.RefreshToken)
	require.Error(t, err)
}

func TestJWTManager_RejectsTokenSignedWithAnotherSecret(t *testing.T) {
	t.Parallel()

	issuer := auth.NewJWTManager(testAuthConfig())

	otherCfg := testAuthConfig()
	otherCfg.JWTSecret = "a-completely-different-secret-value-32ch"
	verifier := auth.NewJWTManager(otherCfg)

	pair, err := issuer.GenerateTokenPair(uuid.New(), models.RoleUser)
	require.NoError(t, err)

	_, err = verifier.ParseAccessToken(pair.AccessToken)
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestJWTManager_RejectsWrongIssuer(t *testing.T) {
	t.Parallel()

	issuerCfg := testAuthConfig()
	issuerCfg.Issuer = "someone-else"

	pair, err := auth.NewJWTManager(issuerCfg).GenerateTokenPair(uuid.New(), models.RoleUser)
	require.NoError(t, err)

	_, err = auth.NewJWTManager(testAuthConfig()).ParseAccessToken(pair.AccessToken)
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestJWTManager_RejectsExpiredToken(t *testing.T) {
	t.Parallel()

	cfg := testAuthConfig()
	cfg.JWTExpiry = time.Millisecond
	manager := auth.NewJWTManager(cfg)

	pair, err := manager.GenerateTokenPair(uuid.New(), models.RoleUser)
	require.NoError(t, err)

	// jwt applies a small clock skew allowance, so sleep past it.
	time.Sleep(1100 * time.Millisecond)

	_, err = manager.ParseAccessToken(pair.AccessToken)
	require.ErrorIs(t, err, auth.ErrExpiredToken)
}

func TestJWTManager_RejectsGarbage(t *testing.T) {
	t.Parallel()

	manager := auth.NewJWTManager(testAuthConfig())

	for _, token := range []string{"", "abc", "a.b.c", "Bearer x.y.z"} {
		_, err := manager.ParseAccessToken(token)
		require.Error(t, err, "token %q must be rejected", token)
	}
}

func TestJWTManager_EachPairHasUniqueJTIs(t *testing.T) {
	t.Parallel()

	manager := auth.NewJWTManager(testAuthConfig())
	userID := uuid.New()

	first, err := manager.GenerateTokenPair(userID, models.RoleUser)
	require.NoError(t, err)
	second, err := manager.GenerateTokenPair(userID, models.RoleUser)
	require.NoError(t, err)

	require.NotEqual(t, first.AccessJTI, second.AccessJTI)
	require.NotEqual(t, first.RefreshJTI, second.RefreshJTI)
}
