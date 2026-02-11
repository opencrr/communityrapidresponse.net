-- Migration: Add MFA support to users table
-- Date: 2026-01-30
-- Description: Adds columns for TOTP-based multi-factor authentication

-- Add MFA columns to users table
ALTER TABLE users
    ADD COLUMN mfa_secret VARCHAR(255) DEFAULT NULL COMMENT 'Encrypted TOTP secret key (base64 encoded)',
    ADD COLUMN mfa_enabled BOOLEAN NOT NULL DEFAULT FALSE COMMENT 'Whether MFA is currently enabled',
    ADD COLUMN mfa_backup_codes TEXT DEFAULT NULL COMMENT 'JSON array of hashed backup codes',
    ADD COLUMN mfa_setup_required BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Whether user must set up MFA on next login';

-- Index for efficiently finding users who need MFA setup
CREATE INDEX idx_users_mfa_setup_required ON users(mfa_setup_required);

-- Note: Existing users will have mfa_setup_required = TRUE (default)
-- which means they'll be prompted to set up MFA on next login
