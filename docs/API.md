# REST API Reference

The Community Rapid Response API is a JSON-over-HTTPS API. This document is a
reference for the endpoints surfaced by [`internal/handlers/router.go`](../internal/handlers/router.go).
For example payloads and request flows, see [DESIGN.md](../DESIGN.md). For a higher
level overview of the system, see [ARCHITECTURE.md](ARCHITECTURE.md).

All endpoints are versioned under `/api/v1/`.

## Authentication

Authentication is JWT-based. On successful login the API issues a token of one of
the following types:

| Token type           | Lifetime  | Used for                                         |
|----------------------|-----------|--------------------------------------------------|
| `full`               | 24 hours  | All authenticated endpoints                      |
| `mfa_setup`          | 10 minutes| `/api/v1/mfa/setup`, `/api/v1/mfa/setup/complete`|
| `pending_mfa`        | 10 minutes| `/api/v1/mfa/verify`                             |
| `email_unverified`   | 10 minutes| `/api/v1/auth/resend-verification`               |

Tokens may be supplied either as:

- An `Authorization: Bearer <token>` header, or
- An HttpOnly cookie (set automatically by the API on login).

A token of the wrong type for an endpoint is rejected with `401 Unauthorized`.

### CSRF and CORS

- CORS allowed origins are configured via `CORS_ALLOWED_ORIGINS`.
- CSRF protection is enabled when `CSRF_ENABLED=true`. State-changing requests
  (`POST`/`PUT`/`PATCH`/`DELETE`) require a CSRF token issued via the standard
  double-submit cookie pattern.

### Rate Limiting

Global per-IP rate limiting is applied when `RATE_LIMIT_ENABLED=true` (default
100 req/min/IP). Auth endpoints have tighter, dedicated limits:

| Endpoint                              | Limit              |
|---------------------------------------|--------------------|
| `POST /api/v1/auth/login`             | 10 / 5 min / IP    |
| `POST /api/v1/auth/register`          | 3 / hr / IP        |
| `POST /api/v1/auth/forgot-password`   | 5 / 15 min / IP    |
| `POST /api/v1/auth/reset-password`    | 10 / 15 min / IP   |
| `POST /api/v1/auth/resend-verification` | 3 / 15 min / IP  |

Application-level rate limits also apply to verification (3/30d/user), vouches
(10/month/user), and blocklist proposals (5/month/user).

## Endpoint Index

### Health

| Method | Path                  | Auth | Notes                  |
|--------|-----------------------|------|------------------------|
| GET    | `/health`             | none | Health check           |
| GET    | `/api/v1/health`      | none | Health check (aliased) |

### Auth

| Method | Path                                          | Auth              | Notes                                                |
|--------|-----------------------------------------------|-------------------|------------------------------------------------------|
| POST   | `/api/v1/auth/register`                       | none              | Create account                                       |
| POST   | `/api/v1/auth/login`                          | none              | Returns `full`, `mfa_setup`, or `pending_mfa` token  |
| POST   | `/api/v1/auth/logout`                         | none              | Clears auth cookie                                   |
| GET    | `/api/v1/auth/verify-email`                   | none              | Verify email via emailed link                        |
| POST   | `/api/v1/auth/forgot-password`                | none              | Initiate password reset                              |
| POST   | `/api/v1/auth/reset-password`                 | none              | Complete password reset                              |
| GET    | `/api/v1/auth/validate-reset-token`           | none              | Check whether a reset token is still valid           |
| POST   | `/api/v1/auth/change-password`                | `full`            | Change password while logged in                      |
| POST   | `/api/v1/auth/resend-verification`            | `email_unverified`| Resend verification email                            |

### MFA

| Method | Path                                | Auth          | Notes                                              |
|--------|-------------------------------------|---------------|----------------------------------------------------|
| POST   | `/api/v1/mfa/setup`                 | `mfa_setup`   | Returns QR code + provisioning URI                 |
| POST   | `/api/v1/mfa/setup/complete`        | `mfa_setup`   | Confirms TOTP code, returns backup codes + `full`  |
| POST   | `/api/v1/mfa/verify`                | `pending_mfa` | Verifies TOTP or backup code, returns `full` token |

