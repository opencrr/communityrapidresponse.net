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

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/handlers"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/mocks"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// IntegrationTestSuite holds all dependencies for integration tests
type IntegrationTestSuite struct {
	t               *testing.T
	db              *database.DB
	server          *httptest.Server
	userRepo        *database.UserRepository
	regionRepo      *database.RegionRepository
	verifyRepo      *database.VerificationRepository
	groupRepo       *database.SignalGroupRepository
	jwtAuth         *middleware.JWTAuth
	mockPostgrid    *mocks.MockPostgridService
	mockMapbox      *mocks.MockMapboxService
}

// SetupIntegrationTest creates a new test suite with mocked external services
func SetupIntegrationTest(t *testing.T) *IntegrationTestSuite {
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

	// Create repositories
	userRepo := database.NewUserRepository(db)
	regionRepo := database.NewRegionRepository(db)
	verifyRepo := database.NewVerificationRepository(db)
	groupRepo := database.NewSignalGroupRepository(db)
	schoolRepo := database.NewSchoolRepository(db)
	communityGroupRepo := database.NewGroupRepository(db)
	districtRepo := database.NewSchoolDistrictRepository(db)
	auditRepo := database.NewAuditRepository(db)
	encryptedSecretRepo := database.NewEncryptedSecretRepository(db)
	encryptionKeyRepo := database.NewEncryptionKeyRepository(db)
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

	// Create handlers with mock services
	authHandler := handlers.NewAuthHandler(userRepo, jwtAuth)
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

	encryptionHandler := handlers.NewEncryptionHandler(encryptionKeyRepo, encryptedSecretRepo, regionRepo, schoolRepo, userRepo, communityGroupRepo, groupRepo)

	// Create router (rate limiting disabled for tests)
	router := handlers.NewRouter(
		authHandler, mfaHandler, regionHandler, verificationHandler, adminHandler,
		schoolHandler, encryptionHandler, nil, nil, jwtAuth, nil, nil, nil,
		[]string{"*"}, nil,
	)
	handler := router.Setup()

	server := httptest.NewServer(handler)

	suite := &IntegrationTestSuite{
		t:               t,
		db:              db,
		server:          server,
		userRepo:        userRepo,
		regionRepo:      regionRepo,
		verifyRepo:      verifyRepo,
		groupRepo:       groupRepo,
		jwtAuth:         jwtAuth,
		mockPostgrid:    mockPostgrid,
		mockMapbox:      mockMapbox,
	}

	t.Cleanup(func() {
		server.Close()
		_ = db.Close()
	})

	return suite
}

// Helper methods

func (s *IntegrationTestSuite) request(method, path string, body interface{}, token string) *http.Response {
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

func (s *IntegrationTestSuite) registerUser(username, email, password string) (string, string) {
	resp := s.request("POST", "/api/v1/auth/register", map[string]string{
		"username": username,
		"email":    email,
		"password": password,
	}, "")
	defer func() { _ = resp.Body.Close() }()

	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)

	userID := registerResp.UserID

	// If user already exists, get their ID from the database
	if userID == "" {
		var id string
		_ = s.db.QueryRowContext(context.Background(), "SELECT id FROM users WHERE email = ?", email).Scan(&id)
		userID = id
	}

	// Disable MFA requirements for test user so login returns a full token
	_, _ = s.db.ExecContext(context.Background(), "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", userID)

	// Login to get token
	resp2 := s.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, "")
	defer func() { _ = resp2.Body.Close() }()

	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp2.Body).Decode(&loginResp)

	return userID, loginResp.Token
}

func (s *IntegrationTestSuite) cleanup(userIDs ...string) {
	ctx := context.Background()
	for _, id := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM verification_requests WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM vouches WHERE voucher_user_id = ? OR vouched_user_id = ?", id, id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	}
}

// makeUserVouchVerified sets a user as vouch-verified in the database.
// Call this BEFORE login/re-login so the JWT includes the correct claims.
func (s *IntegrationTestSuite) makeUserVouchVerified(userID string) {
	_, err := s.db.ExecContext(context.Background(),
		"UPDATE users SET vouch_verified = TRUE, verification_tier = 1 WHERE id = ?", userID)
	if err != nil {
		s.t.Fatalf("Failed to make user vouch-verified: %v", err)
	}
}

// reloginUser logs in an existing user and returns a fresh token with updated claims.
func (s *IntegrationTestSuite) reloginUser(email, password string) string {
	resp := s.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, "")
	defer func() { _ = resp.Body.Close() }()

	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	return loginResp.Token
}

func (s *IntegrationTestSuite) cleanupRegion(regionID string) {
	ctx := context.Background()
	_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE region_id = ?", regionID)
	_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", regionID)
	_, _ = s.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", regionID)
}

// Tests

