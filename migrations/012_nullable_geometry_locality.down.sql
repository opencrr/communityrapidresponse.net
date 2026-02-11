-- Rollback Migration 012: Nullable geometry and locality
-- Make geometry column NOT NULL again
-- WARNING: This will fail if any rows have NULL geometry

ALTER TABLE geographic_regions MODIFY COLUMN geometry GEOMETRY NOT NULL;
