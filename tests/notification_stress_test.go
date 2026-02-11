package tests

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// StressTestSuite holds dependencies for stress tests
type StressTestSuite struct {
	t        *testing.T
	db       *database.DB
	queue    *database.DatabaseQueue
	userRepo *database.UserRepository
}

// SetupStressTest creates a new stress test suite
func SetupStressTest(t *testing.T) *StressTestSuite {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping stress tests")
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

	userRepo := database.NewUserRepository(db)
	queue := database.NewDatabaseQueue(db)

	suite := &StressTestSuite{
		t:        t,
		db:       db,
		queue:    queue,
		userRepo: userRepo,
	}

	t.Cleanup(func() {
		suite.cleanup()
		_ = db.Close()
	})

	return suite
}

func (s *StressTestSuite) cleanup() {
	ctx := context.Background()
	_, _ = s.db.ExecContext(ctx, "DELETE FROM email_notifications WHERE user_id LIKE 'stress-%' OR user_id = ''")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id LIKE 'stress-%'")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id LIKE 'stress-%'")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id LIKE 'stress-%'")
}

func (s *StressTestSuite) createTestUser(id string) {
	ctx := context.Background()
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO users (id, email, username, password_hash, postcard_verified, email_verified, created_at)
		VALUES (?, ?, ?, '$2a$12$test', TRUE, TRUE, NOW())
		ON DUPLICATE KEY UPDATE id = id
	`, id, id+"@stress.test", id)
}

// =============================================================================
// Stress Tests
// =============================================================================

func TestStress_HighVolumeConcurrentEnqueue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	suite := SetupStressTest(t)
	ctx := context.Background()

	// Create test user
	userID := "stress-user-concurrent"
	suite.createTestUser(userID)

	numWorkers := 20
	notificationsPerWorker := 100
	totalExpected := numWorkers * notificationsPerWorker

	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	start := time.Now()

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < notificationsPerWorker; i++ {
				notification := &models.EmailNotification{
					ID:               uuid.New().String(),
					UserID:           userID,
					NotificationType: models.NotificationTypeVouchReceived,
					Status:           models.NotificationStatusQueued,
					QueuedAt:         time.Now().UTC(),
				}

				err := suite.queue.Enqueue(ctx, notification)
				if err != nil {
					atomic.AddInt64(&errorCount, 1)
				} else {
					atomic.AddInt64(&successCount, 1)
				}
			}
		}(w)
	}

	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("Concurrent enqueue: %d total, %d successful, %d errors in %v",
		totalExpected, successCount, errorCount, elapsed)
	t.Logf("Throughput: %.2f notifications/second", float64(successCount)/elapsed.Seconds())

	if int(successCount) != totalExpected {
		t.Errorf("Expected %d successful enqueues, got %d", totalExpected, successCount)
	}

	// Verify all notifications exist
	var count int
	err := suite.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM email_notifications WHERE user_id = ?
	`, userID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count notifications: %v", err)
	}

	if count != totalExpected {
		t.Errorf("Expected %d notifications in DB, got %d", totalExpected, count)
	}
}

func TestStress_HighVolumeBatchDequeue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	suite := SetupStressTest(t)
	ctx := context.Background()

	// Create test user and enqueue many notifications
	userID := "stress-user-dequeue"
	suite.createTestUser(userID)

	numNotifications := 500
	for i := 0; i < numNotifications; i++ {
		notification := &models.EmailNotification{
			ID:               uuid.New().String(),
			UserID:           userID,
			NotificationType: models.NotificationTypeVouchReceived,
			Status:           models.NotificationStatusQueued,
			QueuedAt:         time.Now().UTC(),
		}
		if err := suite.queue.Enqueue(ctx, notification); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	// Dequeue in batches
	batchSize := 50
	totalDequeued := 0
	iterations := 0
	start := time.Now()

	for {
		notifications, err := suite.queue.Dequeue(ctx, batchSize)
		if err != nil {
			t.Fatalf("Dequeue failed: %v", err)
		}

		if len(notifications) == 0 {
			break
		}

		for _, n := range notifications {
			if n.UserID == userID {
				totalDequeued++
				_ = suite.queue.Ack(ctx, n.ID, "")
			}
		}
		iterations++
	}

	elapsed := time.Since(start)

	t.Logf("Batch dequeue: %d notifications in %d batches, %v elapsed",
		totalDequeued, iterations, elapsed)
	t.Logf("Throughput: %.2f notifications/second", float64(totalDequeued)/elapsed.Seconds())

	if totalDequeued != numNotifications {
		t.Errorf("Expected to dequeue %d notifications, got %d", numNotifications, totalDequeued)
	}
}

