package middleware

import (
	"context"
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v3"

	"github.com/Karan0009/wordotron_api/internal/utils"
	"github.com/Karan0009/wordotron_api/pkg/apperror"
)

// ErrorHandler is the single place where an error becomes an HTTP response.
// Handlers and services return errors; nobody writes an error body by hand.
func ErrorHandler(log *slog.Logger) fiber.ErrorHandler {
	return func(c fiber.Ctx, err error) error {
		appErr := translate(err)

		attrs := []slog.Attr{
			slog.String("method", c.Method()),
			slog.String("path", c.Path()),
			slog.Int("status", appErr.Status),
			slog.String("code", string(appErr.Code)),
			slog.String("error", err.Error()),
		}

		if appErr.Status >= fiber.StatusInternalServerError {
			log.LogAttrs(c.Context(), slog.LevelError, "request failed", attrs...)
		} else {
			log.LogAttrs(c.Context(), slog.LevelDebug, "request rejected", attrs...)
		}

		return c.Status(appErr.Status).JSON(utils.ErrorEnvelope{
			Success:   false,
			Message:   appErr.Message,
			Code:      appErr.Code,
			Errors:    appErr.Fields,
			RequestID: RequestIDFrom(c),
		})
	}
}

// translate normalises framework and runtime errors into *apperror.Error so
// the response shape never depends on where the failure originated.
func translate(err error) *apperror.Error {
	if appErr, ok := apperror.As(err); ok {
		return appErr
	}

	// Fiber reports routing, body-limit and parser failures as *fiber.Error.
	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		switch fiberErr.Code {
		case fiber.StatusNotFound:
			return apperror.New(apperror.CodeNotFound, fiber.StatusNotFound, "The requested endpoint does not exist")
		case fiber.StatusMethodNotAllowed:
			return apperror.New(apperror.CodeBadRequest, fiber.StatusMethodNotAllowed, "Method not allowed for this endpoint")
		case fiber.StatusRequestEntityTooLarge:
			return apperror.PayloadTooLarge("")
		case fiber.StatusUnprocessableEntity, fiber.StatusBadRequest:
			return apperror.BadRequest("The request body could not be parsed")
		default:
			return apperror.New(apperror.CodeInternal, fiberErr.Code, fiberErr.Message).Wrap(err)
		}
	}

	// A cancelled client connection is not a server fault.
	if errors.Is(err, context.Canceled) {
		return apperror.New(apperror.CodeBadRequest, 499, "Client closed the request").Wrap(err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return apperror.New(apperror.CodeUnavailable, fiber.StatusGatewayTimeout, "The request timed out").Wrap(err)
	}

	return apperror.Internal(err)
}
