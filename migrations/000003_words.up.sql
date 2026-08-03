BEGIN;

-- The shared vocabulary catalogue. A word holds no meaning by itself - a
-- word can have several senses (word_senses), since "bank" alone has at
-- least two unrelated meanings even within the same part of speech.
CREATE TABLE IF NOT EXISTS words (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    term           CITEXT      NOT NULL,
    -- BCP 47 language subtag. Kept short and lowercase by a CHECK rather than
    -- a lookup table: the set is large, stable, and validated at the edge.
    language       TEXT        NOT NULL DEFAULT 'en'
                               CHECK (language ~ '^[a-z]{2}(-[A-Za-z0-9]{2,8})*$'),
    pronunciation  TEXT        CHECK (pronunciation IS NULL OR length(pronunciation) <= 200),
    tags           TEXT[]      DEFAULT '{}',
    -- Who contributed it. SET NULL rather than CASCADE: deleting a person must
    -- not delete vocabulary other people are studying.
    created_by     UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One word row per term per language.
CREATE UNIQUE INDEX IF NOT EXISTS words_term_language_key ON words (language, term);

CREATE INDEX IF NOT EXISTS words_created_at_idx ON words (created_at DESC);
CREATE INDEX IF NOT EXISTS words_language_idx   ON words (language);
CREATE INDEX IF NOT EXISTS words_created_by_idx ON words (created_by);
CREATE INDEX IF NOT EXISTS words_term_trgm_idx  ON words USING GIN (term gin_trgm_ops);
CREATE INDEX IF NOT EXISTS words_tags_idx       ON words USING GIN (tags);

-- One row per meaning of a word: a part of speech, a definition, and the
-- lexical metadata specific to that meaning (synonyms/antonyms vary by
-- meaning, not by term, so they live here rather than on the parent word).
CREATE TABLE IF NOT EXISTS word_senses (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    word_id        UUID        NOT NULL REFERENCES words (id) ON DELETE CASCADE,
    part_of_speech TEXT        CHECK (part_of_speech IN (
                                   'noun', 'verb', 'adjective', 'adverb',
                                   'pronoun', 'preposition', 'conjunction',
                                   'interjection', 'phrase'
                               )),
    definition     TEXT        NOT NULL CHECK (length(definition) BETWEEN 1 AND 2000),
    example        TEXT        CHECK (example IS NULL OR length(example) <= 2000),
    meta           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- Display order when a word has multiple senses (most common first).
    sense_order    SMALLINT    NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS word_senses_word_id_idx ON word_senses (word_id, sense_order);

-- The mapping. Everything about one person's relationship with one word lives
-- here, which keeps the catalogue itself free of per-user state. A person
-- learns a term, not one specific sense of it.
CREATE TABLE IF NOT EXISTS user_words (
    user_id          UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    word_id          UUID        NOT NULL REFERENCES words (id) ON DELETE CASCADE,
    status           TEXT        NOT NULL DEFAULT 'learning'
                                 CHECK (status IN ('learning', 'known', 'archived')),
    -- A private note, distinct from the shared definition.
    notes            TEXT        CHECK (notes IS NULL OR length(notes) <= 2000),
    is_favourite     BOOLEAN     NOT NULL DEFAULT FALSE,
    review_count     INTEGER     NOT NULL DEFAULT 0 CHECK (review_count >= 0),
    correct_count    INTEGER     NOT NULL DEFAULT 0 CHECK (correct_count >= 0),
    -- Leitner box, 0-5. Drives the interval in due_at.
    box              SMALLINT    NOT NULL DEFAULT 0 CHECK (box BETWEEN 0 AND 5),
    last_reviewed_at TIMESTAMPTZ,
    due_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    added_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- The natural key: a person maps a word once.
    PRIMARY KEY (user_id, word_id),
    CHECK (correct_count <= review_count)
);

-- The two queries that matter: "my list, filtered by status" and "what is due".
CREATE INDEX IF NOT EXISTS user_words_user_status_idx ON user_words (user_id, status);
CREATE INDEX IF NOT EXISTS user_words_user_due_idx
    ON user_words (user_id, due_at) WHERE status <> 'archived';
CREATE INDEX IF NOT EXISTS user_words_word_id_idx     ON user_words (word_id);
CREATE INDEX IF NOT EXISTS user_words_favourite_idx
    ON user_words (user_id) WHERE is_favourite;

CREATE TRIGGER words_set_updated_at
    BEFORE UPDATE ON words
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER word_senses_set_updated_at
    BEFORE UPDATE ON word_senses
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER user_words_set_updated_at
    BEFORE UPDATE ON user_words
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

COMMIT;