### Current User

| Method | Path                                          | Auth   | Notes                                |
|--------|-----------------------------------------------|--------|--------------------------------------|
| GET    | `/api/v1/users/me`                            | `full` | Get current user                     |
| DELETE | `/api/v1/users/me`                            | `full` | Delete own account                   |
| GET    | `/api/v1/users/me/deletion-preflight`         | `full` | Check what cascades on delete        |

### Verification

| Method | Path                                          | Auth   | Notes                                          |
|--------|-----------------------------------------------|--------|------------------------------------------------|
| GET    | `/api/v1/verification/status`                 | `full` | Postcard + vouch status                        |
| POST   | `/api/v1/verification/postcard/request`       | `full` | Submit address; address is never stored        |
| POST   | `/api/v1/verification/postcard/verify`        | `full` | Submit one-time postcard code                  |
| POST   | `/api/v1/verification/vouch/request`          | `full` | Open a vouch request for the current user      |
| POST   | `/api/v1/verification/vouch`                  | `full` | Vouch for another user                         |
| GET    | `/api/v1/verification/vouch/pending`          | `full` | List pending vouch requests for current user   |
| GET    | `/api/v1/verification/vouch/status/:user_id`  | `full` | Get vouch status for a target user             |

### Regions (Communities)

Note: the public API uses `/api/v1/communities/...`. Internally these are called
"regions" and back the same data model.

| Method | Path                                                   | Auth   | Notes                                       |
|--------|--------------------------------------------------------|--------|---------------------------------------------|
| GET    | `/api/v1/communities`                                  | `full` | List regions scoped to user membership      |
| POST   | `/api/v1/communities`                                  | `full` | Create region (admin)                       |
| GET    | `/api/v1/communities/admin`                            | `full` | List regions the user administers           |
| GET    | `/api/v1/communities/:id`                              | `full` | Region detail + sub-regions via `ST_Contains` |
| PUT    | `/api/v1/communities/:id`                              | `full` | Rename (admin/superuser)                    |
| DELETE | `/api/v1/communities/:id`                              | `full` | Cascade delete (superuser only)             |
| GET    | `/api/v1/communities/:id/members`                      | `full` | Members (email-stripped)                    |
| GET    | `/api/v1/communities/:id/users`                        | `full` | Member detail for admins                    |
| POST   | `/api/v1/communities/:id/membership-requests`          | `full` | Request to join                             |
| GET    | `/api/v1/communities/:id/membership-requests`          | `full` | List pending requests (admin)               |
| POST   | `/api/v1/communities/:id/invitations`                  | `full` | Invite user (admin)                         |
| POST   | `/api/v1/communities/:id/blocklist-proposals`          | `full` | Open blocklist proposal (≥3 admins required)|
| POST   | `/api/v1/communities/:id/reports`                      | `full` | Report user (scope = region)                |

### Membership Requests

| Method | Path                                              | Auth   | Notes                          |
|--------|---------------------------------------------------|--------|--------------------------------|
| GET    | `/api/v1/membership-requests`                     | `full` | List own requests              |
| GET    | `/api/v1/membership-requests/admin`               | `full` | List all pending (admin view)  |
| GET    | `/api/v1/membership-requests/:id`                 | `full` | Detail                         |
| DELETE | `/api/v1/membership-requests/:id`                 | `full` | Cancel                         |
| POST   | `/api/v1/membership-requests/:id/vote`            | `full` | Vote (≥2 approvals to accept)  |

### Invitations

| Method | Path                                         | Auth   | Notes                |
|--------|----------------------------------------------|--------|----------------------|
| GET    | `/api/v1/invitations`                        | `full` | List own invitations |
| POST   | `/api/v1/invitations/:id/respond`            | `full` | Accept or decline    |

### Signal Groups

A Signal group belongs to exactly one of a region, a school, or a school district
(three-way XOR).

