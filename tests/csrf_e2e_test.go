package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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

const csrfTestSecret = "csrf_test_secret_key_at_least_32_chars"

// CSRFTestSuite holds dependencies for CSRF integration/e2e tests.
// Unlike other test suites, this one creates the router WITH CSRF enabled.
type CSRFTestSuite struct {
	t        *testing.T
	db       *database.DB
	server   *httptest.Server
	userRepo *database.UserRepository
	jwtAuth  *middleware.JWTAuth
}

// SetupCSRFTest creates a test suite with CSRF protection enabled in the router.
func SetupCSRFTest(t *testing.T) *CSRFTestSuite {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping CSRF e2e tests")
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
	regionRepo := database.NewRegionRepository(db)
	verifyRepo := database.NewVerificationRepository(db)
	vouchRepo := database.NewVouchRepository(db)
	groupRepo := database.NewSignalGroupRepository(db)
	membershipRepo := database.NewMembershipRepository(db)
	schoolRepo := database.NewSchoolRepository(db)
	districtRepo := database.NewSchoolDistrictRepository(db)
	schoolVouchRepo := database.NewSchoolVouchRepository(db)
	auditRepo := database.NewAuditRepository(db)

	jwtConfig := &config.JWTConfig{
		Secret:          csrfTestSecret,
		ExpirationHours: 24,
		Issuer:          "csrf_test_issuer",
	}
	jwtAuth := middleware.NewJWTAuth(jwtConfig)

	mockPostgrid := mocks.NewMockPostgridService()
	mockMapbox := mocks.NewMockMapboxService()

	authHandler := handlers.NewAuthHandler(userRepo, jwtAuth)
	regionHandler := handlers.NewRegionHandler(regionRepo, mockMapbox, nil)
	verificationHandler := handlers.NewVerificationHandler(
		nil, verifyRepo, vouchRepo, userRepo, regionRepo,
		mockPostgrid, mockMapbox, nil,
		false, 30,
	)
	consensusConfig := &config.ConsensusConfig{VotePercent: 50, VoteFloor: 3}
	signalGroupHandler := handlers.NewSignalGroupHandler(
		nil, groupRepo, nil, regionRepo, nil,
	)
	adminHandler := handlers.NewAdminHandler(userRepo, regionRepo, nil)

	mfaConfig := &config.MFAConfig{
		EncryptionKey: "01234567890123456789012345678901",
		Issuer:        "Test MFA",
	}
	mfaService, _ := services.NewMFAService(mfaConfig)
	mfaHandler := handlers.NewMFAHandler(nil, userRepo, mfaService, jwtAuth, false, nil)
	membershipHandler := handlers.NewMembershipHandler(nil, membershipRepo, regionRepo, userRepo, nil)

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

	// CSRF enabled for this test suite
	csrfConfig := &handlers.CSRFConfig{
		Enabled:       true,
		Secret:        csrfTestSecret,
		SecureCookies: false,
	}

	router := handlers.NewRouter(
		authHandler, mfaHandler, regionHandler, signalGroupHandler, verificationHandler, adminHandler,
		membershipHandler, blocklistProposalHandler, nil, schoolHandler, nil, nil, nil, nil, jwtAuth, nil, nil, csrfConfig,
		[]string{"*"}, nil,
	)
	handler := router.Setup()
	server := httptest.NewServer(handler)

	suite := &CSRFTestSuite{
		t:        t,
		db:       db,
		server:   server,
		userRepo: userRepo,
		jwtAuth:  jwtAuth,
	}

	t.Cleanup(func() {
		server.Close()
		_ = db.Close()
	})

	return suite
}

// newCookieClient creates an http.Client with a cookie jar so cookies persist across requests.
func (s *CSRFTestSuite) newCookieClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

// doRequest performs an HTTP request and returns the response.
func (s *CSRFTestSuite) doRequest(client *http.Client, method, path string, body interface{}, token string) *http.Response {
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

	resp, err := client.Do(req)
	if err != nil {
		s.t.Fatalf("Request failed: %v", err)
	}
	return resp
}

