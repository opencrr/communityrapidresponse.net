# Community Rapid Response - Development Commands
# Run `just` or `just help` to see available commands

# Default recipe - show help
default:
    @just --list

# =============================================================================
# Environment Setup
# =============================================================================

# Initialize development environment (first time setup)
init: _check-deps
    @echo "🚀 Initializing development environment..."
    @if [ ! -f .env ]; then cp .env.example .env && echo "✅ Created .env from .env.example"; else echo "ℹ️  .env already exists"; fi
    @just deps
    @just db-start
    @echo "✅ Development environment ready!"
    @echo ""
    @echo "Next steps:"
    @echo "  just dev     - Start the development server"
    @echo "  just test    - Run tests"
    @echo "  just help    - Show all commands"

# Check required dependencies
_check-deps:
    @command -v docker >/dev/null 2>&1 || { echo "❌ Docker is required but not installed."; exit 1; }
    @command -v go >/dev/null 2>&1 || { echo "❌ Go is required but not installed."; exit 1; }
    @echo "✅ Dependencies checked"

# Install Go dependencies
deps:
    @echo "📦 Installing Go dependencies..."
    go mod download
    go mod tidy

# =============================================================================
# Development Server
# =============================================================================

# Start development server with hot reload (in Docker)
dev: db-start
    @echo "🔥 Starting development server with hot reload..."
    docker compose up app --build

# Start development server with web frontend (in Docker)
dev-full: db-start
    @echo "🔥 Starting development server with web frontend..."
    docker compose up app web --build

# Start development server locally (without Docker)
dev-local: db-start
    @echo "🔥 Starting local development server..."
    @if command -v air >/dev/null 2>&1; then \
        air -c .air.toml; \
    else \
        echo "Installing air for hot reload..."; \
        go install github.com/air-verse/air@latest; \
        air -c .air.toml; \
    fi

# Run the server once (no hot reload)
run: db-start
    @echo "▶️  Running server..."
    go run ./cmd/server

# Build the application
build:
    @echo "🔨 Building application..."
    go build -o bin/server ./cmd/server
    @echo "✅ Binary created at bin/server"

# Build production Docker image
build-prod:
    @echo "🐳 Building production Docker image..."
    docker build --target production -t communityrapidresponse:latest .

# =============================================================================
# Database
# =============================================================================

# Start database containers
db-start:
    @echo "🗄️  Starting database..."
    docker compose up -d db db-test
    @echo "⏳ Waiting for database to be ready..."
    @sleep 5
    @just _wait-for-db
    @echo "✅ Database is ready"

# Wait for database to be healthy
_wait-for-db:
    @for i in 1 2 3 4 5 6 7 8 9 10; do \
        if docker compose exec -T db mariadb -u root -prootpassword -e "SELECT 1" >/dev/null 2>&1; then \
            break; \
        fi; \
        echo "  Waiting for database... ($$i/10)"; \
        sleep 2; \
    done

# Stop database containers
db-stop:
    @echo "🛑 Stopping database..."
    docker compose down db db-test

# Connect to database CLI (just db-cli [test])
db-cli env="dev":
    #!/usr/bin/env bash
    set -euo pipefail
    if [ "{{env}}" = "test" ]; then
        docker compose exec db-test mariadb -u root -ptestroot communityrapidresponse_test
    else
        docker compose exec db mariadb -u communityrapidresponse -pdevpassword communityrapidresponse
    fi


