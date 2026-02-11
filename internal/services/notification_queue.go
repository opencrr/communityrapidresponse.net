package services

import (
	"context"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// NotificationQueue abstracts the notification queue backend.
// Implementations: DatabaseQueue (MVP), SQSQueue (future), RabbitMQQueue (future)
type NotificationQueue interface {
	// Enqueue adds a notification to the queue
	Enqueue(ctx context.Context, notification *models.EmailNotification) error

	// Dequeue retrieves pending notifications (for worker processing)
	Dequeue(ctx context.Context, limit int) ([]*models.EmailNotification, error)

	// Ack marks a notification as successfully processed
	Ack(ctx context.Context, id string, contentHash string) error

	// Nack marks a notification as failed (will be retried)
	Nack(ctx context.Context, id string, errorMsg string) error

	// WasSentRecently checks rate limiting (type/resource based)
	WasSentRecently(ctx context.Context, userID, notificationType, resourceID string, within time.Duration) (bool, error)

	// WasContentSentRecently checks content-based deduplication
	WasContentSentRecently(ctx context.Context, contentHash string, within time.Duration) (bool, error)

	// QueueDepth returns the number of queued (pending) notifications
	QueueDepth(ctx context.Context) (int64, error)
}
