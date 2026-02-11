-- Migration 016 Down: Remove blacklist proposal expiration and address blacklisting

-- Drop blacklisted_addresses table
DROP TABLE IF EXISTS blacklisted_addresses;

-- Remove address_hash column from users
ALTER TABLE users DROP COLUMN address_hash;

-- Drop expires index from blacklist_proposals
DROP INDEX idx_blacklist_proposals_expires ON blacklist_proposals;

-- Remove expires_at column from blacklist_proposals
ALTER TABLE blacklist_proposals DROP COLUMN expires_at;
