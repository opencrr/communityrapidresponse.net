-- Rollback Migration 010: Email verification
-- Remove email verification columns from users table

DROP INDEX IF EXISTS idx_users_email_normalized ON users;

ALTER TABLE users
    DROP COLUMN IF EXISTS email_normalized,
    DROP COLUMN IF EXISTS email_verified;
