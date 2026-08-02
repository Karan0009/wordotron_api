# Backend API

Production-oriented Go backend for a Vite + React + TypeScript PWA.

Fiber v3 · PostgreSQL (pgx + sqlc) · Redis · JWT with refresh-token rotation ·
golang-migrate · Clean Architecture.

---

## Contents

- [Quick start](#quick-start)
- [Architecture](#architecture)
- [Configuration](#configuration)
- [API](#api)
- [Development](#development)
- [Database](#database)
- [Testing](#testing)
- [Deployment](#deployment)
- [Design decisions](#design-decisions)
- [Connecting the frontend](#connecting-the-frontend)

---

## Quick start

### With Docker Compose (recommended)

```bash
cp .env.example .env

# Generate real secrets - the API refuses to start in production without them.
sed -i "s|^JWT_SECRET=.*|JWT_SECRET=$(openssl rand -base64 48 | tr -d '\n')|" .env
sed -i "s|^REFRESH_SECRET=.*|REFRESH_SECRET=$(openssl rand -base64 48 | tr -d '\n')|" .env

make docker-up          # postgres + redis + migrations + api
curl localhost:8080/health
```

`docker compose up` runs the migration container to completion before the API
starts, so the schema is always in place on first boot.

Interactive documentation: <http://localhost:8080/docs>

### Locally, against containerised dependencies

```bash
go mod tidy             # required once: the repository ships without go.sum
./scripts/dev.sh        # starts postgres + redis, migrates, runs the API
```

Optional development accounts:

```bash
make seed
# admin@example.com / Admin1234Secure
# user@example.com  / User12345Secure
```

### Prerequisites

| Tool | Version | Needed for |
| --- | --- | --- |
| Go | 1.25+ | Fiber v3 requires it |
| Docker + Compose | recent | Local stack and integration tests |
| [golang-migrate](https://github.com/golang-migrate/migrate) | v4 | `make migrate` |
| [sqlc](https://sqlc.dev) | v1.28+ | `make sqlc` |
| [golangci-lint](https://golangci-lint.run) | v2 | `make lint` |

Only Go and Docker are needed to run the service; the rest are for development.

---

## Architecture

Dependencies point inwards. The transport layer knows about services, services
know about repository interfaces, and nothing in the core knows about HTTP.

```
        ┌────────────────────────────────────────────────────────┐
HTTP →  │ server → routes → middleware → handlers                │  transport
        ├────────────────────────────────────────────────────────┤
        │ services                       (business rules, authz) │  use cases
        ├────────────────────────────────────────────────────────┤
        │ repository (interfaces)  →  sqlc queries                │  persistence
        ├────────────────────────────────────────────────────────┤
        │ models · lib/auth · lib/storage · config                │  domain + ports
        └────────────────────────────────────────────────────────┘
```

```
backend/
├── cmd/
│   └── app_main.go            entry point: config, wiring, graceful shutdown
├── app/
│   ├── config/                env loading and validation
│   ├── handlers/               HTTP handlers
│   ├── serializers/           request/response DTOs
│   ├── services/              business logic
│   ├── models/                domain types
│   ├── repository/            repository interfaces + sqlc-backed implementations
│   │   └── db/                 generated code (do not edit)
│   ├── middleware/            request ID, logging, auth, rate limit, errors
│   ├── routes/                route table (Dependencies + RegisterRoutes)
│   ├── server/                Fiber app construction + middleware stack
│   └── lib/                   cross-cutting infra
│       ├── wordotronDb.go      pgx pool + transaction helper
│       ├── redis.go            redis client
│       ├── utils.go            response envelope, pagination parsing
│       ├── errors.go           typed errors with HTTP mapping
│       ├── logger.go           slog setup and context propagation
│       ├── auth/               bcrypt, JWT, Redis session store
│       ├── storage/            object storage: local | S3 | MinIO
│       └── validation/         validator wiring and error translation
├── migrations/                golang-migrate SQL files
├── sql/queries/                sqlc source queries
├── docs/openapi.yaml           API specification
├── scripts/                    seed data, dev helper
└── tests/integration/          testcontainers end-to-end tests
```

Every collaborator is an interface passed through a constructor. There is no
global mutable state and no service locator, so any layer can be replaced with a
fake in a test.

---

## Configuration

All configuration comes from the environment; `.env` is loaded when present.
See [`.env.example`](.env.example) for the full annotated list.

| Variable | Default | Notes |
| --- | --- | --- |
| `APP_ENV` | `development` | `development` \| `staging` \| `production` |
| `PORT` | `8080` | |
| `FRONTEND_URL` | `http://localhost:5173` | Used to build password-reset links |
| `DATABASE_URL` | — | Required |
| `REDIS_URL` | — | Required |
| `JWT_SECRET` | — | Required, ≥ 32 chars |
| `JWT_EXPIRY` | `15m` | Must be ≤ 1h |
| `REFRESH_SECRET` | — | Required, ≥ 32 chars, must differ from `JWT_SECRET` |
| `REFRESH_EXPIRY` | `720h` | |
| `BCRYPT_COST` | `12` | |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:5173` | Comma separated; `*` is rejected in production |
| `RATE_LIMIT_MAX` / `RATE_LIMIT_WINDOW` | `120` / `1m` | Global limit |
| `AUTH_RATE_LIMIT_MAX` / `AUTH_RATE_LIMIT_WINDOW` | `10` / `1m` | Credential endpoints |
| `STORAGE_PROVIDER` | `local` | `local` \| `s3` (MinIO uses `s3`) |
| `BODY_LIMIT_BYTES` | `4194304` | Maximum request body |
| `TRUSTED_PROXIES` | empty | Required before `X-Forwarded-For` is honoured |

Configuration is validated at start-up. A weak secret, a wildcard CORS origin in
production or an unreachable dependency stops the process with a readable
message rather than failing later under load.

---

## API

Base path `/api/v1`. Full specification at `/docs` (Swagger UI) or
[`docs/openapi.yaml`](docs/openapi.yaml).

### Endpoints

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| `POST` | `/auth/register` | — | Create an account and email a verification link |
| `POST` | `/auth/verify-email` | — | Consume a verification token, unblocks login |
| `POST` | `/auth/login` | — | Exchange credentials for tokens |
| `POST` | `/auth/refresh` | — | Rotate the refresh token |
| `POST` | `/auth/logout` | bearer | Revoke the current session |
| `POST` | `/auth/logout-all` | bearer | Revoke every session |
| `POST` | `/auth/change-password` | bearer | Change password, reissue tokens |
| `POST` | `/auth/forgot-password` | — | Email a reset link |
| `POST` | `/auth/reset-password` | — | Consume a reset token |
| `GET` | `/users/me` | bearer | Current profile |
| `PATCH` | `/users/me` | bearer | Update own profile |
| `POST` | `/users/me/avatar` | bearer | Upload an avatar (multipart) |
| `GET` | `/users` | admin | Paginated, filtered list |
| `POST` | `/users` | admin | Create a user with a role |
| `GET` | `/users/{id}` | bearer | Own record, or any as admin |
| `PATCH` | `/users/{id}` | bearer | Own record, or any as admin |
| `DELETE` | `/users/{id}` | bearer | Own record, or any as admin |
| `GET` | `/health` | — | `{"status":"ok"}` |
| `GET` | `/health/ready` | — | Dependency checks, 503 when degraded |

### Response envelope

```jsonc
// success
{ "success": true, "data": { } }

// paginated
{ "success": true, "data": [ ], "meta": { "page": 1, "limit": 20, "total": 42,
  "total_pages": 3, "has_next": true, "has_prev": false } }

// error
{ "success": false, "message": "The request contains invalid fields",
  "code": "VALIDATION_ERROR",
  "errors": [ { "field": "email", "message": "Must be a valid email address" } ],
  "request_id": "0f2b0e2c-3a1d-4c9a-9e21-6b7c8d9e0f11" }
```

`request_id` matches the `X-Request-ID` header and every server log line for
that request, which makes support tickets traceable.

### Listing and filtering

```
GET /api/v1/users?page=2&limit=25&sort=created_at&order=desc&search=jane&role=admin&is_active=true
```

`sort` is validated against an allow-list of columns, so the parameter can never
reach SQL as free text.

---

## Development

```bash
make help              # list every target
make run               # run the API
make test              # unit tests with the race detector
make test-integration  # containerised end-to-end tests
make lint              # golangci-lint
make fmt vet           # formatting and vetting
make sqlc              # regenerate query code
make migrate           # apply migrations
make hooks             # install pre-commit hooks
```

Because the repository ships without `go.sum`, run `go mod tidy` (or
`make tidy`) once after cloning.

---

## Database

Queries are written as SQL in `sql/queries/` and compiled into type-safe Go by
sqlc. Handlers never contain SQL and the query layer is never bypassed.

```bash
make migrate-create NAME=add_widgets   # scaffold a migration pair
make migrate                           # apply
make migrate-down                      # roll back one step
make sqlc                              # regenerate after editing queries
```

Multi-statement operations use `Store.WithTx`, which hands the closure a store
bound to a single transaction:

```go
err := store.WithTx(ctx, func(tx repository.Store) error {
    if err := tx.PasswordResets().MarkUsed(ctx, tokenID); err != nil {
        return err
    }
    return tx.Users().UpdatePassword(ctx, userID, hash)
})
```

---

## Testing

| Layer | Location | Dependencies |
| --- | --- | --- |
| Unit | `app/**/*_test.go` | None |
| Handler | `app/handlers/*_test.go` | `httptest` + service stubs |
| Integration | `tests/integration/` | Docker (testcontainers) |

Integration tests start real Postgres and Redis containers, run the migrations
and exercise the full HTTP stack, covering registration, login, refresh-token
rotation and reuse detection, logout blacklisting, password changes, and
role-based authorisation.

```bash
make test               # fast, no Docker
make test-integration   # requires a running Docker daemon
make test-cover         # writes coverage.html
```

---

## Deployment

The image is multi-stage: a `golang:1.25-alpine` builder produces a static,
trimmed binary that runs on Alpine as UID 10001 with a `HEALTHCHECK`.

```bash
make docker
docker run --env-file .env -p 8080:8080 backend-api:latest
```

Point orchestrator readiness probes at `/health/ready` (which verifies Postgres
and Redis) and liveness at `/health`. On `SIGTERM` the server stops accepting
connections and drains in-flight requests within `SHUTDOWN_TIMEOUT`.

CI (`.github/workflows/ci.yml`) runs `go mod tidy` drift detection, `gofmt`,
`go vet`, golangci-lint, unit tests, integration tests, and both a binary and a
Docker build.

### Production checklist

- [ ] Distinct 48-byte random values for `JWT_SECRET` and `REFRESH_SECRET`
- [ ] `APP_ENV=production`, `LOG_FORMAT=json`
- [ ] `CORS_ALLOWED_ORIGINS` set to the exact SPA origins
- [ ] `TRUSTED_PROXIES` set to your load balancer CIDRs (otherwise the rate
      limiter buckets everyone into a single IP)
- [ ] `STORAGE_PROVIDER=s3` for more than one replica
- [ ] TLS terminated in front of the service; `COOKIE_SECURE=true`
- [ ] Password-reset links are only logged (`app/services/auth.go`), not emailed — wire up real delivery before relying on this in production
- [ ] Database backups and connection-pool sizing reviewed against `DB_MAX_CONNS`

---

## Design decisions

**Refresh tokens live in Redis, keyed per user.** A refresh token is a JWT
signed with a separate secret, but validity is decided by Redis, so revocation
is immediate. Tokens rotate on every refresh; presenting a rotated-out token is
treated as theft and drops every session for that account.

**Access tokens are revocable.** Logout blacklists the token's JTI for its
remaining lifetime, and password changes, role changes and deactivation write a
per-user revocation epoch that invalidates every token issued before it. Both
checks are one pipelined Redis round trip.

**Authorisation lives in the service layer**, not in route middleware, so the
same rules apply if the use case is later called from a worker or a gRPC
endpoint. Middleware only answers "who is this?".

**Errors are values.** `apperror.Error` carries a code, an HTTP status, a
client-safe message and optional field errors. Handlers return them; one
`ErrorHandler` renders them. Internal causes are logged, never serialised.

**Roles are a CHECK-constrained text column** rather than a Postgres enum, so
adding a role is an ordinary migration instead of an `ALTER TYPE` with locking
implications.

**Sorting is allow-listed.** Dynamic `ORDER BY` is expressed as `CASE`
expressions over bound parameters, so a client cannot influence the query plan
or inject SQL through `?sort=`.

**Account enumeration is treated as a bug.** Login returns one message and one
timing profile for unknown addresses and wrong passwords, and
`/auth/forgot-password` always returns the same 200.

**Registration requires email verification.** `/auth/register` creates the
account and sends (logs, until a mailer is wired up) a single-use verification
link; no session is issued. `/auth/login` rejects the account until
`/auth/verify-email` consumes that link. Verification tokens follow the same
scheme as password-reset tokens (random 256-bit value, SHA-256 hash stored,
single use, expiring) rather than a JWT — a real refresh token is a live login
credential, so it's the wrong thing to put in an email.

**The storage provider is a port.** `storage.Storage` has three
implementations' worth of surface behind one interface; switching from local
disk to MinIO or S3 is an environment-variable change.

**Rate limiting fails open.** A Redis outage degrades throttling instead of
taking down the API; the incident is logged.

---

## Connecting the frontend

Vite dev server on `http://localhost:5173`, API on `http://localhost:8080`.
Proxy in `vite.config.ts` to keep the SPA on one origin:

```ts
export default defineConfig({
  server: {
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: true },
    },
  },
});
```

A minimal client that refreshes transparently on `401`:

```ts
let accessToken: string | null = null;

async function request(path: string, init: RequestInit = {}): Promise<Response> {
  const send = () =>
    fetch(`/api/v1${path}`, {
      ...init,
      headers: {
        "Content-Type": "application/json",
        ...(accessToken ? { Authorization: `Bearer ${accessToken}` } : {}),
        ...init.headers,
      },
    });

  let response = await send();
  if (response.status !== 401) return response;

  // Rotate once, then replay the original request.
  const refreshed = await fetch("/api/v1/auth/refresh", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ refresh_token: localStorage.getItem("refresh_token") }),
  });
  if (!refreshed.ok) {
    accessToken = null;
    throw new Error("session expired");
  }

  const { data } = await refreshed.json();
  accessToken = data.tokens.access_token;
  localStorage.setItem("refresh_token", data.tokens.refresh_token);
  return send();
}
```

Keep the access token in memory only. For the refresh token, set
`REFRESH_COOKIE_ENABLED=true` and the API will also issue it as an `HttpOnly`,
`SameSite=Strict` cookie scoped to `/api/v1/auth`, which puts it out of reach of
XSS; the client can then call `/auth/refresh` with an empty body.

Because the PWA service worker may replay requests after a period offline,
handle `401` on any cached request by refreshing rather than logging out, and
treat `409` on registration as a normal form error rather than a failure.

---

## License

MIT
