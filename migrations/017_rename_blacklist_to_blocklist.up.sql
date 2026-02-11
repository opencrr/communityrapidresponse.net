-- Migration 017: Rename blacklist to blocklist terminology
-- This migration renames all blacklist-related tables, columns, and indexes
-- Note: Foreign keys are automatically updated when tables are renamed

-- 1. Rename tables (using stored procedure for idempotency)
DROP PROCEDURE IF EXISTS rename_blacklist_tables;
DELIMITER //
CREATE PROCEDURE rename_blacklist_tables()
BEGIN
    -- Rename blacklist_proposals to blocklist_proposals if it exists
    IF EXISTS (SELECT 1 FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'blacklist_proposals') THEN
        RENAME TABLE blacklist_proposals TO blocklist_proposals;
    END IF;

    -- Rename blacklist_votes to blocklist_votes if it exists
    IF EXISTS (SELECT 1 FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'blacklist_votes') THEN
        RENAME TABLE blacklist_votes TO blocklist_votes;
    END IF;

    -- Rename blacklisted_users to blocked_users if it exists
    IF EXISTS (SELECT 1 FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'blacklisted_users') THEN
        RENAME TABLE blacklisted_users TO blocked_users;
    END IF;

    -- Rename blacklisted_addresses to blocked_addresses if it exists
    IF EXISTS (SELECT 1 FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'blacklisted_addresses') THEN
        RENAME TABLE blacklisted_addresses TO blocked_addresses;
    END IF;
END //
DELIMITER ;
CALL rename_blacklist_tables();
DROP PROCEDURE IF EXISTS rename_blacklist_tables;

-- 2. Rename columns in blocked_users table (using stored procedure for idempotency)
DROP PROCEDURE IF EXISTS rename_blocked_users_columns;
DELIMITER //
CREATE PROCEDURE rename_blocked_users_columns()
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'blocked_users' AND COLUMN_NAME = 'blacklisted_at') THEN
        ALTER TABLE blocked_users
            CHANGE COLUMN blacklisted_at blocked_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            CHANGE COLUMN blacklisted_until blocked_until TIMESTAMP NULL;
    END IF;
END //
DELIMITER ;
CALL rename_blocked_users_columns();
DROP PROCEDURE IF EXISTS rename_blocked_users_columns;

-- 3. Rename columns in blocked_addresses table
DROP PROCEDURE IF EXISTS rename_blocked_addresses_columns;
DELIMITER //
CREATE PROCEDURE rename_blocked_addresses_columns()
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'blocked_addresses' AND COLUMN_NAME = 'blacklisted_at') THEN
        ALTER TABLE blocked_addresses
            CHANGE COLUMN blacklisted_at blocked_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP;
    END IF;
END //
DELIMITER ;
CALL rename_blocked_addresses_columns();
DROP PROCEDURE IF EXISTS rename_blocked_addresses_columns;

-- 4. Rename columns in users table
DROP PROCEDURE IF EXISTS rename_users_columns;
DELIMITER //
CREATE PROCEDURE rename_users_columns()
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'is_blacklisted') THEN
        ALTER TABLE users
            CHANGE COLUMN is_blacklisted is_blocked BOOLEAN NOT NULL DEFAULT FALSE,
            CHANGE COLUMN blacklisted_at blocked_at TIMESTAMP NULL,
            CHANGE COLUMN blacklisted_by blocked_by CHAR(36) NULL,
            CHANGE COLUMN blacklist_reason block_reason TEXT NULL;
    END IF;
END //
DELIMITER ;
CALL rename_users_columns();
DROP PROCEDURE IF EXISTS rename_users_columns;

-- 5. Rename indexes
DROP PROCEDURE IF EXISTS rename_indexes;
DELIMITER //
CREATE PROCEDURE rename_indexes()
BEGIN
    -- Rename idx_blacklisted_until to idx_blocked_until on blocked_users
    IF EXISTS (SELECT 1 FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'blocked_users' AND INDEX_NAME = 'idx_blacklisted_until') THEN
        DROP INDEX idx_blacklisted_until ON blocked_users;
        CREATE INDEX idx_blocked_until ON blocked_users(blocked_until);
    END IF;

    -- Rename idx_is_blacklisted to idx_is_blocked on users
    IF EXISTS (SELECT 1 FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND INDEX_NAME = 'idx_is_blacklisted') THEN
        DROP INDEX idx_is_blacklisted ON users;
        CREATE INDEX idx_is_blocked ON users(is_blocked);
    END IF;

    -- Rename idx_blacklist_proposals_expires to idx_blocklist_proposals_expires
    IF EXISTS (SELECT 1 FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'blocklist_proposals' AND INDEX_NAME = 'idx_blacklist_proposals_expires') THEN
        DROP INDEX idx_blacklist_proposals_expires ON blocklist_proposals;
        CREATE INDEX idx_blocklist_proposals_expires ON blocklist_proposals(status, expires_at);
    END IF;
END //
DELIMITER ;
CALL rename_indexes();
DROP PROCEDURE IF EXISTS rename_indexes;
