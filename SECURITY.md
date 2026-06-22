# Security Policy

Community Rapid Response exists to help people connect with verified neighbors. The
trust that powers that experience depends on us holding a high security and privacy
bar. We welcome reports of vulnerabilities and concerns from the security community.

## Supported Versions

This project is actively developed against the `main` branch. Security fixes are
applied to `main` and rolled out via the most recent tagged release. Older releases
do not receive backports unless explicitly noted. Always run a build from a recent
`main` or release tag.

## Reporting a Vulnerability

**Please do not open public GitHub issues for suspected vulnerabilities.** Instead,
report privately via one of the following channels:

- GitHub Security Advisory: <https://github.com/opencrr/communityrapidresponse.net/security/advisories/new>
  (use "Report a vulnerability" — this keeps the report private to maintainers).
- Email: `security@communityrapidresponse.net`. Encrypt sensitive material if possible.

Include:

- A description of the issue and the impact you believe it has.
- Steps to reproduce, including affected endpoint(s), payload(s), and any
  authentication state required.
- Your name or handle for credit (optional) and how you would like to be acknowledged.

### Response Targets

- **Acknowledgement**: within 3 business days of receipt.
- **Triage and severity assessment**: within 7 business days.
- **Fix or mitigation plan**: communicated within 30 days for high/critical issues.

We will keep you informed throughout the process and credit reporters who request it
once a fix has shipped.

## Scope

The following are in scope:

- The Go backend in this repository (handlers, middleware, services).
- The database schema, migrations, and data-handling logic.
- The static frontend assets under [`static/`](static/) and [`templates/`](templates/)
  that ship with the backend.

The following are explicitly **out of scope**:

- Third-party providers we integrate with (Mapbox, Postgrid/Lob, SendGrid, Signal).
  Please report issues there to the upstream vendor.
- Denial-of-service attacks, volumetric attacks, or social engineering against
  maintainers or users.
- Issues that require physical access to a user's device, root access to the host,
  or compromised user credentials obtained outside this system.
- Findings that depend on disabled-by-default development flags such as
  `MFA_REQUIRED=false` or `RATE_LIMIT_ENABLED=false`. Production deployments are
  expected to enable both.

## Security Posture

This is a short summary of the controls in place. See [DESIGN.md](DESIGN.md) and
[CLAUDE.md](CLAUDE.md) for full detail.

- **Zero address storage**: Street addresses, apartment numbers, and precise GPS
  coordinates are never persisted. Addresses live in memory only for the lifetime
  of a verification request, are sent to the mailing provider (Postgrid/Lob), and
  are then discarded.
- **MFA**: TOTP-based MFA is mandatory in production. MFA secrets are encrypted
  with AES-256-GCM before being written to the database.
- **Token model**: JWT tokens are issued for distinct purposes (`full`,
  `mfa_setup`, `pending_mfa`, `email_unverified`) so a token issued for one
  purpose cannot be replayed against another. Full tokens expire in 24 hours;
  intermediate tokens expire in 10 minutes.
- **Cookies**: Tokens are stored in HttpOnly cookies. Production deployments set
  `SECURE_COOKIES=true` (requires HTTPS).
- **Password storage**: bcrypt at cost factor 12, minimum 12-character passwords.
- **Account lockout**: 10 failed login attempts trigger a 15-minute lock; locks
  are audit-logged.
- **Rate limiting**: Per-IP global limits plus tighter limits on `login`,
  `register`, `forgot-password`, `reset-password`, and `resend-verification`.
  Verification (3/30d) and vouches (10/month) are also rate-limited.
- **Address validation**: Postgrid/Lob rejects PO Boxes and CMRA addresses; only
  residential/business street addresses are accepted.
- **Email content**: Sensitive material (invite links, verification codes,
  addresses) is **never** included in emails. Notifications instruct users to log
  in to view sensitive details.
- **Encryption at rest**: Production deployments enable MariaDB data-at-rest
  encryption (or filesystem-level encryption). See [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).
- **Consensus governance**: Deletions, user blocklisting, and Signal/Meshtastic
  invite/secret updates require 3-admin approval to limit unilateral abuse.
- **Audit logging**: All administrative actions are written to an audit log with
  a 90-day retention.

## Dependency Risk

We run a defensive dependency audit (`just deps-scan`) against `go.mod`/`go.sum`
using `govulncheck` and `go list -m -u all`. The most recent triaged snapshot —
direct/indirect inventory, risk tiers, and a prioritized remediation list — is
maintained in
[docs/security/dependency-risk-report.md](docs/security/dependency-risk-report.md).
Re-run the audit with:

```bash
just deps-scan
```

## Coordinated Disclosure

We support coordinated disclosure. Once a fix has been released and deployed, we
will publish a security advisory describing the issue, affected versions, and the
mitigation. Reporters who request credit will be named in the advisory.

Thank you for helping keep Community Rapid Response and its users safe.
