# Dependency Risk Report

**Snapshot date:** 2026-06-02
**Repository:** `github.com/opencrr/communityrapidresponse.net`
**Go toolchain:** `go1.25.9`
**Generator:** `scripts/dependency-risk-scan.sh`

This is a one-time, human-curated risk assessment of the modules declared in
`go.mod`. The companion script (`scripts/dependency-risk-scan.sh`) produces a
fresh machine-generated version of the same information on demand and on a
weekly schedule via `.github/workflows/dependency-risk.yml`.

This report intentionally does **not** upgrade any dependency. It exists to
make the current risk surface explicit so upgrades can be planned and tracked.

## Scope

The 11 modules listed in `go.mod`:

- 7 direct dependencies
- 4 indirect dependencies

`go list -m all` surfaces an additional ~19 transitive test-only dependencies
that the scanner script also catalogues. Those are excluded from this
hand-curated report because they enter the build graph only via dev/test
tooling and are not exercised by the production binary.

## Findings at a Glance

| Severity | Count | Notes |
|----------|-------|-------|
| HIGH     | 2     | Active vulnerabilities in Go stdlib reachable from running code (`net.Dial`, `http.Client.Do`). Fixed in `go1.25.10`. |
| MEDIUM   | 2     | `golang.org/x/crypto v0.50.0` carries 21 latent SSH vulnerabilities (none reached today; future code paths could). `filippo.io/edwards25519 v1.1.0` has an upstream correctness issue fixed in `v1.1.1`. |
| LOW      | 5     | Outdated minor/patch releases on `sentry-go`, `go-sql-driver/mysql`, `boombuler/barcode`, `golang.org/x/sys`, `golang.org/x/text`. |
| INFO     | 2     | `boombuler/barcode` pinned to a pre-1.0 commit from 2019; `DATA-DOG/go-sqlmock` last released January 2024 but appears mature. |

Vulnerability counts come from `govulncheck` on 2026-06-02. The Go stdlib
findings are the ones to act on first: they are the only items with a traced
call chain from production code.

## Dependency Inventory

| Module | Direct | Current | Latest | Released | License | Risk |
|--------|:------:|---------|--------|----------|---------|:----:|
| `github.com/DATA-DOG/go-sqlmock` | ✓ | v1.5.2 | v1.5.2 | 2024-01-06 | MIT (BSD-style) | LOW |
| `github.com/getsentry/sentry-go` | ✓ | v0.45.1 | v0.46.2 | 2026-04-13 | MIT | LOW |
| `github.com/go-sql-driver/mysql` | ✓ | v1.9.3 | v1.10.0 | 2025-06-13 | MPL-2.0 | LOW |
| `github.com/golang-jwt/jwt/v5` | ✓ | v5.3.1 | v5.3.1 | 2026-01-28 | MIT | LOW |
| `github.com/google/uuid` | ✓ | v1.6.0 | v1.6.0 | 2024-01-23 | BSD-3-Clause | LOW |
| `github.com/pquerna/otp` | ✓ | v1.5.0 | v1.5.0 | 2024-12-31 | Apache-2.0 | LOW |
| `golang.org/x/crypto` | ✓ | v0.50.0 | v0.52.0 | 2026-04-09 | BSD-3-Clause | MEDIUM |
| `filippo.io/edwards25519` |   | v1.1.0 | v1.2.0 | 2023-12-10 | BSD-3-Clause | MEDIUM |
| `github.com/boombuler/barcode` |   | v1.0.1-0.20190219062509-6c824513bacc | v1.1.0 | 2019-02-19 | MIT | INFO |
| `golang.org/x/sys` |   | v0.43.0 | v0.45.0 | 2026-03-27 | BSD-3-Clause | LOW |
| `golang.org/x/text` |   | v0.36.0 | v0.37.0 | 2026-04-09 | BSD-3-Clause | LOW |

Licenses are all permissive (MIT, BSD, Apache-2.0, MPL-2.0). None of these
require source disclosure for users of this codebase.

## Active Vulnerabilities (HIGH)

`govulncheck ./...` finds two vulnerabilities **reachable from production
code** today. Both are Go standard library issues fixed in `go1.25.10`.

### GO-2026-4971 — `net.Dial` panic on Windows NUL byte
- **Affected:** `net@go1.25.9` (standard library)
- **Fixed in:** `net@go1.25.10`
- **Reached via:**
  - `internal/services/email_smtp.go:111` → `smtp.SendMail` → `net.Dial`
  - `internal/services/email_smtp.go:127` → `tls.Dial` → `net.Dialer.DialContext`
  - `cmd/server/main.go:363` → `http.Server.ListenAndServe` → `net.Listen`
- **Production impact:** Linux-only deployment makes this a low-likelihood
  trigger, but the symbol is reachable on any platform and CI runs on
  Ubuntu, so the call chain is real.

