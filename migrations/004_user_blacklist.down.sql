-- Rollback Migration 004: User blacklist
-- Remove blacklist columns from users table

DROP INDEX IF EXISTS idx_is_blacklisted ON users;

ALTER TABLE users
    DROP COLUMN IF EXISTS blacklist_reason,
    DROP COLUMN IF EXISTS blacklisted_by,
    DROP COLUMN IF EXISTS blacklisted_at,
    DROP COLUMN IF EXISTS is_blacklisted;
