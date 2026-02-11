-- Add deleted_at column to users table for soft-delete support
ALTER TABLE users ADD COLUMN deleted_at TIMESTAMP NULL DEFAULT NULL;

-- Index for filtering out deleted users
CREATE INDEX idx_users_deleted_at ON users (deleted_at);
