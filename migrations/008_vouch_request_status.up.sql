-- Migration 008: Add verification_status to user_regions for tracking pending vouch requests
--
-- This migration adds support for the vouch request flow where users can request
-- vouch verification by specifying their city/state. The pending status tracks
-- users who have requested vouching but haven't yet received 2 vouches.

-- Add verification_status column to user_regions
-- Default 'verified' preserves backward compatibility for existing records
ALTER TABLE user_regions
    ADD COLUMN verification_status ENUM('pending', 'verified') NOT NULL DEFAULT 'verified'
    AFTER is_admin;

-- Add index for efficient queries on user verification status
CREATE INDEX idx_user_regions_status ON user_regions(user_id, verification_status);
