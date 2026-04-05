-- Shareable invite links for groups
CREATE TABLE IF NOT EXISTS group_invite_links (
    id CHAR(36) PRIMARY KEY,
    group_id CHAR(36) NOT NULL,
    token VARCHAR(64) NOT NULL,
    created_by CHAR(36) NULL,
    expires_at TIMESTAMP NULL,
    max_uses INT NULL,
    use_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_group_invite_links_token (token),
    INDEX idx_group_invite_links_group (group_id),
    CONSTRAINT fk_group_invite_links_group FOREIGN KEY (group_id) REFERENCES `groups`(id) ON DELETE CASCADE,
    CONSTRAINT fk_group_invite_links_created_by FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Direct invitations from admins to specific users
CREATE TABLE IF NOT EXISTS group_invitations (
    id CHAR(36) PRIMARY KEY,
    group_id CHAR(36) NOT NULL,
    user_id CHAR(36) NOT NULL,
    invited_by CHAR(36) NULL,
    status ENUM('pending', 'accepted', 'declined', 'expired') NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NULL,
    responded_at TIMESTAMP NULL,

    UNIQUE KEY uk_group_invitations_pending (group_id, user_id, status),
    INDEX idx_group_invitations_user (user_id),
    CONSTRAINT fk_group_invitations_group FOREIGN KEY (group_id) REFERENCES `groups`(id) ON DELETE CASCADE,
    CONSTRAINT fk_group_invitations_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_group_invitations_invited_by FOREIGN KEY (invited_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
