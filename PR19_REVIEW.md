# PR 19 Review: E2E Encryption, Meshtastic Channels, Secret Update Proposals

## Migration 025 (`user_encryption_keys`)

- **Minor**: `key_salt CHAR(24)` and `key_iv CHAR(16)` are fixed-length for base64-encoded values. `VARCHAR(32)` would be more resilient to encoding/length changes.
- Otherwise clean. One table, one FK with CASCADE.

## Migration 026 (`encrypted_secrets` + proposals)

- **Major**: `TRUNCATE TABLE signal_groups` (lines 95-97) wipes all signal group data. Even with the "no production data" comment, this is dangerous in a migration. If it runs against a populated database, data is destroyed silently. Consider removing it (the `DROP COLUMN IF EXISTS` statements handle the schema changes) or making it explicit in the migration filename.
- **Medium**: `secret_update_proposals` has `region_id`, `school_id`, `district_id` nullable columns but **no 3-way XOR CHECK constraint**. `signal_groups` and `meshtastic_channels` both enforce this at the schema level. Inconsistent — only application code guards this.
- **Minor**: `encrypted_secrets` has `updated_at` but no `created_at`. Inconsistent with other tables.
- **Minor**: `resolved_at` vs `finalized_at` on `secret_update_proposals` — semantic distinction unclear from schema alone. A comment would help.
- Down migration is well-structured — drops in correct dependency order and restores old invite_link schema.

## Migration 027 (`meshtastic_channels`)

- Clean. 3-way XOR CHECK constraint correctly applied. FK to `encrypted_secrets` added via ALTER (necessary ordering). `is_active` soft-delete consistent with `signal_groups`.

## Models

### `encryption.go` — UserEncryptionKey and request/response types

- **Medium**: `CreateEncryptionKeyRequest` and `RotateEncryptionKeyRequest` are structurally identical (same 4 fields). Consider consolidating into one type, or add a comment explaining the semantic difference that warrants two types.
- **Minor**: No `validate:` struct tags on any encryption request types (`CreateEncryptionKeyRequest`, `UpdateEncryptionKeyRequest`, `RotateEncryptionKeyRequest`). All other request types in the codebase (e.g., `CreateSignalGroupRequest`, `CreateMeshtasticChannelRequest`) use `validate:"required"` tags. The handler must be doing its own validation, but this is inconsistent.
- `WrappedPrivateKey` is exposed in JSON via `json:"wrapped_private_key"`. This is fine — it's encrypted client-side — but worth noting that the full key backup is returned to any authenticated request for the user's own key.

### `encrypted_secret.go` — EncryptedSecret, proposals, votes

- Well-structured. Clean separation between DB models, request types, and response types.
- `ProposalStatusApprovedPendingFinalization` is defined at line 180 as a standalone const, separate from the other `ProposalStatus` constants (which live in `proposal.go`). Should be co-located with the others for discoverability.
- `SecretProposalListFilter` has string fields for `Status`, `EncryptedSecretID`, etc. with no pointer types — empty string is the "not set" sentinel. This is fine but different from `ProposalListFilter` which also uses bare strings.
- Good use of `omitempty` on nullable/optional fields.
- The two-phase flow (approved → finalized) is modeled clearly with `ResolvedAt` and `FinalizedAt` — resolves the schema-level ambiguity noted in migrations.

### `meshtastic.go` — MeshtasticChannel and related types

- Clean. Follows the exact same patterns as `SignalGroup` (Public wrapper, WithSecret embedding, Create/Update request types).
- `MeshtasticChannel.CreatedBy` is `*string` (nullable) while the DB schema has `created_by CHAR(36) NOT NULL`. Mismatch — the model allows nil but the database doesn't.
- `MeshtasticChannelPublic` uses `Name` for the JSON key (`json:"name"`) while the DB model uses `ChannelName` (`db:"channel_name"`). This is a deliberate API-friendliness decision but could cause confusion when debugging query results vs API responses.

### `signal_group.go` — Changes

- `invite_link` fields cleanly removed. `SignalGroupWithSecret` replaces the old model with encrypted secret embedding.
- `CreateSignalGroupRequest` now requires `EncryptedPayload`, `EncryptionIV`, and `WrappedKeys` — enforcing encryption at the API level.
- `UpdateSignalGroupRequest` correctly only allows name/description changes (not secret updates, which require consensus).

### `notification.go` — New constants

