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

-- Drop old 3-way XOR constraint (unnamed in original CREATE TABLE, MariaDB auto-names it)
ALTER TABLE meshtastic_channels DROP CONSTRAINT IF EXISTS CONSTRAINT_1;
ALTER TABLE meshtastic_channels DROP CONSTRAINT IF EXISTS chk_meshtastic_channel_owner;

-- Add 4-way XOR constraint (exactly one of region_id, school_id, district_id, owner_group_id must be set)
ALTER TABLE meshtastic_channels ADD CONSTRAINT chk_meshtastic_channel_owner CHECK (
    (region_id IS NOT NULL AND school_id IS NULL AND district_id IS NULL AND owner_group_id IS NULL) OR
    (region_id IS NULL AND school_id IS NOT NULL AND district_id IS NULL AND owner_group_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NOT NULL AND owner_group_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NULL AND owner_group_id IS NOT NULL)
);