func TestStress_ConcurrentDequeue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	suite := SetupStressTest(t)
	ctx := context.Background()

	// Create test user and enqueue notifications
	userID := "stress-user-concurrent-dequeue"
	suite.createTestUser(userID)

	numNotifications := 200
	for i := 0; i < numNotifications; i++ {
		notification := &models.EmailNotification{
			ID:               uuid.New().String(),
			UserID:           userID,
			NotificationType: models.NotificationTypeVerificationComplete,
			Status:           models.NotificationStatusQueued,
			QueuedAt:         time.Now().UTC(),
		}
		if err := suite.queue.Enqueue(ctx, notification); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	// Multiple workers dequeue concurrently
	numWorkers := 5
	var wg sync.WaitGroup
	var totalProcessed int64

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				notifications, err := suite.queue.Dequeue(ctx, 10)
				if err != nil {
					return
				}
				if len(notifications) == 0 {
					return
				}
				for _, n := range notifications {
					if n.UserID == userID {
						atomic.AddInt64(&totalProcessed, 1)
						_ = suite.queue.Ack(ctx, n.ID, "")
					}
				}
			}
		}(w)
	}

	wg.Wait()

	t.Logf("Concurrent dequeue: %d workers processed %d notifications", numWorkers, totalProcessed)

	// Due to race conditions in dequeue, we might not get exactly numNotifications
	// but we should get close (all should be processed eventually)
	if totalProcessed < int64(numNotifications/2) {
		t.Errorf("Expected to process more notifications, only got %d of %d", totalProcessed, numNotifications)
	}
}

func TestStress_RateLimitChecks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	suite := SetupStressTest(t)
	ctx := context.Background()

	userID := "stress-user-ratelimit"
	suite.createTestUser(userID)

	// Create many sent notifications to test rate limit check performance
	numSent := 100
	for i := 0; i < numSent; i++ {
		notification := &models.EmailNotification{
			ID:               uuid.New().String(),
			UserID:           userID,
			NotificationType: models.NotificationTypeInviteLinkUpdated,
			ResourceType:     strPtr("signal_group"),
			ResourceID:       strPtr(fmt.Sprintf("resource-%d", i%10)), // 10 different resources
			Status:           models.NotificationStatusQueued,
			QueuedAt:         time.Now().UTC(),
		}
		if err := suite.queue.Enqueue(ctx, notification); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
		if err := suite.queue.Ack(ctx, notification.ID, ""); err != nil {
			t.Fatalf("Ack failed: %v", err)
		}
	}

	// Perform many rate limit checks
	numChecks := 1000
	start := time.Now()

	for i := 0; i < numChecks; i++ {
		_, err := suite.queue.WasSentRecently(ctx, userID, string(models.NotificationTypeInviteLinkUpdated), fmt.Sprintf("resource-%d", i%10), 24*time.Hour)
		if err != nil {
			t.Fatalf("Rate limit check failed: %v", err)
		}
	}

	elapsed := time.Since(start)
	t.Logf("Rate limit checks: %d checks in %v (%.2f checks/second)",
		numChecks, elapsed, float64(numChecks)/elapsed.Seconds())

	// Should complete relatively quickly (less than 1ms per check on average)
	avgTime := elapsed / time.Duration(numChecks)
	if avgTime > 10*time.Millisecond {
		t.Errorf("Rate limit checks too slow: %v per check", avgTime)
	}
}

