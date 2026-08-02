// utils.go contains the small HTTP helpers shared by every handler: the
// response envelope, query-parameter parsing and the sort allow-list guard.
package lib

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/Karan0009/wordotron_api/app/models"
)

// SuccessEnvelope is the shape of every 2xx response body.
type SuccessEnvelope struct {
	Success bool             `json:"success"`
	Data    any              `json:"data"`
	Meta    *models.PageMeta `json:"meta,omitempty"`
}

// ErrorEnvelope is the shape of every non-2xx response body.
type ErrorEnvelope struct {
	Success   bool         `json:"success"`
	Message   string       `json:"message"`
	Code      Code         `json:"code,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
	RequestID string       `json:"request_id,omitempty"`
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
	if id := RequestIDFromContext(c.Context()); id != "" {
		return id
	}
	return c.Get(fiber.HeaderXRequestID)
}

// SortWhitelist restricts which columns a client may sort by. Sorting is the
// classic injection vector in list endpoints, so the value is validated against
// an allow-list here and never interpolated into SQL.
type SortWhitelist struct {
	Allowed []string
	Default string
}

// Contains reports whether field is sortable.
func (w SortWhitelist) Contains(field string) bool {
	for _, allowed := range w.Allowed {
		if allowed == field {
			return true
		}
	}
	return false
}

// ParsePageParams reads page, limit, sort, order and search from the query
// string, validates them and returns normalised values.
//
//	?page=2&limit=25&sort=created_at&order=desc&search=jane
func ParsePageParams(c fiber.Ctx, whitelist SortWhitelist) (models.PageParams, error) {
	params := models.PageParams{
		Page:   fiber.Query(c, "page", models.DefaultPage),
		Limit:  fiber.Query(c, "limit", models.DefaultLimit),
		Sort:   strings.TrimSpace(fiber.Query(c, "sort", whitelist.Default)),
		Order:  models.SortOrder(strings.ToLower(strings.TrimSpace(fiber.Query(c, "order", string(models.SortDesc))))),
		Search: strings.TrimSpace(fiber.Query(c, "search", "")),
	}

	var fields []FieldError

	if params.Page < 1 {
		fields = append(fields, FieldError{Field: "page", Message: "must be 1 or greater"})
	}
	if params.Limit < 1 || params.Limit > models.MaxLimit {
		fields = append(fields, FieldError{
			Field:   "limit",
			Message: "must be between 1 and 100",
		})
	}
	if !whitelist.Contains(params.Sort) {
		fields = append(fields, FieldError{
			Field:   "sort",
			Message: "must be one of: " + strings.Join(whitelist.Allowed, ", "),
		})
	}
	if !params.Order.Valid() {
		fields = append(fields, FieldError{Field: "order", Message: "must be asc or desc"})
	}
	if len(params.Search) > 128 {
		fields = append(fields, FieldError{Field: "search", Message: "must be at most 128 characters"})
	}

	if len(fields) > 0 {
		return models.PageParams{}, Validation(fields)
	}

	return params.Normalize(whitelist.Default), nil
}
