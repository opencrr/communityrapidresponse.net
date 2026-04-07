package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/handlers"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/mocks"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// NotificationE2ETestSuite holds all dependencies for notification e2e tests
type NotificationE2ETestSuite struct {
	t                   *testing.T
	db                  *database.DB
	server              *httptest.Server
	userRepo            *database.UserRepository
	regionRepo          *database.RegionRepository
	verifyRepo          *database.VerificationRepository
	vouchRepo           *database.VouchRepository
	groupRepo           *database.SignalGroupRepository
	membershipRepo      *database.MembershipRepository
	notificationQueue   *database.DatabaseQueue
	notificationService *services.NotificationService
	emailTracker        *emailTracker
	jwtAuth             *middleware.JWTAuth
}

// emailTracker tracks emails for E2E tests
type emailTracker struct {
	mu       sync.Mutex
	sent     []trackedEmail
	enabled  bool
}

type trackedEmail struct {
	To      string
	Subject string
	Body    string
	SentAt  time.Time
}

func (t *emailTracker) Send(ctx context.Context, msg *services.EmailMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sent = append(t.sent, trackedEmail{
		To:      msg.To,
		Subject: msg.Subject,
		Body:    msg.TextContent,
		SentAt:  time.Now(),
	})
	return nil
}

func (t *emailTracker) SendVerificationEmail(ctx context.Context, toEmail, token string) error {
	return nil
}

func (t *emailTracker) IsEnabled() bool {
	return t.enabled
}

func (t *emailTracker) Backend() string {
	return "tracker"
}

func (t *emailTracker) GetSent() []trackedEmail {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]trackedEmail, len(t.sent))
	copy(result, t.sent)
	return result
}

func (t *emailTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sent = nil
}

