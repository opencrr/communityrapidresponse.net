CREATE TABLE IF NOT EXISTS group_blocks (
    id CHAR(36) PRIMARY KEY,
    blocker_group_id CHAR(36) NOT NULL,
    blocked_group_id CHAR(36) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_group_blocks (blocker_group_id, blocked_group_id),
    INDEX idx_group_blocks_blocked (blocked_group_id),
    CONSTRAINT fk_group_blocks_blocker FOREIGN KEY (blocker_group_id) REFERENCES `groups`(id) ON DELETE CASCADE,
    CONSTRAINT fk_group_blocks_blocked FOREIGN KEY (blocked_group_id) REFERENCES `groups`(id) ON DELETE CASCADE,
    CONSTRAINT chk_group_blocks_no_self CHECK (blocker_group_id != blocked_group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