- `NotificationTypeRekeyingNeeded` added for rekey email notifications.
- New audit action constants for encryption key rotation, secret rekeying, and meshtastic channel CRUD. Well-organized.

### `school.go` — Changes

- `CreateSchoolSignalGroupRequest` updated to require encryption fields. Consistent with `CreateSignalGroupRequest`.

### Model Tests

- Comprehensive — 30+ new test functions covering JSON serialization round-trips for all new types.
- Tests verify `omitempty` behavior (nil fields omitted from JSON).
- Tests cover both signal group and meshtastic channel variants of `EncryptedSecret`.
- Good coverage of the new proposal types and their complex response structures.

## Database Repositories

### `encryption_keys.go` — EncryptionKeyRepository

- **Medium**: `GetPublicKeysForRegion` (line 145) has **no verification status filter** on `user_regions`. It joins on `user_regions` without checking vouch/postcard status. The school (`GetPublicKeysForSchool`) and district (`GetPublicKeysForDistrict`) variants correctly filter by `us.verification_status = 'verified'`. This means unverified region members could have their public keys included when wrapping DEKs — they'd receive wrapped keys granting them access to secrets they shouldn't be able to decrypt.
- `Create` uses `ON DUPLICATE KEY UPDATE` (line 34) — upsert semantics for idempotency. This means calling Create twice overwrites the keypair silently. Fine for the use case, but worth noting that key rotation via `Create` won't update `created_at` (it's excluded from the UPDATE clause).
- `GetPublicKeysByUserIDs` builds the IN clause manually with string concatenation (lines 88-97). This is correct and safe (parameterized) but could use a utility function to reduce repetition — this pattern appears elsewhere in the codebase.
- Good empty-slice initialization pattern at lines 117-119 (avoids returning `null` in JSON).

### `encrypted_secrets.go` — EncryptedSecretRepository

- **Minor**: `Create` and `CreateTx` (lines 29-94) are nearly identical — the only difference is `Create` wraps in `r.db.Transaction` while `CreateTx` takes an existing `*sql.Tx`. This is a reasonable pattern for supporting both standalone and nested-transaction usage, but the duplicated logic is a maintenance risk. Could extract a shared `insertSecret` helper.
- **Medium**: `SubmitRekey` (line 228) doesn't verify that the calling user actually has a valid (non-rekey-needed) key for the secret. The authorization check must be in the handler — but a missing check there would allow any user to overwrite another user's wrapped DEK. Verify the handler enforces this.
- `UpdatePayloadAndKeys` (line 154) deletes all existing keys and re-inserts — this is a clean atomic approach but means any user who had a key and isn't in the new `wrappedKeys` list silently loses access. This is presumably intentional (re-keying after a compromise), but no soft-delete or audit trail of removed access.
- `GetPendingRekeys` (line 197) is a well-designed query — it finds secrets where someone needs re-keying AND the calling user has a valid key to re-wrap from. Good use of self-join on `encrypted_secret_keys`.

### `secret_update_proposals.go` — SecretUpdateProposalRepository

- **Medium**: `GetPendingBySecretForUpdate` (line 93) compares error strings: `err.Error() == "proposal not found"` (line 103). This is fragile — should use a sentinel error (like `ErrDeletionProposalNotFound`) instead of string comparison. If the error message in `scanProposal` changes, this silently breaks.
- **Medium**: `UpdateStatusTx` (line 137) always sets `resolved_at` when updating status, regardless of what the new status is. This means setting status to `approved_pending_finalization` also sets `resolved_at`, which seems semantically wrong — the proposal isn't "resolved" yet, it's awaiting finalization. `resolved_at` should probably only be set for terminal statuses (approved, rejected, expired).
- `List` (line 241) has a complex non-superuser query with three LEFT JOINs to check admin access across region/school/district scopes. The query is correct but could be slow on large datasets — no LIMIT/pagination. The superuser path also lacks pagination.
- `ExpirePendingProposals` (line 335) correctly expires both `pending` and `approved_pending_finalization` proposals.
- `StartExpirationWorker` follows the established pattern with Sentry monitoring integration. Runs immediately on start, then on interval.
- `scanProposal` returns `errors.New("proposal not found")` — ad-hoc error instead of a package-level sentinel. Inconsistent with `ErrEncryptedSecretNotFound`, `ErrMeshtasticChannelNotFound`, etc.

### `meshtastic_channels.go` — MeshtasticChannelRepository

