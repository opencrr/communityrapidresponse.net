-- Migration 017 Down: Revert blocklist back to blacklist terminology

-- 1. Rename indexes back
DROP INDEX idx_blocklist_proposals_expires ON blocklist_proposals;
CREATE INDEX idx_blacklist_proposals_expires ON blocklist_proposals(status, expires_at);

DROP INDEX idx_is_blocked ON users;
CREATE INDEX idx_is_blacklisted ON users(is_blocked);

DROP INDEX idx_blocked_until ON blocked_users;
CREATE INDEX idx_blacklisted_until ON blocked_users(blocked_until);

-- 2. Rename columns in users table back
ALTER TABLE users
    CHANGE COLUMN is_blocked is_blacklisted BOOLEAN NOT NULL DEFAULT FALSE,
    CHANGE COLUMN blocked_at blacklisted_at TIMESTAMP NULL,
    CHANGE COLUMN blocked_by blacklisted_by CHAR(36) NULL,
    CHANGE COLUMN block_reason blacklist_reason TEXT NULL;

-- 3. Rename columns in blocked_addresses table back
ALTER TABLE blocked_addresses
    CHANGE COLUMN blocked_at blacklisted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP;

-- 4. Rename columns in blocked_users table back
ALTER TABLE blocked_users
    CHANGE COLUMN blocked_at blacklisted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHANGE COLUMN blocked_until blacklisted_until TIMESTAMP NULL;

-- 5. Update foreign key constraints back
ALTER TABLE blocked_addresses DROP FOREIGN KEY blocked_addresses_ibfk_2;
ALTER TABLE blocked_addresses ADD CONSTRAINT blacklisted_addresses_ibfk_2
    FOREIGN KEY (proposal_id) REFERENCES blocklist_proposals(id);

ALTER TABLE blocked_users DROP FOREIGN KEY blocked_users_ibfk_1;
ALTER TABLE blocked_users ADD CONSTRAINT blacklisted_users_ibfk_1
    FOREIGN KEY (proposal_id) REFERENCES blocklist_proposals(id);

ALTER TABLE blocklist_votes DROP FOREIGN KEY blocklist_votes_ibfk_1;
ALTER TABLE blocklist_votes ADD CONSTRAINT blocklist_votes_ibfk_1
    FOREIGN KEY (proposal_id) REFERENCES blocklist_proposals(id) ON DELETE CASCADE;

-- 6. Rename tables back
RENAME TABLE blocked_addresses TO blacklisted_addresses;
RENAME TABLE blocked_users TO blacklisted_users;
RENAME TABLE blocklist_votes TO blacklist_votes;
RENAME TABLE blocklist_proposals TO blacklist_proposals;
