-- Create meshtastic_channels table
CREATE TABLE IF NOT EXISTS meshtastic_channels (
    id CHAR(36) PRIMARY KEY,
    region_id CHAR(36) NULL,
    school_id CHAR(36) NULL,
    district_id CHAR(36) NULL,
    channel_name VARCHAR(255) NOT NULL,
    description TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    created_by CHAR(36) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id) ON DELETE CASCADE,
    FOREIGN KEY (school_id) REFERENCES schools(id) ON DELETE CASCADE,
    FOREIGN KEY (district_id) REFERENCES school_districts(id) ON DELETE CASCADE,
    FOREIGN KEY (created_by) REFERENCES users(id),
    INDEX idx_meshtastic_region (region_id),
    INDEX idx_meshtastic_school (school_id),
    INDEX idx_meshtastic_district (district_id),
    INDEX idx_meshtastic_active (is_active),
    CHECK (
        (region_id IS NOT NULL AND school_id IS NULL AND district_id IS NULL) OR
        (region_id IS NULL AND school_id IS NOT NULL AND district_id IS NULL) OR
        (region_id IS NULL AND school_id IS NULL AND district_id IS NOT NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Add FK from encrypted_secrets.meshtastic_channel_id to meshtastic_channels(id)
ALTER TABLE encrypted_secrets
    ADD CONSTRAINT fk_encrypted_secrets_meshtastic
    FOREIGN KEY (meshtastic_channel_id) REFERENCES meshtastic_channels(id) ON DELETE CASCADE;
