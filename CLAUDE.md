# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Maintenance Instructions

**IMPORTANT**: When DESIGN.md is updated, automatically update this file to reflect those changes. Keep this file as a concise summary of DESIGN.md for quick reference.

## Project Overview

**Community Rapid Response** - a platform enabling people to connect within specific geographic regions (neighborhoods, city blocks, rural towns) and schools through verified Signal group chats. Residency is proven via physical postcard verification for regions; school membership uses vouch-based verification.

## Planned Technology Stack

- **Backend**: Go (net/http or Chi/Gin)
- **Database**: MariaDB with spatial extensions
- **Frontend**: Standard HTML/CSS/JavaScript (progressive enhancement)
- **Mapping**: Mapbox GL JS (map display), Mapbox Geocoding API (address lookup), Mapbox Draw (polygon creation)
- **Address & Mail Service**: Postgrid API (address validation, PO Box/CMRA rejection, postcard delivery)
- **Hosting**: Cloud VPS

## Architecture

### Core Concepts

- **User Verification Model**:
  - Unverified: No access to Signal groups
  - Postcard-verified only: Read-only access to Signal groups
  - Vouch-verified only: Read-only access to Signal groups (vouched by 2+ verified users)
  - **Admin (Both postcard AND vouch verified)**: Can create regions, manage Signal groups, vouch for others
  - **Superuser**: Global admin access to all regions. Must be created directly in the database (is_superuser flag). Can grant/revoke vouch verification for any user.
- **Geographic Hierarchy**: City Block → Neighborhood → Locality (optional) → City/Town → County → State
  - Locality is for boroughs/sub-city divisions (e.g., Brooklyn in NYC)
  - Most neighborhoods (~92%) and some localities lack polygon boundaries
- **Region Access Control**: All region endpoints require authentication; users see only regions they belong to (direct or via parent hierarchy); superusers see all regions
- **Sub-Region Containment**: Uses MariaDB `ST_Contains` for geographic containment (for regions with geometry) and parent_id traversal (for regions without geometry)
- **Admin Boundary Restriction**: Admins can only create sub-regions within regions they already have admin access to (validated via `ST_Contains`)
- **Zero Address Storage**: Addresses are NEVER stored in our database - processed in memory only, sent directly to Postgrid, then discarded
- **Consensus Governance**: 3-admin approval required for deletions, user blacklisting, and Signal group invite link updates
- **Schools & School Districts**: Parallel organizational entity to regions. Schools sourced from NCES data, verified via school-scoped vouches (no postcard). Bootstrap mode applies when <3 admins (3 vouches required). Schools and districts can each have Signal groups.
- **Three-Way XOR for Signal Groups**: `signal_groups.region_id` is nullable; each group belongs to exactly one of region/school/district (CHECK constraint enforced)

### Database Schema (MariaDB)

Key tables: `users`, `geographic_regions`, `user_regions`, `admin_boundaries`, `verification_requests`, `vouches`, `signal_groups`, `deletion_proposals`, `deletion_votes`, `secret_update_proposals`, `secret_update_votes`, `proposal_wrapped_keys`, `encrypted_secrets`, `encrypted_secret_keys`, `user_encryption_keys`, `blocklist_proposals`, `blocklist_votes`, `blocked_users`, `blocked_addresses`, `meshtastic_channels`, `password_reset_tokens`, `rate_limits`, `user_reports`, `email_notifications`, `audit_log`, `sub_region_membership_requests`, `sub_region_membership_votes`, `school_districts`, `schools`, `user_schools`, `school_vouches`, `school_blocked_users`

Geographic regions use MariaDB spatial `GEOMETRY SRID 4326` for boundary storage (nullable for regions without boundaries). UUIDs are `CHAR(36)` generated at the application layer.

**Region Types**: `state`, `county`, `city`, `locality`, `neighborhood`, `city_block`

**Nullable Geometry Note**: The geometry column is nullable. Most neighborhoods (~92%) and some localities lack OSM polygon boundaries. These regions are valid and defined by parent relationship. All `ST_Contains` queries filter out NULL geometry.

**Spatial Index Note**: The spatial index on `geographic_regions.geometry` is disabled due to MariaDB Error 1207 compatibility issues with `ST_Contains` queries. Sub-region containment queries work correctly without the index.

### Database Migrations

