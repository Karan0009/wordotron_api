package utils

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/Karan0009/wordotron_api/internal/models"
	"github.com/Karan0009/wordotron_api/pkg/apperror"
)

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

	var fields []apperror.FieldError

	if params.Page < 1 {
		fields = append(fields, apperror.FieldError{Field: "page", Message: "must be 1 or greater"})
	}
	if params.Limit < 1 || params.Limit > models.MaxLimit {
		fields = append(fields, apperror.FieldError{
			Field:   "limit",
			Message: "must be between 1 and 100",
		})
	}
	if !whitelist.Contains(params.Sort) {
		fields = append(fields, apperror.FieldError{
			Field:   "sort",
			Message: "must be one of: " + strings.Join(whitelist.Allowed, ", "),
		})
	}
	if !params.Order.Valid() {
		fields = append(fields, apperror.FieldError{Field: "order", Message: "must be asc or desc"})
	}
	if len(params.Search) > 128 {
		fields = append(fields, apperror.FieldError{Field: "search", Message: "must be at most 128 characters"})
	}

	if len(fields) > 0 {
		return models.PageParams{}, apperror.Validation(fields)
	}

	return params.Normalize(whitelist.Default), nil
}