// doRequestWithCSRF performs an HTTP request with the CSRF token header set from the client's cookie jar.
func (s *CSRFTestSuite) doRequestWithCSRF(client *http.Client, method, path string, body interface{}, token string) *http.Response {
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

	// Extract CSRF token from cookie jar and set as header
	csrfToken := s.getCSRFTokenFromJar(client)
	if csrfToken != "" {
		req.Header.Set("X-CSRF-Token", csrfToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		s.t.Fatalf("Request failed: %v", err)
	}
	return resp
}

// getCSRFTokenFromJar reads the csrf_token cookie from the client's cookie jar.
func (s *CSRFTestSuite) getCSRFTokenFromJar(client *http.Client) string {
	cookies := client.Jar.Cookies(mustParseURL(s.server.URL))
	for _, c := range cookies {
		if c.Name == "csrf_token" {
			return c.Value
		}
	}
	return ""
}

// registerAndLogin creates a test user and returns (userID, jwtToken).
func (s *CSRFTestSuite) registerAndLogin(client *http.Client, username, email, password string) (string, string) {
	resp := s.doRequest(client, "POST", "/api/v1/auth/register", map[string]string{
		"username": username,
		"email":    email,
		"password": password,
	}, "")
	defer func() { _ = resp.Body.Close() }()

	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)

	userID := registerResp.UserID
	if userID == "" {
		var id string
		_ = s.db.QueryRowContext(context.Background(), "SELECT id FROM users WHERE email = ?", email).Scan(&id)
		userID = id
	}

	// Disable MFA and mark email verified for test
	_, _ = s.db.ExecContext(context.Background(),
		"UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, email_verified = TRUE WHERE id = ?", userID)

	resp2 := s.doRequest(client, "POST", "/api/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, "")
	defer func() { _ = resp2.Body.Close() }()

	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp2.Body).Decode(&loginResp)

	return userID, loginResp.Token
}

func (s *CSRFTestSuite) cleanup(userIDs ...string) {
	ctx := context.Background()
	for _, id := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM school_vouches WHERE voucher_user_id = ? OR vouched_user_id = ?", id, id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_schools WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM verification_requests WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM vouches WHERE voucher_user_id = ? OR vouched_user_id = ?", id, id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	}
}

func mustParseURL(rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return u
}

// ---------------------------------------------------------------------------
// Integration Tests — CSRF middleware behavior through the full router stack
// ---------------------------------------------------------------------------

func TestCSRFIntegration_GetRequestSetsTokenCookie(t *testing.T) {
	suite := SetupCSRFTest(t)

	client := suite.newCookieClient()
	resp := suite.doRequest(client, "GET", "/api/v1/health", nil, "")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from health, got %d", resp.StatusCode)
	}

	csrfToken := suite.getCSRFTokenFromJar(client)
	if csrfToken == "" {
		t.Fatal("expected csrf_token cookie to be set after GET request")
	}
}

func TestCSRFIntegration_CookieAttributes(t *testing.T) {
	suite := SetupCSRFTest(t)

	// Use a client without cookie jar so we can inspect raw Set-Cookie headers
	resp := suite.doRequest(&http.Client{Timeout: 10 * time.Second}, "GET", "/api/v1/health", nil, "")
	defer func() { _ = resp.Body.Close() }()

	var csrfCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "csrf_token" {
			csrfCookie = c
			break
		}
	}

	if csrfCookie == nil {
		t.Fatal("expected csrf_token cookie in response")
	}

	if csrfCookie.HttpOnly {
		t.Error("csrf_token cookie must NOT be HttpOnly (JS needs to read it)")
	}

	if csrfCookie.Path != "/" {
		t.Errorf("expected Path '/', got '%s'", csrfCookie.Path)
	}
}

func TestCSRFIntegration_CookieNotOverwrittenOnSubsequentGET(t *testing.T) {
	suite := SetupCSRFTest(t)

	client := suite.newCookieClient()

	// First GET sets the cookie
	resp1 := suite.doRequest(client, "GET", "/api/v1/health", nil, "")
	_ = resp1.Body.Close()

	firstToken := suite.getCSRFTokenFromJar(client)
	if firstToken == "" {
		t.Fatal("expected csrf_token cookie after first GET")
	}

	// Second GET should not overwrite the existing cookie
	resp2 := suite.doRequest(client, "GET", "/api/v1/health", nil, "")
	_ = resp2.Body.Close()

	// Check that the response does NOT contain a new Set-Cookie for csrf_token
	for _, c := range resp2.Cookies() {
		if c.Name == "csrf_token" {
			t.Error("server should not set a new csrf_token cookie when one already exists")
		}
	}

	secondToken := suite.getCSRFTokenFromJar(client)
	if secondToken != firstToken {
		t.Error("csrf_token should remain the same across GET requests")
	}
}

