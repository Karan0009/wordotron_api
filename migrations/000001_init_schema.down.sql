BEGIN;

DROP TRIGGER IF EXISTS users_set_updated_at ON users;
DROP FUNCTION IF EXISTS set_updated_at();
DROP TABLE IF EXISTS password_reset_tokens;
DROP TABLE IF EXISTS users;

COMMIT;
