#!/bin/bash
# Apply all up migrations for Docker initialization
# This script runs during docker-entrypoint initialization

set -e

MIGRATIONS_DIR="/migrations"

if [ ! -d "$MIGRATIONS_DIR" ]; then
    echo "Migrations directory not found: $MIGRATIONS_DIR"
    exit 0
fi

# Use mariadb command (mysql is not available in MariaDB 11+ containers)
DB_CMD="mariadb"

echo "Applying migrations from $MIGRATIONS_DIR..."

for migration in "$MIGRATIONS_DIR"/*.up.sql; do
    if [ -f "$migration" ]; then
        filename=$(basename "$migration" .up.sql)
        echo "  Applying: $filename"
        $DB_CMD -u root -p"$MYSQL_ROOT_PASSWORD" "$MYSQL_DATABASE" < "$migration"
        $DB_CMD -u root -p"$MYSQL_ROOT_PASSWORD" "$MYSQL_DATABASE" -e "INSERT IGNORE INTO schema_migrations (version) VALUES ('$filename');"
    fi
done

echo "Migrations complete."
