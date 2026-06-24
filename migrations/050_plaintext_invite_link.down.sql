-- Rollback Migration 050: Remove plaintext_invite_link column
ALTER TABLE signal_groups DROP COLUMN IF EXISTS plaintext_invite_link;
