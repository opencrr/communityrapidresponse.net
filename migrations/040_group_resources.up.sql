CREATE TABLE IF NOT EXISTS group_resources (
    id CHAR(36) PRIMARY KEY,
    group_id CHAR(36) NOT NULL,
    title VARCHAR(255) NOT NULL,
    url VARCHAR(2048) NOT NULL,
    description VARCHAR(500),
    access_tier ENUM('open', 'resident', 'member', 'trusted', 'admin_only') NOT NULL DEFAULT 'member',
    created_by CHAR(36) NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    INDEX idx_group_resources_group (group_id),
    CONSTRAINT fk_group_resources_group FOREIGN KEY (group_id) REFERENCES `groups`(id) ON DELETE CASCADE,
    CONSTRAINT fk_group_resources_created_by FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
