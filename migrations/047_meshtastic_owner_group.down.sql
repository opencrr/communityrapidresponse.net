-- Remove any meshtastic channels owned by groups (they'd violate the old constraint)
DELETE FROM meshtastic_channels WHERE owner_group_id IS NOT NULL;

-- Drop the active 4-way XOR CHECK by its real name (looked up rather than assumed,
-- mirroring the up migration so a rename in either direction can't leave a stale
-- constraint behind).
-- INVARIANT: meshtastic_channels has exactly ONE CHECK constraint here (the owner
-- XOR), so LIMIT 1 is unambiguous; make this specific if a second CHECK is ever added.
SET @cur_check := (
    SELECT CONSTRAINT_NAME FROM information_schema.TABLE_CONSTRAINTS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'meshtastic_channels'
      AND CONSTRAINT_TYPE = 'CHECK'
    LIMIT 1
);
SET @drop_sql := IF(@cur_check IS NOT NULL,
    CONCAT('ALTER TABLE meshtastic_channels DROP CONSTRAINT `', @cur_check, '`'),
    'DO 0');
PREPARE drop_cur_check FROM @drop_sql;
EXECUTE drop_cur_check;
DEALLOCATE PREPARE drop_cur_check;

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
