-- Encrypted secrets storage (replaces plaintext invite links)
CREATE TABLE IF NOT EXISTS encrypted_secrets (
    id CHAR(36) PRIMARY KEY,
    secret_type VARCHAR(50) NOT NULL,
    signal_group_id CHAR(36) NULL,
    meshtastic_channel_id CHAR(36) NULL,
    encrypted_payload TEXT NOT NULL,
    encryption_iv CHAR(24) NOT NULL,
    updated_by CHAR(36) NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (updated_by) REFERENCES users(id),
    INDEX idx_signal_group (signal_group_id),
    INDEX idx_meshtastic_channel (meshtastic_channel_id),
    INDEX idx_secret_type (secret_type),
    CONSTRAINT chk_encrypted_secret_owner CHECK (
        (signal_group_id IS NOT NULL AND meshtastic_channel_id IS NULL) OR
        (signal_group_id IS NULL AND meshtastic_channel_id IS NOT NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Per-user wrapped DEKs for each encrypted secret
CREATE TABLE IF NOT EXISTS encrypted_secret_keys (
    secret_id CHAR(36) NOT NULL,
    user_id CHAR(36) NOT NULL,
    wrapped_dek TEXT NOT NULL,
    rekey_needed BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (secret_id, user_id),
    FOREIGN KEY (secret_id) REFERENCES encrypted_secrets(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    INDEX idx_rekey (rekey_needed)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Generic secret update proposals (replaces invite_link_update_proposals)
CREATE TABLE IF NOT EXISTS secret_update_proposals (
    id CHAR(36) PRIMARY KEY,
    encrypted_secret_id CHAR(36) NOT NULL,
    region_id CHAR(36) NULL,
    school_id CHAR(36) NULL,
    district_id CHAR(36) NULL,
    proposed_by CHAR(36) NOT NULL,
    encrypted_payload TEXT NOT NULL,
    encryption_iv CHAR(24) NOT NULL,
    reason TEXT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    resolved_at TIMESTAMP NULL,
    finalized_at TIMESTAMP NULL,
    FOREIGN KEY (encrypted_secret_id) REFERENCES encrypted_secrets(id) ON DELETE CASCADE,
    FOREIGN KEY (proposed_by) REFERENCES users(id),
    INDEX idx_status (status),
    INDEX idx_encrypted_secret (encrypted_secret_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Votes on secret update proposals
CREATE TABLE IF NOT EXISTS secret_update_votes (
    id CHAR(36) PRIMARY KEY,
    proposal_id CHAR(36) NOT NULL,
    voter_id CHAR(36) NOT NULL,
    vote BOOLEAN NOT NULL,
    voted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY unique_proposal_voter (proposal_id, voter_id),
    FOREIGN KEY (proposal_id) REFERENCES secret_update_proposals(id) ON DELETE CASCADE,
    FOREIGN KEY (voter_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Admin-scoped wrapped keys for proposals (only admins get DEK during voting)
CREATE TABLE IF NOT EXISTS proposal_wrapped_keys (
    proposal_id CHAR(36) NOT NULL,
    user_id CHAR(36) NOT NULL,
    wrapped_dek TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (proposal_id, user_id),
    FOREIGN KEY (proposal_id) REFERENCES secret_update_proposals(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Add FK from encrypted_secrets to signal_groups
ALTER TABLE encrypted_secrets ADD CONSTRAINT fk_encrypted_secrets_signal_group
    FOREIGN KEY (signal_group_id) REFERENCES signal_groups(id) ON DELETE CASCADE;

-- Remove plaintext invite link columns from signal_groups (IF EXISTS for idempotency)
ALTER TABLE signal_groups DROP COLUMN IF EXISTS invite_link;
ALTER TABLE signal_groups DROP COLUMN IF EXISTS invite_link_updated_at;
ALTER TABLE signal_groups DROP FOREIGN KEY IF EXISTS fk_signal_groups_invite_updated_by;
ALTER TABLE signal_groups DROP COLUMN IF EXISTS invite_link_updated_by;

-- Drop old invite link proposal tables (votes first due to FK)
DROP TABLE IF EXISTS invite_link_update_votes;
DROP TABLE IF EXISTS invite_link_update_proposals;