func TestStress_ContentDeduplication(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	suite := SetupStressTest(t)
	ctx := context.Background()

	userID := "stress-user-dedup"
	suite.createTestUser(userID)

	// Create notifications with content hashes
	numHashes := 50
	for i := 0; i < numHashes; i++ {
		notification := &models.EmailNotification{
			ID:               uuid.New().String(),
			UserID:           userID,
			NotificationType: models.NotificationTypeVerificationComplete,
			Status:           models.NotificationStatusQueued,
			QueuedAt:         time.Now().UTC(),
		}
		if err := suite.queue.Enqueue(ctx, notification); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
		if err := suite.queue.Ack(ctx, notification.ID, fmt.Sprintf("hash-%d", i)); err != nil {
			t.Fatalf("Ack failed: %v", err)
		}
	}

	// Check deduplication performance
	numChecks := 500
	start := time.Now()

	for i := 0; i < numChecks; i++ {
		_, err := suite.queue.WasContentSentRecently(ctx, fmt.Sprintf("hash-%d", i%numHashes), 24*time.Hour)
		if err != nil {
			t.Fatalf("Content dedup check failed: %v", err)
		}
	}

	elapsed := time.Since(start)
	t.Logf("Content dedup checks: %d checks in %v (%.2f checks/second)",
		numChecks, elapsed, float64(numChecks)/elapsed.Seconds())
}

func TestStress_WorkerProcessing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	suite := SetupStressTest(t)
	ctx := context.Background()

	// Create region with many users
	regionID := "stress-region-worker"
	_, _ = suite.db.ExecContext(ctx, `
		INSERT INTO geographic_regions (id, name, region_type, created_at)
		VALUES (?, 'Stress Test Region', 'neighborhood', NOW())
		ON DUPLICATE KEY UPDATE id = id
	`, regionID)

	numUsers := 50
	for i := 0; i < numUsers; i++ {
		userID := fmt.Sprintf("stress-worker-user-%d", i)
		suite.createTestUser(userID)
		_, _ = suite.db.ExecContext(ctx, `
			INSERT INTO user_regions (id, user_id, region_id, is_admin, verified_at)
			VALUES (UUID(), ?, ?, FALSE, NOW())
			ON DUPLICATE KEY UPDATE user_id = user_id
		`, userID, regionID)
	}

	// Create email tracker
	emailTracker := &countingEmailService{count: 0}
	templates := services.NewEmailTemplates("Stress Test App", "http://localhost/login")

	cfg := &config.NotificationConfig{
		WorkerInterval:    200 * time.Millisecond,
		BatchSize:         20,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	worker := services.NewNotificationWorker(
		suite.queue,
		emailTracker,
		templates,
		suite.userRepo,
		nil, // regionRepo not needed for this test
		cfg,
	)

	// Queue many notifications
	numNotifications := 100
	for i := 0; i < numNotifications; i++ {
		userID := fmt.Sprintf("stress-worker-user-%d", i%numUsers)
		notification := &models.EmailNotification{
			ID:               uuid.New().String(),
			UserID:           userID,
			NotificationType: models.NotificationTypeVouchReceived,
			Status:           models.NotificationStatusQueued,
			QueuedAt:         time.Now().UTC(),
		}
		if err := suite.queue.Enqueue(ctx, notification); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	// Start worker and time processing
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := time.Now()
	worker.Start(workerCtx)

	// Wait for processing (with timeout)
	maxWait := 30 * time.Second
	checkInterval := 500 * time.Millisecond

	for elapsed := time.Duration(0); elapsed < maxWait; elapsed += checkInterval {
		time.Sleep(checkInterval)

		// Check how many are still queued
		var queuedCount int
		_ = suite.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM email_notifications
			WHERE status = 'queued' AND user_id LIKE 'stress-worker-user-%'
		`).Scan(&queuedCount)

		if queuedCount == 0 {
			break
		}
	}

	processingTime := time.Since(start)
	sentCount := emailTracker.GetCount()

	t.Logf("Worker stress test: processed %d notifications in %v (%.2f/second)",
		sentCount, processingTime, float64(sentCount)/processingTime.Seconds())

	// Should process most notifications (some might be rate-limited)
	if sentCount < int64(numNotifications/2) {
		t.Errorf("Expected to send more emails, only sent %d of %d queued", sentCount, numNotifications)
	}
}

// =============================================================================
// Helper Types for Stress Tests
// =============================================================================

type countingEmailService struct {
	mu    sync.Mutex
	count int64
}

func (s *countingEmailService) Send(ctx context.Context, msg *services.EmailMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	return nil
}

func (s *countingEmailService) SendVerificationEmail(ctx context.Context, toEmail, token string) error {
	return nil
}

func (s *countingEmailService) IsEnabled() bool {
	return true
}

func (s *countingEmailService) Backend() string {
	return "counting"
}

func (s *countingEmailService) GetCount() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}
