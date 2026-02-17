-- Migration 031: Backfill ancestor region memberships for ancestor-level vouching
-- This is a data-only migration — no DDL changes needed.
--
-- For each existing user_region entry, we insert pending membership entries
-- for all ancestor regions (parent → grandparent → ... → state) where the user
-- doesn't already have a membership.
--
-- For already-verified user_regions, we cascade verified status to ancestors.

-- Step 1: Create a temporary table with all (user_id, ancestor_region_id) pairs
-- that should exist but don't yet
CREATE TEMPORARY TABLE IF NOT EXISTS tmp_ancestor_backfill (
    user_id CHAR(36) COLLATE utf8mb4_unicode_ci NOT NULL,
    region_id CHAR(36) COLLATE utf8mb4_unicode_ci NOT NULL,
    verification_status VARCHAR(20) COLLATE utf8mb4_unicode_ci NOT NULL,
    is_admin TINYINT(1) NOT NULL,
    verified_at DATETIME NOT NULL,
    PRIMARY KEY (user_id, region_id)
);

-- Step 2: Populate the temp table using a recursive CTE
-- For each user_region, walk up the parent chain and collect missing ancestors.
-- Use the "best" status: if user is verified at child, ancestors should be verified too.
-- is_admin is always FALSE for ancestor entries — admin rights are region-specific
-- and do not propagate upward through the hierarchy.
INSERT IGNORE INTO tmp_ancestor_backfill (user_id, region_id, verification_status, is_admin, verified_at)
SELECT DISTINCT ur.user_id, ancestor_id, ur.verification_status, FALSE, ur.verified_at
FROM user_regions ur
JOIN (
    -- Get all ancestor relationships: (child_id, ancestor_id)
    WITH RECURSIVE region_ancestors AS (
        SELECT id AS child_id, parent_region_id AS ancestor_id
        FROM geographic_regions
        WHERE parent_region_id IS NOT NULL
        UNION ALL
        SELECT ra.child_id, gr.parent_region_id AS ancestor_id
        FROM region_ancestors ra
        JOIN geographic_regions gr ON ra.ancestor_id = gr.id
        WHERE gr.parent_region_id IS NOT NULL
    )
    SELECT child_id, ancestor_id FROM region_ancestors
) ancestors ON ur.region_id = ancestors.child_id
WHERE NOT EXISTS (
    SELECT 1 FROM user_regions existing
    WHERE existing.user_id = ur.user_id
    AND existing.region_id = ancestors.ancestor_id
);

-- Step 3: Insert the backfilled entries into user_regions.
-- INSERT IGNORE suppresses all errors (not just duplicate key), but this is safe here
-- because all data originates from valid existing user_regions rows — foreign key
-- and other constraint violations should not occur.
INSERT IGNORE INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at)
SELECT UUID(), user_id, region_id, is_admin, verification_status, verified_at
FROM tmp_ancestor_backfill;

-- Step 4: For users who are verified at a child region but somehow have a pending
-- ancestor entry (e.g., from a partial earlier migration), upgrade those ancestors.
-- We use a temp table because MariaDB can't reference the outer UPDATE table columns
-- inside a CTE within an EXISTS subquery.
CREATE TEMPORARY TABLE IF NOT EXISTS tmp_pending_to_verify (
    user_id CHAR(36) COLLATE utf8mb4_unicode_ci NOT NULL,
    region_id CHAR(36) COLLATE utf8mb4_unicode_ci NOT NULL,
    PRIMARY KEY (user_id, region_id)
);

-- Find pending ancestor entries that have a verified descendant entry.
-- "ancestor of a verified region" = the verified region's parent chain includes this region.
INSERT IGNORE INTO tmp_pending_to_verify (user_id, region_id)
SELECT DISTINCT ur_pending.user_id, ur_pending.region_id
FROM user_regions ur_pending
JOIN (
    -- Build (descendant_region_id, ancestor_region_id) pairs
    WITH RECURSIVE ancestors AS (
        SELECT id AS descendant_id, parent_region_id AS ancestor_id
        FROM geographic_regions
        WHERE parent_region_id IS NOT NULL
        UNION ALL
        SELECT a.descendant_id, gr.parent_region_id AS ancestor_id
        FROM ancestors a
        JOIN geographic_regions gr ON a.ancestor_id = gr.id
        WHERE gr.parent_region_id IS NOT NULL
    )
    SELECT descendant_id, ancestor_id FROM ancestors
) lineage ON ur_pending.region_id = lineage.ancestor_id
JOIN user_regions ur_verified
    ON ur_verified.user_id = ur_pending.user_id
    AND ur_verified.region_id = lineage.descendant_id
    AND ur_verified.verification_status = 'verified'
WHERE ur_pending.verification_status = 'pending';

UPDATE user_regions ur
JOIN tmp_pending_to_verify ptv
    ON ur.user_id = ptv.user_id AND ur.region_id = ptv.region_id
SET ur.verification_status = 'verified',
    ur.verified_at = COALESCE(ur.verified_at, NOW());

DROP TEMPORARY TABLE IF EXISTS tmp_pending_to_verify;

-- Cleanup
DROP TEMPORARY TABLE IF EXISTS tmp_ancestor_backfill;
