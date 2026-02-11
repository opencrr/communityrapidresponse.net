-- Add email verification columns to users table
ALTER TABLE users
    ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN email_normalized VARCHAR(255) DEFAULT NULL;

-- Create unique index on normalized email to prevent duplicates
CREATE UNIQUE INDEX idx_users_email_normalized ON users(email_normalized);

-- Grandfather existing users as verified with their email normalized
UPDATE users SET email_verified = TRUE, email_normalized = LOWER(email);
