BEGIN;

-- Free-form lexical metadata (synonyms, antonyms, etc.) that doesn't warrant
-- its own column: it's optional, variably shaped, and never queried by key
-- in the current feature set. JSONB rather than JSON so it's stored parsed
-- (no reparse on read) and indexable later if a lookup need shows up.
ALTER TABLE words ADD COLUMN IF NOT EXISTS meta JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMIT;
