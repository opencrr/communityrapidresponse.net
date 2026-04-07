package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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

// AuthRateLimitTestSuite holds dependencies for auth-specific rate limit E2E tests
type AuthRateLimitTestSuite struct {
	t        *testing.T
	db       *database.DB
	server   *httptest.Server
	userRepo *database.UserRepository
	limiter  services.RateLimiter
}

// SetupAuthRateLimitE2ETest creates a test suite with auth-specific rate limiting enabled.
// Global rate limiting is set high so it does not interfere.
func SetupAuthRateLimitE2ETest(t *testing.T) *AuthRateLimitTestSuite {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping auth rate limit e2e tests")
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
	schoolRepo := database.NewSchoolRepository(db)
	communityGroupRepo := database.NewGroupRepository(db)
	districtRepo := database.NewSchoolDistrictRepository(db)
	auditRepo := database.NewAuditRepository(db)

	// Create JWT auth
	jwtConfig := &config.JWTConfig{
		Secret:          "test_secret_key_at_least_32_characters_long",
		ExpirationHours: 24,
		Issuer:          "test_issuer",
	}
	jwtAuth := middleware.NewJWTAuth(jwtConfig)

	// Create mock services
	mockPostgrid := mocks.NewMockPostgridService()
	mockMapbox := mocks.NewMockMapboxService()

	// Create auth handler WITH full constructor (needed for lockout, which uses auditRepo + db)
	authHandler := handlers.NewAuthHandlerWithEmailService(
		db, userRepo, jwtAuth, nil, jwtConfig.Secret,
		false, false, auditRepo, nil, nil, "http://localhost:3000", nil,
	)

	// Set up auth-specific rate limiter
	limiter := services.NewInMemoryRateLimiter()
	authHandler.SetRateLimiter(limiter)

	// Create other handlers
	regionHandler := handlers.NewRegionHandler(regionRepo, mockMapbox, nil)
	verificationHandler := handlers.NewVerificationHandler(
		nil, verifyRepo, vouchRepo, userRepo, regionRepo,
		mockPostgrid, mockMapbox, nil,
		false, 30,
	)
	adminHandler := handlers.NewAdminHandler(userRepo, regionRepo, nil)

	mfaConfig := &config.MFAConfig{
		EncryptionKey: "01234567890123456789012345678901",
		Issuer:        "Test MFA",
	}
	mfaService, _ := services.NewMFAService(mfaConfig)
	mfaHandler := handlers.NewMFAHandler(nil, userRepo, mfaService, jwtAuth, false, nil)

	schoolHandler := handlers.NewSchoolHandler(
		schoolRepo, districtRepo, communityGroupRepo, userRepo, auditRepo, nil,
	)

	// Create router WITHOUT global rate limiting (pass nil) so only auth rate limits apply
	router := handlers.NewRouter(
		authHandler, mfaHandler, regionHandler, verificationHandler, adminHandler,
		schoolHandler, nil, nil, nil, jwtAuth, nil, nil, nil,
		[]string{"*"}, nil,
	)
	handler := router.Setup()

	server := httptest.NewServer(handler)

	suite := &AuthRateLimitTestSuite{
		t:        t,
		db:       db,
		server:   server,
		userRepo: userRepo,
		limiter:  limiter,
	}

	t.Cleanup(func() {
		server.Close()
		_ = db.Close()
	})

	return suite
}

func (s *AuthRateLimitTestSuite) postJSON(path string, body interface{}) *http.Response {
	bodyBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", s.server.URL+path, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("Request failed: %v", err)
	}
	return resp
}

// registerAndLogin creates a test user, disables MFA, and returns the userID.
func (s *AuthRateLimitTestSuite) registerAndLogin(username, email, password string) string {
	resp := s.postJSON("/api/v1/auth/register", map[string]string{
		"username": username, "email": email, "password": password,
	})
	defer func() { _ = resp.Body.Close() }()

	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	userID := registerResp.UserID

	if userID == "" {
		var id string
		_ = s.db.QueryRowContext(context.Background(), "SELECT id FROM users WHERE email = ?", email).Scan(&id)
		userID = id
	}

	// Disable MFA
	_, _ = s.db.ExecContext(context.Background(),
		"UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", userID)

	return userID
}

// ============================================
// E2E Tests - Auth Rate Limiting
// ============================================

