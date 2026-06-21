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
		(id, region_id, school_id, district_id, owner_group_id, connection_id, group_name, description, access_tier, created_by, created_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		group.ID,
		group.RegionID,
		group.SchoolID,
		group.DistrictID,
		group.OwnerGroupID,
		group.ConnectionID,
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
		SELECT id, region_id, school_id, district_id, owner_group_id, connection_id, group_name,
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
		&group.OwnerGroupID,
		&group.ConnectionID,
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
		(id, region_id, school_id, district_id, owner_group_id, connection_id, group_name, description, access_tier, created_by, created_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := tx.ExecContext(ctx, query,
		group.ID,
		group.RegionID,
		group.SchoolID,
		group.DistrictID,
		group.OwnerGroupID,
		group.ConnectionID,
		group.GroupName,
		group.Description,
		group.AccessTier,
		group.CreatedBy,
		group.CreatedAt,
		group.IsActive,
	)

	return err
}

// ListBySchool retrieves signal groups for a school
func (r *SignalGroupRepository) ListBySchool(ctx context.Context, schoolID string) ([]*models.SignalGroup, error) {
	query := `
		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.owner_group_id, sg.connection_id, sg.group_name,
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
			&group.OwnerGroupID,
			&group.ConnectionID,
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
		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.owner_group_id, sg.connection_id, sg.group_name,
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
			&group.OwnerGroupID,
			&group.ConnectionID,
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

// ListByOwnerGroup retrieves active signal groups owned by the given group
func (r *SignalGroupRepository) ListByOwnerGroup(ctx context.Context, ownerGroupID string) ([]*models.SignalGroup, error) {
	query := `
		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.owner_group_id, sg.connection_id, sg.group_name,
			sg.description, sg.access_tier, sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion
		FROM signal_groups sg
		WHERE sg.owner_group_id = ? AND sg.is_active = TRUE
		ORDER BY sg.created_at
	`

	rows, err := r.db.QueryContext(ctx, query, ownerGroupID)
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
			&group.OwnerGroupID,
			&group.ConnectionID,
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

// CountByOwnerGroup counts active signal groups for an owner group
func (r *SignalGroupRepository) CountByOwnerGroup(ctx context.Context, ownerGroupID string) (int, error) {
	query := `SELECT COUNT(*) FROM signal_groups WHERE owner_group_id = ? AND is_active = TRUE`
	var count int
	err := r.db.QueryRowContext(ctx, query, ownerGroupID).Scan(&count)
	return count, err
}

// CountByOwnerGroupForUpdate counts active signal groups for an owner group with a row lock
func (r *SignalGroupRepository) CountByOwnerGroupForUpdate(ctx context.Context, tx *sql.Tx, ownerGroupID string) (int, error) {
	query := `SELECT COUNT(*) FROM signal_groups WHERE owner_group_id = ? AND is_active = TRUE FOR UPDATE`
	var count int
	err := tx.QueryRowContext(ctx, query, ownerGroupID).Scan(&count)
	return count, err
}

// CountByConnection counts active signal groups for a connection
func (r *SignalGroupRepository) CountByConnection(ctx context.Context, connectionID string) (int, error) {
	query := `SELECT COUNT(*) FROM signal_groups WHERE connection_id = ? AND is_active = TRUE`
	var count int
	err := r.db.QueryRowContext(ctx, query, connectionID).Scan(&count)
	return count, err
}

// CountByConnectionForUpdate counts active signal groups for a connection with a row lock
func (r *SignalGroupRepository) CountByConnectionForUpdate(ctx context.Context, tx *sql.Tx, connectionID string) (int, error) {
	query := `SELECT COUNT(*) FROM signal_groups WHERE connection_id = ? AND is_active = TRUE FOR UPDATE`
	var count int
	err := tx.QueryRowContext(ctx, query, connectionID).Scan(&count)
	return count, err
}

// ListByConnection retrieves active signal groups for a connection
func (r *SignalGroupRepository) ListByConnection(ctx context.Context, connectionID string) ([]*models.SignalGroup, error) {
	query := `
		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.owner_group_id, sg.connection_id, sg.group_name,
			sg.description, sg.access_tier, sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion
		FROM signal_groups sg
		WHERE sg.connection_id = ? AND sg.is_active = TRUE
		ORDER BY sg.created_at
	`

	rows, err := r.db.QueryContext(ctx, query, connectionID)
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
			&group.OwnerGroupID,
			&group.ConnectionID,
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

// CreateForOwnerGroup creates a signal group owned by a group (not a region/school/district).
//
// ENCRYPTION LIMITATION: group-owned chats are metadata-only today. There is no
// recipient public-key enumeration for the group scope (see the GetPublicKeys
// handler), so wrapped DEKs are not provisioned and there is no revocation path on
// membership/tier change. Wiring encryption here is a tracked follow-up and must
// land together with revocation.
func (r *SignalGroupRepository) CreateForOwnerGroup(ctx context.Context, group *models.SignalGroup) error {
	if group.OwnerGroupID == nil || *group.OwnerGroupID == "" {
		return errors.New("owner_group_id is required")
	}
	// Ensure the other owner fields are nil for the 5-way XOR constraint
	group.RegionID = nil
	group.SchoolID = nil
	group.DistrictID = nil
	group.ConnectionID = nil

	return r.Create(ctx, group)
}
