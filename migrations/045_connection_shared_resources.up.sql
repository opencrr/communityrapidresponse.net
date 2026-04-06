CREATE TABLE IF NOT EXISTS connection_shared_resources (
    id CHAR(36) PRIMARY KEY,
    resource_id CHAR(36) NOT NULL,
    connection_id CHAR(36) NOT NULL,
    shared_by_group_id CHAR(36) NOT NULL,
    visibility ENUM('admin_only', 'all_members') NOT NULL DEFAULT 'admin_only',
    shared_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_csr (resource_id, connection_id),
    INDEX idx_csr_connection (connection_id),
    INDEX idx_csr_group (shared_by_group_id),
    CONSTRAINT fk_csr_resource FOREIGN KEY (resource_id) REFERENCES group_resources(id) ON DELETE CASCADE,
    CONSTRAINT fk_csr_connection FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE,
    CONSTRAINT fk_csr_group FOREIGN KEY (shared_by_group_id) REFERENCES `groups`(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
