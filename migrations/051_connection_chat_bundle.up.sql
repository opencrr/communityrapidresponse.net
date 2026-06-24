-- Add encrypted bundle fields to connection_chat_proposals for admin_only chats
ALTER TABLE connection_chat_proposals
    ADD COLUMN encrypted_payload TEXT NULL AFTER access_level,
    ADD COLUMN encryption_iv CHAR(24) NULL AFTER encrypted_payload,
    ADD COLUMN proposer_user_id CHAR(36) NULL AFTER encryption_iv;

ALTER TABLE connection_chat_proposals
    ADD CONSTRAINT fk_ccp_proposer_user FOREIGN KEY (proposer_user_id) REFERENCES users(id) ON DELETE SET NULL;

-- Wrapped DEKs for connection chat proposals; no FK to users since these are
-- client-supplied and validated only when the secret rows are finally created.
CREATE TABLE IF NOT EXISTS connection_chat_proposal_wrapped_keys (
    proposal_id CHAR(36) NOT NULL,
    user_id     CHAR(36) NOT NULL,
    wrapped_dek TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (proposal_id, user_id),
    CONSTRAINT fk_ccpwk_proposal FOREIGN KEY (proposal_id)
        REFERENCES connection_chat_proposals(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
