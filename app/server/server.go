// Package server builds the Fiber application: the middleware stack, the
// versioned API surface and the operational endpoints. Adding /api/v2 later
// means adding one group here, not restructuring the project.
package server

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/recover"

	"github.com/Karan0009/wordotron_api/app/middleware"
	"github.com/Karan0009/wordotron_api/app/routes"
)

// NewApp builds a configured Fiber instance with the middleware stack and all
// routes registered.
func NewApp(deps routes.Dependencies) *fiber.App {
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
	routes.RegisterRoutes(app, deps)

	return app
}

func registerMiddleware(app *fiber.App, deps routes.Dependencies) {
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
