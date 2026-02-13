-- Remove FK constraint from encrypted_secrets
ALTER TABLE encrypted_secrets
    DROP FOREIGN KEY fk_encrypted_secrets_meshtastic;

-- Drop meshtastic_channels table
DROP TABLE IF EXISTS meshtastic_channels;
