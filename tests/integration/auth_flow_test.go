//go:build integration

// Package integration exercises the API against real Postgres and Redis
// instances started by testcontainers. Run it with:
//
//	make test-integration
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Karan0009/wordotron_api/app/config"
	"github.com/Karan0009/wordotron_api/app/handlers"
	"github.com/Karan0009/wordotron_api/app/lib"
	"github.com/Karan0009/wordotron_api/app/lib/auth"
	"github.com/Karan0009/wordotron_api/app/lib/storage"
	"github.com/Karan0009/wordotron_api/app/lib/validation"
	"github.com/Karan0009/wordotron_api/app/repository"
	"github.com/Karan0009/wordotron_api/app/routes"
	"github.com/Karan0009/wordotron_api/app/server"
	"github.com/Karan0009/wordotron_api/app/services"
	"golang.org/x/crypto/bcrypt"
)

const (
	testPassword    = "Sup3rSecretPassword"
	migrationsPath  = "file://../../migrations"
	containerBootTO = 120 * time.Second
)

// testEnv holds everything a test needs to talk to the API.
type testEnv struct {
	app   *fiber.App
	cfg   *config.Config
	store repository.Store
}

// newTestEnv starts the containers, migrates the schema and wires the real
// application graph. The containers are shared by every test in the package.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), containerBootTO)
	defer cancel()

	pgContainer, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("appdb"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := pgContainer.Terminate(context.Background()); err != nil {
			t.Logf("terminate postgres: %v", err)
		}
	})

	redisContainer, err := tcredis.Run(ctx, "redis:7-alpine")
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := redisContainer.Terminate(context.Background()); err != nil {
			t.Logf("terminate redis: %v", err)
		}
	})

	databaseURL, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	redisURL, err := redisContainer.ConnectionString(ctx)
	require.NoError(t, err)

	runMigrations(t, databaseURL)

	cfg := &config.Config{
		App: config.App{
			Env:               config.EnvDevelopment,
			Name:              "backend-api-test",
			Port:              8080,
			FrontendURL:       "http://localhost:5173",
			ResetPasswordPath: "/reset-password",
			ShutdownTimeout:   5 * time.Second,
			BodyLimitBytes:    4 * 1024 * 1024,
		},
		Log: config.Log{Level: "error", Format: "text"},
		Auth: config.Auth{
			JWTSecret:           "integration-test-access-secret-value-32",
			RefreshSecret:       "integration-test-refresh-secret-value-32",
			JWTExpiry:           15 * time.Minute,
			RefreshExpiry:       24 * time.Hour,
			Issuer:              "backend-api-test",
			BcryptCost:          bcrypt.MinCost,
			PasswordResetExpiry: time.Hour,
		},
		CORS:    config.CORS{AllowedOrigins: []string{"http://localhost:5173"}},
		Limiter: config.RateLimit{Enabled: false},
		Storage: config.Storage{
			Provider:       "local",
			LocalPath:      t.TempDir(),
			PublicBaseURL:  "http://localhost:8080/files",
			MaxUploadBytes: 1024 * 1024,
		},
	}

	log := lib.New(lib.Config{Level: "error", Format: "text", Output: io.Discard})

	pool, err := lib.NewPostgres(ctx, config.Database{
		URL:      databaseURL,
		MaxConns: 5,
		MinConns: 1,
	}, log)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	redisClient, err := lib.NewRedis(ctx, config.Redis{URL: redisURL}, log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = redisClient.Close() })

	files, err := storage.New(ctx, cfg.Storage, log)
	require.NoError(t, err)

	hasher, err := auth.NewBcryptHasher(cfg.Auth.BcryptCost)
	require.NoError(t, err)

	validator, err := validation.New()
	require.NoError(t, err)

	tokens := auth.NewJWTManager(cfg.Auth)
	sessions := auth.NewRedisSessionStore(redisClient, cfg.Auth.RefreshExpiry)
	store := repository.NewStore(pool)

	authService := services.NewAuthService(store, hasher, tokens, sessions, cfg, log)
	userService := services.NewUserService(store, hasher, sessions, files, cfg, log)

	app := server.NewApp(routes.Dependencies{
		Config:   cfg,
		Logger:   log,
		Redis:    redisClient,
		Tokens:   tokens,
		Sessions: sessions,
		Auth:     handlers.NewAuthHandler(authService, validator, cfg),
		Users:    handlers.NewUserHandler(userService, validator, cfg),
		Health:   handlers.NewHealthHandler(pool, redisClient, "test", cfg.App.Env),
		Files:    handlers.NewFileHandler(files),
	})

	return &testEnv{app: app, cfg: cfg, store: store}
}

