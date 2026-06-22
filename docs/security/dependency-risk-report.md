# Dependency Risk Report

**Scan date:** 2026-06-07
**Branch:** `security/dependency-risk-scan`
**Module:** `github.com/opencrr/communityrapidresponse.net`
**Go toolchain:** `go1.25.9`
**Tools used:** `go list -m -u all`, `go vet ./...`, `go mod verify`, `govulncheck`
(`golang.org/x/vuln/cmd/govulncheck@latest`).

This report is reproducible — re-run with `just deps-scan`.

## How to read this report

- **Risk tier** is one of `LOW`, `MEDIUM`, `HIGH`, `CRITICAL`, based on:
  - Known CVEs called by our code (`CRITICAL` / `HIGH`).
  - Known CVEs in imported modules, but not reachable from our call graph
    (`MEDIUM`).
  - Staleness >12 months, single-maintainer projects, or pseudo-version pins
    (`MEDIUM` or `LOW` depending on signal).
  - Currently maintained, recent release, no known CVEs (`LOW`).
- All findings are **defensive**: this audit changes no application code. Each
  finding is a recommendation, not an executed change.

## Summary

| Tier      | Count | Notes                                                                          |
| --------- | ----- | ------------------------------------------------------------------------------ |
| CRITICAL  | 0     | No critical CVEs reachable from our call graph.                                |
| HIGH      | 1     | `go1.25.9` standard library — 4 CVEs reachable from our code, fix is a Go bump. |
| MEDIUM    | 3     | `golang.org/x/crypto`, `golang.org/x/sys`, `boombuler/barcode` (pseudo-version). |
| LOW       | 9     | All other direct/indirect modules — current, low risk.                          |

`go mod verify` reports **all modules verified** (`go.sum` hashes match
downloaded modules). `go vet ./...` is clean.

## Direct dependencies

| Module                                | Current                                       | Latest    | License        | Risk tier | Notes                                                                                  |
| ------------------------------------- | --------------------------------------------- | --------- | -------------- | --------- | -------------------------------------------------------------------------------------- |
| `github.com/DATA-DOG/go-sqlmock`      | `v1.5.2`                                      | `v1.5.2`  | BSD-3-Clause   | LOW       | Test-only dep. Maintained, current. Not in production binary.                          |
| `github.com/getsentry/sentry-go`      | `v0.46.2`                                     | `v0.46.2` | MIT            | LOW       | Vendor-maintained, multiple maintainers, current release.                              |
| `github.com/go-sql-driver/mysql`      | `v1.10.0`                                     | `v1.10.0` | MPL-2.0        | LOW       | Active community project, current release, no known CVEs.                              |
| `github.com/golang-jwt/jwt/v5`        | `v5.3.1`                                      | `v5.3.1`  | MIT            | LOW       | Active fork (post-`dgrijalva`), multiple maintainers, current.                         |
| `github.com/google/uuid`              | `v1.6.0`                                      | `v1.6.0`  | BSD-3-Clause   | LOW       | Google-maintained, current.                                                            |
| `github.com/pquerna/otp`              | `v1.5.0`                                      | `v1.5.0`  | Apache-2.0     | LOW       | Smaller project (single primary maintainer), but current and no known CVEs.            |
| `golang.org/x/crypto`                 | `v0.51.0`                                     | `v0.52.0` | BSD-3-Clause   | MEDIUM    | One minor behind; **16 CVEs** fixed in `v0.52.0`, all in `ssh/*` which we do not call. |

## Indirect dependencies

| Module                            | Current                                            | Latest    | License        | Risk tier | Notes                                                                              |
| --------------------------------- | -------------------------------------------------- | --------- | -------------- | --------- | ---------------------------------------------------------------------------------- |
| `filippo.io/edwards25519`         | `v1.2.0`                                           | `v1.2.0`  | BSD-3-Clause   | LOW       | Tiny crypto primitive lib, maintained by Filippo Valsorda; current.                |
| `github.com/boombuler/barcode`    | `v1.0.1-0.20190219062509-6c824513bacc`             | `v1.1.0`  | MIT            | MEDIUM    | **Pseudo-version pinned to a 2019 commit** (pulled in by `pquerna/otp`).            |
| `golang.org/x/sys`                | `v0.44.0`                                          | `v0.45.0` | BSD-3-Clause   | MEDIUM    | One minor behind. No known reachable CVEs, but should be bumped on principle.       |
| `golang.org/x/text`               | `v0.37.0`                                          | `v0.37.0` | BSD-3-Clause   | LOW       | Current.                                                                           |

## govulncheck findings

`govulncheck ./...` was run against the public Go vulnerability database. Results
are split into three buckets by reachability.

