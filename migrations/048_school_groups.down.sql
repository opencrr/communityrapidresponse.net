ALTER TABLE `groups` DROP FOREIGN KEY fk_groups_school;
ALTER TABLE `groups` DROP INDEX idx_groups_school;
ALTER TABLE `groups` DROP COLUMN school_id;