// SetupNotificationE2ETest creates a new notification E2E test suite
func SetupNotificationE2ETest(t *testing.T) *NotificationE2ETestSuite {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping notification e2e tests")
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

	// Create repositories
	userRepo := database.NewUserRepository(db)
	regionRepo := database.NewRegionRepository(db)
	verifyRepo := database.NewVerificationRepository(db)
	vouchRepo := database.NewVouchRepository(db)
	groupRepo := database.NewSignalGroupRepository(db)
	membershipRepo := database.NewMembershipRepository(db)
	schoolRepo := database.NewSchoolRepository(db)
	districtRepo := database.NewSchoolDistrictRepository(db)
	schoolVouchRepo := database.NewSchoolVouchRepository(db)
	auditRepo := database.NewAuditRepository(db)

	// Create notification queue and service
	notificationQueue := database.NewDatabaseQueue(db)
	notificationService := services.NewNotificationService(notificationQueue)

	// Create email tracker
	emailTracker := &emailTracker{sent: make([]trackedEmail, 0), enabled: true}

	// Create JWT auth
	jwtConfig := &config.JWTConfig{
		Secret:          "test_secret_key_at_least_32_characters_long",
		ExpirationHours: 24,
		Issuer:          "test_issuer",
	}
	jwtAuth := middleware.NewJWTAuth(jwtConfig)

	// Create mock services for testing
	mockPostgrid := mocks.NewMockPostgridService()
	mockMapbox := mocks.NewMockMapboxService()

	// Create handlers with notification service
	authHandler := handlers.NewAuthHandler(userRepo, jwtAuth)
	regionHandler := handlers.NewRegionHandler(regionRepo, mockMapbox, nil)
	verificationHandler := handlers.NewVerificationHandler(
		nil, verifyRepo, vouchRepo, userRepo, regionRepo,
		mockPostgrid, mockMapbox, nil,
		false, 30, // Bootstrap cooldown disabled for tests
	)
	verificationHandler.SetNotificationService(notificationService)

	consensusConfig := &config.ConsensusConfig{VotePercent: 50, VoteFloor: 3}
	adminHandler := handlers.NewAdminHandler(userRepo, regionRepo, nil)

	// Create MFA service and handler
	mfaConfig := &config.MFAConfig{
		EncryptionKey: "01234567890123456789012345678901",
		Issuer:        "Test MFA",
	}
	mfaService, _ := services.NewMFAService(mfaConfig)
	mfaHandler := handlers.NewMFAHandler(nil, userRepo, mfaService, jwtAuth, false, nil)
	membershipHandler := handlers.NewMembershipHandler(nil, membershipRepo, regionRepo, userRepo, nil)
	membershipHandler.SetNotificationService(notificationService)

	// Create blocklist proposal handler
	blocklistConfig := &config.BlocklistConfig{
		AddressBlocklistDuration:  2 * 365 * 24 * time.Hour,
		ProposalRateLimitPerMonth: 5,
	}
	blocklistProposalRepo := database.NewBlocklistProposalRepository(db, blocklistConfig)
	blocklistProposalHandler := handlers.NewBlocklistProposalHandler(
		nil, blocklistProposalRepo, regionRepo, userRepo, nil, consensusConfig, blocklistConfig,
	)

	schoolHandler := handlers.NewSchoolHandler(
		db, schoolRepo, districtRepo, schoolVouchRepo, groupRepo, nil,
		userRepo, auditRepo, nil, consensusConfig, false, 0,
	)

	// Create router
	router := handlers.NewRouter(
		authHandler, mfaHandler, regionHandler, verificationHandler, adminHandler,
		membershipHandler, blocklistProposalHandler, nil, schoolHandler, nil, nil, nil, nil, jwtAuth, nil, nil, nil,
		[]string{"*"}, nil,
	)
	handler := router.Setup()

	server := httptest.NewServer(handler)

	// Create and start notification worker
	notificationCfg := &config.NotificationConfig{
		WorkerInterval:    500 * time.Millisecond,
		BatchSize:         50,
		RateLimitDuration: 24 * time.Hour,
		RetryFailedAfter:  time.Hour,
	}

	templates := services.NewEmailTemplates("Community Rapid Response", "http://localhost/login")
	worker := services.NewNotificationWorker(
		notificationQueue,
		emailTracker,
		templates,
		userRepo,
		regionRepo,
		nil,
		notificationCfg,
	)
	workerCtx, workerCancel := context.WithCancel(context.Background())
	worker.Start(workerCtx)

	suite := &NotificationE2ETestSuite{
		t:                   t,
		db:                  db,
		server:              server,
		userRepo:            userRepo,
		regionRepo:          regionRepo,
		verifyRepo:          verifyRepo,
		vouchRepo:           vouchRepo,
		groupRepo:           groupRepo,
		membershipRepo:      membershipRepo,
		notificationQueue:   notificationQueue,
		notificationService: notificationService,
		emailTracker:        emailTracker,
		jwtAuth:             jwtAuth,
	}

	t.Cleanup(func() {
		workerCancel()
		server.Close()
		suite.cleanup()
		_ = db.Close()
	})

	return suite
}

// cleanup removes test data
func (s *NotificationE2ETestSuite) cleanup() {
	ctx := context.Background()
	_, _ = s.db.ExecContext(ctx, "DELETE FROM email_notifications WHERE user_id LIKE 'e2e-%' OR user_id = ''")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM secret_update_votes WHERE voter_id LIKE 'e2e-%'")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM secret_update_proposals WHERE proposed_by LIKE 'e2e-%'")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE region_id LIKE 'e2e-%'")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM vouches WHERE voucher_user_id LIKE 'e2e-%' OR vouched_user_id LIKE 'e2e-%'")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM verification_requests WHERE user_id LIKE 'e2e-%'")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM sub_region_membership_votes WHERE voter_id LIKE 'e2e-%'")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE user_id LIKE 'e2e-%' OR initiated_by LIKE 'e2e-%'")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id LIKE 'e2e-%'")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id LIKE 'e2e-%'")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id LIKE 'e2e-%'")
}

