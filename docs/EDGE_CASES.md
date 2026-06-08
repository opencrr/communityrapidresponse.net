# Edge Cases — Core Flows

A living checklist of edge cases reviewers and test authors should keep in mind when
modifying the core flows of Community Rapid Response. This is **not a spec** — it
is a survey of what *could* go wrong, organized so that gaps are easy to spot.

## How to use this document

- **Reviewers**: when a PR touches one of the listed flows, scan the matching section
  and confirm any items the change interacts with are still handled.
- **Test authors**: items marked `Uncovered` or `Partial` are high-value targets for
  new tests. The suggested test type (unit/integration/e2e) is a hint, not a mandate.
- **Implementers**: if you change a behavior listed here, update the matching row.
- This document intentionally lives next to `DESIGN.md`. Flow names mirror the
  numbered flows in `CLAUDE.md` / `DESIGN.md` so they can be cross-referenced.

### Coverage legend

| Tag | Meaning |
|---|---|
| `Covered` | At least one targeted test exercises this edge case. |
| `Partial` | Adjacent behavior is tested, but the specific boundary or invariant is not asserted directly. |
| `Uncovered` | No test exercises this edge case as of the last sweep. |

Categories used inside each flow:

- **Validation** — malformed/missing input, boundary values
- **Authorization** — role boundaries, tier requirements, defense-in-depth
- **Spatial** — `ST_Contains`, nullable geometry, US-bounds, geocoding
- **Concurrency** — races, transactional atomicity, double-spend / replay
- **Rate-limit & quota** — per-IP, per-user, per-resource counters
- **Time & expiration** — token TTLs, proposal/request expiry, lockouts
- **State transitions** — pending → approved/rejected/expired/cancelled
- **Cascade & consistency** — deletes, blacklists, sub-region propagation
- **Privacy invariants** — zero-address-storage, no-secrets-in-email, audit redaction
- **Observability** — audit log entries, structured error surfaces

---

## 1. Auth / MFA

### Validation
- `Covered` Password below 12-char minimum is rejected (`auth_test.go`).
- `Covered` Duplicate email registration is rejected.
- `Partial` Email normalization (trimming, casing, `+` alias) — basic alias check exists; full Unicode normalization not asserted. *(unit)*
- `Uncovered` Registration with whitespace-only or zero-length username field. *(unit)*

### Authorization
- `Covered` `mfa_setup` token cannot be used as a `full` token (`mfa_test.go`).
- `Covered` `pending_mfa` token cannot be used as a `full` token.
- `Covered` Superuser status is re-read from DB, not trusted from JWT (`admin_test.go:RequireSuperuser_DBRecheck`).
- `Partial` Reused `pending_mfa` token after successful MFA (replay) — token type checked, but per-token nonce not asserted. *(integration)*

### Concurrency
- `Covered` MFA backup-code consumption is transactional (no double-redeem).
- `Uncovered` Concurrent login attempts during account lockout transition (10th failure + parallel attempt). *(integration)*
- `Uncovered` Password reset token consumed while another reset is in flight. *(integration)*

### Rate-limit & quota
- `Covered` Login 10 / 5 min per IP (`auth_ratelimit_e2e_test.go`).
- `Covered` Register 3 / hr per IP.
- `Covered` Forgot-password 5 / 15 min, reset-password 10 / 15 min, resend-verification 3 / 15 min.
- `Partial` Account lockout resets on successful login — reset is implemented, but boundary "9 failures then 1 success then 9 failures" not asserted. *(integration)*
- `Uncovered` Rate-limit behavior under IPv6 vs IPv4 keying, and under proxy / `X-Forwarded-For`. *(integration)*

### Time & expiration
- `Covered` Account lockout 15 minutes after 10 failures.
- `Partial` Email-verification JWT 24h expiration — token created with TTL, but post-expiry attempt path not exercised. *(integration)*
- `Partial` `mfa_setup` / `pending_mfa` 10-minute TTL — generation tested, expiry path not. *(integration)*
- `Uncovered` Full JWT used past 24h expiration on a protected endpoint. *(integration)*
- `Uncovered` Clock skew between issuance and validation (server with skewed clock). *(unit)*

