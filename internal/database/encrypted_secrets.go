package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

var (
	ErrEncryptedSecretNotFound = errors.New("encrypted secret not found")
)

// EncryptedSecretRepository handles encrypted secret database operations
type EncryptedSecretRepository struct {
	db *DB
}

// NewEncryptedSecretRepository creates a new encrypted secret repository
func NewEncryptedSecretRepository(db *DB) *EncryptedSecretRepository {
	return &EncryptedSecretRepository{db: db}
}

// Create creates an encrypted secret with wrapped keys in a transaction
func (r *EncryptedSecretRepository) Create(ctx context.Context, secret *models.EncryptedSecret, wrappedKeys []models.WrappedKeyEntry) error {
	secret.ID = uuid.New().String()
	secret.UpdatedAt = time.Now().UTC()

	return r.db.Transaction(ctx, func(tx *sql.Tx) error {
		secretQuery := `
			INSERT INTO encrypted_secrets
			(id, secret_type, signal_group_id, meshtastic_channel_id, encrypted_payload, encryption_iv, updated_by, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`
		_, err := tx.ExecContext(ctx, secretQuery,
			secret.ID, secret.SecretType, secret.SignalGroupID, secret.MeshtasticChannelID,
			secret.EncryptedPayload, secret.EncryptionIV, secret.UpdatedBy, secret.UpdatedAt,
		)
		if err != nil {
			return err
		}

		keyQuery := `
			INSERT INTO encrypted_secret_keys (secret_id, user_id, wrapped_dek, created_at)
			VALUES (?, ?, ?, ?)
		`
		now := time.Now().UTC()
		for _, wk := range wrappedKeys {
			_, err := tx.ExecContext(ctx, keyQuery, secret.ID, wk.UserID, wk.WrappedDEK, now)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// CreateTx creates an encrypted secret with wrapped keys within an existing transaction
func (r *EncryptedSecretRepository) CreateTx(ctx context.Context, tx *sql.Tx, secret *models.EncryptedSecret, wrappedKeys []models.WrappedKeyEntry) error {
	secret.ID = uuid.New().String()
	secret.UpdatedAt = time.Now().UTC()

	// #nosec G101 -- variable name matches "secret" pattern, not a credential
	secretQuery := `
		INSERT INTO encrypted_secrets
		(id, secret_type, signal_group_id, meshtastic_channel_id, encrypted_payload, encryption_iv, updated_by, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := tx.ExecContext(ctx, secretQuery,
		secret.ID, secret.SecretType, secret.SignalGroupID, secret.MeshtasticChannelID,
		secret.EncryptedPayload, secret.EncryptionIV, secret.UpdatedBy, secret.UpdatedAt,
	)
	if err != nil {
		return err
	}

	keyQuery := `
		INSERT INTO encrypted_secret_keys (secret_id, user_id, wrapped_dek, created_at)
		VALUES (?, ?, ?, ?)
	`
	now := time.Now().UTC()
	for _, wk := range wrappedKeys {
		_, err := tx.ExecContext(ctx, keyQuery, secret.ID, wk.UserID, wk.WrappedDEK, now)
		if err != nil {
			return err
		}
	}

	return nil
}

// GetByID retrieves an encrypted secret by its ID
func (r *EncryptedSecretRepository) GetByID(ctx context.Context, id string) (*models.EncryptedSecret, error) {
	query := `
		SELECT id, secret_type, signal_group_id, meshtastic_channel_id,
			encrypted_payload, encryption_iv, updated_by, updated_at
		FROM encrypted_secrets
		WHERE id = ?
	`

	secret := &models.EncryptedSecret{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&secret.ID, &secret.SecretType, &secret.SignalGroupID, &secret.MeshtasticChannelID,
		&secret.EncryptedPayload, &secret.EncryptionIV, &secret.UpdatedBy, &secret.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEncryptedSecretNotFound
	}
	if err != nil {
		return nil, err
	}
	return secret, nil
}

// GetBySignalGroupID retrieves the encrypted secret for a signal group
func (r *EncryptedSecretRepository) GetBySignalGroupID(ctx context.Context, groupID string) (*models.EncryptedSecret, error) {
	query := `
		SELECT id, secret_type, signal_group_id, meshtastic_channel_id,
			encrypted_payload, encryption_iv, updated_by, updated_at
		FROM encrypted_secrets
		WHERE signal_group_id = ?
	`

	secret := &models.EncryptedSecret{}
	err := r.db.QueryRowContext(ctx, query, groupID).Scan(
		&secret.ID, &secret.SecretType, &secret.SignalGroupID, &secret.MeshtasticChannelID,
		&secret.EncryptedPayload, &secret.EncryptionIV, &secret.UpdatedBy, &secret.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEncryptedSecretNotFound
	}
	if err != nil {
		return nil, err
	}
	return secret, nil
}

// GetByMeshtasticChannelID retrieves the encrypted secret for a meshtastic channel
func (r *EncryptedSecretRepository) GetByMeshtasticChannelID(ctx context.Context, channelID string) (*models.EncryptedSecret, error) {
	query := `
		SELECT id, secret_type, signal_group_id, meshtastic_channel_id,
			encrypted_payload, encryption_iv, updated_by, updated_at
		FROM encrypted_secrets
		WHERE meshtastic_channel_id = ?
	`

	secret := &models.EncryptedSecret{}
	err := r.db.QueryRowContext(ctx, query, channelID).Scan(
		&secret.ID, &secret.SecretType, &secret.SignalGroupID, &secret.MeshtasticChannelID,
		&secret.EncryptedPayload, &secret.EncryptionIV, &secret.UpdatedBy, &secret.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEncryptedSecretNotFound
	}
	if err != nil {
		return nil, err
	}
	return secret, nil
}

// GetWrappedDEK retrieves a user's wrapped DEK for a specific secret
func (r *EncryptedSecretRepository) GetWrappedDEK(ctx context.Context, secretID, userID string) (string, error) {
	query := `SELECT wrapped_dek FROM encrypted_secret_keys WHERE secret_id = ? AND user_id = ?`
	var wrappedDEK string
	err := r.db.QueryRowContext(ctx, query, secretID, userID).Scan(&wrappedDEK)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrEncryptedSecretNotFound
	}
	return wrappedDEK, err
}

// UpdatePayloadAndKeys atomically updates the encrypted payload and all wrapped keys
func (r *EncryptedSecretRepository) UpdatePayloadAndKeys(ctx context.Context, secretID, payload, iv, updatedBy string, wrappedKeys []models.WrappedKeyEntry) error {
	return r.db.Transaction(ctx, func(tx *sql.Tx) error {
		updateQuery := `
			UPDATE encrypted_secrets
			SET encrypted_payload = ?, encryption_iv = ?, updated_by = ?, updated_at = ?
			WHERE id = ?
		`
		_, err := tx.ExecContext(ctx, updateQuery, payload, iv, updatedBy, time.Now().UTC(), secretID)
		if err != nil {
			return err
		}

		// Delete existing keys and re-insert
		_, err = tx.ExecContext(ctx, `DELETE FROM encrypted_secret_keys WHERE secret_id = ?`, secretID)
		if err != nil {
			return err
		}

		keyQuery := `
			INSERT INTO encrypted_secret_keys (secret_id, user_id, wrapped_dek, created_at)
			VALUES (?, ?, ?, ?)
		`
		now := time.Now().UTC()
		for _, wk := range wrappedKeys {
			_, err := tx.ExecContext(ctx, keyQuery, secretID, wk.UserID, wk.WrappedDEK, now)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// FlagRekeyForUser sets rekey_needed=true on all keys for a user
func (r *EncryptedSecretRepository) FlagRekeyForUser(ctx context.Context, userID string) error {
	query := `UPDATE encrypted_secret_keys SET rekey_needed = TRUE WHERE user_id = ?`
	_, err := r.db.ExecContext(ctx, query, userID)
	return err
}

// GetPendingRekeys returns secrets where another user needs re-keying and the calling user has a valid key.
// Also returns the calling user's wrapped DEK so they can unwrap and re-wrap for the target.
// Covers both signal-group and meshtastic-channel scoped secrets.
func (r *EncryptedSecretRepository) GetPendingRekeys(ctx context.Context, memberUserID string) ([]PendingRekey, error) {
	query := `
		SELECT DISTINCT esk_need.secret_id, esk_need.user_id AS target_user_id,
			uek.public_key AS target_public_key,
			esk_have.wrapped_dek AS caller_wrapped_dek,
			sg.id AS group_id,
			sg.group_name,
			sg.connection_id,
			es.encrypted_payload,
			es.encryption_iv
		FROM encrypted_secret_keys esk_need
		INNER JOIN encrypted_secret_keys esk_have ON esk_need.secret_id = esk_have.secret_id
		INNER JOIN user_encryption_keys uek ON esk_need.user_id = uek.user_id
		INNER JOIN encrypted_secrets es ON esk_need.secret_id = es.id
		LEFT JOIN signal_groups sg ON es.signal_group_id = sg.id
		WHERE esk_need.rekey_needed = TRUE
		AND esk_have.user_id = ?
		AND esk_have.rekey_needed = FALSE
	`

	rows, err := r.db.QueryContext(ctx, query, memberUserID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []PendingRekey
	for rows.Next() {
		var pr PendingRekey
		if err := rows.Scan(&pr.SecretID, &pr.TargetUserID, &pr.TargetPublicKey, &pr.CallerWrappedDEK,
			&pr.GroupID, &pr.GroupName, &pr.ConnectionID, &pr.EncryptedPayload, &pr.EncryptionIV); err != nil {
			return nil, err
		}
		results = append(results, pr)
	}
	return results, rows.Err()
}

// SubmitRekey updates a single wrapped key for a target user.
// Only updates rows where rekey_needed is TRUE to prevent replay/overwrite attacks.
func (r *EncryptedSecretRepository) SubmitRekey(ctx context.Context, secretID, targetUserID, wrappedDEK string) error {
	query := `
		UPDATE encrypted_secret_keys
		SET wrapped_dek = ?, rekey_needed = FALSE, created_at = ?
		WHERE secret_id = ? AND user_id = ? AND rekey_needed = TRUE
	`
	result, err := r.db.ExecContext(ctx, query, wrappedDEK, time.Now().UTC(), secretID, targetUserID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return fmt.Errorf("no pending rekey found for secret %s user %s", secretID, targetUserID)
	}
	return nil
}

// PendingRekey represents a pending re-key operation
type PendingRekey struct {
	SecretID           string `json:"secret_id"`
	TargetUserID       string `json:"target_user_id"`
	TargetPublicKey    string `json:"target_public_key"`
	CallerWrappedDEK   string `json:"caller_wrapped_dek"`
	GroupID            *string `json:"group_id,omitempty"`
	GroupName          *string `json:"group_name,omitempty"`
	ConnectionID       *string `json:"connection_id,omitempty"`
	EncryptedPayload   string `json:"encrypted_payload,omitempty"`
	EncryptionIV       string `json:"encryption_iv,omitempty"`
}

// GetUsersWithSharedSecrets returns distinct user IDs who share at least one secret with the given user
func (r *EncryptedSecretRepository) GetUsersWithSharedSecrets(ctx context.Context, userID string) ([]string, error) {
	query := `
		SELECT DISTINCT esk2.user_id
		FROM encrypted_secret_keys esk1
		INNER JOIN encrypted_secret_keys esk2 ON esk1.secret_id = esk2.secret_id
		WHERE esk1.user_id = ? AND esk2.user_id != ?
	`

	rows, err := r.db.QueryContext(ctx, query, userID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var userIDs []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, uid)
	}
	return userIDs, rows.Err()
}

// RevokeConnectionSecretKeysForGroup deletes encrypted_secret_keys for users in a leaving group
// and flags rekey_needed on users in surviving groups within the connection.
func (r *EncryptedSecretRepository) RevokeConnectionSecretKeysForGroup(ctx context.Context, tx *sql.Tx, connID, groupID string) error {
	// Get all encrypted secrets for this connection
	// #nosec G101 -- variable name matches "secret" pattern, not a credential
	secretIDsQuery := `
		SELECT DISTINCT es.id
		FROM encrypted_secrets es
		INNER JOIN signal_groups sg ON es.signal_group_id = sg.id
		WHERE sg.connection_id = ?
	`
	rows, err := tx.QueryContext(ctx, secretIDsQuery, connID)
	if err != nil {
		return fmt.Errorf("get connection secrets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var secretIDs []string
	for rows.Next() {
		var secretID string
		if err := rows.Scan(&secretID); err != nil {
			return fmt.Errorf("scan secret id: %w", err)
		}
		secretIDs = append(secretIDs, secretID)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate secrets: %w", err)
	}

	if len(secretIDs) == 0 {
		return nil
	}

	// Get users in the leaving group
	leavingUsersQuery := `SELECT user_id FROM group_members WHERE group_id = ?`
	rows, err = tx.QueryContext(ctx, leavingUsersQuery, groupID)
	if err != nil {
		return fmt.Errorf("get leaving group users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var leavingUserIDs []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return fmt.Errorf("scan leaving user id: %w", err)
		}
		leavingUserIDs = append(leavingUserIDs, userID)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate leaving users: %w", err)
	}

	// Delete keys for leaving group members
	if len(leavingUserIDs) > 0 {
		secretPlaceholders := make([]string, len(secretIDs))
		userPlaceholders := make([]string, len(leavingUserIDs))
		args := make([]interface{}, 0, len(secretIDs)+len(leavingUserIDs))

		for i, sID := range secretIDs {
			secretPlaceholders[i] = "?"
			args = append(args, sID)
		}
		for i, uID := range leavingUserIDs {
			userPlaceholders[i] = "?"
			args = append(args, uID)
		}

		// #nosec G201 -- SQL string is parameterized with ? placeholders; no user input in format
		deleteQuery := fmt.Sprintf(`
			DELETE FROM encrypted_secret_keys
			WHERE secret_id IN (%s)
			AND user_id IN (%s)
		`, strings.Join(secretPlaceholders, ","), strings.Join(userPlaceholders, ","))

		_, err = tx.ExecContext(ctx, deleteQuery, args...)
		if err != nil {
			return fmt.Errorf("delete leaving user keys: %w", err)
		}
	}

	// Get users in surviving groups (members of other groups in the connection)
	survivingUsersQuery := `
		SELECT DISTINCT gm.user_id
		FROM group_members gm
		INNER JOIN connection_members cm ON gm.group_id = cm.group_id
		WHERE cm.connection_id = ?
		AND gm.group_id != ?
	`
	rows, err = tx.QueryContext(ctx, survivingUsersQuery, connID, groupID)
	if err != nil {
		return fmt.Errorf("get surviving group users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var survivingUserIDs []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return fmt.Errorf("scan surviving user id: %w", err)
		}
		survivingUserIDs = append(survivingUserIDs, userID)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate surviving users: %w", err)
	}

	// Flag rekey_needed for surviving users
	if len(survivingUserIDs) > 0 {
		secretPlaceholders := make([]string, len(secretIDs))
		userPlaceholders := make([]string, len(survivingUserIDs))
		args := make([]interface{}, 0, len(secretIDs)+len(survivingUserIDs))

		for i, sID := range secretIDs {
			secretPlaceholders[i] = "?"
			args = append(args, sID)
		}
		for i, uID := range survivingUserIDs {
			userPlaceholders[i] = "?"
			args = append(args, uID)
		}

		// #nosec G201 -- SQL string is parameterized with ? placeholders; no user input in format
		updateQuery := fmt.Sprintf(`
			UPDATE encrypted_secret_keys
			SET rekey_needed = TRUE
			WHERE secret_id IN (%s)
			AND user_id IN (%s)
		`, strings.Join(secretPlaceholders, ","), strings.Join(userPlaceholders, ","))

		_, err = tx.ExecContext(ctx, updateQuery, args...)
		if err != nil {
			return fmt.Errorf("flag rekey for survivors: %w", err)
		}
	}

	return nil
}

// RevokeGroupSecretKeysForUser deletes encrypted_secret_keys for a user leaving a group
// and flags rekey_needed=TRUE on the remaining recipients' rows for those secrets.
func (r *EncryptedSecretRepository) RevokeGroupSecretKeysForUser(ctx context.Context, tx *sql.Tx, groupID, userID string) error {
	deleteQuery := `
		DELETE esk FROM encrypted_secret_keys esk
		JOIN encrypted_secrets es ON esk.secret_id = es.id
		JOIN signal_groups sg ON es.signal_group_id = sg.id
		WHERE sg.owner_group_id = ? AND esk.user_id = ?
	`
	if _, err := tx.ExecContext(ctx, deleteQuery, groupID, userID); err != nil {
		return fmt.Errorf("delete revoked user keys: %w", err)
	}

	flagQuery := `
		UPDATE encrypted_secret_keys esk
		JOIN encrypted_secrets es ON esk.secret_id = es.id
		JOIN signal_groups sg ON es.signal_group_id = sg.id
		SET esk.rekey_needed = TRUE
		WHERE sg.owner_group_id = ?
	`
	if _, err := tx.ExecContext(ctx, flagQuery, groupID); err != nil {
		return fmt.Errorf("flag rekey for survivors: %w", err)
	}

	return nil
}