func TestIntegration_VerificationFlow_Success(t *testing.T) {
	suite := SetupIntegrationTest(t)

	// Reset mocks
	suite.mockPostgrid.Reset()
	suite.mockMapbox.Reset()

	// Configure mock responses
	suite.mockPostgrid.DefaultDeliverable = true
	suite.mockPostgrid.DefaultIsPOBox = false
	suite.mockPostgrid.DefaultIsCMRA = false

	suite.mockMapbox.DefaultBoundaryType = "city"
	suite.mockMapbox.DefaultBoundaryName = "San Francisco"
	suite.mockMapbox.DefaultBoundaryState = "California"

	// Register user, make vouch-verified, then re-login for updated token
	userID, _ := suite.registerUser("verifyuser", "verify@test.com", "securepassword123")
	defer suite.cleanup(userID)
	suite.makeUserVouchVerified(userID)
	token := suite.reloginUser("verify@test.com", "securepassword123")

	// Create a region that matches the mock geocode result
	ctx := context.Background()
	region := &models.GeographicRegion{
		Name:       "San Francisco",
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = suite.regionRepo.Create(ctx, region, geoJSON)
	defer suite.cleanupRegion(region.ID)

	t.Run("request postcard verification", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 Main St",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		// Check that services were called
		if len(suite.mockPostgrid.ValidateAddressCalls) == 0 {
			t.Error("Expected ValidateAddress to be called")
		}
		if len(suite.mockMapbox.GeocodeAddressCalls) == 0 {
			t.Error("Expected GeocodeAddress to be called")
		}
		if len(suite.mockPostgrid.SendPostcardCalls) == 0 {
			t.Error("Expected SendPostcard to be called")
		}

		// Verify the address was passed correctly
		if len(suite.mockPostgrid.ValidateAddressCalls) > 0 {
			call := suite.mockPostgrid.ValidateAddressCalls[0]
			if call.Address.Line1 != "123 Main St" {
				t.Errorf("Expected address line1 '123 Main St', got '%s'", call.Address.Line1)
			}
		}
	})
}

func TestIntegration_VerificationFlow_POBoxRejected(t *testing.T) {
	suite := SetupIntegrationTest(t)

	// Reset mocks
	suite.mockPostgrid.Reset()
	suite.mockMapbox.Reset()

	// Configure mock to detect PO Box
	suite.mockPostgrid.DefaultDeliverable = true
	suite.mockPostgrid.DefaultIsPOBox = true // Force PO Box detection

	// Register user, make vouch-verified, then re-login for updated token
	userID, _ := suite.registerUser("poboxuser", "pobox@test.com", "securepassword123")
	defer suite.cleanup(userID)
	suite.makeUserVouchVerified(userID)
	token := suite.reloginUser("pobox@test.com", "securepassword123")

	// Create a region
	ctx := context.Background()
	region := &models.GeographicRegion{
		Name:       "San Francisco",
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = suite.regionRepo.Create(ctx, region, geoJSON)
	defer suite.cleanupRegion(region.ID)

	t.Run("rejects PO Box address", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "PO Box 123",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 400 for PO Box, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body["error"] != "po_box_not_allowed" {
			t.Errorf("Expected error code 'po_box_not_allowed', got '%v'", body["error"])
		}
	})
}

func TestIntegration_VerificationFlow_CMRARejected(t *testing.T) {
	suite := SetupIntegrationTest(t)

	// Reset mocks
	suite.mockPostgrid.Reset()

	// Configure mock to detect CMRA
	suite.mockPostgrid.DefaultDeliverable = true
	suite.mockPostgrid.DefaultIsCMRA = true

	// Register user, make vouch-verified, then re-login for updated token
	userID, _ := suite.registerUser("cmrauser", "cmra@test.com", "securepassword123")
	defer suite.cleanup(userID)
	suite.makeUserVouchVerified(userID)
	token := suite.reloginUser("cmra@test.com", "securepassword123")

	// Create a region
	ctx := context.Background()
	region := &models.GeographicRegion{
		Name:       "San Francisco",
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = suite.regionRepo.Create(ctx, region, geoJSON)
	defer suite.cleanupRegion(region.ID)

	t.Run("rejects CMRA address", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 Main St #PMB456",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 400 for CMRA, got %d: %v", resp.StatusCode, body)
		}
	})
}

func TestIntegration_VerificationFlow_CommercialRejected(t *testing.T) {
	suite := SetupIntegrationTest(t)

	// Reset mocks
	suite.mockPostgrid.Reset()

	// Configure mock to detect commercial address
	suite.mockPostgrid.DefaultDeliverable = true
	suite.mockPostgrid.DefaultIsCommercial = true

	// Register user, make vouch-verified, then re-login for updated token
	userID, _ := suite.registerUser("commercialuser", "commercial@test.com", "securepassword123")
	defer suite.cleanup(userID)
	suite.makeUserVouchVerified(userID)
	token := suite.reloginUser("commercial@test.com", "securepassword123")

	// Create a region
	ctx := context.Background()
	region := &models.GeographicRegion{
		Name:       "San Francisco",
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = suite.regionRepo.Create(ctx, region, geoJSON)
	defer suite.cleanupRegion(region.ID)

	t.Run("rejects commercial address", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "456 Business Park Dr",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 400 for commercial address, got %d: %v", resp.StatusCode, body)
		}

		// Verify error message
		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body["error"] != "commercial_not_allowed" {
			t.Errorf("Expected error 'commercial_not_allowed', got %v", body["error"])
		}
	})
}

func TestIntegration_VerificationFlow_UndeliverableRejected(t *testing.T) {
	suite := SetupIntegrationTest(t)

	// Reset mocks
	suite.mockPostgrid.Reset()

	// Configure mock to return undeliverable
	suite.mockPostgrid.DefaultDeliverable = false

	// Register user, make vouch-verified, then re-login for updated token
	userID, _ := suite.registerUser("undelivuser", "undeliv@test.com", "securepassword123")
	defer suite.cleanup(userID)
	suite.makeUserVouchVerified(userID)
	token := suite.reloginUser("undeliv@test.com", "securepassword123")

	// Create a region
	ctx := context.Background()
	region := &models.GeographicRegion{
		Name:       "Test Region",
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = suite.regionRepo.Create(ctx, region, geoJSON)
	defer suite.cleanupRegion(region.ID)

	t.Run("rejects undeliverable address", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "99999 Nonexistent St",
				"city":        "Fake City",
				"state":       "XX",
				"postal_code": "00000",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400 for undeliverable, got %d", resp.StatusCode)
		}
	})
}

