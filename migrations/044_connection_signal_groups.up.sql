-- Add connection_id to signal_groups (5th XOR option)
ALTER TABLE signal_groups
    ADD COLUMN connection_id CHAR(36) NULL AFTER owner_group_id;

ALTER TABLE signal_groups
    ADD CONSTRAINT fk_signal_groups_connection
    FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

ALTER TABLE signal_groups
    ADD INDEX idx_signal_groups_connection (connection_id);

-- Update XOR constraint to 5-way
ALTER TABLE signal_groups DROP CONSTRAINT chk_signal_group_owner;

ALTER TABLE signal_groups ADD CONSTRAINT chk_signal_group_owner CHECK (
    (region_id IS NOT NULL AND school_id IS NULL AND district_id IS NULL AND owner_group_id IS NULL AND connection_id IS NULL) OR
    (region_id IS NULL AND school_id IS NOT NULL AND district_id IS NULL AND owner_group_id IS NULL AND connection_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NOT NULL AND owner_group_id IS NULL AND connection_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NULL AND owner_group_id IS NOT NULL AND connection_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NULL AND owner_group_id IS NULL AND connection_id IS NOT NULL)
);

-- Connection signal chat proposals (consensus-based creation)
CREATE TABLE IF NOT EXISTS connection_chat_proposals (
    id CHAR(36) PRIMARY KEY,
    connection_id CHAR(36) NOT NULL,
    proposer_group_id CHAR(36) NOT NULL,
    group_name VARCHAR(255) NOT NULL,
    description VARCHAR(1000),
    access_level ENUM('admin_only', 'all_members') NOT NULL DEFAULT 'admin_only',
    status ENUM('pending', 'approved', 'declined', 'expired') NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NULL,

    INDEX idx_ccp_connection (connection_id),
    CONSTRAINT fk_ccp_connection FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE,
    CONSTRAINT fk_ccp_proposer FOREIGN KEY (proposer_group_id) REFERENCES `groups`(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Per-group votes on chat proposals
CREATE TABLE IF NOT EXISTS connection_chat_proposal_votes (
    id CHAR(36) PRIMARY KEY,
    proposal_id CHAR(36) NOT NULL,
    group_id CHAR(36) NOT NULL,
    status ENUM('pending', 'approved', 'declined') NOT NULL DEFAULT 'pending',
    responded_at TIMESTAMP NULL,

    UNIQUE KEY uk_ccpv (proposal_id, group_id),
    CONSTRAINT fk_ccpv_proposal FOREIGN KEY (proposal_id) REFERENCES connection_chat_proposals(id) ON DELETE CASCADE,
    CONSTRAINT fk_ccpv_group FOREIGN KEY (group_id) REFERENCES `groups`(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
