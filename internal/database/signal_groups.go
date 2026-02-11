package database

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"

	gosentry "github.com/getsentry/sentry-go"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
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
	group.InviteLinkUpdatedAt = group.CreatedAt
	group.IsActive = true

	query := `
		INSERT INTO signal_groups
		(id, region_id, school_id, district_id, group_name, invite_link, invite_link_updated_at,
		 invite_link_updated_by, description, created_by, created_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		group.ID,
		group.RegionID,
		group.SchoolID,
		group.DistrictID,
		group.GroupName,
		group.InviteLink,
		group.InviteLinkUpdatedAt,
		group.InviteLinkUpdatedBy,
		group.Description,
		group.CreatedBy,
		group.CreatedAt,
		group.IsActive,
	)

	return err
}

// GetByID retrieves a signal group by ID
func (r *SignalGroupRepository) GetByID(ctx context.Context, id string) (*models.SignalGroup, error) {
	query := `
		SELECT id, region_id, school_id, district_id, group_name, invite_link,
			invite_link_updated_at, invite_link_updated_by,
			description, created_by, created_at, is_active
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
		&group.InviteLink,
		&group.InviteLinkUpdatedAt,
		&group.InviteLinkUpdatedBy,
		&group.Description,
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
		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.group_name, sg.invite_link,
			sg.invite_link_updated_at, sg.invite_link_updated_by,
			sg.description, sg.created_by, sg.created_at, sg.is_active,
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
			&group.InviteLink,
			&group.InviteLinkUpdatedAt,
			&group.InviteLinkUpdatedBy,
			&group.Description,
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

// Update updates a signal group (without invite link - that requires consensus)
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