- Clean and well-structured. Follows the same patterns as `SignalGroupRepository`.
- `ListByUser` (line 159) uses a 3-way UNION for region/school/district scopes — same approach as signal groups. Region scope has no verification filter (mirrors the concern noted in `encryption_keys.go` for `GetPublicKeysForRegion`).
- `ListByAdminUser` (line 229) uses recursive CTE for region admin hierarchy traversal — consistent with `SignalGroupRepository.ListByAdminUser`.
- `Create` and `CreateChannelTx` are duplicated (same pattern as `EncryptedSecretRepository.Create`/`CreateTx`). Same maintenance concern about duplicated logic.
- `Update` (line 309) builds dynamic SET clause for partial updates — correct approach, matches `SignalGroupRepository.Update`.
- Count methods (lines 347-393) provide both regular and `ForUpdate` variants for all three scopes — consistent with signal groups.

### `signal_groups.go` — Changes

- Massively simplified — 675 lines deleted. All invite_link-related queries, proposal management, and vote counting removed. The repository now only handles basic CRUD, listing, and counting.
- `Create` no longer deals with `invite_link` at all — encryption is handled through `EncryptedSecretRepository`.
- `CreateGroupTx` added for transactional signal group creation (used when creating group + encrypted secret atomically).
- `ListByUser` and `ListByAdminUser` are the same complex UNION queries as before, just without invite_link columns.
- Query patterns are very consistent with the new `MeshtasticChannelRepository` — clearly copy-paste adapted, which is fine for consistency but means bugs in one will likely be in both.

### `deletion_proposals.go` — Changes

- Minimal: comment update and `invite_link_update_proposals` → `secret_update_proposals` table reference in `ApplySubRegionDeletionTx`. Correct and complete.

## Handlers

### `encryption.go` — EncryptionHandler

- **Critical**: `SubmitRekeys` (line 253) has **no authorization check** beyond authentication. Any authenticated user can call `SubmitRekey` for **any** secret/target_user combination. The repo's `SubmitRekey` blindly updates the wrapped DEK. A malicious user could overwrite another user's wrapped DEK with garbage, effectively locking them out of the secret. Should verify the caller has a valid (non-rekey-needed) key for the secret before allowing re-key submission.
- **Medium**: `GetPublicKeys` (line 173) enforces the 3-way XOR scope validation in the handler, which is good. But it does **not** verify the caller is a member of the scope they're querying. Any authenticated user can fetch public keys for any region/school/district. This leaks membership information (who has encryption keys in a given scope).
- `UploadKeys` uses `Create` which has upsert semantics — calling it twice replaces the keypair. This is documented in the repo but the handler doesn't distinguish between initial setup and re-upload.
- `RotateKeys` (line 128) correctly flags rekey after key rotation, and logs-but-doesn't-fail if the rekey flagging errors.
- Good nil-check on `encryptedSecretRepo` in `GetPendingRekeys` and `SubmitRekeys` for graceful degradation.

### `meshtastic.go` — MeshtasticHandler

- Well-structured with scope-aware admin checks for create/update operations.
- `createRegionChannel` correctly checks bootstrap mode (line 113) — channels can't be created in bootstrap regions.
- `createSchoolChannel` does **not** check bootstrap mode for schools, which is consistent with how school Signal groups work.
- `createChannelWithSecret` (line 257) uses transactional limit check + create atomically — good pattern to prevent race conditions on the 5-channel-per-scope limit.
- **Minor**: Non-transactional fallback (line 292) creates channel then secret in two separate operations — if the secret creation fails, an orphaned channel is left behind. The transactional path handles this correctly.
- **Minor**: `List` (line 314) and `ListAdmin` (line 381) duplicate the response construction loop for converting channels to `MeshtasticChannelWithSecret`. Could extract a helper.
- N+1 query pattern in `List`/`ListAdmin`: for each channel, makes 2 additional queries (`GetByMeshtasticChannelID` + `GetWrappedDEK`). Fine for small channel counts per scope (max 5), but would be a concern if the limit were higher.
- District admin check (line 207) iterates all schools in the district via `GetUserSchool` per school — N+1 pattern. Same pattern used in `Update` (line 479) and in `SecretUpdateHandler`. Consider a `IsDistrictAdmin` repo method.

### `secret_updates.go` — SecretUpdateHandler

