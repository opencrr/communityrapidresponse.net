-- Rollback Migration 002: New Verification Model
-- Remove verification columns from users table

-- Revert tier data (best effort - data may have changed)
UPDATE users SET verification_tier = 1 WHERE postcard_verified = TRUE AND vouch_verified = TRUE;
UPDATE users SET verification_tier = 2 WHERE postcard_verified = FALSE AND vouch_verified = TRUE;

-- Drop indexes
DROP INDEX IF EXISTS idx_is_superuser ON users;
DROP INDEX IF EXISTS idx_vouch_verified ON users;
DROP INDEX IF EXISTS idx_postcard_verified ON users;

-- Drop columns
ALTER TABLE users
    DROP COLUMN IF EXISTS is_superuser,
    DROP COLUMN IF EXISTS vouch_verified,
    DROP COLUMN IF EXISTS postcard_verified;
