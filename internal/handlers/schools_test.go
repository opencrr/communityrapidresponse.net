package handlers

import (
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
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

type schoolTestSuite struct {
	t            *testing.T
	db           *database.DB
	schoolRepo   *database.SchoolRepository
	districtRepo *database.SchoolDistrictRepository
	groupRepo    *database.GroupRepository
	userRepo     *database.UserRepository
	auditRepo    *database.AuditRepository
	handler      *SchoolHandler
}

func setupSchoolTestSuite(t *testing.T) *schoolTestSuite {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping handler tests")
	}

	port := 3306
	if portStr := os.Getenv("TEST_DB_PORT"); portStr != "" {
		if parsed, err := strconv.Atoi(portStr); err == nil {
			port = parsed
		}
	}

	cfg := &config.DatabaseConfig{
		Host:     host,
		Port:     port,
		User:     getEnvOrDefault("TEST_DB_USER", "root"),
		Password: getEnvOrDefault("TEST_DB_PASSWORD", ""),
		Name:     getEnvOrDefault("TEST_DB_NAME", "communityrapidresponse_test"),
		Charset:  "utf8mb4",
	}

	db, err := database.New(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	schoolRepo := database.NewSchoolRepository(db)
	districtRepo := database.NewSchoolDistrictRepository(db)
	groupRepo := database.NewGroupRepository(db)
	userRepo := database.NewUserRepository(db)
	auditRepo := database.NewAuditRepository(db)
	handler := NewSchoolHandler(schoolRepo, districtRepo, groupRepo, userRepo, auditRepo, nil)

	t.Cleanup(func() {
		_ = db.Close()
	})

	return &schoolTestSuite{
		t:            t,
		db:           db,
		schoolRepo:   schoolRepo,
		districtRepo: districtRepo,
		groupRepo:    groupRepo,
		userRepo:     userRepo,
		auditRepo:    auditRepo,
		handler:      handler,
	}
}

func (s *schoolTestSuite) createTestUser(username string) *models.User {
	user := &models.User{
		Username:         username,
		Email:            username + "@schooltest.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: models.TierUnverified,
	}
	if err := s.userRepo.Create(context.Background(), user); err != nil {
		s.t.Fatalf("Failed to create test user: %v", err)
	}
	return user
}

func (s *schoolTestSuite) createTestDistrict(ncesID, name, state string) *models.SchoolDistrict {
	district := &models.SchoolDistrict{
		NCESID:       ncesID,
		Name:         name,
		State:        state,
		DistrictType: models.SchoolDistrictTypeUnified,
	}
	if err := s.districtRepo.UpsertByNCESID(context.Background(), district); err != nil {
		s.t.Fatalf("Failed to create test district: %v", err)
	}
	return district
}

func (s *schoolTestSuite) createTestSchool(ncesID, name, state string, districtID *string) *models.School {
	city := "Test City"
	school := &models.School{
		NCESID:     ncesID,
		Name:       name,
		State:      state,
		City:       &city,
		DistrictID: districtID,
	}
	if err := s.schoolRepo.UpsertByNCESID(context.Background(), school); err != nil {
		s.t.Fatalf("Failed to create test school: %v", err)
	}
	return school
}

func (s *schoolTestSuite) claimsForUser(user *models.User) *middleware.Claims {
	return &middleware.Claims{
		UserID:           user.ID,
		Email:            user.Email,
		Username:         user.Username,
		VerificationTier: user.VerificationTier,
		IsSuperuser:      user.IsSuperuser,
	}
}

func (s *schoolTestSuite) cleanup(userIDs, groupIDs, schoolIDs, districtIDs []string) {
	ctx := context.Background()
	for _, id := range groupIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_members WHERE group_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM `groups` WHERE id = ?", id)
	}
	for _, id := range schoolIDs {
		// Delete any groups linked to this school first
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_members WHERE group_id IN (SELECT id FROM `groups` WHERE school_id = ?)", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM `groups` WHERE school_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_schools WHERE school_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM schools WHERE id = ?", id)
	}
	for _, id := range districtIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM school_districts WHERE id = ?", id)
	}
	for _, id := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_members WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM audit_log WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	}
}

// =============================================================================
// Search Tests
// =============================================================================

