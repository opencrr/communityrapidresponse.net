-- Groups: independent organizations with geographic scope
CREATE TABLE IF NOT EXISTS `groups` (
    id CHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    status ENUM('provisional', 'active') NOT NULL DEFAULT 'provisional',
    visibility ENUM('listed', 'unlisted') NOT NULL DEFAULT 'unlisted',
    founding_threshold INT NULL,
    created_by CHAR(36) NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    graduated_at TIMESTAMP NULL,

    CONSTRAINT fk_groups_created_by FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS group_members (
    id CHAR(36) PRIMARY KEY,
    group_id CHAR(36) NOT NULL,
    user_id CHAR(36) NOT NULL,
    is_admin BOOLEAN NOT NULL DEFAULT FALSE,
    is_founding_member BOOLEAN NOT NULL DEFAULT FALSE,
    trust_level ENUM('member', 'trusted') NOT NULL DEFAULT 'member',
    joined_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_group_members (group_id, user_id),
    INDEX idx_group_members_user_id (user_id),
    CONSTRAINT fk_group_members_group FOREIGN KEY (group_id) REFERENCES `groups`(id) ON DELETE CASCADE,
    CONSTRAINT fk_group_members_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS group_regions (
    id CHAR(36) PRIMARY KEY,
    group_id CHAR(36) NOT NULL,
    region_id CHAR(36) NOT NULL,

    UNIQUE KEY uk_group_regions (group_id, region_id),
    INDEX idx_group_regions_region_id (region_id),
    CONSTRAINT fk_group_regions_group FOREIGN KEY (group_id) REFERENCES `groups`(id) ON DELETE CASCADE,
    CONSTRAINT fk_group_regions_region FOREIGN KEY (region_id) REFERENCES geographic_regions(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS group_topic_tags (
    id CHAR(36) PRIMARY KEY,
    group_id CHAR(36) NOT NULL,
    tag VARCHAR(100) NOT NULL,

    UNIQUE KEY uk_group_topic_tags (group_id, tag),
    CONSTRAINT fk_group_topic_tags_group FOREIGN KEY (group_id) REFERENCES `groups`(id) ON DELETE CASCADE,
    INDEX idx_group_topic_tags_tag (tag)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
