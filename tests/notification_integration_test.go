package tests

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// NotificationTestSuite holds dependencies for notification integration tests
type NotificationTestSuite struct {
	t                   *testing.T
	db                  *database.DB
	queue               *database.DatabaseQueue
	userRepo            *database.UserRepository
	regionRepo          *database.RegionRepository
	notificationService *services.NotificationService
}

// SetupNotificationTest creates a new notification test suite
func SetupNotificationTest(t *testing.T) *NotificationTestSuite {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping notification integration tests")
	}

	port := 3306
	if p := os.Getenv("TEST_DB_PORT"); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &port)
	}

	cfg := &config.DatabaseConfig{
		Host:     host,
		Port:     port,
		User:     getEnvOrDefault("TEST_DB_USER", "test"),
		Password: getEnvOrDefault("TEST_DB_PASSWORD", "testpassword"),
		Name:     getEnvOrDefault("TEST_DB_NAME", "communityrapidresponse_test"),
		Charset:  "utf8mb4",
	}

	db, err := database.New(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Create repositories and queue
	userRepo := database.NewUserRepository(db)
	regionRepo := database.NewRegionRepository(db)
	queue := database.NewDatabaseQueue(db)
	notificationService := services.NewNotificationService(queue)

	suite := &NotificationTestSuite{
		t:                   t,
		db:                  db,
		queue:               queue,
		userRepo:            userRepo,
		regionRepo:          regionRepo,
		notificationService: notificationService,
	}

	t.Cleanup(func() {
		suite.cleanup()
		_ = db.Close()
	})

	return suite
}

// cleanup removes test data
func (s *NotificationTestSuite) cleanup() {
	ctx := context.Background()
	// Clean up notifications
	_, _ = s.db.ExecContext(ctx, "DELETE FROM email_notifications WHERE user_id LIKE 'test-%' OR user_id = ''")
	// Clean up test users
	_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id LIKE 'test-%'")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id LIKE 'test-%'")
	// Clean up test regions
	_, _ = s.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id LIKE 'test-%'")
}

// createTestUser creates a test user in the database
func (s *NotificationTestSuite) createTestUser(id, email, username string, postcardVerified, vouchVerified bool) *models.User {
	ctx := context.Background()
	user := &models.User{
		ID:               id,
		Email:            email,
		Username:         username,
		PasswordHash:     "$2a$12$test", // Dummy hash
		PostcardVerified: postcardVerified,
		VouchVerified:    vouchVerified,
		EmailVerified:    true,
		CreatedAt:        time.Now().UTC(),
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, email, username, password_hash, postcard_verified, vouch_verified, email_verified, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, user.ID, user.Email, user.Username, user.PasswordHash, user.PostcardVerified, user.VouchVerified, user.EmailVerified, user.CreatedAt)

	if err != nil {
		s.t.Fatalf("Failed to create test user: %v", err)
	}

	return user
}

// createTestRegion creates a test region in the database
func (s *NotificationTestSuite) createTestRegion(id, name string) *models.GeographicRegion {
	ctx := context.Background()
	region := &models.GeographicRegion{
		ID:         id,
		Name:       name,
		RegionType: models.RegionTypeNeighborhood,
		CreatedAt:  time.Now().UTC(),
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO geographic_regions (id, name, region_type, created_at)
		VALUES (?, ?, ?, ?)
	`, region.ID, region.Name, region.RegionType, region.CreatedAt)

	if err != nil {
		s.t.Fatalf("Failed to create test region: %v", err)
	}

	return region
}

// addUserToRegion adds a user to a region
func (s *NotificationTestSuite) addUserToRegion(userID, regionID string, isAdmin bool) {
	ctx := context.Background()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_regions (id, user_id, region_id, is_admin, verified_at)
		VALUES (UUID(), ?, ?, ?, NOW())
	`, userID, regionID, isAdmin)

	if err != nil {
		s.t.Fatalf("Failed to add user to region: %v", err)
	}
}

// =============================================================================
// Database Queue Integration Tests
// =============================================================================

