-- Remove account lockout columns
ALTER TABLE users
    DROP COLUMN failed_login_attempts,
    DROP COLUMN locked_until;
