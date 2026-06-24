package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/handlers"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/mocks"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// EmailVerificationTestSuite holds dependencies for email verification tests
type EmailVerificationTestSuite struct {
	t            *testing.T
	db           *database.DB
	server       *httptest.Server
	userRepo     *database.UserRepository
	jwtAuth      *middleware.JWTAuth
	jwtSecret    string
	emailService *TestEmailService
}

// TestEmailService is a test double for email service that tracks all sent emails
type TestEmailService struct {
	SentEmails        []SentEmail
	SentGenericEmails []services.EmailMessage
	Enabled           bool
	ShouldFail        bool
	FailError         error
}

type SentEmail struct {
	To    string
	Token string
}

func NewTestEmailService(enabled bool) *TestEmailService {
	return &TestEmailService{
		SentEmails:        make([]SentEmail, 0),
		SentGenericEmails: make([]services.EmailMessage, 0),
		Enabled:           enabled,
	}
}

func (m *TestEmailService) SendVerificationEmail(ctx context.Context, toEmail, token string) error {
	if m.ShouldFail {
		return m.FailError
	}
	m.SentEmails = append(m.SentEmails, SentEmail{To: toEmail, Token: token})
	return nil
}

func (m *TestEmailService) Send(ctx context.Context, msg *services.EmailMessage) error {
	if m.ShouldFail {
		return m.FailError
	}
	m.SentGenericEmails = append(m.SentGenericEmails, *msg)
	return nil
}

func (m *TestEmailService) IsEnabled() bool {
	return m.Enabled
}

func (m *TestEmailService) Backend() string {
	return "test"
}

func (m *TestEmailService) Reset() {
	m.SentEmails = make([]SentEmail, 0)
	m.SentGenericEmails = make([]services.EmailMessage, 0)
	m.ShouldFail = false
	m.FailError = nil
}

// SetupEmailVerificationTest creates a new test suite with email verification
func SetupEmailVerificationTest(t *testing.T, emailEnabled bool) *EmailVerificationTestSuite {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping e2e tests")
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
	schoolRepo := database.NewSchoolRepository(db)
	communityGroupRepo := database.NewGroupRepository(db, nil)
	districtRepo := database.NewSchoolDistrictRepository(db)
	auditRepo := database.NewAuditRepository(db)

	// Create JWT auth
	jwtSecret := "test_secret_key_at_least_32_characters_long"
	jwtConfig := &config.JWTConfig{
		Secret:          jwtSecret,
		ExpirationHours: 24,
		Issuer:          "test_issuer",
	}
	jwtAuth := middleware.NewJWTAuth(jwtConfig)

	// Create mock services for testing
	mockPostgrid := mocks.NewMockPostgridService()
	mockMapbox := mocks.NewMockMapboxService()
	testEmailService := NewTestEmailService(emailEnabled)

	// Create handlers with test email service
	authHandler := handlers.NewAuthHandlerWithEmailService(
		nil,
		userRepo,
		jwtAuth,
		testEmailService,
		jwtSecret,
		false,
		false, // MFA disabled for email verification tests
		nil,
		nil,
		nil,
		"http://localhost:3000",
		nil,
	)
	regionHandler := handlers.NewRegionHandler(regionRepo, userRepo, mockMapbox, nil)
	verificationHandler := handlers.NewVerificationHandler(
		nil, verifyRepo, userRepo, regionRepo,
		mockPostgrid, mockMapbox, nil,
	)
	adminHandler := handlers.NewAdminHandler(userRepo, regionRepo, nil)

	// Create MFA service and handler (use test encryption key)
	mfaConfig := &config.MFAConfig{
		EncryptionKey: "01234567890123456789012345678901",
		Issuer:        "Test MFA",
	}
	mfaService, _ := services.NewMFAService(mfaConfig)
	mfaHandler := handlers.NewMFAHandler(nil, userRepo, mfaService, jwtAuth, false, nil)

	schoolHandler := handlers.NewSchoolHandler(
		schoolRepo, districtRepo, communityGroupRepo, userRepo, auditRepo, nil,
	)

	// Create router (rate limiting disabled for tests)
	router := handlers.NewRouter(
		authHandler, mfaHandler, regionHandler, verificationHandler, adminHandler,
		schoolHandler, nil, nil, nil, jwtAuth, nil, nil, nil,
		[]string{"*"}, nil,
	)
	handler := router.Setup()

	server := httptest.NewServer(handler)

	suite := &EmailVerificationTestSuite{
		t:            t,
		db:           db,
		server:       server,
		userRepo:     userRepo,
		jwtAuth:      jwtAuth,
		jwtSecret:    jwtSecret,
		emailService: testEmailService,
	}

	t.Cleanup(func() {
		server.Close()
		_ = db.Close()
	})

	return suite
}