func runMigrations(t *testing.T, databaseURL string) {
	t.Helper()

	// golang-migrate selects its driver from the URL scheme.
	migrateURL := strings.Replace(databaseURL, "postgres://", "pgx5://", 1)

	m, err := migrate.New(migrationsPath, migrateURL)
	require.NoError(t, err)
	defer func() {
		sourceErr, dbErr := m.Close()
		if sourceErr != nil {
			t.Logf("close migration source: %v", sourceErr)
		}
		if dbErr != nil {
			t.Logf("close migration db: %v", dbErr)
		}
	}()

	require.NoError(t, m.Up())
}

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

type apiResponse struct {
	status int
	body   map[string]any
}

func (e *testEnv) do(t *testing.T, method, path, body, bearer string) apiResponse {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	}
	if bearer != "" {
		req.Header.Set(fiber.HeaderAuthorization, "Bearer "+bearer)
	}

	resp, err := e.app.Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	decoded := map[string]any{}
	if len(raw) > 0 {
		require.NoError(t, json.Unmarshal(raw, &decoded), "body was: %s", raw)
	}

	return apiResponse{status: resp.StatusCode, body: decoded}
}

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@example.com", prefix, time.Now().UnixNano())
}

func tokensFrom(t *testing.T, resp apiResponse) (accessToken, refreshToken string) {
	t.Helper()

	data, ok := resp.body["data"].(map[string]any)
	require.True(t, ok, "response has no data object: %v", resp.body)

	tokens, ok := data["tokens"].(map[string]any)
	require.True(t, ok, "response has no tokens object: %v", resp.body)

	return tokens["access_token"].(string), tokens["refresh_token"].(string)
}

func (e *testEnv) register(t *testing.T, email string) apiResponse {
	t.Helper()
	return e.do(t, fiber.MethodPost, "/api/v1/auth/register",
		fmt.Sprintf(`{"email":%q,"password":%q,"full_name":"Test User"}`, email, testPassword), "")
}

// verify marks email as verified directly via the repository, bypassing the
// email link: the plaintext verification token never reaches a black-box
// HTTP test, only its hash does.
func (e *testEnv) verify(t *testing.T, email string) {
	t.Helper()

	user, err := e.store.Users().GetByEmail(context.Background(), email)
	require.NoError(t, err)
	require.NoError(t, e.store.Users().MarkEmailVerified(context.Background(), user.ID))
}