Migrations use an idempotent system with rollback support. Each migration has `.up.sql` (forward) and `.down.sql` (rollback) files tracked in `schema_migrations` table.

**Commands:**
```bash
just db-migrate              # Apply pending migrations (idempotent)
just db-migrate test         # Apply to test database
just db-migrate-down [env] [N]  # Rollback N migrations (default: dev, 1)
just db-migrate-status       # Show applied/pending migrations
```

**File naming:** `NNN_description.up.sql` / `NNN_description.down.sql`

### Map UI (Mapbox)

- **Region Browser**: View existing regions as color-coded polygons; click for details
- **Address Verification**: Mapbox Geocoding converts address to coordinates; identifies containing region for verification
- **Region Creation**: Admins use Mapbox Draw plugin to draw polygon boundaries; boundary overlay shows their admin regions; drawing constrained to regions where user has admin access
- **Hierarchy Navigation**: Breadcrumb navigation between city/neighborhood/block levels

### API Design

REST API with JWT authentication (24-hour expiration, httpOnly cookies). All authenticated endpoints require `Authorization: Bearer <token>` header.

Key endpoint groups:
- `/api/v1/health` - liveness probe (no auth)
- `/api/v1/auth/*` - registration, login, logout, email verification, password reset
  - POST `/api/v1/auth/register`, `/api/v1/auth/login`, `/api/v1/auth/logout`
  - GET `/api/v1/auth/verify-email`, `/api/v1/auth/validate-reset-token`
  - POST `/api/v1/auth/forgot-password`, `/api/v1/auth/reset-password`
  - POST `/api/v1/auth/change-password` (auth), `/api/v1/auth/resend-verification` (email-unverified token)
- `/api/v1/mfa/*` - MFA setup and verification
- `/api/v1/users/me` - current user info
  - GET `/api/v1/users/me/deletion-preflight` - preview account deletion impact
- `/api/v1/verification/*` - postcard and vouch verification
  - GET `/api/v1/verification/status` - current verification state
  - POST `/api/v1/verification/vouch/request` - request vouch verification for an address
  - GET `/api/v1/verification/vouch/pending` - list inbound vouch requests
- `/api/v1/communities/*` - geographic region management (all require authentication; handler routes use the `communities` prefix even though the domain still calls them regions)
  - GET `/api/v1/communities` - list regions (scoped by user membership; superusers see all)
  - GET `/api/v1/communities/admin` - list regions where the caller is an admin
  - GET `/api/v1/communities/:id` - get region with sub-regions (uses ST_Contains for geographic containment)
  - PUT `/api/v1/communities/:id` - update region name (admin/superuser)
  - DELETE `/api/v1/communities/:id` - delete region with cascade (superuser only)
- `/api/v1/signal-groups/*` - Signal group CRUD
  - GET `/api/v1/signal-groups/admin` - list signal groups for the caller's admin regions
  - POST `/api/v1/signal-groups/:id/secret-proposals` - propose invite-link/secret rotation (replaces the old invite-link-proposals route)
- `/api/v1/secret-proposals/*` - consensus-based Signal/Meshtastic secret rotation proposals (replaces invite-link-update-proposals)
  - GET `/api/v1/secret-proposals` - list proposals; GET `/api/v1/secret-proposals/:id` - proposal detail
  - POST `/api/v1/secret-proposals/:id/vote`, `/api/v1/secret-proposals/:id/expire`
- `/api/v1/encrypted-secrets/:id/finalize` - finalize a proposal's wrapped-key distribution
- `/api/v1/encryption/*` - per-user keypair management for end-to-end-encrypted secrets
  - GET/POST/PUT `/api/v1/encryption/keys`, POST `/api/v1/encryption/keys/rotate`
  - GET `/api/v1/encryption/public-keys`, `/api/v1/encryption/pending-rekeys`
  - POST `/api/v1/encryption/rekey`
- `/api/v1/meshtastic-channels/*` - Meshtastic channel CRUD (mirrors signal-groups)
  - GET/POST `/api/v1/meshtastic-channels`, PUT `/api/v1/meshtastic-channels/:id`
  - GET `/api/v1/meshtastic-channels/admin` - list channels for the caller's admin regions
- `/api/v1/deletion-proposals/*` - consensus-based asset deletion
  - GET/POST `/api/v1/deletion-proposals`, GET `/api/v1/deletion-proposals/:id`
  - POST `/api/v1/deletion-proposals/:id/vote`, `/api/v1/deletion-proposals/:id/expire`
