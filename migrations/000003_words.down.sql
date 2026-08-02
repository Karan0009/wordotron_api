BEGIN;

DROP TRIGGER IF EXISTS user_words_set_updated_at ON user_words;
DROP TRIGGER IF EXISTS word_senses_set_updated_at ON word_senses;
DROP TRIGGER IF EXISTS words_set_updated_at ON words;
DROP TABLE IF EXISTS user_words;
DROP TABLE IF EXISTS word_senses;
DROP TABLE IF EXISTS words;

COMMIT;
