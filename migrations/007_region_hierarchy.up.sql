-- Migration: Update region hierarchy to State -> County -> City -> Neighborhood
-- Previous hierarchy: city_block, neighborhood, town, city
-- New hierarchy: state, county, city, neighborhood (city_block reserved for future)

-- Note: MariaDB doesn't support ALTER ENUM directly, but we can modify the CHECK constraint
-- or just expand the allowed values. Since we're using VARCHAR for region_type, we just need
-- to update any application-level validation.

-- Remove 'town' type - any existing 'town' regions should be updated to 'city'
UPDATE geographic_regions SET region_type = 'city' WHERE region_type = 'town';

-- Add index on parent_region_id for faster hierarchy traversal
-- (This may already exist, so we use IF NOT EXISTS pattern)
DROP PROCEDURE IF EXISTS add_parent_index;
DELIMITER //
CREATE PROCEDURE add_parent_index()
BEGIN
    DECLARE index_exists INT DEFAULT 0;
    SELECT COUNT(*) INTO index_exists FROM information_schema.statistics
    WHERE table_schema = DATABASE()
    AND table_name = 'geographic_regions'
    AND index_name = 'idx_parent_region_id';

    IF index_exists = 0 THEN
        CREATE INDEX idx_parent_region_id ON geographic_regions(parent_region_id);
    END IF;
END //
DELIMITER ;
CALL add_parent_index();
DROP PROCEDURE add_parent_index;

-- Add index on region_type for filtering
DROP PROCEDURE IF EXISTS add_type_index;
DELIMITER //
CREATE PROCEDURE add_type_index()
BEGIN
    DECLARE index_exists INT DEFAULT 0;
    SELECT COUNT(*) INTO index_exists FROM information_schema.statistics
    WHERE table_schema = DATABASE()
    AND table_name = 'geographic_regions'
    AND index_name = 'idx_region_type';

    IF index_exists = 0 THEN
        CREATE INDEX idx_region_type ON geographic_regions(region_type);
    END IF;
END //
DELIMITER ;
CALL add_type_index();
DROP PROCEDURE add_type_index;

-- Add composite index for name + type lookups (used when finding existing regions)
DROP PROCEDURE IF EXISTS add_name_type_index;
DELIMITER //
CREATE PROCEDURE add_name_type_index()
BEGIN
    DECLARE index_exists INT DEFAULT 0;
    SELECT COUNT(*) INTO index_exists FROM information_schema.statistics
    WHERE table_schema = DATABASE()
    AND table_name = 'geographic_regions'
    AND index_name = 'idx_region_name_type';

    IF index_exists = 0 THEN
        CREATE INDEX idx_region_name_type ON geographic_regions(name, region_type);
    END IF;
END //
DELIMITER ;
CALL add_name_type_index();
DROP PROCEDURE add_name_type_index;
