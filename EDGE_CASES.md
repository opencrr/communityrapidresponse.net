# Edge Case Enumeration

A grounded inventory of edge cases across the platform's critical flows. Each
entry references the actual code path where the behavior lives or should live,
notes current test coverage, and proposes a guard or test where appropriate.

Coverage legend:

- ✅ **covered** — explicit test exists and exercises this branch
- ⚠️ **partial** — adjacent path is tested but this specific edge is not
- ❌ **uncovered** — no test directly exercises this path
- ❓ **unknown** — coverage could not be confirmed without runtime inspection

Scope: documentation only. No code or tests are changed in this PR. The
"Coverage Gaps" section at the end prioritizes follow-up work.

---

## 1. Postcard Verification

Primary paths: `internal/handlers/verification.go`,
`internal/services/postgrid.go`, `internal/services/lob.go`,
`internal/database/verification.go`.

Constants: `maxVerificationRequestsPer30Days = 3`,
`verificationCodeLockoutThreshold = 5` (handlers/verification.go:24, :33).

### 1.1 Vouch verification missing
- Scenario: user without `VouchVerified=true` calls `POST /verification/postcard/request`.
- Expected: 403 `vouch_required` — postcard upgrades a vouched user, not the reverse.
- Code: handlers/verification.go:110.
- Coverage: ✅ verification_test.go exercises the gate.

### 1.2 Missing or partial address fields
- Scenario: any of Line1, City, State, PostalCode is empty.
- Expected: 400 `validation_error`.
- Code: handlers/verification.go:123.
- Coverage: ✅ verification_test.go.
- Edge: whitespace-only fields are not trimmed before the empty check — a
  payload of `"city": "   "` passes validation and is sent to Postgrid.
  Suggested guard: `strings.TrimSpace` before the empty-string comparison.

### 1.3 PO Box / CMRA / commercial address
- Scenario: validated address comes back as PO Box, CMRA (UPS Store mailbox),
  or commercial.
- Expected: 400 with `po_box_not_allowed`, `cmra_not_allowed`,
  `commercial_not_allowed` respectively.
- Code: handlers/verification.go:160–194; services/lob.go:186–196;
  services/postgrid.go `isPOBoxAddress`.
- Coverage: ✅ lob_test.go, postgrid_test.go cover detection; handler tests
  cover the rejection branches.
- Edge: `isPOBoxAddress` is a regex on the formatted string — addresses like
  `"PO Box 123 c/o Recipient"` or international variants
  (`"Boîte Postale"`, `"Casilla"`) may slip through if Postgrid does not flag
  them. ❓ — depends on Postgrid signal fidelity.

### 1.4 Address validation API failure
- Scenario: Postgrid/Lob returns 5xx, network timeout, or malformed JSON.
- Expected: 502 `address_validation_failed`, no DB row created.
- Code: handlers/verification.go:140-146.
- Coverage: ⚠️ partial — happy path and rejection responses mocked; explicit
  timeout / 5xx not exhaustively tested.

### 1.5 Geocoding failure after successful address validation
- Scenario: Postgrid accepts address; Mapbox geocoding fails.
- Expected: 502 `geocoding_failed`; no DB row, no postcard sent.
- Code: handlers/verification.go:197-203.
- Coverage: ⚠️ partial — mapbox_test.go covers service; handler-level failure
  branch coverage is light.

### 1.6 Rate limit race condition
- Scenario: user submits two concurrent verification requests at count = 2.
  Both pass the pre-check (1.6a) but only one should commit.
- Expected: row-locked `CountRecentByUserForUpdate` in the transaction
  prevents both from inserting; second returns 429 `rate_limit`.
- Code: handlers/verification.go:300–306 (atomic check inside tx).
- Coverage: ❌ no concurrent-request test. Pre-check at line 134 plus the
  in-tx check at line 304 is the right design, but the race window between
  them is only closed by the FOR UPDATE lock — worth a regression test using
  `t.Parallel` + `sync.WaitGroup`.

### 1.7 Postgrid `SendPostcard` fails after row insert
- Scenario: rate-limit insert succeeds, then Postgrid call fails.
- Expected: row deleted to avoid consuming a rate-limit slot for an unsent
  postcard.
- Code: handlers/verification.go:325–333 (`h.verificationRepo.Delete`).
- Coverage: ⚠️ partial — the cleanup is best-effort (`_ =`), and if delete
  itself fails the row remains as `mailed` with `postgrid_request_id="pending"`
  forever. Suggested test: simulate SendPostcard error, assert row removed.

### 1.8 `UpdatePostgridRequestID` failure (non-fatal)
- Scenario: postcard sent, but updating the DB row with the real Postgrid ID
  fails.
- Expected: error logged + Sentry capture; user flow continues because nothing
  branches on this column.
