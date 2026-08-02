package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Karan0009/wordotron_api/app/config"
	"github.com/Karan0009/wordotron_api/app/handlers"
	"github.com/Karan0009/wordotron_api/app/lib"
	"github.com/Karan0009/wordotron_api/app/lib/auth"
	"github.com/Karan0009/wordotron_api/app/lib/validation"
	"github.com/Karan0009/wordotron_api/app/middleware"
	"github.com/Karan0009/wordotron_api/app/models"
	"github.com/Karan0009/wordotron_api/app/services"
)

// ---------------------------------------------------------------------------
// Test double
// ---------------------------------------------------------------------------

// stubAuthService records what it was called with and returns canned results,
// which keeps handler tests free of a database.
type stubAuthService struct {
	registerFn func(context.Context, services.RegisterInput) (*models.User, error)
	loginFn    func(context.Context, services.LoginInput) (*services.AuthResult, error)

	gotLogin services.LoginInput
}

var _ services.AuthService = (*stubAuthService)(nil)

func (s *stubAuthService) Register(ctx context.Context, in services.RegisterInput) (*models.User, error) {
	if s.registerFn != nil {
		return s.registerFn(ctx, in)
	}
	return nil, lib.Internal(nil)
}

func (s *stubAuthService) Login(ctx context.Context, in services.LoginInput) (*services.AuthResult, error) {
	s.gotLogin = in
	if s.loginFn != nil {
		return s.loginFn(ctx, in)
	}
	return nil, lib.Internal(nil)
}

func (s *stubAuthService) Refresh(context.Context, string) (*services.AuthResult, error) {
	return nil, lib.Unauthorized("")
}
func (s *stubAuthService) Logout(context.Context, services.LogoutInput) error { return nil }
func (s *stubAuthService) LogoutAll(context.Context, uuid.UUID) error         { return nil }
func (s *stubAuthService) ChangePassword(context.Context, uuid.UUID, string, string) (*services.AuthResult, error) {
	return nil, lib.Unauthorized("")
}
func (s *stubAuthService) ForgotPassword(context.Context, string) error        { return nil }
func (s *stubAuthService) ResetPassword(context.Context, string, string) error { return nil }
func (s *stubAuthService) VerifyEmail(context.Context, string) error           { return nil }

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

func testUser() *models.User {
	return &models.User{
		ID:        uuid.New(),
		Email:     "jane@example.com",
		FullName:  "Jane Doe",
		Role:      models.RoleUser,
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

func testAuthResult() *services.AuthResult {
	return &services.AuthResult{
		User: testUser(),
		Tokens: auth.TokenPair{
			AccessToken:  "access.jwt.token",
			RefreshToken: "refresh.jwt.token",
			TokenType:    "Bearer",
			ExpiresIn:    900,
			ExpiresAt:    time.Now().Add(15 * time.Minute).UTC(),
		},
	}
}

// newTestApp mounts the auth routes on a Fiber app with the real error
// handler, so error envelopes are asserted exactly as clients see them.
func newTestApp(t *testing.T, svc services.AuthService) *fiber.App {
	t.Helper()

	validator, err := validation.New()
	require.NoError(t, err)

	log := lib.New(lib.Config{Level: "error", Format: "text", Output: io.Discard})
	cfg := &config.Config{}

	app := fiber.New(fiber.Config{ErrorHandler: middleware.ErrorHandler(log)})
	app.Use(middleware.RequestID())

	handler := handlers.NewAuthHandler(svc, validator, cfg)
	app.Post("/api/v1/auth/register", handler.Register)
	app.Post("/api/v1/auth/login", handler.Login)

	return app
}

func postJSON(t *testing.T, app *fiber.App, path, body string) (int, map[string]any) {
	t.Helper()

	req := httptest.NewRequest(fiber.MethodPost, path, strings.NewReader(body))
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded), "body was: %s", raw)

	return resp.StatusCode, decoded
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestLogin_Success(t *testing.T) {
	svc := &stubAuthService{
		loginFn: func(context.Context, services.LoginInput) (*services.AuthResult, error) {
			return testAuthResult(), nil
		},
	}
	app := newTestApp(t, svc)

	status, body := postJSON(t, app, "/api/v1/auth/login",
		`{"email":"jane@example.com","password":"Sup3rSecretPassword"}`)

	require.Equal(t, fiber.StatusOK, status)
	require.Equal(t, true, body["success"])

	data, ok := body["data"].(map[string]any)
	require.True(t, ok)

	user := data["user"].(map[string]any)
	require.Equal(t, "jane@example.com", user["email"])
	require.NotContains(t, user, "password_hash", "the hash must never be serialised")

	tokens := data["tokens"].(map[string]any)
	require.Equal(t, "Bearer", tokens["token_type"])
	require.Equal(t, "jane@example.com", svc.gotLogin.Email)
}

func TestLogin_InvalidCredentialsReturnsErrorEnvelope(t *testing.T) {
	app := newTestApp(t, &stubAuthService{
		loginFn: func(context.Context, services.LoginInput) (*services.AuthResult, error) {
			return nil, lib.Unauthorized("Invalid email or password")
		},
	})

	status, body := postJSON(t, app, "/api/v1/auth/login",
		`{"email":"jane@example.com","password":"wrong-password"}`)

	require.Equal(t, fiber.StatusUnauthorized, status)
	require.Equal(t, false, body["success"])
	require.Equal(t, "Invalid email or password", body["message"])
	require.Equal(t, string(lib.CodeUnauthorized), body["code"])
	require.NotEmpty(t, body["request_id"])
}

func TestRegister_ValidationErrors(t *testing.T) {
	app := newTestApp(t, &stubAuthService{})

	status, body := postJSON(t, app, "/api/v1/auth/register",
		`{"email":"not-an-email","password":"short","full_name":""}`)

	require.Equal(t, fiber.StatusUnprocessableEntity, status)
	require.Equal(t, false, body["success"])

	errorsList, ok := body["errors"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, errorsList)

	fields := map[string]bool{}
	for _, item := range errorsList {
		entry := item.(map[string]any)
		fields[entry["field"].(string)] = true
		require.NotEmpty(t, entry["message"])
	}
	require.True(t, fields["email"])
	require.True(t, fields["password"])
	require.True(t, fields["full_name"])
}

func TestRegister_WeakPasswordRejected(t *testing.T) {
	app := newTestApp(t, &stubAuthService{})

	// Long enough, but no uppercase and no digit.
	status, _ := postJSON(t, app, "/api/v1/auth/register",
		`{"email":"jane@example.com","password":"alllowercaseletters","full_name":"Jane Doe"}`)

	require.Equal(t, fiber.StatusUnprocessableEntity, status)
}

func TestRegister_MalformedJSON(t *testing.T) {
	app := newTestApp(t, &stubAuthService{})

	status, body := postJSON(t, app, "/api/v1/auth/register", `{"email":`)

	require.Equal(t, fiber.StatusBadRequest, status)
	require.Equal(t, false, body["success"])
}

func TestRegister_ConflictIsPropagated(t *testing.T) {
	app := newTestApp(t, &stubAuthService{
		registerFn: func(context.Context, services.RegisterInput) (*models.User, error) {
			return nil, lib.Conflict("An account with this email already exists")
		},
	})

	status, body := postJSON(t, app, "/api/v1/auth/register",
		`{"email":"jane@example.com","password":"Sup3rSecretPassword","full_name":"Jane Doe"}`)

	require.Equal(t, fiber.StatusConflict, status)
	require.Equal(t, string(lib.CodeConflict), body["code"])
}