func TestIntegration_VerificationFlow_ServiceFailure(t *testing.T) {
	suite := SetupIntegrationTest(t)

	// Reset mocks
	suite.mockPostgrid.Reset()

	// Configure mock to fail
	suite.mockPostgrid.ShouldFail = true
	suite.mockPostgrid.FailError = fmt.Errorf("postgrid service unavailable")

	// Register user, make vouch-verified, then re-login for updated token
	userID, _ := suite.registerUser("failuser", "fail@test.com", "securepassword123")
	defer suite.cleanup(userID)
	suite.makeUserVouchVerified(userID)
	token := suite.reloginUser("fail@test.com", "securepassword123")

	// Create a region
	ctx := context.Background()
	region := &models.GeographicRegion{
		Name:       "San Francisco",
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = suite.regionRepo.Create(ctx, region, geoJSON)
	defer suite.cleanupRegion(region.ID)

	t.Run("handles service failure gracefully", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 Main St",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadGateway {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 502 for service failure, got %d: %v", resp.StatusCode, body)
		}
	})
}

func TestIntegration_MockServiceTracking(t *testing.T) {
	suite := SetupIntegrationTest(t)

	// Reset mocks
	suite.mockPostgrid.Reset()
	suite.mockMapbox.Reset()

	// Register user, make vouch-verified, then re-login for updated token
	userID, _ := suite.registerUser("trackuser", "track@test.com", "securepassword123")
	defer suite.cleanup(userID)
	suite.makeUserVouchVerified(userID)
	token := suite.reloginUser("track@test.com", "securepassword123")

	// Create a region
	ctx := context.Background()
	region := &models.GeographicRegion{
		Name:       "San Francisco",
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = suite.regionRepo.Create(ctx, region, geoJSON)
	defer suite.cleanupRegion(region.ID)

	t.Run("tracks all service calls", func(t *testing.T) {
		// Make verification request
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "456 Oak Ave",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94103",
			},
		}, token)
		_ = resp.Body.Close()

		// Verify call tracking
		if len(suite.mockPostgrid.ValidateAddressCalls) != 1 {
			t.Errorf("Expected 1 ValidateAddress call, got %d", len(suite.mockPostgrid.ValidateAddressCalls))
		}

		if len(suite.mockMapbox.GeocodeAddressCalls) != 1 {
			t.Errorf("Expected 1 GeocodeAddress call, got %d", len(suite.mockMapbox.GeocodeAddressCalls))
		}

		// Skip further checks if no calls were made
		if len(suite.mockPostgrid.ValidateAddressCalls) == 0 {
			t.Skip("No ValidateAddress calls made, skipping detailed checks")
		}

		// Check address details were captured
		validateCall := suite.mockPostgrid.ValidateAddressCalls[0]
		if validateCall.Address.Line1 != "456 Oak Ave" {
			t.Errorf("Expected address '456 Oak Ave', got '%s'", validateCall.Address.Line1)
		}
		if validateCall.Address.City != "San Francisco" {
			t.Errorf("Expected city 'San Francisco', got '%s'", validateCall.Address.City)
		}

		// Check postcard was sent with verification code
		if len(suite.mockPostgrid.SendPostcardCalls) > 0 {
			postcardCall := suite.mockPostgrid.SendPostcardCalls[0]
			if postcardCall.VerificationCode == "" {
				t.Error("Expected verification code in postcard call")
			}
			if postcardCall.Address.Line1 != "456 Oak Ave" {
				t.Errorf("Expected address in postcard call")
			}
		}
	})
}

func TestIntegration_CustomMockBehavior(t *testing.T) {
	suite := SetupIntegrationTest(t)

	// Reset mocks
	suite.mockPostgrid.Reset()
	suite.mockMapbox.Reset()

	// Register user, make vouch-verified, then re-login for updated token
	userID, _ := suite.registerUser("customuser", "custom@test.com", "securepassword123")
	defer suite.cleanup(userID)
	suite.makeUserVouchVerified(userID)
	token := suite.reloginUser("custom@test.com", "securepassword123")

	// Create a region
	ctx := context.Background()
	region := &models.GeographicRegion{
		Name:       "Test Region",
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = suite.regionRepo.Create(ctx, region, geoJSON)
	defer suite.cleanupRegion(region.ID)

	t.Run("uses custom mock function", func(t *testing.T) {
		// Set up custom behavior
		customCalled := false
		suite.mockPostgrid.ValidateAddressFunc = func(ctx context.Context, address *models.Address) (*services.AddressValidationResult, error) {
			customCalled = true
			// Simulate a specific address being undeliverable
			if address.PostalCode == "00000" {
				return &services.AddressValidationResult{
					IsDeliverable: false,
					Reason:        "Invalid postal code",
				}, nil
			}
			return &services.AddressValidationResult{
				IsDeliverable: true,
			}, nil
		}

		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 Test St",
				"city":        "Test City",
				"state":       "TS",
				"postal_code": "00000",
			},
		}, token)
		_ = resp.Body.Close()

		if !customCalled {
			t.Error("Expected custom mock function to be called")
		}
	})
}

// =============================================================================
// Regions Admin Endpoint Tests
// =============================================================================

