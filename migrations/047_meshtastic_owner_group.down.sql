-- Remove any meshtastic channels owned by groups (they'd violate the old constraint)
DELETE FROM meshtastic_channels WHERE owner_group_id IS NOT NULL;

-- Drop 4-way XOR constraint
ALTER TABLE meshtastic_channels DROP CONSTRAINT chk_meshtastic_channel_owner;

-- Restore 3-way XOR constraint
ALTER TABLE meshtastic_channels ADD CONSTRAINT chk_meshtastic_channel_owner CHECK (
    (region_id IS NOT NULL AND school_id IS NULL AND district_id IS NULL) OR
    (region_id IS NULL AND school_id IS NOT NULL AND district_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NOT NULL)
);

-- Drop FK and index
ALTER TABLE meshtastic_channels DROP FOREIGN KEY fk_meshtastic_channels_owner_group;
ALTER TABLE meshtastic_channels DROP INDEX idx_meshtastic_channels_owner_group;

-- Drop columns
ALTER TABLE meshtastic_channels DROP COLUMN access_tier;
ALTER TABLE meshtastic_channels DROP COLUMN owner_group_id;
