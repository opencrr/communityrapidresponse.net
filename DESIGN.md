# DESIGN.md - Community Rapid Response

## Project Overview

**Community Rapid Response** is a secure platform that enables people to connect within specific geographic regions (neighborhoods, city blocks, rural towns) through verified Signal group chats. The system proves residency before granting access, maintaining privacy through ephemeral verification data.

## Core Principles

1. **Privacy First**: All verification data is ephemeral - deleted immediately after confirmation
2. **Encryption at Rest**: All stored data must be encrypted at rest using industry-standard encryption
3. **Dual Verification for Admin Rights**: Users must complete BOTH postcard and vouch verification to gain admin privileges
4. **Community Governance**: Consensus-based moderation for critical actions
5. **Geographic Hierarchy**: Support for city blocks, neighborhoods, and towns
6. **Decentralization Ready**: Start centralized, enable community self-hosting later

## User Roles & Permissions

### Verification Status

Users have two independent verification flags that can be obtained in any order:
- **Postcard-verified**: Proven residency via physical postcard sent to their address
- **Vouch-verified**: Endorsed by 2+ fully verified users from the same region

### Unverified Users (Neither verification)
- **Status**: New account, no verification completed
- **Privileges**:
  - Can browse map and view regions
  - Can register and create account
  - Can request postcard verification OR vouch verification
- **Restrictions**:
  - Cannot access Signal group invite links
  - Cannot participate in any governance

### Postcard-Verified Only
- **Verification Method**: Physical postcard with one-time code sent to claimed address
- **Privileges**:
  - Can view Signal group invite links for their region
  - Can join and participate in regional Signal groups
  - Can complete vouch verification to gain admin rights
- **Restrictions**:
  - Cannot vouch for others
  - Cannot create regions or Signal groups
  - Cannot propose deletions or blacklisting
  - Read-only access (no admin rights)

### Vouch-Verified Only
- **Verification Method**: Endorsed by 2+ fully verified users from same geographic region
- **Privileges**:
  - Can view Signal group invite links for their region
  - Can join and participate in regional Signal groups
  - Can complete postcard verification to gain admin rights
- **Restrictions**:
  - Cannot vouch for others
  - Cannot create regions or Signal groups
  - Cannot propose deletions or blacklisting
  - Read-only access (no admin rights)

### Fully Verified (Admin) - BOTH Postcard AND Vouch Verified
- **Verification Method**: Completed BOTH postcard verification AND vouch verification
- **Privileges**:
  - Full admin rights for their verified region
  - Can create Signal groups for their region and sub-regions
  - Can vouch for new users
  - Can create geographic sub-regions **only within their verified city limits** (or county limits if in unincorporated area)
  - Can propose asset deletions (requires 3-admin consensus)
  - Can propose user blacklisting (requires 3-admin consensus)
- **Geographic Restrictions**:
  - Region creation limited to the municipal boundary (city/town) where postcard was verified
  - For unincorporated addresses, region creation limited to county boundary
  - Cannot create regions outside their verified administrative boundary
  - Can verify in multiple locations to expand administrative reach

### Verification Comparison Table

| Capability | Unverified | Postcard Only | Vouch Only | Both (Admin) |
|------------|------------|---------------|------------|--------------|
| View regions/map | ✅ | ✅ | ✅ | ✅ |
| Access Signal groups | ❌ | ✅ | ✅ | ✅ |
| Vouch for others | ❌ | ❌ | ❌ | ✅ |
| Create regions | ❌ | ❌ | ❌ | ✅ |
| Create Signal groups | ❌ | ❌ | ❌ | ✅ |
| Propose deletions | ❌ | ❌ | ❌ | ✅ |
| Propose blacklisting | ❌ | ❌ | ❌ | ✅ |

### Superusers
- **Creation**: Must be created directly in the database via CLI commands (no self-registration)
- **Privileges**:
  - All admin privileges globally (not limited to specific regions)
  - Can create regions anywhere (bypasses geographic containment checks)
  - Can delete any region
  - Can grant/revoke vouch verification for any user
  - Can view and manage all users
- **Management Commands**:
  - `just create-superuser` - Interactive superuser creation
  - `just promote-superuser EMAIL` - Promote existing user to superuser
  - `just demote-superuser EMAIL` - Demote superuser to regular user
  - `just list-superusers` - List all superuser accounts
- **Database Flag**: `users.is_superuser = TRUE`

### Application Managers
- Handle edge cases (regions with <3 admins needing deletions)
- System maintenance and oversight
- Bootstrap new regions

## Geographic Hierarchy

```
State
    └── County
        └── City/Town
            └── Locality (optional, e.g., Brooklyn in NYC)
                └── Neighborhood
                    └── City Block
```

**Hierarchy Levels:**
- **State**: Top-level administrative boundary
- **County**: County/district within a state
- **City/Town**: Municipal boundary
- **Locality**: Borough/sub-city division (e.g., Brooklyn in NYC) - only present in large cities
- **Neighborhood**: Named neighborhood area
- **City Block**: Most specific level (future use)

**Granularity by Region Type:**
- **Major Cities with Boroughs**: City Block → Neighborhood → Locality → City
- **Major Cities**: City Block → Neighborhood → City
- **Suburban Areas**: Neighborhood → Town
- **Rural Areas**: Town level only

**Regions Without Geometry:**
- Most neighborhoods (~92%) and some localities lack polygon boundaries in OSM
- These regions are valid and defined by their parent relationship
- Users assigned to regions without geometry are found via parent_id traversal
- Map display uses parent's boundary or shows as label only

**Admin Scope:**
- Users verified at any level receive admin rights up to the neighborhood level
- Example: User verifies at "123 Main St" → gets admin for their block, parent neighborhood, and city

**Geographic Boundary Restrictions:**
- Admins can only create new sub-regions within regions they already have admin access to
- The "most specific region" (smallest containing region) determines the boundary
- Region creation validates new polygon falls entirely within an existing region the user admins
- Uses existing region hierarchy and `user_regions.is_admin` for authorization
- No separate boundary storage needed - leverages existing `geographic_regions` geometry
- Superusers bypass this restriction and can create regions anywhere

## Schools & School Districts

Schools are an organizational entity parallel to geographic regions, enabling communities to form around educational institutions rather than geography.

### Concept

- **Data Source**: School and district data is sourced from the National Center for Education Statistics (NCES) and seeded via `cmd/seed-schools/`
- **Hierarchy**: School Districts > Schools (districts group schools geographically)
- **Independence**: Schools are separate from the geographic region hierarchy; users can belong to both regions and schools simultaneously
- **Signal Groups**: Schools and districts can each have their own Signal groups (up to 5 per entity), independent from region Signal groups

### School Membership & Verification

Users join schools and become verified through a vouch-based system scoped to the school:

1. **Join**: Any authenticated user can join a school (creates `pending` membership)
2. **Vouch**: Verified school members vouch for pending members
3. **Verification**: After enough vouches, user becomes a verified school member
4. **Admin**: Verified members with sufficient vouches gain admin rights for the school

**Bootstrap Mode**: When a school has fewer than 3 admins, relaxed rules apply:
- 3 vouches required instead of 2
- Any verified member can vouch (not just admins)
- This helps new school communities establish their initial admin base

### Three-Way XOR for Signal Groups

The `signal_groups` table enforces that each group belongs to exactly one of: a geographic region, a school, or a school district. The `region_id` column is now nullable, and a CHECK constraint ensures the three-way XOR:

```sql
CHECK (
    (region_id IS NOT NULL AND school_id IS NULL AND district_id IS NULL) OR
    (region_id IS NULL AND school_id IS NOT NULL AND district_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NOT NULL)
)
```

### Seed Schools Command

```bash
just seed-schools           # Seed NCES school data into local database
```

The seeder reads NCES CSV data and creates `school_districts` and `schools` records with NCES IDs, names, addresses, and geographic coordinates.

## System Architecture

### High-Level Components

```
┌─────────────────────────────────┐
│         Web Frontend            │
│   (HTML/CSS/JS + Mapbox GL JS)  │
└────────────────┬────────────────┘
                 │
         ┌───────┴───────┐
         ▼               ▼
┌─────────────────┐  ┌──────────┐
│   Go Backend    │  │  Mapbox  │
│   (HTTP API)    │  │   API    │
└────────┬────────┘  └──────────┘
         │
    ┌────┴────┬──────────────┐
    ▼         ▼              ▼
┌─────────┐ ┌──────────┐ ┌──────────┐
│ MariaDB │ │Postgrid  │ │  Signal  │
│(Spatial)│ │   API    │ │ (Groups) │
└──────────┘ └──────────┘ └──────────┘
```

### Technology Stack

- **Backend**: Go (net/http or lightweight framework like Chi/Gin)
- **Database**: MariaDB with spatial extensions
- **Frontend**: Standard HTML/CSS/JavaScript (progressive enhancement)
- **Mapping**: Mapbox GL JS (map display, geocoding, polygon drawing)
- **Address Validation & Mail**: Postgrid API (address verification, standardization, and postcard delivery)
- **Hosting**: Cloud VPS
- **External Integration**: Signal (via invite links, no API integration)

### Mapbox Integration

Community Rapid Response uses Mapbox for all geographic visualization and interaction.

**Mapbox Services Used:**
- **Mapbox GL JS**: Interactive map rendering in the browser
- **Mapbox Geocoding API**: Address-to-coordinates conversion for verification
- **Mapbox Draw**: Polygon drawing tools for region creation

**Frontend Map Features:**

1. **Region Browser**
   - Display existing regions as polygon overlays
   - Click region to view details, admin count, Signal groups
   - Color-coded by region type (city, neighborhood, block)
   - Zoom-dependent visibility (blocks only visible at high zoom)

2. **Address Verification UI**
   - **Privacy notice displayed prominently**: "Your address is never stored in our system. It is only used once to mail your verification postcard."
   - User enters address in search box (Postgrid autocomplete)
   - Mapbox Geocoding API converts to coordinates
   - Map centers on location with marker
   - System highlights which existing region contains the address
   - If no region exists, prompts user to create one (if eligible)
   - **Before submission**: Confirmation message reiterating address is not stored

3. **Region Creation UI** (Admins only)
   - Mapbox Draw plugin for polygon creation
   - Draw tools: polygon, rectangle, freehand
   - Snap-to-existing-boundary option
   - Preview overlap with existing regions
   - **Boundary restriction overlay**: Shows the regions the admin has access to
   - Drawing constrained to admin's regions (visual indicator when drawing outside)
   - Validation: new region must be contained within an existing region the user admins

4. **Region Hierarchy Navigation**
   - Breadcrumb navigation: City → Neighborhood → Block
   - Click parent region to zoom out
   - Click sub-region to zoom in
   - Sidebar list of regions at current zoom level

**Backend Geocoding:**
- Server-side geocoding validation using Mapbox API
- Verify user-provided coordinates match claimed address
- Check address falls within claimed region polygon (MariaDB Spatial `ST_Contains`)

**Region Boundary Validation:**
- Admin region creation uses existing region hierarchy for authorization
- `ST_Contains(existing_region.geometry, new_region.geometry)` validates containment
- Admin must have `is_admin=true` for at least one region containing the new region
- No external API calls needed for boundary validation - uses stored region geometry

**API Keys & Security:**
- Mapbox public token for frontend (restricted to app domain)
- Mapbox secret token for server-side geocoding (never exposed to client)
- Rate limiting on geocoding requests to control costs

