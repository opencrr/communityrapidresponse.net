# Test Coverage Gaps

A snapshot of where the Go test suite has the thinnest coverage, intended as a
prioritized backlog for future test work. Numbers come from `go test ./... -cover`
and a sweep for `internal/` source files lacking a sibling `*_test.go`.

This document is a survey, not a verdict on code quality — many of the lowest
percentages reflect packages that are inherently integration-test heavy (real
MariaDB, real HTTP handlers wired against a real router) rather than units
worth mocking up.

## Per-package coverage

Captured against `main` at the time of this audit
(commit ancestor: 8bdab47, date: 2026-06-08).

| Package | Coverage | Notes |
|---|---:|---|
| `internal/logging` | 100.0% | Small, pure-Go logging helpers. |
| `internal/config` | 92.1% | Env-var loading; well-covered by table tests. |
| `internal/middleware` | 85.8% | Auth/rate-limit middleware; mostly unit-tested. |
| `internal/services` | 48.1% → **55.4% after this PR** | Mixed: many small services covered, NCES and email mock previously 0%. |
| `internal/sentry` | 42.6% | Optional Sentry wrapper; covers init + a subset of helpers. |
| `cmd/doc-drift` | 54.3% | Tool-package — `main` paths uncovered. |
| `internal/handlers` | 12.5% | HTTP handlers; most exercise the live DB via integration tests. |
| `internal/database` | 0.2% | All meaningful coverage lives in `tests/` integration suite, not in-package unit tests. |
| `internal/models` | no statements | Pure struct definitions. |
| `internal/mocks` | 0.0% | Test doubles — not exercised directly. |
| `internal/testutil` | 0.0% | Test helpers — not exercised directly. |
| `cmd/server` | 0.0% | Process entrypoint; no tests. |
| `cmd/seed-schools` | 0.0% | One-shot tool; no tests. |
| `cmd/tech-debt-classify` | 0.0% | One-shot tool; no tests. |

After the unit tests added in this PR:

| Package | Before | After |
|---|---:|---:|
| `internal/services` | 48.1% | **55.4%** |

(`go test ./internal/services/... -cover` on this branch.)

## Source files with no sibling `*_test.go`

Production code in `internal/` (and `cmd/`) that has no co-located test file. A
missing test file does not always mean "untested" — interfaces and pure model
types have nothing to test, and database-backed files are typically exercised
by `tests/` integration suites rather than a unit test in the same directory.

### Worth targeting (executable, unit-testable)

- `internal/services/nces.go` (314 lines) — HTTP client against the NCES
  ArcGIS API. **Addressed in this PR** via `nces_test.go` (httptest fakes).
- `internal/services/email_mock.go` (51 lines) — `MockEmailService` used by
  development/test paths. **Addressed in this PR** via `email_mock_test.go`.
- `internal/services/email_sendgrid.go` (152 lines) — SendGrid REST client.
  Worth a fake-HTTP unit test similar to `nces_test.go`. **Follow-up.**
- `internal/handlers/router.go` (1285 lines) — route wiring. Coverage would
  come from black-box request tests in the `tests/` suite rather than a pure
  unit test of `router.go` itself. **Follow-up.**
- `cmd/server/main.go` (386 lines) — bootstrapping logic. Splitting wiring
  from `main()` would make it testable. **Follow-up.**

### Tested via integration suites in `tests/`

These files have no unit-test sibling, but `tests/` exercises them end-to-end
against a real MariaDB. They are flagged here only so the gap is visible:

- `internal/database/regions.go` (2158 lines)
- `internal/database/users.go` (1010 lines)
- `internal/database/schools.go` (841 lines — sibling `schools_test.go` does
  exist; called out for completeness only)
- `internal/database/verification.go` (578 lines)
- `internal/database/signal_groups.go` (502 lines)
- `internal/database/meshtastic_channels.go` (448 lines)
- `internal/database/secret_update_proposals.go` (420 lines)
- `internal/database/user_reports.go` (418 lines)
- `internal/database/encrypted_secrets.go` (294 lines)
- `internal/database/school_districts.go` (268 lines)
- `internal/database/school_vouches.go` (212 lines)
- `internal/database/encryption_keys.go` (202 lines)
- `internal/database/errors.go` (22 lines — error sentinels only)

### Nothing to test directly (interfaces / model types / test helpers)

Listed for completeness; no action expected:

- `internal/services/notification_queue.go` — interface only (33 lines, no
  executable code). The interface contract is exercised by callers of
  `NotificationService` in `notification_test.go`, and the production
  implementation lives in `internal/database/notification_queue.go` which has
  its own `_test.go`.
- `internal/services/interfaces.go` — service interface declarations.
- `internal/services/testdata/osm/fixtures.go` — fixture loader for tests.
- `internal/mocks/services.go` — hand-written mock services.
- `internal/testutil/testutil.go` — shared test setup helpers.
- All `internal/models/*.go` — pure struct definitions (`no statements`
  coverage).

## Recommended priorities

Ranked by **effort vs. risk reduction**, smallest/highest-impact first:

1. **`internal/services/email_sendgrid.go`** — copy the `httptest`-based
   pattern established in `nces_test.go` to cover send/error paths.
   *(Estimated: < 1 day. Pure unit test, no external deps.)*
2. **`internal/handlers/*` (12.5% → higher)** — add `httptest.Server`-style
   handler tests for the highest-risk endpoints first:
   - `/api/v1/auth/login` and MFA paths (security-critical).
   - `/api/v1/verification/*` (security-critical, addresses).
   - `/api/v1/blocklist-proposals/*` (consensus governance).
   *(Estimated: 2–4 days. Requires a test DB or careful mocking of the
   database layer.)*
3. **`internal/database/*` (0.2% in-package)** — accept that meaningful
   coverage will come from the `tests/` integration suite rather than
   in-package unit tests. Track that suite's coverage separately and ensure
   new database files always get a corresponding integration test added in
   `tests/`.
4. **`cmd/server/main.go`** — extract a `run(ctx, cfg) error` function so the
   process entrypoint can be tested without spawning the binary.

## Methodology

- Per-package percentages: `go test ./... -cover` from a clean tree.
- Untested file list: `find internal cmd -name '*.go' -not -name '*_test.go'`
  cross-referenced with the presence of `<basename>_test.go` in the same
  directory.
- "Addressed in this PR" entries reflect tests added on branch
  `nightshift/test-gap-finder`.
