# PII Exposure Scan — Summary

This document is a sanitized summary of an automated PII exposure scan of the
repository. The full raw report (with line-level detail) is kept out of git
via `PII_SCAN_REPORT.md` in `.gitignore`. This summary captures the actionable
findings and recommended remediations.

## Scope

- Included: `cmd/`, `internal/`, `migrations/`, `tests/`, `static/`,
  `templates/`, `docker/`, `docker-compose.yml`, `Dockerfile`, `justfile`,
  `nginx*.conf`, `.env.example`, `.github/`, `README.md`.
- Excluded: `vendor/`, `go.sum`, `bin/`, `dist/`, `tmp/`, `static/dist/`,
  generated assets, and third-party fixtures that contain only public geo data.

## Categories Audited

1. Hardcoded PII patterns (emails, phones, SSNs, CCs, addresses, names)
2. PII in logs / error messages
3. Plaintext secrets in env/config/CI files
4. Unencrypted storage of PII in DB schema / Go models
5. `.gitignore` coverage for sensitive file types

## Findings

### Critical — 0

No real personal data, no committed production secrets, and no plaintext
passwords were found in tracked files.

### High — 2

- **Audit log captures email on registration and failed login**
  (`internal/handlers/auth.go` around the registration and login-failed paths).
  Persisting the attempted email in `audit_log.details` on failed login enables
  account enumeration from a database breach. *Recommendation:* drop `email`
  from these audit detail payloads (or store a SHA-256 hash for correlation).
- **`docker-compose.yml` falls back to a weak default for `JWT_SECRET` and
  `MFA_ENCRYPTION_KEY`** if the variables are unset. The application's config
  validator does reject these strings in production, but the compose-level
  fallback bypasses any layer that reads compose env vars without going through
  the validator. *Recommendation:* remove the `:-…` defaults so an unset value
  fails loudly.

### Medium — 4

- **Verification codes stored plaintext**
  (`migrations/001_initial_schema.up.sql` — `verification_requests.verification_code`).
  Codes are short-lived (30 days) but a DB breach exposes any pending codes
  and the index allows direct lookup. *Recommendation:* hash the code (bcrypt
  cost 10–12 or HMAC-SHA-256 with a server-side pepper) before insert, and
  query by hash.
- **MFA backup codes hashed with `bcrypt.DefaultCost` (10)** while
  passwords use cost 12 (`internal/services/mfa.go`). Backup codes are
  longer-lived than passwords, so the cost should match or exceed.
  *Recommendation:* raise to cost 12.
- **CI workflow hardcodes test DB credentials inline**
  (`.github/workflows/test.yml`). They are test-only secrets, but moving them
  to repository Actions secrets establishes the right contract before any
  production credential is ever added to the same file.
- **`docker-compose.yml` dev-DB defaults `rootpassword` / `devpassword`** are
  fine for local-only use but are foot-guns if compose is ever pointed at a
  shared/remote DB. *Recommendation:* keep the defaults but add a startup
  guard that refuses to boot the `app` service with these values when
  `ENVIRONMENT != development`.

### Low / informational

- `.env.example` and several frontend files (`static/js/pages/about.js`,
  `static/js/pages/help.js`, `static/js/pages/privacy.js`,
  `static/js/pages/terms.js`, `static/js/components/footer.js`,
  `docker-compose.yml`) embed organizational contact addresses
  (`help@…`, `admin@…`, `noreply@…`). These are intentional, user-facing
  support/contact addresses, not PII — listed here only for awareness.
- `schools.street_address` (`migrations/019_schools.up.sql`) stores institutional
  addresses sourced from the NCES public dataset. This is a public-record
  address of a public institution, distinct from the user-residence "Zero
  Address Storage" policy in `CLAUDE.md`.
- `README.md` contains a `123 Main St` placeholder in configuration examples.
  Consider replacing with an obviously fictional value (e.g. `123 Example St`).

## `.gitignore` Coverage

The existing `.gitignore` already covered `*.pem`, `*.key`, `*.crt`, `*.pfx`,
`*.p12`, `credentials.*`, `secrets.*`, `*.csv`, `*.xlsx`, `*.xls`,
`SECURITY_AUDIT.md`, and `PII_SCAN_REPORT.md`. Gaps closed in the same change
as this summary:

- `.env.*` (with `!.env.example` negation) — previously only `.env.local` and
  `.env.*.local` were ignored, so `.env.production` / `.env.staging` would
  have been committed if created.
- Database dump and local-DB extensions: `*.sql.gz`, `*.sql.zip`, `*.dump`,
  `*.bak`, `*.sqlite`, `*.sqlite3`, `*.db`.
- Additional key formats: `*.p8`, `*.jks`.

Migration `.sql` files remain tracked (they are schema, not data dumps); the
patterns above target dump/backup artifacts.

## Verified Good Practices

- Passwords stored via bcrypt cost 12.
- TOTP secrets encrypted with AES-256-GCM before storage.
- Password reset tokens stored as SHA-256 hashes (raw token only in email).
- Email service uses a `redactEmail` helper when logging recipient addresses.
- `nginx.conf` / `nginx.test.conf` deny direct access to `.env`, `.sql`,
  `.bak`, `.config` files.
- Production-mode config validator rejects the documented weak `JWT_SECRET`
  and `MFA_ENCRYPTION_KEY` defaults.
- No `.env`, `*.pem`, `*.key`, `*.crt`, `credentials.*`, or `secrets.*` files
  are currently tracked.

## Suggested Follow-up Tickets

1. Stop persisting `email` in `audit_log.details` for failed-login and
   user-registered events; replace with a hashed correlation token.
2. Hash `verification_code` at rest in `verification_requests`.
3. Raise MFA backup-code bcrypt cost to 12.
4. Remove fallback defaults for `JWT_SECRET` and `MFA_ENCRYPTION_KEY` in
   `docker-compose.yml`.
5. Move CI test DB credentials to GitHub Actions secrets.
