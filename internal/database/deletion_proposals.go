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
	ErrDeletionProposalNotFound = errors.New("deletion proposal not found")
)

// DeletionProposalRepository handles deletion proposal database operations
type DeletionProposalRepository struct {
	db *DB
}

// NewDeletionProposalRepository creates a new deletion proposal repository
func NewDeletionProposalRepository(db *DB) *DeletionProposalRepository {
	return &DeletionProposalRepository{db: db}
}

// Create creates a new deletion proposal
func (r *DeletionProposalRepository) Create(ctx context.Context, proposal *models.DeletionProposal) error {
	proposal.ID = uuid.New().String()
	proposal.CreatedAt = time.Now().UTC()
	proposal.Status = models.ProposalStatusPending

	query := `
		INSERT INTO deletion_proposals
		(id, asset_type, asset_id, region_id, school_id, district_id, proposed_by, reason, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		proposal.ID,
		proposal.AssetType,
		proposal.AssetID,
		proposal.RegionID,
		proposal.SchoolID,
		proposal.DistrictID,
		proposal.ProposedBy,
		proposal.Reason,
		proposal.Status,
		proposal.CreatedAt,
	)

	return err
}

// GetByID retrieves a proposal by ID
func (r *DeletionProposalRepository) GetByID(ctx context.Context, id string) (*models.DeletionProposal, error) {
	return r.getByIDWith(ctx, r.db, id, false)
}

// GetPendingByAsset retrieves a pending proposal for a specific asset
func (r *DeletionProposalRepository) GetPendingByAsset(ctx context.Context, assetType models.AssetType, assetID string) (*models.DeletionProposal, error) {
	return r.getPendingByAssetWith(ctx, r.db, assetType, assetID, false)
}

// AddVote adds a vote to a proposal
func (r *DeletionProposalRepository) AddVote(ctx context.Context, proposalID, voterID string, vote bool) error {
	return r.addVoteWith(ctx, r.db, proposalID, voterID, vote)
}

// CountApprovalVotes counts approval votes for a proposal
func (r *DeletionProposalRepository) CountApprovalVotes(ctx context.Context, proposalID string) (int, error) {
	return r.countApprovalVotesWith(ctx, r.db, proposalID)
}

// HasVoted checks if a user has already voted on a proposal
func (r *DeletionProposalRepository) HasVoted(ctx context.Context, proposalID, voterID string) (bool, error) {
	return r.hasVotedWith(ctx, r.db, proposalID, voterID)
}

// UpdateStatus updates proposal status
func (r *DeletionProposalRepository) UpdateStatus(ctx context.Context, id string, status models.ProposalStatus) error {
	return r.updateStatusWith(ctx, r.db, id, status)
}

// GetByIDWithVotes returns proposal with all vote details
func (r *DeletionProposalRepository) GetByIDWithVotes(ctx context.Context, id string) (*models.DeletionProposalDetailResponse, error) {
	query := `
		SELECT p.id, p.asset_type, p.asset_id,
			COALESCE(sg.group_name, ar.name, 'Unknown') AS asset_name,
			COALESCE(p.region_id, '') AS region_id,
			COALESCE(gr.name, '') AS region_name,
			COALESCE(p.school_id, '') AS school_id,
			COALESCE(sc.name, '') AS school_name,
			COALESCE(p.district_id, '') AS district_id,
			COALESCE(sd.name, '') AS district_name,
			p.proposed_by, u.username,
			COALESCE(p.reason, '') AS reason,
			p.status, p.created_at, p.resolved_at
		FROM deletion_proposals p
		LEFT JOIN signal_groups sg ON p.asset_type = 'signal_group' AND p.asset_id = sg.id
		LEFT JOIN geographic_regions ar ON p.asset_type = 'sub_region' AND p.asset_id = ar.id
		LEFT JOIN geographic_regions gr ON p.region_id = gr.id
		LEFT JOIN schools sc ON p.school_id = sc.id
		LEFT JOIN school_districts sd ON p.district_id = sd.id
		LEFT JOIN users u ON p.proposed_by = u.id
		WHERE p.id = ?
	`

	var proposerID, proposerUsername sql.NullString
	proposal := &models.DeletionProposalDetailResponse{}

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&proposal.ID,
		&proposal.AssetType,
		&proposal.AssetID,
		&proposal.AssetName,
		&proposal.RegionID,
		&proposal.RegionName,
		&proposal.SchoolID,
		&proposal.SchoolName,
		&proposal.DistrictID,
		&proposal.DistrictName,
		&proposerID,
		&proposerUsername,
		&proposal.Reason,
		&proposal.Status,
		&proposal.CreatedAt,
		&proposal.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDeletionProposalNotFound
	}
	if err != nil {
		return nil, err
	}

	proposal.ProposedBy = models.ProposalUser{
		ID:       proposerID.String,
		Username: proposerUsername.String,
	}

	// Fetch votes
	votesQuery := `
		SELECT v.voter_id, u.username, v.vote, v.voted_at
		FROM deletion_votes v
		JOIN users u ON v.voter_id = u.id
		WHERE v.proposal_id = ?
		ORDER BY v.voted_at
	`

	rows, err := r.db.QueryContext(ctx, votesQuery, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	proposal.Votes = []models.DeletionVoteDetail{}
	for rows.Next() {
		var vote models.DeletionVoteDetail
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

// ListByUser returns proposals for regions/schools/districts where user is admin
func (r *DeletionProposalRepository) ListByUser(ctx context.Context, userID string, filter models.ProposalListFilter) ([]*models.DeletionProposalSummary, error) {
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
		SELECT DISTINCT p.id, p.asset_type,
			COALESCE(sg.group_name, ar.name, 'Unknown') AS asset_name,
			p.reason,
			u.username AS proposed_by,
			(SELECT COUNT(*) FROM deletion_votes v WHERE v.proposal_id = p.id AND v.vote = TRUE) AS votes,
			p.status, p.created_at
		FROM deletion_proposals p
		LEFT JOIN signal_groups sg ON p.asset_type = 'signal_group' AND p.asset_id = sg.id
		LEFT JOIN geographic_regions ar ON p.asset_type = 'sub_region' AND p.asset_id = ar.id
		LEFT JOIN users u ON p.proposed_by = u.id
		WHERE (
			-- Region scope: user is admin in the region (with upward propagation)
			p.region_id IN (SELECT id FROM user_admin_regions)
			OR
			-- School scope: user is verified admin of the school
			p.school_id IN (
				SELECT us.school_id FROM user_schools us
				WHERE us.user_id = ? AND us.is_admin = TRUE AND us.verification_status = 'verified'
			)
			OR
			-- District scope: user is verified admin of any school in the district
			p.district_id IN (
				SELECT s.district_id FROM schools s
				INNER JOIN user_schools us ON us.school_id = s.id
				WHERE us.user_id = ? AND us.is_admin = TRUE AND us.verification_status = 'verified'
				AND s.district_id IS NOT NULL
			)
		)
	`

	args := []interface{}{userID, userID, userID}

	if filter.Status != "" {
		query += " AND p.status = ?"
		args = append(args, filter.Status)
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

	return r.scanProposalSummaries(rows)
}

// ListAll returns all proposals (for superusers only)
func (r *DeletionProposalRepository) ListAll(ctx context.Context, filter models.ProposalListFilter) ([]*models.DeletionProposalSummary, error) {
	query := `
		SELECT p.id, p.asset_type,
			COALESCE(sg.group_name, ar.name, 'Unknown') AS asset_name,
			p.reason,
			u.username AS proposed_by,
			(SELECT COUNT(*) FROM deletion_votes v WHERE v.proposal_id = p.id AND v.vote = TRUE) AS votes,
			p.status, p.created_at
		FROM deletion_proposals p
		LEFT JOIN signal_groups sg ON p.asset_type = 'signal_group' AND p.asset_id = sg.id
		LEFT JOIN geographic_regions ar ON p.asset_type = 'sub_region' AND p.asset_id = ar.id
		LEFT JOIN users u ON p.proposed_by = u.id
		WHERE 1=1
	`

	args := []interface{}{}

	if filter.Status != "" {
		query += " AND p.status = ?"
		args = append(args, filter.Status)
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

	return r.scanProposalSummaries(rows)
}

// scanProposalSummaries scans rows into DeletionProposalSummary slices
func (r *DeletionProposalRepository) scanProposalSummaries(rows *sql.Rows) ([]*models.DeletionProposalSummary, error) {
	proposals := []*models.DeletionProposalSummary{}
	for rows.Next() {
		p := &models.DeletionProposalSummary{}
		var proposedBy sql.NullString
		if err := rows.Scan(
			&p.ID,
			&p.AssetType,
			&p.AssetName,
			&p.Reason,
			&proposedBy,
			&p.Votes,
			&p.Status,
			&p.CreatedAt,
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

// --- Transactional methods for race condition prevention ---

// getByIDWith is the shared implementation for GetByID / GetByIDForUpdate
func (r *DeletionProposalRepository) getByIDWith(ctx context.Context, q Querier, id string, forUpdate bool) (*models.DeletionProposal, error) {
	query := `
		SELECT id, asset_type, asset_id, region_id, school_id, district_id,
			proposed_by, reason, status, created_at, resolved_at
		FROM deletion_proposals
		WHERE id = ?
	`
	if forUpdate {
		query += " FOR UPDATE"
	}

	proposal := &models.DeletionProposal{}
	err := q.QueryRowContext(ctx, query, id).Scan(
		&proposal.ID,
		&proposal.AssetType,
		&proposal.AssetID,
		&proposal.RegionID,
		&proposal.SchoolID,
		&proposal.DistrictID,
		&proposal.ProposedBy,
		&proposal.Reason,
		&proposal.Status,
		&proposal.CreatedAt,
		&proposal.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDeletionProposalNotFound
	}
	if err != nil {
		return nil, err
	}

	return proposal, nil
}

// GetByIDForUpdate locks and retrieves a proposal within a transaction
func (r *DeletionProposalRepository) GetByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (*models.DeletionProposal, error) {
	return r.getByIDWith(ctx, tx, id, true)
}

// getPendingByAssetWith is the shared implementation
func (r *DeletionProposalRepository) getPendingByAssetWith(ctx context.Context, q Querier, assetType models.AssetType, assetID string, forUpdate bool) (*models.DeletionProposal, error) {
	query := `
		SELECT id, asset_type, asset_id, region_id, school_id, district_id,
			proposed_by, reason, status, created_at, resolved_at
		FROM deletion_proposals
		WHERE asset_type = ? AND asset_id = ? AND status = 'pending'
	`
	if forUpdate {
		query += " FOR UPDATE"
	}

	proposal := &models.DeletionProposal{}
	err := q.QueryRowContext(ctx, query, assetType, assetID).Scan(
		&proposal.ID,
		&proposal.AssetType,
		&proposal.AssetID,
		&proposal.RegionID,
		&proposal.SchoolID,
		&proposal.DistrictID,
		&proposal.ProposedBy,
		&proposal.Reason,
		&proposal.Status,
		&proposal.CreatedAt,
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

// GetPendingByAssetForUpdate locks pending proposals for duplicate check
func (r *DeletionProposalRepository) GetPendingByAssetForUpdate(ctx context.Context, tx *sql.Tx, assetType models.AssetType, assetID string) (*models.DeletionProposal, error) {
	return r.getPendingByAssetWith(ctx, tx, assetType, assetID, true)
}

// hasVotedWith is the shared implementation
func (r *DeletionProposalRepository) hasVotedWith(ctx context.Context, q Querier, proposalID, voterID string) (bool, error) {
	query := `SELECT COUNT(*) > 0 FROM deletion_votes WHERE proposal_id = ? AND voter_id = ?`
	var exists bool
	err := q.QueryRowContext(ctx, query, proposalID, voterID).Scan(&exists)
	return exists, err
}

// HasVotedTx checks if a user has voted within a transaction
func (r *DeletionProposalRepository) HasVotedTx(ctx context.Context, tx *sql.Tx, proposalID, voterID string) (bool, error) {
	return r.hasVotedWith(ctx, tx, proposalID, voterID)
}

// addVoteWith is the shared implementation
func (r *DeletionProposalRepository) addVoteWith(ctx context.Context, q Querier, proposalID, voterID string, vote bool) error {
	id := uuid.New().String()
	query := `
		INSERT INTO deletion_votes (id, proposal_id, voter_id, vote, voted_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := q.ExecContext(ctx, query, id, proposalID, voterID, vote, time.Now().UTC())
	return err
}

// AddVoteTx adds a vote within a transaction
func (r *DeletionProposalRepository) AddVoteTx(ctx context.Context, tx *sql.Tx, proposalID, voterID string, vote bool) error {
	return r.addVoteWith(ctx, tx, proposalID, voterID, vote)
}

// countApprovalVotesWith is the shared implementation
func (r *DeletionProposalRepository) countApprovalVotesWith(ctx context.Context, q Querier, proposalID string) (int, error) {
	query := `SELECT COUNT(*) FROM deletion_votes WHERE proposal_id = ? AND vote = TRUE`
	var count int
	err := q.QueryRowContext(ctx, query, proposalID).Scan(&count)
	return count, err
}

// CountApprovalVotesTx counts approval votes within a transaction
func (r *DeletionProposalRepository) CountApprovalVotesTx(ctx context.Context, tx *sql.Tx, proposalID string) (int, error) {
	return r.countApprovalVotesWith(ctx, tx, proposalID)
}

// updateStatusWith is the shared implementation
func (r *DeletionProposalRepository) updateStatusWith(ctx context.Context, q Querier, id string, status models.ProposalStatus) error {
	now := time.Now().UTC()
	query := `UPDATE deletion_proposals SET status = ?, resolved_at = ? WHERE id = ?`
	_, err := q.ExecContext(ctx, query, status, now, id)
	return err
}

// UpdateStatusTx updates proposal status within a transaction
func (r *DeletionProposalRepository) UpdateStatusTx(ctx context.Context, tx *sql.Tx, id string, status models.ProposalStatus) error {
	return r.updateStatusWith(ctx, tx, id, status)
}

// CreateTx creates a new deletion proposal within a transaction
func (r *DeletionProposalRepository) CreateTx(ctx context.Context, tx *sql.Tx, proposal *models.DeletionProposal) error {
	proposal.ID = uuid.New().String()
	proposal.CreatedAt = time.Now().UTC()
	proposal.Status = models.ProposalStatusPending

	query := `
		INSERT INTO deletion_proposals
		(id, asset_type, asset_id, region_id, school_id, district_id, proposed_by, reason, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := tx.ExecContext(ctx, query,
		proposal.ID,
		proposal.AssetType,
		proposal.AssetID,
		proposal.RegionID,
		proposal.SchoolID,
		proposal.DistrictID,
		proposal.ProposedBy,
		proposal.Reason,
		proposal.Status,
		proposal.CreatedAt,
	)

	return err
}

// ApplySignalGroupDeletionTx deactivates a signal group within a transaction
func (r *DeletionProposalRepository) ApplySignalGroupDeletionTx(ctx context.Context, tx *sql.Tx, assetID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE signal_groups SET is_active = FALSE WHERE id = ?`, assetID)
	return err
}

// ApplySubRegionDeletionTx cascade-deletes a region and all its children within a transaction.
// Mirrors RegionRepository.Delete but operates on the passed-in transaction.
func (r *DeletionProposalRepository) ApplySubRegionDeletionTx(ctx context.Context, tx *sql.Tx, regionID string) error {
	// Delete signal groups in this region (also cascades encrypted_secrets/secret_update_proposals via FK)
	if _, err := tx.ExecContext(ctx, `DELETE FROM signal_groups WHERE region_id = ?`, regionID); err != nil {
		return err
	}

	// Delete user_regions associations
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_regions WHERE region_id = ?`, regionID); err != nil {
		return err
	}

	// Nullify region references in tables with non-cascading FKs to preserve historical records
	if _, err := tx.ExecContext(ctx, `UPDATE verification_requests SET region_id = NULL WHERE region_id = ?`, regionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE vouches SET region_id = NULL WHERE region_id = ?`, regionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE deletion_proposals SET region_id = NULL WHERE region_id = ?`, regionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE secret_update_proposals SET region_id = NULL WHERE region_id = ?`, regionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE blocklist_proposals SET region_id = NULL WHERE region_id = ?`, regionID); err != nil {
		return err
	}

	// Find child regions
	var childIDs []string
	rows, err := tx.QueryContext(ctx, `SELECT id FROM geographic_regions WHERE parent_region_id = ?`, regionID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var childID string
		if err := rows.Scan(&childID); err != nil {
			_ = rows.Close()
			return err
		}
		childIDs = append(childIDs, childID)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Recursively delete each child
	for _, childID := range childIDs {
		if err := r.ApplySubRegionDeletionTx(ctx, tx, childID); err != nil {
			return err
		}
	}

	// Delete the region itself
	_, err = tx.ExecContext(ctx, `DELETE FROM geographic_regions WHERE id = ?`, regionID)
	return err
}
