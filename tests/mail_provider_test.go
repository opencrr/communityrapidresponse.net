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

// MailProviderTestSuite holds dependencies for mail provider tests
type MailProviderTestSuite struct {
	t            *testing.T
	db           *database.DB
	server       *httptest.Server
	userRepo     *database.UserRepository
	regionRepo   *database.RegionRepository
	verifyRepo   *database.VerificationRepository
	jwtAuth      *middleware.JWTAuth
	mockMail     *mocks.MockPostgridService
	mockMapbox   *mocks.MockMapboxService
	mailProvider config.MailProvider
}

// SetupMailProviderTest creates a new test suite for mail provider tests
func SetupMailProviderTest(t *testing.T, mailProvider config.MailProvider) *MailProviderTestSuite {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping mail provider tests")
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
	communityGroupRepo := database.NewGroupRepository(db, nil)
	districtRepo := database.NewSchoolDistrictRepository(db)
	auditRepo := database.NewAuditRepository(db)

	jwtConfig := &config.JWTConfig{
		Secret:          "test_secret_key_at_least_32_characters_long",
		ExpirationHours: 24,
		Issuer:          "test_issuer",
	}
	jwtAuth := middleware.NewJWTAuth(jwtConfig)

	// Create mock services - works for both Lob and Postgrid since they implement same interface
	mockMail := mocks.NewMockPostgridService()
	mockMapbox := mocks.NewMockMapboxService()

	emailCfg := &config.EmailConfig{Backend: config.EmailBackendMock, Enabled: false}
	emailService, _ := services.NewEmailService(emailCfg)

	authHandler := handlers.NewAuthHandlerWithEmailService(
		nil, userRepo, jwtAuth, emailService, jwtConfig.Secret, false, false, nil, nil, nil, "http://localhost:3000", nil,
	)
	regionHandler := handlers.NewRegionHandler(regionRepo, userRepo, mockMapbox, nil)
	verificationHandler := handlers.NewVerificationHandler(
		nil, verifyRepo, userRepo, regionRepo, mockMail, mockMapbox, nil,
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
		authHandler, mfaHandler, regionHandler,
		verificationHandler, adminHandler, schoolHandler, nil, nil, nil, jwtAuth, rateLimiter, nil, nil,
		[]string{"*"}, nil,
	)

	server := httptest.NewServer(router.Setup())

	suite := &MailProviderTestSuite{
		t:            t,
		db:           db,
		server:       server,
		userRepo:     userRepo,
		regionRepo:   regionRepo,
		verifyRepo:   verifyRepo,
		jwtAuth:      jwtAuth,
		mockMail:     mockMail,
		mockMapbox:   mockMapbox,
		mailProvider: mailProvider,
	}

	t.Cleanup(func() {
		server.Close()
		_ = db.Close()
	})

	return suite
}

