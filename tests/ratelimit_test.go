package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/handlers"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/mocks"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// RateLimitTestSuite holds dependencies for rate limiting tests
type RateLimitTestSuite struct {
	t           *testing.T
	db          *database.DB
	server      *httptest.Server
	rateLimiter *services.DBRateLimiter
}

// SetupRateLimitTest creates a test suite with rate limiting enabled
func SetupRateLimitTest(t *testing.T, limit int, windowSecs int) *RateLimitTestSuite {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping rate limit tests")
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

	// Clean up rate_limits table before tests
	_, _ = db.ExecContext(context.Background(), "DELETE FROM rate_limits")

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

	// Create handlers
	authHandler := handlers.NewAuthHandler(userRepo, jwtAuth)
	regionHandler := handlers.NewRegionHandler(regionRepo, mockMapbox, nil)
	verificationHandler := handlers.NewVerificationHandler(
		nil, verifyRepo, vouchRepo, userRepo, regionRepo,
		mockPostgrid, mockMapbox, nil,
		false, 30, // Bootstrap cooldown disabled for tests
	)
	consensusConfig := &config.ConsensusConfig{VotePercent: 50, VoteFloor: 3}
	signalGroupHandler := handlers.NewSignalGroupHandler(
		nil, groupRepo, nil, regionRepo, nil,
	)
	adminHandler := handlers.NewAdminHandler(userRepo, regionRepo, nil)

	// Create MFA service and handler
	mfaConfig := &config.MFAConfig{
		EncryptionKey: "01234567890123456789012345678901",
		Issuer:        "Test MFA",
	}
	mfaService, _ := services.NewMFAService(mfaConfig)
	mfaHandler := handlers.NewMFAHandler(nil, userRepo, mfaService, jwtAuth, false, nil)
	membershipHandler := handlers.NewMembershipHandler(nil, membershipRepo, regionRepo, userRepo, nil)

	// Create blocklist proposal handler
	blocklistConfig := &config.BlocklistConfig{
		AddressBlocklistDuration:  2 * 365 * 24 * time.Hour,
		ProposalRateLimitPerMonth: 5,
	}
	blocklistProposalRepo := database.NewBlocklistProposalRepository(db, blocklistConfig)
	blocklistProposalHandler := handlers.NewBlocklistProposalHandler(
		nil, blocklistProposalRepo, regionRepo, userRepo, nil, consensusConfig, blocklistConfig,
	)

	// Create rate limiter with database backend
	rateLimiter := services.NewDBRateLimiter(db.DB)

	// Create rate limit config
	rateLimitConfig := &handlers.RateLimitOptions{
		Enabled:    true,
		Limit:      limit,
		WindowSecs: windowSecs,
	}

	schoolHandler := handlers.NewSchoolHandler(
		db, schoolRepo, districtRepo, schoolVouchRepo, groupRepo, nil,
		userRepo, auditRepo, nil, consensusConfig, false, 0,
	)

	// Create router WITH rate limiting enabled
	router := handlers.NewRouter(
		authHandler, mfaHandler, regionHandler, signalGroupHandler, verificationHandler, adminHandler,
		membershipHandler, blocklistProposalHandler, nil, schoolHandler, nil, nil, nil, nil, jwtAuth, rateLimiter, rateLimitConfig, nil,
		[]string{"*"}, nil,
	)
	handler := router.Setup()

	// Trust loopback so X-Forwarded-For headers from the test client are honoured
	middleware.SetTrustedProxies([]string{"127.0.0.0/8"})

	server := httptest.NewServer(handler)

	suite := &RateLimitTestSuite{
		t:           t,
		db:          db,
		server:      server,
		rateLimiter: rateLimiter,
	}

	t.Cleanup(func() {
		server.Close()
		// Clean up rate_limits table
		_, _ = db.ExecContext(context.Background(), "DELETE FROM rate_limits")
		_ = db.Close()
		// Reset trusted proxies to avoid leaking state
		middleware.SetTrustedProxies(nil)
	})

	return suite
}

