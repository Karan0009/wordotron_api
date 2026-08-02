-- name: AddWordToUser :one
-- Idempotent by design: adding a word already in the list is not an error, it
-- just returns the existing row untouched so the client can be naive about it.
INSERT INTO user_words (user_id, word_id, status, notes, is_favourite)
VALUES (
    $1,
    $2,
    COALESCE(sqlc.narg('status')::text, 'learning'),
    sqlc.narg('notes')::text,
    COALESCE(sqlc.narg('is_favourite')::bool, FALSE)
)
ON CONFLICT (user_id, word_id) DO UPDATE
    SET updated_at = user_words.updated_at
RETURNING *;

-- name: GetUserWord :one
SELECT * FROM user_words WHERE user_id = $1 AND word_id = $2;

-- name: UpdateUserWord :one
UPDATE user_words
SET status       = COALESCE(sqlc.narg('status')::text, status),
    notes        = CASE WHEN sqlc.arg('clear_notes')::bool THEN NULL
                        ELSE COALESCE(sqlc.narg('notes')::text, notes) END,
    is_favourite = COALESCE(sqlc.narg('is_favourite')::bool, is_favourite)
WHERE user_id = sqlc.arg('user_id')::uuid AND word_id = sqlc.arg('word_id')::uuid
RETURNING *;

-- name: RemoveWordFromUser :execrows
DELETE FROM user_words WHERE user_id = $1 AND word_id = $2;

-- name: RecordReview :one
-- Leitner scheduling, done in one statement so the counters and the next due
-- date can never disagree. A correct answer promotes the card one box (max 5),
-- a wrong answer sends it back to box 0 for same-day repetition. Intervals are
-- 0d, 1d, 2d, 4d, 7d, 15d.
UPDATE user_words
SET review_count     = review_count + 1,
    correct_count    = correct_count + CASE WHEN sqlc.arg('correct')::bool THEN 1 ELSE 0 END,
    box              = CASE WHEN sqlc.arg('correct')::bool THEN LEAST(box + 1, 5) ELSE 0 END,
    status           = CASE
                           WHEN status = 'archived' THEN status
                           WHEN sqlc.arg('correct')::bool AND box + 1 >= 5 THEN 'known'
                           ELSE 'learning'
                       END,
    last_reviewed_at = NOW(),
    due_at           = NOW() + (CASE
                           WHEN NOT sqlc.arg('correct')::bool THEN INTERVAL '10 minutes'
                           WHEN box + 1 = 1 THEN INTERVAL '1 day'
                           WHEN box + 1 = 2 THEN INTERVAL '2 days'
                           WHEN box + 1 = 3 THEN INTERVAL '4 days'
                           WHEN box + 1 = 4 THEN INTERVAL '7 days'
                           ELSE INTERVAL '15 days'
                       END)
WHERE user_id = sqlc.arg('user_id')::uuid AND word_id = sqlc.arg('word_id')::uuid
RETURNING *;

-- name: ListUserWords :many
-- The list view joins the catalogue so one round trip returns everything the
-- client renders. Column names are prefixed to keep them unambiguous in Go.
SELECT
    uw.user_id,
    uw.word_id,
    uw.status,
    uw.notes,
    uw.is_favourite,
    uw.review_count,
    uw.correct_count,
    uw.box,
    uw.last_reviewed_at,
    uw.due_at,
    uw.added_at,
    uw.updated_at,
    w.term           AS word_term,
    w.language       AS word_language,
    w.pronunciation  AS word_pronunciation,
    w.tags           AS word_tags,
    w.created_by     AS word_created_by,
    w.created_at     AS word_created_at,
    w.updated_at     AS word_updated_at
FROM user_words uw
JOIN words w ON w.id = uw.word_id
WHERE uw.user_id = sqlc.arg('user_id')::uuid
  -- Definitions live on word_senses now; this only matches the term itself.
  AND (sqlc.narg('search')::text IS NULL
        OR w.term ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('status')::text IS NULL       OR uw.status = sqlc.narg('status')::text)
  AND (sqlc.narg('language')::text IS NULL     OR w.language = sqlc.narg('language')::text)
  AND (sqlc.narg('tags')::text[] IS NULL       OR w.tags @> sqlc.narg('tags')::text[])
  AND (sqlc.narg('is_favourite')::bool IS NULL OR uw.is_favourite = sqlc.narg('is_favourite')::bool)
  -- `due_only` selects the review queue: everything scheduled for now or past.
  AND (NOT sqlc.arg('due_only')::bool          OR (uw.due_at <= NOW() AND uw.status <> 'archived'))
ORDER BY
    CASE WHEN sqlc.arg('sort_by')::text = 'added_at' AND sqlc.arg('sort_order')::text = 'asc'  THEN uw.added_at END ASC,
    CASE WHEN sqlc.arg('sort_by')::text = 'added_at' AND sqlc.arg('sort_order')::text = 'desc' THEN uw.added_at END DESC,
    CASE WHEN sqlc.arg('sort_by')::text = 'due_at'   AND sqlc.arg('sort_order')::text = 'asc'  THEN uw.due_at END ASC,
    CASE WHEN sqlc.arg('sort_by')::text = 'due_at'   AND sqlc.arg('sort_order')::text = 'desc' THEN uw.due_at END DESC,
    CASE WHEN sqlc.arg('sort_by')::text = 'term'     AND sqlc.arg('sort_order')::text = 'asc'  THEN w.term END ASC,
    CASE WHEN sqlc.arg('sort_by')::text = 'term'     AND sqlc.arg('sort_order')::text = 'desc' THEN w.term END DESC,
    uw.word_id ASC
LIMIT sqlc.arg('page_limit')::int OFFSET sqlc.arg('page_offset')::int;

-- name: CountUserWords :one
SELECT COUNT(*)
FROM user_words uw
JOIN words w ON w.id = uw.word_id
WHERE uw.user_id = sqlc.arg('user_id')::uuid
  AND (sqlc.narg('search')::text IS NULL
        OR w.term ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('status')::text IS NULL       OR uw.status = sqlc.narg('status')::text)
  AND (sqlc.narg('language')::text IS NULL     OR w.language = sqlc.narg('language')::text)
  AND (sqlc.narg('tags')::text[] IS NULL       OR w.tags @> sqlc.narg('tags')::text[])
  AND (sqlc.narg('is_favourite')::bool IS NULL OR uw.is_favourite = sqlc.narg('is_favourite')::bool)
  AND (NOT sqlc.arg('due_only')::bool          OR (uw.due_at <= NOW() AND uw.status <> 'archived'));

-- name: GetUserWordStats :one
-- Powers the progress header in one query rather than five counts.
SELECT
    COUNT(*)::bigint                                                            AS total,
    COUNT(*) FILTER (WHERE status = 'learning')::bigint                          AS learning,
    COUNT(*) FILTER (WHERE status = 'known')::bigint                             AS known,
    COUNT(*) FILTER (WHERE status = 'archived')::bigint                          AS archived,
    COUNT(*) FILTER (WHERE is_favourite)::bigint                                 AS favourites,
    COUNT(*) FILTER (WHERE due_at <= NOW() AND status <> 'archived')::bigint      AS due,
    COALESCE(SUM(review_count), 0)::bigint                                       AS reviews,
    COALESCE(SUM(correct_count), 0)::bigint                                      AS correct
FROM user_words
WHERE user_id = $1;
