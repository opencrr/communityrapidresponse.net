-- Rollback Migration 007: Region hierarchy changes
-- Note: Cannot reliably restore 'town' data as it was converted to 'city'

DROP INDEX IF EXISTS idx_region_name_type ON geographic_regions;
DROP INDEX IF EXISTS idx_region_type ON geographic_regions;
DROP INDEX IF EXISTS idx_parent_region_id ON geographic_regions;