func TestCSRFIntegration_ExemptAuthEndpoints(t *testing.T) {
	suite := SetupCSRFTest(t)

	// Auth endpoints are exempt — POST without CSRF token should succeed (or fail for
	// business logic reasons, but NOT with 403 CSRF error)
	exemptPaths := []struct {
		path    string
		payload interface{}
	}{
		{"/api/v1/auth/register", map[string]string{
			"username": fmt.Sprintf("csrfexempt%d", time.Now().UnixNano()),
			"email":    fmt.Sprintf("csrfexempt%d@test.com", time.Now().UnixNano()),
			"password": "securepassword123",
		}},
		{"/api/v1/auth/login", map[string]string{
			"email":    "nonexistent@test.com",
			"password": "wrongpassword123",
		}},
		{"/api/v1/auth/logout", nil},
		{"/api/v1/auth/forgot-password", map[string]string{
			"email": "nonexistent@test.com",
		}},
		{"/api/v1/auth/reset-password", map[string]string{
			"token":    "invalidtoken",
			"password": "newpassword12345",
		}},
		{"/api/v1/auth/resend-verification", nil},
	}

	for _, tc := range exemptPaths {
		t.Run(tc.path, func(t *testing.T) {
			// Use a fresh client with NO csrf_token cookie and NO header
			client := &http.Client{Timeout: 10 * time.Second}
			resp := suite.doRequest(client, "POST", tc.path, tc.payload, "")
			defer func() { _ = resp.Body.Close() }()

			// Should NOT be 403 csrf_validation_failed
			if resp.StatusCode == http.StatusForbidden {
				var body map[string]string
				_ = json.NewDecoder(resp.Body).Decode(&body)
				if body["error"] == "csrf_validation_failed" {
					t.Errorf("exempt path %s returned CSRF 403", tc.path)
				}
			}
		})
	}

	// Clean up any user that was successfully registered
	ctx := context.Background()
	rows, _ := suite.db.QueryContext(ctx, "SELECT id FROM users WHERE email LIKE 'csrfexempt%@test.com'")
	if rows != nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var userID string
			_ = rows.Scan(&userID)
			suite.cleanup(userID)
		}
	}
}

func TestCSRFIntegration_ExemptMFAEndpoints(t *testing.T) {
	suite := SetupCSRFTest(t)

	mfaPaths := []string{
		"/api/v1/mfa/setup",
		"/api/v1/mfa/setup/complete",
		"/api/v1/mfa/verify",
	}

	for _, path := range mfaPaths {
		t.Run(path, func(t *testing.T) {
			client := &http.Client{Timeout: 10 * time.Second}
			resp := suite.doRequest(client, "POST", path, nil, "")
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode == http.StatusForbidden {
				var body map[string]string
				_ = json.NewDecoder(resp.Body).Decode(&body)
				if body["error"] == "csrf_validation_failed" {
					t.Errorf("exempt MFA path %s returned CSRF 403", path)
				}
			}
		})
	}
}

func TestCSRFIntegration_ProtectedEndpointBlocksWithoutToken(t *testing.T) {
	suite := SetupCSRFTest(t)

	// Create a user directly in the database for authenticated requests
	ctx := context.Background()
	userID := fmt.Sprintf("csrf-nt-%d", time.Now().UnixNano()%100000000)
	user := &models.User{
		ID:            userID,
		Username:      fmt.Sprintf("csrfnotoken%d", time.Now().UnixNano()),
		Email:         fmt.Sprintf("csrfnotoken%d@test.com", time.Now().UnixNano()),
		PasswordHash:  "$2a$12$test.test.test.test.test.test.test.test.test.test.test.test",
		EmailVerified: true,
	}
	if err := suite.userRepo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer suite.cleanup(userID)

	jwtToken, _ := suite.jwtAuth.GenerateToken(user)

	// POST to a protected endpoint WITHOUT csrf_token cookie or header
	client := &http.Client{Timeout: 10 * time.Second}
	resp := suite.doRequest(client, "POST", "/api/v1/verification/postcard/request", map[string]string{
		"address": "123 Test St",
	}, jwtToken)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 without CSRF token, got %d", resp.StatusCode)
	}

	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "csrf_validation_failed" {
		t.Errorf("expected csrf_validation_failed error, got %q", body["error"])
	}
}

