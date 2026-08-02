-- name: CreateWord :one
INSERT INTO words (term, language, pronunciation, tags, created_by)
VALUES (
    $1,
    $2,
    sqlc.narg('pronunciation')::text,
    sqlc.arg('tags')::text[],
    sqlc.narg('created_by')::uuid
)
RETURNING *;

-- name: GetWord :one
SELECT * FROM words WHERE id = $1;

-- name: GetWordByTerm :one
SELECT * FROM words WHERE language = $1 AND term = $2;

-- name: UpdateWord :one
-- Partial update: every field falls back to its current value, so a caller
-- sends only what changed.
UPDATE words
SET term          = COALESCE(sqlc.narg('term')::citext, term),
    language      = COALESCE(sqlc.narg('language')::text, language),
    pronunciation = CASE WHEN sqlc.arg('clear_pronunciation')::bool THEN NULL
                        ELSE COALESCE(sqlc.narg('pronunciation')::text, pronunciation) END,
    tags          = COALESCE(sqlc.narg('tags')::text[], tags)
WHERE id = sqlc.arg('id')::uuid
RETURNING *;

-- name: DeleteWord :execrows
DELETE FROM words WHERE id = $1;

-- name: ListWords :many
SELECT * FROM words
WHERE (sqlc.narg('search')::text IS NULL       OR term ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('language')::text IS NULL     OR language = sqlc.narg('language')::text)
  -- Tag filter is a containment check, so it uses the GIN index.
  AND (sqlc.narg('tags')::text[] IS NULL       OR tags @> sqlc.narg('tags')::text[])
  AND (sqlc.narg('created_by')::uuid IS NULL   OR created_by = sqlc.narg('created_by')::uuid)
ORDER BY
    CASE WHEN sqlc.arg('sort_by')::text = 'created_at' AND sqlc.arg('sort_order')::text = 'asc'  THEN created_at END ASC,
    CASE WHEN sqlc.arg('sort_by')::text = 'created_at' AND sqlc.arg('sort_order')::text = 'desc' THEN created_at END DESC,
    CASE WHEN sqlc.arg('sort_by')::text = 'updated_at' AND sqlc.arg('sort_order')::text = 'asc'  THEN updated_at END ASC,
    CASE WHEN sqlc.arg('sort_by')::text = 'updated_at' AND sqlc.arg('sort_order')::text = 'desc' THEN updated_at END DESC,
    CASE WHEN sqlc.arg('sort_by')::text = 'term'       AND sqlc.arg('sort_order')::text = 'asc'  THEN term END ASC,
    CASE WHEN sqlc.arg('sort_by')::text = 'term'       AND sqlc.arg('sort_order')::text = 'desc' THEN term END DESC,
    id ASC
LIMIT sqlc.arg('page_limit')::int OFFSET sqlc.arg('page_offset')::int;

-- name: CountWords :one
SELECT COUNT(*) FROM words
WHERE (sqlc.narg('search')::text IS NULL       OR term ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('language')::text IS NULL     OR language = sqlc.narg('language')::text)
  AND (sqlc.narg('tags')::text[] IS NULL       OR tags @> sqlc.narg('tags')::text[])
  AND (sqlc.narg('created_by')::uuid IS NULL   OR created_by = sqlc.narg('created_by')::uuid);

-- name: ListWordTags :many
-- Distinct tags with usage counts, for filter menus and autocomplete.
SELECT tag, COUNT(*)::bigint AS uses
FROM words, UNNEST(tags) AS tag
WHERE (sqlc.narg('language')::text IS NULL OR language = sqlc.narg('language')::text)
GROUP BY tag
ORDER BY uses DESC, tag ASC
LIMIT sqlc.arg('page_limit')::int;
