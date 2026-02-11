-- Rollback migration 015: Restore user_id NOT NULL constraint

-- First delete any notifications with NULL user_id (fan-out events)
DELETE FROM email_notifications WHERE user_id IS NULL;

-- Drop the foreign key constraint
ALTER TABLE email_notifications
DROP FOREIGN KEY fk_email_notifications_user;

-- Modify user_id to be NOT NULL
ALTER TABLE email_notifications
MODIFY COLUMN user_id CHAR(36) NOT NULL;

-- Re-add the foreign key
ALTER TABLE email_notifications
ADD CONSTRAINT fk_email_notifications_user
FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