// UpdateInviteLink updates the invite link (called after consensus approval)
func (r *SignalGroupRepository) UpdateInviteLink(ctx context.Context, id, inviteLink, updatedBy string) error {
	query := `
		UPDATE signal_groups
		SET invite_link = ?, invite_link_updated_at = ?, invite_link_updated_by = ?
		WHERE id = ?
	`
	_, err := r.db.ExecContext(ctx, query, inviteLink, time.Now().UTC(), updatedBy, id)
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
		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.group_name, sg.invite_link,
			sg.invite_link_updated_at, sg.invite_link_updated_by, sg.description,
			sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion,
			COALESCE(gr.name, '') AS region_name,
			'' AS school_name,
			'' AS district_name
		FROM signal_groups sg
		INNER JOIN user_regions ur ON sg.region_id = ur.region_id
		LEFT JOIN geographic_regions gr ON sg.region_id = gr.id
		WHERE ur.user_id = ? AND sg.is_active = TRUE

		UNION

		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.group_name, sg.invite_link,
			sg.invite_link_updated_at, sg.invite_link_updated_by, sg.description,
			sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion,
			'' AS region_name,
			COALESCE(s.name, '') AS school_name,
			'' AS district_name
		FROM signal_groups sg
		INNER JOIN user_schools us ON sg.school_id = us.school_id
		LEFT JOIN schools s ON sg.school_id = s.id
		WHERE us.user_id = ? AND us.verification_status = 'verified' AND sg.is_active = TRUE

		UNION

		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.group_name, sg.invite_link,
			sg.invite_link_updated_at, sg.invite_link_updated_by, sg.description,
			sg.created_by, sg.created_at, sg.is_active,
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
			&group.InviteLink,
			&group.InviteLinkUpdatedAt,
			&group.InviteLinkUpdatedBy,
			&group.Description,
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
			sg.group_name, sg.invite_link,
			sg.invite_link_updated_at, sg.invite_link_updated_by, sg.description,
			sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion
		FROM signal_groups sg
		INNER JOIN user_admin_regions uar ON sg.region_id = uar.id
		INNER JOIN geographic_regions gr ON sg.region_id = gr.id
		WHERE sg.is_active = TRUE

		UNION

		SELECT DISTINCT sg.id, sg.region_id, sg.school_id, sg.district_id,
			'' AS region_name, s.name AS school_name, '' AS district_name,
			sg.group_name, sg.invite_link,
			sg.invite_link_updated_at, sg.invite_link_updated_by, sg.description,
			sg.created_by, sg.created_at, sg.is_active,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion
		FROM signal_groups sg
		INNER JOIN schools s ON sg.school_id = s.id
		INNER JOIN user_schools us ON s.id = us.school_id
		WHERE us.user_id = ? AND us.is_admin = TRUE AND us.verification_status = 'verified' AND sg.is_active = TRUE

		UNION

		SELECT DISTINCT sg.id, sg.region_id, sg.school_id, sg.district_id,
			'' AS region_name, '' AS school_name, sd.name AS district_name,
			sg.group_name, sg.invite_link,
			sg.invite_link_updated_at, sg.invite_link_updated_by, sg.description,
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
			&group.InviteLink,
			&group.InviteLinkUpdatedAt,
			&group.InviteLinkUpdatedBy,
			&group.Description,
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
	group.InviteLinkUpdatedAt = group.CreatedAt
	group.IsActive = true

	query := `
		INSERT INTO signal_groups
		(id, region_id, school_id, district_id, group_name, invite_link, invite_link_updated_at,
		 invite_link_updated_by, description, created_by, created_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := tx.ExecContext(ctx, query,
		group.ID,
		group.RegionID,
		group.SchoolID,
		group.DistrictID,
		group.GroupName,
		group.InviteLink,
		group.InviteLinkUpdatedAt,
		group.InviteLinkUpdatedBy,
		group.Description,
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

// UpdateInviteLinkTx updates the invite link within a transaction
func (r *SignalGroupRepository) UpdateInviteLinkTx(ctx context.Context, tx *sql.Tx, id, inviteLink, updatedBy string) error {
	query := `
		UPDATE signal_groups
		SET invite_link = ?, invite_link_updated_at = ?, invite_link_updated_by = ?
		WHERE id = ?
	`
	_, err := tx.ExecContext(ctx, query, inviteLink, time.Now().UTC(), updatedBy, id)
	return err
}

// ListBySchool retrieves signal groups for a school
func (r *SignalGroupRepository) ListBySchool(ctx context.Context, schoolID string) ([]*models.SignalGroup, error) {
	query := `
		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.group_name, sg.invite_link,
			sg.invite_link_updated_at, sg.invite_link_updated_by,
			sg.description, sg.created_by, sg.created_at, sg.is_active,
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
			&group.InviteLink,
			&group.InviteLinkUpdatedAt,
			&group.InviteLinkUpdatedBy,
			&group.Description,
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
		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.group_name, sg.invite_link,
			sg.invite_link_updated_at, sg.invite_link_updated_by,
			sg.description, sg.created_by, sg.created_at, sg.is_active,
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
			&group.InviteLink,
			&group.InviteLinkUpdatedAt,
			&group.InviteLinkUpdatedBy,
			&group.Description,
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

// InviteLinkProposalRepository handles invite link proposal operations
type InviteLinkProposalRepository struct {
	db *DB
}

// NewInviteLinkProposalRepository creates a new invite link proposal repository
func NewInviteLinkProposalRepository(db *DB) *InviteLinkProposalRepository {
	return &InviteLinkProposalRepository{db: db}
}

// Create creates a new invite link update proposal
func (r *InviteLinkProposalRepository) Create(ctx context.Context, proposal *models.InviteLinkUpdateProposal) error {
	proposal.ID = uuid.New().String()
	proposal.CreatedAt = time.Now().UTC()
	proposal.ExpiresAt = proposal.CreatedAt.Add(7 * 24 * time.Hour) // 7 days
	proposal.Status = models.ProposalStatusPending

	query := `
		INSERT INTO invite_link_update_proposals
		(id, signal_group_id, region_id, proposed_by, new_invite_link, reason, status, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		proposal.ID,
		proposal.SignalGroupID,
		proposal.RegionID,
		proposal.ProposedBy,
		proposal.NewInviteLink,
		proposal.Reason,
		proposal.Status,
		proposal.CreatedAt,
		proposal.ExpiresAt,
	)

	return err
}

// GetByID retrieves a proposal by ID
func (r *InviteLinkProposalRepository) GetByID(ctx context.Context, id string) (*models.InviteLinkUpdateProposal, error) {
	query := `
		SELECT id, signal_group_id, region_id, proposed_by, new_invite_link, reason,
			status, created_at, expires_at, resolved_at
		FROM invite_link_update_proposals
		WHERE id = ?
	`

	proposal := &models.InviteLinkUpdateProposal{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&proposal.ID,
		&proposal.SignalGroupID,
		&proposal.RegionID,
		&proposal.ProposedBy,
		&proposal.NewInviteLink,
		&proposal.Reason,
		&proposal.Status,
		&proposal.CreatedAt,
		&proposal.ExpiresAt,
		&proposal.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("proposal not found")
	}
	if err != nil {
		return nil, err
	}

	return proposal, nil
}

// GetPendingBySignalGroup retrieves pending proposal for a signal group
func (r *InviteLinkProposalRepository) GetPendingBySignalGroup(ctx context.Context, signalGroupID string) (*models.InviteLinkUpdateProposal, error) {
	query := `
		SELECT id, signal_group_id, region_id, proposed_by, new_invite_link, reason,
			status, created_at, expires_at, resolved_at
		FROM invite_link_update_proposals
		WHERE signal_group_id = ? AND status = 'pending' AND expires_at > NOW()
	`

	proposal := &models.InviteLinkUpdateProposal{}
	err := r.db.QueryRowContext(ctx, query, signalGroupID).Scan(
		&proposal.ID,
		&proposal.SignalGroupID,
		&proposal.RegionID,
		&proposal.ProposedBy,
		&proposal.NewInviteLink,
		&proposal.Reason,
		&proposal.Status,
		&proposal.CreatedAt,
		&proposal.ExpiresAt,
		&proposal.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return proposal, nil
}

// AddVote adds a vote to a proposal
func (r *InviteLinkProposalRepository) AddVote(ctx context.Context, proposalID, voterID string, vote bool) error {
	id := uuid.New().String()
	query := `
		INSERT INTO invite_link_update_votes (id, proposal_id, voter_id, vote, voted_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := r.db.ExecContext(ctx, query, id, proposalID, voterID, vote, time.Now().UTC())
	return err
}

// CountApprovalVotes counts approval votes for a proposal
func (r *InviteLinkProposalRepository) CountApprovalVotes(ctx context.Context, proposalID string) (int, error) {
	query := `SELECT COUNT(*) FROM invite_link_update_votes WHERE proposal_id = ? AND vote = TRUE`
	var count int
	err := r.db.QueryRowContext(ctx, query, proposalID).Scan(&count)
	return count, err
}

// HasVoted checks if a user has already voted on a proposal
func (r *InviteLinkProposalRepository) HasVoted(ctx context.Context, proposalID, voterID string) (bool, error) {
	query := `SELECT COUNT(*) > 0 FROM invite_link_update_votes WHERE proposal_id = ? AND voter_id = ?`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, proposalID, voterID).Scan(&exists)
	return exists, err
}

// UpdateStatus updates proposal status
func (r *InviteLinkProposalRepository) UpdateStatus(ctx context.Context, id string, status models.ProposalStatus) error {
	now := time.Now().UTC()
	query := `UPDATE invite_link_update_proposals SET status = ?, resolved_at = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, status, now, id)
	return err
}

// GetByIDWithVotes returns proposal with all vote details
func (r *InviteLinkProposalRepository) GetByIDWithVotes(ctx context.Context, id string) (*models.InviteLinkProposalDetailResponse, error) {
	// Main query to get proposal details
	query := `
		SELECT p.id, p.signal_group_id, sg.group_name, p.region_id, gr.name,
			p.proposed_by, u.username, p.new_invite_link, p.reason,
			p.status, p.created_at, p.expires_at, p.resolved_at
		FROM invite_link_update_proposals p
		JOIN signal_groups sg ON p.signal_group_id = sg.id
		JOIN geographic_regions gr ON p.region_id = gr.id
		LEFT JOIN users u ON p.proposed_by = u.id
		WHERE p.id = ?
	`

	var proposerID, proposerUsername sql.NullString
	var reason sql.NullString
	var regionID string
	proposal := &models.InviteLinkProposalDetailResponse{}

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&proposal.ID,
		&proposal.SignalGroupID,
		&proposal.SignalGroupName,
		&regionID,
		&proposal.RegionName,
		&proposerID,
		&proposerUsername,
		&proposal.NewInviteLink,
		&reason,
		&proposal.Status,
		&proposal.CreatedAt,
		&proposal.ExpiresAt,
		&proposal.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("proposal not found")
	}
	if err != nil {
		return nil, err
	}

	proposal.RegionID = regionID
	if reason.Valid {
		proposal.Reason = reason.String
	}
	proposal.ProposedBy = models.ProposalUser{
		ID:       proposerID.String,
		Username: proposerUsername.String,
	}

	// Fetch votes
	votesQuery := `
		SELECT v.voter_id, u.username, v.vote, v.voted_at
		FROM invite_link_update_votes v
		JOIN users u ON v.voter_id = u.id
		WHERE v.proposal_id = ?
		ORDER BY v.voted_at
	`

	rows, err := r.db.QueryContext(ctx, votesQuery, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	proposal.Votes = []models.InviteLinkVoteDetail{}
	for rows.Next() {
		var vote models.InviteLinkVoteDetail
		if err := rows.Scan(&vote.VoterID, &vote.Username, &vote.Vote, &vote.VotedAt); err != nil {
			return nil, err
		}
		proposal.Votes = append(proposal.Votes, vote)
		if vote.Vote {
			proposal.CurrentVotes++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return proposal, nil
}

// ListByUser returns proposals for regions where user is admin
func (r *InviteLinkProposalRepository) ListByUser(ctx context.Context, userID string, filter models.ProposalListFilter) ([]*models.InviteLinkProposalSummary, error) {
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
		SELECT DISTINCT p.id, p.signal_group_id, sg.group_name, gr.name AS region_name,
			u.username AS proposed_by, p.reason,
			(SELECT COUNT(*) FROM invite_link_update_votes v WHERE v.proposal_id = p.id AND v.vote = TRUE) AS votes,
			p.status, p.created_at, p.expires_at
		FROM invite_link_update_proposals p
		JOIN signal_groups sg ON p.signal_group_id = sg.id
		JOIN geographic_regions gr ON p.region_id = gr.id
		JOIN user_admin_regions uar ON p.region_id = uar.id
		LEFT JOIN users u ON p.proposed_by = u.id
		WHERE 1=1
	`

	args := []interface{}{userID}

	if filter.Status != "" {
		query += " AND p.status = ?"
		args = append(args, filter.Status)
	}
	if filter.SignalGroupID != "" {
		query += " AND p.signal_group_id = ?"
		args = append(args, filter.SignalGroupID)
	}
	if filter.RegionID != "" {
		query += " AND p.region_id = ?"
		args = append(args, filter.RegionID)
	}

	query += " ORDER BY p.created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	proposals := []*models.InviteLinkProposalSummary{}
	for rows.Next() {
		p := &models.InviteLinkProposalSummary{}
		var proposedBy sql.NullString
		if err := rows.Scan(
			&p.ID,
			&p.SignalGroupID,
			&p.GroupName,
			&p.RegionName,
			&proposedBy,
			&p.Reason,
			&p.Votes,
			&p.Status,
			&p.CreatedAt,
			&p.ExpiresAt,
		); err != nil {
			return nil, err
		}
		p.ProposedBy = proposedBy.String
		proposals = append(proposals, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return proposals, nil
}

// ListAll returns all proposals (for superusers only)
func (r *InviteLinkProposalRepository) ListAll(ctx context.Context, filter models.ProposalListFilter) ([]*models.InviteLinkProposalSummary, error) {
	query := `
		SELECT p.id, p.signal_group_id, sg.group_name, gr.name AS region_name,
			u.username AS proposed_by, p.reason,
			(SELECT COUNT(*) FROM invite_link_update_votes v WHERE v.proposal_id = p.id AND v.vote = TRUE) AS votes,
			p.status, p.created_at, p.expires_at
		FROM invite_link_update_proposals p
		JOIN signal_groups sg ON p.signal_group_id = sg.id
		JOIN geographic_regions gr ON p.region_id = gr.id
		LEFT JOIN users u ON p.proposed_by = u.id
		WHERE 1=1
	`

	args := []interface{}{}

	if filter.Status != "" {
		query += " AND p.status = ?"
		args = append(args, filter.Status)
	}
	if filter.SignalGroupID != "" {
		query += " AND p.signal_group_id = ?"
		args = append(args, filter.SignalGroupID)
	}
	if filter.RegionID != "" {
		query += " AND p.region_id = ?"
		args = append(args, filter.RegionID)
	}

	query += " ORDER BY p.created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	proposals := []*models.InviteLinkProposalSummary{}
	for rows.Next() {
		p := &models.InviteLinkProposalSummary{}
		var proposedBy sql.NullString
		if err := rows.Scan(
			&p.ID,
			&p.SignalGroupID,
			&p.GroupName,
			&p.RegionName,
			&proposedBy,
			&p.Reason,
			&p.Votes,
			&p.Status,
			&p.CreatedAt,
			&p.ExpiresAt,
		); err != nil {
			return nil, err
		}
		p.ProposedBy = proposedBy.String
		proposals = append(proposals, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return proposals, nil
}

// ExpirePendingProposals marks expired proposals and returns count
func (r *InviteLinkProposalRepository) ExpirePendingProposals(ctx context.Context) (int64, error) {
	query := `
		UPDATE invite_link_update_proposals
		SET status = 'expired', resolved_at = NOW()
		WHERE status = 'pending' AND expires_at < NOW()
	`
	result, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// StartExpirationWorker starts a background goroutine that periodically expires old proposals
func (r *InviteLinkProposalRepository) StartExpirationWorker(ctx context.Context, interval time.Duration) {
	monitorSlug := "crr-proposal-expiration"
	intervalMinutes := int(interval.Minutes())
	if intervalMinutes < 1 {
		intervalMinutes = 1
	}
	monitorConfig := appSentry.IntervalSchedule(intervalMinutes, gosentry.MonitorScheduleUnitMinute)

	runExpiration := func() {
		checkInID := appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusInProgress, nil, monitorConfig)
		if expired, err := r.ExpirePendingProposals(ctx); err != nil {
			log.Printf("ERROR: Proposal expiration failed: %v", err)
			_ = appSentry.CaptureError(err, "component", "proposal_worker", "operation", "expire_proposals")
			appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusError, checkInID, monitorConfig)
		} else {
			if expired > 0 {
				log.Printf("INFO: Expired %d invite link proposals", expired)
			}
			appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusOK, checkInID, monitorConfig)
		}
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Run once immediately at startup
		runExpiration()

		for {
			select {
			case <-ctx.Done():
				log.Println("INFO: Proposal expiration worker stopped")
				return
			case <-ticker.C:
				runExpiration()
			}
		}
	}()

	log.Printf("INFO: Proposal expiration worker started (interval: %v)", interval)
}

// --- Transactional methods for race condition prevention ---

// getByIDWith is the shared implementation for GetByID / GetByIDForUpdate
func (r *InviteLinkProposalRepository) getByIDWith(ctx context.Context, q Querier, id string, forUpdate bool) (*models.InviteLinkUpdateProposal, error) {
	query := `
		SELECT id, signal_group_id, region_id, proposed_by, new_invite_link, reason,
			status, created_at, expires_at, resolved_at
		FROM invite_link_update_proposals
		WHERE id = ?
	`
	if forUpdate {
		query += " FOR UPDATE"
	}

	proposal := &models.InviteLinkUpdateProposal{}
	err := q.QueryRowContext(ctx, query, id).Scan(
		&proposal.ID,
		&proposal.SignalGroupID,
		&proposal.RegionID,
		&proposal.ProposedBy,
		&proposal.NewInviteLink,
		&proposal.Reason,
		&proposal.Status,
		&proposal.CreatedAt,
		&proposal.ExpiresAt,
		&proposal.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("proposal not found")
	}
	if err != nil {
		return nil, err
	}

	return proposal, nil
}

// GetByIDForUpdate locks and retrieves a proposal within a transaction
func (r *InviteLinkProposalRepository) GetByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (*models.InviteLinkUpdateProposal, error) {
	return r.getByIDWith(ctx, tx, id, true)
}

// getPendingBySignalGroupWith is the shared implementation
func (r *InviteLinkProposalRepository) getPendingBySignalGroupWith(ctx context.Context, q Querier, signalGroupID string, forUpdate bool) (*models.InviteLinkUpdateProposal, error) {
	query := `
		SELECT id, signal_group_id, region_id, proposed_by, new_invite_link, reason,
			status, created_at, expires_at, resolved_at
		FROM invite_link_update_proposals
		WHERE signal_group_id = ? AND status = 'pending' AND expires_at > NOW()
	`
	if forUpdate {
		query += " FOR UPDATE"
	}

	proposal := &models.InviteLinkUpdateProposal{}
	err := q.QueryRowContext(ctx, query, signalGroupID).Scan(
		&proposal.ID,
		&proposal.SignalGroupID,
		&proposal.RegionID,
		&proposal.ProposedBy,
		&proposal.NewInviteLink,
		&proposal.Reason,
		&proposal.Status,
		&proposal.CreatedAt,
		&proposal.ExpiresAt,
		&proposal.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return proposal, nil
}

// GetPendingBySignalGroupForUpdate locks pending proposals for duplicate check
func (r *InviteLinkProposalRepository) GetPendingBySignalGroupForUpdate(ctx context.Context, tx *sql.Tx, signalGroupID string) (*models.InviteLinkUpdateProposal, error) {
	return r.getPendingBySignalGroupWith(ctx, tx, signalGroupID, true)
}

// hasVotedWith is the shared implementation
func (r *InviteLinkProposalRepository) hasVotedWith(ctx context.Context, q Querier, proposalID, voterID string) (bool, error) {
	query := `SELECT COUNT(*) > 0 FROM invite_link_update_votes WHERE proposal_id = ? AND voter_id = ?`
	var exists bool
	err := q.QueryRowContext(ctx, query, proposalID, voterID).Scan(&exists)
	return exists, err
}

// HasVotedTx checks if a user has voted within a transaction
func (r *InviteLinkProposalRepository) HasVotedTx(ctx context.Context, tx *sql.Tx, proposalID, voterID string) (bool, error) {
	return r.hasVotedWith(ctx, tx, proposalID, voterID)
}

// addVoteWith is the shared implementation
func (r *InviteLinkProposalRepository) addVoteWith(ctx context.Context, q Querier, proposalID, voterID string, vote bool) error {
	id := uuid.New().String()
	query := `
		INSERT INTO invite_link_update_votes (id, proposal_id, voter_id, vote, voted_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := q.ExecContext(ctx, query, id, proposalID, voterID, vote, time.Now().UTC())
	return err
}

// AddVoteTx adds a vote within a transaction
func (r *InviteLinkProposalRepository) AddVoteTx(ctx context.Context, tx *sql.Tx, proposalID, voterID string, vote bool) error {
	return r.addVoteWith(ctx, tx, proposalID, voterID, vote)
}

// countApprovalVotesWith is the shared implementation
func (r *InviteLinkProposalRepository) countApprovalVotesWith(ctx context.Context, q Querier, proposalID string) (int, error) {
	query := `SELECT COUNT(*) FROM invite_link_update_votes WHERE proposal_id = ? AND vote = TRUE`
	var count int
	err := q.QueryRowContext(ctx, query, proposalID).Scan(&count)
	return count, err
}

// CountApprovalVotesTx counts approval votes within a transaction
func (r *InviteLinkProposalRepository) CountApprovalVotesTx(ctx context.Context, tx *sql.Tx, proposalID string) (int, error) {
	return r.countApprovalVotesWith(ctx, tx, proposalID)
}

// updateStatusWith is the shared implementation
func (r *InviteLinkProposalRepository) updateStatusWith(ctx context.Context, q Querier, id string, status models.ProposalStatus) error {
	now := time.Now().UTC()
	query := `UPDATE invite_link_update_proposals SET status = ?, resolved_at = ? WHERE id = ?`
	_, err := q.ExecContext(ctx, query, status, now, id)
	return err
}

// UpdateStatusTx updates proposal status within a transaction
func (r *InviteLinkProposalRepository) UpdateStatusTx(ctx context.Context, tx *sql.Tx, id string, status models.ProposalStatus) error {
	return r.updateStatusWith(ctx, tx, id, status)
}

// CreateTx creates a new invite link proposal within a transaction
func (r *InviteLinkProposalRepository) CreateTx(ctx context.Context, tx *sql.Tx, proposal *models.InviteLinkUpdateProposal) error {
	proposal.ID = uuid.New().String()
	proposal.CreatedAt = time.Now().UTC()
	proposal.ExpiresAt = proposal.CreatedAt.Add(7 * 24 * time.Hour)
	proposal.Status = models.ProposalStatusPending

	query := `
		INSERT INTO invite_link_update_proposals
		(id, signal_group_id, region_id, proposed_by, new_invite_link, reason, status, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := tx.ExecContext(ctx, query,
		proposal.ID,
		proposal.SignalGroupID,
		proposal.RegionID,
		proposal.ProposedBy,
		proposal.NewInviteLink,
		proposal.Reason,
		proposal.Status,
		proposal.CreatedAt,
		proposal.ExpiresAt,
	)

	return err
}
