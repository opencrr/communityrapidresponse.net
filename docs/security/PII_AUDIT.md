# PII Exposure Audit

Repository-wide scan for potential PII exposure across five categories:
hardcoded PII literals, PII in logs/error messages, plaintext secrets in
env/config/CI files, unencrypted PII storage, and `.gitignore` coverage gaps.

- **Scan date:** 2026-06-06
- **Scope included:** `cmd/`, `internal/`, `migrations/`, `tests/`, `static/`,
  `templates/`, `docker/`, `.github/workflows/`, `.env.example`,
  `docker-compose.yml`, `Dockerfile`, `justfile`, `nginx*.conf`, `*.md` docs.
- **Scope excluded:** `vendor/`, `go.sum`, `bin/`, `dist/`, `tmp/`,
  `static/dist/`, and third-party OSM fixtures (which are public geo data).
- **Cross-referenced against:** the project's documented Zero Address Storage
  policy and AES-256-GCM TOTP-secret encryption requirement in `CLAUDE.md`.

A companion summary already exists at
[`docs/security/pii-scan-summary.md`](./pii-scan-summary.md); this audit
re-runs the checks against current `main` and produces the structured,
per-finding output requested by the audit spec.

## Findings

### 1. Audit log persists user email on registration

- **file:** `internal/handlers/auth.go`
- **line:** 225–228
- **category:** unencrypted-storage
- **severity:** high
- **detail:** On successful registration, the handler writes
  `{"username": user.Username, "email": user.Email}` to `audit_log.details`
  (plaintext `JSON` column per `migrations/001_initial_schema.up.sql:240`).
  Audit records are kept for 90 days. A DB breach therefore exposes every
  user's email tied to a timestamp and IP — a PII linkage that has no
  operational need (the `user_id` foreign key already correlates the event).
- **recommendation:** Drop `email` (and `username`, which is also user-chosen)
  from this `details` map. If correlation across the audit table is needed,
  store a peppered SHA-256 hash of the email rather than the cleartext value.

### 2. Audit log persists attempted email on failed login

- **file:** `internal/handlers/auth.go`
- **line:** 282–285
- **category:** unencrypted-storage
- **severity:** high
- **detail:** When `GetByEmailOrUsername` returns `ErrUserNotFound`, the
  handler writes `{"email": req.Email, "reason": "user_not_found"}` to
  `audit_log.details`. This persists arbitrary attacker-supplied strings —
  including valid email addresses from credential-stuffing lists — in a
  long-lived table, enabling account-enumeration and harvesting from a
  later DB breach.
- **recommendation:** Either omit `email` entirely (the `reason` field is
  enough for forensics) or hash it with a server-side pepper before
  persisting. The matching `invalid_password` branch at line 340–342
  already does the right thing (only `reason`, no email) — apply the same
  pattern to the `user_not_found` branch.

### 3. `verification_code` stored in plaintext

- **file:** `migrations/001_initial_schema.up.sql`
- **line:** 72, 82
- **category:** unencrypted-storage
- **severity:** medium
- **detail:** `verification_requests.verification_code` is a `VARCHAR(32)`
  with a `UNIQUE` constraint and a direct lookup index (`idx_verification_code`).
  Codes are written and queried in cleartext (`internal/database/verification.go`
  lines 47, 68, 208, 277). The codes are short-lived (~30-day expiry), but
  a database snapshot still exposes every pending postcard code — and any
  attacker who reads it can complete verification for the corresponding
  user without ever receiving the postcard.
- **recommendation:** Hash the code at rest (HMAC-SHA-256 with a server-side
  pepper is sufficient for a short opaque token and preserves O(1) lookup
  via `WHERE verification_code_hash = ?`). Migration would: (a) add
  `verification_code_hash CHAR(64)`, (b) backfill from existing rows,
  (c) drop the plaintext column and rename the index.

### 4. MFA backup codes use bcrypt cost 10 while passwords use 12

- **file:** `internal/services/mfa.go`
- **line:** 163
- **category:** unencrypted-storage
- **severity:** medium
- **detail:** Backup codes are hashed with `bcrypt.DefaultCost` (10) at
  line 163, while user passwords use cost 12 at `internal/handlers/auth.go`
  lines 193, 706, 908. Backup codes are functionally equivalent to a
  password for account recovery and are longer-lived than session
  passwords (rotated only on regeneration), so they should not be cheaper
  to crack offline than the primary password.