- **Critical**: `Finalize` (line 274) has **no authorization check** beyond authentication. Any authenticated user can finalize any secret update — no verification that they are an admin of the relevant scope, or that an approved proposal exists for this secret. The handler directly calls `UpdatePayloadAndKeys` which replaces the encrypted payload and all wrapped keys. A malicious user could replace a secret's content with arbitrary data. Must verify: (1) caller is a scope admin, (2) an `approved_pending_finalization` proposal exists for this secret.
- **Medium**: `Finalize` doesn't mark the proposal as finalized. After `UpdatePayloadAndKeys` succeeds, the proposal stays in `approved_pending_finalization` status forever. The `MarkFinalized` repo method exists but is never called.
- **Medium**: `GetProposal` (line 316) uses `consensusConfig.VoteFloor` for `VotesNeeded` calculation (line 336-337) instead of the actual admin count for the scope. This means the displayed "votes needed" may be wrong if the scope has more admins than the floor.
- `CreateProposal` correctly checks for existing pending proposals within a transaction and auto-votes for the proposer.
- `Vote` uses `FOR UPDATE` locking to prevent race conditions in concurrent voting — good.
- `ExpireProposal` correctly allows superusers to expire both `pending` and `approved_pending_finalization` proposals.
- `resolveGroupScope` (line 436) runs a raw SQL query directly in the handler instead of using the signal group repository. This breaks the repository pattern.
- `verifyDistrictAdmin` (line 518) hardcodes `adminCount = consensusConfig.VoteFloor` (line 541) instead of actually counting district admins. This means district secret proposals always use the floor vote count regardless of actual admin count.

### `auth.go` — Changes

- Clean addition of `encryptionKeyRepo` as 12th parameter to `NewAuthHandlerWithEmailService`.
- Login response now includes `has_encryption_keys` boolean — useful for frontend to determine if key setup is needed.
- The encryption key check is a best-effort query (log-and-continue on error) — appropriate.

### `signal_groups.go` — Changes

- Massively simplified: all invite_link proposal logic removed (~535 lines deleted). Now delegates to `SecretUpdateHandler`.
- `Create` now atomically creates both the signal group and its encrypted secret in a transaction.
- `List`/`ListAdmin` now fetch encrypted secrets per group (N+1 pattern, same as meshtastic).
- Non-transactional fallback in `Create` (line 148) has the same orphan risk as meshtastic: if encrypted secret creation fails, the signal group exists without a secret.

### `schools.go` — Changes

- `InviteLinkProposalRepository` replaced with `EncryptedSecretRepository` throughout.
- `ListSignalGroups`, `CreateSignalGroup`, `ListDistrictSignalGroups`, `CreateDistrictSignalGroup` all updated to use encrypted secrets pattern.
- Same N+1 pattern for fetching encrypted secrets per group in list endpoints.

### `router.go` — Changes

- New routes cleanly wired: encryption keys, meshtastic channels, secret proposals, encrypted secret finalization.
- All new routes properly wrapped in `r.authenticated()`.
- Nil checks on `r.encryption` in handler wrappers provide graceful degradation.
- `handleMeshtasticChannelByID` routes `secret-proposals` sub-route to `secretUpdates.CreateProposal` — but this handler expects a signal group ID (it calls `GetBySignalGroupID`), not a meshtastic channel ID. This appears to be a **bug** — meshtastic channel secret proposals won't work through this route.
- **NewRouter** now takes 23 params (was 20). All test files need updating.

## Services

### `email_templates.go` — Rekey notification template

- Clean addition. `buildRekeyingNeeded` follows the exact same pattern as all other templates.
- **Good security practice**: email contains no sensitive data — just tells the user to log in. The re-keying happens automatically on login via frontend crypto code.
- Uses `html.EscapeString` for the login URL in HTML content — correct.
- Text and HTML content both correctly convey that "re-keying happens automatically when you log in — no manual action required."

### `notification.go` — QueueRekeyingNeededEvent

- **Major**: `QueueRekeyingNeededEvent` is defined and fully tested, but **never called** from anywhere — not from the encryption handler's `RotateKeys`, not from `auth.go`'s password reset flow, not from `main.go`. The rekey notification pipeline is dead code. When a user rotates their keys, peers who share secrets are never notified to log in and help re-key.
- The fan-out event pattern (empty `UserID` = fan-out) is consistent with `QueueInviteLinkUpdatedEvent`.
- `ResourceID` stores the rotated user's ID — the worker uses this to look up shared-secret peers.

### `notification_worker.go` — Rekey fan-out worker