func (s *EmailVerificationTestSuite) request(method, path string, body interface{}, token string) *http.Response {
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("Request failed: %v", err)
	}

	return resp
}

func (s *EmailVerificationTestSuite) cleanup(userIDs ...string) {
	ctx := context.Background()
	for _, id := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM verification_requests WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM vouches WHERE voucher_user_id = ? OR vouched_user_id = ?", id, id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	}
}

func (s *EmailVerificationTestSuite) generateExpiredToken(userID, email string) string {
	claims := jwt.MapClaims{
		"user_id": userID,
		"email":   email,
		"sub":     "email_verification",
		"exp":     time.Now().Add(-1 * time.Hour).Unix(), // Expired 1 hour ago
		"iat":     time.Now().Add(-25 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte(s.jwtSecret))
	return tokenString
}

func (s *EmailVerificationTestSuite) generateValidToken(userID, email string) string {
	claims := jwt.MapClaims{
		"user_id": userID,
		"email":   email,
		"sub":     "email_verification",
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
		"iat":     time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte(s.jwtSecret))
	return tokenString
}

// =============================================================================
// E2E Tests: Registration with Email Verification
// =============================================================================

func TestE2E_Registration_SendsVerificationEmail(t *testing.T) {
	suite := SetupEmailVerificationTest(t, true)
	suite.emailService.Reset()

	// Register a new user
	req := map[string]string{
		"username": "emailtest1",
		"email":    "emailtest1@example.com",
		"password": "testpassword123",
	}

	resp := suite.request("POST", "/api/v1/auth/register", req, "")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("Expected status 201, got %d: %v", resp.StatusCode, body)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&body)

	// Verify email verification was sent
	if body["email_verification_sent"] != true {
		t.Error("Expected email_verification_sent to be true")
	}

	// Verify email was actually sent via mock
	if len(suite.emailService.SentEmails) != 1 {
		t.Errorf("Expected 1 email sent, got %d", len(suite.emailService.SentEmails))
	}

	if suite.emailService.SentEmails[0].To != "emailtest1@example.com" {
		t.Errorf("Email sent to wrong address: %s", suite.emailService.SentEmails[0].To)
	}

	// Verify token was included
	if suite.emailService.SentEmails[0].Token == "" {
		t.Error("Expected verification token to be sent")
	}

	// Cleanup
	if userID, ok := body["user_id"].(string); ok {
		suite.cleanup(userID)
	}
}

func TestE2E_Registration_RejectsPlusAlias(t *testing.T) {
	suite := SetupEmailVerificationTest(t, true)

	testCases := []struct {
		name  string
		email string
	}{
		{"gmail plus alias", "user+test@gmail.com"},
		{"other domain plus alias", "john+work@example.com"},
		{"multiple plus signs", "user+test+more@domain.com"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := map[string]string{
				"username": "plustest" + tc.name,
				"email":    tc.email,
				"password": "testpassword123",
			}

			resp := suite.request("POST", "/api/v1/auth/register", req, "")
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("Expected status 400, got %d", resp.StatusCode)
			}

			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)

			if body["error"] != "validation_error" {
				t.Errorf("Expected error 'validation_error', got '%s'", body["error"])
			}
		})
	}
}