- `/api/v1/blocklist-proposals/*` - user blocklisting (requires ≥3 admins in region; renamed from blacklist-proposals)
  - GET `/api/v1/blocklist-proposals`, GET `/api/v1/blocklist-proposals/:id`
  - POST `/api/v1/blocklist-proposals/:id/vote`, `/api/v1/blocklist-proposals/:id/expire`
  - POST `/api/v1/communities/:id/blocklist-proposals` - create proposal for a region
- `/api/v1/reports/*` - user reports created via region/school/district sub-routes; admins list and resolve them here
  - GET `/api/v1/reports`, GET `/api/v1/reports/:id`, POST `/api/v1/reports/:id/resolve`
  - POST `/api/v1/communities/:id/reports`, `/api/v1/schools/:id/reports`, `/api/v1/school-districts/:id/reports`
- `/api/v1/admin/users` - search users (superuser only)
- `/api/v1/admin/users/:id/grant-vouch` - grant vouch verification (superuser only)
- `/api/v1/admin/users/:id/revoke-vouch` - revoke vouch verification (superuser only)
- `/api/v1/admin/audit-logs` - read audit log entries (superuser only)
- `/api/v1/admin/audit-logs/export` - export audit log as CSV (superuser only)
- `/api/v1/admin/blocked-addresses` - list addresses blocked from verification (superuser only)
- `/api/v1/membership-requests/*` - sub-region membership management
  - POST `/api/v1/communities/:id/membership-requests` - request to join sub-region (verified users)
  - GET `/api/v1/membership-requests` - list user's own requests
  - GET `/api/v1/membership-requests/admin` - list pending requests for admin's regions
  - GET/DELETE `/api/v1/membership-requests/:id` - get/cancel request
  - POST `/api/v1/membership-requests/:id/vote` - vote on request (2+ approvals needed)
- `/api/v1/invitations/*` - admin invitations to sub-regions
  - POST `/api/v1/communities/:id/invitations` - invite user to sub-region (admin only)
  - GET `/api/v1/invitations` - list user's pending invitations
  - POST `/api/v1/invitations/:id/respond` - accept/decline invitation
- `/api/v1/schools/*` - school management
  - GET `/api/v1/schools` - search schools (auth)
  - GET `/api/v1/schools/my` - list user's schools (auth)
  - GET `/api/v1/schools/:id` - get school details (auth)
  - POST `/api/v1/schools/:id/join` - join a school (auth)
  - POST `/api/v1/schools/:id/leave` - leave a school (auth)
  - POST `/api/v1/schools/:id/vouch` - vouch for school member (auth)
  - GET `/api/v1/schools/:id/vouch/pending` - list pending vouch requests (auth)
  - GET `/api/v1/schools/:id/vouch-status/:user_id` - get vouch status (auth)
  - GET `/api/v1/schools/:id/members` - list school members (auth)
  - GET/POST `/api/v1/schools/:id/signal-groups` - list/create school Signal groups (auth/admin)
- `/api/v1/school-districts/*` - school district management
  - GET `/api/v1/school-districts` - search districts (auth)
  - GET `/api/v1/school-districts/:id` - get district details (auth)
  - GET/POST `/api/v1/school-districts/:id/signal-groups` - list/create district Signal groups (auth/admin)

**Region Type Hierarchy:**
- State regions cannot have a parent region
- County regions must have a State parent
- City/Town regions must have a County parent
- Locality regions must have a City parent
- Neighborhood regions must have a Locality OR City parent
- City Block regions must have a Neighborhood parent

## Key Flows