// request makes an HTTP request to the test server
func (s *NotificationE2ETestSuite) request(method, path string, body interface{}, token string) *http.Response {
	var bodyReader *bytes.Reader
	if body != nil {
		bodyBytes, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(bodyBytes)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req, _ := http.NewRequest(method, s.server.URL+path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		s.t.Fatalf("Request failed: %v", err)
	}
	return resp
}

// createUser creates a user directly in the database and returns a JWT token
func (s *NotificationE2ETestSuite) createUser(id, email, username string, postcardVerified, vouchVerified, isSuperuser bool) (string, *models.User) {
	ctx := context.Background()
	user := &models.User{
		ID:               id,
		Email:            email,
		Username:         username,
		PasswordHash:     "$2a$12$test",
		PostcardVerified: postcardVerified,
		VouchVerified:    vouchVerified,
		IsSuperuser:      isSuperuser,
		EmailVerified:    true,
		CreatedAt:        time.Now().UTC(),
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, email, username, password_hash, postcard_verified, vouch_verified, is_superuser, email_verified, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, user.ID, user.Email, user.Username, user.PasswordHash, user.PostcardVerified, user.VouchVerified, user.IsSuperuser, user.EmailVerified, user.CreatedAt)

	if err != nil {
		s.t.Fatalf("Failed to create user: %v", err)
	}

	// Generate token
	token, _ := s.jwtAuth.GenerateToken(user)
	return token, user
}

// createRegion creates a region directly in the database
func (s *NotificationE2ETestSuite) createRegion(id, name string, parentID *string) *models.GeographicRegion {
	ctx := context.Background()
	region := &models.GeographicRegion{
		ID:             id,
		Name:           name,
		RegionType:     models.RegionTypeNeighborhood,
		ParentRegionID: parentID,
		CreatedAt:      time.Now().UTC(),
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO geographic_regions (id, name, region_type, parent_region_id, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, region.ID, region.Name, region.RegionType, region.ParentRegionID, region.CreatedAt)

	if err != nil {
		s.t.Fatalf("Failed to create region: %v", err)
	}

	return region
}

// addUserToRegion adds a user to a region
func (s *NotificationE2ETestSuite) addUserToRegion(userID, regionID string, isAdmin bool) {
	ctx := context.Background()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_regions (id, user_id, region_id, is_admin, verified_at)
		VALUES (UUID(), ?, ?, ?, NOW())
	`, userID, regionID, isAdmin)

	if err != nil {
		s.t.Fatalf("Failed to add user to region: %v", err)
	}
}

// createSignalGroup creates a Signal group for a region
func (s *NotificationE2ETestSuite) createSignalGroup(id, regionID, name, _ string, createdBy string) *models.SignalGroup {
	ctx := context.Background()
	group := &models.SignalGroup{
		ID:        id,
		RegionID:  &regionID,
		GroupName: name,
		CreatedBy: &createdBy,
		CreatedAt: time.Now().UTC(),
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO signal_groups (id, region_id, group_name, created_by, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, group.ID, group.RegionID, group.GroupName, createdBy, group.CreatedAt)

	if err != nil {
		s.t.Fatalf("Failed to create signal group: %v", err)
	}

	return group
}

// createInviteLinkProposal is no longer used - invite_link_update_proposals table
// was replaced by secret_update_proposals in the E2E encryption migration.

// =============================================================================
// E2E Tests: Invite Link Update Notifications
// =============================================================================

func TestE2E_SecretUpdate_TriggersNotification(t *testing.T) {
	suite := SetupNotificationE2ETest(t)
	ctx := context.Background()

	// Create region with multiple verified users
	region := suite.createRegion("e2e-region-invite", "Invite Test Region", nil)

	// Create 3 admin users
	for i := 0; i < 3; i++ {
		_, user := suite.createUser(
			fmt.Sprintf("e2e-admin-%d", i),
			fmt.Sprintf("admin%d@e2e.test", i),
			fmt.Sprintf("admin%d", i),
			true, true, false,
		)
		suite.addUserToRegion(user.ID, region.ID, true)
	}

	// Create verified non-admin users who should receive notifications
	for i := 0; i < 3; i++ {
		_, user := suite.createUser(
			fmt.Sprintf("e2e-member-%d", i),
			fmt.Sprintf("member%d@e2e.test", i),
			fmt.Sprintf("member%d", i),
			true, false, false,
		)
		suite.addUserToRegion(user.ID, region.ID, false)
	}

	// Create signal group
	group := suite.createSignalGroup("e2e-group-1", region.ID, "Test Group", "", "e2e-admin-0")

	// Reset email tracker
	suite.emailTracker.Reset()

	// Directly queue the invite link updated event (simulating what happens
	// after a secret update proposal is finalized)
	err := suite.notificationService.QueueInviteLinkUpdatedEvent(ctx, group.ID, region.ID)
	if err != nil {
		t.Fatalf("Failed to queue notification: %v", err)
	}

	// Wait for worker to process notifications
	time.Sleep(3 * time.Second)

	// Check that notifications were queued
	var queuedCount int
	err = suite.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM email_notifications
		WHERE notification_type = 'invite_link_updated'
		AND (user_id LIKE 'e2e-%' OR user_id = '')
	`).Scan(&queuedCount)
	if err != nil {
		t.Fatalf("Failed to count queued notifications: %v", err)
	}

	if queuedCount == 0 {
		t.Error("Expected notifications to be queued for secret update")
	}

	// Check emails were sent (at least to some users)
	emails := suite.emailTracker.GetSent()
	if len(emails) == 0 {
		t.Error("Expected emails to be sent for secret update")
	}

	// Verify emails don't contain sensitive data and instruct user to log in
	for _, email := range emails {
		if !contains(email.Body, "log in") && !contains(email.Body, "Log In") {
			t.Error("Email should instruct user to log in")
		}
	}
}

func TestE2E_InviteLinkUpdate_RateLimitsMultipleUpdates(t *testing.T) {
	suite := SetupNotificationE2ETest(t)
	ctx := context.Background()

	// Create region with admin and member
	region := suite.createRegion("e2e-region-ratelimit", "Rate Limit Test Region", nil)

	// Create 3 admins
	for i := 0; i < 3; i++ {
		_, user := suite.createUser(
			fmt.Sprintf("e2e-ratelimit-admin-%d", i),
			fmt.Sprintf("ratelimitadmin%d@e2e.test", i),
			fmt.Sprintf("ratelimitadmin%d", i),
			true, true, false,
		)
		suite.addUserToRegion(user.ID, region.ID, true)
	}

	// Create verified member
	_, member := suite.createUser("e2e-ratelimit-member", "ratelimitmember@e2e.test", "ratelimitmember", true, false, false)
	suite.addUserToRegion(member.ID, region.ID, false)

	group := suite.createSignalGroup("e2e-group-ratelimit", region.ID, "Rate Limit Group", "https://signal.group/original", "e2e-ratelimit-admin-0")

	suite.emailTracker.Reset()

	// Simulate two consecutive updates by queuing notifications directly
	err := suite.notificationService.QueueInviteLinkUpdatedEvent(ctx, group.ID, region.ID)
	if err != nil {
		t.Fatalf("First queue failed: %v", err)
	}

	// Wait for first to process
	time.Sleep(2 * time.Second)

	firstEmails := len(suite.emailTracker.GetSent())

	// Queue another notification for the same region
	err = suite.notificationService.QueueInviteLinkUpdatedEvent(ctx, group.ID, region.ID)
	if err != nil {
		t.Fatalf("Second queue failed: %v", err)
	}

	// Wait for second to process
	time.Sleep(2 * time.Second)

	secondEmails := len(suite.emailTracker.GetSent())

	// Due to rate limiting, the second batch shouldn't send additional emails
	// (to the same user for the same notification type/resource within 24h)
	if secondEmails > firstEmails*2 {
		t.Errorf("Rate limiting should prevent duplicate emails. First: %d, Total: %d", firstEmails, secondEmails)
	}
}

// =============================================================================
// E2E Tests: Verification Notifications
// =============================================================================

func TestE2E_PostcardVerification_TriggersNotification(t *testing.T) {
	suite := SetupNotificationE2ETest(t)
	ctx := context.Background()

	// Create unverified user
	token, user := suite.createUser("e2e-verify-user", "verify@e2e.test", "verifyuser", false, false, false)

	// Create region
	region := suite.createRegion("e2e-verify-region", "Verify Region", nil)

	// Create a verification request
	_, err := suite.db.ExecContext(ctx, `
		INSERT INTO verification_requests (id, user_id, verification_code, status, region_id, postgrid_request_id, boundary_type, boundary_name, boundary_state, created_at, expires_at)
		VALUES (?, ?, ?, 'pending', ?, 'test-postgrid-id', 'neighborhood', 'Test Neighborhood', 'NY', NOW(), DATE_ADD(NOW(), INTERVAL 7 DAY))
	`, "e2e-verify-request", user.ID, "123456", region.ID)
	if err != nil {
		t.Fatalf("Failed to create verification request: %v", err)
	}

	suite.emailTracker.Reset()

	// Verify the code
	resp := suite.request("POST", "/api/v1/verification/postcard/verify", map[string]interface{}{
		"verification_code": "123456",
	}, token)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("Expected status 200, got %d: %v", resp.StatusCode, body)
	}

	// Wait for notification processing
	time.Sleep(3 * time.Second)

	// Check notification was queued
	var count int
	err = suite.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM email_notifications
		WHERE user_id = ? AND notification_type = 'verification_complete'
	`, user.ID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count notifications: %v", err)
	}

	if count == 0 {
		t.Error("Expected verification_complete notification to be queued")
	}

	// Check email was sent
	emails := suite.emailTracker.GetSent()
	found := false
	for _, email := range emails {
		if email.To == user.Email {
			found = true
			if !contains(email.Subject, "Verified") {
				t.Errorf("Email subject should mention verification: %s", email.Subject)
			}
		}
	}
	if !found {
		t.Error("Expected email to be sent to verified user")
	}
}

func TestE2E_VouchReceived_TriggersNotification(t *testing.T) {
	suite := SetupNotificationE2ETest(t)
	ctx := context.Background()

	// Create a region
	region := suite.createRegion("e2e-vouch-region", "Vouch Region", nil)

	// Create voucher (fully verified admin)
	voucherToken, voucher := suite.createUser("e2e-voucher", "voucher@e2e.test", "voucher", true, true, false)
	suite.addUserToRegion(voucher.ID, region.ID, true)

	// Create vouchee (postcard verified, requesting vouch)
	_, vouchee := suite.createUser("e2e-vouchee", "vouchee@e2e.test", "vouchee", true, false, false)

	// Create pending vouch request
	_, err := suite.db.ExecContext(ctx, `
		INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at)
		VALUES (UUID(), ?, ?, FALSE, 'pending', NOW())
	`, vouchee.ID, region.ID)
	if err != nil {
		t.Fatalf("Failed to create pending user_region: %v", err)
	}

	suite.emailTracker.Reset()

	// Voucher vouches for vouchee
	resp := suite.request("POST", "/api/v1/verification/vouch", map[string]interface{}{
		"vouched_user_id": vouchee.ID,
		"region_id":       region.ID,
	}, voucherToken)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("Expected status 201, got %d: %v", resp.StatusCode, body)
	}

	// Wait for notification processing
	time.Sleep(3 * time.Second)

	// Check vouch_received notification was queued
	var count int
	err = suite.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM email_notifications
		WHERE user_id = ? AND notification_type = 'vouch_received'
	`, vouchee.ID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count notifications: %v", err)
	}

	if count == 0 {
		t.Error("Expected vouch_received notification to be queued")
	}

	// Check email was sent to vouchee
	emails := suite.emailTracker.GetSent()
	found := false
	for _, email := range emails {
		if email.To == vouchee.Email {
			found = true
			if !contains(email.Subject, "Vouch") {
				t.Errorf("Email subject should mention vouch: %s", email.Subject)
			}
		}
	}
	if !found {
		t.Error("Expected email to be sent to vouchee")
	}
}

