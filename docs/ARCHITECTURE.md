# Architecture Overview

This document is a concise, pointer-style summary of how Community Rapid Response is
put together. For the full design — including database schema, API contracts, and
implementation phases — see [DESIGN.md](../DESIGN.md). For day-to-day quick reference,
see [CLAUDE.md](../CLAUDE.md).

## Tech Stack

| Layer        | Choice                                              |
|--------------|-----------------------------------------------------|
| Language     | Go 1.25.x                                           |
| HTTP         | `net/http` standard library + custom `ServeMux`-based router |
| Auth         | JWT (HS256) via `github.com/golang-jwt/jwt/v5`      |
| MFA          | TOTP via `github.com/pquerna/otp`, AES-256-GCM at rest |
| Database     | MariaDB 10.x with spatial extensions (`GEOMETRY SRID 4326`) |
| Driver       | `github.com/go-sql-driver/mysql`                    |
| Frontend     | Server-rendered HTML + vanilla JS (progressive enhancement) |
| Mapping      | Mapbox GL JS, Mapbox Geocoding/Tile APIs, Mapbox Draw |
| Address & mail | Postgrid or Lob (selected via `MAIL_PROVIDER`)    |
| Email        | SendGrid, SMTP, or mock (selected via `EMAIL_BACKEND`) |
| Observability| Sentry (`github.com/getsentry/sentry-go`), structured logs |
| Build/Dev    | Docker Compose, [`justfile`](../justfile), `air` for hot reload |

## Package Layout

```
cmd/
  server/             # HTTP server entrypoint
  seed-schools/       # one-shot NCES schools loader
internal/
  config/             # env-driven configuration
  database/           # connection management and repositories
  handlers/           # HTTP handlers + router.go
  middleware/         # JWT auth, CORS, CSRF, rate-limit, security headers, recoverer
  models/             # domain types
  services/           # external integrations: mapbox, postgrid/lob, email, mfa, nces, ratelimit, notification
  logging/            # structured logger
  sentry/             # Sentry initialization
  mocks/              # mock implementations for tests
  testutil/           # shared test helpers
migrations/           # NNN_description.up.sql / NNN_description.down.sql pairs
tests/                # integration and end-to-end tests
static/, templates/   # served frontend assets
```

The dependency direction is one-way: `cmd` → `handlers` → `services` + `database`
→ `models`/`config`. Tests live alongside the package they exercise.

## Request Lifecycle

A request flows through the following middleware chain (outer to inner — see
[`internal/handlers/router.go`](../internal/handlers/router.go)):

```
Logger → RequestContext → Sentry → Recoverer → RateLimit (optional) →
CORS → SecurityHeaders → CSRF (optional) → MaxBodySize (1 MB) → ContentType → ServeMux
```

Handlers themselves wrap their `http.HandlerFunc` with one of the JWT auth helpers:

- `authenticated` — requires a `full` token.
- `authenticatedMFASetup` — requires an `mfa_setup` token.
- `authenticatedPendingMFA` — requires a `pending_mfa` token.
- `authenticatedEmailUnverified` — requires an `email_unverified` token.

Per-token-type scoping means a token issued during MFA setup cannot be replayed
against a protected resource endpoint and vice versa.

## Core Domain Concepts

### Verification Model

Two independent verification paths, both required to become an admin:

1. **Postcard verification** — physical mail through Postgrid/Lob to a real
   address. Address is never persisted (see "Zero address storage" below). On
   success, the user gains read-only access in their region.
2. **Vouch verification** — two existing fully-verified users from a shared
   geographic ancestor vouch for a new user. Bootstrap mode (when a region has
   <3 admins) requires exact-region vouching.

A user with **both** verifications becomes a region admin: they can create
sub-regions, manage Signal groups, and vouch for others.

A **superuser** flag (`is_superuser`) is set directly in the database and
bypasses scoping for administrative tooling such as auditing, blocklists, and
emergency vouch grants.

