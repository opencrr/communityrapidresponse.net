-- Add plaintext_invite_link column to signal_groups
-- Used only for open-tier chats; nullable for groups that don't use this tier
ALTER TABLE signal_groups
    ADD COLUMN plaintext_invite_link TEXT NULL AFTER invite_link_updated_by;
