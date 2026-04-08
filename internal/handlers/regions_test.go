package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/mocks"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

type RegionTestSuite struct {
	t          *testing.T
	db         *database.DB
	regionRepo *database.RegionRepository
	userRepo   *database.UserRepository
	handler    *RegionHandler
	jwtAuth    *middleware.JWTAuth
}

func setupRegionTestSuite(t *testing.T) *RegionTestSuite {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping handler tests")
	}

	port := 3306
	if portStr := os.Getenv("TEST_DB_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	cfg := &config.DatabaseConfig{
		Host:     host,
		Port:     port,
		User:     os.Getenv("TEST_DB_USER"),
		Password: os.Getenv("TEST_DB_PASSWORD"),
		Name:     os.Getenv("TEST_DB_NAME"),
		Charset:  "utf8mb4",
	}

	if cfg.User == "" {
		cfg.User = "test"
	}
	if cfg.Password == "" {
		cfg.Password = "testpassword"
	}
	if cfg.Name == "" {
		cfg.Name = "communityrapidresponse_test"
	}

	db, err := database.New(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	regionRepo := database.NewRegionRepository(db)
	userRepo := database.NewUserRepository(db)
	mockMapbox := mocks.NewMockMapboxService()
	handler := NewRegionHandler(regionRepo, mockMapbox, nil)

	jwtConfig := &config.JWTConfig{
		Secret:          "test_secret_key_at_least_32_characters_long",
		ExpirationHours: 24,
		Issuer:          "test_issuer",
	}
	jwtAuth := middleware.NewJWTAuth(jwtConfig)

	suite := &RegionTestSuite{
		t:          t,
		db:         db,
		regionRepo: regionRepo,
		userRepo:   userRepo,
		handler:    handler,
		jwtAuth:    jwtAuth,
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return suite
}

func (s *RegionTestSuite) createTestUser(username string, tier models.VerificationTier, isSuperuser bool) (*models.User, string) {
	ctx := context.Background()
	user := &models.User{
		Username:         username,
		Email:            username + "@test.com",
		PasswordHash:     "hashedpassword",
		VerificationTier: tier,
		IsSuperuser:      isSuperuser,
	}
	err := s.userRepo.Create(ctx, user)
	if err != nil {
		s.t.Fatalf("Failed to create test user: %v", err)
	}

	token, err := s.jwtAuth.GenerateToken(user)
	if err != nil {
		s.t.Fatalf("Failed to generate token: %v", err)
	}

	return user, token
}

func (s *RegionTestSuite) createTestRegion(name string, regionType models.RegionType, parentID *string) *models.GeographicRegion {
	ctx := context.Background()
	region := &models.GeographicRegion{
		Name:           name,
		RegionType:     regionType,
		ParentRegionID: parentID,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	err := s.regionRepo.Create(ctx, region, geoJSON)
	if err != nil {
		s.t.Fatalf("Failed to create test region: %v", err)
	}
	return region
}

func (s *RegionTestSuite) cleanup(userIDs []string, regionIDs []string) {
	ctx := context.Background()
	for _, id := range regionIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE region_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", id)
	}
	for _, id := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	}
}

// Tests for Region Update

func TestRegionHandler_Update_Success(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create admin user
	user, _ := suite.createTestUser("updateadmin", models.TierPostcard, false)

	// Create region
	region := suite.createTestRegion("Original Name", models.RegionTypeCity, nil)

	// Make user admin of region
	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, true)

	defer suite.cleanup([]string{user.ID}, []string{region.ID})

	t.Run("admin can update region name", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/regions/"+region.ID, bytes.NewReader([]byte(`{"name":"Updated Name"}`)))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			Username:         user.Username,
			VerificationTier: user.VerificationTier,
			IsSuperuser:      false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		q := req.URL.Query()
		q.Set("id", region.ID)
		req.URL.RawQuery = q.Encode()

		rec := httptest.NewRecorder()
		suite.handler.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["name"] != "Updated Name" {
			t.Errorf("Expected name 'Updated Name', got '%s'", body["name"])
		}
	})
}

