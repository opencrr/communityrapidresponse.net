package database

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	gosentry "github.com/getsentry/sentry-go"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
)

// SecretUpdateProposalRepository handles secret update proposal operations
type SecretUpdateProposalRepository struct {
	db *DB
}

// NewSecretUpdateProposalRepository creates a new secret update proposal repository
func NewSecretUpdateProposalRepository(db *DB) *SecretUpdateProposalRepository {
	return &SecretUpdateProposalRepository{db: db}
}

// CreateTx creates a new proposal with admin-scoped wrapped keys within a transaction
func (r *SecretUpdateProposalRepository) CreateTx(ctx context.Context, tx *sql.Tx, proposal *models.SecretUpdateProposal, wrappedKeys []models.WrappedKeyEntry) error {
	proposal.ID = uuid.New().String()
	proposal.CreatedAt = time.Now().UTC()
	proposal.ExpiresAt = proposal.CreatedAt.Add(7 * 24 * time.Hour)
	proposal.Status = models.ProposalStatusPending

	query := `
		INSERT INTO secret_update_proposals
		(id, encrypted_secret_id, region_id, school_id, district_id, proposed_by,
		 encrypted_payload, encryption_iv, reason, status, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := tx.ExecContext(ctx, query,
		proposal.ID, proposal.EncryptedSecretID,
		proposal.RegionID, proposal.SchoolID, proposal.DistrictID,
		proposal.ProposedBy, proposal.EncryptedPayload, proposal.EncryptionIV,
		proposal.Reason, proposal.Status, proposal.CreatedAt, proposal.ExpiresAt,
	)
	if err != nil {
		return err
	}

	// Insert admin-scoped wrapped keys
	keyQuery := `
		INSERT INTO proposal_wrapped_keys (proposal_id, user_id, wrapped_dek, created_at)
		VALUES (?, ?, ?, ?)
	`
	now := time.Now().UTC()
	for _, wk := range wrappedKeys {
		_, err := tx.ExecContext(ctx, keyQuery, proposal.ID, wk.UserID, wk.WrappedDEK, now)
		if err != nil {
			return err
		}
	}

	return nil
}

// GetByID retrieves a proposal by ID
func (r *SecretUpdateProposalRepository) GetByID(ctx context.Context, id string) (*models.SecretUpdateProposal, error) {
	query := `
		SELECT id, encrypted_secret_id, region_id, school_id, district_id, proposed_by,
			encrypted_payload, encryption_iv, reason, status, created_at, expires_at,
			resolved_at, finalized_at
		FROM secret_update_proposals
		WHERE id = ?
	`
	return r.scanProposal(r.db.QueryRowContext(ctx, query, id))
}

// GetByIDForUpdate locks and retrieves a proposal within a transaction
func (r *SecretUpdateProposalRepository) GetByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (*models.SecretUpdateProposal, error) {
	query := `
		SELECT id, encrypted_secret_id, region_id, school_id, district_id, proposed_by,
			encrypted_payload, encryption_iv, reason, status, created_at, expires_at,
			resolved_at, finalized_at
		FROM secret_update_proposals
		WHERE id = ?
		FOR UPDATE
	`
	return r.scanProposal(tx.QueryRowContext(ctx, query, id))
}

// GetPendingBySecretForUpdate retrieves any pending proposal for a secret with a row lock
func (r *SecretUpdateProposalRepository) GetPendingBySecretForUpdate(ctx context.Context, tx *sql.Tx, secretID string) (*models.SecretUpdateProposal, error) {
	query := `
		SELECT id, encrypted_secret_id, region_id, school_id, district_id, proposed_by,
			encrypted_payload, encryption_iv, reason, status, created_at, expires_at,
			resolved_at, finalized_at
		FROM secret_update_proposals
		WHERE encrypted_secret_id = ? AND status IN ('pending', 'approved_pending_finalization') AND expires_at > NOW()
		FOR UPDATE
	`
	proposal, err := r.scanProposal(tx.QueryRowContext(ctx, query, secretID))
	if errors.Is(err, ErrSecretProposalNotFound) {
		return nil, nil
	}
	return proposal, err
}

// AddVoteTx adds a vote within a transaction
func (r *SecretUpdateProposalRepository) AddVoteTx(ctx context.Context, tx *sql.Tx, proposalID, voterID string, vote bool) error {
	id := uuid.New().String()
	query := `
		INSERT INTO secret_update_votes (id, proposal_id, voter_id, vote, voted_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := tx.ExecContext(ctx, query, id, proposalID, voterID, vote, time.Now().UTC())
	return err
}

// HasVotedTx checks if a user has voted within a transaction
func (r *SecretUpdateProposalRepository) HasVotedTx(ctx context.Context, tx *sql.Tx, proposalID, voterID string) (bool, error) {
	query := `SELECT COUNT(*) > 0 FROM secret_update_votes WHERE proposal_id = ? AND voter_id = ?`
	var exists bool
	err := tx.QueryRowContext(ctx, query, proposalID, voterID).Scan(&exists)
	return exists, err
}

// CountApprovalVotesTx counts approval votes within a transaction
func (r *SecretUpdateProposalRepository) CountApprovalVotesTx(ctx context.Context, tx *sql.Tx, proposalID string) (int, error) {
	query := `SELECT COUNT(*) FROM secret_update_votes WHERE proposal_id = ? AND vote = TRUE`
	var count int
	err := tx.QueryRowContext(ctx, query, proposalID).Scan(&count)
	return count, err
}

// UpdateStatusTx updates proposal status within a transaction.
// Only terminal statuses (approved, rejected, expired) set resolved_at.
func (r *SecretUpdateProposalRepository) UpdateStatusTx(ctx context.Context, tx *sql.Tx, id string, status models.ProposalStatus) error {
	if status == models.ProposalStatusApprovedPendingFinalization {
		query := `UPDATE secret_update_proposals SET status = ? WHERE id = ?`
		_, err := tx.ExecContext(ctx, query, status, id)
		return err
	}
	now := time.Now().UTC()
	query := `UPDATE secret_update_proposals SET status = ?, resolved_at = ? WHERE id = ?`
	_, err := tx.ExecContext(ctx, query, status, now, id)
	return err
}

// UpdateStatus updates proposal status.
// Only terminal statuses (approved, rejected, expired) set resolved_at.
func (r *SecretUpdateProposalRepository) UpdateStatus(ctx context.Context, id string, status models.ProposalStatus) error {
	if status == models.ProposalStatusApprovedPendingFinalization {
		query := `UPDATE secret_update_proposals SET status = ? WHERE id = ?`
		_, err := r.db.ExecContext(ctx, query, status, id)
		return err
	}
	now := time.Now().UTC()
	query := `UPDATE secret_update_proposals SET status = ?, resolved_at = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, status, now, id)
	return err
}

// MarkFinalized marks a proposal as finalized
func (r *SecretUpdateProposalRepository) MarkFinalized(ctx context.Context, id string) error {
	now := time.Now().UTC()
	query := `UPDATE secret_update_proposals SET finalized_at = ?, status = 'approved' WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, now, id)
	return err
}

