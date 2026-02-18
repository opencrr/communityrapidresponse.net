-- Add token_invalidated_at column to users table
-- Used to invalidate JWT tokens issued before a privilege change
ALTER TABLE users ADD COLUMN token_invalidated_at TIMESTAMP NULL DEFAULT NULL;
