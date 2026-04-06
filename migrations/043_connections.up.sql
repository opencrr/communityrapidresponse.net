-- Connections: groups of groups
CREATE TABLE IF NOT EXISTS connections (
    id CHAR(36) PRIMARY KEY,
    name VARCHAR(255),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Connection membership
CREATE TABLE IF NOT EXISTS connection_members (
    id CHAR(36) PRIMARY KEY,
    connection_id CHAR(36) NOT NULL,
    group_id CHAR(36) NOT NULL,
    joined_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_connection_members (connection_id, group_id),
    INDEX idx_connection_members_group (group_id),
    CONSTRAINT fk_connection_members_connection FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE,
    CONSTRAINT fk_connection_members_group FOREIGN KEY (group_id) REFERENCES `groups`(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Connection proposals (formation and expansion)
CREATE TABLE IF NOT EXISTS connection_proposals (
    id CHAR(36) PRIMARY KEY,
    connection_id CHAR(36) NULL,
    proposer_group_id CHAR(36) NOT NULL,
    proposal_type ENUM('formation', 'expansion') NOT NULL,
    status ENUM('pending', 'accepted', 'declined', 'expired') NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NULL,

    INDEX idx_connection_proposals_connection (connection_id),
    CONSTRAINT fk_connection_proposals_connection FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE,
    CONSTRAINT fk_connection_proposals_proposer FOREIGN KEY (proposer_group_id) REFERENCES `groups`(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Per-group votes on connection proposals
CREATE TABLE IF NOT EXISTS connection_proposal_groups (
    id CHAR(36) PRIMARY KEY,
    proposal_id CHAR(36) NOT NULL,
    group_id CHAR(36) NOT NULL,
    status ENUM('pending', 'accepted', 'declined') NOT NULL DEFAULT 'pending',
    responded_at TIMESTAMP NULL,

    UNIQUE KEY uk_connection_proposal_groups (proposal_id, group_id),
    CONSTRAINT fk_cpg_proposal FOREIGN KEY (proposal_id) REFERENCES connection_proposals(id) ON DELETE CASCADE,
    CONSTRAINT fk_cpg_group FOREIGN KEY (group_id) REFERENCES `groups`(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