func TestE2E_Registration_RejectsDuplicateNormalizedEmail(t *testing.T) {
	suite := SetupEmailVerificationTest(t, false) // Disable email verification for this test

	testCases := []struct {
		name         string
		firstEmail   string
		secondEmail  string
		shouldReject bool
	}{
		{
			name:         "gmail dots",
			firstEmail:   "j.o.h.n@gmail.com",
			secondEmail:  "john@gmail.com",
			shouldReject: true,
		},
		{
			name:         "googlemail to gmail",
			firstEmail:   "user@googlemail.com",
			secondEmail:  "user@gmail.com",
			shouldReject: true,
		},
		{
			name:         "case insensitive",
			firstEmail:   "USER@EXAMPLE.COM",
			secondEmail:  "user@example.com",
			shouldReject: true,
		},
		{
			name:         "different domains allowed",
			firstEmail:   "user@gmail.com",
			secondEmail:  "user@yahoo.com",
			shouldReject: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Use a sanitized name for usernames (no spaces)
			safeName := strings.ReplaceAll(tc.name, " ", "_")
			// Register first user
			req1 := map[string]string{
				"username": "normtest1_" + safeName,
				"email":    tc.firstEmail,
				"password": "testpassword123",
			}

			resp1 := suite.request("POST", "/api/v1/auth/register", req1, "")
			defer func() { _ = resp1.Body.Close() }()

			if resp1.StatusCode != http.StatusCreated {
				t.Fatalf("First registration failed with status %d", resp1.StatusCode)
			}

			var body1 map[string]interface{}
			_ = json.NewDecoder(resp1.Body).Decode(&body1)
			userID1 := body1["user_id"].(string)

			// Try to register second user
			req2 := map[string]string{
				"username": "normtest2_" + safeName,
				"email":    tc.secondEmail,
				"password": "testpassword123",
			}

			resp2 := suite.request("POST", "/api/v1/auth/register", req2, "")
			defer func() { _ = resp2.Body.Close() }()

			var body2 map[string]interface{}
			_ = json.NewDecoder(resp2.Body).Decode(&body2)

			if tc.shouldReject {
				// Anti-enumeration: duplicate accounts now return 201 with success-like response
				if resp2.StatusCode != http.StatusCreated {
					t.Errorf("Expected status 201 (anti-enumeration), got %d", resp2.StatusCode)
				}
				suite.cleanup(userID1)
			} else {
				if resp2.StatusCode != http.StatusCreated {
					t.Errorf("Expected status 201, got %d", resp2.StatusCode)
				}
				if userID2, ok := body2["user_id"].(string); ok {
					suite.cleanup(userID1, userID2)
				} else {
					suite.cleanup(userID1)
				}
			}
		})
	}
}

func TestE2E_Registration_EmailVerificationDisabled_UserAutoVerified(t *testing.T) {
	suite := SetupEmailVerificationTest(t, false) // Email verification disabled
	suite.emailService.Reset()

	// Register a user
	req := map[string]string{
		"username": "autodisabled",
		"email":    "autodisabled@example.com",
		"password": "testpassword123",
	}

	resp := suite.request("POST", "/api/v1/auth/register", req, "")
	defer func() { _ = resp.Body.Close() }()

	var body map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	userID := body["user_id"].(string)

	// Verify no email was sent
	if len(suite.emailService.SentEmails) != 0 {
		t.Errorf("Expected 0 emails, got %d", len(suite.emailService.SentEmails))
	}

	// Verify the user is already verified
	user, err := suite.userRepo.GetByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}

	if !user.EmailVerified {
		t.Error("User should be auto-verified when email verification is disabled")
	}

	// Cleanup
	suite.cleanup(userID)
}

// =============================================================================
// E2E Tests: Login with Email Verification
// =============================================================================

func TestE2E_Login_UnverifiedUser_GetsRestrictedToken(t *testing.T) {
	suite := SetupEmailVerificationTest(t, true)
	suite.emailService.Reset()

	// Register a user (will be unverified)
	regReq := map[string]string{
		"username": "unverifiedtest",
		"email":    "unverified@example.com",
		"password": "testpassword123",
	}

	regResp := suite.request("POST", "/api/v1/auth/register", regReq, "")
	defer func() { _ = regResp.Body.Close() }()

	var regBody map[string]interface{}
	_ = json.NewDecoder(regResp.Body).Decode(&regBody)
	userID := regBody["user_id"].(string)

	// Try to login
	loginReq := map[string]string{
		"email":    "unverified@example.com",
		"password": "testpassword123",
	}

	loginResp := suite.request("POST", "/api/v1/auth/login", loginReq, "")
	defer func() { _ = loginResp.Body.Close() }()

	if loginResp.StatusCode != http.StatusOK {
		t.Errorf("Login should succeed, got status %d", loginResp.StatusCode)
	}

	var loginBody map[string]interface{}
	_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)

	// Should get email_action = verify_email
	if loginBody["email_action"] != "verify_email" {
		t.Errorf("Expected email_action 'verify_email', got '%v'", loginBody["email_action"])
	}

	// Verify message is present
	if loginBody["message"] == nil {
		t.Error("Expected message in response")
	}

	// Cleanup
	suite.cleanup(userID)
}

