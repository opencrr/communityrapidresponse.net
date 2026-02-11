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
	ErrMembershipRequestNotFound     = errors.New("membership request not found")
	ErrDuplicatePendingRequest       = errors.New("a pending request already exists for this user and region")
	ErrUserAlreadyInRegion           = errors.New("user is already a member of this region")
	ErrUserNotInParentRegion         = errors.New("user is not a member of the parent region")
	ErrRegionNotSubRegion            = errors.New("target region is not a sub-region")
	ErrMaxPendingRequestsReached     = errors.New("maximum pending requests limit reached")
	ErrAlreadyVoted                  = errors.New("user has already voted on this request")
)

const (
	// RequestExpirationDays is the number of days before a request expires
	RequestExpirationDays = 7
	// MaxPendingRequestsPerUser is the maximum number of pending requests a user can have
	MaxPendingRequestsPerUser = 5
	// RequiredVotesForApproval is the number of admin votes needed to approve a request
	RequiredVotesForApproval = 2
)

// MembershipRepository handles sub-region membership request operations
type MembershipRepository struct {
	db *DB
}

// NewMembershipRepository creates a new membership repository
func NewMembershipRepository(db *DB) *MembershipRepository {
	return &MembershipRepository{db: db}
}

// Create creates a new membership request
func (r *MembershipRepository) Create(ctx context.Context, req *models.SubRegionMembershipRequest) error {
	req.ID = uuid.New().String()
	req.CreatedAt = time.Now().UTC()
	req.ExpiresAt = req.CreatedAt.Add(RequestExpirationDays * 24 * time.Hour)
	req.Status = models.MembershipRequestStatusPending

	query := `
		INSERT INTO sub_region_membership_requests
		(id, user_id, region_id, parent_region_id, request_type, status, initiated_by, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		req.ID,
		req.UserID,
		req.RegionID,
		req.ParentRegionID,
		req.RequestType,
		req.Status,
		req.InitiatedBy,
		req.CreatedAt,
		req.ExpiresAt,
	)

	return err
}

// GetByID retrieves a membership request by ID
func (r *MembershipRepository) GetByID(ctx context.Context, id string) (*models.SubRegionMembershipRequest, error) {
	query := `
		SELECT id, user_id, region_id, parent_region_id, request_type, status,
			initiated_by, created_at, expires_at, resolved_at
		FROM sub_region_membership_requests
		WHERE id = ?
	`

	req := &models.SubRegionMembershipRequest{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&req.ID,
		&req.UserID,
		&req.RegionID,
		&req.ParentRegionID,
		&req.RequestType,
		&req.Status,
		&req.InitiatedBy,
		&req.CreatedAt,
		&req.ExpiresAt,
		&req.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMembershipRequestNotFound
	}
	if err != nil {
		return nil, err
	}

	return req, nil
}

// GetByIDWithVotes retrieves a membership request with vote details
func (r *MembershipRepository) GetByIDWithVotes(ctx context.Context, id string) (*models.MembershipRequestDetailResponse, error) {
	query := `
		SELECT r.id, r.user_id, u.username, r.region_id, gr.name,
			r.parent_region_id, pgr.name,
			r.request_type, r.initiated_by, iu.username,
			r.status, r.created_at, r.expires_at, r.resolved_at
		FROM sub_region_membership_requests r
		JOIN users u ON r.user_id = u.id
		JOIN geographic_regions gr ON r.region_id = gr.id
		JOIN geographic_regions pgr ON r.parent_region_id = pgr.id
		LEFT JOIN users iu ON r.initiated_by = iu.id
		WHERE r.id = ?
	`

	var initiatedByID, initiatedByUsername sql.NullString
	resp := &models.MembershipRequestDetailResponse{}

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&resp.ID,
		&resp.UserID,
		&resp.Username,
		&resp.RegionID,
		&resp.RegionName,
		&resp.ParentRegionID,
		&resp.ParentRegionName,
		&resp.RequestType,
		&initiatedByID,
		&initiatedByUsername,
		&resp.Status,
		&resp.CreatedAt,
		&resp.ExpiresAt,
		&resp.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMembershipRequestNotFound
	}
	if err != nil {
		return nil, err
	}

	resp.InitiatedBy = models.ProposalUser{
		ID:       initiatedByID.String,
		Username: initiatedByUsername.String,
	}

	// Fetch votes
	votesQuery := `
		SELECT v.voter_id, u.username, v.vote, v.voted_at
		FROM sub_region_membership_votes v
		JOIN users u ON v.voter_id = u.id
		WHERE v.request_id = ?
		ORDER BY v.voted_at
	`

	rows, err := r.db.QueryContext(ctx, votesQuery, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	resp.Votes = []models.MembershipVoteDetail{}
	for rows.Next() {
		var vote models.MembershipVoteDetail
		if err := rows.Scan(&vote.VoterID, &vote.Username, &vote.Vote, &vote.VotedAt); err != nil {
			return nil, err
		}
		resp.Votes = append(resp.Votes, vote)
		if vote.Vote {
			resp.CurrentVotes++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	resp.VotesNeeded = RequiredVotesForApproval

	return resp, nil
}

// GetPendingByUserAndRegion checks if there's already a pending request for a user/region pair
func (r *MembershipRepository) GetPendingByUserAndRegion(ctx context.Context, userID, regionID string) (*models.SubRegionMembershipRequest, error) {
	query := `
		SELECT id, user_id, region_id, parent_region_id, request_type, status,
			initiated_by, created_at, expires_at, resolved_at
		FROM sub_region_membership_requests
		WHERE user_id = ? AND region_id = ? AND status = 'pending' AND expires_at > NOW()
	`

	req := &models.SubRegionMembershipRequest{}
	err := r.db.QueryRowContext(ctx, query, userID, regionID).Scan(
		&req.ID,
		&req.UserID,
		&req.RegionID,
		&req.ParentRegionID,
		&req.RequestType,
		&req.Status,
		&req.InitiatedBy,
		&req.CreatedAt,
		&req.ExpiresAt,
		&req.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return req, nil
}

// CountPendingByUser counts pending requests for a user
func (r *MembershipRepository) CountPendingByUser(ctx context.Context, userID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM sub_region_membership_requests
		WHERE user_id = ? AND status = 'pending' AND expires_at > NOW()
	`
	var count int
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&count)
	return count, err
}

