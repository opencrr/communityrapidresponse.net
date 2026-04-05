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
	ErrSignalGroupNotFound = errors.New("signal group not found")
)

// SignalGroupRepository handles signal group database operations
type SignalGroupRepository struct {
	db *DB
}

// NewSignalGroupRepository creates a new signal group repository
func NewSignalGroupRepository(db *DB) *SignalGroupRepository {
	return &SignalGroupRepository{db: db}
}

// Create creates a new signal group
func (r *SignalGroupRepository) Create(ctx context.Context, group *models.SignalGroup) error {
	group.ID = uuid.New().String()
	group.CreatedAt = time.Now().UTC()
	group.IsActive = true
	if group.AccessTier == "" {
		group.AccessTier = models.AccessTierMember
	}

	query := `
		INSERT INTO signal_groups
		(id, region_id, school_id, district_id, group_name, description, access_tier, created_by, created_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		group.ID,
		group.RegionID,
		group.SchoolID,
		group.DistrictID,
		group.GroupName,
		group.Description,
		group.AccessTier,
		group.CreatedBy,
		group.CreatedAt,
		group.IsActive,
	)

	return err
}

// GetByID retrieves a signal group by ID
func (r *SignalGroupRepository) GetByID(ctx context.Context, id string) (*models.SignalGroup, error) {
	query := `
		SELECT id, region_id, school_id, district_id, group_name,
			description, access_tier, created_by, created_at, is_active
		FROM signal_groups
		WHERE id = ?
	`

	group := &models.SignalGroup{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&group.ID,
		&group.RegionID,
		&group.SchoolID,
		&group.DistrictID,
		&group.GroupName,
		&group.Description,
		&group.AccessTier,
		&group.CreatedBy,
		&group.CreatedAt,
		&group.IsActive,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSignalGroupNotFound
	}
	if err != nil {
		return nil, err
	}

	return group, nil
}

// ListByRegion retrieves signal groups for a region
func (r *SignalGroupRepository) ListByRegion(ctx context.Context, regionID string) ([]*models.SignalGroup, error) {
	query := `
		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.group_name,
			sg.description, sg.access_tier, sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion,
			COALESCE(gr.name, '') AS region_name
		FROM signal_groups sg
		LEFT JOIN geographic_regions gr ON sg.region_id = gr.id
		WHERE sg.region_id = ? AND sg.is_active = TRUE
		ORDER BY sg.group_name
	`

	rows, err := r.db.QueryContext(ctx, query, regionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var groups []*models.SignalGroup
	for rows.Next() {
		group := &models.SignalGroup{}
		if err := rows.Scan(
			&group.ID,
			&group.RegionID,
			&group.SchoolID,
			&group.DistrictID,
			&group.GroupName,
			&group.Description,
			&group.AccessTier,
			&group.CreatedBy,
			&group.CreatedAt,
			&group.IsActive,
			&group.HasPendingDeletion,
			&group.RegionName,
		); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return groups, nil
}

// Update updates a signal group's name and/or description
func (r *SignalGroupRepository) Update(ctx context.Context, id string, name, description *string) error {
	query := `UPDATE signal_groups SET `
	args := []interface{}{}
	updates := []string{}

	if name != nil {
		updates = append(updates, "group_name = ?")
		args = append(args, *name)
	}
	if description != nil {
		updates = append(updates, "description = ?")
		args = append(args, *description)
	}

	if len(updates) == 0 {
		return nil
	}

	for i, u := range updates {
		if i > 0 {
			query += ", "
		}
		query += u
	}
	query += " WHERE id = ?"
	args = append(args, id)

	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

// Deactivate marks a signal group as inactive
func (r *SignalGroupRepository) Deactivate(ctx context.Context, id string) error {
	query := `UPDATE signal_groups SET is_active = FALSE WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// CountByRegion counts signal groups for a region
func (r *SignalGroupRepository) CountByRegion(ctx context.Context, regionID string) (int, error) {
	query := `SELECT COUNT(*) FROM signal_groups WHERE region_id = ? AND is_active = TRUE`
	var count int
	err := r.db.QueryRowContext(ctx, query, regionID).Scan(&count)
	return count, err
}

// ListByUser retrieves all signal groups for regions, schools, and districts the user has access to
func (r *SignalGroupRepository) ListByUser(ctx context.Context, userID string) ([]*models.SignalGroup, error) {
	query := `
		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.group_name,
			sg.description, sg.access_tier, sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion,
			COALESCE(gr.name, '') AS region_name,
			'' AS school_name,
			'' AS district_name
		FROM signal_groups sg
		INNER JOIN user_regions ur ON sg.region_id = ur.region_id
		LEFT JOIN geographic_regions gr ON sg.region_id = gr.id
		WHERE ur.user_id = ? AND sg.is_active = TRUE

		UNION

		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.group_name,
			sg.description, sg.access_tier, sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion,
			'' AS region_name,
			COALESCE(s.name, '') AS school_name,
			'' AS district_name
		FROM signal_groups sg
		INNER JOIN user_schools us ON sg.school_id = us.school_id
		LEFT JOIN schools s ON sg.school_id = s.id
		WHERE us.user_id = ? AND us.verification_status = 'verified' AND sg.is_active = TRUE

		UNION

		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.group_name,
			sg.description, sg.access_tier, sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion,
			'' AS region_name,
			'' AS school_name,
			COALESCE(sd.name, '') AS district_name
		FROM signal_groups sg
		INNER JOIN school_districts sd ON sg.district_id = sd.id
		INNER JOIN schools s ON s.district_id = sd.id
		INNER JOIN user_schools us ON s.id = us.school_id
		WHERE us.user_id = ? AND us.verification_status = 'verified' AND sg.is_active = TRUE

		ORDER BY group_name
	`

	rows, err := r.db.QueryContext(ctx, query, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var groups []*models.SignalGroup
	for rows.Next() {
		group := &models.SignalGroup{}
		if err := rows.Scan(
			&group.ID,
			&group.RegionID,
			&group.SchoolID,
			&group.DistrictID,
			&group.GroupName,
			&group.Description,
			&group.AccessTier,
			&group.CreatedBy,
			&group.CreatedAt,
			&group.IsActive,
			&group.HasPendingDeletion,
			&group.RegionName,
			&group.SchoolName,
			&group.DistrictName,
		); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return groups, nil
}

// ListByAdminUser retrieves all signal groups for regions, schools, and districts where the user is an admin
// Admin rights propagate up the hierarchy: if user is admin of a child region,
// they can also see signal groups in parent regions
func (r *SignalGroupRepository) ListByAdminUser(ctx context.Context, userID string) ([]*models.SignalGroup, error) {
	query := `
		WITH RECURSIVE user_admin_regions AS (
			-- Base case: regions the user is directly an admin of
			SELECT gr.id, gr.parent_region_id
			FROM geographic_regions gr
			INNER JOIN user_regions ur ON gr.id = ur.region_id
			WHERE ur.user_id = ? AND ur.is_admin = TRUE

			UNION

			-- Recursive case: parent regions (admin rights propagate up)
			SELECT gr.id, gr.parent_region_id
			FROM geographic_regions gr
			INNER JOIN user_admin_regions uar ON gr.id = uar.parent_region_id
		)
		SELECT DISTINCT sg.id, sg.region_id, sg.school_id, sg.district_id,
			gr.name AS region_name, '' AS school_name, '' AS district_name,
			sg.group_name, sg.description, sg.access_tier,
			sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion
		FROM signal_groups sg
		INNER JOIN user_admin_regions uar ON sg.region_id = uar.id
		INNER JOIN geographic_regions gr ON sg.region_id = gr.id
		WHERE sg.is_active = TRUE

		UNION

		SELECT DISTINCT sg.id, sg.region_id, sg.school_id, sg.district_id,
			'' AS region_name, s.name AS school_name, '' AS district_name,
			sg.group_name, sg.description, sg.access_tier,
			sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion
		FROM signal_groups sg
		INNER JOIN schools s ON sg.school_id = s.id
		INNER JOIN user_schools us ON s.id = us.school_id
		WHERE us.user_id = ? AND us.is_admin = TRUE AND us.verification_status = 'verified' AND sg.is_active = TRUE

		UNION

		SELECT DISTINCT sg.id, sg.region_id, sg.school_id, sg.district_id,
			'' AS region_name, '' AS school_name, sd.name AS district_name,
			sg.group_name, sg.description, sg.access_tier,
			sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion
		FROM signal_groups sg
		INNER JOIN school_districts sd ON sg.district_id = sd.id
		INNER JOIN schools s ON s.district_id = sd.id
		INNER JOIN user_schools us ON s.id = us.school_id
		WHERE us.user_id = ? AND us.is_admin = TRUE AND us.verification_status = 'verified' AND sg.is_active = TRUE

		ORDER BY region_name, school_name, district_name, group_name
	`

	rows, err := r.db.QueryContext(ctx, query, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var groups []*models.SignalGroup
	for rows.Next() {
		group := &models.SignalGroup{}
		if err := rows.Scan(
			&group.ID,
			&group.RegionID,
			&group.SchoolID,
			&group.DistrictID,
			&group.RegionName,
			&group.SchoolName,
			&group.DistrictName,
			&group.GroupName,
			&group.Description,
			&group.AccessTier,
			&group.CreatedBy,
			&group.CreatedAt,
			&group.IsActive,
			&group.HasPendingDeletion,
		); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return groups, nil
}

// CreateGroupTx creates a new signal group within a transaction
func (r *SignalGroupRepository) CreateGroupTx(ctx context.Context, tx *sql.Tx, group *models.SignalGroup) error {
	group.ID = uuid.New().String()
	group.CreatedAt = time.Now().UTC()
	group.IsActive = true
	if group.AccessTier == "" {
		group.AccessTier = models.AccessTierMember
	}

	query := `
		INSERT INTO signal_groups
		(id, region_id, school_id, district_id, group_name, description, access_tier, created_by, created_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := tx.ExecContext(ctx, query,
		group.ID,
		group.RegionID,
		group.SchoolID,
		group.DistrictID,
		group.GroupName,
		group.Description,
		group.AccessTier,
		group.CreatedBy,
		group.CreatedAt,
		group.IsActive,
	)

	return err
}

// CountByRegionForUpdate counts signal groups for a region with a row lock
func (r *SignalGroupRepository) CountByRegionForUpdate(ctx context.Context, tx *sql.Tx, regionID string) (int, error) {
	query := `SELECT COUNT(*) FROM signal_groups WHERE region_id = ? AND is_active = TRUE FOR UPDATE`
	var count int
	err := tx.QueryRowContext(ctx, query, regionID).Scan(&count)
	return count, err
}

// ListBySchool retrieves signal groups for a school
func (r *SignalGroupRepository) ListBySchool(ctx context.Context, schoolID string) ([]*models.SignalGroup, error) {
	query := `
		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.group_name,
			sg.description, sg.access_tier, sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion
		FROM signal_groups sg
		WHERE sg.school_id = ? AND sg.is_active = TRUE
		ORDER BY sg.group_name
	`

	rows, err := r.db.QueryContext(ctx, query, schoolID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var groups []*models.SignalGroup
	for rows.Next() {
		group := &models.SignalGroup{}
		if err := rows.Scan(
			&group.ID,
			&group.RegionID,
			&group.SchoolID,
			&group.DistrictID,
			&group.GroupName,
			&group.Description,
			&group.AccessTier,
			&group.CreatedBy,
			&group.CreatedAt,
			&group.IsActive,
			&group.HasPendingDeletion,
		); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return groups, nil
}

// ListByDistrict retrieves signal groups for a district
func (r *SignalGroupRepository) ListByDistrict(ctx context.Context, districtID string) ([]*models.SignalGroup, error) {
	query := `
		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.group_name,
			sg.description, sg.access_tier, sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion
		FROM signal_groups sg
		WHERE sg.district_id = ? AND sg.is_active = TRUE
		ORDER BY sg.group_name
	`

	rows, err := r.db.QueryContext(ctx, query, districtID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var groups []*models.SignalGroup
	for rows.Next() {
		group := &models.SignalGroup{}
		if err := rows.Scan(
			&group.ID,
			&group.RegionID,
			&group.SchoolID,
			&group.DistrictID,
			&group.GroupName,
			&group.Description,
			&group.AccessTier,
			&group.CreatedBy,
			&group.CreatedAt,
			&group.IsActive,
			&group.HasPendingDeletion,
		); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return groups, nil
}

// CountBySchool counts signal groups for a school
func (r *SignalGroupRepository) CountBySchool(ctx context.Context, schoolID string) (int, error) {
	query := `SELECT COUNT(*) FROM signal_groups WHERE school_id = ? AND is_active = TRUE`
	var count int
	err := r.db.QueryRowContext(ctx, query, schoolID).Scan(&count)
	return count, err
}

// CountByDistrict counts signal groups for a district
func (r *SignalGroupRepository) CountByDistrict(ctx context.Context, districtID string) (int, error) {
	query := `SELECT COUNT(*) FROM signal_groups WHERE district_id = ? AND is_active = TRUE`
	var count int
	err := r.db.QueryRowContext(ctx, query, districtID).Scan(&count)
	return count, err
}

// CountBySchoolForUpdate counts signal groups for a school with a row lock
func (r *SignalGroupRepository) CountBySchoolForUpdate(ctx context.Context, tx *sql.Tx, schoolID string) (int, error) {
	query := `SELECT COUNT(*) FROM signal_groups WHERE school_id = ? AND is_active = TRUE FOR UPDATE`
	var count int
	err := tx.QueryRowContext(ctx, query, schoolID).Scan(&count)
	return count, err
}

// CountByDistrictForUpdate counts signal groups for a district with a row lock
func (r *SignalGroupRepository) CountByDistrictForUpdate(ctx context.Context, tx *sql.Tx, districtID string) (int, error) {
	query := `SELECT COUNT(*) FROM signal_groups WHERE district_id = ? AND is_active = TRUE FOR UPDATE`
	var count int
	err := tx.QueryRowContext(ctx, query, districtID).Scan(&count)
	return count, err
}