| Method | Path                                              | Auth   | Notes                                |
|--------|---------------------------------------------------|--------|--------------------------------------|
| GET    | `/api/v1/signal-groups`                           | `full` | List visible Signal groups           |
| POST   | `/api/v1/signal-groups`                           | `full` | Create (admin)                       |
| GET    | `/api/v1/signal-groups/admin`                     | `full` | Groups the user administers          |
| PUT    | `/api/v1/signal-groups/:id`                       | `full` | Update group metadata                |
| POST   | `/api/v1/signal-groups/:id/secret-proposals`      | `full` | Propose a new invite link/secret     |

### Secret-Update Proposals (Invite Links, etc.)

3-admin consensus, with end-to-end encrypted secret payloads finalized after
approval.

| Method | Path                                                | Auth   | Notes                                 |
|--------|-----------------------------------------------------|--------|---------------------------------------|
| GET    | `/api/v1/secret-proposals`                          | `full` | List proposals visible to the user    |
| GET    | `/api/v1/secret-proposals/:id`                      | `full` | Proposal detail                       |
| POST   | `/api/v1/secret-proposals/:id/vote`                 | `full` | Vote on a proposal                    |
| POST   | `/api/v1/secret-proposals/:id/expire`               | `full` | Manually expire (superuser only)      |
| POST   | `/api/v1/encrypted-secrets/:id/finalize`            | `full` | Finalize an encrypted secret payload  |

### Encryption (E2E Key Management)

| Method | Path                                              | Auth   | Notes                                  |
|--------|---------------------------------------------------|--------|----------------------------------------|
| GET    | `/api/v1/encryption/keys`                         | `full` | Retrieve user key material             |
| POST   | `/api/v1/encryption/keys`                         | `full` | Upload key material                    |
| PUT    | `/api/v1/encryption/keys`                         | `full` | Update key material                    |
| POST   | `/api/v1/encryption/keys/rotate`                  | `full` | Rotate user keys                       |
| GET    | `/api/v1/encryption/public-keys`                  | `full` | Lookup public keys for a set of users  |
| GET    | `/api/v1/encryption/pending-rekeys`                | `full` | List pending rekey requests            |
| POST   | `/api/v1/encryption/rekey`                        | `full` | Submit rekey payloads                  |

### Deletion Proposals (Consensus Delete)

| Method | Path                                              | Auth   | Notes              |
|--------|---------------------------------------------------|--------|--------------------|
| GET    | `/api/v1/deletion-proposals`                      | `full` | List proposals     |
| POST   | `/api/v1/deletion-proposals`                      | `full` | Create proposal    |
| GET    | `/api/v1/deletion-proposals/:id`                  | `full` | Proposal detail    |
| POST   | `/api/v1/deletion-proposals/:id/vote`             | `full` | Vote               |
| POST   | `/api/v1/deletion-proposals/:id/expire`           | `full` | Force-expire       |

### Blocklist Proposals

| Method | Path                                              | Auth   | Notes                          |
|--------|---------------------------------------------------|--------|--------------------------------|
| GET    | `/api/v1/blocklist-proposals`                     | `full` | List proposals                 |
| GET    | `/api/v1/blocklist-proposals/:id`                 | `full` | Detail                         |
| POST   | `/api/v1/blocklist-proposals/:id/vote`            | `full` | Vote                           |
| POST   | `/api/v1/blocklist-proposals/:id/expire`          | `full` | Force-expire (superuser only)  |

### User Reports

| Method | Path                                              | Auth   | Notes                       |
|--------|---------------------------------------------------|--------|-----------------------------|
| GET    | `/api/v1/reports`                                 | `full` | List reports                |
| GET    | `/api/v1/reports/:id`                             | `full` | Detail                      |
| POST   | `/api/v1/reports/:id/resolve`                     | `full` | Resolve report              |

### Schools

