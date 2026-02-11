-- Migration: Make geometry nullable and add locality region type
-- This allows regions without boundaries (neighborhoods, localities) to be stored

-- Make geometry column nullable for regions without OSM boundaries
-- Most neighborhoods (~92%) and some localities lack polygon boundaries
-- Note: SRID is preserved from original column definition
ALTER TABLE geographic_regions MODIFY COLUMN geometry GEOMETRY NULL;

-- Update the region_type constraint comment to include locality
-- Note: MariaDB doesn't have ENUM constraints, but document the valid values:
-- Valid region types: 'state', 'county', 'city', 'locality', 'neighborhood', 'city_block'
-- Locality is used for boroughs and sub-city divisions (e.g., Brooklyn in NYC)
