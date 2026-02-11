package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// DatabaseQueue implements the NotificationQueue interface using the email_notifications table
type DatabaseQueue struct {
	db *DB
}

// NewDatabaseQueue creates a new DatabaseQueue
func NewDatabaseQueue(db *DB) *DatabaseQueue {
	return &DatabaseQueue{db: db}
}

// Enqueue adds a notification to the queue
func (q *DatabaseQueue) Enqueue(ctx context.Context, n *models.EmailNotification) error {
	query := `
		INSERT INTO email_notifications (id, user_id, notification_type, resource_type, resource_id, status, queued_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	// Convert empty UserID to NULL for fan-out events
	var userID interface{}
	if n.UserID == "" {
		userID = nil
	} else {
		userID = n.UserID
	}

	_, err := q.db.ExecContext(ctx, query,
		n.ID,
		userID,
		n.NotificationType,
		n.ResourceType,
		n.ResourceID,
		models.NotificationStatusQueued,
		n.QueuedAt,
	)
	return err
}

// Dequeue retrieves pending notifications for processing
func (q *DatabaseQueue) Dequeue(ctx context.Context, limit int) ([]*models.EmailNotification, error) {
	query := `
		SELECT id, user_id, notification_type, resource_type, resource_id, status, queued_at, sent_at, error_message, content_hash
		FROM email_notifications
		WHERE status = ?
		ORDER BY queued_at ASC
		LIMIT ?
	`
	rows, err := q.db.QueryContext(ctx, query, models.NotificationStatusQueued, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var notifications []*models.EmailNotification
	for rows.Next() {
		n := &models.EmailNotification{}
		var userID sql.NullString
		var contentHash sql.NullString
		err := rows.Scan(
			&n.ID,
			&userID,
			&n.NotificationType,
			&n.ResourceType,
			&n.ResourceID,
			&n.Status,
			&n.QueuedAt,
			&n.SentAt,
			&n.ErrorMessage,
			&contentHash,
		)
		if err != nil {
			return nil, err
		}
		if userID.Valid {
			n.UserID = userID.String
		}
		if contentHash.Valid {
			n.ContentHash = &contentHash.String
		}
		notifications = append(notifications, n)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return notifications, nil
}

// Ack marks a notification as successfully processed
func (q *DatabaseQueue) Ack(ctx context.Context, id string, contentHash string) error {
	var query string
	var args []interface{}

	if contentHash != "" {
		query = `
			UPDATE email_notifications
			SET status = ?, sent_at = ?, content_hash = ?
			WHERE id = ?
		`
		args = []interface{}{models.NotificationStatusSent, time.Now().UTC(), contentHash, id}
	} else {
		query = `
			UPDATE email_notifications
			SET status = ?, sent_at = ?
			WHERE id = ?
		`
		args = []interface{}{models.NotificationStatusSent, time.Now().UTC(), id}
	}

	_, err := q.db.ExecContext(ctx, query, args...)
	return err
}

// Nack marks a notification as failed (will be retried)
func (q *DatabaseQueue) Nack(ctx context.Context, id string, errorMsg string) error {
	query := `
		UPDATE email_notifications
		SET status = ?, error_message = ?
		WHERE id = ?
	`
	_, err := q.db.ExecContext(ctx, query, models.NotificationStatusFailed, errorMsg, id)
	return err
}

// WasSentRecently checks if a similar notification was sent recently (rate limiting)
func (q *DatabaseQueue) WasSentRecently(ctx context.Context, userID, notificationType, resourceID string, within time.Duration) (bool, error) {
	cutoff := time.Now().UTC().Add(-within)

	var query string
	var args []interface{}

	if resourceID != "" {
		query = `
			SELECT COUNT(*) FROM email_notifications
			WHERE user_id = ? AND notification_type = ? AND resource_id = ? AND status = ? AND sent_at > ?
		`
		args = []interface{}{userID, notificationType, resourceID, models.NotificationStatusSent, cutoff}
	} else {
		query = `
			SELECT COUNT(*) FROM email_notifications
			WHERE user_id = ? AND notification_type = ? AND resource_id IS NULL AND status = ? AND sent_at > ?
		`
		args = []interface{}{userID, notificationType, models.NotificationStatusSent, cutoff}
	}

	var count int
	err := q.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// WasContentSentRecently checks if identical email content was sent to user recently (deduplication)
func (q *DatabaseQueue) WasContentSentRecently(ctx context.Context, contentHash string, within time.Duration) (bool, error) {
	cutoff := time.Now().UTC().Add(-within)

	query := `
		SELECT COUNT(*) FROM email_notifications
		WHERE content_hash = ? AND status = ? AND sent_at > ?
	`

	var count int
	err := q.db.QueryRowContext(ctx, query, contentHash, models.NotificationStatusSent, cutoff).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// QueueDepth returns the number of queued (pending) notifications
func (q *DatabaseQueue) QueueDepth(ctx context.Context) (int64, error) {
	var count int64
	err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM email_notifications WHERE status = ?`,
		models.NotificationStatusQueued).Scan(&count)
	return count, err
}

// RetryFailed re-queues failed notifications for retry
func (q *DatabaseQueue) RetryFailed(ctx context.Context, failedAfter time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-failedAfter)

	query := `
		UPDATE email_notifications
		SET status = ?, error_message = NULL
		WHERE status = ? AND queued_at < ?
	`

	result, err := q.db.ExecContext(ctx, query, models.NotificationStatusQueued, models.NotificationStatusFailed, cutoff)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}
