-- Remove content deduplication index and column
DROP INDEX idx_content_hash ON email_notifications;
ALTER TABLE email_notifications DROP COLUMN content_hash;
