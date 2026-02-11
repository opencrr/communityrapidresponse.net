-- Reverse migration: drop school tables and restore signal_groups

-- Step 1: Drop the three-way XOR constraint
ALTER TABLE signal_groups DROP CONSTRAINT chk_signal_group_owner;

-- Step 2: Drop school/district foreign keys and columns
ALTER TABLE signal_groups DROP FOREIGN KEY fk_signal_groups_district;
ALTER TABLE signal_groups DROP FOREIGN KEY fk_signal_groups_school;
ALTER TABLE signal_groups DROP FOREIGN KEY fk_signal_groups_region;

ALTER TABLE signal_groups DROP INDEX idx_district_id;
ALTER TABLE signal_groups DROP INDEX idx_school_id;

ALTER TABLE signal_groups DROP COLUMN district_id;
ALTER TABLE signal_groups DROP COLUMN school_id;

-- Step 3: Restore region_id as NOT NULL
ALTER TABLE signal_groups MODIFY COLUMN region_id CHAR(36) NOT NULL;

-- Step 4: Restore original foreign key
ALTER TABLE signal_groups ADD FOREIGN KEY (region_id) REFERENCES geographic_regions(id) ON DELETE CASCADE;

-- Step 5: Drop school tables in dependency order
DROP TABLE IF EXISTS school_blocked_users;
DROP TABLE IF EXISTS school_vouches;
DROP TABLE IF EXISTS user_schools;
DROP TABLE IF EXISTS schools;
DROP TABLE IF EXISTS school_districts;
