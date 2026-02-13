-- Remove 3-way XOR CHECK constraint from secret_update_proposals
ALTER TABLE secret_update_proposals DROP CONSTRAINT IF EXISTS chk_secret_proposal_scope;
