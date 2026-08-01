BEGIN;

-- An account created through Google has no password. Making the column
-- nullable is the honest representation: NULL means "this account cannot be
-- signed into with a password", which is exactly what the login path checks.
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;

-- One row per external identity. Keeping this separate from users means a
-- person can attach several providers later without another schema change,
-- and revoking one provider is a DELETE rather than a nullable column dance.
CREATE TABLE IF NOT EXISTS oauth_accounts (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider            TEXT        NOT NULL CHECK (provider IN ('google')),
    -- Google's `sub`: stable for the life of the account, unlike the email.
    provider_account_id TEXT        NOT NULL,
    email               CITEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- An external identity belongs to exactly one local account.
    UNIQUE (provider, provider_account_id),
    -- And a local account links each provider at most once.
    UNIQUE (user_id, provider)
);

CREATE INDEX IF NOT EXISTS oauth_accounts_user_id_idx ON oauth_accounts (user_id);

CREATE TRIGGER oauth_accounts_set_updated_at
    BEFORE UPDATE ON oauth_accounts
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

COMMIT;
