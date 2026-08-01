// Package utils contains the small HTTP helpers shared by every handler:
// the response envelope and query-parameter parsing.
package utils

import (
	"github.com/gofiber/fiber/v3"

	"github.com/Karan0009/wordotron_api/internal/models"
	"github.com/Karan0009/wordotron_api/pkg/apperror"
	"github.com/Karan0009/wordotron_api/pkg/logger"
)

// SuccessEnvelope is the shape of every 2xx response body.
type SuccessEnvelope struct {
	Success bool             `json:"success"`
	Data    any              `json:"data"`
	Meta    *models.PageMeta `json:"meta,omitempty"`
}

// ErrorEnvelope is the shape of every non-2xx response body.
type ErrorEnvelope struct {
	Success   bool                  `json:"success"`
	Message   string                `json:"message"`
	Code      apperror.Code         `json:"code,omitempty"`
	Errors    []apperror.FieldError `json:"errors,omitempty"`
	RequestID string                `json:"request_id,omitempty"`
}

// JSON writes a success envelope with an explicit status code.
func JSON(c fiber.Ctx, status int, data any) error {
	return c.Status(status).JSON(SuccessEnvelope{Success: true, Data: data})
}

// OK writes 200 with the given payload.
func OK(c fiber.Ctx, data any) error {
	return JSON(c, fiber.StatusOK, data)
}

// Created writes 201 with the given payload.
func Created(c fiber.Ctx, data any) error {
	return JSON(c, fiber.StatusCreated, data)
}

// Message writes a success envelope whose payload is a single message, used
// for operations that have nothing meaningful to return.
func Message(c fiber.Ctx, status int, message string) error {
	return JSON(c, status, fiber.Map{"message": message})
}

// Paginated writes a list payload plus its pagination metadata. The items live
// under "data" so clients can treat every success response identically.
func Paginated[T any](c fiber.Ctx, page models.Page[T]) error {
	meta := page.Meta
	items := page.Items
	if items == nil {
		items = []T{}
	}
	return c.Status(fiber.StatusOK).JSON(SuccessEnvelope{
		Success: true,
		Data:    items,
		Meta:    &meta,
	})
}

// RequestID returns the correlation ID assigned to the current request.
func RequestID(c fiber.Ctx) string {
	if id := logger.RequestIDFromContext(c.Context()); id != "" {
		return id
	}
	return c.Get(fiber.HeaderXRequestID)
}