- Well-structured. `fanOutRekeyingNotification` follows the same pattern as `fanOutInviteLinkNotification`.
- Good defensive checks: nil `ResourceID`, nil `SecretKeyLookup`, lookup errors — all properly nack the event.
- `SecretKeyLookup` interface is clean — single method `GetUsersWithSharedSecrets`.
- The worker `processQueue` now handles unknown fan-out types by nacking them (line 144) — good improvement over silently ignoring.
- `enrichTemplateData` correctly handles `NotificationTypeRekeyingNeeded` as a no-op case — no sensitive data to enrich.
- **Minor**: `NotificationServiceInterface` in `helpers.go` (line 62) was **not** updated to include `QueueRekeyingNeededEvent`. The interface only has the 5 original methods. This means the `SecretUpdateHandler.notificationService` field (typed as `NotificationServiceInterface`) can't call `QueueRekeyingNeededEvent` even if it wanted to — the method isn't on the interface. This would need to be added for the rekey notification path to work end-to-end.

### `email_templates_test.go` — Changes

- Added test case for `NotificationTypeRekeyingNeeded` with correct assertions (subject, body content, no sensitive data).
- Also added a test case for `SubRegionInvitation` — appears to have been a pre-existing gap that was filled here.

### `notification_test.go` — Changes

- `TestNotificationService_QueueRekeyingNeededEvent` — thorough test verifying fan-out event structure (empty UserID, correct resource type/ID, queued status).
- `TestNotificationService_QueueSubRegionInvitation` — also added, filling a pre-existing test gap.

### `notification_worker_test.go` — Changes

- Excellent test coverage: 7 new test functions covering the rekey fan-out path:
  - Happy path: fan-out expansion to per-user notifications
  - Edge case: no shared secrets (empty list)
  - Error case: missing ResourceID
  - Error case: nil SecretKeyLookup
  - Error case: lookup returns error
  - End-to-end: per-user rekey email content verification
  - Unknown fan-out type handling
- Good use of `nackTrackingQueue` and `ackTrackingQueue` wrappers for precise assertions on queue operations.
- Existing tests correctly updated to pass `nil` as 6th param (`secretKeyLookup`) to `NewNotificationWorker`.

## Tests

### Overall Test Count

- `encryption_test.go` (handlers): 31 tests — UploadKeys, GetKeys, UpdateKeys, RotateKeys, GetPendingRekeys, SubmitRekeys, GetPublicKeys
- `meshtastic_test.go` (handlers): 44 tests — Create (region/school/district/validation), List, ListAdmin, Update
- `secret_updates_test.go` (handlers): 42 tests — CreateProposal, Vote, Finalize, GetProposal, ListProposals, ExpireProposal
- `encryption_repos_test.go` (database): 30 tests — EncryptionKeyRepository, EncryptedSecretRepository, MeshtasticChannelRepository, SecretUpdateProposalRepository
- **Total new unit/integration tests: ~147**

### Handler Tests (sqlmock-based)

**Quality: Good overall.** Tests use a clean suite pattern with `setupXxxTestSuite` helpers and sqlmock for database mocking. Each handler method has auth, validation, and happy-path coverage.

- **Encryption handler tests**: Thorough coverage of all endpoints. Good tests for graceful degradation (nil secret repo, FlagRekey error still succeeds). Validation tests cover all missing-field combinations. **Gap**: `SubmitRekeys` success test doesn't verify the caller has a valid key for the secret — confirms the critical authorization gap noted in the handler review.
- **Meshtastic handler tests**: Excellent scope coverage — tests for region, school, and district create paths, each with admin/non-admin/limit-reached variants. Bootstrap mode rejection tested. Good claims helpers (`adminClaims()`, `superuserClaims()`, `vouchedClaims()`, `unverifiedClaims()`, `postcardOnlyClaims()`). Empty scope strings test is a nice edge case.
- **Secret update handler tests**: `CreateProposal` tests cover region/school/district admin checks. `Vote` tests cover happy path and approval threshold. **Gap**: `Finalize` success test doesn't verify caller authorization — confirms the critical gap. No negative test for "unauthorized user calls Finalize". `GetProposal` tests use VoteFloor for `VotesNeeded` (consistent with handler implementation but still incorrect).
- **Minor**: `regionAdminClaims()` in `secret_updates_test.go` is functionally identical to `adminClaims()` in `meshtastic_test.go`. Could share across test files.

### Database Repository Tests (real DB)

**Quality: Good.** Tests run against a real MariaDB instance (via `testDB(t)`). Clean test helpers (`encCreateUser`, `encCreateRegion`, `encCreateSignalGroup`, `encCreateSecret`). Proper cleanup via deferred DELETE statements.

