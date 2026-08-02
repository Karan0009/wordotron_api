-- name: CreateEmailVerificationToken :one
INSERT INTO email_verification_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetEmailVerificationToken :one
SELECT * FROM email_verification_tokens WHERE token_hash = $1;

-- name: MarkEmailVerificationTokenUsed :execrows
UPDATE email_verification_tokens
SET used_at = NOW()
WHERE id = $1 AND used_at IS NULL;

-- name: InvalidateUserEmailVerificationTokens :exec
UPDATE email_verification_tokens
SET used_at = NOW()
WHERE user_id = $1 AND used_at IS NULL;

-- name: DeleteExpiredEmailVerificationTokens :execrows
DELETE FROM email_verification_tokens WHERE expires_at < NOW();
