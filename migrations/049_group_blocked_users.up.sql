-- Per-group user bans. group_blocks is group-to-group; this lets a group's admins
-- keep an individual user out (harassment/safety), enforced on every join/invite/
-- vouch path so a removed member can't simply rejoin.
CREATE TABLE IF NOT EXISTS group_blocked_users (
    id CHAR(36) PRIMARY KEY,
    group_id CHAR(36) NOT NULL,
    user_id CHAR(36) NOT NULL,
    blocked_by CHAR(36) NULL,
    reason VARCHAR(500) NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_group_blocked_users (group_id, user_id),
    INDEX idx_group_blocked_users_user (user_id),
    CONSTRAINT fk_group_blocked_users_group FOREIGN KEY (group_id) REFERENCES `groups`(id) ON DELETE CASCADE,
    CONSTRAINT fk_group_blocked_users_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_group_blocked_users_blocked_by FOREIGN KEY (blocked_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