func TestCSRFIntegration_ProtectedEndpointBlocksMismatchedTokens(t *testing.T) {
	suite := SetupCSRFTest(t)

	ctx := context.Background()
	userID := fmt.Sprintf("csrf-mm-%d", time.Now().UnixNano()%100000000)
	user := &models.User{
		ID:            userID,
		Username:      fmt.Sprintf("csrfmismatch%d", time.Now().UnixNano()),
		Email:         fmt.Sprintf("csrfmismatch%d@test.com", time.Now().UnixNano()),
		PasswordHash:  "$2a$12$test.test.test.test.test.test.test.test.test.test.test.test",
		EmailVerified: true,
	}
	if err := suite.userRepo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer suite.cleanup(userID)

	jwtToken, _ := suite.jwtAuth.GenerateToken(user)

	// Get a CSRF token via GET
	client := suite.newCookieClient()
	resp1 := suite.doRequest(client, "GET", "/api/v1/health", nil, "")
	_ = resp1.Body.Close()

	csrfToken := suite.getCSRFTokenFromJar(client)
	if csrfToken == "" {
		t.Fatal("expected csrf_token cookie after GET")
	}

	// POST with a DIFFERENT token in the header than what's in the cookie
	bodyBytes, _ := json.Marshal(map[string]string{"address": "123 Test St"})
	req, _ := http.NewRequest("POST", suite.server.URL+"/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("X-CSRF-Token", "completely-wrong-token-value")

	resp2, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 with mismatched CSRF tokens, got %d", resp2.StatusCode)
	}
}

func TestCSRFIntegration_ProtectedEndpointBlocksMissingHeader(t *testing.T) {
	suite := SetupCSRFTest(t)

	ctx := context.Background()
	userID := fmt.Sprintf("csrf-nh-%d", time.Now().UnixNano()%100000000)
	user := &models.User{
		ID:            userID,
		Username:      fmt.Sprintf("csrfnoheader%d", time.Now().UnixNano()),
		Email:         fmt.Sprintf("csrfnoheader%d@test.com", time.Now().UnixNano()),
		PasswordHash:  "$2a$12$test.test.test.test.test.test.test.test.test.test.test.test",
		EmailVerified: true,
	}
	if err := suite.userRepo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer suite.cleanup(userID)

	jwtToken, _ := suite.jwtAuth.GenerateToken(user)

	// Get CSRF cookie via GET
	client := suite.newCookieClient()
	resp1 := suite.doRequest(client, "GET", "/api/v1/health", nil, "")
	_ = resp1.Body.Close()

	// POST with cookie (auto-sent by jar) but WITHOUT X-CSRF-Token header
	resp2 := suite.doRequest(client, "POST", "/api/v1/verification/postcard/request", map[string]string{
		"address": "123 Test St",
	}, jwtToken)
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 without CSRF header, got %d", resp2.StatusCode)
	}
}

func TestCSRFIntegration_ProtectedEndpointPassesWithValidToken(t *testing.T) {
	suite := SetupCSRFTest(t)

	ctx := context.Background()
	userID := fmt.Sprintf("csrf-v-%d", time.Now().UnixNano()%100000000)
	user := &models.User{
		ID:            userID,
		Username:      fmt.Sprintf("csrfvalid%d", time.Now().UnixNano()),
		Email:         fmt.Sprintf("csrfvalid%d@test.com", time.Now().UnixNano()),
		PasswordHash:  "$2a$12$test.test.test.test.test.test.test.test.test.test.test.test",
		EmailVerified: true,
	}
	if err := suite.userRepo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer suite.cleanup(userID)

	jwtToken, _ := suite.jwtAuth.GenerateToken(user)

	// GET to receive CSRF cookie
	client := suite.newCookieClient()
	resp1 := suite.doRequest(client, "GET", "/api/v1/health", nil, "")
	_ = resp1.Body.Close()

	// POST with valid CSRF (cookie + matching header)
	resp2 := suite.doRequestWithCSRF(client, "POST", "/api/v1/verification/postcard/request", map[string]string{
		"address": "123 Test St",
	}, jwtToken)
	defer func() { _ = resp2.Body.Close() }()

	// Should NOT be 403 CSRF — may be 400/401/etc for business logic, but not CSRF rejection
	if resp2.StatusCode == http.StatusForbidden {
		var body map[string]string
		_ = json.NewDecoder(resp2.Body).Decode(&body)
		if body["error"] == "csrf_validation_failed" {
			t.Error("valid CSRF token should not be rejected")
		}
	}
}