func (s *RateLimitTestSuite) request(method, path string, clientIP string) *http.Response {
	req, _ := http.NewRequest(method, s.server.URL+path, nil)
	req.Header.Set("Content-Type", "application/json")
	if clientIP != "" {
		req.Header.Set("X-Forwarded-For", clientIP)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("Request failed: %v", err)
	}

	return resp
}

// cleanupRateLimits removes all rate limit entries for a given key
func (s *RateLimitTestSuite) cleanupRateLimits(key string) {
	ctx := context.Background()
	_ = s.rateLimiter.Reset(ctx, key)
}

// ============================================
// Integration Tests - DBRateLimiter with real database
// ============================================

func TestIntegration_DBRateLimiter_AllowsRequestsUnderLimit(t *testing.T) {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping integration tests")
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
	defer func() { _ = db.Close() }()

	// Clean up before test
	_, _ = db.ExecContext(context.Background(), "DELETE FROM rate_limits")

	limiter := services.NewDBRateLimiter(db.DB)
	ctx := context.Background()
	testKey := "integration-test-ip-1"

	defer func() { _ = limiter.Reset(ctx, testKey) }()

	// Make 5 requests with a limit of 10
	for i := 0; i < 5; i++ {
		allowed, remaining, _, err := limiter.Allow(ctx, testKey, 10, time.Minute)
		if err != nil {
			t.Fatalf("Request %d: unexpected error: %v", i+1, err)
		}
		if !allowed {
			t.Errorf("Request %d: should be allowed (under limit)", i+1)
		}
		expectedRemaining := 10 - (i + 1)
		if remaining != expectedRemaining {
			t.Errorf("Request %d: expected remaining %d, got %d", i+1, expectedRemaining, remaining)
		}
	}
}

func TestIntegration_DBRateLimiter_BlocksRequestsOverLimit(t *testing.T) {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping integration tests")
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
	defer func() { _ = db.Close() }()

	// Clean up before test
	_, _ = db.ExecContext(context.Background(), "DELETE FROM rate_limits")

	limiter := services.NewDBRateLimiter(db.DB)
	ctx := context.Background()
	testKey := "integration-test-ip-2"

	defer func() { _ = limiter.Reset(ctx, testKey) }()

	// Use up the limit (3 requests)
	for i := 0; i < 3; i++ {
		allowed, _, _, err := limiter.Allow(ctx, testKey, 3, time.Minute)
		if err != nil {
			t.Fatalf("Request %d: unexpected error: %v", i+1, err)
		}
		if !allowed {
			t.Errorf("Request %d: should be allowed (at or under limit)", i+1)
		}
	}

	// Next request should be blocked
	allowed, remaining, _, err := limiter.Allow(ctx, testKey, 3, time.Minute)
	if err != nil {
		t.Fatalf("Request 4: unexpected error: %v", err)
	}
	if allowed {
		t.Error("Request 4: should be blocked (over limit)")
	}
	if remaining != 0 {
		t.Errorf("Request 4: expected remaining 0, got %d", remaining)
	}
}

func TestIntegration_DBRateLimiter_DifferentIPsAreIndependent(t *testing.T) {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping integration tests")
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
	defer func() { _ = db.Close() }()

	// Clean up before test
	_, _ = db.ExecContext(context.Background(), "DELETE FROM rate_limits")

	limiter := services.NewDBRateLimiter(db.DB)
	ctx := context.Background()
	ip1 := "192.168.1.100"
	ip2 := "192.168.1.101"

	defer func() {
		_ = limiter.Reset(ctx, ip1)
		_ = limiter.Reset(ctx, ip2)
	}()

	// Exhaust limit for ip1
	for i := 0; i < 3; i++ {
		_, _, _, _ = limiter.Allow(ctx, ip1, 3, time.Minute)
	}

	// Verify ip1 is blocked
	allowed, _, _, _ := limiter.Allow(ctx, ip1, 3, time.Minute)
	if allowed {
		t.Error("ip1 should be blocked (over limit)")
	}

	// ip2 should still be allowed
	allowed, remaining, _, err := limiter.Allow(ctx, ip2, 3, time.Minute)
	if err != nil {
		t.Fatalf("ip2: unexpected error: %v", err)
	}
	if !allowed {
		t.Error("ip2 should be allowed (independent from ip1)")
	}
	if remaining != 2 {
		t.Errorf("ip2: expected remaining 2, got %d", remaining)
	}
}

