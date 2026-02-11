-- Migration 018: Add password_reset_tokens table for forgot-password flow
-- Tokens are stored as SHA-256 hashes (64 hex chars) for security.
-- Raw tokens are sent to the user via email and never stored.

CREATE TABLE IF NOT EXISTS password_reset_tokens (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    token_hash CHAR(64) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    used_at TIMESTAMP NULL,
    UNIQUE KEY uk_token_hash (token_hash),
    INDEX idx_prt_user_id (user_id),
    INDEX idx_prt_expires_at (expires_at),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
