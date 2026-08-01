-- Development seed data. Run with: make seed
--
-- Credentials (development only - never load this into a real environment):
--   admin@example.com / Admin1234Secure
--   user@example.com  / User12345Secure
--
-- The hashes below are bcrypt cost 12 of the passwords above.

BEGIN;

INSERT INTO users (email, password_hash, full_name, role, is_active, email_verified_at)
VALUES
    ('admin@example.com',
     '$2b$12$oFXvBWc3AwAlR/UIeTS0vu.l3j7l1vb8FgrX18Ob1rN20BQ0OGT1q',
     'Ada Admin',
     'admin',
     TRUE,
     NOW()),
    ('user@example.com',
     '$2b$12$ajFmAk9076lYxlXpAHRHi.l./eN6cDz8Dtrkev6gDpS8gq0fHTqBy',
     'Uma User',
     'user',
     TRUE,
     NOW())
ON CONFLICT (email) DO NOTHING;

COMMIT;

-- ---------------------------------------------------------------------------
-- Vocabulary catalogue and one person's list, for development.
-- ---------------------------------------------------------------------------

BEGIN;

INSERT INTO words (term, language, part_of_speech, definition, example, pronunciation, tags, created_by)
VALUES
    ('perspicacious', 'en', 'adjective',
     'Having a ready insight into and understanding of things.',
     'A perspicacious reader spots the flaw on the first page.',
     '/ˌpɜːspɪˈkeɪʃəs/', ARRAY['c1', 'formal'],
     (SELECT id FROM users WHERE email = 'admin@example.com')),
    ('ephemeral', 'en', 'adjective',
     'Lasting for a very short time.',
     'Fame in that industry is ephemeral.',
     '/ɪˈfɛm(ə)rəl/', ARRAY['c1', 'literary'],
     (SELECT id FROM users WHERE email = 'admin@example.com')),
    ('bank', 'en', 'noun',
     'The land alongside a river or lake.',
     'They walked along the south bank until dusk.',
     '/baŋk/', ARRAY['a2', 'geography'],
     (SELECT id FROM users WHERE email = 'admin@example.com')),
    ('bank', 'en', 'verb',
     'To tilt an aircraft laterally when turning.',
     'The pilot banked sharply to the left.',
     '/baŋk/', ARRAY['b2', 'aviation'],
     (SELECT id FROM users WHERE email = 'admin@example.com')),
    ('serendipity', 'en', 'noun',
     'The occurrence of events by chance in a happy or beneficial way.',
     'Finding that bookshop was pure serendipity.',
     '/ˌsɛr(ə)nˈdɪpɪti/', ARRAY['c1'],
     (SELECT id FROM users WHERE email = 'admin@example.com')),
    ('sobremesa', 'es', 'noun',
     'The time spent at the table talking after a meal has finished.',
     'La sobremesa duró más que la comida.',
     NULL, ARRAY['culture', 'untranslatable'],
     (SELECT id FROM users WHERE email = 'admin@example.com')),
    ('flâner', 'fr', 'verb',
     'To wander the streets with no particular destination, observing as you go.',
     'Il aime flâner le long de la Seine.',
     NULL, ARRAY['culture'],
     (SELECT id FROM users WHERE email = 'admin@example.com'))
ON CONFLICT DO NOTHING;

-- Give the demo user a list at a few different stages of learning.
INSERT INTO user_words (user_id, word_id, status, box, review_count, correct_count, is_favourite, due_at, notes)
SELECT
    (SELECT id FROM users WHERE email = 'user@example.com'),
    w.id,
    CASE WHEN w.term IN ('bank', 'ephemeral') THEN 'known' ELSE 'learning' END,
    CASE WHEN w.term IN ('bank', 'ephemeral') THEN 5 ELSE 1 END,
    CASE WHEN w.term IN ('bank', 'ephemeral') THEN 8 ELSE 2 END,
    CASE WHEN w.term IN ('bank', 'ephemeral') THEN 7 ELSE 1 END,
    w.term = 'serendipity',
    -- Half the list is already due, so the review queue is not empty on a
    -- fresh database.
    CASE WHEN w.term IN ('perspicacious', 'sobremesa') THEN NOW() - INTERVAL '1 hour'
         ELSE NOW() + INTERVAL '3 days' END,
    CASE WHEN w.term = 'sobremesa' THEN 'No single-word English equivalent.' ELSE NULL END
FROM words w
WHERE EXISTS (SELECT 1 FROM users WHERE email = 'user@example.com')
ON CONFLICT (user_id, word_id) DO NOTHING;

COMMIT;
