package middleware

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/app/lib"
	"github.com/Karan0009/wordotron_api/app/lib/auth"
	"github.com/Karan0009/wordotron_api/app/models"
)

const bearerPrefix = "Bearer "

// Authenticate verifies the bearer token, checks it against the revocation
// state in Redis and stores the claims for downstream handlers.
func Authenticate(tokens auth.TokenManager, sessions auth.SessionStore) fiber.Handler {
	return func(c fiber.Ctx) error {
		raw, err := bearerToken(c)
		if err != nil {
			return err
		}

		claims, err := tokens.ParseAccessToken(raw)
		if err != nil {
			if errors.Is(err, auth.ErrExpiredToken) {
				return lib.Unauthorized("Access token has expired").Wrap(err)
			}
			return lib.Unauthorized("Invalid access token").Wrap(err)
		}

		userID, err := claims.UserID()
		if err != nil {
			return lib.Unauthorized("Invalid access token").Wrap(err)
		}

		// A token can be cryptographically valid but revoked: logout, password
		// change, role change or deactivation all invalidate it early.
		valid, err := sessions.IsAccessTokenValid(c.Context(), userID, claims.ID, claims.IssuedAtTime())
		if err != nil {
			return lib.Internal(err)
		}
		if !valid {
			return lib.Unauthorized("Session is no longer valid, please sign in again")
		}

		c.Locals(ClaimsKey, claims)
		return c.Next()
	}
}

// RequireRoles rejects callers whose role is not in the allow-list. It must be
// registered after Authenticate.
func RequireRoles(roles ...models.Role) fiber.Handler {
	allowed := make(map[models.Role]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}

	return func(c fiber.Ctx) error {
		claims, ok := ClaimsFrom(c)
		if !ok {
			return lib.Unauthorized("")
		}
		if _, permitted := allowed[claims.Role]; !permitted {
			return lib.Forbidden("")
		}
		return c.Next()
	}
}

// ClaimsFrom returns the verified claims attached by Authenticate.
func ClaimsFrom(c fiber.Ctx) (*auth.Claims, bool) {
	claims, ok := c.Locals(ClaimsKey).(*auth.Claims)
	return claims, ok
}

// UserID returns the authenticated user's ID.
func UserID(c fiber.Ctx) (uuid.UUID, error) {
	claims, ok := ClaimsFrom(c)
	if !ok {
		return uuid.Nil, lib.Unauthorized("")
	}
	return claims.UserID()
}

// UserIDString returns the authenticated user's ID, or "" when anonymous. It
// exists for logging, where an error would be noise.
func UserIDString(c fiber.Ctx) string {
	claims, ok := ClaimsFrom(c)
	if !ok {
		return ""
	}
	return claims.Subject
}

// UserRole returns the authenticated user's role.
func UserRole(c fiber.Ctx) models.Role {
	claims, ok := ClaimsFrom(c)
	if !ok {
		return ""
	}
	return claims.Role
}

func bearerToken(c fiber.Ctx) (string, error) {
	header := c.Get(fiber.HeaderAuthorization)
	if header == "" {
		return "", lib.Unauthorized("Authorization header is missing")
	}
	if len(header) <= len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return "", lib.Unauthorized("Authorization header must use the Bearer scheme")
	}

	token := strings.TrimSpace(header[len(bearerPrefix):])
	if token == "" {
		return "", lib.Unauthorized("Bearer token is empty")
	}
	return token, nil
}