func TestIntegration_DBRateLimiter_ResetClearsLimit(t *testing.T) {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping integration tests")
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
	defer func() { _ = db.Close() }()

	// Clean up before test
	_, _ = db.ExecContext(context.Background(), "DELETE FROM rate_limits")

	limiter := services.NewDBRateLimiter(db.DB)
	ctx := context.Background()
	testKey := "integration-test-ip-reset"

	defer func() { _ = limiter.Reset(ctx, testKey) }()

	// Exhaust limit
	for i := 0; i < 5; i++ {
		_, _, _, _ = limiter.Allow(ctx, testKey, 5, time.Minute)
	}

	// Verify blocked
	allowed, _, _, _ := limiter.Allow(ctx, testKey, 5, time.Minute)
	if allowed {
		t.Error("Should be blocked before reset")
	}

	// Reset
	err = limiter.Reset(ctx, testKey)
	if err != nil {
		t.Fatalf("Failed to reset: %v", err)
	}

	// Should be allowed again
	allowed, remaining, _, err := limiter.Allow(ctx, testKey, 5, time.Minute)
	if err != nil {
		t.Fatalf("After reset: unexpected error: %v", err)
	}
	if !allowed {
		t.Error("Should be allowed after reset")
	}
	if remaining != 4 {
		t.Errorf("After reset: expected remaining 4, got %d", remaining)
	}
}

// ============================================
// E2E Tests - Full HTTP stack with rate limiting
// ============================================

func TestE2E_RateLimiting_SetsCorrectHeaders(t *testing.T) {
	suite := SetupRateLimitTest(t, 10, 60)
	clientIP := "10.0.0.1"
	defer suite.cleanupRateLimits(clientIP)

	resp := suite.request("GET", "/health", clientIP)
	defer func() { _ = resp.Body.Close() }()

	// Check rate limit headers
	limitHeader := resp.Header.Get("X-RateLimit-Limit")
	if limitHeader != "10" {
		t.Errorf("Expected X-RateLimit-Limit=10, got %s", limitHeader)
	}

	remainingHeader := resp.Header.Get("X-RateLimit-Remaining")
	if remainingHeader != "9" {
		t.Errorf("Expected X-RateLimit-Remaining=9, got %s", remainingHeader)
	}

	resetHeader := resp.Header.Get("X-RateLimit-Reset")
	if resetHeader == "" {
		t.Error("Expected X-RateLimit-Reset header to be set")
	}

	// Verify reset time is in the future
	resetUnix, err := strconv.ParseInt(resetHeader, 10, 64)
	if err != nil {
		t.Errorf("Failed to parse reset time: %v", err)
	}
	if resetUnix <= time.Now().Unix() {
		t.Error("Reset time should be in the future")
	}
}

func TestE2E_RateLimiting_AllowsRequestsUnderLimit(t *testing.T) {
	suite := SetupRateLimitTest(t, 5, 60)
	clientIP := "10.0.0.2"
	defer suite.cleanupRateLimits(clientIP)

	// Make 5 requests (at limit)
	for i := 0; i < 5; i++ {
		resp := suite.request("GET", "/health", clientIP)
		statusCode := resp.StatusCode
		_ = resp.Body.Close()

		if statusCode != http.StatusOK {
			t.Errorf("Request %d: expected status 200, got %d", i+1, statusCode)
		}
	}
}

func TestE2E_RateLimiting_BlocksRequestsOverLimit(t *testing.T) {
	suite := SetupRateLimitTest(t, 3, 60)
	clientIP := "10.0.0.3"
	defer suite.cleanupRateLimits(clientIP)

	// Make 3 requests (exhaust limit)
	for i := 0; i < 3; i++ {
		resp := suite.request("GET", "/health", clientIP)
		_ = resp.Body.Close()
	}

	// 4th request should be blocked
	resp := suite.request("GET", "/health", clientIP)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("Expected status 429, got %d", resp.StatusCode)
	}

	// Check Retry-After header
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		t.Error("Expected Retry-After header to be set")
	}

	// Check response body
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	if body["error"] != "rate_limit_exceeded" {
		t.Errorf("Expected error=rate_limit_exceeded, got %v", body["error"])
	}
}

