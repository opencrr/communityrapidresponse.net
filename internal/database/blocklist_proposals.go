package database

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	gosentry "github.com/getsentry/sentry-go"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
)

var (
	ErrBlocklistProposalNotFound = errors.New("blocklist proposal not found")
)

// BlocklistProposalRepository handles blocklist proposal database operations
type BlocklistProposalRepository struct {
	db              *DB
	blocklistConfig *config.BlocklistConfig
}

// NewBlocklistProposalRepository creates a new blocklist proposal repository
func NewBlocklistProposalRepository(db *DB, blocklistConfig *config.BlocklistConfig) *BlocklistProposalRepository {
	return &BlocklistProposalRepository{
		db:              db,
		blocklistConfig: blocklistConfig,
	}
}

// Create creates a new blocklist proposal
func (r *BlocklistProposalRepository) Create(ctx context.Context, proposal *models.BlocklistProposal) error {
	proposal.ID = uuid.New().String()
	proposal.CreatedAt = time.Now().UTC()
	proposal.ExpiresAt = proposal.CreatedAt.Add(7 * 24 * time.Hour) // 7 days
	proposal.Status = models.ProposalStatusPending

	query := `
		INSERT INTO blocklist_proposals
		(id, target_user_id, region_id, proposed_by, reason, evidence, status, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		proposal.ID,
		proposal.TargetUserID,
		proposal.RegionID,
		proposal.ProposedBy,
		proposal.Reason,
		proposal.Evidence,
		proposal.Status,
		proposal.CreatedAt,
		proposal.ExpiresAt,
	)

	return err
}

// GetByID retrieves a proposal by ID
func (r *BlocklistProposalRepository) GetByID(ctx context.Context, id string) (*models.BlocklistProposal, error) {
	query := `
		SELECT id, target_user_id, region_id, proposed_by, reason, evidence,
			status, created_at, expires_at, resolved_at
		FROM blocklist_proposals
		WHERE id = ?
	`

	proposal := &models.BlocklistProposal{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&proposal.ID,
		&proposal.TargetUserID,
		&proposal.RegionID,
		&proposal.ProposedBy,
		&proposal.Reason,
		&proposal.Evidence,
		&proposal.Status,
		&proposal.CreatedAt,
		&proposal.ExpiresAt,
		&proposal.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBlocklistProposalNotFound
	}
	if err != nil {
		return nil, err
	}

	return proposal, nil
}

// GetPendingByTargetUser retrieves pending proposal for a target user in a specific region
func (r *BlocklistProposalRepository) GetPendingByTargetUser(ctx context.Context, targetUserID, regionID string) (*models.BlocklistProposal, error) {
	query := `
		SELECT id, target_user_id, region_id, proposed_by, reason, evidence,
			status, created_at, expires_at, resolved_at
		FROM blocklist_proposals
		WHERE target_user_id = ? AND region_id = ? AND status = 'pending' AND expires_at > NOW()
	`

	proposal := &models.BlocklistProposal{}
	err := r.db.QueryRowContext(ctx, query, targetUserID, regionID).Scan(
		&proposal.ID,
		&proposal.TargetUserID,
		&proposal.RegionID,
		&proposal.ProposedBy,
		&proposal.Reason,
		&proposal.Evidence,
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
func (r *BlocklistProposalRepository) AddVote(ctx context.Context, proposalID, voterID string, vote bool) error {
	id := uuid.New().String()
	query := `
		INSERT INTO blocklist_votes (id, proposal_id, voter_id, vote, voted_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := r.db.ExecContext(ctx, query, id, proposalID, voterID, vote, time.Now().UTC())
	return err
}

// CountApprovalVotes counts approval votes for a proposal
func (r *BlocklistProposalRepository) CountApprovalVotes(ctx context.Context, proposalID string) (int, error) {
	query := `SELECT COUNT(*) FROM blocklist_votes WHERE proposal_id = ? AND vote = TRUE`
	var count int
	err := r.db.QueryRowContext(ctx, query, proposalID).Scan(&count)
	return count, err
}

// HasVoted checks if a user has already voted on a proposal
func (r *BlocklistProposalRepository) HasVoted(ctx context.Context, proposalID, voterID string) (bool, error) {
	query := `SELECT COUNT(*) > 0 FROM blocklist_votes WHERE proposal_id = ? AND voter_id = ?`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, proposalID, voterID).Scan(&exists)
	return exists, err
}