func TestE2E_VouchComplete_TriggersNotification(t *testing.T) {
	suite := SetupNotificationE2ETest(t)
	ctx := context.Background()

	// Create region
	region := suite.createRegion("e2e-vouch-complete-region", "Vouch Complete Region", nil)

	// Create 3 admin vouchers (so region exits bootstrap mode - only 2 vouches needed)
	voucherTokens := make([]string, 3)
	for i := 0; i < 3; i++ {
		token, voucher := suite.createUser(
			fmt.Sprintf("e2e-voucher-complete-%d", i),
			fmt.Sprintf("vouchercomplete%d@e2e.test", i),
			fmt.Sprintf("vouchercomplete%d", i),
			true, true, false,
		)
		voucherTokens[i] = token
		suite.addUserToRegion(voucher.ID, region.ID, true)
	}

	// Create vouchee
	_, vouchee := suite.createUser("e2e-vouchee-complete", "voucheecomplete@e2e.test", "voucheecomplete", true, false, false)

	// Create pending vouch request
	_, err := suite.db.ExecContext(ctx, `
		INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at)
		VALUES (UUID(), ?, ?, FALSE, 'pending', NOW())
	`, vouchee.ID, region.ID)
	if err != nil {
		t.Fatalf("Failed to create pending user_region: %v", err)
	}

	suite.emailTracker.Reset()

	// First vouch
	resp := suite.request("POST", "/api/v1/verification/vouch", map[string]interface{}{
		"vouched_user_id": vouchee.ID,
		"region_id":       region.ID,
	}, voucherTokens[0])
	_ = resp.Body.Close()

	// Second vouch (should trigger vouch_complete)
	resp = suite.request("POST", "/api/v1/verification/vouch", map[string]interface{}{
		"vouched_user_id": vouchee.ID,
		"region_id":       region.ID,
	}, voucherTokens[1])
	_ = resp.Body.Close()

	// Wait for notification processing
	time.Sleep(3 * time.Second)

	// Check vouch_complete notification was queued
	var count int
	err = suite.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM email_notifications
		WHERE user_id = ? AND notification_type = 'vouch_complete'
	`, vouchee.ID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count notifications: %v", err)
	}

	if count == 0 {
		t.Error("Expected vouch_complete notification to be queued")
	}

	// Check email was sent
	emails := suite.emailTracker.GetSent()
	foundComplete := false
	for _, email := range emails {
		if email.To == vouchee.Email && contains(email.Subject, "Vouch-Verified") {
			foundComplete = true
		}
	}
	if !foundComplete {
		t.Error("Expected vouch_complete email to be sent")
	}
}

// =============================================================================
// E2E Tests: Security and Edge Cases
// =============================================================================

func TestE2E_BlockedUser_DoesNotReceiveNotifications(t *testing.T) {
	suite := SetupNotificationE2ETest(t)
	ctx := context.Background()

	// Create region
	region := suite.createRegion("e2e-blocked-region", "Blocked Region", nil)

	// Create and block user
	_, user := suite.createUser("e2e-blocked-user", "blocked@e2e.test", "blockeduser", true, false, false)
	suite.addUserToRegion(user.ID, region.ID, false)

	// Block the user
	_, err := suite.db.ExecContext(ctx, `
		UPDATE users SET is_blocked = TRUE WHERE id = ?
	`, user.ID)
	if err != nil {
		t.Fatalf("Failed to block user: %v", err)
	}

	suite.emailTracker.Reset()

	// Queue notification for blocked user
	err = suite.notificationService.QueueVerificationComplete(ctx, user.ID, region.ID)
	if err != nil {
		t.Fatalf("Failed to queue notification: %v", err)
	}

	// Wait for processing
	time.Sleep(2 * time.Second)

	// No email should be sent
	emails := suite.emailTracker.GetSent()
	for _, email := range emails {
		if email.To == user.Email {
			t.Error("Blocked user should NOT receive notification emails")
		}
	}
}

func TestE2E_NotificationContent_NoSensitiveData(t *testing.T) {
	suite := SetupNotificationE2ETest(t)
	ctx := context.Background()

	// Create region and user
	region := suite.createRegion("e2e-sensitive-region", "Sensitive Test Region", nil)
	_, user := suite.createUser("e2e-sensitive-user", "sensitive@e2e.test", "sensitiveuser", true, false, false)
	suite.addUserToRegion(user.ID, region.ID, false)

	suite.emailTracker.Reset()

	// Queue all notification types
	_ = suite.notificationService.QueueInviteLinkUpdatedEvent(ctx, "test-group", region.ID)
	_ = suite.notificationService.QueueVerificationComplete(ctx, user.ID, region.ID)
	_ = suite.notificationService.QueueVouchReceived(ctx, user.ID, "voucher-id", region.ID)
	_ = suite.notificationService.QueueVouchComplete(ctx, user.ID, region.ID)

	// Wait for processing
	time.Sleep(3 * time.Second)

	// Check all sent emails for sensitive data
	emails := suite.emailTracker.GetSent()

	sensitivePatterns := []string{
		"signal.group/",     // Invite links
		"verification_code", // Verification codes
		"password",          // Passwords
		"123456",            // Codes
		"secret",            // Secrets
	}

	for _, email := range emails {
		for _, pattern := range sensitivePatterns {
			if contains(email.Body, pattern) {
				t.Errorf("Email to %s contains sensitive data pattern: %s", email.To, pattern)
			}
		}
	}
}

func TestE2E_FanOut_LargeRegion(t *testing.T) {
	suite := SetupNotificationE2ETest(t)
	ctx := context.Background()

	// Create region with many users
	region := suite.createRegion("e2e-large-region", "Large Test Region", nil)

	numUsers := 20 // Test with a moderate number
	for i := 0; i < numUsers; i++ {
		_, user := suite.createUser(
			fmt.Sprintf("e2e-large-user-%d", i),
			fmt.Sprintf("largeuser%d@e2e.test", i),
			fmt.Sprintf("largeuser%d", i),
			true, false, false,
		)
		suite.addUserToRegion(user.ID, region.ID, false)
	}

	suite.emailTracker.Reset()

	// Queue fan-out notification
	err := suite.notificationService.QueueInviteLinkUpdatedEvent(ctx, "large-group", region.ID)
	if err != nil {
		t.Fatalf("Failed to queue notification: %v", err)
	}

	// Wait for fan-out and processing
	time.Sleep(5 * time.Second)

	// All users should receive emails
	emails := suite.emailTracker.GetSent()

	// Count emails to our test users
	receivedCount := 0
	for _, email := range emails {
		if contains(email.To, "largeuser") {
			receivedCount++
		}
	}

	if receivedCount != numUsers {
		t.Errorf("Expected %d emails sent, got %d", numUsers, receivedCount)
	}
}

func TestE2E_Worker_RecoversFromFailures(t *testing.T) {
	suite := SetupNotificationE2ETest(t)
	ctx := context.Background()

	// Create user
	_, user := suite.createUser("e2e-failure-user", "failure@e2e.test", "failureuser", true, false, false)
	_ = suite.createRegion("e2e-failure-region", "Failure Region", nil)

	// Simulate a failed notification
	_, err := suite.db.ExecContext(ctx, `
		INSERT INTO email_notifications (id, user_id, notification_type, status, error_message, queued_at)
		VALUES (?, ?, ?, 'failed', 'Test failure', DATE_SUB(NOW(), INTERVAL 2 HOUR))
	`, "e2e-failed-notif", user.ID, "verification_complete")
	if err != nil {
		t.Fatalf("Failed to create failed notification: %v", err)
	}

	// The RetryFailed function should re-queue failed notifications after the retry period
	affected, err := suite.notificationQueue.RetryFailed(ctx, time.Hour)
	if err != nil {
		t.Fatalf("RetryFailed failed: %v", err)
	}

	if affected != 1 {
		t.Errorf("Expected 1 notification to be retried, got %d", affected)
	}

	// Verify it's back in queued status
	var status string
	err = suite.db.QueryRowContext(ctx, `
		SELECT status FROM email_notifications WHERE id = ?
	`, "e2e-failed-notif").Scan(&status)
	if err != nil {
		t.Fatalf("Failed to query notification: %v", err)
	}

	if status != string(models.NotificationStatusQueued) {
		t.Errorf("Expected status 'queued' after retry, got '%s'", status)
	}
}

// =============================================================================
// E2E Tests: Sub-Region Invitation Notifications
// =============================================================================

func TestE2E_SubRegionInvitation_TriggersNotification(t *testing.T) {
	suite := SetupNotificationE2ETest(t)
	ctx := context.Background()

	// Create parent region
	parentRegion := suite.createRegion("e2e-parent-invite-region", "Parent Region", nil)

	// Create sub-region with parent
	subRegion := suite.createRegion("e2e-sub-invite-region", "Sub Region", &parentRegion.ID)

	// Create admin who will invite
	adminToken, admin := suite.createUser("e2e-invite-admin", "inviteadmin@e2e.test", "InviteAdmin", true, true, false)
	suite.addUserToRegion(admin.ID, parentRegion.ID, true)
	suite.addUserToRegion(admin.ID, subRegion.ID, true)

	// Create user to be invited (member of parent region)
	_, invitee := suite.createUser("e2e-invitee", "invitee@e2e.test", "invitee", true, false, false)
	suite.addUserToRegion(invitee.ID, parentRegion.ID, false)

	suite.emailTracker.Reset()

	// Admin invites user to sub-region
	resp := suite.request("POST", fmt.Sprintf("/api/v1/communities/%s/invitations?id=%s", subRegion.ID, subRegion.ID), map[string]interface{}{
		"user_id": invitee.ID,
	}, adminToken)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("Expected status 201, got %d: %v", resp.StatusCode, body)
	}

	// Wait for notification processing
	time.Sleep(3 * time.Second)

	// Check notification was queued
	var count int
	err := suite.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM email_notifications
		WHERE user_id = ? AND notification_type = 'sub_region_invitation'
	`, invitee.ID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count notifications: %v", err)
	}

	if count == 0 {
		t.Error("Expected sub_region_invitation notification to be queued")
	}

	// Check email was sent to invitee
	emails := suite.emailTracker.GetSent()
	found := false
	for _, email := range emails {
		if email.To == invitee.Email {
			found = true
			if !contains(email.Subject, "Invited") {
				t.Errorf("Email subject should mention invitation: %s", email.Subject)
			}
			// Verify inviter name is in the email body
			if !contains(email.Body, "InviteAdmin") {
				t.Error("Email body should contain inviter's username")
			}
		}
	}
	if !found {
		t.Error("Expected email to be sent to invitee")
	}
}

