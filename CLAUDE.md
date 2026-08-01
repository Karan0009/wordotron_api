# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Production-oriented Go backend for a Vite + React + TypeScript PWA. Fiber v3 ·
PostgreSQL (pgx + sqlc) · Redis · JWT with refresh-token rotation ·
golang-migrate · Clean Architecture.

## Commands

```bash
go mod tidy             # required once after cloning: repo ships without go.sum
./scripts/dev.sh        # starts postgres + redis, migrates, runs the API locally

make run                # run the API (requires postgres + redis reachable)
make build              # compile static binary to ./bin
make test               # unit tests, race detector, no external deps
make test-integration   # testcontainers end-to-end tests (needs Docker daemon)
make test-cover         # unit tests + coverage.html
make lint               # golangci-lint
make fmt vet            # gofmt + go vet
make sqlc               # regenerate internal/repository/db from sql/queries
make migrate            # apply pending migrations
make migrate-down       # roll back most recent migration
make migrate-create NAME=add_widgets   # scaffold a migration pair
make seed               # insert dev seed data (admin@example.com / user@example.com)
make docker-up          # full stack via docker compose (api + postgres + redis + migrations)
```

Single test: `go test -race -run TestName ./internal/handlers/...` (swap the
package path). Integration tests live under `./tests/...` and require
`-tags=integration`.

## Architecture

Dependencies point inward. Transport knows about services; services know about
repository interfaces; nothing in the core knows about HTTP.

```
routes → middleware → handlers        transport
service (business rules, authz)       use cases
repository (interfaces) → sqlc        persistence
models · auth · storage · mailer · config   domain + ports
```

- `internal/handlers/` — HTTP handlers + DTOs. No SQL, no business rules.
- `internal/service/` — business logic and **authorization** (not middleware —
  middleware only answers "who is this?"; authz decisions live here so the
  same rules apply if a use case is later called from a worker or gRPC).
- `internal/repository/` — repository interfaces plus sqlc-backed
  implementations. `internal/repository/db/` is sqlc-generated; never edit by
  hand, run `make sqlc` after changing `sql/queries/*.sql`.
- `internal/auth/` — bcrypt, JWT, Redis session store.
- `internal/storage/` — object storage port with local / S3 / MinIO
  implementations selected by `STORAGE_PROVIDER`.
- `internal/middleware/` — request ID, logging, auth, rate limit, error
  rendering.
- `pkg/apperror/` — typed errors (code, HTTP status, client-safe message,
  optional field errors). Handlers return them; one `ErrorHandler` renders
  them. Internal causes are logged, never serialized to the client.
- `pkg/logger/` — slog setup and context propagation.
- Every collaborator is an interface passed through a constructor — no global
  mutable state, no service locator, so any layer can be faked in tests.

### Key design decisions (see README "Design decisions" for full rationale)

- Refresh tokens are JWTs whose validity is decided by Redis (keyed per
  user), so revocation is immediate. Rotation on every refresh; presenting a
  rotated-out token drops every session for that account (theft response).
- Access tokens are revocable via a blacklisted JTI (logout) and a per-user
  revocation epoch (password/role change, deactivation) — both checked in one
  pipelined Redis round trip.
- Multi-statement DB operations use `Store.WithTx`, which hands the closure a
  store bound to a single transaction (see README "Database" for the
  pattern).
- Roles are a CHECK-constrained text column, not a Postgres enum, so adding a
  role is an ordinary migration.
- Sorting is allow-listed and expressed as `CASE` over bound parameters —
  never format a client `?sort=` value into SQL.
- Login/forgot-password intentionally avoid account enumeration: one message,
  one timing profile, always-200 on forgot-password.
- Rate limiting fails open on Redis outage rather than taking down the API.
- Config is validated at startup (weak secrets, wildcard CORS in production,
  unreachable dependencies stop the process immediately with a readable
  error) — see `.env.example` and `internal/config/config.go`.

### Response envelope

Success: `{ "success": true, "data": {} }`. Paginated responses add `meta`
(`page`, `limit`, `total`, `total_pages`, `has_next`, `has_prev`). Errors:
`{ "success": false, "message", "code", "errors": [{field, message}], "request_id" }`.
`request_id` matches `X-Request-ID` and every server log line for the
request.

## Database

SQL lives in `sql/queries/*.sql`; sqlc compiles it into
`internal/repository/db/` (generated, do not edit). Handlers never contain
SQL and the query layer is never bypassed. After editing queries or adding a
migration, run `make sqlc`. `scripts/check-queries.py` exists for query
sanity checks.

## Linting

`golangci-lint` config is in `.golangci.yml` (v2). Generated code under
`internal/repository/db/` is excluded from most linters. Local import prefix
is `github.com/Karan0009/wordotron_api` (update in `.golangci.yml` if the module path
changes).
