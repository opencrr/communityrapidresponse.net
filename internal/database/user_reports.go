package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

var (
	ErrReportNotFound = errors.New("report not found")
)

// UserReportRepository handles user report database operations
type UserReportRepository struct {
	db *DB
}

// NewUserReportRepository creates a new user report repository
func NewUserReportRepository(db *DB) *UserReportRepository {
	return &UserReportRepository{db: db}
}

// Create creates a new user report
func (r *UserReportRepository) Create(ctx context.Context, report *models.UserReport) error {
	report.ID = uuid.New().String()
	report.CreatedAt = time.Now().UTC()
	report.Status = models.ReportStatusPending

	query := `
		INSERT INTO user_reports
		(id, reporter_id, reported_user_id, region_id, school_id, district_id, reason, details, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		report.ID,
		report.ReporterID,
		report.ReportedUserID,
		report.RegionID,
		report.SchoolID,
		report.DistrictID,
		report.Reason,
		report.Details,
		report.Status,
		report.CreatedAt,
	)

	return err
}

// GetByID retrieves a report by ID
func (r *UserReportRepository) GetByID(ctx context.Context, id string) (*models.UserReport, error) {
	query := `
		SELECT id, reporter_id, reported_user_id, region_id, school_id, district_id,
			reason, details, status, resolved_by, resolution_note, blocklist_proposal_id,
			created_at, resolved_at
		FROM user_reports
		WHERE id = ?
	`

	report := &models.UserReport{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&report.ID,
		&report.ReporterID,
		&report.ReportedUserID,
		&report.RegionID,
		&report.SchoolID,
		&report.DistrictID,
		&report.Reason,
		&report.Details,
		&report.Status,
		&report.ResolvedBy,
		&report.ResolutionNote,
		&report.BlocklistProposalID,
		&report.CreatedAt,
		&report.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReportNotFound
	}
	if err != nil {
		return nil, err
	}

	return report, nil
}

// GetByIDWithDetails retrieves a report with joined user and scope info
func (r *UserReportRepository) GetByIDWithDetails(ctx context.Context, id string) (*models.ReportDetailResponse, error) {
	query := `
		SELECT ur.id, reporter.username, ur.reported_user_id, reported.username,
			ur.region_id, ur.school_id, ur.district_id,
			gr.name, s.name, sd.name,
			ur.reason, ur.details, ur.status,
			resolver.username, ur.resolution_note, ur.blocklist_proposal_id,
			ur.created_at, ur.resolved_at
		FROM user_reports ur
		JOIN users reporter ON ur.reporter_id = reporter.id
		JOIN users reported ON ur.reported_user_id = reported.id
		LEFT JOIN geographic_regions gr ON ur.region_id = gr.id
		LEFT JOIN schools s ON ur.school_id = s.id
		LEFT JOIN school_districts sd ON ur.district_id = sd.id
		LEFT JOIN users resolver ON ur.resolved_by = resolver.id
		WHERE ur.id = ?
	`

	var regionID, schoolID, districtID sql.NullString
	var regionName, schoolName, districtName sql.NullString
	var resolverUsername sql.NullString

	detail := &models.ReportDetailResponse{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&detail.ID,
		&detail.ReporterUsername,
		&detail.ReportedUserID,
		&detail.ReportedUsername,
		&regionID,
		&schoolID,
		&districtID,
		&regionName,
		&schoolName,
		&districtName,
		&detail.Reason,
		&detail.Details,
		&detail.Status,
		&resolverUsername,
		&detail.ResolutionNote,
		&detail.BlocklistProposalID,
		&detail.CreatedAt,
		&detail.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReportNotFound
	}
	if err != nil {
		return nil, err
	}

	// Determine scope type and name
	if regionID.Valid {
		detail.ScopeType = "region"
		detail.ScopeID = regionID.String
		detail.ScopeName = regionName.String
	} else if schoolID.Valid {
		detail.ScopeType = "school"
		detail.ScopeID = schoolID.String
		detail.ScopeName = schoolName.String
	} else if districtID.Valid {
		detail.ScopeType = "district"
		detail.ScopeID = districtID.String
		detail.ScopeName = districtName.String
	}

	if resolverUsername.Valid {
		detail.ResolvedByUsername = &resolverUsername.String
	}

	return detail, nil
}

// ListByRegionAdmin returns reports in regions where the user is admin
func (r *UserReportRepository) ListByRegionAdmin(ctx context.Context, userID string, filter models.ReportListFilter) ([]*models.ReportSummary, error) {
	query := `
		WITH RECURSIVE user_admin_regions AS (
			SELECT gr.id, gr.parent_region_id
			FROM geographic_regions gr
			INNER JOIN user_regions ur ON gr.id = ur.region_id
			WHERE ur.user_id = ? AND ur.is_admin = TRUE

			UNION

			SELECT gr.id, gr.parent_region_id
			FROM geographic_regions gr
			INNER JOIN user_admin_regions uar ON gr.id = uar.parent_region_id
		)
		SELECT DISTINCT ur.id, reported.username, ur.reported_user_id,
			'region' AS scope_type, gr.name AS scope_name, COALESCE(ur.region_id, '') AS scope_id,
			ur.reason, ur.status,
			(SELECT COUNT(*) FROM user_reports ur2 WHERE ur2.reported_user_id = ur.reported_user_id AND ur2.status = 'pending') AS report_count,
			ur.created_at
		FROM user_reports ur
		JOIN users reported ON ur.reported_user_id = reported.id
		JOIN geographic_regions gr ON ur.region_id = gr.id
		JOIN user_admin_regions uar ON ur.region_id = uar.id
		WHERE ur.region_id IS NOT NULL
	`

	args := []interface{}{userID}

	if filter.Status != "" {
		query += " AND ur.status = ?"
		args = append(args, filter.Status)
	}
	if filter.RegionID != "" {
		query += " AND ur.region_id = ?"
		args = append(args, filter.RegionID)
	}

	query += " ORDER BY ur.created_at DESC"

	return r.scanReportSummaries(ctx, query, args...)
}

// ListBySchoolAdmin returns reports in schools where the user is admin
func (r *UserReportRepository) ListBySchoolAdmin(ctx context.Context, userID string, filter models.ReportListFilter) ([]*models.ReportSummary, error) {
	// Reports scoped to schools where user is a verified admin
	query := `
		SELECT DISTINCT ur.id, reported.username, ur.reported_user_id,
			CASE
				WHEN ur.school_id IS NOT NULL THEN 'school'
				WHEN ur.district_id IS NOT NULL THEN 'district'
			END AS scope_type,
			COALESCE(s.name, sd.name) AS scope_name,
			COALESCE(ur.school_id, ur.district_id, '') AS scope_id,
			ur.reason, ur.status,
			(SELECT COUNT(*) FROM user_reports ur2 WHERE ur2.reported_user_id = ur.reported_user_id AND ur2.status = 'pending') AS report_count,
			ur.created_at
		FROM user_reports ur
		JOIN users reported ON ur.reported_user_id = reported.id
		LEFT JOIN schools s ON ur.school_id = s.id
		LEFT JOIN school_districts sd ON ur.district_id = sd.id
		WHERE (
			(ur.school_id IS NOT NULL AND EXISTS (
				SELECT 1 FROM user_schools us
				WHERE us.user_id = ? AND us.school_id = ur.school_id
				AND us.is_admin = TRUE AND us.verification_status = 'verified'
			))
			OR
			(ur.district_id IS NOT NULL AND EXISTS (
				SELECT 1 FROM user_schools us
				JOIN schools ds ON us.school_id = ds.id
				WHERE us.user_id = ? AND ds.district_id = ur.district_id
				AND us.is_admin = TRUE AND us.verification_status = 'verified'
			))
		)
	`

	args := []interface{}{userID, userID}

	if filter.Status != "" {
		query += " AND ur.status = ?"
		args = append(args, filter.Status)
	}
	if filter.SchoolID != "" {
		query += " AND ur.school_id = ?"
		args = append(args, filter.SchoolID)
	}
	if filter.DistrictID != "" {
		query += " AND ur.district_id = ?"
		args = append(args, filter.DistrictID)
	}

	query += " ORDER BY ur.created_at DESC"

	return r.scanReportSummaries(ctx, query, args...)
}

// ListAll returns all reports (superuser only)
func (r *UserReportRepository) ListAll(ctx context.Context, filter models.ReportListFilter) ([]*models.ReportSummary, error) {
	query := `
		SELECT ur.id, reported.username, ur.reported_user_id,
			CASE
				WHEN ur.region_id IS NOT NULL THEN 'region'
				WHEN ur.school_id IS NOT NULL THEN 'school'
				WHEN ur.district_id IS NOT NULL THEN 'district'
			END AS scope_type,
			COALESCE(gr.name, s.name, sd.name) AS scope_name,
			COALESCE(ur.region_id, ur.school_id, ur.district_id, '') AS scope_id,
			ur.reason, ur.status,
			(SELECT COUNT(*) FROM user_reports ur2 WHERE ur2.reported_user_id = ur.reported_user_id AND ur2.status = 'pending') AS report_count,
			ur.created_at
		FROM user_reports ur
		JOIN users reported ON ur.reported_user_id = reported.id
		LEFT JOIN geographic_regions gr ON ur.region_id = gr.id
		LEFT JOIN schools s ON ur.school_id = s.id
		LEFT JOIN school_districts sd ON ur.district_id = sd.id
		WHERE 1=1
	`

	args := []interface{}{}

	if filter.Status != "" {
		query += " AND ur.status = ?"
		args = append(args, filter.Status)
	}
	if filter.RegionID != "" {
		query += " AND ur.region_id = ?"
		args = append(args, filter.RegionID)
	}
	if filter.SchoolID != "" {
		query += " AND ur.school_id = ?"
		args = append(args, filter.SchoolID)
	}
	if filter.DistrictID != "" {
		query += " AND ur.district_id = ?"
		args = append(args, filter.DistrictID)
	}

	query += " ORDER BY ur.created_at DESC"

	return r.scanReportSummaries(ctx, query, args...)
}

// CountReportsThisWeek counts how many reports a user has filed in the past 7 days
func (r *UserReportRepository) CountReportsThisWeek(ctx context.Context, reporterID string) (int, error) {
	query := `
		SELECT COUNT(*) FROM user_reports
		WHERE reporter_id = ?
		AND created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
	`
	var count int
	err := r.db.QueryRowContext(ctx, query, reporterID).Scan(&count)
	return count, err
}

// GetPendingByReporterAndTarget checks for an existing pending report from this reporter against this target in the same scope
func (r *UserReportRepository) GetPendingByReporterAndTarget(ctx context.Context, reporterID, targetUserID string, regionID, schoolID, districtID *string) (*models.UserReport, error) {
	query := `
		SELECT id, reporter_id, reported_user_id, region_id, school_id, district_id,
			reason, details, status, resolved_by, resolution_note, blocklist_proposal_id,
			created_at, resolved_at
		FROM user_reports
		WHERE reporter_id = ? AND reported_user_id = ? AND status = 'pending'
	`

	args := []interface{}{reporterID, targetUserID}

	if regionID != nil {
		query += " AND region_id = ?"
		args = append(args, *regionID)
	} else if schoolID != nil {
		query += " AND school_id = ?"
		args = append(args, *schoolID)
	} else if districtID != nil {
		query += " AND district_id = ?"
		args = append(args, *districtID)
	}

	report := &models.UserReport{}
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&report.ID,
		&report.ReporterID,
		&report.ReportedUserID,
		&report.RegionID,
		&report.SchoolID,
		&report.DistrictID,
		&report.Reason,
		&report.Details,
		&report.Status,
		&report.ResolvedBy,
		&report.ResolutionNote,
		&report.BlocklistProposalID,
		&report.CreatedAt,
		&report.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return report, nil
}

// UpdateStatus updates the status of a report with resolution details
func (r *UserReportRepository) UpdateStatus(ctx context.Context, id, status string, resolvedBy *string, note *string, proposalID *string) error {
	now := time.Now().UTC()
	query := `
		UPDATE user_reports
		SET status = ?, resolved_by = ?, resolution_note = ?, blocklist_proposal_id = ?, resolved_at = ?
		WHERE id = ?
	`
	_, err := r.db.ExecContext(ctx, query, status, resolvedBy, note, proposalID, now, id)
	return err
}

// scanReportSummaries is a helper to scan rows into ReportSummary slices
func (r *UserReportRepository) scanReportSummaries(ctx context.Context, query string, args ...interface{}) ([]*models.ReportSummary, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	reports := []*models.ReportSummary{}
	for rows.Next() {
		report := &models.ReportSummary{}
		if err := rows.Scan(
			&report.ID,
			&report.ReportedUser,
			&report.ReportedUserID,
			&report.ScopeType,
			&report.ScopeName,
			&report.ScopeID,
			&report.Reason,
			&report.Status,
			&report.ReportCount,
			&report.CreatedAt,
		); err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return reports, nil
}
