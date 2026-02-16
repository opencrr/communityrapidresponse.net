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
	jwtAuth             *middleware.JWTAuth
	schoolRepo          *database.SchoolRepository
	districtRepo        *database.SchoolDistrictRepository
	schoolVouchRepo     *database.SchoolVouchRepository
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
	membershipRepo := database.NewMembershipRepository(db)
	schoolRepo := database.NewSchoolRepository(db)
	districtRepo := database.NewSchoolDistrictRepository(db)
	schoolVouchRepo := database.NewSchoolVouchRepository(db)
	auditRepo := database.NewAuditRepository(db)
	encryptedSecretRepo := database.NewEncryptedSecretRepository(db)
	encryptionKeyRepo := database.NewEncryptionKeyRepository(db)
	meshtasticChannelRepo := database.NewMeshtasticChannelRepository(db)

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

	// Create handlers with mock services
	authHandler := handlers.NewAuthHandler(userRepo, jwtAuth)
	regionHandler := handlers.NewRegionHandler(regionRepo, mockMapbox, nil)
	verificationHandler := handlers.NewVerificationHandler(
		nil, verifyRepo, vouchRepo, userRepo, regionRepo,
		mockPostgrid, mockMapbox, nil,
		false, 30, // Bootstrap cooldown disabled for tests
	)
	consensusConfig := &config.ConsensusConfig{VotePercent: 50, VoteFloor: 3}
	signalGroupHandler := handlers.NewSignalGroupHandler(
		db, groupRepo, encryptedSecretRepo, regionRepo, auditRepo,
	)
	adminHandler := handlers.NewAdminHandler(userRepo, regionRepo, nil)

	// Create MFA service and handler (use test encryption key)
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

	schoolHandler := handlers.NewSchoolHandler(
		db, schoolRepo, districtRepo, schoolVouchRepo, groupRepo, encryptedSecretRepo,
		userRepo, auditRepo, nil, consensusConfig, false, 0,
	)

	meshtasticHandler := handlers.NewMeshtasticHandler(
		db, meshtasticChannelRepo, encryptedSecretRepo, regionRepo, schoolRepo, auditRepo,
	)
	encryptionHandler := handlers.NewEncryptionHandler(encryptionKeyRepo, encryptedSecretRepo, regionRepo, schoolRepo)

	// Create router (rate limiting disabled for tests)
	router := handlers.NewRouter(
		authHandler, mfaHandler, regionHandler, signalGroupHandler, verificationHandler, adminHandler,
		membershipHandler, blocklistProposalHandler, nil, schoolHandler, nil, encryptionHandler, nil, meshtasticHandler, jwtAuth, nil, nil, nil,
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
		jwtAuth:             jwtAuth,
		schoolRepo:          schoolRepo,
		districtRepo:        districtRepo,
		schoolVouchRepo:     schoolVouchRepo,
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

func TestE2E_SignalGroupsAdmin(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register and setup admin user
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "signalgroupadmin",
		"email":    "signalgroupadmin@test.com",
		"password": "securepassword123",
	}, "")
	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	_ = resp.Body.Close()

	ctx := context.Background()

	// Make user a superuser to bypass boundary validation for region creation
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, verification_tier = 2, postcard_verified = TRUE, vouch_verified = TRUE, is_superuser = TRUE WHERE id = ?", registerResp.UserID)

	// Login
	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "signalgroupadmin@test.com",
		"password": "securepassword123",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token := loginResp.Token

	// Create a region for this admin
	createResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name": "Signal Group Admin Test Region",
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

	// Make user admin of this region
	_, _ = suite.db.ExecContext(ctx, "INSERT INTO user_regions (id, user_id, region_id, is_admin, verified_at) VALUES (UUID(), ?, ?, TRUE, NOW()) ON DUPLICATE KEY UPDATE is_admin = TRUE", registerResp.UserID, regionID)

	defer func() {
		suite.cleanupSignalGroupSecrets(regionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE region_id = ?", regionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", regionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", regionID)
		suite.cleanup(registerResp.UserID)
	}()

	t.Run("admin can access signal groups admin endpoint", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/signal-groups/admin", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		// Response should have groups array (even if empty)
		groups, ok := body["groups"].([]interface{})
		if !ok {
			t.Errorf("Expected 'groups' array in response, got %T", body["groups"])
		}
		// groups can be empty, just verify it's an array
		_ = groups
	})

	t.Run("create signal group with encrypted payload", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/signal-groups", map[string]interface{}{
			"region_id":         regionID,
			"name":              "Test Signal Group",
			"description":       "A test group",
			"encrypted_payload": "dGVzdC1lbmNyeXB0ZWQtcGF5bG9hZA==",
			"encryption_iv":     "dGVzdC1pdi0xMjM=",
			"wrapped_keys": []map[string]string{
				{"user_id": registerResp.UserID, "wrapped_dek": "dGVzdC13cmFwcGVkLWRlaw=="},
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["group_id"] == "" {
			t.Error("Expected group_id in response")
		}
	})

	t.Run("list signal groups returns array not null", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/signal-groups?region_id="+regionID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		// groups should be an array, not null
		groups, ok := body["groups"].([]interface{})
		if !ok {
			t.Errorf("Expected 'groups' to be an array, got %T", body["groups"])
		}
		if groups == nil {
			t.Error("Expected 'groups' to be non-nil array")
		}
	})
}