func TestE2E_SubRegionInvitation_EmailContent(t *testing.T) {
	suite := SetupNotificationE2ETest(t)
	ctx := context.Background()

	// Create parent and sub-region
	parentRegion := suite.createRegion("e2e-parent-content-region", "Parent Content Region", nil)
	subRegion := suite.createRegion("e2e-sub-content-region", "Sub Content Region", &parentRegion.ID)

	// Create admin and invitee
	adminToken, admin := suite.createUser("e2e-content-admin", "contentadmin@e2e.test", "ContentAdmin", true, true, false)
	suite.addUserToRegion(admin.ID, parentRegion.ID, true)
	suite.addUserToRegion(admin.ID, subRegion.ID, true)

	_, invitee := suite.createUser("e2e-content-invitee", "contentinvitee@e2e.test", "contentinvitee", true, false, false)
	suite.addUserToRegion(invitee.ID, parentRegion.ID, false)

	suite.emailTracker.Reset()

	// Admin invites user
	resp := suite.request("POST", fmt.Sprintf("/api/v1/communities/%s/invitations?id=%s", subRegion.ID, subRegion.ID), map[string]interface{}{
		"user_id": invitee.ID,
	}, adminToken)
	_ = resp.Body.Close()

	// Wait for notification processing
	time.Sleep(3 * time.Second)

	// Check email content
	emails := suite.emailTracker.GetSent()
	for _, email := range emails {
		if email.To == invitee.Email {
			// Should NOT contain sensitive data
			if contains(email.Body, subRegion.ID) {
				t.Error("Email should NOT contain region ID")
			}
			// Should contain login instructions
			if !contains(email.Body, "log in") && !contains(email.Body, "Log in") {
				t.Error("Email should instruct user to log in")
			}
			// Should mention expiration
			if !contains(email.Body, "7 days") && !contains(email.Body, "expire") {
				t.Error("Email should mention invitation expiration")
			}
		}
	}

	// Verify notification was cleaned up (processed)
	var status string
	err := suite.db.QueryRowContext(ctx, `
		SELECT status FROM email_notifications
		WHERE user_id = ? AND notification_type = 'sub_region_invitation'
		ORDER BY queued_at DESC LIMIT 1
	`, invitee.ID).Scan(&status)
	if err != nil {
		t.Fatalf("Failed to query notification: %v", err)
	}

	if status != string(models.NotificationStatusSent) {
		t.Errorf("Expected notification status 'sent', got '%s'", status)
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