func TestE2E_AuthRateLimit_LoginBlocked(t *testing.T) {
	suite := SetupAuthRateLimitE2ETest(t)

	// Login rate limit is 10 per 5 minutes
	// Make 10 login requests with a nonexistent email
	for i := 0; i < 10; i++ {
		resp := suite.postJSON("/api/v1/auth/login", map[string]string{
			"email":    "nonexistent@authrltest.com",
			"password": "anypassword12345",
		})
		statusCode := resp.StatusCode
		_ = resp.Body.Close()

		if statusCode == http.StatusTooManyRequests {
			t.Fatalf("Request %d: got rate limited too early, expected 401", i+1)
		}
		if statusCode != http.StatusUnauthorized {
			t.Fatalf("Request %d: expected 401, got %d", i+1, statusCode)
		}
	}

	// 11th request should be rate limited
	resp := suite.postJSON("/api/v1/auth/login", map[string]string{
		"email":    "nonexistent@authrltest.com",
		"password": "anypassword12345",
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("Expected status 429 for rate-limited login, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "rate_limited" {
		t.Errorf("Expected error 'rate_limited', got '%v'", body["error"])
	}
}

func TestE2E_AuthRateLimit_RegisterBlocked(t *testing.T) {
	suite := SetupAuthRateLimitE2ETest(t)

	// Clean up test users
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@authregrltest.com'")
	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@authregrltest.com'")
	})

	// Register rate limit is 3 per hour
	for i := 0; i < 3; i++ {
		resp := suite.postJSON("/api/v1/auth/register", map[string]string{
			"username": fmt.Sprintf("regrl%d", i),
			"email":    fmt.Sprintf("regrl%d@authregrltest.com", i),
			"password": "securepassword123",
		})
		statusCode := resp.StatusCode
		_ = resp.Body.Close()

		if statusCode == http.StatusTooManyRequests {
			t.Fatalf("Request %d: got rate limited too early", i+1)
		}
	}

	// 4th registration should be rate limited
	resp := suite.postJSON("/api/v1/auth/register", map[string]string{
		"username": "regrlblocked",
		"email":    "regrlblocked@authregrltest.com",
		"password": "securepassword123",
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("Expected status 429 for rate-limited register, got %d", resp.StatusCode)
	}
}

func TestE2E_AuthRateLimit_ForgotPasswordBlocked(t *testing.T) {
	suite := SetupAuthRateLimitE2ETest(t)

	// ForgotPassword rate limit is 5 per 15 minutes
	// Handler will return 503 (password reset not configured) but rate limit is consumed
	for i := 0; i < 5; i++ {
		resp := suite.postJSON("/api/v1/auth/forgot-password", map[string]string{
			"email": "test@forgotrltest.com",
		})
		statusCode := resp.StatusCode
		_ = resp.Body.Close()

		if statusCode == http.StatusTooManyRequests {
			t.Fatalf("Request %d: got rate limited too early", i+1)
		}
	}

	// 6th request should be rate limited
	resp := suite.postJSON("/api/v1/auth/forgot-password", map[string]string{
		"email": "test@forgotrltest.com",
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("Expected status 429 for rate-limited forgot-password, got %d", resp.StatusCode)
	}
}

func TestE2E_AuthRateLimit_AccountLockoutAccumulates(t *testing.T) {
	suite := SetupAuthRateLimitE2ETest(t)

	// Clean up and register a user
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@e2elockout1.com'")
	userID := suite.registerAndLogin("e2elockout1", "user@e2elockout1.com", "securepassword123")

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", userID)
	})

	// Set counter to 9 via DB (avoids consuming rate limit tokens)
	_, _ = suite.db.ExecContext(context.Background(),
		"UPDATE users SET failed_login_attempts = 9, locked_until = NULL WHERE id = ?", userID)

	// One more wrong password should trigger lockout (10th attempt)
	resp := suite.postJSON("/api/v1/auth/login", map[string]string{
		"email":    "user@e2elockout1.com",
		"password": "wrongpassword123",
	})
	_ = resp.Body.Close()

	// Verify account is now locked
	user, err := suite.userRepo.GetByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}
	if user.FailedLoginAttempts != 10 {
		t.Errorf("Expected 10 failed attempts, got %d", user.FailedLoginAttempts)
	}
	if user.LockedUntil == nil {
		t.Fatal("Expected account to be locked")
	}
}

func TestE2E_AuthRateLimit_LockedAccountRejectsCorrectPassword(t *testing.T) {
	suite := SetupAuthRateLimitE2ETest(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@e2elockout2.com'")
	userID := suite.registerAndLogin("e2elockout2", "user@e2elockout2.com", "securepassword123")

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", userID)
	})

	// Lock the account via DB
	lockUntil := time.Now().Add(15 * time.Minute)
	_, _ = suite.db.ExecContext(context.Background(),
		"UPDATE users SET failed_login_attempts = 10, locked_until = ? WHERE id = ?", lockUntil, userID)

	resp := suite.postJSON("/api/v1/auth/login", map[string]string{
		"email":    "user@e2elockout2.com",
		"password": "securepassword123", // Correct password
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("Expected 429 for locked account, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "account_locked" {
		t.Errorf("Expected error 'account_locked', got '%v'", body["error"])
	}
}

func TestE2E_AuthRateLimit_LoginSucceedsAfterLockExpires(t *testing.T) {
	suite := SetupAuthRateLimitE2ETest(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@e2elockout3.com'")
	userID := suite.registerAndLogin("e2elockout3", "user@e2elockout3.com", "securepassword123")

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", userID)
	})

	// Set lock to the past via DB
	lockPast := time.Now().Add(-1 * time.Minute)
	_, _ = suite.db.ExecContext(context.Background(),
		"UPDATE users SET failed_login_attempts = 10, locked_until = ? WHERE id = ?", lockPast, userID)

	resp := suite.postJSON("/api/v1/auth/login", map[string]string{
		"email":    "user@e2elockout3.com",
		"password": "securepassword123",
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 after lock expired, got %d", resp.StatusCode)
	}

	// Verify counter was reset
	user, err := suite.userRepo.GetByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}
	if user.FailedLoginAttempts != 0 {
		t.Errorf("Expected FailedLoginAttempts=0 after successful login, got %d", user.FailedLoginAttempts)
	}
}