func TestIntegration_PostcardVerification_UserAddedToCityRegion(t *testing.T) {
	suite := SetupIntegrationTest(t)

	ctx := context.Background()

	// Reset mocks
	suite.mockPostgrid.Reset()
	suite.mockMapbox.Reset()

	// Configure mock responses
	suite.mockPostgrid.DefaultDeliverable = true
	suite.mockPostgrid.DefaultIsPOBox = false
	suite.mockPostgrid.DefaultIsCMRA = false

	// Set unique coordinates to avoid conflicts with other tests
	// Using rural Montana area: 47.0, -110.0
	suite.mockMapbox.DefaultLatitude = 47.0
	suite.mockMapbox.DefaultLongitude = -110.0
	suite.mockMapbox.DefaultBoundaryType = "city"
	suite.mockMapbox.DefaultBoundaryName = "Postcard Test City"
	suite.mockMapbox.DefaultBoundaryState = "Montana"

	// Register user, make vouch-verified, then re-login for updated token
	userID, _ := suite.registerUser("postcardcityuser", "postcardcity@test.com", "securepassword123")
	defer suite.cleanup(userID)
	suite.makeUserVouchVerified(userID)
	token := suite.reloginUser("postcardcity@test.com", "securepassword123")

	// Track created regions for cleanup
	var createdRegionIDs []string
	defer func() {
		// Clean up in reverse order (city first, then county, then state)
		for i := len(createdRegionIDs) - 1; i >= 0; i-- {
			suite.cleanupRegion(createdRegionIDs[i])
		}
	}()

	t.Run("request postcard verification auto-creates region hierarchy", func(t *testing.T) {
		// Request postcard verification WITHOUT specifying region_id
		// This triggers auto-creation of the region hierarchy
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"address": map[string]string{
				"line1":       "123 Main St",
				"city":        "Postcard Test City",
				"state":       "MT",
				"postal_code": "59001",
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

		// Verify region was created and returned
		regionInfo, ok := body["region"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected 'region' in response")
		}

		regionID, ok := regionInfo["id"].(string)
		if !ok || regionID == "" {
			t.Fatal("Expected region ID in response")
		}

		// Track the created region for cleanup
		createdRegionIDs = append(createdRegionIDs, regionID)

		// Verify the region is a city type
		region, err := suite.regionRepo.GetByID(ctx, regionID)
		if err != nil {
			t.Fatalf("Failed to get region: %v", err)
		}

		if region.RegionType != models.RegionTypeCity {
			t.Errorf("Expected region type 'city', got '%s'", region.RegionType)
		}

		// Verify parent hierarchy was created (county and state)
		if region.ParentRegionID != nil {
			countyRegion, err := suite.regionRepo.GetByID(ctx, *region.ParentRegionID)
			if err != nil {
				t.Fatalf("Failed to get county region: %v", err)
			}
			createdRegionIDs = append(createdRegionIDs, countyRegion.ID)

			if countyRegion.RegionType != models.RegionTypeCounty {
				t.Errorf("Expected parent region type 'county', got '%s'", countyRegion.RegionType)
			}

			if countyRegion.ParentRegionID != nil {
				stateRegion, err := suite.regionRepo.GetByID(ctx, *countyRegion.ParentRegionID)
				if err != nil {
					t.Fatalf("Failed to get state region: %v", err)
				}
				createdRegionIDs = append(createdRegionIDs, stateRegion.ID)

				if stateRegion.RegionType != models.RegionTypeState {
					t.Errorf("Expected grandparent region type 'state', got '%s'", stateRegion.RegionType)
				}
			}
		}
	})

	t.Run("verify code adds user to city region only", func(t *testing.T) {
		// Get the verification code from the mock
		if len(suite.mockPostgrid.SendPostcardCalls) == 0 {
			t.Fatal("Expected SendPostcard to be called")
		}
		verificationCode := suite.mockPostgrid.SendPostcardCalls[0].VerificationCode

		// Verify the code
		resp := suite.request("POST", "/api/v1/verification/postcard/verify", map[string]string{
			"verification_code": verificationCode,
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Fatalf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}

		// Verify user was added to exactly one region (the city)
		var count int
		err := suite.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_regions WHERE user_id = ?", userID).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to count user_regions: %v", err)
		}

		if count != 1 {
			t.Errorf("Expected user to be in exactly 1 region, got %d", count)
		}

		// Verify the region is the city
		var regionID string
		err = suite.db.QueryRowContext(ctx, "SELECT region_id FROM user_regions WHERE user_id = ?", userID).Scan(&regionID)
		if err != nil {
			t.Fatalf("Failed to get user region: %v", err)
		}

		region, err := suite.regionRepo.GetByID(ctx, regionID)
		if err != nil {
			t.Fatalf("Failed to get region: %v", err)
		}

		if region.RegionType != models.RegionTypeCity {
			t.Errorf("Expected user to be added to city region, but was added to '%s' region", region.RegionType)
		}

		// Verify the user is NOT directly added to county or state
		if len(createdRegionIDs) > 1 {
			for _, rid := range createdRegionIDs[1:] { // Skip the city (first one)
				var memberCount int
				err := suite.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_regions WHERE user_id = ? AND region_id = ?", userID, rid).Scan(&memberCount)
				if err != nil {
					t.Fatalf("Failed to check membership: %v", err)
				}
				if memberCount > 0 {
					parentRegion, _ := suite.regionRepo.GetByID(ctx, rid)
					t.Errorf("User should NOT be directly added to %s region, but was", parentRegion.RegionType)
				}
			}
		}
	})

	t.Run("user postcard_verified flag is set", func(t *testing.T) {
		user, err := suite.userRepo.GetByID(ctx, userID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}

		if !user.PostcardVerified {
			t.Error("Expected postcard_verified to be true")
		}

		if user.VerificationTier != models.TierPostcard {
			t.Errorf("Expected verification tier %d, got %d", models.TierPostcard, user.VerificationTier)
		}
	})
}

