// Package apperror provides the single error type that travels from the
// repository layer up to the HTTP boundary, where it is rendered into the
// standard error envelope. Transport packages are deliberately not imported
// here so the same errors can be reused by workers or CLI entry points.
package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

// Code is a stable, machine-readable identifier that clients can branch on
// without parsing human-facing messages.
type Code string

const (
	CodeBadRequest      Code = "BAD_REQUEST"
	CodeValidation      Code = "VALIDATION_ERROR"
	CodeUnauthorized    Code = "UNAUTHORIZED"
	CodeForbidden       Code = "FORBIDDEN"
	CodeNotFound        Code = "NOT_FOUND"
	CodeConflict        Code = "CONFLICT"
	CodeTooManyRequests Code = "TOO_MANY_REQUESTS"
	CodePayloadTooLarge Code = "PAYLOAD_TOO_LARGE"
	CodeUnsupportedType Code = "UNSUPPORTED_MEDIA_TYPE"
	CodeInternal        Code = "INTERNAL_ERROR"
	CodeUnavailable     Code = "SERVICE_UNAVAILABLE"
)

// FieldError describes a single invalid input field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error is an application error carrying everything the HTTP layer needs to
// build a response, plus an optional wrapped cause that is logged but never
// serialised to the client.
type Error struct {
	Code    Code
	Status  int
	Message string
	Fields  []FieldError

	cause error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.cause }

// Wrap attaches an underlying cause and returns a copy, keeping *Error values
// safe to declare as package-level sentinels.
func (e *Error) Wrap(err error) *Error {
	clone := *e
	clone.cause = err
	return &clone
}

// WithFields attaches field-level details and returns a copy.
func (e *Error) WithFields(fields ...FieldError) *Error {
	clone := *e
	clone.Fields = fields
	return &clone
}

// WithMessage overrides the client-facing message and returns a copy.
func (e *Error) WithMessage(msg string) *Error {
	clone := *e
	clone.Message = msg
	return &clone
}

// New builds an error with an explicit code and status.
func New(code Code, status int, message string) *Error {
	return &Error{Code: code, Status: status, Message: message}
}

func BadRequest(message string) *Error {
	return New(CodeBadRequest, http.StatusBadRequest, message)
}

// Validation reports one or more invalid request fields.
func Validation(fields []FieldError) *Error {
	return &Error{
		Code:    CodeValidation,
		Status:  http.StatusUnprocessableEntity,
		Message: "The request contains invalid fields",
		Fields:  fields,
	}
}

func Unauthorized(message string) *Error {
	if message == "" {
		message = "Authentication is required"
	}
	return New(CodeUnauthorized, http.StatusUnauthorized, message)
}

func Forbidden(message string) *Error {
	if message == "" {
		message = "You do not have permission to perform this action"
	}
	return New(CodeForbidden, http.StatusForbidden, message)
}

// NotFound renders as "User not found" for NotFound("User").
func NotFound(resource string) *Error {
	if resource == "" {
		resource = "Resource"
	}
	return New(CodeNotFound, http.StatusNotFound, resource+" not found")
}

func Conflict(message string) *Error {
	return New(CodeConflict, http.StatusConflict, message)
}

func TooManyRequests(message string) *Error {
	if message == "" {
		message = "Too many requests, please slow down"
	}
	return New(CodeTooManyRequests, http.StatusTooManyRequests, message)
}

func PayloadTooLarge(message string) *Error {
	if message == "" {
		message = "Request body is too large"
	}
	return New(CodePayloadTooLarge, http.StatusRequestEntityTooLarge, message)
}

func UnsupportedMediaType(message string) *Error {
	if message == "" {
		message = "Unsupported media type"
	}
	return New(CodeUnsupportedType, http.StatusUnsupportedMediaType, message)
}

// Internal hides the cause from the client while preserving it for logs.
func Internal(err error) *Error {
	return (&Error{
		Code:    CodeInternal,
		Status:  http.StatusInternalServerError,
		Message: "An unexpected error occurred",
	}).Wrap(err)
}

func Unavailable(message string, err error) *Error {
	if message == "" {
		message = "Service temporarily unavailable"
	}
	return New(CodeUnavailable, http.StatusServiceUnavailable, message).Wrap(err)
}

// As reports whether err is (or wraps) an *Error.
func As(err error) (*Error, bool) {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

// From coerces any error into an *Error, defaulting to an internal error so a
// leaked driver message never reaches a client.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	if appErr, ok := As(err); ok {
		return appErr
	}
	return Internal(err)
}
