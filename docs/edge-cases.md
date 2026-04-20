# Edge Cases Enumeration

Structured enumeration of edge cases across 6 critical domains. Each entry includes a priority (P0–P2) and whether test coverage exists.

## 1. Authentication (Auth)

| # | Edge Case | Priority | Tested |
|---|-----------|----------|--------|
| A1 | Whitespace-only username on registration | P0 | Yes |
| A2 | Whitespace-only email on registration | P0 | Yes |
| A3 | Whitespace-only password on registration | P0 | Yes |
| A4 | Unicode characters in password (emoji, CJK, combining marks) | P1 | Yes |
| A5 | Email case sensitivity on login (e.g., `User@Test.COM` vs `user@test.com`) | P0 | Yes |
| A6 | Account lockout expires exactly at boundary (locked_until == now) | P0 | Yes |
| A7 | Login attempt on soft-deleted account | P1 | Yes |
| A8 | Login attempt on blocked account | P1 | Yes |
| A9 | MFA setup token expires mid-flow (10-minute window) | P0 | Yes |
| A10 | Registration with email alias (`user+tag@example.com`) | P0 | Yes |
| A11 | Password at minimum boundary (exactly 12 characters) | P1 | Yes |
| A12 | Password at maximum boundary (exactly 128 characters) | P1 | Yes |
| A13 | Password exceeding maximum (129 characters) | P1 | Yes |
| A14 | Username with leading/trailing spaces | P1 | Yes |
| A15 | Duplicate registration with different email casing | P0 | Yes |
| A16 | Login with username instead of email | P1 | Yes |
| A17 | Empty JSON body on login | P0 | Yes |
| A18 | Failed login count resets on successful login | P1 | No |

## 2. Verification

| # | Edge Case | Priority | Tested |
|---|-----------|----------|--------|
| V1 | Verification code reuse after successful verification | P0 | Yes |
| V2 | Expired verification code at exact boundary | P0 | Yes |
| V3 | Verification code case sensitivity (uppercase vs lowercase hex) | P0 | Yes |
| V4 | Vouch for already vouch-verified user | P1 | Yes |
| V5 | Vouch during bootstrap-to-normal mode transition | P0 | Yes |
| V6 | Self-vouch attempt | P0 | Yes |
| V7 | Circular vouch detection (A vouched B, B tries to vouch A) | P0 | Yes |
| V8 | Verification code lockout at 5 failed attempts | P0 | Yes |
| V9 | Vouch from user in different region (no shared ancestor below state) | P1 | Yes |
| V10 | State-level vouch attempt (explicitly forbidden) | P0 | Yes |
| V11 | Monthly vouch limit boundary (10th vouch succeeds, 11th fails) | P0 | Yes |
| V12 | Vouch for blocked user in region | P0 | Yes |
| V13 | Postcard verification rate limit (3 requests per 30 days) | P1 | No |
| V14 | Vouch lookup by email vs username vs ID | P1 | Yes |
| V15 | PO Box address rejection | P1 | No |
| V16 | CMRA address rejection | P1 | No |

## 3. Regions

| # | Edge Case | Priority | Tested |
|---|-----------|----------|--------|
| R1 | Self-intersecting polygon (bowtie shape) | P0 | Yes |
| R2 | Zero-area polygon (all points collinear) | P0 | Yes |
| R3 | Deeply nested hierarchy creation (state→county→city→neighborhood→block) | P1 | Yes |
| R4 | Region name at maximum length boundary | P1 | Yes |
| R5 | Duplicate region creation in same location (name+type collision) | P0 | Yes |
| R6 | Region creation outside admin boundary | P0 | Yes |
| R7 | State region with parent (invalid hierarchy) | P0 | Yes |
| R8 | County region without state parent | P0 | Yes |
| R9 | City block without neighborhood parent | P0 | Yes |
| R10 | Empty geometry field | P0 | Yes |
| R11 | Region name with only whitespace | P1 | Yes |
| R12 | Region name with Unicode characters | P2 | Yes |
| R13 | Polygon outside US bounds (non-superuser) | P1 | Yes |
| R14 | Polygon with very many vertices (performance) | P2 | No |
| R15 | Region deletion cascades to child regions and user_regions | P0 | No |
| R16 | Creating region with centroid exactly on boundary of admin region | P2 | No |

