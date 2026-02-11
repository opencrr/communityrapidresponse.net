-- Migration 004: Change geometry columns to GEOMETRY type
-- This allows storing both Polygon and MultiPolygon types (from OSM boundaries)

-- Modify the geometry column to accept any geometry type
-- This column may already be GEOMETRY or POLYGON - both will work
ALTER TABLE geographic_regions MODIFY COLUMN geometry GEOMETRY NOT NULL;

-- Also update admin_boundaries table if it has geometry
ALTER TABLE admin_boundaries MODIFY COLUMN boundary_geometry GEOMETRY;
