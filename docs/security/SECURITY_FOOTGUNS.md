# Security Foot-Gun Audit

Status: 2026-06-05 — security/footgun-anti-patterns branch
Scope: `cmd/`, `internal/`, `migrations/`, `static/`, `templates/`, `go.mod`
Method: targeted grep + manual code review for OWASP-style anti-patterns,
cross-checked against the project-specific invariants in `CLAUDE.md`
(zero address storage, MFA token typing, superuser-only admin endpoints,
3-admin consensus, no secrets in email).

This report enumerates findings by severity, the low-risk fixes applied in
the same PR, and the items that remain as documented TODOs.

---

## TL;DR

- **No Critical or High findings.** The codebase has well-implemented JWT
  token-type validation, parameterised SQL, AES-256-GCM at-rest encryption,
  CSRF double-submit, narrowed CORS, body-size limits, and per-action auth
  rate limits.
- **Fixed in this PR (low risk):**
  - `IdleTimeout` set on the HTTP server (Slowloris / keep-alive resource
    exhaustion mitigation).
  - `ReadHeaderTimeout` set explicitly (defense-in-depth; not relying on the
    `ReadTimeout` fallback).
  - SMTP `Send()` no longer logs the email `Subject` verbatim when the
    backend is disabled (could correlate users to reset/verification flows).
  - Admin user-search `page` cap reduced from 1,000,000 to 10,000 (large
    page numbers still issue expensive `LIMIT/OFFSET` against the users
    table).
  - User-Agent truncation now emits a `slog.Debug` so the audit log shows
    when a value was clipped at 512 bytes.
- **Tracked TODOs (documented but not changed in this PR):**
  - `golang.org/x/crypto` is pinned to `v0.51.0`; upgrade to the latest
    `v0.x` line during the next dependency sweep.
  - `json.NewDecoder(...).Decode()` is used without
    `DisallowUnknownFields()`. Consider enabling it on the auth/admin
    write paths so typos don't silently no-op.
  - Audit-log `Query()` filter assembly uses `fmt.Sprintf` to splice
    `placeholders` (safe today — only `?` placeholders are joined, values
    flow through `args`). Add a regression test to make sure no
    user-controlled string ever lands in `conditions`.

---

## Methodology Notes

For each category below, the audit ran a focused grep + read pass:

- SQL injection: every `fmt.Sprintf` near `Query`/`Exec`, every string
  concatenation that ends up in a SQL statement.
- Weak crypto: `crypto/md5`, `crypto/sha1`, `crypto/des`, `crypto/rc4`,
  `math/rand` in token / nonce / ID paths.
- Hardcoded secrets: literal API keys / passwords / JWT secrets in `.go`
  files (the dev defaults in `.env.example` are intentional and gated by
  `Config.Validate()` for production / staging).
- Path traversal, SSRF, command injection: `filepath.Join`,
  `http.ServeFile`, `http.Get`, `exec.Command` against any user-controlled
  input.
- Template/XSS: `template.HTML` / `template.JS` / `template.URL` casts.
- Auth/Authz/IDOR: `r.mux.HandleFunc` calls that bypass `r.authenticated`,
  endpoints that read a resource ID from path without verifying ownership.
- Cookie/JWT: `http.SetCookie` invocations with `Secure`, `HttpOnly`,
  `SameSite`; JWT signing method enforcement.
- TLS/HTTP: `InsecureSkipVerify`, `http.DefaultClient` without timeouts.
- Context misuse: `context.Background()` inside HTTP handlers.
- Concurrency: `sync.Mutex` copied by value, map access without locks.
- Sensitive data in logs: `slog.*` calls referencing `address`, `street`,
  `coordinates`, `secret`, `token`, `password`, `invite_link`,
  `verification_code`, etc.
- Open redirects: `http.Redirect` with user-controlled targets.
- CSRF/CORS: GET that mutates state, wildcard origin + credentials.
- Rate limits: every endpoint documented in CLAUDE.md as rate-limited.
- Parsing footguns: `strconv.Atoi` without bounds, JSON decoding without
  size limits / unknown-field rejection.
- Dependencies: `go.mod` versions vs. known-stale crypto libs.

---

## Findings

### Critical / High

None identified.

### Medium

#### M1 — `golang.org/x/crypto` is pinned to v0.51.0