// GetWrappedDEK retrieves an admin's wrapped DEK for a proposal
func (r *SecretUpdateProposalRepository) GetWrappedDEK(ctx context.Context, proposalID, userID string) (string, error) {
	query := `SELECT wrapped_dek FROM proposal_wrapped_keys WHERE proposal_id = ? AND user_id = ?`
	var wrappedDEK string
	err := r.db.QueryRowContext(ctx, query, proposalID, userID).Scan(&wrappedDEK)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrWrappedKeyNotFound
	}
	return wrappedDEK, err
}

// GetByIDWithVotes returns proposal with all vote details
func (r *SecretUpdateProposalRepository) GetByIDWithVotes(ctx context.Context, id string) (*models.SecretProposalDetailResponse, error) {
	query := `
		SELECT p.id, p.encrypted_secret_id, es.secret_type,
			COALESCE(gr.name, s.name, sd.name, '') AS scope_name,
			COALESCE(sg.group_name, '') AS group_name,
			p.proposed_by, COALESCE(u.username, '') AS username,
			p.encrypted_payload, p.encryption_iv, p.reason,
			p.status, p.created_at, p.expires_at, p.resolved_at, p.finalized_at
		FROM secret_update_proposals p
		JOIN encrypted_secrets es ON p.encrypted_secret_id = es.id
		LEFT JOIN signal_groups sg ON es.signal_group_id = sg.id
		LEFT JOIN geographic_regions gr ON p.region_id = gr.id
		LEFT JOIN schools s ON p.school_id = s.id
		LEFT JOIN school_districts sd ON p.district_id = sd.id
		LEFT JOIN users u ON p.proposed_by = u.id
		WHERE p.id = ?
	`

	var reason sql.NullString
	proposal := &models.SecretProposalDetailResponse{}

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&proposal.ID, &proposal.EncryptedSecretID, &proposal.SecretType,
		&proposal.ScopeName, &proposal.GroupName,
		&proposal.ProposedBy.ID, &proposal.ProposedBy.Username,
		&proposal.EncryptedPayload, &proposal.EncryptionIV, &reason,
		&proposal.Status, &proposal.CreatedAt, &proposal.ExpiresAt,
		&proposal.ResolvedAt, &proposal.FinalizedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSecretProposalNotFound
	}
	if err != nil {
		return nil, err
	}

	if reason.Valid {
		proposal.Reason = reason.String
	}

	// Fetch votes
	votesQuery := `
		SELECT v.voter_id, u.username, v.vote, v.voted_at
		FROM secret_update_votes v
		JOIN users u ON v.voter_id = u.id
		WHERE v.proposal_id = ?
		ORDER BY v.voted_at
	`
	rows, err := r.db.QueryContext(ctx, votesQuery, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	proposal.Votes = []models.SecretVoteDetail{}
	for rows.Next() {
		var vote models.SecretVoteDetail
		if err := rows.Scan(&vote.VoterID, &vote.Username, &vote.Vote, &vote.VotedAt); err != nil {
			return nil, err
		}
		proposal.Votes = append(proposal.Votes, vote)
		if vote.Vote {
			proposal.CurrentVotes++
		}
	}
	return proposal, rows.Err()
}

