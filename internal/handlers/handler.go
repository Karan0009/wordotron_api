// Package handlers contains the HTTP transport layer. Handlers do three
// things: decode and validate input, delegate to a service, and encode the
// result. Business rules never live here.
package handlers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/internal/middleware"
	"github.com/Karan0009/wordotron_api/internal/service"
	"github.com/Karan0009/wordotron_api/internal/validation"
	"github.com/Karan0009/wordotron_api/pkg/apperror"
)

// base carries the dependencies every handler needs.
type base struct {
	validator *validation.Validator
}

// bind decodes the JSON body into out and validates it.
func (b *base) bind(c fiber.Ctx, out any) error {
	if err := c.Bind().Body(out); err != nil {
		return apperror.BadRequest("The request body is not valid JSON").Wrap(err)
	}
	return b.validator.Struct(out)
}

// uuidParam reads a UUID route parameter, e.g. /users/:id.
func uuidParam(c fiber.Ctx, name string) (uuid.UUID, error) {
	raw := c.Params(name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.BadRequest("Invalid identifier").
			WithFields(apperror.FieldError{Field: name, Message: "Must be a valid UUID"}).Wrap(err)
	}
	return id, nil
}

// actorFrom builds the service-layer caller identity from the verified claims.
func actorFrom(c fiber.Ctx) (service.Actor, error) {
	claims, ok := middleware.ClaimsFrom(c)
	if !ok {
		return service.Actor{}, apperror.Unauthorized("")
	}
	id, err := claims.UserID()
	if err != nil {
		return service.Actor{}, apperror.Unauthorized("").Wrap(err)
	}
	return service.Actor{ID: id, Role: claims.Role}, nil
}
