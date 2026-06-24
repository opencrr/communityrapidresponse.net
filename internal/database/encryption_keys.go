package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

var ErrEncryptionKeyNotFound = errors.New("encryption key not found")

// EncryptionKeyRepository handles user encryption key database operations
type EncryptionKeyRepository struct {
	db *DB
}

// NewEncryptionKeyRepository creates a new encryption key repository
func NewEncryptionKeyRepository(db *DB) *EncryptionKeyRepository {
	return &EncryptionKeyRepository{db: db}
}

// Create stores a new user encryption key
func (r *EncryptionKeyRepository) Create(ctx context.Context, key *models.UserEncryptionKey) error {
	now := time.Now().UTC()
	key.CreatedAt = now
	key.RotatedAt = now

	query := `
		INSERT INTO user_encryption_keys (user_id, public_key, wrapped_private_key, key_salt, key_iv, created_at, rotated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			public_key = VALUES(public_key),
			wrapped_private_key = VALUES(wrapped_private_key),
			key_salt = VALUES(key_salt),
			key_iv = VALUES(key_iv),
			rotated_at = VALUES(rotated_at)
	`

	_, err := r.db.ExecContext(ctx, query,
		key.UserID,
		key.PublicKey,
		key.WrappedPrivateKey,
		key.KeySalt,
		key.KeyIV,
		key.CreatedAt,
		key.RotatedAt,
	)
	return err
}

// GetByUserID retrieves encryption keys for a user
func (r *EncryptionKeyRepository) GetByUserID(ctx context.Context, userID string) (*models.UserEncryptionKey, error) {
	query := `
		SELECT user_id, public_key, wrapped_private_key, key_salt, key_iv, created_at, rotated_at
		FROM user_encryption_keys
		WHERE user_id = ?
	`

	key := &models.UserEncryptionKey{}
	err := r.db.QueryRowContext(ctx, query, userID).Scan(
		&key.UserID,
		&key.PublicKey,
		&key.WrappedPrivateKey,
		&key.KeySalt,
		&key.KeyIV,
		&key.CreatedAt,
		&key.RotatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEncryptionKeyNotFound
	}
	if err != nil {
		return nil, err
	}

	return key, nil
}

// GetPublicKeysByUserIDs retrieves public keys for multiple users
func (r *EncryptionKeyRepository) GetPublicKeysByUserIDs(ctx context.Context, userIDs []string) ([]models.PublicKeyEntry, error) {
	if len(userIDs) == 0 {
		return []models.PublicKeyEntry{}, nil
	}

	query := `SELECT user_id, public_key FROM user_encryption_keys WHERE user_id IN (`
	args := make([]interface{}, len(userIDs))
	for i, id := range userIDs {
		if i > 0 {
			query += ", "
		}
		query += "?"
		args[i] = id
	}
	query += ")"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var keys []models.PublicKeyEntry
	for rows.Next() {
		var entry models.PublicKeyEntry
		if err := rows.Scan(&entry.UserID, &entry.PublicKey); err != nil {
			return nil, err
		}
		keys = append(keys, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if keys == nil {
		keys = []models.PublicKeyEntry{}
	}
	return keys, nil
}

// Update re-wraps the private key (e.g. after password change) without changing the public key
func (r *EncryptionKeyRepository) Update(ctx context.Context, userID, wrappedPrivateKey, keySalt, keyIV string) error {
	query := `
		UPDATE user_encryption_keys
		SET wrapped_private_key = ?, key_salt = ?, key_iv = ?
		WHERE user_id = ?
	`
	result, err := r.db.ExecContext(ctx, query, wrappedPrivateKey, keySalt, keyIV, userID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrEncryptionKeyNotFound
	}
	return nil
}

// GetPublicKeysForRegion retrieves public keys for all verified members of a region
func (r *EncryptionKeyRepository) GetPublicKeysForRegion(ctx context.Context, regionID string) ([]models.PublicKeyEntry, error) {
	query := `
		SELECT ek.user_id, ek.public_key
		FROM user_encryption_keys ek
		INNER JOIN user_regions ur ON ek.user_id = ur.user_id
		INNER JOIN users u ON ek.user_id = u.id
		WHERE ur.region_id = ? AND u.vouch_verified = TRUE AND u.is_superuser = FALSE
	`
	return r.queryPublicKeys(ctx, query, regionID)
}

// GetPublicKeysForSchool retrieves public keys for all verified members of a school
func (r *EncryptionKeyRepository) GetPublicKeysForSchool(ctx context.Context, schoolID string) ([]models.PublicKeyEntry, error) {
	query := `
		SELECT ek.user_id, ek.public_key
		FROM user_encryption_keys ek
		INNER JOIN user_schools us ON ek.user_id = us.user_id
		INNER JOIN users u ON ek.user_id = u.id
		WHERE us.school_id = ? AND us.verification_status = 'verified' AND u.is_superuser = FALSE
	`
	return r.queryPublicKeys(ctx, query, schoolID)
}

// GetPublicKeysForDistrict retrieves public keys for verified members of any school in a district
func (r *EncryptionKeyRepository) GetPublicKeysForDistrict(ctx context.Context, districtID string) ([]models.PublicKeyEntry, error) {
	query := `
		SELECT DISTINCT ek.user_id, ek.public_key
		FROM user_encryption_keys ek
		INNER JOIN user_schools us ON ek.user_id = us.user_id
		INNER JOIN schools s ON us.school_id = s.id
		INNER JOIN users u ON ek.user_id = u.id
		WHERE s.district_id = ? AND us.verification_status = 'verified' AND u.is_superuser = FALSE
	`
	return r.queryPublicKeys(ctx, query, districtID)
}

// GetPublicKeysForGroup retrieves public keys for entitled members of a group at a specific access tier
func (r *EncryptionKeyRepository) GetPublicKeysForGroup(ctx context.Context, groupID string, tier models.AccessTier) ([]models.PublicKeyEntry, error) {
	tierPredicate, err := GroupTierMembershipPredicate(tier)
	if err != nil {
		return nil, err
	}

	query := `
		SELECT ek.user_id, ek.public_key
		FROM user_encryption_keys ek
		INNER JOIN group_members gm ON ek.user_id = gm.user_id
		INNER JOIN users u ON ek.user_id = u.id
		WHERE gm.group_id = ? AND ` + tierPredicate + ` AND u.is_superuser = FALSE
	`
	return r.queryPublicKeys(ctx, query, groupID)
}

func (r *EncryptionKeyRepository) queryPublicKeys(ctx context.Context, query string, scopeID string) ([]models.PublicKeyEntry, error) {
	rows, err := r.db.QueryContext(ctx, query, scopeID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var keys []models.PublicKeyEntry
	for rows.Next() {
		var entry models.PublicKeyEntry
		if err := rows.Scan(&entry.UserID, &entry.PublicKey); err != nil {
			return nil, err
		}
		keys = append(keys, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if keys == nil {
		keys = []models.PublicKeyEntry{}
	}
	return keys, nil
}
