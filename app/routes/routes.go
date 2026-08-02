// Package routes registers the versioned API surface and the operational
// endpoints. Adding /api/v2 later means adding one group here, not
// restructuring the project. App construction (the Fiber instance and the
// middleware stack) lives in app/server.
package routes

import (
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"

	"github.com/Karan0009/wordotron_api/app/config"
	"github.com/Karan0009/wordotron_api/app/handlers"
	"github.com/Karan0009/wordotron_api/app/lib/auth"
	"github.com/Karan0009/wordotron_api/app/middleware"
	"github.com/Karan0009/wordotron_api/app/models"
)

// openAPIPath is where the served specification lives relative to the working
// directory. The Dockerfile copies it to /app/docs/openapi.yaml.
const openAPIPath = "./docs/openapi.yaml"

// Dependencies is the explicit dependency-injection surface for the router.
// Every collaborator is passed in; nothing is resolved from a global.
type Dependencies struct {
	Config   *config.Config
	Logger   *slog.Logger
	Redis    redis.UniversalClient
	Tokens   auth.TokenManager
	Sessions auth.SessionStore

	Auth   *handlers.AuthHandler
	Users  *handlers.UserHandler
	Health *handlers.HealthHandler
	Files  *handlers.FileHandler
}

// RegisterRoutes mounts every route on app.
func RegisterRoutes(app *fiber.App, deps Dependencies) {
	cfg := deps.Config

	// ---- Operational endpoints (unversioned, never rate limited away) ----
	app.Get("/health", deps.Health.Health)
	app.Get("/health/live", deps.Health.Live)
	app.Get("/health/ready", deps.Health.Ready)

	app.Get("/openapi.yaml", func(c fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, "application/yaml; charset=utf-8")
		return c.SendFile(openAPIPath)
	})
	app.Get("/docs", docsPage)

	// Locally stored uploads are streamed by the API; with S3 or MinIO the
	// client goes straight to the object store.
	if deps.Files != nil && cfg.Storage.Provider == "local" {
		app.Get("/files/*", deps.Files.Serve)
	}

	authenticated := middleware.Authenticate(deps.Tokens, deps.Sessions)
	adminOnly := middleware.RequireRoles(models.RoleAdmin)

	// Brute-force protection for credential endpoints.
	authLimiter := middleware.RateLimit(middleware.RateLimitConfig{
		Client:    deps.Redis,
		Logger:    deps.Logger,
		Enabled:   cfg.Limiter.Enabled,
		Max:       cfg.Limiter.AuthMax,
		Window:    cfg.Limiter.AuthWindow,
		KeyPrefix: "ratelimit:auth",
	})

	v1 := app.Group("/api/v1")

	// ---- Authentication ----
	authGroup := v1.Group("/auth")
	authGroup.Post("/register", authLimiter, deps.Auth.Register)
	authGroup.Post("/login", authLimiter, deps.Auth.Login)
	authGroup.Post("/refresh", authLimiter, deps.Auth.Refresh)
	authGroup.Post("/forgot-password", authLimiter, deps.Auth.ForgotPassword)
	authGroup.Post("/reset-password", authLimiter, deps.Auth.ResetPassword)
	authGroup.Post("/verify-email", authLimiter, deps.Auth.VerifyEmail)
	authGroup.Post("/logout", authenticated, deps.Auth.Logout)
	authGroup.Post("/logout-all", authenticated, deps.Auth.LogoutAll)
	authGroup.Post("/change-password", authenticated, authLimiter, deps.Auth.ChangePassword)

	// ---- Users ----
	users := v1.Group("/users", authenticated)
	// The literal /me routes are registered before /:id so they are not
	// swallowed by the parameterised route.
	users.Get("/me", deps.Users.Me)
	users.Patch("/me", deps.Users.UpdateMe)
	users.Post("/me/avatar", deps.Users.UploadAvatar)

	users.Get("/", adminOnly, deps.Users.List)
	users.Post("/", adminOnly, deps.Users.Create)
	users.Get("/:id", deps.Users.Get)
	users.Patch("/:id", deps.Users.Update)
	users.Delete("/:id", deps.Users.Delete)
}

// docsPage renders Swagger UI against the served specification.
func docsPage(c fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	return c.SendString(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>API Documentation</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
    <script>
      window.onload = () => {
        window.ui = SwaggerUIBundle({
          url: "/openapi.yaml",
          dom_id: "#swagger-ui",
          deepLinking: true,
          persistAuthorization: true,
        });
      };
    </script>
  </body>
</html>`)
}