func (s *MailProviderTestSuite) request(method, path string, body interface{}, token string) *http.Response {
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

func (s *MailProviderTestSuite) registerUser(username, email, password string) (string, string) {
	resp := s.request("POST", "/api/v1/auth/register", map[string]string{
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

	_, _ = s.db.ExecContext(context.Background(), "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", userID)

	resp2 := s.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, "")
	defer func() { _ = resp2.Body.Close() }()

	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp2.Body).Decode(&loginResp)

	return userID, loginResp.Token
}

func (s *MailProviderTestSuite) cleanup(userIDs ...string) {
	ctx := context.Background()
	for _, id := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM verification_requests WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM vouches WHERE voucher_user_id = ? OR vouched_user_id = ?", id, id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	}
}

func (s *MailProviderTestSuite) cleanupRegion(regionID string) {
	ctx := context.Background()
	_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE region_id = ?", regionID)
	_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", regionID)
	_, _ = s.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", regionID)
}

// =============================================================================
// Mail Provider Factory Tests
// =============================================================================

func TestMailProvider_FactoryReturnsCorrectType(t *testing.T) {
	t.Run("lob_provider_returns_lob_service", func(t *testing.T) {
		cfg := &config.Config{
			MailProvider: config.MailProviderLob,
			Lob: config.LobConfig{
				APIKey: "test_lob_key",
			},
		}

		service, err := services.NewMailService(cfg)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, ok := service.(*services.LobService)
		if !ok {
			t.Error("Expected LobService when MAIL_PROVIDER=lob")
		}
	})

	t.Run("postgrid_provider_returns_postgrid_service", func(t *testing.T) {
		cfg := &config.Config{
			MailProvider: config.MailProviderPostgrid,
			Postgrid: config.PostgridConfig{
				AddressVerificationAPIKey: "test_key",
			},
		}

		service, err := services.NewMailService(cfg)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, ok := service.(*services.PostgridService)
		if !ok {
			t.Error("Expected PostgridService when MAIL_PROVIDER=postgrid")
		}
	})

	t.Run("empty_provider_defaults_to_lob", func(t *testing.T) {
		cfg := &config.Config{
			MailProvider: "",
			Lob: config.LobConfig{
				APIKey: "",
			},
		}

		service, err := services.NewMailService(cfg)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, ok := service.(*services.LobService)
		if !ok {
			t.Error("Expected LobService when MAIL_PROVIDER is empty (default)")
		}
	})

	t.Run("invalid_provider_returns_error", func(t *testing.T) {
		cfg := &config.Config{
			MailProvider: "invalid",
		}

		_, err := services.NewMailService(cfg)
		if err == nil {
			t.Error("Expected error for invalid mail provider")
		}
	})
}

// =============================================================================
// Lob Service Integration Tests
// =============================================================================

func TestIntegration_Lob_VerificationFlow_Success(t *testing.T) {
	suite := SetupMailProviderTest(t, config.MailProviderLob)

	suite.mockMail.Reset()
	suite.mockMapbox.Reset()

	suite.mockMail.DefaultDeliverable = true
	suite.mockMail.DefaultIsPOBox = false
	suite.mockMail.DefaultIsCMRA = false

	suite.mockMapbox.DefaultBoundaryType = "city"
	suite.mockMapbox.DefaultBoundaryName = "Lob Test City"
	suite.mockMapbox.DefaultBoundaryState = "California"

	userID, _ := suite.registerUser("lobuser", "lob@test.com", "securepassword123")
	defer suite.cleanup(userID)

	ctx := context.Background()

	// Make user vouch-verified (required before postcard request)
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET vouch_verified = TRUE, verification_tier = 1 WHERE id = ?", userID)

	// Re-login to get fresh token with updated claims
	loginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email": "lob@test.com", "password": "securepassword123",
	}, "")
	var loginBody models.LoginResponse
	_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)
	_ = loginResp.Body.Close()
	token := loginBody.Token

	region := &models.GeographicRegion{
		Name:       "Lob Test City",
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = suite.regionRepo.Create(ctx, region, geoJSON)
	defer suite.cleanupRegion(region.ID)

	t.Run("address_validation_called", func(t *testing.T) {
		suite.mockMail.Reset()

		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 Lob Street",
				"city":        "Lob Test City",
				"state":       "CA",
				"postal_code": "94102",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if len(suite.mockMail.ValidateAddressCalls) == 0 {
			t.Error("Expected ValidateAddress to be called")
		}

		if len(suite.mockMail.ValidateAddressCalls) > 0 {
			call := suite.mockMail.ValidateAddressCalls[0]
			if call.Address.Line1 != "123 Lob Street" {
				t.Errorf("Expected address '123 Lob Street', got '%s'", call.Address.Line1)
			}
		}
	})

	t.Run("postcard_sent_on_success", func(t *testing.T) {
		suite.mockMail.Reset()

		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "456 Lob Avenue",
				"city":        "Lob Test City",
				"state":       "CA",
				"postal_code": "94103",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if len(suite.mockMail.SendPostcardCalls) == 0 {
			t.Error("Expected SendPostcard to be called")
		}

		if len(suite.mockMail.SendPostcardCalls) > 0 {
			call := suite.mockMail.SendPostcardCalls[0]
			if call.VerificationCode == "" {
				t.Error("Expected verification code in postcard call")
			}
		}
	})
}

