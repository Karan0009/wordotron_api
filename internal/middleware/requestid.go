// Package middleware contains the cross-cutting HTTP concerns: correlation
// IDs, structured access logging, authentication, rate limiting and the
// centralised error handler.
package middleware

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/pkg/logger"
)

type ctxKey string

const (
	// RequestIDKey is the Locals key holding the correlation ID.
	RequestIDKey ctxKey = "request_id"
	// ClaimsKey is the Locals key holding the verified JWT claims.
	ClaimsKey ctxKey = "auth_claims"
)

// maxInboundRequestIDLen guards against a client stuffing an unbounded header
// value into every log line.
const maxInboundRequestIDLen = 64

// RequestID assigns a correlation ID to the request, echoes it back on the
// response and stores it in the request context so downstream logs pick it up
// automatically.
func RequestID() fiber.Handler {
	return func(c fiber.Ctx) error {
		id := c.Get(fiber.HeaderXRequestID)
		if id == "" || len(id) > maxInboundRequestIDLen {
			id = uuid.NewString()
		}

		c.Locals(RequestIDKey, id)
		c.Set(fiber.HeaderXRequestID, id)
		c.SetContext(logger.WithRequestID(c.Context(), id))

		return c.Next()
	}
}

// RequestIDFrom returns the correlation ID for the current request.
func RequestIDFrom(c fiber.Ctx) string {
	if id, ok := c.Locals(RequestIDKey).(string); ok {
		return id
	}
	return ""
}
