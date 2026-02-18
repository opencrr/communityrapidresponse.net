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
	ErrUserNotFound                 = errors.New("user not found")
	ErrUserAlreadyExists            = errors.New("user already exists")
	ErrEmailAlreadyExists           = errors.New("email already exists")
	ErrNormalizedEmailAlreadyExists = errors.New("an account with this email already exists")
)

// UserRepository handles user database operations
type UserRepository struct {
	db *DB
}

// NewUserRepository creates a new user repository
func NewUserRepository(db *DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create creates a new user
func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	user.ID = uuid.New().String()
	user.CreatedAt = time.Now().UTC()
	user.MFASetupRequired = true // All new users must set up MFA

	query := `
		INSERT INTO users (id, username, email, password_hash, verification_tier,
		                   postcard_verified, vouch_verified, is_superuser,
		                   mfa_secret, mfa_enabled, mfa_backup_codes, mfa_setup_required,
		                   email_verified, email_normalized,
		                   created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		user.ID,
		user.Username,
		user.Email,
		user.PasswordHash,
		user.VerificationTier,
		user.PostcardVerified,
		user.VouchVerified,
		user.IsSuperuser,
		user.MFASecret,
		user.MFAEnabled,
		user.MFABackupCodes,
		user.MFASetupRequired,
		user.EmailVerified,
		user.EmailNormalized,
		user.CreatedAt,
	)

	if err != nil {
		// Check for duplicate key errors
		if isDuplicateKeyError(err) {
			if containsString(err.Error(), "username") {
				return ErrUserAlreadyExists
			}
			if containsString(err.Error(), "email_normalized") {
				return ErrNormalizedEmailAlreadyExists
			}
			if containsString(err.Error(), "email") {
				return ErrEmailAlreadyExists
			}
			return ErrUserAlreadyExists
		}
		return err
	}

	return nil
}

// GetByID retrieves a user by ID
func (r *UserRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
	query := `
		SELECT id, username, email, password_hash, verification_tier,
		       postcard_verified, vouch_verified, is_superuser,
		       mfa_secret, mfa_enabled, mfa_backup_codes, mfa_setup_required,
		       email_verified, email_normalized,
		       is_blocked, blocked_at, blocked_by, block_reason,
		       address_hash, created_at, last_login, deleted_at,
		       failed_login_attempts, locked_until, failed_mfa_attempts
		FROM users WHERE id = ?
	`

	user := &models.User{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.VerificationTier,
		&user.PostcardVerified,
		&user.VouchVerified,
		&user.IsSuperuser,
		&user.MFASecret,
		&user.MFAEnabled,
		&user.MFABackupCodes,
		&user.MFASetupRequired,
		&user.EmailVerified,
		&user.EmailNormalized,
		&user.IsBlocked,
		&user.BlockedAt,
		&user.BlockedBy,
		&user.BlockReason,
		&user.AddressHash,
		&user.CreatedAt,
		&user.LastLogin,
		&user.DeletedAt,
		&user.FailedLoginAttempts,
		&user.LockedUntil,
		&user.FailedMFAAttempts,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetByEmail retrieves a user by email
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `
		SELECT id, username, email, password_hash, verification_tier,
		       postcard_verified, vouch_verified, is_superuser,
		       mfa_secret, mfa_enabled, mfa_backup_codes, mfa_setup_required,
		       email_verified, email_normalized,
		       is_blocked, blocked_at, blocked_by, block_reason,
		       address_hash, created_at, last_login, deleted_at,
		       failed_login_attempts, locked_until, failed_mfa_attempts
		FROM users WHERE email = ?
	`

	user := &models.User{}
	err := r.db.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.VerificationTier,
		&user.PostcardVerified,
		&user.VouchVerified,
		&user.IsSuperuser,
		&user.MFASecret,
		&user.MFAEnabled,
		&user.MFABackupCodes,
		&user.MFASetupRequired,
		&user.EmailVerified,
		&user.EmailNormalized,
		&user.IsBlocked,
		&user.BlockedAt,
		&user.BlockedBy,
		&user.BlockReason,
		&user.AddressHash,
		&user.CreatedAt,
		&user.LastLogin,
		&user.DeletedAt,
		&user.FailedLoginAttempts,
		&user.LockedUntil,
		&user.FailedMFAAttempts,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetByUsername retrieves a user by username
func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*models.User, error) {
	query := `
		SELECT id, username, email, password_hash, verification_tier,
		       postcard_verified, vouch_verified, is_superuser,
		       mfa_secret, mfa_enabled, mfa_backup_codes, mfa_setup_required,
		       email_verified, email_normalized,
		       is_blocked, blocked_at, blocked_by, block_reason,
		       address_hash, created_at, last_login, deleted_at,
		       failed_login_attempts, locked_until, failed_mfa_attempts
		FROM users WHERE username = ?
	`

	user := &models.User{}
	err := r.db.QueryRowContext(ctx, query, username).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.VerificationTier,
		&user.PostcardVerified,
		&user.VouchVerified,
		&user.IsSuperuser,
		&user.MFASecret,
		&user.MFAEnabled,
		&user.MFABackupCodes,
		&user.MFASetupRequired,
		&user.EmailVerified,
		&user.EmailNormalized,
		&user.IsBlocked,
		&user.BlockedAt,
		&user.BlockedBy,
		&user.BlockReason,
		&user.AddressHash,
		&user.CreatedAt,
		&user.LastLogin,
		&user.DeletedAt,
		&user.FailedLoginAttempts,
		&user.LockedUntil,
		&user.FailedMFAAttempts,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetByEmailOrUsername retrieves a user by email or username
// Used for login where the user can enter either identifier
func (r *UserRepository) GetByEmailOrUsername(ctx context.Context, identifier string) (*models.User, error) {
	query := `
		SELECT id, username, email, password_hash, verification_tier,
		       postcard_verified, vouch_verified, is_superuser,
		       mfa_secret, mfa_enabled, mfa_backup_codes, mfa_setup_required,
		       email_verified, email_normalized,
		       is_blocked, blocked_at, blocked_by, block_reason,
		       address_hash, created_at, last_login, deleted_at,
		       failed_login_attempts, locked_until, failed_mfa_attempts
		FROM users WHERE email = ? OR username = ?
	`

	user := &models.User{}
	err := r.db.QueryRowContext(ctx, query, identifier, identifier).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.VerificationTier,
		&user.PostcardVerified,
		&user.VouchVerified,
		&user.IsSuperuser,
		&user.MFASecret,
		&user.MFAEnabled,
		&user.MFABackupCodes,
		&user.MFASetupRequired,
		&user.EmailVerified,
		&user.EmailNormalized,
		&user.IsBlocked,
		&user.BlockedAt,
		&user.BlockedBy,
		&user.BlockReason,
		&user.AddressHash,
		&user.CreatedAt,
		&user.LastLogin,
		&user.DeletedAt,
		&user.FailedLoginAttempts,
		&user.LockedUntil,
		&user.FailedMFAAttempts,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

// UpdateLastLogin updates the user's last login timestamp
func (r *UserRepository) UpdateLastLogin(ctx context.Context, userID string) error {
	query := `UPDATE users SET last_login = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, time.Now().UTC(), userID)
	return err
}

