package middleware

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
)

// LoggerConfig configures the access log.
type LoggerConfig struct {
	Logger *slog.Logger
	// SkipPaths are logged at debug level only; use it for health probes that
	// would otherwise dominate the log volume.
	SkipPaths []string
}

// RequestLogger emits one structured line per request containing the request
// ID, method, path, status and duration.
func RequestLogger(cfg LoggerConfig) fiber.Handler {
	skip := make(map[string]struct{}, len(cfg.SkipPaths))
	for _, p := range cfg.SkipPaths {
		skip[p] = struct{}{}
	}

	return func(c fiber.Ctx) error {
		start := time.Now()

		// Run the rest of the chain first: the error handler has already
		// written the status by the time this returns.
		chainErr := c.Next()

		duration := time.Since(start)
		status := c.Response().StatusCode()
		path := c.Path()

		attrs := []slog.Attr{
			slog.String("method", c.Method()),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Duration("duration", duration),
			slog.String("ip", c.IP()),
			slog.String("user_agent", c.Get(fiber.HeaderUserAgent)),
		}
		if query := string(c.Request().URI().QueryString()); query != "" {
			attrs = append(attrs, slog.String("query", query))
		}
		if userID := UserIDString(c); userID != "" {
			attrs = append(attrs, slog.String("user_id", userID))
		}
		if chainErr != nil {
			attrs = append(attrs, slog.String("error", chainErr.Error()))
		}

		level := slog.LevelInfo
		switch {
		case status >= fiber.StatusInternalServerError:
			level = slog.LevelError
		case status >= fiber.StatusBadRequest:
			level = slog.LevelWarn
		}
		if _, skipped := skip[path]; skipped && level == slog.LevelInfo {
			level = slog.LevelDebug
		}

		cfg.Logger.LogAttrs(c.Context(), level, "http request", attrs...)
		return chainErr
	}
}
