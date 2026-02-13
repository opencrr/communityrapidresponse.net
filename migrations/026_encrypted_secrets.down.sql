-- Re-add invite link columns to signal_groups
ALTER TABLE signal_groups ADD COLUMN invite_link TEXT NOT NULL DEFAULT '' AFTER group_name;
ALTER TABLE signal_groups ADD COLUMN invite_link_updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP AFTER invite_link;
ALTER TABLE signal_groups ADD COLUMN invite_link_updated_by CHAR(36) AFTER invite_link_updated_at;
ALTER TABLE signal_groups ADD CONSTRAINT signal_groups_ibfk_2 FOREIGN KEY (invite_link_updated_by) REFERENCES users(id);

-- Recreate invite link proposal tables
CREATE TABLE IF NOT EXISTS invite_link_update_proposals (
    id CHAR(36) PRIMARY KEY,
    signal_group_id CHAR(36) NOT NULL,
    region_id CHAR(36),
    proposed_by CHAR(36),
    new_invite_link TEXT NOT NULL,
    reason TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    resolved_at TIMESTAMP NULL,
    INDEX idx_status (status),
    INDEX idx_signal_group (signal_group_id),
    INDEX idx_region (region_id),
    FOREIGN KEY (signal_group_id) REFERENCES signal_groups(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id),
    FOREIGN KEY (proposed_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS invite_link_update_votes (
    id CHAR(36) PRIMARY KEY,
    proposal_id CHAR(36) NOT NULL,
    voter_id CHAR(36) NOT NULL,
    vote BOOLEAN NOT NULL,
    voted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_proposal_voter (proposal_id, voter_id),
    INDEX idx_proposal_id (proposal_id),
    FOREIGN KEY (proposal_id) REFERENCES invite_link_update_proposals(id) ON DELETE CASCADE,
    FOREIGN KEY (voter_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Drop encrypted secret tables (in dependency order)
DROP TABLE IF EXISTS proposal_wrapped_keys;
DROP TABLE IF EXISTS secret_update_votes;
DROP TABLE IF EXISTS secret_update_proposals;
DROP TABLE IF EXISTS encrypted_secret_keys;
ALTER TABLE encrypted_secrets DROP FOREIGN KEY fk_encrypted_secrets_signal_group;
DROP TABLE IF EXISTS encrypted_secrets;
