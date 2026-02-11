package services

import (
	"context"
	"testing"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// mockUserLookup implements UserLookup for testing
type mockUserLookup struct {
	users            map[string]*models.User
	verifiedUsers    map[string][]*models.User
	getByIDCalls     int
	getVerifiedCalls int
}

func newMockUserLookup() *mockUserLookup {
	return &mockUserLookup{
		users:         make(map[string]*models.User),
		verifiedUsers: make(map[string][]*models.User),
	}
}

func (m *mockUserLookup) GetByID(ctx context.Context, id string) (*models.User, error) {
	m.getByIDCalls++
	if user, ok := m.users[id]; ok {
		return user, nil
	}
	return nil, nil
}

func (m *mockUserLookup) GetVerifiedUsersInRegion(ctx context.Context, regionID string, offset, limit int) ([]*models.User, error) {
	m.getVerifiedCalls++
	users, ok := m.verifiedUsers[regionID]
	if !ok {
		return nil, nil
	}

	if offset >= len(users) {
		return nil, nil
	}

	end := offset + limit
	if end > len(users) {
		end = len(users)
	}

	return users[offset:end], nil
}

// mockRegionLookup implements RegionLookup for testing
type mockRegionLookup struct {
	regions map[string]*models.GeographicRegion
}

func newMockRegionLookup() *mockRegionLookup {
	return &mockRegionLookup{
		regions: make(map[string]*models.GeographicRegion),
	}
}

func (m *mockRegionLookup) GetByID(ctx context.Context, id string) (*models.GeographicRegion, error) {
	if region, ok := m.regions[id]; ok {
		return region, nil
	}
	return nil, nil
}

// mockEmailService implements EmailServiceInterface for testing
type mockEmailService struct {
	sentEmails []EmailMessage
}

func newMockEmailService() *mockEmailService {
	return &mockEmailService{
		sentEmails: make([]EmailMessage, 0),
	}
}

func (m *mockEmailService) Send(ctx context.Context, msg *EmailMessage) error {
	m.sentEmails = append(m.sentEmails, *msg)
	return nil
}

func (m *mockEmailService) SendVerificationEmail(ctx context.Context, toEmail, token string) error {
	return nil
}

func (m *mockEmailService) IsEnabled() bool {
	return true
}

func (m *mockEmailService) Backend() string {
	return "mock"
}

func TestNotificationWorker_ProcessNotification(t *testing.T) {
	ctx := context.Background()
	queue := newMockQueue()
	emailService := newMockEmailService()
	templates := NewEmailTemplates("Test App", "http://localhost/login")
	userLookup := newMockUserLookup()
	regionLookup := newMockRegionLookup()

	cfg := &config.NotificationConfig{
		WorkerInterval:    time.Minute,
		BatchSize:         10,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	worker := NewNotificationWorker(queue, emailService, templates, userLookup, regionLookup, cfg)

	// Set up test user
	userLookup.users["user-123"] = &models.User{
		ID:       "user-123",
		Email:    "test@example.com",
		Username: "testuser",
	}

	// Set up test region
	regionLookup.regions["region-456"] = &models.GeographicRegion{
		ID:   "region-456",
		Name: "Test Neighborhood",
	}

	// Queue a notification
	resourceType := "region"
	resourceID := "region-456"
	notification := &models.EmailNotification{
		ID:               "notif-1",
		UserID:           "user-123",
		NotificationType: models.NotificationTypeVerificationComplete,
		ResourceType:     &resourceType,
		ResourceID:       &resourceID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}
	_ = queue.Enqueue(ctx, notification)

	// Process the queue
	_ = worker.processQueue(ctx)

	// Check that email was sent
	if len(emailService.sentEmails) != 1 {
		t.Fatalf("Expected 1 email sent, got %d", len(emailService.sentEmails))
	}

	email := emailService.sentEmails[0]
	if email.To != "test@example.com" {
		t.Errorf("Expected email to 'test@example.com', got '%s'", email.To)
	}

	if email.Subject == "" {
		t.Error("Expected non-empty subject")
	}
}

func TestNotificationWorker_RateLimiting(t *testing.T) {
	ctx := context.Background()
	queue := newMockQueue()
	emailService := newMockEmailService()
	templates := NewEmailTemplates("Test App", "http://localhost/login")
	userLookup := newMockUserLookup()
	regionLookup := newMockRegionLookup()

	cfg := &config.NotificationConfig{
		WorkerInterval:    time.Minute,
		BatchSize:         10,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	worker := NewNotificationWorker(queue, emailService, templates, userLookup, regionLookup, cfg)

	// Set up test user
	userLookup.users["user-123"] = &models.User{
		ID:       "user-123",
		Email:    "test@example.com",
		Username: "testuser",
	}

	// Mark as already sent recently
	queue.sentRecently["user-123:verification_complete:region-456"] = true

	// Queue a notification
	resourceType := "region"
	resourceID := "region-456"
	notification := &models.EmailNotification{
		ID:               "notif-1",
		UserID:           "user-123",
		NotificationType: models.NotificationTypeVerificationComplete,
		ResourceType:     &resourceType,
		ResourceID:       &resourceID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}
	_ = queue.Enqueue(ctx, notification)

	// Process the queue
	_ = worker.processQueue(ctx)

	// Email should NOT be sent due to rate limiting
	if len(emailService.sentEmails) != 0 {
		t.Fatalf("Expected 0 emails sent (rate limited), got %d", len(emailService.sentEmails))
	}
}

func TestNotificationWorker_BlockedUser(t *testing.T) {
	ctx := context.Background()
	queue := newMockQueue()
	emailService := newMockEmailService()
	templates := NewEmailTemplates("Test App", "http://localhost/login")
	userLookup := newMockUserLookup()
	regionLookup := newMockRegionLookup()

	cfg := &config.NotificationConfig{
		WorkerInterval:    time.Minute,
		BatchSize:         10,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	worker := NewNotificationWorker(queue, emailService, templates, userLookup, regionLookup, cfg)

	// Set up blocked user
	userLookup.users["user-123"] = &models.User{
		ID:        "user-123",
		Email:     "test@example.com",
		Username:  "testuser",
		IsBlocked: true,
	}

	// Queue a notification
	resourceType := "region"
	resourceID := "region-456"
	notification := &models.EmailNotification{
		ID:               "notif-1",
		UserID:           "user-123",
		NotificationType: models.NotificationTypeVerificationComplete,
		ResourceType:     &resourceType,
		ResourceID:       &resourceID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}
	_ = queue.Enqueue(ctx, notification)

	// Process the queue
	_ = worker.processQueue(ctx)

	// Email should NOT be sent for blocked user
	if len(emailService.sentEmails) != 0 {
		t.Fatalf("Expected 0 emails sent (user blocked), got %d", len(emailService.sentEmails))
	}
}

func TestNotificationWorker_FanOut(t *testing.T) {
	ctx := context.Background()
	queue := newMockQueue()
	emailService := newMockEmailService()
	templates := NewEmailTemplates("Test App", "http://localhost/login")
	userLookup := newMockUserLookup()
	regionLookup := newMockRegionLookup()

	cfg := &config.NotificationConfig{
		WorkerInterval:    time.Minute,
		BatchSize:         10,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	worker := NewNotificationWorker(queue, emailService, templates, userLookup, regionLookup, cfg)

	// Set up verified users in region
	userLookup.verifiedUsers["region-456"] = []*models.User{
		{ID: "user-1", Email: "user1@example.com", Username: "user1"},
		{ID: "user-2", Email: "user2@example.com", Username: "user2"},
		{ID: "user-3", Email: "user3@example.com", Username: "user3"},
	}

	// Queue a fan-out event (empty UserID)
	resourceType := "signal_group"
	resourceID := "region-456"
	fanOutEvent := &models.EmailNotification{
		ID:               "fanout-1",
		UserID:           "", // Empty = fan-out event
		NotificationType: models.NotificationTypeInviteLinkUpdated,
		ResourceType:     &resourceType,
		ResourceID:       &resourceID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}
	_ = queue.Enqueue(ctx, fanOutEvent)

	// Process the queue - this should expand the fan-out event
	_ = worker.processQueue(ctx)

	// Check that individual notifications were queued
	notifications, _ := queue.Dequeue(ctx, 100)

	if len(notifications) != 3 {
		t.Fatalf("Expected 3 notifications after fan-out, got %d", len(notifications))
	}

	// Verify each notification has a user ID
	for i, n := range notifications {
		if n.UserID == "" {
			t.Errorf("Notification %d: expected non-empty UserID", i)
		}
		if n.NotificationType != models.NotificationTypeInviteLinkUpdated {
			t.Errorf("Notification %d: expected type 'invite_link_updated', got '%s'", i, n.NotificationType)
		}
	}
}

func TestHashEmailContent(t *testing.T) {
	// Same content should produce same hash
	hash1 := hashEmailContent("user-1", "Subject", "Body content")
	hash2 := hashEmailContent("user-1", "Subject", "Body content")

	if hash1 != hash2 {
		t.Error("Expected identical content to produce identical hash")
	}

	// Different user should produce different hash
	hash3 := hashEmailContent("user-2", "Subject", "Body content")

	if hash1 == hash3 {
		t.Error("Expected different user to produce different hash")
	}

	// Different subject should produce different hash
	hash4 := hashEmailContent("user-1", "Different Subject", "Body content")

	if hash1 == hash4 {
		t.Error("Expected different subject to produce different hash")
	}

	// Hash should be 64 characters (SHA-256 hex)
	if len(hash1) != 64 {
		t.Errorf("Expected hash length 64, got %d", len(hash1))
	}
}