func TestE2E_RateLimiting_DifferentIPsAreIndependent(t *testing.T) {
	suite := SetupRateLimitTest(t, 2, 60)
	clientIP1 := "10.0.0.4"
	clientIP2 := "10.0.0.5"
	defer suite.cleanupRateLimits(clientIP1)
	defer suite.cleanupRateLimits(clientIP2)

	// Exhaust limit for clientIP1
	for i := 0; i < 2; i++ {
		resp := suite.request("GET", "/health", clientIP1)
		_ = resp.Body.Close()
	}

	// clientIP1 should now be blocked
	resp1 := suite.request("GET", "/health", clientIP1)
	statusIP1 := resp1.StatusCode
	_ = resp1.Body.Close()

	if statusIP1 != http.StatusTooManyRequests {
		t.Errorf("clientIP1: expected status 429, got %d", statusIP1)
	}

	// clientIP2 should still be allowed
	resp2 := suite.request("GET", "/health", clientIP2)
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("clientIP2: expected status 200, got %d", resp2.StatusCode)
	}

	// Check remaining count for clientIP2
	remainingHeader := resp2.Header.Get("X-RateLimit-Remaining")
	if remainingHeader != "1" {
		t.Errorf("clientIP2: expected X-RateLimit-Remaining=1, got %s", remainingHeader)
	}
}

func TestE2E_RateLimiting_DecrementsRemainingCorrectly(t *testing.T) {
	suite := SetupRateLimitTest(t, 5, 60)
	clientIP := "10.0.0.6"
	defer suite.cleanupRateLimits(clientIP)

	expectedRemaining := []string{"4", "3", "2", "1", "0"}

	for i, expected := range expectedRemaining {
		resp := suite.request("GET", "/health", clientIP)
		remaining := resp.Header.Get("X-RateLimit-Remaining")
		_ = resp.Body.Close()

		if remaining != expected {
			t.Errorf("Request %d: expected remaining %s, got %s", i+1, expected, remaining)
		}
	}
}

func TestE2E_RateLimiting_WorksWithXRealIPHeader(t *testing.T) {
	suite := SetupRateLimitTest(t, 3, 60)

	// Make a request with X-Real-IP instead of X-Forwarded-For
	req, _ := http.NewRequest("GET", suite.server.URL+"/health", nil)
	req.Header.Set("X-Real-IP", "10.0.0.7")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Should have rate limit headers
	if resp.Header.Get("X-RateLimit-Limit") != "3" {
		t.Errorf("Expected X-RateLimit-Limit=3, got %s", resp.Header.Get("X-RateLimit-Limit"))
	}

	// Clean up
	suite.cleanupRateLimits("10.0.0.7")
}

func TestE2E_RateLimiting_ProtectsAllAPIEndpoints(t *testing.T) {
	suite := SetupRateLimitTest(t, 2, 60)
	clientIP := "10.0.0.8"
	defer suite.cleanupRateLimits(clientIP)

	// Use up the limit
	for i := 0; i < 2; i++ {
		resp := suite.request("GET", "/health", clientIP)
		_ = resp.Body.Close()
	}

	// Different endpoints should also be blocked (same IP)
	endpoints := []string{
		"/api/v1/auth/register",
		"/api/v1/communities",
	}

	for _, endpoint := range endpoints {
		resp := suite.request("GET", endpoint, clientIP)
		statusCode := resp.StatusCode
		_ = resp.Body.Close()

		// Note: These might return 401 or 405 normally, but when rate limited
		// they should return 429 before hitting the handler
		if statusCode != http.StatusTooManyRequests {
			t.Errorf("Endpoint %s: expected status 429, got %d", endpoint, statusCode)
		}
	}
}
