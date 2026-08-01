# syntax=docker/dockerfile:1.7

# ---------------------------------------------------------------------------
# Stage 1 - build a static binary
# ---------------------------------------------------------------------------
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# Dependencies are resolved in their own layer so source edits do not bust the
# module cache.
COPY go.mod go.su[m] ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download all || true

COPY . .

ARG VERSION=dev
ARG BUILD_TIME=unknown

# go mod tidy runs here too: the repository ships without go.sum, so this makes
# a fresh clone buildable with no local Go toolchain.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
        -o /out/api ./cmd/api

# ---------------------------------------------------------------------------
# Stage 2 - hot-reload development image (docker-compose.override.yml only)
# ---------------------------------------------------------------------------
FROM golang:1.25-alpine AS dev

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

RUN go install github.com/air-verse/air@v1.66.1

# Source arrives via the bind mount declared in docker-compose.override.yml,
# so there is no COPY here.
CMD ["air", "-c", ".air.toml"]

# ---------------------------------------------------------------------------
# Stage 3 - minimal runtime
# ---------------------------------------------------------------------------
FROM alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates tzdata wget && \
    addgroup -g 10001 -S app && \
    adduser -u 10001 -S -G app app

WORKDIR /app

COPY --from=builder /out/api /app/api
COPY --chown=app:app docs/openapi.yaml /app/docs/openapi.yaml

# Local-storage fallback directory (unused when STORAGE_PROVIDER=s3).
RUN mkdir -p /app/.data/uploads && chown -R app:app /app

USER app:app

EXPOSE 8080

ENV APP_ENV=production \
    PORT=8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/app/api"]