1. **Login with MFA**: User enters email/password → if MFA setup required: receive `mfa_setup` token → complete `/api/v1/mfa/setup` and `/api/v1/mfa/setup/complete` → receive full token. If MFA already enabled: receive `pending_mfa` token → verify TOTP via `/api/v1/mfa/verify` → receive full token.
2. **Postcard Verification**: User enters address (UI shows "address never stored") → Postgrid validates (rejects PO Boxes/CMRA) → Mapbox geocodes + extracts full hierarchy (state/county/city/locality/neighborhood) → region hierarchy created (with OSM boundaries where available) → user assigned to most specific region (neighborhood > locality > city) → postcard sent via Postgrid → address discarded from memory (NEVER written to DB) → user enters code → user gains read-only access (admin access requires vouch verification too)
3. **Vouch Verification Request**: User enters address → Mapbox geocodes → pending `user_regions` entries created at all hierarchy levels (neighborhood through state) → user connects with verified neighbors (out-of-band) → 2 different fully verified users from same or shared ancestor region (up to city/county, NOT state) vouch via app → system validates (10/month limit, no circular patterns, ancestor-level matching) → verification cascades upward when threshold met → user gains vouch-verified status (read-only access, or admin access if already postcard-verified). Bootstrap mode still requires exact-region vouching.
4. **Becoming an Admin**: User must complete BOTH postcard verification AND vouch verification → once both are complete, user gains full admin rights (create regions, manage groups, vouch for others)
5. **User Blacklisting**: Admin proposes blacklist (requires ≥3 admins in region) → 3 admins vote to approve → user access revoked, cascades to sub-regions
6. **Region Creation**: Admin (with both verifications) uses Mapbox Draw to create polygon → validated to ensure polygon is within a region where user has admin access (MariaDB `ST_Contains`) → type/parent hierarchy enforced (city→neighborhood→block) → stored in MariaDB
7. **Region Deletion**: Superuser only → cascade deletes user_regions, signal_groups, and child regions
8. **Signal Group Invite Update**: Admin proposes new invite link (requires ≥3 admins in region) → other admins vote → on 3rd approval: link updated + email notifications queued for all verified users → email says "log in to view new link" (link NOT included in email for security) → rate limited to 1 notification per user per group per 24h
9. **Superuser Operations**: Superuser (created in DB with is_superuser=true) can search for any user → grant or revoke vouch verification → this bypasses the normal vouch process for bootstrapping or special cases
10. **Sub-Region Membership (Self-Selection)**: Verified user requests to join admin-created sub-region (city block, custom neighborhood) → validates user is in parent region → request created with 7-day expiration → 2+ region admins vote to approve → on approval: user added to region (with admin flag if fully verified)
11. **Sub-Region Membership (Admin Invitation)**: Admin invites user to sub-region → validates user is in parent region → invitation created with 7-day expiration → user accepts/declines → on accept: user added to region
12. **School Membership & Verification**: User searches NCES schools → joins school (pending membership) → gets vouched by verified school members (3 in bootstrap, 2 normally) → on verification: gains access to school Signal groups; becomes admin if enough vouches
13. **School Signal Groups**: School admins create Signal groups for their school → verified members can view invite links → district admins can create district-level groups

## Privacy & Security Requirements

**Zero Address Storage Policy:**
- Addresses exist only in server memory during verification request
- Address sent directly to Postgrid for mailing, then discarded
- Database never contains street addresses, apartment numbers, or specific coordinates
- UI explicitly communicates: "Your address is never stored in our system"
- No geographic data retained from verification process

**Security:**
- **MFA**: TOTP-based MFA required in production; can be disabled for development with `MFA_REQUIRED=false`
- **MFA Token Types**: `full` (24h), `mfa_setup` (10min), `pending_mfa` (10min) - prevents token misuse
- **MFA Secret Encryption**: TOTP secrets encrypted with AES-256-GCM before database storage
- **Encryption at Rest**: All stored data encrypted (AES-256) via MariaDB data-at-rest encryption or filesystem-level
- **Address Validation**: PO Boxes and CMRA addresses rejected; only residential/business street addresses accepted
- **Email Security**: Sensitive data (invite links, verification codes, addresses) NEVER included in emails
- Bcrypt password hashing (cost factor 12), minimum 12 characters
- Rate limiting: 3 verification requests/30 days, 10 vouches/month, 5 blacklist proposals/month, 100 API requests/min/IP
- **Auth rate limiting** (per IP): login 10/5min, register 3/hr, forgot-password 5/15min, reset-password 10/15min, resend-verification 3/15min
- **Account lockout**: 10 failed login attempts → 15-minute lock; resets on successful login; audit-logged
- Audit logging for all admin actions (90-day retention)
- Only fully verified users (both postcard AND vouch) can vouch for others
- Blacklisting requires minimum 3 admins in region (prevents abuse)

## Reference

See DESIGN.md for complete specifications including database schema, API contracts, and implementation phases.