### State transitions
- `Covered` Soft-delete vs hard-delete on account deletion based on blocklist association (`auth_test.go:DeleteAccount`).
- `Partial` Login attempt against a soft-deleted (scrubbed) account. *(integration)*
- `Uncovered` MFA disabled mid-session — existing `full` tokens still valid until expiry; behavior not explicitly tested. *(integration)*

### Privacy invariants
- `Covered` MFA TOTP secret encrypted with AES-256-GCM before DB write.
- `Uncovered` Audit-log payload for failed login does not echo password / plaintext token. *(unit)*
- `Uncovered` Password-reset email contains no token leak beyond the single-use link. *(unit)*

### Observability
- `Covered` Audit entries for `UserRegistered`, `UserLogin`, `UserLoginFailed`, `AccountLocked`, `PasswordChanged`, `MFASetupCompleted`, `MFAVerified`.
- `Uncovered` Audit entry when JWT is rejected due to expired claim vs. malformed signature (distinguishable cause). *(unit)*

---

## 2. Postcard Verification

### Validation
- `Covered` PO Box rejection (`verification_test.go:417`).
- `Covered` CMRA rejection (`verification_test.go:456`).
- `Uncovered` Address validation when Postgrid returns ambiguous / multi-candidate response. *(unit)*
- `Uncovered` Unicode / extended-ASCII address normalization. *(unit)*

### Authorization
- `Covered` Tier upgrade to `TierPostcard` on successful code entry.
- `Partial` Auto-upgrade of `user_regions` rows to admin when the same user was already vouch-verified — code path exists, edge case where vouch row is `pending` at moment of postcard verification not asserted. *(integration)*

### Spatial
- `Partial` Geocoded coordinates contained by an existing region polygon — assignment happens via `ST_Contains` but containment-boundary edge (point on polygon edge) not asserted. *(unit)*
- `Uncovered` Geocoded address that falls outside any known region (nullable geometry parent only). *(integration)*
- `Uncovered` Address in a neighborhood whose geometry is `NULL` (parent_id-only assignment). *(integration)*

### Concurrency
- `Uncovered` Two simultaneous postcard requests for the same user (second should be rate-limited or invalidate the first). *(integration)*
- `Uncovered` Code-verification attempt arriving while request is being expired by background job. *(integration)*

### Rate-limit & quota
- `Partial` 3 requests / 30 days — counter exists, 30-day rolling-window boundary not explicitly asserted. *(integration)*
- `Covered` Max 5 code-entry attempts before permanent lock (`verification_test.go:VerifyCode_Lockout`).

### Time & expiration
- `Covered` Code rejected after request expires.
- `Uncovered` Code submitted exactly at expiry boundary (server-time tie). *(unit)*

### State transitions
- `Uncovered` Re-issuing a verification request after a previous one expired vs. after one was used. *(integration)*

### Cascade & consistency
- `Uncovered` Postcard verification when the assigned region is deleted between request and code entry. *(integration)*

### Privacy invariants
- `Partial` Address never persisted — service uses in-memory pipeline, but no test asserts absence of address columns or scrubs across audit / sentry / structured logs. *(unit + grep)*
- `Uncovered` Postgrid request/response objects are not logged at INFO level. *(unit)*
- `Uncovered` Audit entry for `PostcardRequested` contains no address fragment. *(unit)*

### Observability
- `Covered` `PostcardRequested`, `PostcardVerified` audit entries.
- `Uncovered` Audit entry distinguishes "code wrong" from "code expired" from "request locked". *(unit)*

---

## 3. Vouch Verification

### Validation
- `Covered` Circular vouch rejected (`verification_test.go:1311`).
- `Partial` State-level vouch forbidden — guard exists in code, no explicit negative test. *(unit)*
- `Uncovered` Vouch where voucher and vouchee share no ancestor up to city/county. *(integration)*

### Authorization
- `Covered` Normal-mode voucher must be fully verified (postcard + vouch).
- `Covered` Bootstrap-mode allows pending members to vouch.
- `Uncovered` Voucher who became vouch-verified less than the cooldown ago (bootstrap mode). *(integration)*

### Rate-limit & quota
- `Covered` 10 vouches / month per user.
- `Partial` Bootstrap cooldown between vouches by same user — cooldown enforced (`verification_test.go:2831`); boundary (cooldown + 1s) not asserted. *(unit)*