## 4. Consensus Voting (Blocklist Proposals)

| # | Edge Case | Priority | Tested |
|---|-----------|----------|--------|
| B1 | Double voting on same proposal | P0 | Yes |
| B2 | Admin loses status after proposal created, before vote counted | P0 | Yes |
| B3 | Rate limit boundary at month edge (votes in December vs January) | P1 | Yes |
| B4 | Proposal against soft-deleted user | P1 | Yes |
| B5 | Voting on already-approved proposal (status != pending) | P0 | Yes |
| B6 | Voting on already-rejected proposal | P0 | Yes |
| B7 | Proposer's auto-vote counted correctly | P1 | Yes |
| B8 | Consensus threshold exactly met (not exceeded) | P0 | Yes |
| B9 | Blocklist proposal when fewer than 3 admins in region | P0 | Yes |
| B10 | Blocklist proposal for superuser | P0 | No (existing) |
| B11 | Proposal reason with only whitespace | P1 | Yes |
| B12 | Proposal reason exceeding max length | P2 | Yes |
| B13 | Vote on proposal from different region (not admin) | P0 | Yes |
| B14 | Concurrent votes reaching threshold simultaneously | P1 | No |
| B15 | Blocklist cascading to sub-regions | P1 | No |

## 5. Schools

| # | Edge Case | Priority | Tested |
|---|-----------|----------|--------|
| S1 | Self-vouching attempt | P0 | Yes |
| S2 | Vouching for user not in school (hasn't joined) | P0 | Yes |
| S3 | Leaving school while vouch is pending | P1 | Yes |
| S4 | Vouch for blocked user in school | P0 | Yes |
| S5 | Monthly vouch limit boundary (10th succeeds, 11th fails) | P0 | Yes |
| S6 | Bootstrap mode transition (2nd admin joins, mode changes) | P0 | Yes |
| S7 | Duplicate school join attempt | P0 | Yes |
| S8 | Vouching in bootstrap mode (any member can vouch) | P1 | Yes |
| S9 | Vouching in normal mode (only verified members) | P1 | Yes |
| S10 | Admin cap at 3 per school in bootstrap | P1 | Yes |
| S11 | Vouch lookup by email vs username vs ID | P1 | Yes |
| S12 | Already-vouched duplicate vouch attempt | P0 | Yes |
| S13 | School Signal group creation by non-admin | P1 | No |
| S14 | District Signal group creation permissions | P1 | No |
| S15 | Joining school after being blocked | P0 | No (existing) |

## 6. Membership (Sub-Region)

| # | Edge Case | Priority | Tested |
|---|-----------|----------|--------|
| M1 | Requesting membership in region user already belongs to | P0 | Yes |
| M2 | Voting on expired membership request | P0 | Yes |
| M3 | Concurrent duplicate membership requests for same region | P0 | Yes |
| M4 | Request from user not in parent region | P0 | Yes |
| M5 | Vote on request from non-admin | P0 | Yes |
| M6 | Double vote on same request | P0 | Yes |
| M7 | Request for top-level region (no parent) | P0 | Yes |
| M8 | Exceeding max pending requests per user | P1 | Yes |
| M9 | Admin status granted to fully verified user on approval | P1 | Yes |
| M10 | Request expiration exactly at boundary (expires_at == now) | P1 | Yes |
| M11 | Voting after requester leaves parent region | P1 | No |
| M12 | Admin invitation response after expiration | P1 | No |
| M13 | Accepting invitation to region user already joined | P1 | No |
| M14 | Canceling already-approved request | P1 | Yes |

---

## Summary

| Domain | Total Cases | P0 | P1 | P2 | Test Coverage |
|--------|------------|----|----|----|----|
| Authentication | 18 | 8 | 9 | 1 | 17/18 |
| Verification | 16 | 9 | 7 | 0 | 13/16 |
| Regions | 16 | 7 | 5 | 4 | 12/16 |
| Consensus Voting | 15 | 7 | 5 | 3 | 12/15 |
| Schools | 15 | 7 | 6 | 2 | 12/15 |
| Membership | 14 | 7 | 5 | 2 | 11/14 |
| **Total** | **94** | **45** | **37** | **12** | **77/94** |
