package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

type signalGroupTestSuite struct {
	t                   *testing.T
	db                  *database.DB
	userRepo            *database.UserRepository
	regionRepo          *database.RegionRepository
	groupRepo           *database.SignalGroupRepository
	encryptedSecretRepo *database.EncryptedSecretRepository
	handler             *SignalGroupHandler
}

func setupSignalGroupTestSuite(t *testing.T) *signalGroupTestSuite {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	regionRepo := database.NewRegionRepository(db)
	groupRepo := database.NewSignalGroupRepository(db)
	encryptedSecretRepo := database.NewEncryptedSecretRepository(db)
	handler := NewSignalGroupHandler(db, groupRepo, encryptedSecretRepo, regionRepo, nil)

	return &signalGroupTestSuite{
		t:                   t,
		db:                  db,
		userRepo:            userRepo,
		regionRepo:          regionRepo,
		groupRepo:           groupRepo,
		encryptedSecretRepo: encryptedSecretRepo,
		handler:             handler,
	}
}

func (s *signalGroupTestSuite) createTestUser(username string, tier models.VerificationTier) *models.User {
	user := &models.User{
		Username:         username,
		Email:            username + "@signalgrouptest.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: tier,
	}
	if err := s.userRepo.Create(context.Background(), user); err != nil {
		s.t.Fatalf("Failed to create test user: %v", err)
	}
	return user
}

func (s *signalGroupTestSuite) createTestRegion(name string) *models.GeographicRegion {
	region := &models.GeographicRegion{
		Name:       name,
		RegionType: models.RegionTypeCity,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	if err := s.regionRepo.Create(context.Background(), region, geoJSON); err != nil {
		s.t.Fatalf("Failed to create test region: %v", err)
	}
	return region
}

func (s *signalGroupTestSuite) addUserToRegion(userID, regionID string, isAdmin bool) {
	_, err := s.db.ExecContext(context.Background(),
		"INSERT INTO user_regions (id, user_id, region_id, is_admin, verified_at) VALUES (UUID(), ?, ?, ?, NOW())",
		userID, regionID, isAdmin)
	if err != nil {
		s.t.Fatalf("Failed to add user to region: %v", err)
	}
}

func (s *signalGroupTestSuite) cleanup(userIDs []string, regionIDs []string, groupIDs []string) {
	ctx := context.Background()
	for _, id := range groupIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", id)
	}
	for _, id := range regionIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE region_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", id)
	}
	for _, id := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	}
}

// =============================================================================
// Create Tests
// =============================================================================

