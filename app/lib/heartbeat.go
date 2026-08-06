// heartbeat.go pings the frontend and this service's own health endpoint on
// an interval so free-tier hosts that spin down on inactivity stay warm.
package lib

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/Karan0009/wordotron_api/app/config"
)

// StartHeartbeat blocks until ctx is cancelled, pinging cfg.App.FrontendURL
// and cfg.App.BackendURL/health/live every cfg.App.HeartbeatInterval. Call it
// in its own goroutine. A no-op when HeartbeatEnabled is false.
func StartHeartbeat(ctx context.Context, cfg *config.Config, log *slog.Logger) {
	if !cfg.App.HeartbeatEnabled {
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	log.Info("heartbeat enabled", slog.Duration("interval", cfg.App.HeartbeatInterval))

	ticker := time.NewTicker(cfg.App.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingHeartbeatTarget(ctx, client, log, "backend", cfg.App.BackendURL+"/health/live")
			pingHeartbeatTarget(ctx, client, log, "frontend", cfg.App.FrontendURL+"/")
		}
	}
}

func pingHeartbeatTarget(ctx context.Context, client *http.Client, log *slog.Logger, name, url string) {
	if url == "" || url == "/" {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Error("heartbeat request build failed", slog.String("target", name), slog.String("error", err.Error()))
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Warn("heartbeat ping failed", slog.String("target", name), slog.String("error", err.Error()))
		return
	}
	defer resp.Body.Close()

	log.Debug("heartbeat ping ok", slog.String("target", name), slog.Int("status", resp.StatusCode))
}