- **File:** `go.mod:12`
- **Category:** Vulnerable dependencies
- **Description:** The project pins `golang.org/x/crypto v0.51.0`. This is
  not currently known-CVE for any code path in use here (bcrypt and
  AES-GCM via `crypto/aes` are stdlib), but `golang.org/x/crypto` is the
  most security-sensitive third-party module in the tree. The version
  should track upstream within a minor-release window.
- **Remediation (deferred):** `go get -u golang.org/x/crypto` during the
  next dependency sweep, run `govulncheck`, ship in its own PR so the
  upgrade is reviewable on its own.

#### M2 — HTTP server has no `IdleTimeout` / `ReadHeaderTimeout`

- **File:** `cmd/server/main.go:353-358` (pre-fix)
- **Category:** TLS / HTTP misconfiguration (Slowloris vector)
- **Description:** Only `ReadTimeout` and `WriteTimeout` were set on
  `http.Server`. With keep-alive enabled (Go default), an attacker can
  hold idle connections open until socket exhaustion. Without an
  explicit `ReadHeaderTimeout`, slow-header attacks rely on the
  `ReadTimeout` fallback only.
- **Fix applied in this PR:** Added `IdleTimeout: 120s` and
  `ReadHeaderTimeout: 10s` on the HTTP server.

#### M3 — Admin user-search allows `page` up to 1,000,000

- **File:** `internal/handlers/admin.go:71-78` (pre-fix)
- **Category:** Denial of service / parsing footgun
- **Description:** `ListUsers` (superuser only) accepts `page` values up
  to one million. With `limit=100` that's an `OFFSET 99,999,900` against
  the `users` table — every paged request scans and discards the entire
  table prefix. Superuser-only mitigates the blast radius, but a
  compromised admin or a buggy admin UI loop can easily hit this.
- **Fix applied in this PR:** `page` capped at 10,000 (≤1M users at
  `limit=100` is still reachable; large exports should use the audit-log
  export endpoint or be added as a dedicated export route).

### Low

#### L1 — SMTP backend logs subject line when sends are disabled

- **File:** `internal/services/email_smtp.go:68` (pre-fix)
- **Category:** Sensitive data in logs
- **Description:** When SMTP is disabled (`EMAIL_ENABLED=false` /
  development), `Send()` logs the email subject verbatim alongside the
  redacted recipient. Subjects from this codebase include
  `"Verify your email address - Community Rapid Response"` and
  password-reset subjects, which makes it trivial to grep logs for
  which users are mid-reset or mid-verification.
- **Fix applied in this PR:** dropped `subject` from the disabled-mode log
  line. Recipient is still redacted via `redactEmail()`.

#### L2 — User-Agent truncation is silent

- **File:** `internal/database/audit.go:44-48` (pre-fix)
- **Category:** Audit log fidelity
- **Description:** Audit log writes truncate `User-Agent` to 512 bytes
  with no signal that truncation happened. A crafted client that pads
  its UA past the limit gets a quietly clipped trail. Low impact (UA is
  attacker-controlled anyway) but trivial to fix.
- **Fix applied in this PR:** `slog.Debug` emitted when truncation
  happens, including the original length.

#### L3 — `fmt.Sprintf` used to assemble audit-log `WHERE` clause

- **File:** `internal/database/audit.go:104, 138, 158`
- **Category:** SQL injection (negative — currently safe)
- **Description:** The audit-log query builder uses `fmt.Sprintf` to
  splice a join of `?` placeholders into the WHERE clause and to splice
  the WHERE clause string into the count/select statements. This is
  **safe today** — only literal `?` strings and a fixed-length WHERE
  fragment are interpolated, and the values flow through `args` to
  `QueryRowContext` / `QueryContext`. The pattern is brittle, though:
  any future contributor who appends a user-controlled string into
  `conditions` (e.g., a dynamic ORDER BY) opens an injection.
- **Remediation (deferred):** Add a unit test asserting `Query()`
  produces only `?` placeholders, and document the invariant inline.

#### L4 — JSON request bodies are decoded without `DisallowUnknownFields`

- **Files:** `internal/handlers/*.go` (every `json.NewDecoder(r.Body)`
  call site)