func TestCSRFIntegration_AllUnsafeMethodsRequireToken(t *testing.T) {
	suite := SetupCSRFTest(t)

	ctx := context.Background()
	userID := fmt.Sprintf("csrf-m-%d", time.Now().UnixNano()%100000000)
	user := &models.User{
		ID:            userID,
		Username:      fmt.Sprintf("csrfmethods%d", time.Now().UnixNano()),
		Email:         fmt.Sprintf("csrfmethods%d@test.com", time.Now().UnixNano()),
		PasswordHash:  "$2a$12$test.test.test.test.test.test.test.test.test.test.test.test",
		EmailVerified: true,
	}
	if err := suite.userRepo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer suite.cleanup(userID)

	jwtToken, _ := suite.jwtAuth.GenerateToken(user)

	unsafeMethods := []struct {
		method string
		path   string
	}{
		{"POST", "/api/v1/verification/postcard/request"},
		{"PUT", "/api/v1/communities/fake-id"},
		{"DELETE", "/api/v1/users/me"},
	}

	for _, tc := range unsafeMethods {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			client := &http.Client{Timeout: 10 * time.Second}
			resp := suite.doRequest(client, tc.method, tc.path, nil, jwtToken)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s %s without CSRF: expected 403, got %d", tc.method, tc.path, resp.StatusCode)
				return
			}

			var body map[string]string
			_ = json.NewDecoder(resp.Body).Decode(&body)
			if body["error"] != "csrf_validation_failed" {
				t.Errorf("expected csrf_validation_failed, got %q", body["error"])
			}
		})
	}
}

func TestCSRFIntegration_AllUnsafeMethodsPassWithValidToken(t *testing.T) {
	suite := SetupCSRFTest(t)

	ctx := context.Background()
	userID := fmt.Sprintf("csrf-pa-%d", time.Now().UnixNano()%100000000)
	user := &models.User{
		ID:            userID,
		Username:      fmt.Sprintf("csrfpassall%d", time.Now().UnixNano()),
		Email:         fmt.Sprintf("csrfpassall%d@test.com", time.Now().UnixNano()),
		PasswordHash:  "$2a$12$test.test.test.test.test.test.test.test.test.test.test.test",
		EmailVerified: true,
	}
	if err := suite.userRepo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer suite.cleanup(userID)

	jwtToken, _ := suite.jwtAuth.GenerateToken(user)

	unsafeMethods := []struct {
		method string
		path   string
	}{
		{"POST", "/api/v1/verification/postcard/request"},
		{"PUT", "/api/v1/communities/fake-id"},
		{"DELETE", "/api/v1/users/me"},
	}

	for _, tc := range unsafeMethods {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			client := suite.newCookieClient()

			// GET to set CSRF cookie
			resp1 := suite.doRequest(client, "GET", "/api/v1/health", nil, "")
			_ = resp1.Body.Close()

			// Request with valid CSRF
			resp2 := suite.doRequestWithCSRF(client, tc.method, tc.path, nil, jwtToken)
			defer func() { _ = resp2.Body.Close() }()

			// Should not be CSRF 403
			if resp2.StatusCode == http.StatusForbidden {
				var body map[string]string
				_ = json.NewDecoder(resp2.Body).Decode(&body)
				if body["error"] == "csrf_validation_failed" {
					t.Errorf("%s %s with valid CSRF should not return CSRF 403", tc.method, tc.path)
				}
			}
		})
	}
}

func TestCSRFIntegration_SafeMethodsAlwaysPass(t *testing.T) {
	suite := SetupCSRFTest(t)

	safeMethods := []string{"GET", "HEAD", "OPTIONS"}

	for _, method := range safeMethods {
		t.Run(method, func(t *testing.T) {
			client := &http.Client{Timeout: 10 * time.Second}
			resp := suite.doRequest(client, method, "/api/v1/health", nil, "")
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode == http.StatusForbidden {
				var body map[string]string
				_ = json.NewDecoder(resp.Body).Decode(&body)
				if body["error"] == "csrf_validation_failed" {
					t.Errorf("safe method %s should not trigger CSRF validation", method)
				}
			}
		})
	}
}

func TestCSRFIntegration_OptionsPreflightBypassesCSRF(t *testing.T) {
	suite := SetupCSRFTest(t)

	// OPTIONS is used for CORS preflight — must pass without CSRF
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("OPTIONS", suite.server.URL+"/api/v1/verification/postcard/request", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "X-CSRF-Token")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusForbidden {
		t.Error("OPTIONS preflight should not be blocked by CSRF")
	}

	// Verify CORS headers allow X-CSRF-Token
	allowedHeaders := resp.Header.Get("Access-Control-Allow-Headers")
	if allowedHeaders == "" {
		t.Error("expected Access-Control-Allow-Headers in response")
	}
}

