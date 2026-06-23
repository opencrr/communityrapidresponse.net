-- Add owner_group_id column to signal_groups
ALTER TABLE signal_groups
    ADD COLUMN owner_group_id CHAR(36) NULL AFTER district_id;

-- Add FK constraint
ALTER TABLE signal_groups
    ADD CONSTRAINT fk_signal_groups_owner_group
    FOREIGN KEY (owner_group_id) REFERENCES `groups`(id) ON DELETE CASCADE;

-- Add index for lookups
ALTER TABLE signal_groups
    ADD INDEX idx_signal_groups_owner_group (owner_group_id);

-- Drop old 3-way XOR constraint
ALTER TABLE signal_groups DROP CONSTRAINT chk_signal_group_owner;

-- Add new 4-way XOR constraint (exactly one of region_id, school_id, district_id, owner_group_id must be set)
ALTER TABLE signal_groups ADD CONSTRAINT chk_signal_group_owner CHECK (
    (region_id IS NOT NULL AND school_id IS NULL AND district_id IS NULL AND owner_group_id IS NULL) OR
    (region_id IS NULL AND school_id IS NOT NULL AND district_id IS NULL AND owner_group_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NOT NULL AND owner_group_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NULL AND owner_group_id IS NOT NULL)
);