// UpdateStatus updates proposal status
func (r *BlocklistProposalRepository) UpdateStatus(ctx context.Context, id string, status models.ProposalStatus) error {
	now := time.Now().UTC()
	query := `UPDATE blocklist_proposals SET status = ?, resolved_at = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, status, now, id)
	return err
}

// GetByIDWithVotes returns proposal with all vote details
func (r *BlocklistProposalRepository) GetByIDWithVotes(ctx context.Context, id string) (*models.BlocklistProposalDetailResponse, error) {
	// Main query to get proposal details
	query := `
		SELECT p.id, p.target_user_id, tu.username, tu.verification_tier, p.region_id, gr.name,
			p.proposed_by, u.username, p.reason, p.evidence,
			p.status, p.created_at, p.expires_at, p.resolved_at
		FROM blocklist_proposals p
		JOIN users tu ON p.target_user_id = tu.id
		JOIN geographic_regions gr ON p.region_id = gr.id
		LEFT JOIN users u ON p.proposed_by = u.id
		WHERE p.id = ?
	`

	var proposerID, proposerUsername sql.NullString
	var evidence sql.NullString
	var targetUserID, targetUsername string
	var targetVerificationTier int
	var regionID string
	proposal := &models.BlocklistProposalDetailResponse{}

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&proposal.ID,
		&targetUserID,
		&targetUsername,
		&targetVerificationTier,
		&regionID,
		&proposal.RegionName,
		&proposerID,
		&proposerUsername,
		&proposal.Reason,
		&evidence,
		&proposal.Status,
		&proposal.CreatedAt,
		&proposal.ExpiresAt,
		&proposal.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBlocklistProposalNotFound
	}
	if err != nil {
		return nil, err
	}

	proposal.RegionID = regionID
	proposal.TargetUser = models.PublicUser{
		ID:               targetUserID,
		Username:         targetUsername,
		VerificationTier: models.VerificationTier(targetVerificationTier),
	}
	if evidence.Valid {
		proposal.Evidence = evidence.String
	}
	proposal.ProposedBy = models.ProposalUser{
		ID:       proposerID.String,
		Username: proposerUsername.String,
	}

	// Fetch votes
	votesQuery := `
		SELECT v.voter_id, u.username, v.vote, v.voted_at
		FROM blocklist_votes v
		JOIN users u ON v.voter_id = u.id
		WHERE v.proposal_id = ?
		ORDER BY v.voted_at
	`

	rows, err := r.db.QueryContext(ctx, votesQuery, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	proposal.Votes = []models.BlocklistVoteDetail{}
	for rows.Next() {
		var vote models.BlocklistVoteDetail
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
func (r *BlocklistProposalRepository) ListByUser(ctx context.Context, userID string, filter models.ProposalListFilter) ([]*models.BlocklistProposalSummary, error) {
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
		SELECT DISTINCT p.id, tu.id, tu.username, tu.verification_tier, gr.name AS region_name,
			u.username AS proposed_by, p.reason,
			(SELECT COUNT(*) FROM blocklist_votes v WHERE v.proposal_id = p.id AND v.vote = TRUE) AS votes,
			p.status, p.created_at, p.expires_at
		FROM blocklist_proposals p
		JOIN users tu ON p.target_user_id = tu.id
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

	proposals := []*models.BlocklistProposalSummary{}
	for rows.Next() {
		p := &models.BlocklistProposalSummary{}
		var targetUserID, targetUsername string
		var targetVerificationTier int
		var proposedBy sql.NullString
		var expiresAt time.Time
		if err := rows.Scan(
			&p.ID,
			&targetUserID,
			&targetUsername,
			&targetVerificationTier,
			&p.RegionName,
			&proposedBy,
			&p.Reason,
			&p.Votes,
			&p.Status,
			&p.CreatedAt,
			&expiresAt,
		); err != nil {
			return nil, err
		}
		p.TargetUser = models.PublicUser{
			ID:               targetUserID,
			Username:         targetUsername,
			VerificationTier: models.VerificationTier(targetVerificationTier),
		}
		p.ProposedBy = proposedBy.String
		p.ExpiresAt = expiresAt
		proposals = append(proposals, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return proposals, nil
}

// ListAll returns all proposals (for superusers only)
func (r *BlocklistProposalRepository) ListAll(ctx context.Context, filter models.ProposalListFilter) ([]*models.BlocklistProposalSummary, error) {
	query := `
		SELECT p.id, tu.id, tu.username, tu.verification_tier, gr.name AS region_name,
			u.username AS proposed_by, p.reason,
			(SELECT COUNT(*) FROM blocklist_votes v WHERE v.proposal_id = p.id AND v.vote = TRUE) AS votes,
			p.status, p.created_at, p.expires_at
		FROM blocklist_proposals p
		JOIN users tu ON p.target_user_id = tu.id
		JOIN geographic_regions gr ON p.region_id = gr.id
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

	proposals := []*models.BlocklistProposalSummary{}
	for rows.Next() {
		p := &models.BlocklistProposalSummary{}
		var targetUserID, targetUsername string
		var targetVerificationTier int
		var proposedBy sql.NullString
		var expiresAt time.Time
		if err := rows.Scan(
			&p.ID,
			&targetUserID,
			&targetUsername,
			&targetVerificationTier,
			&p.RegionName,
			&proposedBy,
			&p.Reason,
			&p.Votes,
			&p.Status,
			&p.CreatedAt,
			&expiresAt,
		); err != nil {
			return nil, err
		}
		p.TargetUser = models.PublicUser{
			ID:               targetUserID,
			Username:         targetUsername,
			VerificationTier: models.VerificationTier(targetVerificationTier),
		}
		p.ProposedBy = proposedBy.String
		p.ExpiresAt = expiresAt
		proposals = append(proposals, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return proposals, nil
}

// CountProposalsThisMonth counts how many proposals a user has created this month
func (r *BlocklistProposalRepository) CountProposalsThisMonth(ctx context.Context, userID string) (int, error) {
	query := `
		SELECT COUNT(*) FROM blocklist_proposals
		WHERE proposed_by = ?
		AND created_at >= DATE_FORMAT(NOW(), '%Y-%m-01')
	`
	var count int
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&count)
	return count, err
}

// ExpirePendingProposals marks expired proposals and returns count
func (r *BlocklistProposalRepository) ExpirePendingProposals(ctx context.Context) (int64, error) {
	query := `
		UPDATE blocklist_proposals
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
func (r *BlocklistProposalRepository) StartExpirationWorker(ctx context.Context, interval time.Duration) {
	monitorSlug := "crr-blocklist-expiration"
	intervalMinutes := int(interval.Minutes())
	if intervalMinutes < 1 {
		intervalMinutes = 1
	}
	monitorConfig := appSentry.IntervalSchedule(intervalMinutes, gosentry.MonitorScheduleUnitMinute)

	runExpiration := func() {
		checkInID := appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusInProgress, nil, monitorConfig)
		if expired, err := r.ExpirePendingProposals(ctx); err != nil {
			slog.Error("blocklist proposal expiration failed", "error", err)
			_ = appSentry.CaptureError(err, "component", "blocklist_worker", "operation", "expire_proposals")
			appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusError, checkInID, monitorConfig)
		} else {
			if expired > 0 {
				slog.Info("expired blocklist proposals", "count", expired)
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
				slog.Info("blocklist proposal expiration worker stopped")
				return
			case <-ticker.C:
				runExpiration()
			}
		}
	}()

	slog.Info("blocklist proposal expiration worker started", "interval", interval)
}

// ApplyBlocklist applies the blocklist to a user - removes them from ALL regions and blocks their address
func (r *BlocklistProposalRepository) ApplyBlocklist(ctx context.Context, userID, regionID, proposalID, blockedBy, reason string) error {
	return r.db.Transaction(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC()

		// 1. Set global block flag on user
		_, err := tx.ExecContext(ctx, `
			UPDATE users SET is_blocked = TRUE, blocked_at = ?, blocked_by = ?, block_reason = ?
			WHERE id = ?
		`, now, blockedBy, reason, userID)
		if err != nil {
			return err
		}

		// 2. Remove user from ALL regions
		_, err = tx.ExecContext(ctx, `DELETE FROM user_regions WHERE user_id = ?`, userID)
		if err != nil {
			return err
		}

		// 3. Track in blocked_users table
		blocklistID := uuid.New().String()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO blocked_users (id, user_id, region_id, proposal_id, blocked_at)
			VALUES (?, ?, ?, ?, ?)
		`, blocklistID, userID, regionID, proposalID, now)
		if err != nil {
			return err
		}

		// 4. Block the user's verified address (if they have one)
		var addressHash *string
		err = tx.QueryRowContext(ctx, `SELECT address_hash FROM users WHERE id = ?`, userID).Scan(&addressHash)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		if addressHash != nil && *addressHash != "" {
			// Calculate expiration based on config
			expiresAt := now.Add(r.blocklistConfig.AddressBlocklistDuration)

			addressBlocklistID := uuid.New().String()
			_, err = tx.ExecContext(ctx, `
				INSERT INTO blocked_addresses (id, address_hash, user_id, proposal_id, blocked_at, expires_at)
				VALUES (?, ?, ?, ?, ?, ?)
				ON DUPLICATE KEY UPDATE
					expires_at = VALUES(expires_at),
					proposal_id = VALUES(proposal_id)
			`, addressBlocklistID, *addressHash, userID, proposalID, now, expiresAt)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// IsAddressBlocked checks if an address hash is currently blocked
func (r *BlocklistProposalRepository) IsAddressBlocked(ctx context.Context, addressHash string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM blocked_addresses
		WHERE address_hash = ? AND expires_at > NOW()
	`, addressHash).Scan(&count)
	return count > 0, err
}

// ExpireAddress sets the expires_at to NOW() for a blocked address (soft expiration)
func (r *BlocklistProposalRepository) ExpireAddress(ctx context.Context, addressHash string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE blocked_addresses SET expires_at = NOW() WHERE address_hash = ?
	`, addressHash)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return errors.New("address not found")
	}
	return nil
}