- All CRUD operations tested for EncryptionKeyRepository, EncryptedSecretRepository, MeshtasticChannelRepository, and SecretUpdateProposalRepository.
- Key behaviors verified: upsert semantics, not-found errors, rekey flagging + pending rekey retrieval, shared users lookup, proposal voting + counting, expiration of past-due proposals, `MarkFinalized` (sets `finalized_at` and status to `approved`).
- `TestEncryptionKeyRepository_GetPublicKeysForRegion` creates users with `TierPostcard` verification — does **not** test whether unverified users would be incorrectly included (confirms the verification filter gap noted in repo review).
- `TestSecretUpdateProposalRepository_UpdateStatus` asserts `ResolvedAt` is set when status changes to `approved_pending_finalization` — confirms the `UpdateStatusTx` always-sets-resolved_at behavior is intentional (or at least tested-as-is).
- `TestSecretUpdateProposalRepository_List` tests both superuser and non-superuser admin paths. Good coverage of status filtering.

### E2E Tests

- `TestE2E_EncryptedSecretFlow`: Creates signal group with encrypted payload + wrapped keys, verifies encrypted secret retrieval, tests key upload/get/update lifecycle. Good end-to-end validation of the encryption round-trip.
- `TestE2E_MeshtasticChannelCRUD`: Full CRUD lifecycle — create (with encrypted secret), list (by region, by school, admin view), update name/description, verify encrypted secret on list responses.
- `TestE2E_MeshtasticChannelScoping`: Tests the 5-channel-per-scope limit and cross-scope isolation (region channels don't appear in school lists).
- `registerOrGetUserID` helper refactored from `registerUser` — avoids UNIQUE constraint failures on re-registration.
- Cleanup properly handles new tables (`encrypted_secret_keys`, `encrypted_secrets`, `meshtastic_channels`).

### Updated Test Files (constructor signature changes)

All 9 test files correctly updated for new `NewRouter` signature (23 params): `api_contract_test.go`, `csrf_e2e_test.go`, `deletion_proposals_e2e_test.go`, `e2e_test.go`, `email_verification_test.go`, `integration_test.go`, `mail_provider_test.go`, `notification_e2e_test.go`, `ratelimit_test.go`.

All 3 test files correctly updated for new `NewAuthHandlerWithEmailService` signature (12 params): `api_contract_test.go`, `email_verification_test.go`, `mail_provider_test.go`.

`notification_e2e_test.go` refactored: invite_link proposal voting replaced with direct queue injection. `createInviteLinkProposal` helper removed. Good simplification.

### Test Gaps Summary

1. **No negative authorization tests for `Finalize`** — no test where a non-admin calls Finalize and gets rejected (because the handler doesn't reject them)
2. **No negative authorization tests for `SubmitRekeys`** — same issue
3. **No test for `GetPublicKeysForRegion` with unverified users** — would expose the missing verification filter
4. **No test for meshtastic secret proposals via the router** — would expose the signal-group-ID-vs-meshtastic-channel-ID routing bug
5. **Rekey notification dead code** — `QueueRekeyingNeededEvent` is tested in isolation but never integration-tested because it's never called

## Frontend

### Crypto Layer (`static/js/crypto/`)

**Quality: Excellent.** Clean three-layer architecture: `keyManager.js` (low-level RSA-OAEP + PBKDF2) → `envelope.js` (AES-256-GCM envelope encryption) → `index.js` (high-level flows) → `rekey.js` (re-keying workflow).

- RSA-OAEP 2048-bit keypairs with PBKDF2 (600K iterations, SHA-256) password wrapping. 16-byte salt, 12-byte IV — all appropriate.
- Restored private keys are non-extractable — good security practice.
- `generateKeypair()` in `keyManager.js` is dead code — only `generateBackupableKeypair()` is used.
- `rotateKeys()` in `index.js` clears local keys before generating new ones — correct.
- Error handling returns `false` with `console.error` — callers handle gracefully.

### Meshtastic Decoder (`static/js/meshtastic/`)

- `decoder.js`: Minimal protobuf wire format decoder for Meshtastic ChannelSet URLs. Well-implemented; no need for a full protobuf library.
- `display.js`: Human-readable formatting and PSK security analysis (warns about default key, no encryption, AES-128).
- No user input rendered as HTML from these modules — data objects only.

### API Modules

- `encryption.js`, `meshtastic.js`, `secretProposals.js` — clean API wrappers. No issues.
- **Minor**: `ApiError` import in `secretProposals.js` is unused.

### Pages — New

- **`meshtastic.js`**: Decrypts all channel secrets in parallel, caches per `secret_id`, groups by scope, renders QR codes. Good UX with loading/empty states.
- **`admin/secretProposals.js`**: Tabbed filter UI, modal detail with decryption, voting, finalization (re-encrypts for ALL members, not just admins). Excellent error handling distinguishing API vs crypto errors.
- **`admin/manageMeshtastic.js`**: Channel CRUD with E2E encryption, live URL preview via protobuf decoder, delete via proposal workflow.

### Pages — Modified

- **`login.js`**: After login, checks for local key → restores from backup if server has keys → initializes fresh if not. Background re-key check.
- **`register.js`**: Calls `initializeEncryption(password)` after registration. **Minor**: Called before email verification — may fail silently if API requires auth.
- **`resetPassword.js`**: Calls `rotateKeys(newPassword)` — generates new keypair, flags old DEKs for re-keying. Correct.
- **`profile.js`**: Calls `rewrapBackup(newPassword)` on password change — re-wraps private key backup with new password.
- **`groups.js`**: Complete rewrite for E2E encrypted invite links. Decrypt-then-render pattern with session caching.
- **`dashboard.js`**: Adds Meshtastic channels section, admin tools for Meshtastic/Proposals. Signal groups now decrypt invite links.
- **`regionDetail.js`, `schoolDetail.js`, `districtDetail.js`**: Integrated Meshtastic channels with decryption and shared card component.
- **`admin/manageGroups.js`**: Complete rewrite — group creation encrypts invite link, link update uses secret proposal workflow.
- **`app.js`**: New routes (`/meshtastic`, `/admin/meshtastic`, `/admin/proposals`). Background re-key check on init.
- **`help.js`**: New "Meshtastic" tab with comprehensive documentation.

### Frontend Bugs

1. **Bug**: Edit modal data extraction from DOM in `manageGroups.js` and `manageMeshtastic.js` — `card.querySelector('.group-card__name')?.textContent` includes badge text (e.g., "My ChannelDelete Proposed" instead of "My Channel"). Fix: use a `data-name` attribute or separate element.
2. **Minor**: Silent encryption failure on login — if `restoreEncryption()` fails, user proceeds without keys, sees "key not available" for encrypted content. No toast/warning shown.
3. **Minor**: Decryption cache (`Map`) in `groups.js` and `meshtastic.js` not cleared in `cleanup()` — persists across navigations. Not a security risk but stale data concern.

### Frontend Observations

- **Good XSS prevention**: `escapeHtml()` used consistently across all files for user-provided content. Pattern is safe.
- **Duplicate `escapeHtml()`**: Defined identically in ~10+ files. Consider extracting to a shared utility.
- **Shared component**: `meshtasticCard.js` reused across meshtastic, regionDetail, schoolDetail, districtDetail pages — good extraction.
- **QR code**: Lazily loaded from local `/static/vendor/qrcode.min.js` (not CDN) — good for integrity.

## main.go Wiring

### New Repositories

- `EncryptedSecretRepository` (line 75)
- `SecretUpdateProposalRepository` (line 76)
- `EncryptionKeyRepository` (line 87)
- `MeshtasticChannelRepository` (line 268)

All initialized with `db` — standard pattern. No issues.

### New Handlers

- `EncryptionHandler(encryptionKeyRepo, encryptedSecretRepo)` — matches constructor exactly
- `SecretUpdateHandler(db, proposalRepo, secretRepo, encryptionKeyRepo, regionRepo, schoolRepo, auditRepo, &cfg.Consensus)` — matches constructor exactly
- `MeshtasticHandler(db, channelRepo, encryptedSecretRepo, regionRepo, schoolRepo, auditRepo)` — matches constructor exactly

### Modified Handlers

- `AuthHandler` — receives `encryptionKeyRepo` as 12th param. Matches.
- `SignalGroupHandler` — removed `inviteLinkProposalRepo` and `consensusConfig`, replaced with `encryptedSecretRepo`. Matches.
- `SchoolHandler` — replaced `inviteLinkProposalRepo` with `encryptedSecretRepo`. Matches.
- `NotificationWorker` — receives `encryptedSecretRepo` as 6th param (satisfies `SecretKeyLookup` interface). Matches.

### Router Wiring

`NewRouter` receives 20 params with 3 new ones (`encryptionHandler`, `secretUpdateHandler`, `meshtasticHandler`) inserted between `userReportHandler` and `jwtAuth`. Matches constructor.

### Background Workers

- `secretUpdateProposalRepo.StartExpirationWorker` replaces old `inviteLinkProposalRepo.StartExpirationWorker`. Runs hourly.
- `signalGroupHandler.SetNotificationService(notificationService)` correctly removed (voting logic moved to `SecretUpdateHandler`).
- `secretUpdateHandler.SetNotificationService(notificationService)` correctly added.

### Observations

- **Pre-existing**: `schoolHandler.SetNotificationService(notificationService)` is never called — `notificationService` field remains nil. Not introduced by this PR.
- **Dead code path**: Router nil-guards on `r.encryption` are unreachable since `EncryptionHandler` is always created. Defensive but unnecessary.
- No configuration changes needed — crypto is entirely client-side. Reuses existing `cfg.Consensus` for vote thresholds.
- **No compile-time or runtime wiring errors detected.**

---

## Overall Assessment

### Critical Issues (must fix before merge)

1. **`Finalize` handler has no authorization** (`secret_updates.go`) — any authenticated user can replace any secret's encrypted content and wrapped keys. Must verify caller is a scope admin AND an `approved_pending_finalization` proposal exists for the secret.
2. **`SubmitRekeys` handler has no authorization** (`encryption.go`) — any authenticated user can overwrite any other user's wrapped DEK with garbage, locking them out. Must verify the caller has a valid (non-rekey-needed) key for the secret.

### Major Issues (should fix before merge)

3. **`TRUNCATE TABLE signal_groups`** in migration 026 — wipes all data. Should be removed or migration renamed to make destructive intent explicit.
4. **`QueueRekeyingNeededEvent` is never called** — the rekey notification pipeline (template, service method, worker fan-out) is fully built and tested but dead code. When a user rotates keys, peers are never notified to re-key.

### Medium Issues (fix or acknowledge)

5. **`GetPublicKeysForRegion` missing verification filter** — unverified region members get wrapped DEKs, granting them secret access they shouldn't have.
6. **Meshtastic secret-proposals routing bug** — `handleMeshtasticChannelByID` routes to `CreateProposal` which calls `GetBySignalGroupID`, not meshtastic channel ID.
7. **`Finalize` never calls `MarkFinalized`** — proposals stay in `approved_pending_finalization` forever.
8. **`UpdateStatusTx` always sets `resolved_at`** — non-terminal status `approved_pending_finalization` incorrectly gets a resolved timestamp.
9. **`GetPendingBySecretForUpdate` uses fragile string comparison** — `err.Error() == "proposal not found"` instead of sentinel error.
10. **`GetPublicKeys` leaks scope membership** — any authenticated user can enumerate who has encryption keys in any scope.
11. **`resolveGroupScope` runs raw SQL in handler** — breaks repository pattern.
12. **`verifyDistrictAdmin` hardcodes VoteFloor** — district proposals always use floor vote count regardless of actual admin count.
13. **`secret_update_proposals` missing 3-way XOR CHECK constraint** — unlike `signal_groups` and `meshtastic_channels`.

### Minor Issues

14. Edit modal DOM scraping bug in `manageGroups.js` and `manageMeshtastic.js` — badge text included in channel/group name
15. `CreateEncryptionKeyRequest` / `RotateEncryptionKeyRequest` structurally identical — consolidate or document
16. No `validate:` struct tags on encryption request types
17. `MeshtasticChannel.CreatedBy` is `*string` but DB has `NOT NULL`
18. `NotificationServiceInterface` not updated with `QueueRekeyingNeededEvent`
19. Dead code: `generateKeypair()` in `keyManager.js`
20. Duplicate `escapeHtml()` across ~10 frontend files

### Strengths

- **Excellent crypto architecture**: Clean three-layer frontend crypto (keyManager → envelope → index) with proper key management lifecycle.
- **Good security practices**: Non-extractable restored keys, PBKDF2 with 600K iterations, no sensitive data in emails, XSS prevention throughout frontend.
- **Comprehensive tests**: 147 new tests covering all new handlers, repos, and E2E flows.
- **Clean wiring**: All new dependencies correctly threaded through main.go, handlers, and router.
- **Consistent patterns**: Meshtastic channels follow identical patterns to signal groups (3-way XOR, scope-aware admin checks, transactional creation, soft-delete).
- **Good simplification**: Removing ~675 lines from `signal_groups.go` and ~535 lines from the signal groups handler by extracting to the shared secret proposal system.
