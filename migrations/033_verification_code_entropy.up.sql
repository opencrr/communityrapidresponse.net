ALTER TABLE verification_requests ADD COLUMN failed_verification_attempts INT NOT NULL DEFAULT 0;