// UpdateVerificationTier updates the user's verification tier
func (r *UserRepository) UpdateVerificationTier(ctx context.Context, userID string, tier models.VerificationTier) error {
	query := `UPDATE users SET verification_tier = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, tier, userID)
	return err
}

// SetPostcardVerified marks a user as postcard verified
func (r *UserRepository) SetPostcardVerified(ctx context.Context, userID string, verified bool) error {
	query := `UPDATE users SET postcard_verified = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, verified, userID)
	return err
}

// SetVouchVerified marks a user as vouch verified
func (r *UserRepository) SetVouchVerified(ctx context.Context, userID string, verified bool) error {
	query := `UPDATE users SET vouch_verified = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, verified, userID)
	return err
}

// Search searches for users by email or username with pagination
func (r *UserRepository) Search(ctx context.Context, query string, page, limit int) ([]models.User, int, error) {
	offset := (page - 1) * limit

	// Count total matching users
	countQuery := `
		SELECT COUNT(*) FROM users
		WHERE (? = '' OR email LIKE ? OR username LIKE ?)
	`
	searchPattern := "%" + query + "%"
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, query, searchPattern, searchPattern).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Get users with pagination
	selectQuery := `
		SELECT id, username, email, password_hash, verification_tier,
		       postcard_verified, vouch_verified, is_superuser,
		       mfa_secret, mfa_enabled, mfa_backup_codes, mfa_setup_required,
		       email_verified, email_normalized,
		       is_blocked, blocked_at, blocked_by, block_reason,
		       address_hash, created_at, last_login, deleted_at,
		       failed_login_attempts, locked_until, failed_mfa_attempts
		FROM users
		WHERE (? = '' OR email LIKE ? OR username LIKE ?)
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`

	rows, err := r.db.QueryContext(ctx, selectQuery, query, searchPattern, searchPattern, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	users := make([]models.User, 0)
	for rows.Next() {
		var user models.User
		if err := rows.Scan(
			&user.ID,
			&user.Username,
			&user.Email,
			&user.PasswordHash,
			&user.VerificationTier,
			&user.PostcardVerified,
			&user.VouchVerified,
			&user.IsSuperuser,
			&user.MFASecret,
			&user.MFAEnabled,
			&user.MFABackupCodes,
			&user.MFASetupRequired,
			&user.EmailVerified,
			&user.EmailNormalized,
			&user.IsBlocked,
			&user.BlockedAt,
			&user.BlockedBy,
			&user.BlockReason,
			&user.AddressHash,
			&user.CreatedAt,
			&user.LastLogin,
			&user.DeletedAt,
			&user.FailedLoginAttempts,
			&user.LockedUntil,
			&user.FailedMFAAttempts,
		); err != nil {
			return nil, 0, err
		}
		// Don't expose password hash
		user.PasswordHash = ""
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

// Block marks a user as blocked
func (r *UserRepository) Block(ctx context.Context, userID, blockedBy, reason string) error {
	now := time.Now().UTC()
	query := `
		UPDATE users
		SET is_blocked = TRUE, blocked_at = ?, blocked_by = ?, block_reason = ?
		WHERE id = ?
	`
	_, err := r.db.ExecContext(ctx, query, now, blockedBy, reason, userID)
	return err
}

// Unblock removes a user from the blocklist
func (r *UserRepository) Unblock(ctx context.Context, userID string) error {
	query := `
		UPDATE users
		SET is_blocked = FALSE, blocked_at = NULL, blocked_by = NULL, block_reason = NULL
		WHERE id = ?
	`
	_, err := r.db.ExecContext(ctx, query, userID)
	return err
}

// Delete permanently deletes a user and all associated data
func (r *UserRepository) Delete(ctx context.Context, userID string) error {
	// The foreign key constraints with ON DELETE CASCADE will handle related records
	query := `DELETE FROM users WHERE id = ?`
	result, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// GetUserWithRegions retrieves a user with their associated regions
func (r *UserRepository) GetUserWithRegions(ctx context.Context, userID string) (*models.UserWithRegions, error) {
	user, err := r.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	result := &models.UserWithRegions{
		User: *user,
	}

	// Get user regions
	regionsQuery := `
		SELECT ur.region_id, gr.name, ur.is_admin
		FROM user_regions ur
		JOIN geographic_regions gr ON ur.region_id = gr.id
		WHERE ur.user_id = ?
	`
	rows, err := r.db.QueryContext(ctx, regionsQuery, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var region models.UserRegionInfo
		if err := rows.Scan(&region.RegionID, &region.RegionName, &region.IsAdmin); err != nil {
			return nil, err
		}
		result.Regions = append(result.Regions, region)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Get authorized boundaries
	boundariesQuery := `
		SELECT id, boundary_type, boundary_name, boundary_state, verified_at
		FROM admin_boundaries
		WHERE user_id = ?
	`
	boundaryRows, err := r.db.QueryContext(ctx, boundariesQuery, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = boundaryRows.Close() }()

	for boundaryRows.Next() {
		var boundary models.AdminBoundaryInfo
		if err := boundaryRows.Scan(
			&boundary.ID,
			&boundary.Type,
			&boundary.Name,
			&boundary.State,
			&boundary.VerifiedAt,
		); err != nil {
			return nil, err
		}
		result.AuthorizedBoundaries = append(result.AuthorizedBoundaries, boundary)
	}
	if err := boundaryRows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// SetMFASecret sets the encrypted MFA secret for a user
func (r *UserRepository) SetMFASecret(ctx context.Context, userID string, encryptedSecret string) error {
	query := `UPDATE users SET mfa_secret = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, encryptedSecret, userID)
	return err
}