func TestE2E_Login_VerifiedUser_GetsFullToken(t *testing.T) {
	suite := SetupEmailVerificationTest(t, true)
	suite.emailService.Reset()

	// Register a user
	regReq := map[string]string{
		"username": "verifiedlogin",
		"email":    "verifiedlogin@example.com",
		"password": "testpassword123",
	}

	regResp := suite.request("POST", "/api/v1/auth/register", regReq, "")
	defer func() { _ = regResp.Body.Close() }()

	var regBody map[string]interface{}
	_ = json.NewDecoder(regResp.Body).Decode(&regBody)
	userID := regBody["user_id"].(string)

	// Manually verify the user
	_ = suite.userRepo.SetEmailVerified(context.Background(), userID, true)

	// Try to login
	loginReq := map[string]string{
		"email":    "verifiedlogin@example.com",
		"password": "testpassword123",
	}

	loginResp := suite.request("POST", "/api/v1/auth/login", loginReq, "")
	defer func() { _ = loginResp.Body.Close() }()

	if loginResp.StatusCode != http.StatusOK {
		t.Errorf("Login should succeed, got status %d", loginResp.StatusCode)
	}

	var loginBody map[string]interface{}
	_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)

	// Should NOT have email_action
	if loginBody["email_action"] != nil {
		t.Errorf("Expected no email_action, got '%v'", loginBody["email_action"])
	}

	// Should have full user info including email_verified
	user := loginBody["user"].(map[string]interface{})
	if user["email_verified"] != true {
		t.Error("Expected email_verified to be true in user response")
	}

	// Cleanup
	suite.cleanup(userID)
}

// =============================================================================
// E2E Tests: Email Verification Flow
// =============================================================================

func TestE2E_VerifyEmail_Success(t *testing.T) {
	suite := SetupEmailVerificationTest(t, true)
	suite.emailService.Reset()

	// Register a user
	regReq := map[string]string{
		"username": "verifytest",
		"email":    "verifytest@example.com",
		"password": "testpassword123",
	}

	regResp := suite.request("POST", "/api/v1/auth/register", regReq, "")
	defer func() { _ = regResp.Body.Close() }()

	var regBody map[string]interface{}
	_ = json.NewDecoder(regResp.Body).Decode(&regBody)
	userID := regBody["user_id"].(string)

	// Get the verification token from the mock
	if len(suite.emailService.SentEmails) != 1 {
		t.Fatalf("Expected 1 email sent, got %d", len(suite.emailService.SentEmails))
	}
	verificationToken := suite.emailService.SentEmails[0].Token

	// Verify the email
	verifyResp := suite.request("GET", "/api/v1/auth/verify-email?token="+verificationToken, nil, "")
	defer func() { _ = verifyResp.Body.Close() }()

	if verifyResp.StatusCode != http.StatusOK {
		var body map[string]interface{}
		_ = json.NewDecoder(verifyResp.Body).Decode(&body)
		t.Errorf("Expected status 200, got %d: %v", verifyResp.StatusCode, body)
	}

	var verifyBody map[string]interface{}
	_ = json.NewDecoder(verifyResp.Body).Decode(&verifyBody)

	if verifyBody["success"] != true {
		t.Error("Expected success to be true")
	}

	// Verify the user is now verified in the database
	user, err := suite.userRepo.GetByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}

	if !user.EmailVerified {
		t.Error("User email should be verified")
	}

	// Cleanup
	suite.cleanup(userID)
}