func TestCSRFIntegration_HealthCheckWorksWithoutCSRF(t *testing.T) {
	suite := SetupCSRFTest(t)

	client := &http.Client{Timeout: 10 * time.Second}
	resp := suite.doRequest(client, "GET", "/api/v1/health", nil, "")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health check should return 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// E2E Tests — full multi-step workflows with CSRF protection
// ---------------------------------------------------------------------------

func TestCSRF_E2E_RegisterLoginAndAuthenticatedAction(t *testing.T) {
	suite := SetupCSRFTest(t)

	client := suite.newCookieClient()
	username := fmt.Sprintf("csrfe2e%d", time.Now().UnixNano())
	email := fmt.Sprintf("csrfe2e%d@test.com", time.Now().UnixNano())
	password := "securepassword123"

	// Step 1: Register (exempt — no CSRF needed)
	resp1 := suite.doRequest(client, "POST", "/api/v1/auth/register", map[string]string{
		"username": username,
		"email":    email,
		"password": password,
	}, "")
	defer func() { _ = resp1.Body.Close() }()

	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp1.Body).Decode(&registerResp)
	userID := registerResp.UserID
	if userID == "" {
		var id string
		_ = suite.db.QueryRowContext(context.Background(), "SELECT id FROM users WHERE email = ?", email).Scan(&id)
		userID = id
	}
	defer suite.cleanup(userID)

	// Disable MFA and verify email for test
	_, _ = suite.db.ExecContext(context.Background(),
		"UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, email_verified = TRUE WHERE id = ?", userID)

	// Step 2: Login (exempt — no CSRF needed)
	resp2 := suite.doRequest(client, "POST", "/api/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, "")
	defer func() { _ = resp2.Body.Close() }()

	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp2.Body).Decode(&loginResp)
	jwtToken := loginResp.Token
	if jwtToken == "" {
		t.Fatal("expected JWT token after login")
	}

	// Step 3: GET to acquire CSRF cookie (simulating page load)
	resp3 := suite.doRequest(client, "GET", "/api/v1/users/me", nil, jwtToken)
	_ = resp3.Body.Close()

	csrfToken := suite.getCSRFTokenFromJar(client)
	if csrfToken == "" {
		t.Fatal("expected csrf_token cookie after authenticated GET")
	}

	// Step 4: Authenticated POST WITHOUT CSRF header — should fail
	resp4 := suite.doRequest(client, "POST", "/api/v1/verification/vouch/request", map[string]interface{}{
		"address": map[string]string{
			"line1":       "123 Main St",
			"city":        "Test City",
			"state":       "TS",
			"postal_code": "12345",
		},
	}, jwtToken)
	defer func() { _ = resp4.Body.Close() }()

	if resp4.StatusCode != http.StatusForbidden {
		t.Errorf("POST without CSRF header: expected 403, got %d", resp4.StatusCode)
	}

	// Step 5: Authenticated POST WITH valid CSRF header — should pass CSRF check
	resp5 := suite.doRequestWithCSRF(client, "POST", "/api/v1/verification/vouch/request", map[string]interface{}{
		"address": map[string]string{
			"line1":       "123 Main St",
			"city":        "Test City",
			"state":       "TS",
			"postal_code": "12345",
		},
	}, jwtToken)
	defer func() { _ = resp5.Body.Close() }()

	if resp5.StatusCode == http.StatusForbidden {
		var body map[string]string
		_ = json.NewDecoder(resp5.Body).Decode(&body)
		if body["error"] == "csrf_validation_failed" {
			t.Error("POST with valid CSRF should not be rejected")
		}
	}
}

func TestCSRF_E2E_MultipleUsersIndependentTokens(t *testing.T) {
	suite := SetupCSRFTest(t)

	// Two users should get different CSRF tokens
	client1 := suite.newCookieClient()
	client2 := suite.newCookieClient()

	resp1 := suite.doRequest(client1, "GET", "/api/v1/health", nil, "")
	_ = resp1.Body.Close()

	resp2 := suite.doRequest(client2, "GET", "/api/v1/health", nil, "")
	_ = resp2.Body.Close()

	token1 := suite.getCSRFTokenFromJar(client1)
	token2 := suite.getCSRFTokenFromJar(client2)

	if token1 == "" || token2 == "" {
		t.Fatal("both clients should have csrf_token cookies")
	}

	if token1 == token2 {
		t.Error("different clients should receive different CSRF tokens")
	}
}

