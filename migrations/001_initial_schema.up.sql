-- Community Rapid Response Database Schema
-- Migration 001: Initial Schema
-- MariaDB 10.5+ with spatial extensions

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id CHAR(36) PRIMARY KEY,
    username VARCHAR(255) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    verification_tier INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login TIMESTAMP NULL,
    INDEX idx_verification_tier (verification_tier)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Geographic regions table
CREATE TABLE IF NOT EXISTS geographic_regions (
    id CHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    region_type VARCHAR(50) NOT NULL,
    parent_region_id CHAR(36),
    geometry POLYGON NOT NULL,
    created_by CHAR(36),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_region_type (region_type),
    INDEX idx_parent_region (parent_region_id),
    -- Note: Spatial index disabled due to MariaDB Error 1207 with ST_Contains queries
    -- SPATIAL INDEX idx_geometry (geometry),
    FOREIGN KEY (parent_region_id) REFERENCES geographic_regions(id),
    FOREIGN KEY (created_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- User regions (membership) table
CREATE TABLE IF NOT EXISTS user_regions (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    region_id CHAR(36) NOT NULL,
    is_admin BOOLEAN DEFAULT FALSE,
    verified_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_user_region (user_id, region_id),
    INDEX idx_user_id (user_id),
    INDEX idx_region_id (region_id),
    INDEX idx_admins (region_id, is_admin),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Admin boundaries table
-- Note: boundary_geometry is nullable so we skip SPATIAL INDEX (can add later if needed)
CREATE TABLE IF NOT EXISTS admin_boundaries (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    boundary_type VARCHAR(50) NOT NULL,
    boundary_name VARCHAR(255) NOT NULL,
    boundary_state VARCHAR(100) NOT NULL,
    boundary_country VARCHAR(100) NOT NULL DEFAULT 'USA',
    mapbox_place_id VARCHAR(255),
    boundary_geometry POLYGON,
    verified_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_user_boundary (user_id, boundary_type, boundary_name, boundary_state),
    INDEX idx_user_id (user_id),
    INDEX idx_boundary_name (boundary_name, boundary_state),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Verification requests table (NO address stored)
CREATE TABLE IF NOT EXISTS verification_requests (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    region_id CHAR(36),
    verification_code VARCHAR(32) UNIQUE NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    postgrid_request_id VARCHAR(255) NOT NULL,
    boundary_type VARCHAR(50) NOT NULL,
    boundary_name VARCHAR(255) NOT NULL,
    boundary_state VARCHAR(100) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    verified_at TIMESTAMP NULL,
    INDEX idx_user_status (user_id, status),
    INDEX idx_verification_code (verification_code),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Vouches table
CREATE TABLE IF NOT EXISTS vouches (
    id CHAR(36) PRIMARY KEY,
    voucher_user_id CHAR(36) NOT NULL,
    vouched_user_id CHAR(36) NOT NULL,
    region_id CHAR(36),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_voucher_vouched (voucher_user_id, vouched_user_id),
    INDEX idx_vouched_user (vouched_user_id),
    FOREIGN KEY (voucher_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (vouched_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id),
    CONSTRAINT chk_no_self_vouch CHECK (voucher_user_id != vouched_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Signal groups table
CREATE TABLE IF NOT EXISTS signal_groups (
    id CHAR(36) PRIMARY KEY,
    region_id CHAR(36) NOT NULL,
    group_name VARCHAR(255) NOT NULL,
    invite_link TEXT NOT NULL,
    invite_link_updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    invite_link_updated_by CHAR(36),
    description TEXT,
    created_by CHAR(36),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    is_active BOOLEAN DEFAULT TRUE,
    INDEX idx_region_id (region_id),
    INDEX idx_active (is_active),
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id) ON DELETE CASCADE,
    FOREIGN KEY (invite_link_updated_by) REFERENCES users(id),
    FOREIGN KEY (created_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Deletion proposals table
CREATE TABLE IF NOT EXISTS deletion_proposals (
    id CHAR(36) PRIMARY KEY,
    asset_type VARCHAR(50) NOT NULL,
    asset_id CHAR(36) NOT NULL,
    region_id CHAR(36),
    proposed_by CHAR(36),
    reason TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMP NULL,
    INDEX idx_status (status),
    INDEX idx_asset (asset_type, asset_id),
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id),
    FOREIGN KEY (proposed_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Deletion votes table
CREATE TABLE IF NOT EXISTS deletion_votes (
    id CHAR(36) PRIMARY KEY,
    proposal_id CHAR(36) NOT NULL,
    voter_id CHAR(36) NOT NULL,
    vote BOOLEAN NOT NULL,
    voted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_proposal_voter (proposal_id, voter_id),
    INDEX idx_proposal_id (proposal_id),
    FOREIGN KEY (proposal_id) REFERENCES deletion_proposals(id) ON DELETE CASCADE,
    FOREIGN KEY (voter_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Invite link update proposals table
CREATE TABLE IF NOT EXISTS invite_link_update_proposals (
    id CHAR(36) PRIMARY KEY,
    signal_group_id CHAR(36) NOT NULL,
    region_id CHAR(36),
    proposed_by CHAR(36),
    new_invite_link TEXT NOT NULL,
    reason TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    resolved_at TIMESTAMP NULL,
    INDEX idx_status (status),
    INDEX idx_signal_group (signal_group_id),
    INDEX idx_region (region_id),
    FOREIGN KEY (signal_group_id) REFERENCES signal_groups(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id),
    FOREIGN KEY (proposed_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Invite link update votes table
CREATE TABLE IF NOT EXISTS invite_link_update_votes (
    id CHAR(36) PRIMARY KEY,
    proposal_id CHAR(36) NOT NULL,
    voter_id CHAR(36) NOT NULL,
    vote BOOLEAN NOT NULL,
    voted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_proposal_voter (proposal_id, voter_id),
    INDEX idx_proposal_id (proposal_id),
    FOREIGN KEY (proposal_id) REFERENCES invite_link_update_proposals(id) ON DELETE CASCADE,
    FOREIGN KEY (voter_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Blacklist proposals table
CREATE TABLE IF NOT EXISTS blacklist_proposals (
    id CHAR(36) PRIMARY KEY,
    target_user_id CHAR(36) NOT NULL,
    region_id CHAR(36),
    proposed_by CHAR(36),
    reason TEXT NOT NULL,
    evidence TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMP NULL,
    INDEX idx_status (status),
    INDEX idx_target_user (target_user_id),
    INDEX idx_region (region_id),
    FOREIGN KEY (target_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id),
    FOREIGN KEY (proposed_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Blacklist votes table
CREATE TABLE IF NOT EXISTS blacklist_votes (
    id CHAR(36) PRIMARY KEY,
    proposal_id CHAR(36) NOT NULL,
    voter_id CHAR(36) NOT NULL,
    vote BOOLEAN NOT NULL,
    voted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_proposal_voter (proposal_id, voter_id),
    INDEX idx_proposal_id (proposal_id),
    FOREIGN KEY (proposal_id) REFERENCES blacklist_proposals(id) ON DELETE CASCADE,
    FOREIGN KEY (voter_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Blacklisted users table
CREATE TABLE IF NOT EXISTS blacklisted_users (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    region_id CHAR(36) NOT NULL,
    proposal_id CHAR(36),
    blacklisted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    blacklisted_until TIMESTAMP NULL,
    UNIQUE KEY uk_user_region (user_id, region_id),
    INDEX idx_user_id (user_id),
    INDEX idx_region_id (region_id),
    INDEX idx_blacklisted_until (blacklisted_until),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id) ON DELETE CASCADE,
    FOREIGN KEY (proposal_id) REFERENCES blacklist_proposals(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Audit log table
CREATE TABLE IF NOT EXISTS audit_log (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36),
    action VARCHAR(100) NOT NULL,
    resource_type VARCHAR(50),
    resource_id CHAR(36),
    details JSON,
    ip_address VARCHAR(45),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_user_action (user_id, action),
    INDEX idx_created_at (created_at),
    FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Email notifications table
CREATE TABLE IF NOT EXISTS email_notifications (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    notification_type VARCHAR(50) NOT NULL,
    resource_type VARCHAR(50),
    resource_id CHAR(36),
    status VARCHAR(50) NOT NULL DEFAULT 'queued',
    queued_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at TIMESTAMP NULL,
    error_message TEXT,
    INDEX idx_user_type (user_id, notification_type),
    INDEX idx_status (status),
    INDEX idx_queued_at (queued_at),
    INDEX idx_rate_limit (user_id, notification_type, resource_id, queued_at),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
