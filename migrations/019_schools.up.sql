-- Schools as organizational entity
-- Adds school_districts, schools, user_schools, school_vouches, school_blocked_users
-- Alters signal_groups to support school/district ownership with three-way XOR

-- School districts (NCES LEA data)
CREATE TABLE IF NOT EXISTS school_districts (
    id CHAR(36) PRIMARY KEY,
    nces_id VARCHAR(7) NOT NULL,
    name VARCHAR(255) NOT NULL,
    state VARCHAR(2) NOT NULL,
    district_type ENUM('unified','elementary','secondary') NOT NULL DEFAULT 'unified',
    geometry GEOMETRY NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_nces_id (nces_id),
    INDEX idx_name_state (name, state),
    INDEX idx_state (state)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Schools (NCES school data)
CREATE TABLE IF NOT EXISTS schools (
    id CHAR(36) PRIMARY KEY,
    nces_id VARCHAR(12) NOT NULL,
    district_id CHAR(36) NULL,
    name VARCHAR(255) NOT NULL,
    street_address VARCHAR(255) NULL,
    city VARCHAR(100) NULL,
    state VARCHAR(2) NOT NULL,
    zip VARCHAR(10) NULL,
    latitude DECIMAL(10,7) NULL,
    longitude DECIMAL(10,7) NULL,
    region_id CHAR(36) NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_nces_id (nces_id),
    INDEX idx_name (name),
    INDEX idx_state (state),
    INDEX idx_district_id (district_id),
    INDEX idx_region_id (region_id),
    FOREIGN KEY (district_id) REFERENCES school_districts(id) ON DELETE SET NULL,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- User-school membership (like user_regions)
CREATE TABLE IF NOT EXISTS user_schools (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    school_id CHAR(36) NOT NULL,
    is_admin BOOLEAN NOT NULL DEFAULT FALSE,
    verification_status ENUM('pending','verified') NOT NULL DEFAULT 'pending',
    verified_at TIMESTAMP NULL,
    UNIQUE KEY uk_user_school (user_id, school_id),
    INDEX idx_user_id (user_id),
    INDEX idx_school_id (school_id),
    INDEX idx_school_admin (school_id, is_admin),
    INDEX idx_user_status (user_id, verification_status),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (school_id) REFERENCES schools(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- School vouches (separate from region vouches, scoped to school)
CREATE TABLE IF NOT EXISTS school_vouches (
    id CHAR(36) PRIMARY KEY,
    voucher_user_id CHAR(36) NOT NULL,
    vouched_user_id CHAR(36) NOT NULL,
    school_id CHAR(36) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_voucher_vouched_school (voucher_user_id, vouched_user_id, school_id),
    INDEX idx_vouched_school (vouched_user_id, school_id),
    INDEX idx_voucher (voucher_user_id),
    CONSTRAINT chk_no_self_vouch CHECK (voucher_user_id != vouched_user_id),
    FOREIGN KEY (voucher_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (vouched_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (school_id) REFERENCES schools(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- School-level blocklist
CREATE TABLE IF NOT EXISTS school_blocked_users (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    school_id CHAR(36) NOT NULL,
    blocked_by CHAR(36) NOT NULL,
    reason TEXT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_user_school_block (user_id, school_id),
    INDEX idx_school_id (school_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (school_id) REFERENCES schools(id) ON DELETE CASCADE,
    FOREIGN KEY (blocked_by) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Alter signal_groups to support school and district ownership
-- Step 1: Drop existing foreign key on region_id so we can modify the column
ALTER TABLE signal_groups DROP FOREIGN KEY signal_groups_ibfk_1;

-- Step 2: Make region_id nullable
ALTER TABLE signal_groups MODIFY COLUMN region_id CHAR(36) NULL;

-- Step 3: Add school_id and district_id columns
ALTER TABLE signal_groups ADD COLUMN school_id CHAR(36) NULL AFTER region_id;
ALTER TABLE signal_groups ADD COLUMN district_id CHAR(36) NULL AFTER school_id;

-- Step 4: Add indexes
ALTER TABLE signal_groups ADD INDEX idx_school_id (school_id);
ALTER TABLE signal_groups ADD INDEX idx_district_id (district_id);

-- Step 5: Add foreign keys
ALTER TABLE signal_groups ADD CONSTRAINT fk_signal_groups_region FOREIGN KEY (region_id) REFERENCES geographic_regions(id) ON DELETE CASCADE;
ALTER TABLE signal_groups ADD CONSTRAINT fk_signal_groups_school FOREIGN KEY (school_id) REFERENCES schools(id) ON DELETE CASCADE;
ALTER TABLE signal_groups ADD CONSTRAINT fk_signal_groups_district FOREIGN KEY (district_id) REFERENCES school_districts(id) ON DELETE CASCADE;

-- Step 6: Add three-way XOR constraint (exactly one of region_id, school_id, district_id must be set)
ALTER TABLE signal_groups ADD CONSTRAINT chk_signal_group_owner CHECK (
    (region_id IS NOT NULL AND school_id IS NULL AND district_id IS NULL) OR
    (region_id IS NULL AND school_id IS NOT NULL AND district_id IS NULL) OR
    (region_id IS NULL AND school_id IS NULL AND district_id IS NOT NULL)
);