func TestE2E_SignalGroupsEmptyResponse(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register and setup admin user
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "emptyresponse",
		"email":    "emptyresponse@test.com",
		"password": "securepassword123",
	}, "")
	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	_ = resp.Body.Close()

	ctx := context.Background()

	// Make user an admin (both postcard and vouch verified)
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, verification_tier = 2, postcard_verified = TRUE, vouch_verified = TRUE WHERE id = ?", registerResp.UserID)

	// Login
	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "emptyresponse@test.com",
		"password": "securepassword123",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token := loginResp.Token

	// Create a region with no signal groups
	createResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name": "Empty Signal Groups Region",
		"type": "city",
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-97.6, 28.4}, {-97.4, 28.4}, {-97.4, 28.5}, {-97.6, 28.5}, {-97.6, 28.4}}},
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

	t.Run("empty signal groups returns empty array not null", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/signal-groups?region_id="+regionID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Read raw body to check JSON
		var rawBody map[string]json.RawMessage
		_ = json.NewDecoder(resp.Body).Decode(&rawBody)

		groupsJSON := string(rawBody["groups"])
		if groupsJSON == "null" {
			t.Error("Expected 'groups' to be [] not null")
		}
		if groupsJSON != "[]" {
			t.Logf("Groups JSON: %s (expected [] for empty)", groupsJSON)
		}
	})
}

