BEGIN;

-- citext gives us case-insensitive, still-indexable email addresses without
-- sprinkling lower() over every query.
CREATE EXTENSION IF NOT EXISTS citext;

-- Role is a CHECK-constrained text column rather than a native enum: adding a
-- role later is a one-line migration instead of an ALTER TYPE dance, and sqlc
-- maps it to a plain string.
CREATE TABLE IF NOT EXISTS users (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email             CITEXT      NOT NULL UNIQUE,
    password_hash     TEXT        NOT NULL,
    full_name         TEXT        NOT NULL,
    role              TEXT        NOT NULL DEFAULT 'user'
                                  CHECK (role IN ('user', 'admin')),
    avatar_url        TEXT,
    is_active         BOOLEAN     NOT NULL DEFAULT TRUE,
    email_verified_at TIMESTAMPTZ,
    last_login_at     TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS users_created_at_idx ON users (created_at DESC);
CREATE INDEX IF NOT EXISTS users_role_idx        ON users (role);

-- Trigram index keeps `?search=` scans off sequential plans as the table grows.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS users_full_name_trgm_idx ON users USING GIN (full_name gin_trgm_ops);
CREATE INDEX IF NOT EXISTS users_email_trgm_idx     ON users USING GIN (email gin_trgm_ops);

-- Password reset tokens live in Postgres (not Redis) because they are low
-- volume, single use, and worth auditing after the fact.
CREATE TABLE IF NOT EXISTS password_reset_tokens (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash TEXT        NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS password_reset_tokens_user_id_idx    ON password_reset_tokens (user_id);
CREATE INDEX IF NOT EXISTS password_reset_tokens_expires_at_idx ON password_reset_tokens (expires_at);

CREATE OR REPLACE FUNCTION set_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

COMMIT;