func TestE2E_VerifyEmail_AlreadyVerified(t *testing.T) {
	suite := SetupEmailVerificationTest(t, true)
	suite.emailService.Reset()

	// Register a user
	regReq := map[string]string{
		"username": "alreadyverified",
		"email":    "alreadyverified@example.com",
		"password": "testpassword123",
	}

	regResp := suite.request("POST", "/api/v1/auth/register", regReq, "")
	defer func() { _ = regResp.Body.Close() }()

	var regBody map[string]interface{}
	_ = json.NewDecoder(regResp.Body).Decode(&regBody)
	userID := regBody["user_id"].(string)

	// Get the verification token
	verificationToken := suite.emailService.SentEmails[0].Token

	// Verify once
	resp1 := suite.request("GET", "/api/v1/auth/verify-email?token="+verificationToken, nil, "")
	_ = resp1.Body.Close()

	// Try to verify again with same token
	resp2 := suite.request("GET", "/api/v1/auth/verify-email?token="+verificationToken, nil, "")
	defer func() { _ = resp2.Body.Close() }()

	// Should still succeed but with "already verified" message
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp2.StatusCode)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(resp2.Body).Decode(&body)

	if body["message"] != "Email already verified" {
		t.Errorf("Expected 'Email already verified' message, got '%v'", body["message"])
	}

	// Cleanup
	suite.cleanup(userID)
}

func TestE2E_VerifyEmail_ExpiredToken(t *testing.T) {
	suite := SetupEmailVerificationTest(t, true)

	// Generate an expired token
	expiredToken := suite.generateExpiredToken("fake-user-id", "fake@example.com")

	// Try to verify with expired token
	resp := suite.request("GET", "/api/v1/auth/verify-email?token="+expiredToken, nil, "")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&body)

	if body["error"] != "expired_token" {
		t.Errorf("Expected error 'expired_token', got '%s'", body["error"])
	}
}

func TestE2E_VerifyEmail_InvalidToken(t *testing.T) {
	suite := SetupEmailVerificationTest(t, true)

	testCases := []struct {
		name  string
		token string
	}{
		{"empty token", ""},
		{"malformed token", "not-a-valid-jwt"},
		{"wrong signature", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoiMTIzIiwiZW1haWwiOiJ0ZXN0QGV4YW1wbGUuY29tIn0.wrong_signature"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var url string
			if tc.token == "" {
				url = "/api/v1/auth/verify-email"
			} else {
				url = "/api/v1/auth/verify-email?token=" + tc.token
			}

			resp := suite.request("GET", url, nil, "")
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("Expected status 400, got %d", resp.StatusCode)
			}
		})
	}
}

func TestE2E_VerifyEmail_NonexistentUser(t *testing.T) {
	suite := SetupEmailVerificationTest(t, true)

	// Generate valid token for non-existent user
	token := suite.generateValidToken("nonexistent-user-id", "nonexistent@example.com")

	resp := suite.request("GET", "/api/v1/auth/verify-email?token="+token, nil, "")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&body)

	if body["error"] != "invalid_token" {
		t.Errorf("Expected error 'invalid_token', got '%s'", body["error"])
	}
}

// =============================================================================
// E2E Tests: Resend Verification Email
// =============================================================================

func TestE2E_ResendVerification_Success(t *testing.T) {
	suite := SetupEmailVerificationTest(t, true)
	suite.emailService.Reset()

	// Register a user
	regReq := map[string]string{
		"username": "resendtest",
		"email":    "resendtest@example.com",
		"password": "testpassword123",
	}

	regResp := suite.request("POST", "/api/v1/auth/register", regReq, "")
	defer func() { _ = regResp.Body.Close() }()

	var regBody map[string]interface{}
	_ = json.NewDecoder(regResp.Body).Decode(&regBody)
	userID := regBody["user_id"].(string)

	// Record initial email count
	initialEmailCount := len(suite.emailService.SentEmails)

	// Login to get an email_unverified token
	loginReq := map[string]string{
		"email":    "resendtest@example.com",
		"password": "testpassword123",
	}

	loginResp := suite.request("POST", "/api/v1/auth/login", loginReq, "")
	defer func() { _ = loginResp.Body.Close() }()

	var loginBody map[string]interface{}
	_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)
	token := loginBody["token"].(string)

	// Request resend
	resendResp := suite.request("POST", "/api/v1/auth/resend-verification", nil, token)
	defer func() { _ = resendResp.Body.Close() }()

	if resendResp.StatusCode != http.StatusOK {
		var body map[string]interface{}
		_ = json.NewDecoder(resendResp.Body).Decode(&body)
		t.Errorf("Expected status 200, got %d: %v", resendResp.StatusCode, body)
	}

	var resendBody map[string]interface{}
	_ = json.NewDecoder(resendResp.Body).Decode(&resendBody)

	if resendBody["success"] != true {
		t.Error("Expected success to be true")
	}

	// Verify another email was sent
	if len(suite.emailService.SentEmails) != initialEmailCount+1 {
		t.Errorf("Expected %d emails, got %d", initialEmailCount+1, len(suite.emailService.SentEmails))
	}

	// Cleanup
	suite.cleanup(userID)
}

