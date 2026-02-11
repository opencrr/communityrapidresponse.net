-- Rollback migration 018: Remove password_reset_tokens table
DROP TABLE IF EXISTS password_reset_tokens;
