-- Topic board postings (one per group)
CREATE TABLE IF NOT EXISTS topic_board_postings (
    id CHAR(36) PRIMARY KEY,
    group_id CHAR(36) NOT NULL,
    region_label VARCHAR(255) NOT NULL,
    auto_region_label VARCHAR(255),
    description VARCHAR(500) NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uk_topic_board_postings_group (group_id),
    CONSTRAINT fk_topic_board_postings_group FOREIGN KEY (group_id) REFERENCES `groups`(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Topic tags for postings (many-to-many)
CREATE TABLE IF NOT EXISTS topic_board_tags (
    posting_id CHAR(36) NOT NULL,
    tag VARCHAR(100) NOT NULL,

    PRIMARY KEY (posting_id, tag),
    CONSTRAINT fk_topic_board_tags_posting FOREIGN KEY (posting_id) REFERENCES topic_board_postings(id) ON DELETE CASCADE,
    INDEX idx_topic_board_tags_tag (tag)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