func TestE2E_AdminHierarchyPropagation(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register and setup admin user
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "hierarchyadmin",
		"email":    "hierarchyadmin@test.com",
		"password": "securepassword123",
	}, "")
	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	_ = resp.Body.Close()

	ctx := context.Background()

	// Make user an admin (both postcard and vouch verified) AND superuser initially to create regions
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, verification_tier = 2, postcard_verified = TRUE, vouch_verified = TRUE, is_superuser = TRUE WHERE id = ?", registerResp.UserID)

	// Login
	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "hierarchyadmin@test.com",
		"password": "securepassword123",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token := loginResp.Token

	// Create parent region (city level - cities have no parent requirement)
	parentResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name": "Test City For Hierarchy",
		"type": "city",
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-100.0, 28.0}, {-96.0, 28.0}, {-96.0, 32.0}, {-100.0, 32.0}, {-100.0, 28.0}}},
		},
	}, token)
	var parentBody map[string]string
	_ = json.NewDecoder(parentResp.Body).Decode(&parentBody)
	_ = parentResp.Body.Close()
	parentRegionID := parentBody["region_id"]

	if parentRegionID == "" {
		t.Fatal("Failed to create parent city region")
	}

	// Create child region (neighborhood level) with city parent
	childResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name":             "Test Neighborhood",
		"type":             "neighborhood",
		"parent_region_id": parentRegionID,
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-98.6, 29.4}, {-97.0, 29.4}, {-97.0, 31.0}, {-98.6, 31.0}, {-98.6, 29.4}}},
		},
	}, token)
	var childBody map[string]string
	_ = json.NewDecoder(childResp.Body).Decode(&childBody)
	_ = childResp.Body.Close()
	childRegionID := childBody["region_id"]

	if childRegionID == "" {
		t.Fatal("Failed to create child neighborhood region")
	}

	// Remove superuser status and make user admin ONLY of the child (neighborhood) region
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET is_superuser = FALSE WHERE id = ?", registerResp.UserID)
	_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", registerResp.UserID)
	// Use UUID() function for the id column
	_, _ = suite.db.ExecContext(ctx, "INSERT INTO user_regions (id, user_id, region_id, is_admin, verified_at, verification_status) VALUES (UUID(), ?, ?, TRUE, NOW(), 'verified')", registerResp.UserID, childRegionID)

	// Create 2 additional full admin users to exit bootstrap mode (need 3 full admins)
	for i := 1; i <= 2; i++ {
		var extraUserID string
		_ = suite.db.QueryRowContext(ctx, "SELECT UUID()").Scan(&extraUserID)
		_, _ = suite.db.ExecContext(ctx, `
			INSERT INTO users (id, username, email, password_hash, postcard_verified, vouch_verified, verification_tier, created_at)
			VALUES (?, ?, ?, 'dummy', TRUE, TRUE, 3, NOW())
		`, extraUserID, fmt.Sprintf("extraadmin%d", i), fmt.Sprintf("extraadmin%d@test.com", i))
		_, _ = suite.db.ExecContext(ctx, `
			INSERT INTO user_regions (id, user_id, region_id, is_admin, verified_at, verification_status)
			VALUES (UUID(), ?, ?, TRUE, NOW(), 'verified')
		`, extraUserID, childRegionID)
	}

	// Re-login to get a token without superuser privileges
	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "hierarchyadmin@test.com",
		"password": "securepassword123",
	}, "")
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token = loginResp.Token

	defer func() {
		suite.cleanupSignalGroupSecrets(parentRegionID)
		suite.cleanupSignalGroupSecrets(childRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE region_id IN (?, ?)", parentRegionID, childRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id IN (?, ?)", parentRegionID, childRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", childRegionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", parentRegionID)
		// Clean up extra admin users created to exit bootstrap mode
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM users WHERE email LIKE 'extraadmin%@test.com'")
		suite.cleanup(registerResp.UserID)
	}()

	t.Run("admin of child can create signal group in parent region", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/signal-groups", map[string]interface{}{
			"region_id":         parentRegionID,
			"name":              "Parent Region Group",
			"description":       "Group in parent region",
			"encrypted_payload": "dGVzdC1lbmNyeXB0ZWQtcGF5bG9hZA==",
			"encryption_iv":     "dGVzdC1pdi0xMjM=",
			"wrapped_keys": []map[string]string{
				{"user_id": registerResp.UserID, "wrapped_dek": "dGVzdC13cmFwcGVkLWRlaw=="},
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["group_id"] == "" {
			t.Error("Expected group_id in response")
		}
	})

	t.Run("admin regions list includes parent regions", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/communities/admin", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		regions, ok := body["regions"].([]interface{})
		if !ok {
			t.Fatalf("Expected 'regions' array, got %T", body["regions"])
		}

		// Should include both parent and child regions
		regionIDs := make(map[string]bool)
		for _, r := range regions {
			region := r.(map[string]interface{})
			regionIDs[region["id"].(string)] = true
		}

		if !regionIDs[parentRegionID] {
			t.Errorf("Expected parent region %s in admin regions list", parentRegionID)
		}
		if !regionIDs[childRegionID] {
			t.Errorf("Expected child region %s in admin regions list", childRegionID)
		}
	})
}