func TestRegionHandler_Update_NotAdmin(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create non-admin user
	user, _ := suite.createTestUser("notadmin", models.TierPostcard, false)

	// Create region (user is NOT admin)
	region := suite.createTestRegion("Test Region", models.RegionTypeCity, nil)

	defer suite.cleanup([]string{user.ID}, []string{region.ID})

	t.Run("non-admin cannot update region", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/regions/"+region.ID, bytes.NewReader([]byte(`{"name":"Hacked Name"}`)))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			Username:         user.Username,
			VerificationTier: user.VerificationTier,
			IsSuperuser:      false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		q := req.URL.Query()
		q.Set("id", region.ID)
		req.URL.RawQuery = q.Encode()

		rec := httptest.NewRecorder()
		suite.handler.Update(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})
}

func TestRegionHandler_Update_SuperuserBypass(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create superuser
	user, _ := suite.createTestUser("superuserupdate", models.TierPostcard, true)

	// Create region (superuser is NOT admin but can still update)
	region := suite.createTestRegion("Super Region", models.RegionTypeCity, nil)

	defer suite.cleanup([]string{user.ID}, []string{region.ID})

	t.Run("superuser can update any region", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/regions/"+region.ID, bytes.NewReader([]byte(`{"name":"Superuser Updated"}`)))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			Username:         user.Username,
			VerificationTier: user.VerificationTier,
			IsSuperuser:      true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		q := req.URL.Query()
		q.Set("id", region.ID)
		req.URL.RawQuery = q.Encode()

		rec := httptest.NewRecorder()
		suite.handler.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// Tests for Region Delete

func TestRegionHandler_Delete_SuperuserOnly(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create superuser
	superuser, _ := suite.createTestUser("superuserdelete", models.TierPostcard, true)

	// Create regular admin
	admin, _ := suite.createTestUser("regularadmin", models.TierPostcard, false)

	// Create region
	region := suite.createTestRegion("Delete Me", models.RegionTypeCity, nil)

	// Make admin an admin of region
	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, true)

	defer suite.cleanup([]string{superuser.ID, admin.ID}, []string{})

	t.Run("regular admin cannot delete region", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/regions/"+region.ID, nil)

		claims := &middleware.Claims{
			UserID:           admin.ID,
			Email:            admin.Email,
			Username:         admin.Username,
			VerificationTier: admin.VerificationTier,
			IsSuperuser:      false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		q := req.URL.Query()
		q.Set("id", region.ID)
		req.URL.RawQuery = q.Encode()

		rec := httptest.NewRecorder()
		suite.handler.Delete(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})

	t.Run("superuser can delete region", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/regions/"+region.ID, nil)

		claims := &middleware.Claims{
			UserID:           superuser.ID,
			Email:            superuser.Email,
			Username:         superuser.Username,
			VerificationTier: superuser.VerificationTier,
			IsSuperuser:      true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		q := req.URL.Query()
		q.Set("id", region.ID)
		req.URL.RawQuery = q.Encode()

		rec := httptest.NewRecorder()
		suite.handler.Delete(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// Tests for Region Type/Parent Hierarchy

func TestRegionHandler_Create_TypeHierarchy(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create superuser to bypass admin boundary validation (this test is about type hierarchy)
	user, _ := suite.createTestUser("hierarchyuser", models.TierPostcard, true)

	// Create a city region for parent testing
	city := suite.createTestRegion("Test City", models.RegionTypeCity, nil)

	// Create a neighborhood region
	neighborhood := suite.createTestRegion("Test Neighborhood", models.RegionTypeNeighborhood, &city.ID)

	defer suite.cleanup([]string{user.ID}, []string{neighborhood.ID, city.ID})

	testCases := []struct {
		name        string
		regionType  models.RegionType
		parentID    *string
		shouldFail  bool
		errorMsg    string
	}{
		{
			name:       "city with city parent should fail",
			regionType: models.RegionTypeCity,
			parentID:   &city.ID,
			shouldFail: true,
			errorMsg:   "city regions must have a county parent",
		},
		{
			name:       "neighborhood without parent should fail",
			regionType: models.RegionTypeNeighborhood,
			parentID:   nil,
			shouldFail: true,
			errorMsg:   "neighborhood regions must have an existing city parent",
		},
		{
			name:       "neighborhood with neighborhood parent should fail",
			regionType: models.RegionTypeNeighborhood,
			parentID:   &neighborhood.ID,
			shouldFail: true,
			errorMsg:   "neighborhood regions must have a city parent",
		},
		{
			name:       "city_block without parent should fail",
			regionType: models.RegionTypeCityBlock,
			parentID:   nil,
			shouldFail: true,
			errorMsg:   "city block regions must have an existing neighborhood parent",
		},
		{
			name:       "city_block with city parent should fail",
			regionType: models.RegionTypeCityBlock,
			parentID:   &city.ID,
			shouldFail: true,
			errorMsg:   "city block regions must have a neighborhood parent",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reqBody := map[string]interface{}{
				"name": "Test Region",
				"type": tc.regionType,
				"geometry": map[string]interface{}{
					"type":        "Polygon",
					"coordinates": [][][]float64{{{-122.5, 37.7}, {-122.4, 37.7}, {-122.4, 37.8}, {-122.5, 37.8}, {-122.5, 37.7}}},
				},
			}
			if tc.parentID != nil {
				reqBody["parent_region_id"] = *tc.parentID
			}

			bodyBytes, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/regions", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			claims := &middleware.Claims{
				UserID:           user.ID,
				Email:            user.Email,
				Username:         user.Username,
				VerificationTier: user.VerificationTier,
				IsSuperuser:      true,
			}
			ctx := middleware.ContextWithUser(req.Context(), claims)
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()
			suite.handler.Create(rec, req)

			if tc.shouldFail {
				if rec.Code != http.StatusBadRequest {
					t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
				}
				var body map[string]interface{}
				_ = json.NewDecoder(rec.Body).Decode(&body)
				if msg, ok := body["message"].(string); ok {
					if msg != tc.errorMsg {
						t.Errorf("Expected error '%s', got '%s'", tc.errorMsg, msg)
					}
				}
			}
		})
	}
}

func TestRegionHandler_Create_ValidHierarchy(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create superuser to bypass admin boundary validation (this test is about type hierarchy)
	user, _ := suite.createTestUser("validhierarchy", models.TierPostcard, true)

	defer suite.cleanup([]string{user.ID}, []string{})

	t.Run("city without parent succeeds", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"name": "Valid City",
			"type": models.RegionTypeCity,
			"geometry": map[string]interface{}{
				"type":        "Polygon",
				"coordinates": [][][]float64{{{-122.5, 37.7}, {-122.4, 37.7}, {-122.4, 37.8}, {-122.5, 37.8}, {-122.5, 37.7}}},
			},
		}

		bodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/regions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			Username:         user.Username,
			VerificationTier: user.VerificationTier,
			IsSuperuser:      true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		// Cleanup created region
		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["region_id"] != "" {
			suite.cleanup([]string{}, []string{body["region_id"]})
		}
	})
}

// Tests for Region Geometry in Response

func TestRegionHandler_Get_IncludesGeometry(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create superuser (can see any region)
	superuser, _ := suite.createTestUser("getgeometrysuperuser", models.TierPostcard, true)

	// Create region
	region := suite.createTestRegion("Geometry Region", models.RegionTypeCity, nil)

	defer suite.cleanup([]string{superuser.ID}, []string{region.ID})

	t.Run("get region includes geometry", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/regions/"+region.ID, nil)

		claims := &middleware.Claims{
			UserID:           superuser.ID,
			Email:            superuser.Email,
			Username:         superuser.Username,
			VerificationTier: superuser.VerificationTier,
			IsSuperuser:      true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		q := req.URL.Query()
		q.Set("id", region.ID)
		req.URL.RawQuery = q.Encode()

		rec := httptest.NewRecorder()
		suite.handler.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if body["geometry"] == nil {
			t.Error("Expected geometry field in response")
		}
	})
}

func TestRegionHandler_List_IncludesGeometry(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create superuser (can see all regions)
	superuser, _ := suite.createTestUser("listgeometrysuperuser", models.TierPostcard, true)

	// Create region
	region := suite.createTestRegion("List Geometry Region", models.RegionTypeCity, nil)

	defer suite.cleanup([]string{superuser.ID}, []string{region.ID})

	t.Run("list regions includes geometry", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/regions", nil)

		claims := &middleware.Claims{
			UserID:           superuser.ID,
			Email:            superuser.Email,
			Username:         superuser.Username,
			VerificationTier: superuser.VerificationTier,
			IsSuperuser:      true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		regions, ok := body["regions"].([]interface{})
		if !ok || len(regions) == 0 {
			t.Skip("No regions found to test geometry")
		}

		firstRegion := regions[0].(map[string]interface{})
		if firstRegion["geometry"] == nil {
			t.Error("Expected geometry field in region response")
		}
	})
}

// Tests for Region Access Control

func TestRegionHandler_List_RequiresAuth(t *testing.T) {
	suite := setupRegionTestSuite(t)

	t.Run("unauthenticated request fails", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/regions", nil)

		rec := httptest.NewRecorder()
		suite.handler.List(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestRegionHandler_List_RegularUserSeesOwnRegions(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create regular user
	user, _ := suite.createTestUser("regularlistuser", models.TierPostcard, false)

	// Create two regions - user is member of only one
	region1 := suite.createTestRegion("User Region", models.RegionTypeCity, nil)
	region2 := suite.createTestRegion("Other Region", models.RegionTypeCity, nil)

	// Add user to region1 only
	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region1.ID, false)

	defer suite.cleanup([]string{user.ID}, []string{region1.ID, region2.ID})

	t.Run("regular user only sees their regions", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/regions", nil)

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			Username:         user.Username,
			VerificationTier: user.VerificationTier,
			IsSuperuser:      false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		regions, ok := body["regions"].([]interface{})
		if !ok {
			t.Fatal("Expected regions array in response")
		}

		// User should see their region and parent regions (state, county auto-created)
		// but not the other region they're not associated with
		foundOwnRegion := false
		foundOtherRegion := false
		for _, r := range regions {
			reg := r.(map[string]interface{})
			if reg["id"] == region1.ID {
				foundOwnRegion = true
			}
			if reg["id"] == region2.ID {
				foundOtherRegion = true
			}
		}

		if !foundOwnRegion {
			t.Error("Expected to find user's region in list")
		}
		if foundOtherRegion {
			t.Error("Should not see regions user is not associated with")
		}
	})
}

func TestRegionHandler_Get_RequiresAuth(t *testing.T) {
	suite := setupRegionTestSuite(t)

	region := suite.createTestRegion("Auth Test Region", models.RegionTypeCity, nil)
	defer suite.cleanup([]string{}, []string{region.ID})

	t.Run("unauthenticated request fails", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/regions/"+region.ID, nil)

		q := req.URL.Query()
		q.Set("id", region.ID)
		req.URL.RawQuery = q.Encode()

		rec := httptest.NewRecorder()
		suite.handler.Get(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestRegionHandler_Get_RegularUserAccessControl(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create regular user
	user, _ := suite.createTestUser("getaccessuser", models.TierPostcard, false)

	// Create two regions
	memberRegion := suite.createTestRegion("Member Region", models.RegionTypeCity, nil)
	otherRegion := suite.createTestRegion("Other Region", models.RegionTypeCity, nil)

	// Add user to memberRegion only
	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, memberRegion.ID, false)

	defer suite.cleanup([]string{user.ID}, []string{memberRegion.ID, otherRegion.ID})

	t.Run("can access region user is member of", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/regions/"+memberRegion.ID, nil)

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			Username:         user.Username,
			VerificationTier: user.VerificationTier,
			IsSuperuser:      false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		q := req.URL.Query()
		q.Set("id", memberRegion.ID)
		req.URL.RawQuery = q.Encode()

		rec := httptest.NewRecorder()
		suite.handler.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("cannot access region user is not member of", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/regions/"+otherRegion.ID, nil)

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			Username:         user.Username,
			VerificationTier: user.VerificationTier,
			IsSuperuser:      false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		q := req.URL.Query()
		q.Set("id", otherRegion.ID)
		req.URL.RawQuery = q.Encode()

		rec := httptest.NewRecorder()
		suite.handler.Get(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestRegionHandler_Get_SuperuserCanAccessAny(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create superuser
	superuser, _ := suite.createTestUser("getsuperuser", models.TierPostcard, true)

	// Create region that superuser is NOT a member of
	region := suite.createTestRegion("Any Region", models.RegionTypeCity, nil)

	defer suite.cleanup([]string{superuser.ID}, []string{region.ID})

	t.Run("superuser can access any region", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/regions/"+region.ID, nil)

		claims := &middleware.Claims{
			UserID:           superuser.ID,
			Email:            superuser.Email,
			Username:         superuser.Username,
			VerificationTier: superuser.VerificationTier,
			IsSuperuser:      true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		q := req.URL.Query()
		q.Set("id", region.ID)
		req.URL.RawQuery = q.Encode()

		rec := httptest.NewRecorder()
		suite.handler.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// =============================================================================
// IsUserAdmin Hierarchy Tests
// =============================================================================

func TestRegionHandler_AdminHierarchy(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create admin user
	adminUser, _ := suite.createTestUser("hierarchyadmin", models.TierPostcard, false)

	// Create region hierarchy: state -> county -> city
	stateRegion := suite.createTestRegion("Hierarchy State", models.RegionTypeState, nil)
	countyRegion := suite.createTestRegion("Hierarchy County", models.RegionTypeCounty, &stateRegion.ID)
	cityRegion := suite.createTestRegion("Hierarchy City", models.RegionTypeCity, &countyRegion.ID)

	// Make user admin of city only
	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, adminUser.ID, cityRegion.ID, true)

	defer suite.cleanup([]string{adminUser.ID}, []string{cityRegion.ID, countyRegion.ID, stateRegion.ID})

	t.Run("admin of child can update parent region", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/regions/"+countyRegion.ID, bytes.NewReader([]byte(`{"name":"Updated County Name"}`)))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			Username:         adminUser.Username,
			VerificationTier: adminUser.VerificationTier,
			IsSuperuser:      false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		q := req.URL.Query()
		q.Set("id", countyRegion.ID)
		req.URL.RawQuery = q.Encode()

		rec := httptest.NewRecorder()
		suite.handler.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("admin of child can update grandparent region", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/regions/"+stateRegion.ID, bytes.NewReader([]byte(`{"name":"Updated State Name"}`)))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			Username:         adminUser.Username,
			VerificationTier: adminUser.VerificationTier,
			IsSuperuser:      false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		q := req.URL.Query()
		q.Set("id", stateRegion.ID)
		req.URL.RawQuery = q.Encode()

		rec := httptest.NewRecorder()
		suite.handler.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestRegionHandler_Get_SubRegionsContainment(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create superuser
	superuser, _ := suite.createTestUser("subregionsuperuser", models.TierPostcard, true)

	ctx := context.Background()

	// Create a large parent region (state-sized, covering a wide area)
	parentRegion := &models.GeographicRegion{
		Name:       "Parent State",
		RegionType: models.RegionTypeState,
	}
	// Large polygon covering a big area
	parentGeoJSON := `{"type":"Polygon","coordinates":[[[-123.0,37.0],[-121.0,37.0],[-121.0,38.5],[-123.0,38.5],[-123.0,37.0]]]}`
	err := suite.regionRepo.Create(ctx, parentRegion, parentGeoJSON)
	if err != nil {
		t.Fatalf("Failed to create parent region: %v", err)
	}

	// Create a child region contained within the parent
	childRegion := &models.GeographicRegion{
		Name:       "Child City",
		RegionType: models.RegionTypeCity,
	}
	// Smaller polygon inside the parent
	childGeoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.5],[-122.3,37.5],[-122.3,37.7],[-122.5,37.7],[-122.5,37.5]]]}`
	err = suite.regionRepo.Create(ctx, childRegion, childGeoJSON)
	if err != nil {
		t.Fatalf("Failed to create child region: %v", err)
	}

	// Create a region outside the parent (should NOT be in sub-regions)
	outsideRegion := &models.GeographicRegion{
		Name:       "Outside Region",
		RegionType: models.RegionTypeCity,
	}
	// Polygon outside the parent
	outsideGeoJSON := `{"type":"Polygon","coordinates":[[[-120.0,35.0],[-119.0,35.0],[-119.0,36.0],[-120.0,36.0],[-120.0,35.0]]]}`
	err = suite.regionRepo.Create(ctx, outsideRegion, outsideGeoJSON)
	if err != nil {
		t.Fatalf("Failed to create outside region: %v", err)
	}

	defer suite.cleanup([]string{superuser.ID}, []string{childRegion.ID, outsideRegion.ID, parentRegion.ID})

	t.Run("sub-regions include geographically contained regions", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/regions/"+parentRegion.ID, nil)

		claims := &middleware.Claims{
			UserID:           superuser.ID,
			Email:            superuser.Email,
			Username:         superuser.Username,
			VerificationTier: superuser.VerificationTier,
			IsSuperuser:      true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		q := req.URL.Query()
		q.Set("id", parentRegion.ID)
		req.URL.RawQuery = q.Encode()

		rec := httptest.NewRecorder()
		suite.handler.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		subRegions, ok := body["sub_regions"].([]interface{})
		if !ok {
			// sub_regions might be nil/missing if empty
			if body["sub_regions"] == nil {
				subRegions = []interface{}{}
			} else {
				t.Fatalf("Expected sub_regions array in response, got: %v", body)
			}
		}

		// Should contain the child region
		foundChild := false
		foundOutside := false
		for _, sr := range subRegions {
			subRegion := sr.(map[string]interface{})
			if subRegion["id"] == childRegion.ID {
				foundChild = true
				// Verify geometry is included
				if subRegion["geometry"] == nil {
					t.Error("Expected geometry in sub-region")
				}
			}
			if subRegion["id"] == outsideRegion.ID {
				foundOutside = true
			}
		}

		if !foundChild {
			t.Error("Expected child region to be in sub-regions (geographically contained)")
		}
		if foundOutside {
			t.Error("Outside region should NOT be in sub-regions (not contained)")
		}
	})
}

// =============================================================================
// Admin Region Boundary Validation Tests
// =============================================================================

func (s *RegionTestSuite) createTestRegionWithGeometry(name string, regionType models.RegionType, parentID *string, geoJSON string) *models.GeographicRegion {
	ctx := context.Background()
	region := &models.GeographicRegion{
		Name:           name,
		RegionType:     regionType,
		ParentRegionID: parentID,
	}
	err := s.regionRepo.Create(ctx, region, geoJSON)
	if err != nil {
		s.t.Fatalf("Failed to create test region: %v", err)
	}
	return region
}

func TestRegionHandler_Create_AdminBoundaryValidation(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create admin user (needs both postcard and vouch verification)
	adminUser, _ := suite.createTestUser("boundaryadmin", models.TierPostcard, false)
	ctx := context.Background()
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET postcard_verified = TRUE, vouch_verified = TRUE WHERE id = ?", adminUser.ID)

	// Create a large admin region (state-sized)
	// Polygon covering SF Bay Area: roughly -123 to -121 longitude, 37 to 39 latitude
	adminRegionGeoJSON := `{"type":"Polygon","coordinates":[[[-123.0,37.0],[-121.0,37.0],[-121.0,39.0],[-123.0,39.0],[-123.0,37.0]]]}`
	adminRegion := suite.createTestRegionWithGeometry("Admin State", models.RegionTypeState, nil, adminRegionGeoJSON)

	// Make user admin of this region
	_ = suite.regionRepo.AddUserToRegion(ctx, adminUser.ID, adminRegion.ID, true)

	defer suite.cleanup([]string{adminUser.ID}, []string{adminRegion.ID})

	t.Run("can create region inside admin region", func(t *testing.T) {
		// Create a region inside the admin region (centered in SF Bay Area)
		reqBody := map[string]interface{}{
			"name": "Inside City",
			"type": models.RegionTypeCity,
			"geometry": map[string]interface{}{
				"type":        "Polygon",
				"coordinates": [][][]float64{{{-122.5, 37.7}, {-122.3, 37.7}, {-122.3, 37.9}, {-122.5, 37.9}, {-122.5, 37.7}}},
			},
		}

		bodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/regions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			Username:         adminUser.Username,
			VerificationTier: adminUser.VerificationTier,
			PostcardVerified: true,
			VouchVerified:    true,
			IsSuperuser:      false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		// Cleanup created region
		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["region_id"] != "" {
			suite.cleanup([]string{}, []string{body["region_id"]})
		}
	})

	t.Run("cannot create region outside admin region", func(t *testing.T) {
		// Create a region outside the admin region (Los Angeles area)
		reqBody := map[string]interface{}{
			"name": "Outside City",
			"type": models.RegionTypeCity,
			"geometry": map[string]interface{}{
				"type":        "Polygon",
				"coordinates": [][][]float64{{{-118.5, 34.0}, {-118.3, 34.0}, {-118.3, 34.2}, {-118.5, 34.2}, {-118.5, 34.0}}},
			},
		}

		bodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/regions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			Username:         adminUser.Username,
			VerificationTier: adminUser.VerificationTier,
			PostcardVerified: true,
			VouchVerified:    true,
			IsSuperuser:      false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Create(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if body["error"] != "outside_admin_regions" {
			t.Errorf("Expected error 'outside_admin_regions', got '%s'", body["error"])
		}

		// Verify admin_regions is included in error response
		if body["admin_regions"] == nil {
			t.Error("Expected admin_regions in error response")
		}
	})

	t.Run("cannot create region partially outside admin region", func(t *testing.T) {
		// Create a region that overlaps but extends outside the admin region
		reqBody := map[string]interface{}{
			"name": "Partial City",
			"type": models.RegionTypeCity,
			"geometry": map[string]interface{}{
				"type":        "Polygon",
				"coordinates": [][][]float64{{{-121.5, 38.5}, {-120.5, 38.5}, {-120.5, 39.5}, {-121.5, 39.5}, {-121.5, 38.5}}},
			},
		}

		bodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/regions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			Username:         adminUser.Username,
			VerificationTier: adminUser.VerificationTier,
			PostcardVerified: true,
			VouchVerified:    true,
			IsSuperuser:      false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Create(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestRegionHandler_Create_SuperuserBypassesBoundaryCheck(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create superuser
	superuser, _ := suite.createTestUser("boundarysuperuser", models.TierPostcard, true)

	defer suite.cleanup([]string{superuser.ID}, []string{})

	t.Run("superuser can create region anywhere", func(t *testing.T) {
		// Create a region anywhere (superuser doesn't need admin regions)
		reqBody := map[string]interface{}{
			"name": "Superuser City",
			"type": models.RegionTypeCity,
			"geometry": map[string]interface{}{
				"type":        "Polygon",
				"coordinates": [][][]float64{{{-118.5, 34.0}, {-118.3, 34.0}, {-118.3, 34.2}, {-118.5, 34.2}, {-118.5, 34.0}}},
			},
		}

		bodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/regions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           superuser.ID,
			Email:            superuser.Email,
			Username:         superuser.Username,
			VerificationTier: superuser.VerificationTier,
			IsSuperuser:      true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		// Cleanup created region
		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["region_id"] != "" {
			suite.cleanup([]string{}, []string{body["region_id"]})
		}
	})
}

func TestRegionHandler_Create_NoAdminRegions(t *testing.T) {
	suite := setupRegionTestSuite(t)

	// Create admin user without any admin regions
	adminUser, _ := suite.createTestUser("noadminregions", models.TierPostcard, false)
	ctx := context.Background()
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET postcard_verified = TRUE, vouch_verified = TRUE WHERE id = ?", adminUser.ID)

	defer suite.cleanup([]string{adminUser.ID}, []string{})

	t.Run("user with no admin regions cannot create region", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"name": "No Admin City",
			"type": models.RegionTypeCity,
			"geometry": map[string]interface{}{
				"type":        "Polygon",
				"coordinates": [][][]float64{{{-122.5, 37.7}, {-122.3, 37.7}, {-122.3, 37.9}, {-122.5, 37.9}, {-122.5, 37.7}}},
			},
		}

		bodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/regions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			Username:         adminUser.Username,
			VerificationTier: adminUser.VerificationTier,
			PostcardVerified: true,
			VouchVerified:    true,
			IsSuperuser:      false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Create(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if body["error"] != "outside_admin_regions" {
			t.Errorf("Expected error 'outside_admin_regions', got '%s'", body["error"])
		}

		// admin_regions should be empty array
		adminRegions, ok := body["admin_regions"].([]interface{})
		if !ok {
			t.Error("Expected admin_regions array in response")
		} else if len(adminRegions) != 0 {
			t.Errorf("Expected empty admin_regions, got %d", len(adminRegions))
		}
	})
}

// TestCalculateGeometryCentroid tests the centroid calculation for Polygon and MultiPolygon
func TestCalculateGeometryCentroid(t *testing.T) {
	t.Run("returns zero for nil geometry", func(t *testing.T) {
		result := calculateGeometryCentroid(nil)
		if result[0] != 0 || result[1] != 0 {
			t.Errorf("Expected [0, 0], got [%f, %f]", result[0], result[1])
		}
	})

	t.Run("returns zero for nil coordinates", func(t *testing.T) {
		geom := &models.GeoJSONGeometry{
			Type:        "Polygon",
			Coordinates: nil,
		}
		result := calculateGeometryCentroid(geom)
		if result[0] != 0 || result[1] != 0 {
			t.Errorf("Expected [0, 0], got [%f, %f]", result[0], result[1])
		}
	})

	t.Run("calculates centroid for Polygon", func(t *testing.T) {
		// Simple square polygon centered at (-122.45, 37.75)
		geom := &models.GeoJSONGeometry{
			Type: "Polygon",
			Coordinates: [][][]float64{
				{
					{-122.5, 37.7},
					{-122.4, 37.7},
					{-122.4, 37.8},
					{-122.5, 37.8},
					{-122.5, 37.7}, // Close the ring
				},
			},
		}
		result := calculateGeometryCentroid(geom)

		// Average of corner points should be center
		expectedLng := (-122.5 + -122.4 + -122.4 + -122.5 + -122.5) / 5.0
		expectedLat := (37.7 + 37.7 + 37.8 + 37.8 + 37.7) / 5.0

		// Use tolerance for floating point comparison
		if abs(result[0]-expectedLng) > 0.001 {
			t.Errorf("Expected longitude ~%f, got %f", expectedLng, result[0])
		}
		if abs(result[1]-expectedLat) > 0.001 {
			t.Errorf("Expected latitude ~%f, got %f", expectedLat, result[1])
		}
	})

	t.Run("calculates centroid for MultiPolygon from JSON unmarshal", func(t *testing.T) {
		// JSON unmarshal produces []interface{} coordinates, not typed arrays
		multiPolygonJSON := `{
			"type": "MultiPolygon",
			"coordinates": [
				[
					[
						[-74.0, 40.7],
						[-74.0, 40.8],
						[-73.9, 40.8],
						[-73.9, 40.7],
						[-74.0, 40.7]
					]
				]
			]
		}`

		var geom models.GeoJSONGeometry
		if err := json.Unmarshal([]byte(multiPolygonJSON), &geom); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		result := calculateGeometryCentroid(&geom)

		// Should calculate centroid from first polygon's outer ring
		// Average of 5 points
		expectedLng := (-74.0 + -74.0 + -73.9 + -73.9 + -74.0) / 5.0
		expectedLat := (40.7 + 40.8 + 40.8 + 40.7 + 40.7) / 5.0

		// Use tolerance for floating point comparison
		if abs(result[0]-expectedLng) > 0.001 {
			t.Errorf("Expected longitude ~%f, got %f", expectedLng, result[0])
		}
		if abs(result[1]-expectedLat) > 0.001 {
			t.Errorf("Expected latitude ~%f, got %f", expectedLat, result[1])
		}
	})

	t.Run("handles Polygon from JSON unmarshal", func(t *testing.T) {
		// JSON unmarshal produces []interface{} coordinates for Polygon too
		polygonJSON := `{
			"type": "Polygon",
			"coordinates": [
				[
					[-122.5, 37.7],
					[-122.4, 37.7],
					[-122.4, 37.8],
					[-122.5, 37.8],
					[-122.5, 37.7]
				]
			]
		}`

		var geom models.GeoJSONGeometry
		if err := json.Unmarshal([]byte(polygonJSON), &geom); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		result := calculateGeometryCentroid(&geom)

		// Should not return zero
		if result[0] == 0 && result[1] == 0 {
			t.Error("Expected non-zero centroid for valid polygon")
		}

		// Should be in reasonable range
		if result[0] < -123 || result[0] > -122 {
			t.Errorf("Longitude %f out of expected range", result[0])
		}
		if result[1] < 37 || result[1] > 38 {
			t.Errorf("Latitude %f out of expected range", result[1])
		}
	})

	t.Run("handles real-world NYC-like MultiPolygon", func(t *testing.T) {
		// Simplified NYC-like MultiPolygon
		nycJSON := `{
			"type": "MultiPolygon",
			"coordinates": [
				[
					[
						[-74.258843, 40.498875],
						[-74.258, 40.497],
						[-73.700, 40.879],
						[-73.728, 40.889],
						[-74.258843, 40.498875]
					]
				]
			]
		}`

		var geom models.GeoJSONGeometry
		if err := json.Unmarshal([]byte(nycJSON), &geom); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		result := calculateGeometryCentroid(&geom)

		// Should return a valid NYC-area centroid
		if result[0] == 0 && result[1] == 0 {
			t.Error("Expected non-zero centroid for NYC-like polygon")
		}

		// Should be in NYC area
		if result[0] < -75 || result[0] > -73 {
			t.Errorf("Longitude %f not in NYC area", result[0])
		}
		if result[1] < 40 || result[1] > 41 {
			t.Errorf("Latitude %f not in NYC area", result[1])
		}
	})
}

// abs returns the absolute value of x
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