// List returns proposals filtered by scope and status
func (r *SecretUpdateProposalRepository) List(ctx context.Context, userID string, isSuperuser bool, filter models.SecretProposalListFilter) ([]*models.SecretProposalSummary, error) {
	var query string
	var args []interface{}

	if isSuperuser {
		query = `
			SELECT p.id, p.encrypted_secret_id, es.secret_type,
				COALESCE(gr.name, s.name, sd.name, '') AS scope_name,
				COALESCE(sg.group_name, '') AS group_name,
				COALESCE(u.username, '') AS proposed_by, p.reason,
				(SELECT COUNT(*) FROM secret_update_votes v WHERE v.proposal_id = p.id AND v.vote = TRUE) AS votes,
				p.status, p.created_at, p.expires_at
			FROM secret_update_proposals p
			JOIN encrypted_secrets es ON p.encrypted_secret_id = es.id
			LEFT JOIN signal_groups sg ON es.signal_group_id = sg.id
			LEFT JOIN geographic_regions gr ON p.region_id = gr.id
			LEFT JOIN schools s ON p.school_id = s.id
			LEFT JOIN school_districts sd ON p.district_id = sd.id
			LEFT JOIN users u ON p.proposed_by = u.id
			WHERE 1=1
		`
	} else {
		// Non-superuser: only proposals for scopes they admin
		query = `
			SELECT DISTINCT p.id, p.encrypted_secret_id, es.secret_type,
				COALESCE(gr.name, s.name, sd.name, '') AS scope_name,
				COALESCE(sg.group_name, '') AS group_name,
				COALESCE(u.username, '') AS proposed_by, p.reason,
				(SELECT COUNT(*) FROM secret_update_votes v WHERE v.proposal_id = p.id AND v.vote = TRUE) AS votes,
				p.status, p.created_at, p.expires_at
			FROM secret_update_proposals p
			JOIN encrypted_secrets es ON p.encrypted_secret_id = es.id
			LEFT JOIN signal_groups sg ON es.signal_group_id = sg.id
			LEFT JOIN geographic_regions gr ON p.region_id = gr.id
			LEFT JOIN schools s ON p.school_id = s.id
			LEFT JOIN school_districts sd ON p.district_id = sd.id
			LEFT JOIN users u ON p.proposed_by = u.id
			LEFT JOIN user_regions ur ON p.region_id = ur.region_id AND ur.user_id = ? AND ur.is_admin = TRUE
			LEFT JOIN user_schools us_s ON p.school_id = us_s.school_id AND us_s.user_id = ? AND us_s.is_admin = TRUE AND us_s.verification_status = 'verified'
			LEFT JOIN schools ds ON p.district_id = ds.district_id
			LEFT JOIN user_schools us_d ON ds.id = us_d.school_id AND us_d.user_id = ? AND us_d.is_admin = TRUE AND us_d.verification_status = 'verified'
			WHERE (ur.user_id IS NOT NULL OR us_s.user_id IS NOT NULL OR us_d.user_id IS NOT NULL)
		`
		args = append(args, userID, userID, userID)
	}

	if filter.Status != "" {
		query += " AND p.status = ?"
		args = append(args, filter.Status)
	}
	if filter.EncryptedSecretID != "" {
		query += " AND p.encrypted_secret_id = ?"
		args = append(args, filter.EncryptedSecretID)
	}
	if filter.RegionID != "" {
		query += " AND p.region_id = ?"
		args = append(args, filter.RegionID)
	}
	if filter.SchoolID != "" {
		query += " AND p.school_id = ?"
		args = append(args, filter.SchoolID)
	}
	if filter.DistrictID != "" {
		query += " AND p.district_id = ?"
		args = append(args, filter.DistrictID)
	}

	query += " ORDER BY p.created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	proposals := []*models.SecretProposalSummary{}
	for rows.Next() {
		p := &models.SecretProposalSummary{}
		var proposedBy sql.NullString
		if err := rows.Scan(
			&p.ID, &p.EncryptedSecretID, &p.SecretType,
			&p.ScopeName, &p.GroupName,
			&proposedBy, &p.Reason,
			&p.Votes, &p.Status, &p.CreatedAt, &p.ExpiresAt,
		); err != nil {
			return nil, err
		}
		p.ProposedBy = proposedBy.String
		proposals = append(proposals, p)
	}
	return proposals, rows.Err()
}

