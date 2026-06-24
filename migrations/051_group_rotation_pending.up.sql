-- 051: Add group_rotation_pending flag to encrypted_secret_keys
-- Distinguishes member-removal rotations (full DEK re-key) from user key-pair rotations (DEK re-wrap only)
ALTER TABLE encrypted_secret_keys
    ADD COLUMN group_rotation_pending BOOLEAN NOT NULL DEFAULT FALSE;
