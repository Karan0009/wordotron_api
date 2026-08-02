package middleware

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"

	"github.com/Karan0009/wordotron_api/app/lib"
)

// RateLimitConfig describes one rate-limiting rule.
type RateLimitConfig struct {
	Client redis.UniversalClient
	Logger *slog.Logger
	// Enabled turns the middleware into a no-op when false.
	Enabled bool
	// Max requests allowed per Window.
	Max int
	// Window is the length of the fixed window.
	Window time.Duration
	// KeyPrefix separates independent rules (global, auth, upload...).
	KeyPrefix string
}

// RateLimit implements a fixed-window counter in Redis, so the limit is shared
// across every replica of the API rather than per process.
//
// The window is fixed rather than sliding: it costs one round trip instead of
// a sorted-set read-modify-write, and the burst it permits at a window
// boundary is acceptable for abuse prevention. Swap in a token bucket via Lua
// if you need smoother enforcement.
func RateLimit(cfg RateLimitConfig) fiber.Handler {
	if !cfg.Enabled || cfg.Client == nil || cfg.Max <= 0 {
		return func(c fiber.Ctx) error { return c.Next() }
	}

	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = "ratelimit:default"
	}
	windowSeconds := int64(cfg.Window.Seconds())
	if windowSeconds < 1 {
		windowSeconds = 1
	}

	return func(c fiber.Ctx) error {
		identity := rateLimitIdentity(c)
		window := time.Now().Unix() / windowSeconds
		key := prefix + ":" + identity + ":" + strconv.FormatInt(window, 10)

		count, ttl, err := incrementWindow(c.Context(), cfg.Client, key, cfg.Window)
		if err != nil {
			// Fail open: a Redis outage should degrade rate limiting, not the
			// whole API.
			cfg.Logger.WarnContext(c.Context(), "rate limiter unavailable, allowing request",
				slog.String("error", err.Error()))
			return c.Next()
		}

		remaining := cfg.Max - int(count)
		if remaining < 0 {
			remaining = 0
		}

		c.Set("X-RateLimit-Limit", strconv.Itoa(cfg.Max))
		c.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Set("X-RateLimit-Reset", strconv.FormatInt(int64(ttl.Seconds()), 10))

		if int(count) > cfg.Max {
			c.Set("Retry-After", strconv.FormatInt(int64(ttl.Seconds()), 10))
			return lib.TooManyRequests("")
		}

		return c.Next()
	}
}

// incrementWindow bumps the counter and returns the new value plus the time
// left in the window.
func incrementWindow(ctx context.Context, client redis.UniversalClient, key string, window time.Duration) (int64, time.Duration, error) {
	pipe := client.TxPipeline()
	incr := pipe.Incr(ctx, key)
	// NX keeps the original expiry: re-arming it on every hit would let a
	// steady stream of requests extend the window indefinitely.
	pipe.ExpireNX(ctx, key, window)
	ttl := pipe.TTL(ctx, key)

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, 0, err
	}

	remaining := ttl.Val()
	if remaining < 0 {
		remaining = window
	}
	return incr.Val(), remaining, nil
}

// rateLimitIdentity buckets authenticated callers by user ID (so a shared NAT
// does not punish everyone behind it) and anonymous ones by IP.
func rateLimitIdentity(c fiber.Ctx) string {
	if userID := UserIDString(c); userID != "" {
		return "user:" + userID
	}
	return "ip:" + c.IP()
}