func TestIntegration_DatabaseQueue_EnqueueDequeue(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	// Create a test user first (for foreign key constraint)
	user := suite.createTestUser("test-user-1", "test1@example.com", "testuser1", true, false)

	// Create a notification
	resourceType := "region"
	resourceID := "test-region-1"
	notification := &models.EmailNotification{
		ID:               uuid.New().String(),
		UserID:           user.ID,
		NotificationType: models.NotificationTypeVerificationComplete,
		ResourceType:     &resourceType,
		ResourceID:       &resourceID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}

	// Enqueue
	err := suite.queue.Enqueue(ctx, notification)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Dequeue
	notifications, err := suite.queue.Dequeue(ctx, 10)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	// Should find our notification
	found := false
	for _, n := range notifications {
		if n.ID == notification.ID {
			found = true
			if n.UserID != user.ID {
				t.Errorf("Expected UserID %s, got %s", user.ID, n.UserID)
			}
			if n.NotificationType != models.NotificationTypeVerificationComplete {
				t.Errorf("Expected type verification_complete, got %s", n.NotificationType)
			}
			if n.Status != models.NotificationStatusQueued {
				t.Errorf("Expected status queued, got %s", n.Status)
			}
		}
	}

	if !found {
		t.Error("Notification not found in dequeue results")
	}
}

func TestIntegration_DatabaseQueue_AckMarksAsSent(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	user := suite.createTestUser("test-user-2", "test2@example.com", "testuser2", true, false)

	notification := &models.EmailNotification{
		ID:               uuid.New().String(),
		UserID:           user.ID,
		NotificationType: models.NotificationTypeVouchReceived,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}

	err := suite.queue.Enqueue(ctx, notification)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Ack the notification
	contentHash := "test-content-hash-123"
	err = suite.queue.Ack(ctx, notification.ID, contentHash)
	if err != nil {
		t.Fatalf("Ack failed: %v", err)
	}

	// Verify it's now marked as sent
	var status string
	var storedHash *string
	err = suite.db.QueryRowContext(ctx, `
		SELECT status, content_hash FROM email_notifications WHERE id = ?
	`, notification.ID).Scan(&status, &storedHash)
	if err != nil {
		t.Fatalf("Failed to query notification: %v", err)
	}

	if status != string(models.NotificationStatusSent) {
		t.Errorf("Expected status 'sent', got '%s'", status)
	}

	if storedHash == nil || *storedHash != contentHash {
		t.Errorf("Expected content_hash '%s', got '%v'", contentHash, storedHash)
	}

	// Should not appear in dequeue anymore
	notifications, _ := suite.queue.Dequeue(ctx, 100)
	for _, n := range notifications {
		if n.ID == notification.ID {
			t.Error("Acked notification should not appear in dequeue")
		}
	}
}

func TestIntegration_DatabaseQueue_NackMarksFailed(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	user := suite.createTestUser("test-user-3", "test3@example.com", "testuser3", true, false)

	notification := &models.EmailNotification{
		ID:               uuid.New().String(),
		UserID:           user.ID,
		NotificationType: models.NotificationTypeVouchComplete,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}

	err := suite.queue.Enqueue(ctx, notification)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Nack the notification with error
	errorMsg := "SMTP connection failed"
	err = suite.queue.Nack(ctx, notification.ID, errorMsg)
	if err != nil {
		t.Fatalf("Nack failed: %v", err)
	}

	// Verify it's marked as failed
	var status string
	var storedError *string
	err = suite.db.QueryRowContext(ctx, `
		SELECT status, error_message FROM email_notifications WHERE id = ?
	`, notification.ID).Scan(&status, &storedError)
	if err != nil {
		t.Fatalf("Failed to query notification: %v", err)
	}

	if status != string(models.NotificationStatusFailed) {
		t.Errorf("Expected status 'failed', got '%s'", status)
	}

	if storedError == nil || *storedError != errorMsg {
		t.Errorf("Expected error_message '%s', got '%v'", errorMsg, storedError)
	}
}

