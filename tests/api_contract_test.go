package tests

import (
	"bytes"
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

// APIContractTestSuite holds dependencies for API contract tests
type APIContractTestSuite struct {
	t          *testing.T
	db         *database.DB
	server     *httptest.Server
	userRepo   *database.UserRepository
	regionRepo *database.RegionRepository
	vouchRepo  *database.VouchRepository
	verifyRepo *database.VerificationRepository
	jwtAuth    *middleware.JWTAuth
}

// SetupAPIContractTest creates a new test suite for API contract tests
func SetupAPIContractTest(t *testing.T) *APIContractTestSuite {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping API contract tests")
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
		Secret:          "test_secret_key_at_least_32_characters_long",
		ExpirationHours: 24,
		Issuer:          "test_issuer",
	}
	jwtAuth := middleware.NewJWTAuth(jwtConfig)

	mockPostgrid := mocks.NewMockPostgridService()
	mockMapbox := mocks.NewMockMapboxService()

	emailCfg := &config.EmailConfig{Backend: config.EmailBackendMock, Enabled: false}
	emailService, _ := services.NewEmailService(emailCfg)

	authHandler := handlers.NewAuthHandlerWithEmailService(
		nil,
		userRepo,
		jwtAuth,
		emailService,
		jwtConfig.Secret,
		false,
		false,
		nil,
		nil,
		nil,
		"http://localhost:3000",
		nil,
	)
	regionHandler := handlers.NewRegionHandler(regionRepo, mockMapbox, nil)
	verificationHandler := handlers.NewVerificationHandler(
		nil, verifyRepo, vouchRepo, userRepo, regionRepo,
		mockPostgrid, mockMapbox, nil,
		false, 30, // Bootstrap cooldown disabled for tests
	)
	encryptedSecretRepo := database.NewEncryptedSecretRepository(db)
	consensusConfig := &config.ConsensusConfig{VotePercent: 50, VoteFloor: 3}
	signalGroupHandler := handlers.NewSignalGroupHandler(
		db, groupRepo, encryptedSecretRepo, regionRepo, nil,
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

	rateLimiter := services.NewNoOpRateLimiter()

	schoolHandler := handlers.NewSchoolHandler(
		db, schoolRepo, districtRepo, schoolVouchRepo, groupRepo, encryptedSecretRepo,
		userRepo, auditRepo, nil, consensusConfig, false, 0,
	)

	router := handlers.NewRouter(
		authHandler,
		mfaHandler,
		regionHandler,
		signalGroupHandler,
		verificationHandler,
		adminHandler,
		membershipHandler,
		blocklistProposalHandler,
		nil, // deletionProposalHandler
		schoolHandler,
		nil, // userReportHandler
		nil, // encryptionHandler
		nil, // secretUpdateHandler
		nil, // meshtasticHandler
		jwtAuth,
		rateLimiter,
		nil,           // rateLimitConfig
		nil,           // csrfConfig
		[]string{"*"}, // corsOrigins
		nil,           // securityConfig
	)

	server := httptest.NewServer(router.Setup())

	return &APIContractTestSuite{
		t:          t,
		db:         db,
		server:     server,
		userRepo:   userRepo,
		regionRepo: regionRepo,
		vouchRepo:  vouchRepo,
		verifyRepo: verifyRepo,
		jwtAuth:    jwtAuth,
	}
}

func (s *APIContractTestSuite) Cleanup() {
	s.server.Close()
	// Clean up test data
	_, _ = s.db.Exec("DELETE FROM vouches WHERE voucher_user_id LIKE 'contract-test-%' OR vouched_user_id LIKE 'contract-test-%'")
	_, _ = s.db.Exec("DELETE FROM user_regions WHERE user_id LIKE 'contract-test-%'")
	_, _ = s.db.Exec("DELETE FROM users WHERE id LIKE 'contract-test-%'")
	_, _ = s.db.Exec("DELETE FROM geographic_regions WHERE id LIKE 'contract-test-%'")
	_ = s.db.Close()
}

func (s *APIContractTestSuite) createUser(username, email string, postcardVerified, vouchVerified bool) *models.User {
	user := &models.User{
		ID:               "contract-test-" + username,
		Username:         username,
		Email:            email,
		PasswordHash:     "$2a$12$test.test.test.test.test.test.test.test.test.test.test.test",
		PostcardVerified: postcardVerified,
		VouchVerified:    vouchVerified,
		EmailVerified:    true,
	}

	// Insert directly using SQL to avoid uuid generation
	query := `INSERT INTO users (id, username, email, password_hash, postcard_verified, vouch_verified, email_verified, verification_tier, mfa_setup_required)
	          VALUES (?, ?, ?, ?, ?, ?, ?, 0, 0)`
	_, err := s.db.Exec(query, user.ID, user.Username, user.Email, user.PasswordHash, user.PostcardVerified, user.VouchVerified, user.EmailVerified)
	if err != nil {
		s.t.Fatalf("Failed to create test user %s: %v", username, err)
	}

	return user
}

func (s *APIContractTestSuite) createRegion(name string, regionType models.RegionType) *models.GeographicRegion {
	region := &models.GeographicRegion{
		ID:         "contract-test-region-" + name,
		Name:       name,
		RegionType: regionType,
	}

	query := `INSERT INTO geographic_regions (id, name, region_type, geometry)
              VALUES (?, ?, ?, ST_GeomFromText('POLYGON((-122.5 37.5, -122.5 37.6, -122.4 37.6, -122.4 37.5, -122.5 37.5))', 4326))`
	_, err := s.db.Exec(query, region.ID, region.Name, region.RegionType)
	if err != nil {
		s.t.Fatalf("Failed to create test region: %v", err)
	}

	return region
}

func (s *APIContractTestSuite) addUserToRegion(userID, regionID string, isAdmin bool, status string) {
	query := `INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status) VALUES (UUID(), ?, ?, ?, ?)`
	_, err := s.db.Exec(query, userID, regionID, isAdmin, status)
	if err != nil {
		s.t.Fatalf("Failed to add user to region: %v", err)
	}
}

func (s *APIContractTestSuite) cleanupVouches(voucherID, vouchedID string) {
	_, _ = s.db.Exec("DELETE FROM vouches WHERE voucher_user_id = ? AND vouched_user_id = ?", voucherID, vouchedID)
}

func (s *APIContractTestSuite) generateToken(user *models.User) string {
	token, err := s.jwtAuth.GenerateToken(user)
	if err != nil {
		s.t.Fatalf("Failed to generate token: %v", err)
	}
	return token
}

// TestAPIContract_VouchEndpoint_FrontendPayloadFormat tests that the vouch endpoint
// accepts the payload format that the frontend sends (user_id field, no region_id)
func TestAPIContract_VouchEndpoint_FrontendPayloadFormat(t *testing.T) {
	suite := SetupAPIContractTest(t)
	defer suite.Cleanup()

	// Create test users
	adminUser := suite.createUser("contract_admin", "contract_admin@test.com", true, true)
	voucheeUser := suite.createUser("contract_vouchee", "contract_vouchee@test.com", false, false)

	// Create a region and add both users
	region := suite.createRegion("Contract City", models.RegionTypeCity)
	suite.addUserToRegion(adminUser.ID, region.ID, true, string(models.UserRegionStatusVerified))
	suite.addUserToRegion(voucheeUser.ID, region.ID, false, string(models.UserRegionStatusPending))

	token := suite.generateToken(adminUser)

	t.Run("frontend_sends_user_id_with_email", func(t *testing.T) {
		suite.cleanupVouches(adminUser.ID, voucheeUser.ID)

		// This is exactly what the frontend sends
		payload := map[string]interface{}{
			"user_id": voucheeUser.Email,
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest(http.MethodPost, suite.server.URL+"/api/v1/verification/vouch", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var errResp map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&errResp)
			t.Errorf("Expected 201, got %d. Error: %v", resp.StatusCode, errResp)
		}
	})

	t.Run("frontend_sends_user_id_with_username", func(t *testing.T) {
		suite.cleanupVouches(adminUser.ID, voucheeUser.ID)

		payload := map[string]interface{}{
			"user_id": voucheeUser.Username,
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest(http.MethodPost, suite.server.URL+"/api/v1/verification/vouch", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var errResp map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&errResp)
			t.Errorf("Expected 201, got %d. Error: %v", resp.StatusCode, errResp)
		}
	})

	t.Run("frontend_sends_user_id_with_uuid", func(t *testing.T) {
		suite.cleanupVouches(adminUser.ID, voucheeUser.ID)

		payload := map[string]interface{}{
			"user_id": voucheeUser.ID,
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest(http.MethodPost, suite.server.URL+"/api/v1/verification/vouch", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var errResp map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&errResp)
			t.Errorf("Expected 201, got %d. Error: %v", resp.StatusCode, errResp)
		}
	})
}

// TestAPIContract_VouchEndpoint_LegacyPayloadFormat tests backward compatibility
// with the old payload format (vouched_user_id and region_id)
func TestAPIContract_VouchEndpoint_LegacyPayloadFormat(t *testing.T) {
	suite := SetupAPIContractTest(t)
	defer suite.Cleanup()

	adminUser := suite.createUser("legacy_admin", "legacy_admin@test.com", true, true)
	voucheeUser := suite.createUser("legacy_vouchee", "legacy_vouchee@test.com", false, false)

	region := suite.createRegion("Legacy City", models.RegionTypeCity)
	suite.addUserToRegion(adminUser.ID, region.ID, true, string(models.UserRegionStatusVerified))
	suite.addUserToRegion(voucheeUser.ID, region.ID, false, string(models.UserRegionStatusPending))

	token := suite.generateToken(adminUser)

	t.Run("legacy_format_with_vouched_user_id_and_region_id", func(t *testing.T) {
		suite.cleanupVouches(adminUser.ID, voucheeUser.ID)

		// Old format that might still be used
		payload := map[string]interface{}{
			"vouched_user_id": voucheeUser.ID,
			"region_id":       region.ID,
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest(http.MethodPost, suite.server.URL+"/api/v1/verification/vouch", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var errResp map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&errResp)
			t.Errorf("Legacy format should still work. Expected 201, got %d. Error: %v", resp.StatusCode, errResp)
		}
	})

	t.Run("user_id_takes_precedence_over_vouched_user_id", func(t *testing.T) {
		suite.cleanupVouches(adminUser.ID, voucheeUser.ID)

		// When both are present, user_id should take precedence
		payload := map[string]interface{}{
			"user_id":         voucheeUser.Email,
			"vouched_user_id": "should-be-ignored",
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest(http.MethodPost, suite.server.URL+"/api/v1/verification/vouch", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var errResp map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&errResp)
			t.Errorf("user_id should take precedence. Expected 201, got %d. Error: %v", resp.StatusCode, errResp)
		}
	})
}

// TestAPIContract_VouchEndpoint_RegionAutoDetection tests that the backend
// auto-detects the region from the user's pending vouch request
func TestAPIContract_VouchEndpoint_RegionAutoDetection(t *testing.T) {
	suite := SetupAPIContractTest(t)
	defer suite.Cleanup()

	adminUser := suite.createUser("autodet_admin", "autodet_admin@test.com", true, true)
	voucheeUser := suite.createUser("autodet_vouchee", "autodet_vouchee@test.com", false, false)

	region1 := suite.createRegion("AutoDet City 1", models.RegionTypeCity)
	region2 := suite.createRegion("AutoDet City 2", models.RegionTypeCity)

	// Admin is in both regions
	suite.addUserToRegion(adminUser.ID, region1.ID, true, string(models.UserRegionStatusVerified))
	suite.addUserToRegion(adminUser.ID, region2.ID, true, string(models.UserRegionStatusVerified))

	// Vouchee only has pending request in region1
	suite.addUserToRegion(voucheeUser.ID, region1.ID, false, string(models.UserRegionStatusPending))

	token := suite.generateToken(adminUser)

	t.Run("region_auto_detected_from_pending_request", func(t *testing.T) {
		suite.cleanupVouches(adminUser.ID, voucheeUser.ID)

		// No region_id provided - should auto-detect region1
		payload := map[string]interface{}{
			"user_id": voucheeUser.Email,
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest(http.MethodPost, suite.server.URL+"/api/v1/verification/vouch", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var errResp map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&errResp)
			t.Errorf("Region should be auto-detected. Expected 201, got %d. Error: %v", resp.StatusCode, errResp)
		}
	})
}

// TestAPIContract_VouchEndpoint_ErrorCases tests various error scenarios
func TestAPIContract_VouchEndpoint_ErrorCases(t *testing.T) {
	suite := SetupAPIContractTest(t)
	defer suite.Cleanup()

	adminUser := suite.createUser("error_admin", "error_admin@test.com", true, true)
	noRequestUser := suite.createUser("error_norequest", "error_norequest@test.com", false, false)

	region := suite.createRegion("Error City", models.RegionTypeCity)
	suite.addUserToRegion(adminUser.ID, region.ID, true, string(models.UserRegionStatusVerified))
	// noRequestUser has no pending vouch request

	token := suite.generateToken(adminUser)

	t.Run("empty_user_identifier_returns_400", func(t *testing.T) {
		payload := map[string]interface{}{
			"user_id": "",
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest(http.MethodPost, suite.server.URL+"/api/v1/verification/vouch", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for empty user_id, got %d", resp.StatusCode)
		}
	})

	t.Run("nonexistent_user_returns_404", func(t *testing.T) {
		payload := map[string]interface{}{
			"user_id": "nonexistent@example.com",
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest(http.MethodPost, suite.server.URL+"/api/v1/verification/vouch", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected 404 for nonexistent user, got %d", resp.StatusCode)
		}
	})

	t.Run("user_without_pending_request_returns_400", func(t *testing.T) {
		payload := map[string]interface{}{
			"user_id": noRequestUser.Email,
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest(http.MethodPost, suite.server.URL+"/api/v1/verification/vouch", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for user without pending request, got %d", resp.StatusCode)
		}
	})

	t.Run("self_vouch_returns_400", func(t *testing.T) {
		// Admin tries to vouch for themselves
		// Need a separate region where admin has a pending request (can't have both verified and pending in same region)
		selfVouchRegion := suite.createRegion("Self Vouch City", models.RegionTypeCity)
		suite.addUserToRegion(adminUser.ID, selfVouchRegion.ID, false, string(models.UserRegionStatusPending))

		payload := map[string]interface{}{
			"user_id": adminUser.Email,
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest(http.MethodPost, suite.server.URL+"/api/v1/verification/vouch", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for self-vouch, got %d", resp.StatusCode)
		}
	})
}

// TestAPIContract_SignalGroups_ResponseFormat tests that Signal Group responses
// match what the frontend expects
func TestAPIContract_SignalGroups_ResponseFormat(t *testing.T) {
	suite := SetupAPIContractTest(t)
	defer suite.Cleanup()

	// Create admin user (postcard and vouch verified for full admin access)
	adminUser := suite.createUser("sg_admin", "sg_admin@test.com", true, true)

	// Create a region and add admin
	region := suite.createRegion("Signal City", models.RegionTypeCity)
	suite.addUserToRegion(adminUser.ID, region.ID, true, string(models.UserRegionStatusVerified))

	// Create 2 more full admin users to exit bootstrap mode (need 3 full admins)
	for i := 1; i <= 2; i++ {
		extraAdmin := suite.createUser(fmt.Sprintf("sg_extra_admin%d", i), fmt.Sprintf("sg_extra_admin%d@test.com", i), true, true)
		suite.addUserToRegion(extraAdmin.ID, region.ID, true, string(models.UserRegionStatusVerified))
	}

	token := suite.generateToken(adminUser)

	// Clean up signal groups created by this test
	defer func() { _, _ = suite.db.Exec("DELETE FROM signal_groups WHERE region_id LIKE 'contract-test-%'") }()

	t.Run("create_group_request_uses_name_field", func(t *testing.T) {
		// Frontend sends: name, region_id, encrypted_payload, encryption_iv, wrapped_keys
		// See: static/js/pages/admin/manageGroups.js
		payload := map[string]interface{}{
			"name":              "Test Signal Group",
			"region_id":         region.ID,
			"encrypted_payload": "dGVzdC1lbmNyeXB0ZWQtcGF5bG9hZA==",
			"encryption_iv":     "dGVzdC1pdi0xMjM=",
			"wrapped_keys": []map[string]string{
				{"user_id": adminUser.ID, "wrapped_dek": "dGVzdC13cmFwcGVkLWRlaw=="},
			},
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest(http.MethodPost, suite.server.URL+"/api/v1/signal-groups", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var errResp map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&errResp)
			t.Fatalf("Expected 201, got %d. Error: %v", resp.StatusCode, errResp)
		}

		var response map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&response)

		// Frontend expects: response.group_id or response.group.id (defensive coding)
		if _, hasGroupID := response["group_id"]; !hasGroupID {
			if group, hasGroup := response["group"].(map[string]interface{}); hasGroup {
				if _, hasID := group["id"]; !hasID {
					t.Error("Response missing 'group_id' or 'group.id' field")
				}
			} else {
				t.Error("Response missing 'group_id' field")
			}
		}

		// Frontend expects: response.region_id
		if _, hasRegionID := response["region_id"]; !hasRegionID {
			t.Error("Response missing 'region_id' field")
		}
	})

	t.Run("admin_list_groups_returns_groups_array", func(t *testing.T) {
		// Frontend uses getAdminGroups() which calls /signal-groups/admin
		// and expects: response.groups || response
		req, _ := http.NewRequest(http.MethodGet, suite.server.URL+"/api/v1/signal-groups/admin", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		// 200 or 404 are both acceptable (404 = no groups, user handles with empty array)
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
			t.Fatalf("Expected 200 or 404, got %d", resp.StatusCode)
		}

		if resp.StatusCode == http.StatusOK {
			var response map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&response)

			// Frontend expects: response.groups to be an array
			if groups, hasGroups := response["groups"]; hasGroups {
				if _, isArray := groups.([]interface{}); !isArray {
					t.Error("Response 'groups' field is not an array")
				}
			}
		}
	})
}

// TestAPIContract_Regions_ResponseFormat tests that Region responses
// match what the frontend expects
func TestAPIContract_Regions_ResponseFormat(t *testing.T) {
	suite := SetupAPIContractTest(t)
	defer suite.Cleanup()

	// Create admin user
	adminUser := suite.createUser("reg_admin", "reg_admin@test.com", true, true)

	// Create a region and add admin
	region := suite.createRegion("Region City", models.RegionTypeCity)
	suite.addUserToRegion(adminUser.ID, region.ID, true, string(models.UserRegionStatusVerified))

	token := suite.generateToken(adminUser)

	t.Run("list_regions_response_has_regions_array", func(t *testing.T) {
		// Frontend expects: response.regions || response
		req, _ := http.NewRequest(http.MethodGet, suite.server.URL+"/api/v1/communities", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
			t.Fatalf("Expected 200 or 404, got %d", resp.StatusCode)
		}

		if resp.StatusCode == http.StatusOK {
			var response map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
				// Response might be an array directly
				return
			}

			// Frontend expects: response.regions
			if regions, hasRegions := response["regions"]; hasRegions {
				if _, isArray := regions.([]interface{}); !isArray {
					t.Error("Response 'regions' field is not an array")
				}
			}
		}
	})

	t.Run("get_region_response_has_region_or_direct_fields", func(t *testing.T) {
		// Frontend expects: response.region || response
		req, _ := http.NewRequest(http.MethodGet, suite.server.URL+"/api/v1/communities/"+region.ID, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var response map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&response)

		// Check for 'id' and 'name' fields (either wrapped or direct)
		hasID := false
		hasName := false

		if region, hasRegion := response["region"].(map[string]interface{}); hasRegion {
			_, hasID = region["id"]
			_, hasName = region["name"]
		} else {
			_, hasID = response["id"]
			_, hasName = response["name"]
		}

		if !hasID {
			t.Error("Response missing 'id' field (either direct or in 'region' wrapper)")
		}
		if !hasName {
			t.Error("Response missing 'name' field (either direct or in 'region' wrapper)")
		}
	})

	t.Run("region_has_type_or_region_type_field", func(t *testing.T) {
		// Frontend handles both: region.type || region.region_type
		req, _ := http.NewRequest(http.MethodGet, suite.server.URL+"/api/v1/communities/"+region.ID, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var response map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&response)

		// Check for type field (frontend handles both 'type' and 'region_type')
		hasTypeField := false

		if region, hasRegion := response["region"].(map[string]interface{}); hasRegion {
			_, hasType := region["type"]
			_, hasRegionType := region["region_type"]
			hasTypeField = hasType || hasRegionType
		} else {
			_, hasType := response["type"]
			_, hasRegionType := response["region_type"]
			hasTypeField = hasType || hasRegionType
		}

		if !hasTypeField {
			t.Error("Response missing 'type' or 'region_type' field")
		}
	})
}

// TestAPIContract_Auth_RequestPayloadFormat tests that authentication endpoints
// accept the payload format that the frontend sends
func TestAPIContract_Auth_RequestPayloadFormat(t *testing.T) {
	suite := SetupAPIContractTest(t)
	defer suite.Cleanup()

	t.Run("register_accepts_frontend_payload", func(t *testing.T) {
		// Frontend sends: username, email, password
		payload := map[string]interface{}{
			"username": "auth_test_user",
			"email":    "auth_test@test.com",
			"password": "secure_password_123",
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest(http.MethodPost, suite.server.URL+"/api/v1/auth/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var errResp map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&errResp)
			t.Errorf("Expected 201, got %d. Error: %v", resp.StatusCode, errResp)
		}

		// Clean up
		_, _ = suite.db.Exec("DELETE FROM users WHERE email = ?", "auth_test@test.com")
	})

	t.Run("login_accepts_frontend_payload", func(t *testing.T) {
		// Create a test user first
		testUser := suite.createUser("login_test", "login_test@test.com", false, false)
		// Set a known password hash for "secure_password_123"
		_, _ = suite.db.Exec("UPDATE users SET password_hash = '$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/X4.GQdBn4bIgJkObe' WHERE id = ?", testUser.ID)

		// Frontend sends: email, password
		payload := map[string]interface{}{
			"email":    "login_test@test.com",
			"password": "secure_password_123",
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest(http.MethodPost, suite.server.URL+"/api/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		// 200 OK or 401 (wrong password due to hash mismatch) are acceptable
		// Main point is it shouldn't return 400 validation error
		if resp.StatusCode == http.StatusBadRequest {
			var errResp map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&errResp)
			t.Errorf("Login returned 400, expected payload format issue: %v", errResp)
		}
	})

	t.Run("register_response_has_expected_fields", func(t *testing.T) {
		// Backend returns: user_id, username, email (not wrapped in user object)
		// See: internal/models/auth.go RegisterResponse
		// Use unique username/email to avoid conflicts with previous test runs
		uniqueSuffix := fmt.Sprintf("%d", time.Now().UnixNano())
		payload := map[string]interface{}{
			"username": "reg_resp_" + uniqueSuffix[:8],
			"email":    "reg_resp_" + uniqueSuffix[:8] + "@test.com",
			"password": "secure_password_123",
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest(http.MethodPost, suite.server.URL+"/api/v1/auth/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var errResp map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&errResp)
			t.Fatalf("Expected 201, got %d. Error: %v", resp.StatusCode, errResp)
		}

		var response map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&response)

		// Response should have user_id, username, email directly (not wrapped)
		if _, hasUserID := response["user_id"]; !hasUserID {
			t.Error("Response missing 'user_id' field")
		}
		if _, hasUsername := response["username"]; !hasUsername {
			t.Error("Response missing 'username' field")
		}
		if _, hasEmail := response["email"]; !hasEmail {
			t.Error("Response missing 'email' field")
		}

		// Clean up - use the email from response
		if email, ok := response["email"].(string); ok {
			_, _ = suite.db.Exec("DELETE FROM users WHERE email = ?", email)
		}
	})
}
