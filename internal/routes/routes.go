// Package routes builds the Fiber application: the middleware stack, the
// versioned API surface and the operational endpoints. Adding /api/v2 later
// means adding one group here, not restructuring the project.
package routes

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/redis/go-redis/v9"

	"github.com/Karan0009/wordotron_api/internal/auth"
	"github.com/Karan0009/wordotron_api/internal/config"
	"github.com/Karan0009/wordotron_api/internal/handlers"
	"github.com/Karan0009/wordotron_api/internal/middleware"
	"github.com/Karan0009/wordotron_api/internal/models"
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

// NewApp builds a configured Fiber instance with the middleware stack and all
// routes registered.
func NewApp(deps Dependencies) *fiber.App {
	cfg := deps.Config

	fiberCfg := fiber.Config{
		AppName:       cfg.App.Name,
		BodyLimit:     cfg.App.BodyLimitBytes,
		ReadTimeout:   15 * time.Second,
		WriteTimeout:  30 * time.Second,
		IdleTimeout:   60 * time.Second,
		CaseSensitive: true,
		StrictRouting: false,
		// Do not advertise the framework.
		ServerHeader: "",
		ErrorHandler: middleware.ErrorHandler(deps.Logger),
	}

	// X-Forwarded-For is only honoured when the request comes from a proxy we
	// named, otherwise a client could spoof its own IP past the rate limiter.
	if len(cfg.App.TrustedProxies) > 0 {
		fiberCfg.TrustProxy = true
		fiberCfg.TrustProxyConfig = fiber.TrustProxyConfig{
			Proxies: cfg.App.TrustedProxies,
		}
	}

	app := fiber.New(fiberCfg)

	registerMiddleware(app, deps)
	registerRoutes(app, deps)

	return app
}

func registerMiddleware(app *fiber.App, deps Dependencies) {
	cfg := deps.Config

	// Order matters. The correlation ID must exist before anything logs, and
	// recovery must wrap everything that can panic.
	app.Use(middleware.RequestID())

	app.Use(middleware.RequestLogger(middleware.LoggerConfig{
		Logger:    deps.Logger,
		SkipPaths: []string{"/health", "/health/live", "/health/ready"},
	}))

	app.Use(recover.New(recover.Config{
		EnableStackTrace: !cfg.App.IsProduction(),
	}))

	app.Use(helmet.New(helmet.Config{
		// The docs page pulls Swagger UI from a CDN, which the API's own CSP
		// would otherwise block.
		Next: func(c fiber.Ctx) bool {
			return c.Path() == "/docs"
		},
		XSSProtection:           "0",
		ContentTypeNosniff:      "nosniff",
		XFrameOptions:           "DENY",
		ReferrerPolicy:          "strict-origin-when-cross-origin",
		CrossOriginOpenerPolicy: "same-origin",
		// This is a JSON API: it never serves markup, so everything is denied
		// by default.
		ContentSecurityPolicy: "default-src 'none'; frame-ancestors 'none'; base-uri 'none'",
		// One year, honoured by browsers only over TLS.
		HSTSMaxAge:            31536000,
		HSTSPreloadEnabled:    false,
		HSTSExcludeSubdomains: false,
	}))

	app.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.CORS.AllowedOrigins,
		AllowMethods:     []string{fiber.MethodGet, fiber.MethodPost, fiber.MethodPatch, fiber.MethodPut, fiber.MethodDelete, fiber.MethodOptions},
		AllowHeaders:     []string{fiber.HeaderOrigin, fiber.HeaderContentType, fiber.HeaderAccept, fiber.HeaderAuthorization, fiber.HeaderXRequestID},
		ExposeHeaders:    []string{fiber.HeaderXRequestID, "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"},
		AllowCredentials: cfg.CORS.AllowCredentials,
		MaxAge:           int((12 * time.Hour).Seconds()),
	}))

	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
	}))

	// Global throttle. Auth endpoints get a second, stricter limiter below.
	app.Use(middleware.RateLimit(middleware.RateLimitConfig{
		Client:    deps.Redis,
		Logger:    deps.Logger,
		Enabled:   cfg.Limiter.Enabled,
		Max:       cfg.Limiter.Max,
		Window:    cfg.Limiter.Window,
		KeyPrefix: "ratelimit:global",
	}))
}

func registerRoutes(app *fiber.App, deps Dependencies) {
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