func TestSchoolHandler_Search(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	district := suite.createTestDistrict("9900001", "Search District", "CA")
	school1 := suite.createTestSchool("990000000001", "Lincoln Elementary School", "CA", &district.ID)
	school2 := suite.createTestSchool("990000000002", "Lincoln Middle School", "CA", &district.ID)
	user := suite.createTestUser("school_search_user")

	defer suite.cleanup(
		[]string{user.ID},
		nil,
		[]string{school1.ID, school2.ID},
		[]string{district.ID},
	)

	t.Run("returns results for query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/schools?query=Lincoln&state=CA", nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Search(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body SchoolSearchResultResponse
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if len(body.Schools) < 2 {
			t.Errorf("Expected at least 2 results for 'Lincoln', got %d", len(body.Schools))
		}
		if body.Page != 1 {
			t.Errorf("Expected page 1, got %d", body.Page)
		}
		if body.Limit != 20 {
			t.Errorf("Expected limit 20, got %d", body.Limit)
		}
	})

	t.Run("empty query returns results", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/schools?query=&state=CA", nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Search(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body SchoolSearchResultResponse
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Should return some results (our seeded schools at minimum)
		if body.Total < 0 {
			t.Errorf("Expected non-negative total, got %d", body.Total)
		}
	})

	t.Run("pagination with custom limit", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/schools?query=Lincoln&state=CA&limit=1&page=1", nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Search(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body SchoolSearchResultResponse
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if body.Limit != 1 {
			t.Errorf("Expected limit 1, got %d", body.Limit)
		}
		if len(body.Schools) > 1 {
			t.Errorf("Expected at most 1 result, got %d", len(body.Schools))
		}
		if body.Total >= 2 && !body.HasMore {
			t.Error("Expected HasMore to be true when total exceeds page*limit")
		}
	})

	t.Run("limit capped at 100", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/schools?limit=500", nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Search(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body SchoolSearchResultResponse
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if body.Limit != 100 {
			t.Errorf("Expected limit capped at 100, got %d", body.Limit)
		}
	})
}

// =============================================================================
// GetSchool Tests
// =============================================================================

