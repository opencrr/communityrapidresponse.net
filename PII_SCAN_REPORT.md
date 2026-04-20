# PII Exposure Scan Report

**Repository:** opencrr  
**Date:** 2026-04-20  
**Prior Scan:** 2026-04-13  
**Scope:** All source files, configs, scripts, and data files (excluding vendored code in static/js/vendor/)

---

## Remediation Status (from 2026-04-13 scan)

| # | Finding | Status |
|---|---------|--------|
| 1 | Mapbox public token in HTML template | **Open** (medium) — unchanged, public token by design |
| 2 | Email address logged in SMTP service | **FIXED** — now uses `redactEmail()` |
| 3 | Email address logged in SendGrid service | **FIXED** — now uses `redactEmail()` |
| 4 | Email address logged in Mock service | **FIXED** — now uses `redactEmail()` |
| 5 | User ID in error message (mfa.go) | **Open** (low) — unchanged, UUID only |
| 6 | Test data uses semi-realistic emails | **Open** (low) — unchanged |
| 7 | .gitignore missing cert/key patterns | **FIXED** — patterns added |
| 8 | PII_SCAN_REPORT.md not in .gitignore | **FIXED** — added |
| 9 | Dev credentials in docker-compose.yml | **Open** (low) — mitigated by prod validation |
| 10 | address_hash field never populated | **Open** (low) — feature gap, no PII stored |

**Summary:** 4 of 10 prior findings resolved. No regressions introduced.

---

## Current Findings

### Finding 1: Mapbox Public Token in HTML Template

- **file:** templates/index.html
- **line:** 20
- **category:** env-secret
- **severity:** medium
- **detail:** A Mapbox public access token (`pk.eyJ...`) is hardcoded in the HTML template. While `pk.` prefix tokens are PUBLIC tokens designed for client-side browser use and are not secrets, an unrestricted public token could be abused for API quota consumption.
- **recommendation:** Configure URL restrictions for this token in the Mapbox dashboard to limit usage to your domain(s). No need to move to env var since public tokens are meant for browser exposure.

---

### Finding 2: User ID in Error Message

- **file:** internal/handlers/mfa.go
- **line:** 325
- **category:** pii-in-logs
- **severity:** low
- **detail:** `fmt.Errorf("MFA secret is nil or empty for user %s", user.ID)` includes user ID in the error message. User IDs are UUIDs (pseudonymous identifiers), not directly PII, making this low risk.
- **recommendation:** Consider using a structured error with the user ID as a separate field rather than interpolating into the message string, for consistency with the structured logging approach used elsewhere.

---

### Finding 3: Test Data Uses Semi-Realistic Email Addresses

- **file:** internal/services/email_test.go, tests/email_verification_test.go
- **line:** various (email_test.go:29-33, email_verification_test.go:1199-1202)
- **category:** hardcoded-pii
- **severity:** low
- **detail:** Test emails use real-looking patterns like `john.doe@gmail.com`, `John.Doe@Gmail.com`, `J.Smith@GOOGLEMAIL.COM`. While these are clearly test data for email normalization validation, they could match real people's addresses.
- **recommendation:** Prefer RFC 2606 reserved domains (`@example.com`, `@test.example`) or obviously fake patterns. Most test files already use `@example.com` or `@e2e.test` which is good practice.

---

### Finding 4: Development Credentials in docker-compose.yml

- **file:** docker-compose.yml
- **line:** 8, 33, 72, 102, 143, 216, 244
- **category:** env-secret
- **severity:** low
- **detail:** docker-compose.yml contains development/test credentials (e.g., `testroot`, `devpassword`, test JWT secrets, test MFA keys). These are clearly labeled as development defaults and the production config in `internal/config/config.go` explicitly rejects these known defaults in production/staging environments.
- **recommendation:** No immediate action needed. The production validation guard is an excellent defense. Consider adding a comment in docker-compose.yml noting the production rejection mechanism for developer awareness.

---

### Finding 5: Address Hash Field Defined But Never Populated