- **recommendation:** Replace `bcrypt.DefaultCost` with `12` to match the
  password hashing cost. Existing hashes do not need migration —
  `bcrypt.CompareHashAndPassword` handles per-hash cost transparently, and
  cost-12 hashes will be written on the next backup-code regeneration.

### 5. `docker-compose.yml` falls back to documented-weak `JWT_SECRET` and `MFA_ENCRYPTION_KEY`

- **file:** `docker-compose.yml`
- **line:** 72, 102
- **category:** env-secret
- **severity:** medium
- **detail:** The `app` service interpolates
  `JWT_SECRET=${JWT_SECRET:-development_secret_key_change_in_production_at_least_32_chars}`
  and `MFA_ENCRYPTION_KEY=${MFA_ENCRYPTION_KEY:-01234567890123456789012345678901}`.
  Production-mode validation in `internal/config/config.go:484-494` does
  reject these exact strings, so a misconfigured production boot fails
  loudly — but the `:-…` fallback still ships the weak default into the
  container if anyone runs compose with `ENVIRONMENT=staging` set
  externally, or with `ENVIRONMENT` momentarily unset during shell
  expansion of an orchestrator wrapper. Defense-in-depth dictates that
  these values should never have a default.
- **recommendation:** Remove the `:-…` defaults so unset vars cause the
  container to fail at startup. Keep the `.env.example` documentation of
  the placeholder values intact (they remain present in `.env.example` for
  the *development* path, not in compose itself).

### 6. CI workflow hardcodes test DB credentials inline

- **file:** `.github/workflows/test.yml`
- **line:** 29, 53, 65, 82, 91, 100, 109, 118
- **category:** env-secret
- **severity:** medium
- **detail:** `MYSQL_ROOT_PASSWORD: testroot` is set as a literal in the
  workflow service block, and the same value is embedded inline in every
  test-job step (`-ptestroot`, `TEST_DB_PASSWORD: testroot`). These are
  test-only credentials for an ephemeral runner-local MariaDB, so the
  immediate exposure is low — but committing a credential literal to a
  workflow normalizes the pattern. The first time someone needs a real
  per-environment secret (Lob staging key, SendGrid key, etc.) for a
  workflow, the path of least resistance is to add it the same way.
- **recommendation:** Move the value to a workflow-level `env:` block (or
  to repository Actions secrets if it grows beyond test use), and
  reference it as `${{ secrets.TEST_DB_PASSWORD }}` / `${{ env.TEST_DB_PASSWORD }}`.
  This establishes the right pattern before any production credential is
  added to the same file.

### 7. `docker-compose.yml` ships predictable dev DB credentials

- **file:** `docker-compose.yml`
- **line:** 8, 11, 33, 70, 141, 214
- **category:** env-secret
- **severity:** low
- **detail:** `rootpassword`, `devpassword`, and `testroot` are the
  defaults baked into the `db`, `db-test`, `app`, `test`, and `app-test`
  services. The MariaDB container binds to a host port (`3306` by default
  via `${DB_PORT:-3306}:3306`), so a developer who runs `docker compose up`
  on a multi-tenant host briefly exposes a known-credential database.
  This is well-understood behavior for a dev-only compose file but is
  worth a startup guard for misuse.
- **recommendation:** Add a small guard to the `app` entrypoint that
  refuses to boot if `ENVIRONMENT != development` AND the DB password
  equals `devpassword` or `rootpassword`. This mirrors the existing
  `JWT_SECRET` validator at `internal/config/config.go:484-494`.

### 8. `README.md` uses an ambiguous "123 Main St" placeholder

- **file:** `README.md`
- **line:** 127
- **category:** hardcoded-pii
- **severity:** low
- **detail:** `LOB_RETURN_ADDRESS=123 Main St` appears in the example
  configuration block. "123 Main St" is widely used as a placeholder, but
  it does also exist as a real address in most US cities. For a
  privacy-first project that documents a Zero Address Storage policy, the
  example reads more crisply with an unambiguously fictional value.
  Also present in `DESIGN.md:138`, `DESIGN.md:2285`, `DESIGN.md:2347`,
  and the vouch/verification UI placeholders
  (`static/js/pages/vouch.js:263`, `static/js/pages/verification.js:186`).
- **recommendation:** Replace user-facing placeholders with
  `123 Example St` (or similar `.example`-style text) in the README, the
  design doc, and the two form placeholders. Keep test fixtures as-is
  (they exist to exercise the real-address validation path).

### 9. Test fixtures use realistic-looking residential and institutional addresses