### GO-2026-4918 — HTTP/2 infinite loop on bad `SETTINGS_MAX_FRAME_SIZE`
- **Affected:** `net/http@go1.25.9` (standard library)
- **Fixed in:** `net/http@go1.25.10`
- **Reached via:** `internal/services/mapbox.go:1138` → `http.Client.Do`
- **Production impact:** A hostile Mapbox-API impersonator could lock up the
  outbound HTTP/2 connection. Mitigated by TLS pinning behaviour of the
  default client, but the trace exists.

### Remediation
Bump the Go toolchain from `1.25.9` to `1.25.10` (or higher). Both `go.mod`
and `.github/workflows/test.yml` pin to `1.25.9`; both need to move together.
This is a single-line change per file and unblocks both findings.

## Latent Vulnerabilities (MEDIUM)

These are CVEs in dependencies whose vulnerable symbols are **not currently
called** by production code. Tracked because a future change could activate
them.

### `golang.org/x/crypto v0.50.0` — 21 SSH-related vulnerabilities
A cluster of SSH/SFTP issues (GO-2026-5005, 5006, 5013–5019, 5023, 5033, and
others) all fixed in `golang.org/x/crypto v0.52.0`. None of these symbols are
reached from the current codebase (we use `golang.org/x/crypto/bcrypt` only),
but the dependency surface is large.

**Remediation:** Bump to `v0.52.0` in the next dependency-update PR. Mechanical
change; no API breakage expected.

### `filippo.io/edwards25519 v1.1.0` — GO-2026-4503
Correctness issue in scalar field arithmetic. Fixed in `v1.1.1`. This is an
indirect dependency pulled in by `golang-jwt/jwt/v5` for EdDSA support. The
project's JWT usage is HMAC-based (`HS256`), so the affected EdDSA paths are
not exercised today.

**Remediation:** A `go get filippo.io/edwards25519@v1.1.1 && go mod tidy`
suffices. Safe to bundle with the next dependency update.

## Outdated Releases (LOW)

| Module | From | To | Notes |
|--------|------|------|-------|
| `getsentry/sentry-go` | v0.45.1 | v0.46.2 | Two recent patch releases since the last bump (PR #65). |
| `go-sql-driver/mysql` | v1.9.3 | v1.10.0 | Minor release; needs read-through for protocol changes before bumping. |
| `boombuler/barcode` | v1.0.1-0.2019… | v1.1.0 | First tagged release in years; brings the pseudo-version up to a proper tag. |
| `golang.org/x/sys` | v0.43.0 | v0.45.0 | Routine. |
| `golang.org/x/text` | v0.36.0 | v0.37.0 | Routine. |

## Maintenance Observations (INFO)

- `github.com/boombuler/barcode` was pinned to a 2019 pseudo-version for the
  better part of six years. A v1.1.0 tag landed in July 2025; the module is
  still maintained but releases are infrequent. Worth tracking but not a risk
  in itself.
- `github.com/DATA-DOG/go-sqlmock` has had no release since January 2024.
  The library is feature-complete for our use; treat as mature, not
  abandoned, unless `database/sql` grows new behaviour we rely on.
- All other direct dependencies have shipped releases within the last
  18 months and are actively maintained.

## Indirect Test-Only Modules

`go list -m all` reports an additional set of indirect modules
(`github.com/stretchr/testify`, `github.com/google/go-cmp`,
`gopkg.in/yaml.v3`, `golang.org/x/tools`, etc.) that exist only because they
are transitive dependencies of `sqlmock`, `sentry-go`, and `crypto`. They are
not declared in `go.mod`, do not appear in the production binary, and are not
reachable from production code paths. They are out of scope for this report
but visible in the automated scanner output for completeness.

## Recommended Action Plan

Three independent PRs, ordered by ROI:

1. **(Now)** Bump Go toolchain to `1.25.10` in `go.mod` and
   `.github/workflows/test.yml`. Closes both HIGH findings.
2. **(Next sprint)** Bump `golang.org/x/crypto` to `v0.52.0` and
   `filippo.io/edwards25519` to `v1.1.1`. Closes both MEDIUM findings.
3. **(When convenient)** Routine bump of `sentry-go`, `go-sql-driver/mysql`,
   `golang.org/x/sys`, `golang.org/x/text`, `boombuler/barcode`.

The Dependabot configuration already covers (3); (1) and (2) need manual PRs.

## How This Report Is Refreshed

- **Locally:** `just deps-risk` or
  `bash scripts/dependency-risk-scan.sh > /tmp/report.md`.
- **CI:** `.github/workflows/dependency-risk.yml` runs every Monday at 12:00
  UTC, uploads the generated Markdown as a `dependency-risk-report` artifact,
  and opens (or comments on) a tracking issue labelled `dependency-risk` when
  active vulnerabilities are detected.