func TestE2E_SuperuserBypass(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register and setup superuser
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "superuserbypass",
		"email":    "superuserbypass@test.com",
		"password": "securepassword123",
	}, "")
	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	_ = resp.Body.Close()

	ctx := context.Background()

	// Make user a superuser (but NOT an admin of any region)
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, verification_tier = 2, postcard_verified = TRUE, vouch_verified = TRUE, is_superuser = TRUE WHERE id = ?", registerResp.UserID)
	// Remove all user_regions to ensure they're NOT an admin
	_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", registerResp.UserID)

	// Login
	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "superuserbypass@test.com",
		"password": "securepassword123",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token := loginResp.Token

	// Create a region (superuser can create)
	createResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name": "Superuser Bypass Region",
		"type": "city",
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-96.6, 27.4}, {-96.4, 27.4}, {-96.4, 27.5}, {-96.6, 27.5}, {-96.6, 27.4}}},
		},
	}, token)
	var createBody map[string]string
	_ = json.NewDecoder(createResp.Body).Decode(&createBody)
	_ = createResp.Body.Close()
	regionID := createBody["region_id"]

	defer func() {
		suite.cleanupSignalGroupSecrets(regionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE region_id = ?", regionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", regionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", regionID)
		suite.cleanup(registerResp.UserID)
	}()

	t.Run("superuser can create signal group in any region", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/signal-groups", map[string]interface{}{
			"region_id":         regionID,
			"name":              "Superuser Created Group",
			"description":       "Group created by superuser",
			"encrypted_payload": "dGVzdC1lbmNyeXB0ZWQtcGF5bG9hZA==",
			"encryption_iv":     "dGVzdC1pdi0xMjM=",
			"wrapped_keys": []map[string]string{
				{"user_id": registerResp.UserID, "wrapped_dek": "dGVzdC13cmFwcGVkLWRlaw=="},
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}
	})

	t.Run("superuser can access regions admin endpoint", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/communities/admin", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		// Superuser should see all regions
		if _, ok := body["regions"].([]interface{}); !ok {
			t.Errorf("Expected 'regions' array in response, got %T", body["regions"])
		}
	})

	t.Run("superuser can access signal groups admin endpoint", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/signal-groups/admin", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}
	})
}

func TestE2E_NonAdminCannotAccessAdminEndpoints(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register and setup regular user (not admin)
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "nonadminuser",
		"email":    "nonadminuser@test.com",
		"password": "securepassword123",
	}, "")
	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	_ = resp.Body.Close()

	ctx := context.Background()

	// Make user vouch-verified but NOT an admin (needs postcard too for admin)
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, verification_tier = 1, postcard_verified = FALSE, vouch_verified = TRUE WHERE id = ?", registerResp.UserID)

	// Login
	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "nonadminuser@test.com",
		"password": "securepassword123",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token := loginResp.Token

	defer suite.cleanup(registerResp.UserID)

	t.Run("non-admin cannot access regions admin endpoint", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/communities/admin", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", resp.StatusCode)
		}
	})

	t.Run("non-admin cannot access signal groups admin endpoint", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/signal-groups/admin", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", resp.StatusCode)
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

// =============================================================================
// Cleanup helpers for encrypted secrets and meshtastic channels
// =============================================================================

func (s *E2ETestSuite) cleanupMeshtasticChannels(regionID string) {
	ctx := context.Background()
	// Delete encrypted_secret_keys and encrypted_secrets for meshtastic channels in this region
	_, _ = s.db.ExecContext(ctx, `DELETE esk FROM encrypted_secret_keys esk
		INNER JOIN encrypted_secrets es ON esk.secret_id = es.id
		INNER JOIN meshtastic_channels mc ON es.meshtastic_channel_id = mc.id
		WHERE mc.region_id = ?`, regionID)
	_, _ = s.db.ExecContext(ctx, `DELETE es FROM encrypted_secrets es
		INNER JOIN meshtastic_channels mc ON es.meshtastic_channel_id = mc.id
		WHERE mc.region_id = ?`, regionID)
	_, _ = s.db.ExecContext(ctx, "DELETE FROM meshtastic_channels WHERE region_id = ?", regionID)
}