### Time & expiration
- `Uncovered` Pending vouch request behavior over 30+ day stale window. *(integration)*

### State transitions
- `Covered` Threshold cascades upward when met (`VouchComplete` audit).
- `Partial` Bootstrap → normal transition mid-flight: a vouch given during bootstrap when region tips to ≥3 admins between vote validation and write. *(integration)*
- `Uncovered` Revocation of a vouch (manual or via superuser) reverts cascade correctly. *(integration)*

### Cascade & consistency
- `Uncovered` Threshold met in a region whose parent geometry was just updated. *(integration)*

### Privacy invariants
- `Uncovered` Vouch records do not surface vouchee address or PII in admin search responses. *(unit)*

### Observability
- `Covered` `VouchRequested`, `VouchGiven`, `VouchComplete` audit entries.

---

## 4. Region Creation & Hierarchy

### Validation
- `Covered` Type-parent hierarchy enforced (state→county→city→neighborhood→block).
- `Uncovered` Self-intersecting polygon submitted by admin. *(unit)*
- `Uncovered` Polygon with > N vertices (DoS surface). *(unit)*
- `Uncovered` Polygon spanning the antimeridian / dateline. *(unit)*

### Authorization
- `Covered` Non-superuser cannot create a region outside their admin region (`regions_test.go:Create_AdminBoundaryValidation`).
- `Covered` Superuser bypasses admin-region containment.
- `Partial` Admin who lost vouch verification mid-session attempting a region create. *(integration)*

### Spatial
- `Partial` `ST_Contains` validates admin-region containment — happy path covered, boundary point (on edge of admin polygon) untested. *(unit)*
- `Partial` `IsGeometryWithinUS` — invoked, but US-edge / Alaska / Hawaii / territories edges not asserted. *(unit)*
- `Uncovered` `ST_Contains` query against region with `NULL` geometry — must be filtered out, no test asserts this. *(unit)*
- `Uncovered` Spatial index disabled (per `CLAUDE.md`) — performance regression test for large polygon set. *(integration)*

### Concurrency
- `Uncovered` Two admins creating overlapping sub-regions simultaneously. *(integration)*

### Time & expiration
- N/A (regions do not expire).

### State transitions
- `Covered` Region deletion cascades to child regions (`deletion_proposals_test.go:ConsensusSubRegion`).
- `Partial` Region update (rename) while a deletion proposal targets it. *(integration)*

### Cascade & consistency
- `Covered` Cascade delete of `user_regions`, `signal_groups`, child regions on region delete.
- `Uncovered` Cascade behavior when child region has active membership requests. *(integration)*
- `Uncovered` Audit log retention of deleted region's history (90-day window). *(integration)*

### Privacy invariants
- N/A.

### Observability
- `Covered` `RegionCreated`, `RegionUpdated`, `RegionDeleted` audit entries.

---

## 5. Sub-region Membership Requests & Invitations

### Validation
- `Covered` Parent-region membership required before request.
- `Uncovered` `MaxPendingRequestsPerUser` limit not asserted explicitly. *(integration)*
- `Uncovered` Self-invitation by an admin to themselves. *(unit)*

### Authorization
- `Covered` Only admins can vote on requests.
- `Covered` Auto-grant of admin flag on approval when invitee is fully verified.
- `Uncovered` User who loses verification between request and approval — should the admin grant still apply? *(integration)*

### Concurrency
- `Uncovered` Two admins voting simultaneously on the same request (race on the 2nd approval). *(integration)*
- `Uncovered` Admin votes approve while another admin votes reject in the same instant. *(integration)*

### Rate-limit & quota
- `Uncovered` Burst of invitations from one admin to many users. *(integration)*

### Time & expiration
- `Partial` 7-day request expiration — voting on expired request returns conflict, but exact 7-day boundary not asserted. *(unit)*
- `Uncovered` Request created just before parent region is deleted. *(integration)*

### State transitions
- `Covered` Invitation accept / decline paths.
- `Uncovered` Cancel-by-user after the first approval vote but before the second. *(integration)*

### Cascade & consistency
- `Uncovered` Approval applied while parent region is being deleted (race). *(integration)*

### Privacy invariants
- `Uncovered` Invitation email does not include requester address or PII. *(unit)*

### Observability
- `Covered` `SubRegionRequestCreated`, `SubRegionRequestVoted`, `SubRegionRequestApproved` audit entries.

