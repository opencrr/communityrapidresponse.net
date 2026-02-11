-- Migration 005: Fix foreign key constraints to allow user deletion
-- Some foreign keys reference users without ON DELETE CASCADE/SET NULL

-- geographic_regions.created_by - SET NULL to preserve region but clear creator reference
ALTER TABLE geographic_regions DROP FOREIGN KEY geographic_regions_ibfk_1;
ALTER TABLE geographic_regions MODIFY COLUMN created_by CHAR(36) NULL;
ALTER TABLE geographic_regions ADD CONSTRAINT fk_regions_created_by
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;

-- signal_groups.invite_link_updated_by - SET NULL to preserve group
ALTER TABLE signal_groups DROP FOREIGN KEY signal_groups_ibfk_2;
ALTER TABLE signal_groups ADD CONSTRAINT fk_signal_groups_invite_updated_by
    FOREIGN KEY (invite_link_updated_by) REFERENCES users(id) ON DELETE SET NULL;

-- signal_groups.created_by - SET NULL to preserve group
ALTER TABLE signal_groups DROP FOREIGN KEY signal_groups_ibfk_3;
ALTER TABLE signal_groups MODIFY COLUMN created_by CHAR(36) NULL;
ALTER TABLE signal_groups ADD CONSTRAINT fk_signal_groups_created_by
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;

-- deletion_proposals.proposed_by - SET NULL (proposal remains for audit)
ALTER TABLE deletion_proposals DROP FOREIGN KEY deletion_proposals_ibfk_1;
ALTER TABLE deletion_proposals MODIFY COLUMN proposed_by CHAR(36) NULL;
ALTER TABLE deletion_proposals ADD CONSTRAINT fk_deletion_proposals_proposed_by
    FOREIGN KEY (proposed_by) REFERENCES users(id) ON DELETE SET NULL;

-- invite_link_update_proposals.proposed_by - SET NULL
ALTER TABLE invite_link_update_proposals DROP FOREIGN KEY invite_link_update_proposals_ibfk_3;
ALTER TABLE invite_link_update_proposals MODIFY COLUMN proposed_by CHAR(36) NULL;
ALTER TABLE invite_link_update_proposals ADD CONSTRAINT fk_invite_proposals_proposed_by
    FOREIGN KEY (proposed_by) REFERENCES users(id) ON DELETE SET NULL;

-- blacklist_proposals.proposed_by - SET NULL
ALTER TABLE blacklist_proposals DROP FOREIGN KEY blacklist_proposals_ibfk_3;
ALTER TABLE blacklist_proposals MODIFY COLUMN proposed_by CHAR(36) NULL;
ALTER TABLE blacklist_proposals ADD CONSTRAINT fk_blacklist_proposals_proposed_by
    FOREIGN KEY (proposed_by) REFERENCES users(id) ON DELETE SET NULL;

-- email_notifications.user_id - CASCADE (delete notifications when user is deleted)
ALTER TABLE email_notifications DROP FOREIGN KEY email_notifications_ibfk_1;
ALTER TABLE email_notifications ADD CONSTRAINT fk_email_notifications_user
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

-- audit_log.user_id - SET NULL to preserve audit records
ALTER TABLE audit_log DROP FOREIGN KEY audit_log_ibfk_1;
ALTER TABLE audit_log MODIFY COLUMN user_id CHAR(36) NULL;
ALTER TABLE audit_log ADD CONSTRAINT fk_audit_log_user
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL;
