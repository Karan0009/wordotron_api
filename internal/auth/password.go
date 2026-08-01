// Package auth implements the credential primitives used by the service
// layer: password hashing, JWT issuance/verification and session state. It has
// no knowledge of HTTP, which keeps it reusable from workers and CLI tools.
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// ErrPasswordMismatch is returned when a plaintext password does not match a
// stored hash.
var ErrPasswordMismatch = errors.New("auth: password does not match")

// Hasher abstracts password hashing so the algorithm can be swapped (argon2id,
// for example) without touching the service layer.
type Hasher interface {
	Hash(plain string) (string, error)
	Verify(hash, plain string) error
	// DummyVerify burns roughly the same CPU as a real Verify. Login handlers
	// call it when the account does not exist so response time does not reveal
	// which emails are registered.
	DummyVerify()
}

// BcryptHasher is the default Hasher implementation.
type BcryptHasher struct {
	cost      int
	dummyHash []byte
}

var _ Hasher = (*BcryptHasher)(nil)

// NewBcryptHasher validates the cost factor and precomputes the hash used by
// DummyVerify. Constructing it is deliberately expensive (one bcrypt round);
// do it once at start-up.
func NewBcryptHasher(cost int) (*BcryptHasher, error) {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return nil, fmt.Errorf("auth: bcrypt cost %d out of range [%d,%d]", cost, bcrypt.MinCost, bcrypt.MaxCost)
	}

	filler := make([]byte, 24)
	if _, err := rand.Read(filler); err != nil {
		return nil, fmt.Errorf("auth: generate dummy secret: %w", err)
	}

	dummy, err := bcrypt.GenerateFromPassword([]byte(base64.RawStdEncoding.EncodeToString(filler)), cost)
	if err != nil {
		return nil, fmt.Errorf("auth: precompute dummy hash: %w", err)
	}

	return &BcryptHasher{cost: cost, dummyHash: dummy}, nil
}

// Hash returns the bcrypt hash of plain.
func (h *BcryptHasher) Hash(plain string) (string, error) {
	// bcrypt silently truncates beyond 72 bytes; reject instead of surprising
	// the caller with a password whose tail is ignored.
	if len(plain) > 72 {
		return "", errors.New("auth: password exceeds 72 bytes")
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(plain), h.cost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(hashed), nil
}

// Verify reports whether plain matches hash, returning ErrPasswordMismatch on
// a clean mismatch and a wrapped error for malformed hashes.
func (h *BcryptHasher) Verify(hash, plain string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
		return ErrPasswordMismatch
	default:
		return fmt.Errorf("auth: compare password: %w", err)
	}
}

// DummyVerify performs a throwaway comparison to equalise timing.
func (h *BcryptHasher) DummyVerify() {
	_ = bcrypt.CompareHashAndPassword(h.dummyHash, []byte("dummy-password"))
}

// NeedsRehash reports whether a stored hash was produced with a weaker cost
// than the current configuration, so it can be upgraded on next login.
func (h *BcryptHasher) NeedsRehash(hash string) bool {
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		return true
	}
	return cost < h.cost
}