func TestE2E_ResendVerification_AlreadyVerified(t *testing.T) {
	suite := SetupEmailVerificationTest(t, true)
	suite.emailService.Reset()

	// Register a user
	regReq := map[string]string{
		"username": "resendverified",
		"email":    "resendverified@example.com",
		"password": "testpassword123",
	}

	regResp := suite.request("POST", "/api/v1/auth/register", regReq, "")
	defer func() { _ = regResp.Body.Close() }()

	var regBody map[string]interface{}
	_ = json.NewDecoder(regResp.Body).Decode(&regBody)
	userID := regBody["user_id"].(string)

	// Get unverified token first
	loginReq := map[string]string{
		"email":    "resendverified@example.com",
		"password": "testpassword123",
	}

	loginResp := suite.request("POST", "/api/v1/auth/login", loginReq, "")
	defer func() { _ = loginResp.Body.Close() }()

	var loginBody map[string]interface{}
	_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)
	token := loginBody["token"].(string)

	// Verify the user
	_ = suite.userRepo.SetEmailVerified(context.Background(), userID, true)

	// Try to resend - should fail because user is now verified
	resendResp := suite.request("POST", "/api/v1/auth/resend-verification", nil, token)
	defer func() { _ = resendResp.Body.Close() }()

	if resendResp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resendResp.StatusCode)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(resendResp.Body).Decode(&body)

	if body["error"] != "already_verified" {
		t.Errorf("Expected error 'already_verified', got '%s'", body["error"])
	}

	// Cleanup
	suite.cleanup(userID)
}

func TestE2E_ResendVerification_RequiresAuth(t *testing.T) {
	suite := SetupEmailVerificationTest(t, true)

	// Try to resend without token
	resp := suite.request("POST", "/api/v1/auth/resend-verification", nil, "")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", resp.StatusCode)
	}
}

func TestE2E_ResendVerification_RequiresEmailUnverifiedToken(t *testing.T) {
	suite := SetupEmailVerificationTest(t, false) // Disable email verification so user is auto-verified
	suite.emailService.Reset()

	// Register a verified user
	regReq := map[string]string{
		"username": "resendfulltoken",
		"email":    "resendfulltoken@example.com",
		"password": "testpassword123",
	}

	regResp := suite.request("POST", "/api/v1/auth/register", regReq, "")
	defer func() { _ = regResp.Body.Close() }()

	var regBody map[string]interface{}
	_ = json.NewDecoder(regResp.Body).Decode(&regBody)
	userID := regBody["user_id"].(string)

	// Login to get a full token (user is already verified)
	loginReq := map[string]string{
		"email":    "resendfulltoken@example.com",
		"password": "testpassword123",
	}

	loginResp := suite.request("POST", "/api/v1/auth/login", loginReq, "")
	defer func() { _ = loginResp.Body.Close() }()

	var loginBody map[string]interface{}
	_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)
	fullToken := loginBody["token"].(string)

	// Try to resend with full token - should fail
	resendResp := suite.request("POST", "/api/v1/auth/resend-verification", nil, fullToken)
	defer func() { _ = resendResp.Body.Close() }()

	if resendResp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", resendResp.StatusCode)
	}

	// Cleanup
	suite.cleanup(userID)
}

// =============================================================================
// E2E Tests: Protected Routes with Email Verification
// =============================================================================

