package auth_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/Karan0009/wordotron_api/app/lib/auth"
)

// testCost keeps the suite fast; production uses the configured cost.
const testCost = bcrypt.MinCost

func newHasher(t *testing.T) *auth.BcryptHasher {
	t.Helper()
	hasher, err := auth.NewBcryptHasher(testCost)
	require.NoError(t, err)
	return hasher
}

func TestBcryptHasher_HashAndVerify(t *testing.T) {
	t.Parallel()

	hasher := newHasher(t)
	const password = "Sup3rSecretPassword"

	hash, err := hasher.Hash(password)
	require.NoError(t, err)
	require.NotEqual(t, password, hash, "the plaintext must never be stored")

	require.NoError(t, hasher.Verify(hash, password))
}

func TestBcryptHasher_VerifyRejectsWrongPassword(t *testing.T) {
	t.Parallel()

	hasher := newHasher(t)

	hash, err := hasher.Hash("Sup3rSecretPassword")
	require.NoError(t, err)

	err = hasher.Verify(hash, "not-the-password")
	require.ErrorIs(t, err, auth.ErrPasswordMismatch)
}

func TestBcryptHasher_HashIsSalted(t *testing.T) {
	t.Parallel()

	hasher := newHasher(t)

	first, err := hasher.Hash("same-password")
	require.NoError(t, err)
	second, err := hasher.Hash("same-password")
	require.NoError(t, err)

	require.NotEqual(t, first, second, "equal passwords must produce different hashes")
}

func TestBcryptHasher_RejectsOverlongPassword(t *testing.T) {
	t.Parallel()

	hasher := newHasher(t)

	// bcrypt silently truncates past 72 bytes, so we reject instead.
	_, err := hasher.Hash(strings.Repeat("a", 73))
	require.Error(t, err)
}

func TestNewBcryptHasher_RejectsInvalidCost(t *testing.T) {
	t.Parallel()

	_, err := auth.NewBcryptHasher(3)
	require.Error(t, err)

	_, err = auth.NewBcryptHasher(40)
	require.Error(t, err)
}

func TestBcryptHasher_VerifyRejectsMalformedHash(t *testing.T) {
	t.Parallel()

	hasher := newHasher(t)

	err := hasher.Verify("not-a-bcrypt-hash", "whatever")
	require.Error(t, err)
	require.False(t, errors.Is(err, auth.ErrPasswordMismatch),
		"a malformed hash is an operational error, not a failed comparison")
}

func TestBcryptHasher_NeedsRehash(t *testing.T) {
	t.Parallel()

	weak, err := auth.NewBcryptHasher(bcrypt.MinCost)
	require.NoError(t, err)
	strong, err := auth.NewBcryptHasher(bcrypt.MinCost + 2)
	require.NoError(t, err)

	hash, err := weak.Hash("password")
	require.NoError(t, err)

	require.True(t, strong.NeedsRehash(hash))
	require.False(t, weak.NeedsRehash(hash))
}