- **Category:** Parsing footgun / API hygiene
- **Description:** Unknown fields in request bodies are silently
  ignored. A client that misspells `is_superuser` won't get a 400 — the
  server will treat the field as absent. This isn't a privilege-escalation
  bug because the handlers explicitly read named fields, but it makes
  API misuse harder to detect during integration work.
- **Remediation (deferred):** Decide whether to enforce strict decoding
  globally (writes only) and update affected handlers + tests.

#### L5 — Optional improvement: `noSniff` on JSON exports

- **File:** `internal/handlers/admin.go:625-668`
- **Category:** Hardening
- **Description:** Audit-log CSV/JSON exports already set
  `Content-Disposition: attachment`, but `X-Content-Type-Options: nosniff`
  is only applied via the global middleware. Exports through this path
  inherit the header (verified via `SecurityHeadersWithConfig`), so this
  is fine — flagged here only to confirm.

### Informational / Defenses Verified

The following common foot-guns were checked and the codebase already
handles them correctly. Listing them so reviewers can confirm coverage.

| Category | Status | Where |
|---|---|---|
| SQL injection | Parameterised | all `*Context` query call sites |
| Weak crypto | Only AES-GCM, HMAC-SHA256, bcrypt-12 | `internal/services/mfa.go`, `internal/middleware/csrf.go`, `internal/handlers/auth.go` |
| Hardcoded secrets | None in `*.go`; `.env.example` defaults rejected in prod | `internal/config/config.go:478-494` |
| Path traversal | No user-controlled `filepath.Join` outside tests | n/a |
| SSRF | All outbound HTTP targets fixed base URLs | `internal/services/{mapbox,lob,postgrid,nces}.go` |
| Command injection | No `os/exec` usage | n/a |
| Template/XSS | No `template.HTML/JS/URL/CSS/Attr` casts | n/a |
| Auth/Authz | Every authenticated route wraps `r.authenticated`; superuser endpoints re-check DB | `internal/handlers/router.go`, `internal/handlers/admin.go:44` |
| JWT footguns | Signing method enforced; token-type checks; cache-backed revocation | `internal/middleware/auth.go:117-122,192-226` |
| Cookie footguns | `HttpOnly`, `Secure` (dynamic), `SameSite=Strict/Lax` | `internal/handlers/auth.go:408`, `internal/middleware/csrf.go:154` |
| InsecureSkipVerify | Zero matches | grep |
| HTTP client timeouts | All outbound clients set `Timeout: 30s` | `internal/services/{mapbox,lob,postgrid,sendgrid,nces}.go` |
| Context misuse | Handlers consistently use `r.Context()`; `context.Background()` only in startup and worker bootstrap | `cmd/server/main.go`, `internal/database/database.go:34` |
| Concurrency | `sync.Mutex` always behind pointer receivers | `internal/services/ratelimit.go:179` |
| Open redirects | No `http.Redirect` in handler paths | grep |
| CSRF | Double-submit + HMAC signature; exempt list limited to pre-auth endpoints | `internal/middleware/csrf.go:23-37` |
| CORS | Wildcard origin never paired with `Allow-Credentials` | `internal/middleware/middleware.go:73-103` |
| Rate limits | Per-IP global + per-action auth caps; account lockout after 10 fails | `internal/handlers/auth.go:27-39`, `internal/handlers/router.go:222-225` |
| Request body cap | 1 MiB via `MaxBodySize` middleware | `internal/handlers/router.go:214` |
| Outbound body cap | `readLimitedBody` with explicit 5 MiB / 50 MiB ceilings | `internal/services/http_helpers.go` |
| Zero address storage | Addresses only logged by `address.City` at Debug; no street/line1/coordinates in any log | grep across `internal/` |
| Email security | Sensitive content (invite links, codes) never embedded in outbound email; "log in to view" pattern enforced | `internal/services/notification_worker.go` |

---

## Remaining Recommendations

1. Add a `go.sum` hygiene step in CI: `govulncheck ./...` on every PR.
2. Track L3 and L4 as separate tickets; L4 in particular benefits from a
   shared `decodeStrict(r, &v)` helper rather than touching each handler.
3. When the next pass on `internal/handlers/admin.go` happens, replace
   offset pagination on `ListUsers` with cursor pagination (id-after) so
   the `page` cap stops mattering.