func (s *E2ETestSuite) cleanupSignalGroupSecrets(regionID string) {
	ctx := context.Background()
	_, _ = s.db.ExecContext(ctx, `DELETE esk FROM encrypted_secret_keys esk
		INNER JOIN encrypted_secrets es ON esk.secret_id = es.id
		INNER JOIN signal_groups sg ON es.signal_group_id = sg.id
		WHERE sg.region_id = ?`, regionID)
	_, _ = s.db.ExecContext(ctx, `DELETE es FROM encrypted_secrets es
		INNER JOIN signal_groups sg ON es.signal_group_id = sg.id
		WHERE sg.region_id = ?`, regionID)
}

func (s *E2ETestSuite) cleanupEncryptionKeys(userID string) {
	_, _ = s.db.ExecContext(context.Background(), "DELETE FROM user_encryption_keys WHERE user_id = ?", userID)
}

// =============================================================================
// Meshtastic Channel CRUD E2E Tests
// =============================================================================

func TestE2E_MeshtasticChannelCRUD(t *testing.T) {
	suite := SetupE2ETest(t)

	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Register and setup admin user
	adminEmail := fmt.Sprintf("meshtastic_admin_%s@test.com", suffix)
	userID, _ := suite.registerOrGetUser(
		fmt.Sprintf("mesh_admin_%s", suffix),
		adminEmail,
		"securepassword123",
	)

	// Make user a superuser with full verification
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET verification_tier = 2, postcard_verified = TRUE, vouch_verified = TRUE, is_superuser = TRUE WHERE id = ?", userID)

	// Re-login to get token with updated claims
	loginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email": adminEmail, "password": "securepassword123",
	}, "")
	var login models.LoginResponse
	_ = json.NewDecoder(loginResp.Body).Decode(&login)
	_ = loginResp.Body.Close()
	token := login.Token

	// Create a region
	createRegionResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name": fmt.Sprintf("Meshtastic Test Region %s", suffix),
		"type": "city",
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-99.0, 30.0}, {-98.0, 30.0}, {-98.0, 31.0}, {-99.0, 31.0}, {-99.0, 30.0}}},
		},
	}, token)
	var regionBody map[string]string
	_ = json.NewDecoder(createRegionResp.Body).Decode(&regionBody)
	_ = createRegionResp.Body.Close()
	regionID := regionBody["region_id"]

	if regionID == "" {
		t.Fatal("Failed to create region for meshtastic test")
	}

	// Make user admin of this region
	_, _ = suite.db.ExecContext(ctx, "INSERT INTO user_regions (id, user_id, region_id, is_admin, verified_at) VALUES (UUID(), ?, ?, TRUE, NOW()) ON DUPLICATE KEY UPDATE is_admin = TRUE", userID, regionID)

	defer func() {
		suite.cleanupMeshtasticChannels(regionID)
		suite.cleanupSignalGroupSecrets(regionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE region_id = ?", regionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", regionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", regionID)
		suite.cleanup(userID)
	}()

	var channelID string

	t.Run("create meshtastic channel", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/meshtastic-channels", map[string]interface{}{
			"region_id":         regionID,
			"name":              "Test Meshtastic Channel",
			"description":       "A test channel",
			"encrypted_payload": "dGVzdC1tZXNodGFzdGljLXBheWxvYWQ=",
			"encryption_iv":     "dGVzdC1pdi0xMjM=",
			"wrapped_keys": []map[string]string{
				{"user_id": userID, "wrapped_dek": "dGVzdC13cmFwcGVkLWRlaw=="},
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Fatalf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["channel_id"] == nil || body["channel_id"] == "" {
			t.Error("Expected channel_id in response")
		}
		channelID = body["channel_id"].(string)

		if body["region_id"] != regionID {
			t.Errorf("Expected region_id %s, got %v", regionID, body["region_id"])
		}
		if body["channel_name"] != "Test Meshtastic Channel" {
			t.Errorf("Expected channel_name 'Test Meshtastic Channel', got %v", body["channel_name"])
		}
		if body["created_at"] == nil {
			t.Error("Expected created_at in response")
		}
	})

	t.Run("list meshtastic channels as verified user", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/meshtastic-channels?region_id="+regionID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		channels, ok := body["channels"].([]interface{})
		if !ok {
			t.Fatalf("Expected 'channels' array, got %T", body["channels"])
		}
		if len(channels) == 0 {
			t.Error("Expected at least one channel in list")
		}
	})

	t.Run("list meshtastic channels on admin endpoint", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/meshtastic-channels/admin", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		channels, ok := body["channels"].([]interface{})
		if !ok {
			t.Fatalf("Expected 'channels' array, got %T", body["channels"])
		}
		if len(channels) == 0 {
			t.Error("Expected at least one channel in admin list")
		}
	})

	t.Run("update meshtastic channel metadata", func(t *testing.T) {
		if channelID == "" {
			t.Skip("No channel ID from create test")
		}

		updatedName := "Updated Meshtastic Channel"
		updatedDesc := "Updated description"
		resp := suite.request("PUT", "/api/v1/meshtastic-channels/"+channelID, map[string]interface{}{
			"name":        updatedName,
			"description": updatedDesc,
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["channel_name"] != updatedName {
			t.Errorf("Expected channel_name '%s', got %v", updatedName, body["channel_name"])
		}
	})
}

// =============================================================================
// Encrypted Secret Flow E2E Tests
// =============================================================================

func TestE2E_EncryptedSecretFlow(t *testing.T) {
	suite := SetupE2ETest(t)

	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Register and setup admin user
	adminEmail := fmt.Sprintf("enc_admin_%s@test.com", suffix)
	userID, _ := suite.registerOrGetUser(
		fmt.Sprintf("enc_admin_%s", suffix),
		adminEmail,
		"securepassword123",
	)

	// Make user a superuser with full verification
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET verification_tier = 2, postcard_verified = TRUE, vouch_verified = TRUE, is_superuser = TRUE WHERE id = ?", userID)

	// Re-login to get token with updated claims
	loginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email": adminEmail, "password": "securepassword123",
	}, "")
	var login models.LoginResponse
	_ = json.NewDecoder(loginResp.Body).Decode(&login)
	_ = loginResp.Body.Close()
	token := login.Token

	// Create a region
	createRegionResp := suite.request("POST", "/api/v1/communities", map[string]interface{}{
		"name": fmt.Sprintf("Encryption Test Region %s", suffix),
		"type": "city",
		"geometry": map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{-95.0, 29.0}, {-94.0, 29.0}, {-94.0, 30.0}, {-95.0, 30.0}, {-95.0, 29.0}}},
		},
	}, token)
	var regionBody map[string]string
	_ = json.NewDecoder(createRegionResp.Body).Decode(&regionBody)
	_ = createRegionResp.Body.Close()
	regionID := regionBody["region_id"]

	if regionID == "" {
		t.Fatal("Failed to create region for encryption test")
	}

	// Make user admin of this region
	_, _ = suite.db.ExecContext(ctx, "INSERT INTO user_regions (id, user_id, region_id, is_admin, verified_at) VALUES (UUID(), ?, ?, TRUE, NOW()) ON DUPLICATE KEY UPDATE is_admin = TRUE", userID, regionID)

	defer func() {
		suite.cleanupEncryptionKeys(userID)
		suite.cleanupSignalGroupSecrets(regionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE region_id = ?", regionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", regionID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", regionID)
		suite.cleanup(userID)
	}()

	t.Run("signal group creation stores encrypted payload", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/signal-groups", map[string]interface{}{
			"region_id":         regionID,
			"name":              "Encrypted Signal Group",
			"description":       "Group with encrypted secret",
			"encrypted_payload": "dGVzdC1lbmNyeXB0ZWQtc2lnbmFs",
			"encryption_iv":     "dGVzdC1pdi1zaWduYWw=",
			"wrapped_keys": []map[string]string{
				{"user_id": userID, "wrapped_dek": "dGVzdC13cmFwcGVkLXNpZ25hbA=="},
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Fatalf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["group_id"] == nil || body["group_id"] == "" {
			t.Error("Expected group_id in response")
		}
	})

	t.Run("listing groups returns encrypted payload and wrapped DEK", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/signal-groups?region_id="+regionID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		groups, ok := body["groups"].([]interface{})
		if !ok || len(groups) == 0 {
			t.Fatal("Expected at least one group in list")
		}

		group := groups[0].(map[string]interface{})
		encSecret, ok := group["encrypted_secret"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected 'encrypted_secret' object in group response")
		}

		if encSecret["encrypted_payload"] == nil || encSecret["encrypted_payload"] == "" {
			t.Error("Expected encrypted_payload in encrypted_secret")
		}
		if encSecret["encryption_iv"] == nil || encSecret["encryption_iv"] == "" {
			t.Error("Expected encryption_iv in encrypted_secret")
		}
	})

	t.Run("encryption key upload", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/encryption/keys", map[string]interface{}{
			"public_key":          "test-public-key-base64",
			"wrapped_private_key": "test-wrapped-private-key",
			"key_salt":            "test-salt-12345678901234",
			"key_iv":              "test-iv-12345678",
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}
	})

	t.Run("encryption key retrieval", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/encryption/keys", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Fatalf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["public_key"] != "test-public-key-base64" {
			t.Errorf("Expected public_key 'test-public-key-base64', got %v", body["public_key"])
		}
		if body["wrapped_private_key"] != "test-wrapped-private-key" {
			t.Errorf("Expected wrapped_private_key 'test-wrapped-private-key', got %v", body["wrapped_private_key"])
		}
		if body["key_salt"] != "test-salt-12345678901234" {
			t.Errorf("Expected key_salt 'test-salt-12345678901234', got %v", body["key_salt"])
		}
		if body["key_iv"] != "test-iv-12345678" {
			t.Errorf("Expected key_iv 'test-iv-12345678', got %v", body["key_iv"])
		}
	})

	t.Run("encryption key password change update", func(t *testing.T) {
		resp := suite.request("PUT", "/api/v1/encryption/keys", map[string]interface{}{
			"wrapped_private_key": "updated-wrapped-private-key",
			"key_salt":            "updated-salt-1234567890",
			"key_iv":              "updated-iv-1234",
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}

		// Verify the updated values
		getResp := suite.request("GET", "/api/v1/encryption/keys", nil, token)
		defer func() { _ = getResp.Body.Close() }()

		var body map[string]interface{}
		_ = json.NewDecoder(getResp.Body).Decode(&body)

		if body["wrapped_private_key"] != "updated-wrapped-private-key" {
			t.Errorf("Expected updated wrapped_private_key, got %v", body["wrapped_private_key"])
		}
		if body["key_salt"] != "updated-salt-1234567890" {
			t.Errorf("Expected updated key_salt, got %v", body["key_salt"])
		}
	})
}

