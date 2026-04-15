# Technical Debt Inventory

Classified inventory of technical debt in the opencrr codebase. Generated 2026-04-15.

---

## Critical

### C1: Unverified Region Access in GetPublicKeysForRegion

| Field | Value |
|-------|-------|
| **Category** | Security |
| **Severity** | Critical |
| **File(s)** | `internal/database/encryption_keys.go:145-153` |
| **Effort** | S |

**Issue:** `GetPublicKeysForRegion` filters only on `u.vouch_verified = TRUE` (a global user flag) without checking per-region verification status (`ur.verification_status`). By contrast, `GetPublicKeysForSchool` (line 157) and `GetPublicKeysForDistrict` (line 168) correctly check scope-specific `us.verification_status = 'verified'`. A globally vouch-verified user who is not a verified member of a specific region could receive wrapped DEKs for that region's secrets.

**Fix:** Add a `JOIN user_regions ur ON ur.user_id = u.id AND ur.region_id = ?` with `ur.verification_status = 'verified'` to match the school/district pattern.

---

## High

### H1: Incorrect resolved_at Semantics in UpdateStatusTx

| Field | Value |
|-------|-------|
| **Category** | Correctness |
| **Severity** | High |
| **File(s)** | `internal/database/secret_update_proposals.go:136-162` |
| **Effort** | S |

**Issue:** `UpdateStatusTx` (line 136) and `UpdateStatus` (line 152) set `resolved_at` for all statuses except `approved_pending_finalization`. The code comment says "Only terminal statuses (approved, rejected, expired) set resolved_at," but the implementation inverts this: any non-`approved_pending_finalization` status (including `pending` or future non-terminal statuses) writes `resolved_at`.

**Fix:** Replace the single carve-out with an explicit check for terminal statuses:
```go
if status == models.ProposalStatusApproved || status == models.ProposalStatusRejected || status == models.ProposalStatusExpired {
    // set resolved_at
}
```

### H2: Missing Pagination on List() Query

| Field | Value |
|-------|-------|
| **Category** | Correctness |
| **Severity** | High |
| **File(s)** | `internal/database/secret_update_proposals.go:253-343` |
| **Effort** | S |

**Issue:** The `List` function builds a query with `ORDER BY p.created_at DESC` but applies no `LIMIT` or `OFFSET`. The `SecretProposalListFilter` struct (`internal/models/encrypted_secret.go:171-177`) has no pagination fields. As proposal volume grows, this will return unbounded result sets.

**Fix:** Add `Limit` and `Offset` fields to `SecretProposalListFilter` and append `LIMIT ? OFFSET ?` to the query. Default limit to a sensible value (e.g., 50).

### H3: Missing Validation Tags on Encrypted Secret Request Types

| Field | Value |
|-------|-------|
| **Category** | Correctness |
| **Severity** | High |
| **File(s)** | `internal/models/encrypted_secret.go:78-97` |
| **Effort** | S |

**Issue:** `CreateEncryptedSecretRequest` (line 78), `CreateSecretProposalRequest` (line 85), and `FinalizeSecretUpdateRequest` (line 93) have no `validate` struct tags on any field. Compare to `CreateEncryptionKeyRequest` (`internal/models/encryption.go:17-22`) and `CreateMeshtasticChannelRequest` (`internal/models/meshtastic.go:45-54`), which both use `validate:"required"`. Missing validation allows empty payloads to reach the database layer.

**Fix:** Add `validate:"required"` tags to all fields on these three request types.

---

## Medium

### M1: Create/CreateTx Code Duplication

| Field | Value |
|-------|-------|
| **Category** | Maintainability |
| **Severity** | Medium |
| **File(s)** | `internal/database/blocklist_proposals.go` (lines 38, 756), `internal/database/deletion_proposals.go` (lines 29, 449), `internal/database/encrypted_secrets.go` (lines 29, 64), `internal/database/meshtastic_channels.go` (lines 29, 56), `internal/database/password_reset.go` (lines 40, 139), `internal/database/school_vouches.go` (lines 25, 46) |
| **Effort** | M |

**Issue:** Six repository files duplicate their `Create` and `CreateTx` methods. The bodies are identical except for the executor (`r.db` vs `tx`). Any bug fix or field addition must be applied in both places.

**Fix:** Have `Create` delegate to `CreateTx` by wrapping in a transaction, as some repositories already do (e.g., `encrypted_secrets.go:Create` wraps `CreateTx` but still duplicates the query inline). Extract the shared logic so `Create` is a one-liner calling `CreateTx`.

### M2: Missing Test Coverage

| Field | Value |
|-------|-------|
| **Category** | Testing |
| **Severity** | Medium |
| **File(s)** | 36 production `.go` files (see below) |
| **Effort** | L |

**Issue:** 36 production files have no corresponding `_test.go` file. The largest untested files are:

| File | Lines |
|------|-------|
| `internal/database/regions.go` | 2,158 |
| `internal/handlers/router.go` | 1,285 |
| `internal/database/users.go` | 1,010 |
| `internal/database/signal_groups.go` | 502 |

Full list of untested packages:
- **internal/database/**: `audit.go`, `encrypted_secrets.go`, `encryption_keys.go`, `errors.go`, `meshtastic_channels.go`, `regions.go`, `school_districts.go`, `school_vouches.go`, `secret_update_proposals.go`, `signal_groups.go`, `user_reports.go`, `users.go`, `verification.go` (13 files)
- **internal/handlers/**: `router.go` (1 file)
- **internal/logging/**: `logging.go` (1 file)
- **internal/middleware/**: `request_context.go` (1 file)
- **internal/mocks/**: `services.go` (1 file)
- **internal/models/**: `auth.go`, `encrypted_secret.go`, `encryption.go`, `membership.go`, `meshtastic.go`, `notification.go`, `proposal.go`, `region.go`, `report.go`, `school.go`, `signal_group.go`, `user.go`, `user_status.go`, `verification.go` (14 files)
- **internal/services/**: `email_mock.go`, `email_sendgrid.go`, `interfaces.go`, `nces.go`, `notification_queue.go` (5 files)

**Fix:** Prioritize tests for the database and handler layers, starting with `regions.go` and `users.go` which contain the most complex query logic. Model files are lower priority as they are mostly struct definitions.

---

## Low

### L1: Schema Inconsistency — encrypted_secrets Missing created_at

| Field | Value |
|-------|-------|
| **Category** | Schema |
| **Severity** | Low |
| **File(s)** | `migrations/026_encrypted_secrets.up.sql:2-19`, `internal/models/encrypted_secret.go:14-23` |
| **Effort** | S |

**Issue:** The `encrypted_secrets` table has `updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP` but no `created_at` column. The child table `encrypted_secret_keys` does have `created_at`. This inconsistency makes it impossible to determine when a secret was first created without inspecting child key records.

**Fix:** Add a `created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP` column via migration, and add `CreatedAt time.Time` to the model.

### L2: MeshtasticChannel.CreatedBy Model/Schema Nullability Mismatch

| Field | Value |
|-------|-------|
| **Category** | Schema |
| **Severity** | Low |
| **File(s)** | `migrations/027_meshtastic_channels.up.sql:10`, `internal/models/meshtastic.go:16` |
| **Effort** | S |

**Issue:** The database schema defines `created_by CHAR(36) NOT NULL` but the Go model declares `CreatedBy *string` (nullable pointer). The mismatch means nil values could be set in application code but rejected at the database level, and the pointer indirection is unnecessary overhead for a non-nullable field.

**Fix:** Change the model field to `CreatedBy string` (non-pointer).

### L3: Duplicate Request Types — CreateEncryptionKeyRequest vs RotateEncryptionKeyRequest

| Field | Value |
|-------|-------|
| **Category** | Maintainability |
| **Severity** | Low |
| **File(s)** | `internal/models/encryption.go:17-22, 32-37` |
| **Effort** | S |

**Issue:** `CreateEncryptionKeyRequest` and `RotateEncryptionKeyRequest` are identical structs (same fields, same tags). The code comment on line 33 acknowledges this. Any field change must be made in both places.

**Fix:** Use a type alias (`type RotateEncryptionKeyRequest = CreateEncryptionKeyRequest`) or a shared base type. If the types need to diverge in the future, keep them separate but document why.

---

## Summary

### Counts by Severity

| Severity | Count |
|----------|-------|
| Critical | 1 |
| High | 3 |
| Medium | 2 |
| Low | 3 |
| **Total** | **9** |

### Counts by Category

| Category | Count |
|----------|-------|
| Security | 1 |
| Correctness | 3 |
| Maintainability | 2 |
| Testing | 1 |
| Schema | 2 |

### Recommended Remediation Order

1. **C1** (Security) — Fix region encryption key access check. Immediate security risk, small effort.
2. **H1** (Correctness) — Fix resolved_at semantics. Data integrity issue, small effort.
3. **H3** (Correctness) — Add validation tags. Allows empty payloads to bypass validation, small effort.
4. **H2** (Correctness) — Add pagination to List(). Unbounded queries, small effort.
5. **L2** (Schema) — Fix nullability mismatch. Quick model change, prevents subtle bugs.
6. **L1** (Schema) — Add created_at to encrypted_secrets. Schema consistency, small migration.
7. **L3** (Maintainability) — Deduplicate request types. Low risk, small effort.
8. **M1** (Maintainability) — Refactor Create/CreateTx pattern. Medium effort, improves maintainability across 6 files.
9. **M2** (Testing) — Add test coverage. Large effort, best tackled incrementally alongside feature work.
