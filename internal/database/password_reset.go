package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrResetTokenNotFound = errors.New("password reset token not found")
	ErrResetTokenExpired  = errors.New("password reset token has expired")
	ErrResetTokenUsed     = errors.New("password reset token has already been used")
	ErrTooManyResetTokens = errors.New("too many active password reset tokens")
)

// PasswordResetToken represents a password reset token record
type PasswordResetToken struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	TokenHash string     `json:"token_hash"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

// PasswordResetRepository handles password reset token database operations
type PasswordResetRepository struct {
	db *DB
}

// NewPasswordResetRepository creates a new password reset repository
func NewPasswordResetRepository(db *DB) *PasswordResetRepository {
	return &PasswordResetRepository{db: db}
}

// Create inserts a new password reset token
func (r *PasswordResetRepository) Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*PasswordResetToken, error) {
	token := &PasswordResetToken{
		ID:        uuid.New().String(),
		UserID:    userID,
		TokenHash: tokenHash,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: expiresAt,
	}

	query := `
		INSERT INTO password_reset_tokens (id, user_id, token_hash, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := r.db.ExecContext(ctx, query,
		token.ID, token.UserID, token.TokenHash, token.CreatedAt, token.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}

	return token, nil
}

// GetByTokenHash retrieves a token by its hash, rejecting used or expired tokens
func (r *PasswordResetRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*PasswordResetToken, error) {
	query := `
		SELECT id, user_id, token_hash, created_at, expires_at, used_at
		FROM password_reset_tokens
		WHERE token_hash = ?
	`

	token := &PasswordResetToken{}
	err := r.db.QueryRowContext(ctx, query, tokenHash).Scan(
		&token.ID, &token.UserID, &token.TokenHash,
		&token.CreatedAt, &token.ExpiresAt, &token.UsedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrResetTokenNotFound
	}
	if err != nil {
		return nil, err
	}

	if token.UsedAt != nil {
		return nil, ErrResetTokenUsed
	}
	if time.Now().UTC().After(token.ExpiresAt) {
		return nil, ErrResetTokenExpired
	}

	return token, nil
}

// MarkUsed sets the used_at timestamp for a token
func (r *PasswordResetRepository) MarkUsed(ctx context.Context, tokenID string) error {
	query := `UPDATE password_reset_tokens SET used_at = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, time.Now().UTC(), tokenID)
	return err
}

// InvalidateForUser marks all unused tokens for a user as used
func (r *PasswordResetRepository) InvalidateForUser(ctx context.Context, userID string) error {
	query := `
		UPDATE password_reset_tokens
		SET used_at = ?
		WHERE user_id = ? AND used_at IS NULL
	`
	_, err := r.db.ExecContext(ctx, query, time.Now().UTC(), userID)
	return err
}

// CountActiveForUser counts unexpired, unused tokens for a user
func (r *PasswordResetRepository) CountActiveForUser(ctx context.Context, userID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM password_reset_tokens
		WHERE user_id = ? AND used_at IS NULL AND expires_at > ?
	`
	var count int
	err := r.db.QueryRowContext(ctx, query, userID, time.Now().UTC()).Scan(&count)
	return count, err
}

// --- Transactional methods for race condition prevention ---

// CountActiveForUserForUpdate counts active tokens with a lock for rate limiting
func (r *PasswordResetRepository) CountActiveForUserForUpdate(ctx context.Context, tx *sql.Tx, userID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM password_reset_tokens
		WHERE user_id = ? AND used_at IS NULL AND expires_at > ?
		FOR UPDATE
	`
	var count int
	err := tx.QueryRowContext(ctx, query, userID, time.Now().UTC()).Scan(&count)
	return count, err
}

// CreateTx inserts a new password reset token within a transaction
func (r *PasswordResetRepository) CreateTx(ctx context.Context, tx *sql.Tx, userID, tokenHash string, expiresAt time.Time) (*PasswordResetToken, error) {
	token := &PasswordResetToken{
		ID:        uuid.New().String(),
		UserID:    userID,
		TokenHash: tokenHash,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: expiresAt,
	}

	query := `
		INSERT INTO password_reset_tokens (id, user_id, token_hash, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := tx.ExecContext(ctx, query,
		token.ID, token.UserID, token.TokenHash, token.CreatedAt, token.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}

	return token, nil
}

// CleanupExpired deletes tokens that have been expired for more than 24 hours
func (r *PasswordResetRepository) CleanupExpired(ctx context.Context) (int64, error) {
	query := `
		DELETE FROM password_reset_tokens
		WHERE expires_at < ?
	`
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	result, err := r.db.ExecContext(ctx, query, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
