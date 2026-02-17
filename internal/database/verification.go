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
	ErrVerificationNotFound = errors.New("verification request not found")
	ErrVerificationExpired  = errors.New("verification code expired")
	ErrInvalidCode          = errors.New("invalid verification code")
)

// VerificationRepository handles verification database operations
type VerificationRepository struct {
	db *DB
}

// NewVerificationRepository creates a new verification repository
func NewVerificationRepository(db *DB) *VerificationRepository {
	return &VerificationRepository{db: db}
}

// CreateVerificationRequest creates a new verification request
func (r *VerificationRepository) CreateVerificationRequest(ctx context.Context, req *models.VerificationRequest) error {
	req.ID = uuid.New().String()
	req.CreatedAt = time.Now().UTC()
	req.ExpiresAt = req.CreatedAt.Add(30 * 24 * time.Hour) // 30 days

	query := `
		INSERT INTO verification_requests
		(id, user_id, region_id, verification_code, status, postgrid_request_id,
		 boundary_type, boundary_name, boundary_state, postcard_ref, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		req.ID,
		req.UserID,
		req.RegionID,
		req.VerificationCode,
		req.Status,
		req.PostgridRequestID,
		req.BoundaryType,
		req.BoundaryName,
		req.BoundaryState,
		req.PostcardRef,
		req.CreatedAt,
		req.ExpiresAt,
	)

	return err
}

// GetByCode retrieves a verification request by code
func (r *VerificationRepository) GetByCode(ctx context.Context, code string) (*models.VerificationRequest, error) {
	query := `
		SELECT id, user_id, region_id, verification_code, status, postgrid_request_id,
			boundary_type, boundary_name, boundary_state, postcard_ref, created_at, expires_at, verified_at
		FROM verification_requests
		WHERE verification_code = ?
	`

	req := &models.VerificationRequest{}
	err := r.db.QueryRowContext(ctx, query, code).Scan(
		&req.ID,
		&req.UserID,
		&req.RegionID,
		&req.VerificationCode,
		&req.Status,
		&req.PostgridRequestID,
		&req.BoundaryType,
		&req.BoundaryName,
		&req.BoundaryState,
		&req.PostcardRef,
		&req.CreatedAt,
		&req.ExpiresAt,
		&req.VerifiedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVerificationNotFound
	}
	if err != nil {
		return nil, err
	}

	return req, nil
}

// GetPendingByUserID retrieves pending verification requests for a user
func (r *VerificationRepository) GetPendingByUserID(ctx context.Context, userID string) ([]models.VerificationRequest, error) {
	query := `
		SELECT id, user_id, region_id, verification_code, status, postgrid_request_id,
			boundary_type, boundary_name, boundary_state, postcard_ref, created_at, expires_at, verified_at
		FROM verification_requests
		WHERE user_id = ? AND status IN ('pending', 'mailed') AND expires_at > NOW()
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var requests []models.VerificationRequest
	for rows.Next() {
		var req models.VerificationRequest
		if err := rows.Scan(
			&req.ID,
			&req.UserID,
			&req.RegionID,
			&req.VerificationCode,
			&req.Status,
			&req.PostgridRequestID,
			&req.BoundaryType,
			&req.BoundaryName,
			&req.BoundaryState,
			&req.PostcardRef,
			&req.CreatedAt,
			&req.ExpiresAt,
			&req.VerifiedAt,
		); err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return requests, nil
}

// UpdatePostgridRequestID sets the postgrid_request_id after a postcard is sent
func (r *VerificationRepository) UpdatePostgridRequestID(ctx context.Context, id string, postgridRequestID string) error {
	query := `UPDATE verification_requests SET postgrid_request_id = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, postgridRequestID, id)
	return err
}

// UpdateStatus updates the status of a verification request
func (r *VerificationRepository) UpdateStatus(ctx context.Context, id string, status models.VerificationStatus) error {
	query := `UPDATE verification_requests SET status = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, status, id)
	return err
}

