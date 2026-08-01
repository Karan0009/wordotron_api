// Package validation adapts go-playground/validator to the API's error
// envelope: struct tags in, apperror.FieldError slices out.
package validation

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/go-playground/validator/v10"

	"github.com/Karan0009/wordotron_api/internal/models"
	"github.com/Karan0009/wordotron_api/pkg/apperror"
)

// Validator validates request DTOs. The zero value is not usable; call New.
type Validator struct {
	validate *validator.Validate
}

// New builds a Validator with the project's custom rules registered.
func New() (*Validator, error) {
	v := validator.New(validator.WithRequiredStructEnabled())

	// Report the JSON field name so clients can map errors onto their forms.
	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
		if name == "-" || name == "" {
			return field.Name
		}
		return name
	})

	if err := v.RegisterValidation("strongpassword", strongPassword); err != nil {
		return nil, fmt.Errorf("register strongpassword: %w", err)
	}
	if err := v.RegisterValidation("role", validRole); err != nil {
		return nil, fmt.Errorf("register role: %w", err)
	}

	return &Validator{validate: v}, nil
}

// Struct validates s and returns an *apperror.Error with per-field messages
// when validation fails.
func (v *Validator) Struct(s any) error {
	if err := v.validate.Struct(s); err != nil {
		var invalid *validator.InvalidValidationError
		if errors.As(err, &invalid) {
			return apperror.Internal(err)
		}

		var validationErrs validator.ValidationErrors
		if errors.As(err, &validationErrs) {
			fields := make([]apperror.FieldError, 0, len(validationErrs))
			for _, fe := range validationErrs {
				fields = append(fields, apperror.FieldError{
					Field:   fe.Field(),
					Message: message(fe),
				})
			}
			return apperror.Validation(fields)
		}
		return apperror.Internal(err)
	}
	return nil
}

// strongPassword requires a mix of character classes. Length is enforced
// separately by the `min` tag so the two messages stay distinct.
func strongPassword(fl validator.FieldLevel) bool {
	password := fl.Field().String()

	var hasUpper, hasLower, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	return hasUpper && hasLower && hasDigit
}

func validRole(fl validator.FieldLevel) bool {
	return models.Role(fl.Field().String()).Valid()
}

// message renders a human-readable sentence for a failed rule.
func message(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "This field is required"
	case "email":
		return "Must be a valid email address"
	case "min":
		if fe.Kind() == reflect.String {
			return fmt.Sprintf("Must be at least %s characters", fe.Param())
		}
		return fmt.Sprintf("Must be at least %s", fe.Param())
	case "max":
		if fe.Kind() == reflect.String {
			return fmt.Sprintf("Must be at most %s characters", fe.Param())
		}
		return fmt.Sprintf("Must be at most %s", fe.Param())
	case "strongpassword":
		return "Must contain an uppercase letter, a lowercase letter and a digit"
	case "role":
		return "Must be one of: user, admin"
	case "oneof":
		return fmt.Sprintf("Must be one of: %s", strings.ReplaceAll(fe.Param(), " ", ", "))
	case "uuid", "uuid4":
		return "Must be a valid UUID"
	case "eqfield":
		return fmt.Sprintf("Must match %s", fe.Param())
	case "url":
		return "Must be a valid URL"
	case "gte":
		return fmt.Sprintf("Must be greater than or equal to %s", fe.Param())
	case "lte":
		return fmt.Sprintf("Must be less than or equal to %s", fe.Param())
	default:
		return fmt.Sprintf("Failed the %q rule", fe.Tag())
	}
}
