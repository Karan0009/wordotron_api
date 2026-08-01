-- name: CreatePasswordResetToken :one
INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetPasswordResetToken :one
SELECT * FROM password_reset_tokens WHERE token_hash = $1;

-- name: MarkPasswordResetTokenUsed :execrows
UPDATE password_reset_tokens
SET used_at = NOW()
WHERE id = $1 AND used_at IS NULL;

-- name: InvalidateUserPasswordResetTokens :exec
UPDATE password_reset_tokens
SET used_at = NOW()
WHERE user_id = $1 AND used_at IS NULL;

-- name: DeleteExpiredPasswordResetTokens :execrows
DELETE FROM password_reset_tokens WHERE expires_at < NOW();