---

## 6. Blacklisting (Blocklist Proposals)

### Validation
- `Covered` Proposal rejected when region has < 3 admins (vote floor).
- `Uncovered` Proposal targeting a superuser. *(integration)*
- `Uncovered` Proposal targeting a user already blocklisted in this region. *(unit)*

### Authorization
- `Covered` Only admins may propose / vote.
- `Uncovered` Voter who lost admin status between proposal and vote. *(integration)*

### Concurrency
- `Partial` Transactional vote + apply — code uses a transaction; explicit "two simultaneous 3rd votes" race not asserted. *(integration)*

### Rate-limit & quota
- `Partial` `ProposalRateLimitPerMonth` — rate limit test exists but monthly boundary not asserted. *(integration)*

### Time & expiration
- `Uncovered` 7-day proposal expiration (created + 7d). *(integration)*

### State transitions
- `Uncovered` Proposal where target user deletes their account mid-vote. *(integration)*

### Cascade & consistency
- `Partial` Cascade to sub-regions — `IsUserBlockedAnywhere` covers reads; explicit assertion that a child-region row is created on parent blacklist is missing. *(integration)*
- `Uncovered` Blacklist applied to a user who is currently in a Signal group — group membership revocation flow. *(integration)*

### Privacy invariants
- `Uncovered` Blacklist reason / notes do not leak into the target user's notifications. *(unit)*

### Observability
- `Covered` `ProposalCreated`, `ProposalVoted`, `ProposalApplied`, `AffectedUserBlocklisted` audit entries.

---

## 7. Deletion Proposals (3-Admin Consensus)

### Validation
- `Covered` 3-admin consensus required across region / school / district scopes.
- `Uncovered` Proposal to delete a region that has active sub-region membership requests. *(integration)*

### Authorization
- `Covered` Scope-correct admin check (region admin vs school admin vs district admin).
- `Uncovered` Superuser deleting via the consensus flow vs the direct delete endpoint — equivalence not asserted. *(integration)*

### Concurrency
- `Uncovered` Two admins casting the 3rd vote simultaneously. *(integration)*

### Time & expiration
- `Partial` Proposal expiration — superuser-expire path covered (`ExpireProposal`); 7-day natural expiration boundary not. *(unit)*

### State transitions
- `Uncovered` Deletion proposal raised while a rename/update is in flight. *(integration)*

### Cascade & consistency
- `Covered` Sub-region cascade on region deletion.
- `Uncovered` Signal-group consensus deletion that leaves the region without any signal group at all (regression of bootstrap state). *(integration)*

### Observability
- `Covered` `DeletionProposalCreated`, `DeletionProposalVoted`, `DeletionProposalApplied` audit entries.

---

## 8. Signal Group Invite-Link Updates

### Validation
- `Covered` Max 5 groups / region (`signal_groups_test.go:235`).
- `Uncovered` Encrypted payload that does not decrypt under any wrapped key (corrupted upload). *(unit)*
- `Uncovered` IV reuse across updates (cryptographic invariant). *(unit)*

### Authorization
- `Covered` Bootstrap mode blocks group creation until ≥ 3 full admins.
- `Uncovered` Admin who lost vouch verification mid-proposal. *(integration)*

### Concurrency
- `Uncovered` Two admins proposing different invite-link updates within the same minute (which wins, are both rate-limited?). *(integration)*

### Rate-limit & quota
- `Uncovered` Notification rate limit: 1 email per user per group per 24h. *(integration)*
- `Uncovered` Burst of invite-link updates triggering many notifications. *(integration)*

### Time & expiration
- `Uncovered` Invite-link proposal voting window. *(integration)*

### State transitions
- `Uncovered` Proposal applied while a region delete is in flight. *(integration)*

### Cascade & consistency
- `Uncovered` Signal group whose region was deleted before a pending proposal applies. *(integration)*

### Privacy invariants
- `Uncovered` Invite-link email body contains **no** invite link, encrypted payload, or wrapped key — only "log in to view". *(unit, regex check)*
- `Uncovered` AES-256-GCM payload algorithm validation on read (not just write). *(unit)*
- `Uncovered` Encrypted payload absent from sentry / structured logs on error paths. *(unit)*

