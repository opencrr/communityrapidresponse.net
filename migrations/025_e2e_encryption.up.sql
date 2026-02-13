-- 025: E2E encryption - user encryption keys
-- Each user gets an RSA-OAEP keypair; the private key is wrapped with PBKDF2-derived AES key

CREATE TABLE user_encryption_keys (
    user_id CHAR(36) PRIMARY KEY,
    public_key TEXT NOT NULL,
    wrapped_private_key TEXT NOT NULL,
    key_salt CHAR(24) NOT NULL,
    key_iv CHAR(16) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    rotated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
