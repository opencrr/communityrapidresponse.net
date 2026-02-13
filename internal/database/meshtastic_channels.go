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
	ErrMeshtasticChannelNotFound = errors.New("meshtastic channel not found")
)

// MeshtasticChannelRepository handles meshtastic channel database operations
type MeshtasticChannelRepository struct {
	db *DB
}

// NewMeshtasticChannelRepository creates a new meshtastic channel repository
func NewMeshtasticChannelRepository(db *DB) *MeshtasticChannelRepository {
	return &MeshtasticChannelRepository{db: db}
}

// Create creates a new meshtastic channel
func (r *MeshtasticChannelRepository) Create(ctx context.Context, channel *models.MeshtasticChannel) error {
	channel.ID = uuid.New().String()
	channel.CreatedAt = time.Now().UTC()
	channel.IsActive = true

	query := `
		INSERT INTO meshtastic_channels
		(id, region_id, school_id, district_id, channel_name, description, created_by, created_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		channel.ID,
		channel.RegionID,
		channel.SchoolID,
		channel.DistrictID,
		channel.ChannelName,
		channel.Description,
		channel.CreatedBy,
		channel.CreatedAt,
		channel.IsActive,
	)

	return err
}

// CreateChannelTx creates a new meshtastic channel within a transaction
func (r *MeshtasticChannelRepository) CreateChannelTx(ctx context.Context, tx *sql.Tx, channel *models.MeshtasticChannel) error {
	channel.ID = uuid.New().String()
	channel.CreatedAt = time.Now().UTC()
	channel.IsActive = true

	query := `
		INSERT INTO meshtastic_channels
		(id, region_id, school_id, district_id, channel_name, description, created_by, created_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := tx.ExecContext(ctx, query,
		channel.ID,
		channel.RegionID,
		channel.SchoolID,
		channel.DistrictID,
		channel.ChannelName,
		channel.Description,
		channel.CreatedBy,
		channel.CreatedAt,
		channel.IsActive,
	)

	return err
}

// GetByID retrieves a meshtastic channel by ID
func (r *MeshtasticChannelRepository) GetByID(ctx context.Context, id string) (*models.MeshtasticChannel, error) {
	query := `
		SELECT id, region_id, school_id, district_id, channel_name,
			description, created_by, created_at, is_active
		FROM meshtastic_channels
		WHERE id = ?
	`

	channel := &models.MeshtasticChannel{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&channel.ID,
		&channel.RegionID,
		&channel.SchoolID,
		&channel.DistrictID,
		&channel.ChannelName,
		&channel.Description,
		&channel.CreatedBy,
		&channel.CreatedAt,
		&channel.IsActive,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMeshtasticChannelNotFound
	}
	if err != nil {
		return nil, err
	}

	return channel, nil
}

// ListByRegion retrieves meshtastic channels for a region
func (r *MeshtasticChannelRepository) ListByRegion(ctx context.Context, regionID string) ([]*models.MeshtasticChannel, error) {
	query := `
		SELECT mc.id, mc.region_id, mc.school_id, mc.district_id, mc.channel_name,
			mc.description, mc.created_by, mc.created_at, mc.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'meshtastic_channel' AND asset_id = mc.id AND status = 'pending') AS has_pending_deletion,
			COALESCE(gr.name, '') AS region_name
		FROM meshtastic_channels mc
		LEFT JOIN geographic_regions gr ON mc.region_id = gr.id
		WHERE mc.region_id = ? AND mc.is_active = TRUE
		ORDER BY mc.channel_name
	`

	return r.scanChannelsWithRegionName(ctx, query, regionID)
}

// ListBySchool retrieves meshtastic channels for a school
func (r *MeshtasticChannelRepository) ListBySchool(ctx context.Context, schoolID string) ([]*models.MeshtasticChannel, error) {
	query := `
		SELECT mc.id, mc.region_id, mc.school_id, mc.district_id, mc.channel_name,
			mc.description, mc.created_by, mc.created_at, mc.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'meshtastic_channel' AND asset_id = mc.id AND status = 'pending') AS has_pending_deletion
		FROM meshtastic_channels mc
		WHERE mc.school_id = ? AND mc.is_active = TRUE
		ORDER BY mc.channel_name
	`

	return r.scanChannels(ctx, query, schoolID)
}

// ListByDistrict retrieves meshtastic channels for a district
func (r *MeshtasticChannelRepository) ListByDistrict(ctx context.Context, districtID string) ([]*models.MeshtasticChannel, error) {
	query := `
		SELECT mc.id, mc.region_id, mc.school_id, mc.district_id, mc.channel_name,
			mc.description, mc.created_by, mc.created_at, mc.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'meshtastic_channel' AND asset_id = mc.id AND status = 'pending') AS has_pending_deletion
		FROM meshtastic_channels mc
		WHERE mc.district_id = ? AND mc.is_active = TRUE
		ORDER BY mc.channel_name
	`

	return r.scanChannels(ctx, query, districtID)
}

// ListByUser retrieves all meshtastic channels for regions, schools, and districts the user has access to
func (r *MeshtasticChannelRepository) ListByUser(ctx context.Context, userID string) ([]*models.MeshtasticChannel, error) {
	query := `
		SELECT mc.id, mc.region_id, mc.school_id, mc.district_id, mc.channel_name,
			mc.description, mc.created_by, mc.created_at, mc.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'meshtastic_channel' AND asset_id = mc.id AND status = 'pending') AS has_pending_deletion,
			COALESCE(gr.name, '') AS region_name,
			'' AS school_name,
			'' AS district_name
		FROM meshtastic_channels mc
		INNER JOIN user_regions ur ON mc.region_id = ur.region_id
		LEFT JOIN geographic_regions gr ON mc.region_id = gr.id
		WHERE ur.user_id = ? AND mc.is_active = TRUE

		UNION

		SELECT mc.id, mc.region_id, mc.school_id, mc.district_id, mc.channel_name,
			mc.description, mc.created_by, mc.created_at, mc.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'meshtastic_channel' AND asset_id = mc.id AND status = 'pending') AS has_pending_deletion,
			'' AS region_name,
			COALESCE(s.name, '') AS school_name,
			'' AS district_name
		FROM meshtastic_channels mc
		INNER JOIN user_schools us ON mc.school_id = us.school_id
		LEFT JOIN schools s ON mc.school_id = s.id
		WHERE us.user_id = ? AND us.verification_status = 'verified' AND mc.is_active = TRUE

		UNION

		SELECT mc.id, mc.region_id, mc.school_id, mc.district_id, mc.channel_name,
			mc.description, mc.created_by, mc.created_at, mc.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'meshtastic_channel' AND asset_id = mc.id AND status = 'pending') AS has_pending_deletion,
			'' AS region_name,
			'' AS school_name,
			COALESCE(sd.name, '') AS district_name
		FROM meshtastic_channels mc
		INNER JOIN school_districts sd ON mc.district_id = sd.id
		INNER JOIN schools s ON s.district_id = sd.id
		INNER JOIN user_schools us ON s.id = us.school_id
		WHERE us.user_id = ? AND us.verification_status = 'verified' AND mc.is_active = TRUE

		ORDER BY channel_name
	`

	rows, err := r.db.QueryContext(ctx, query, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var channels []*models.MeshtasticChannel
	for rows.Next() {
		ch := &models.MeshtasticChannel{}
		if err := rows.Scan(
			&ch.ID, &ch.RegionID, &ch.SchoolID, &ch.DistrictID,
			&ch.ChannelName, &ch.Description, &ch.CreatedBy,
			&ch.CreatedAt, &ch.IsActive, &ch.HasPendingDeletion,
			&ch.RegionName, &ch.SchoolName, &ch.DistrictName,
		); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return channels, nil
}

// ListByAdminUser retrieves all meshtastic channels for regions, schools, and districts where the user is an admin
func (r *MeshtasticChannelRepository) ListByAdminUser(ctx context.Context, userID string) ([]*models.MeshtasticChannel, error) {
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
		SELECT DISTINCT mc.id, mc.region_id, mc.school_id, mc.district_id,
			gr.name AS region_name, '' AS school_name, '' AS district_name,
			mc.channel_name, mc.description,
			mc.created_by, mc.created_at, mc.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'meshtastic_channel' AND asset_id = mc.id AND status = 'pending') AS has_pending_deletion
		FROM meshtastic_channels mc
		INNER JOIN user_admin_regions uar ON mc.region_id = uar.id
		INNER JOIN geographic_regions gr ON mc.region_id = gr.id
		WHERE mc.is_active = TRUE

		UNION

		SELECT DISTINCT mc.id, mc.region_id, mc.school_id, mc.district_id,
			'' AS region_name, s.name AS school_name, '' AS district_name,
			mc.channel_name, mc.description,
			mc.created_by, mc.created_at, mc.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'meshtastic_channel' AND asset_id = mc.id AND status = 'pending') AS has_pending_deletion
		FROM meshtastic_channels mc
		INNER JOIN schools s ON mc.school_id = s.id
		INNER JOIN user_schools us ON s.id = us.school_id
		WHERE us.user_id = ? AND us.is_admin = TRUE AND us.verification_status = 'verified' AND mc.is_active = TRUE

		UNION

		SELECT DISTINCT mc.id, mc.region_id, mc.school_id, mc.district_id,
			'' AS region_name, '' AS school_name, sd.name AS district_name,
			mc.channel_name, mc.description,
			mc.created_by, mc.created_at, mc.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'meshtastic_channel' AND asset_id = mc.id AND status = 'pending') AS has_pending_deletion
		FROM meshtastic_channels mc
		INNER JOIN school_districts sd ON mc.district_id = sd.id
		INNER JOIN schools s ON s.district_id = sd.id
		INNER JOIN user_schools us ON s.id = us.school_id
		WHERE us.user_id = ? AND us.is_admin = TRUE AND us.verification_status = 'verified' AND mc.is_active = TRUE

		ORDER BY region_name, school_name, district_name, channel_name
	`

	rows, err := r.db.QueryContext(ctx, query, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var channels []*models.MeshtasticChannel
	for rows.Next() {
		ch := &models.MeshtasticChannel{}
		if err := rows.Scan(
			&ch.ID, &ch.RegionID, &ch.SchoolID, &ch.DistrictID,
			&ch.RegionName, &ch.SchoolName, &ch.DistrictName,
			&ch.ChannelName, &ch.Description,
			&ch.CreatedBy, &ch.CreatedAt, &ch.IsActive,
			&ch.HasPendingDeletion,
		); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return channels, nil
}

// Update updates a meshtastic channel's name and/or description
func (r *MeshtasticChannelRepository) Update(ctx context.Context, id string, name, description *string) error {
	query := `UPDATE meshtastic_channels SET `
	args := []interface{}{}
	updates := []string{}

	if name != nil {
		updates = append(updates, "channel_name = ?")
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

// Deactivate marks a meshtastic channel as inactive (soft delete)
func (r *MeshtasticChannelRepository) Deactivate(ctx context.Context, id string) error {
	query := `UPDATE meshtastic_channels SET is_active = FALSE WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// CountByRegion counts meshtastic channels for a region
func (r *MeshtasticChannelRepository) CountByRegion(ctx context.Context, regionID string) (int, error) {
	query := `SELECT COUNT(*) FROM meshtastic_channels WHERE region_id = ? AND is_active = TRUE`
	var count int
	err := r.db.QueryRowContext(ctx, query, regionID).Scan(&count)
	return count, err
}

// CountByRegionForUpdate counts meshtastic channels for a region with a row lock
func (r *MeshtasticChannelRepository) CountByRegionForUpdate(ctx context.Context, tx *sql.Tx, regionID string) (int, error) {
	query := `SELECT COUNT(*) FROM meshtastic_channels WHERE region_id = ? AND is_active = TRUE FOR UPDATE`
	var count int
	err := tx.QueryRowContext(ctx, query, regionID).Scan(&count)
	return count, err
}

// CountBySchool counts meshtastic channels for a school
func (r *MeshtasticChannelRepository) CountBySchool(ctx context.Context, schoolID string) (int, error) {
	query := `SELECT COUNT(*) FROM meshtastic_channels WHERE school_id = ? AND is_active = TRUE`
	var count int
	err := r.db.QueryRowContext(ctx, query, schoolID).Scan(&count)
	return count, err
}

// CountBySchoolForUpdate counts meshtastic channels for a school with a row lock
func (r *MeshtasticChannelRepository) CountBySchoolForUpdate(ctx context.Context, tx *sql.Tx, schoolID string) (int, error) {
	query := `SELECT COUNT(*) FROM meshtastic_channels WHERE school_id = ? AND is_active = TRUE FOR UPDATE`
	var count int
	err := tx.QueryRowContext(ctx, query, schoolID).Scan(&count)
	return count, err
}

// CountByDistrict counts meshtastic channels for a district
func (r *MeshtasticChannelRepository) CountByDistrict(ctx context.Context, districtID string) (int, error) {
	query := `SELECT COUNT(*) FROM meshtastic_channels WHERE district_id = ? AND is_active = TRUE`
	var count int
	err := r.db.QueryRowContext(ctx, query, districtID).Scan(&count)
	return count, err
}

// CountByDistrictForUpdate counts meshtastic channels for a district with a row lock
func (r *MeshtasticChannelRepository) CountByDistrictForUpdate(ctx context.Context, tx *sql.Tx, districtID string) (int, error) {
	query := `SELECT COUNT(*) FROM meshtastic_channels WHERE district_id = ? AND is_active = TRUE FOR UPDATE`
	var count int
	err := tx.QueryRowContext(ctx, query, districtID).Scan(&count)
	return count, err
}

// scanChannels scans basic channel rows (without name joins)
func (r *MeshtasticChannelRepository) scanChannels(ctx context.Context, query string, args ...interface{}) ([]*models.MeshtasticChannel, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var channels []*models.MeshtasticChannel
	for rows.Next() {
		ch := &models.MeshtasticChannel{}
		if err := rows.Scan(
			&ch.ID, &ch.RegionID, &ch.SchoolID, &ch.DistrictID,
			&ch.ChannelName, &ch.Description, &ch.CreatedBy,
			&ch.CreatedAt, &ch.IsActive, &ch.HasPendingDeletion,
		); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return channels, nil
}

// scanChannelsWithRegionName scans channel rows including region name join
func (r *MeshtasticChannelRepository) scanChannelsWithRegionName(ctx context.Context, query string, args ...interface{}) ([]*models.MeshtasticChannel, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var channels []*models.MeshtasticChannel
	for rows.Next() {
		ch := &models.MeshtasticChannel{}
		if err := rows.Scan(
			&ch.ID, &ch.RegionID, &ch.SchoolID, &ch.DistrictID,
			&ch.ChannelName, &ch.Description, &ch.CreatedBy,
			&ch.CreatedAt, &ch.IsActive, &ch.HasPendingDeletion,
			&ch.RegionName,
		); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return channels, nil
}
