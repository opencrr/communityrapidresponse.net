package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// SchoolVouchRepository handles school vouch database operations
type SchoolVouchRepository struct {
	db *DB
}

// NewSchoolVouchRepository creates a new school vouch repository
func NewSchoolVouchRepository(db *DB) *SchoolVouchRepository {
	return &SchoolVouchRepository{db: db}
}

// Create creates a new school vouch
func (r *SchoolVouchRepository) Create(ctx context.Context, vouch *models.SchoolVouch) error {
	vouch.ID = uuid.New().String()
	vouch.CreatedAt = time.Now().UTC()

	query := `
		INSERT INTO school_vouches (id, voucher_user_id, vouched_user_id, school_id, created_at)
		VALUES (?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		vouch.ID,
		vouch.VoucherUserID,
		vouch.VouchedUserID,
		vouch.SchoolID,
		vouch.CreatedAt,
	)

	return err
}

// CreateTx creates a new school vouch within a transaction
func (r *SchoolVouchRepository) CreateTx(ctx context.Context, tx *sql.Tx, vouch *models.SchoolVouch) error {
	vouch.ID = uuid.New().String()
	vouch.CreatedAt = time.Now().UTC()

	query := `
		INSERT INTO school_vouches (id, voucher_user_id, vouched_user_id, school_id, created_at)
		VALUES (?, ?, ?, ?, ?)
	`

	_, err := tx.ExecContext(ctx, query,
		vouch.ID,
		vouch.VoucherUserID,
		vouch.VouchedUserID,
		vouch.SchoolID,
		vouch.CreatedAt,
	)

	return err
}

// HasVouched checks if a user has already vouched for another user in a school
func (r *SchoolVouchRepository) HasVouched(ctx context.Context, voucherID, vouchedID, schoolID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM school_vouches WHERE voucher_user_id = ? AND vouched_user_id = ? AND school_id = ?)`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, voucherID, vouchedID, schoolID).Scan(&exists)
	return exists, err
}

// HasVouchedTx checks if a user has already vouched for another user in a school within a transaction
func (r *SchoolVouchRepository) HasVouchedTx(ctx context.Context, tx *sql.Tx, voucherID, vouchedID, schoolID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM school_vouches WHERE voucher_user_id = ? AND vouched_user_id = ? AND school_id = ?)`
	var exists bool
	err := tx.QueryRowContext(ctx, query, voucherID, vouchedID, schoolID).Scan(&exists)
	return exists, err
}

// CountVouchesForUser counts vouches received by a user in a school
func (r *SchoolVouchRepository) CountVouchesForUser(ctx context.Context, userID, schoolID string) (int, error) {
	query := `SELECT COUNT(*) FROM school_vouches WHERE vouched_user_id = ? AND school_id = ?`
	var count int
	err := r.db.QueryRowContext(ctx, query, userID, schoolID).Scan(&count)
	return count, err
}

// CountVouchesForUserTx counts vouches received by a user in a school within a transaction
func (r *SchoolVouchRepository) CountVouchesForUserTx(ctx context.Context, tx *sql.Tx, userID, schoolID string) (int, error) {
	query := `SELECT COUNT(*) FROM school_vouches WHERE vouched_user_id = ? AND school_id = ?`
	var count int
	err := tx.QueryRowContext(ctx, query, userID, schoolID).Scan(&count)
	return count, err
}

// CountVouchesThisMonth counts vouches given by a user this month across all schools
func (r *SchoolVouchRepository) CountVouchesThisMonth(ctx context.Context, voucherID string) (int, error) {
	query := `
		SELECT COUNT(*) FROM school_vouches
		WHERE voucher_user_id = ?
		AND created_at >= DATE_FORMAT(NOW(), '%Y-%m-01')
	`
	var count int
	err := r.db.QueryRowContext(ctx, query, voucherID).Scan(&count)
	return count, err
}

// CountVouchesThisMonthForUpdate counts vouches given this month with a row lock for race prevention
func (r *SchoolVouchRepository) CountVouchesThisMonthForUpdate(ctx context.Context, tx *sql.Tx, voucherID string) (int, error) {
	query := `
		SELECT COUNT(*) FROM school_vouches
		WHERE voucher_user_id = ?
		AND created_at >= DATE_FORMAT(NOW(), '%Y-%m-01')
		FOR UPDATE
	`
	var count int
	err := tx.QueryRowContext(ctx, query, voucherID).Scan(&count)
	return count, err
}

// GetVouchersForUser retrieves voucher details for a user in a school
func (r *SchoolVouchRepository) GetVouchersForUser(ctx context.Context, userID, schoolID string) ([]models.VoucherInfo, error) {
	query := `
		SELECT u.id, u.username, sv.created_at
		FROM school_vouches sv
		JOIN users u ON sv.voucher_user_id = u.id
		WHERE sv.vouched_user_id = ? AND sv.school_id = ?
		ORDER BY sv.created_at
	`

	rows, err := r.db.QueryContext(ctx, query, userID, schoolID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var vouchers []models.VoucherInfo
	for rows.Next() {
		var voucherInfo models.VoucherInfo
		if err := rows.Scan(&voucherInfo.UserID, &voucherInfo.Username, &voucherInfo.VouchedAt); err != nil {
			return nil, err
		}
		vouchers = append(vouchers, voucherInfo)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return vouchers, nil
}

// GetLastVouchTimeByVoucher retrieves the last time a voucher gave a vouch (for bootstrap cooldown)
func (r *SchoolVouchRepository) GetLastVouchTimeByVoucher(ctx context.Context, voucherID string) (*time.Time, error) {
	query := `
		SELECT MAX(created_at)
		FROM school_vouches
		WHERE voucher_user_id = ?
	`

	var lastVouchTime *time.Time
	err := r.db.QueryRowContext(ctx, query, voucherID).Scan(&lastVouchTime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return lastVouchTime, nil
}

// GetPendingVouchUsers retrieves users in a school with pending verification status
func (r *SchoolVouchRepository) GetPendingVouchUsers(ctx context.Context, schoolID string) ([]models.PendingSchoolVouchUser, error) {
	query := `
		SELECT u.id, u.username, u.email, us.school_id, s.name AS school_name,
			(SELECT COUNT(*) FROM school_vouches sv WHERE sv.vouched_user_id = u.id AND sv.school_id = us.school_id) AS vouch_count
		FROM user_schools us
		JOIN users u ON us.user_id = u.id
		JOIN schools s ON us.school_id = s.id
		WHERE us.school_id = ? AND us.verification_status = 'pending'
		ORDER BY u.username
	`

	rows, err := r.db.QueryContext(ctx, query, schoolID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var pendingUsers []models.PendingSchoolVouchUser
	for rows.Next() {
		var pendingUser models.PendingSchoolVouchUser
		if err := rows.Scan(
			&pendingUser.UserID,
			&pendingUser.Username,
			&pendingUser.Email,
			&pendingUser.SchoolID,
			&pendingUser.SchoolName,
			&pendingUser.VouchCount,
		); err != nil {
			return nil, err
		}
		pendingUsers = append(pendingUsers, pendingUser)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return pendingUsers, nil
}