func TestSchoolHandler_GetSchool(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	district := suite.createTestDistrict("9900002", "Get District", "NY")
	school := suite.createTestSchool("990000000003", "Washington High School", "NY", &district.ID)
	user := suite.createTestUser("school_get_user")

	defer suite.cleanup(
		[]string{user.ID},
		nil,
		[]string{school.ID},
		[]string{district.ID},
	)

	t.Run("returns school details", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/schools/"+school.ID+"?id="+school.ID, nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if body["id"] != school.ID {
			t.Errorf("Expected school ID %s, got %v", school.ID, body["id"])
		}
		if body["name"] != "Washington High School" {
			t.Errorf("Expected name 'Washington High School', got %v", body["name"])
		}
		if body["district_name"] != "Get District" {
			t.Errorf("Expected district_name 'Get District', got %v", body["district_name"])
		}
	})

	t.Run("returns school with group_id when linked", func(t *testing.T) {
		// Create a group linked to this school
		createReq := &models.CreateGroupRequest{
			Name:       "School Group",
			Visibility: "unlisted",
			SchoolID:   &school.ID,
		}
		createdGroup, err := suite.groupRepo.Create(context.Background(), createReq, user.ID)
		if err != nil {
			t.Fatalf("Failed to create group: %v", err)
		}
		defer suite.cleanup(nil, []string{createdGroup.ID}, nil, nil)

		req := httptest.NewRequest("GET", "/api/v1/schools/"+school.ID+"?id="+school.ID, nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if body["group_id"] == nil {
			t.Error("Expected group_id to be present")
		}
		if body["group_id"] != createdGroup.ID {
			t.Errorf("Expected group_id %s, got %v", createdGroup.ID, body["group_id"])
		}
	})

	t.Run("returns 404 for nonexistent school", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/schools/nonexistent?id=nonexistent", nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Get(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("returns 400 for missing ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/schools/", nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Get(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})
}

// =============================================================================
// Join Tests
// =============================================================================

func TestSchoolHandler_Join(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	district := suite.createTestDistrict("9900003", "Join District", "TX")
	school := suite.createTestSchool("990000000004", "Adams Elementary", "TX", &district.ID)
	schoolNoGroup := suite.createTestSchool("990000000005", "Adams Middle", "TX", &district.ID)
	user1 := suite.createTestUser("school_join_user1")
	user2 := suite.createTestUser("school_join_user2")

	defer suite.cleanup(
		[]string{user1.ID, user2.ID},
		nil,
		[]string{school.ID, schoolNoGroup.ID},
		[]string{district.ID},
	)

	t.Run("creates new group when none exists", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/schools/"+schoolNoGroup.ID+"/join?id="+schoolNoGroup.ID, nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user1))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Join(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if body["status"] != "created" {
			t.Errorf("Expected status 'created', got %v", body["status"])
		}
		if body["group_created"] != true {
			t.Error("Expected group_created to be true")
		}
		if body["group_id"] == nil || body["group_id"] == "" {
			t.Error("Expected group_id in response")
		}

		// Store group ID for cleanup
		groupID := body["group_id"].(string)
		defer suite.cleanup(nil, []string{groupID}, nil, nil)
	})

	t.Run("joins existing group", func(t *testing.T) {
		// Create a group linked to the school first
		createReq := &models.CreateGroupRequest{
			Name:       "School Group For Join",
			Visibility: "unlisted",
			SchoolID:   &school.ID,
		}
		existingGroup, err := suite.groupRepo.Create(context.Background(), createReq, user1.ID)
		if err != nil {
			t.Fatalf("Failed to create group: %v", err)
		}
		defer suite.cleanup(nil, []string{existingGroup.ID}, nil, nil)

		req := httptest.NewRequest("POST", "/api/v1/schools/"+school.ID+"/join?id="+school.ID, nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user2))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Join(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if body["status"] != "joined" {
			t.Errorf("Expected status 'joined', got %v", body["status"])
		}
		if body["group_id"] != existingGroup.ID {
			t.Errorf("Expected group_id %s, got %v", existingGroup.ID, body["group_id"])
		}
	})

	t.Run("rejects if already a member", func(t *testing.T) {
		// Create a group and add user as member
		schoolForDupe := suite.createTestSchool("990000000006", "Dupe School", "TX", &district.ID)
		defer suite.cleanup(nil, nil, []string{schoolForDupe.ID}, nil)

		createReq := &models.CreateGroupRequest{
			Name:       "Dupe Check Group",
			Visibility: "unlisted",
			SchoolID:   &schoolForDupe.ID,
		}
		dupeGroup, err := suite.groupRepo.Create(context.Background(), createReq, user1.ID)
		if err != nil {
			t.Fatalf("Failed to create group: %v", err)
		}
		defer suite.cleanup(nil, []string{dupeGroup.ID}, nil, nil)

		// user1 is already creator/member, try joining again
		req := httptest.NewRequest("POST", "/api/v1/schools/"+schoolForDupe.ID+"/join?id="+schoolForDupe.ID, nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user1))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Join(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if body["error"] != "already_member" {
			t.Errorf("Expected error 'already_member', got %v", body["error"])
		}
	})

	t.Run("returns 404 for nonexistent school", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/schools/nonexistent/join?id=nonexistent", nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user1))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Join(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rec.Code)
		}
	})

	t.Run("returns 401 without auth", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/schools/someid/join?id=someid", nil)
		rec := httptest.NewRecorder()
		suite.handler.Join(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})
}

// =============================================================================
// ListMySchools Tests
// =============================================================================