- Code: handlers/verification.go:338-341.
- Coverage: ❌ no test for this specific failure mode. Low risk per code
  comment, but worth asserting the comment is still true (no later code reads
  `PostgridRequestID`).

### 1.9 Code verification: wrong user attempts code
- Scenario: User A submits the code mailed to User B.
- Expected: increment failed-attempts on the *target* row (not attacker's),
  then return `ErrVerificationNotFound`; lock at 5 failures.
- Code: handlers/verification.go:418–429.
- Coverage: ✅ verification_test.go covers lockout threshold.
- Edge: incrementing failed_attempts on the *target* row means an attacker
  who guesses another user's code can DoS the legitimate user's verification.
  Suggested guard: separate "attempts against this code" from "this user's
  attempts" or scope by `(code, attempter_user_id)`.

### 1.10 Code verification: expired code
- Scenario: user submits valid code after `expires_at` (30 days).
- Expected: error `code_expired`; row remains for audit.
- Code: handlers/verification.go:433.
- Coverage: ✅ verification_test.go.
- Edge: `ExpireOldRequests` (verification.go:186) sweeps pending/mailed rows
  to status `expired`. If the sweeper hasn't run yet, the time check above
  catches it; both paths converge.

### 1.11 Code verification: already-verified request
- Scenario: same valid code submitted twice after success.
- Expected: `database.ErrAlreadyVerified`.
- Code: handlers/verification.go:438.
- Coverage: ✅.

### 1.12 Case sensitivity / whitespace in code
- Scenario: user enters `" ABC-123 "` or mixed case.
- Expected: normalized via `strings.ToLower(strings.TrimSpace(...))` before
  lookup.
- Code: handlers/verification.go:402.
- Coverage: ⚠️ unknown whether tests assert both spacing AND case; worth a
  table-driven test.

### 1.13 Postcard reference collision
- Scenario: `postcardRef` generation (handlers/verification.go:1862) uses a
  custom unbiased charset; theoretical collision possible across millions of
  postcards.
- Expected: DB unique constraint causes insert to fail, returning a 5xx that
  the user can retry.
- Coverage: ❌ no collision test (probability is tiny but the failure mode is
  silent). Suggested: add a uniqueness constraint check in the test, and
  consider retrying once on collision.

---

## 2. Vouch Verification

Primary paths: `internal/handlers/verification.go` (Vouch, RequestVouch),
`internal/database/verification.go` (VouchRepository).

Constants: `requiredVouchesForTier2 = 2`, `bootstrapVouchesRequired = 3`,
`maxVouchesPerMonth = 10`, `minAdminsToEndBootstrap = 3`
(handlers/verification.go:25-30).

### 2.1 Self-vouch
- Scenario: voucher_user_id == vouched_user_id.
- Expected: 400 `invalid_request`.
- Code: handlers/verification.go:894.
- Coverage: ✅ verification_test.go.

### 2.2 Circular vouch
- Scenario: B vouches for A, then A tries to vouch for B.
- Expected: 400 `circular_vouch`.
- Code: handlers/verification.go:1005.
- Coverage: ✅.
- Edge: only catches direct A↔B cycles. Triangle (A→B→C→A) is allowed by
  design but worth documenting as a deliberate non-edge.

### 2.3 State-level vouching
- Scenario: target region is a `state` type.
- Expected: 400 `state_level_vouch`.
- Code: handlers/verification.go:933.
- Coverage: ⚠️ partial — unit-tested for the rejection but downstream effect
  (state user_regions still upgrade via ancestor cascade) needs an end-to-end
  test.

### 2.4 Monthly vouch limit boundary
- Scenario: voucher has exactly 9 vouches this month, submits 10th.
- Expected: 10th succeeds, 11th gets 429 `vouch_limit`.
- Code: handlers/verification.go:1076, 1144.
- Coverage: ⚠️ partial — limit is tested but the exact boundary (N == N-1 vs
  N == N) is worth an explicit case.
- Edge: month boundary is calendar month. A user can submit 10 on the last
  day and 10 more the next day (20 in 24h). Not necessarily a bug — document
  the intent.

### 2.5 Double-vouch race
- Scenario: voucher submits two vouches for same user concurrently.
- Expected: `HasVouchedTx` inside transaction returns true on second; 409
  `already_vouched`.
- Code: handlers/verification.go:1063–1068.
- Coverage: ❌ no concurrent test.

### 2.6 Bootstrap-mode exact-region requirement
- Scenario: region in bootstrap (admin count < 3); voucher submits at the
  city level when vouchee's most-specific pending region is a neighborhood.
- Expected: 400 `bootstrap_exact_region`.
- Code: handlers/verification.go:951–963.
- Coverage: ⚠️ partial — bootstrap is asserted but ancestor mismatch is not
  uniformly tested.

### 2.7 Bootstrap-mode cooldown
- Scenario: voucher submits two vouches within
  `bootstrapCooldownMinutes`.
- Expected: 429 `vouch_cooldown` with `cooldown_end` timestamp.
- Code: handlers/verification.go:970–987.
- Coverage: ❌ cooldown logic is feature-flagged; test for both
  enabled/disabled.

### 2.8 Normal-mode voucher not fully verified
- Scenario: outside bootstrap, voucher has only postcard or only vouch (not
  both).
- Expected: 403 `forbidden`.
- Code: handlers/verification.go:989–995.
- Coverage: ✅.

### 2.9 Voucher not in region (region check)
- Scenario: voucher is verified globally but not a member of the *target*
  region.
- Expected: 403 `not_in_region`.
- Code: handlers/verification.go:1013–1026.
- Coverage: ⚠️ partial.

### 2.10 Vouchee is blocklisted in region
- Scenario: target user blocked via blocklist_proposals in this region.
- Expected: 403 `user_blocked`.
- Code: handlers/verification.go:1029–1037.
- Coverage: ✅ blocklist_proposals_test.go + verification_test.go.

### 2.11 Ancestor-region vouch cascade
- Scenario: vouches accumulate at a child region; ancestor upgrade is
  triggered via `UpgradeUserRegionAndAncestorsToVerifiedTx`.
- Expected: target region + all ancestors upgraded atomically.
- Code: handlers/verification.go:1107.
- Coverage: ⚠️ partial — happy path tested; partial failure (one ancestor
  update fails mid-cascade) needs to be covered to ensure transaction rolls
  back cleanly.

### 2.12 Bootstrap exit during vouch
- Scenario: third admin is created mid-flight while a vouch is being
  processed.
- Expected: `IsRegionInBootstrapMode` was read outside the tx (line 939) so
  `vouchesRequired` may be stale. If admin count tipped from 2→3 between
  read and commit, the request still uses bootstrap=3 vouches.
- Code: handlers/verification.go:939, used at 946-948.
- Coverage: ❌. Low-frequency but real consistency hole. Suggested guard:
  re-read inside tx with `FOR UPDATE` semantics or accept stale value
  explicitly with a doc comment.

### 2.13 Auto-upgrade with not-yet-postcard-verified user
- Scenario: vouchee crosses vouch threshold but `PostcardVerified=false`.
- Expected: tier set to `vouched`, `isAdmin=false`, ancestor regions upgraded
  to "verified" but not admin.
- Code: handlers/verification.go:1104, 1107.
- Coverage: ✅.

### 2.14 Token invalidation after upgrade
- Scenario: vouchee was logged in with a stale JWT before the upgrade.
- Expected: `InvalidateTokens` called so future requests get fresh claims.
- Code: handlers/verification.go:1172–1175.
- Coverage: ⚠️ partial — invalidation called but no test asserts the old
  token is rejected after upgrade.

### 2.15 Unknown user identifier
- Scenario: voucher submits `user_identifier` that matches nothing
  (email/username/UUID).
- Expected: 404 `user_not_found`.
- Code: handlers/verification.go:886–889.
- Coverage: ✅.
- Edge: lookup order is email → username → UUID. A username matching the
  literal text of someone else's UUID would resolve to the wrong user; the
  current order makes this benign, but consider validating UUID format
  before falling back.

---

## 3. MFA

Primary paths: `internal/handlers/mfa.go`, `internal/services/mfa.go`,
`internal/middleware/auth.go`.

Constants: `mfaLockoutThreshold = 5` (handlers/mfa.go:17);
token types `full` / `pending_mfa` / `mfa_setup` (middleware/auth.go:28-30);
short-lived tokens get 10-minute expiration (middleware/auth.go:90-92).

### 3.1 Wrong token type for MFA verify
- Scenario: user sends a `full` token to `/mfa/verify` or a `pending_mfa`
  token to a protected endpoint.
- Expected: middleware rejects with 401.
- Code: middleware/auth.go token-type checks.
- Coverage: ✅ middleware/auth_test.go covers all three token types.

### 3.2 MFA setup token used after MFA enabled
- Scenario: user already enabled MFA, presents a stale `mfa_setup` token.
- Expected: `/mfa/setup` rejects because user already has secret.
- Code: handlers/mfa.go (InitSetup).
- Coverage: ⚠️ partial.

### 3.3 Account locked at exactly threshold
- Scenario: user makes 4 failed attempts (remaining=1), then a 5th.
- Expected: 5th triggers `mfa_locked` 429; subsequent calls return the
  pre-check lockout at handlers/mfa.go:330.
- Code: handlers/mfa.go:265, 330.
- Coverage: ✅ users_mfa_lockout_test.go.

### 3.4 DB error while incrementing failed attempts
- Scenario: `IncrementFailedMFAAttempts` returns error.
- Expected: fail closed → return locked (handlers/mfa.go:248-256).
- Code: handlers/mfa.go:247-256.
- Coverage: ⚠️ partial.
- Edge: this is correct behavior, but it means a flaky DB triggers a lock
  for legitimate users. Document or add a metric.

### 3.5 MFA secret missing/empty for enabled user
- Scenario: `MFAEnabled=true` but `MFASecret` is nil or "" (data corruption
  or migration bug).
- Expected: 500 with explicit error message; no bypass.
- Code: handlers/mfa.go:324-327.
- Coverage: ❌. Worth a regression test to prevent any future code path
  that "fails open" if secret missing.

### 3.6 Backup code reuse
- Scenario: same backup code submitted twice (codes are one-time).
- Expected: second use rejected and counted as failed attempt.
- Code: handlers/mfa.go backup code branch (line 358+).
- Coverage: ❓ need to verify backup codes are removed/marked-used after
  consumption.

### 3.7 TOTP code drift
- Scenario: client clock differs from server by ±1 step (30s).
- Expected: TOTP library typically allows ±1 step; depends on
  `services/mfa.go` `ValidateCode` window setting.
- Code: services/mfa.go:132-140.
- Coverage: ❓.

### 3.8 Successful MFA resets failed counter
- Scenario: 3 failed attempts, then correct code.
- Expected: counter reset to 0 on success.
- Code: handlers/mfa.go (success branch should call `ResetFailedMFAAttempts`).
- Coverage: ⚠️ partial — assert that counter goes back to 0.

### 3.9 Short-lived token expiration window
- Scenario: user takes >10 minutes between login and MFA verify.
- Expected: 401 `expired_token`.
- Code: middleware/auth.go:90-92 sets 10-min lifetime.
- Coverage: ✅ middleware/auth_test.go.

---

## 4. Region Hierarchy and Creation

Primary paths: `internal/handlers/regions.go`, `internal/database/regions.go`.

### 4.1 Type hierarchy violation
- Scenario: admin tries to create a `city_block` with a `state` parent, or a
  `county` with no parent.
- Expected: 400 validation error per CLAUDE.md hierarchy rules
  (state→county→city→locality/neighborhood→city_block).
- Code: handlers/regions.go Create; database/regions.go Create.
- Coverage: ⚠️ partial — explicit hierarchy validation should be table-driven
  across all invalid pairs.

### 4.2 Admin-boundary violation on draw
- Scenario: admin of City X draws a polygon that crosses into City Y.
- Expected: `ST_Contains(adminRegion.geometry, newPolygon)` must be true; 403
  otherwise.
- Code: database/regions.go (ST_Contains validation).
- Coverage: ❌ — no explicit "draws outside my admin region" test surfaced.
  High-value to add.

### 4.3 NULL geometry parent
- Scenario: admin creates a neighborhood under a city that has no polygon.
- Expected: containment check falls back to parent_id traversal; creation
  allowed if parent type is `city` or `locality`.
- Code: database/regions.go CreateWithOptionalGeometry.
- Coverage: ⚠️ partial.

### 4.4 NULL geometry child
- Scenario: neighborhood with NULL geometry needs to determine its sub-region
  membership.
- Expected: ST_Contains queries filter `geometry IS NOT NULL`; pure
  parent_id traversal substitutes.
- Code: database/regions.go (CLAUDE.md notes spatial index disabled).
- Coverage: ⚠️ partial — explicit test for ~92% of neighborhoods being
  NULL-geometry would catch regressions to the parent_id fallback.

### 4.5 Polygon self-intersecting / invalid
- Scenario: malicious or buggy client sends an invalid GeoJSON polygon.
- Expected: `ST_GeomFromGeoJSON` rejects with DB error; surfaced as 400.
- Code: database/regions.go:38 area.
- Coverage: ❌. Suggested: invalid GeoJSON fixture test.

### 4.6 SRID mismatch
- Scenario: client sends geometry in non-4326 SRID.
- Expected: rejected (schema enforces SRID 4326).
- Code: migrations + database/regions.go.
- Coverage: ❌. Low likelihood from web clients, but a hardening test would
  document the contract.

### 4.7 Duplicate region (same name + parent)
- Scenario: two admins create regions with the same name under the same
  parent.
- Expected: undefined — no unique constraint surfaced. Both succeed; users
  may see ambiguous results.
- Code: database/regions.go.
- Coverage: ❌. Suggested: either add `(parent_region_id, name)` uniqueness
  or document acceptance.

---

## 5. Sub-Region Membership

Primary paths: `internal/handlers/membership.go`,
`internal/database/membership.go`.

Constants: `RequestExpirationDays = 7`, `MaxPendingRequestsPerUser = 5`,
`RequiredVotesForApproval = 2` (database/membership.go:30-34).

### 5.1 Requesting membership in a top-level region
- Scenario: user requests join on a state/county region (no parent).
- Expected: 400 `invalid_region` (must be sub-region).
- Code: handlers/membership.go:82–85.
- Coverage: ✅.

### 5.2 User not in parent region
- Scenario: user requests sub-region membership but isn't in the parent's
  hierarchy.
- Expected: 403 `not_in_parent`.
- Code: handlers/membership.go:104.
- Coverage: ✅.

### 5.3 Duplicate pending request
- Scenario: user has an existing pending request for the same region.
- Expected: 409 with `existing_request_id`.
- Code: handlers/membership.go:115.
- Coverage: ✅.

### 5.4 Pending request limit boundary
- Scenario: user has 5 pending requests, submits a 6th.
- Expected: 429 `too_many_requests`.
- Code: handlers/membership.go:130.
- Coverage: ⚠️ partial — assert exact boundary value (4→OK, 5→deny).

### 5.5 Request expiration race vs final approving vote
- Scenario: 6 days, 23 hours after creation, second admin approves at
  expiry boundary.
- Expected: if `time.Now() > ExpiresAt`, request should be treated as
  expired even if vote count was met.
- Code: handlers/membership.go (vote handler around line 448).
- Coverage: ❌. Time-boundary regression worth a fake-clock test.

### 5.6 Admin votes twice
- Scenario: same admin sends two vote requests on same membership request.
- Expected: 409 / `ErrAlreadyVoted`.
- Code: database/membership.go `ErrAlreadyVoted`.
- Coverage: ✅.

### 5.7 Concurrent approving votes
- Scenario: two admins approve simultaneously at vote count = 1.
- Expected: first commit triggers approval; second is no-op (request now in
  `approved` state).
- Code: handlers/membership.go:448, 482.
- Coverage: ❌ — concurrency test would harden.

### 5.8 Self-membership invitation
- Scenario: admin invites themselves.
- Expected: should be no-op or 400; behavior currently unclear.
- Code: handlers/membership.go (invitation flow).
- Coverage: ❓.

### 5.9 Invited user is blocked in region
- Scenario: admin invites a user who is blocklisted.
- Expected: 403 / `user_blocked`.
- Code: ❓ — not verified that invitations check blocklist.
- Coverage: ❌. High-value gap.

### 5.10 Vouch-verified but not postcard-verified at acceptance
- Scenario: user accepts an invitation; should they become admin (if also
  postcard) or member?
- Expected: `isAdmin = (postcard && vouch)`.
- Code: acceptance flow in membership.go.
- Coverage: ⚠️ partial.

---

## 6. Consensus Governance (Blocklist / Deletion / Invite-Link)

Primary paths: `internal/handlers/blocklist_proposals.go`,
`deletion_proposals.go`, `secret_updates.go`;
`internal/database/blocklist_proposals.go`, `deletion_proposals.go`,
`secret_update_proposals.go`.

Vote math: `ConsensusConfig.RequiredVotes(adminCount) =
max(ceil(adminCount * VotePercent/100), VoteFloor)`. Default
`VotePercent=50`, `VoteFloor=3` (config/config.go:101-123, 320-322).

### 6.1 Insufficient admins to propose
- Scenario: region has 2 admins; admin tries to propose blocklist.
- Expected: 403 `insufficient_admins` with `admins_required=3`.
- Code: handlers/blocklist_proposals.go:115-123.
- Coverage: ✅.

### 6.2 Self-blocklist
- Scenario: admin proposes blocking themselves.
- Expected: 400.
- Code: handlers/blocklist_proposals.go (~line 91).
- Coverage: ⚠️ partial.

### 6.3 Blocklist target is superuser
- Scenario: target user is a superuser.
- Expected: 403 `cannot_blocklist_superuser`.
- Code: handlers/blocklist_proposals.go:135.
- Coverage: ✅.

### 6.4 Target not a region member
- Scenario: target user has no `user_region` row for this region.
- Expected: 400 `user_not_in_region`.
- Code: handlers/blocklist_proposals.go:146.
- Coverage: ✅.

### 6.5 Duplicate pending proposal for same target
- Scenario: second admin opens a proposal for the same target in the same
  region while one is pending.
- Expected: 409 with `existing_proposal_id`.
- Code: handlers/blocklist_proposals.go:204-216.
- Coverage: ✅.

### 6.6 Monthly proposal rate limit
- Scenario: proposer hits `BlocklistConfig.ProposalRateLimitPerMonth` (default 5).
- Expected: 429 with quota in body.
- Code: handlers/blocklist_proposals.go:157.
- Coverage: ✅.

### 6.7 Vote race at threshold
- Scenario: two admins vote yes simultaneously when proposal needs 1 more
  vote.
- Expected: first commit executes the action (block / delete / update);
  second is no-op.
- Code: handlers/blocklist_proposals.go:321-346 (re-checks vote count
  inside tx).
- Coverage: ❌ concurrency test.

### 6.8 Admin count drops below floor between proposal and vote
- Scenario: proposal created when 3 admins; one admin loses status before
  third vote.
- Expected: `RequiredVotes(2)` = 3 (floor); proposal needs a vote that no
  longer-existing admin can supply. Effectively the proposal stalls.
- Code: handlers/blocklist_proposals.go:286-292 — re-reads admin count per
  vote, so the threshold floats with admin count.
- Coverage: ❌. Subtle but realistic; suggested test ensures the threshold
  recalculates and the proposal cannot be silently auto-approved by a drop
  in `adminCount`.

### 6.9 Voter is no longer admin at vote time
- Scenario: admin proposed a blocklist, then lost admin status; tries to
  vote.
- Expected: 403 `not_region_admin`.
- Code: handlers/blocklist_proposals.go:282.
- Coverage: ⚠️ partial.

### 6.10 Negative votes ("no" votes)
- Scenario: admin explicitly votes no.
- Expected: behavior depends on schema; if "no" votes are tracked, threshold
  logic must not count them as approvals.
- Code: `AddVoteTx(..., true)` (line 188) hardcodes true at proposal time;
  vote handler accepts a vote_value but the threshold check counts approvals
  only. Worth a test that an explicit "no" never trips the threshold.
- Coverage: ⚠️ partial.

### 6.11 Proposal targets already-blocked user
- Scenario: admin proposes blocklist for a user already blocked in region.
- Expected: 409 or 400.
- Code: ❓ — not surfaced in the proposal-create path.
- Coverage: ❌.

### 6.12 Invite-link update notification fanout
- Scenario: invite-link proposal passes; system queues email notifications.
- Expected: emails do **not** contain the new invite link; rate-limited 1 per
  user per group per 24h.
- Code: services/notification_worker.go:283 (`WasSentRecently`);
  config `RateLimitDuration` (config/config.go:96, default 24h).
- Coverage: ⚠️ partial — rate limit tested in
  notification_worker_test.go; assert that the *body* never contains the
  invite link.

---

## 7. Signal Groups

Primary paths: `internal/handlers/signal_groups.go`,
`internal/database/signal_groups.go`.

Constants: `maxGroupsPerRegion = 5` (handlers/signal_groups.go:16).

### 7.1 Region in bootstrap mode
- Scenario: region has <3 fully verified admins; admin tries to create a
  group.
- Expected: 403 `region_in_bootstrap` (unless caller is superuser).
- Code: handlers/signal_groups.go:82-98.
- Coverage: ✅ signal_groups_test.go.

### 7.2 Group limit boundary
- Scenario: region already has 5 groups; admin creates a 6th.
- Expected: 409 `limit_reached`.
- Code: handlers/signal_groups.go:114, 144.
- Coverage: ✅.

### 7.3 Missing payload fields
- Scenario: any of region_id / name / encrypted_payload / IV / wrapped_keys
  empty.
- Expected: 400 `validation_error`.
- Code: handlers/signal_groups.go:60-63.
- Coverage: ✅.
- Edge: `name` is trimmed but other fields are not. Multi-byte whitespace
  or zero-width characters could pass — suggested unicode test.

### 7.4 Three-way XOR constraint violation
- Scenario: client sends both `region_id` and `school_id` (or all three).
- Expected: DB CHECK constraint rejects insert; handler should pre-check and
  return 400.
- Code: schema-level CHECK constraint; handler reads only `region_id`.
- Coverage: ❌ — the handler does not validate against school/district fields
  on the request struct. Suggested: schema test that exercises the CHECK
  and asserts a clear error message.

### 7.5 Transaction failure between group create and secret create
- Scenario: `CreateGroupTx` succeeds, `CreateTx` (encrypted secret) fails.
- Expected: transaction rollback leaves no orphaned group row.
- Code: handlers/signal_groups.go:108-129.
- Coverage: ⚠️ partial — assert rollback on failure (inject fault in
  encryptedSecretRepo).

### 7.6 Non-transactional fallback path
- Scenario: `h.db == nil` (test mode) — group is created, then secret
  creation fails.
- Expected: orphaned group remains (no rollback possible).
- Code: handlers/signal_groups.go:138-163.
- Coverage: ❌. This fallback path is test-only but the code shape invites
  bugs in tests. Consider removing the nil-db branch.

### 7.7 Group list visibility
- Scenario: vouch-only user lists groups for a region they're a member of.
- Expected: list returned (Tier ≥ Vouched required at line 192).
- Code: handlers/signal_groups.go:192.
- Coverage: ✅.

---

## 8. School Vouching

Primary paths: `internal/handlers/schools.go`,
`internal/database/school_vouches.go`, `internal/database/schools.go`.

Constants: `schoolBootstrapVouchesRequired = 3`,
`schoolNormalVouchesRequired = 2`, `minSchoolAdminsToEndBootstrap = 3`,
`maxSchoolVouchesPerMonth = 10` (handlers/schools.go:24-27).

### 8.1 Bootstrap threshold transition
- Scenario: school has 2 admins, third user crosses threshold mid-vouch.
- Expected: same staleness concern as 2.12 — `currentAdminCount` is read
  outside tx; vouchee may be admined or not depending on read order.
- Code: handlers/schools.go:469, 532.
- Coverage: ❌. Suggested: race test.

### 8.2 Cross-school vouch budget
- Scenario: voucher has used 9 vouches across schools A, B, C this month;
  10th in school D.
- Expected: monthly cap is global (10), not per-school.
- Code: handlers/schools.go:445, 513.
- Coverage: ⚠️ partial.

### 8.3 Self-vouch
- Scenario: school member vouches for themselves.
- Expected: 400.
- Code: handlers/schools.go (vouch handler).
- Coverage: ⚠️ partial.

### 8.4 Vouch for non-member
- Scenario: voucher vouches for a user who isn't in the school's pending
  membership list.
- Expected: 400/404.
- Code: handlers/schools.go vouch flow.
- Coverage: ⚠️ partial.

### 8.5 School blocked user
- Scenario: vouchee was previously blocked from the school.
- Expected: vouch rejected.
- Code: `school_blocked_users` table.
- Coverage: ⚠️ partial.

### 8.6 District admin tries to create district group while bootstrap
- Scenario: district has <3 verified admins.
- Expected: equivalent to 7.1 — should reject. Worth confirming code path
  exists for districts.
- Code: handlers/schools.go district group create.
- Coverage: ❓.

---

## 9. Cascading Deletes

Primary paths: `internal/database/regions.go:1199-1270` (`RegionRepository.Delete`).

### 9.1 Delete region with only direct children
- Scenario: city has neighborhoods; delete city.
- Expected: signal_groups + user_regions + child regions all removed in one
  tx.
- Code: regions.go:1209-1255.
- Coverage: ⚠️ partial — handler admin tests cover happy path.

### 9.2 Delete region with grandchildren (3+ levels)
- Scenario: state → county → city → neighborhood → block; delete state.
- Expected: full subtree removed.
- Code: regions.go:1239-1252 only iterates **one level** of children (no
  recursion). Grandchild signal_groups and user_regions are not deleted.
- Coverage: ❌. **This looks like a bug**: the loop comment says "delete
  child's signal groups / user_regions / region itself" but does not recurse.
  Either FK ON DELETE CASCADE silently handles it (worth confirming in
  schema) or grandchild rows are orphaned. Suggested: write a test that
  deletes a 3-level subtree and asserts all rows gone.

### 9.3 Delete non-existent region
- Scenario: `regionID` does not exist.
- Expected: `ErrRegionNotFound`.
- Code: regions.go:1265-1267.
- Coverage: ⚠️ partial.

### 9.4 Non-superuser invokes delete
- Scenario: regular admin calls delete endpoint.
- Expected: 403 (per CLAUDE.md, superuser-only).
- Code: handlers/admin.go DeleteRegion gate.
- Coverage: ✅.

### 9.5 Delete during active membership requests
- Scenario: pending sub_region_membership_requests reference the region
  being deleted.
- Expected: requests should be cleaned up or marked invalid; current code
  does not delete from membership tables.
- Code: regions.go:1199-1270 — only deletes signal_groups, user_regions,
  geographic_regions. No mention of membership requests, vouches, audit log,
  blocklist proposals.
- Coverage: ❌. **Potential foreign-key violation**: if these tables have
  FK to geographic_regions without ON DELETE CASCADE, the DELETE will fail
  partway through the tx. Suggested: schema audit + extend delete to clean
  up dependent rows or rely on FK CASCADE.

### 9.6 Status cache invalidation
- Scenario: users had cached "admin in deleted region" state.
- Expected: cache eviction for affected users.
- Code: ❓ — not visible in the delete tx.
- Coverage: ❌.

---

## 10. Auth Rate Limits and Account Lockout

Primary paths: `internal/handlers/auth.go`, `internal/middleware/ratelimit.go`,
`internal/services/ratelimit.go`.

Constants (handlers/auth.go:28-39): login 10/5min, register 3/1h,
forgot-password 5/15min, reset-password 10/15min, resend-verify 3/15min,
account lockout threshold 10 failures / 15-min lock.

### 10.1 Behind a trusted proxy
- Scenario: request arrives with `X-Forwarded-For: bad-ip, real-ip`.
- Expected: `GetClientIP` extracts the correct IP based on trusted-proxy
  config (middleware/ratelimit.go:75-93).
- Coverage: ⚠️ partial — assert trusted vs untrusted proxy behavior.

### 10.2 Rate limiter returns error
- Scenario: backing store (Redis or similar) unreachable.
- Expected: per `checkAuthRateLimit` (auth.go:85), request is **allowed** on
  limiter error. This is fail-open, opposite of MFA's fail-closed behavior.
- Coverage: ❌. Document the deliberate divergence (or change to fail-closed
  if security-critical).

### 10.3 Account lockout boundary
- Scenario: 9 failed logins, then a 10th wrong attempt.
- Expected: 10th sets `locked_until = now + 15min`; subsequent attempts
  rejected with locked error.
- Code: auth.go:326-327.
- Coverage: ✅ users_lockout_test.go.

### 10.4 Successful login resets failed counter
- Scenario: 5 failed attempts, then correct password.
- Expected: counter reset to 0.
- Coverage: ⚠️ partial.

### 10.5 IP-based limit during legitimate burst
- Scenario: one office NAT'd behind a single IP exceeds 100 req/min.
- Expected: 429 with `X-RateLimit-*` headers.
- Code: middleware/ratelimit.go.
- Coverage: ✅ ratelimit_test.go.
- Edge: NAT/CGNAT shared IPs may DoS legitimate users — document as known
  trade-off.

### 10.6 Distributed clock skew
- Scenario: rate limit window expires across DB nodes with different clocks.
- Expected: minimal impact since windows are short; worth ensuring the
  limiter uses server time consistently.
- Coverage: ❓.

### 10.7 Lockout interplay with MFA
- Scenario: user is account-locked from failed passwords; admin manually
  unlocks; failed MFA counter is still elevated.
- Expected: MFA and password counters are independent; manual unlock should
  reset both.
- Code: separate counters in users table.
- Coverage: ❌. Suggested: explicit reset on superuser unlock.

---

## Coverage Gaps — Risk-Prioritized

The following uncovered scenarios are the highest leverage to address next.

### Critical (security or data integrity)

1. **9.2 Multi-level cascading delete** (database/regions.go:1239) only
   removes one level of children. Without `ON DELETE CASCADE` on the
   schema, grandchild rows are orphaned. **Verify schema, then add a
   3-level subtree deletion test.**
2. **9.5 Cascading delete ignores related tables** —
   sub_region_membership_requests, vouches, blocklist_proposals,
   audit_log entries referencing the region. **Schema audit + test that
   delete succeeds with active dependent rows.**
3. **7.4 Signal-group three-way XOR enforcement** is DB-side only; the
   handler does not validate `school_id` / `district_id` are absent when
   creating a region group. **Add handler-level pre-check + a CHECK
   constraint regression test.**
4. **5.9 Sub-region invitations not checked against blocklist** — admins
   could invite a blocked user. **Add a blocklist check at invitation
   creation.**
5. **1.9 Failed-attempt counter is per-code, not per-attempter** — an
   attacker can DoS another user's postcard verification by guessing 5
   codes. **Scope failures by `(code, attempter_user_id)` or rate-limit by
   attempter.**

### High (race conditions / consistency)

6. **2.12 / 8.1 Bootstrap-mode staleness** — `IsRegionInBootstrapMode` is
   read outside the vouch tx; admin-count transitions during a vouch can
   produce inconsistent threshold decisions. **Re-read inside the tx or
   document the staleness explicitly.**
7. **6.8 Admin-count fluctuation during proposal lifecycle** — required
   vote count recalculates per-vote based on live admin count; proposals
   may stall or auto-approve unexpectedly. **Document and test.**
8. **1.6 / 2.5 / 5.7 / 6.7 Concurrent-vote / concurrent-vouch / concurrent
   verification races** — design uses FOR UPDATE locks correctly but no
   regression tests assert this. **Add table-driven concurrency tests
   using `t.Parallel` + `WaitGroup`.**

### Medium (correctness around boundaries)

9. **5.5 Membership expiration at vote boundary** — approving vote arriving
   ~7 days after request creation may slip through. **Fake-clock test.**
10. **3.5 MFA secret missing for enabled user** — silent fail-open risk if
    future code changes the empty-secret branch. **Pin behavior with a
    test.**
11. **7.6 Non-transactional signal-group fallback** (`h.db == nil`) can
    orphan groups in test code. **Remove the fallback or assert rollback in
    tests.**
12. **6.12 Invite-link emails must never contain the link** — assert body
    content in notification_worker_test.

### Low (hygiene / documentation)

13. **1.2 Whitespace-only address fields** pass validation. Trim before
    empty-string check.
14. **4.7 Region uniqueness** — same name + parent allowed; either enforce
    or document.
15. **10.2 Auth rate limiter fail-open vs MFA fail-closed** — divergent
    policy worth documenting.
16. **2.15 User lookup ambiguity** — username matching a UUID string could
    resolve to wrong user; validate UUID format first.

---

## Out of Scope

- Internationalization edges (address formats outside US/Canada Postgrid
  coverage).
- Mapbox quota exhaustion / billing edges.
- Email deliverability (bounces, spam filters).
- Migration rollback edge cases — covered by `db-migrate-down` integration.
- Performance / scale (this document is correctness-focused).