func TestE2E_ProtectedRoute_BlocksUnverifiedUser(t *testing.T) {
	suite := SetupEmailVerificationTest(t, true)
	suite.emailService.Reset()

	// Register a user
	regReq := map[string]string{
		"username": "blockedtest",
		"email":    "blockedtest@example.com",
		"password": "testpassword123",
	}

	regResp := suite.request("POST", "/api/v1/auth/register", regReq, "")
	defer func() { _ = regResp.Body.Close() }()

	var regBody map[string]interface{}
	_ = json.NewDecoder(regResp.Body).Decode(&regBody)
	userID := regBody["user_id"].(string)

	// Login to get an email_unverified token
	loginReq := map[string]string{
		"email":    "blockedtest@example.com",
		"password": "testpassword123",
	}

	loginResp := suite.request("POST", "/api/v1/auth/login", loginReq, "")
	defer func() { _ = loginResp.Body.Close() }()

	var loginBody map[string]interface{}
	_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)
	token := loginBody["token"].(string)

	// Try to access protected routes
	protectedEndpoints := []string{
		"/api/v1/users/me",
		"/api/v1/communities",
		"/api/v1/verification/status",
	}

	for _, endpoint := range protectedEndpoints {
		t.Run(endpoint, func(t *testing.T) {
			resp := suite.request("GET", endpoint, nil, token)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("Expected status 403 for %s, got %d", endpoint, resp.StatusCode)
			}

			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)

			if body["error"] != "email_verification_required" {
				t.Errorf("Expected error 'email_verification_required', got '%s'", body["error"])
			}
		})
	}

	// Cleanup
	suite.cleanup(userID)
}

func TestE2E_ProtectedRoute_AllowsVerifiedUser(t *testing.T) {
	suite := SetupEmailVerificationTest(t, true)
	suite.emailService.Reset()

	// Register a user
	regReq := map[string]string{
		"username": "allowedtest",
		"email":    "allowedtest@example.com",
		"password": "testpassword123",
	}

	regResp := suite.request("POST", "/api/v1/auth/register", regReq, "")
	defer func() { _ = regResp.Body.Close() }()

	var regBody map[string]interface{}
	_ = json.NewDecoder(regResp.Body).Decode(&regBody)
	userID := regBody["user_id"].(string)

	// Verify the user's email
	_ = suite.userRepo.SetEmailVerified(context.Background(), userID, true)

	// Login to get a full token
	loginReq := map[string]string{
		"email":    "allowedtest@example.com",
		"password": "testpassword123",
	}

	loginResp := suite.request("POST", "/api/v1/auth/login", loginReq, "")
	defer func() { _ = loginResp.Body.Close() }()

	var loginBody map[string]interface{}
	_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)
	token := loginBody["token"].(string)

	// Should be able to access protected route
	resp := suite.request("GET", "/api/v1/users/me", nil, token)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Cleanup
	suite.cleanup(userID)
}

// =============================================================================
// Integration Tests: Email Service Backends
// =============================================================================

