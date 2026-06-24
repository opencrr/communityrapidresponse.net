DROP TABLE IF EXISTS connection_chat_proposal_wrapped_keys;

ALTER TABLE connection_chat_proposals
    DROP FOREIGN KEY IF EXISTS fk_ccp_proposer_user;

ALTER TABLE connection_chat_proposals
    DROP COLUMN IF EXISTS proposer_user_id,
    DROP COLUMN IF EXISTS encryption_iv,
    DROP COLUMN IF EXISTS encrypted_payload;
