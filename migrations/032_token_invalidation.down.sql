-- Remove token_invalidated_at column from users table
ALTER TABLE users DROP COLUMN token_invalidated_at;
