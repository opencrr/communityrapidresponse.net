package services

import (
	"context"
	"fmt"
	"strings"
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

	worker := NewNotificationWorker(queue, emailService, templates, userLookup, regionLookup, nil, cfg)

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

	worker := NewNotificationWorker(queue, emailService, templates, userLookup, regionLookup, nil, cfg)

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

	worker := NewNotificationWorker(queue, emailService, templates, userLookup, regionLookup, nil, cfg)

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

	worker := NewNotificationWorker(queue, emailService, templates, userLookup, regionLookup, nil, cfg)

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

// mockSecretKeyLookup implements SecretKeyLookup for testing
type mockSecretKeyLookup struct {
	sharedUsers map[string][]string // userID → list of user IDs sharing secrets
	err         error
}

func newMockSecretKeyLookup() *mockSecretKeyLookup {
	return &mockSecretKeyLookup{sharedUsers: make(map[string][]string)}
}

func (m *mockSecretKeyLookup) GetUsersWithSharedSecrets(ctx context.Context, userID string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.sharedUsers[userID], nil
}

// nackTrackingQueue wraps mockQueue to track Nack calls for test assertions
type nackTrackingQueue struct {
	*mockQueue
	nacks []nackRecord
}

type nackRecord struct {
	id       string
	errorMsg string
}

func newNackTrackingQueue() *nackTrackingQueue {
	return &nackTrackingQueue{
		mockQueue: newMockQueue(),
		nacks:     make([]nackRecord, 0),
	}
}

func (q *nackTrackingQueue) Nack(ctx context.Context, id string, errorMsg string) error {
	q.nacks = append(q.nacks, nackRecord{id: id, errorMsg: errorMsg})
	return nil
}

// ackTrackingQueue wraps mockQueue to track Ack calls for test assertions
type ackTrackingQueue struct {
	*mockQueue
	acks []string
}

func newAckTrackingQueue() *ackTrackingQueue {
	return &ackTrackingQueue{
		mockQueue: newMockQueue(),
		acks:      make([]string, 0),
	}
}

func (q *ackTrackingQueue) Ack(ctx context.Context, id string, contentHash string) error {
	q.acks = append(q.acks, id)
	return q.mockQueue.Ack(ctx, id, contentHash)
}

func TestNotificationWorker_RekeyingFanOut(t *testing.T) {
	ctx := context.Background()
	queue := newMockQueue()
	emailService := newMockEmailService()
	templates := NewEmailTemplates("Test App", "http://localhost/login")
	userLookup := newMockUserLookup()
	regionLookup := newMockRegionLookup()
	secretKeyLookup := newMockSecretKeyLookup()

	cfg := &config.NotificationConfig{
		WorkerInterval:    time.Minute,
		BatchSize:         10,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	// Set up users sharing secrets with the rotated user
	secretKeyLookup.sharedUsers["rotated-user-1"] = []string{"user-a", "user-b", "user-c"}

	worker := NewNotificationWorker(queue, emailService, templates, userLookup, regionLookup, secretKeyLookup, cfg)

	// Queue a rekeying fan-out event (empty UserID = fan-out)
	resourceType := "encryption_key"
	rotatedUserID := "rotated-user-1"
	fanOutEvent := &models.EmailNotification{
		ID:               "rekey-fanout-1",
		UserID:           "", // Empty = fan-out event
		NotificationType: models.NotificationTypeRekeyingNeeded,
		ResourceType:     &resourceType,
		ResourceID:       &rotatedUserID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}
	_ = queue.Enqueue(ctx, fanOutEvent)

	// Process the queue - this should expand the fan-out event
	_ = worker.processQueue(ctx)

	// Check that individual notifications were queued for each user sharing secrets
	notifications, _ := queue.Dequeue(ctx, 100)

	if len(notifications) != 3 {
		t.Fatalf("Expected 3 notifications after rekeying fan-out, got %d", len(notifications))
	}

	expectedUserIDs := map[string]bool{"user-a": true, "user-b": true, "user-c": true}
	for i, n := range notifications {
		if n.UserID == "" {
			t.Errorf("Notification %d: expected non-empty UserID", i)
		}
		if !expectedUserIDs[n.UserID] {
			t.Errorf("Notification %d: unexpected UserID '%s'", i, n.UserID)
		}
		if n.NotificationType != models.NotificationTypeRekeyingNeeded {
			t.Errorf("Notification %d: expected type 'rekeying_needed', got '%s'", i, n.NotificationType)
		}
		if n.ResourceID == nil || *n.ResourceID != "rotated-user-1" {
			t.Errorf("Notification %d: expected ResourceID 'rotated-user-1', got '%v'", i, n.ResourceID)
		}
		if n.ResourceType == nil || *n.ResourceType != "encryption_key" {
			t.Errorf("Notification %d: expected ResourceType 'encryption_key', got '%v'", i, n.ResourceType)
		}
	}
}

func TestNotificationWorker_RekeyingFanOut_NoSharedSecrets(t *testing.T) {
	ctx := context.Background()
	trackingQueue := newAckTrackingQueue()
	emailService := newMockEmailService()
	templates := NewEmailTemplates("Test App", "http://localhost/login")
	userLookup := newMockUserLookup()
	regionLookup := newMockRegionLookup()
	secretKeyLookup := newMockSecretKeyLookup()

	cfg := &config.NotificationConfig{
		WorkerInterval:    time.Minute,
		BatchSize:         10,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	// No shared users for this user (empty slice)
	secretKeyLookup.sharedUsers["lonely-user"] = []string{}

	worker := NewNotificationWorker(trackingQueue, emailService, templates, userLookup, regionLookup, secretKeyLookup, cfg)

	// Queue a rekeying fan-out event
	resourceType := "encryption_key"
	rotatedUserID := "lonely-user"
	fanOutEvent := &models.EmailNotification{
		ID:               "rekey-fanout-empty",
		UserID:           "",
		NotificationType: models.NotificationTypeRekeyingNeeded,
		ResourceType:     &resourceType,
		ResourceID:       &rotatedUserID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}
	_ = trackingQueue.Enqueue(ctx, fanOutEvent)

	// Process the queue
	_ = worker.processQueue(ctx)

	// Verify the fan-out event was acked
	if len(trackingQueue.acks) != 1 {
		t.Fatalf("Expected 1 ack call, got %d", len(trackingQueue.acks))
	}
	if trackingQueue.acks[0] != "rekey-fanout-empty" {
		t.Errorf("Expected ack for 'rekey-fanout-empty', got '%s'", trackingQueue.acks[0])
	}

	// Verify no per-user notifications were queued
	remaining, _ := trackingQueue.Dequeue(ctx, 100)
	if len(remaining) != 0 {
		t.Fatalf("Expected 0 notifications queued, got %d", len(remaining))
	}
}

func TestNotificationWorker_RekeyingFanOut_MissingResourceID(t *testing.T) {
	ctx := context.Background()
	trackingQueue := newNackTrackingQueue()
	emailService := newMockEmailService()
	templates := NewEmailTemplates("Test App", "http://localhost/login")
	userLookup := newMockUserLookup()
	regionLookup := newMockRegionLookup()
	secretKeyLookup := newMockSecretKeyLookup()

	cfg := &config.NotificationConfig{
		WorkerInterval:    time.Minute,
		BatchSize:         10,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	worker := NewNotificationWorker(trackingQueue, emailService, templates, userLookup, regionLookup, secretKeyLookup, cfg)

	// Queue a rekeying fan-out event with nil ResourceID
	fanOutEvent := &models.EmailNotification{
		ID:               "rekey-fanout-no-resource",
		UserID:           "",
		NotificationType: models.NotificationTypeRekeyingNeeded,
		ResourceType:     nil,
		ResourceID:       nil, // Missing!
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}
	_ = trackingQueue.Enqueue(ctx, fanOutEvent)

	// Process the queue
	_ = worker.processQueue(ctx)

	// Verify the event was nacked
	if len(trackingQueue.nacks) != 1 {
		t.Fatalf("Expected 1 nack call, got %d", len(trackingQueue.nacks))
	}
	if trackingQueue.nacks[0].id != "rekey-fanout-no-resource" {
		t.Errorf("Expected nack for 'rekey-fanout-no-resource', got '%s'", trackingQueue.nacks[0].id)
	}
	if !strings.Contains(trackingQueue.nacks[0].errorMsg, "missing user ID") {
		t.Errorf("Expected nack error to contain 'missing user ID', got '%s'", trackingQueue.nacks[0].errorMsg)
	}
}

func TestNotificationWorker_RekeyingFanOut_NilSecretKeyLookup(t *testing.T) {
	ctx := context.Background()
	trackingQueue := newNackTrackingQueue()
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

	// Pass nil for secretKeyLookup
	worker := NewNotificationWorker(trackingQueue, emailService, templates, userLookup, regionLookup, nil, cfg)

	// Queue a rekeying fan-out event
	resourceType := "encryption_key"
	rotatedUserID := "some-user"
	fanOutEvent := &models.EmailNotification{
		ID:               "rekey-fanout-nil-lookup",
		UserID:           "",
		NotificationType: models.NotificationTypeRekeyingNeeded,
		ResourceType:     &resourceType,
		ResourceID:       &rotatedUserID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}
	_ = trackingQueue.Enqueue(ctx, fanOutEvent)

	// Process the queue
	_ = worker.processQueue(ctx)

	// Verify the event was nacked
	if len(trackingQueue.nacks) != 1 {
		t.Fatalf("Expected 1 nack call, got %d", len(trackingQueue.nacks))
	}
	if trackingQueue.nacks[0].id != "rekey-fanout-nil-lookup" {
		t.Errorf("Expected nack for 'rekey-fanout-nil-lookup', got '%s'", trackingQueue.nacks[0].id)
	}
	if !strings.Contains(trackingQueue.nacks[0].errorMsg, "secret key lookup not configured") {
		t.Errorf("Expected nack error to contain 'secret key lookup not configured', got '%s'", trackingQueue.nacks[0].errorMsg)
	}
}

func TestNotificationWorker_RekeyingFanOut_LookupError(t *testing.T) {
	ctx := context.Background()
	trackingQueue := newNackTrackingQueue()
	emailService := newMockEmailService()
	templates := NewEmailTemplates("Test App", "http://localhost/login")
	userLookup := newMockUserLookup()
	regionLookup := newMockRegionLookup()
	secretKeyLookup := newMockSecretKeyLookup()

	cfg := &config.NotificationConfig{
		WorkerInterval:    time.Minute,
		BatchSize:         10,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	// Configure the lookup to return an error
	secretKeyLookup.err = fmt.Errorf("database connection failed")

	worker := NewNotificationWorker(trackingQueue, emailService, templates, userLookup, regionLookup, secretKeyLookup, cfg)

	// Queue a rekeying fan-out event
	resourceType := "encryption_key"
	rotatedUserID := "some-user"
	fanOutEvent := &models.EmailNotification{
		ID:               "rekey-fanout-lookup-error",
		UserID:           "",
		NotificationType: models.NotificationTypeRekeyingNeeded,
		ResourceType:     &resourceType,
		ResourceID:       &rotatedUserID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}
	_ = trackingQueue.Enqueue(ctx, fanOutEvent)

	// Process the queue
	_ = worker.processQueue(ctx)

	// Verify the event was nacked with the error message
	if len(trackingQueue.nacks) != 1 {
		t.Fatalf("Expected 1 nack call, got %d", len(trackingQueue.nacks))
	}
	if trackingQueue.nacks[0].id != "rekey-fanout-lookup-error" {
		t.Errorf("Expected nack for 'rekey-fanout-lookup-error', got '%s'", trackingQueue.nacks[0].id)
	}
	if !strings.Contains(trackingQueue.nacks[0].errorMsg, "database connection failed") {
		t.Errorf("Expected nack error to contain 'database connection failed', got '%s'", trackingQueue.nacks[0].errorMsg)
	}
}

func TestNotificationWorker_RekeyingEmail(t *testing.T) {
	ctx := context.Background()
	queue := newMockQueue()
	emailService := newMockEmailService()
	templates := NewEmailTemplates("Test App", "http://localhost/login")
	userLookup := newMockUserLookup()
	regionLookup := newMockRegionLookup()
	secretKeyLookup := newMockSecretKeyLookup()

	cfg := &config.NotificationConfig{
		WorkerInterval:    time.Minute,
		BatchSize:         10,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	worker := NewNotificationWorker(queue, emailService, templates, userLookup, regionLookup, secretKeyLookup, cfg)

	// Set up test user who will receive the rekeying notification
	userLookup.users["user-recipient"] = &models.User{
		ID:       "user-recipient",
		Email:    "recipient@example.com",
		Username: "recipient",
	}

	// Queue a per-user rekeying notification (already fanned out)
	resourceType := "encryption_key"
	rotatedUserID := "rotated-user-1"
	notification := &models.EmailNotification{
		ID:               "rekey-notif-1",
		UserID:           "user-recipient",
		NotificationType: models.NotificationTypeRekeyingNeeded,
		ResourceType:     &resourceType,
		ResourceID:       &rotatedUserID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}
	_ = queue.Enqueue(ctx, notification)

	// Process the queue
	_ = worker.processQueue(ctx)

	// Check that exactly one email was sent
	if len(emailService.sentEmails) != 1 {
		t.Fatalf("Expected 1 email sent, got %d", len(emailService.sentEmails))
	}

	email := emailService.sentEmails[0]

	// Verify email recipient
	if email.To != "recipient@example.com" {
		t.Errorf("Expected email to 'recipient@example.com', got '%s'", email.To)
	}

	// Verify subject contains re-keying
	if !strings.Contains(email.Subject, "Re-keying") {
		t.Errorf("Expected subject to contain 'Re-keying', got '%s'", email.Subject)
	}

	// Verify text content contains re-keying information
	if !strings.Contains(email.TextContent, "re-keying") {
		t.Errorf("Expected text content to contain 're-keying', got '%s'", email.TextContent)
	}

	// Verify text content contains login URL
	if !strings.Contains(email.TextContent, "http://localhost/login") {
		t.Errorf("Expected text content to contain login URL, got '%s'", email.TextContent)
	}

	// Verify HTML content also exists
	if email.HTMLContent == "" {
		t.Error("Expected non-empty HTML content")
	}
}

func TestNotificationWorker_UnknownFanOutType(t *testing.T) {
	ctx := context.Background()
	trackingQueue := newNackTrackingQueue()
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

	worker := NewNotificationWorker(trackingQueue, emailService, templates, userLookup, regionLookup, nil, cfg)

	// Queue a fan-out event with an unknown notification type
	resourceType := "unknown_resource"
	resourceID := "some-id"
	fanOutEvent := &models.EmailNotification{
		ID:               "fanout-unknown-type",
		UserID:           "", // Empty = fan-out event
		NotificationType: models.NotificationType("totally_unknown_type"),
		ResourceType:     &resourceType,
		ResourceID:       &resourceID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}
	_ = trackingQueue.Enqueue(ctx, fanOutEvent)

	// Process the queue
	_ = worker.processQueue(ctx)

	// Verify the event was nacked with "unknown fan-out type"
	if len(trackingQueue.nacks) != 1 {
		t.Fatalf("Expected 1 nack call, got %d", len(trackingQueue.nacks))
	}
	if trackingQueue.nacks[0].id != "fanout-unknown-type" {
		t.Errorf("Expected nack for 'fanout-unknown-type', got '%s'", trackingQueue.nacks[0].id)
	}
	if !strings.Contains(trackingQueue.nacks[0].errorMsg, "unknown fan-out type") {
		t.Errorf("Expected nack error to contain 'unknown fan-out type', got '%s'", trackingQueue.nacks[0].errorMsg)
	}

	// Verify no emails were sent
	if len(emailService.sentEmails) != 0 {
		t.Errorf("Expected 0 emails sent, got %d", len(emailService.sentEmails))
	}
}
