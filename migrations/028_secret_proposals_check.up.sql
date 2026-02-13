-- Add 3-way XOR CHECK constraint on secret_update_proposals
-- Ensures exactly one of region_id/school_id/district_id is NOT NULL
ALTER TABLE secret_update_proposals ADD CONSTRAINT chk_secret_proposal_scope CHECK (
    (region_id IS NOT NULL AND school_id IS NULL AND district_id IS NULL) OR
    (region_id IS NULL AND school_id IS NOT NULL AND district_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NOT NULL)
);
