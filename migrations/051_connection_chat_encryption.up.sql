-- Add encrypted bundle columns to connection_chat_proposals for restricted-tier chats
ALTER TABLE connection_chat_proposals
    ADD COLUMN encrypted_payload TEXT NULL AFTER access_level,
    ADD COLUMN encryption_iv TEXT NULL AFTER encrypted_payload;

-- Create table for wrapped keys associated with chat proposals
CREATE TABLE IF NOT EXISTS connection_chat_wrapped_keys (
    id CHAR(36) PRIMARY KEY,
    proposal_id CHAR(36) NOT NULL,
    user_id CHAR(36) NOT NULL,
    wrapped_dek TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    INDEX idx_ccwk_proposal (proposal_id),
    UNIQUE KEY uk_ccwk_proposal_user (proposal_id, user_id),
    CONSTRAINT fk_ccwk_proposal FOREIGN KEY (proposal_id) REFERENCES connection_chat_proposals(id) ON DELETE CASCADE,
    CONSTRAINT fk_ccwk_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
