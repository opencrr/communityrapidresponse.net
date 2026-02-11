-- Create user_reports table for user reporting/flagging system
CREATE TABLE user_reports (
    id                    CHAR(36)    NOT NULL PRIMARY KEY,
    reporter_id           CHAR(36)    NOT NULL,
    reported_user_id      CHAR(36)    NOT NULL,
    region_id             CHAR(36)    NULL,
    school_id             CHAR(36)    NULL,
    district_id           CHAR(36)    NULL,
    reason                VARCHAR(50) NOT NULL,
    details               TEXT        NULL,
    status                VARCHAR(50) NOT NULL DEFAULT 'pending',
    resolved_by           CHAR(36)    NULL,
    resolution_note       TEXT        NULL,
    blocklist_proposal_id CHAR(36)    NULL,
    created_at            TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at           TIMESTAMP   NULL,

    FOREIGN KEY (reporter_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (reported_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (region_id) REFERENCES geographic_regions(id) ON DELETE CASCADE,
    FOREIGN KEY (school_id) REFERENCES schools(id) ON DELETE CASCADE,
    FOREIGN KEY (district_id) REFERENCES school_districts(id) ON DELETE CASCADE,
    FOREIGN KEY (resolved_by) REFERENCES users(id) ON DELETE SET NULL,
    FOREIGN KEY (blocklist_proposal_id) REFERENCES blocklist_proposals(id) ON DELETE SET NULL,

    -- Exactly one scope must be set (3-way XOR, same as signal_groups)
    CONSTRAINT chk_report_scope CHECK (
        (region_id IS NOT NULL AND school_id IS NULL AND district_id IS NULL) OR
        (region_id IS NULL AND school_id IS NOT NULL AND district_id IS NULL) OR
        (region_id IS NULL AND school_id IS NULL AND district_id IS NOT NULL)
    ),
    CONSTRAINT chk_report_not_self CHECK (reporter_id != reported_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_user_reports_region ON user_reports (region_id, status);
CREATE INDEX idx_user_reports_school ON user_reports (school_id, status);
CREATE INDEX idx_user_reports_district ON user_reports (district_id, status);
CREATE INDEX idx_user_reports_reported ON user_reports (reported_user_id);
CREATE INDEX idx_user_reports_reporter_created ON user_reports (reporter_id, created_at);