// ExpirePendingProposals marks expired proposals
func (r *SecretUpdateProposalRepository) ExpirePendingProposals(ctx context.Context) (int64, error) {
	query := `
		UPDATE secret_update_proposals
		SET status = 'expired', resolved_at = NOW()
		WHERE status IN ('pending', 'approved_pending_finalization') AND expires_at < NOW()
	`
	result, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// StartExpirationWorker starts a background goroutine that periodically expires old proposals
func (r *SecretUpdateProposalRepository) StartExpirationWorker(ctx context.Context, interval time.Duration) {
	monitorSlug := "crr-secret-proposal-expiration"
	intervalMinutes := int(interval.Minutes())
	if intervalMinutes < 1 {
		intervalMinutes = 1
	}
	monitorConfig := appSentry.IntervalSchedule(intervalMinutes, gosentry.MonitorScheduleUnitMinute)

	runExpiration := func() {
		checkInID := appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusInProgress, nil, monitorConfig)
		if expired, err := r.ExpirePendingProposals(ctx); err != nil {
			slog.Error("secret proposal expiration failed", "error", err)
			_ = appSentry.CaptureError(err, "component", "secret_proposal_worker", "operation", "expire_proposals")
			appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusError, checkInID, monitorConfig)
		} else {
			if expired > 0 {
				slog.Info("expired secret update proposals", "count", expired)
			}
			appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusOK, checkInID, monitorConfig)
		}
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		runExpiration()

		for {
			select {
			case <-ctx.Done():
				slog.Info("secret proposal expiration worker stopped")
				return
			case <-ticker.C:
				runExpiration()
			}
		}
	}()

	slog.Info("secret proposal expiration worker started", "interval", interval)
}

// scanProposal scans a single proposal row
func (r *SecretUpdateProposalRepository) scanProposal(row *sql.Row) (*models.SecretUpdateProposal, error) {
	proposal := &models.SecretUpdateProposal{}
	err := row.Scan(
		&proposal.ID, &proposal.EncryptedSecretID,
		&proposal.RegionID, &proposal.SchoolID, &proposal.DistrictID,
		&proposal.ProposedBy, &proposal.EncryptedPayload, &proposal.EncryptionIV,
		&proposal.Reason, &proposal.Status,
		&proposal.CreatedAt, &proposal.ExpiresAt, &proposal.ResolvedAt, &proposal.FinalizedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSecretProposalNotFound
	}
	if err != nil {
		return nil, err
	}
	return proposal, nil
}
