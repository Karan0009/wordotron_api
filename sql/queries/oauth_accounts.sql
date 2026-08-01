-- name: GetOAuthAccount :one
-- Looks up a local account by the external identity. Keyed on the provider's
-- stable subject id, never the email, which the person can change upstream.
SELECT * FROM oauth_accounts
WHERE provider = $1 AND provider_account_id = $2;

-- name: GetOAuthAccountsByUser :many
SELECT * FROM oauth_accounts
WHERE user_id = $1
ORDER BY created_at ASC;

-- name: LinkOAuthAccount :one
INSERT INTO oauth_accounts (user_id, provider, provider_account_id, email)
VALUES ($1, $2, $3, sqlc.narg('email')::citext)
RETURNING *;

-- name: UpdateOAuthAccountEmail :one
-- Keeps the recorded provider email current without touching the local one.
UPDATE oauth_accounts
SET email = sqlc.narg('email')::citext
WHERE provider = $1 AND provider_account_id = $2
RETURNING *;

-- name: UnlinkOAuthAccount :execrows
DELETE FROM oauth_accounts
WHERE user_id = $1 AND provider = $2;

-- name: CreateOAuthUser :one
-- Registration through a provider. The password hash is deliberately absent:
-- NULL means this account has no password to sign in with. The email arrives
-- already verified by the provider, so email_verified_at is set here.
INSERT INTO users (email, full_name, avatar_url, email_verified_at, role)
VALUES (
    $1,
    $2,
    sqlc.narg('avatar_url')::text,
    NOW(),
    'user'
)
RETURNING *;

-- name: MarkEmailVerified :one
-- Used when an existing password account is linked to a provider that has
-- already verified the same address.
UPDATE users
SET email_verified_at = COALESCE(email_verified_at, NOW())
WHERE id = $1
RETURNING *;

-- name: SetUserAvatarIfEmpty :one
-- The provider's picture is only adopted when the person has not chosen one,
-- so a later sign-in never overwrites an uploaded avatar.
UPDATE users
SET avatar_url = COALESCE(avatar_url, sqlc.narg('avatar_url')::text)
WHERE id = $1
RETURNING *;