func TestIntegration_DatabaseQueue_RateLimiting(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	user := suite.createTestUser("test-user-4", "test4@example.com", "testuser4", true, false)
	resourceID := "test-region-rate"

	// Create and immediately send a notification
	notification := &models.EmailNotification{
		ID:               uuid.New().String(),
		UserID:           user.ID,
		NotificationType: models.NotificationTypeInviteLinkUpdated,
		ResourceType:     strPtr("signal_group"),
		ResourceID:       &resourceID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}

	err := suite.queue.Enqueue(ctx, notification)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	err = suite.queue.Ack(ctx, notification.ID, "")
	if err != nil {
		t.Fatalf("Ack failed: %v", err)
	}

	// Check if it was recently sent
	wasSent, err := suite.queue.WasSentRecently(ctx, user.ID, string(models.NotificationTypeInviteLinkUpdated), resourceID, 24*time.Hour)
	if err != nil {
		t.Fatalf("WasSentRecently failed: %v", err)
	}

	if !wasSent {
		t.Error("Expected WasSentRecently to return true for recently sent notification")
	}

	// Check for a different resource - should NOT be rate limited
	wasSent, err = suite.queue.WasSentRecently(ctx, user.ID, string(models.NotificationTypeInviteLinkUpdated), "different-resource", 24*time.Hour)
	if err != nil {
		t.Fatalf("WasSentRecently failed: %v", err)
	}

	if wasSent {
		t.Error("Expected WasSentRecently to return false for different resource")
	}

	// Check for a different user - should NOT be rate limited
	wasSent, err = suite.queue.WasSentRecently(ctx, "different-user", string(models.NotificationTypeInviteLinkUpdated), resourceID, 24*time.Hour)
	if err != nil {
		t.Fatalf("WasSentRecently failed: %v", err)
	}

	if wasSent {
		t.Error("Expected WasSentRecently to return false for different user")
	}
}

func TestIntegration_DatabaseQueue_ContentDeduplication(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	user := suite.createTestUser("test-user-5", "test5@example.com", "testuser5", true, false)
	contentHash := "sha256-content-hash-abc123"

	// Create and send a notification with content hash
	notification := &models.EmailNotification{
		ID:               uuid.New().String(),
		UserID:           user.ID,
		NotificationType: models.NotificationTypeVerificationComplete,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}

	err := suite.queue.Enqueue(ctx, notification)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	err = suite.queue.Ack(ctx, notification.ID, contentHash)
	if err != nil {
		t.Fatalf("Ack failed: %v", err)
	}

	// Check if content was recently sent
	wasSent, err := suite.queue.WasContentSentRecently(ctx, contentHash, 24*time.Hour)
	if err != nil {
		t.Fatalf("WasContentSentRecently failed: %v", err)
	}

	if !wasSent {
		t.Error("Expected WasContentSentRecently to return true for recently sent content")
	}

	// Check for different content hash
	wasSent, err = suite.queue.WasContentSentRecently(ctx, "different-hash", 24*time.Hour)
	if err != nil {
		t.Fatalf("WasContentSentRecently failed: %v", err)
	}

	if wasSent {
		t.Error("Expected WasContentSentRecently to return false for different hash")
	}
}

func TestIntegration_DatabaseQueue_DequeueOrdering(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	user := suite.createTestUser("test-user-6", "test6@example.com", "testuser6", true, false)

	// Create notifications with different timestamps
	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		ids[i] = uuid.New().String()
		notification := &models.EmailNotification{
			ID:               ids[i],
			UserID:           user.ID,
			NotificationType: models.NotificationTypeVouchReceived,
			Status:           models.NotificationStatusQueued,
			QueuedAt:         time.Now().UTC().Add(time.Duration(i) * time.Second),
		}

		err := suite.queue.Enqueue(ctx, notification)
		if err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// Dequeue should return in FIFO order (oldest first)
	notifications, err := suite.queue.Dequeue(ctx, 10)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	// Find our notifications and verify order
	var foundOrder []string
	for _, n := range notifications {
		for _, id := range ids {
			if n.ID == id {
				foundOrder = append(foundOrder, id)
			}
		}
	}

	if len(foundOrder) < 3 {
		t.Fatalf("Expected to find 3 notifications, found %d", len(foundOrder))
	}

	// First notification should be the oldest
	if foundOrder[0] != ids[0] {
		t.Error("Expected oldest notification to be dequeued first")
	}
}

