# Contributing to Community Rapid Response

Thanks for your interest in contributing. This guide covers how to set up a local
development environment, run tests, and submit changes. For project background and
high-level architecture, see [README.md](README.md) and [DESIGN.md](DESIGN.md).

## Prerequisites

- [Go](https://golang.org/dl/) 1.25.9 (matches `go.mod`)
- [Docker](https://docs.docker.com/get-docker/) and Docker Compose
- [just](https://github.com/casey/just) command runner
- Git

Optional but recommended:
- [air](https://github.com/air-verse/air) for hot reload (`just dev-local` will install it on demand)
- A Mapbox account and a Postgrid/Lob account for end-to-end verification flows

## Local Setup

```bash
git clone https://github.com/opencrr/communityrapidresponse.net.git
cd communityrapidresponse.net

# First-time setup: copies .env.example to .env, installs Go deps,
# and starts the MariaDB container.
just init

# Start the development server with hot reload (in Docker).
just dev
```

The HTTP API listens on `http://localhost:8080`. See [README.md](README.md#environment-variables)
for the full list of environment variables and [.env.example](.env.example) for sane
defaults.

### Database Migrations

Migrations are idempotent and tracked in the `schema_migrations` table. Each migration
is a paired `NNN_description.up.sql` / `NNN_description.down.sql` file under
[`migrations/`](migrations/).

```bash
just db-migrate           # apply pending migrations to the dev database
just db-migrate test      # apply to the test database
just db-migrate-status    # show applied/pending migrations
just db-migrate-down 1    # roll back the most recent migration
```

When changing the schema, add a new pair of migration files; do not edit applied ones.

### Seeding Schools

NCES school data is bulk-loaded via a separate command:

```bash
just seed-schools         # or `go run ./cmd/seed-schools`
```

## Running Tests

Tests are designed to run inside Docker so they share the dev database setup.

```bash
just test                 # full test suite
just test-unit            # unit tests only (no database)
just test-integration     # integration tests
just test-handlers        # HTTP handler tests
just test-db              # database/repository tests
just test-coverage        # generate a coverage report
just test-run TestName    # run a single test by name
```

If you change handler or service behavior, add or update the corresponding tests
under `internal/handlers/*_test.go`, `internal/services/*_test.go`, or `tests/`.

## Code Style

- Run `just fmt` before committing — it wraps `gofmt`/`goimports`.
- Run `just lint` — it invokes `golangci-lint` using the rules in [`.golangci.yml`](.golangci.yml).
- Keep packages small and focused. Internal layout:
  - `cmd/`        — binaries (`server`, `seed-schools`)
  - `internal/config`        — env-driven configuration
  - `internal/database`      — repositories and DB connection
  - `internal/handlers`      — HTTP handlers and routing
  - `internal/middleware`    — auth, rate-limit, CORS, CSRF, security headers
  - `internal/models`        — domain types
  - `internal/services`      — external integrations (Mapbox, Postgrid/Lob, SendGrid, SMTP, MFA)
  - `internal/logging`, `internal/sentry` — observability
- Match the existing error style — handlers should produce structured JSON errors via the
  shared helpers in `internal/handlers/helpers.go`.

## Branching, Commits, and Pull Requests

1. Branch off `main`. Use descriptive branch names, for example:
   - `feat/<topic>` for new features
   - `fix/<topic>` for bug fixes
   - `docs/<topic>` for documentation
   - `chore/<topic>` for tooling, deps, refactors
2. Keep commits focused and write a short, imperative subject line. Reference an
   issue or PR number where helpful.
3. Run `just fmt`, `just lint`, and `just test` locally before pushing.
4. Open a pull request against `main` with:
   - A summary of the change and its motivation.
   - Notes on schema changes, breaking changes, or new env vars.
   - A test plan (commands run, scenarios exercised).
5. Address review feedback in additional commits — squash on merge.

## Privacy and Security Expectations

This project takes a hard line on address storage: addresses are processed in memory
only and never written to the database. When contributing code that touches
verification, geocoding, or mailing flows, do not introduce persistence of street
addresses, apartment numbers, or precise GPS coordinates. See [SECURITY.md](SECURITY.md)
for the responsible disclosure process and the full security posture.

## Useful Documentation

- [README.md](README.md) — project overview, quick start, API summary
- [DESIGN.md](DESIGN.md) — authoritative design spec
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — system architecture overview
- [docs/API.md](docs/API.md) — REST API reference
- [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) — deployment and operations
- [CLAUDE.md](CLAUDE.md) — quick-reference for AI assistants