### 1. Reachable from our code — HIGH priority

These four CVEs are in the Go standard library used by our binary and our code
is on the call path. **All four are fixed by upgrading the Go toolchain** from
`go1.25.9` to `go1.25.11` (or later in the `1.25.x` line).

| ID            | Title                                                                                           | Fixed in       |
| ------------- | ----------------------------------------------------------------------------------------------- | -------------- |
| GO-2026-5039  | Arbitrary inputs included in errors without escaping in `net/textproto`                         | `go1.25.11`    |
| GO-2026-5037  | Inefficient candidate hostname parsing in `crypto/x509`                                         | `go1.25.11`    |
| GO-2026-4971  | Panic in `Dial`/`LookupPort` when handling NUL byte on Windows in `net`                         | `go1.25.10`    |
| GO-2026-4918  | Infinite loop in HTTP/2 transport given bad `SETTINGS_MAX_FRAME_SIZE` in `net/http/internal/http2` | `go1.25.10`    |

Sample call sites govulncheck identified:

- `internal/services/mapbox.go:1126` → `MapboxService.GetNeighborhoodBoundary`
  → `net/textproto`, `crypto/x509`, `net/http`.
- `internal/services/email_smtp.go:111` / `:127` / `:133` → SMTP client →
  `net.Dial`, `tls.Dial`, `textproto.Reader.ReadResponse`.
- `internal/services/mfa.go:92` → `MFAService.EncryptSecret` →
  `io.ReadFull` → `crypto/x509`.
- `cmd/server/main.go:363` → `http.Server.ListenAndServe` → `net.Listen`.

### 2. Imported but not reached from our call graph — MEDIUM priority

3 standard-library CVEs in packages we import transitively but don't call from
our code paths. Still fixed by the same Go toolchain bump.

| ID            | Title                                                                  | Fixed in       |
| ------------- | ---------------------------------------------------------------------- | -------------- |
| GO-2026-5038  | Quadratic complexity in `WordDecoder.DecodeHeader` in `mime`            | `go1.25.11`    |
| GO-2026-4981  | Crash when handling long CNAME response in `net`                        | `go1.25.10`    |
| GO-2026-4976  | `ReverseProxy` forwards queries with > urlmaxqueryparams in `httputil`  | `go1.25.10`    |

### 3. Module-level CVEs (not reachable) — INFORMATIONAL

17 CVEs in modules we depend on, but govulncheck reports our code does not
exercise the vulnerable symbols. The fix is to bump
`golang.org/x/crypto` to `v0.52.0` (also resolves additional `stdlib`
template/mail CVEs).

| ID            | Title                                                                                  | Module / fix                                          |
| ------------- | -------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| GO-2026-5033  | Pathological inputs can panic client in `crypto/ssh/agent`                              | `golang.org/x/crypto` → `v0.52.0`                     |
| GO-2026-5023  | `VerifiedPublicKeyCallback` permissions skip enforcement in `crypto/ssh`                | `golang.org/x/crypto` → `v0.52.0`                     |
| GO-2026-5021  | Auth bypass via unenforced `@revoked` status in `crypto/ssh/knownhosts`                 | `golang.org/x/crypto` → `v0.52.0`                     |
| GO-2026-5020  | Infinite loop on large channel writes in `crypto/ssh`                                   | `golang.org/x/crypto` → `v0.52.0`                     |
| GO-2026-5019  | FIDO/U2F security key physical interaction bypass in `crypto/ssh`                       | `golang.org/x/crypto` → `v0.52.0`                     |
| GO-2026-5018  | Pathological RSA/DSA parameters may cause DoS in `crypto/ssh`                           | `golang.org/x/crypto` → `v0.52.0`                     |
| GO-2026-5017  | Client can cause server deadlock on unexpected responses in `crypto/ssh`                | `golang.org/x/crypto` → `v0.52.0`                     |
| GO-2026-5016  | Memory leak when rejecting channels (DoS) in `crypto/ssh`                               | `golang.org/x/crypto` → `v0.52.0`                     |
| GO-2026-5015  | Server panic during `CheckHostKey`/`Authenticate` in `crypto/ssh`                       | `golang.org/x/crypto` → `v0.52.0`                     |
| GO-2026-5014  | Bypass of certificate restrictions in `crypto/ssh`                                      | `golang.org/x/crypto` → `v0.52.0`                     |
| GO-2026-5013  | Byte arithmetic underflow and panic in `crypto/ssh`                                     | `golang.org/x/crypto` → `v0.52.0`                     |
| GO-2026-5006  | Agent constraints dropped when forwarding keys in `crypto/ssh/agent`                    | `golang.org/x/crypto` → `v0.52.0`                     |
| GO-2026-5005  | Key constraints not enforced in `crypto/ssh/agent`                                      | `golang.org/x/crypto` → `v0.52.0`                     |
| GO-2026-4986  | Quadratic string concatenation in `consumeComment` in `net/mail`                        | `stdlib` → `go1.25.10`                                |
| GO-2026-4982  | Bypass of meta content URL escaping causes XSS in `html/template`                       | `stdlib` → `go1.25.10`                                |
| GO-2026-4980  | Escaper bypass leads to XSS in `html/template`                                          | `stdlib` → `go1.25.10`                                |
| GO-2026-4977  | Quadratic string concatenation in `consumePhrase` in `net/mail`                         | `stdlib` → `go1.25.10`                                |