func TestIntegration_DatabaseQueue_BatchLimit(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	user := suite.createTestUser("test-user-7", "test7@example.com", "testuser7", true, false)

	// Create 10 notifications
	for i := 0; i < 10; i++ {
		notification := &models.EmailNotification{
			ID:               uuid.New().String(),
			UserID:           user.ID,
			NotificationType: models.NotificationTypeVouchComplete,
			Status:           models.NotificationStatusQueued,
			QueuedAt:         time.Now().UTC(),
		}

		err := suite.queue.Enqueue(ctx, notification)
		if err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	// Dequeue with limit of 5
	notifications, err := suite.queue.Dequeue(ctx, 5)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	// Should get at most 5
	if len(notifications) > 5 {
		t.Errorf("Expected at most 5 notifications, got %d", len(notifications))
	}
}

// =============================================================================
// Notification Service Integration Tests
// =============================================================================

func TestIntegration_NotificationService_QueueInviteLinkUpdated(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	groupID := "test-group-1"
	regionID := "test-region-1"

	err := suite.notificationService.QueueInviteLinkUpdatedEvent(ctx, groupID, regionID)
	if err != nil {
		t.Fatalf("QueueInviteLinkUpdatedEvent failed: %v", err)
	}

	// Verify notification was queued
	notifications, err := suite.queue.Dequeue(ctx, 10)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	found := false
	for _, n := range notifications {
		if n.NotificationType == models.NotificationTypeInviteLinkUpdated && n.UserID == "" {
			found = true
			if n.ResourceID == nil || *n.ResourceID != regionID {
				t.Errorf("Expected ResourceID %s, got %v", regionID, n.ResourceID)
			}
		}
	}

	if !found {
		t.Error("Fan-out notification not found in queue")
	}
}

func TestIntegration_NotificationService_AllNotificationTypes(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	user := suite.createTestUser("test-user-types", "types@example.com", "testtypes", true, false)
	regionID := "test-region-types"
	voucherID := "test-voucher-1"
	inviterID := "test-inviter-1"

	// Queue all notification types
	err := suite.notificationService.QueueVerificationComplete(ctx, user.ID, regionID)
	if err != nil {
		t.Errorf("QueueVerificationComplete failed: %v", err)
	}

	err = suite.notificationService.QueueVouchReceived(ctx, user.ID, voucherID, regionID)
	if err != nil {
		t.Errorf("QueueVouchReceived failed: %v", err)
	}

	err = suite.notificationService.QueueVouchComplete(ctx, user.ID, regionID)
	if err != nil {
		t.Errorf("QueueVouchComplete failed: %v", err)
	}

	err = suite.notificationService.QueueInviteLinkUpdatedEvent(ctx, "group-1", regionID)
	if err != nil {
		t.Errorf("QueueInviteLinkUpdatedEvent failed: %v", err)
	}

	err = suite.notificationService.QueueSubRegionInvitation(ctx, user.ID, inviterID, regionID)
	if err != nil {
		t.Errorf("QueueSubRegionInvitation failed: %v", err)
	}

	// Verify all were queued
	notifications, err := suite.queue.Dequeue(ctx, 100)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	types := make(map[models.NotificationType]bool)
	for _, n := range notifications {
		types[n.NotificationType] = true
	}

	expectedTypes := []models.NotificationType{
		models.NotificationTypeVerificationComplete,
		models.NotificationTypeVouchReceived,
		models.NotificationTypeVouchComplete,
		models.NotificationTypeInviteLinkUpdated,
		models.NotificationTypeSubRegionInvitation,
	}

	for _, expected := range expectedTypes {
		if !types[expected] {
			t.Errorf("Expected notification type %s not found in queue", expected)
		}
	}
}

func TestIntegration_NotificationService_QueueSubRegionInvitation(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	// Create invitee and inviter
	invitee := suite.createTestUser("test-invitee", "invitee@example.com", "testinvitee", true, false)
	inviter := suite.createTestUser("test-inviter", "inviter@example.com", "testinviter", true, true)
	regionID := "test-region-invite"

	err := suite.notificationService.QueueSubRegionInvitation(ctx, invitee.ID, inviter.ID, regionID)
	if err != nil {
		t.Fatalf("QueueSubRegionInvitation failed: %v", err)
	}

	// Verify notification was queued
	notifications, err := suite.queue.Dequeue(ctx, 10)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	found := false
	for _, n := range notifications {
		if n.NotificationType == models.NotificationTypeSubRegionInvitation && n.UserID == invitee.ID {
			found = true
			if n.ResourceID == nil || *n.ResourceID != inviter.ID {
				t.Errorf("Expected ResourceID %s (inviter ID), got %v", inviter.ID, n.ResourceID)
			}
			if n.ResourceType == nil || *n.ResourceType != "membership_invitation" {
				t.Errorf("Expected ResourceType 'membership_invitation', got %v", n.ResourceType)
			}
		}
	}

	if !found {
		t.Error("Sub-region invitation notification not found in queue")
	}
}

func TestIntegration_NotificationWorker_ProcessesSubRegionInvitation(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	// Create invitee and inviter
	invitee := suite.createTestUser("test-worker-invitee", "workerinvitee@example.com", "workerinvitee", true, false)
	inviter := suite.createTestUser("test-worker-inviter", "workerinviter@example.com", "InviterUsername", true, true)

	// Create mock email service to track sent emails
	emailService := &trackingEmailService{sent: make([]string, 0)}
	templates := services.NewEmailTemplates("Test App", "http://localhost/login")

	cfg := &config.NotificationConfig{
		WorkerInterval:    time.Second,
		BatchSize:         50,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	worker := services.NewNotificationWorker(
		suite.queue,
		emailService,
		templates,
		suite.userRepo,
		suite.regionRepo,
		nil,
		cfg,
	)

	// Queue a sub-region invitation notification
	err := suite.notificationService.QueueSubRegionInvitation(ctx, invitee.ID, inviter.ID, "test-region")
	if err != nil {
		t.Fatalf("QueueSubRegionInvitation failed: %v", err)
	}

	// Start worker in background
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	worker.Start(workerCtx)

	// Wait for processing
	time.Sleep(2 * time.Second)

	// Verify email was sent to the invitee
	if len(emailService.sent) == 0 {
		t.Error("Expected email to be sent for sub-region invitation")
	}

	foundInviteeEmail := false
	for _, to := range emailService.sent {
		if to == invitee.Email {
			foundInviteeEmail = true
		}
	}

	if !foundInviteeEmail {
		t.Errorf("Expected email to be sent to invitee %s, got %v", invitee.Email, emailService.sent)
	}
}

// =============================================================================
// Notification Worker Integration Tests
// =============================================================================

func TestIntegration_NotificationWorker_ProcessesQueue(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	// Create test user and region
	user := suite.createTestUser("test-user-worker", "worker@example.com", "testworker", true, false)
	region := suite.createTestRegion("test-region-worker", "Test Neighborhood")

	// Create mock email service to track sent emails
	emailService := &trackingEmailService{sent: make([]string, 0)}

	// Create email templates
	templates := services.NewEmailTemplates("Test App", "http://localhost/login")

	// Create worker config
	cfg := &config.NotificationConfig{
		WorkerInterval:    time.Second,
		BatchSize:         50,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	// Create worker with test dependencies
	worker := services.NewNotificationWorker(
		suite.queue,
		emailService,
		templates,
		suite.userRepo,
		suite.regionRepo,
		nil,
		cfg,
	)

	// Queue a notification
	resourceType := "region"
	notification := &models.EmailNotification{
		ID:               uuid.New().String(),
		UserID:           user.ID,
		NotificationType: models.NotificationTypeVerificationComplete,
		ResourceType:     &resourceType,
		ResourceID:       &region.ID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}

	err := suite.queue.Enqueue(ctx, notification)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Start worker in background
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	worker.Start(workerCtx)

	// Wait for processing
	time.Sleep(2 * time.Second)

	// Verify email was sent
	if len(emailService.sent) == 0 {
		t.Error("Expected email to be sent")
	}

	// Verify notification is no longer queued
	notifications, _ := suite.queue.Dequeue(ctx, 100)
	for _, n := range notifications {
		if n.ID == notification.ID {
			t.Error("Notification should have been processed and removed from queue")
		}
	}
}

func TestIntegration_NotificationWorker_FanOutExpansion(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	// Clean up any stale notifications from previous test runs
	_, _ = suite.db.ExecContext(ctx, "DELETE FROM email_notifications WHERE status = 'queued' OR status = 'fan_out_pending'")

	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%100000000)

	// Create test region with multiple verified users
	region := suite.createTestRegion(fmt.Sprintf("test-fanout-%s", suffix), "Fanout Test Region")

	users := make([]*models.User, 5)
	for i := 0; i < 5; i++ {
		users[i] = suite.createTestUser(
			fmt.Sprintf("test-fo-%d-%s", i, suffix),
			fmt.Sprintf("fo%d_%s@example.com", i, suffix),
			fmt.Sprintf("fo%d_%s", i, suffix),
			true, false,
		)
		suite.addUserToRegion(users[i].ID, region.ID, false)
	}

	// Create mock email service
	emailService := &trackingEmailService{sent: make([]string, 0)}
	templates := services.NewEmailTemplates("Test App", "http://localhost/login")

	cfg := &config.NotificationConfig{
		WorkerInterval:    500 * time.Millisecond,
		BatchSize:         50,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	worker := services.NewNotificationWorker(
		suite.queue,
		emailService,
		templates,
		suite.userRepo,
		suite.regionRepo,
		nil,
		cfg,
	)

	// Queue a fan-out event
	err := suite.notificationService.QueueInviteLinkUpdatedEvent(ctx, "test-group", region.ID)
	if err != nil {
		t.Fatalf("QueueInviteLinkUpdatedEvent failed: %v", err)
	}

	// Start worker
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	worker.Start(workerCtx)

	// Wait for fan-out and processing
	time.Sleep(3 * time.Second)

	// Should have sent emails to all 5 users
	if len(emailService.sent) != 5 {
		t.Errorf("Expected 5 emails sent, got %d", len(emailService.sent))
	}

	// Verify each user received an email
	for _, user := range users {
		found := false
		for _, to := range emailService.sent {
			if to == user.Email {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("User %s did not receive email", user.Email)
		}
	}
}

func TestIntegration_NotificationWorker_SkipsBlockedUsers(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	// Create blocked user
	user := suite.createTestUser("test-blocked", "blocked@example.com", "blocked", true, false)

	// Block the user
	_, err := suite.db.ExecContext(ctx, `
		UPDATE users SET is_blocked = TRUE WHERE id = ?
	`, user.ID)
	if err != nil {
		t.Fatalf("Failed to block user: %v", err)
	}

	emailService := &trackingEmailService{sent: make([]string, 0)}
	templates := services.NewEmailTemplates("Test App", "http://localhost/login")

	cfg := &config.NotificationConfig{
		WorkerInterval:    500 * time.Millisecond,
		BatchSize:         50,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	worker := services.NewNotificationWorker(
		suite.queue,
		emailService,
		templates,
		suite.userRepo,
		suite.regionRepo,
		nil,
		cfg,
	)

	// Queue a notification for blocked user
	notification := &models.EmailNotification{
		ID:               uuid.New().String(),
		UserID:           user.ID,
		NotificationType: models.NotificationTypeVerificationComplete,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}

	err = suite.queue.Enqueue(ctx, notification)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	worker.Start(workerCtx)
	time.Sleep(2 * time.Second)

	// No email should be sent to blocked user
	if len(emailService.sent) != 0 {
		t.Errorf("Expected 0 emails sent to blocked user, got %d", len(emailService.sent))
	}
}

func TestIntegration_NotificationWorker_RateLimitingPreventsDoubleEmails(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	// Clean up any stale notifications from previous test runs
	_, _ = suite.db.ExecContext(ctx, "DELETE FROM email_notifications WHERE status = 'queued' OR status = 'fan_out_pending'")

	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%100000000)

	user := suite.createTestUser(fmt.Sprintf("test-rl-%s", suffix), fmt.Sprintf("rl_%s@example.com", suffix), fmt.Sprintf("rl_%s", suffix), true, false)
	region := suite.createTestRegion(fmt.Sprintf("test-rl-r-%s", suffix), "Rate Limit Region")

	emailService := &trackingEmailService{sent: make([]string, 0)}
	templates := services.NewEmailTemplates("Test App", "http://localhost/login")

	cfg := &config.NotificationConfig{
		WorkerInterval:    500 * time.Millisecond,
		BatchSize:         50,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	worker := services.NewNotificationWorker(
		suite.queue,
		emailService,
		templates,
		suite.userRepo,
		suite.regionRepo,
		nil,
		cfg,
	)

	// Queue the same notification twice
	resourceType := "region"
	for i := 0; i < 2; i++ {
		notification := &models.EmailNotification{
			ID:               uuid.New().String(),
			UserID:           user.ID,
			NotificationType: models.NotificationTypeVerificationComplete,
			ResourceType:     &resourceType,
			ResourceID:       &region.ID,
			Status:           models.NotificationStatusQueued,
			QueuedAt:         time.Now().UTC(),
		}

		err := suite.queue.Enqueue(ctx, notification)
		if err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	worker.Start(workerCtx)
	time.Sleep(3 * time.Second)

	// Should only send ONE email due to rate limiting
	if len(emailService.sent) != 1 {
		t.Errorf("Expected 1 email sent (rate limited), got %d", len(emailService.sent))
	}
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

func TestIntegration_DatabaseQueue_ConcurrentEnqueue(t *testing.T) {
	suite := SetupNotificationTest(t)
	ctx := context.Background()

	user := suite.createTestUser("test-concurrent", "concurrent@example.com", "concurrent", true, false)

	// Enqueue from multiple goroutines
	var wg sync.WaitGroup
	numWorkers := 10
	notificationsPerWorker := 10

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < notificationsPerWorker; i++ {
				notification := &models.EmailNotification{
					ID:               uuid.New().String(),
					UserID:           user.ID,
					NotificationType: models.NotificationTypeVouchReceived,
					Status:           models.NotificationStatusQueued,
					QueuedAt:         time.Now().UTC(),
				}

				err := suite.queue.Enqueue(ctx, notification)
				if err != nil {
					t.Errorf("Worker %d: Enqueue failed: %v", workerID, err)
				}
			}
		}(w)
	}

	wg.Wait()

	// Verify all notifications were created
	var count int
	err := suite.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM email_notifications WHERE user_id = ?
	`, user.ID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count notifications: %v", err)
	}

	expected := numWorkers * notificationsPerWorker
	if count != expected {
		t.Errorf("Expected %d notifications, got %d", expected, count)
	}
}

// =============================================================================
// Helper Types
// =============================================================================

// trackingEmailService tracks sent emails for testing
type trackingEmailService struct {
	sent []string
	mu   sync.Mutex
}

func (s *trackingEmailService) Send(ctx context.Context, msg *services.EmailMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, msg.To)
	return nil
}

func (s *trackingEmailService) SendVerificationEmail(ctx context.Context, toEmail, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, toEmail)
	return nil
}

func (s *trackingEmailService) IsEnabled() bool {
	return true
}

func (s *trackingEmailService) Backend() string {
	return "tracking"
}

func strPtr(s string) *string {
	return &s
}
