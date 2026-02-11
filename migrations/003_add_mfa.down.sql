-- Rollback Migration 003: Add MFA support
-- Remove MFA columns from users table

DROP INDEX IF EXISTS idx_users_mfa_setup_required ON users;

ALTER TABLE users
    DROP COLUMN IF EXISTS mfa_setup_required,
    DROP COLUMN IF EXISTS mfa_backup_codes,
    DROP COLUMN IF EXISTS mfa_enabled,
    DROP COLUMN IF EXISTS mfa_secret;