We do not import `crypto/ssh` at all, so the SSH-family CVEs cannot be
reached. We do render HTML templates, so the `html/template` CVEs deserve a
double-check after the Go bump even though the call-graph scan didn't flag them
as reachable from our handlers.

## Per-dependency risk notes

### `golang.org/x/crypto v0.51.0` — MEDIUM

- One minor behind upstream (`v0.52.0`).
- `v0.52.0` ships 16 advisories, all in `ssh/*` and `ssh/agent`/`knownhosts`.
- We use `golang.org/x/crypto/bcrypt` (password hashing). Bcrypt is **not** in the
  affected packages, so there is no production code path we know to be at
  immediate risk.
- Still: stale crypto modules accumulate risk quickly. Bump on the same PR as
  the Go toolchain bump.

### `golang.org/x/sys v0.44.0` — MEDIUM

- One minor behind upstream (`v0.45.0`).
- No CVEs flagged, but `x/sys` is a base dependency that ratchets quickly with
  toolchain changes — keeping it in lock-step with the Go release is hygiene.

### `github.com/boombuler/barcode v1.0.1-0.20190219062509-6c824513bacc` — MEDIUM

- Pinned to a **pseudo-version from February 2019** — pulled in transitively by
  `github.com/pquerna/otp` for QR code rendering during MFA setup.
- Upstream has tagged `v1.1.0` since.
- A pseudo-version pin means a dependency-resolver upgrade alone won't pick up
  the fix; the upstream consumer (`pquerna/otp`) is what dictates the version,
  so a `go get -u github.com/pquerna/otp` may be needed to roll forward.
- No known CVEs against the pinned version, but a 7-year-old commit is a
  maintenance smell.

### Standard library (`go1.25.9`) — HIGH

- 4 reachable CVEs (listed above). All addressed by upgrading to `go1.25.11+`.
- Action: bump the `go` directive in `go.mod` and the toolchain images in
  `docker/`, then re-run `just deps-scan` to confirm zero reachable CVEs.

### Other modules — LOW

- `getsentry/sentry-go`, `go-sql-driver/mysql`, `golang-jwt/jwt/v5`,
  `google/uuid`, `pquerna/otp`, `DATA-DOG/go-sqlmock`,
  `filippo.io/edwards25519`, `golang.org/x/text`: all on their latest release
  with no open vulnerabilities reported by govulncheck.

## Prioritized remediation

1. **Bump the Go toolchain to `go1.25.11` (or newer in the `1.25.x` line).**
   This resolves 4 HIGH (reachable) and 6 additional informational stdlib
   CVEs. Update `go.mod` `go` directive, `docker/` base image tags, and any
   CI workflow `go-version` pins. Then run `just deps-scan` to confirm.

2. **Upgrade `golang.org/x/crypto` to `v0.52.0`.** Clears 13 `ssh/*` module CVEs
   (not reachable today, but defense in depth). Same PR as the Go bump.

3. **Upgrade `golang.org/x/sys` to `v0.45.0`.** Hygiene; keep in lock-step with
   the toolchain bump.

4. **Refresh `github.com/pquerna/otp` (and its `boombuler/barcode` transitive)
   off the 2019 pseudo-version.** `go get -u github.com/pquerna/otp` followed
   by `go mod tidy` should pull `boombuler/barcode` to `v1.1.0`. Run the MFA
   setup flow end-to-end after the change.

5. **Recurring posture.** Run `just deps-scan` weekly in CI (or as part of
   `just security`) and gate releases on a clean report. The recipe is
   deliberately a thin wrapper around `go list -m -u all` + `govulncheck` so it
   can be diffed in PR review.

## Repro

```bash
just deps-scan
```

Equivalent manual commands:

```bash
go list -m -u all
go mod verify
go vet ./...
govulncheck ./...   # install via: go install golang.org/x/vuln/cmd/govulncheck@latest
```