// registerAndLogin registers, verifies and logs in, returning the login
// response so tokensFrom works exactly as it did before verification was
// required.
func (e *testEnv) registerAndLogin(t *testing.T, email string) apiResponse {
	t.Helper()

	require.Equal(t, fiber.StatusCreated, e.register(t, email).status)
	e.verify(t, email)

	return e.do(t, fiber.MethodPost, "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":%q}`, email, testPassword), "")
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestHealthEndpoints(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, fiber.MethodGet, "/health", "", "")
	require.Equal(t, fiber.StatusOK, resp.status)
	require.Equal(t, "ok", resp.body["status"])

	ready := env.do(t, fiber.MethodGet, "/health/ready", "", "")
	require.Equal(t, fiber.StatusOK, ready.status)
	require.Equal(t, "ok", ready.body["status"])
}

func TestRegisterLoginAndFetchProfile(t *testing.T) {
	env := newTestEnv(t)
	email := uniqueEmail("flow")

	registered := env.register(t, email)
	require.Equal(t, fiber.StatusCreated, registered.status)
	require.Equal(t, true, registered.body["success"])
	// Register no longer starts a session: the account can't log in yet.
	require.NotContains(t, registered.body["data"], "tokens")

	// Registering the same address twice is a conflict, not a 500.
	duplicate := env.register(t, email)
	require.Equal(t, fiber.StatusConflict, duplicate.status)

	env.verify(t, email)

	loggedIn := env.do(t, fiber.MethodPost, "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":%q}`, email, testPassword), "")
	require.Equal(t, fiber.StatusOK, loggedIn.status)

	accessToken, _ := tokensFrom(t, loggedIn)

	profile := env.do(t, fiber.MethodGet, "/api/v1/users/me", "", accessToken)
	require.Equal(t, fiber.StatusOK, profile.status)

	user := profile.body["data"].(map[string]any)
	require.Equal(t, email, user["email"])
	require.Equal(t, "user", user["role"])
	require.NotContains(t, user, "password_hash")
}

