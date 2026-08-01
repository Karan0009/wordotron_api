#!/usr/bin/env bash
#
# Bring up the local dependencies (postgres + redis) and run the API on the
# host, so `go run` restarts are instant and the debugger attaches normally.
set -euo pipefail

cd "$(dirname "$0")/.."

if [[ ! -f .env ]]; then
    echo "==> creating .env from .env.example"
    cp .env.example .env
    echo "==> generating development secrets"
    JWT_SECRET="$(openssl rand -base64 48 | tr -d '\n')"
    REFRESH_SECRET="$(openssl rand -base64 48 | tr -d '\n')"
    sed -i.bak "s|^JWT_SECRET=.*|JWT_SECRET=${JWT_SECRET}|" .env
    sed -i.bak "s|^REFRESH_SECRET=.*|REFRESH_SECRET=${REFRESH_SECRET}|" .env
    rm -f .env.bak
fi

echo "==> starting postgres and redis"
docker compose up -d postgres redis

echo "==> waiting for postgres"
until docker compose exec -T postgres pg_isready -U postgres >/dev/null 2>&1; do
    sleep 1
done

echo "==> applying migrations"
make migrate

echo "==> starting the api"
exec go run ./cmd/api
