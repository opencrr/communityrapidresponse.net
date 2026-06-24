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
	if channel.AccessTier == "" {
		channel.AccessTier = models.AccessTierMember
	}

	query := `
		INSERT INTO meshtastic_channels
		(id, region_id, school_id, district_id, owner_group_id, channel_name, description, access_tier, created_by, created_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		channel.ID,
		channel.RegionID,
		channel.SchoolID,
		channel.DistrictID,
		channel.OwnerGroupID,
		channel.ChannelName,
		channel.Description,
		channel.AccessTier,
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
	if channel.AccessTier == "" {
		channel.AccessTier = models.AccessTierMember
	}

	query := `
		INSERT INTO meshtastic_channels
		(id, region_id, school_id, district_id, owner_group_id, channel_name, description, access_tier, created_by, created_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := tx.ExecContext(ctx, query,
		channel.ID,
		channel.RegionID,
		channel.SchoolID,
		channel.DistrictID,
		channel.OwnerGroupID,
		channel.ChannelName,
		channel.Description,
		channel.AccessTier,
		channel.CreatedBy,
		channel.CreatedAt,
		channel.IsActive,
	)

	return err
}

// GetByID retrieves a meshtastic channel by ID
func (r *MeshtasticChannelRepository) GetByID(ctx context.Context, id string) (*models.MeshtasticChannel, error) {
	query := `
		SELECT id, region_id, school_id, district_id, owner_group_id, channel_name,
			description, access_tier, created_by, created_at, is_active
		FROM meshtastic_channels
		WHERE id = ?
	`

	channel := &models.MeshtasticChannel{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&channel.ID,
		&channel.RegionID,
		&channel.SchoolID,
		&channel.DistrictID,
		&channel.OwnerGroupID,
		&channel.ChannelName,
		&channel.Description,
		&channel.AccessTier,
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

// ListByOwnerGroup retrieves active meshtastic channels for a group
func (r *MeshtasticChannelRepository) ListByOwnerGroup(ctx context.Context, ownerGroupID string) ([]*models.MeshtasticChannel, error) {
	query := `
		SELECT mc.id, mc.region_id, mc.school_id, mc.district_id, mc.owner_group_id, mc.channel_name,
			mc.description, mc.access_tier, mc.created_by, mc.created_at, mc.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'meshtastic_channel' AND asset_id = mc.id AND status = 'pending') AS has_pending_deletion
		FROM meshtastic_channels mc
		WHERE mc.owner_group_id = ? AND mc.is_active = TRUE
		ORDER BY mc.created_at
	`

	return r.scanChannels(ctx, query, ownerGroupID)
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

// CountByOwnerGroup counts active meshtastic channels for an owner group
func (r *MeshtasticChannelRepository) CountByOwnerGroup(ctx context.Context, ownerGroupID string) (int, error) {
	query := `SELECT COUNT(*) FROM meshtastic_channels WHERE owner_group_id = ? AND is_active = TRUE`
	var count int
	err := r.db.QueryRowContext(ctx, query, ownerGroupID).Scan(&count)
	return count, err
}

// CountByOwnerGroupForUpdate counts active meshtastic channels for an owner group with a row lock
func (r *MeshtasticChannelRepository) CountByOwnerGroupForUpdate(ctx context.Context, tx *sql.Tx, ownerGroupID string) (int, error) {
	query := `SELECT COUNT(*) FROM meshtastic_channels WHERE owner_group_id = ? AND is_active = TRUE FOR UPDATE`
	var count int
	err := tx.QueryRowContext(ctx, query, ownerGroupID).Scan(&count)
	return count, err
}

// CreateForOwnerGroup creates a meshtastic channel owned by a group (not a region/school/district)
func (r *MeshtasticChannelRepository) CreateForOwnerGroup(ctx context.Context, channel *models.MeshtasticChannel) error {
	if channel.OwnerGroupID == nil || *channel.OwnerGroupID == "" {
		return errors.New("owner_group_id is required")
	}
	// Ensure the other owner fields are nil for the 4-way XOR constraint
	channel.RegionID = nil
	channel.SchoolID = nil
	channel.DistrictID = nil

	return r.Create(ctx, channel)
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
			&ch.ID, &ch.RegionID, &ch.SchoolID, &ch.DistrictID, &ch.OwnerGroupID,
			&ch.ChannelName, &ch.Description, &ch.AccessTier, &ch.CreatedBy,
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
