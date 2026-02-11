package services

import (
	"context"
	"testing"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// mockQueue implements NotificationQueue for testing
type mockQueue struct {
	notifications []*models.EmailNotification
	sentRecently  map[string]bool
	contentSent   map[string]bool
}

func newMockQueue() *mockQueue {
	return &mockQueue{
		notifications: make([]*models.EmailNotification, 0),
		sentRecently:  make(map[string]bool),
		contentSent:   make(map[string]bool),
	}
}

func (q *mockQueue) Enqueue(ctx context.Context, n *models.EmailNotification) error {
	q.notifications = append(q.notifications, n)
	return nil
}

func (q *mockQueue) Dequeue(ctx context.Context, limit int) ([]*models.EmailNotification, error) {
	if len(q.notifications) == 0 {
		return nil, nil
	}

	count := limit
	if count > len(q.notifications) {
		count = len(q.notifications)
	}

	result := q.notifications[:count]
	q.notifications = q.notifications[count:]
	return result, nil
}

func (q *mockQueue) Ack(ctx context.Context, id string, contentHash string) error {
	if contentHash != "" {
		q.contentSent[contentHash] = true
	}
	return nil
}

func (q *mockQueue) Nack(ctx context.Context, id string, errorMsg string) error {
	return nil
}

func (q *mockQueue) WasSentRecently(ctx context.Context, userID, notificationType, resourceID string, within time.Duration) (bool, error) {
	key := userID + ":" + notificationType + ":" + resourceID
	return q.sentRecently[key], nil
}

func (q *mockQueue) WasContentSentRecently(ctx context.Context, contentHash string, within time.Duration) (bool, error) {
	return q.contentSent[contentHash], nil
}

func (q *mockQueue) QueueDepth(ctx context.Context) (int64, error) {
	return int64(len(q.notifications)), nil
}

func TestNotificationService_QueueInviteLinkUpdatedEvent(t *testing.T) {
	ctx := context.Background()
	queue := newMockQueue()
	service := NewNotificationService(queue)

	err := service.QueueInviteLinkUpdatedEvent(ctx, "group-123", "region-456")
	if err != nil {
		t.Fatalf("QueueInviteLinkUpdatedEvent failed: %v", err)
	}

	if len(queue.notifications) != 1 {
		t.Fatalf("Expected 1 notification, got %d", len(queue.notifications))
	}

	n := queue.notifications[0]

	// Fan-out events have empty UserID
	if n.UserID != "" {
		t.Errorf("Expected empty UserID for fan-out event, got '%s'", n.UserID)
	}

	if n.NotificationType != models.NotificationTypeInviteLinkUpdated {
		t.Errorf("Expected notification type 'invite_link_updated', got '%s'", n.NotificationType)
	}

	if n.ResourceID == nil || *n.ResourceID != "region-456" {
		t.Errorf("Expected ResourceID 'region-456', got '%v'", n.ResourceID)
	}

	if n.Status != models.NotificationStatusQueued {
		t.Errorf("Expected status 'queued', got '%s'", n.Status)
	}
}

func TestNotificationService_QueueVerificationComplete(t *testing.T) {
	ctx := context.Background()
	queue := newMockQueue()
	service := NewNotificationService(queue)

	err := service.QueueVerificationComplete(ctx, "user-123", "region-456")
	if err != nil {
		t.Fatalf("QueueVerificationComplete failed: %v", err)
	}

	if len(queue.notifications) != 1 {
		t.Fatalf("Expected 1 notification, got %d", len(queue.notifications))
	}

	n := queue.notifications[0]

	if n.UserID != "user-123" {
		t.Errorf("Expected UserID 'user-123', got '%s'", n.UserID)
	}

	if n.NotificationType != models.NotificationTypeVerificationComplete {
		t.Errorf("Expected notification type 'verification_complete', got '%s'", n.NotificationType)
	}

	if n.ResourceID == nil || *n.ResourceID != "region-456" {
		t.Errorf("Expected ResourceID 'region-456', got '%v'", n.ResourceID)
	}
}

func TestNotificationService_QueueVouchReceived(t *testing.T) {
	ctx := context.Background()
	queue := newMockQueue()
	service := NewNotificationService(queue)

	err := service.QueueVouchReceived(ctx, "user-123", "voucher-456", "region-789")
	if err != nil {
		t.Fatalf("QueueVouchReceived failed: %v", err)
	}

	if len(queue.notifications) != 1 {
		t.Fatalf("Expected 1 notification, got %d", len(queue.notifications))
	}

	n := queue.notifications[0]

	if n.UserID != "user-123" {
		t.Errorf("Expected UserID 'user-123', got '%s'", n.UserID)
	}

	if n.NotificationType != models.NotificationTypeVouchReceived {
		t.Errorf("Expected notification type 'vouch_received', got '%s'", n.NotificationType)
	}

	// ResourceID should be the voucher ID (for lookup)
	if n.ResourceID == nil || *n.ResourceID != "voucher-456" {
		t.Errorf("Expected ResourceID 'voucher-456', got '%v'", n.ResourceID)
	}
}

func TestNotificationService_QueueVouchComplete(t *testing.T) {
	ctx := context.Background()
	queue := newMockQueue()
	service := NewNotificationService(queue)

	err := service.QueueVouchComplete(ctx, "user-123", "region-456")
	if err != nil {
		t.Fatalf("QueueVouchComplete failed: %v", err)
	}

	if len(queue.notifications) != 1 {
		t.Fatalf("Expected 1 notification, got %d", len(queue.notifications))
	}

	n := queue.notifications[0]

	if n.UserID != "user-123" {
		t.Errorf("Expected UserID 'user-123', got '%s'", n.UserID)
	}

	if n.NotificationType != models.NotificationTypeVouchComplete {
		t.Errorf("Expected notification type 'vouch_complete', got '%s'", n.NotificationType)
	}

	if n.ResourceID == nil || *n.ResourceID != "region-456" {
		t.Errorf("Expected ResourceID 'region-456', got '%v'", n.ResourceID)
	}
}

func TestNotificationService_UUIDs(t *testing.T) {
	ctx := context.Background()
	queue := newMockQueue()
	service := NewNotificationService(queue)

	// Queue multiple notifications
	_ = service.QueueVerificationComplete(ctx, "user-1", "region-1")
	_ = service.QueueVerificationComplete(ctx, "user-2", "region-2")

	if len(queue.notifications) != 2 {
		t.Fatalf("Expected 2 notifications, got %d", len(queue.notifications))
	}

	// Verify UUIDs are unique
	if queue.notifications[0].ID == queue.notifications[1].ID {
		t.Error("Expected unique IDs for each notification")
	}

	// Verify IDs are non-empty
	if queue.notifications[0].ID == "" || queue.notifications[1].ID == "" {
		t.Error("Expected non-empty notification IDs")
	}
}
