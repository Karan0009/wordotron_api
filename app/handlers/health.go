package handlers

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// HealthHandler answers liveness and readiness probes.
type HealthHandler struct {
	pool      *pgxpool.Pool
	redis     redis.UniversalClient
	version   string
	env       string
	startedAt time.Time
}

// NewHealthHandler builds the health handler.
func NewHealthHandler(pool *pgxpool.Pool, redisClient redis.UniversalClient, version, env string) *HealthHandler {
	return &HealthHandler{
		pool:      pool,
		redis:     redisClient,
		version:   version,
		env:       env,
		startedAt: time.Now(),
	}
}

// Health is the shallow probe used by Docker and load balancers.
//
//	@Summary		Health check
//	@Description	Returns 200 as long as the process is running. Does not touch the database.
//	@Tags			health
//	@Produce		json
//	@Success		200	{object}	map[string]string
//	@Router			/health [get]
func (h *HealthHandler) Health(c fiber.Ctx) error {
	// Deliberately not wrapped in the standard envelope: probes and uptime
	// checks expect this exact body.
	return c.JSON(fiber.Map{"status": "ok"})
}

// Live reports process liveness plus build metadata.
//
//	@Summary		Liveness probe
//	@Tags			health
//	@Produce		json
//	@Success		200	{object}	map[string]any
//	@Router			/health/live [get]
func (h *HealthHandler) Live(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status":  "ok",
		"version": h.version,
		"env":     h.env,
		"uptime":  time.Since(h.startedAt).Round(time.Second).String(),
	})
}

// Ready verifies that every dependency the API needs is reachable. Kubernetes
// should route traffic based on this endpoint, not /health.
//
//	@Summary		Readiness probe
//	@Description	Checks Postgres and Redis connectivity. Returns 503 when a dependency is down.
//	@Tags			health
//	@Produce		json
//	@Success		200	{object}	map[string]any
//	@Failure		503	{object}	map[string]any
//	@Router			/health/ready [get]
func (h *HealthHandler) Ready(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
	defer cancel()

	checks := fiber.Map{}
	ready := true

	if err := h.pool.Ping(ctx); err != nil {
		checks["postgres"] = "unavailable"
		ready = false
	} else {
		checks["postgres"] = "ok"
	}

	if err := h.redis.Ping(ctx).Err(); err != nil {
		checks["redis"] = "unavailable"
		ready = false
	} else {
		checks["redis"] = "ok"
	}

	status := fiber.StatusOK
	overall := "ok"
	if !ready {
		status = fiber.StatusServiceUnavailable
		overall = "degraded"
	}

	return c.Status(status).JSON(fiber.Map{
		"status": overall,
		"checks": checks,
	})
}