// MarkVerified marks a verification request as verified
func (r *VerificationRepository) MarkVerified(ctx context.Context, id string) error {
	now := time.Now().UTC()
	query := `UPDATE verification_requests SET status = 'verified', verified_at = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, now, id)
	return err
}

// Delete removes a verification request
func (r *VerificationRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM verification_requests WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// CountRecentByUser counts recent verification requests for rate limiting
func (r *VerificationRepository) CountRecentByUser(ctx context.Context, userID string, days int) (int, error) {
	query := `
		SELECT COUNT(*) FROM verification_requests
		WHERE user_id = ? AND created_at > DATE_SUB(NOW(), INTERVAL ? DAY)
	`
	var count int
	err := r.db.QueryRowContext(ctx, query, userID, days).Scan(&count)
	return count, err
}

// ExpireOldRequests marks old pending requests as expired
func (r *VerificationRepository) ExpireOldRequests(ctx context.Context) (int64, error) {
	query := `
		UPDATE verification_requests
		SET status = 'expired'
		WHERE status IN ('pending', 'mailed') AND expires_at < NOW()
	`
	result, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// --- Transactional methods for race condition prevention ---

// GetByCodeForUpdate locks and retrieves a verification request by code within a transaction
func (r *VerificationRepository) GetByCodeForUpdate(ctx context.Context, tx *sql.Tx, code string) (*models.VerificationRequest, error) {
	query := `
		SELECT id, user_id, region_id, verification_code, status, postgrid_request_id,
			boundary_type, boundary_name, boundary_state, postcard_ref, created_at, expires_at, verified_at
		FROM verification_requests
		WHERE verification_code = ?
		FOR UPDATE
	`

	req := &models.VerificationRequest{}
	err := tx.QueryRowContext(ctx, query, code).Scan(
		&req.ID,
		&req.UserID,
		&req.RegionID,
		&req.VerificationCode,
		&req.Status,
		&req.PostgridRequestID,
		&req.BoundaryType,
		&req.BoundaryName,
		&req.BoundaryState,
		&req.PostcardRef,
		&req.CreatedAt,
		&req.ExpiresAt,
		&req.VerifiedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVerificationNotFound
	}
	if err != nil {
		return nil, err
	}

	return req, nil
}

// CountRecentByUserForUpdate counts recent requests with a lock for rate limiting
func (r *VerificationRepository) CountRecentByUserForUpdate(ctx context.Context, tx *sql.Tx, userID string, days int) (int, error) {
	query := `
		SELECT COUNT(*) FROM verification_requests
		WHERE user_id = ? AND created_at > DATE_SUB(NOW(), INTERVAL ? DAY)
		FOR UPDATE
	`
	var count int
	err := tx.QueryRowContext(ctx, query, userID, days).Scan(&count)
	return count, err
}

// MarkVerifiedTx marks a verification request as verified within a transaction
func (r *VerificationRepository) MarkVerifiedTx(ctx context.Context, tx *sql.Tx, id string) error {
	now := time.Now().UTC()
	query := `UPDATE verification_requests SET status = 'verified', verified_at = ? WHERE id = ?`
	_, err := tx.ExecContext(ctx, query, now, id)
	return err
}

// CreateVerificationRequestTx creates a verification request within a transaction
func (r *VerificationRepository) CreateVerificationRequestTx(ctx context.Context, tx *sql.Tx, req *models.VerificationRequest) error {
	req.ID = uuid.New().String()
	req.CreatedAt = time.Now().UTC()
	req.ExpiresAt = req.CreatedAt.Add(30 * 24 * time.Hour)

	query := `
		INSERT INTO verification_requests
		(id, user_id, region_id, verification_code, status, postgrid_request_id,
		 boundary_type, boundary_name, boundary_state, postcard_ref, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := tx.ExecContext(ctx, query,
		req.ID,
		req.UserID,
		req.RegionID,
		req.VerificationCode,
		req.Status,
		req.PostgridRequestID,
		req.BoundaryType,
		req.BoundaryName,
		req.BoundaryState,
		req.PostcardRef,
		req.CreatedAt,
		req.ExpiresAt,
	)

	return err
}

// VouchRepository handles vouch database operations
type VouchRepository struct {
	db *DB
}

// NewVouchRepository creates a new vouch repository
func NewVouchRepository(db *DB) *VouchRepository {
	return &VouchRepository{db: db}
}

// Create creates a new vouch
func (r *VouchRepository) Create(ctx context.Context, vouch *models.Vouch) error {
	vouch.ID = uuid.New().String()
	vouch.CreatedAt = time.Now().UTC()

	query := `
		INSERT INTO vouches (id, voucher_user_id, vouched_user_id, region_id, created_at)
		VALUES (?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		vouch.ID,
		vouch.VoucherUserID,
		vouch.VouchedUserID,
		vouch.RegionID,
		vouch.CreatedAt,
	)

	return err
}

// CountVouchesForUser counts vouches received by a user for a region
func (r *VouchRepository) CountVouchesForUser(ctx context.Context, userID string, regionID string) (int, error) {
	query := `SELECT COUNT(*) FROM vouches WHERE vouched_user_id = ? AND region_id = ?`
	var count int
	err := r.db.QueryRowContext(ctx, query, userID, regionID).Scan(&count)
	return count, err
}

// GetVouchersForUser retrieves vouchers who have vouched for a user
func (r *VouchRepository) GetVouchersForUser(ctx context.Context, userID string, regionID string) ([]models.VoucherInfo, error) {
	query := `
		SELECT u.id, u.username, v.created_at
		FROM vouches v
		JOIN users u ON v.voucher_user_id = u.id
		WHERE v.vouched_user_id = ? AND v.region_id = ?
		ORDER BY v.created_at
	`

	rows, err := r.db.QueryContext(ctx, query, userID, regionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var vouchers []models.VoucherInfo
	for rows.Next() {
		var v models.VoucherInfo
		if err := rows.Scan(&v.UserID, &v.Username, &v.VouchedAt); err != nil {
			return nil, err
		}
		vouchers = append(vouchers, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return vouchers, nil
}

// HasVouched checks if a user has already vouched for another user
func (r *VouchRepository) HasVouched(ctx context.Context, voucherID, vouchedID string) (bool, error) {
	query := `SELECT COUNT(*) > 0 FROM vouches WHERE voucher_user_id = ? AND vouched_user_id = ?`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, voucherID, vouchedID).Scan(&exists)
	return exists, err
}

// CountVouchesThisMonth counts vouches given by a user this month
func (r *VouchRepository) CountVouchesThisMonth(ctx context.Context, voucherID string) (int, error) {
	query := `
		SELECT COUNT(*) FROM vouches
		WHERE voucher_user_id = ?
		AND MONTH(created_at) = MONTH(NOW())
		AND YEAR(created_at) = YEAR(NOW())
	`
	var count int
	err := r.db.QueryRowContext(ctx, query, voucherID).Scan(&count)
	return count, err
}

// IsUserBlockedInRegion checks if a user is blocked in a specific region
func (r *VouchRepository) IsUserBlockedInRegion(ctx context.Context, userID, regionID string) (bool, error) {
	query := `SELECT COUNT(*) > 0 FROM blocked_users WHERE user_id = ? AND region_id = ?`
	var blocked bool
	err := r.db.QueryRowContext(ctx, query, userID, regionID).Scan(&blocked)
	return blocked, err
}

// UsersShareRegion checks if two users share membership in any region
func (r *VouchRepository) UsersShareRegion(ctx context.Context, userID1, userID2 string) (bool, error) {
	query := `
		SELECT COUNT(*) > 0 FROM user_regions ur1
		JOIN user_regions ur2 ON ur1.region_id = ur2.region_id
		WHERE ur1.user_id = ? AND ur2.user_id = ?
	`
	var shared bool
	err := r.db.QueryRowContext(ctx, query, userID1, userID2).Scan(&shared)
	return shared, err
}

// GetLastVouchTimeByVoucher retrieves the most recent vouch time for a voucher
// Used for bootstrap mode cooldown enforcement
func (r *VouchRepository) GetLastVouchTimeByVoucher(ctx context.Context, voucherID string) (*time.Time, error) {
	query := `SELECT MAX(created_at) FROM vouches WHERE voucher_user_id = ?`
	var lastVouchTime sql.NullTime
	err := r.db.QueryRowContext(ctx, query, voucherID).Scan(&lastVouchTime)
	if err != nil {
		return nil, err
	}
	if !lastVouchTime.Valid {
		return nil, nil // No previous vouches
	}
	return &lastVouchTime.Time, nil
}

// CountVouchesForUserWithDescendants counts vouches at a region AND all its descendant regions.
// A vouch at region X counts for X and all ancestors. So counting at level Y means
// counting vouches at Y and all descendants.
func (r *VouchRepository) CountVouchesForUserWithDescendants(ctx context.Context, userID string, regionID string) (int, error) {
	query := `
		WITH RECURSIVE descendants AS (
			SELECT id FROM geographic_regions WHERE id = ?
			UNION ALL
			SELECT gr.id FROM geographic_regions gr
			JOIN descendants d ON gr.parent_region_id = d.id
		)
		SELECT COUNT(*) FROM vouches
		WHERE vouched_user_id = ? AND region_id IN (SELECT id FROM descendants)
	`
	var count int
	err := r.db.QueryRowContext(ctx, query, regionID, userID).Scan(&count)
	return count, err
}

// CountVouchesForUserWithDescendantsTx counts vouches with descendants within a transaction
func (r *VouchRepository) CountVouchesForUserWithDescendantsTx(ctx context.Context, tx *sql.Tx, userID string, regionID string) (int, error) {
	query := `
		WITH RECURSIVE descendants AS (
			SELECT id FROM geographic_regions WHERE id = ?
			UNION ALL
			SELECT gr.id FROM geographic_regions gr
			JOIN descendants d ON gr.parent_region_id = d.id
		)
		SELECT COUNT(*) FROM vouches
		WHERE vouched_user_id = ? AND region_id IN (SELECT id FROM descendants)
	`
	var count int
	err := tx.QueryRowContext(ctx, query, regionID, userID).Scan(&count)
	return count, err
}

// GetVouchersForUserWithDescendants retrieves vouchers who have vouched for a user
// at a region or any of its descendant regions
func (r *VouchRepository) GetVouchersForUserWithDescendants(ctx context.Context, userID string, regionID string) ([]models.VoucherInfo, error) {
	query := `
		WITH RECURSIVE descendants AS (
			SELECT id FROM geographic_regions WHERE id = ?
			UNION ALL
			SELECT gr.id FROM geographic_regions gr
			JOIN descendants d ON gr.parent_region_id = d.id
		)
		SELECT u.id, u.username, v.created_at
		FROM vouches v
		JOIN users u ON v.voucher_user_id = u.id
		WHERE v.vouched_user_id = ? AND v.region_id IN (SELECT id FROM descendants)
		ORDER BY v.created_at
	`

	rows, err := r.db.QueryContext(ctx, query, regionID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var vouchers []models.VoucherInfo
	for rows.Next() {
		var v models.VoucherInfo
		if err := rows.Scan(&v.UserID, &v.Username, &v.VouchedAt); err != nil {
			return nil, err
		}
		vouchers = append(vouchers, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return vouchers, nil
}

// --- Transactional methods for race condition prevention ---

// HasVouchedTx checks if a user has already vouched for another within a transaction
func (r *VouchRepository) HasVouchedTx(ctx context.Context, tx *sql.Tx, voucherID, vouchedID string) (bool, error) {
	query := `SELECT COUNT(*) > 0 FROM vouches WHERE voucher_user_id = ? AND vouched_user_id = ?`
	var exists bool
	err := tx.QueryRowContext(ctx, query, voucherID, vouchedID).Scan(&exists)
	return exists, err
}

// CountVouchesThisMonthForUpdate counts monthly vouches with a lock
func (r *VouchRepository) CountVouchesThisMonthForUpdate(ctx context.Context, tx *sql.Tx, voucherID string) (int, error) {
	query := `
		SELECT COUNT(*) FROM vouches
		WHERE voucher_user_id = ?
		AND MONTH(created_at) = MONTH(NOW())
		AND YEAR(created_at) = YEAR(NOW())
		FOR UPDATE
	`
	var count int
	err := tx.QueryRowContext(ctx, query, voucherID).Scan(&count)
	return count, err
}

// CountVouchesForUserTx counts vouches received by a user within a transaction
func (r *VouchRepository) CountVouchesForUserTx(ctx context.Context, tx *sql.Tx, userID string, regionID string) (int, error) {
	query := `SELECT COUNT(*) FROM vouches WHERE vouched_user_id = ? AND region_id = ?`
	var count int
	err := tx.QueryRowContext(ctx, query, userID, regionID).Scan(&count)
	return count, err
}

// CreateVouchTx creates a new vouch within a transaction
func (r *VouchRepository) CreateVouchTx(ctx context.Context, tx *sql.Tx, vouch *models.Vouch) error {
	vouch.ID = uuid.New().String()
	vouch.CreatedAt = time.Now().UTC()

	query := `
		INSERT INTO vouches (id, voucher_user_id, vouched_user_id, region_id, created_at)
		VALUES (?, ?, ?, ?, ?)
	`

	_, err := tx.ExecContext(ctx, query,
		vouch.ID,
		vouch.VoucherUserID,
		vouch.VouchedUserID,
		vouch.RegionID,
		vouch.CreatedAt,
	)

	return err
}