func TestSignalGroupHandler_Create(t *testing.T) {
	suite := setupSignalGroupTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@signalgrouptest.com'")

	adminUser := suite.createTestUser("sg_admin", models.TierPostcard)
	regularUser := suite.createTestUser("sg_regular", models.TierUnverified)
	region := suite.createTestRegion("Signal Group Test Region")

	suite.addUserToRegion(adminUser.ID, region.ID, true)
	suite.addUserToRegion(regularUser.ID, region.ID, false)

	defer suite.cleanup([]string{adminUser.ID, regularUser.ID}, []string{region.ID}, []string{})

	t.Run("admin can create signal group", func(t *testing.T) {
		body := map[string]interface{}{
			"region_id":         region.ID,
			"name":              "Test Signal Group",
			"description":       "A test group",
			"encrypted_payload": "test-encrypted-payload",
			"encryption_iv":     "test-iv",
			"wrapped_keys":      []map[string]string{{"user_id": adminUser.ID, "wrapped_dek": "test-dek"}},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			VerificationTier: adminUser.VerificationTier,
			IsSuperuser:      true, // Bypass bootstrap mode for this test
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["group_id"] == nil {
			t.Error("Expected group_id in response")
		}
		if respBody["group_name"] != "Test Signal Group" {
			t.Errorf("Expected group_name 'Test Signal Group', got %v", respBody["group_name"])
		}

		// Clean up the created group
		if groupID, ok := respBody["group_id"].(string); ok {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM signal_groups WHERE id = ?", groupID)
		}
	})

	t.Run("non-admin cannot create signal group", func(t *testing.T) {
		body := map[string]interface{}{
			"region_id":         region.ID,
			"name":              "Unauthorized Group",
			"encrypted_payload": "test-encrypted-payload",
			"encryption_iv":     "test-iv",
			"wrapped_keys":      []map[string]string{{"user_id": regularUser.ID, "wrapped_dek": "test-dek"}},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           regularUser.ID,
			Email:            regularUser.Email,
			VerificationTier: regularUser.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Create(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})

	t.Run("missing required fields fails", func(t *testing.T) {
		body := map[string]string{
			"region_id": region.ID,
			// Missing name, encrypted_payload, encryption_iv, and wrapped_keys
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			VerificationTier: adminUser.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("unauthenticated request fails", func(t *testing.T) {
		body := map[string]interface{}{
			"region_id":         region.ID,
			"name":              "Unauth Group",
			"encrypted_payload": "test-encrypted-payload",
			"encryption_iv":     "test-iv",
			"wrapped_keys":      []map[string]string{{"user_id": adminUser.ID, "wrapped_dek": "test-dek"}},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		suite.handler.Create(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})

	t.Run("group limit prevents creation", func(t *testing.T) {
		// Create 5 groups to hit the limit
		for i := 0; i < 5; i++ {
			group := &models.SignalGroup{
				RegionID:  &region.ID,
				GroupName: "Limit Test Group",
				CreatedBy: &adminUser.ID,
			}
			_ = suite.groupRepo.Create(context.Background(), group)
		}

		body := map[string]interface{}{
			"region_id":         region.ID,
			"name":              "Over Limit Group",
			"encrypted_payload": "test-encrypted-payload",
			"encryption_iv":     "test-iv",
			"wrapped_keys":      []map[string]string{{"user_id": adminUser.ID, "wrapped_dek": "test-dek"}},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			VerificationTier: adminUser.VerificationTier,
			IsSuperuser:      true, // Bypass bootstrap mode for this test
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Create(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}

		// Clean up
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM signal_groups WHERE region_id = ?", region.ID)
	})
}

// =============================================================================
// List Tests
// =============================================================================

func TestSignalGroupHandler_List(t *testing.T) {
	suite := setupSignalGroupTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@signalgrouptest.com'")

	verifiedUser := suite.createTestUser("sg_verified", models.TierVouched)
	unverifiedUser := suite.createTestUser("sg_unverified", models.TierUnverified)
	region := suite.createTestRegion("List Test Region")

	// Create a signal group
	group := &models.SignalGroup{
		RegionID:  &region.ID,
		GroupName: "List Test Group",
		CreatedBy: &verifiedUser.ID,
	}
	_ = suite.groupRepo.Create(context.Background(), group)

	defer suite.cleanup([]string{verifiedUser.ID, unverifiedUser.ID}, []string{region.ID}, []string{group.ID})

	t.Run("verified user can list signal groups", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/signal-groups?region_id="+region.ID, nil)

		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: verifiedUser.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body models.SignalGroupListResponse
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if len(body.Groups) == 0 {
			t.Error("Expected at least one group in response")
		}
	})

	t.Run("unverified user cannot list signal groups", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/signal-groups?region_id="+region.ID, nil)

		claims := &middleware.Claims{
			UserID:           unverifiedUser.ID,
			Email:            unverifiedUser.Email,
			VerificationTier: unverifiedUser.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.List(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})

	t.Run("missing region_id returns user's groups", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/signal-groups", nil)

		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: verifiedUser.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.List(rec, req)

		// Without region_id, the handler now returns all groups for the user
		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		// Verify response has groups array
		if _, ok := respBody["groups"].([]interface{}); !ok {
			t.Error("Expected 'groups' array in response")
		}
	})

	t.Run("unauthenticated request fails", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/signal-groups?region_id="+region.ID, nil)

		rec := httptest.NewRecorder()
		suite.handler.List(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})
}

// =============================================================================
// Update Tests
// =============================================================================

func TestSignalGroupHandler_Update(t *testing.T) {
	suite := setupSignalGroupTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@signalgrouptest.com'")

	adminUser := suite.createTestUser("sg_update_admin", models.TierPostcard)
	regularUser := suite.createTestUser("sg_update_regular", models.TierPostcard)
	region := suite.createTestRegion("Update Test Region")

	suite.addUserToRegion(adminUser.ID, region.ID, true)
	suite.addUserToRegion(regularUser.ID, region.ID, false)

	group := &models.SignalGroup{
		RegionID:  &region.ID,
		GroupName: "Original Name",
		CreatedBy: &adminUser.ID,
	}
	_ = suite.groupRepo.Create(context.Background(), group)

	defer suite.cleanup([]string{adminUser.ID, regularUser.ID}, []string{region.ID}, []string{group.ID})

	t.Run("admin can update signal group name", func(t *testing.T) {
		body := map[string]string{
			"name":        "Updated Name",
			"description": "Updated description",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("PUT", "/api/v1/signal-groups/"+group.ID, bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", group.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			VerificationTier: adminUser.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("non-admin cannot update signal group", func(t *testing.T) {
		body := map[string]string{
			"name": "Unauthorized Update",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("PUT", "/api/v1/signal-groups/"+group.ID, bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", group.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           regularUser.ID,
			Email:            regularUser.Email,
			VerificationTier: regularUser.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Update(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})

	t.Run("non-existent group returns 404", func(t *testing.T) {
		body := map[string]string{
			"name": "Update Non-existent",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("PUT", "/api/v1/signal-groups/nonexistent", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", "nonexistent")
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			VerificationTier: adminUser.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Update(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rec.Code)
		}
	})
}

// =============================================================================
// ListAdmin Tests
// =============================================================================

func TestSignalGroupHandler_ListAdmin(t *testing.T) {
	suite := setupSignalGroupTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@signalgrouptest.com'")

	// Create admin user (needs both postcard and vouch verification)
	adminUser := suite.createTestUser("sg_listadmin_user", models.TierVouched)
	_, _ = suite.db.ExecContext(context.Background(), "UPDATE users SET postcard_verified = TRUE, vouch_verified = TRUE WHERE id = ?", adminUser.ID)

	// Create non-admin user
	regularUser := suite.createTestUser("sg_listadmin_regular", models.TierPostcard)

	// Create region hierarchy: state -> county -> city
	stateRegion := suite.createTestRegionWithType("Test State", models.RegionTypeState, nil)
	countyRegion := suite.createTestRegionWithType("Test County", models.RegionTypeCounty, &stateRegion.ID)
	cityRegion := suite.createTestRegionWithType("Test City", models.RegionTypeCity, &countyRegion.ID)

	// Make user admin of city only
	suite.addUserToRegion(adminUser.ID, cityRegion.ID, true)

	// Create signal groups in each region
	cityGroup := &models.SignalGroup{
		RegionID:  &cityRegion.ID,
		GroupName: "City Group",
		CreatedBy: &adminUser.ID,
	}
	_ = suite.groupRepo.Create(context.Background(), cityGroup)

	countyGroup := &models.SignalGroup{
		RegionID:  &countyRegion.ID,
		GroupName: "County Group",
		CreatedBy: &adminUser.ID,
	}
	_ = suite.groupRepo.Create(context.Background(), countyGroup)

	stateGroup := &models.SignalGroup{
		RegionID:  &stateRegion.ID,
		GroupName: "State Group",
		CreatedBy: &adminUser.ID,
	}
	_ = suite.groupRepo.Create(context.Background(), stateGroup)

	defer suite.cleanup(
		[]string{adminUser.ID, regularUser.ID},
		[]string{cityRegion.ID, countyRegion.ID, stateRegion.ID},
		[]string{cityGroup.ID, countyGroup.ID, stateGroup.ID},
	)

	t.Run("admin sees groups from admin regions and parent regions", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/signal-groups/admin", nil)

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			VerificationTier: adminUser.VerificationTier,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListAdmin(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body models.SignalGroupListResponse
		_ = json.NewDecoder(rec.Body).Decode(&body)

		// Should see all 3 groups (city direct, county parent, state grandparent)
		if len(body.Groups) != 3 {
			t.Errorf("Expected 3 groups, got %d", len(body.Groups))
		}

		// Verify groups have region names
		for _, g := range body.Groups {
			if g.RegionName == "" {
				t.Error("Expected region_name in response")
			}
		}
	})

	t.Run("non-admin gets forbidden", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/signal-groups/admin", nil)

		claims := &middleware.Claims{
			UserID:           regularUser.ID,
			Email:            regularUser.Email,
			VerificationTier: regularUser.VerificationTier,
			PostcardVerified: true,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListAdmin(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})
}

// =============================================================================
// Create with name field Tests
// =============================================================================

func TestSignalGroupHandler_Create_NameField(t *testing.T) {
	suite := setupSignalGroupTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@signalgrouptest.com'")

	adminUser := suite.createTestUser("sg_name_admin", models.TierPostcard)
	region := suite.createTestRegion("Name Test Region")
	suite.addUserToRegion(adminUser.ID, region.ID, true)

	defer suite.cleanup([]string{adminUser.ID}, []string{region.ID}, []string{})

	t.Run("create with name field succeeds", func(t *testing.T) {
		body := map[string]interface{}{
			"region_id":         region.ID,
			"name":              "Test Group Name",
			"encrypted_payload": "test-encrypted-payload",
			"encryption_iv":     "test-iv",
			"wrapped_keys":      []map[string]string{{"user_id": adminUser.ID, "wrapped_dek": "test-dek"}},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			VerificationTier: adminUser.VerificationTier,
			IsSuperuser:      true, // Bypass bootstrap mode for this test
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["group_name"] != "Test Group Name" {
			t.Errorf("Expected group_name 'Test Group Name', got %v", respBody["group_name"])
		}

		// Clean up
		if groupID, ok := respBody["group_id"].(string); ok {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM signal_groups WHERE id = ?", groupID)
		}
	})
}

// =============================================================================
// Superuser Bypass Tests
// =============================================================================

func TestSignalGroupHandler_Create_SuperuserBypass(t *testing.T) {
	suite := setupSignalGroupTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@signalgrouptest.com'")

	superuser := suite.createTestUser("sg_superuser", models.TierPostcard)
	_, _ = suite.db.ExecContext(context.Background(), "UPDATE users SET is_superuser = TRUE WHERE id = ?", superuser.ID)

	region := suite.createTestRegion("Superuser Test Region")
	// Note: superuser is NOT added to region as admin

	defer suite.cleanup([]string{superuser.ID}, []string{region.ID}, []string{})

	t.Run("superuser can create group in any region", func(t *testing.T) {
		body := map[string]interface{}{
			"region_id":         region.ID,
			"name":              "Superuser Group",
			"encrypted_payload": "test-encrypted-payload",
			"encryption_iv":     "test-iv",
			"wrapped_keys":      []map[string]string{{"user_id": superuser.ID, "wrapped_dek": "test-dek"}},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           superuser.ID,
			Email:            superuser.Email,
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

		// Clean up
		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)
		if groupID, ok := respBody["group_id"].(string); ok {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM signal_groups WHERE id = ?", groupID)
		}
	})
}

// =============================================================================
// Admin Hierarchy Tests
// =============================================================================

func TestSignalGroupHandler_Create_AdminHierarchy(t *testing.T) {
	suite := setupSignalGroupTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@signalgrouptest.com'")

	adminUser := suite.createTestUser("sg_hierarchy_admin", models.TierPostcard)

	// Create region hierarchy
	stateRegion := suite.createTestRegionWithType("Hierarchy State", models.RegionTypeState, nil)
	countyRegion := suite.createTestRegionWithType("Hierarchy County", models.RegionTypeCounty, &stateRegion.ID)
	cityRegion := suite.createTestRegionWithType("Hierarchy City", models.RegionTypeCity, &countyRegion.ID)

	// Make user admin of city only
	suite.addUserToRegion(adminUser.ID, cityRegion.ID, true)

	defer suite.cleanup([]string{adminUser.ID}, []string{cityRegion.ID, countyRegion.ID, stateRegion.ID}, []string{})

	t.Run("admin of child can create group in parent region", func(t *testing.T) {
		// Try to create group in county (parent of city where user is admin)
		body := map[string]interface{}{
			"region_id":         countyRegion.ID,
			"name":              "County Group via Hierarchy",
			"encrypted_payload": "test-encrypted-payload",
			"encryption_iv":     "test-iv",
			"wrapped_keys":      []map[string]string{{"user_id": adminUser.ID, "wrapped_dek": "test-dek"}},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			VerificationTier: adminUser.VerificationTier,
			IsSuperuser:      true, // Bypass bootstrap mode for this test
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		// Clean up
		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)
		if groupID, ok := respBody["group_id"].(string); ok {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM signal_groups WHERE id = ?", groupID)
		}
	})

	t.Run("admin of child can create group in grandparent region", func(t *testing.T) {
		// Try to create group in state (grandparent of city where user is admin)
		body := map[string]interface{}{
			"region_id":         stateRegion.ID,
			"name":              "State Group via Hierarchy",
			"encrypted_payload": "test-encrypted-payload",
			"encryption_iv":     "test-iv",
			"wrapped_keys":      []map[string]string{{"user_id": adminUser.ID, "wrapped_dek": "test-dek"}},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			VerificationTier: adminUser.VerificationTier,
			IsSuperuser:      true, // Bypass bootstrap mode for this test
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		// Clean up
		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)
		if groupID, ok := respBody["group_id"].(string); ok {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM signal_groups WHERE id = ?", groupID)
		}
	})
}

// =============================================================================
// List without region_id Tests
// =============================================================================

func TestSignalGroupHandler_List_NoRegionID(t *testing.T) {
	suite := setupSignalGroupTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@signalgrouptest.com'")

	verifiedUser := suite.createTestUser("sg_list_noregion", models.TierPostcard)
	region := suite.createTestRegion("List No Region Test")

	// Add user to region
	suite.addUserToRegion(verifiedUser.ID, region.ID, false)

	// Create a signal group
	group := &models.SignalGroup{
		RegionID:  &region.ID,
		GroupName: "User's Region Group",
		CreatedBy: &verifiedUser.ID,
	}
	_ = suite.groupRepo.Create(context.Background(), group)

	defer suite.cleanup([]string{verifiedUser.ID}, []string{region.ID}, []string{group.ID})

	t.Run("list without region_id returns user's groups", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/signal-groups", nil)

		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: verifiedUser.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body models.SignalGroupListResponse
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if len(body.Groups) == 0 {
			t.Error("Expected at least one group in response")
		}

		// Verify the group has 'name' field (not 'group_name')
		foundGroup := false
		for _, g := range body.Groups {
			if g.Name == "User's Region Group" {
				foundGroup = true
			}
		}
		if !foundGroup {
			t.Error("Expected to find user's region group")
		}
	})
}

// =============================================================================
// Empty Response Tests
// =============================================================================

func TestSignalGroupHandler_List_EmptyResponse(t *testing.T) {
	suite := setupSignalGroupTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@signalgrouptest.com'")

	verifiedUser := suite.createTestUser("sg_empty_user", models.TierPostcard)
	region := suite.createTestRegion("Empty Region")
	suite.addUserToRegion(verifiedUser.ID, region.ID, false)

	// Don't create any groups

	defer suite.cleanup([]string{verifiedUser.ID}, []string{region.ID}, []string{})

	t.Run("empty list returns empty array not null", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/signal-groups", nil)

		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: verifiedUser.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Check raw JSON to verify it's [] not null
		bodyStr := rec.Body.String()
		if bodyStr == "" || bodyStr == "null" {
			t.Error("Expected non-null response")
		}

		var body models.SignalGroupListResponse
		_ = json.NewDecoder(bytes.NewReader([]byte(bodyStr))).Decode(&body)

		// Groups should be empty array, not nil
		if body.Groups == nil {
			t.Error("Expected groups to be empty array, not nil")
		}
	})
}

// Helper to create region with specific type
func (s *signalGroupTestSuite) createTestRegionWithType(name string, regionType models.RegionType, parentID *string) *models.GeographicRegion {
	region := &models.GeographicRegion{
		Name:           name,
		RegionType:     regionType,
		ParentRegionID: parentID,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	if err := s.regionRepo.Create(context.Background(), region, geoJSON); err != nil {
		s.t.Fatalf("Failed to create test region: %v", err)
	}
	return region
}