func TestIntegration_PostcardVerification_ExistingRegionHierarchy(t *testing.T) {
	suite := SetupIntegrationTest(t)

	ctx := context.Background()

	// Reset mocks
	suite.mockPostgrid.Reset()
	suite.mockMapbox.Reset()

	// Configure mock responses
	suite.mockPostgrid.DefaultDeliverable = true
	suite.mockPostgrid.DefaultIsPOBox = false
	suite.mockPostgrid.DefaultIsCMRA = false

	// Set unique coordinates: rural Wyoming area 43.0, -108.0
	suite.mockMapbox.DefaultLatitude = 43.0
	suite.mockMapbox.DefaultLongitude = -108.0
	suite.mockMapbox.DefaultBoundaryType = "city"
	suite.mockMapbox.DefaultBoundaryName = "Existing Hierarchy City"
	suite.mockMapbox.DefaultBoundaryState = "Wyoming"

	// Pre-create a region hierarchy (state -> county -> city) that contains the mock coordinates
	stateGeoJSON := `{"type":"Polygon","coordinates":[[[-111.0,41.0],[-104.0,41.0],[-104.0,45.0],[-111.0,45.0],[-111.0,41.0]]]}`
	stateRegion := &models.GeographicRegion{
		Name:       "Existing Hierarchy State",
		RegionType: models.RegionTypeState,
	}
	_ = suite.regionRepo.Create(ctx, stateRegion, stateGeoJSON)
	defer suite.cleanupRegion(stateRegion.ID)

	countyGeoJSON := `{"type":"Polygon","coordinates":[[[-109.0,42.0],[-107.0,42.0],[-107.0,44.0],[-109.0,44.0],[-109.0,42.0]]]}`
	countyRegion := &models.GeographicRegion{
		Name:           "Existing Hierarchy County",
		RegionType:     models.RegionTypeCounty,
		ParentRegionID: &stateRegion.ID,
	}
	_ = suite.regionRepo.Create(ctx, countyRegion, countyGeoJSON)
	defer suite.cleanupRegion(countyRegion.ID)

	cityGeoJSON := `{"type":"Polygon","coordinates":[[[-108.5,42.5],[-107.5,42.5],[-107.5,43.5],[-108.5,43.5],[-108.5,42.5]]]}`
	cityRegion := &models.GeographicRegion{
		Name:           "Existing Hierarchy City",
		RegionType:     models.RegionTypeCity,
		ParentRegionID: &countyRegion.ID,
	}
	_ = suite.regionRepo.Create(ctx, cityRegion, cityGeoJSON)
	defer suite.cleanupRegion(cityRegion.ID)

	// Register user, make vouch-verified, then re-login for updated token
	userID, _ := suite.registerUser("existinghierarchyuser", "existinghierarchy@test.com", "securepassword123")
	defer suite.cleanup(userID)
	suite.makeUserVouchVerified(userID)
	token := suite.reloginUser("existinghierarchy@test.com", "securepassword123")

	t.Run("uses existing city region when coordinates match", func(t *testing.T) {
		// Request postcard verification WITHOUT specifying region_id
		// Should detect and use the existing city region
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"address": map[string]string{
				"line1":       "456 Oak Ave",
				"city":        "Existing Hierarchy City",
				"state":       "WY",
				"postal_code": "82001",
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

		// Verify the existing city region was used
		regionInfo, ok := body["region"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected 'region' in response")
		}

		returnedRegionID, ok := regionInfo["id"].(string)
		if !ok || returnedRegionID == "" {
			t.Fatal("Expected region ID in response")
		}

		// Should be the existing city region
		if returnedRegionID != cityRegion.ID {
			t.Errorf("Expected existing city region ID %s, got %s", cityRegion.ID, returnedRegionID)
		}

		// Verify no new regions were created (the region should NOT be marked as created)
		if created, ok := regionInfo["created"].(bool); ok && created {
			t.Error("Expected region to NOT be marked as created since it already existed")
		}
	})

	t.Run("verify code adds user to existing city region", func(t *testing.T) {
		// Get the verification code from the mock
		if len(suite.mockPostgrid.SendPostcardCalls) == 0 {
			t.Fatal("Expected SendPostcard to be called")
		}
		verificationCode := suite.mockPostgrid.SendPostcardCalls[0].VerificationCode

		// Verify the code
		resp := suite.request("POST", "/api/v1/verification/postcard/verify", map[string]string{
			"verification_code": verificationCode,
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Fatalf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}

		// Verify user was added to the city region only
		var regionID string
		err := suite.db.QueryRowContext(ctx, "SELECT region_id FROM user_regions WHERE user_id = ?", userID).Scan(&regionID)
		if err != nil {
			t.Fatalf("Failed to get user region: %v", err)
		}

		if regionID != cityRegion.ID {
			t.Errorf("Expected user to be added to city region %s, got %s", cityRegion.ID, regionID)
		}

		// Verify user is NOT in county or state regions directly
		var countyMemberCount, stateMemberCount int
		_ = suite.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_regions WHERE user_id = ? AND region_id = ?", userID, countyRegion.ID).Scan(&countyMemberCount)
		_ = suite.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_regions WHERE user_id = ? AND region_id = ?", userID, stateRegion.ID).Scan(&stateMemberCount)

		if countyMemberCount > 0 {
			t.Error("User should NOT be directly added to county region")
		}
		if stateMemberCount > 0 {
			t.Error("User should NOT be directly added to state region")
		}
	})
}

// TestIntegration_RegionAPI_MultiPolygonGeometry tests that MultiPolygon geometry is correctly
// returned in API responses. This covers a bug where NYC's MultiPolygon boundary wasn't displaying
// because the GeoJSONGeometry struct only supported Polygon coordinates.
func TestIntegration_RegionAPI_MultiPolygonGeometry(t *testing.T) {
	suite := SetupIntegrationTest(t)
	ctx := context.Background()

	// Create a superuser
	userID, _ := suite.registerUser("multipoly_test_user", "multipoly_test@test.com", "TestPassword123!")
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET is_superuser = TRUE, verification_tier = 2, postcard_verified = TRUE, vouch_verified = TRUE WHERE id = ?", userID)

	// Get a fresh token with superuser privileges
	resp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "multipoly_test@test.com",
		"password": "TestPassword123!",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token := loginResp.Token

	// Create a region with MultiPolygon geometry directly in the database
	// This simulates what OSM returns for NYC
	multiPolygonGeoJSON := `{"type":"MultiPolygon","coordinates":[[[[-74.0,40.7],[-74.0,40.8],[-73.9,40.8],[-73.9,40.7],[-74.0,40.7]]]]}`

	region := &models.GeographicRegion{
		Name:       "MultiPoly Test City",
		RegionType: models.RegionTypeCity,
		CreatedBy:  &userID,
	}
	if err := suite.regionRepo.Create(ctx, region, multiPolygonGeoJSON); err != nil {
		t.Fatalf("Failed to create region with MultiPolygon: %v", err)
	}

	defer suite.cleanupRegion(region.ID)
	defer suite.cleanup(userID)

	// Add user to region so they can access it
	_ = suite.regionRepo.AddUserToRegion(ctx, userID, region.ID, true)

	t.Run("MultiPolygon geometry is returned in API response", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/communities/"+region.ID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var regionResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&regionResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Check that geometry is present
		geometry, ok := regionResp["geometry"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected geometry field in response")
		}

		// Check that type is MultiPolygon
		geomType, ok := geometry["type"].(string)
		if !ok || geomType != "MultiPolygon" {
			t.Errorf("Expected geometry type 'MultiPolygon', got '%v'", geometry["type"])
		}

		// Check that coordinates exist and are valid
		coords, ok := geometry["coordinates"].([]interface{})
		if !ok || len(coords) == 0 {
			t.Error("Expected non-empty coordinates array")
		}
	})
}

// TestIntegration_VerificationFlow_NYCLikeAddress tests the full verification flow for an
// address with a locality (like Brooklyn in NYC). This covers the extended 5-level hierarchy:
// State -> County -> City -> Locality -> Neighborhood
func TestIntegration_VerificationFlow_NYCLikeAddress(t *testing.T) {
	suite := SetupIntegrationTest(t)
	ctx := context.Background()

	// Clean up any leftover regions from previous test runs to ensure isolation
	_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id IN (SELECT id FROM geographic_regions WHERE name IN ('Williamsburg', 'Brooklyn', 'New York, New York', 'Kings County') OR (name = 'New York' AND region_type = 'state'))")
	_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE name = 'Williamsburg' AND region_type = 'neighborhood'")
	_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE name = 'Brooklyn' AND region_type = 'locality'")
	_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE name = 'New York, New York' AND region_type = 'city'")
	_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE name = 'Kings County' AND region_type = 'county'")
	_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE name = 'New York' AND region_type = 'state'")

	// Reset and configure mocks for NYC-like address
	suite.mockPostgrid.Reset()
	suite.mockMapbox.Reset()

	// Configure mock to return NYC-like geocode result
	suite.mockMapbox.DefaultLatitude = 40.7081
	suite.mockMapbox.DefaultLongitude = -73.9571
	suite.mockMapbox.DefaultBoundaryType = "city"
	suite.mockMapbox.DefaultBoundaryName = "New York"
	suite.mockMapbox.DefaultBoundaryState = "New York"
	suite.mockMapbox.DefaultCountyName = "Kings County"
	suite.mockMapbox.DefaultLocalityName = "Brooklyn"
	suite.mockMapbox.DefaultNeighborhoodName = "Williamsburg"
	suite.mockMapbox.DefaultPlaceID = "place.nyc_123"

	// Use custom functions for boundary lookups to return proper GeoJSON
	suite.mockMapbox.GetCityBoundaryFunc = func(ctx context.Context, lat, lng float64, geocodeResult *services.GeocodeResult, address *models.Address) (*services.CityBoundary, error) {
		return &services.CityBoundary{
			PlaceID: "osm_nyc",
			Name:    "New York",
			State:   "New York",
			Type:    "city",
			GeoJSON: `{"type":"Polygon","coordinates":[[[-74.3,40.5],[-73.7,40.5],[-73.7,40.9],[-74.3,40.9],[-74.3,40.5]]]}`,
			Center:  [2]float64{lng, lat},
		}, nil
	}

	suite.mockMapbox.GetStateBoundaryFunc = func(ctx context.Context, stateName string, lat, lng float64) (*services.StateBoundary, error) {
		return &services.StateBoundary{
			PlaceID: "osm_ny_state",
			Name:    "New York",
			GeoJSON: `{"type":"Polygon","coordinates":[[[-79.8,40.5],[-71.8,40.5],[-71.8,45.0],[-79.8,45.0],[-79.8,40.5]]]}`,
			Center:  [2]float64{lng, lat},
		}, nil
	}

	suite.mockMapbox.GetCountyForCoordinatesFunc = func(ctx context.Context, lat, lng float64, stateName string) (*services.CountyBoundary, error) {
		return &services.CountyBoundary{
			PlaceID: "osm_kings",
			Name:    "Kings County",
			State:   stateName,
			GeoJSON: `{"type":"Polygon","coordinates":[[[-74.05,40.57],[-73.83,40.57],[-73.83,40.74],[-74.05,40.74],[-74.05,40.57]]]}`,
			Center:  [2]float64{lng, lat},
		}, nil
	}

	suite.mockMapbox.GetLocalityBoundaryFunc = func(ctx context.Context, localityName, cityName, stateName string, lat, lng float64) (*services.LocalityBoundary, error) {
		return &services.LocalityBoundary{
			PlaceID: "osm_brooklyn",
			Name:    "Brooklyn",
			State:   stateName,
			City:    cityName,
			GeoJSON: `{"type":"Polygon","coordinates":[[[-74.05,40.57],[-73.83,40.57],[-73.83,40.74],[-74.05,40.74],[-74.05,40.57]]]}`,
			Center:  [2]float64{lng, lat},
		}, nil
	}

	suite.mockMapbox.GetNeighborhoodBoundaryFunc = func(ctx context.Context, neighborhoodName, localityName, cityName, stateName string, lat, lng float64) (*services.NeighborhoodBoundary, error) {
		// Most neighborhoods don't have boundaries
		return &services.NeighborhoodBoundary{
			PlaceID:  "",
			Name:     neighborhoodName,
			State:    stateName,
			City:     cityName,
			Locality: localityName,
			GeoJSON:  "", // No geometry for neighborhood
			Center:   [2]float64{lng, lat},
		}, nil
	}

	// Register user, make vouch-verified, then re-login for updated token
	userID, _ := suite.registerUser("nyc_verify_user", "nyc_verify@test.com", "TestPassword123!")
	defer suite.cleanup(userID)
	suite.makeUserVouchVerified(userID)
	token := suite.reloginUser("nyc_verify@test.com", "TestPassword123!")

	t.Run("verification creates full 5-level hierarchy", func(t *testing.T) {
		// Submit verification request with Brooklyn address
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"address": map[string]string{
				"line1":       "123 Bedford Ave",
				"city":        "Brooklyn",
				"state":       "NY",
				"postal_code": "11211",
				"country":     "US",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var errBody map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&errBody)
			t.Fatalf("Expected 201, got %d: %v", resp.StatusCode, errBody)
		}

		var verifyResp models.PostcardVerificationResponse
		_ = json.NewDecoder(resp.Body).Decode(&verifyResp)

		if verifyResp.VerificationID == "" {
			t.Error("Expected verification ID")
		}

		// Get the verification code from the database (it's not returned to client)
		var verificationCode string
		err := suite.db.QueryRowContext(ctx, "SELECT verification_code FROM verification_requests WHERE id = ?", verifyResp.VerificationID).Scan(&verificationCode)
		if err != nil {
			t.Fatalf("Failed to get verification code from database: %v", err)
		}

		// Now verify with the code
		verifyResp2 := suite.request("POST", "/api/v1/verification/postcard/verify", map[string]string{
			"verification_code": verificationCode,
		}, token)
		defer func() { _ = verifyResp2.Body.Close() }()

		if verifyResp2.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			_ = json.NewDecoder(verifyResp2.Body).Decode(&errBody)
			t.Fatalf("Verification failed: %d: %v", verifyResp2.StatusCode, errBody)
		}

		// Check that the full hierarchy was created
		// State
		var stateCount int
		err = suite.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM geographic_regions WHERE name = 'New York' AND region_type = 'state'").Scan(&stateCount)
		if err != nil || stateCount == 0 {
			t.Error("Expected state region to be created")
		}

		// County
		var countyCount int
		err = suite.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM geographic_regions WHERE name = 'Kings County' AND region_type = 'county'").Scan(&countyCount)
		if err != nil || countyCount == 0 {
			t.Error("Expected county region to be created")
		}

		// City
		var cityCount int
		err = suite.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM geographic_regions WHERE name LIKE 'New York%' AND region_type = 'city'").Scan(&cityCount)
		if err != nil || cityCount == 0 {
			t.Error("Expected city region to be created")
		}

		// Locality
		var localityCount int
		err = suite.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM geographic_regions WHERE name = 'Brooklyn' AND region_type = 'locality'").Scan(&localityCount)
		if err != nil || localityCount == 0 {
			t.Error("Expected locality region to be created")
		}

		// Neighborhood (may not be created if it has no geometry)
		var neighborhoodCount int
		_ = suite.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM geographic_regions WHERE name = 'Williamsburg' AND region_type = 'neighborhood'").Scan(&neighborhoodCount)
		// Neighborhood creation is optional, so don't fail if not created

		// Check user was assigned to the most specific region
		var userRegionType string
		err = suite.db.QueryRowContext(ctx, `
			SELECT gr.region_type
			FROM user_regions ur
			JOIN geographic_regions gr ON ur.region_id = gr.id
			WHERE ur.user_id = ?
		`, userID).Scan(&userRegionType)
		if err != nil {
			t.Fatalf("Failed to get user region: %v", err)
		}

		// User should be assigned to locality or neighborhood (most specific available)
		if userRegionType != "locality" && userRegionType != "neighborhood" {
			t.Errorf("Expected user to be in locality or neighborhood, got %s", userRegionType)
		}

		// Clean up created regions
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", userID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE name = 'Williamsburg' AND region_type = 'neighborhood'")
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE name = 'Brooklyn' AND region_type = 'locality'")
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE name LIKE 'New York%' AND region_type = 'city'")
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE name = 'Kings County' AND region_type = 'county'")
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE name = 'New York' AND region_type = 'state'")
	})
}