### Observability
- `Covered` `SignalGroupCreated`, `SecretUpdateApplied` audit entries.
- `Uncovered` Audit entry on a *failed* secret update (decryption fail, vote race lose). *(unit)*

---

## 9. Schools & School Districts

### Validation
- `Covered` NCES lookup by name / state.
- `Uncovered` Joining a school that has been administratively closed in NCES (stale record). *(integration)*

### Authorization
- `Covered` Bootstrap (3-vouch) vs normal (2-vouch) mode for school vouches.
- `Partial` District-admin permission inherited from member schools — happy path covered, edge of "district admin status revoked when last admin school is deleted" not asserted. *(integration)*

### Rate-limit & quota
- `Covered` 10 school-vouches / month per user (`schools_test.go:660`).
- `Uncovered` Cooldown between school vouches in bootstrap mode. *(unit)*

### Time & expiration
- `Uncovered` Pending school membership over long stale window. *(integration)*

### State transitions
- `Uncovered` Member with a `school_blocked_users` row attempting to re-join. *(integration)*

### Cascade & consistency
- `Uncovered` School-district deletion behavior when member schools have active Signal groups. *(integration)*

### Privacy invariants
- `Uncovered` School-vouch records do not surface user addresses or postcard status. *(unit)*

### Observability
- `Covered` `SchoolVouchGiven`, `SchoolVouchComplete`, `SchoolMembershipApproved`.

---

## 10. Superuser Operations

### Validation
- `Covered` User search query + pagination.
- `Uncovered` Granting vouch verification to a soft-deleted account. *(integration)*

### Authorization
- `Covered` `requireSuperuser` re-reads `is_superuser` from DB (`admin_test.go:RequireSuperuser_DBRecheck`).
- `Uncovered` Superuser-bit revoked between JWT issuance and a privileged call. *(integration)*

### Concurrency
- `Uncovered` Two superusers granting / revoking vouch for the same user simultaneously. *(integration)*

### State transitions
- `Covered` Status cache invalidated on grant / revoke.
- `Uncovered` Grant vouch where user has no prior `user_regions` rows at all. *(integration)*

### Privacy invariants
- `Uncovered` Search response does not surface user addresses, MFA secret blobs, or password hashes. *(unit, schema check)*

### Observability
- `Covered` `SuperuserUserSearch`, `SuperuserVouchGranted`, `SuperuserVouchRevoked` audit entries.

---

## Top risks / follow-ups

The highest-value uncovered edge cases, in rough priority order. Each is a candidate
for a dedicated test PR.

1. **Signal-group invite-link email scrubbing** — assert via regex that outbound
   invite-link notifications contain no invite link, no encrypted payload, no
   wrapped key. Direct hit on the project's "no secrets in email" invariant.
2. **Notification rate limit (1 / user / group / 24h)** — currently completely
   uncovered; abuse vector for spamming verified users.
3. **`ST_Contains` against NULL-geometry regions** — `CLAUDE.md` notes ~92% of
   neighborhoods lack geometry; a regression that stops filtering NULL would either
   crash queries or silently miss containment. No test asserts this filter.
4. **Address-never-stored invariant, end-to-end** — extend tests to grep audit log
   rows, sentry breadcrumbs, and structured-log output for any street-address
   fragment after a verification request. Primary privacy guarantee of the system.
5. **AES-256-GCM round-trip on signal-group payload** — current tests assert the
   payload is set, not that it decrypts with the expected algorithm. Corruption /
   downgrade attacks would pass silently today.
6. **Concurrency on consensus 3rd vote** — blacklist, deletion, and invite-link
   proposals all share the "3rd approval triggers apply" pattern; none have a
   simultaneous-vote race test. Risk: double-apply or inconsistent state.
7. **Expired full JWT** — exercise the path where a 24h JWT is presented after
   expiry on a protected endpoint, including audit-log differentiation between
   expired vs malformed.
8. **Account lockout reset boundary** — assert behavior at "9 failures → 1 success
   → 9 failures" to confirm the counter resets, not just the timer.
9. **Membership-request cancel between approval votes** — exercise the
   transition where the first admin has already approved but the user cancels;
   ensure the second approval cannot still apply.
10. **Auto-upgrade to admin on postcard verification when vouch row is pending** —
    timing window where the cascade decision is made on stale verification status.
