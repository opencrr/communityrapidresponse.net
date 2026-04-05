-- Remove any signal groups owned by groups (they'd violate the old constraint)
DELETE FROM signal_groups WHERE owner_group_id IS NOT NULL;

-- Drop 4-way XOR constraint
ALTER TABLE signal_groups DROP CONSTRAINT chk_signal_group_owner;

-- Restore 3-way XOR constraint
ALTER TABLE signal_groups ADD CONSTRAINT chk_signal_group_owner CHECK (
    (region_id IS NOT NULL AND school_id IS NULL AND district_id IS NULL) OR
    (region_id IS NULL AND school_id IS NOT NULL AND district_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NOT NULL)
);

-- Drop FK and index
ALTER TABLE signal_groups DROP FOREIGN KEY fk_signal_groups_owner_group;
ALTER TABLE signal_groups DROP INDEX idx_signal_groups_owner_group;

-- Drop column
ALTER TABLE signal_groups DROP COLUMN owner_group_id;
