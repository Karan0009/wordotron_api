// Command api is the HTTP entry point. It owns process lifecycle only:
// configuration, dependency construction, listening and graceful shutdown.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v3"

	"github.com/Karan0009/wordotron_api/internal/auth"
	"github.com/Karan0009/wordotron_api/internal/config"
	"github.com/Karan0009/wordotron_api/internal/database"
	"github.com/Karan0009/wordotron_api/internal/handlers"
	"github.com/Karan0009/wordotron_api/internal/mailer"
	"github.com/Karan0009/wordotron_api/internal/repository"
	"github.com/Karan0009/wordotron_api/internal/routes"
	"github.com/Karan0009/wordotron_api/internal/service"
	"github.com/Karan0009/wordotron_api/internal/storage"
	"github.com/Karan0009/wordotron_api/internal/validation"
	"github.com/Karan0009/wordotron_api/pkg/logger"
)

// Injected at build time via -ldflags.
var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet when configuration fails, so this one
		// message goes straight to stderr.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logger.New(logger.Config{Level: cfg.Log.Level, Format: cfg.Log.Format})
	slog.SetDefault(log)

	log.Info("starting api",
		slog.String("version", version),
		slog.String("build_time", buildTime),
		slog.String("env", cfg.App.Env),
		slog.String("addr", cfg.App.Addr()),
	)

	// Cancelled on SIGINT/SIGTERM; also bounds start-up work.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ---- Infrastructure -----------------------------------------------
	pool, err := database.NewPostgres(ctx, cfg.DB, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	redisClient, err := database.NewRedis(ctx, cfg.Redis, log)
	if err != nil {
		return err
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			log.Error("closing redis failed", slog.String("error", err.Error()))
		}
	}()

	files, err := storage.New(ctx, cfg.Storage, log)
	if err != nil {
		return err
	}

	// ---- Domain wiring -------------------------------------------------
	hasher, err := auth.NewBcryptHasher(cfg.Auth.BcryptCost)
	if err != nil {
		return err
	}

	validator, err := validation.New()
	if err != nil {
		return err
	}

	tokens := auth.NewJWTManager(cfg.Auth)
	sessions := auth.NewRedisSessionStore(redisClient, cfg.Auth.RefreshExpiry)
	store := repository.NewStore(pool)
	mail := mailer.NewLogMailer(log)

	authService := service.NewAuthService(store, hasher, tokens, sessions, mail, cfg, log)
	userService := service.NewUserService(store, hasher, sessions, files, cfg, log)

	app := routes.NewApp(routes.Dependencies{
		Config:   cfg,
		Logger:   log,
		Redis:    redisClient,
		Tokens:   tokens,
		Sessions: sessions,
		Auth:     handlers.NewAuthHandler(authService, validator, cfg),
		Users:    handlers.NewUserHandler(userService, validator, cfg),
		Health:   handlers.NewHealthHandler(pool, redisClient, version, cfg.App.Env),
		Files:    handlers.NewFileHandler(files),
	})

	// todo:
	// A single in-process maintenance loop. When the workload justifies it,
	// move this into cmd/worker and drive it from a queue instead.
	// go pruneExpiredResetTokens(ctx, store, log)

	// ---- Serve ----------------------------------------------------------
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- app.Listen(cfg.App.Addr(), fiber.ListenConfig{
			DisableStartupMessage: true,
			EnablePrefork:         false,
		})
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-ctx.Done():
		log.Info("shutdown signal received", slog.Duration("timeout", cfg.App.ShutdownTimeout))
	}

	// Stop accepting new connections and let in-flight requests finish.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.App.ShutdownTimeout)
	defer cancel()

	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Warn("graceful shutdown timed out, closing anyway")
		} else {
			return fmt.Errorf("shutdown: %w", err)
		}
	}

	log.Info("shutdown complete")
	return nil
}

// pruneExpiredResetTokens deletes stale password reset rows once an hour.
// func pruneExpiredResetTokens(ctx context.Context, store repository.Store, log *slog.Logger) {
// 	ticker := time.NewTicker(time.Hour)
// 	defer ticker.Stop()

// 	for {
// 		select {
// 		case <-ctx.Done():
// 			return
// 		case <-ticker.C:
// 			jobCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
// 			deleted, err := store.PasswordResets().DeleteExpired(jobCtx)
// 			cancel()

// 			if err != nil {
// 				log.Error("prune reset tokens failed", slog.String("error", err.Error()))
// 				continue
// 			}
// 			if deleted > 0 {
// 				log.Info("pruned expired reset tokens", slog.Int64("count", deleted))
// 			}
// 		}
// 	}
// }
