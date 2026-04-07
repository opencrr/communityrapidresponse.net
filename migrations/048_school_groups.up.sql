-- Link groups to schools
ALTER TABLE `groups`
    ADD COLUMN school_id CHAR(36) NULL AFTER show_address_verification;

ALTER TABLE `groups`
    ADD CONSTRAINT fk_groups_school
    FOREIGN KEY (school_id) REFERENCES schools(id) ON DELETE SET NULL;

ALTER TABLE `groups`
    ADD INDEX idx_groups_school (school_id);