### Geographic Hierarchy

```
State → County → City/Town → Locality (optional) → Neighborhood → City Block
```

- Regions are stored in `geographic_regions` with a nullable `GEOMETRY SRID 4326`
  column. ~92% of neighborhoods and some localities have no polygon — they are
  defined purely by `parent_id`.
- Sub-region containment is resolved using a combination of:
  - `ST_Contains` on regions with geometry, and
  - `parent_id` traversal for the rest.
- The spatial index on `geographic_regions.geometry` is intentionally disabled
  due to a MariaDB Error 1207 incompatibility with `ST_Contains`. Containment
  queries still work correctly without it.

### Three-Way XOR for Signal Groups

A Signal group belongs to **exactly one** of:

- a region (`region_id`), or
- a school (`school_id`), or
- a school district (`district_id`).

Enforced at the DB layer with a `CHECK` constraint. The same XOR shape applies
to Meshtastic channels.

### Consensus Governance

Critical mutations require 3-admin approval. The shape is consistent across all
proposals:

| Action                    | Proposal table                    | Vote table                    |
|---------------------------|-----------------------------------|-------------------------------|
| Asset deletion            | `deletion_proposals`              | `deletion_votes`              |
| Signal/Meshtastic secret update | `secret_update_proposals` (formerly `invite_link_update_proposals`) | `secret_update_votes` |
| User blocklisting         | `blocklist_proposals`             | `blocklist_votes`             |
| Sub-region join request   | `sub_region_membership_requests`  | `sub_region_membership_votes` |

Blocklisting requires a region to have ≥3 admins. Proposals expire after 7 days.

### Schools and School Districts

Schools (sourced from NCES) and school districts run in parallel to regions:

- Membership is vouch-based only (no postcard). Bootstrap mode (<3 verified
  admins) requires 3 vouches; otherwise 2.
- Both schools and districts can have their own Signal groups, scoped via the
  three-way XOR.

## Data Flow: Verification (Zero Address Storage)

```
[User] --address--> [API] -> [Postgrid/Lob validate]
                             |--reject PO Box/CMRA
                             v
                       [Mapbox geocode]
                             v
                  [Extract hierarchy: state→county→city→locality→neighborhood]
                             v
       [Upsert geographic_regions (with or without geometry)]
                             v
       [Create pending user_regions entries at each level]
                             v
              [Postgrid/Lob mails postcard with one-time code]
                             v
              [Address dropped from memory — never written to DB]

Later:
[User submits code] --> [API verifies] --> [user_regions activated]
```

The DB never sees a street address. Only city/county/state names, region
boundaries (from OpenStreetMap via Mapbox), and a Postgrid/Lob tracking ID are
persisted.

## Spatial Storage Notes

- Geometries are stored as `GEOMETRY SRID 4326` (WGS84 lon/lat). Polygons come
  from OSM via Mapbox; the application layer is responsible for ensuring valid
  polygon orientation and closure.
- All `ST_Contains` queries explicitly filter `WHERE geometry IS NOT NULL` so
  geometry-less regions do not produce spurious matches.
- UUIDs are `CHAR(36)` generated at the application layer (not via DB defaults)
  to keep migrations portable.

## Observability

- **Logging**: structured request logs (method, path, status, latency, IP) are
  emitted via `internal/logging`.
- **Sentry**: errors and panics are forwarded via the Sentry HTTP middleware
  when `SENTRY_DSN` is configured.
- **Audit log**: administrative actions are recorded in the `audit_log` table
  (90-day retention).

## Further Reading

- [DESIGN.md](../DESIGN.md) — full design specification.
- [docs/API.md](API.md) — REST API reference.
- [docs/DEPLOYMENT.md](DEPLOYMENT.md) — deployment, env vars, operations.
- [CLAUDE.md](../CLAUDE.md) — quick reference for AI assistants.