# Run database migrations (just db-migrate [test])
# Migrations are idempotent - only pending migrations are applied
db-migrate env="dev":
    #!/usr/bin/env bash
    set -euo pipefail
    echo "🔄 Running migrations..."

    if [ "{{env}}" = "test" ]; then
        echo "Target: local test Docker"
    else
        echo "Target: local Docker"
    fi

    # Function to run SQL
    run_sql() {
        if [ "{{env}}" = "test" ]; then
            docker compose exec -T db-test mariadb -u root -ptestroot communityrapidresponse_test -e "$1"
        else
            docker compose exec -T db mariadb -u root -prootpassword communityrapidresponse -e "$1"
        fi
    }

    run_file() {
        if [ "{{env}}" = "test" ]; then
            docker compose exec -T db-test mariadb -u root -ptestroot communityrapidresponse_test < "$1"
        else
            docker compose exec -T db mariadb -u root -prootpassword communityrapidresponse < "$1"
        fi
    }

    # Ensure schema_migrations table exists
    run_sql "CREATE TABLE IF NOT EXISTS schema_migrations (version VARCHAR(255) PRIMARY KEY, applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);"

    # Run pending up migrations
    pending=0
    for migration in migrations/*.up.sql; do
        filename=$(basename "$migration" .up.sql)
        # Check if already applied
        applied=$(run_sql "SELECT 1 FROM schema_migrations WHERE version='$filename'" 2>/dev/null | tail -n1 || echo "")
        if [ -z "$applied" ] || [ "$applied" = "1" ]; then
            # Need to check more carefully - if result contains "1" it's applied
            check=$(run_sql "SELECT COUNT(*) FROM schema_migrations WHERE version='$filename'" 2>/dev/null | tail -n1 || echo "0")
            if [ "$check" = "0" ]; then
                echo "  ⬆️  Applying: $filename"
                run_file "$migration"
                run_sql "INSERT INTO schema_migrations (version) VALUES ('$filename');"
                pending=$((pending + 1))
            else
                echo "  ✓  Already applied: $filename"
            fi
        else
            echo "  ✓  Already applied: $filename"
        fi
    done

    if [ $pending -eq 0 ]; then
        echo "✅ No pending migrations"
    else
        echo "✅ Applied $pending migration(s)"
    fi

# Rollback the last N migrations (just db-migrate-down [env] [count])
db-migrate-down env="dev" count="1":
    #!/usr/bin/env bash
    set -euo pipefail
    echo "🔄 Rolling back {{count}} migration(s)..."

    if [ "{{env}}" = "test" ]; then
        echo "Target: local test Docker"
    else
        echo "Target: local Docker"
    fi

    run_sql() {
        if [ "{{env}}" = "test" ]; then
            docker compose exec -T db-test mariadb -u root -ptestroot communityrapidresponse_test -N -e "$1"
        else
            docker compose exec -T db mariadb -u root -prootpassword communityrapidresponse -N -e "$1"
        fi
    }

    run_file() {
        if [ "{{env}}" = "test" ]; then
            docker compose exec -T db-test mariadb -u root -ptestroot communityrapidresponse_test < "$1"
        else
            docker compose exec -T db mariadb -u root -prootpassword communityrapidresponse < "$1"
        fi
    }

    # Get the last N applied migrations (excluding schema_migrations itself)
    migrations_to_rollback=$(run_sql "SELECT version FROM schema_migrations WHERE version != '000_schema_migrations' ORDER BY version DESC LIMIT {{count}}" 2>/dev/null || echo "")

    if [ -z "$migrations_to_rollback" ]; then
        echo "❌ No migrations to rollback"
        exit 1
    fi

    rolled_back=0
    for version in $migrations_to_rollback; do
        down_file="migrations/${version}.down.sql"
        if [ -f "$down_file" ]; then
            echo "  ⬇️  Rolling back: $version"
            run_file "$down_file"
            run_sql "DELETE FROM schema_migrations WHERE version='$version';"
            rolled_back=$((rolled_back + 1))
        else
            echo "  ❌ No down migration found: $down_file"
            exit 1
        fi
    done

    echo "✅ Rolled back $rolled_back migration(s)"

# Show migration status (just db-migrate-status [test])
db-migrate-status env="dev":
    #!/usr/bin/env bash
    set -euo pipefail
    echo "📋 Migration status"

    if [ "{{env}}" = "test" ]; then
        echo "Target: local test Docker"
    else
        echo "Target: local Docker"
    fi

    run_sql() {
        if [ "{{env}}" = "test" ]; then
            docker compose exec -T db-test mariadb -u root -ptestroot communityrapidresponse_test -N -e "$1" 2>/dev/null || echo ""
        else
            docker compose exec -T db mariadb -u root -prootpassword communityrapidresponse -N -e "$1" 2>/dev/null || echo ""
        fi
    }

    # Check if schema_migrations table exists
    table_exists=$(run_sql "SELECT 1 FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='schema_migrations'" || echo "")
    if [ -z "$table_exists" ]; then
        echo "  ⚠️  schema_migrations table does not exist"
        echo "  Run 'just db-migrate' to initialize"
        exit 0
    fi

    echo ""
    echo "Applied migrations:"
    applied=$(run_sql "SELECT version, applied_at FROM schema_migrations ORDER BY version" || echo "")
    if [ -z "$applied" ]; then
        echo "  (none)"
    else
        echo "$applied" | while read -r line; do
            echo "  ✓ $line"
        done
    fi

    echo ""
    echo "Pending migrations:"
    pending=0
    for migration in migrations/*.up.sql; do
        filename=$(basename "$migration" .up.sql)
        check=$(run_sql "SELECT COUNT(*) FROM schema_migrations WHERE version='$filename'" || echo "0")
        if [ "$check" = "0" ]; then
            echo "  ○ $filename"
            pending=$((pending + 1))
        fi
    done
    if [ $pending -eq 0 ]; then
        echo "  (none)"
    fi

# Reset database (drop and recreate) (just db-reset [test])
db-reset env="dev":
    #!/usr/bin/env bash
    set -euo pipefail
    echo "⚠️  Resetting database..."
    if [ "{{env}}" = "test" ]; then
        docker compose exec -T db-test mariadb -u root -ptestroot -e "DROP DATABASE IF EXISTS communityrapidresponse_test; CREATE DATABASE communityrapidresponse_test;"
        just db-migrate test
    else
        docker compose exec -T db mariadb -u root -prootpassword -e "DROP DATABASE IF EXISTS communityrapidresponse; CREATE DATABASE communityrapidresponse;"
        just db-migrate
    fi
    echo "✅ Database reset complete"


# Show database logs
db-logs:
    docker compose logs -f db

# =============================================================================
# Web Server
# =============================================================================

# Start web server (serves static frontend, proxies to Go backend)
web-start: db-start
    @echo "🌐 Starting web server..."
    docker compose up -d app web
    @just _wait-for-web
    @echo "✅ Web server is ready at http://localhost:3000"

# Stop web server
web-stop:
    @echo "🛑 Stopping web server..."
    docker compose stop web app

# Wait for web server to be healthy
_wait-for-web:
    @for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
        if curl -sf http://localhost:3000/health >/dev/null 2>&1; then \
            break; \
        fi; \
        echo "  Waiting for web server... ($$i/15)"; \
        sleep 2; \
    done

# Start test environment (test database + test app + test web server)
test-env-start: db-start _wait-for-test-db
    @echo "🧪 Starting test environment..."
    docker compose --profile test up -d app-test web-test
    @just _wait-for-test-app
    @just _wait-for-test-web
    @echo ""
    @echo "✅ Test environment is ready:"
    @echo "   - Test API:      http://localhost:8081"
    @echo "   - Test Web:      http://localhost:3001"
    @echo "   - Test Database: localhost:3307"

# Stop test environment (app, web, and test database)
test-env-stop:
    @echo "🛑 Stopping test environment..."
    docker compose --profile test stop app-test web-test
    docker compose stop db-test
    @echo "✅ Test environment stopped"

# Stop and remove all test containers and volumes
test-env-down:
    @echo "🛑 Tearing down test environment..."
    docker compose --profile test down
    docker compose stop db-test
    docker compose rm -f db-test
    @echo "✅ Test environment torn down"

# Wait for test app to be healthy
_wait-for-test-app:
    @for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
        if curl -sf http://localhost:8081/health >/dev/null 2>&1; then \
            break; \
        fi; \
        echo "  Waiting for test app... ($$i/15)"; \
        sleep 2; \
    done

# Wait for test web server to be healthy
_wait-for-test-web:
    @for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
        if curl -sf http://localhost:3001/templates/index.html >/dev/null 2>&1; then \
            break; \
        fi; \
        echo "  Waiting for test web server... ($$i/15)"; \
        sleep 2; \
    done

# =============================================================================
# Testing
# =============================================================================

# Test environment variables for running tests on host (limited - can't access Docker DB)
TEST_ENV := "TEST_DB_HOST=localhost TEST_DB_PORT=3307 TEST_DB_USER=root TEST_DB_PASSWORD=testroot TEST_DB_NAME=communityrapidresponse_test"
TEST_WEB_ENV := "TEST_WEB_URL=http://localhost:3001 TEST_API_URL=http://localhost:8081"

# Run all tests inside Docker (recommended - full database access)
test: db-start _wait-for-test-db
    @echo "🧪 Running all tests in Docker..."
    docker compose run --rm test go test -p 1 ./... -timeout 120s

# Run all tests on host (excludes handler tests due to Docker networking)
test-host: db-start _wait-for-test-db
    @echo "🧪 Running tests on host..."
    {{TEST_ENV}} go test -p 1 ./internal/config/... ./internal/middleware/... ./internal/models/... ./internal/services/... ./internal/database/... ./tests/... -timeout 120s

# Run unit tests only (no database required, uses mocks)
test-unit:
    @echo "🧪 Running unit tests (no database)..."
    go test ./internal/config/... ./internal/middleware/... ./internal/models/... -v
    @echo ""
    @echo "🧪 Running service tests with mocks..."
    go test ./internal/services/... -v

# Run integration tests (requires database, uses mocked external services)
test-integration: db-start _wait-for-test-db
    @echo "🧪 Running integration tests in Docker..."
    docker compose run --rm test go test ./tests/... -v -run Integration

# Run e2e tests (requires database + web server, uses mocked external services)
test-e2e: test-env-start
    @echo "🧪 Running end-to-end tests in Docker..."
    docker compose run --rm -e TEST_WEB_URL=http://web-test -e TEST_API_URL=http://app-test:8080 test go test ./tests/... -v -run E2E

# Run e2e tests on host (requires test environment to be running)
test-e2e-host: test-env-start
    @echo "🧪 Running end-to-end tests on host..."
    {{TEST_ENV}} {{TEST_WEB_ENV}} go test ./tests/... -v -run E2E -timeout 120s

# Run database tests (requires database)
test-db: db-start _wait-for-test-db
    @echo "🧪 Running database tests in Docker..."
    docker compose run --rm test go test ./internal/database/... -v

# Run handler tests (requires database)
test-handlers: db-start _wait-for-test-db
    @echo "🧪 Running handler tests in Docker..."
    docker compose run --rm test go test ./internal/handlers/... -v

# Run tests with coverage
test-coverage: db-start _wait-for-test-db
    @echo "🧪 Running tests with coverage in Docker..."
    docker compose run --rm test go test ./... -cover -coverprofile=coverage.out
    go tool cover -func=coverage.out

# Generate HTML coverage report
test-coverage-html: test-coverage
    go tool cover -html=coverage.out -o coverage.html
    @echo "✅ Coverage report generated: coverage.html"
    @if command -v open >/dev/null 2>&1; then open coverage.html; fi

# Run a specific test by name
test-run NAME: db-start _wait-for-test-db
    @echo "🧪 Running test: {{NAME}} in Docker..."
    docker compose run --rm test go test ./... -v -run {{NAME}}

# Run tests in watch mode
test-watch:
    @if command -v watchexec >/dev/null 2>&1; then \
        watchexec -e go -r -- go test ./internal/... -short -v; \
    else \
        echo "watchexec not installed. Run: brew install watchexec"; \
        exit 1; \
    fi

# Run benchmarks
bench:
    @echo "⏱️  Running benchmarks..."
    go test ./... -bench=. -benchmem

# Wait for test database to be ready
_wait-for-test-db:
    @for i in 1 2 3 4 5 6 7 8 9 10; do \
        if docker compose exec -T db-test mariadb -u root -ptestroot -e "SELECT 1" >/dev/null 2>&1; then \
            break; \
        fi; \
        echo "  Waiting for test database... ($$i/10)"; \
        sleep 2; \
    done

# =============================================================================
# Code Quality
# =============================================================================

# Run all linters
lint:
    @echo "🔍 Running linters..."
    @if command -v golangci-lint >/dev/null 2>&1; then \
        golangci-lint run ./...; \
    else \
        echo "golangci-lint not installed. Run: brew install golangci-lint"; \
        go vet ./...; \
    fi

# Format code
fmt:
    @echo "✨ Formatting code..."
    go fmt ./...
    @if command -v goimports >/dev/null 2>&1; then goimports -w .; fi

# Run go vet
vet:
    @echo "🔍 Running go vet..."
    go vet ./...

# Tidy go modules
tidy:
    @echo "🧹 Tidying modules..."
    go mod tidy

# Detect drift between prose docs (CLAUDE.md/DESIGN.md/README.md) and the real code/schema
doc-drift *ARGS:
    @go run ./cmd/doc-drift {{ARGS}}

# Run security scanners (gosec + govulncheck)
security:
    @echo "🔒 Running security scanners..."
    @command -v gosec >/dev/null 2>&1 || { echo "Installing gosec..."; go install github.com/securego/gosec/v2/cmd/gosec@latest; }
    gosec ./...
    @command -v govulncheck >/dev/null 2>&1 || { echo "Installing govulncheck..."; go install golang.org/x/vuln/cmd/govulncheck@latest; }
    govulncheck ./...
    @echo "✅ Security scan complete"

# Verify module dependency integrity
verify:
    @echo "🔍 Verifying module dependencies..."
    go mod verify

# =============================================================================
# Docker Management
# =============================================================================

# Start all services
up:
    @echo "🚀 Starting all services..."
    docker compose up -d
    @echo ""
    @echo "✅ Services running:"
    @echo "   - API:      http://localhost:8080"
    @echo "   - Web:      http://localhost:3000"
    @echo "   - Database: localhost:3306"
    @echo ""
    @echo "Run 'just up-with-tools' to also start Adminer (DB UI)"

# Stop all services
down:
    @echo "🛑 Stopping all services..."
    docker compose down

# Restart all services
restart: down up

# View logs
logs:
    docker compose logs -f

# View app logs only
logs-app:
    docker compose logs -f app

# Clean up Docker resources and build/test artifacts
clean:
    @echo "🧹 Cleaning up..."
    docker compose down -v --remove-orphans
    rm -rf tmp/ bin/ coverage.out coverage.html
    rm -f *.test server
    @echo "✅ Cleanup complete"

# Start with Adminer (database UI)
up-with-tools:
    @echo "🚀 Starting all services with tools..."
    docker compose --profile tools up -d
    @echo ""
    @echo "✅ Services running:"
    @echo "   - API:      http://localhost:8080"
    @echo "   - Web:      http://localhost:3000"
    @echo "   - Database: localhost:3306"
    @echo "   - Adminer:  http://localhost:8082"

# =============================================================================
# Utilities
# =============================================================================

# Create database on local MySQL/MariaDB (non-Docker setup)
# Requires DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME environment variables
create-db:
    #!/usr/bin/env bash
    set -euo pipefail
    DB_HOST="${DB_HOST:-localhost}"
    DB_PORT="${DB_PORT:-3306}"
    DB_NAME="${DB_NAME:-communityrapidresponse}"
    DB_USER="${DB_USER:-communityrapidresponse}"
    DB_PASSWORD="${DB_PASSWORD:-}"
    if [ -z "$DB_PASSWORD" ]; then
        echo "❌ DB_PASSWORD environment variable is required"
        exit 1
    fi
    echo "🗄️  Creating database on $DB_HOST:$DB_PORT..."
    read -s -p "MySQL root password: " ROOT_PASSWORD
    echo ""
    mysql -h "$DB_HOST" -P "$DB_PORT" -u root -p"$ROOT_PASSWORD" -e "CREATE DATABASE IF NOT EXISTS $DB_NAME CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"
    mysql -h "$DB_HOST" -P "$DB_PORT" -u root -p"$ROOT_PASSWORD" -e "CREATE USER IF NOT EXISTS '$DB_USER'@'%' IDENTIFIED BY '$DB_PASSWORD';"
    mysql -h "$DB_HOST" -P "$DB_PORT" -u root -p"$ROOT_PASSWORD" -e "GRANT ALL PRIVILEGES ON $DB_NAME.* TO '$DB_USER'@'%';"
    mysql -h "$DB_HOST" -P "$DB_PORT" -u root -p"$ROOT_PASSWORD" -e "FLUSH PRIVILEGES;"
    echo "✅ Database '$DB_NAME' created with user '$DB_USER'"

# Apply migrations to local MySQL/MariaDB (non-Docker setup)
# Requires DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME environment variables
migrate-local:
    #!/usr/bin/env bash
    set -euo pipefail
    DB_HOST="${DB_HOST:-localhost}"
    DB_PORT="${DB_PORT:-3306}"
    DB_NAME="${DB_NAME:-communityrapidresponse}"
    DB_USER="${DB_USER:-communityrapidresponse}"
    DB_PASSWORD="${DB_PASSWORD:-}"
    if [ -z "$DB_PASSWORD" ]; then
        echo "❌ DB_PASSWORD environment variable is required"
        exit 1
    fi
    MYSQL="mysql -h $DB_HOST -P $DB_PORT -u $DB_USER -p$DB_PASSWORD $DB_NAME"
    echo "🔄 Applying migrations to $DB_HOST:$DB_PORT/$DB_NAME..."

    # Ensure schema_migrations table exists
    $MYSQL -e "CREATE TABLE IF NOT EXISTS schema_migrations (version VARCHAR(255) PRIMARY KEY, applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);"

    # Run pending up migrations
    pending=0
    for migration in migrations/*.up.sql; do
        filename=$(basename "$migration" .up.sql)
        check=$($MYSQL -N -e "SELECT COUNT(*) FROM schema_migrations WHERE version='$filename'" 2>/dev/null || echo "0")
        if [ "$check" = "0" ]; then
            echo "  ⬆️  Applying: $filename"
            $MYSQL < "$migration"
            $MYSQL -e "INSERT INTO schema_migrations (version) VALUES ('$filename');"
            pending=$((pending + 1))
        else
            echo "  ✓  Already applied: $filename"
        fi
    done

    if [ $pending -eq 0 ]; then
        echo "✅ No pending migrations"
    else
        echo "✅ Applied $pending migration(s)"
    fi

# Check that esbuild is installed
_check-esbuild:
    @command -v esbuild >/dev/null 2>&1 || { echo "❌ esbuild is required but not installed."; echo "   Install with: go install github.com/evanw/esbuild/cmd/esbuild@latest"; exit 1; }

# Bundle frontend assets with content-hashed filenames for production
build-frontend: _check-esbuild
    #!/usr/bin/env bash
    set -euo pipefail
    echo "🔨 Building frontend bundles..."

    # Clean previous build
    rm -rf static/dist
    mkdir -p static/dist

    # Bundle JS
    esbuild static/js/app.js \
        --bundle \
        --minify \
        --sourcemap \
        --entry-names='[name]-[hash]' \
        --outdir=static/dist \
        --format=esm \
        --target=es2020

    # Bundle CSS
    esbuild static/css/main.css \
        --bundle \
        --minify \
        --entry-names='[name]-[hash]' \
        --outdir=static/dist

    # Find generated hashed filenames
    JS_FILE=$(basename static/dist/app-*.js | head -1)
    CSS_FILE=$(basename static/dist/main-*.css | head -1)
    FAVICON_V=$(git rev-parse --short HEAD)

    echo "  JS:  $JS_FILE"
    echo "  CSS: $CSS_FILE"

    # Update index.html to reference hashed bundles (handles both source and previous dist paths)
    sed -i '' -E "s|src=\"/static/[^\"]+\.js\"|src=\"/static/dist/$JS_FILE\"|" templates/index.html
    sed -i '' -E "s|href=\"/static/[^\"]+\.css\"|href=\"/static/dist/$CSS_FILE\"|" templates/index.html
    sed -i '' -E "s|(/static/images/[^\"?]*)(\?v=[^\"]*)?|\1?v=$FAVICON_V|g" templates/index.html

    echo "✅ Frontend build complete"

# Generate a secure JWT secret
generate-secret:
    @openssl rand -base64 32

# Check API health
health:
    @curl -s http://localhost:8080/health | jq . || echo "Server not running"

# Show environment info
info:
    @echo "=== Environment Info ==="
    @echo "Go version: $(go version)"
    @echo "Docker version: $(docker --version)"
    @echo ""
    @echo "=== Container Status ==="
    @docker compose ps

# Open Adminer in browser
adminer:
    @if [ "$(docker compose ps -q adminer 2>/dev/null)" = "" ]; then \
        echo "Starting Adminer..."; \
        docker compose --profile tools up -d adminer; \
    fi
    @echo "Opening Adminer at http://localhost:8082"
    @if command -v open >/dev/null 2>&1; then open http://localhost:8082; fi

# =============================================================================
# Superuser Management
# =============================================================================

# Create a superuser account (interactive)
create-superuser: _maybe-db-start
    #!/usr/bin/env bash
    set -euo pipefail
    echo "👤 Create Superuser Account"
    echo "=========================="
    echo ""
    read -p "Email: " email
    read -p "Username: " username
    read -s -p "Password: " password
    echo ""
    read -s -p "Confirm Password: " password_confirm
    echo ""
    if [ "$password" != "$password_confirm" ]; then
        echo "❌ Passwords do not match"
        exit 1
    fi
    if [ ${#password} -lt 8 ]; then
        echo "❌ Password must be at least 8 characters"
        exit 1
    fi
    uuid=$(uuidgen | tr '[:upper:]' '[:lower:]')
    if command -v htpasswd >/dev/null 2>&1; then
        password_hash=$(htpasswd -bnBC 12 "" "$password" | tr -d ':\n' | sed 's/\$2y/\$2a/')
    else
        echo "⚠️  htpasswd not found, using Python for bcrypt..."
        password_hash=$(python3 -c "import bcrypt; print(bcrypt.hashpw('$password'.encode(), bcrypt.gensalt(12)).decode())")
    fi
    # Generate TOTP secret (base32, 32 characters)
    mfa_secret=$(openssl rand -base64 20 | tr -d '/+=' | head -c 32 | tr '[:lower:]' '[:upper:]')
    # Generate 10 backup codes
    backup_codes=""
    backup_codes_hashed="["
    echo ""
    echo "📱 MFA Setup"
    echo "============"
    echo ""
    echo "Backup Codes (save these!):"
    for i in {1..10}; do
        code=$(openssl rand -hex 4 | tr '[:lower:]' '[:upper:]')
        code_formatted="${code:0:4}-${code:4:4}"
        echo "  $i. $code_formatted"
        code_hash=$(htpasswd -bnBC 10 "" "$code" | tr -d ':\n' | sed 's/\$2y/\$2a/')
        if [ $i -gt 1 ]; then
            backup_codes_hashed="$backup_codes_hashed,"
        fi
        backup_codes_hashed="$backup_codes_hashed\"$code_hash\""
    done
    backup_codes_hashed="$backup_codes_hashed]"
    echo ""
    # Note: mfa_secret is stored unencrypted here - in production it should be encrypted with MFA_ENCRYPTION_KEY
    # For CLI-created superusers, we skip encryption as they may not have the key available
    # The user will need to re-setup MFA through the web interface for proper encryption
    sql="INSERT INTO users (id, email, username, password_hash, email_verified, postcard_verified, vouch_verified, is_superuser, verification_tier, mfa_enabled, mfa_setup_required, mfa_backup_codes) VALUES ('$uuid', '$email', '$username', '$password_hash', TRUE, TRUE, TRUE, TRUE, 1, FALSE, TRUE, '$backup_codes_hashed') ON DUPLICATE KEY UPDATE is_superuser = TRUE, email_verified = TRUE, postcard_verified = TRUE, vouch_verified = TRUE, verification_tier = 1, mfa_setup_required = TRUE;"
    just _db-query "$sql"
    echo ""
    echo "✅ Superuser created successfully!"
    echo "   Email: $email"
    echo "   Username: $username"
    echo ""
    echo "⚠️  MFA Setup Required: The user must complete MFA setup on first login."

# Create a superuser with specified credentials (non-interactive)
create-superuser-batch EMAIL USERNAME PASSWORD: _maybe-db-start
    #!/usr/bin/env bash
    set -euo pipefail
    email="{{EMAIL}}"
    username="{{USERNAME}}"
    password="{{PASSWORD}}"
    if [ ${#password} -lt 8 ]; then
        echo "❌ Password must be at least 8 characters"
        exit 1
    fi
    uuid=$(uuidgen | tr '[:upper:]' '[:lower:]')
    if command -v htpasswd >/dev/null 2>&1; then
        password_hash=$(htpasswd -bnBC 12 "" "$password" | tr -d ':\n' | sed 's/\$2y/\$2a/')
    else
        password_hash=$(python3 -c "import bcrypt; print(bcrypt.hashpw('$password'.encode(), bcrypt.gensalt(12)).decode())")
    fi
    # Generate backup codes for MFA
    backup_codes_hashed="["
    echo "Backup codes:"
    for i in {1..10}; do
        code=$(openssl rand -hex 4 | tr '[:lower:]' '[:upper:]')
        code_formatted="${code:0:4}-${code:4:4}"
        echo "  $i. $code_formatted"
        code_hash=$(htpasswd -bnBC 10 "" "$code" | tr -d ':\n' | sed 's/\$2y/\$2a/')
        if [ $i -gt 1 ]; then
            backup_codes_hashed="$backup_codes_hashed,"
        fi
        backup_codes_hashed="$backup_codes_hashed\"$code_hash\""
    done
    backup_codes_hashed="$backup_codes_hashed]"
    sql="INSERT INTO users (id, email, username, password_hash, email_verified, postcard_verified, vouch_verified, is_superuser, verification_tier, mfa_enabled, mfa_setup_required, mfa_backup_codes) VALUES ('$uuid', '$email', '$username', '$password_hash', TRUE, TRUE, TRUE, TRUE, 1, FALSE, TRUE, '$backup_codes_hashed') ON DUPLICATE KEY UPDATE is_superuser = TRUE, email_verified = TRUE, postcard_verified = TRUE, vouch_verified = TRUE, verification_tier = 1, mfa_setup_required = TRUE;"
    just _db-query "$sql"
    echo "✅ Superuser created: $email (MFA setup required on first login)"

# Promote an existing user to superuser by email
promote-superuser EMAIL: _maybe-db-start
    @echo "👑 Promoting user to superuser: {{EMAIL}}"
    @just _db-query "UPDATE users SET is_superuser = TRUE, email_verified = TRUE, postcard_verified = TRUE, vouch_verified = TRUE, verification_tier = 1 WHERE email = '{{EMAIL}}';"
    @echo "✅ User promoted to superuser"

# Demote a superuser back to regular user by email
demote-superuser EMAIL: _maybe-db-start
    @echo "⬇️  Demoting superuser: {{EMAIL}}"
    @just _db-query "UPDATE users SET is_superuser = FALSE WHERE email = '{{EMAIL}}';"
    @echo "✅ Superuser demoted (verification status preserved)"

# List all superusers
list-superusers: _maybe-db-start
    @echo "👑 Superuser Accounts"
    @echo "===================="
    @just _db-query "SELECT id, email, username, created_at FROM users WHERE is_superuser = TRUE;" | column -t

# List all users
list-users: _maybe-db-start
    @echo "👥 All Users"
    @echo "============"
    @just _db-query "SELECT id, email, username, email_verified, postcard_verified, vouch_verified, is_superuser, created_at FROM users ORDER BY created_at DESC;" | column -t

# Start local database
_maybe-db-start:
    @just db-start

# =============================================================================
# Development Helpers
# =============================================================================

# Get verification code for a user by email
get-verification-code EMAIL: _maybe-db-start
    @echo "🔑 Looking up verification code for: {{EMAIL}}"
    @just _db-query "SELECT vr.verification_code, vr.status, vr.created_at, vr.expires_at FROM verification_requests vr JOIN users u ON vr.user_id = u.id WHERE u.email = '{{EMAIL}}' ORDER BY vr.created_at DESC LIMIT 1;" | column -t

# Get verification code for a user by username
get-verification-code-by-username USERNAME: _maybe-db-start
    @echo "🔑 Looking up verification code for: {{USERNAME}}"
    @just _db-query "SELECT vr.verification_code, vr.status, vr.created_at, vr.expires_at FROM verification_requests vr JOIN users u ON vr.user_id = u.id WHERE u.username = '{{USERNAME}}' ORDER BY vr.created_at DESC LIMIT 1;" | column -t

# List all active verification requests
list-pending-verifications: _maybe-db-start
    @echo "📋 Active Verification Requests"
    @echo "================================"
    @just _db-query "SELECT u.email, u.username, vr.verification_code, vr.status, vr.created_at, vr.expires_at FROM verification_requests vr JOIN users u ON vr.user_id = u.id WHERE vr.status IN ('pending', 'mailed') ORDER BY vr.created_at DESC;" | column -t

# List users waiting for vouch verification
list-pending-vouches: _maybe-db-start
    @echo "🤝 Users Waiting for Vouch Verification"
    @echo "========================================"
    @just _db-query "SELECT u.email, u.username, gr.name as region, (SELECT COUNT(*) FROM vouches v WHERE v.vouched_user_id = u.id AND v.region_id = gr.id) as vouches_received, ur.verified_at as requested_at FROM user_regions ur JOIN users u ON ur.user_id = u.id JOIN geographic_regions gr ON ur.region_id = gr.id WHERE ur.verification_status = 'pending' ORDER BY ur.verified_at DESC;" | column -t

# List all school memberships - which users have joined which schools
list-school-memberships: _maybe-db-start
    @echo "🏫 School Memberships"
    @echo "====================="
    @just _db-query "SELECT u.email, u.username, s.name as school, s.city, s.state, us.verification_status, us.is_admin, COALESCE(d.name, '') as district FROM user_schools us JOIN users u ON us.user_id = u.id JOIN schools s ON us.school_id = s.id LEFT JOIN school_districts d ON s.district_id = d.id ORDER BY s.state, s.name, u.username;"

# List users with pending school verifications - waiting for vouches
list-pending-school-vouches: _maybe-db-start
    @echo "🏫 Pending School Verifications"
    @echo "================================"
    @just _db-query "SELECT u.email, u.username, s.name as school, s.city, s.state, (SELECT COUNT(*) FROM school_vouches sv WHERE sv.vouched_user_id = u.id AND sv.school_id = s.id) as vouches_received, CASE WHEN (SELECT COUNT(*) FROM user_schools us2 WHERE us2.school_id = s.id AND us2.is_admin = TRUE AND us2.verification_status = 'verified') < 3 THEN 3 ELSE 2 END as vouches_needed FROM user_schools us JOIN users u ON us.user_id = u.id JOIN schools s ON us.school_id = s.id WHERE us.verification_status = 'pending' ORDER BY s.state, s.name, u.username;"

# Seed schools from NCES public dataset
seed-schools *ARGS:
    #!/usr/bin/env bash
    set -euo pipefail
    go build -o bin/seed-schools ./cmd/seed-schools

    # Load local .env for DB credentials (Docker dev DB)
    if [ -f .env ]; then
        set -a
        source .env
        set +a
    fi
    ./bin/seed-schools {{ARGS}}

# =============================================================================
# Configuration
# =============================================================================

# Helper: Run a SQL query on local Docker database
# Usage: just _db-query "SELECT * FROM users"
# Usage: just _db-query "SELECT * FROM users" test
_db-query QUERY env="dev":
    #!/usr/bin/env bash
    set -eo pipefail
    if [ "{{env}}" = "test" ]; then
        docker compose exec -T db-test mariadb -u root -ptestroot communityrapidresponse_test -e "{{QUERY}}"
    else
        docker compose exec -T db mariadb -u root -prootpassword communityrapidresponse -e "{{QUERY}}"
    fi

# =============================================================================
# Help
# =============================================================================

# Show detailed help
help:
    @echo "Community Rapid Response - Development Commands"
    @echo "========================================="
    @echo ""
    @echo "Quick Start:"
    @echo "  just init              - First time setup"
    @echo "  just dev               - Start development server (API only)"
    @echo "  just dev-full          - Start development server with web frontend"
    @echo "  just test              - Run all tests"
    @echo ""
    @echo "Web Server:"
    @echo "  just web-start         - Start web server (frontend + API proxy)"
    @echo "  just web-stop          - Stop web server"
    @echo "  just test-env-start    - Start full test environment"
    @echo "  just test-env-stop     - Stop test environment"
    @echo "  just test-env-down     - Tear down test environment completely"
    @echo ""
    @echo "Database (append 'test' for test database, e.g. just db-cli test):"
    @echo "  just db-start                      - Start database containers"
    @echo "  just db-stop                       - Stop database containers"
    @echo "  just db-cli [env]                  - Connect to database CLI"
    @echo "  just db-migrate [env]              - Run database migrations"
    @echo "  just db-migrate-down [env] [N]     - Rollback N migrations (default: 1)"
    @echo "  just db-migrate-status [env]       - Show migration status"
    @echo "  just db-reset [env]                - Reset database (drop and recreate)"
    @echo ""
    @echo "Testing:"
    @echo "  just test              - Run all tests (unit + integration + e2e)"
    @echo "  just test-unit         - Run unit tests only (no database)"
    @echo "  just test-integration  - Run integration tests (mocked services)"
    @echo "  just test-e2e          - Run end-to-end tests (with web server)"
    @echo "  just test-e2e-host     - Run e2e tests on host (requires test-env-start)"
    @echo "  just test-db           - Run database tests"
    @echo "  just test-handlers     - Run handler tests"
    @echo "  just test-coverage     - Run tests with coverage report"
    @echo "  just test-run <name>   - Run a specific test by name"
    @echo ""
    @echo "Superuser Management:"
    @echo "  just create-superuser              - Create superuser (interactive)"
    @echo "  just create-superuser-batch <email> <username> <password>"
    @echo "                                     - Create superuser (non-interactive)"
    @echo "  just promote-superuser <email>     - Promote existing user to superuser"
    @echo "  just demote-superuser <email>      - Demote superuser to regular user"
    @echo "  just list-superusers               - List all superuser accounts"
    @echo "  just list-users                    - List all users"
    @echo ""
    @echo "Non-Docker Database Setup:"
    @echo "  just create-db                     - Create database on local MySQL"
    @echo "  just migrate-local                 - Apply migrations to local MySQL"
    @echo ""
    @echo "Development Helpers:"
    @echo "  just get-verification-code <email> - Get verification code for a user"
    @echo "  just get-verification-code-by-username <username>"
    @echo "                                     - Get verification code by username"
    @echo "  just list-pending-verifications    - List all pending postcard verifications"
    @echo "  just list-pending-vouches          - List users waiting for vouch verification"
    @echo "  just list-school-memberships       - List all school memberships (user/school)"
    @echo "  just list-pending-school-vouches   - List users pending school verification"
    @echo ""
    @just --list