- **file:** `internal/services/lob_test.go`, `internal/services/mapbox_geocode_test.go`,
  `internal/services/testdata/lob/residential_address.json`,
  `internal/services/testdata/mapbox/geocode_*.json`
- **line:** various
- **category:** hardcoded-pii
- **severity:** medium
- **detail:** Fixtures embed plausibly real US addresses:
  `100 Main Street, Boston MA 02129` (residential_address.json line 7),
  `1600 Pennsylvania Avenue NW, Washington DC` (lob_test.go line 710 — the
  White House, public),
  `5201 Great America Pkwy, Santa Clara CA` (lob_test.go line 767 — a known
  CMRA test address),
  `123 Bedford Avenue, Brooklyn, NY 11211` (mapbox_geocode_test.go line 127),
  `100 Pike Street, Seattle, WA 98101` (mapbox_geocode_test.go line 190),
  `100 Market Street, San Francisco, CA 94105` (mapbox_geocode_test.go line 61).
  None of these are private residences of identifiable individuals — they
  are public landmarks or generic numeric-prefix street names — so the
  PII risk is low. Flagged as medium per task guidance for "fixtures with
  realistic-looking PII," primarily so future fixture additions don't
  drift toward genuine residences.
- **recommendation:** Document a "fixture address policy" in
  `tests/CONTRIBUTING.md` or similar: address fixtures must be either
  (a) clearly fictional (`123 Example St`), (b) a well-known public
  landmark, or (c) a known commercial test address (e.g. Lob's published
  CMRA sample). No changes required to existing fixtures.

### 10. `schools.street_address` stored plaintext

- **file:** `migrations/019_schools.up.sql`
- **line:** 25
- **category:** unencrypted-storage
- **severity:** low
- **detail:** `street_address VARCHAR(255) NULL` is populated by the
  NCES-data seed job (`cmd/seed-schools/main.go:178`). These are
  public-record addresses of public institutions — explicitly distinct
  from the user-residence "Zero Address Storage" policy in `CLAUDE.md`.
  Listed here only so a future reviewer can confirm that user-supplied
  residence data never lands in this column.
- **recommendation:** No change needed. Optionally add a `-- public NCES
  institutional data; not user PII` comment on the migration column to
  pre-empt future reviewers.

### 11. Organizational contact addresses in static frontend and config

- **file:** `static/js/pages/privacy.js`, `static/js/pages/help.js`,
  `static/js/pages/terms.js`, `.env.example`, `docker-compose.yml`,
  `internal/config/config.go`, `SECURITY.md`
- **line:** various (e.g. `static/js/pages/privacy.js:29`, `.env.example:174`,
  `docker-compose.yml:107`, `internal/config/config.go:305`,
  `SECURITY.md:21`)
- **category:** hardcoded-pii
- **severity:** low
- **detail:** `noreply@communityrapidresponse.net`,
  `help@communityrapidresponse.net`, and
  `security@communityrapidresponse.net` are embedded as user-facing
  support/contact strings. These are intentional organizational
  addresses, not PII belonging to an identifiable individual.
- **recommendation:** No change. Listed for completeness so the scan is
  reproducible.

### 12. `DEPLOYMENT.md` uses `admin@example.com` placeholder

- **file:** `docs/DEPLOYMENT.md`
- **line:** 221
- **category:** hardcoded-pii
- **severity:** low
- **detail:** `just promote-superuser admin@example.com` is documentation
  of an admin-bootstrap command. `example.com` is the IANA-reserved
  documentation domain; this is exactly the right placeholder.
- **recommendation:** No change. Listed for completeness.

### 13. `.gitignore` coverage is comprehensive — no gaps found

- **file:** `.gitignore`
- **line:** entire file
- **category:** gitignore-gap
- **severity:** low (informational)
- **detail:** `.gitignore` already covers all sensitive-file categories
  enumerated in the task brief:
  - env files: `.env`, `.env.*` with `!.env.example` negation (lines 27–29)
  - keys/certs: `*.pem`, `*.key`, `*.crt`, `*.pfx`, `*.p12`, `*.p8`,
    `*.jks` (lines 72–76, 95–96)
  - credentials: `credentials.*`, `secrets.*` (lines 79–80)
  - data dumps: `*.csv`, `*.xlsx`, `*.xls`, `*.sql.gz`, `*.sql.zip`,
    `*.dump`, `*.bak`, `*.sqlite`, `*.sqlite3`, `*.db` (lines 83–92)
  - prior scan artifacts: `SECURITY_AUDIT.md`, `PII_SCAN_REPORT.md`
    (lines 68–69)
  A tracked-file check (`git ls-files | grep -iE …`) confirms zero
  matches for any of the above patterns except `.env.example`, which is
  legitimately allowed by the explicit negation. Migration `*.sql` files
  remain tracked (they are schema, not data dumps) — the dump-extension
  patterns are scoped tightly enough to avoid catching them.
