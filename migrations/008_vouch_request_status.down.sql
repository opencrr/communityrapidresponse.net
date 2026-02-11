-- Rollback Migration 008: Vouch request status
-- Remove verification_status column from user_regions

DROP INDEX IF EXISTS idx_user_regions_status ON user_regions;

ALTER TABLE user_regions DROP COLUMN IF EXISTS verification_status;
