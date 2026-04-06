-- Drop chat proposal tables
DROP TABLE IF EXISTS connection_chat_proposal_votes;
DROP TABLE IF EXISTS connection_chat_proposals;

-- Remove connection signal groups before dropping column
DELETE FROM signal_groups WHERE connection_id IS NOT NULL;

-- Drop 5-way XOR constraint
ALTER TABLE signal_groups DROP CONSTRAINT chk_signal_group_owner;

-- Restore 4-way XOR constraint
ALTER TABLE signal_groups ADD CONSTRAINT chk_signal_group_owner CHECK (
    (region_id IS NOT NULL AND school_id IS NULL AND district_id IS NULL AND owner_group_id IS NULL) OR
    (region_id IS NULL AND school_id IS NOT NULL AND district_id IS NULL AND owner_group_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NOT NULL AND owner_group_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NULL AND owner_group_id IS NOT NULL)
);

-- Drop connection_id column
ALTER TABLE signal_groups DROP FOREIGN KEY fk_signal_groups_connection;
ALTER TABLE signal_groups DROP INDEX idx_signal_groups_connection;
ALTER TABLE signal_groups DROP COLUMN connection_id;