- **file:** internal/models/user.go (line 36), migrations/016_blacklist_proposal_expires.up.sql (line 22)
- **line:** 36, 22
- **category:** unencrypted-storage
- **severity:** low
- **detail:** The `address_hash` field (CHAR(64), SHA-256) is defined in the user model and database schema but is never populated during the verification flow. The blocklist system references it for address-based blocking but it's always NULL. This is a feature gap rather than a storage vulnerability — no address data is actually stored unencrypted.
- **recommendation:** Either implement address hash population during postcard verification to enable address-based blocklisting, or remove the unused column to reduce schema surface area.

---

### Finding 6: CI Workflow Uses Hardcoded Test Credentials

- **file:** .github/workflows/test.yml
- **line:** 29, 53, 65, 80-84, 88-92, 97-101, 107-111, 116-120
- **category:** env-secret
- **severity:** low
- **detail:** The CI workflow uses hardcoded test database credentials (`testroot`, `root`). These are ephemeral CI service containers destroyed after each run, and the passwords are identical to those in docker-compose.yml (which are rejected in production). No risk of credential reuse.
- **recommendation:** No action needed. CI test credentials for ephemeral containers are standard practice and pose no risk.

---

## Positive Findings (No Issues)

The following areas were scanned and found to be properly secured:

- **Email logging**: All email services now use `redactEmail()` helper (shows only first char + `***@domain.com`)
- **Password storage**: bcrypt with cost factor 12 (internal/handlers/auth.go:193)
- **MFA secret encryption**: AES-256-GCM with random nonce (internal/services/mfa.go:79-98)
- **MFA backup codes**: bcrypt hashed individually (internal/services/mfa.go:157-176)
- **Password reset tokens**: SHA-256 hashed, raw token never stored (internal/handlers/auth.go:785)
- **Zero-address-storage policy**: Verified in postgrid.go, lob.go, mapbox.go, verification.go — addresses processed in memory only, never written to database
- **No SSN, credit card, or phone number storage** anywhere in the schema
- **School addresses**: Public NCES institutional data (not user PII)
- **Audit logging**: Metadata only, no PII in audit details
- **Encryption keys**: Private keys are wrapped/encrypted before storage
- **Production config validation**: Rejects known development defaults in production/staging
- **.gitignore coverage**: Covers `.env*`, `*.pem`, `*.key`, `*.crt`, `*.pfx`, `*.p12`, `credentials.*`, `secrets.*`, `*.csv`, `*.xlsx`, `*.xls`, and security reports
- **No tracked sensitive files**: No certificates, keys, or data dumps in git history
- **CI workflow**: Uses only GitHub Actions secrets (`${{ secrets.* }}`) for real credentials; test credentials are for ephemeral containers
- **No PII in error wrapping chains**: `fmt.Errorf` calls contain only geographic names or generic IDs, not user PII

---

## Summary

| Category | Critical | High | Medium | Low | Total |
|---|---|---|---|---|---|
| hardcoded-pii | 0 | 0 | 0 | 1 | 1 |
| pii-in-logs | 0 | 0 | 0 | 1 | 1 |
| env-secret | 0 | 0 | 1 | 2 | 3 |
| unencrypted-storage | 0 | 0 | 0 | 1 | 1 |
| gitignore-gap | 0 | 0 | 0 | 0 | 0 |
| **Total** | **0** | **0** | **1** | **5** | **6** |

**Overall Assessment:** The repository demonstrates strong security practices with significant improvement since the last scan. The two prior high-severity findings (email addresses in logs) have been fully remediated with a `redactEmail()` helper. No critical or high findings remain. The single medium finding (Mapbox public token) is acceptable by design but warrants URL restriction. All low findings are informational with minimal risk. The zero-address-storage policy, encryption practices, and production configuration validation remain exemplary.

**Comparison to Prior Scan (2026-04-13):**
- Findings reduced from 10 to 6
- High-severity findings reduced from 2 to 0
- Medium-severity findings reduced from 4 to 1
- All email PII logging issues resolved
- All .gitignore gaps resolved