func TestLoginWithWrongPasswordIsRejected(t *testing.T) {
	env := newTestEnv(t)
	email := uniqueEmail("wrongpass")

	require.Equal(t, fiber.StatusCreated, env.register(t, email).status)

	resp := env.do(t, fiber.MethodPost, "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":"CompletelyWrong1"}`, email), "")

	require.Equal(t, fiber.StatusUnauthorized, resp.status)
	require.Equal(t, false, resp.body["success"])
	// The same message is used for unknown accounts, so nothing is leaked.
	require.Equal(t, "Invalid email or password", resp.body["message"])
}

func TestProtectedRouteRequiresToken(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, fiber.MethodGet, "/api/v1/users/me", "", "")
	require.Equal(t, fiber.StatusUnauthorized, resp.status)

	bad := env.do(t, fiber.MethodGet, "/api/v1/users/me", "", "not-a-jwt")
	require.Equal(t, fiber.StatusUnauthorized, bad.status)
}

func TestRefreshRotatesAndInvalidatesOldToken(t *testing.T) {
	env := newTestEnv(t)
	email := uniqueEmail("refresh")

	loggedIn := env.registerAndLogin(t, email)
	_, refreshToken := tokensFrom(t, loggedIn)

	rotated := env.do(t, fiber.MethodPost, "/api/v1/auth/refresh",
		fmt.Sprintf(`{"refresh_token":%q}`, refreshToken), "")
	require.Equal(t, fiber.StatusOK, rotated.status)

	newAccess, newRefresh := tokensFrom(t, rotated)
	require.NotEqual(t, refreshToken, newRefresh)

	// The rotated-out token must not work again.
	replayed := env.do(t, fiber.MethodPost, "/api/v1/auth/refresh",
		fmt.Sprintf(`{"refresh_token":%q}`, refreshToken), "")
	require.Equal(t, fiber.StatusUnauthorized, replayed.status)

	// Reuse detection revokes the whole family, including the new token.
	afterReuse := env.do(t, fiber.MethodPost, "/api/v1/auth/refresh",
		fmt.Sprintf(`{"refresh_token":%q}`, newRefresh), "")
	require.Equal(t, fiber.StatusUnauthorized, afterReuse.status)

	// And the access token issued alongside it is dead too.
	profile := env.do(t, fiber.MethodGet, "/api/v1/users/me", "", newAccess)
	require.Equal(t, fiber.StatusUnauthorized, profile.status)
}

func TestLogoutBlacklistsAccessToken(t *testing.T) {
	env := newTestEnv(t)
	email := uniqueEmail("logout")

	loggedIn := env.registerAndLogin(t, email)
	accessToken, refreshToken := tokensFrom(t, loggedIn)

	require.Equal(t, fiber.StatusOK,
		env.do(t, fiber.MethodGet, "/api/v1/users/me", "", accessToken).status)

	loggedOut := env.do(t, fiber.MethodPost, "/api/v1/auth/logout",
		fmt.Sprintf(`{"refresh_token":%q}`, refreshToken), accessToken)
	require.Equal(t, fiber.StatusOK, loggedOut.status)

	require.Equal(t, fiber.StatusUnauthorized,
		env.do(t, fiber.MethodGet, "/api/v1/users/me", "", accessToken).status)

	require.Equal(t, fiber.StatusUnauthorized,
		env.do(t, fiber.MethodPost, "/api/v1/auth/refresh",
			fmt.Sprintf(`{"refresh_token":%q}`, refreshToken), "").status)
}

func TestChangePasswordIssuesNewSession(t *testing.T) {
	env := newTestEnv(t)
	email := uniqueEmail("changepw")

	loggedIn := env.registerAndLogin(t, email)
	accessToken, _ := tokensFrom(t, loggedIn)

	const newPassword = "EvenB3tterPassword"

	changed := env.do(t, fiber.MethodPost, "/api/v1/auth/change-password",
		fmt.Sprintf(`{"current_password":%q,"new_password":%q}`, testPassword, newPassword), accessToken)
	require.Equal(t, fiber.StatusOK, changed.status)

	freshAccess, _ := tokensFrom(t, changed)
	require.Equal(t, fiber.StatusOK,
		env.do(t, fiber.MethodGet, "/api/v1/users/me", "", freshAccess).status)

	// The old credentials no longer work.
	require.Equal(t, fiber.StatusUnauthorized,
		env.do(t, fiber.MethodPost, "/api/v1/auth/login",
			fmt.Sprintf(`{"email":%q,"password":%q}`, email, testPassword), "").status)

	require.Equal(t, fiber.StatusOK,
		env.do(t, fiber.MethodPost, "/api/v1/auth/login",
			fmt.Sprintf(`{"email":%q,"password":%q}`, email, newPassword), "").status)
}

func TestNonAdminCannotListUsers(t *testing.T) {
	env := newTestEnv(t)

	loggedIn := env.registerAndLogin(t, uniqueEmail("listdenied"))
	accessToken, _ := tokensFrom(t, loggedIn)

	resp := env.do(t, fiber.MethodGet, "/api/v1/users?page=1&limit=10", "", accessToken)
	require.Equal(t, fiber.StatusForbidden, resp.status)
}

func TestForgotPasswordDoesNotRevealAccounts(t *testing.T) {
	env := newTestEnv(t)

	known := uniqueEmail("forgot")
	require.Equal(t, fiber.StatusCreated, env.register(t, known).status)

	forKnown := env.do(t, fiber.MethodPost, "/api/v1/auth/forgot-password",
		fmt.Sprintf(`{"email":%q}`, known), "")
	forUnknown := env.do(t, fiber.MethodPost, "/api/v1/auth/forgot-password",
		`{"email":"nobody-here@example.com"}`, "")

	require.Equal(t, fiber.StatusOK, forKnown.status)
	require.Equal(t, fiber.StatusOK, forUnknown.status)
	require.Equal(t, forKnown.body["data"], forUnknown.body["data"])
}

func TestLoginBlockedUntilEmailVerified(t *testing.T) {
	env := newTestEnv(t)
	email := uniqueEmail("unverified")

	require.Equal(t, fiber.StatusCreated, env.register(t, email).status)

	resp := env.do(t, fiber.MethodPost, "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":%q}`, email, testPassword), "")

	require.Equal(t, fiber.StatusForbidden, resp.status)
	require.Equal(t, false, resp.body["success"])

	env.verify(t, email)

	afterVerify := env.do(t, fiber.MethodPost, "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":%q}`, email, testPassword), "")
	require.Equal(t, fiber.StatusOK, afterVerify.status)
}

func TestValidationRejectsWeakPassword(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, fiber.MethodPost, "/api/v1/auth/register",
		`{"email":"weak@example.com","password":"weak","full_name":"Weak User"}`, "")

	require.Equal(t, fiber.StatusUnprocessableEntity, resp.status)
	require.NotEmpty(t, resp.body["errors"])
}
