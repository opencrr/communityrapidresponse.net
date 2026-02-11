-- Migration 016: Add expires_at to blacklist_proposals, address_hash to users, and blacklisted_addresses table
-- This migration adds expiration tracking for blacklist proposals and address blacklisting functionality

-- Add expires_at column to blacklist_proposals
ALTER TABLE blacklist_proposals
ADD COLUMN expires_at TIMESTAMP NULL AFTER created_at;

-- Set default expiration for existing proposals (7 days from creation)
UPDATE blacklist_proposals
SET expires_at = DATE_ADD(created_at, INTERVAL 7 DAY)
WHERE expires_at IS NULL;

-- Make expires_at NOT NULL after populating existing data
ALTER TABLE blacklist_proposals
MODIFY COLUMN expires_at TIMESTAMP NOT NULL;

-- Add index for efficient expiration queries
CREATE INDEX idx_blacklist_proposals_expires ON blacklist_proposals(status, expires_at);

-- Add address_hash column to users table (stores SHA-256 hash of verified address)
-- This enables address blacklisting without storing actual addresses (zero-storage policy)
ALTER TABLE users ADD COLUMN address_hash CHAR(64) NULL AFTER blacklist_reason;

-- Create blacklisted_addresses table for tracking blacklisted addresses
CREATE TABLE IF NOT EXISTS blacklisted_addresses (
    id CHAR(36) PRIMARY KEY,
    address_hash CHAR(64) NOT NULL,
    user_id CHAR(36) NOT NULL,           -- User who was blacklisted at this address
    proposal_id CHAR(36),                 -- Link to blacklist proposal (nullable for manual blacklists)
    blacklisted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,        -- When the address blacklist expires (default: 2 years)
    UNIQUE KEY uk_address_hash (address_hash),
    INDEX idx_expires (expires_at),
    INDEX idx_user_id (user_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (proposal_id) REFERENCES blacklist_proposals(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