// EnableMFA enables MFA for a user and stores backup codes
func (r *UserRepository) EnableMFA(ctx context.Context, userID string, backupCodesJSON string) error {
	query := `UPDATE users SET mfa_enabled = TRUE, mfa_setup_required = FALSE, mfa_backup_codes = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, backupCodesJSON, userID)
	return err
}

// UpdateBackupCodes updates the backup codes for a user
func (r *UserRepository) UpdateBackupCodes(ctx context.Context, userID string, backupCodesJSON string) error {
	query := `UPDATE users SET mfa_backup_codes = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, backupCodesJSON, userID)
	return err
}

// DisableMFA disables MFA for a user (for admin reset purposes)
func (r *UserRepository) DisableMFA(ctx context.Context, userID string) error {
	query := `UPDATE users SET mfa_enabled = FALSE, mfa_secret = NULL, mfa_backup_codes = NULL, mfa_setup_required = TRUE WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, userID)
	return err
}

// GetByNormalizedEmail retrieves a user by their normalized email address
func (r *UserRepository) GetByNormalizedEmail(ctx context.Context, normalizedEmail string) (*models.User, error) {
	query := `
		SELECT id, username, email, password_hash, verification_tier,
		       postcard_verified, vouch_verified, is_superuser,
		       mfa_secret, mfa_enabled, mfa_backup_codes, mfa_setup_required,
		       email_verified, email_normalized,
		       is_blocked, blocked_at, blocked_by, block_reason,
		       address_hash, created_at, last_login, deleted_at,
		       failed_login_attempts, locked_until, failed_mfa_attempts
		FROM users WHERE email_normalized = ?
	`

	user := &models.User{}
	err := r.db.QueryRowContext(ctx, query, normalizedEmail).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.VerificationTier,
		&user.PostcardVerified,
		&user.VouchVerified,
		&user.IsSuperuser,
		&user.MFASecret,
		&user.MFAEnabled,
		&user.MFABackupCodes,
		&user.MFASetupRequired,
		&user.EmailVerified,
		&user.EmailNormalized,
		&user.IsBlocked,
		&user.BlockedAt,
		&user.BlockedBy,
		&user.BlockReason,
		&user.AddressHash,
		&user.CreatedAt,
		&user.LastLogin,
		&user.DeletedAt,
		&user.FailedLoginAttempts,
		&user.LockedUntil,
		&user.FailedMFAAttempts,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

// SetEmailVerified marks a user's email as verified
func (r *UserRepository) SetEmailVerified(ctx context.Context, userID string, verified bool) error {
	query := `UPDATE users SET email_verified = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, verified, userID)
	return err
}

