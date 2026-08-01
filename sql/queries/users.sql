-- name: CreateUser :one
INSERT INTO users (email, password_hash, full_name, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: UpdateUser :one
-- COALESCE lets a single statement act as a partial update: NULL means "leave
-- this column alone", so PATCH semantics need no dynamic SQL.
UPDATE users
SET full_name  = COALESCE(sqlc.narg('full_name')::text, full_name),
    avatar_url = COALESCE(sqlc.narg('avatar_url')::text, avatar_url),
    role       = COALESCE(sqlc.narg('role')::text, role),
    is_active  = COALESCE(sqlc.narg('is_active')::bool, is_active)
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: UpdateLastLogin :exec
UPDATE users SET last_login_at = NOW() WHERE id = $1;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = $1;

-- name: ListUsers :many
SELECT * FROM users
WHERE (sqlc.narg('search')::text IS NULL
        OR email     ILIKE '%' || sqlc.narg('search')::text || '%'
        OR full_name ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('role')::text IS NULL      OR role = sqlc.narg('role')::text)
  AND (sqlc.narg('is_active')::bool IS NULL OR is_active = sqlc.narg('is_active')::bool)
ORDER BY
    CASE WHEN sqlc.arg('sort_by')::text = 'created_at' AND sqlc.arg('sort_order')::text = 'asc'  THEN created_at END ASC,
    CASE WHEN sqlc.arg('sort_by')::text = 'created_at' AND sqlc.arg('sort_order')::text = 'desc' THEN created_at END DESC,
    CASE WHEN sqlc.arg('sort_by')::text = 'updated_at' AND sqlc.arg('sort_order')::text = 'asc'  THEN updated_at END ASC,
    CASE WHEN sqlc.arg('sort_by')::text = 'updated_at' AND sqlc.arg('sort_order')::text = 'desc' THEN updated_at END DESC,
    CASE WHEN sqlc.arg('sort_by')::text = 'email'      AND sqlc.arg('sort_order')::text = 'asc'  THEN email END ASC,
    CASE WHEN sqlc.arg('sort_by')::text = 'email'      AND sqlc.arg('sort_order')::text = 'desc' THEN email END DESC,
    CASE WHEN sqlc.arg('sort_by')::text = 'full_name'  AND sqlc.arg('sort_order')::text = 'asc'  THEN full_name END ASC,
    CASE WHEN sqlc.arg('sort_by')::text = 'full_name'  AND sqlc.arg('sort_order')::text = 'desc' THEN full_name END DESC,
    id ASC
LIMIT sqlc.arg('page_limit')::int OFFSET sqlc.arg('page_offset')::int;

-- name: CountUsers :one
SELECT COUNT(*) FROM users
WHERE (sqlc.narg('search')::text IS NULL
        OR email     ILIKE '%' || sqlc.narg('search')::text || '%'
        OR full_name ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('role')::text IS NULL      OR role = sqlc.narg('role')::text)
  AND (sqlc.narg('is_active')::bool IS NULL OR is_active = sqlc.narg('is_active')::bool);

-- name: EmailExists :one
SELECT EXISTS (SELECT 1 FROM users WHERE email = $1);