// TestIntegration_RegionAPI_SubRegionsExcludeAncestors tests that ancestor regions are NOT
// returned as sub-regions even when their geometry is contained within a child region.
// This covers the bug where NYC (city) contained Kings County (parent) geometrically,
// causing Kings County to appear as a sub-region of NYC.
func TestIntegration_RegionAPI_SubRegionsExcludeAncestors(t *testing.T) {
	suite := SetupIntegrationTest(t)
	ctx := context.Background()

	// Create a superuser
	userID, _ := suite.registerUser("ancestor_test_user", "ancestor_test@test.com", "TestPassword123!")
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET is_superuser = TRUE, verification_tier = 2, postcard_verified = TRUE, vouch_verified = TRUE WHERE id = ?", userID)

	// Get a fresh token with superuser privileges
	resp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "ancestor_test@test.com",
		"password": "TestPassword123!",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token := loginResp.Token

	// Create a hierarchy similar to NYC: State -> County -> City
	// BUT the city polygon is LARGER than the county polygon (simulating NYC spanning multiple counties)

	// State (largest)
	stateGeoJSON := `{"type":"Polygon","coordinates":[[[-75.0,40.0],[-73.0,40.0],[-73.0,42.0],[-75.0,42.0],[-75.0,40.0]]]}`
	stateRegion := &models.GeographicRegion{
		Name:       "Ancestor Test State",
		RegionType: models.RegionTypeState,
		CreatedBy:  &userID,
	}
	if err := suite.regionRepo.Create(ctx, stateRegion, stateGeoJSON); err != nil {
		t.Fatalf("Failed to create state: %v", err)
	}

	// County (SMALLER than city - simulating Kings County)
	countyGeoJSON := `{"type":"Polygon","coordinates":[[[-74.1,40.6],[-73.9,40.6],[-73.9,40.75],[-74.1,40.75],[-74.1,40.6]]]}`
	countyRegion := &models.GeographicRegion{
		Name:           "Ancestor Test County",
		RegionType:     models.RegionTypeCounty,
		ParentRegionID: &stateRegion.ID,
		CreatedBy:      &userID,
	}
	if err := suite.regionRepo.Create(ctx, countyRegion, countyGeoJSON); err != nil {
		t.Fatalf("Failed to create county: %v", err)
	}

	// City (LARGER than county - simulating NYC spanning multiple counties)
	// City polygon contains the county polygon geographically
	cityGeoJSON := `{"type":"Polygon","coordinates":[[[-74.3,40.5],[-73.7,40.5],[-73.7,40.9],[-74.3,40.9],[-74.3,40.5]]]}`
	cityRegion := &models.GeographicRegion{
		Name:           "Ancestor Test City",
		RegionType:     models.RegionTypeCity,
		ParentRegionID: &countyRegion.ID, // Parent is county by hierarchy
		CreatedBy:      &userID,
	}
	if err := suite.regionRepo.Create(ctx, cityRegion, cityGeoJSON); err != nil {
		t.Fatalf("Failed to create city: %v", err)
	}

	// Locality (child of city)
	localityGeoJSON := `{"type":"Polygon","coordinates":[[[-74.05,40.65],[-73.95,40.65],[-73.95,40.72],[-74.05,40.72],[-74.05,40.65]]]}`
	localityRegion := &models.GeographicRegion{
		Name:           "Ancestor Test Locality",
		RegionType:     models.RegionTypeLocality,
		ParentRegionID: &cityRegion.ID,
		CreatedBy:      &userID,
	}
	if err := suite.regionRepo.Create(ctx, localityRegion, localityGeoJSON); err != nil {
		t.Fatalf("Failed to create locality: %v", err)
	}

	// Add user to all regions
	_ = suite.regionRepo.AddUserToRegion(ctx, userID, stateRegion.ID, true)
	_ = suite.regionRepo.AddUserToRegion(ctx, userID, countyRegion.ID, true)
	_ = suite.regionRepo.AddUserToRegion(ctx, userID, cityRegion.ID, true)
	_ = suite.regionRepo.AddUserToRegion(ctx, userID, localityRegion.ID, true)

	defer suite.cleanupRegion(localityRegion.ID)
	defer suite.cleanupRegion(cityRegion.ID)
	defer suite.cleanupRegion(countyRegion.ID)
	defer suite.cleanupRegion(stateRegion.ID)
	defer suite.cleanup(userID)

	t.Run("city sub-regions do NOT include parent county even if geometrically contained", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/communities/"+cityRegion.ID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var regionResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&regionResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Check sub_regions
		subRegions, ok := regionResp["sub_regions"].([]interface{})
		if !ok {
			subRegions = []interface{}{}
		}

		// Locality should be in sub-regions (it's a child)
		foundLocality := false
		foundCounty := false
		foundState := false

		for _, sr := range subRegions {
			subRegion := sr.(map[string]interface{})
			id := subRegion["id"].(string)
			if id == localityRegion.ID {
				foundLocality = true
			}
			if id == countyRegion.ID {
				foundCounty = true
			}
			if id == stateRegion.ID {
				foundState = true
			}
		}

		if !foundLocality {
			t.Error("Expected locality to appear as sub-region of city")
		}
		if foundCounty {
			t.Error("County should NOT appear as sub-region of city (county is an ancestor)")
		}
		if foundState {
			t.Error("State should NOT appear as sub-region of city (state is an ancestor)")
		}
	})

	t.Run("county sub-regions include city as child by parent_id", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/communities/"+countyRegion.ID, nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var regionResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&regionResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		subRegions, ok := regionResp["sub_regions"].([]interface{})
		if !ok {
			t.Fatal("Expected sub_regions in response")
		}

		// City should be in sub-regions (city has county as parent)
		foundCity := false
		for _, sr := range subRegions {
			subRegion := sr.(map[string]interface{})
			if subRegion["id"].(string) == cityRegion.ID {
				foundCity = true
				break
			}
		}

		if !foundCity {
			t.Error("Expected city to appear as sub-region of county (city has county as parent)")
		}
	})
}

// TestIntegration_VouchCrossRegionRejection verifies that a voucher must be a
// member of the specific target region, not just any region shared with the vouchee.
// (Security audit #14)