| Method | Path                                                  | Auth   | Notes                                |
|--------|-------------------------------------------------------|--------|--------------------------------------|
| GET    | `/api/v1/schools`                                     | `full` | Search by name/state                 |
| GET    | `/api/v1/schools/my`                                  | `full` | List user's schools                  |
| GET    | `/api/v1/schools/:id`                                 | `full` | School detail                        |
| POST   | `/api/v1/schools/:id/join`                            | `full` | Join (pending membership)            |
| POST   | `/api/v1/schools/:id/leave`                           | `full` | Leave                                |
| POST   | `/api/v1/schools/:id/vouch`                           | `full` | Vouch for a school member            |
| GET    | `/api/v1/schools/:id/vouch/pending`                   | `full` | List pending vouch requests          |
| GET    | `/api/v1/schools/:id/vouch-status/:user_id`           | `full` | Get vouch status for a user          |
| GET    | `/api/v1/schools/:id/members`                         | `full` | List members                         |
| GET    | `/api/v1/schools/:id/signal-groups`                   | `full` | List school Signal groups            |
| POST   | `/api/v1/schools/:id/signal-groups`                   | `full` | Create (school admin)                |
| POST   | `/api/v1/schools/:id/reports`                         | `full` | Report user (scope = school)         |

### School Districts

| Method | Path                                                       | Auth   | Notes                              |
|--------|------------------------------------------------------------|--------|------------------------------------|
| GET    | `/api/v1/school-districts`                                 | `full` | Search                             |
| GET    | `/api/v1/school-districts/:id`                             | `full` | Detail                             |
| GET    | `/api/v1/school-districts/:id/members`                     | `full` | District-level members             |
| GET    | `/api/v1/school-districts/:id/signal-groups`               | `full` | List district Signal groups        |
| POST   | `/api/v1/school-districts/:id/signal-groups`               | `full` | Create (district admin)            |
| POST   | `/api/v1/school-districts/:id/reports`                     | `full` | Report user (scope = district)     |

### Meshtastic Channels

Same shape as Signal groups, with three-way XOR ownership.

| Method | Path                                                       | Auth   | Notes                                 |
|--------|------------------------------------------------------------|--------|---------------------------------------|
| GET    | `/api/v1/meshtastic-channels`                              | `full` | List                                  |
| POST   | `/api/v1/meshtastic-channels`                              | `full` | Create                                |
| GET    | `/api/v1/meshtastic-channels/admin`                        | `full` | Channels the user administers         |
| PUT    | `/api/v1/meshtastic-channels/:id`                          | `full` | Update                                |
| POST   | `/api/v1/meshtastic-channels/:id/secret-proposals`         | `full` | Propose a new channel secret          |

### Admin (Superuser)

| Method | Path                                                  | Auth   | Notes                              |
|--------|-------------------------------------------------------|--------|------------------------------------|
| GET    | `/api/v1/admin/users`                                 | `full` | Search users                       |
| GET    | `/api/v1/admin/users/:id`                             | `full` | Get user                           |
| DELETE | `/api/v1/admin/users/:id`                             | `full` | Delete user                        |
| POST   | `/api/v1/admin/users/:id/grant-vouch`                 | `full` | Grant vouch verification           |
| POST   | `/api/v1/admin/users/:id/revoke-vouch`                | `full` | Revoke vouch verification          |
| POST   | `/api/v1/admin/users/:id/block`                       | `full` | Block user                         |
| POST   | `/api/v1/admin/users/:id/unblock`                     | `full` | Unblock user                       |
| GET    | `/api/v1/admin/audit-logs`                            | `full` | List audit log entries             |
| GET    | `/api/v1/admin/audit-logs/export`                     | `full` | Export audit log as CSV/JSON       |
| GET    | `/api/v1/admin/blocked-addresses`                     | `full` | List blocked address hashes        |
| POST   | `/api/v1/admin/blocked-addresses/:hash/expire`        | `full` | Expire a blocked address hash      |

## Errors

Errors are returned as JSON of the shape:

```json
{ "code": "snake_case_code", "message": "Human readable description" }
```

Standard error codes include `unauthorized`, `forbidden`, `not_found`,
`invalid_id`, `missing_id`, `method_not_allowed`, `rate_limited`, `validation_failed`.

## Source of Truth

The router is the canonical list of endpoints. If this document diverges, trust
[`internal/handlers/router.go`](../internal/handlers/router.go). DESIGN.md is the
canonical source for request/response payload shapes and business rules.
