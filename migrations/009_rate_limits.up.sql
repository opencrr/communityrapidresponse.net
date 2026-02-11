-- Rate limiting table for tracking API request counts
-- Supports both IP-based and user-based rate limiting

CREATE TABLE rate_limits (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    limit_type VARCHAR(20) NOT NULL,       -- 'ip', 'user', 'user_endpoint'
    limit_key VARCHAR(255) NOT NULL,       -- IP address, user ID, or composite key
    endpoint VARCHAR(100) DEFAULT NULL,    -- Optional: for per-endpoint limits
    request_count INT NOT NULL DEFAULT 1,
    window_start TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_rate_limit (limit_type, limit_key, window_start),
    INDEX idx_cleanup (window_start)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
