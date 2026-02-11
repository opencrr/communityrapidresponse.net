ALTER TABLE deletion_proposals DROP FOREIGN KEY fk_deletion_proposals_district;
ALTER TABLE deletion_proposals DROP FOREIGN KEY fk_deletion_proposals_school;
ALTER TABLE deletion_proposals DROP COLUMN district_id;
ALTER TABLE deletion_proposals DROP COLUMN school_id;
