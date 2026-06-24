-- 051 rollback: remove group_rotation_pending flag
ALTER TABLE encrypted_secret_keys
    DROP COLUMN group_rotation_pending;