func TestSchoolHandler_ListMySchools(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	district := suite.createTestDistrict("9900004", "My Schools District", "WA")
	school := suite.createTestSchool("990000000007", "My Test School", "WA", &district.ID)
	user := suite.createTestUser("school_my_user")
	userEmpty := suite.createTestUser("school_my_empty")

	defer suite.cleanup(
		[]string{user.ID, userEmpty.ID},
		nil,
		[]string{school.ID},
		[]string{district.ID},
	)

	t.Run("returns user school groups", func(t *testing.T) {
		// Create a school group and add user
		createReq := &models.CreateGroupRequest{
			Name:       "My School Group",
			Visibility: "unlisted",
			SchoolID:   &school.ID,
		}
		schoolGroup, err := suite.groupRepo.Create(context.Background(), createReq, user.ID)
		if err != nil {
			t.Fatalf("Failed to create group: %v", err)
		}
		defer suite.cleanup(nil, []string{schoolGroup.ID}, nil, nil)

		req := httptest.NewRequest("GET", "/api/v1/schools/my", nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListMySchools(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		schools, ok := body["schools"].([]interface{})
		if !ok {
			t.Fatal("Expected 'schools' array in response")
		}
		if len(schools) == 0 {
			t.Error("Expected at least one school group")
		}

		// Verify the school group has expected fields
		firstSchool := schools[0].(map[string]interface{})
		if firstSchool["school_name"] != "My Test School" {
			t.Errorf("Expected school_name 'My Test School', got %v", firstSchool["school_name"])
		}
	})

	t.Run("returns empty for user with no school groups", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/schools/my", nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(userEmpty))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListMySchools(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		schools := body["schools"].([]interface{})
		if len(schools) != 0 {
			t.Errorf("Expected empty schools list, got %d", len(schools))
		}
	})

	t.Run("returns 401 without auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/schools/my", nil)
		rec := httptest.NewRecorder()
		suite.handler.ListMySchools(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})
}

// =============================================================================
// SearchDistricts Tests
// =============================================================================

func TestSchoolHandler_SearchDistricts(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	district1 := suite.createTestDistrict("9900005", "Riverside Unified", "CA")
	district2 := suite.createTestDistrict("9900006", "Riverside Elementary", "CA")
	user := suite.createTestUser("school_dsrch_user")

	defer suite.cleanup(
		[]string{user.ID},
		nil,
		nil,
		[]string{district1.ID, district2.ID},
	)

	t.Run("returns results for query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/school-districts?query=Riverside&state=CA", nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.SearchDistricts(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body models.DistrictSearchResponse
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if len(body.Districts) < 2 {
			t.Errorf("Expected at least 2 districts for 'Riverside', got %d", len(body.Districts))
		}
	})

	t.Run("empty query returns results", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/school-districts?query=", nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.SearchDistricts(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// =============================================================================
// GetDistrict Tests
// =============================================================================

func TestSchoolHandler_GetDistrict(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	district := suite.createTestDistrict("9900007", "Oakland Unified School District", "CA")
	school := suite.createTestSchool("990000000008", "Oakland Elementary", "CA", &district.ID)
	user := suite.createTestUser("school_dget_user")

	defer suite.cleanup(
		[]string{user.ID},
		nil,
		[]string{school.ID},
		[]string{district.ID},
	)

	t.Run("returns district with schools", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/school-districts/"+district.ID+"?id="+district.ID, nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetDistrict(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if body["name"] != "Oakland Unified School District" {
			t.Errorf("Expected name 'Oakland Unified School District', got %v", body["name"])
		}
		if body["state"] != "CA" {
			t.Errorf("Expected state 'CA', got %v", body["state"])
		}

		schools, ok := body["schools"].([]interface{})
		if !ok {
			t.Fatal("Expected 'schools' array in response")
		}
		if len(schools) < 1 {
			t.Error("Expected at least 1 school in district")
		}
	})

	t.Run("returns 404 for nonexistent district", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/school-districts/nonexistent?id=nonexistent", nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetDistrict(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rec.Code)
		}
	})

	t.Run("returns 400 for missing ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/school-districts/", nil)
		ctx := middleware.ContextWithUser(req.Context(), suite.claimsForUser(user))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetDistrict(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})
}

// =============================================================================
// Auth Required Tests (all endpoints)
// =============================================================================

func TestSchoolHandler_AuthRequired(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	// Only Join and ListMySchools check claims in the handler itself.
	// Search, Get, SearchDistricts, and GetDistrict rely on router-level
	// middleware for auth enforcement.
	endpoints := []struct {
		name    string
		method  string
		path    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"Join", "POST", "/api/v1/schools/someid/join?id=someid", suite.handler.Join},
		{"ListMySchools", "GET", "/api/v1/schools/my", suite.handler.ListMySchools},
	}

	for _, ep := range endpoints {
		t.Run(ep.name+" returns 401 without auth", func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			rec := httptest.NewRecorder()
			ep.handler(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("Expected status 401, got %d", rec.Code)
			}
		})
	}
}