func TestIntegration_Lob_POBoxRejection(t *testing.T) {
	suite := SetupMailProviderTest(t, config.MailProviderLob)

	suite.mockMail.Reset()
	suite.mockMail.DefaultDeliverable = true
	suite.mockMail.DefaultIsPOBox = true // Lob detects PO Box via zip_code_type

	userID, _ := suite.registerUser("lobpobox", "lobpobox@test.com", "securepassword123")
	defer suite.cleanup(userID)

	ctx := context.Background()

	// Make user vouch-verified (required before postcard request)
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET vouch_verified = TRUE, verification_tier = 1 WHERE id = ?", userID)

	// Re-login to get fresh token with updated claims
	loginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email": "lobpobox@test.com", "password": "securepassword123",
	}, "")
	var loginBody models.LoginResponse
	_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)
	_ = loginResp.Body.Close()
	token := loginBody.Token

	region := &models.GeographicRegion{
		Name:       "Lob PO Box City",
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = suite.regionRepo.Create(ctx, region, geoJSON)
	defer suite.cleanupRegion(region.ID)

	t.Run("rejects_po_box_address", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "PO Box 123",
				"city":        "Lob PO Box City",
				"state":       "CA",
				"postal_code": "94102",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for PO Box, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body["error"] != "po_box_not_allowed" {
			t.Errorf("Expected error 'po_box_not_allowed', got '%v'", body["error"])
		}
	})
}

func TestIntegration_Lob_CMRARejection(t *testing.T) {
	suite := SetupMailProviderTest(t, config.MailProviderLob)

	suite.mockMail.Reset()
	suite.mockMail.DefaultDeliverable = true
	suite.mockMail.DefaultIsCMRA = true // Lob detects CMRA via dpv_cmra field

	userID, _ := suite.registerUser("lobcmra", "lobcmra@test.com", "securepassword123")
	defer suite.cleanup(userID)

	ctx := context.Background()

	// Make user vouch-verified (required before postcard request)
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET vouch_verified = TRUE, verification_tier = 1 WHERE id = ?", userID)

	// Re-login to get fresh token with updated claims
	loginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email": "lobcmra@test.com", "password": "securepassword123",
	}, "")
	var loginBody models.LoginResponse
	_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)
	_ = loginResp.Body.Close()
	token := loginBody.Token

	region := &models.GeographicRegion{
		Name:       "Lob CMRA City",
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = suite.regionRepo.Create(ctx, region, geoJSON)
	defer suite.cleanupRegion(region.ID)

	t.Run("rejects_cmra_address", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 Main St #PMB456",
				"city":        "Lob CMRA City",
				"state":       "CA",
				"postal_code": "94102",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for CMRA, got %d", resp.StatusCode)
		}
	})
}

func TestIntegration_Lob_CommercialRejection(t *testing.T) {
	suite := SetupMailProviderTest(t, config.MailProviderLob)

	suite.mockMail.Reset()
	suite.mockMail.DefaultDeliverable = true
	suite.mockMail.DefaultIsCommercial = true // Lob detects commercial via dpv_residential field

	userID, _ := suite.registerUser("lobcommercial", "lobcommercial@test.com", "securepassword123")
	defer suite.cleanup(userID)

	ctx := context.Background()

	// Make user vouch-verified (required before postcard request)
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET vouch_verified = TRUE, verification_tier = 1 WHERE id = ?", userID)

	// Re-login to get fresh token with updated claims
	loginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email": "lobcommercial@test.com", "password": "securepassword123",
	}, "")
	var loginBody models.LoginResponse
	_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)
	_ = loginResp.Body.Close()
	token := loginBody.Token

	region := &models.GeographicRegion{
		Name:       "Lob Commercial City",
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = suite.regionRepo.Create(ctx, region, geoJSON)
	defer suite.cleanupRegion(region.ID)

	t.Run("rejects_commercial_address", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "789 Corporate Blvd",
				"city":        "Lob Commercial City",
				"state":       "CA",
				"postal_code": "94102",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for commercial address, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body["error"] != "commercial_not_allowed" {
			t.Errorf("Expected error 'commercial_not_allowed', got %v", body["error"])
		}
	})
}

