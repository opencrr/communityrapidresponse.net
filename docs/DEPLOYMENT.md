# Deployment Guide

This guide covers running Community Rapid Response in a production-like environment.
It complements the developer-oriented [README.md](../README.md) and
[CONTRIBUTING.md](../CONTRIBUTING.md), and assumes you have already chosen and
provisioned a host (cloud VPS, dedicated server, or managed container platform).

Infrastructure provisioning (Terraform/Ansible) is maintained in a separate
deployment repository; this document focuses on the application itself.

## Topology

A standard deployment runs four things:

1. **Reverse proxy** (Nginx) — terminates TLS, serves static assets, proxies API
   requests to the Go backend. See [`nginx.conf`](../nginx.conf) for the in-repo
   reference config; production deployments typically extend it with TLS, HSTS,
   and rate limiting at the edge.
2. **Go backend** — the binary built from `cmd/server`. Listens on
   `SERVER_HOST:SERVER_PORT` (defaults `0.0.0.0:8080`).
3. **MariaDB 11.x** — with spatial extensions. The application schema is managed
   via [`migrations/`](../migrations/).
4. **Outbound integrations** — Mapbox (geocoding/tiles), Postgrid or Lob
   (address validation + postcard mailing), SendGrid or SMTP (transactional
   email), Sentry (errors).

Optional:
- A second MariaDB instance as a hot standby/replica.
- A bastion or jump host for DB access.
- A Meshtastic gateway (out of scope for this repo; the API only stores channel
  metadata).

## Build

Production binaries are built via the multi-stage [`Dockerfile`](../Dockerfile).

```bash
just build-prod
# or:
docker build --target production -t communityrapidresponse:latest .
```

The resulting image runs `cmd/server` as PID 1 and exposes port 8080.

## Configuration

All configuration is environment-driven. The canonical list of variables lives in
[`.env.example`](../.env.example) and is read by [`internal/config`](../internal/config).
Below is the production-relevant subset.

### Server

| Variable        | Required | Example                | Notes                              |
|-----------------|----------|------------------------|------------------------------------|
| `SERVER_HOST`   | yes      | `0.0.0.0`              | Bind address                       |
| `SERVER_PORT`   | yes      | `8080`                 | Bind port                          |
| `ENVIRONMENT`   | yes      | `production`           | Used by logging/Sentry             |
| `SECURE_COOKIES`| yes      | `true`                 | **Must be `true` in production**   |

### Database

| Variable        | Required | Example                                | Notes                                  |
|-----------------|----------|----------------------------------------|----------------------------------------|
| `DB_HOST`       | yes      | `db.internal`                          |                                        |
| `DB_PORT`       | yes      | `3306`                                 |                                        |
| `DB_USER`       | yes      | `communityrapidresponse`               | Use a least-privilege user             |
| `DB_PASSWORD`   | yes      | (secret)                               | Provide via secret manager             |
| `DB_NAME`       | yes      | `communityrapidresponse`               |                                        |

Run migrations on every deploy:

```bash
just db-migrate                       # against the configured prod DB
# or run inside the container:
docker run --rm --env-file=prod.env communityrapidresponse:latest \
  /app/scripts/migrate.sh up
```

### Authentication and MFA

| Variable                | Required | Example                          | Notes                                              |
|-------------------------|----------|----------------------------------|----------------------------------------------------|
| `JWT_SECRET`            | yes      | 32+ char random string           | Rotate via deploy with rolling restart             |
| `JWT_EXPIRATION_HOURS`  | yes      | `24`                             | Full-token lifetime                                |
| `JWT_ISSUER`            | yes      | `communityrapidresponse`         |                                                    |
| `MFA_REQUIRED`          | yes      | `true`                           | **Must be `true` in production**                   |
| `MFA_ENCRYPTION_KEY`    | yes      | 32-byte key                      | AES-256-GCM key for TOTP secret encryption         |
| `MFA_ISSUER`            | yes      | `Community Rapid Response`       | Shown in authenticator apps                        |

