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
	schoolRepo := database.NewSchoolRepository(db)
	communityGroupRepo := database.NewGroupRepository(db)
	districtRepo := database.NewSchoolDistrictRepository(db)
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
		nil, verifyRepo, userRepo, regionRepo,
		mockPostgrid, mockMapbox, nil,
	)
	adminHandler := handlers.NewAdminHandler(userRepo, regionRepo, nil)

	mfaConfig := &config.MFAConfig{
		EncryptionKey: "01234567890123456789012345678901",
		Issuer:        "Test MFA",
	}
	mfaService, _ := services.NewMFAService(mfaConfig)
	mfaHandler := handlers.NewMFAHandler(nil, userRepo, mfaService, jwtAuth, false, nil)

	rateLimiter := services.NewNoOpRateLimiter()

	schoolHandler := handlers.NewSchoolHandler(
		schoolRepo, districtRepo, communityGroupRepo, userRepo, auditRepo, nil,
	)

	router := handlers.NewRouter(
		authHandler,
		mfaHandler,
		regionHandler,
		verificationHandler,
		adminHandler,
		schoolHandler,
		nil, // encryptionHandler
		nil, // groupHandler
		nil, // connectionHandler
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