func TestIntegration_Lob_ServiceFailure(t *testing.T) {
	suite := SetupMailProviderTest(t, config.MailProviderLob)

	suite.mockMail.Reset()
	suite.mockMail.ShouldFail = true
	suite.mockMail.FailError = fmt.Errorf("lob service unavailable")

	userID, _ := suite.registerUser("lobfail", "lobfail@test.com", "securepassword123")
	defer suite.cleanup(userID)

	ctx := context.Background()

	// Make user vouch-verified (required before postcard request)
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET vouch_verified = TRUE, verification_tier = 1 WHERE id = ?", userID)

	// Re-login to get fresh token with updated claims
	loginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email": "lobfail@test.com", "password": "securepassword123",
	}, "")
	var loginBody models.LoginResponse
	_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)
	_ = loginResp.Body.Close()
	token := loginBody.Token

	region := &models.GeographicRegion{
		Name:       "Lob Fail City",
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = suite.regionRepo.Create(ctx, region, geoJSON)
	defer suite.cleanupRegion(region.ID)

	t.Run("handles_lob_service_failure", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 Fail Street",
				"city":        "Lob Fail City",
				"state":       "CA",
				"postal_code": "94102",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadGateway {
			t.Errorf("Expected 502 for service failure, got %d", resp.StatusCode)
		}
	})
}

// =============================================================================
// Lob Service Unit Tests - API Response Parsing
// =============================================================================

func TestLobService_DeliverabilityStatus(t *testing.T) {
	tests := []struct {
		name                string
		deliverability      string
		expectedDeliverable bool
	}{
		{"deliverable", "deliverable", true},
		{"deliverable_missing_unit", "deliverable_missing_unit", true},
		{"deliverable_unnecessary_unit", "deliverable_unnecessary_unit", true},
		{"deliverable_incorrect_unit", "deliverable_incorrect_unit", true},
		{"undeliverable", "undeliverable", false},
		{"no_match", "no_match", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"id":             "us_ver_test",
					"deliverability": tt.deliverability,
					"deliverability_analysis": map[string]interface{}{
						"dpv_cmra":         "N",
						"dpv_confirmation": "Y",
					},
					"components": map[string]interface{}{
						"city":          "TEST CITY",
						"state":         "CA",
						"zip_code":      "94102",
						"zip_code_type": "standard",
					},
					"primary_line": "123 TEST ST",
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			service := services.NewLobService(&config.LobConfig{
				APIKey:  "test_key",
				BaseURL: server.URL,
			})

			result, err := service.ValidateAddress(context.Background(), &models.Address{
				Line1:      "123 Test St",
				City:       "Test City",
				State:      "CA",
				PostalCode: "94102",
			})
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.IsDeliverable != tt.expectedDeliverable {
				t.Errorf("Expected IsDeliverable=%v for %s, got %v",
					tt.expectedDeliverable, tt.deliverability, result.IsDeliverable)
			}
		})
	}
}

func TestLobService_ZipCodeType_POBoxDetection(t *testing.T) {
	tests := []struct {
		name          string
		zipCodeType   string
		expectedPOBox bool
	}{
		{"standard_zip", "standard", false},
		{"po_box_zip", "po_box", true},
		{"unique_zip", "unique", false},
		{"military_zip", "military", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"id":             "us_ver_test",
					"deliverability": "deliverable",
					"deliverability_analysis": map[string]interface{}{
						"dpv_cmra":         "N",
						"dpv_confirmation": "Y",
					},
					"components": map[string]interface{}{
						"city":          "TEST CITY",
						"state":         "CA",
						"zip_code":      "94102",
						"zip_code_type": tt.zipCodeType,
					},
					"primary_line": "123 TEST ST",
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			service := services.NewLobService(&config.LobConfig{
				APIKey:  "test_key",
				BaseURL: server.URL,
			})

			result, err := service.ValidateAddress(context.Background(), &models.Address{
				Line1:      "123 Test St",
				City:       "Test City",
				State:      "CA",
				PostalCode: "94102",
			})
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.IsPOBox != tt.expectedPOBox {
				t.Errorf("Expected IsPOBox=%v for zip_code_type=%s, got %v",
					tt.expectedPOBox, tt.zipCodeType, result.IsPOBox)
			}
		})
	}
}

