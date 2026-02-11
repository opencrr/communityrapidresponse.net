-- Community Rapid Response Database Schema
-- Migration 002: New Verification Model
-- Adds separate postcard_verified, vouch_verified, and is_superuser flags

-- Add new verification columns to users table
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS postcard_verified BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS vouch_verified BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS is_superuser BOOLEAN NOT NULL DEFAULT FALSE;

-- Add indexes for the new columns
CREATE INDEX IF NOT EXISTS idx_postcard_verified ON users(postcard_verified);
CREATE INDEX IF NOT EXISTS idx_vouch_verified ON users(vouch_verified);
CREATE INDEX IF NOT EXISTS idx_is_superuser ON users(is_superuser);

-- Migrate existing tier data to new model:
-- Tier 1 (was postcard-verified admin) -> postcard_verified=true, vouch_verified=true (to maintain admin access)
-- Tier 2 (was vouched read-only) -> vouch_verified=true
UPDATE users SET postcard_verified = TRUE, vouch_verified = TRUE WHERE verification_tier = 1;
UPDATE users SET vouch_verified = TRUE WHERE verification_tier = 2;

-- Note: verification_tier column is kept for backwards compatibility but should not be used
-- in new code. Use postcard_verified, vouch_verified, and is_superuser instead.
