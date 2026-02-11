-- Migration tracking table
-- This table tracks which migrations have been applied to the database
CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(255) PRIMARY KEY COMMENT 'Migration filename without .up.sql/.down.sql suffix',
    applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP COMMENT 'When the migration was applied'
);
