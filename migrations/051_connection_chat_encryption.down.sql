-- Drop wrapped keys table
DROP TABLE IF EXISTS connection_chat_wrapped_keys;

-- Remove encrypted bundle columns from connection_chat_proposals
ALTER TABLE connection_chat_proposals
    DROP COLUMN encryption_iv,
    DROP COLUMN encrypted_payload;
