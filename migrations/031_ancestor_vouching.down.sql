-- Migration 031 down: No-op
-- Backfilled ancestor membership rows are harmless and indistinguishable
-- from organically created ones. Removing them could break existing vouches
-- that reference ancestor-level regions.
SELECT 1;
