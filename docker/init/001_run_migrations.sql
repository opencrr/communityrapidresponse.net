-- Docker initialization: Run all up migrations
-- This file sources all .up.sql files from the migrations directory

-- Note: MariaDB docker-entrypoint doesn't support SOURCE command easily,
-- so we copy the essential schema here for initial Docker setup.
-- For production, use `just db-migrate` which tracks applied migrations.

-- Create schema_migrations table
CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(255) PRIMARY KEY COMMENT 'Migration filename without .up.sql/.down.sql suffix',
    applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP COMMENT 'When the migration was applied'
);
