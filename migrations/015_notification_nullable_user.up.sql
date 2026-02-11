-- Migration 015: Make user_id nullable in email_notifications for fan-out events
-- Fan-out events are queued without a user_id and expanded by the worker to individual notifications

-- Drop the existing foreign key constraint
ALTER TABLE email_notifications
DROP FOREIGN KEY fk_email_notifications_user;

-- Modify user_id to be nullable
ALTER TABLE email_notifications
MODIFY COLUMN user_id CHAR(36) NULL;

-- Add the foreign key back with ON DELETE SET NULL for the nullable column
ALTER TABLE email_notifications
ADD CONSTRAINT fk_email_notifications_user
FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
