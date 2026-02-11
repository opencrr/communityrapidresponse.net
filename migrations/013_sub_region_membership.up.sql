-- Migration: 013_sub_region_membership
-- Description: Add tables for sub-region membership requests and votes
-- This supports the self-selection and admin invitation flows for adding
-- users to admin-created sub-regions (city blocks, custom neighborhoods)

-- Membership requests table (for both self-selection and admin invitations)
CREATE TABLE IF NOT EXISTS sub_region_membership_requests (
    id CHAR(36) NOT NULL,
    user_id CHAR(36) NOT NULL,
    region_id CHAR(36) NOT NULL,
    parent_region_id CHAR(36) NOT NULL,
    request_type ENUM('request', 'invitation') NOT NULL,
    status ENUM('pending', 'approved', 'rejected', 'expired', 'cancelled') NOT NULL DEFAULT 'pending',
    initiated_by CHAR(36) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    resolved_at TIMESTAMP NULL,

    PRIMARY KEY (id),
    -- Only one pending request per user per region
    UNIQUE KEY uk_pending_user_region (user_id, region_id, status),
    INDEX idx_region_status (region_id, status),
    INDEX idx_user_id (user_id),
    INDEX idx_expires_at (expires_at),
    INDEX idx_initiated_by (initiated_by),

    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id) ON DELETE CASCADE,
    FOREIGN KEY (parent_region_id) REFERENCES geographic_regions(id) ON DELETE CASCADE,
    FOREIGN KEY (initiated_by) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Votes on membership requests (2+ approvals required for self-selection)
CREATE TABLE IF NOT EXISTS sub_region_membership_votes (
    id CHAR(36) NOT NULL,
    request_id CHAR(36) NOT NULL,
    voter_id CHAR(36) NOT NULL,
    vote BOOLEAN NOT NULL,  -- true=approve, false=reject
    voted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    -- One vote per admin per request
    UNIQUE KEY uk_request_voter (request_id, voter_id),
    INDEX idx_request_id (request_id),

    FOREIGN KEY (request_id) REFERENCES sub_region_membership_requests(id) ON DELETE CASCADE,
    FOREIGN KEY (voter_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
