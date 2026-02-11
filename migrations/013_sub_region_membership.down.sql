-- Rollback: 013_sub_region_membership
-- Description: Remove sub-region membership request tables

DROP TABLE IF EXISTS sub_region_membership_votes;
DROP TABLE IF EXISTS sub_region_membership_requests;
