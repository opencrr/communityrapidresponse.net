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
			sg.connection_id
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
			&pr.GroupID, &pr.GroupName, &pr.ConnectionID); err != nil {
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
	SecretID         string  `json:"secret_id"`
	TargetUserID     string  `json:"target_user_id"`
	TargetPublicKey  string  `json:"target_public_key"`
	CallerWrappedDEK string  `json:"caller_wrapped_dek"`
	GroupID          *string `json:"group_id,omitempty"`
	GroupName        *string `json:"group_name,omitempty"`
	ConnectionID     *string `json:"connection_id,omitempty"`
}

// PendingGroupRotation represents a pending group/connection DEK rotation where a member was removed.
// Unlike PendingRekey (user key-pair rotation), this requires generating a fresh DEK and
// re-encrypting the payload for all surviving recipients.
type PendingGroupRotation struct {
	SecretID         string                  `json:"secret_id"`
	EncryptedPayload string                  `json:"encrypted_payload"`
	EncryptionIV     string                  `json:"encryption_iv"`
	CallerWrappedDEK string                  `json:"caller_wrapped_dek"`
	GroupID          *string                 `json:"group_id,omitempty"`
	GroupName        *string                 `json:"group_name,omitempty"`
	ConnectionID     *string                 `json:"connection_id,omitempty"`
	Recipients       []models.PublicKeyEntry `json:"recipients"`
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

// GetPendingGroupRotations returns secrets where the caller is a surviving recipient of a group/connection
// member-removal rotation. Each entry includes the caller's wrapped DEK (to decrypt the old payload)
// and the full list of current recipients to re-wrap the fresh DEK for.
func (r *EncryptedSecretRepository) GetPendingGroupRotations(ctx context.Context, callerUserID string) ([]PendingGroupRotation, error) {
	query := `
		SELECT es.id, es.encrypted_payload, es.encryption_iv,
			esk_self.wrapped_dek AS caller_wrapped_dek,
			sg.id AS signal_group_id, sg.group_name, sg.connection_id,
			esk_all.user_id AS recipient_user_id,
			uek.public_key AS recipient_public_key
		FROM encrypted_secret_keys esk_self
		INNER JOIN encrypted_secrets es ON esk_self.secret_id = es.id
		INNER JOIN encrypted_secret_keys esk_all ON esk_all.secret_id = es.id
		INNER JOIN user_encryption_keys uek ON esk_all.user_id = uek.user_id
		LEFT JOIN signal_groups sg ON es.signal_group_id = sg.id
		WHERE esk_self.user_id = ?
		AND esk_self.group_rotation_pending = TRUE
		ORDER BY es.id, esk_all.user_id
	`

	rows, err := r.db.QueryContext(ctx, query, callerUserID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	rotationMap := make(map[string]*PendingGroupRotation)
	var order []string

	for rows.Next() {
		var secretID, encryptedPayload, encryptionIV, callerWrappedDEK string
		var groupID, groupName, connectionID *string
		var recipientUserID, recipientPublicKey string

		if err := rows.Scan(
			&secretID, &encryptedPayload, &encryptionIV, &callerWrappedDEK,
			&groupID, &groupName, &connectionID,
			&recipientUserID, &recipientPublicKey,
		); err != nil {
			return nil, err
		}

		if _, exists := rotationMap[secretID]; !exists {
			rotationMap[secretID] = &PendingGroupRotation{
				SecretID:         secretID,
				EncryptedPayload: encryptedPayload,
				EncryptionIV:     encryptionIV,
				CallerWrappedDEK: callerWrappedDEK,
				GroupID:          groupID,
				GroupName:        groupName,
				ConnectionID:     connectionID,
				Recipients:       []models.PublicKeyEntry{},
			}
			order = append(order, secretID)
		}

		rotationMap[secretID].Recipients = append(rotationMap[secretID].Recipients, models.PublicKeyEntry{
			UserID:    recipientUserID,
			PublicKey: recipientPublicKey,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	results := make([]PendingGroupRotation, 0, len(order))
	for _, id := range order {
		results = append(results, *rotationMap[id])
	}
	return results, nil
}

// SubmitGroupRotation validates that the caller is a pending group-rotation survivor for the secret,
// then atomically replaces the encrypted payload and all wrapped keys. The submitted wrapped_keys set
// must EXACTLY match the authoritative current-recipient set for the secret (every surviving recipient,
// including the caller, and no one else). This prevents a survivor from dropping other survivors
// (locking them out of the re-encrypted payload) or re-granting access to a removed/arbitrary user.
// All checks and the destructive delete/re-insert run in a single transaction to avoid TOCTOU races.
func (r *EncryptedSecretRepository) SubmitGroupRotation(ctx context.Context, secretID, callerUserID, payload, iv string, wrappedKeys []models.WrappedKeyEntry) error {
	return r.db.Transaction(ctx, func(tx *sql.Tx) error {
		// 1. The caller must be a flagged survivor of a pending group rotation for this secret.
		var count int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM encrypted_secret_keys
			WHERE secret_id = ? AND user_id = ? AND group_rotation_pending = TRUE
		`, secretID, callerUserID).Scan(&count); err != nil {
			return fmt.Errorf("check pending group rotation: %w", err)
		}
		if count == 0 {
			return fmt.Errorf("no pending group rotation for secret %s caller %s", secretID, callerUserID)
		}

		// 2. Load the authoritative current-recipient set (rows survive after removed members were deleted).
		rows, err := tx.QueryContext(ctx, `SELECT user_id FROM encrypted_secret_keys WHERE secret_id = ?`, secretID)
		if err != nil {
			return fmt.Errorf("load current recipients: %w", err)
		}
		currentRecipients := make(map[string]bool)
		for rows.Next() {
			var uid string
			if err := rows.Scan(&uid); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan current recipient: %w", err)
			}
			currentRecipients[uid] = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate current recipients: %w", err)
		}
		_ = rows.Close()

		// 3. The submitted wrapped_keys set must exactly match the current recipient set.
		submitted := make(map[string]bool, len(wrappedKeys))
		for _, wk := range wrappedKeys {
			if wk.UserID == "" || wk.WrappedDEK == "" {
				return fmt.Errorf("wrapped_keys entries must have a user_id and wrapped_dek")
			}
			if submitted[wk.UserID] {
				return fmt.Errorf("duplicate wrapped_key entry for user %s", wk.UserID)
			}
			submitted[wk.UserID] = true
			if !currentRecipients[wk.UserID] {
				return fmt.Errorf("wrapped_keys includes user %s who is not a current recipient", wk.UserID)
			}
		}
		for uid := range currentRecipients {
			if !submitted[uid] {
				return fmt.Errorf("wrapped_keys is missing surviving recipient %s", uid)
			}
		}

		// 4. Replace the payload and re-insert exactly the validated wrapped keys (clearing pending flags).
		if _, err := tx.ExecContext(ctx, `
			UPDATE encrypted_secrets
			SET encrypted_payload = ?, encryption_iv = ?, updated_by = ?, updated_at = ?
			WHERE id = ?
		`, payload, iv, callerUserID, time.Now().UTC(), secretID); err != nil {
			return fmt.Errorf("update payload: %w", err)
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM encrypted_secret_keys WHERE secret_id = ?`, secretID); err != nil {
			return fmt.Errorf("delete old keys: %w", err)
		}

		now := time.Now().UTC()
		for _, wk := range wrappedKeys {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO encrypted_secret_keys (secret_id, user_id, wrapped_dek, created_at)
				VALUES (?, ?, ?, ?)
			`, secretID, wk.UserID, wk.WrappedDEK, now); err != nil {
				return fmt.Errorf("insert wrapped key for user %s: %w", wk.UserID, err)
			}
		}

		return nil
	})
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

	// Flag group_rotation_pending for surviving users so they know to generate a fresh DEK
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
			SET group_rotation_pending = TRUE
			WHERE secret_id IN (%s)
			AND user_id IN (%s)
		`, strings.Join(secretPlaceholders, ","), strings.Join(userPlaceholders, ","))

		_, err = tx.ExecContext(ctx, updateQuery, args...)
		if err != nil {
			return fmt.Errorf("flag group rotation for survivors: %w", err)
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
		SET esk.group_rotation_pending = TRUE
		WHERE sg.owner_group_id = ?
	`
	if _, err := tx.ExecContext(ctx, flagQuery, groupID); err != nil {
		return fmt.Errorf("flag group rotation for survivors: %w", err)
	}

	return nil
}

// GetSecretsByGroupID retrieves all encrypted secrets for a signal group with their wrapped keys
type SecretWithKeys struct {
	ID          string        `json:"id"`
	WrappedKeys []WrappedKey  `json:"wrapped_keys"`
}

// WrappedKey represents a wrapped DEK for a user
type WrappedKey struct {
	UserID     string `json:"user_id"`
	WrappedDEK string `json:"wrapped_dek"`
}

func (r *EncryptedSecretRepository) GetSecretsByGroupID(ctx context.Context, groupID string) ([]SecretWithKeys, error) {
	query := `
		SELECT es.id, esk.user_id, esk.wrapped_dek
		FROM encrypted_secrets es
		INNER JOIN signal_groups sg ON es.signal_group_id = sg.id
		INNER JOIN encrypted_secret_keys esk ON es.id = esk.secret_id
		WHERE sg.id = ?
		ORDER BY es.id, esk.user_id
	`

	rows, err := r.db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	secretMap := make(map[string]*SecretWithKeys)
	var order []string

	for rows.Next() {
		var secretID, userID, wrappedDEK string
		if err := rows.Scan(&secretID, &userID, &wrappedDEK); err != nil {
			return nil, err
		}

		if _, exists := secretMap[secretID]; !exists {
			secretMap[secretID] = &SecretWithKeys{
				ID:          secretID,
				WrappedKeys: []WrappedKey{},
			}
			order = append(order, secretID)
		}

		secretMap[secretID].WrappedKeys = append(secretMap[secretID].WrappedKeys, WrappedKey{
			UserID:     userID,
			WrappedDEK: wrappedDEK,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	results := make([]SecretWithKeys, 0, len(order))
	for _, id := range order {
		results = append(results, *secretMap[id])
	}
	return results, nil
}