func TestCSRF_E2E_TokenFromOneClientCannotBeUsedByAnother(t *testing.T) {
	suite := SetupCSRFTest(t)

	ctx := context.Background()
	userID := fmt.Sprintf("csrf-cc-%d", time.Now().UnixNano()%100000000)
	user := &models.User{
		ID:            userID,
		Username:      fmt.Sprintf("csrfcross%d", time.Now().UnixNano()),
		Email:         fmt.Sprintf("csrfcross%d@test.com", time.Now().UnixNano()),
		PasswordHash:  "$2a$12$test.test.test.test.test.test.test.test.test.test.test.test",
		EmailVerified: true,
	}
	if err := suite.userRepo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer suite.cleanup(userID)

	jwtToken, _ := suite.jwtAuth.GenerateToken(user)

	// Client A gets a CSRF token
	clientA := suite.newCookieClient()
	resp1 := suite.doRequest(clientA, "GET", "/api/v1/health", nil, "")
	_ = resp1.Body.Close()
	tokenA := suite.getCSRFTokenFromJar(clientA)

	// Client B gets a different CSRF token
	clientB := suite.newCookieClient()
	resp2 := suite.doRequest(clientB, "GET", "/api/v1/health", nil, "")
	_ = resp2.Body.Close()

	// Client B tries to use Client A's token in the header (but has its own cookie)
	bodyBytes, _ := json.Marshal(map[string]string{"address": "123 Test St"})
	req, _ := http.NewRequest("POST", suite.server.URL+"/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("X-CSRF-Token", tokenA) // Using A's token as header, but B's cookie is sent

	resp3, err := clientB.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp3.Body.Close() }()

	if resp3.StatusCode != http.StatusForbidden {
		t.Errorf("cross-client CSRF token should be rejected: expected 403, got %d", resp3.StatusCode)
	}
}

func TestCSRF_E2E_TokenReusableAcrossMultipleRequests(t *testing.T) {
	suite := SetupCSRFTest(t)

	ctx := context.Background()
	userID := fmt.Sprintf("csrf-ru-%d", time.Now().UnixNano()%100000000)
	user := &models.User{
		ID:            userID,
		Username:      fmt.Sprintf("csrfreuse%d", time.Now().UnixNano()),
		Email:         fmt.Sprintf("csrfreuse%d@test.com", time.Now().UnixNano()),
		PasswordHash:  "$2a$12$test.test.test.test.test.test.test.test.test.test.test.test",
		EmailVerified: true,
	}
	if err := suite.userRepo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer suite.cleanup(userID)

	jwtToken, _ := suite.jwtAuth.GenerateToken(user)

	client := suite.newCookieClient()

	// GET to acquire CSRF token
	resp1 := suite.doRequest(client, "GET", "/api/v1/health", nil, "")
	_ = resp1.Body.Close()

	// Same token should work on multiple POST requests
	for i := 0; i < 3; i++ {
		resp := suite.doRequestWithCSRF(client, "POST", "/api/v1/verification/postcard/request", map[string]string{
			"address": fmt.Sprintf("123 Test St #%d", i),
		}, jwtToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode == http.StatusForbidden {
			var body map[string]string
			_ = json.NewDecoder(resp.Body).Decode(&body)
			if body["error"] == "csrf_validation_failed" {
				t.Errorf("request %d: CSRF token should be reusable", i+1)
			}
		}
	}
}

func TestCSRF_E2E_ChangePasswordExemptAsAuthEndpoint(t *testing.T) {
	suite := SetupCSRFTest(t)

	client := suite.newCookieClient()
	username := fmt.Sprintf("csrfchgpw%d", time.Now().UnixNano())
	email := fmt.Sprintf("csrfchgpw%d@test.com", time.Now().UnixNano())
	password := "securepassword123"

	userID, jwtToken := suite.registerAndLogin(client, username, email, password)
	defer suite.cleanup(userID)

	if jwtToken == "" {
		t.Fatal("expected JWT token after login")
	}

	// Change password WITHOUT CSRF — should succeed (exempt under /api/v1/auth/ prefix)
	resp := suite.doRequest(client, "POST", "/api/v1/auth/change-password", map[string]string{
		"current_password": password,
		"new_password":     "newsecurepassword456",
	}, jwtToken)
	defer func() { _ = resp.Body.Close() }()

	// /api/v1/auth/change-password is under the /api/v1/auth/ exempt prefix,
	// so it must NOT be blocked by CSRF validation.
	if resp.StatusCode == http.StatusForbidden {
		var body map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body["error"] == "csrf_validation_failed" {
			t.Error("/api/v1/auth/change-password should be exempt from CSRF (under /api/v1/auth/ prefix)")
		}
	}
}

func TestCSRF_E2E_DeleteAccountRequiresCSRF(t *testing.T) {
	suite := SetupCSRFTest(t)

	ctx := context.Background()
	userID := fmt.Sprintf("csrf-dl-%d", time.Now().UnixNano()%100000000)
	user := &models.User{
		ID:            userID,
		Username:      fmt.Sprintf("csrfdel%d", time.Now().UnixNano()),
		Email:         fmt.Sprintf("csrfdel%d@test.com", time.Now().UnixNano()),
		PasswordHash:  "$2a$12$test.test.test.test.test.test.test.test.test.test.test.test",
		EmailVerified: true,
	}
	if err := suite.userRepo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer suite.cleanup(userID)

	jwtToken, _ := suite.jwtAuth.GenerateToken(user)

	// DELETE /api/v1/users/me without CSRF — should fail
	client := &http.Client{Timeout: 10 * time.Second}
	resp := suite.doRequest(client, "DELETE", "/api/v1/users/me", nil, jwtToken)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("DELETE without CSRF: expected 403, got %d", resp.StatusCode)
	}

	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "csrf_validation_failed" {
		t.Errorf("expected csrf_validation_failed, got %q", body["error"])
	}

	// Now with valid CSRF — should pass CSRF check
	clientWithJar := suite.newCookieClient()
	resp2 := suite.doRequest(clientWithJar, "GET", "/api/v1/health", nil, "")
	_ = resp2.Body.Close()

	resp3 := suite.doRequestWithCSRF(clientWithJar, "DELETE", "/api/v1/users/me", nil, jwtToken)
	defer func() { _ = resp3.Body.Close() }()

	// Should not be CSRF 403
	if resp3.StatusCode == http.StatusForbidden {
		var body2 map[string]string
		_ = json.NewDecoder(resp3.Body).Decode(&body2)
		if body2["error"] == "csrf_validation_failed" {
			t.Error("DELETE with valid CSRF should not be rejected by CSRF middleware")
		}
	}
}