**Cost Considerations:**
- Mapbox free tier: 50,000 map loads/month, 100,000 geocoding requests/month
- Monitor usage; implement caching for repeated geocoding requests
- Consider self-hosted tile server for high-volume self-hosted deployments

### Postgrid Integration

Community Rapid Response uses Postgrid for address validation and postcard delivery. **Addresses are never stored in our database** - they exist only in memory during the verification request and are sent directly to Postgrid.

**Postgrid Services Used:**
- **Address Verification API**: Validates addresses before postcard mailing
- **Address Autocomplete API**: Real-time address suggestions during input
- **Print & Mail API**: Postcard delivery for verification codes

**Privacy-First Address Flow:**

All address processing happens in a single API request. The address exists only in server memory, is validated, and is immediately sent to Postgrid for mailing. We store only tracking information (postgrid_request_id), never the address itself.

```
┌─────────────────────────────────────────────────────────────┐
│  Single Request - Address in Memory Only                    │
│                                                             │
│  1. Receive address from user                               │
│  2. Postgrid: Validate (reject PO Box/CMRA)                │
│  3. Mapbox: Geocode → coordinates + city/county boundary   │
│  4. Validate coordinates within claimed region              │
│  5. Generate verification code                              │
│  6. Postgrid: Send postcard (address goes to Postgrid)     │
│  7. Store: code, region, boundary info, postgrid_request_id│
│  8. Address DISCARDED from memory - never written to DB    │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**What We Store:**
- Verification code
- Region ID
- City/county boundary info (not specific address)
- Postgrid request ID (for delivery tracking)

**What We NEVER Store:**
- Street address
- Apartment/unit number
- Specific location coordinates

**Address Validation Steps:**

1. **User enters address**
   - Frontend uses Postgrid Address Autocomplete for suggestions
   - User selects or confirms address
   - UI displays: "Your address is never stored in our system"

2. **Backend validation via Postgrid Address Verification API**
   - Validates address exists and is deliverable
   - Standardizes address format (USPS standards)
   - Returns address metadata including delivery type

3. **Address type checking**
   - System checks `deliverability` and `address_type` fields
   - **Rejected address types:**
     - PO Boxes (`address_type: "po_box"`)
     - Commercial Mail Receiving Agencies (CMRA) (`cmra: true`)
     - General Delivery addresses
   - **Accepted address types:**
     - Residential addresses
     - Business addresses (physical locations)

4. **Validation response handling**
   - `deliverable`: Address is valid and mailable
   - `deliverable_missing_unit`: Valid but missing apartment/unit number
   - `undeliverable`: Address cannot receive mail
   - `no_match`: Address not found in USPS database

**Rejected Address Rules:**

| Address Type | Reason for Rejection |
|--------------|---------------------|
| PO Box | Does not prove residential location |
| CMRA (e.g., UPS Store) | Commercial forwarding service, not residence |
| General Delivery | Temporary mail holding, not residence |
| Undeliverable | Cannot receive mail |

**Postgrid API Response Example:**
```json
{
  "status": "verified",
  "deliverability": "deliverable",
  "address_type": "residential",
  "cmra": false,
  "standardized_address": {
    "line1": "123 MAIN ST",
    "line2": "APT 4B",
    "city": "SAN FRANCISCO",
    "state": "CA",
    "postal_code": "94102-1234"
  }
}
```

**Fallback for Manual Mailing:**
- When Postgrid is unavailable, queue address for manual review
- Application managers can manually validate and mail postcards
- Track manual mailings in `verification_requests` table

## Database Schema

### MariaDB Configuration

Community Rapid Response uses MariaDB with spatial extensions for geographic data storage and queries.

**Required Settings:**
- MariaDB 10.5+ recommended for full spatial function support
- `innodb_file_per_table=ON` for better storage management
- Character set: `utf8mb4` with collation `utf8mb4_unicode_ci`

**UUID Generation:**
UUIDs are generated at the application layer using Go's `github.com/google/uuid` package. MariaDB stores them as `CHAR(36)`.

### Tables

#### users
```sql
CREATE TABLE users (
    id CHAR(36) PRIMARY KEY,
    username VARCHAR(255) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    verification_tier INT NOT NULL DEFAULT 0, -- 0=unverified, 1=postcard, 2=vouched
    -- MFA fields
    mfa_secret VARCHAR(255) DEFAULT NULL COMMENT 'Encrypted TOTP secret key (AES-256, base64 encoded)',
    mfa_enabled BOOLEAN NOT NULL DEFAULT FALSE COMMENT 'Whether MFA is currently enabled',
    mfa_backup_codes TEXT DEFAULT NULL COMMENT 'JSON array of bcrypt-hashed backup codes',
    mfa_setup_required BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Whether user must set up MFA on next login',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login TIMESTAMP NULL,
    INDEX idx_verification_tier (verification_tier),
    INDEX idx_users_mfa_setup_required (mfa_setup_required)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### geographic_regions
```sql
CREATE TABLE geographic_regions (
    id CHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    region_type VARCHAR(50) NOT NULL, -- 'state', 'county', 'city', 'locality', 'neighborhood', 'city_block'
    parent_region_id CHAR(36),
    geometry GEOMETRY NULL SRID 4326, -- MariaDB Spatial for geographic boundaries (NULL for regions without boundaries)
    created_by CHAR(36),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_region_type (region_type),
    INDEX idx_parent_region (parent_region_id),
    -- Note: Spatial index disabled due to MariaDB Error 1207 with ST_Contains queries
    -- SPATIAL INDEX idx_geometry (geometry),
    FOREIGN KEY (parent_region_id) REFERENCES geographic_regions(id),
    FOREIGN KEY (created_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

**Region Types:**
- `state`: State boundary (always has geometry)
- `county`: County boundary (always has geometry)
- `city`: City/town boundary (always has geometry)
- `locality`: Borough/sub-city division like Brooklyn (may have geometry)
- `neighborhood`: Named neighborhood (usually no geometry - ~92%)
- `city_block`: Most specific level (future use)

**Nullable Geometry:**
The `geometry` column is nullable to support regions without OSM polygon boundaries. Most neighborhoods (~92%) and some localities lack polygon data. These regions are still valid and are defined by their parent relationship. All `ST_Contains` queries must filter out NULL geometry.

**Note on Spatial Indexes:**
MariaDB spatial indexes can cause Error 1207 ("Update locks cannot be acquired during a READ UNCOMMITTED transaction") when used with `ST_Contains` queries in certain transaction isolation modes. The spatial index on the `geometry` column is disabled to ensure reliable query execution. Performance impact is minimal for typical region counts.

#### user_regions
```sql
CREATE TABLE user_regions (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    region_id CHAR(36) NOT NULL,
    is_admin BOOLEAN DEFAULT FALSE,
    verified_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_user_region (user_id, region_id),
    INDEX idx_user_id (user_id),
    INDEX idx_region_id (region_id),
    INDEX idx_admins (region_id, is_admin),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### admin_boundaries (DEPRECATED)
```sql
-- NOTE: This table is deprecated and not used.
-- Region creation authorization now uses the existing region hierarchy:
-- - Admins can create sub-regions within regions they have is_admin=true for
-- - Validation uses ST_Contains(existing_region.geometry, new_region.geometry)
-- - No separate boundary storage needed
-- The table schema is retained for reference but may be removed in a future migration.
CREATE TABLE admin_boundaries (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    boundary_type VARCHAR(50) NOT NULL,
    boundary_name VARCHAR(255) NOT NULL,
    boundary_state VARCHAR(100) NOT NULL,
    boundary_country VARCHAR(100) NOT NULL DEFAULT 'USA',
    mapbox_place_id VARCHAR(255),
    boundary_geometry POLYGON SRID 4326,
    verified_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### verification_requests
```sql
-- NOTE: Address is NEVER stored. It exists only in memory during the verification request,
-- then is sent directly to Postgrid for mailing. We only store tracking information.
CREATE TABLE verification_requests (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    region_id CHAR(36),
    verification_code VARCHAR(32) UNIQUE NOT NULL,
    -- NO ADDRESS FIELDS - address is never persisted, only passed through to Postgrid
    status VARCHAR(50) NOT NULL DEFAULT 'pending', -- 'pending', 'mailed', 'verified', 'expired'
    postgrid_request_id VARCHAR(255) NOT NULL, -- Track Postgrid API call (required for delivery status)
    -- Store boundary info for granting admin rights upon verification
    boundary_type VARCHAR(50) NOT NULL, -- 'city', 'town', 'county'
    boundary_name VARCHAR(255) NOT NULL,
    boundary_state VARCHAR(100) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL, -- 30 days from creation
    verified_at TIMESTAMP NULL,
    INDEX idx_user_status (user_id, status),
    INDEX idx_verification_code (verification_code),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### vouches
```sql
CREATE TABLE vouches (
    id CHAR(36) PRIMARY KEY,
    voucher_user_id CHAR(36) NOT NULL,
    vouched_user_id CHAR(36) NOT NULL,
    region_id CHAR(36),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_voucher_vouched (voucher_user_id, vouched_user_id),
    INDEX idx_vouched_user (vouched_user_id),
    FOREIGN KEY (voucher_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (vouched_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id),
    CONSTRAINT chk_no_self_vouch CHECK (voucher_user_id != vouched_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### signal_groups
```sql
CREATE TABLE signal_groups (
    id CHAR(36) PRIMARY KEY,
    region_id CHAR(36) NULL, -- Nullable: group belongs to region, school, OR district (three-way XOR)
    school_id CHAR(36) NULL,
    district_id CHAR(36) NULL,
    group_name VARCHAR(255) NOT NULL,
    invite_link TEXT NOT NULL,
    invite_link_updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    invite_link_updated_by CHAR(36),
    description TEXT,
    created_by CHAR(36),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    is_active BOOLEAN DEFAULT TRUE,
    INDEX idx_region_id (region_id),
    INDEX idx_school_id (school_id),
    INDEX idx_district_id (district_id),
    INDEX idx_active (is_active),
    CONSTRAINT fk_signal_groups_region FOREIGN KEY (region_id) REFERENCES geographic_regions(id) ON DELETE CASCADE,
    CONSTRAINT fk_signal_groups_school FOREIGN KEY (school_id) REFERENCES schools(id) ON DELETE CASCADE,
    CONSTRAINT fk_signal_groups_district FOREIGN KEY (district_id) REFERENCES school_districts(id) ON DELETE CASCADE,
    FOREIGN KEY (invite_link_updated_by) REFERENCES users(id),
    FOREIGN KEY (created_by) REFERENCES users(id),
    CONSTRAINT chk_signal_group_owner CHECK (
        (region_id IS NOT NULL AND school_id IS NULL AND district_id IS NULL) OR
        (region_id IS NULL AND school_id IS NOT NULL AND district_id IS NULL) OR
        (region_id IS NULL AND school_id IS NULL AND district_id IS NOT NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### school_districts
```sql
CREATE TABLE school_districts (
    id CHAR(36) PRIMARY KEY,
    nces_id VARCHAR(7) NOT NULL,
    name VARCHAR(255) NOT NULL,
    state VARCHAR(2) NOT NULL,
    district_type ENUM('unified','elementary','secondary') NOT NULL DEFAULT 'unified',
    geometry GEOMETRY NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_nces_id (nces_id),
    INDEX idx_name_state (name, state),
    INDEX idx_state (state)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### schools
```sql
CREATE TABLE schools (
    id CHAR(36) PRIMARY KEY,
    nces_id VARCHAR(12) NOT NULL,
    district_id CHAR(36) NULL,
    name VARCHAR(255) NOT NULL,
    street_address VARCHAR(255) NULL,
    city VARCHAR(100) NULL,
    state VARCHAR(2) NOT NULL,
    zip VARCHAR(10) NULL,
    latitude DECIMAL(10,7) NULL,
    longitude DECIMAL(10,7) NULL,
    region_id CHAR(36) NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_nces_id (nces_id),
    INDEX idx_name (name),
    INDEX idx_state (state),
    INDEX idx_district_id (district_id),
    INDEX idx_region_id (region_id),
    FOREIGN KEY (district_id) REFERENCES school_districts(id) ON DELETE SET NULL,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### user_schools
```sql
CREATE TABLE user_schools (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    school_id CHAR(36) NOT NULL,
    is_admin BOOLEAN NOT NULL DEFAULT FALSE,
    verification_status ENUM('pending','verified') NOT NULL DEFAULT 'pending',
    verified_at TIMESTAMP NULL,
    UNIQUE KEY uk_user_school (user_id, school_id),
    INDEX idx_user_id (user_id),
    INDEX idx_school_id (school_id),
    INDEX idx_school_admin (school_id, is_admin),
    INDEX idx_user_status (user_id, verification_status),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (school_id) REFERENCES schools(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### school_vouches
```sql
CREATE TABLE school_vouches (
    id CHAR(36) PRIMARY KEY,
    voucher_user_id CHAR(36) NOT NULL,
    vouched_user_id CHAR(36) NOT NULL,
    school_id CHAR(36) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_voucher_vouched_school (voucher_user_id, vouched_user_id, school_id),
    INDEX idx_vouched_school (vouched_user_id, school_id),
    INDEX idx_voucher (voucher_user_id),
    CONSTRAINT chk_no_self_vouch CHECK (voucher_user_id != vouched_user_id),
    FOREIGN KEY (voucher_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (vouched_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (school_id) REFERENCES schools(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### school_blocked_users
```sql
CREATE TABLE school_blocked_users (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    school_id CHAR(36) NOT NULL,
    blocked_by CHAR(36) NOT NULL,
    reason TEXT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_user_school_block (user_id, school_id),
    INDEX idx_school_id (school_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (school_id) REFERENCES schools(id) ON DELETE CASCADE,
    FOREIGN KEY (blocked_by) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### deletion_proposals
```sql
CREATE TABLE deletion_proposals (
    id CHAR(36) PRIMARY KEY,
    asset_type VARCHAR(50) NOT NULL, -- 'signal_group', 'sub_region'
    asset_id CHAR(36) NOT NULL, -- References signal_groups.id or geographic_regions.id
    region_id CHAR(36),
    proposed_by CHAR(36),
    reason TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'pending', -- 'pending', 'approved', 'rejected'
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMP NULL,
    INDEX idx_status (status),
    INDEX idx_asset (asset_type, asset_id),
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id),
    FOREIGN KEY (proposed_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### deletion_votes
```sql
CREATE TABLE deletion_votes (
    id CHAR(36) PRIMARY KEY,
    proposal_id CHAR(36) NOT NULL,
    voter_id CHAR(36) NOT NULL,
    vote BOOLEAN NOT NULL, -- TRUE=approve, FALSE=reject
    voted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_proposal_voter (proposal_id, voter_id),
    INDEX idx_proposal_id (proposal_id),
    FOREIGN KEY (proposal_id) REFERENCES deletion_proposals(id) ON DELETE CASCADE,
    FOREIGN KEY (voter_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### invite_link_update_proposals
```sql
-- Requires 3-admin consensus to update Signal group invite links
CREATE TABLE invite_link_update_proposals (
    id CHAR(36) PRIMARY KEY,
    signal_group_id CHAR(36) NOT NULL,
    region_id CHAR(36),
    proposed_by CHAR(36),
    new_invite_link TEXT NOT NULL, -- The proposed new invite link
    reason TEXT, -- Why the link is being updated (optional)
    status VARCHAR(50) NOT NULL DEFAULT 'pending', -- 'pending', 'approved', 'rejected', 'expired'
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL, -- 7 days from creation
    resolved_at TIMESTAMP NULL,
    INDEX idx_status (status),
    INDEX idx_signal_group (signal_group_id),
    INDEX idx_region (region_id),
    FOREIGN KEY (signal_group_id) REFERENCES signal_groups(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id),
    FOREIGN KEY (proposed_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### invite_link_update_votes
```sql
CREATE TABLE invite_link_update_votes (
    id CHAR(36) PRIMARY KEY,
    proposal_id CHAR(36) NOT NULL,
    voter_id CHAR(36) NOT NULL,
    vote BOOLEAN NOT NULL, -- TRUE=approve, FALSE=reject
    voted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_proposal_voter (proposal_id, voter_id),
    INDEX idx_proposal_id (proposal_id),
    FOREIGN KEY (proposal_id) REFERENCES invite_link_update_proposals(id) ON DELETE CASCADE,
    FOREIGN KEY (voter_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### audit_log
```sql
CREATE TABLE audit_log (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36),
    action VARCHAR(100) NOT NULL,
    resource_type VARCHAR(50),
    resource_id CHAR(36),
    details JSON, -- MariaDB JSON type for flexible metadata
    ip_address VARCHAR(45), -- Supports both IPv4 and IPv6
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_user_action (user_id, action),
    INDEX idx_created_at (created_at),
    FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### email_notifications
```sql
-- Tracks email notifications sent to users (for rate limiting and audit)
CREATE TABLE email_notifications (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    notification_type VARCHAR(50) NOT NULL, -- 'invite_link_updated', 'verification_complete', 'vouch_received', etc.
    resource_type VARCHAR(50), -- 'signal_group', 'region', etc.
    resource_id CHAR(36),
    status VARCHAR(50) NOT NULL DEFAULT 'queued', -- 'queued', 'sent', 'failed'
    queued_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at TIMESTAMP NULL,
    error_message TEXT,
    INDEX idx_user_type (user_id, notification_type),
    INDEX idx_status (status),
    INDEX idx_queued_at (queued_at),
    -- Rate limiting: find recent notifications of same type for same resource
    INDEX idx_rate_limit (user_id, notification_type, resource_id, queued_at),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### blacklist_proposals
```sql
CREATE TABLE blacklist_proposals (
    id CHAR(36) PRIMARY KEY,
    target_user_id CHAR(36) NOT NULL,
    region_id CHAR(36),
    proposed_by CHAR(36),
    reason TEXT NOT NULL,
    evidence TEXT, -- Description of surreptitious access or bad behavior
    status VARCHAR(50) NOT NULL DEFAULT 'pending', -- 'pending', 'approved', 'rejected'
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMP NULL,
    INDEX idx_status (status),
    INDEX idx_target_user (target_user_id),
    INDEX idx_region (region_id),
    FOREIGN KEY (target_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id),
    FOREIGN KEY (proposed_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### blacklist_votes
```sql
CREATE TABLE blacklist_votes (
    id CHAR(36) PRIMARY KEY,
    proposal_id CHAR(36) NOT NULL,
    voter_id CHAR(36) NOT NULL,
    vote BOOLEAN NOT NULL, -- TRUE=approve blacklist, FALSE=reject
    voted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_proposal_voter (proposal_id, voter_id),
    INDEX idx_proposal_id (proposal_id),
    FOREIGN KEY (proposal_id) REFERENCES blacklist_proposals(id) ON DELETE CASCADE,
    FOREIGN KEY (voter_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### blacklisted_users
```sql
CREATE TABLE blacklisted_users (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    region_id CHAR(36) NOT NULL,
    proposal_id CHAR(36),
    blacklisted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    blacklisted_until TIMESTAMP NULL, -- NULL = permanent, otherwise temporary
    UNIQUE KEY uk_user_region (user_id, region_id),
    INDEX idx_user_id (user_id),
    INDEX idx_region_id (region_id),
    INDEX idx_blacklisted_until (blacklisted_until),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id) ON DELETE CASCADE,
    FOREIGN KEY (proposal_id) REFERENCES blacklist_proposals(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### Spatial Query Examples

MariaDB spatial functions are used for geographic operations:

```sql
-- Check if a point is within a region
SELECT * FROM geographic_regions
WHERE ST_Contains(geometry, ST_GeomFromText('POINT(-122.4194 37.7749)', 4326));

-- Check if new region is within a region the user admins
SELECT gr.id, gr.name FROM geographic_regions gr
INNER JOIN user_regions ur ON gr.id = ur.region_id
WHERE ur.user_id = ? AND ur.is_admin = TRUE
AND ST_Contains(gr.geometry, ST_GeomFromGeoJSON(?));

-- Find all regions containing a point
SELECT * FROM geographic_regions
WHERE ST_Contains(geometry, ST_PointFromText('POINT(-122.4194 37.7749)', 4326));
```

## Database Migrations

### Migration System

The project uses an idempotent migration system with rollback support. Migrations are tracked in a `schema_migrations` table to ensure each migration is only applied once.

**Migration File Structure:**
```
migrations/
├── 000_schema_migrations.up.sql     # Creates tracking table
├── 000_schema_migrations.down.sql   # Drops tracking table
├── 001_initial_schema.up.sql        # Forward migration
├── 001_initial_schema.down.sql      # Rollback migration
├── 002_verification_model.up.sql
├── 002_verification_model.down.sql
└── ...
```

**Naming Convention:**
- `NNN_description.up.sql` - Forward migration (apply schema changes)
- `NNN_description.down.sql` - Rollback migration (revert schema changes)
- Migrations are applied in alphabetical order by filename

### Migration Commands

```bash
# Apply all pending migrations (idempotent - safe to run multiple times)
just db-migrate

# Rollback the last migration
just db-migrate-down

# Rollback the last N migrations
just db-migrate-down 3

# Show migration status (applied and pending)
just db-migrate-status
```

**Environment Targets:**
```bash
just db-migrate              # Local Docker dev database (default)
just db-migrate test         # Local Docker test database
```

### Migration Tracking Table

```sql
CREATE TABLE schema_migrations (
    version VARCHAR(255) PRIMARY KEY,  -- Migration filename without .up.sql/.down.sql
    applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### Writing Migrations

**Up Migration (forward):**
- Use `CREATE TABLE IF NOT EXISTS` for new tables
- Use `ALTER TABLE` for schema modifications
- Include appropriate indexes and constraints

**Down Migration (rollback):**
- Reverse the changes made in the up migration
- Use `DROP TABLE IF EXISTS` for table removal
- Use `DROP INDEX IF EXISTS` before dropping indexes
- Use `ALTER TABLE DROP COLUMN IF EXISTS` for column removal
- Consider data loss implications and document warnings

**Example Migration Pair:**

`013_add_user_preferences.up.sql`:
```sql
-- Add user preferences table
CREATE TABLE IF NOT EXISTS user_preferences (
    user_id CHAR(36) PRIMARY KEY,
    theme VARCHAR(20) DEFAULT 'light',
    notifications_enabled BOOLEAN DEFAULT TRUE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
```

`013_add_user_preferences.down.sql`:
```sql
-- Remove user preferences table
DROP TABLE IF EXISTS user_preferences;
```

### Bootstrapping Existing Databases

For databases that already have migrations applied manually (before tracking was implemented), bootstrap the tracking table:

```sql
-- Create tracking table
CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(255) PRIMARY KEY,
    applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Mark existing migrations as applied
INSERT INTO schema_migrations (version) VALUES
    ('000_schema_migrations'),
    ('001_initial_schema'),
    ('002_verification_model'),
    -- ... add all previously applied migrations
    ('012_nullable_geometry_locality');
```

## API Specifications

### Authentication
All authenticated endpoints require JWT token in `Authorization: Bearer <token>` header.

### Endpoints

#### Authentication & User Management

**POST /api/v1/auth/register**
```json
Request:
{
    "username": "string",
    "email": "string",
    "password": "string"
}

Response: 201
{
    "user_id": "uuid",
    "username": "string",
    "email": "string"
}
```

**POST /api/v1/auth/login**
```json
Request:
{
    "email": "string",
    "password": "string"
}

Response: 200 (MFA setup required - new user)
{
    "token": "mfa_setup_token",
    "token_type": "mfa_setup",
    "mfa_action": "setup",
    "user": {
        "id": "uuid",
        "username": "string",
        "verification_tier": 0,
        "mfa_enabled": false
    }
}

Response: 200 (MFA verification required - existing user with MFA)
{
    "token": "pending_mfa_token",
    "token_type": "pending_mfa",
    "mfa_action": "verify",
    "user": {
        "id": "uuid",
        "username": "string",
        "verification_tier": 1,
        "mfa_enabled": true
    }
}

Response: 200 (Full access - MFA not required or disabled)
{
    "token": "full_jwt_token",
    "token_type": "full",
    "user": {
        "id": "uuid",
        "username": "string",
        "verification_tier": 0
    }
}
```

#### Multi-Factor Authentication (MFA)

All users are required to set up TOTP-based MFA on first login. MFA tokens (setup and pending) expire in 10 minutes.

**POST /api/v1/mfa/setup** (Requires MFA Setup Token)

Initialize MFA setup - generates TOTP secret and QR code.

```json
Response: 200
{
    "qr_code": "data:image/png;base64,...",
    "secret": "BASE32_ENCODED_SECRET"
}
```

**POST /api/v1/mfa/setup/complete** (Requires MFA Setup Token)

Complete MFA setup by verifying a TOTP code from authenticator app.

```json
Request:
{
    "code": "123456"
}

Response: 200
{
    "token": "full_jwt_token",
    "backup_codes": ["XXXX-YYYY", "AAAA-BBBB", ...],
    "user": {
        "id": "uuid",
        "username": "string",
        "mfa_enabled": true
    }
}

Response: 400 (invalid code)
{
    "error": "invalid_code",
    "message": "Invalid TOTP code"
}
```

**POST /api/v1/mfa/verify** (Requires Pending MFA Token)

Verify TOTP code or backup code during login.

```json
Request (TOTP code):
{
    "code": "123456"
}

Request (Backup code):
{
    "backup_code": "XXXX-YYYY"
}

Response: 200
{
    "token": "full_jwt_token",
    "backup_codes_count": 9,
    "user": {
        "id": "uuid",
        "username": "string",
        "mfa_enabled": true
    }
}

Response: 401 (invalid code)
{
    "error": "invalid_code",
    "message": "Invalid MFA code"
}
```

**MFA Token Types:**
- `full`: Standard JWT token with full API access (24h expiration)
- `mfa_setup`: Limited token for MFA setup endpoints only (10min expiration)
- `pending_mfa`: Limited token for MFA verification only (10min expiration)

**GET /api/v1/users/me**
```json
Response: 200
{
    "id": "uuid",
    "username": "string",
    "email": "string",
    "verification_tier": 1,
    "regions": [
        {
            "region_id": "uuid",
            "region_name": "string",
            "is_admin": true
        }
    ],
    "authorized_boundaries": [
        {
            "id": "uuid",
            "type": "city",
            "name": "San Francisco",
            "state": "California",
            "verified_at": "timestamp"
        }
    ]
}
```

**GET /api/v1/users/me/boundaries**
```json
Response: 200
{
    "boundaries": [
        {
            "id": "uuid",
            "type": "city",
            "name": "San Francisco",
            "state": "California",
            "geometry": {
                "type": "Polygon",
                "coordinates": [[[lng, lat], ...]]
            },
            "verified_at": "timestamp"
        }
    ]
}
```
Returns full geometry for authorized boundaries (used by frontend to display boundary overlay on map).

#### Verification

**POST /api/v1/verification/postcard/request**

**Privacy Note:** The address in this request is processed in memory only. It is validated, geocoded, and sent directly to Postgrid for mailing. The address is NEVER stored in our database.

```json
Request:
{
    "region_id": "uuid",
    "address": {
        "line1": "string",
        "line2": "string?",
        "city": "string",
        "state": "string",
        "postal_code": "string",
        "country": "string?"
    }
}

Response: 201
{
    "verification_id": "uuid",
    "status": "mailed",
    "expires_at": "2024-02-15T12:00:00Z",
    "estimated_delivery": "3-5 business days",
    "privacy_notice": "Your address has been sent to our mailing partner and was not stored in our system.",
    "detected_boundary": {
        "type": "city",
        "name": "San Francisco",
        "state": "California"
    }
}

Response: 400 (address validation failed)
{
    "error": "invalid_address",
    "message": "Address validation failed",
    "validation_result": {
        "deliverability": "undeliverable",
        "reason": "Address not found in USPS database"
    }
}

Response: 400 (PO Box rejected)
{
    "error": "po_box_not_allowed",
    "message": "PO Box addresses cannot be used for verification. Please provide a residential or business street address.",
    "validation_result": {
        "address_type": "po_box",
        "deliverability": "deliverable"
    }
}

Response: 400 (CMRA rejected)
{
    "error": "cmra_not_allowed",
    "message": "Commercial mail receiving agencies (e.g., UPS Store mailboxes) cannot be used for verification. Please provide your actual residential or business address.",
    "validation_result": {
        "address_type": "street",
        "cmra": true
    }
}

Response: 400 (address outside region)
{
    "error": "address_outside_region",
    "message": "The provided address is not within the selected geographic region",
    "validated_coordinates": {
        "lat": 37.7749,
        "lng": -122.4194
    }
}
```

**Address Validation Rules:**
- Address is processed in memory only and NEVER stored in our database
- Address must pass Postgrid verification (deliverable status)
- PO Boxes are not allowed (`address_type: "po_box"`)
- CMRA addresses are not allowed (`cmra: true`)
- Address must fall within the claimed geographic region
- Address is sent directly to Postgrid; we only store tracking ID

**POST /api/v1/verification/postcard/verify**
```json
Request:
{
    "verification_code": "string"
}

Response: 200
{
    "success": true,
    "user": {
        "verification_tier": 1,
        "admin_regions": ["uuid", "uuid"]
    }
}
```

**POST /api/v1/verification/vouch**
```json
Request:
{
    "vouched_user_id": "uuid",
    "region_id": "uuid"
}

Response: 201
{
    "vouch_id": "uuid",
    "vouches_needed": 1,
    "total_vouches": 1
}
```

**GET /api/v1/verification/vouch/status/:user_id**
```json
Response: 200
{
    "user_id": "uuid",
    "region_id": "uuid",
    "vouches_received": 1,
    "vouches_needed": 2,
    "vouchers": [
        {
            "username": "string",
            "vouched_at": "timestamp"
        }
    ]
}
```

#### Geographic Regions

**Access Control:**
- All region endpoints require JWT authentication
- Regular users can only view regions they belong to (directly or via parent region hierarchy)
- Superusers can view and filter all regions
- This prevents unauthorized enumeration of regions and their membership

**GET /api/v1/communities**
```json
Query Parameters:
- type: city_block|neighborhood|town|city
- parent_id: uuid
- lat: float
- lng: float

Response: 200
{
    "regions": [
        {
            "id": "uuid",
            "name": "string",
            "type": "neighborhood",
            "geometry": {
                "type": "Polygon",
                "coordinates": [[[lng, lat], ...]]
            },
            "parent_region": {
                "id": "uuid",
                "name": "string"
            },
            "admin_count": 5,
            "member_count": 23
        }
    ]
}

Response: 401 (not authenticated)
{
    "error": "unauthorized",
    "message": "Authentication required"
}
```

**POST /api/v1/communities** (Admins only)
```json
Request:
{
    "name": "string",
    "type": "city_block|neighborhood|town|city",
    "parent_region_id": "uuid",
    "geometry": {
        "type": "Polygon",
        "coordinates": [[[lng, lat], ...]]
    }
}

Response: 201
{
    "region_id": "uuid",
    "name": "string",
    "created_by": "uuid"
}

Response: 403 (if region outside admin's authorized regions)
{
    "error": "outside_admin_regions",
    "message": "Region must be within a region you have admin access to",
    "admin_regions": [
        {
            "id": "uuid",
            "name": "San Francisco",
            "type": "city"
        }
    ]
}
```

**Validation Rules:**
- Region polygon must fall entirely within a region the user has admin access to
- Use MariaDB Spatial `ST_Contains(admin_region.geometry, new_region.geometry)` for validation
- If admin has multiple regions, new region must be contained within at least one
- Partial overlap (region extends outside all admin regions) is rejected
- Superusers bypass this validation and can create regions anywhere

**GET /api/v1/communities/:id**

Returns region details including geographically-contained sub-regions. Sub-regions are determined using MariaDB's `ST_Contains` spatial function, which finds all regions whose geometry is fully contained within this region's geometry. This allows proper geographic containment (e.g., cities within states) regardless of the `parent_region_id` relationship.

```json
Response: 200
{
    "id": "uuid",
    "name": "string",
    "type": "neighborhood",
    "geometry": {
        "type": "Polygon",
        "coordinates": [[[lng, lat], ...]]
    },
    "parent_region": {...},
    "sub_regions": [
        {
            "id": "uuid",
            "name": "string",
            "region_type": "city_block",
            "admin_count": 2,
            "member_count": 15,
            "geometry": {...}
        }
    ],
    "signal_groups": [...],
    "admins": [
        {
            "user_id": "uuid",
            "username": "string",
            "verification_tier": 1
        }
    ]
}

Response: 401 (not authenticated)
{
    "error": "unauthorized",
    "message": "Authentication required"
}

Response: 403 (no access to region)
{
    "error": "forbidden",
    "message": "You do not have access to this region"
}
```

**Sub-Region Containment:**
- Uses `ST_Contains(parent.geometry, child.geometry)` for geographic containment
- Allows cities to appear as sub-regions of states even if `parent_region_id` is NULL
- Sub-regions sorted by type (county, city, neighborhood, city_block) then name
- Note: Spatial indexes are disabled on the `geometry` column due to MariaDB Error 1207 compatibility issues with `ST_Contains` queries

**PUT /api/v1/communities/:id** (Admin or Superuser only)
```json
Request:
{
    "name": "string"
}

Response: 200
{
    "message": "Region updated successfully",
    "id": "uuid",
    "name": "string"
}

Response: 403 (if not admin of region and not superuser)
{
    "error": "forbidden",
    "message": "You must be an admin of this region to update it"
}
```

**DELETE /api/v1/communities/:id** (Superuser only)

Deletes a region and all associated data (user_regions, signal_groups, child regions).

```json
Response: 200
{
    "message": "Region deleted successfully",
    "id": "uuid",
    "name": "string"
}

Response: 403 (if not superuser)
{
    "error": "forbidden",
    "message": "Only superusers can delete regions"
}
```

**Region Type Hierarchy Rules:**

When creating regions, the type and parent must follow this hierarchy:

| Region Type | Parent Requirement |
|-------------|-------------------|
| State | Cannot have a parent region |
| County | Must have a State parent |
| City/Town | Must have a County parent |
| Locality | Must have a City parent |
| Neighborhood | Must have a Locality OR City parent |
| City Block | Must have a Neighborhood parent |

**Note:** Locality regions are optional and only used for cities with sub-divisions (e.g., Brooklyn in NYC). When a city has no localities, neighborhoods are direct children of the city.

```json
Response: 400 (hierarchy violation)
{
    "error": "validation_error",
    "message": "Neighborhood regions must have a Locality or City parent"
}
```

#### Signal Groups

**POST /api/v1/signal-groups** (Admins only)
```json
Request:
{
    "region_id": "uuid",
    "group_name": "string",
    "invite_link": "https://signal.group/...",
    "description": "string?"
}

Response: 201
{
    "group_id": "uuid",
    "region_id": "uuid",
    "group_name": "string",
    "created_at": "timestamp"
}
```

**GET /api/v1/signal-groups**
```json
Query Parameters:
- region_id: uuid

Response: 200
{
    "groups": [
        {
            "id": "uuid",
            "region_name": "string",
            "group_name": "string",
            "description": "string",
            "invite_link": "https://signal.group/...",
            "member_count_estimate": "~50",
            "created_at": "timestamp"
        }
    ]
}
```

**PUT /api/v1/signal-groups/:id** (Admins only)

**Note:** Invite link updates require 3-admin consensus. Use the proposal endpoints below to update invite links.

```json
Request:
{
    "group_name": "string?",
    "description": "string?"
}

Response: 200
{
    "group_id": "uuid",
    "group_name": "string",
    "updated_at": "timestamp"
}

Response: 400 (if invite_link is provided)
{
    "error": "invite_link_requires_consensus",
    "message": "Invite link updates require 3-admin consensus. Please use POST /api/v1/signal-groups/:id/invite-link-proposals to propose a change."
}
```

#### Invite Link Update Proposals

Invite link updates require 3-admin consensus for security. This prevents a single compromised admin account from redirecting users to malicious groups.

**POST /api/v1/signal-groups/:id/invite-link-proposals** (Admins only)
```json
Request:
{
    "new_invite_link": "https://signal.group/...",
    "reason": "string?" // Optional reason for the change
}

Response: 201
{
    "proposal_id": "uuid",
    "signal_group_id": "uuid",
    "status": "pending",
    "votes_needed": 3,
    "current_votes": 1, // Proposer's vote counted automatically
    "expires_at": "timestamp" // 7 days from creation
}

Response: 403 (if region has <3 admins)
{
    "error": "insufficient_admins",
    "message": "Region must have at least 3 admins to update invite links",
    "admin_count": 2,
    "suggestion": "Contact an Application Manager for assistance"
}

Response: 409 (if pending proposal already exists)
{
    "error": "pending_proposal_exists",
    "message": "A pending invite link update proposal already exists for this group",
    "existing_proposal_id": "uuid"
}
```

**POST /api/v1/signal-groups/invite-link-proposals/:id/vote** (Admins only)
```json
Request:
{
    "vote": true // true=approve, false=reject
}

Response: 200
{
    "proposal_id": "uuid",
    "your_vote": true,
    "current_votes": 2,
    "votes_needed": 3,
    "status": "pending"
}

Response: 200 (when 3rd approval is cast - proposal approved)
{
    "proposal_id": "uuid",
    "your_vote": true,
    "current_votes": 3,
    "votes_needed": 3,
    "status": "approved",
    "invite_link_updated": true,
    "notifications_queued": 47,
    "message": "Invite link updated. Email notifications will be sent to verified users."
}

Response: 403 (if voter is not admin in region)
{
    "error": "not_region_admin",
    "message": "You must be an admin of this region to vote"
}

Response: 409 (if already voted)
{
    "error": "already_voted",
    "message": "You have already voted on this proposal"
}
```

**GET /api/v1/signal-groups/invite-link-proposals**
```json
Query Parameters:
- status: pending|approved|rejected|expired
- signal_group_id: uuid
- region_id: uuid

Response: 200
{
    "proposals": [
        {
            "id": "uuid",
            "signal_group_id": "uuid",
            "group_name": "string",
            "region_name": "string",
            "proposed_by": "username",
            "reason": "string",
            "votes": 2,
            "votes_needed": 3,
            "status": "pending",
            "created_at": "timestamp",
            "expires_at": "timestamp"
        }
    ]
}
```

**GET /api/v1/signal-groups/invite-link-proposals/:id**
```json
Response: 200
{
    "id": "uuid",
    "signal_group_id": "uuid",
    "group_name": "string",
    "region_name": "string",
    "new_invite_link": "https://signal.group/...", // Only shown to region admins
    "proposed_by": {
        "id": "uuid",
        "username": "string"
    },
    "reason": "string",
    "status": "pending",
    "votes": [
        {
            "voter": "username",
            "vote": true,
            "voted_at": "timestamp"
        }
    ],
    "votes_needed": 3,
    "created_at": "timestamp",
    "expires_at": "timestamp"
}
```

**Invite Link Update Proposal Rules:**
- Proposer's submission counts as 1 automatic approval vote
- Requires 3 total approval votes to update the link
- Proposals expire after 7 days if not resolved
- Only one pending proposal per Signal group at a time
- Region must have ≥3 admins to create proposals
- Regions with <3 admins must contact Application Managers

**Invite Link Update Notification Behavior (on approval):**
- When proposal reaches 3 approvals, invite link is updated
- System queues email notifications to all verified users (Tier 1 and Tier 2) in the region
- **Email does NOT contain the invite link** (security measure)
- Email contains:
  - Region name and group name
  - Message: "The invite link for [Group Name] in [Region] has been updated. Please log in to view the new link."
  - Link to Community Rapid Response Signal groups page
- Notifications sent asynchronously (queued for background processing)
- Rate limited: max 1 notification per user per group per 24 hours (prevents spam on rapid updates)

#### Deletion Proposals

**POST /api/v1/deletion-proposals** (Admins only)
```json
Request:
{
    "asset_type": "signal_group|sub_region",
    "asset_id": "uuid",
    "reason": "string"
}

Response: 201
{
    "proposal_id": "uuid",
    "status": "pending",
    "votes_needed": 3,
    "current_votes": 1
}
```

**POST /api/v1/deletion-proposals/:id/vote** (Admins only)
```json
Request:
{
    "vote": true
}

Response: 200
{
    "proposal_id": "uuid",
    "your_vote": true,
    "current_votes": 2,
    "votes_needed": 3,
    "status": "pending"
}
```

**GET /api/v1/deletion-proposals**
```json
Query Parameters:
- status: pending|approved|rejected
- region_id: uuid

Response: 200
{
    "proposals": [
        {
            "id": "uuid",
            "asset_type": "signal_group",
            "asset_name": "string",
            "reason": "string",
            "proposed_by": "username",
            "votes": 2,
            "votes_needed": 3,
            "status": "pending",
            "created_at": "timestamp"
        }
    ]
}
```

#### User Blacklist

**POST /api/v1/blocklist-proposals** (Admins only, requires ≥3 admins in region)
```json
Request:
{
    "target_user_id": "uuid",
    "region_id": "uuid",
    "reason": "string",
    "evidence": "string?"
}

Response: 201
{
    "proposal_id": "uuid",
    "status": "pending",
    "votes_needed": 3,
    "current_votes": 1
}

Response: 403 (if region has <3 admins)
{
    "error": "insufficient_admins",
    "message": "Region must have at least 3 admins to propose blacklisting",
    "admin_count": 2
}
```

**POST /api/v1/blocklist-proposals/:id/vote** (Admins only)
```json
Request:
{
    "vote": true
}

Response: 200
{
    "proposal_id": "uuid",
    "your_vote": true,
    "current_votes": 2,
    "votes_needed": 3,
    "status": "pending"
}
```

**GET /api/v1/blocklist-proposals**
```json
Query Parameters:
- status: pending|approved|rejected
- region_id: uuid

Response: 200
{
    "proposals": [
        {
            "id": "uuid",
            "target_user": {
                "id": "uuid",
                "username": "string"
            },
            "region_name": "string",
            "reason": "string",
            "proposed_by": "username",
            "votes": 2,
            "votes_needed": 3,
            "status": "pending",
            "created_at": "timestamp"
        }
    ]
}
```

**GET /api/v1/communities/:id/blacklist**
```json
Response: 200
{
    "blacklisted_users": [
        {
            "user_id": "uuid",
            "username": "string",
            "blacklisted_at": "timestamp",
            "blacklisted_until": "timestamp|null",
            "reason": "string"
        }
    ]
}
```

### Schools

**GET /api/v1/schools** (Authenticated)

Search schools by name and/or state.
```
Query Parameters:
- q: string (search by name)
- state: string (2-letter state code)
- limit: int (default 20)
- offset: int (default 0)
```

**GET /api/v1/schools/my** (Authenticated)

List the current user's school memberships with verification status.

**GET /api/v1/schools/:id** (Authenticated)

Get school details including user's membership status.

**POST /api/v1/schools/:id/join** (Authenticated)

Join a school. Creates a pending membership.

**POST /api/v1/schools/:id/leave** (Authenticated)

Leave a school. Removes user's membership.

**POST /api/v1/schools/:id/vouch** (Authenticated, verified school member)

Vouch for a pending school member.
```json
Request:
{
    "user_id": "uuid"
}
```

**GET /api/v1/schools/:id/vouch/pending** (Authenticated, verified school member)

List pending vouch requests for a school.

**GET /api/v1/schools/:id/vouch-status/:user_id** (Authenticated)

Get vouch verification status for a user at a school.

**GET /api/v1/schools/:id/members** (Authenticated, verified school member)

List members of a school.

**GET /api/v1/schools/:id/signal-groups** (Authenticated, verified school member)

List Signal groups for a school.

**POST /api/v1/schools/:id/signal-groups** (Authenticated, school admin)

Create a Signal group for a school.

### School Districts

**GET /api/v1/school-districts** (Authenticated)

Search school districts by name and/or state.

**GET /api/v1/school-districts/:id** (Authenticated)

Get district details including associated schools.

**GET /api/v1/school-districts/:id/signal-groups** (Authenticated, verified district member)

List Signal groups for a school district.

**POST /api/v1/school-districts/:id/signal-groups** (Authenticated, district admin)

Create a Signal group for a school district.

### Sub-Region Membership Management

These endpoints manage membership requests for admin-created sub-regions (city blocks, custom neighborhoods) where users aren't automatically assigned via geocoding.

**POST /api/v1/communities/:id/membership-requests** (Verified users only)

Request membership in a sub-region. User must be a member of the parent region.
```json
Response: 201
{
    "request_id": "uuid",
    "user_id": "uuid",
    "region_id": "uuid",
    "request_type": "request",
    "status": "pending",
    "votes_needed": 2,
    "current_votes": 0,
    "expires_at": "timestamp",
    "message": "Membership request created. Admins will review your request."
}

Error: 400 - Region is not a sub-region (no parent)
Error: 403 - User not in parent region
Error: 409 - Pending request already exists
Error: 429 - Maximum pending requests reached (5)
```

**GET /api/v1/membership-requests** (User's own requests)
```json
Query Parameters:
- status: pending|approved|rejected|expired|cancelled
- type: request|invitation

Response: 200
{
    "requests": [
        {
            "id": "uuid",
            "user_id": "uuid",
            "username": "string",
            "region_id": "uuid",
            "region_name": "string",
            "request_type": "request",
            "status": "pending",
            "votes": 1,
            "votes_needed": 2,
            "created_at": "timestamp",
            "expires_at": "timestamp"
        }
    ]
}
```

**DELETE /api/v1/membership-requests/:id** (Request owner only)

Cancel a pending membership request.
```json
Response: 200
{
    "request_id": "uuid",
    "status": "cancelled",
    "message": "Membership request cancelled"
}
```

**GET /api/v1/communities/:id/membership-requests** (Region admins only)

List pending membership requests for a region.
```json
Response: 200
{
    "requests": [
        {
            "id": "uuid",
            "user_id": "uuid",
            "username": "string",
            "region_id": "uuid",
            "region_name": "string",
            "request_type": "request",
            "status": "pending",
            "votes": 1,
            "votes_needed": 2,
            "created_at": "timestamp",
            "expires_at": "timestamp"
        }
    ]
}
```

**GET /api/v1/membership-requests/admin** (Admins only)

List all pending membership requests for regions where user is admin.
```json
Response: 200 (same format as above)
```

**GET /api/v1/membership-requests/:id** (Request owner or region admin)
```json
Response: 200
{
    "id": "uuid",
    "user_id": "uuid",
    "username": "string",
    "region_id": "uuid",
    "region_name": "string",
    "parent_region_id": "uuid",
    "parent_region_name": "string",
    "request_type": "request",
    "initiated_by": {
        "id": "uuid",
        "username": "string"
    },
    "status": "pending",
    "votes_needed": 2,
    "current_votes": 1,
    "votes": [
        {
            "voter_id": "uuid",
            "username": "string",
            "vote": true,
            "voted_at": "timestamp"
        }
    ],
    "created_at": "timestamp",
    "expires_at": "timestamp",
    "resolved_at": null,
    "can_vote": true,
    "has_voted": false
}
```

**POST /api/v1/membership-requests/:id/vote** (Region admins only)

Vote on a membership request. 2+ approval votes are required.
```json
Request:
{
    "vote": true
}

Response: 200 (pending)
{
    "request_id": "uuid",
    "user_id": "uuid",
    "region_id": "uuid",
    "request_type": "request",
    "status": "pending",
    "votes_needed": 2,
    "current_votes": 1,
    "your_vote": true
}

Response: 200 (approved - consensus reached)
{
    "request_id": "uuid",
    "user_id": "uuid",
    "region_id": "uuid",
    "request_type": "request",
    "status": "approved",
    "votes_needed": 2,
    "current_votes": 2,
    "your_vote": true,
    "membership_granted": true,
    "message": "Membership approved. User has been added to the region."
}
```

**POST /api/v1/communities/:id/invitations** (Region admins only)

Invite a user to join a sub-region. User must be in parent region.
```json
Request:
{
    "user_id": "uuid"
}

Response: 201
{
    "request_id": "uuid",
    "user_id": "uuid",
    "region_id": "uuid",
    "request_type": "invitation",
    "status": "pending",
    "votes_needed": 0,
    "current_votes": 0,
    "expires_at": "timestamp",
    "message": "Invitation sent. User will be notified."
}
```

**GET /api/v1/invitations** (User's pending invitations)
```json
Response: 200
{
    "requests": [
        {
            "id": "uuid",
            "user_id": "uuid",
            "username": "string",
            "region_id": "uuid",
            "region_name": "string",
            "request_type": "invitation",
            "status": "pending",
            "votes": 0,
            "votes_needed": 0,
            "created_at": "timestamp",
            "expires_at": "timestamp"
        }
    ]
}
```

**POST /api/v1/invitations/:id/respond** (Invitee only)

Accept or decline an invitation.
```json
Request:
{
    "accept": true
}

Response: 200 (accepted)
{
    "invitation_id": "uuid",
    "status": "approved",
    "membership_granted": true,
    "message": "Invitation accepted. You are now a member of the region."
}

Response: 200 (declined)
{
    "invitation_id": "uuid",
    "status": "rejected",
    "message": "Invitation declined."
}
```

## Verification Flows

### Postcard Verification Flow

**Privacy Guarantee: Your address is NEVER stored in our database.**

The entire address validation, geocoding, and postcard mailing happens in a single request. The address exists only in server memory and is discarded immediately after the postcard is sent to Postgrid.

1. **User initiates verification**
   - UI displays prominent notice: "Your address is never stored in our system"
   - User enters physical address in search box (with Postgrid autocomplete)
   - Mapbox Geocoding API converts address to coordinates
   - Map centers on location; system identifies containing region(s)
   - User confirms the detected region or selects from overlapping regions
   - UI displays confirmation: "Your address will only be used to mail your verification postcard and will not be saved"

2. **Single backend request processes address (in memory only)**
   - **Postgrid validation:**
     - Reject if PO Box (`address_type: "po_box"`)
     - Reject if CMRA (`cmra: true`, e.g., UPS Store mailbox)
     - Reject if undeliverable
     - Standardize to USPS format
   - **Mapbox geocoding:**
     - Convert to coordinates
     - Extract full hierarchy: state, county, city, locality (if present), neighborhood (if present)
   - **Region hierarchy creation:**
     - Create/find State → County → City → (Locality) → (Neighborhood)
     - Fetch boundaries from OSM for each level
     - Regions without OSM polygons are created with NULL geometry
     - User assigned to most specific region (neighborhood > locality > city)
   - **Region validation:**
     - Verify coordinates fall within claimed region (MariaDB Spatial `ST_Contains`)
   - **Generate verification code:**
     - Create unique 8-character alphanumeric code
   - **Send postcard immediately via Postgrid Print & Mail API:**
     - Address sent directly to Postgrid (not stored by us)
     - Receive `postgrid_request_id` for tracking

3. **Store tracking record (NO address)**
   - Save to `verification_requests` table:
     - Verification code
     - User ID, Region ID
     - City/county boundary info (not specific address)
     - Postgrid request ID
     - Expiration (30 days)
   - **Address is discarded from memory - never written to database**

4. **User receives postcard (3-5 business days)**
   - Postcard contains verification code and instructions
   - User returns to Community Rapid Response

5. **User enters verification code**
   - System validates code exists and not expired
   - System verifies user_id matches original request
   - On success:
     - Update user verification_tier to 1
     - Grant admin rights for verified region + parent regions up to neighborhood
     - Delete verification_request record
     - Create audit log entry
     - Send confirmation email

6. **Expiration handling**
   - Daily cron job expires codes >30 days old
   - User must re-enter address to request new verification (address not retrievable)

**Re-send Postcard:** If a user needs another postcard (lost mail), they must re-enter their address. We cannot re-send to a "stored" address because we don't store addresses. This is a feature, not a limitation - it ensures maximum privacy.

### Vouching Flow

Vouching allows unverified users to gain vouch-verification through community endorsement. Combined with postcard verification, it enables full admin rights. Users can complete postcard and vouch verification in either order.

**Key Points:**
- Vouch-verified only users get read-only access (Signal groups, but no admin rights)
- Postcard-verified only users also get read-only access
- **Admin rights require BOTH verifications** (postcard AND vouch)
- Only fully verified users (both verifications complete) can vouch for others

#### Prerequisites

**User being vouched (vouchee) must:**
- Have a registered account
- Have requested vouch verification for a specific region
- Be added to `user_regions` for that region (unverified status)
- Not be blacklisted in the target region

**Each voucher must:**
- Be fully verified (BOTH postcard AND vouch verified)
- Be verified in the same region or a shared ancestor region (up to city/county level, NOT state) as the vouchee
- Not have exceeded 10 vouches this month
- Not have already vouched for this user

#### Flow

1. **Vouchee creates account**
   - User registers via `POST /api/v1/auth/register`
   - Account created with `postcard_verified: false`, `vouch_verified: false`
   - Can browse map and regions but cannot access Signal groups

2. **Vouchee requests vouch verification for a region**
   - User navigates to vouch verification request page
   - User enters their address (street, city, state, zip)
   - Address is geocoded via Mapbox to identify the full region hierarchy
   - System creates pending `user_regions` entries at all levels of the hierarchy (neighborhood through state)
   - Address is discarded from memory after geocoding (never stored)
   - `POST /api/v1/verification/vouch/request`
   - Request body: `{ "street": "123 Main St", "city": "Portland", "state": "OR", "zip": "97201" }`

3. **Vouchee connects with verified neighbors (out-of-band)**
   - User finds fully verified neighbors through personal networks
   - This happens outside Community Rapid Response (in person, community events, social media)
   - User requests vouching from at least 2 different verified neighbors

4. **Vouchee can check their vouch status**
   - `GET /api/v1/verification/vouch/status`
   - Shows current vouches received per region and vouches still needed
   - Lists vouchers who have already vouched

5. **First voucher initiates vouch**
   - Voucher (fully verified) navigates to vouch interface in Community Rapid Response
   - Searches for vouchee by username
   - System shows vouchee's pending vouch request region
   - Reviews confirmation: "You are vouching that this person is a legitimate member of [Region]"
   - Submits vouch: `POST /api/v1/verification/vouch`

6. **System validates vouch request**
   - Verify voucher is fully verified (both postcard AND vouch verified)
   - Verify voucher shares a region with the vouchee (same region or shared ancestor up to city/county level)
   - State-level vouching is rejected (ancestor cap is county)
   - In bootstrap mode, voucher must be in the exact same region (ancestor-level vouching disabled)
   - Verify voucher hasn't exceeded 10 vouches this month
   - Verify voucher hasn't already vouched for this user
   - Verify no circular vouch pattern (vouchee hasn't vouched for voucher)
   - Verify vouchee is not blacklisted in region
   - **If any check fails:** Return appropriate error

7. **First vouch recorded**
   - Create entry in `vouches` table
   - Return response: `{ "vouches_needed": 1, "total_vouches": 1 }`
   - Notify vouchee: "You received a vouch from [username]. You need 1 more vouch to be verified."
   - **User remains unverified** (vouch_verified still false)

8. **Second voucher repeats process**
   - Different fully verified user vouches for same vouchee
   - Same validation checks performed
   - `POST /api/v1/verification/vouch`

9. **Auto-upgrade triggered on 2nd vouch**
   - System detects vouchee now has ≥2 vouches for region
   - Vouches accumulate upward: a vouch at neighborhood level counts toward the city threshold too
   - Update user record:
     - Set `vouch_verified: true`
     - Update `user_regions` entry to `verified: true`
     - Verification cascades upward: when threshold met at a level, all ancestor regions (city, county, state) are also verified
     - If user is also postcard-verified, set `is_admin: true` in verified `user_regions`
     - Create audit log entry
   - Notify vouchee: "Congratulations! You are now vouch-verified for [Region]."
   - If also postcard-verified: "You now have full admin rights."
   - Notify vouchers: "[Username] is now verified thanks to your vouch."

#### Vouch Request Endpoint

```
POST /api/v1/verification/vouch/request
Authorization: Bearer <token>
Content-Type: application/json

{
  "street": "123 Main St",
  "city": "Portland",
  "state": "OR",
  "zip": "97201"
}
```

**Response (success):**
```json
{
  "message": "Vouch verification requested",
  "regions": [
    { "region_id": "uuid-neighborhood", "region_name": "Pearl District", "type": "neighborhood" },
    { "region_id": "uuid-city", "region_name": "Portland", "type": "city" },
    { "region_id": "uuid-county", "region_name": "Multnomah County", "type": "county" },
    { "region_id": "uuid-state", "region_name": "Oregon", "type": "state" }
  ],
  "vouches_needed": 2
}
```

**Response (geocoding failed):**
```json
{
  "error": "geocoding_failed",
  "message": "Could not geocode the provided address. Please check your address and try again."
}
```

#### Vouch Endpoint

```
POST /api/v1/verification/vouch
Authorization: Bearer <token>
Content-Type: application/json

{
  "user_id": "uuid-of-vouchee",
  "region_id": "uuid-of-shared-region"  // optional — defaults to voucher's most specific shared region
}
```

The `region_id` parameter allows the voucher to specify which shared ancestor region to vouch at. If omitted, the system selects the most specific shared region between voucher and vouchee.

#### Post-Verification Capabilities

**Vouch-verified only users CAN:**
- ✅ View Signal group invite links for their verified region
- ✅ Join and participate in regional Signal groups
- ✅ Complete postcard verification to gain admin rights

**Vouch-verified only users CANNOT:**
- ❌ Admin groups
- ❌ Vouch for others
- ❌ Create sub-regions
- ❌ Propose deletions or blacklisting
- ❌ Create Signal groups

#### Verification Paths to Admin

| Starting Point | Next Step | Result |
|----------------|-----------|--------|
| Unverified | Complete postcard verification | Postcard-only (read-only) |
| Unverified | Complete vouch verification | Vouch-only (read-only) |
| Postcard-only | Complete vouch verification | **Full Admin** |
| Vouch-only | Complete postcard verification | **Full Admin** |

#### Anti-Abuse Safeguards

1. **Geographic proximity requirement**
   - Voucher must be verified in the same region or a shared ancestor region as the vouchee
   - Ancestor-level vouching is capped at city/county — state-level vouching is rejected
   - Bootstrap mode disables ancestor-level vouching (requires exact same region)
   - Vouchee's address is geocoded to establish region membership (prevents spoofing)
   - Prevents remote/fake vouching while allowing broader community connections

2. **Rate limiting**
   - Maximum 10 vouches per voucher per month
   - Prevents vouching farms

3. **Full verification requirement for vouching**
   - Only users with BOTH postcard and vouch verification can vouch
   - Ensures vouchers have been thoroughly vetted themselves

4. **No self-vouching**
   - Database constraint: `voucher_user_id != vouched_user_id`

5. **Single vouch per pair**
   - Unique constraint on `(voucher_user_id, vouched_user_id)`
   - Cannot vouch for same user twice

6. **Circular pattern detection**
   - System detects and prevents vouching rings
   - Example blocked: A vouches B, B vouches C, C vouches A

7. **Blacklist enforcement**
   - Cannot vouch for blacklisted users
   - Vouches invalidated if voucher is later blacklisted

### School Membership & Verification Flow

Schools use a vouch-only verification model (no postcard verification).

1. **User searches for school**
   - `GET /api/v1/schools?q=name&state=XX`
   - NCES data provides school names, addresses, and district associations

2. **User joins school**
   - `POST /api/v1/schools/:id/join`
   - Creates `user_schools` entry with `verification_status: 'pending'`

3. **User gets vouched by verified school members**
   - Verified members vouch via `POST /api/v1/schools/:id/vouch`
   - **Bootstrap mode** (school has <3 admins): 3 vouches required, any verified member can vouch
   - **Normal mode** (school has ≥3 admins): 2 vouches required, only admins can vouch

4. **Auto-verification on sufficient vouches**
   - System updates `user_schools.verification_status` to `'verified'`
   - If sufficient vouches, user gains `is_admin = true`
   - User can now access school Signal groups and vouch for others

5. **School admin capabilities**
   - Create Signal groups for the school
   - Vouch for pending members
   - Manage school community

## Email Notifications

The system sends email notifications for important events. **Sensitive information (invite links, verification codes, addresses) is NEVER included in emails.**

### Notification Types

| Event | Recipients | Email Contains | Does NOT Contain |
|-------|-----------|----------------|------------------|
| Signal group invite link updated | All verified users in region | Group name, region name, "log in to view" | The actual invite link |
| Verification complete | Verified user | Congratulations, region name | Address, verification code |
| Vouch received | Vouchee | Voucher username, vouches remaining | - |
| Vouch complete (Tier 2) | Newly verified user | Region name, "you can now access groups" | - |
| Blacklist proposal created | Region admins | Target username, region | Evidence details |
| Deletion proposal created | Region admins | Asset name, region | - |

### Signal Group Invite Link Update Notifications

When an invite link update proposal is approved (3-admin consensus reached):

1. **Trigger**: 3rd approval vote on invite link update proposal
2. **Recipients**: All Tier 1 and Tier 2 users verified in that region
3. **Email content**:
   ```
   Subject: [Community Rapid Response] Signal Group Link Updated - [Group Name]

   The invite link for "[Group Name]" in [Region Name] has been updated.

   To view the new invite link, please log in to your Community Rapid Response account:
   [Link to Community Rapid Response Signal groups page]

   For security, invite links are never sent via email.
   ```
4. **Rate limiting**: Max 1 notification per user per group per 24 hours
5. **Processing**: Queued for background processing (async)
6. **Note**: Notifications are NOT sent when proposals are created, only when approved

### Security Principles

1. **No sensitive data in emails**
   - Invite links shown only in authenticated app
   - Verification codes sent via physical mail only
   - Addresses never stored or transmitted

2. **Rate limiting**
   - Prevents notification spam from rapid updates
   - Tracked in `email_notifications` table

3. **Unsubscribe option**
   - Users can opt out of non-essential notifications
   - Critical security notifications (blacklist, account changes) cannot be disabled

### Email Notification Queue

Notifications are processed asynchronously:

1. Event triggers notification
2. Record created in `email_notifications` table with `status: 'queued'`
3. Background worker processes queue
4. On success: `status: 'sent'`, `sent_at` timestamp set
5. On failure: `status: 'failed'`, `error_message` recorded, retry logic applies

## Consensus Mechanism

### 3-Admin Invite Link Update Approval

Signal group invite links require 3-admin consensus to update. This prevents a single compromised admin account from redirecting users to malicious Signal groups.

**Prerequisites:**
- Region must have **minimum 3 admins** to enable invite link updates
- Regions with <3 admins must contact Application Managers

**Invite Link Update Flow:**

1. Admin proposes new invite link with optional reason
2. System validates region has ≥3 admins; rejects if insufficient
3. System checks no pending proposal exists for this group
4. System creates `invite_link_update_proposals` record
5. Proposer's submission counts as 1 automatic approval vote
6. Other admins in region receive notification of pending proposal
7. Admins vote (approve/reject)
8. When 3 approvals reached:
   - Signal group's `invite_link` field updated
   - `invite_link_updated_at` and `invite_link_updated_by` updated
   - Proposal marked as 'approved'
   - Email notifications queued to all verified users in region (link NOT included in email)
   - Audit log entry created
9. If majority rejects before 3 approvals:
   - Proposal marked as 'rejected'
10. If proposal expires (7 days):
    - Proposal marked as 'expired'

**Voting Rules:**
- Admins can only vote once per proposal
- Proposer's proposal counts as 1 automatic approval vote
- Votes cannot be changed after submission
- Proposal expires after 7 days if not resolved
- Only one pending proposal per Signal group at a time

**For regions with <3 admins:**
- Direct invite link updates are not permitted
- Admin must contact Application Manager
- Manager can update link with documented justification
- Decision recorded in audit log

### 3-Admin Deletion Approval

**For regions with ≥3 admins:**

1. Admin proposes deletion of Signal group or sub-region
2. System creates `deletion_proposal` record
3. Other admins in same region receive notification
4. Admins vote (approve/reject)
5. When 3 approvals reached:
   - Asset marked as inactive/deleted
   - Audit log created
   - Proposal marked as 'approved'
6. If majority rejects before 3 approvals:
   - Proposal marked as 'rejected'

**For regions with <3 admins:**

1. Admin proposes deletion
2. System flags proposal as 'requires_manager_approval'
3. Application manager notified
4. Manager reviews and approves/rejects manually
5. Decision recorded in audit log

**Voting Rules:**
- Admins can only vote once per proposal
- Proposer's proposal counts as 1 automatic approval vote
- Votes cannot be changed after submission
- Proposal expires after 30 days if not resolved

### 3-Admin User Blacklist

Users who have joined a region surreptitiously (fraudulent verification, compromised vouches, etc.) can be blacklisted by admin consensus.

**Prerequisites:**
- Region must have **minimum 3 admins** to enable blacklisting
- Regions with <3 admins cannot blacklist users (prevents abuse in small communities)

**Blacklist Flow:**

1. Admin identifies user who gained access through fraudulent means
2. Admin proposes blacklist with required reason and optional evidence
3. System validates region has ≥3 admins; rejects if insufficient
4. System creates `blacklist_proposal` record
5. Other admins in region receive notification
6. Admins vote (approve/reject)
7. When 3 approvals reached:
   - User added to `blacklisted_users` table for that region
   - User's access to region's Signal groups revoked (invite links no longer shown)
   - User's `user_regions` entry for that region removed
   - Any vouches given by the blacklisted user in that region invalidated
   - Audit log entry created
   - Blacklisted user notified (no details given to prevent gaming)
8. If majority rejects before 3 approvals:
   - Proposal marked as 'rejected'

**Blacklist Effects:**
- User cannot access Signal group invite links for the region
- User cannot request verification for that region
- User cannot be vouched for in that region
- Blacklist applies to the specific region and its sub-regions
- User retains access to other unrelated regions

**Blacklist Scope:**
- Blacklist cascades to all sub-regions of the blacklisted region
- Example: Blacklisted from "Downtown" neighborhood → also blacklisted from all city blocks within Downtown

**Appeal Process:**
- Blacklisted users may appeal to Application Managers
- Managers can remove blacklist entries with documented justification
- All appeals and decisions recorded in audit log

**Anti-Abuse Safeguards:**
- Minimum 3-admin requirement prevents small cliques from weaponizing blacklists
- All blacklist actions fully audited
- Proposer cannot vote on their own proposal (their proposal is their vote)
- Rate limit: Maximum 5 blacklist proposals per admin per month

## Security Considerations

### Encryption at Rest

All data stored by the system must be encrypted at rest to protect against unauthorized access to storage media.

1. **Database Encryption**
   - MariaDB Data-at-Rest Encryption or filesystem-level encryption required
   - Enable `innodb_encrypt_tables=ON` and `innodb_encrypt_log=ON`
   - Use AES-256 encryption via `innodb_encryption_algorithm=AES_CTR`
   - Encryption keys stored separately from encrypted data (file-based key management or external KMS)
   - Key rotation policy: minimum annually or upon suspected compromise

2. **Backup Encryption**
   - All database backups encrypted with AES-256
   - Backup encryption keys managed separately from primary database keys
   - Encrypted backups verified for integrity before storage

3. **File Storage Encryption**
   - Any file uploads or static assets stored encrypted
   - Use filesystem-level encryption (e.g., LUKS, dm-crypt) or cloud provider encryption

4. **Audit Log Encryption**
   - Audit logs encrypted at rest to protect sensitive action records
   - Separate encryption keys from primary database

5. **Key Management**
   - Encryption keys stored in secure key management system (KMS)
   - Hardware Security Module (HSM) recommended for production deployments
   - Access to encryption keys strictly limited and audited
   - Keys never stored in application code, environment variables containing keys must be protected

6. **Self-Hosted Requirements**
   - Self-hosted deployments must enable encryption at rest
   - Docker volumes must use encrypted storage
   - Documentation must include encryption setup instructions

### Data Privacy

1. **Zero Address Storage**
   - **Addresses are NEVER written to our database** - not even temporarily
   - Address exists only in server memory during the verification request
   - Address is sent directly to Postgrid for mailing, then discarded from memory
   - Even database administrators cannot retrieve user addresses
   - UI explicitly communicates this to users before they submit

2. **Minimal Data Retention**
   - Only city/county boundary stored (not specific address or coordinates)
   - Verification codes deleted after use or expiration
   - Geographic region membership only (e.g., "User is verified in Downtown neighborhood")
   - No GPS coordinates stored

3. **What We Store vs. What We Don't**
   | Stored | NOT Stored |
   |--------|------------|
   | City/county name | Street address |
   | Region membership | Apartment/unit number |
   | Postgrid tracking ID | GPS coordinates |
   | Verification code | Specific location |

4. **Audit Logging**
   - All admin actions logged
   - IP addresses logged for suspicious activity detection
   - Logs retained for 90 days
   - Audit logs do NOT contain addresses

### Anti-Abuse Measures

1. **Rate Limiting**
   - Verification requests: 3 per user per 30 days
   - Vouch requests: 10 per user per month
   - API: 100 requests per minute per IP
   - Authentication endpoints (per IP):
     - Login: 10 per 5 minutes
     - Registration: 3 per hour
     - Forgot password: 5 per 15 minutes
     - Reset password: 10 per 15 minutes
     - Resend verification: 3 per 15 minutes

2. **Account Lockout**
   - 10 consecutive failed login attempts triggers 15-minute account lock
   - Counter resets on successful login
   - Locked accounts return 429 with "account_locked" error
   - Lockout state tracked via `failed_login_attempts` and `locked_until` columns
   - Account lock events are audit-logged

3. **Vouch Fraud Prevention**
   - Geographic proximity validation (users must share a region)
   - Detection of circular vouching patterns
   - Blacklist check before allowing vouch

4. **Signal Group Spam Prevention**
   - Limit: 5 groups per region
   - Admin-created only
   - Deletion proposals for inactive groups

5. **Surreptitious Access Prevention**
   - User blacklisting via 3-admin consensus
   - Minimum 3 admins required to enable blacklisting (prevents abuse)
   - Blacklist cascades to sub-regions
   - Maximum 5 blacklist proposals per admin per month
   - All blacklist actions audited

### Authentication & Authorization

1. **Password Requirements**
   - Minimum 12 characters
   - Bcrypt hashing (cost factor 12)
   - Password reset via email

2. **Multi-Factor Authentication (MFA)**
   - **Mandatory in production**: MFA setup required on first login
   - **Optional in development**: Set `MFA_REQUIRED=false` to skip MFA (login issues full token immediately)
   - TOTP-based (RFC 6238) using any authenticator app (Google Authenticator, Authy, etc.)
   - 30-second time step with ±1 period tolerance
   - Secret encrypted with AES-256-GCM before storage
   - 10 backup codes generated on setup (single-use, bcrypt hashed)
   - QR code provided as base64 PNG data URL
   - MFA tokens expire in 10 minutes (vs 24 hours for full tokens)
   - When MFA is disabled, MFA endpoints return 503 Service Unavailable

3. **JWT Tokens**
   - Full tokens: 24-hour expiration
   - MFA setup tokens: 10-minute expiration
   - Pending MFA tokens: 10-minute expiration
   - Token type claim prevents misuse of limited tokens
   - Stored in httpOnly cookies

4. **Role-Based Access Control**
   - Tier checking on all protected endpoints
   - Region-specific admin privileges
   - Ownership validation for resource modifications

## Deployment Architecture

### Self-Hosted Architecture

```
┌─────────────────────────────────────┐
│  Community Server (Docker)         │
│                                     │
│  ┌──────────────────────────────┐ │
│  │  Go Application Container    │ │
│  │  - Configurable federation   │ │
│  │  - Local admin UI            │ │
│  └──────────────────────────────┘ │
│                                     │
│  ┌──────────────────────────────┐ │
│  │  MariaDB Container           │ │
│  │  - Volume-mounted data       │ │
│  │  - Encrypted volumes (LUKS)  │ │
│  └──────────────────────────────┘ │
│                                     │
│  ┌──────────────────────────────┐ │
│  │  Nginx Reverse Proxy         │ │
│  │  - SSL/TLS                   │ │
│  │  - Rate limiting             │ │
│  └──────────────────────────────┘ │
└─────────────────────────────────────┘
```

**Self-Hosting Considerations:**
- Docker Compose for easy deployment
- Environment variable configuration
- Optional federation with central instance
- Community-specific branding
- Manual postcard mailing option
- **Encryption at rest required**: All Docker volumes must use encrypted storage (LUKS/dm-crypt)

## Implementation Phases

### Phase 1: MVP (Weeks 1-4)
**Goal**: Core verification and region management

- [ ] Database schema implementation (including admin_boundaries table)
- [ ] Database encryption at rest configuration (MariaDB encryption or filesystem-level)
- [ ] User authentication (register, login, JWT)
- [ ] Mapbox GL JS integration for map display
- [ ] Mapbox Geocoding integration for address lookup
- [ ] Mapbox Draw integration for region polygon creation
- [ ] Region-restricted region creation (MariaDB Spatial ST_Contains validation)
- [ ] Postgrid Address Verification API integration
- [ ] PO Box and CMRA address rejection logic
- [ ] Address standardization (USPS format)
- [ ] Postgrid Print & Mail API integration for postcard delivery
- [ ] Basic geographic region CRUD with MariaDB Spatial
- [ ] Simple web UI for registration and verification
- [ ] Admin dashboard for region management with admin regions overlay

**Deliverables**:
- Working postcard verification with Postgrid address validation
- PO Box and CMRA addresses rejected with clear error messages
- Automatic admin rights for verified region upon successful verification
- Interactive map showing admin's regions for creation boundaries
- Users can create/view regions via map interface (within their admin regions only)
- Basic admin privileges functional with geographic restrictions

### Phase 2: Signal Integration (Weeks 5-6)
**Goal**: Connect regions to Signal groups

- [ ] Signal group CRUD operations
- [ ] Group invite link management
- [ ] Invite link update API endpoint
- [ ] Email notification queue system
- [ ] Invite link update notifications (email without link)
- [ ] Notification rate limiting (1 per user per group per 24h)
- [ ] Region-group associations
- [ ] User access control to groups
- [ ] Group discovery UI

**Deliverables**:
- Admins can add and update Signal groups
- Invite link updates trigger email notifications to region members
- Users can view/join groups for their regions

### Phase 3: Vouching System (Weeks 7-8)
**Goal**: Tier 2 verification via community vouching

- [ ] Vouch request/approval flow
- [ ] Multi-voucher requirement (2+ vouchers)
- [ ] Geographic proximity validation
- [ ] Auto-upgrade to Tier 2
- [ ] Vouch management UI

**Deliverables**:
- Tier 1 users can vouch for neighbors
- Tier 2 users gain access after 2 vouches

### Phase 4: Governance (Weeks 9-10)
**Goal**: Consensus-based deletion and moderation

- [ ] Deletion proposal system
- [ ] Voting mechanism (3-admin consensus)
- [ ] Manager escalation for <3 admin regions
- [ ] User blacklist proposal system (3-admin minimum requirement)
- [ ] Blacklist voting and enforcement
- [ ] Blacklist cascade to sub-regions
- [ ] Proposal notification system
- [ ] Governance dashboard

**Deliverables**:
- Admins can propose/vote on deletions
- Admins can propose/vote on user blacklisting (regions with ≥3 admins)
- Asset removal via consensus
- User access revocation via blacklist consensus

### Phase 5: Polish & Hardening (Weeks 11-12)
**Goal**: Production-ready security and UX

- [ ] Comprehensive audit logging
- [ ] Rate limiting implementation
- [ ] Anti-abuse detection
- [ ] Email notifications
- [ ] Error handling and edge cases
- [ ] Security audit
- [ ] Performance optimization
- [ ] Documentation (user guides, API docs)

**Deliverables**:
- Production-ready application
- Complete documentation
- Security hardened

### Phase 6: Self-Hosting Preparation (Weeks 13-16)
**Goal**: Enable community deployments

- [ ] Docker containerization
- [ ] Configuration externalization
- [ ] Encrypted volume setup scripts and documentation
- [ ] Manual postcard option
- [ ] Community admin tools
- [ ] Federation considerations
- [ ] Deployment documentation

**Deliverables**:
- Docker Compose setup with encrypted volumes
- Self-hosting guide including encryption at rest setup
- Community admin documentation

## Open Questions & Future Considerations

### Technical Decisions Needed

1. **Geographic Data**
   - ✅ Using Mapbox for geocoding and map display
   - ✅ Using MariaDB Spatial for spatial queries and polygon storage
   - Pre-populate with OpenStreetMap boundary data or user-generated only?
   - How to handle disputed boundaries?

2. **Postcard Fallback**
   - When Postgrid unavailable, manual mailing workflow?
   - Admin interface for tracking manual mailings?

3. **Signal Group Limits**
   - What happens when groups reach Signal's member cap?
   - Strategy for splitting large groups?

4. **Region Hierarchy Changes**
   - Can regions be moved between parents?
   - What happens to admins when regions merge/split?

### Future Features

1. **Enhanced Moderation**
   - Temporary suspension of users
   - Reporting system for bad actors
   - Community guidelines enforcement

2. **Communication Tools**
   - In-app messaging for coordination
   - Event announcement system
   - Emergency broadcast capability

3. **Analytics**
   - Regional activity metrics
   - Verification success rates
   - Growth trends

4. **Accessibility**
   - Multi-language support
   - Screen reader optimization
   - Alternative verification for disabled users

5. **Mobile App**
   - Native iOS/Android apps
   - Push notifications
   - GPS-assisted region discovery

## Success Metrics

### V1 Goals
- 100 verified users in first 3 months
- 10 active geographic regions
- <2% verification fraud rate
- 90%+ user satisfaction

### System Health Metrics
- API response time <200ms (p95)
- 99.5% uptime
- Zero data breaches
- Verification completion rate >80%

## References & Resources

- [Mapbox GL JS Documentation](https://docs.mapbox.com/mapbox-gl-js/)
- [Mapbox Geocoding API](https://docs.mapbox.com/api/search/geocoding/)
- [Mapbox Boundaries API](https://docs.mapbox.com/data/boundaries/)
- [Mapbox Draw Plugin](https://github.com/mapbox/mapbox-gl-draw)
- [Postgrid API Documentation](https://docs.postgrid.com/)
- [Signal Group Links](https://support.signal.org/hc/en-us/articles/360007319331-Group-chats)
- [MariaDB Spatial Documentation](https://mariadb.com/kb/en/geographic-geometric-features/)
- [MariaDB Data-at-Rest Encryption](https://mariadb.com/kb/en/data-at-rest-encryption/)
- [Go Best Practices](https://golang.org/doc/effective_go)

---

**Document Version**: 2.7
**Last Updated**: 2026-02-14
**Author**: Brian Oldfield
**Status**: Draft