### Mapbox

| Variable               | Required | Notes                                  |
|------------------------|----------|----------------------------------------|
| `MAPBOX_PUBLIC_TOKEN`  | yes      | Exposed to the browser                 |
| `MAPBOX_SECRET_TOKEN`  | yes      | Server-only; OSM lookups               |

### Mail Provider (Address Validation + Postcards)

Select via `MAIL_PROVIDER=lob` (recommended) or `MAIL_PROVIDER=postgrid` (legacy).

Lob:

| Variable              | Required | Notes                                |
|-----------------------|----------|--------------------------------------|
| `LOB_API_KEY`         | yes      |                                      |
| `LOB_BASE_URL`        | yes      | `https://api.lob.com/v1`             |
| `LOB_RETURN_*`        | yes      | Name/address/city/state/zip/country  |

Postgrid (legacy):

| Variable                          | Required | Notes                                                 |
|-----------------------------------|----------|-------------------------------------------------------|
| `POSTGRID_ADDVER_API_KEY`         | yes      | Address verification                                  |
| `POSTGRID_ADDVER_BASE_URL`        | yes      | `https://api.postgrid.com/v1`                         |
| `POSTGRID_PRINT_API_KEY`          | yes      | Print + mail                                          |
| `POSTGRID_PRINT_BASE_URL`         | yes      | `https://api.postgrid.com/print-mail/v1`              |
| `POSTGRID_RETURN_*`               | yes      | Name/address/city/state/zip/country                   |

### Email

`EMAIL_BACKEND` selects the implementation in `internal/services`:

| Backend     | Variables                                                 |
|-------------|------------------------------------------------------------|
| `mock`      | none (development only)                                    |
| `sendgrid`  | `SENDGRID_API_KEY`                                         |
| `smtp`      | `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`     |

Common:

| Variable                 | Required | Notes                                          |
|--------------------------|----------|------------------------------------------------|
| `EMAIL_ENABLED`          | yes      | `true` in production                           |
| `EMAIL_VERIFICATION_URL` | yes      | Public URL for email-verify links              |
| `EMAIL_FROM_ADDRESS`     | yes      | Must match a verified sender at the provider   |
| `EMAIL_FROM_NAME`        | optional |                                                |

Reminder: invite links, postcard codes, and addresses **must never** appear in
email content (see [SECURITY.md](../SECURITY.md)).

### Rate Limiting and CORS

| Variable                  | Required | Recommended production value                            |
|---------------------------|----------|----------------------------------------------------------|
| `RATE_LIMIT_ENABLED`      | yes      | `true`                                                   |
| `RATE_LIMIT_IP_LIMIT`     | yes      | `100`                                                    |
| `RATE_LIMIT_IP_WINDOW_SECS` | yes    | `60`                                                     |
| `CORS_ALLOWED_ORIGINS`    | yes      | Comma-separated list of public origins                   |

### Observability

| Variable        | Required | Notes                                       |
|-----------------|----------|---------------------------------------------|
| `SENTRY_DSN`    | optional | Enables Sentry error reporting if set       |
| `LOG_LEVEL`     | optional | `info` (default), `debug`, `warn`, `error`  |

## Encryption at Rest

Address data never reaches the database, but **everything else** — users,
verification metadata, audit logs, MFA secrets (already encrypted with
AES-256-GCM at the application layer), Signal/Meshtastic group metadata — does.
Production deployments **must** enable encryption at rest. Two supported
approaches:

1. **MariaDB data-at-rest encryption** (preferred): enable
   `innodb_encrypt_tables=ON`, `innodb_encrypt_log=ON`, and configure a key file
   or KMIP key management plugin. See the MariaDB
   [Data-at-Rest Encryption docs](https://mariadb.com/kb/en/data-at-rest-encryption-overview/).
2. **Filesystem-level encryption** on the host (e.g., LUKS) when MariaDB
   encryption is not available.

Back up keys out of band — losing them is equivalent to losing the database.

## TLS

Terminate TLS at Nginx or upstream load balancer. Ensure:

- `SECURE_COOKIES=true` so cookies are flagged `Secure`.
- HSTS is set on the proxy (`Strict-Transport-Security: max-age=31536000; includeSubDomains; preload`).
- HTTP is redirected to HTTPS at the proxy.

The in-repo [`nginx.conf`](../nginx.conf) is suitable for non-TLS test
environments; extend it for production.

## Docker Compose

The [`docker-compose.yml`](../docker-compose.yml) at the root is **for development**
(hot reload, mock email, lax rate limits). For a production deployment, either:

- Use the production stage of the [`Dockerfile`](../Dockerfile) directly behind
  your orchestrator (Kubernetes, Nomad, ECS, etc.), or
- Maintain a separate compose overlay that sets `target: production`, disables
  the `app`/`web` dev services, and reads secrets from an external secret store.

## Running Migrations on Deploy

Run migrations **before** new code starts serving traffic — this keeps the schema
ahead of the application binary so older replicas can continue serving requests
during a rolling deploy. Migrations are idempotent (`schema_migrations` table
tracks state) so re-running is safe.

```bash
just db-migrate
```

To roll back a single migration (only in emergencies, and only if the `.down.sql`
is known-safe for production data):

```bash
just db-migrate-down prod 1
```

## Bootstrapping the First Superuser

A superuser cannot be created via the API. Create one directly in the database
after the first migration runs:

```bash
just create-superuser            # interactive, runs against the configured DB
# or:
just promote-superuser admin@example.com
```

A superuser can then grant vouch verification to other users to bootstrap
admins per region.

## Health Checks and Readiness

The backend exposes:

- `GET /health`        — liveness
- `GET /api/v1/health` — same payload, aliased path for API clients

Both return `{"status":"ok"}` with HTTP 200. Configure your orchestrator's
liveness and readiness probes against one of these.

## Observability and Alerting

- **Errors**: forwarded to Sentry when `SENTRY_DSN` is set. Recommend tagging
  events with `ENVIRONMENT` and `release` for triage.
- **Audit log**: query `audit_log` for administrative actions (90-day retention
  by design).
- **Logs**: structured JSON via `internal/logging`. Ship to your aggregator
  (e.g., Loki, Datadog) with request ID for tracing.

## Backups and Disaster Recovery

- Schedule encrypted full DB dumps daily and incremental binlog backups.
- Test restores periodically. Keep at least one off-host copy.
- Never include `.env` or `MFA_ENCRYPTION_KEY` material in DB backups — secrets
  belong in a dedicated secret store (HashiCorp Vault, AWS Secrets Manager,
  GCP Secret Manager, etc.).

## Pre-Deploy Checklist

- [ ] `MFA_REQUIRED=true`
- [ ] `SECURE_COOKIES=true`
- [ ] `RATE_LIMIT_ENABLED=true`
- [ ] `EMAIL_ENABLED=true` with a non-mock backend
- [ ] `JWT_SECRET` is at least 32 chars and unique per environment
- [ ] `MFA_ENCRYPTION_KEY` is exactly 32 bytes and unique per environment
- [ ] MariaDB data-at-rest encryption enabled (or filesystem-level equivalent)
- [ ] TLS terminating proxy + HSTS in place
- [ ] Sentry DSN configured
- [ ] Migrations applied
- [ ] Off-host encrypted backup verified by a test restore

## Further Reading

- [docs/ARCHITECTURE.md](ARCHITECTURE.md) — system architecture
- [docs/API.md](API.md) — REST API reference
- [SECURITY.md](../SECURITY.md) — disclosure policy and security posture
- [DESIGN.md](../DESIGN.md) — full design specification