func TestE2E_AuthRateLimit_AccountLockoutAuditLog(t *testing.T) {
	suite := SetupAuthRateLimitE2ETest(t)

	// Create user
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@e2eaudit.com'")
	userID := suite.registerAndLogin("e2eaudit", "user@e2eaudit.com", "securepassword123")

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM audit_log WHERE user_id = ?", userID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", userID)
	})

	// Set attempts to 9, then fail once more to trigger lockout
	_, _ = suite.db.ExecContext(context.Background(),
		"UPDATE users SET failed_login_attempts = 9, locked_until = NULL WHERE id = ?", userID)
	_, _ = suite.db.ExecContext(context.Background(),
		"DELETE FROM audit_log WHERE user_id = ? AND action = ?", userID, models.AuditActionAccountLocked)

	resp := suite.postJSON("/api/v1/auth/login", map[string]string{
		"email":    "user@e2eaudit.com",
		"password": "wrongpassword123",
	})
	_ = resp.Body.Close()

	// Verify audit log entry exists
	var auditCount int
	err := suite.db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM audit_log WHERE user_id = ? AND action = ?",
		userID, models.AuditActionAccountLocked,
	).Scan(&auditCount)
	if err != nil {
		t.Fatalf("Failed to query audit log: %v", err)
	}
	if auditCount == 0 {
		t.Error("Expected audit log entry for account_locked action")
	}
}

func TestE2E_AuthRateLimit_SuccessfulLoginResetsCounter(t *testing.T) {
	suite := SetupAuthRateLimitE2ETest(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@e2ereset.com'")
	userID := suite.registerAndLogin("e2ereset", "user@e2ereset.com", "securepassword123")

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", userID)
	})

	// Set some failed attempts (below lockout threshold)
	_, _ = suite.db.ExecContext(context.Background(),
		"UPDATE users SET failed_login_attempts = 5, locked_until = NULL WHERE id = ?", userID)

	// Login with correct password
	resp := suite.postJSON("/api/v1/auth/login", map[string]string{
		"email":    "user@e2ereset.com",
		"password": "securepassword123",
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	// Verify counter was reset
	user, err := suite.userRepo.GetByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}
	if user.FailedLoginAttempts != 0 {
		t.Errorf("Expected FailedLoginAttempts=0, got %d", user.FailedLoginAttempts)
	}
}

func TestE2E_AuthRateLimit_LockoutViaUsername(t *testing.T) {
	suite := SetupAuthRateLimitE2ETest(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@e2eusername.com'")
	userID := suite.registerAndLogin("e2eusername", "user@e2eusername.com", "securepassword123")

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", userID)
	})

	// Set attempts to 9
	_, _ = suite.db.ExecContext(context.Background(),
		"UPDATE users SET failed_login_attempts = 9, locked_until = NULL WHERE id = ?", userID)

	// Fail login using username (not email)
	resp := suite.postJSON("/api/v1/auth/login", map[string]string{
		"email":    "e2eusername", // Using username
		"password": "wrongpassword123",
	})
	_ = resp.Body.Close()

	// Verify account is locked
	user, err := suite.userRepo.GetByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}
	if user.LockedUntil == nil {
		t.Error("Expected account to be locked via username login")
	}
	if user.FailedLoginAttempts != 10 {
		t.Errorf("Expected 10 failed attempts, got %d", user.FailedLoginAttempts)
	}
}

func TestE2E_AuthRateLimit_DeletedUserNoLockout(t *testing.T) {
	suite := SetupAuthRateLimitE2ETest(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@e2edeleted.com'")
	userID := suite.registerAndLogin("e2edeleted", "user@e2edeleted.com", "securepassword123")

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", userID)
	})

	// Soft-delete the user
	_, _ = suite.db.ExecContext(context.Background(),
		"UPDATE users SET deleted_at = NOW() WHERE id = ?", userID)

	// Login attempt should return 401 (not increment lockout)
	resp := suite.postJSON("/api/v1/auth/login", map[string]string{
		"email":    "user@e2edeleted.com",
		"password": "securepassword123",
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 for deleted user, got %d", resp.StatusCode)
	}

	// Verify no lockout increment
	user, err := suite.userRepo.GetByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}
	if user.FailedLoginAttempts != 0 {
		t.Errorf("Expected no lockout increment for deleted user, got %d", user.FailedLoginAttempts)
	}
}
