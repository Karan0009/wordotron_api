package lib

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Karan0009/wordotron_api/app/config"
)

// NewRedis parses REDIS_URL, applies conservative timeouts and verifies the
// connection before returning.
func NewRedis(ctx context.Context, cfg config.Redis, log *slog.Logger) (*redis.Client, error) {
	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 3 * time.Second
	opts.WriteTimeout = 3 * time.Second
	opts.MaxRetries = 3
	opts.MinIdleConns = 2

	client := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	log.Info("connected to redis", slog.String("addr", opts.Addr), slog.Int("db", opts.DB))
	return client, nil
}