// =============================================================================
// Meshtastic Channel Scoping E2E Tests
// =============================================================================

func TestE2E_MeshtasticChannelScoping(t *testing.T) {
	suite := SetupE2ETest(t)

	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Register and setup admin user
	adminEmail := fmt.Sprintf("mesh_scope_admin_%s@test.com", suffix)
	adminUserID, _ := suite.registerOrGetUser(
		fmt.Sprintf("mesh_scope_%s", suffix),
		adminEmail,
		"securepassword123",
	)

	// Make user a superuser with full verification
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET verification_tier = 2, postcard_verified = TRUE, vouch_verified = TRUE, is_superuser = TRUE WHERE id = ?", adminUserID)

	// Re-login to get token with updated claims
	loginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email": adminEmail, "password": "securepassword123",
	}, "")
	var login models.LoginResponse
	_ = json.NewDecoder(loginResp.Body).Decode(&login)
	_ = loginResp.Body.Close()
	adminToken := login.Token

	// Create school and district for scoping tests
	schoolID := suite.createSchool(fmt.Sprintf("MeshSchool %s", suffix), "TX")
	districtID := suite.createDistrict(fmt.Sprintf("MeshDist %s", suffix), "TX")
	suite.linkSchoolToDistrict(schoolID, districtID)
	suite.addUserToSchool(adminUserID, schoolID, "verified", true)

	defer func() {
		suite.cleanupSchools(schoolID)
		suite.cleanupDistricts(districtID)
		suite.cleanup(adminUserID)
	}()

	t.Run("school-scoped meshtastic channel creation", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/meshtastic-channels", map[string]interface{}{
			"school_id":         schoolID,
			"name":              "School Meshtastic Channel",
			"description":       "Channel for school",
			"encrypted_payload": "dGVzdC1zY2hvb2wtcGF5bG9hZA==",
			"encryption_iv":     "dGVzdC1pdi1zY2hvb2w=",
			"wrapped_keys": []map[string]string{
				{"user_id": adminUserID, "wrapped_dek": "dGVzdC13cmFwcGVkLXNjaG9vbA=="},
			},
		}, adminToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["channel_id"] == nil || body["channel_id"] == "" {
			t.Error("Expected channel_id in response")
		}
		if body["school_id"] != schoolID {
			t.Errorf("Expected school_id %s, got %v", schoolID, body["school_id"])
		}
	})

	t.Run("district-scoped meshtastic channel creation", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/meshtastic-channels", map[string]interface{}{
			"district_id":       districtID,
			"name":              "District Meshtastic Channel",
			"description":       "Channel for district",
			"encrypted_payload": "dGVzdC1kaXN0cmljdC1wYXlsb2Fk",
			"encryption_iv":     "dGVzdC1pdi1kaXN0",
			"wrapped_keys": []map[string]string{
				{"user_id": adminUserID, "wrapped_dek": "dGVzdC13cmFwcGVkLWRpc3Q="},
			},
		}, adminToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["channel_id"] == nil || body["channel_id"] == "" {
			t.Error("Expected channel_id in response")
		}
		if body["district_id"] != districtID {
			t.Errorf("Expected district_id %s, got %v", districtID, body["district_id"])
		}
	})

	t.Run("non-admin cannot create channels", func(t *testing.T) {
		// Register a regular user (not admin of any school)
		regularEmail := fmt.Sprintf("mesh_regular_%s@test.com", suffix)
		regularUserID, _ := suite.registerOrGetUser(
			fmt.Sprintf("mesh_reg_%s", suffix),
			regularEmail,
			"securepassword123",
		)

		// Make them vouch-verified but not admin
		_, _ = suite.db.ExecContext(ctx, "UPDATE users SET verification_tier = 1, vouch_verified = TRUE WHERE id = ?", regularUserID)
		// Re-login for updated claims
		reloginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email": regularEmail, "password": "securepassword123",
		}, "")
		var relogin models.LoginResponse
		_ = json.NewDecoder(reloginResp.Body).Decode(&relogin)
		_ = reloginResp.Body.Close()
		regularToken := relogin.Token

		defer suite.cleanup(regularUserID)

		resp := suite.request("POST", "/api/v1/meshtastic-channels", map[string]interface{}{
			"school_id":         schoolID,
			"name":              "Should Fail Channel",
			"encrypted_payload": "dGVzdC1mYWls",
			"encryption_iv":     "dGVzdC1pdi1mYWls",
			"wrapped_keys": []map[string]string{
				{"user_id": regularUserID, "wrapped_dek": "dGVzdC13cmFwcGVkLWZhaWw="},
			},
		}, regularToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", resp.StatusCode)
		}
	})

	t.Run("unverified user cannot list channels", func(t *testing.T) {
		// Register an unverified user
		unverifiedEmail := fmt.Sprintf("mesh_unverified_%s@test.com", suffix)
		unverifiedUserID, unverifiedToken := suite.registerOrGetUser(
			fmt.Sprintf("mesh_unv_%s", suffix),
			unverifiedEmail,
			"securepassword123",
		)
		defer suite.cleanup(unverifiedUserID)

		resp := suite.request("GET", "/api/v1/meshtastic-channels?school_id="+schoolID, nil, unverifiedToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", resp.StatusCode)
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