func TestLobService_CMRA_Detection(t *testing.T) {
	tests := []struct {
		name         string
		dpvCMRA      string
		expectedCMRA bool
	}{
		{"not_cmra", "N", false},
		{"is_cmra", "Y", true},
		{"empty_cmra", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"id":             "us_ver_test",
					"deliverability": "deliverable",
					"deliverability_analysis": map[string]interface{}{
						"dpv_cmra":         tt.dpvCMRA,
						"dpv_confirmation": "Y",
					},
					"components": map[string]interface{}{
						"city":          "TEST CITY",
						"state":         "CA",
						"zip_code":      "94102",
						"zip_code_type": "standard",
					},
					"primary_line": "123 TEST ST",
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			service := services.NewLobService(&config.LobConfig{
				APIKey:  "test_key",
				BaseURL: server.URL,
			})

			result, err := service.ValidateAddress(context.Background(), &models.Address{
				Line1:      "123 Test St",
				City:       "Test City",
				State:      "CA",
				PostalCode: "94102",
			})
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.IsCMRA != tt.expectedCMRA {
				t.Errorf("Expected IsCMRA=%v for dpv_cmra=%s, got %v",
					tt.expectedCMRA, tt.dpvCMRA, result.IsCMRA)
			}
		})
	}
}

func TestLobService_PostcardRequest_Format(t *testing.T) {
	var receivedRequest map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedRequest)

		response := map[string]interface{}{
			"id": "psc_test123",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := services.NewLobService(&config.LobConfig{
		APIKey:        "test_key",
		BaseURL:       server.URL,
		ReturnName:    "Test Org",
		ReturnLine1:   "123 Return St",
		ReturnCity:    "Return City",
		ReturnState:   "CA",
		ReturnZip:     "94000",
		ReturnCountry: "US",
	})

	_, err := service.SendPostcard(context.Background(), &models.Address{
		Line1:      "456 Recipient Ave",
		City:       "Recipient City",
		State:      "CA",
		PostalCode: "94102",
	}, "VERIFY123", "REF01")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify request format matches Lob API expectations
	to, ok := receivedRequest["to"].(map[string]interface{})
	if !ok {
		t.Fatal("Missing 'to' field in request")
	}
	if to["address_line1"] != "456 Recipient Ave" {
		t.Errorf("Expected address_line1='456 Recipient Ave', got '%v'", to["address_line1"])
	}
	if to["address_city"] != "Recipient City" {
		t.Errorf("Expected address_city='Recipient City', got '%v'", to["address_city"])
	}

	from, ok := receivedRequest["from"].(map[string]interface{})
	if !ok {
		t.Fatal("Missing 'from' field in request")
	}
	if from["name"] != "Test Org" {
		t.Errorf("Expected from.name='Test Org', got '%v'", from["name"])
	}

	if receivedRequest["size"] != "4x6" {
		t.Errorf("Expected size='4x6', got '%v'", receivedRequest["size"])
	}

	if receivedRequest["front"] == nil {
		t.Error("Missing 'front' field in request")
	}
	if receivedRequest["back"] == nil {
		t.Error("Missing 'back' field in request")
	}
}