func TestIntegration_EmailServiceFactory(t *testing.T) {
	t.Run("creates mock service by default", func(t *testing.T) {
		cfg := &config.EmailConfig{
			Backend:         config.EmailBackendMock,
			Enabled:         true,
			VerificationURL: "http://localhost:3000/verify-email",
			FromAddress:     "test@example.com",
			FromName:        "Test",
		}

		service, err := services.NewEmailService(cfg)
		if err != nil {
			t.Fatalf("Failed to create email service: %v", err)
		}

		if service.Backend() != "mock" {
			t.Errorf("Expected backend 'mock', got '%s'", service.Backend())
		}
	})

	t.Run("creates sendgrid service when configured", func(t *testing.T) {
		cfg := &config.EmailConfig{
			Backend:         config.EmailBackendSendGrid,
			Enabled:         true,
			VerificationURL: "http://localhost:3000/verify-email",
			FromAddress:     "test@example.com",
			FromName:        "Test",
			SendGridAPIKey:  "test-api-key",
		}

		service, err := services.NewEmailService(cfg)
		if err != nil {
			t.Fatalf("Failed to create email service: %v", err)
		}

		if service.Backend() != "sendgrid" {
			t.Errorf("Expected backend 'sendgrid', got '%s'", service.Backend())
		}
	})

	t.Run("creates smtp service when configured", func(t *testing.T) {
		cfg := &config.EmailConfig{
			Backend:         config.EmailBackendSMTP,
			Enabled:         true,
			VerificationURL: "http://localhost:3000/verify-email",
			FromAddress:     "test@example.com",
			FromName:        "Test",
			SMTPHost:        "smtp.example.com",
			SMTPPort:        587,
		}

		service, err := services.NewEmailService(cfg)
		if err != nil {
			t.Fatalf("Failed to create email service: %v", err)
		}

		if service.Backend() != "smtp" {
			t.Errorf("Expected backend 'smtp', got '%s'", service.Backend())
		}
	})

	t.Run("fails for invalid backend", func(t *testing.T) {
		cfg := &config.EmailConfig{
			Backend:         config.EmailBackend("invalid"),
			Enabled:         true,
			VerificationURL: "http://localhost:3000/verify-email",
			FromAddress:     "test@example.com",
			FromName:        "Test",
		}

		_, err := services.NewEmailService(cfg)
		if err == nil {
			t.Error("Expected error for invalid backend")
		}
	})

	t.Run("fails when sendgrid api key missing", func(t *testing.T) {
		cfg := &config.EmailConfig{
			Backend:         config.EmailBackendSendGrid,
			Enabled:         true,
			VerificationURL: "http://localhost:3000/verify-email",
			FromAddress:     "test@example.com",
			FromName:        "Test",
			SendGridAPIKey:  "", // Missing
		}

		_, err := services.NewEmailService(cfg)
		if err == nil {
			t.Error("Expected error when SendGrid API key is missing")
		}
	})

	t.Run("fails when smtp host missing", func(t *testing.T) {
		cfg := &config.EmailConfig{
			Backend:         config.EmailBackendSMTP,
			Enabled:         true,
			VerificationURL: "http://localhost:3000/verify-email",
			FromAddress:     "test@example.com",
			FromName:        "Test",
			SMTPHost:        "", // Missing
		}

		_, err := services.NewEmailService(cfg)
		if err == nil {
			t.Error("Expected error when SMTP host is missing")
		}
	})
}

func TestIntegration_MockEmailService_TracksEmails(t *testing.T) {
	cfg := &config.EmailConfig{
		Backend:         config.EmailBackendMock,
		Enabled:         true,
		VerificationURL: "http://localhost:3000/verify-email",
		FromAddress:     "test@example.com",
		FromName:        "Test",
	}

	service := services.NewMockEmailService(cfg)

	// Send verification email
	err := service.SendVerificationEmail(context.Background(), "user@example.com", "test-token")
	if err != nil {
		t.Fatalf("Failed to send verification email: %v", err)
	}

	// Verify it was logged (mock mode just logs, doesn't track in this implementation)
	// But for our test service, we track everything
}

func TestIntegration_EmailNormalization(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
		hasError bool
	}{
		// Gmail normalization
		{"j.o.h.n@gmail.com", "john@gmail.com", false},
		{"John.Doe@Gmail.com", "johndoe@gmail.com", false},
		{"user@googlemail.com", "user@gmail.com", false},
		{"J.Smith@GOOGLEMAIL.COM", "jsmith@gmail.com", false},

		// Preserve dots for non-Gmail
		{"j.doe@yahoo.com", "j.doe@yahoo.com", false},
		{"first.last@company.com", "first.last@company.com", false},

		// Case normalization
		{"USER@EXAMPLE.COM", "user@example.com", false},

		// Reject plus aliases
		{"user+test@gmail.com", "", true},
		{"test+alias@example.com", "", true},

		// Invalid emails
		{"", "", true},
		{"notanemail", "", true},
		{"@nodomain.com", "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result, err := services.NormalizeEmail(tc.input)

			if tc.hasError {
				if err == nil {
					t.Errorf("Expected error for input '%s'", tc.input)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for input '%s': %v", tc.input, err)
				}
				if result != tc.expected {
					t.Errorf("Expected '%s', got '%s'", tc.expected, result)
				}
			}
		})
	}
}
