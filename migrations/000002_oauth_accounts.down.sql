BEGIN;

DROP TRIGGER IF EXISTS oauth_accounts_set_updated_at ON oauth_accounts;
DROP TABLE IF EXISTS oauth_accounts;

-- Rolling back requires every account to have a password again. Accounts that
-- only ever signed in with Google have none, so they are removed rather than
-- silently given an unusable hash.
DELETE FROM users WHERE password_hash IS NULL;
ALTER TABLE users ALTER COLUMN password_hash SET NOT NULL;

COMMIT;
