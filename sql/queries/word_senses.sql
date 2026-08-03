-- name: CreateWordSense :one
INSERT INTO word_senses (word_id, part_of_speech, definition, example, meta, sense_order)
VALUES (
    $1,
    sqlc.narg('part_of_speech')::text,
    $2,
    sqlc.narg('example')::text,
    COALESCE(sqlc.narg('meta')::jsonb, '{}'::jsonb),
    sqlc.arg('sense_order')::smallint
)
RETURNING *;

-- name: GetWordSense :one
SELECT * FROM word_senses WHERE id = $1;

-- name: ListWordSensesByWord :many
SELECT * FROM word_senses WHERE word_id = $1 ORDER BY sense_order ASC, created_at ASC;

-- name: ListWordSensesByWordIDs :many
-- Powers word listing: one query for every sense of every word on the page,
-- instead of one round trip per word.
SELECT * FROM word_senses
WHERE word_id = ANY(sqlc.arg('word_ids')::uuid[])
ORDER BY word_id, sense_order ASC, created_at ASC;

-- name: UpdateWordSense :one
-- Partial update: every field falls back to its current value, so a caller
-- sends only what changed.
UPDATE word_senses
SET part_of_speech = CASE WHEN sqlc.arg('clear_part_of_speech')::bool THEN NULL
                          ELSE COALESCE(sqlc.narg('part_of_speech')::text, part_of_speech) END,
    definition      = COALESCE(sqlc.narg('definition')::text, definition),
    example         = CASE WHEN sqlc.arg('clear_example')::bool THEN NULL
                          ELSE COALESCE(sqlc.narg('example')::text, example) END,
    meta            = COALESCE(sqlc.narg('meta')::jsonb, meta)
WHERE id = sqlc.arg('id')::uuid
RETURNING *;

-- name: DeleteWordSense :execrows
DELETE FROM word_senses WHERE id = $1;

-- name: DeleteWordSensesByWord :exec
DELETE FROM word_senses WHERE word_id = $1;
