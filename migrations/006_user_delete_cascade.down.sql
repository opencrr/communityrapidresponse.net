-- Rollback Migration 006: Restore original foreign key constraints
-- WARNING: This restores constraints without ON DELETE CASCADE/SET NULL

-- audit_log.user_id - restore original constraint
ALTER TABLE audit_log DROP FOREIGN KEY fk_audit_log_user;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_ibfk_1
    FOREIGN KEY (user_id) REFERENCES users(id);

-- email_notifications.user_id - restore original constraint
ALTER TABLE email_notifications DROP FOREIGN KEY fk_email_notifications_user;
ALTER TABLE email_notifications ADD CONSTRAINT email_notifications_ibfk_1
    FOREIGN KEY (user_id) REFERENCES users(id);

-- blacklist_proposals.proposed_by - restore original constraint
ALTER TABLE blacklist_proposals DROP FOREIGN KEY fk_blacklist_proposals_proposed_by;
ALTER TABLE blacklist_proposals ADD CONSTRAINT blacklist_proposals_ibfk_3
    FOREIGN KEY (proposed_by) REFERENCES users(id);

-- invite_link_update_proposals.proposed_by - restore original constraint
ALTER TABLE invite_link_update_proposals DROP FOREIGN KEY fk_invite_proposals_proposed_by;
ALTER TABLE invite_link_update_proposals ADD CONSTRAINT invite_link_update_proposals_ibfk_3
    FOREIGN KEY (proposed_by) REFERENCES users(id);

-- deletion_proposals.proposed_by - restore original constraint
ALTER TABLE deletion_proposals DROP FOREIGN KEY fk_deletion_proposals_proposed_by;
ALTER TABLE deletion_proposals ADD CONSTRAINT deletion_proposals_ibfk_1
    FOREIGN KEY (proposed_by) REFERENCES users(id);

-- signal_groups.created_by - restore original constraint
ALTER TABLE signal_groups DROP FOREIGN KEY fk_signal_groups_created_by;
ALTER TABLE signal_groups ADD CONSTRAINT signal_groups_ibfk_3
    FOREIGN KEY (created_by) REFERENCES users(id);

-- signal_groups.invite_link_updated_by - restore original constraint
ALTER TABLE signal_groups DROP FOREIGN KEY fk_signal_groups_invite_updated_by;
ALTER TABLE signal_groups ADD CONSTRAINT signal_groups_ibfk_2
    FOREIGN KEY (invite_link_updated_by) REFERENCES users(id);

-- geographic_regions.created_by - restore original constraint
ALTER TABLE geographic_regions DROP FOREIGN KEY fk_regions_created_by;
ALTER TABLE geographic_regions ADD CONSTRAINT geographic_regions_ibfk_1
    FOREIGN KEY (created_by) REFERENCES users(id);
