-- Rollback Migration 005: Geometry type change
-- Revert to POLYGON type (may fail if data contains MultiPolygon)

ALTER TABLE admin_boundaries MODIFY COLUMN boundary_geometry POLYGON;
ALTER TABLE geographic_regions MODIFY COLUMN geometry POLYGON NOT NULL;