func TestLobService_BasicAuth_Header(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")

		response := map[string]interface{}{
			"id":             "us_ver_test",
			"deliverability": "deliverable",
			"deliverability_analysis": map[string]interface{}{
				"dpv_cmra": "N",
			},
			"components": map[string]interface{}{
				"zip_code_type": "standard",
			},
			"primary_line": "123 TEST ST",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := services.NewLobService(&config.LobConfig{
		APIKey:  "test_lob_api_key_12345",
		BaseURL: server.URL,
	})

	_, err := service.ValidateAddress(context.Background(), &models.Address{
		Line1: "123 Test St",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify Basic Auth header is present and properly formatted
	if receivedAuth == "" {
		t.Error("Missing Authorization header")
	}
	if len(receivedAuth) < 7 || receivedAuth[:6] != "Basic " {
		t.Errorf("Expected 'Basic ' prefix in Authorization header, got '%s'", receivedAuth)
	}
}

// =============================================================================
// E2E Tests - Full Verification Flow
// =============================================================================

func TestE2E_Lob_CompleteVerificationFlow(t *testing.T) {
	suite := SetupMailProviderTest(t, config.MailProviderLob)

	suite.mockMail.Reset()
	suite.mockMapbox.Reset()

	suite.mockMail.DefaultDeliverable = true
	suite.mockMapbox.DefaultBoundaryType = "city"
	suite.mockMapbox.DefaultBoundaryName = "E2E Test City"
	suite.mockMapbox.DefaultBoundaryState = "California"

	userID, _ := suite.registerUser("e2elobuser", "e2elob@test.com", "securepassword123")
	defer suite.cleanup(userID)

	ctx := context.Background()

	// Make user vouch-verified (required before postcard request in vouch-first flow)
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET vouch_verified = TRUE, verification_tier = 1 WHERE id = ?", userID)

	// Re-login to get fresh token with updated claims
	loginResp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "e2elob@test.com",
		"password": "securepassword123",
	}, "")
	var loginBody models.LoginResponse
	_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)
	_ = loginResp.Body.Close()
	token := loginBody.Token

	region := &models.GeographicRegion{
		Name:       "E2E Test City",
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = suite.regionRepo.Create(ctx, region, geoJSON)
	defer suite.cleanupRegion(region.ID)

	t.Run("complete_postcard_verification_flow", func(t *testing.T) {
		// Step 1: Request postcard verification
		resp := suite.request("POST", "/api/v1/verification/postcard/request", map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 E2E Street",
				"city":        "E2E Test City",
				"state":       "CA",
				"postal_code": "94102",
			},
		}, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Fatalf("Expected 201/200, got %d: %v", resp.StatusCode, body)
		}

		// Verify address was validated
		if len(suite.mockMail.ValidateAddressCalls) == 0 {
			t.Error("Address validation not called")
		}

		// Verify postcard was sent
		if len(suite.mockMail.SendPostcardCalls) == 0 {
			t.Error("Postcard not sent")
		}

		// Step 2: Get the verification code from the mock
		var verificationCode string
		if len(suite.mockMail.SendPostcardCalls) > 0 {
			verificationCode = suite.mockMail.SendPostcardCalls[0].VerificationCode
		}

		if verificationCode == "" {
			t.Fatal("No verification code captured")
		}

		// Step 3: Verify the code
		verifyResp := suite.request("POST", "/api/v1/verification/postcard/verify", map[string]string{
			"verification_code": verificationCode,
		}, token)
		defer func() { _ = verifyResp.Body.Close() }()

		if verifyResp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(verifyResp.Body).Decode(&body)
			t.Errorf("Verification failed with %d: %v", verifyResp.StatusCode, body)
		}

		// Step 4: Check verification status
		statusResp := suite.request("GET", "/api/v1/verification/status", nil, token)
		defer func() { _ = statusResp.Body.Close() }()

		if statusResp.StatusCode != http.StatusOK {
			t.Errorf("Status check failed with %d", statusResp.StatusCode)
		}

		var statusBody map[string]interface{}
		_ = json.NewDecoder(statusResp.Body).Decode(&statusBody)

		if statusBody["postcard_verified"] != true {
			t.Errorf("Expected postcard_verified=true, got %v", statusBody["postcard_verified"])
		}
	})
}
