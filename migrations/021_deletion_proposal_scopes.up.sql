ALTER TABLE deletion_proposals ADD COLUMN school_id CHAR(36) DEFAULT NULL;
ALTER TABLE deletion_proposals ADD COLUMN district_id CHAR(36) DEFAULT NULL;
ALTER TABLE deletion_proposals ADD CONSTRAINT fk_deletion_proposals_school FOREIGN KEY (school_id) REFERENCES schools(id);
ALTER TABLE deletion_proposals ADD CONSTRAINT fk_deletion_proposals_district FOREIGN KEY (district_id) REFERENCES school_districts(id);
