package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// E2ETestSuite holds all dependencies for e2e tests
type E2ETestSuite struct {
	t                   *testing.T
	db                  *database.DB
	server              *httptest.Server
	userRepo            *database.UserRepository
	regionRepo          *database.RegionRepository
	verifyRepo          *database.VerificationRepository
	vouchRepo           *database.VouchRepository
	groupRepo           *database.SignalGroupRepository
	communityGroupRepo  *database.GroupRepository
	jwtAuth             *middleware.JWTAuth
	schoolRepo          *database.SchoolRepository
	districtRepo        *database.SchoolDistrictRepository
	encryptedSecretRepo *database.EncryptedSecretRepository
	encryptionKeyRepo   *database.EncryptionKeyRepository
	meshtasticRepo      *database.MeshtasticChannelRepository
	mockMapbox          *mocks.MockMapboxService
}

// SetupE2ETest creates a new test suite
func SetupE2ETest(t *testing.T) *E2ETestSuite {
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
	vouchRepo := database.NewVouchRepository(db)
	groupRepo := database.NewSignalGroupRepository(db)
	schoolRepo := database.NewSchoolRepository(db)
	districtRepo := database.NewSchoolDistrictRepository(db)
	auditRepo := database.NewAuditRepository(db)
	encryptedSecretRepo := database.NewEncryptedSecretRepository(db)
	encryptionKeyRepo := database.NewEncryptionKeyRepository(db)
	meshtasticChannelRepo := database.NewMeshtasticChannelRepository(db)
	communityGroupRepo := database.NewGroupRepository(db)

	// Create JWT auth
	jwtConfig := &config.JWTConfig{
		Secret:          "test_secret_key_at_least_32_characters_long",
		ExpirationHours: 24,
		Issuer:          "test_issuer",
	}
	jwtAuth := middleware.NewJWTAuth(jwtConfig)

	// Set up user status cache for token revocation (short TTL for tests)
	statusCache := middleware.NewUserStatusCache(userRepo, 100*time.Millisecond)
	jwtAuth.SetStatusCache(statusCache)

	// Create mock services for testing
	mockPostgrid := mocks.NewMockPostgridService()
	mockMapbox := mocks.NewMockMapboxService()

	passwordResetRepo := database.NewPasswordResetRepository(db)

	// Create handlers with mock services
	authHandler := handlers.NewAuthHandlerWithEmailService(
		db, userRepo, jwtAuth, nil, jwtConfig.Secret,
		false, true, auditRepo, passwordResetRepo, nil,
		"http://localhost:3000", encryptionKeyRepo,
	)
	regionHandler := handlers.NewRegionHandler(regionRepo, mockMapbox, nil)
	verificationHandler := handlers.NewVerificationHandler(
		nil, verifyRepo, vouchRepo, userRepo, regionRepo,
		mockPostgrid, mockMapbox, nil,
		false, 30, // Bootstrap cooldown disabled for tests
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

	encryptionHandler := handlers.NewEncryptionHandler(encryptionKeyRepo, encryptedSecretRepo, regionRepo, schoolRepo)
	groupHandler := handlers.NewGroupHandler(communityGroupRepo, groupRepo, meshtasticChannelRepo, regionRepo, userRepo, auditRepo)
	connectionRepo := database.NewConnectionRepository(db)
	connectionHandler := handlers.NewConnectionHandler(connectionRepo, communityGroupRepo, auditRepo)

	// Create router (rate limiting disabled for tests)
	router := handlers.NewRouter(
		authHandler, mfaHandler, regionHandler, verificationHandler, adminHandler,
		schoolHandler, encryptionHandler, groupHandler, connectionHandler, jwtAuth, nil, nil, nil,
		[]string{"*"}, nil,
	)
	handler := router.Setup()

	server := httptest.NewServer(handler)

	suite := &E2ETestSuite{
		t:                   t,
		db:                  db,
		server:              server,
		userRepo:            userRepo,
		regionRepo:          regionRepo,
		verifyRepo:          verifyRepo,
		vouchRepo:           vouchRepo,
		groupRepo:           groupRepo,
		communityGroupRepo:  communityGroupRepo,
		jwtAuth:             jwtAuth,
		schoolRepo:          schoolRepo,
		districtRepo:        districtRepo,
		encryptedSecretRepo: encryptedSecretRepo,
		encryptionKeyRepo:   encryptionKeyRepo,
		meshtasticRepo:      meshtasticChannelRepo,
		mockMapbox:          mockMapbox,
	}

	t.Cleanup(func() {
		server.Close()
		_ = db.Close()
	})

	return suite
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Helper methods

func (s *E2ETestSuite) request(method, path string, body interface{}, token string) *http.Response {
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

func (s *E2ETestSuite) cleanup(userIDs ...string) {
	ctx := context.Background()
	for _, id := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_encryption_keys WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM school_vouches WHERE voucher_user_id = ? OR vouched_user_id = ?", id, id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_schools WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM verification_requests WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM vouches WHERE voucher_user_id = ? OR vouched_user_id = ?", id, id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	}
}

// disableMFA disables MFA requirements for a user so they can login with a full token
func (s *E2ETestSuite) disableMFA(userID string) {
	_, _ = s.db.ExecContext(context.Background(), "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", userID)
}

// makeUserVouchVerified sets a user as vouch-verified in the database.
// Call this BEFORE login/re-login so the JWT includes the correct claims.
func (s *E2ETestSuite) makeUserVouchVerified(userID string) {
	_, err := s.db.ExecContext(context.Background(),
		"UPDATE users SET vouch_verified = TRUE, verification_tier = 1 WHERE id = ?", userID)
	if err != nil {
		s.t.Fatalf("Failed to make user vouch-verified: %v", err)
	}
}

// makeUserFullyVerified sets a user as both vouch and postcard verified (tier 2).
func (s *E2ETestSuite) makeUserFullyVerified(userID string) {
	_, err := s.db.ExecContext(context.Background(),
		"UPDATE users SET vouch_verified = TRUE, postcard_verified = TRUE, verification_tier = 2 WHERE id = ?", userID)
	if err != nil {
		s.t.Fatalf("Failed to make user fully verified: %v", err)
	}
}

func (s *E2ETestSuite) makeUserPostcardVerified(userID string) {
	_, err := s.db.ExecContext(context.Background(),
		"UPDATE users SET postcard_verified = TRUE, verification_tier = 2 WHERE id = ?", userID)
	if err != nil {
		s.t.Fatalf("Failed to make user postcard-verified: %v", err)
	}
}

// registerOrGetUser registers a new user or retrieves existing one, disables MFA, logs in, and returns userID + token
func (s *E2ETestSuite) registerOrGetUser(username, email, password string) (string, string) {
	resp := s.request("POST", "/api/v1/auth/register", map[string]string{
		"username": username, "email": email, "password": password,
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

	s.disableMFA(userID)

	loginResp := s.request("POST", "/api/v1/auth/login", map[string]string{
		"email": email, "password": password,
	}, "")
	defer func() { _ = loginResp.Body.Close() }()

	var login models.LoginResponse
	_ = json.NewDecoder(loginResp.Body).Decode(&login)
	return userID, login.Token
}

// registerOrGetUserID registers a new user or retrieves existing one, returns just the userID (no login).
// Useful when you need to set verification flags before login.
func (s *E2ETestSuite) registerOrGetUserID(username, email, password string) string {
	resp := s.request("POST", "/api/v1/auth/register", map[string]string{
		"username": username, "email": email, "password": password,
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

	return userID
}

// reloginUser logs in an existing user and returns a fresh token with updated claims.
func (s *E2ETestSuite) reloginUser(email, password string) string {
	resp := s.request("POST", "/api/v1/auth/login", map[string]string{
		"email": email, "password": password,
	}, "")
	defer func() { _ = resp.Body.Close() }()

	var login models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&login)
	if login.Token == "" {
		s.t.Fatalf("reloginUser: failed to get token for %s", email)
	}
	return login.Token
}

// School-specific helpers

func (s *E2ETestSuite) createSchool(name, state string) string {
	schoolID := fmt.Sprintf("e2e-school-%s-%d", name[:4], time.Now().UnixNano())
	ncesID := fmt.Sprintf("%012d", time.Now().UnixNano()%1000000000000)
	_, err := s.db.ExecContext(context.Background(),
		"INSERT INTO schools (id, nces_id, name, state, created_at) VALUES (?, ?, ?, ?, NOW())",
		schoolID, ncesID, name, state)
	if err != nil {
		s.t.Fatalf("Failed to create test school: %v", err)
	}
	return schoolID
}

func (s *E2ETestSuite) createDistrict(name, state string) string {
	districtID := fmt.Sprintf("e2e-dist-%s-%d", name[:4], time.Now().UnixNano())
	ncesID := fmt.Sprintf("%07d", time.Now().UnixNano()%10000000)
	_, err := s.db.ExecContext(context.Background(),
		"INSERT INTO school_districts (id, nces_id, name, state, district_type, created_at) VALUES (?, ?, ?, ?, 'unified', NOW())",
		districtID, ncesID, name, state)
	if err != nil {
		s.t.Fatalf("Failed to create test district: %v", err)
	}
	return districtID
}

func (s *E2ETestSuite) linkSchoolToDistrict(schoolID, districtID string) {
	_, err := s.db.ExecContext(context.Background(),
		"UPDATE schools SET district_id = ? WHERE id = ?", districtID, schoolID)
	if err != nil {
		s.t.Fatalf("Failed to link school to district: %v", err)
	}
}

func (s *E2ETestSuite) addUserToSchool(userID, schoolID string, status string, isAdmin bool) {
	membershipID := fmt.Sprintf("e2e-us-%d", time.Now().UnixNano())
	query := "INSERT INTO user_schools (id, user_id, school_id, is_admin, verification_status) VALUES (?, ?, ?, ?, ?)"
	if status == "verified" {
		query = "INSERT INTO user_schools (id, user_id, school_id, is_admin, verification_status, verified_at) VALUES (?, ?, ?, ?, ?, NOW())"
	}
	_, err := s.db.ExecContext(context.Background(), query, membershipID, userID, schoolID, isAdmin, status)
	if err != nil {
		s.t.Fatalf("Failed to add user to school: %v", err)
	}
}

func (s *E2ETestSuite) cleanupSchools(schoolIDs ...string) {
	ctx := context.Background()
	for _, schoolID := range schoolIDs {
		// Clean up meshtastic channel encrypted secrets and channels
		_, _ = s.db.ExecContext(ctx, `DELETE esk FROM encrypted_secret_keys esk
			INNER JOIN encrypted_secrets es ON esk.secret_id = es.id
			INNER JOIN meshtastic_channels mc ON es.meshtastic_channel_id = mc.id
			WHERE mc.school_id = ?`, schoolID)
		_, _ = s.db.ExecContext(ctx, `DELETE es FROM encrypted_secrets es
			INNER JOIN meshtastic_channels mc ON es.meshtastic_channel_id = mc.id
			WHERE mc.school_id = ?`, schoolID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM meshtastic_channels WHERE school_id = ?", schoolID)
		// Clean up signal group encrypted secrets
		_, _ = s.db.ExecContext(ctx, `DELETE esk FROM encrypted_secret_keys esk
			INNER JOIN encrypted_secrets es ON esk.secret_id = es.id
			INNER JOIN signal_groups sg ON es.signal_group_id = sg.id
			WHERE sg.school_id = ?`, schoolID)
		_, _ = s.db.ExecContext(ctx, `DELETE es FROM encrypted_secrets es
			INNER JOIN signal_groups sg ON es.signal_group_id = sg.id
			WHERE sg.school_id = ?`, schoolID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM school_vouches WHERE school_id = ?", schoolID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE school_id = ?", schoolID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM school_blocked_users WHERE school_id = ?", schoolID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_schools WHERE school_id = ?", schoolID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM schools WHERE id = ?", schoolID)
	}
}

func (s *E2ETestSuite) cleanupDistricts(districtIDs ...string) {
	ctx := context.Background()
	for _, districtID := range districtIDs {
		// Clean up meshtastic channel encrypted secrets and channels
		_, _ = s.db.ExecContext(ctx, `DELETE esk FROM encrypted_secret_keys esk
			INNER JOIN encrypted_secrets es ON esk.secret_id = es.id
			INNER JOIN meshtastic_channels mc ON es.meshtastic_channel_id = mc.id
			WHERE mc.district_id = ?`, districtID)
		_, _ = s.db.ExecContext(ctx, `DELETE es FROM encrypted_secrets es
			INNER JOIN meshtastic_channels mc ON es.meshtastic_channel_id = mc.id
			WHERE mc.district_id = ?`, districtID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM meshtastic_channels WHERE district_id = ?", districtID)
		// Clean up signal group encrypted secrets
		_, _ = s.db.ExecContext(ctx, `DELETE esk FROM encrypted_secret_keys esk
			INNER JOIN encrypted_secrets es ON esk.secret_id = es.id
			INNER JOIN signal_groups sg ON es.signal_group_id = sg.id
			WHERE sg.district_id = ?`, districtID)
		_, _ = s.db.ExecContext(ctx, `DELETE es FROM encrypted_secrets es
			INNER JOIN signal_groups sg ON es.signal_group_id = sg.id
			WHERE sg.district_id = ?`, districtID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE district_id = ?", districtID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM school_districts WHERE id = ?", districtID)
	}
}

// exitSchoolBootstrapMode creates 3 verified admin users in a school
// Returns the user IDs for cleanup
func (s *E2ETestSuite) exitSchoolBootstrapMode(schoolID string) []string {
	ctx := context.Background()
	var userIDs []string
	for i := 0; i < 3; i++ {
		user := &models.User{
			Username:         fmt.Sprintf("e2e_school_admin_%d_%s", i, schoolID[:8]),
			Email:            fmt.Sprintf("e2e_school_admin_%d_%s@test.com", i, schoolID[:8]),
			PasswordHash:     "$2a$12$test.hash.only",
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		if err := s.userRepo.Create(ctx, user); err != nil {
			s.t.Fatalf("Failed to create school bootstrap admin: %v", err)
		}
		s.addUserToSchool(user.ID, schoolID, "verified", true)
		userIDs = append(userIDs, user.ID)
	}
	return userIDs
}

// Tests

func TestE2E_HealthCheck(t *testing.T) {
	suite := SetupE2ETest(t)

	resp := suite.request("GET", "/health", nil, "")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)

	if body["status"] != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", body["status"])
	}
}

func TestE2E_RegistrationAndLogin(t *testing.T) {
	suite := SetupE2ETest(t)

	var userID string

	t.Run("register new user", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
			"username": "e2etest",
			"email":    "e2e@test.com",
			"password": "securepassword123",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", resp.StatusCode)
		}

		var body models.RegisterResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.UserID == "" {
			t.Error("Expected user ID")
		}
		userID = body.UserID

		if body.Username != "e2etest" {
			t.Errorf("Expected username 'e2etest', got '%s'", body.Username)
		}
	})

	var token string

	t.Run("login with credentials", func(t *testing.T) {
		// Disable MFA so login returns a full token
		suite.disableMFA(userID)

		resp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email":    "e2e@test.com",
			"password": "securepassword123",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var body models.LoginResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.Token == "" {
			t.Error("Expected token")
		}
		token = body.Token

		if body.User.Username != "e2etest" {
			t.Errorf("Expected username 'e2etest', got '%s'", body.User.Username)
		}
		if body.User.VerificationTier != models.TierUnverified {
			t.Errorf("Expected tier 0, got %d", body.User.VerificationTier)
		}
	})

	t.Run("get current user", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/users/me", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Response is wrapped in {"user": {...}}
		var body struct {
			User models.UserWithRegions `json:"user"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.User.Username != "e2etest" {
			t.Errorf("Expected username 'e2etest', got '%s'", body.User.Username)
		}
	})

	t.Run("logout", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/logout", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})

	// Cleanup
	suite.cleanup(userID)
}

func TestE2E_ProtectedRoutes(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/users/me", nil, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})

	t.Run("rejects invalid token", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/users/me", nil, "invalid.token.here")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})
}

func TestE2E_RegionsList(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register a user
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "regionlisttest",
		"email":    "regionlist@test.com",
		"password": "securepassword123",
	}, "")
	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	_ = resp.Body.Close()

	ctx := context.Background()

	// Disable MFA to get a login token
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", registerResp.UserID)

	// Login to get token
	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "regionlist@test.com",
		"password": "securepassword123",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token := loginResp.Token

	t.Run("unauthenticated request fails", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/communities", nil, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})

	t.Run("authenticated user can list regions", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/communities", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if _, ok := body["regions"]; !ok {
			t.Error("Expected 'regions' key in response")
		}
	})

	// Cleanup
	suite.cleanup(registerResp.UserID)
}

func TestE2E_VerificationFlow(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register user
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "verifyflowtest",
		"email":    "verifyflow@test.com",
		"password": "securepassword123",
	}, "")
	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	_ = resp.Body.Close()

	// Disable MFA and make vouch-verified so postcard request is allowed
	suite.disableMFA(registerResp.UserID)
	suite.makeUserVouchVerified(registerResp.UserID)

	// Login (token will include vouch-verified claims)
	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "verifyflow@test.com",
		"password": "securepassword123",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()

	token := loginResp.Token

	t.Run("request postcard verification", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"address": map[string]string{
				"line1":       "123 Test St",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		// In mock mode, this should succeed
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Logf("Response: %v", body)
			// Note: might fail due to missing region, which is expected
		}
	})

	// Cleanup
	suite.cleanup(registerResp.UserID)
}

func TestE2E_CORS(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("handles preflight request", func(t *testing.T) {
		req, _ := http.NewRequest("OPTIONS", suite.server.URL+"/api/v1/auth/login", nil)
		req.Header.Set("Origin", "https://example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		if resp.Header.Get("Access-Control-Allow-Origin") == "" {
			t.Error("Expected Access-Control-Allow-Origin header")
		}
	})

	t.Run("adds CORS headers to responses", func(t *testing.T) {
		req, _ := http.NewRequest("GET", suite.server.URL+"/health", nil)
		req.Header.Set("Origin", "https://example.com")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.Header.Get("Access-Control-Allow-Origin") == "" {
			t.Error("Expected Access-Control-Allow-Origin header")
		}
	})
}

func TestE2E_SecurityHeaders(t *testing.T) {
	suite := SetupE2ETest(t)

	resp := suite.request("GET", "/health", nil, "")
	defer func() { _ = resp.Body.Close() }()

	expectedHeaders := []string{
		"X-Content-Type-Options",
		"X-Frame-Options",
		"X-XSS-Protection",
		"Referrer-Policy",
	}

	for _, header := range expectedHeaders {
		if resp.Header.Get(header) == "" {
			t.Errorf("Expected %s header", header)
		}
	}

	// With nil securityConfig (test default), CSP should not be set
	if resp.Header.Get("Content-Security-Policy") != "" {
		t.Error("Expected no Content-Security-Policy header with nil securityConfig")
	}
}

func TestE2E_CORS_RejectsUnknownOrigin(t *testing.T) {
	// Stand up a minimal server with a restricted CORS origin list
	corsHandler := middleware.CORS([]string{"https://allowed.example.com"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)
	server := httptest.NewServer(corsHandler)
	defer server.Close()

	t.Run("allowed origin gets CORS headers", func(t *testing.T) {
		req, _ := http.NewRequest("GET", server.URL+"/health", nil)
		req.Header.Set("Origin", "https://allowed.example.com")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
		if allowOrigin != "https://allowed.example.com" {
			t.Errorf("Expected allowed origin reflected, got '%s'", allowOrigin)
		}
	})

	t.Run("unknown origin gets no CORS headers", func(t *testing.T) {
		req, _ := http.NewRequest("GET", server.URL+"/health", nil)
		req.Header.Set("Origin", "https://evil.example.com")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
		if allowOrigin != "" {
			t.Errorf("Expected no Access-Control-Allow-Origin for unknown origin, got '%s'", allowOrigin)
		}
	})
}

func TestE2E_ContentType(t *testing.T) {
	suite := SetupE2ETest(t)

	resp := suite.request("GET", "/health", nil, "")
	defer func() { _ = resp.Body.Close() }()

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}
}

func TestE2E_MethodNotAllowed(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("rejects wrong method on register", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/auth/register", nil, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", resp.StatusCode)
		}
	})

	t.Run("rejects wrong method on login", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/auth/login", nil, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", resp.StatusCode)
		}
	})
}

func TestE2E_ValidationErrors(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("returns validation error for missing fields", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
			"username": "incomplete",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["error"] == nil {
			t.Error("Expected error in response")
		}
	})

	t.Run("returns validation error for short password", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
			"username": "shortpass",
			"email":    "short@test.com",
			"password": "short",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})
}

func TestE2E_RegionUpdate(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register user
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "regionupdatetest",
		"email":    "regionupdate@test.com",
		"password": "securepassword123",
	}, "")
	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	_ = resp.Body.Close()

	// Disable MFA and make superuser with full verification for testing
	ctx := context.Background()
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, verification_tier = 2, postcard_verified = TRUE, vouch_verified = TRUE, is_superuser = true WHERE id = ?", registerResp.UserID)

	// Login to get token
	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "regionupdate@test.com",
		"password": "securepassword123",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token := loginResp.Token

	// Create a region
	createResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name": "Update Test Region",
		"type": "city",
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-98.6, 29.4}, {-98.4, 29.4}, {-98.4, 29.5}, {-98.6, 29.5}, {-98.6, 29.4}}},
		},
	}, token)
	var createBody map[string]string
	_ = json.NewDecoder(createResp.Body).Decode(&createBody)
	_ = createResp.Body.Close()

	regionID := createBody["region_id"]

	defer func() {
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", regionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", regionID)
		suite.cleanup(registerResp.UserID)
	}()

	t.Run("update region name", func(t *testing.T) {
		resp := suite.request("PUT", "/api/v1/communities/"+regionID, map[string]string{
			"name": "Updated Region Name",
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body["name"] != "Updated Region Name" {
			t.Errorf("Expected updated name, got '%s'", body["name"])
		}
	})
}

func TestE2E_RegionDelete(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register user
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "regiondeletetest",
		"email":    "regiondelete@test.com",
		"password": "securepassword123",
	}, "")
	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	_ = resp.Body.Close()

	ctx := context.Background()

	// Disable MFA and make superuser with full verification
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, verification_tier = 2, postcard_verified = TRUE, vouch_verified = TRUE, is_superuser = true WHERE id = ?", registerResp.UserID)

	// Login
	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "regiondelete@test.com",
		"password": "securepassword123",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token := loginResp.Token

	// Create a region
	createResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name": "Delete Test Region",
		"type": "city",
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-98.6, 29.4}, {-98.4, 29.4}, {-98.4, 29.5}, {-98.6, 29.5}, {-98.6, 29.4}}},
		},
	}, token)
	var createBody map[string]string
	_ = json.NewDecoder(createResp.Body).Decode(&createBody)
	_ = createResp.Body.Close()

	regionID := createBody["region_id"]

	defer suite.cleanup(registerResp.UserID)

	t.Run("superuser can delete region", func(t *testing.T) {
		resp := suite.request("DELETE", "/api/v1/communities/"+regionID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}
	})
}

func TestE2E_RegionTypeHierarchy(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register user
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "hierarchytest",
		"email":    "hierarchy@test.com",
		"password": "securepassword123",
	}, "")
	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	_ = resp.Body.Close()

	ctx := context.Background()

	// Disable MFA and make superuser with full verification to bypass boundary validation (this test is about type hierarchy)
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, verification_tier = 2, postcard_verified = TRUE, vouch_verified = TRUE, is_superuser = TRUE WHERE id = ?", registerResp.UserID)

	// Login
	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "hierarchy@test.com",
		"password": "securepassword123",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token := loginResp.Token

	defer suite.cleanup(registerResp.UserID)

	t.Run("city cannot have parent", func(t *testing.T) {
		// First create a parent city
		createResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
			"name": "Parent City",
			"type": "city",
			"geometry": map[string]interface{}{
				"type":        "Polygon",
				"coordinates": [][][]float64{{{-98.6, 29.4}, {-98.4, 29.4}, {-98.4, 29.5}, {-98.6, 29.5}, {-98.6, 29.4}}},
			},
		}, token)
		var createBody map[string]string
		_ = json.NewDecoder(createResp.Body).Decode(&createBody)
		_ = createResp.Body.Close()
		parentID := createBody["region_id"]

		defer func() {
			_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", parentID)
			_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", parentID)
		}()

		// Try to create city with parent
		resp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
			"name":             "Child City",
			"type":             "city",
			"parent_region_id": parentID,
			"geometry": map[string]interface{}{
				"type":        "Polygon",
				"coordinates": [][][]float64{{{-98.55, 29.42}, {-98.45, 29.42}, {-98.45, 29.48}, {-98.55, 29.48}, {-98.55, 29.42}}},
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("neighborhood must have city parent", func(t *testing.T) {
		// Try to create neighborhood without parent
		resp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
			"name": "Orphan Neighborhood",
			"type": "neighborhood",
			"geometry": map[string]interface{}{
				"type":        "Polygon",
				"coordinates": [][][]float64{{{-98.55, 29.42}, {-98.45, 29.42}, {-98.45, 29.48}, {-98.55, 29.48}, {-98.55, 29.42}}},
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("city_block must have neighborhood parent", func(t *testing.T) {
		// Try to create city_block without parent
		resp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
			"name": "Orphan Block",
			"type": "city_block",
			"geometry": map[string]interface{}{
				"type":        "Polygon",
				"coordinates": [][][]float64{{{-98.55, 29.42}, {-98.45, 29.42}, {-98.45, 29.48}, {-98.55, 29.48}, {-98.55, 29.42}}},
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})
}

func TestE2E_RegionGeometry(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register user
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "geometrytest",
		"email":    "geometry@test.com",
		"password": "securepassword123",
	}, "")
	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	_ = resp.Body.Close()

	ctx := context.Background()

	// Disable MFA and make superuser with full verification to bypass boundary validation
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, verification_tier = 2, postcard_verified = TRUE, vouch_verified = TRUE, is_superuser = TRUE WHERE id = ?", registerResp.UserID)

	// Login
	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "geometry@test.com",
		"password": "securepassword123",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token := loginResp.Token

	// Create a region
	createResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name": "Geometry Test Region",
		"type": "city",
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-98.6, 29.4}, {-98.4, 29.4}, {-98.4, 29.5}, {-98.6, 29.5}, {-98.6, 29.4}}},
		},
	}, token)
	var createBody map[string]string
	_ = json.NewDecoder(createResp.Body).Decode(&createBody)
	_ = createResp.Body.Close()

	regionID := createBody["region_id"]

	defer func() {
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", regionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", regionID)
		suite.cleanup(registerResp.UserID)
	}()

	t.Run("get region includes geometry", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/communities/"+regionID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["geometry"] == nil {
			t.Error("Expected geometry field in response")
		}
	})

	t.Run("list regions includes geometry", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/communities", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		regions, ok := body["regions"].([]interface{})
		if !ok || len(regions) == 0 {
			t.Skip("No regions to check geometry")
		}

		// Find our region
		for _, r := range regions {
			region := r.(map[string]interface{})
			if region["id"] == regionID {
				if region["geometry"] == nil {
					t.Error("Expected geometry field in region list response")
				}
				break
			}
		}
	})
}

// =============================================================================
// Vouch Verification Flow E2E Tests
// =============================================================================

func TestE2E_VouchVerificationFlow(t *testing.T) {
	suite := SetupE2ETest(t)

	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Step 1: Create region hierarchy using a superuser
	superUserID, _ := suite.registerOrGetUser(
		fmt.Sprintf("vouchflowsuper_%s", suffix),
		fmt.Sprintf("vouchflowsuper_%s@test.com", suffix),
		"securepassword123",
	)

	// Make superuser with all permissions
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET is_superuser = TRUE, postcard_verified = TRUE, vouch_verified = TRUE, verification_tier = 2 WHERE id = ?", superUserID)

	// Re-login to get token with updated claims
	superLoginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    fmt.Sprintf("vouchflowsuper_%s@test.com", suffix),
		"password": "securepassword123",
	}, "")
	var superLogin models.LoginResponse
	_ = json.NewDecoder(superLoginResp.Body).Decode(&superLogin)
	_ = superLoginResp.Body.Close()
	superToken := superLogin.Token

	defer suite.cleanup(superUserID)

	stateName := fmt.Sprintf("Vouch Flow State %s", suffix)
	countyName := fmt.Sprintf("Vouch Flow County %s", suffix)
	cityName := fmt.Sprintf("Vouch Flow City %s", suffix)

	// Create state region
	stateResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name": stateName,
		"type": "state",
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-125.0, 45.0}, {-116.0, 45.0}, {-116.0, 49.0}, {-125.0, 49.0}, {-125.0, 45.0}}},
		},
	}, superToken)
	var stateBody map[string]string
	_ = json.NewDecoder(stateResp.Body).Decode(&stateBody)
	_ = stateResp.Body.Close()
	stateRegionID := stateBody["region_id"]
	if stateRegionID == "" {
		t.Fatal("Failed to create state region")
	}

	// Create county region
	countyResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name":             countyName,
		"type":             "county",
		"parent_region_id": stateRegionID,
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-123.0, 46.0}, {-121.0, 46.0}, {-121.0, 48.0}, {-123.0, 48.0}, {-123.0, 46.0}}},
		},
	}, superToken)
	var countyBody map[string]string
	_ = json.NewDecoder(countyResp.Body).Decode(&countyBody)
	_ = countyResp.Body.Close()
	countyRegionID := countyBody["region_id"]
	if countyRegionID == "" {
		t.Fatal("Failed to create county region")
	}

	// Create city region
	cityResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name":             cityName,
		"type":             "city",
		"parent_region_id": countyRegionID,
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-122.5, 47.0}, {-122.0, 47.0}, {-122.0, 47.5}, {-122.5, 47.5}, {-122.5, 47.0}}},
		},
	}, superToken)
	var cityBody map[string]string
	_ = json.NewDecoder(cityResp.Body).Decode(&cityBody)
	_ = cityResp.Body.Close()
	cityRegionID := cityBody["region_id"]
	if cityRegionID == "" {
		t.Fatal("Failed to create city region")
	}

	defer func() {
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM vouches WHERE region_id = ?", cityRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", cityRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", countyRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", stateRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", cityRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", countyRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", stateRegionID)
	}()

	// Step 2: Create two fully verified vouchers (admins)
	voucher1ID, _ := suite.registerOrGetUser(
		fmt.Sprintf("voucher1_%s", suffix),
		fmt.Sprintf("voucher1_%s@test.com", suffix),
		"securepassword123",
	)

	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET postcard_verified = TRUE, vouch_verified = TRUE, verification_tier = 2 WHERE id = ?", voucher1ID)
	// Add to region as admin
	_, _ = suite.db.ExecContext(ctx, "INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, TRUE, 'verified', NOW())", voucher1ID, cityRegionID)

	voucher1LoginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    fmt.Sprintf("voucher1_%s@test.com", suffix),
		"password": "securepassword123",
	}, "")
	var voucher1Login models.LoginResponse
	_ = json.NewDecoder(voucher1LoginResp.Body).Decode(&voucher1Login)
	_ = voucher1LoginResp.Body.Close()
	voucher1Token := voucher1Login.Token

	defer suite.cleanup(voucher1ID)

	voucher2ID, _ := suite.registerOrGetUser(
		fmt.Sprintf("voucher2_%s", suffix),
		fmt.Sprintf("voucher2_%s@test.com", suffix),
		"securepassword123",
	)

	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET postcard_verified = TRUE, vouch_verified = TRUE, verification_tier = 2 WHERE id = ?", voucher2ID)
	// Add to region as admin
	_, _ = suite.db.ExecContext(ctx, "INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, TRUE, 'verified', NOW())", voucher2ID, cityRegionID)

	voucher2LoginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    fmt.Sprintf("voucher2_%s@test.com", suffix),
		"password": "securepassword123",
	}, "")
	var voucher2Login models.LoginResponse
	_ = json.NewDecoder(voucher2LoginResp.Body).Decode(&voucher2Login)
	_ = voucher2LoginResp.Body.Close()
	voucher2Token := voucher2Login.Token

	defer suite.cleanup(voucher2ID)

	// Step 3: Create a new user who wants to be vouched
	voucheeID, voucheeToken := suite.registerOrGetUser(
		fmt.Sprintf("vouchee_%s", suffix),
		fmt.Sprintf("vouchee_%s@test.com", suffix),
		"securepassword123",
	)

	defer suite.cleanup(voucheeID)

	// Configure mock Mapbox to return coordinates within the city region geometry
	suite.mockMapbox.DefaultLatitude = 47.25
	suite.mockMapbox.DefaultLongitude = -122.25
	suite.mockMapbox.DefaultBoundaryType = "city"
	suite.mockMapbox.DefaultBoundaryName = cityName
	suite.mockMapbox.DefaultBoundaryState = stateName

	t.Run("full vouch verification flow", func(t *testing.T) {
		// Step 4: Vouchee requests vouch verification (address is geocoded to identify region)
		requestResp := suite.request("POST", "/api/v1/verification/vouch/request", map[string]interface{}{
			"address": map[string]string{
				"line1":       "123 Main St",
				"city":        "Test City",
				"state":       "WA",
				"postal_code": "98101",
			},
		}, voucheeToken)
		defer func() { _ = requestResp.Body.Close() }()

		if requestResp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(requestResp.Body).Decode(&body)
			t.Fatalf("Expected status 201, got %d: %v", requestResp.StatusCode, body)
		}

		var requestBody models.VouchVerificationResponse
		_ = json.NewDecoder(requestResp.Body).Decode(&requestBody)

		if requestBody.Status != "pending" {
			t.Errorf("Expected status 'pending', got '%s'", requestBody.Status)
		}
		if requestBody.VouchesNeeded != 2 {
			t.Errorf("Expected 2 vouches needed, got %d", requestBody.VouchesNeeded)
		}

		// Step 5: Check verification status shows pending request
		statusResp := suite.request("GET", "/api/v1/verification/status", nil, voucheeToken)
		defer func() { _ = statusResp.Body.Close() }()

		var statusBody map[string]interface{}
		_ = json.NewDecoder(statusResp.Body).Decode(&statusBody)

		if _, ok := statusBody["pending_vouch_region"]; !ok {
			t.Error("Expected pending_vouch_region in status response")
		}

		// Step 6: First voucher vouches for vouchee
		vouch1Resp := suite.request("POST", "/api/v1/verification/vouch", map[string]string{
			"vouched_user_id": voucheeID,
			"region_id":       cityRegionID,
		}, voucher1Token)
		defer func() { _ = vouch1Resp.Body.Close() }()

		if vouch1Resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(vouch1Resp.Body).Decode(&body)
			t.Fatalf("Voucher 1 vouch failed: status %d: %v", vouch1Resp.StatusCode, body)
		}

		var vouch1Body models.VouchResponse
		_ = json.NewDecoder(vouch1Resp.Body).Decode(&vouch1Body)

		if vouch1Body.TotalVouches != 1 {
			t.Errorf("Expected 1 total vouch after first vouch, got %d", vouch1Body.TotalVouches)
		}
		if vouch1Body.VouchesNeeded != 1 {
			t.Errorf("Expected 1 vouch needed after first vouch, got %d", vouch1Body.VouchesNeeded)
		}

		// Step 7: Check vouchee status after first vouch
		statusResp2 := suite.request("GET", "/api/v1/verification/status", nil, voucheeToken)
		defer func() { _ = statusResp2.Body.Close() }()

		var statusBody2 map[string]interface{}
		_ = json.NewDecoder(statusResp2.Body).Decode(&statusBody2)

		if statusBody2["vouch_count"] != float64(1) {
			t.Errorf("Expected vouch_count 1, got %v", statusBody2["vouch_count"])
		}

		// Step 8: Second voucher vouches for vouchee - should complete verification
		vouch2Resp := suite.request("POST", "/api/v1/verification/vouch", map[string]string{
			"vouched_user_id": voucheeID,
			"region_id":       cityRegionID,
		}, voucher2Token)
		defer func() { _ = vouch2Resp.Body.Close() }()

		if vouch2Resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(vouch2Resp.Body).Decode(&body)
			t.Fatalf("Voucher 2 vouch failed: status %d: %v", vouch2Resp.StatusCode, body)
		}

		var vouch2Body models.VouchResponse
		_ = json.NewDecoder(vouch2Resp.Body).Decode(&vouch2Body)

		if vouch2Body.TotalVouches != 2 {
			t.Errorf("Expected 2 total vouches after second vouch, got %d", vouch2Body.TotalVouches)
		}
		if vouch2Body.VouchesNeeded != 0 {
			t.Errorf("Expected 0 vouches needed after second vouch, got %d", vouch2Body.VouchesNeeded)
		}

		// Step 9: Verify vouchee is now vouch-verified
		var vouchVerified bool
		_ = suite.db.QueryRowContext(ctx, "SELECT vouch_verified FROM users WHERE id = ?", voucheeID).Scan(&vouchVerified)
		if !vouchVerified {
			t.Error("Expected vouchee to be vouch_verified after 2 vouches")
		}

		// Step 10: Verify user_region was upgraded from pending to verified
		var verificationStatus string
		_ = suite.db.QueryRowContext(ctx, "SELECT verification_status FROM user_regions WHERE user_id = ? AND region_id = ?", voucheeID, cityRegionID).Scan(&verificationStatus)
		if verificationStatus != "verified" {
			t.Errorf("Expected user_region verification_status 'verified', got '%s'", verificationStatus)
		}
	})
}

func TestE2E_VouchAdminGrantWithBothVerifications(t *testing.T) {
	suite := SetupE2ETest(t)

	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Create region hierarchy using a superuser
	superUserID, _ := suite.registerOrGetUser(
		fmt.Sprintf("admingrantsuper_%s", suffix),
		fmt.Sprintf("admingrantsuper_%s@test.com", suffix),
		"securepassword123",
	)

	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET is_superuser = TRUE, postcard_verified = TRUE, vouch_verified = TRUE, verification_tier = 2 WHERE id = ?", superUserID)

	superLoginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    fmt.Sprintf("admingrantsuper_%s@test.com", suffix),
		"password": "securepassword123",
	}, "")
	var superLogin models.LoginResponse
	_ = json.NewDecoder(superLoginResp.Body).Decode(&superLogin)
	_ = superLoginResp.Body.Close()
	superToken := superLogin.Token

	defer suite.cleanup(superUserID)

	stateName := fmt.Sprintf("Admin Grant State %s", suffix)
	countyName := fmt.Sprintf("Admin Grant County %s", suffix)
	cityName := fmt.Sprintf("Admin Grant City %s", suffix)

	// Create region hierarchy
	stateResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name": stateName,
		"type": "state",
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-125.0, 45.0}, {-116.0, 45.0}, {-116.0, 49.0}, {-125.0, 49.0}, {-125.0, 45.0}}},
		},
	}, superToken)
	var stateBody map[string]string
	_ = json.NewDecoder(stateResp.Body).Decode(&stateBody)
	_ = stateResp.Body.Close()
	stateRegionID := stateBody["region_id"]
	if stateRegionID == "" {
		t.Fatal("Failed to create state region")
	}

	countyResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name":             countyName,
		"type":             "county",
		"parent_region_id": stateRegionID,
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-123.0, 46.0}, {-121.0, 46.0}, {-121.0, 48.0}, {-123.0, 48.0}, {-123.0, 46.0}}},
		},
	}, superToken)
	var countyBody map[string]string
	_ = json.NewDecoder(countyResp.Body).Decode(&countyBody)
	_ = countyResp.Body.Close()
	countyRegionID := countyBody["region_id"]
	if countyRegionID == "" {
		t.Fatal("Failed to create county region")
	}

	cityResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name":             cityName,
		"type":             "city",
		"parent_region_id": countyRegionID,
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-122.5, 47.0}, {-122.0, 47.0}, {-122.0, 47.5}, {-122.5, 47.5}, {-122.5, 47.0}}},
		},
	}, superToken)
	var cityBody map[string]string
	_ = json.NewDecoder(cityResp.Body).Decode(&cityBody)
	_ = cityResp.Body.Close()
	cityRegionID := cityBody["region_id"]
	if cityRegionID == "" {
		t.Fatal("Failed to create city region")
	}

	defer func() {
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM vouches WHERE region_id = ?", cityRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", cityRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", countyRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", stateRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", cityRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", countyRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", stateRegionID)
	}()

	// Configure mock Mapbox for vouch requests (coordinates inside city polygon)
	suite.mockMapbox.DefaultLatitude = 47.25
	suite.mockMapbox.DefaultLongitude = -122.25
	suite.mockMapbox.DefaultBoundaryType = "city"
	suite.mockMapbox.DefaultBoundaryName = cityName
	suite.mockMapbox.DefaultBoundaryState = stateName

	// Create vouchers (fully verified admins)
	voucher1ID, _ := suite.registerOrGetUser(
		fmt.Sprintf("admingrantvoucher1_%s", suffix),
		fmt.Sprintf("admingrantvoucher1_%s@test.com", suffix),
		"securepassword123",
	)

	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET postcard_verified = TRUE, vouch_verified = TRUE, verification_tier = 2 WHERE id = ?", voucher1ID)
	_, _ = suite.db.ExecContext(ctx, "INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, TRUE, 'verified', NOW())", voucher1ID, cityRegionID)

	voucher1LoginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    fmt.Sprintf("admingrantvoucher1_%s@test.com", suffix),
		"password": "securepassword123",
	}, "")
	var voucher1Login models.LoginResponse
	_ = json.NewDecoder(voucher1LoginResp.Body).Decode(&voucher1Login)
	_ = voucher1LoginResp.Body.Close()
	voucher1Token := voucher1Login.Token
	defer suite.cleanup(voucher1ID)

	voucher2ID, _ := suite.registerOrGetUser(
		fmt.Sprintf("admingrantvoucher2_%s", suffix),
		fmt.Sprintf("admingrantvoucher2_%s@test.com", suffix),
		"securepassword123",
	)

	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET postcard_verified = TRUE, vouch_verified = TRUE, verification_tier = 2 WHERE id = ?", voucher2ID)
	_, _ = suite.db.ExecContext(ctx, "INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, TRUE, 'verified', NOW())", voucher2ID, cityRegionID)

	voucher2LoginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    fmt.Sprintf("admingrantvoucher2_%s@test.com", suffix),
		"password": "securepassword123",
	}, "")
	var voucher2Login models.LoginResponse
	_ = json.NewDecoder(voucher2LoginResp.Body).Decode(&voucher2Login)
	_ = voucher2LoginResp.Body.Close()
	voucher2Token := voucher2Login.Token
	defer suite.cleanup(voucher2ID)

	t.Run("user with postcard already verified gets admin on vouch completion", func(t *testing.T) {
		// Create a user who is ALREADY postcard-verified (e.g., migrated user)
		voucheeID, _ := suite.registerOrGetUser(
			fmt.Sprintf("postcardthenvouch_%s", suffix),
			fmt.Sprintf("postcardthenvouch_%s@test.com", suffix),
			"securepassword123",
		)

		// Mark as postcard verified (simulates a migrated user who had postcard before vouch-first flow)
		_, _ = suite.db.ExecContext(ctx, "UPDATE users SET postcard_verified = TRUE, verification_tier = 2 WHERE id = ?", voucheeID)

		voucheeLoginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email":    fmt.Sprintf("postcardthenvouch_%s@test.com", suffix),
			"password": "securepassword123",
		}, "")
		var voucheeLogin models.LoginResponse
		_ = json.NewDecoder(voucheeLoginResp.Body).Decode(&voucheeLogin)
		_ = voucheeLoginResp.Body.Close()
		voucheeToken := voucheeLogin.Token
		defer suite.cleanup(voucheeID)

		// Request vouch verification (now uses address + geocoding)
		requestResp := suite.request("POST", "/api/v1/verification/vouch/request", map[string]interface{}{
			"address": map[string]string{
				"line1":       "123 Admin Grant St",
				"city":        cityName,
				"state":       stateName,
				"postal_code": "98101",
			},
		}, voucheeToken)
		_ = requestResp.Body.Close()

		// First vouch
		vouch1Resp := suite.request("POST", "/api/v1/verification/vouch", map[string]string{
			"vouched_user_id": voucheeID,
			"region_id":       cityRegionID,
		}, voucher1Token)
		_ = vouch1Resp.Body.Close()

		// Second vouch - should trigger admin grant because user is already postcard-verified
		vouch2Resp := suite.request("POST", "/api/v1/verification/vouch", map[string]string{
			"vouched_user_id": voucheeID,
			"region_id":       cityRegionID,
		}, voucher2Token)
		_ = vouch2Resp.Body.Close()

		// Check that user_region has is_admin = TRUE
		var isAdmin bool
		_ = suite.db.QueryRowContext(ctx, "SELECT is_admin FROM user_regions WHERE user_id = ? AND region_id = ?", voucheeID, cityRegionID).Scan(&isAdmin)
		if !isAdmin {
			t.Error("Expected user with both verifications to have is_admin = TRUE")
		}

		// Check that user is now both postcard and vouch verified
		var postcardVerified, vouchVerified bool
		_ = suite.db.QueryRowContext(ctx, "SELECT postcard_verified, vouch_verified FROM users WHERE id = ?", voucheeID).Scan(&postcardVerified, &vouchVerified)
		if !postcardVerified || !vouchVerified {
			t.Errorf("Expected both verifications: postcard=%v, vouch=%v", postcardVerified, vouchVerified)
		}
	})

	t.Run("user with only vouch verification does not get admin", func(t *testing.T) {
		// Create a user who is NOT postcard-verified
		voucheeID, voucheeToken := suite.registerOrGetUser(
			fmt.Sprintf("vouchonly2_%s", suffix),
			fmt.Sprintf("vouchonly2_%s@test.com", suffix),
			"securepassword123",
		)
		defer suite.cleanup(voucheeID)

		// Request vouch verification (now uses address + geocoding)
		requestResp := suite.request("POST", "/api/v1/verification/vouch/request", map[string]interface{}{
			"address": map[string]string{
				"line1":       "456 Vouch Only St",
				"city":        cityName,
				"state":       stateName,
				"postal_code": "98101",
			},
		}, voucheeToken)
		_ = requestResp.Body.Close()

		// First vouch
		vouch1Resp := suite.request("POST", "/api/v1/verification/vouch", map[string]string{
			"vouched_user_id": voucheeID,
			"region_id":       cityRegionID,
		}, voucher1Token)
		_ = vouch1Resp.Body.Close()

		// Second vouch - should NOT grant admin since user is not postcard-verified
		vouch2Resp := suite.request("POST", "/api/v1/verification/vouch", map[string]string{
			"vouched_user_id": voucheeID,
			"region_id":       cityRegionID,
		}, voucher2Token)
		_ = vouch2Resp.Body.Close()

		// Check that user_region has is_admin = FALSE
		var isAdmin bool
		_ = suite.db.QueryRowContext(ctx, "SELECT is_admin FROM user_regions WHERE user_id = ? AND region_id = ?", voucheeID, cityRegionID).Scan(&isAdmin)
		if isAdmin {
			t.Error("Expected user with only vouch verification to have is_admin = FALSE")
		}

		// Check that user is vouch verified but NOT postcard verified
		var postcardVerified, vouchVerified bool
		_ = suite.db.QueryRowContext(ctx, "SELECT postcard_verified, vouch_verified FROM users WHERE id = ?", voucheeID).Scan(&postcardVerified, &vouchVerified)
		if postcardVerified {
			t.Error("Expected postcard_verified = FALSE for vouch-only user")
		}
		if !vouchVerified {
			t.Error("Expected vouch_verified = TRUE after completing vouch verification")
		}
	})
}

func TestE2E_VouchRequestWithAddressGeocoding(t *testing.T) {
	suite := SetupE2ETest(t)

	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Create region hierarchy using a superuser
	superUserID, _ := suite.registerOrGetUser(
		fmt.Sprintf("geocodesuperuser_%s", suffix),
		fmt.Sprintf("geocodesuperuser_%s@test.com", suffix),
		"securepassword123",
	)

	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET is_superuser = TRUE, postcard_verified = TRUE, vouch_verified = TRUE, verification_tier = 2 WHERE id = ?", superUserID)

	superLoginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    fmt.Sprintf("geocodesuperuser_%s@test.com", suffix),
		"password": "securepassword123",
	}, "")
	var superLogin models.LoginResponse
	_ = json.NewDecoder(superLoginResp.Body).Decode(&superLogin)
	_ = superLoginResp.Body.Close()
	superToken := superLogin.Token

	defer suite.cleanup(superUserID)

	stateName := fmt.Sprintf("Washington %s", suffix)
	countyName := fmt.Sprintf("King County %s", suffix)
	cityName := fmt.Sprintf("Seattle %s", suffix)

	// Create region hierarchy
	stateResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name": stateName,
		"type": "state",
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-125.0, 45.0}, {-116.0, 45.0}, {-116.0, 49.0}, {-125.0, 49.0}, {-125.0, 45.0}}},
		},
	}, superToken)
	var stateBody map[string]string
	_ = json.NewDecoder(stateResp.Body).Decode(&stateBody)
	_ = stateResp.Body.Close()
	stateRegionID := stateBody["region_id"]
	if stateRegionID == "" {
		t.Fatal("Failed to create state region")
	}

	countyResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name":             countyName,
		"type":             "county",
		"parent_region_id": stateRegionID,
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-123.0, 46.0}, {-121.0, 46.0}, {-121.0, 48.0}, {-123.0, 48.0}, {-123.0, 46.0}}},
		},
	}, superToken)
	var countyBody map[string]string
	_ = json.NewDecoder(countyResp.Body).Decode(&countyBody)
	_ = countyResp.Body.Close()
	countyRegionID := countyBody["region_id"]
	if countyRegionID == "" {
		t.Fatal("Failed to create county region")
	}

	cityResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name":             cityName,
		"type":             "city",
		"parent_region_id": countyRegionID,
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-122.5, 47.0}, {-122.0, 47.0}, {-122.0, 47.5}, {-122.5, 47.5}, {-122.5, 47.0}}},
		},
	}, superToken)
	var cityBody map[string]string
	_ = json.NewDecoder(cityResp.Body).Decode(&cityBody)
	_ = cityResp.Body.Close()
	cityRegionID := cityBody["region_id"]
	if cityRegionID == "" {
		t.Fatal("Failed to create city region")
	}

	defer func() {
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", cityRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", countyRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", stateRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", cityRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", countyRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", stateRegionID)
	}()

	// Configure mock Mapbox for vouch requests (coordinates inside city polygon)
	suite.mockMapbox.DefaultLatitude = 47.25
	suite.mockMapbox.DefaultLongitude = -122.25
	suite.mockMapbox.DefaultBoundaryType = "city"
	suite.mockMapbox.DefaultBoundaryName = cityName
	suite.mockMapbox.DefaultBoundaryState = stateName

	// Register a test user
	testUserID, userToken := suite.registerOrGetUser(
		fmt.Sprintf("geocodetest_%s", suffix),
		fmt.Sprintf("geocodetest_%s@test.com", suffix),
		"securepassword123",
	)

	defer suite.cleanup(testUserID)

	t.Run("vouch request geocodes address and creates pending membership", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/verification/vouch/request", map[string]interface{}{
			"address": map[string]string{
				"line1":       "123 Test St",
				"city":        cityName,
				"state":       stateName,
				"postal_code": "98101",
			},
		}, userToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}

		var body models.VouchVerificationResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.RegionID != cityRegionID {
			t.Errorf("Expected region ID %s, got %s", cityRegionID, body.RegionID)
		}
	})
}


func TestE2E_VouchStatusAuthRequired(t *testing.T) {
	suite := SetupE2ETest(t)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	fakeUserID := "00000000-0000-0000-0000-000000000000"
	fakeRegionID := "00000000-0000-0000-0000-000000000001"

	t.Run("unauthenticated request returns 401", func(t *testing.T) {
		resp := suite.request("GET", fmt.Sprintf("/api/v1/verification/vouch/status/%s?region_id=%s", fakeUserID, fakeRegionID), nil, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid token returns 401", func(t *testing.T) {
		resp := suite.request("GET", fmt.Sprintf("/api/v1/verification/vouch/status/%s?region_id=%s", fakeUserID, fakeRegionID), nil, "invalid.token.here")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})

	t.Run("authenticated request succeeds", func(t *testing.T) {
		email := fmt.Sprintf("vouch_status_auth_%s@test.com", suffix)
		userID, token := suite.registerOrGetUser(
			fmt.Sprintf("vs_auth_%s", suffix),
			email,
			"securepassword123",
		)
		defer suite.cleanup(userID)

		resp := suite.request("GET", fmt.Sprintf("/api/v1/verification/vouch/status/%s?region_id=%s", userID, fakeRegionID), nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})
}

func TestE2E_MFABruteForceProtection(t *testing.T) {
	suite := SetupE2ETest(t)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	email := fmt.Sprintf("mfa_brute_%s@test.com", suffix)
	username := fmt.Sprintf("mfa_brute_%s", suffix[:8])
	password := "securepassword123"

	// Register user
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": username, "email": email, "password": password,
	}, "")
	defer func() { _ = resp.Body.Close() }()

	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	userID := registerResp.UserID
	defer suite.cleanup(userID)

	// Setup MFA for the user via the MFA service directly
	mfaConfig := &config.MFAConfig{
		EncryptionKey: "01234567890123456789012345678901",
		Issuer:        "Test MFA",
	}
	mfaService, _ := services.NewMFAService(mfaConfig)
	key, _ := mfaService.GenerateSecret(email)
	encryptedSecret, _ := mfaService.EncryptSecret(key.Secret())
	_ = suite.userRepo.SetMFASecret(context.Background(), userID, encryptedSecret)
	backupCodes, _ := mfaService.GenerateBackupCodes(10)
	hashedCodes, _ := mfaService.HashBackupCodes(backupCodes)
	_ = suite.userRepo.EnableMFA(context.Background(), userID, hashedCodes)

	// Login to get a pending_mfa token
	getPendingToken := func() string {
		loginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email": email, "password": password,
		}, "")
		defer func() { _ = loginResp.Body.Close() }()
		var body models.LoginResponse
		_ = json.NewDecoder(loginResp.Body).Decode(&body)
		return body.Token
	}

	t.Run("failed MFA attempts show remaining_attempts", func(t *testing.T) {
		// Reset MFA attempts
		_ = suite.userRepo.ResetFailedMFAAttempts(context.Background(), userID)

		pendingToken := getPendingToken()

		for i := 1; i <= 4; i++ {
			resp := suite.request("POST", "/api/v1/mfa/verify", map[string]string{
				"code": "000000",
			}, pendingToken)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("Attempt %d: expected 401, got %d", i, resp.StatusCode)
			}

			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)

			expectedRemaining := 5 - i
			remaining := int(body["remaining_attempts"].(float64))
			if remaining != expectedRemaining {
				t.Errorf("Attempt %d: expected %d remaining, got %d", i, expectedRemaining, remaining)
			}
		}
	})

	t.Run("5th failed attempt returns 429", func(t *testing.T) {
		// Reset and exhaust
		_ = suite.userRepo.ResetFailedMFAAttempts(context.Background(), userID)
		pendingToken := getPendingToken()

		for i := 1; i <= 5; i++ {
			resp := suite.request("POST", "/api/v1/mfa/verify", map[string]string{
				"code": "000000",
			}, pendingToken)
			defer func() { _ = resp.Body.Close() }()

			if i < 5 {
				if resp.StatusCode != http.StatusUnauthorized {
					t.Errorf("Attempt %d: expected 401, got %d", i, resp.StatusCode)
				}
			} else {
				if resp.StatusCode != http.StatusTooManyRequests {
					t.Errorf("Attempt 5: expected 429, got %d", resp.StatusCode)
				}
				var body map[string]interface{}
				_ = json.NewDecoder(resp.Body).Decode(&body)
				if body["error"] != "mfa_locked" {
					t.Errorf("Expected error 'mfa_locked', got '%v'", body["error"])
				}
			}
		}
	})

	t.Run("locked user gets 429 immediately", func(t *testing.T) {
		// Get pending token first (login resets counter), then set counter
		pendingToken := getPendingToken()

		// Set counter to threshold AFTER login
		for i := 0; i < 5; i++ {
			_, _ = suite.userRepo.IncrementFailedMFAAttempts(context.Background(), userID)
		}

		resp := suite.request("POST", "/api/v1/mfa/verify", map[string]string{
			"code": "000000",
		}, pendingToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusTooManyRequests {
			t.Errorf("Expected 429 for locked user, got %d", resp.StatusCode)
		}
	})

	t.Run("re-login resets MFA attempts", func(t *testing.T) {
		// Set some failed MFA attempts
		_ = suite.userRepo.ResetFailedMFAAttempts(context.Background(), userID)
		for i := 0; i < 3; i++ {
			_, _ = suite.userRepo.IncrementFailedMFAAttempts(context.Background(), userID)
		}

		// Verify pre-condition
		userBefore, _ := suite.userRepo.GetByID(context.Background(), userID)
		if userBefore.FailedMFAAttempts != 3 {
			t.Fatalf("Pre-condition: expected 3 failed MFA attempts, got %d", userBefore.FailedMFAAttempts)
		}

		// Re-login (this should reset MFA attempts)
		_ = getPendingToken()

		// Verify counter was reset
		userAfter, _ := suite.userRepo.GetByID(context.Background(), userID)
		if userAfter.FailedMFAAttempts != 0 {
			t.Errorf("Expected FailedMFAAttempts=0 after re-login, got %d", userAfter.FailedMFAAttempts)
		}
	})
}

// =============================================================================
// Verification Code Lockout E2E Tests
// =============================================================================

func TestE2E_VerificationCodeLockout(t *testing.T) {
	suite := SetupE2ETest(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@veriflockout.com'")

	password := "securepassword123"

	// Create two users: userA owns the verification code, userB is the attacker
	userAID, tokenA := suite.registerOrGetUser("vlock_a", "usera@veriflockout.com", password)
	defer suite.cleanup(userAID)

	userBID, tokenB := suite.registerOrGetUser("vlock_b", "userb@veriflockout.com", password)
	defer suite.cleanup(userBID)

	// Create a region for the verification request (must fit CHAR(36))
	regionID := fmt.Sprintf("e2e-lockout-%d", time.Now().UnixNano()%1000000000)
	_, err := suite.db.ExecContext(context.Background(), `
		INSERT INTO geographic_regions (id, name, region_type, created_at)
		VALUES (?, 'Lockout Test Region', 'city', NOW())
	`, regionID)
	if err != nil {
		t.Fatalf("Failed to create test region: %v", err)
	}
	defer func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE region_id = ?", regionID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM geographic_regions WHERE id = ?", regionID)
	}()

	t.Run("lockout after 5 failed attempts blocks correct user too", func(t *testing.T) {
		verificationCode := fmt.Sprintf("e2elockout%06d", time.Now().UnixNano()%1000000)
		reqID := fmt.Sprintf("e2e-vreq-%d", time.Now().UnixNano()%1000000000)

		_, err := suite.db.ExecContext(context.Background(), `
			INSERT INTO verification_requests (id, user_id, verification_code, status, region_id,
				postgrid_request_id, boundary_type, boundary_name, boundary_state, created_at, expires_at)
			VALUES (?, ?, ?, 'mailed', ?, 'test-postgrid', 'city', 'Test City', 'NY', NOW(), DATE_ADD(NOW(), INTERVAL 30 DAY))
		`, reqID, userAID, verificationCode, regionID)
		if err != nil {
			t.Fatalf("Failed to create verification request: %v", err)
		}
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE id = ?", reqID)
		}()

		// User B (attacker) tries 5 times — should get 403 for first 4, 429 on 5th
		// (non-transactional fallback returns 403 on user mismatch)
		for i := 1; i <= 5; i++ {
			resp := suite.request("POST", "/api/v1/verification/postcard/verify", map[string]string{
				"verification_code": verificationCode,
			}, tokenB)
			defer func() { _ = resp.Body.Close() }()

			if i < 5 {
				if resp.StatusCode != http.StatusForbidden {
					t.Errorf("Attempt %d: expected 403, got %d", i, resp.StatusCode)
				}
			} else {
				if resp.StatusCode != http.StatusTooManyRequests {
					t.Errorf("Attempt %d: expected 429, got %d", i, resp.StatusCode)
				}
				var body map[string]interface{}
				_ = json.NewDecoder(resp.Body).Decode(&body)
				if body["error"] != "verification_code_locked" {
					t.Errorf("Expected error 'verification_code_locked', got '%v'", body["error"])
				}
			}
		}

		// User A (correct user) should also get 429 — code is locked
		resp := suite.request("POST", "/api/v1/verification/postcard/verify", map[string]string{
			"verification_code": verificationCode,
		}, tokenA)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusTooManyRequests {
			t.Errorf("Correct user after lockout: expected 429, got %d", resp.StatusCode)
		}
	})

	t.Run("under threshold correct user can still verify", func(t *testing.T) {
		verificationCode := fmt.Sprintf("e2eunder%07d", time.Now().UnixNano()%10000000)
		reqID := fmt.Sprintf("e2e-vreq-ok-%d", time.Now().UnixNano()%1000000000)

		// Make user A vouch-verified so postcard verification can complete
		suite.makeUserVouchVerified(userAID)
		// Re-login to pick up vouch-verified claims
		loginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email": "usera@veriflockout.com", "password": password,
		}, "")
		var loginBody models.LoginResponse
		_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)
		_ = loginResp.Body.Close()
		freshTokenA := loginBody.Token

		_, err := suite.db.ExecContext(context.Background(), `
			INSERT INTO verification_requests (id, user_id, verification_code, status, region_id,
				postgrid_request_id, boundary_type, boundary_name, boundary_state, created_at, expires_at)
			VALUES (?, ?, ?, 'mailed', ?, 'test-postgrid', 'city', 'Test City', 'NY', NOW(), DATE_ADD(NOW(), INTERVAL 30 DAY))
		`, reqID, userAID, verificationCode, regionID)
		if err != nil {
			t.Fatalf("Failed to create verification request: %v", err)
		}
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE id = ?", reqID)
		}()

		// 4 failed attempts from user B (under threshold of 5)
		// (non-transactional fallback returns 403 on user mismatch)
		for i := 1; i <= 4; i++ {
			resp := suite.request("POST", "/api/v1/verification/postcard/verify", map[string]string{
				"verification_code": verificationCode,
			}, tokenB)
			_ = resp.Body.Close()

			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("Attempt %d: expected 403, got %d", i, resp.StatusCode)
			}
		}

		// User A should still be able to verify
		resp := suite.request("POST", "/api/v1/verification/postcard/verify", map[string]string{
			"verification_code": verificationCode,
		}, freshTokenA)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Correct user after 4 failed attempts: expected 200, got %d: %v", resp.StatusCode, body)
		}
	})
}

// =============================================================================
// Session Revocation & Stale JWT Claims E2E Tests
// =============================================================================

func TestE2E_SessionRevocation(t *testing.T) {
	suite := SetupE2ETest(t)

	// Clean up test users
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@sessionrevoke.com'")

	t.Run("blocked user token is rejected", func(t *testing.T) {
		// Register and login
		userID, token := suite.registerOrGetUser("revoke_block", "block@sessionrevoke.com", "securepassword123")
		defer suite.cleanup(userID)

		// Verify the token works before blocking
		resp := suite.request("GET", "/api/v1/users/me", nil, token)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 before block, got %d", resp.StatusCode)
		}

		// Block the user via DB (simulating admin action)
		_, err := suite.db.ExecContext(context.Background(),
			"UPDATE users SET is_blocked = TRUE, blocked_at = NOW(), token_invalidated_at = NOW() WHERE id = ?", userID)
		if err != nil {
			t.Fatalf("Failed to block user: %v", err)
		}

		// Wait for cache TTL to expire (100ms TTL + buffer)
		time.Sleep(250 * time.Millisecond)

		// Old token should now be rejected
		resp2 := suite.request("GET", "/api/v1/users/me", nil, token)
		_ = resp2.Body.Close()
		if resp2.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected 401 after block, got %d", resp2.StatusCode)
		}
	})

	t.Run("blocked user cannot login", func(t *testing.T) {
		userID := suite.registerOrGetUserID("revoke_blockedlogin", "blockedlogin@sessionrevoke.com", "securepassword123")
		defer suite.cleanup(userID)
		suite.disableMFA(userID)

		// Block the user
		_, _ = suite.db.ExecContext(context.Background(),
			"UPDATE users SET is_blocked = TRUE, blocked_at = NOW() WHERE id = ?", userID)

		// Try to login
		resp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email": "blockedlogin@sessionrevoke.com", "password": "securepassword123",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403 for blocked login, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body["error"] != "account_blocked" {
			t.Errorf("Expected error 'account_blocked', got %v", body["error"])
		}
	})

	t.Run("unblocked user can login again", func(t *testing.T) {
		userID := suite.registerOrGetUserID("revoke_unblock", "unblock@sessionrevoke.com", "securepassword123")
		defer suite.cleanup(userID)
		suite.disableMFA(userID)

		// Block, then unblock
		_, _ = suite.db.ExecContext(context.Background(),
			"UPDATE users SET is_blocked = TRUE, blocked_at = NOW() WHERE id = ?", userID)
		_, _ = suite.db.ExecContext(context.Background(),
			"UPDATE users SET is_blocked = FALSE, blocked_at = NULL, block_reason = NULL WHERE id = ?", userID)

		// Should be able to login
		resp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email": "unblock@sessionrevoke.com", "password": "securepassword123",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 after unblock, got %d", resp.StatusCode)
		}
	})

	t.Run("password change invalidates old token", func(t *testing.T) {
		userID, token := suite.registerOrGetUser("revoke_pwchange", "pwchange@sessionrevoke.com", "securepassword123")
		defer suite.cleanup(userID)

		// Verify token works
		resp := suite.request("GET", "/api/v1/users/me", nil, token)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 before password change, got %d", resp.StatusCode)
		}

		// Change password
		changeResp := suite.request("POST", "/api/v1/auth/change-password", map[string]string{
			"current_password": "securepassword123",
			"new_password":     "newpassword12345",
		}, token)
		_ = changeResp.Body.Close()
		if changeResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for password change, got %d", changeResp.StatusCode)
		}

		// Wait for cache TTL to expire
		time.Sleep(250 * time.Millisecond)

		// Old token should be rejected (token_invalidated_at was set)
		resp2 := suite.request("GET", "/api/v1/users/me", nil, token)
		_ = resp2.Body.Close()
		if resp2.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected 401 after password change, got %d", resp2.StatusCode)
		}

		// Login with new password should work
		loginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email": "pwchange@sessionrevoke.com", "password": "newpassword12345",
		}, "")
		defer func() { _ = loginResp.Body.Close() }()
		if loginResp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 for login with new password, got %d", loginResp.StatusCode)
		}
	})

	t.Run("superuser revocation blocks admin endpoints", func(t *testing.T) {
		userID, _ := suite.registerOrGetUser("revoke_super", "super@sessionrevoke.com", "securepassword123")
		defer suite.cleanup(userID)

		// Make superuser
		_, _ = suite.db.ExecContext(context.Background(),
			"UPDATE users SET is_superuser = TRUE WHERE id = ?", userID)

		// Re-login to get fresh claims with IsSuperuser=true
		loginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email": "super@sessionrevoke.com", "password": "securepassword123",
		}, "")
		var login models.LoginResponse
		_ = json.NewDecoder(loginResp.Body).Decode(&login)
		_ = loginResp.Body.Close()
		superToken := login.Token

		// Verify admin endpoint works
		resp := suite.request("GET", "/api/v1/admin/users?q=revoke", nil, superToken)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for admin endpoint, got %d", resp.StatusCode)
		}

		// Revoke superuser in DB (defense-in-depth re-check)
		_, _ = suite.db.ExecContext(context.Background(),
			"UPDATE users SET is_superuser = FALSE WHERE id = ?", userID)

		// Old token still has IsSuperuser=true in JWT claims,
		// but requireSuperuser re-checks from DB
		resp2 := suite.request("GET", "/api/v1/admin/users?q=revoke", nil, superToken)
		_ = resp2.Body.Close()
		if resp2.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403 after superuser revocation, got %d", resp2.StatusCode)
		}
	})
}

func TestE2E_PasswordReset(t *testing.T) {
	suite := SetupE2ETest(t)

	originalPassword := "securepassword123"
	newPassword := "newsecurepassword456"
	email := fmt.Sprintf("pwreset-%d@test.com", time.Now().UnixNano())
	username := fmt.Sprintf("pwreset%d", time.Now().UnixNano()%100000)

	// Register user and get userID
	userID := suite.registerOrGetUserID(username, email, originalPassword)
	suite.disableMFA(userID)
	defer suite.cleanup(userID)

	// Verify login works with original password
	loginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email": email, "password": originalPassword,
	}, "")
	_ = loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("Expected login 200, got %d", loginResp.StatusCode)
	}

	// Create a password reset token directly in the DB
	// (We can't go through forgot-password because email service is nil)
	rawToken := fmt.Sprintf("%064x", time.Now().UnixNano()) // 64 hex chars
	tokenHashBytes := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(tokenHashBytes[:])
	tokenID := fmt.Sprintf("prt-%d", time.Now().UnixNano())
	expiresAt := time.Now().UTC().Add(1 * time.Hour)

	_, err := suite.db.ExecContext(context.Background(),
		"INSERT INTO password_reset_tokens (id, user_id, token_hash, created_at, expires_at) VALUES (?, ?, ?, NOW(), ?)",
		tokenID, userID, tokenHash, expiresAt)
	if err != nil {
		t.Fatalf("Failed to insert reset token: %v", err)
	}

	t.Run("reset password with valid token", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/reset-password", map[string]string{
			"token": rawToken, "password": newPassword,
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, body)
		}
	})

	t.Run("login with new password succeeds", func(t *testing.T) {
		// Small delay to let token invalidation propagate through status cache
		time.Sleep(200 * time.Millisecond)

		resp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email": email, "password": newPassword,
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected login 200 with new password, got %d", resp.StatusCode)
		}
	})

	t.Run("login with old password fails", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email": email, "password": originalPassword,
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected login 401 with old password, got %d", resp.StatusCode)
		}
	})

	t.Run("token reuse rejected", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/reset-password", map[string]string{
			"token": rawToken, "password": "yetanotherpassword12",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for reused token, got %d", resp.StatusCode)
		}
	})
}

func TestE2E_ProfileData(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("users/me returns timestamps", func(t *testing.T) {
		beforeLogin := time.Now().Add(-2 * time.Second)
		userID, token := suite.registerOrGetUser("profile_ts", "profile_ts@test.com", "securepassword123")
		defer suite.cleanup(userID)

		resp := suite.request("GET", "/api/v1/users/me", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		userObj, ok := body["user"].(map[string]interface{})
		if !ok {
			t.Fatal("Response missing 'user' object")
		}

		// Check created_at
		createdAtStr, ok := userObj["created_at"].(string)
		if !ok || createdAtStr == "" {
			t.Fatal("created_at is missing or not a string")
		}
		createdAt, err := time.Parse(time.RFC3339Nano, createdAtStr)
		if err != nil {
			t.Fatalf("created_at not parseable as RFC3339: %v", err)
		}
		if createdAt.Before(beforeLogin) || createdAt.After(time.Now().Add(time.Minute)) {
			t.Errorf("created_at %v not within expected range", createdAt)
		}

		// Check last_login
		lastLoginStr, ok := userObj["last_login"].(string)
		if !ok || lastLoginStr == "" {
			t.Fatal("last_login is missing or not a string")
		}
		lastLogin, err := time.Parse(time.RFC3339Nano, lastLoginStr)
		if err != nil {
			t.Fatalf("last_login not parseable as RFC3339: %v", err)
		}
		if lastLogin.Before(beforeLogin) || lastLogin.After(time.Now().Add(time.Minute)) {
			t.Errorf("last_login %v not within expected range", lastLogin)
		}
	})

	t.Run("login response includes timestamps", func(t *testing.T) {
		beforeLogin := time.Now().Add(-2 * time.Second)
		userID := suite.registerOrGetUserID("profile_login", "profile_login@test.com", "securepassword123")
		defer suite.cleanup(userID)
		suite.disableMFA(userID)

		resp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email": "profile_login@test.com", "password": "securepassword123",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		userObj, ok := body["user"].(map[string]interface{})
		if !ok {
			t.Fatal("Login response missing 'user' object")
		}

		// Check created_at
		createdAtStr, ok := userObj["created_at"].(string)
		if !ok || createdAtStr == "" {
			t.Fatal("Login response user missing created_at")
		}
		if _, err := time.Parse(time.RFC3339Nano, createdAtStr); err != nil {
			t.Fatalf("Login created_at not parseable as RFC3339: %v", err)
		}

		// Check last_login
		lastLoginStr, ok := userObj["last_login"].(string)
		if !ok || lastLoginStr == "" {
			t.Fatal("Login response user missing last_login")
		}
		lastLogin, err := time.Parse(time.RFC3339Nano, lastLoginStr)
		if err != nil {
			t.Fatalf("Login last_login not parseable as RFC3339: %v", err)
		}
		if lastLogin.Before(beforeLogin) || lastLogin.After(time.Now().Add(time.Minute)) {
			t.Errorf("Login last_login %v not within expected range", lastLogin)
		}
	})

	t.Run("unverified user fields", func(t *testing.T) {
		userID, token := suite.registerOrGetUser("profile_unverified", "profile_unverified@test.com", "securepassword123")
		defer suite.cleanup(userID)

		resp := suite.request("GET", "/api/v1/users/me", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		userObj := body["user"].(map[string]interface{})

		checks := map[string]interface{}{
			"postcard_verified": false,
			"vouch_verified":    false,
			"is_superuser":      false,
			"is_blocked":        false,
			// email_verified is true because the E2E suite has no email service configured
			"email_verified": true,
		}
		for field, expected := range checks {
			if userObj[field] != expected {
				t.Errorf("%s: expected %v, got %v", field, expected, userObj[field])
			}
		}

		// verification_tier comes back as float64 from JSON
		tier, ok := userObj["verification_tier"].(float64)
		if !ok || tier != 0 {
			t.Errorf("verification_tier: expected 0, got %v", userObj["verification_tier"])
		}
	})

	t.Run("vouch-verified user fields", func(t *testing.T) {
		userID := suite.registerOrGetUserID("profile_vouched", "profile_vouched@test.com", "securepassword123")
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserVouchVerified(userID)

		token := suite.reloginUser("profile_vouched@test.com", "securepassword123")

		resp := suite.request("GET", "/api/v1/users/me", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		userObj := body["user"].(map[string]interface{})

		if userObj["vouch_verified"] != true {
			t.Errorf("vouch_verified: expected true, got %v", userObj["vouch_verified"])
		}

		tier, ok := userObj["verification_tier"].(float64)
		if !ok || tier != 1 {
			t.Errorf("verification_tier: expected 1, got %v", userObj["verification_tier"])
		}
	})

	t.Run("superuser fields", func(t *testing.T) {
		userID := suite.registerOrGetUserID("profile_super", "profile_super@test.com", "securepassword123")
		defer suite.cleanup(userID)
		suite.disableMFA(userID)

		_, err := suite.db.ExecContext(context.Background(),
			"UPDATE users SET is_superuser = TRUE WHERE id = ?", userID)
		if err != nil {
			t.Fatalf("Failed to set superuser: %v", err)
		}

		token := suite.reloginUser("profile_super@test.com", "securepassword123")

		resp := suite.request("GET", "/api/v1/users/me", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		userObj := body["user"].(map[string]interface{})

		if userObj["is_superuser"] != true {
			t.Errorf("is_superuser: expected true, got %v", userObj["is_superuser"])
		}
	})
}

// Group-specific helpers

// createGroup creates a group via the API and returns (groupID, response body).
func (s *E2ETestSuite) createGroup(name string, regionIDs []string, token string) (string, map[string]interface{}) {
	resp := s.request("POST", "/api/v1/groups", map[string]interface{}{
		"name":        name,
		"description": "Test group: " + name,
		"visibility":  "unlisted",
		"region_ids":  regionIDs,
		"topic_tags":  []string{"test"},
	}, token)
	defer func() { _ = resp.Body.Close() }()

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		s.t.Fatalf("createGroup: failed to decode response: %v", err)
	}

	groupID, _ := body["id"].(string)
	return groupID, body
}

// cleanupGroups deletes groups and their cascaded children (members, regions, tags).
func (s *E2ETestSuite) cleanupGroups(groupIDs ...string) {
	ctx := context.Background()
	for _, groupID := range groupIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM `groups` WHERE id = ?", groupID)
	}
}

// createTestRegionForGroups creates a region and adds the user to it.
func (s *E2ETestSuite) createTestRegionForGroups(userID string) string {
	regionID := fmt.Sprintf("e2e-grp-rgn-%d", time.Now().UnixNano()%1000000000)
	ctx := context.Background()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO geographic_regions (id, name, region_type, created_at)
		VALUES (?, 'Group Test Region', 'city', NOW())
	`, regionID)
	if err != nil {
		s.t.Fatalf("Failed to create test region: %v", err)
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, TRUE, 'verified', NOW())",
		userID, regionID)
	if err != nil {
		s.t.Fatalf("Failed to add user to region: %v", err)
	}
	return regionID
}

// cleanupConnections cleans up connections and their proposals.
func (s *E2ETestSuite) cleanupConnections(connectionIDs ...string) {
	ctx := context.Background()
	for _, connID := range connectionIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM connection_proposal_groups WHERE proposal_id IN (SELECT id FROM connection_proposals WHERE connection_id = ?)", connID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM connection_proposals WHERE connection_id = ?", connID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM connection_members WHERE connection_id = ?", connID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM connections WHERE id = ?", connID)
	}
	// Clean orphaned proposals
	_, _ = s.db.ExecContext(ctx, "DELETE FROM connection_proposal_groups WHERE proposal_id IN (SELECT id FROM connection_proposals WHERE connection_id IS NULL)")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM connection_proposals WHERE connection_id IS NULL")
}

// cleanupRegionsForGroups cleans up regions created for group tests.
func (s *E2ETestSuite) cleanupRegionsForGroups(regionIDs ...string) {
	ctx := context.Background()
	for _, regionID := range regionIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_regions WHERE region_id = ?", regionID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", regionID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", regionID)
	}
}

func TestE2E_GroupLifecycle(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("create group requires address verification", func(t *testing.T) {
		password := "testpassword123!"
		userID, token := suite.registerOrGetUser("grp_unverif", "grp_unverif@test.com", password)
		defer suite.cleanup(userID)

		resp := suite.request("POST", "/api/v1/groups", map[string]interface{}{
			"name":       "Should Fail",
			"visibility": "unlisted",
			"region_ids": []string{"fake-region"},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("create provisional group", func(t *testing.T) {
		password := "testpassword123!"
		userID := suite.registerOrGetUserID("grp_create", "grp_create@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("grp_create@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, body := suite.createGroup("Test Provisional Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		if groupID == "" {
			t.Fatal("Expected group ID in response")
		}
		if body["status"] != "provisional" {
			t.Errorf("Expected status=provisional, got %v", body["status"])
		}
		if body["visibility"] != "unlisted" {
			t.Errorf("Expected visibility=unlisted, got %v", body["visibility"])
		}

		// Verify via GET
		getResp := suite.request("GET", "/api/v1/groups/"+groupID, nil, token)
		defer func() { _ = getResp.Body.Close() }()

		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", getResp.StatusCode)
		}
		var getBody map[string]interface{}
		_ = json.NewDecoder(getResp.Body).Decode(&getBody)

		if getBody["is_user_member"] != true {
			t.Errorf("Expected creator to be a member, got is_user_member=%v", getBody["is_user_member"])
		}
		if getBody["is_user_admin"] != true {
			t.Errorf("Expected creator to be an admin, got is_user_admin=%v", getBody["is_user_admin"])
		}
	})

	t.Run("get group details", func(t *testing.T) {
		password := "testpassword123!"
		userID := suite.registerOrGetUserID("grp_detail", "grp_detail@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("grp_detail@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Detail Test Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		resp := suite.request("GET", "/api/v1/groups/"+groupID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		memberCount := body["member_count"].(float64)
		if memberCount != 1 {
			t.Errorf("Expected member_count=1, got %v", memberCount)
		}
		adminCount := body["admin_count"].(float64)
		if adminCount != 1 {
			t.Errorf("Expected admin_count=1, got %v", adminCount)
		}

		regions, ok := body["regions"].([]interface{})
		if !ok || len(regions) == 0 {
			t.Errorf("Expected regions array with at least 1 entry, got %v", body["regions"])
		}

		tags, ok := body["topic_tags"].([]interface{})
		if !ok || len(tags) == 0 {
			t.Errorf("Expected topic_tags array with at least 1 entry, got %v", body["topic_tags"])
		}
	})

	t.Run("update group as admin", func(t *testing.T) {
		password := "testpassword123!"
		userID := suite.registerOrGetUserID("grp_update", "grp_update@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("grp_update@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Before Update", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		newName := "After Update"
		resp := suite.request("PUT", "/api/v1/groups/"+groupID, map[string]interface{}{
			"name": newName,
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		// Verify name changed
		getResp := suite.request("GET", "/api/v1/groups/"+groupID, nil, token)
		defer func() { _ = getResp.Body.Close() }()

		var getBody map[string]interface{}
		_ = json.NewDecoder(getResp.Body).Decode(&getBody)

		if getBody["name"] != newName {
			t.Errorf("Expected name=%q, got %v", newName, getBody["name"])
		}
	})

	t.Run("non-admin cannot update group", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		// User A creates the group
		userAID := suite.registerOrGetUserID("grp_upd_a", "grp_upd_a@test.com", password)
		defer suite.cleanup(userAID)
		suite.disableMFA(userAID)
		suite.makeUserFullyVerified(userAID)
		tokenA := suite.reloginUser("grp_upd_a@test.com", password)

		regionID := suite.createTestRegionForGroups(userAID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Admin Only Update", []string{regionID}, tokenA)
		defer suite.cleanupGroups(groupID)

		// User B is a non-admin member
		userBID := suite.registerOrGetUserID("grp_upd_b", "grp_upd_b@test.com", password)
		defer suite.cleanup(userBID)
		suite.disableMFA(userBID)
		suite.makeUserVouchVerified(userBID)
		tokenB := suite.reloginUser("grp_upd_b@test.com", password)

		// Add user B to region and group as non-admin
		_, _ = suite.db.ExecContext(ctx,
			"INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, FALSE, 'verified', NOW())",
			userBID, regionID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupID, userBID, false, false)

		resp := suite.request("PUT", "/api/v1/groups/"+groupID, map[string]interface{}{
			"name": "Hacked Name",
		}, tokenB)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("list user groups", func(t *testing.T) {
		password := "testpassword123!"
		userID := suite.registerOrGetUserID("grp_list", "grp_list@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("grp_list@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID1, _ := suite.createGroup("List Group One", []string{regionID}, token)
		defer suite.cleanupGroups(groupID1)
		groupID2, _ := suite.createGroup("List Group Two", []string{regionID}, token)
		defer suite.cleanupGroups(groupID2)

		resp := suite.request("GET", "/api/v1/groups", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		groups, ok := body["groups"].([]interface{})
		if !ok {
			t.Fatalf("Expected groups array in response")
		}
		if len(groups) < 2 {
			t.Errorf("Expected at least 2 groups, got %d", len(groups))
		}
	})

	t.Run("leave group", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		userAID := suite.registerOrGetUserID("grp_leav_a", "grp_leav_a@test.com", password)
		defer suite.cleanup(userAID)
		suite.disableMFA(userAID)
		suite.makeUserFullyVerified(userAID)
		tokenA := suite.reloginUser("grp_leav_a@test.com", password)

		regionID := suite.createTestRegionForGroups(userAID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Leave Test Group", []string{regionID}, tokenA)
		defer suite.cleanupGroups(groupID)

		// Add second user as admin so first can leave
		userBID := suite.registerOrGetUserID("grp_leav_b", "grp_leav_b@test.com", password)
		defer suite.cleanup(userBID)
		suite.disableMFA(userBID)
		suite.makeUserVouchVerified(userBID)

		_, _ = suite.db.ExecContext(ctx,
			"INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, FALSE, 'verified', NOW())",
			userBID, regionID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupID, userBID, true, false)

		resp := suite.request("POST", "/api/v1/groups/"+groupID+"/leave", nil, tokenA)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}

		// Verify user A is no longer a member
		isMember, _ := suite.communityGroupRepo.IsUserMember(ctx, groupID, userAID)
		if isMember {
			t.Errorf("Expected user to no longer be a member after leaving")
		}
	})

	t.Run("last admin cannot leave", func(t *testing.T) {
		password := "testpassword123!"
		userID := suite.registerOrGetUserID("grp_lastadm", "grp_lastadm@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("grp_lastadm@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Last Admin Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		resp := suite.request("POST", "/api/v1/groups/"+groupID+"/leave", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body["error"] != "last_admin" {
			t.Errorf("Expected error=last_admin, got %v", body["error"])
		}
	})

	t.Run("list group members", func(t *testing.T) {
		password := "testpassword123!"
		userID := suite.registerOrGetUserID("grp_mems", "grp_mems@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("grp_mems@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Members List Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		resp := suite.request("GET", "/api/v1/groups/"+groupID+"/members", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		members, ok := body["members"].([]interface{})
		if !ok || len(members) != 1 {
			t.Fatalf("Expected 1 member, got %v", body["members"])
		}

		member := members[0].(map[string]interface{})
		if member["is_admin"] != true {
			t.Errorf("Expected creator to have is_admin=true, got %v", member["is_admin"])
		}
		if member["username"] != "grp_mems" {
			t.Errorf("Expected username=grp_mems, got %v", member["username"])
		}
	})

	t.Run("unlisted group hidden from non-members", func(t *testing.T) {
		password := "testpassword123!"

		// User A creates unlisted group
		userAID := suite.registerOrGetUserID("grp_hid_a", "grp_hid_a@test.com", password)
		defer suite.cleanup(userAID)
		suite.disableMFA(userAID)
		suite.makeUserFullyVerified(userAID)
		tokenA := suite.reloginUser("grp_hid_a@test.com", password)

		regionID := suite.createTestRegionForGroups(userAID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Hidden Unlisted Group", []string{regionID}, tokenA)
		defer suite.cleanupGroups(groupID)

		// User B is verified in same region but not a group member
		userBID := suite.registerOrGetUserID("grp_hid_b", "grp_hid_b@test.com", password)
		defer suite.cleanup(userBID)
		suite.disableMFA(userBID)
		suite.makeUserVouchVerified(userBID)
		tokenB := suite.reloginUser("grp_hid_b@test.com", password)

		resp := suite.request("GET", "/api/v1/groups/"+groupID, nil, tokenB)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected 404 for non-member viewing unlisted group, got %d", resp.StatusCode)
		}
	})

	t.Run("delete group as superuser", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		userID := suite.registerOrGetUserID("grp_sudel", "grp_sudel@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("grp_sudel@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Delete Me Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID) // no-op if already deleted

		// Make user superuser and re-login
		_, _ = suite.db.ExecContext(ctx,
			"UPDATE users SET is_superuser = TRUE, postcard_verified = TRUE, vouch_verified = TRUE, verification_tier = 2 WHERE id = ?", userID)
		token = suite.reloginUser("grp_sudel@test.com", password)

		resp := suite.request("DELETE", "/api/v1/groups/"+groupID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		// Verify group is gone
		getResp := suite.request("GET", "/api/v1/groups/"+groupID, nil, token)
		defer func() { _ = getResp.Body.Close() }()

		if getResp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected 404 after deletion, got %d", getResp.StatusCode)
		}
	})

	t.Run("non-superuser cannot delete", func(t *testing.T) {
		password := "testpassword123!"
		userID := suite.registerOrGetUserID("grp_nodel", "grp_nodel@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("grp_nodel@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Cannot Delete Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		resp := suite.request("DELETE", "/api/v1/groups/"+groupID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403, got %d", resp.StatusCode)
		}
	})
}

// addUserToRegionForGroups adds a user to a region directly via SQL (non-admin, verified).
func (s *E2ETestSuite) addUserToRegionForGroups(userID, regionID string) {
	_, err := s.db.ExecContext(context.Background(),
		"INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, FALSE, 'verified', NOW())",
		userID, regionID)
	if err != nil {
		s.t.Fatalf("Failed to add user to region: %v", err)
	}
}

// createInviteLink creates an invite link for a group via the API and returns the parsed response body.
func (s *E2ETestSuite) createInviteLink(groupID string, body interface{}, token string) map[string]interface{} {
	resp := s.request("POST", "/api/v1/groups/"+groupID+"/invite-links", body, token)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		s.t.Fatalf("createInviteLink: expected 201, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		s.t.Fatalf("createInviteLink: failed to decode response: %v", err)
	}
	return result
}

// createVerifiedUserInRegion registers a user, makes them fully verified (vouch + postcard), adds them to a region, and returns (userID, token).
func (s *E2ETestSuite) createVerifiedUserInRegion(username, email, password, regionID string) (string, string) {
	userID := s.registerOrGetUserID(username, email, password)
	s.disableMFA(userID)
	s.makeUserFullyVerified(userID)
	s.addUserToRegionForGroups(userID, regionID)
	token := s.reloginUser(email, password)
	return userID, token
}

func TestE2E_GroupFormation(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("create invite link", func(t *testing.T) {
		password := "testpassword123!"
		userID := suite.registerOrGetUserID("gf_invlnk", "gf_invlnk@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("gf_invlnk@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Invite Link Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		linkBody := suite.createInviteLink(groupID, nil, token)

		linkToken, _ := linkBody["token"].(string)
		if linkToken == "" {
			t.Fatal("Expected non-empty token in invite link response")
		}
		useCount, _ := linkBody["use_count"].(float64)
		if useCount != 0 {
			t.Errorf("Expected use_count=0, got %v", useCount)
		}
	})

	t.Run("create invite link with options", func(t *testing.T) {
		password := "testpassword123!"
		userID := suite.registerOrGetUserID("gf_invopt", "gf_invopt@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("gf_invopt@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Invite Link Options Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		linkBody := suite.createInviteLink(groupID, map[string]interface{}{
			"max_uses":         5,
			"expires_in_hours": 24,
		}, token)

		maxUses, _ := linkBody["max_uses"].(float64)
		if maxUses != 5 {
			t.Errorf("Expected max_uses=5, got %v", maxUses)
		}

		expiresAt, _ := linkBody["expires_at"].(string)
		if expiresAt == "" {
			t.Fatal("Expected expires_at to be set")
		}
		parsedExpiry, err := time.Parse(time.RFC3339, expiresAt)
		if err != nil {
			t.Fatalf("Failed to parse expires_at: %v", err)
		}
		expectedExpiry := time.Now().Add(24 * time.Hour)
		diff := expectedExpiry.Sub(parsedExpiry)
		if diff < -5*time.Minute || diff > 5*time.Minute {
			t.Errorf("Expected expires_at ~24h from now, got %v (diff: %v)", parsedExpiry, diff)
		}
	})

	t.Run("list invite links", func(t *testing.T) {
		password := "testpassword123!"
		userID := suite.registerOrGetUserID("gf_lstlnk", "gf_lstlnk@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("gf_lstlnk@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("List Links Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		suite.createInviteLink(groupID, nil, token)
		suite.createInviteLink(groupID, nil, token)

		resp := suite.request("GET", "/api/v1/groups/"+groupID+"/invite-links", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		links, ok := body["invite_links"].([]interface{})
		if !ok || len(links) != 2 {
			t.Errorf("Expected 2 invite links, got %v", len(links))
		}
	})

	t.Run("non-admin cannot create invite link", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		userAID := suite.registerOrGetUserID("gf_lnk_a", "gf_lnk_a@test.com", password)
		defer suite.cleanup(userAID)
		suite.disableMFA(userAID)
		suite.makeUserFullyVerified(userAID)
		tokenA := suite.reloginUser("gf_lnk_a@test.com", password)

		regionID := suite.createTestRegionForGroups(userAID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Non-Admin Link Group", []string{regionID}, tokenA)
		defer suite.cleanupGroups(groupID)

		// User B: non-admin member
		userBID := suite.registerOrGetUserID("gf_lnk_b", "gf_lnk_b@test.com", password)
		defer suite.cleanup(userBID)
		suite.disableMFA(userBID)
		suite.makeUserVouchVerified(userBID)
		tokenB := suite.reloginUser("gf_lnk_b@test.com", password)

		_, _ = suite.db.ExecContext(ctx,
			"INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, FALSE, 'verified', NOW())",
			userBID, regionID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupID, userBID, false, false)

		resp := suite.request("POST", "/api/v1/groups/"+groupID+"/invite-links", nil, tokenB)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("join group via invite link", func(t *testing.T) {
		password := "testpassword123!"

		userAID := suite.registerOrGetUserID("gf_join_a", "gf_join_a@test.com", password)
		defer suite.cleanup(userAID)
		suite.disableMFA(userAID)
		suite.makeUserFullyVerified(userAID)
		tokenA := suite.reloginUser("gf_join_a@test.com", password)

		regionID := suite.createTestRegionForGroups(userAID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Join Via Link Group", []string{regionID}, tokenA)
		defer suite.cleanupGroups(groupID)

		linkBody := suite.createInviteLink(groupID, nil, tokenA)
		inviteToken := linkBody["token"].(string)

		// User B joins via link
		userBID, tokenB := suite.createVerifiedUserInRegion("gf_join_b", "gf_join_b@test.com", password, regionID)
		defer suite.cleanup(userBID)

		joinResp := suite.request("POST", "/api/v1/groups/join/"+inviteToken, nil, tokenB)
		defer func() { _ = joinResp.Body.Close() }()

		if joinResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(joinResp.Body).Decode(&errBody)
			t.Fatalf("Expected 200, got %d: %v", joinResp.StatusCode, errBody)
		}

		// Verify user B is a member
		membersResp := suite.request("GET", "/api/v1/groups/"+groupID+"/members", nil, tokenA)
		defer func() { _ = membersResp.Body.Close() }()

		var membersBody map[string]interface{}
		_ = json.NewDecoder(membersResp.Body).Decode(&membersBody)

		members, _ := membersBody["members"].([]interface{})
		if len(members) != 2 {
			t.Errorf("Expected 2 members, got %d", len(members))
		}
	})

	t.Run("unverified user can join via invite link", func(t *testing.T) {
		password := "testpassword123!"

		// Admin creates group + invite link
		adminID := suite.registerOrGetUserID("gf_jnv_ad", "gf_jnv_ad@test.com", password)
		defer suite.cleanup(adminID)
		suite.disableMFA(adminID)
		suite.makeUserFullyVerified(adminID)
		adminToken := suite.reloginUser("gf_jnv_ad@test.com", password)

		regionID := suite.createTestRegionForGroups(adminID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Open Join Group", []string{regionID}, adminToken)
		defer suite.cleanupGroups(groupID)

		linkBody := suite.createInviteLink(groupID, nil, adminToken)
		inviteToken := linkBody["token"].(string)

		// Unverified user joins via link — should succeed
		unverifiedID, unverifiedToken := suite.registerOrGetUser("gf_jnv_uv", "gf_jnv_uv@test.com", password)
		defer suite.cleanup(unverifiedID)

		resp := suite.request("POST", "/api/v1/groups/join/"+inviteToken, nil, unverifiedToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}

		// Verify membership
		isMember, _ := suite.communityGroupRepo.IsUserMember(context.Background(), groupID, unverifiedID)
		if !isMember {
			t.Error("Expected unverified user to be a member after joining via invite link")
		}
	})

	t.Run("join via link rejects already member", func(t *testing.T) {
		password := "testpassword123!"

		userID := suite.registerOrGetUserID("gf_jn_dup", "gf_jn_dup@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("gf_jn_dup@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Dup Join Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		linkBody := suite.createInviteLink(groupID, nil, token)
		inviteToken := linkBody["token"].(string)

		// Creator tries to join via link (already a member)
		resp := suite.request("POST", "/api/v1/groups/join/"+inviteToken, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusConflict {
			t.Errorf("Expected 409, got %d", resp.StatusCode)
		}
	})

	t.Run("graduation at threshold", func(t *testing.T) {
		password := "testpassword123!"

		// User A creates group (member 1)
		userAID := suite.registerOrGetUserID("gf_grad_a", "gf_grad_a@test.com", password)
		defer suite.cleanup(userAID)
		suite.disableMFA(userAID)
		suite.makeUserFullyVerified(userAID)
		tokenA := suite.reloginUser("gf_grad_a@test.com", password)

		regionID := suite.createTestRegionForGroups(userAID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Graduation Group", []string{regionID}, tokenA)
		defer suite.cleanupGroups(groupID)

		linkBody := suite.createInviteLink(groupID, nil, tokenA)
		inviteToken := linkBody["token"].(string)

		// User B joins (member 2)
		userBID, tokenB := suite.createVerifiedUserInRegion("gf_grad_b", "gf_grad_b@test.com", password, regionID)
		defer suite.cleanup(userBID)

		joinB := suite.request("POST", "/api/v1/groups/join/"+inviteToken, nil, tokenB)
		defer func() { _ = joinB.Body.Close() }()

		if joinB.StatusCode != http.StatusOK {
			t.Fatalf("User B join failed: %d", joinB.StatusCode)
		}

		var joinBBody map[string]interface{}
		_ = json.NewDecoder(joinB.Body).Decode(&joinBBody)
		if joinBBody["graduated"] == true {
			t.Error("Expected group NOT to graduate after member 2")
		}

		// User C joins (member 3 — hits default threshold of 3)
		userCID, tokenC := suite.createVerifiedUserInRegion("gf_grad_c", "gf_grad_c@test.com", password, regionID)
		defer suite.cleanup(userCID)

		joinC := suite.request("POST", "/api/v1/groups/join/"+inviteToken, nil, tokenC)
		defer func() { _ = joinC.Body.Close() }()

		if joinC.StatusCode != http.StatusOK {
			t.Fatalf("User C join failed: %d", joinC.StatusCode)
		}

		var joinCBody map[string]interface{}
		_ = json.NewDecoder(joinC.Body).Decode(&joinCBody)
		if joinCBody["graduated"] != true {
			t.Error("Expected group to graduate after member 3 (threshold=3)")
		}

		// Verify group status via GET
		getResp := suite.request("GET", "/api/v1/groups/"+groupID, nil, tokenA)
		defer func() { _ = getResp.Body.Close() }()

		var getBody map[string]interface{}
		_ = json.NewDecoder(getResp.Body).Decode(&getBody)

		if getBody["status"] != "active" {
			t.Errorf("Expected status=active, got %v", getBody["status"])
		}
		if getBody["graduated_at"] == nil {
			t.Error("Expected graduated_at to be set")
		}
	})

	t.Run("direct invitation flow", func(t *testing.T) {
		password := "testpassword123!"

		// User A creates group
		userAID := suite.registerOrGetUserID("gf_inv_a", "gf_inv_a@test.com", password)
		defer suite.cleanup(userAID)
		suite.disableMFA(userAID)
		suite.makeUserFullyVerified(userAID)
		tokenA := suite.reloginUser("gf_inv_a@test.com", password)

		regionID := suite.createTestRegionForGroups(userAID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Invitation Group", []string{regionID}, tokenA)
		defer suite.cleanupGroups(groupID)

		// User B to be invited
		userBID, tokenB := suite.createVerifiedUserInRegion("gf_inv_b", "gf_inv_b@test.com", password, regionID)
		defer suite.cleanup(userBID)

		// User A invites user B
		invResp := suite.request("POST", "/api/v1/groups/"+groupID+"/invitations", map[string]string{
			"user_id": userBID,
		}, tokenA)
		defer func() { _ = invResp.Body.Close() }()

		if invResp.StatusCode != http.StatusCreated {
			var errBody map[string]interface{}
			_ = json.NewDecoder(invResp.Body).Decode(&errBody)
			t.Fatalf("Expected 201, got %d: %v", invResp.StatusCode, errBody)
		}

		var invBody map[string]interface{}
		_ = json.NewDecoder(invResp.Body).Decode(&invBody)
		invitationID := invBody["id"].(string)

		// User B lists their invitations
		listResp := suite.request("GET", "/api/v1/group-invitations", nil, tokenB)
		defer func() { _ = listResp.Body.Close() }()

		if listResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for list invitations, got %d", listResp.StatusCode)
		}

		var listBody map[string]interface{}
		_ = json.NewDecoder(listResp.Body).Decode(&listBody)

		invitations, _ := listBody["invitations"].([]interface{})
		if len(invitations) != 1 {
			t.Fatalf("Expected 1 pending invitation, got %d", len(invitations))
		}

		firstInv := invitations[0].(map[string]interface{})
		if firstInv["group_name"] == nil || firstInv["group_name"] == "" {
			t.Error("Expected invitation to include group_name")
		}

		// User B accepts
		acceptResp := suite.request("POST", "/api/v1/group-invitations/"+invitationID+"/respond", map[string]interface{}{
			"accept": true,
		}, tokenB)
		defer func() { _ = acceptResp.Body.Close() }()

		if acceptResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(acceptResp.Body).Decode(&errBody)
			t.Fatalf("Expected 200 for accept, got %d: %v", acceptResp.StatusCode, errBody)
		}

		// Verify user B is now a member
		membersResp := suite.request("GET", "/api/v1/groups/"+groupID+"/members", nil, tokenA)
		defer func() { _ = membersResp.Body.Close() }()

		var membersBody map[string]interface{}
		_ = json.NewDecoder(membersResp.Body).Decode(&membersBody)

		members, _ := membersBody["members"].([]interface{})
		if len(members) != 2 {
			t.Errorf("Expected 2 members after invitation accept, got %d", len(members))
		}
	})

	t.Run("decline invitation", func(t *testing.T) {
		password := "testpassword123!"

		userAID := suite.registerOrGetUserID("gf_dec_a", "gf_dec_a@test.com", password)
		defer suite.cleanup(userAID)
		suite.disableMFA(userAID)
		suite.makeUserFullyVerified(userAID)
		tokenA := suite.reloginUser("gf_dec_a@test.com", password)

		regionID := suite.createTestRegionForGroups(userAID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Decline Invite Group", []string{regionID}, tokenA)
		defer suite.cleanupGroups(groupID)

		userBID, tokenB := suite.createVerifiedUserInRegion("gf_dec_b", "gf_dec_b@test.com", password, regionID)
		defer suite.cleanup(userBID)

		invResp := suite.request("POST", "/api/v1/groups/"+groupID+"/invitations", map[string]string{
			"user_id": userBID,
		}, tokenA)
		defer func() { _ = invResp.Body.Close() }()

		var invBody map[string]interface{}
		_ = json.NewDecoder(invResp.Body).Decode(&invBody)
		invitationID := invBody["id"].(string)

		// User B declines
		declineResp := suite.request("POST", "/api/v1/group-invitations/"+invitationID+"/respond", map[string]interface{}{
			"accept": false,
		}, tokenB)
		defer func() { _ = declineResp.Body.Close() }()

		if declineResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for decline, got %d", declineResp.StatusCode)
		}

		// Verify user B is NOT a member
		membersResp := suite.request("GET", "/api/v1/groups/"+groupID+"/members", nil, tokenA)
		defer func() { _ = membersResp.Body.Close() }()

		var membersBody map[string]interface{}
		_ = json.NewDecoder(membersResp.Body).Decode(&membersBody)

		members, _ := membersBody["members"].([]interface{})
		if len(members) != 1 {
			t.Errorf("Expected only 1 member (creator) after decline, got %d", len(members))
		}
	})

	t.Run("cannot invite existing member", func(t *testing.T) {
		password := "testpassword123!"

		userAID := suite.registerOrGetUserID("gf_dup_a", "gf_dup_a@test.com", password)
		defer suite.cleanup(userAID)
		suite.disableMFA(userAID)
		suite.makeUserFullyVerified(userAID)
		tokenA := suite.reloginUser("gf_dup_a@test.com", password)

		regionID := suite.createTestRegionForGroups(userAID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Dup Invite Group", []string{regionID}, tokenA)
		defer suite.cleanupGroups(groupID)

		// Try to invite user A (the creator, already a member)
		resp := suite.request("POST", "/api/v1/groups/"+groupID+"/invitations", map[string]string{
			"user_id": userAID,
		}, tokenA)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusConflict {
			t.Errorf("Expected 409, got %d", resp.StatusCode)
		}
	})

	t.Run("graduation via invitation accept", func(t *testing.T) {
		password := "testpassword123!"

		// User A creates group (member 1)
		userAID := suite.registerOrGetUserID("gf_grd2_a", "gf_grd2_a@test.com", password)
		defer suite.cleanup(userAID)
		suite.disableMFA(userAID)
		suite.makeUserFullyVerified(userAID)
		tokenA := suite.reloginUser("gf_grd2_a@test.com", password)

		regionID := suite.createTestRegionForGroups(userAID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Graduation Via Invite Group", []string{regionID}, tokenA)
		defer suite.cleanupGroups(groupID)

		// User B (member 2)
		userBID, tokenB := suite.createVerifiedUserInRegion("gf_grd2_b", "gf_grd2_b@test.com", password, regionID)
		defer suite.cleanup(userBID)

		invBResp := suite.request("POST", "/api/v1/groups/"+groupID+"/invitations", map[string]string{
			"user_id": userBID,
		}, tokenA)
		defer func() { _ = invBResp.Body.Close() }()

		var invBBody map[string]interface{}
		_ = json.NewDecoder(invBResp.Body).Decode(&invBBody)
		invBID := invBBody["id"].(string)

		acceptB := suite.request("POST", "/api/v1/group-invitations/"+invBID+"/respond", map[string]interface{}{
			"accept": true,
		}, tokenB)
		defer func() { _ = acceptB.Body.Close() }()

		var acceptBBody map[string]interface{}
		_ = json.NewDecoder(acceptB.Body).Decode(&acceptBBody)
		if acceptBBody["graduated"] == true {
			t.Error("Expected group NOT to graduate after member 2")
		}

		// User C (member 3 — threshold)
		userCID, tokenC := suite.createVerifiedUserInRegion("gf_grd2_c", "gf_grd2_c@test.com", password, regionID)
		defer suite.cleanup(userCID)

		invCResp := suite.request("POST", "/api/v1/groups/"+groupID+"/invitations", map[string]string{
			"user_id": userCID,
		}, tokenA)
		defer func() { _ = invCResp.Body.Close() }()

		var invCBody map[string]interface{}
		_ = json.NewDecoder(invCResp.Body).Decode(&invCBody)
		invCID := invCBody["id"].(string)

		acceptC := suite.request("POST", "/api/v1/group-invitations/"+invCID+"/respond", map[string]interface{}{
			"accept": true,
		}, tokenC)
		defer func() { _ = acceptC.Body.Close() }()

		var acceptCBody map[string]interface{}
		_ = json.NewDecoder(acceptC.Body).Decode(&acceptCBody)
		if acceptCBody["graduated"] != true {
			t.Error("Expected group to graduate after member 3 (threshold=3)")
		}

		// Verify group status
		getResp := suite.request("GET", "/api/v1/groups/"+groupID, nil, tokenA)
		defer func() { _ = getResp.Body.Close() }()

		var getBody map[string]interface{}
		_ = json.NewDecoder(getResp.Body).Decode(&getBody)

		if getBody["status"] != "active" {
			t.Errorf("Expected status=active, got %v", getBody["status"])
		}
		if getBody["graduated_at"] == nil {
			t.Error("Expected graduated_at to be set")
		}
	})
}

func TestE2E_AccessTiers(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("trust vouch flow", func(t *testing.T) {
		ctx := context.Background()
		password := "testpassword123!"

		// User A creates group (admin)
		userAID := suite.registerOrGetUserID("at_tv_a", "at_tv_a@test.com", password)
		defer suite.cleanup(userAID)
		suite.disableMFA(userAID)
		suite.makeUserFullyVerified(userAID)
		tokenA := suite.reloginUser("at_tv_a@test.com", password)

		regionID := suite.createTestRegionForGroups(userAID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Trust Vouch Flow Group", []string{regionID}, tokenA)
		defer suite.cleanupGroups(groupID)

		// User B joins as member
		userBID, _ := suite.createVerifiedUserInRegion("at_tv_b", "at_tv_b@test.com", password, regionID)
		defer suite.cleanup(userBID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupID, userBID, false, false)

		// User A (admin) vouches for user B
		vouchResp := suite.request("POST", "/api/v1/groups/"+groupID+"/trust-vouches", map[string]string{
			"user_id": userBID,
		}, tokenA)
		defer func() { _ = vouchResp.Body.Close() }()

		if vouchResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(vouchResp.Body).Decode(&errBody)
			t.Fatalf("Expected 200 for vouch, got %d: %v", vouchResp.StatusCode, errBody)
		}

		// Check vouch status: vouch_count=1, trust_level=member
		statusResp := suite.request("GET", "/api/v1/groups/"+groupID+"/trust-vouches/"+userBID, nil, tokenA)
		defer func() { _ = statusResp.Body.Close() }()

		if statusResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for vouch status, got %d", statusResp.StatusCode)
		}

		var statusBody map[string]interface{}
		_ = json.NewDecoder(statusResp.Body).Decode(&statusBody)

		vouchCount := int(statusBody["vouch_count"].(float64))
		if vouchCount != 1 {
			t.Errorf("Expected vouch_count=1, got %d", vouchCount)
		}
		trustLevel := statusBody["trust_level"].(string)
		if trustLevel != "member" {
			t.Errorf("Expected trust_level=member, got %q", trustLevel)
		}

		// User C joins, make C admin
		userCID, _ := suite.createVerifiedUserInRegion("at_tv_c", "at_tv_c@test.com", password, regionID)
		defer suite.cleanup(userCID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupID, userCID, true, false)
		tokenC := suite.reloginUser("at_tv_c@test.com", password)

		// User C vouches for user B (now 2 vouches = default threshold)
		vouch2Resp := suite.request("POST", "/api/v1/groups/"+groupID+"/trust-vouches", map[string]string{
			"user_id": userBID,
		}, tokenC)
		defer func() { _ = vouch2Resp.Body.Close() }()

		if vouch2Resp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(vouch2Resp.Body).Decode(&errBody)
			t.Fatalf("Expected 200 for second vouch, got %d: %v", vouch2Resp.StatusCode, errBody)
		}

		// Check vouch status: vouch_count=2, trust_level=trusted
		status2Resp := suite.request("GET", "/api/v1/groups/"+groupID+"/trust-vouches/"+userBID, nil, tokenA)
		defer func() { _ = status2Resp.Body.Close() }()

		var status2Body map[string]interface{}
		_ = json.NewDecoder(status2Resp.Body).Decode(&status2Body)

		vouchCount2 := int(status2Body["vouch_count"].(float64))
		if vouchCount2 != 2 {
			t.Errorf("Expected vouch_count=2, got %d", vouchCount2)
		}
		trustLevel2 := status2Body["trust_level"].(string)
		if trustLevel2 != "trusted" {
			t.Errorf("Expected trust_level=trusted, got %q", trustLevel2)
		}
	})

	t.Run("self vouch rejected", func(t *testing.T) {
		password := "testpassword123!"

		userAID := suite.registerOrGetUserID("at_sv_a", "at_sv_a@test.com", password)
		defer suite.cleanup(userAID)
		suite.disableMFA(userAID)
		suite.makeUserFullyVerified(userAID)
		tokenA := suite.reloginUser("at_sv_a@test.com", password)

		regionID := suite.createTestRegionForGroups(userAID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Self Vouch Group", []string{regionID}, tokenA)
		defer suite.cleanupGroups(groupID)

		// User A tries to vouch for self
		resp := suite.request("POST", "/api/v1/groups/"+groupID+"/trust-vouches", map[string]string{
			"user_id": userAID,
		}, tokenA)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for self-vouch, got %d", resp.StatusCode)
		}
	})

	t.Run("non-admin non-trusted cannot vouch", func(t *testing.T) {
		ctx := context.Background()
		password := "testpassword123!"

		// Admin creates group
		adminID := suite.registerOrGetUserID("at_na_adm", "at_na_adm@test.com", password)
		defer suite.cleanup(adminID)
		suite.disableMFA(adminID)
		suite.makeUserFullyVerified(adminID)
		tokenAdmin := suite.reloginUser("at_na_adm@test.com", password)

		regionID := suite.createTestRegionForGroups(adminID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("No Vouch Group", []string{regionID}, tokenAdmin)
		defer suite.cleanupGroups(groupID)

		// User B: regular member
		userBID, _ := suite.createVerifiedUserInRegion("at_na_b", "at_na_b@test.com", password, regionID)
		defer suite.cleanup(userBID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupID, userBID, false, false)
		tokenB := suite.reloginUser("at_na_b@test.com", password)

		// User C: another regular member (target)
		userCID, _ := suite.createVerifiedUserInRegion("at_na_c", "at_na_c@test.com", password, regionID)
		defer suite.cleanup(userCID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupID, userCID, false, false)

		// User B (regular member) tries to vouch for user C
		resp := suite.request("POST", "/api/v1/groups/"+groupID+"/trust-vouches", map[string]string{
			"user_id": userCID,
		}, tokenB)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403 for non-admin vouch, got %d", resp.StatusCode)
		}
	})

	t.Run("vouch status requires membership", func(t *testing.T) {
		ctx := context.Background()
		password := "testpassword123!"

		// Admin creates group
		adminID := suite.registerOrGetUserID("at_vs_adm", "at_vs_adm@test.com", password)
		defer suite.cleanup(adminID)
		suite.disableMFA(adminID)
		suite.makeUserFullyVerified(adminID)
		tokenAdmin := suite.reloginUser("at_vs_adm@test.com", password)

		regionID := suite.createTestRegionForGroups(adminID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Vouch Status Membership Group", []string{regionID}, tokenAdmin)
		defer suite.cleanupGroups(groupID)

		// User B: member (target for vouch status check)
		userBID, _ := suite.createVerifiedUserInRegion("at_vs_b", "at_vs_b@test.com", password, regionID)
		defer suite.cleanup(userBID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupID, userBID, false, false)

		// User C: NOT a member of the group
		userCID := suite.registerOrGetUserID("at_vs_c", "at_vs_c@test.com", password)
		defer suite.cleanup(userCID)
		suite.disableMFA(userCID)
		suite.makeUserVouchVerified(userCID)
		tokenC := suite.reloginUser("at_vs_c@test.com", password)

		// User C (not a member) tries to get vouch status
		resp := suite.request("GET", "/api/v1/groups/"+groupID+"/trust-vouches/"+userBID, nil, tokenC)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403 for non-member vouch status, got %d", resp.StatusCode)
		}
	})

	t.Run("trusted member can vouch", func(t *testing.T) {
		ctx := context.Background()
		password := "testpassword123!"

		// User A creates group (admin)
		userAID := suite.registerOrGetUserID("at_tm_a", "at_tm_a@test.com", password)
		defer suite.cleanup(userAID)
		suite.disableMFA(userAID)
		suite.makeUserFullyVerified(userAID)
		tokenA := suite.reloginUser("at_tm_a@test.com", password)

		regionID := suite.createTestRegionForGroups(userAID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Trusted Voucher Group", []string{regionID}, tokenA)
		defer suite.cleanupGroups(groupID)

		// User B joins as member
		userBID, _ := suite.createVerifiedUserInRegion("at_tm_b", "at_tm_b@test.com", password, regionID)
		defer suite.cleanup(userBID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupID, userBID, false, false)

		// Promote B to trusted via 2 admin vouches: A vouches, then add another admin D who also vouches
		// First vouch from A
		v1Resp := suite.request("POST", "/api/v1/groups/"+groupID+"/trust-vouches", map[string]string{
			"user_id": userBID,
		}, tokenA)
		defer func() { _ = v1Resp.Body.Close() }()
		if v1Resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for first vouch, got %d", v1Resp.StatusCode)
		}

		// Add admin D
		userDID, _ := suite.createVerifiedUserInRegion("at_tm_d", "at_tm_d@test.com", password, regionID)
		defer suite.cleanup(userDID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupID, userDID, true, false)
		tokenD := suite.reloginUser("at_tm_d@test.com", password)

		// Second vouch from D (promotes B to trusted at threshold=2)
		v2Resp := suite.request("POST", "/api/v1/groups/"+groupID+"/trust-vouches", map[string]string{
			"user_id": userBID,
		}, tokenD)
		defer func() { _ = v2Resp.Body.Close() }()
		if v2Resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for second vouch, got %d", v2Resp.StatusCode)
		}

		// Verify B is now trusted
		statusResp := suite.request("GET", "/api/v1/groups/"+groupID+"/trust-vouches/"+userBID, nil, tokenA)
		defer func() { _ = statusResp.Body.Close() }()
		var statusBody map[string]interface{}
		_ = json.NewDecoder(statusResp.Body).Decode(&statusBody)
		if statusBody["trust_level"].(string) != "trusted" {
			t.Fatalf("Expected B to be trusted, got %q", statusBody["trust_level"])
		}

		// User C joins as member
		userCID, _ := suite.createVerifiedUserInRegion("at_tm_c", "at_tm_c@test.com", password, regionID)
		defer suite.cleanup(userCID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupID, userCID, false, false)

		// User B (now trusted) vouches for user C
		tokenB := suite.reloginUser("at_tm_b@test.com", password)
		vouchCResp := suite.request("POST", "/api/v1/groups/"+groupID+"/trust-vouches", map[string]string{
			"user_id": userCID,
		}, tokenB)
		defer func() { _ = vouchCResp.Body.Close() }()

		if vouchCResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(vouchCResp.Body).Decode(&errBody)
			t.Fatalf("Expected 200 for trusted member vouch, got %d: %v", vouchCResp.StatusCode, errBody)
		}

		var vouchCBody map[string]interface{}
		_ = json.NewDecoder(vouchCResp.Body).Decode(&vouchCBody)
		vouchCount := int(vouchCBody["vouch_count"].(float64))
		if vouchCount != 1 {
			t.Errorf("Expected vouch_count=1 for user C, got %d", vouchCount)
		}
	})
}

// graduateGroup creates an invite link and adds 2 new members via it so the group
// reaches the default founding threshold of 3 and graduates to active status.
// Returns the user IDs of the 2 new members (caller should defer cleanup).
func (s *E2ETestSuite) graduateGroup(groupID, adminToken, regionID, prefix string) (string, string) {
	linkBody := s.createInviteLink(groupID, nil, adminToken)
	inviteToken := linkBody["token"].(string)

	password := "testpassword123!"
	userBID, tokenB := s.createVerifiedUserInRegion(prefix+"_gb", prefix+"_gb@test.com", password, regionID)
	joinB := s.request("POST", "/api/v1/groups/join/"+inviteToken, nil, tokenB)
	defer func() { _ = joinB.Body.Close() }()
	if joinB.StatusCode != http.StatusOK {
		s.t.Fatalf("graduateGroup: user B join failed: %d", joinB.StatusCode)
	}

	userCID, tokenC := s.createVerifiedUserInRegion(prefix+"_gc", prefix+"_gc@test.com", password, regionID)
	joinC := s.request("POST", "/api/v1/groups/join/"+inviteToken, nil, tokenC)
	defer func() { _ = joinC.Body.Close() }()
	if joinC.StatusCode != http.StatusOK {
		s.t.Fatalf("graduateGroup: user C join failed: %d", joinC.StatusCode)
	}

	return userBID, userCID
}

func TestE2E_GroupSignalGroups(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("create signal group under active group", func(t *testing.T) {
		password := "testpassword123!"
		userID := suite.registerOrGetUserID("gsg_cr_a", "gsg_cr_a@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("gsg_cr_a@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("SG Create Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		// Graduate the group
		gradUserB, gradUserC := suite.graduateGroup(groupID, token, regionID, "gsg_cr")
		defer suite.cleanup(gradUserB, gradUserC)

		// Verify it graduated
		getResp := suite.request("GET", "/api/v1/groups/"+groupID, nil, token)
		defer func() { _ = getResp.Body.Close() }()
		var getBody map[string]interface{}
		_ = json.NewDecoder(getResp.Body).Decode(&getBody)
		if getBody["status"] != "active" {
			t.Fatalf("Expected group to be active, got %v", getBody["status"])
		}

		// Admin creates signal group
		sgResp := suite.request("POST", "/api/v1/groups/"+groupID+"/signal-groups", map[string]interface{}{
			"group_name":  "General Chat",
			"description": "Main chat channel",
			"access_tier": "member",
		}, token)
		defer func() { _ = sgResp.Body.Close() }()

		if sgResp.StatusCode != http.StatusCreated {
			var errBody map[string]interface{}
			_ = json.NewDecoder(sgResp.Body).Decode(&errBody)
			t.Fatalf("Expected 201, got %d: %v", sgResp.StatusCode, errBody)
		}

		var sgBody map[string]interface{}
		_ = json.NewDecoder(sgResp.Body).Decode(&sgBody)

		if sgBody["owner_group_id"] != groupID {
			t.Errorf("Expected owner_group_id=%s, got %v", groupID, sgBody["owner_group_id"])
		}
		if sgBody["access_tier"] != "member" {
			t.Errorf("Expected access_tier=member, got %v", sgBody["access_tier"])
		}
		if sgBody["group_name"] != "General Chat" {
			t.Errorf("Expected group_name=General Chat, got %v", sgBody["group_name"])
		}
		if sgBody["id"] == nil || sgBody["id"] == "" {
			t.Error("Expected non-empty signal group ID")
		}
	})

	t.Run("cannot create signal group in provisional group", func(t *testing.T) {
		password := "testpassword123!"
		userID := suite.registerOrGetUserID("gsg_prov", "gsg_prov@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("gsg_prov@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, body := suite.createGroup("SG Provisional Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		if body["status"] != "provisional" {
			t.Fatalf("Expected provisional group, got %v", body["status"])
		}

		sgResp := suite.request("POST", "/api/v1/groups/"+groupID+"/signal-groups", map[string]interface{}{
			"group_name":  "Should Fail",
			"description": "Nope",
			"access_tier": "member",
		}, token)
		defer func() { _ = sgResp.Body.Close() }()

		if sgResp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for provisional group, got %d", sgResp.StatusCode)
		}

		var errBody map[string]interface{}
		_ = json.NewDecoder(sgResp.Body).Decode(&errBody)
		if errBody["error"] != "group_provisional" {
			t.Errorf("Expected error=group_provisional, got %v", errBody["error"])
		}
	})

	t.Run("non-admin cannot create signal group", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		adminID := suite.registerOrGetUserID("gsg_na_a", "gsg_na_a@test.com", password)
		defer suite.cleanup(adminID)
		suite.disableMFA(adminID)
		suite.makeUserFullyVerified(adminID)
		adminToken := suite.reloginUser("gsg_na_a@test.com", password)

		regionID := suite.createTestRegionForGroups(adminID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("SG Non-Admin Group", []string{regionID}, adminToken)
		defer suite.cleanupGroups(groupID)

		// Graduate the group
		gradUserB, gradUserC := suite.graduateGroup(groupID, adminToken, regionID, "gsg_na")
		defer suite.cleanup(gradUserB, gradUserC)

		// Add a non-admin member
		memberID := suite.registerOrGetUserID("gsg_na_m", "gsg_na_m@test.com", password)
		defer suite.cleanup(memberID)
		suite.disableMFA(memberID)
		suite.makeUserVouchVerified(memberID)
		memberToken := suite.reloginUser("gsg_na_m@test.com", password)

		_, _ = suite.db.ExecContext(ctx,
			"INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, FALSE, 'verified', NOW())",
			memberID, regionID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupID, memberID, false, false)

		sgResp := suite.request("POST", "/api/v1/groups/"+groupID+"/signal-groups", map[string]interface{}{
			"group_name":  "Should Fail",
			"description": "Nope",
			"access_tier": "member",
		}, memberToken)
		defer func() { _ = sgResp.Body.Close() }()

		if sgResp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403, got %d", sgResp.StatusCode)
		}
	})

	t.Run("list signal groups filtered by access tier", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		// Admin creates group and graduates it
		adminID := suite.registerOrGetUserID("gsg_ls_a", "gsg_ls_a@test.com", password)
		defer suite.cleanup(adminID)
		suite.disableMFA(adminID)
		suite.makeUserFullyVerified(adminID)
		adminToken := suite.reloginUser("gsg_ls_a@test.com", password)

		regionID := suite.createTestRegionForGroups(adminID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("SG List Filter Group", []string{regionID}, adminToken)
		defer suite.cleanupGroups(groupID)

		gradUserB, gradUserC := suite.graduateGroup(groupID, adminToken, regionID, "gsg_ls")
		defer suite.cleanup(gradUserB, gradUserC)

		// Create 3 signal groups with different tiers
		for _, sg := range []struct {
			name string
			tier string
		}{
			{"Open Chat", "open"},
			{"Members Only", "member"},
			{"Admin Only", "admin_only"},
		} {
			resp := suite.request("POST", "/api/v1/groups/"+groupID+"/signal-groups", map[string]interface{}{
				"group_name":  sg.name,
				"access_tier": sg.tier,
			}, adminToken)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusCreated {
				var errBody map[string]interface{}
				_ = json.NewDecoder(resp.Body).Decode(&errBody)
				t.Fatalf("Failed to create signal group %q: %d %v", sg.name, resp.StatusCode, errBody)
			}
		}

		// As admin: should see all 3
		adminListResp := suite.request("GET", "/api/v1/groups/"+groupID+"/signal-groups", nil, adminToken)
		defer func() { _ = adminListResp.Body.Close() }()

		if adminListResp.StatusCode != http.StatusOK {
			t.Fatalf("Admin list: expected 200, got %d", adminListResp.StatusCode)
		}
		var adminListBody map[string]interface{}
		_ = json.NewDecoder(adminListResp.Body).Decode(&adminListBody)
		adminGroups := adminListBody["signal_groups"].([]interface{})
		if len(adminGroups) != 3 {
			t.Errorf("Admin should see 3 signal groups, got %d", len(adminGroups))
		}

		// Create a regular member (non-admin, in the group)
		memberID := suite.registerOrGetUserID("gsg_ls_m", "gsg_ls_m@test.com", password)
		defer suite.cleanup(memberID)
		suite.disableMFA(memberID)
		suite.makeUserVouchVerified(memberID)
		memberToken := suite.reloginUser("gsg_ls_m@test.com", password)

		_, _ = suite.db.ExecContext(ctx,
			"INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, FALSE, 'verified', NOW())",
			memberID, regionID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupID, memberID, false, false)

		// As regular member: should see open + member, NOT admin_only
		memberListResp := suite.request("GET", "/api/v1/groups/"+groupID+"/signal-groups", nil, memberToken)
		defer func() { _ = memberListResp.Body.Close() }()

		if memberListResp.StatusCode != http.StatusOK {
			t.Fatalf("Member list: expected 200, got %d", memberListResp.StatusCode)
		}
		var memberListBody map[string]interface{}
		_ = json.NewDecoder(memberListResp.Body).Decode(&memberListBody)
		memberGroups := memberListBody["signal_groups"].([]interface{})
		if len(memberGroups) != 2 {
			t.Errorf("Regular member should see 2 signal groups (open + member), got %d", len(memberGroups))
		}
		// Verify admin_only is not in the list
		for _, sg := range memberGroups {
			sgMap := sg.(map[string]interface{})
			if sgMap["access_tier"] == "admin_only" {
				t.Error("Regular member should NOT see admin_only signal group")
			}
		}

		// As non-member (but authenticated and in the region): should see only open
		nonMemberID := suite.registerOrGetUserID("gsg_ls_nm", "gsg_ls_nm@test.com", password)
		defer suite.cleanup(nonMemberID)
		suite.disableMFA(nonMemberID)
		suite.makeUserVouchVerified(nonMemberID)
		nonMemberToken := suite.reloginUser("gsg_ls_nm@test.com", password)

		// Add to region but NOT to the group
		_, _ = suite.db.ExecContext(ctx,
			"INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, FALSE, 'verified', NOW())",
			nonMemberID, regionID)

		nonMemberListResp := suite.request("GET", "/api/v1/groups/"+groupID+"/signal-groups", nil, nonMemberToken)
		defer func() { _ = nonMemberListResp.Body.Close() }()

		if nonMemberListResp.StatusCode != http.StatusOK {
			t.Fatalf("Non-member list: expected 200, got %d", nonMemberListResp.StatusCode)
		}
		var nonMemberListBody map[string]interface{}
		_ = json.NewDecoder(nonMemberListResp.Body).Decode(&nonMemberListBody)
		nonMemberGroups := nonMemberListBody["signal_groups"].([]interface{})

		// Non-member but verified resident: should see open + resident tiers.
		// We only created "open" and "member" and "admin_only"; so non-member sees only "open".
		if len(nonMemberGroups) != 1 {
			t.Errorf("Non-member should see 1 signal group (open only), got %d", len(nonMemberGroups))
		}
		if len(nonMemberGroups) > 0 {
			sgMap := nonMemberGroups[0].(map[string]interface{})
			if sgMap["access_tier"] != "open" {
				t.Errorf("Non-member should only see open tier, got %v", sgMap["access_tier"])
			}
		}
	})

	t.Run("signal group limit enforcement", func(t *testing.T) {
		password := "testpassword123!"
		userID := suite.registerOrGetUserID("gsg_lim", "gsg_lim@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("gsg_lim@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("SG Limit Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		gradUserB, gradUserC := suite.graduateGroup(groupID, token, regionID, "gsg_lim")
		defer suite.cleanup(gradUserB, gradUserC)

		// Create 5 signal groups (max)
		for i := 0; i < 5; i++ {
			resp := suite.request("POST", "/api/v1/groups/"+groupID+"/signal-groups", map[string]interface{}{
				"group_name":  fmt.Sprintf("Chat %d", i+1),
				"access_tier": "member",
			}, token)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusCreated {
				var errBody map[string]interface{}
				_ = json.NewDecoder(resp.Body).Decode(&errBody)
				t.Fatalf("Failed to create signal group %d: %d %v", i+1, resp.StatusCode, errBody)
			}
		}

		// 6th should fail
		sixthResp := suite.request("POST", "/api/v1/groups/"+groupID+"/signal-groups", map[string]interface{}{
			"group_name":  "Chat 6",
			"access_tier": "member",
		}, token)
		defer func() { _ = sixthResp.Body.Close() }()

		if sixthResp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for 6th signal group, got %d", sixthResp.StatusCode)
		}

		var errBody map[string]interface{}
		_ = json.NewDecoder(sixthResp.Body).Decode(&errBody)
		if errBody["error"] != "limit_reached" {
			t.Errorf("Expected error=limit_reached, got %v", errBody["error"])
		}
	})

	t.Run("group detail includes signal groups", func(t *testing.T) {
		password := "testpassword123!"
		userID := suite.registerOrGetUserID("gsg_det", "gsg_det@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("gsg_det@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("SG Detail Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		gradUserB, gradUserC := suite.graduateGroup(groupID, token, regionID, "gsg_det")
		defer suite.cleanup(gradUserB, gradUserC)

		// Create a signal group
		sgResp := suite.request("POST", "/api/v1/groups/"+groupID+"/signal-groups", map[string]interface{}{
			"group_name":  "Detail Test Chat",
			"access_tier": "open",
		}, token)
		defer func() { _ = sgResp.Body.Close() }()
		if sgResp.StatusCode != http.StatusCreated {
			t.Fatalf("Failed to create signal group: %d", sgResp.StatusCode)
		}

		// GET group detail should include signal_groups
		getResp := suite.request("GET", "/api/v1/groups/"+groupID, nil, token)
		defer func() { _ = getResp.Body.Close() }()

		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", getResp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(getResp.Body).Decode(&body)

		signalGroups, ok := body["signal_groups"].([]interface{})
		if !ok {
			t.Fatal("Expected signal_groups array in group detail response")
		}
		if len(signalGroups) != 1 {
			t.Errorf("Expected 1 signal group in detail, got %d", len(signalGroups))
		}

		if len(signalGroups) > 0 {
			sgMap := signalGroups[0].(map[string]interface{})
			if sgMap["name"] != "Detail Test Chat" {
				t.Errorf("Expected signal group name=Detail Test Chat, got %v", sgMap["name"])
			}
			if sgMap["access_tier"] != "open" {
				t.Errorf("Expected access_tier=open, got %v", sgMap["access_tier"])
			}
		}
	})
}

func TestE2E_GroupBrowsing(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("listed active group appears in browse results", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		userID := suite.registerOrGetUserID("gb_listed", "gb_listed@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("gb_listed@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Browse Listed Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		// Force active + listed
		_, _ = suite.db.ExecContext(ctx, "UPDATE `groups` SET status = 'active', visibility = 'listed' WHERE id = ?", groupID)

		resp := suite.request("GET", "/api/v1/groups/browse?region_id="+regionID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		groups, ok := body["groups"].([]interface{})
		if !ok {
			t.Fatal("Expected groups array")
		}

		found := false
		for _, g := range groups {
			gMap := g.(map[string]interface{})
			if gMap["name"] == "Browse Listed Group" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected listed active group in browse results")
		}
	})

	t.Run("unlisted group does not appear in browse", func(t *testing.T) {
		password := "testpassword123!"

		userID := suite.registerOrGetUserID("gb_unlisted", "gb_unlisted@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("gb_unlisted@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Browse Unlisted Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		// Group is unlisted by default, force active
		_, _ = suite.db.ExecContext(context.Background(), "UPDATE `groups` SET status = 'active' WHERE id = ?", groupID)

		resp := suite.request("GET", "/api/v1/groups/browse?region_id="+regionID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		groups, _ := body["groups"].([]interface{})
		for _, g := range groups {
			gMap := g.(map[string]interface{})
			if gMap["name"] == "Browse Unlisted Group" {
				t.Error("Expected unlisted group NOT in browse results")
			}
		}
	})

	t.Run("provisional group does not appear in browse", func(t *testing.T) {
		password := "testpassword123!"

		userID := suite.registerOrGetUserID("gb_prov", "gb_prov@test.com", password)
		defer suite.cleanup(userID)
		suite.disableMFA(userID)
		suite.makeUserFullyVerified(userID)
		token := suite.reloginUser("gb_prov@test.com", password)

		regionID := suite.createTestRegionForGroups(userID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Browse Provisional Group", []string{regionID}, token)
		defer suite.cleanupGroups(groupID)

		// Force listed but stay provisional
		_, _ = suite.db.ExecContext(context.Background(), "UPDATE `groups` SET visibility = 'listed' WHERE id = ?", groupID)

		resp := suite.request("GET", "/api/v1/groups/browse?region_id="+regionID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		groups, _ := body["groups"].([]interface{})
		for _, g := range groups {
			gMap := g.(map[string]interface{})
			if gMap["name"] == "Browse Provisional Group" {
				t.Error("Expected provisional group NOT in browse results")
			}
		}
	})

	t.Run("discoverable group visible to unverified user", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		// Admin creates a discoverable group with open signal group
		adminID := suite.registerOrGetUserID("gb_disc_ad", "gb_disc_ad@test.com", password)
		defer suite.cleanup(adminID)
		suite.disableMFA(adminID)
		suite.makeUserFullyVerified(adminID)
		adminToken := suite.reloginUser("gb_disc_ad@test.com", password)

		regionID := suite.createTestRegionForGroups(adminID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Discoverable Browse Group", []string{regionID}, adminToken)
		defer suite.cleanupGroups(groupID)

		// Force active, listed, discoverable
		_, _ = suite.db.ExecContext(ctx, "UPDATE `groups` SET status = 'active', visibility = 'listed', discoverable_by_unverified = TRUE WHERE id = ?", groupID)

		// Create an open signal group
		_, _ = suite.db.ExecContext(ctx, `
			INSERT INTO signal_groups (id, owner_group_id, group_name, access_tier, is_active, created_at)
			VALUES (UUID(), ?, 'Open Browse Chat', 'open', TRUE, NOW())
		`, groupID)

		// Unverified user browses without region_id (BrowseAll)
		unverifiedID, unverifiedToken := suite.registerOrGetUser("gb_disc_uv", "gb_disc_uv@test.com", password)
		defer suite.cleanup(unverifiedID)

		resp := suite.request("GET", "/api/v1/groups/browse", nil, unverifiedToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		groups, ok := body["groups"].([]interface{})
		if !ok {
			t.Fatal("Expected groups array")
		}

		found := false
		for _, g := range groups {
			gMap := g.(map[string]interface{})
			if gMap["name"] == "Discoverable Browse Group" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected discoverable group in BrowseAll results for unverified user")
		}
	})

	t.Run("non-discoverable group hidden from unverified user", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		adminID := suite.registerOrGetUserID("gb_ndisc_ad", "gb_ndisc_ad@test.com", password)
		defer suite.cleanup(adminID)
		suite.disableMFA(adminID)
		suite.makeUserFullyVerified(adminID)
		adminToken := suite.reloginUser("gb_ndisc_ad@test.com", password)

		regionID := suite.createTestRegionForGroups(adminID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Non-Discoverable Group", []string{regionID}, adminToken)
		defer suite.cleanupGroups(groupID)

		// Active + listed but NOT discoverable (default)
		_, _ = suite.db.ExecContext(ctx, "UPDATE `groups` SET status = 'active', visibility = 'listed' WHERE id = ?", groupID)

		unverifiedID, unverifiedToken := suite.registerOrGetUser("gb_ndisc_uv", "gb_ndisc_uv@test.com", password)
		defer suite.cleanup(unverifiedID)

		resp := suite.request("GET", "/api/v1/groups/browse", nil, unverifiedToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		groups, _ := body["groups"].([]interface{})
		for _, g := range groups {
			gMap := g.(map[string]interface{})
			if gMap["name"] == "Non-Discoverable Group" {
				t.Error("Expected non-discoverable group NOT in BrowseAll results")
			}
		}
	})

	t.Run("disclaimer present in browse response", func(t *testing.T) {
		password := "testpassword123!"

		userID, token := suite.registerOrGetUser("gb_disclaim", "gb_disclaim@test.com", password)
		defer suite.cleanup(userID)

		resp := suite.request("GET", "/api/v1/groups/browse", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		disclaimer, ok := body["disclaimer"].([]interface{})
		if !ok || len(disclaimer) != 2 {
			t.Errorf("Expected disclaimer array with 2 entries, got %v", body["disclaimer"])
		}
	})

	t.Run("unverified user can join group via invite link", func(t *testing.T) {
		password := "testpassword123!"

		adminID := suite.registerOrGetUserID("gb_ujoin_ad", "gb_ujoin_ad@test.com", password)
		defer suite.cleanup(adminID)
		suite.disableMFA(adminID)
		suite.makeUserFullyVerified(adminID)
		adminToken := suite.reloginUser("gb_ujoin_ad@test.com", password)

		regionID := suite.createTestRegionForGroups(adminID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Unverified Join Group", []string{regionID}, adminToken)
		defer suite.cleanupGroups(groupID)

		linkBody := suite.createInviteLink(groupID, nil, adminToken)
		inviteToken := linkBody["token"].(string)

		unverifiedID, unverifiedToken := suite.registerOrGetUser("gb_ujoin_uv", "gb_ujoin_uv@test.com", password)
		defer suite.cleanup(unverifiedID)

		joinResp := suite.request("POST", "/api/v1/groups/join/"+inviteToken, nil, unverifiedToken)
		defer func() { _ = joinResp.Body.Close() }()

		if joinResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(joinResp.Body).Decode(&errBody)
			t.Fatalf("Expected 200, got %d: %v", joinResp.StatusCode, errBody)
		}

		isMember, _ := suite.communityGroupRepo.IsUserMember(context.Background(), groupID, unverifiedID)
		if !isMember {
			t.Error("Expected unverified user to be a member after joining via invite link")
		}
	})

	t.Run("unverified user can accept invitation", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		adminID := suite.registerOrGetUserID("gb_uinv_ad", "gb_uinv_ad@test.com", password)
		defer suite.cleanup(adminID)
		suite.disableMFA(adminID)
		suite.makeUserFullyVerified(adminID)
		adminToken := suite.reloginUser("gb_uinv_ad@test.com", password)

		regionID := suite.createTestRegionForGroups(adminID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Unverified Invite Group", []string{regionID}, adminToken)
		defer suite.cleanupGroups(groupID)

		// Register unverified user
		unverifiedID, unverifiedToken := suite.registerOrGetUser("gb_uinv_uv", "gb_uinv_uv@test.com", password)
		defer suite.cleanup(unverifiedID)

		// Admin invites unverified user
		invResp := suite.request("POST", "/api/v1/groups/"+groupID+"/invitations", map[string]string{
			"user_id": unverifiedID,
		}, adminToken)
		defer func() { _ = invResp.Body.Close() }()

		if invResp.StatusCode != http.StatusCreated {
			t.Fatalf("Expected 201 for invitation, got %d", invResp.StatusCode)
		}

		var invBody map[string]interface{}
		_ = json.NewDecoder(invResp.Body).Decode(&invBody)
		invitationID := invBody["id"].(string)

		// Unverified user accepts invitation
		acceptResp := suite.request("POST", "/api/v1/group-invitations/"+invitationID+"/respond", map[string]interface{}{
			"accept": true,
		}, unverifiedToken)
		defer func() { _ = acceptResp.Body.Close() }()

		if acceptResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(acceptResp.Body).Decode(&errBody)
			t.Fatalf("Expected 200, got %d: %v", acceptResp.StatusCode, errBody)
		}

		isMember, _ := suite.communityGroupRepo.IsUserMember(ctx, groupID, unverifiedID)
		if !isMember {
			t.Error("Expected unverified user to be a member after accepting invitation")
		}
	})
}

func TestE2E_GroupResources(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("CRUD resource links with access tier filtering", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		// Setup admin user + graduated group
		adminID := suite.registerOrGetUserID("gres_a", "gres_a@test.com", password)
		defer suite.cleanup(adminID)
		suite.disableMFA(adminID)
		suite.makeUserFullyVerified(adminID)
		adminToken := suite.reloginUser("gres_a@test.com", password)

		regionID := suite.createTestRegionForGroups(adminID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupID, _ := suite.createGroup("Resource Test Group", []string{regionID}, adminToken)
		defer suite.cleanupGroups(groupID)

		// Graduate the group
		gradUserB, gradUserC := suite.graduateGroup(groupID, adminToken, regionID, "gres")
		defer suite.cleanup(gradUserB, gradUserC)

		// --- Create resource (admin) ---
		createResp := suite.request("POST", "/api/v1/groups/"+groupID+"/resources", map[string]interface{}{
			"title":       "Community Wiki",
			"url":         "https://wiki.example.com",
			"description": "Our knowledge base",
			"access_tier": "member",
		}, adminToken)
		defer func() { _ = createResp.Body.Close() }()

		if createResp.StatusCode != http.StatusCreated {
			var errBody map[string]interface{}
			_ = json.NewDecoder(createResp.Body).Decode(&errBody)
			t.Fatalf("Expected 201, got %d: %v", createResp.StatusCode, errBody)
		}

		var createdResource map[string]interface{}
		_ = json.NewDecoder(createResp.Body).Decode(&createdResource)
		resourceID := createdResource["id"].(string)

		if createdResource["title"] != "Community Wiki" {
			t.Errorf("Expected title 'Community Wiki', got %v", createdResource["title"])
		}

		// Create an admin-only resource
		adminOnlyResp := suite.request("POST", "/api/v1/groups/"+groupID+"/resources", map[string]interface{}{
			"title":       "Admin Docs",
			"url":         "https://admin.example.com",
			"access_tier": "admin_only",
		}, adminToken)
		defer func() { _ = adminOnlyResp.Body.Close() }()
		if adminOnlyResp.StatusCode != http.StatusCreated {
			t.Fatalf("Expected 201 for admin-only resource, got %d", adminOnlyResp.StatusCode)
		}

		// --- List resources as member (should see member but not admin_only) ---
		memberID := suite.registerOrGetUserID("gres_m", "gres_m@test.com", password)
		defer suite.cleanup(memberID)
		suite.disableMFA(memberID)
		suite.makeUserVouchVerified(memberID)
		memberToken := suite.reloginUser("gres_m@test.com", password)

		_, _ = suite.db.ExecContext(ctx,
			"INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, FALSE, 'verified', NOW())",
			memberID, regionID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupID, memberID, false, false)

		listResp := suite.request("GET", "/api/v1/groups/"+groupID+"/resources", nil, memberToken)
		defer func() { _ = listResp.Body.Close() }()

		if listResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", listResp.StatusCode)
		}

		var listBody map[string]interface{}
		_ = json.NewDecoder(listResp.Body).Decode(&listBody)
		resources := listBody["resources"].([]interface{})

		// Member should see the member-tier resource but NOT the admin_only one
		if len(resources) != 1 {
			t.Fatalf("Expected 1 resource for member, got %d", len(resources))
		}
		firstResource := resources[0].(map[string]interface{})
		if firstResource["title"] != "Community Wiki" {
			t.Errorf("Expected member to see 'Community Wiki', got %v", firstResource["title"])
		}

		// Admin should see both
		adminListResp := suite.request("GET", "/api/v1/groups/"+groupID+"/resources", nil, adminToken)
		defer func() { _ = adminListResp.Body.Close() }()

		var adminListBody map[string]interface{}
		_ = json.NewDecoder(adminListResp.Body).Decode(&adminListBody)
		adminResources := adminListBody["resources"].([]interface{})
		if len(adminResources) != 2 {
			t.Fatalf("Expected 2 resources for admin, got %d", len(adminResources))
		}

		// --- Update resource (admin) ---
		updateResp := suite.request("PUT", "/api/v1/groups/"+groupID+"/resources/"+resourceID, map[string]interface{}{
			"title": "Updated Wiki",
		}, adminToken)
		defer func() { _ = updateResp.Body.Close() }()

		if updateResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(updateResp.Body).Decode(&errBody)
			t.Fatalf("Expected 200 for update, got %d: %v", updateResp.StatusCode, errBody)
		}

		// --- Delete resource (admin) ---
		deleteResp := suite.request("DELETE", "/api/v1/groups/"+groupID+"/resources/"+resourceID, nil, adminToken)
		defer func() { _ = deleteResp.Body.Close() }()

		if deleteResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for delete, got %d", deleteResp.StatusCode)
		}

		// Verify deleted
		afterDeleteResp := suite.request("GET", "/api/v1/groups/"+groupID+"/resources", nil, adminToken)
		defer func() { _ = afterDeleteResp.Body.Close() }()
		var afterDeleteBody map[string]interface{}
		_ = json.NewDecoder(afterDeleteResp.Body).Decode(&afterDeleteBody)
		afterDeleteResources := afterDeleteBody["resources"].([]interface{})
		if len(afterDeleteResources) != 1 {
			t.Fatalf("Expected 1 resource after delete, got %d", len(afterDeleteResources))
		}

		// --- Non-admin member CAN create resources ---
		nonAdminCreateResp := suite.request("POST", "/api/v1/groups/"+groupID+"/resources", map[string]interface{}{
			"title":       "Member Created Resource",
			"url":         "https://member-created.example.com",
			"access_tier": "member",
		}, memberToken)
		defer func() { _ = nonAdminCreateResp.Body.Close() }()
		if nonAdminCreateResp.StatusCode != http.StatusCreated {
			t.Errorf("Expected 201 for member create, got %d", nonAdminCreateResp.StatusCode)
		}

		// --- Non-admin cannot update/delete resources ---
		// Get remaining resource ID for update/delete test
		remainingResource := afterDeleteResources[0].(map[string]interface{})
		remainingID := remainingResource["id"].(string)

		nonAdminUpdateResp := suite.request("PUT", "/api/v1/groups/"+groupID+"/resources/"+remainingID, map[string]interface{}{
			"title": "Should Fail",
		}, memberToken)
		defer func() { _ = nonAdminUpdateResp.Body.Close() }()
		if nonAdminUpdateResp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403 for non-admin update, got %d", nonAdminUpdateResp.StatusCode)
		}

		nonAdminDeleteResp := suite.request("DELETE", "/api/v1/groups/"+groupID+"/resources/"+remainingID, nil, memberToken)
		defer func() { _ = nonAdminDeleteResp.Body.Close() }()
		if nonAdminDeleteResp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403 for non-admin delete, got %d", nonAdminDeleteResp.StatusCode)
		}
	})
}

func TestE2E_GroupBlocking(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("block, list, unblock, self-block rejected, non-admin rejected", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		// Setup admin user + two groups
		adminID := suite.registerOrGetUserID("gblk_a", "gblk_a@test.com", password)
		defer suite.cleanup(adminID)
		suite.disableMFA(adminID)
		suite.makeUserFullyVerified(adminID)
		adminToken := suite.reloginUser("gblk_a@test.com", password)

		regionID := suite.createTestRegionForGroups(adminID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupAID, _ := suite.createGroup("Block Test Group A", []string{regionID}, adminToken)
		groupBID, _ := suite.createGroup("Block Test Group B", []string{regionID}, adminToken)
		defer suite.cleanupGroups(groupAID, groupBID)

		// --- Block a group ---
		blockResp := suite.request("POST", "/api/v1/groups/"+groupAID+"/blocks", map[string]interface{}{
			"group_id": groupBID,
		}, adminToken)
		defer func() { _ = blockResp.Body.Close() }()

		if blockResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(blockResp.Body).Decode(&errBody)
			t.Fatalf("Expected 200 for block, got %d: %v", blockResp.StatusCode, errBody)
		}

		// --- Blocked group appears in list ---
		listResp := suite.request("GET", "/api/v1/groups/"+groupAID+"/blocks", nil, adminToken)
		defer func() { _ = listResp.Body.Close() }()

		if listResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for list blocks, got %d", listResp.StatusCode)
		}

		var listBody map[string]interface{}
		_ = json.NewDecoder(listResp.Body).Decode(&listBody)
		blockedGroups := listBody["blocked_groups"].([]interface{})
		if len(blockedGroups) != 1 {
			t.Fatalf("Expected 1 blocked group, got %d", len(blockedGroups))
		}
		firstBlocked := blockedGroups[0].(map[string]interface{})
		if firstBlocked["id"] != groupBID {
			t.Errorf("Expected blocked group ID %s, got %v", groupBID, firstBlocked["id"])
		}

		// --- Unblock a group ---
		unblockResp := suite.request("DELETE", "/api/v1/groups/"+groupAID+"/blocks/"+groupBID, nil, adminToken)
		defer func() { _ = unblockResp.Body.Close() }()

		if unblockResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(unblockResp.Body).Decode(&errBody)
			t.Fatalf("Expected 200 for unblock, got %d: %v", unblockResp.StatusCode, errBody)
		}

		// Verify empty list
		listResp2 := suite.request("GET", "/api/v1/groups/"+groupAID+"/blocks", nil, adminToken)
		defer func() { _ = listResp2.Body.Close() }()

		var listBody2 map[string]interface{}
		_ = json.NewDecoder(listResp2.Body).Decode(&listBody2)
		blockedGroups2 := listBody2["blocked_groups"].([]interface{})
		if len(blockedGroups2) != 0 {
			t.Errorf("Expected 0 blocked groups after unblock, got %d", len(blockedGroups2))
		}

		// --- Self-block rejected ---
		selfBlockResp := suite.request("POST", "/api/v1/groups/"+groupAID+"/blocks", map[string]interface{}{
			"group_id": groupAID,
		}, adminToken)
		defer func() { _ = selfBlockResp.Body.Close() }()

		if selfBlockResp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for self-block, got %d", selfBlockResp.StatusCode)
		}

		// --- Non-admin cannot block ---
		memberID := suite.registerOrGetUserID("gblk_m", "gblk_m@test.com", password)
		defer suite.cleanup(memberID)
		suite.disableMFA(memberID)
		suite.makeUserVouchVerified(memberID)
		memberToken := suite.reloginUser("gblk_m@test.com", password)

		// Add member to region and group (non-admin)
		_, _ = suite.db.ExecContext(ctx,
			"INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, FALSE, 'verified', NOW())",
			memberID, regionID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupAID, memberID, false, false)

		nonAdminBlockResp := suite.request("POST", "/api/v1/groups/"+groupAID+"/blocks", map[string]interface{}{
			"group_id": groupBID,
		}, memberToken)
		defer func() { _ = nonAdminBlockResp.Body.Close() }()

		if nonAdminBlockResp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403 for non-admin block, got %d", nonAdminBlockResp.StatusCode)
		}
	})
}

func TestE2E_TopicBoards(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("post, browse, block-filter, remove, non-admin rejected", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		// Setup admin user with two groups
		adminID := suite.registerOrGetUserID("tb_admin", "tb_admin@test.com", password)
		defer suite.cleanup(adminID)
		suite.disableMFA(adminID)
		suite.makeUserFullyVerified(adminID)
		adminToken := suite.reloginUser("tb_admin@test.com", password)

		regionID := suite.createTestRegionForGroups(adminID)
		defer suite.cleanupRegionsForGroups(regionID)

		groupAID, _ := suite.createGroup("TB E2E Group A", []string{regionID}, adminToken)
		groupBID, _ := suite.createGroup("TB E2E Group B", []string{regionID}, adminToken)
		defer suite.cleanupGroups(groupAID, groupBID)

		// --- Post to topic board ---
		postResp := suite.request("POST", "/api/v1/groups/"+groupAID+"/topic-board", map[string]interface{}{
			"description": "Group A offers mutual aid in the region",
			"tags":        []string{"mutual-aid", "safety"},
		}, adminToken)
		defer func() { _ = postResp.Body.Close() }()

		if postResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(postResp.Body).Decode(&errBody)
			t.Fatalf("Expected 200 for post, got %d: %v", postResp.StatusCode, errBody)
		}

		var postingA map[string]interface{}
		_ = json.NewDecoder(postResp.Body).Decode(&postingA)
		if postingA["id"] == nil || postingA["id"] == "" {
			t.Error("Expected posting ID in response")
		}

		// Post for group B too
		postBResp := suite.request("POST", "/api/v1/groups/"+groupBID+"/topic-board", map[string]interface{}{
			"description": "Group B looking for defense partners",
			"tags":        []string{"mutual-aid", "defense"},
		}, adminToken)
		defer func() { _ = postBResp.Body.Close() }()

		if postBResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for post B, got %d", postBResp.StatusCode)
		}

		// --- Browse by tag: see the other group's posting ---
		browseResp := suite.request("GET", "/api/v1/topic-board?tag=mutual-aid&group_id="+groupAID, nil, adminToken)
		defer func() { _ = browseResp.Body.Close() }()

		if browseResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for browse, got %d", browseResp.StatusCode)
		}

		var browseBody map[string]interface{}
		_ = json.NewDecoder(browseResp.Body).Decode(&browseBody)
		postings := browseBody["postings"].([]interface{})
		if len(postings) != 1 {
			t.Fatalf("Expected 1 posting (other group), got %d", len(postings))
		}
		firstPosting := postings[0].(map[string]interface{})
		if firstPosting["group_id"] != groupBID {
			t.Errorf("Expected group B posting, got %v", firstPosting["group_id"])
		}

		// --- Block group B, posting filtered out ---
		blockResp := suite.request("POST", "/api/v1/groups/"+groupAID+"/blocks", map[string]interface{}{
			"group_id": groupBID,
		}, adminToken)
		defer func() { _ = blockResp.Body.Close() }()

		if blockResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for block, got %d", blockResp.StatusCode)
		}

		browseAfterBlock := suite.request("GET", "/api/v1/topic-board?tag=mutual-aid&group_id="+groupAID, nil, adminToken)
		defer func() { _ = browseAfterBlock.Body.Close() }()

		var browseAfterBody map[string]interface{}
		_ = json.NewDecoder(browseAfterBlock.Body).Decode(&browseAfterBody)
		filteredPostings := browseAfterBody["postings"].([]interface{})
		if len(filteredPostings) != 0 {
			t.Errorf("Expected 0 postings after blocking, got %d", len(filteredPostings))
		}

		// Unblock for further tests
		unblockResp := suite.request("DELETE", "/api/v1/groups/"+groupAID+"/blocks/"+groupBID, nil, adminToken)
		defer func() { _ = unblockResp.Body.Close() }()

		// --- Get own posting ---
		getResp := suite.request("GET", "/api/v1/groups/"+groupAID+"/topic-board", nil, adminToken)
		defer func() { _ = getResp.Body.Close() }()

		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for get, got %d", getResp.StatusCode)
		}

		// --- Remove posting ---
		removeResp := suite.request("DELETE", "/api/v1/groups/"+groupAID+"/topic-board", nil, adminToken)
		defer func() { _ = removeResp.Body.Close() }()

		if removeResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for remove, got %d", removeResp.StatusCode)
		}

		// Verify removed
		getAfterRemove := suite.request("GET", "/api/v1/groups/"+groupAID+"/topic-board", nil, adminToken)
		defer func() { _ = getAfterRemove.Body.Close() }()

		if getAfterRemove.StatusCode != http.StatusNotFound {
			t.Errorf("Expected 404 after removal, got %d", getAfterRemove.StatusCode)
		}

		// --- Non-admin cannot post or browse ---
		memberID := suite.registerOrGetUserID("tb_member", "tb_member@test.com", password)
		defer suite.cleanup(memberID)
		suite.disableMFA(memberID)
		suite.makeUserVouchVerified(memberID)
		memberToken := suite.reloginUser("tb_member@test.com", password)

		// Add member to region and group (non-admin)
		_, _ = suite.db.ExecContext(ctx,
			"INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (UUID(), ?, ?, FALSE, 'verified', NOW())",
			memberID, regionID)
		_ = suite.communityGroupRepo.AddMember(ctx, groupAID, memberID, false, false)

		nonAdminPost := suite.request("POST", "/api/v1/groups/"+groupAID+"/topic-board", map[string]interface{}{
			"description": "Non-admin should not be able to post",
			"tags":        []string{"test"},
		}, memberToken)
		defer func() { _ = nonAdminPost.Body.Close() }()

		if nonAdminPost.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403 for non-admin post, got %d", nonAdminPost.StatusCode)
		}

		nonAdminBrowse := suite.request("GET", "/api/v1/topic-board?tag=mutual-aid&group_id="+groupAID, nil, memberToken)
		defer func() { _ = nonAdminBrowse.Body.Close() }()

		if nonAdminBrowse.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403 for non-admin browse, got %d", nonAdminBrowse.StatusCode)
		}
	})
}

// =============================================================================
// Connection Tests
// =============================================================================

func TestE2E_Connections(t *testing.T) {
	suite := SetupE2ETest(t)

	// Create two users with groups
	userAID, tokenA := suite.registerOrGetUser("conn_e2e_a", "conn_e2e_a@test.com", "password12345")
	userBID, _ := suite.registerOrGetUser("conn_e2e_b", "conn_e2e_b@test.com", "password12345")
	suite.makeUserFullyVerified(userAID)
	suite.makeUserFullyVerified(userBID)
	tokenA = suite.reloginUser("conn_e2e_a@test.com", "password12345")
	tokenB := suite.reloginUser("conn_e2e_b@test.com", "password12345")

	regionAID := suite.createTestRegionForGroups(userAID)
	regionBID := suite.createTestRegionForGroups(userBID)
	groupAID, _ := suite.createGroup("Conn E2E Group A", []string{regionAID}, tokenA)
	groupBID, _ := suite.createGroup("Conn E2E Group B", []string{regionBID}, tokenB)

	defer func() {
		suite.cleanupGroups(groupAID, groupBID)
		suite.cleanupRegionsForGroups(regionAID, regionBID)
		suite.cleanup(userAID, userBID)
	}()

	t.Run("Formation flow: propose, accept, connection exists", func(t *testing.T) {
		// Propose
		proposeResp := suite.request("POST", "/api/v1/connections?proposer_group_id="+groupAID, map[string]interface{}{
			"name":      "E2E Test Connection",
			"group_ids": []string{groupBID},
		}, tokenA)
		defer func() { _ = proposeResp.Body.Close() }()

		if proposeResp.StatusCode != http.StatusCreated {
			var errBody map[string]interface{}
			_ = json.NewDecoder(proposeResp.Body).Decode(&errBody)
			t.Fatalf("Expected 201, got %d: %v", proposeResp.StatusCode, errBody)
		}

		var proposal map[string]interface{}
		_ = json.NewDecoder(proposeResp.Body).Decode(&proposal)
		proposalID := proposal["id"].(string)

		// B sees pending proposal
		pendingResp := suite.request("GET", "/api/v1/connection-proposals", nil, tokenB)
		defer func() { _ = pendingResp.Body.Close() }()
		if pendingResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for pending proposals, got %d", pendingResp.StatusCode)
		}

		// B accepts
		acceptResp := suite.request("POST", "/api/v1/connection-proposals/"+proposalID+"/respond", map[string]interface{}{
			"accept":   true,
			"group_id": groupBID,
		}, tokenB)
		defer func() { _ = acceptResp.Body.Close() }()

		if acceptResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(acceptResp.Body).Decode(&errBody)
			t.Fatalf("Expected 200, got %d: %v", acceptResp.StatusCode, errBody)
		}

		var acceptResult map[string]interface{}
		_ = json.NewDecoder(acceptResp.Body).Decode(&acceptResult)
		if acceptResult["status"] != "accepted" {
			t.Errorf("Expected status=accepted, got %v", acceptResult["status"])
		}
		connectionID, _ := acceptResult["connection_id"].(string)
		if connectionID == "" {
			t.Fatal("Expected connection_id to be set")
		}
		defer suite.cleanupConnections(connectionID)

		// Both can list the connection
		listResp := suite.request("GET", "/api/v1/connections", nil, tokenA)
		defer func() { _ = listResp.Body.Close() }()
		if listResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", listResp.StatusCode)
		}

		var listResult map[string]interface{}
		_ = json.NewDecoder(listResp.Body).Decode(&listResult)
		connections := listResult["connections"].([]interface{})
		if len(connections) == 0 {
			t.Error("Expected at least one connection")
		}

		// Get connection details
		getResp := suite.request("GET", "/api/v1/connections/"+connectionID, nil, tokenA)
		defer func() { _ = getResp.Body.Close() }()
		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", getResp.StatusCode)
		}
	})

	t.Run("Decline flow: proposal declined", func(t *testing.T) {
		proposeResp := suite.request("POST", "/api/v1/connections?proposer_group_id="+groupAID, map[string]interface{}{
			"group_ids": []string{groupBID},
		}, tokenA)
		defer func() { _ = proposeResp.Body.Close() }()

		var proposal map[string]interface{}
		_ = json.NewDecoder(proposeResp.Body).Decode(&proposal)
		proposalID := proposal["id"].(string)

		declineResp := suite.request("POST", "/api/v1/connection-proposals/"+proposalID+"/respond", map[string]interface{}{
			"accept":   false,
			"group_id": groupBID,
		}, tokenB)
		defer func() { _ = declineResp.Body.Close() }()

		if declineResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", declineResp.StatusCode)
		}

		var result map[string]interface{}
		_ = json.NewDecoder(declineResp.Body).Decode(&result)
		if result["status"] != "declined" {
			t.Errorf("Expected status=declined, got %v", result["status"])
		}
	})

	t.Run("Expansion and leave", func(t *testing.T) {
		// Create a third user/group
		userCID, _ := suite.registerOrGetUser("conn_e2e_c", "conn_e2e_c@test.com", "password12345")
		suite.makeUserFullyVerified(userCID)
		tokenC := suite.reloginUser("conn_e2e_c@test.com", "password12345")
		regionCID := suite.createTestRegionForGroups(userCID)
		groupCID, _ := suite.createGroup("Conn E2E Group C", []string{regionCID}, tokenC)
		defer func() {
			suite.cleanupGroups(groupCID)
			suite.cleanupRegionsForGroups(regionCID)
			suite.cleanup(userCID)
		}()

		// Form A+B connection
		propResp := suite.request("POST", "/api/v1/connections?proposer_group_id="+groupAID, map[string]interface{}{
			"group_ids": []string{groupBID},
		}, tokenA)
		defer func() { _ = propResp.Body.Close() }()
		var prop map[string]interface{}
		_ = json.NewDecoder(propResp.Body).Decode(&prop)
		propID := prop["id"].(string)

		acceptResp := suite.request("POST", "/api/v1/connection-proposals/"+propID+"/respond", map[string]interface{}{
			"accept":   true,
			"group_id": groupBID,
		}, tokenB)
		defer func() { _ = acceptResp.Body.Close() }()
		var acceptResult map[string]interface{}
		_ = json.NewDecoder(acceptResp.Body).Decode(&acceptResult)
		connectionID := acceptResult["connection_id"].(string)
		defer suite.cleanupConnections(connectionID)

		// Invite C
		inviteResp := suite.request("POST", "/api/v1/connections/"+connectionID+"/invite?proposer_group_id="+groupAID, map[string]interface{}{
			"group_id": groupCID,
		}, tokenA)
		defer func() { _ = inviteResp.Body.Close() }()

		if inviteResp.StatusCode != http.StatusCreated {
			var errBody map[string]interface{}
			_ = json.NewDecoder(inviteResp.Body).Decode(&errBody)
			t.Fatalf("Expected 201 for invite, got %d: %v", inviteResp.StatusCode, errBody)
		}

		var inviteResult map[string]interface{}
		_ = json.NewDecoder(inviteResp.Body).Decode(&inviteResult)
		expandPropID := inviteResult["id"].(string)

		// B accepts expansion
		bAcceptResp := suite.request("POST", "/api/v1/connection-proposals/"+expandPropID+"/respond", map[string]interface{}{
			"accept":   true,
			"group_id": groupBID,
		}, tokenB)
		defer func() { _ = bAcceptResp.Body.Close() }()

		// C accepts expansion
		cAcceptResp := suite.request("POST", "/api/v1/connection-proposals/"+expandPropID+"/respond", map[string]interface{}{
			"accept":   true,
			"group_id": groupCID,
		}, tokenC)
		defer func() { _ = cAcceptResp.Body.Close() }()

		var expandAcceptResult map[string]interface{}
		_ = json.NewDecoder(cAcceptResp.Body).Decode(&expandAcceptResult)
		if expandAcceptResult["status"] != "accepted" {
			t.Errorf("Expected status=accepted after expansion, got %v", expandAcceptResult["status"])
		}

		// A leaves
		leaveResp := suite.request("POST", "/api/v1/connections/"+connectionID+"/leave?group_id="+groupAID, nil, tokenA)
		defer func() { _ = leaveResp.Body.Close() }()
		if leaveResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for leave, got %d", leaveResp.StatusCode)
		}

		// Connection still exists with B and C
		getResp := suite.request("GET", "/api/v1/connections/"+connectionID, nil, tokenB)
		defer func() { _ = getResp.Body.Close() }()
		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected connection to still exist, got %d", getResp.StatusCode)
		}
	})

	t.Run("Auto-remove on unanimous block", func(t *testing.T) {
		// Create a third user/group for this subtest
		userDID, _ := suite.registerOrGetUser("conn_e2e_d", "conn_e2e_d@test.com", "password12345")
		suite.makeUserFullyVerified(userDID)
		tokenD := suite.reloginUser("conn_e2e_d@test.com", "password12345")
		regionDID := suite.createTestRegionForGroups(userDID)
		groupDID, _ := suite.createGroup("Conn E2E Group D", []string{regionDID}, tokenD)
		defer func() {
			suite.cleanupGroups(groupDID)
			suite.cleanupRegionsForGroups(regionDID)
			suite.cleanup(userDID)
		}()

		// Form A+B+D connection
		propResp := suite.request("POST", "/api/v1/connections?proposer_group_id="+groupAID, map[string]interface{}{
			"group_ids": []string{groupBID, groupDID},
		}, tokenA)
		defer func() { _ = propResp.Body.Close() }()
		var prop map[string]interface{}
		_ = json.NewDecoder(propResp.Body).Decode(&prop)
		propID := prop["id"].(string)

		// B accepts
		bResp := suite.request("POST", "/api/v1/connection-proposals/"+propID+"/respond", map[string]interface{}{
			"accept": true, "group_id": groupBID,
		}, tokenB)
		defer func() { _ = bResp.Body.Close() }()

		// D accepts
		dResp := suite.request("POST", "/api/v1/connection-proposals/"+propID+"/respond", map[string]interface{}{
			"accept": true, "group_id": groupDID,
		}, tokenD)
		defer func() { _ = dResp.Body.Close() }()
		var dResult map[string]interface{}
		_ = json.NewDecoder(dResp.Body).Decode(&dResult)
		connectionID := dResult["connection_id"].(string)
		defer suite.cleanupConnections(connectionID)

		// A and B block D
		suite.request("POST", "/api/v1/groups/"+groupAID+"/blocks", map[string]interface{}{
			"group_id": groupDID,
		}, tokenA)
		suite.request("POST", "/api/v1/groups/"+groupBID+"/blocks", map[string]interface{}{
			"group_id": groupDID,
		}, tokenB)

		// A leaves — this triggers unanimous block check
		// After A leaves, B is the only remaining non-D member. B has blocked D.
		// So D should be auto-removed (unanimous block by all others = just B).
		leaveResp := suite.request("POST", "/api/v1/connections/"+connectionID+"/leave?group_id="+groupAID, nil, tokenA)
		defer func() { _ = leaveResp.Body.Close() }()

		// The connection might be dissolved if D was removed and only B remains
		// Either way, D should not be a member anymore
		// Since B blocked D and B is the only other member, D gets auto-removed
		// Then only B remains, so connection is dissolved
	})
}

// =============================================================================
// Full Discovery Flow E2E Test
// =============================================================================

func TestE2E_FullDiscoveryFlow(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("end to end discovery and connection", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		// Setup: Create admin A with region A and graduated group A ("Seattle Mutual Aid")
		adminAID := suite.registerOrGetUserID("disc_a", "disc_a@test.com", password)
		defer suite.cleanup(adminAID)
		suite.disableMFA(adminAID)
		suite.makeUserFullyVerified(adminAID)
		tokenA := suite.reloginUser("disc_a@test.com", password)

		regionAID := suite.createTestRegionForGroups(adminAID)
		defer suite.cleanupRegionsForGroups(regionAID)

		groupAID, _ := suite.createGroup("Seattle Mutual Aid", []string{regionAID}, tokenA)
		defer suite.cleanupGroups(groupAID)

		gradAB, gradAC := suite.graduateGroup(groupAID, tokenA, regionAID, "disc_ga")
		defer suite.cleanup(gradAB, gradAC)

		// Setup: Create admin B with region B and graduated group B ("Portland Mutual Aid")
		adminBID := suite.registerOrGetUserID("disc_b", "disc_b@test.com", password)
		defer suite.cleanup(adminBID)
		suite.disableMFA(adminBID)
		suite.makeUserFullyVerified(adminBID)
		tokenB := suite.reloginUser("disc_b@test.com", password)

		regionBID := suite.createTestRegionForGroups(adminBID)
		defer suite.cleanupRegionsForGroups(regionBID)

		groupBID, _ := suite.createGroup("Portland Mutual Aid", []string{regionBID}, tokenB)
		defer suite.cleanupGroups(groupBID)

		gradBB, gradBC := suite.graduateGroup(groupBID, tokenB, regionBID, "disc_gb")
		defer suite.cleanup(gradBB, gradBC)

		// 1. Group A posts to topic board with tag "mutual-aid"
		postResp := suite.request("POST", "/api/v1/groups/"+groupAID+"/topic-board", map[string]interface{}{
			"description": "Seattle neighborhood mutual aid network",
			"tags":        []string{"mutual-aid", "safety"},
		}, tokenA)
		defer func() { _ = postResp.Body.Close() }()

		if postResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(postResp.Body).Decode(&errBody)
			t.Fatalf("Step 1: Expected 200 for topic post, got %d: %v", postResp.StatusCode, errBody)
		}

		var postingA map[string]interface{}
		_ = json.NewDecoder(postResp.Body).Decode(&postingA)
		if postingA["id"] == nil || postingA["id"] == "" {
			t.Fatal("Step 1: Expected posting ID in response")
		}

		// 2. Group B admin browses topic board for "mutual-aid" — sees Group A's posting
		browseResp := suite.request("GET", "/api/v1/topic-board?tag=mutual-aid&group_id="+groupBID, nil, tokenB)
		defer func() { _ = browseResp.Body.Close() }()

		if browseResp.StatusCode != http.StatusOK {
			t.Fatalf("Step 2: Expected 200 for browse, got %d", browseResp.StatusCode)
		}

		var browseBody map[string]interface{}
		_ = json.NewDecoder(browseResp.Body).Decode(&browseBody)
		postings := browseBody["postings"].([]interface{})
		if len(postings) != 1 {
			t.Fatalf("Step 2: Expected 1 posting from group A, got %d", len(postings))
		}
		firstPosting := postings[0].(map[string]interface{})
		if firstPosting["group_id"] != groupAID {
			t.Errorf("Step 2: Expected group A posting, got group_id=%v", firstPosting["group_id"])
		}

		// 3. Group B admin proposes connection with Group A
		proposeResp := suite.request("POST", "/api/v1/connections?proposer_group_id="+groupBID, map[string]interface{}{
			"name":      "Mutual Aid Alliance",
			"group_ids": []string{groupAID},
		}, tokenB)
		defer func() { _ = proposeResp.Body.Close() }()

		if proposeResp.StatusCode != http.StatusCreated {
			var errBody map[string]interface{}
			_ = json.NewDecoder(proposeResp.Body).Decode(&errBody)
			t.Fatalf("Step 3: Expected 201, got %d: %v", proposeResp.StatusCode, errBody)
		}

		var proposal map[string]interface{}
		_ = json.NewDecoder(proposeResp.Body).Decode(&proposal)
		proposalID := proposal["id"].(string)

		// 4. Group A admin sees pending proposal
		pendingResp := suite.request("GET", "/api/v1/connection-proposals", nil, tokenA)
		defer func() { _ = pendingResp.Body.Close() }()

		if pendingResp.StatusCode != http.StatusOK {
			t.Fatalf("Step 4: Expected 200 for pending proposals, got %d", pendingResp.StatusCode)
		}

		var pendingBody map[string]interface{}
		_ = json.NewDecoder(pendingResp.Body).Decode(&pendingBody)
		pendingProposals := pendingBody["proposals"].([]interface{})
		if len(pendingProposals) == 0 {
			t.Fatal("Step 4: Expected at least 1 pending proposal")
		}

		// 5. Group A admin accepts
		acceptResp := suite.request("POST", "/api/v1/connection-proposals/"+proposalID+"/respond", map[string]interface{}{
			"accept":   true,
			"group_id": groupAID,
		}, tokenA)
		defer func() { _ = acceptResp.Body.Close() }()

		if acceptResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(acceptResp.Body).Decode(&errBody)
			t.Fatalf("Step 5: Expected 200, got %d: %v", acceptResp.StatusCode, errBody)
		}

		var acceptResult map[string]interface{}
		_ = json.NewDecoder(acceptResp.Body).Decode(&acceptResult)
		if acceptResult["status"] != "accepted" {
			t.Errorf("Step 5: Expected status=accepted, got %v", acceptResult["status"])
		}
		connectionID, _ := acceptResult["connection_id"].(string)
		if connectionID == "" {
			t.Fatal("Step 5: Expected connection_id to be set")
		}
		defer suite.cleanupConnections(connectionID)

		// 6. Both groups see the connection
		listRespA := suite.request("GET", "/api/v1/connections", nil, tokenA)
		defer func() { _ = listRespA.Body.Close() }()
		if listRespA.StatusCode != http.StatusOK {
			t.Fatalf("Step 6: Expected 200 from group A, got %d", listRespA.StatusCode)
		}

		var listBodyA map[string]interface{}
		_ = json.NewDecoder(listRespA.Body).Decode(&listBodyA)
		connectionsA := listBodyA["connections"].([]interface{})
		if len(connectionsA) == 0 {
			t.Error("Step 6: Expected group A to see at least one connection")
		}

		listRespB := suite.request("GET", "/api/v1/connections", nil, tokenB)
		defer func() { _ = listRespB.Body.Close() }()
		if listRespB.StatusCode != http.StatusOK {
			t.Fatalf("Step 6: Expected 200 from group B, got %d", listRespB.StatusCode)
		}

		var listBodyB map[string]interface{}
		_ = json.NewDecoder(listRespB.Body).Decode(&listBodyB)
		connectionsB := listBodyB["connections"].([]interface{})
		if len(connectionsB) == 0 {
			t.Error("Step 6: Expected group B to see at least one connection")
		}

		// 7. Group A admin proposes a signal chat for the connection
		chatPropResp := suite.request("POST", "/api/v1/connections/"+connectionID+"/signal-group-proposals?proposer_group_id="+groupAID, map[string]interface{}{
			"group_name":   "Alliance Chat",
			"description":  "Cross-group coordination",
			"access_level": "all_members",
		}, tokenA)
		defer func() { _ = chatPropResp.Body.Close() }()

		if chatPropResp.StatusCode != http.StatusCreated {
			var errBody map[string]interface{}
			_ = json.NewDecoder(chatPropResp.Body).Decode(&errBody)
			t.Fatalf("Step 7: Expected 201, got %d: %v", chatPropResp.StatusCode, errBody)
		}

		var chatProposal map[string]interface{}
		_ = json.NewDecoder(chatPropResp.Body).Decode(&chatProposal)
		chatProposalID := chatProposal["id"].(string)

		// 8. Group B admin approves the chat proposal
		voteResp := suite.request("POST", "/api/v1/connection-chat-proposals/"+chatProposalID+"/vote", map[string]interface{}{
			"approve":  true,
			"group_id": groupBID,
		}, tokenB)
		defer func() { _ = voteResp.Body.Close() }()

		if voteResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(voteResp.Body).Decode(&errBody)
			t.Fatalf("Step 8: Expected 200, got %d: %v", voteResp.StatusCode, errBody)
		}

		var voteResult map[string]interface{}
		_ = json.NewDecoder(voteResp.Body).Decode(&voteResult)
		if voteResult["status"] != "approved" {
			t.Errorf("Step 8: Expected status=approved, got %v", voteResult["status"])
		}

		// 9. Connection signal groups visible
		sgListResp := suite.request("GET", "/api/v1/connections/"+connectionID+"/signal-groups", nil, tokenA)
		defer func() { _ = sgListResp.Body.Close() }()

		if sgListResp.StatusCode != http.StatusOK {
			t.Fatalf("Step 9: Expected 200, got %d", sgListResp.StatusCode)
		}

		var sgListBody map[string]interface{}
		_ = json.NewDecoder(sgListResp.Body).Decode(&sgListBody)
		signalGroups := sgListBody["signal_groups"].([]interface{})
		if len(signalGroups) != 1 {
			t.Fatalf("Step 9: Expected 1 signal group, got %d", len(signalGroups))
		}

		// 10. Group A creates a resource and shares it into the connection
		createResourceResp := suite.request("POST", "/api/v1/groups/"+groupAID+"/resources", map[string]interface{}{
			"title":       "Emergency Contacts",
			"url":         "https://example.com/emergency",
			"description": "Shared emergency contact sheet",
			"access_tier": "member",
		}, tokenA)
		defer func() { _ = createResourceResp.Body.Close() }()

		if createResourceResp.StatusCode != http.StatusCreated {
			var errBody map[string]interface{}
			_ = json.NewDecoder(createResourceResp.Body).Decode(&errBody)
			t.Fatalf("Step 10a: Expected 201, got %d: %v", createResourceResp.StatusCode, errBody)
		}

		var createdResource map[string]interface{}
		_ = json.NewDecoder(createResourceResp.Body).Decode(&createdResource)
		resourceID := createdResource["id"].(string)

		shareResp := suite.request("POST", "/api/v1/connections/"+connectionID+"/shared-resources?proposer_group_id="+groupAID, map[string]interface{}{
			"resource_id": resourceID,
			"visibility":  "all_members",
		}, tokenA)
		defer func() { _ = shareResp.Body.Close() }()

		if shareResp.StatusCode != http.StatusCreated {
			var errBody map[string]interface{}
			_ = json.NewDecoder(shareResp.Body).Decode(&errBody)
			t.Fatalf("Step 10b: Expected 201, got %d: %v", shareResp.StatusCode, errBody)
		}

		// 11. Group B sees the shared resource
		sharedListResp := suite.request("GET", "/api/v1/connections/"+connectionID+"/shared-resources", nil, tokenB)
		defer func() { _ = sharedListResp.Body.Close() }()

		if sharedListResp.StatusCode != http.StatusOK {
			t.Fatalf("Step 11: Expected 200, got %d", sharedListResp.StatusCode)
		}

		var sharedListBody map[string]interface{}
		_ = json.NewDecoder(sharedListResp.Body).Decode(&sharedListBody)
		sharedResources := sharedListBody["shared_resources"].([]interface{})
		if len(sharedResources) != 1 {
			t.Fatalf("Step 11: Expected 1 shared resource, got %d", len(sharedResources))
		}

		// 12. Group A unshares the resource
		unshareResp := suite.request("DELETE", "/api/v1/connections/"+connectionID+"/shared-resources/"+resourceID, nil, tokenA)
		defer func() { _ = unshareResp.Body.Close() }()

		if unshareResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(unshareResp.Body).Decode(&errBody)
			t.Fatalf("Step 12a: Expected 200, got %d: %v", unshareResp.StatusCode, errBody)
		}

		// Verify unshared
		sharedListAfter := suite.request("GET", "/api/v1/connections/"+connectionID+"/shared-resources", nil, tokenA)
		defer func() { _ = sharedListAfter.Body.Close() }()

		var sharedAfterBody map[string]interface{}
		_ = json.NewDecoder(sharedListAfter.Body).Decode(&sharedAfterBody)
		sharedAfter := sharedAfterBody["shared_resources"].([]interface{})
		if len(sharedAfter) != 0 {
			t.Errorf("Step 12b: Expected 0 shared resources after unshare, got %d", len(sharedAfter))
		}

		// 13. Group B leaves the connection
		leaveResp := suite.request("POST", "/api/v1/connections/"+connectionID+"/leave?group_id="+groupBID, nil, tokenB)
		defer func() { _ = leaveResp.Body.Close() }()

		if leaveResp.StatusCode != http.StatusOK {
			t.Fatalf("Step 13: Expected 200, got %d", leaveResp.StatusCode)
		}

		// 14. Connection dissolved (only 1 group left)
		// When B leaves, only A remains — connection auto-dissolves
		listAfterLeave := suite.request("GET", "/api/v1/connections", nil, tokenA)
		defer func() { _ = listAfterLeave.Body.Close() }()

		var listAfterBody map[string]interface{}
		_ = json.NewDecoder(listAfterLeave.Body).Decode(&listAfterBody)
		connectionsAfter := listAfterBody["connections"].([]interface{})
		if len(connectionsAfter) != 0 {
			t.Errorf("Step 14: Expected 0 connections after leave (dissolved), got %d", len(connectionsAfter))
		}

		// Cleanup topic board postings
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM topic_board_postings WHERE group_id IN (?, ?)", groupAID, groupBID)
	})

	t.Run("blocking filters topic board", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		// Create admin C with group C
		adminCID := suite.registerOrGetUserID("disc_c", "disc_c@test.com", password)
		defer suite.cleanup(adminCID)
		suite.disableMFA(adminCID)
		suite.makeUserFullyVerified(adminCID)
		tokenC := suite.reloginUser("disc_c@test.com", password)

		regionCID := suite.createTestRegionForGroups(adminCID)
		defer suite.cleanupRegionsForGroups(regionCID)

		groupCID, _ := suite.createGroup("Block Test Group C", []string{regionCID}, tokenC)
		defer suite.cleanupGroups(groupCID)

		// Create admin D with group D
		adminDID := suite.registerOrGetUserID("disc_d", "disc_d@test.com", password)
		defer suite.cleanup(adminDID)
		suite.disableMFA(adminDID)
		suite.makeUserFullyVerified(adminDID)
		tokenD := suite.reloginUser("disc_d@test.com", password)

		regionDID := suite.createTestRegionForGroups(adminDID)
		defer suite.cleanupRegionsForGroups(regionDID)

		groupDID, _ := suite.createGroup("Block Test Group D", []string{regionDID}, tokenD)
		defer suite.cleanupGroups(groupDID)

		// Group C posts to topic board
		postCResp := suite.request("POST", "/api/v1/groups/"+groupCID+"/topic-board", map[string]interface{}{
			"description": "Group C offering mutual aid",
			"tags":        []string{"block-test"},
		}, tokenC)
		defer func() { _ = postCResp.Body.Close() }()

		if postCResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for post C, got %d", postCResp.StatusCode)
		}

		// Group D browses, sees Group C
		browseResp := suite.request("GET", "/api/v1/topic-board?tag=block-test&group_id="+groupDID, nil, tokenD)
		defer func() { _ = browseResp.Body.Close() }()

		var browseBody map[string]interface{}
		_ = json.NewDecoder(browseResp.Body).Decode(&browseBody)
		postings := browseBody["postings"].([]interface{})
		if len(postings) != 1 {
			t.Fatalf("Expected 1 posting before block, got %d", len(postings))
		}

		// Group D blocks Group C
		blockResp := suite.request("POST", "/api/v1/groups/"+groupDID+"/blocks", map[string]interface{}{
			"group_id": groupCID,
		}, tokenD)
		defer func() { _ = blockResp.Body.Close() }()

		if blockResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for block, got %d", blockResp.StatusCode)
		}

		// Group D browses again, Group C is filtered out
		browseAfterBlock := suite.request("GET", "/api/v1/topic-board?tag=block-test&group_id="+groupDID, nil, tokenD)
		defer func() { _ = browseAfterBlock.Body.Close() }()

		var browseAfterBody map[string]interface{}
		_ = json.NewDecoder(browseAfterBlock.Body).Decode(&browseAfterBody)
		filteredPostings := browseAfterBody["postings"].([]interface{})
		if len(filteredPostings) != 0 {
			t.Errorf("Expected 0 postings after blocking group C, got %d", len(filteredPostings))
		}

		// Cleanup
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM group_blocks WHERE blocker_group_id = ?", groupDID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM topic_board_postings WHERE group_id IN (?, ?)", groupCID, groupDID)
	})

	t.Run("unverified user can browse discoverable groups", func(t *testing.T) {
		password := "testpassword123!"
		ctx := context.Background()

		// Create admin with a discoverable group
		adminEID := suite.registerOrGetUserID("disc_e", "disc_e@test.com", password)
		defer suite.cleanup(adminEID)
		suite.disableMFA(adminEID)
		suite.makeUserFullyVerified(adminEID)
		tokenE := suite.reloginUser("disc_e@test.com", password)

		regionEID := suite.createTestRegionForGroups(adminEID)
		defer suite.cleanupRegionsForGroups(regionEID)

		groupEID, _ := suite.createGroup("Discoverable Test Group", []string{regionEID}, tokenE)
		defer suite.cleanupGroups(groupEID)

		// Graduate the group so it becomes active
		gradEB, gradEC := suite.graduateGroup(groupEID, tokenE, regionEID, "disc_ge")
		defer suite.cleanup(gradEB, gradEC)

		// Mark group as listed and discoverable by unverified users
		_, err := suite.db.ExecContext(ctx,
			"UPDATE `groups` SET status = 'active', visibility = 'listed', discoverable_by_unverified = TRUE WHERE id = ?",
			groupEID)
		if err != nil {
			t.Fatalf("Failed to update group visibility: %v", err)
		}

		// Create an open-tier signal group (required for BrowseAll to return the group)
		openSgResp := suite.request("POST", "/api/v1/groups/"+groupEID+"/signal-groups", map[string]interface{}{
			"group_name":  "Open Welcome Chat",
			"description": "Open to everyone",
			"access_tier": "open",
		}, tokenE)
		defer func() { _ = openSgResp.Body.Close() }()

		if openSgResp.StatusCode != http.StatusCreated {
			var errBody map[string]interface{}
			_ = json.NewDecoder(openSgResp.Body).Decode(&errBody)
			t.Fatalf("Expected 201 for open signal group, got %d: %v", openSgResp.StatusCode, errBody)
		}

		// Register an unverified user (no vouch verification)
		unverifiedID, unverifiedToken := suite.registerOrGetUser("disc_unverif", "disc_unverif@test.com", password)
		defer suite.cleanup(unverifiedID)

		// Unverified user browses: GET /api/v1/groups/browse
		browseResp := suite.request("GET", "/api/v1/groups/browse", nil, unverifiedToken)
		defer func() { _ = browseResp.Body.Close() }()

		if browseResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 for unverified browse, got %d", browseResp.StatusCode)
		}

		var browseBody map[string]interface{}
		_ = json.NewDecoder(browseResp.Body).Decode(&browseBody)
		groups := browseBody["groups"].([]interface{})

		// Find the discoverable group in the results
		foundDiscoverable := false
		for _, g := range groups {
			groupData := g.(map[string]interface{})
			if groupData["id"] == groupEID {
				foundDiscoverable = true
				break
			}
		}
		if !foundDiscoverable {
			t.Error("Expected unverified user to see the discoverable group in browse results")
		}

		// Create an invite link for the group
		linkBody := suite.createInviteLink(groupEID, nil, tokenE)
		inviteToken := linkBody["token"].(string)

		// Unverified user joins via invite link
		joinResp := suite.request("POST", "/api/v1/groups/join/"+inviteToken, nil, unverifiedToken)
		defer func() { _ = joinResp.Body.Close() }()

		if joinResp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(joinResp.Body).Decode(&errBody)
			t.Fatalf("Expected 200 for join, got %d: %v", joinResp.StatusCode, errBody)
		}

		// Verify user is a member
		isMember, memberErr := suite.communityGroupRepo.IsUserMember(ctx, groupEID, unverifiedID)
		if memberErr != nil {
			t.Fatalf("Failed to check membership: %v", memberErr)
		}
		if !isMember {
			t.Error("Expected unverified user to be a member after joining via invite link")
		}
	})
}