func TestCSRF_E2E_CORSPreflightAllowsCSRFHeader(t *testing.T) {
	suite := SetupCSRFTest(t)

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("OPTIONS", suite.server.URL+"/api/v1/communities", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization, X-CSRF-Token")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("CORS preflight: expected 200, got %d", resp.StatusCode)
	}

	allowHeaders := resp.Header.Get("Access-Control-Allow-Headers")
	if allowHeaders == "" {
		t.Error("missing Access-Control-Allow-Headers response header")
	}
}

func TestCSRF_E2E_LogoutExemptFromCSRF(t *testing.T) {
	suite := SetupCSRFTest(t)

	// POST to /api/v1/auth/logout without CSRF — should be exempt
	client := &http.Client{Timeout: 10 * time.Second}
	resp := suite.doRequest(client, "POST", "/api/v1/auth/logout", nil, "")
	defer func() { _ = resp.Body.Close() }()

	// Should NOT get CSRF 403
	if resp.StatusCode == http.StatusForbidden {
		var body map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body["error"] == "csrf_validation_failed" {
			t.Error("logout should be exempt from CSRF")
		}
	}
}

func TestCSRF_E2E_CSRFTokenSetOnExemptPOST(t *testing.T) {
	suite := SetupCSRFTest(t)

	// Even exempt POSTs should set the CSRF cookie for subsequent use
	client := suite.newCookieClient()
	resp := suite.doRequest(client, "POST", "/api/v1/auth/login", map[string]string{
		"email":    "nonexistent@example.com",
		"password": "password12345",
	}, "")
	defer func() { _ = resp.Body.Close() }()

	csrfToken := suite.getCSRFTokenFromJar(client)
	if csrfToken == "" {
		t.Error("expected csrf_token cookie to be set even on exempt POST endpoints")
	}
}

func TestCSRFIntegration_DeletionPreflightGETPassesWithoutCSRF(t *testing.T) {
	suite := SetupCSRFTest(t)

	ctx := context.Background()
	userID := fmt.Sprintf("csrf-pf-%d", time.Now().UnixNano()%100000000)
	user := &models.User{
		ID:            userID,
		Username:      fmt.Sprintf("csrfpre%d", time.Now().UnixNano()),
		Email:         fmt.Sprintf("csrfpre%d@test.com", time.Now().UnixNano()),
		PasswordHash:  "$2a$12$test.test.test.test.test.test.test.test.test.test.test.test",
		EmailVerified: true,
	}
	if err := suite.userRepo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer suite.cleanup(userID)

	jwtToken, _ := suite.jwtAuth.GenerateToken(user)

	// GET endpoints should never be blocked by CSRF
	client := &http.Client{Timeout: 10 * time.Second}
	resp := suite.doRequest(client, "GET", "/api/v1/users/me/deletion-preflight", nil, jwtToken)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusForbidden {
		var body map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body["error"] == "csrf_validation_failed" {
			t.Error("GET request should not be blocked by CSRF")
		}
	}
}
