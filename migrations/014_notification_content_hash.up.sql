-- Add content_hash column for content-based deduplication
-- This stores a SHA-256 hash of the email content to prevent duplicate emails
ALTER TABLE email_notifications
ADD COLUMN content_hash CHAR(64) NULL AFTER error_message;

-- Index for content deduplication lookups
CREATE INDEX idx_content_hash ON email_notifications(user_id, content_hash, sent_at);