// ListBlockedAddresses returns all blocked addresses (for superuser admin)
func (r *BlocklistProposalRepository) ListBlockedAddresses(ctx context.Context, activeOnly bool) ([]*models.BlockedAddress, error) {
	query := `
		SELECT ba.id, ba.address_hash, ba.user_id, u.username, ba.proposal_id,
			ba.blocked_at, ba.expires_at, ba.expires_at > NOW() AS is_active
		FROM blocked_addresses ba
		JOIN users u ON ba.user_id = u.id
	`

	if activeOnly {
		query += " WHERE ba.expires_at > NOW()"
	}

	query += " ORDER BY ba.blocked_at DESC"

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	addresses := []*models.BlockedAddress{}
	for rows.Next() {
		a := &models.BlockedAddress{}
		if err := rows.Scan(
			&a.ID,
			&a.AddressHash,
			&a.UserID,
			&a.Username,
			&a.ProposalID,
			&a.BlockedAt,
			&a.ExpiresAt,
			&a.IsActive,
		); err != nil {
			return nil, err
		}
		addresses = append(addresses, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return addresses, nil
}

// --- Transactional methods for race condition prevention ---

// getByIDWith is the shared implementation for GetByID / GetByIDForUpdate
func (r *BlocklistProposalRepository) getByIDWith(ctx context.Context, q Querier, id string, forUpdate bool) (*models.BlocklistProposal, error) {
	query := `
		SELECT id, target_user_id, region_id, proposed_by, reason, evidence,
			status, created_at, expires_at, resolved_at
		FROM blocklist_proposals
		WHERE id = ?
	`
	if forUpdate {
		query += " FOR UPDATE"
	}

	proposal := &models.BlocklistProposal{}
	err := q.QueryRowContext(ctx, query, id).Scan(
		&proposal.ID,
		&proposal.TargetUserID,
		&proposal.RegionID,
		&proposal.ProposedBy,
		&proposal.Reason,
		&proposal.Evidence,
		&proposal.Status,
		&proposal.CreatedAt,
		&proposal.ExpiresAt,
		&proposal.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBlocklistProposalNotFound
	}
	if err != nil {
		return nil, err
	}

	return proposal, nil
}

// GetByIDForUpdate locks and retrieves a proposal within a transaction
func (r *BlocklistProposalRepository) GetByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (*models.BlocklistProposal, error) {
	return r.getByIDWith(ctx, tx, id, true)
}

// getPendingByTargetUserWith is the shared implementation
func (r *BlocklistProposalRepository) getPendingByTargetUserWith(ctx context.Context, q Querier, targetUserID, regionID string, forUpdate bool) (*models.BlocklistProposal, error) {
	query := `
		SELECT id, target_user_id, region_id, proposed_by, reason, evidence,
			status, created_at, expires_at, resolved_at
		FROM blocklist_proposals
		WHERE target_user_id = ? AND region_id = ? AND status = 'pending' AND expires_at > NOW()
	`
	if forUpdate {
		query += " FOR UPDATE"
	}

	proposal := &models.BlocklistProposal{}
	err := q.QueryRowContext(ctx, query, targetUserID, regionID).Scan(
		&proposal.ID,
		&proposal.TargetUserID,
		&proposal.RegionID,
		&proposal.ProposedBy,
		&proposal.Reason,
		&proposal.Evidence,
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

// GetPendingByTargetUserForUpdate locks pending proposals for duplicate check
func (r *BlocklistProposalRepository) GetPendingByTargetUserForUpdate(ctx context.Context, tx *sql.Tx, targetUserID, regionID string) (*models.BlocklistProposal, error) {
	return r.getPendingByTargetUserWith(ctx, tx, targetUserID, regionID, true)
}

// hasVotedWith is the shared implementation
func (r *BlocklistProposalRepository) hasVotedWith(ctx context.Context, q Querier, proposalID, voterID string) (bool, error) {
	query := `SELECT COUNT(*) > 0 FROM blocklist_votes WHERE proposal_id = ? AND voter_id = ?`
	var exists bool
	err := q.QueryRowContext(ctx, query, proposalID, voterID).Scan(&exists)
	return exists, err
}

// HasVotedTx checks if a user has voted within a transaction
func (r *BlocklistProposalRepository) HasVotedTx(ctx context.Context, tx *sql.Tx, proposalID, voterID string) (bool, error) {
	return r.hasVotedWith(ctx, tx, proposalID, voterID)
}

// addVoteWith is the shared implementation
func (r *BlocklistProposalRepository) addVoteWith(ctx context.Context, q Querier, proposalID, voterID string, vote bool) error {
	id := uuid.New().String()
	query := `
		INSERT INTO blocklist_votes (id, proposal_id, voter_id, vote, voted_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := q.ExecContext(ctx, query, id, proposalID, voterID, vote, time.Now().UTC())
	return err
}

// AddVoteTx adds a vote within a transaction
func (r *BlocklistProposalRepository) AddVoteTx(ctx context.Context, tx *sql.Tx, proposalID, voterID string, vote bool) error {
	return r.addVoteWith(ctx, tx, proposalID, voterID, vote)
}

// countApprovalVotesWith is the shared implementation
func (r *BlocklistProposalRepository) countApprovalVotesWith(ctx context.Context, q Querier, proposalID string) (int, error) {
	query := `SELECT COUNT(*) FROM blocklist_votes WHERE proposal_id = ? AND vote = TRUE`
	var count int
	err := q.QueryRowContext(ctx, query, proposalID).Scan(&count)
	return count, err
}

// CountApprovalVotesTx counts approval votes within a transaction
func (r *BlocklistProposalRepository) CountApprovalVotesTx(ctx context.Context, tx *sql.Tx, proposalID string) (int, error) {
	return r.countApprovalVotesWith(ctx, tx, proposalID)
}

// updateStatusWith is the shared implementation
func (r *BlocklistProposalRepository) updateStatusWith(ctx context.Context, q Querier, id string, status models.ProposalStatus) error {
	now := time.Now().UTC()
	query := `UPDATE blocklist_proposals SET status = ?, resolved_at = ? WHERE id = ?`
	_, err := q.ExecContext(ctx, query, status, now, id)
	return err
}

// UpdateStatusTx updates proposal status within a transaction
func (r *BlocklistProposalRepository) UpdateStatusTx(ctx context.Context, tx *sql.Tx, id string, status models.ProposalStatus) error {
	return r.updateStatusWith(ctx, tx, id, status)
}

// CreateTx creates a new blocklist proposal within a transaction
func (r *BlocklistProposalRepository) CreateTx(ctx context.Context, tx *sql.Tx, proposal *models.BlocklistProposal) error {
	proposal.ID = uuid.New().String()
	proposal.CreatedAt = time.Now().UTC()
	proposal.ExpiresAt = proposal.CreatedAt.Add(7 * 24 * time.Hour)
	proposal.Status = models.ProposalStatusPending

	query := `
		INSERT INTO blocklist_proposals
		(id, target_user_id, region_id, proposed_by, reason, evidence, status, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := tx.ExecContext(ctx, query,
		proposal.ID,
		proposal.TargetUserID,
		proposal.RegionID,
		proposal.ProposedBy,
		proposal.Reason,
		proposal.Evidence,
		proposal.Status,
		proposal.CreatedAt,
		proposal.ExpiresAt,
	)

	return err
}

// ApplyBlocklistTx applies the blocklist to a user within an existing transaction
// This avoids nested transactions when called from within a handler's transaction
func (r *BlocklistProposalRepository) ApplyBlocklistTx(ctx context.Context, tx *sql.Tx, userID, regionID, proposalID, blockedBy, reason string) error {
	now := time.Now().UTC()

	// 1. Set global block flag on user
	_, err := tx.ExecContext(ctx, `
		UPDATE users SET is_blocked = TRUE, blocked_at = ?, blocked_by = ?, block_reason = ?
		WHERE id = ?
	`, now, blockedBy, reason, userID)
	if err != nil {
		return err
	}

	// 2. Remove user from ALL regions
	_, err = tx.ExecContext(ctx, `DELETE FROM user_regions WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}

	// 3. Track in blocked_users table
	blocklistID := uuid.New().String()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO blocked_users (id, user_id, region_id, proposal_id, blocked_at)
		VALUES (?, ?, ?, ?, ?)
	`, blocklistID, userID, regionID, proposalID, now)
	if err != nil {
		return err
	}

	// 4. Block the user's verified address (if they have one)
	var addressHash *string
	err = tx.QueryRowContext(ctx, `SELECT address_hash FROM users WHERE id = ?`, userID).Scan(&addressHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	if addressHash != nil && *addressHash != "" {
		expiresAt := now.Add(r.blocklistConfig.AddressBlocklistDuration)

		addressBlocklistID := uuid.New().String()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO blocked_addresses (id, address_hash, user_id, proposal_id, blocked_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				expires_at = VALUES(expires_at),
				proposal_id = VALUES(proposal_id)
		`, addressBlocklistID, *addressHash, userID, proposalID, now, expiresAt)
		if err != nil {
			return err
		}
	}

	return nil
}

// IsUserInRegion checks if a user is a member of a specific region
func (r *BlocklistProposalRepository) IsUserInRegion(ctx context.Context, userID, regionID string) (bool, error) {
	// Check if user is a member of the region OR any of its child regions
	var count int
	err := r.db.QueryRowContext(ctx, `
		WITH RECURSIVE region_tree AS (
			-- Base case: the target region
			SELECT id FROM geographic_regions WHERE id = ?
			UNION ALL
			-- Recursive case: all child regions
			SELECT gr.id
			FROM geographic_regions gr
			INNER JOIN region_tree rt ON gr.parent_region_id = rt.id
		)
		SELECT COUNT(*) FROM user_regions ur
		WHERE ur.user_id = ? AND ur.region_id IN (SELECT id FROM region_tree)
	`, regionID, userID).Scan(&count)
	return count > 0, err
}