// ListPendingByRegion lists pending requests for a specific region
func (r *MembershipRepository) ListPendingByRegion(ctx context.Context, regionID string) ([]models.MembershipRequestSummary, error) {
	query := `
		SELECT r.id, r.user_id, u.username, r.region_id, gr.name,
			r.request_type, r.status,
			(SELECT COUNT(*) FROM sub_region_membership_votes v WHERE v.request_id = r.id AND v.vote = TRUE) as votes,
			r.created_at, r.expires_at
		FROM sub_region_membership_requests r
		JOIN users u ON r.user_id = u.id
		JOIN geographic_regions gr ON r.region_id = gr.id
		WHERE r.region_id = ? AND r.status = 'pending' AND r.expires_at > NOW()
		ORDER BY r.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, regionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	requests := []models.MembershipRequestSummary{}
	for rows.Next() {
		var req models.MembershipRequestSummary
		if err := rows.Scan(
			&req.ID,
			&req.UserID,
			&req.Username,
			&req.RegionID,
			&req.RegionName,
			&req.RequestType,
			&req.Status,
			&req.Votes,
			&req.CreatedAt,
			&req.ExpiresAt,
		); err != nil {
			return nil, err
		}
		req.VotesNeeded = RequiredVotesForApproval
		requests = append(requests, req)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return requests, nil
}

// ListPendingForAdminUser lists pending requests for all regions where user is an admin
func (r *MembershipRepository) ListPendingForAdminUser(ctx context.Context, userID string) ([]models.MembershipRequestSummary, error) {
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
		SELECT DISTINCT r.id, r.user_id, u.username, r.region_id, gr.name,
			r.request_type, r.status,
			(SELECT COUNT(*) FROM sub_region_membership_votes v WHERE v.request_id = r.id AND v.vote = TRUE) as votes,
			r.created_at, r.expires_at
		FROM sub_region_membership_requests r
		JOIN users u ON r.user_id = u.id
		JOIN geographic_regions gr ON r.region_id = gr.id
		JOIN user_admin_regions uar ON r.region_id = uar.id
		WHERE r.status = 'pending' AND r.expires_at > NOW()
		ORDER BY r.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	requests := []models.MembershipRequestSummary{}
	for rows.Next() {
		var req models.MembershipRequestSummary
		if err := rows.Scan(
			&req.ID,
			&req.UserID,
			&req.Username,
			&req.RegionID,
			&req.RegionName,
			&req.RequestType,
			&req.Status,
			&req.Votes,
			&req.CreatedAt,
			&req.ExpiresAt,
		); err != nil {
			return nil, err
		}
		req.VotesNeeded = RequiredVotesForApproval
		requests = append(requests, req)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return requests, nil
}

// ListUserRequests lists all requests for a specific user (their own requests)
func (r *MembershipRepository) ListUserRequests(ctx context.Context, userID string, filter models.MembershipRequestListFilter) ([]models.MembershipRequestSummary, error) {
	query := `
		SELECT r.id, r.user_id, u.username, r.region_id, gr.name,
			r.request_type, r.status,
			(SELECT COUNT(*) FROM sub_region_membership_votes v WHERE v.request_id = r.id AND v.vote = TRUE) as votes,
			r.created_at, r.expires_at
		FROM sub_region_membership_requests r
		JOIN users u ON r.user_id = u.id
		JOIN geographic_regions gr ON r.region_id = gr.id
		WHERE r.user_id = ?
	`
	args := []interface{}{userID}

	if filter.Status != "" {
		query += " AND r.status = ?"
		args = append(args, filter.Status)
	}
	if filter.RequestType != "" {
		query += " AND r.request_type = ?"
		args = append(args, filter.RequestType)
	}

	query += " ORDER BY r.created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	requests := []models.MembershipRequestSummary{}
	for rows.Next() {
		var req models.MembershipRequestSummary
		if err := rows.Scan(
			&req.ID,
			&req.UserID,
			&req.Username,
			&req.RegionID,
			&req.RegionName,
			&req.RequestType,
			&req.Status,
			&req.Votes,
			&req.CreatedAt,
			&req.ExpiresAt,
		); err != nil {
			return nil, err
		}
		req.VotesNeeded = RequiredVotesForApproval
		requests = append(requests, req)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return requests, nil
}

// ListInvitationsForUser lists pending invitations for a user
func (r *MembershipRepository) ListInvitationsForUser(ctx context.Context, userID string) ([]models.MembershipRequestSummary, error) {
	query := `
		SELECT r.id, r.user_id, u.username, r.region_id, gr.name,
			r.request_type, r.status,
			0 as votes,
			r.created_at, r.expires_at
		FROM sub_region_membership_requests r
		JOIN users u ON r.user_id = u.id
		JOIN geographic_regions gr ON r.region_id = gr.id
		WHERE r.user_id = ? AND r.request_type = 'invitation' AND r.status = 'pending' AND r.expires_at > NOW()
		ORDER BY r.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	requests := []models.MembershipRequestSummary{}
	for rows.Next() {
		var req models.MembershipRequestSummary
		if err := rows.Scan(
			&req.ID,
			&req.UserID,
			&req.Username,
			&req.RegionID,
			&req.RegionName,
			&req.RequestType,
			&req.Status,
			&req.Votes,
			&req.CreatedAt,
			&req.ExpiresAt,
		); err != nil {
			return nil, err
		}
		req.VotesNeeded = 0 // Invitations don't need votes
		requests = append(requests, req)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return requests, nil
}

// AddVote adds a vote to a membership request
func (r *MembershipRepository) AddVote(ctx context.Context, requestID, voterID string, vote bool) error {
	id := uuid.New().String()
	query := `
		INSERT INTO sub_region_membership_votes (id, request_id, voter_id, vote, voted_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := r.db.ExecContext(ctx, query, id, requestID, voterID, vote, time.Now().UTC())
	return err
}

// HasVoted checks if a user has already voted on a request
func (r *MembershipRepository) HasVoted(ctx context.Context, requestID, voterID string) (bool, error) {
	query := `SELECT COUNT(*) > 0 FROM sub_region_membership_votes WHERE request_id = ? AND voter_id = ?`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, requestID, voterID).Scan(&exists)
	return exists, err
}

// CountApprovalVotes counts approval votes for a request
func (r *MembershipRepository) CountApprovalVotes(ctx context.Context, requestID string) (int, error) {
	query := `SELECT COUNT(*) FROM sub_region_membership_votes WHERE request_id = ? AND vote = TRUE`
	var count int
	err := r.db.QueryRowContext(ctx, query, requestID).Scan(&count)
	return count, err
}

// UpdateStatus updates the status of a membership request
func (r *MembershipRepository) UpdateStatus(ctx context.Context, id string, status models.MembershipRequestStatus) error {
	now := time.Now().UTC()
	query := `UPDATE sub_region_membership_requests SET status = ?, resolved_at = ? WHERE id = ?`
	result, err := r.db.ExecContext(ctx, query, status, now, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrMembershipRequestNotFound
	}

	return nil
}

// Cancel cancels a pending request (only by the request owner)
func (r *MembershipRepository) Cancel(ctx context.Context, id, userID string) error {
	now := time.Now().UTC()
	query := `
		UPDATE sub_region_membership_requests
		SET status = 'cancelled', resolved_at = ?
		WHERE id = ? AND user_id = ? AND status = 'pending'
	`
	result, err := r.db.ExecContext(ctx, query, now, id, userID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrMembershipRequestNotFound
	}

	return nil
}

// ExpirePendingRequests marks expired requests and returns count
func (r *MembershipRepository) ExpirePendingRequests(ctx context.Context) (int64, error) {
	query := `
		UPDATE sub_region_membership_requests
		SET status = 'expired', resolved_at = NOW()
		WHERE status = 'pending' AND expires_at < NOW()
	`
	result, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// StartExpirationWorker starts a background goroutine that periodically expires old requests
func (r *MembershipRepository) StartExpirationWorker(ctx context.Context, interval time.Duration) {
	monitorSlug := "crr-membership-expiration"
	intervalMinutes := int(interval.Minutes())
	if intervalMinutes < 1 {
		intervalMinutes = 1
	}
	monitorConfig := appSentry.IntervalSchedule(intervalMinutes, gosentry.MonitorScheduleUnitMinute)

	runExpiration := func() {
		checkInID := appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusInProgress, nil, monitorConfig)
		if expired, err := r.ExpirePendingRequests(ctx); err != nil {
			log.Printf("ERROR: Membership request expiration failed: %v", err)
			_ = appSentry.CaptureError(err, "component", "membership_worker", "operation", "expire_requests")
			appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusError, checkInID, monitorConfig)
		} else {
			if expired > 0 {
				log.Printf("INFO: Expired %d membership requests", expired)
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
				log.Println("INFO: Membership request expiration worker stopped")
				return
			case <-ticker.C:
				runExpiration()
			}
		}
	}()

	log.Printf("INFO: Membership request expiration worker started (interval: %v)", interval)
}

// --- Transactional methods for race condition prevention ---

// GetByIDForUpdate locks and retrieves a membership request within a transaction
func (r *MembershipRepository) GetByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (*models.SubRegionMembershipRequest, error) {
	query := `
		SELECT id, user_id, region_id, parent_region_id, request_type, status,
			initiated_by, created_at, expires_at, resolved_at
		FROM sub_region_membership_requests
		WHERE id = ?
		FOR UPDATE
	`

	req := &models.SubRegionMembershipRequest{}
	err := tx.QueryRowContext(ctx, query, id).Scan(
		&req.ID,
		&req.UserID,
		&req.RegionID,
		&req.ParentRegionID,
		&req.RequestType,
		&req.Status,
		&req.InitiatedBy,
		&req.CreatedAt,
		&req.ExpiresAt,
		&req.ResolvedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMembershipRequestNotFound
	}
	if err != nil {
		return nil, err
	}

	return req, nil
}

// hasVotedWith is the shared implementation
func (r *MembershipRepository) hasVotedWith(ctx context.Context, q Querier, requestID, voterID string) (bool, error) {
	query := `SELECT COUNT(*) > 0 FROM sub_region_membership_votes WHERE request_id = ? AND voter_id = ?`
	var exists bool
	err := q.QueryRowContext(ctx, query, requestID, voterID).Scan(&exists)
	return exists, err
}

// HasVotedTx checks if a user has voted within a transaction
func (r *MembershipRepository) HasVotedTx(ctx context.Context, tx *sql.Tx, requestID, voterID string) (bool, error) {
	return r.hasVotedWith(ctx, tx, requestID, voterID)
}

// addVoteWith is the shared implementation
func (r *MembershipRepository) addVoteWith(ctx context.Context, q Querier, requestID, voterID string, vote bool) error {
	id := uuid.New().String()
	query := `
		INSERT INTO sub_region_membership_votes (id, request_id, voter_id, vote, voted_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := q.ExecContext(ctx, query, id, requestID, voterID, vote, time.Now().UTC())
	return err
}

// AddVoteTx adds a vote within a transaction
func (r *MembershipRepository) AddVoteTx(ctx context.Context, tx *sql.Tx, requestID, voterID string, vote bool) error {
	return r.addVoteWith(ctx, tx, requestID, voterID, vote)
}

// countApprovalVotesWith is the shared implementation
func (r *MembershipRepository) countApprovalVotesWith(ctx context.Context, q Querier, requestID string) (int, error) {
	query := `SELECT COUNT(*) FROM sub_region_membership_votes WHERE request_id = ? AND vote = TRUE`
	var count int
	err := q.QueryRowContext(ctx, query, requestID).Scan(&count)
	return count, err
}

// CountApprovalVotesTx counts approval votes within a transaction
func (r *MembershipRepository) CountApprovalVotesTx(ctx context.Context, tx *sql.Tx, requestID string) (int, error) {
	return r.countApprovalVotesWith(ctx, tx, requestID)
}

// updateStatusWith is the shared implementation
func (r *MembershipRepository) updateStatusWith(ctx context.Context, q Querier, id string, status models.MembershipRequestStatus) error {
	now := time.Now().UTC()
	query := `UPDATE sub_region_membership_requests SET status = ?, resolved_at = ? WHERE id = ?`
	result, err := q.ExecContext(ctx, query, status, now, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrMembershipRequestNotFound
	}

	return nil
}

// UpdateStatusTx updates the status of a membership request within a transaction
func (r *MembershipRepository) UpdateStatusTx(ctx context.Context, tx *sql.Tx, id string, status models.MembershipRequestStatus) error {
	return r.updateStatusWith(ctx, tx, id, status)
}
