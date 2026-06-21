-- Add owner_group_id column to meshtastic_channels
ALTER TABLE meshtastic_channels
    ADD COLUMN owner_group_id CHAR(36) NULL AFTER district_id;

-- Add access_tier column
ALTER TABLE meshtastic_channels
    ADD COLUMN access_tier ENUM('open', 'resident', 'member', 'trusted', 'admin_only')
    NOT NULL DEFAULT 'member'
    AFTER description;

-- Add FK constraint
ALTER TABLE meshtastic_channels
    ADD CONSTRAINT fk_meshtastic_channels_owner_group
    FOREIGN KEY (owner_group_id) REFERENCES `groups`(id) ON DELETE CASCADE;

-- Add index for lookups
ALTER TABLE meshtastic_channels
    ADD INDEX idx_meshtastic_channels_owner_group (owner_group_id);

-- Drop the original 3-way XOR CHECK. It was created unnamed in migration 027, so
-- MariaDB auto-assigned a name whose exact value is version-dependent (e.g.
-- CONSTRAINT_1 on some versions, meshtastic_channels_chk_1 on others). Guessing
-- the name with DROP ... IF EXISTS silently no-ops on a mismatch, leaving the old
-- 3-way constraint to AND with the new 4-way and reject every owner_group_id
-- insert. Instead, look the name up from information_schema and drop it explicitly.
SET @old_check := (
    SELECT CONSTRAINT_NAME FROM information_schema.TABLE_CONSTRAINTS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'meshtastic_channels'
      AND CONSTRAINT_TYPE = 'CHECK'
    LIMIT 1
);
SET @drop_sql := IF(@old_check IS NOT NULL,
    CONCAT('ALTER TABLE meshtastic_channels DROP CONSTRAINT `', @old_check, '`'),
    'DO 0');
PREPARE drop_old_check FROM @drop_sql;
EXECUTE drop_old_check;
DEALLOCATE PREPARE drop_old_check;

-- Add 4-way XOR constraint (exactly one of region_id, school_id, district_id, owner_group_id must be set)
ALTER TABLE meshtastic_channels ADD CONSTRAINT chk_meshtastic_channel_owner CHECK (
    (region_id IS NOT NULL AND school_id IS NULL AND district_id IS NULL AND owner_group_id IS NULL) OR
    (region_id IS NULL AND school_id IS NOT NULL AND district_id IS NULL AND owner_group_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NOT NULL AND owner_group_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NULL AND owner_group_id IS NOT NULL)
);