- **recommendation:** No change. The committed PII audit report
  (`PII_AUDIT.md`) itself contains no real PII — it cites only line
  numbers and references the recommendations, mirroring the existing
  `docs/security/pii-scan-summary.md` pattern. The pre-existing
  `.gitignore` entry for `PII_SCAN_REPORT.md` covers the raw-scan
  artifact pattern if one is ever generated.

## Summary

### Counts by category

| Category               | Findings |
|------------------------|----------|
| unencrypted-storage    | 4        |
| env-secret             | 3        |
| hardcoded-pii          | 4        |
| pii-in-logs            | 0        |
| gitignore-gap          | 0 (1 informational item, no gaps) |
| **Total**              | **12 findings + 1 informational** |

### Counts by severity

| Severity | Findings |
|----------|----------|
| critical | 0        |
| high     | 2        |
| medium   | 4        |
| low      | 7        |

### Verified good practices (not findings — confirmed in this scan)

- Passwords stored via bcrypt cost 12
  (`internal/handlers/auth.go:193`, `:706`, `:908`).
- TOTP secrets encrypted with AES-256-GCM before storage
  (`internal/services/mfa.go:58-125`, comment on
  `migrations/003_add_mfa.up.sql:7`).
- Password reset tokens stored as SHA-256 hashes
  (`migrations/018_password_reset_tokens.up.sql:8`).
- Verified-address fingerprints stored as SHA-256 hash, never the address
  itself (`migrations/016_blacklist_proposal_expires.up.sql:22`,
  `internal/models/user.go:36`).
- Signal-group invite links stored encrypted with per-user wrapped DEKs
  (`migrations/026_encrypted_secrets.up.sql`), with the plaintext
  `invite_link` column explicitly dropped at line 84.
- All email service backends use a `redactEmail()` helper before logging
  recipient addresses
  (`internal/services/email.go:16`, `email_mock.go:40,46`,
  `email_sendgrid.go:58,104`, `email_smtp.go:68,116,166`).
- Production-mode config validation rejects the documented weak defaults
  for `JWT_SECRET`, `MFA_ENCRYPTION_KEY`, `DB_PASSWORD`, and `JWT_ISSUER`
  (`internal/config/config.go:484-494`).
- `nginx.conf` / `nginx.test.conf` deny direct browser access to `.env`,
  `.sql`, `.bak`, `.config` files.
- No `.env`, `*.pem`, `*.key`, `*.crt`, `credentials.*`, `secrets.*`, or
  data-dump files are currently tracked in git (verified via
  `git ls-files`).
- No PII-bearing logging statements found in production code paths;
  `slog`/`fmt.Errorf` calls that include user fields are limited to the
  `user_id` UUID, never raw email/name/address/phone.

### Suggested follow-up tickets (prioritized)

1. **High** — Remove `email` from the `user_registered` and
   `user_login_failed (user_not_found)` audit detail payloads
   (`internal/handlers/auth.go:225-228`, `:282-285`). Replace with a
   peppered SHA-256 hash if cross-event correlation is needed.
2. **Medium** — Hash `verification_requests.verification_code` at rest
   (HMAC-SHA-256 with a server-side pepper). New migration + update of
   the four lookup sites in `internal/database/verification.go`.
3. **Medium** — Raise MFA backup-code bcrypt cost from `DefaultCost` (10)
   to `12` in `internal/services/mfa.go:163`.
4. **Medium** — Remove `JWT_SECRET` and `MFA_ENCRYPTION_KEY` fallback
   defaults from `docker-compose.yml` lines 72 and 102.
5. **Medium** — Move CI test DB credentials to a workflow `env:` block or
   Actions secrets in `.github/workflows/test.yml`.
6. **Low** — Replace `123 Main St` placeholders in `README.md`,
   `DESIGN.md`, and the two UI form placeholders with `123 Example St`.
7. **Low** — Add a startup guard refusing to boot the `app` service with
   `devpassword`/`rootpassword` when `ENVIRONMENT != development`.
