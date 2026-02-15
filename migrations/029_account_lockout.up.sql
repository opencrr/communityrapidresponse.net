-- Add account lockout columns for brute-force protection
ALTER TABLE users
    ADD COLUMN failed_login_attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN locked_until TIMESTAMP NULL DEFAULT NULL;