// UpdatePassword updates a user's password hash
func (r *UserRepository) UpdatePassword(ctx context.Context, userID, newPasswordHash string) error {
	query := `UPDATE users SET password_hash = ? WHERE id = ?`
	result, err := r.db.ExecContext(ctx, query, newPasswordHash, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetAddressHash stores the SHA-256 hash of a user's verified address
// This is used for address blocking without storing actual addresses
func (r *UserRepository) SetAddressHash(ctx context.Context, userID, hash string) error {
	query := `UPDATE users SET address_hash = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, hash, userID)
	return err
}

// GetAddressHash retrieves a user's address hash
func (r *UserRepository) GetAddressHash(ctx context.Context, userID string) (*string, error) {
	var hash *string
	query := `SELECT address_hash FROM users WHERE id = ?`
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&hash)
	if err != nil {
		return nil, err
	}
	return hash, nil
}

// Helper functions
func isDuplicateKeyError(err error) bool {
	return containsString(err.Error(), "Duplicate entry")
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStringHelper(s, substr))
}

func containsStringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- Transactional methods for race condition prevention ---

// GetByIDForUpdate locks and retrieves a user by ID within a transaction
func (r *UserRepository) GetByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (*models.User, error) {
	query := `
		SELECT id, username, email, password_hash, verification_tier,
		       postcard_verified, vouch_verified, is_superuser,
		       mfa_secret, mfa_enabled, mfa_backup_codes, mfa_setup_required,
		       email_verified, email_normalized,
		       is_blocked, blocked_at, blocked_by, block_reason,
		       address_hash, created_at, last_login, deleted_at,
		       failed_login_attempts, locked_until, failed_mfa_attempts
		FROM users WHERE id = ?
		FOR UPDATE
	`

	user := &models.User{}
	err := tx.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.VerificationTier,
		&user.PostcardVerified,
		&user.VouchVerified,
		&user.IsSuperuser,
		&user.MFASecret,
		&user.MFAEnabled,
		&user.MFABackupCodes,
		&user.MFASetupRequired,
		&user.EmailVerified,
		&user.EmailNormalized,
		&user.IsBlocked,
		&user.BlockedAt,
		&user.BlockedBy,
		&user.BlockReason,
		&user.AddressHash,
		&user.CreatedAt,
		&user.LastLogin,
		&user.DeletedAt,
		&user.FailedLoginAttempts,
		&user.LockedUntil,
		&user.FailedMFAAttempts,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

// UpdateBackupCodesTx updates backup codes within a transaction
func (r *UserRepository) UpdateBackupCodesTx(ctx context.Context, tx *sql.Tx, userID string, backupCodesJSON string) error {
	query := `UPDATE users SET mfa_backup_codes = ? WHERE id = ?`
	_, err := tx.ExecContext(ctx, query, backupCodesJSON, userID)
	return err
}

// SetVouchVerifiedTx marks a user as vouch verified within a transaction
func (r *UserRepository) SetVouchVerifiedTx(ctx context.Context, tx *sql.Tx, userID string, verified bool) error {
	query := `UPDATE users SET vouch_verified = ? WHERE id = ?`
	_, err := tx.ExecContext(ctx, query, verified, userID)
	return err
}

// UpdateVerificationTierTx updates verification tier within a transaction
func (r *UserRepository) UpdateVerificationTierTx(ctx context.Context, tx *sql.Tx, userID string, tier models.VerificationTier) error {
	query := `UPDATE users SET verification_tier = ? WHERE id = ?`
	_, err := tx.ExecContext(ctx, query, tier, userID)
	return err
}

// SetPostcardVerifiedTx marks a user as postcard verified within a transaction
func (r *UserRepository) SetPostcardVerifiedTx(ctx context.Context, tx *sql.Tx, userID string, verified bool) error {
	query := `UPDATE users SET postcard_verified = ? WHERE id = ?`
	_, err := tx.ExecContext(ctx, query, verified, userID)
	return err
}

// SoftDelete scrubs PII from a user while preserving the row for block enforcement
func (r *UserRepository) SoftDelete(ctx context.Context, userID, scrubUUID string) error {
	query := `
		UPDATE users
		SET username = ?,
		    email = ?,
		    password_hash = '',
		    mfa_secret = NULL,
		    mfa_backup_codes = NULL,
		    mfa_enabled = FALSE,
		    deleted_at = NOW()
		WHERE id = ?
	`
	scrubUsername := "deleted-" + scrubUUID
	scrubEmail := "deleted-" + scrubUUID + "@deleted.invalid"
	result, err := r.db.ExecContext(ctx, query, scrubUsername, scrubEmail, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// IsUserBlockedAnywhere checks if a user is blocked in any context (global, region, school, or address)
func (r *UserRepository) IsUserBlockedAnywhere(ctx context.Context, userID string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM users WHERE id = ? AND is_blocked = TRUE
			UNION ALL
			SELECT 1 FROM blocked_users WHERE user_id = ?
			UNION ALL
			SELECT 1 FROM school_blocked_users WHERE user_id = ?
			UNION ALL
			SELECT 1 FROM blocked_addresses ba
			JOIN users u ON u.address_hash = ba.address_hash
			WHERE u.id = ? AND ba.expires_at > NOW()
		)
	`
	var isBlocked bool
	err := r.db.QueryRowContext(ctx, query, userID, userID, userID, userID).Scan(&isBlocked)
	if err != nil {
		return false, err
	}
	return isBlocked, nil
}

// SoleAdminRegion represents a region where a user is the sole admin
type SoleAdminRegion struct {
	ID   string
	Name string
}

// GetSoleAdminRegions finds regions where the given user is the only admin
func (r *UserRepository) GetSoleAdminRegions(ctx context.Context, userID string) ([]SoleAdminRegion, error) {
	query := `
		SELECT gr.id, gr.name
		FROM user_regions ur
		JOIN geographic_regions gr ON ur.region_id = gr.id
		WHERE ur.user_id = ? AND ur.is_admin = TRUE
		AND NOT EXISTS (
			SELECT 1 FROM user_regions ur2
			WHERE ur2.region_id = ur.region_id
			AND ur2.is_admin = TRUE
			AND ur2.user_id != ?
		)
	`
	rows, err := r.db.QueryContext(ctx, query, userID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var regions []SoleAdminRegion
	for rows.Next() {
		var region SoleAdminRegion
		if err := rows.Scan(&region.ID, &region.Name); err != nil {
			return nil, err
		}
		regions = append(regions, region)
	}
	return regions, rows.Err()
}

// SoleVerifiedSchool represents a school where a user is the sole verified member
type SoleVerifiedSchool struct {
	ID   string
	Name string
}

// GetSoleVerifiedSchools finds schools where the given user is the only verified member
func (r *UserRepository) GetSoleVerifiedSchools(ctx context.Context, userID string) ([]SoleVerifiedSchool, error) {
	query := `
		SELECT s.id, s.name
		FROM user_schools us
		JOIN schools s ON us.school_id = s.id
		WHERE us.user_id = ? AND us.verification_status = 'verified'
		AND NOT EXISTS (
			SELECT 1 FROM user_schools us2
			WHERE us2.school_id = us.school_id
			AND us2.verification_status = 'verified'
			AND us2.user_id != ?
		)
	`
	rows, err := r.db.QueryContext(ctx, query, userID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var schools []SoleVerifiedSchool
	for rows.Next() {
		var school SoleVerifiedSchool
		if err := rows.Scan(&school.ID, &school.Name); err != nil {
			return nil, err
		}
		schools = append(schools, school)
	}
	return schools, rows.Err()
}

// IncrementFailedLoginAttempts atomically increments the failed login counter and returns the new count
func (r *UserRepository) IncrementFailedLoginAttempts(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.db.Transaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET failed_login_attempts = failed_login_attempts + 1 WHERE id = ?`, userID); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT failed_login_attempts FROM users WHERE id = ?`, userID).Scan(&count)
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// LockAccount sets the locked_until timestamp for an account
func (r *UserRepository) LockAccount(ctx context.Context, userID string, lockedUntil time.Time) error {
	query := `UPDATE users SET locked_until = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, lockedUntil.UTC(), userID)
	return err
}

// ResetFailedLoginAttempts clears the failed login counter and lock
func (r *UserRepository) ResetFailedLoginAttempts(ctx context.Context, userID string) error {
	query := `UPDATE users SET failed_login_attempts = 0, locked_until = NULL WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, userID)
	return err
}

// IncrementFailedMFAAttempts atomically increments the failed MFA counter and returns the new count
func (r *UserRepository) IncrementFailedMFAAttempts(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.db.Transaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET failed_mfa_attempts = failed_mfa_attempts + 1 WHERE id = ?`, userID); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT failed_mfa_attempts FROM users WHERE id = ?`, userID).Scan(&count)
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ResetFailedMFAAttempts clears the failed MFA attempt counter
func (r *UserRepository) ResetFailedMFAAttempts(ctx context.Context, userID string) error {
	query := `UPDATE users SET failed_mfa_attempts = 0 WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, userID)
	return err
}

// GetUserStatus returns the minimal fields needed for token validation.
// This is a lightweight query used by the middleware status cache.
func (r *UserRepository) GetUserStatus(ctx context.Context, userID string) (*models.UserStatus, error) {
	query := `SELECT token_invalidated_at, is_blocked, deleted_at FROM users WHERE id = ?`

	status := &models.UserStatus{}
	err := r.db.QueryRowContext(ctx, query, userID).Scan(
		&status.TokenInvalidatedAt,
		&status.IsBlocked,
		&status.DeletedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return status, nil
}

// InvalidateTokens sets the token_invalidated_at timestamp so existing tokens
// issued before this time will be rejected by the auth middleware.
func (r *UserRepository) InvalidateTokens(ctx context.Context, userID string) error {
	query := `UPDATE users SET token_invalidated_at = NOW() WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, userID)
	return err
}

// UpdatePasswordTx updates a user's password hash within a transaction
func (r *UserRepository) UpdatePasswordTx(ctx context.Context, tx *sql.Tx, userID, newPasswordHash string) error {
	query := `UPDATE users SET password_hash = ? WHERE id = ?`
	result, err := tx.ExecContext(ctx, query, newPasswordHash, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// InvalidateTokensTx sets the token_invalidated_at timestamp within a transaction
func (r *UserRepository) InvalidateTokensTx(ctx context.Context, tx *sql.Tx, userID string) error {
	query := `UPDATE users SET token_invalidated_at = NOW() WHERE id = ?`
	_, err := tx.ExecContext(ctx, query, userID)
	return err
}

// GetVerifiedUsersInRegion returns users who are postcard OR vouch verified in a specific region.
// Results are paginated for memory efficiency when dealing with large regions.
func (r *UserRepository) GetVerifiedUsersInRegion(ctx context.Context, regionID string, offset, limit int) ([]*models.User, error) {
	query := `
		SELECT u.id, u.username, u.email, u.verification_tier,
		       u.postcard_verified, u.vouch_verified, u.is_superuser,
		       u.email_verified, u.is_blocked, u.created_at
		FROM users u
		JOIN user_regions ur ON u.id = ur.user_id
		WHERE ur.region_id = ?
		  AND (u.postcard_verified = TRUE OR u.vouch_verified = TRUE)
		  AND u.is_blocked = FALSE
		ORDER BY u.created_at ASC
		LIMIT ? OFFSET ?
	`

	rows, err := r.db.QueryContext(ctx, query, regionID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var users []*models.User
	for rows.Next() {
		user := &models.User{}
		if err := rows.Scan(
			&user.ID,
			&user.Username,
			&user.Email,
			&user.VerificationTier,
			&user.PostcardVerified,
			&user.VouchVerified,
			&user.IsSuperuser,
			&user.EmailVerified,
			&user.IsBlocked,
			&user.CreatedAt,
		); err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}
