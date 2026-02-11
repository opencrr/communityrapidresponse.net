-- Community Rapid Response Database Schema
-- Migration 003: Add global blacklist flag to users
-- Allows superusers to blacklist users globally (prevents login)

-- Add is_blacklisted column to users table
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS is_blacklisted BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS blacklisted_at TIMESTAMP NULL,
    ADD COLUMN IF NOT EXISTS blacklisted_by CHAR(36) NULL,
    ADD COLUMN IF NOT EXISTS blacklist_reason TEXT NULL;

-- Add index for blacklisted users
CREATE INDEX IF NOT EXISTS idx_is_blacklisted ON users(is_blacklisted);
