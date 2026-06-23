-- Add access tier to signal groups (per-chat access policy)
ALTER TABLE signal_groups
    ADD COLUMN access_tier ENUM('open', 'resident', 'member', 'trusted', 'admin_only')
    NOT NULL DEFAULT 'member'
    AFTER description;

-- Add configurable trusted vouch threshold to groups
ALTER TABLE `groups`
    ADD COLUMN trusted_vouch_threshold INT NOT NULL DEFAULT 2
    AFTER founding_threshold;

-- Trust vouches within a group (for promoting members to trusted tier)
CREATE TABLE IF NOT EXISTS group_trust_vouches (
    id CHAR(36) PRIMARY KEY,
    group_id CHAR(36) NOT NULL,
    voucher_user_id CHAR(36) NOT NULL,
    vouched_user_id CHAR(36) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_group_trust_vouches (group_id, voucher_user_id, vouched_user_id),
    INDEX idx_group_trust_vouches_vouched (group_id, vouched_user_id),
    CONSTRAINT fk_group_trust_vouches_group FOREIGN KEY (group_id) REFERENCES `groups`(id) ON DELETE CASCADE,
    CONSTRAINT fk_group_trust_vouches_voucher FOREIGN KEY (voucher_user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_group_trust_vouches_vouched FOREIGN KEY (vouched_user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT chk_group_trust_vouches_no_self CHECK (voucher_user_id != vouched_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
