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
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

type GroupTestSuite struct {
	t          *testing.T
	db         *database.DB
	groupRepo  *database.GroupRepository
	regionRepo *database.RegionRepository
	userRepo   *database.UserRepository
	auditRepo  *database.AuditRepository
	handler    *GroupHandler
	jwtAuth    *middleware.JWTAuth
}

func setupGroupTestSuite(t *testing.T) *GroupTestSuite {
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

	groupRepo := database.NewGroupRepository(db)
	regionRepo := database.NewRegionRepository(db)
	userRepo := database.NewUserRepository(db)
	auditRepo := database.NewAuditRepository(db)
	handler := NewGroupHandler(groupRepo, regionRepo, userRepo, auditRepo)

	jwtConfig := &config.JWTConfig{
		Secret:          "test_secret_key_at_least_32_characters_long",
		ExpirationHours: 24,
		Issuer:          "test_issuer",
	}
	jwtAuth := middleware.NewJWTAuth(jwtConfig)

	suite := &GroupTestSuite{
		t:          t,
		db:         db,
		groupRepo:  groupRepo,
		regionRepo: regionRepo,
		userRepo:   userRepo,
		auditRepo:  auditRepo,
		handler:    handler,
		jwtAuth:    jwtAuth,
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return suite
}

func (s *GroupTestSuite) createTestUser(username string, tier models.VerificationTier, isSuperuser bool) *models.User {
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
	return user
}

func (s *GroupTestSuite) createTestRegion(name string, regionType models.RegionType, parentID *string) *models.GeographicRegion {
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

func (s *GroupTestSuite) claimsForUser(user *models.User) *middleware.Claims {
	return &middleware.Claims{
		UserID:           user.ID,
		Email:            user.Email,
		Username:         user.Username,
		VerificationTier: user.VerificationTier,
		VouchVerified:    user.VouchVerified,
		PostcardVerified: user.PostcardVerified,
		IsSuperuser:      user.IsSuperuser,
	}
}

func (s *GroupTestSuite) cleanup(userIDs, regionIDs, groupIDs []string) {
	ctx := context.Background()
	for _, id := range groupIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_topic_tags WHERE group_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_members WHERE group_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_regions WHERE group_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM `groups` WHERE id = ?", id)
	}
	for _, id := range regionIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_regions WHERE region_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", id)
	}
	for _, id := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_members WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM audit_log WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	}
}

// --- Create Tests ---

func TestGroupHandler_Create_Success(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpcreate_ok", models.TierVouched, false)
	user.VouchVerified = true
	region := suite.createTestRegion("GrpTestRegion1", models.RegionTypeCity, nil)

	// Make user a member of the region
	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, false)

	defer suite.cleanup([]string{user.ID}, []string{region.ID}, nil)

	claims := suite.claimsForUser(user)
	body := models.CreateGroupRequest{
		Name:       "Test Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/groups", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created models.Group
	_ = json.NewDecoder(rec.Body).Decode(&created)
	if created.ID == "" {
		t.Error("Expected group ID in response")
	}
	if created.Name != "Test Group" {
		t.Errorf("Expected name 'Test Group', got '%s'", created.Name)
	}

	// Cleanup created group
	suite.cleanup(nil, nil, []string{created.ID})
}

func TestGroupHandler_Create_UnverifiedUser(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpcreate_unverified", models.TierUnverified, false)
	region := suite.createTestRegion("GrpTestRegion2", models.RegionTypeCity, nil)

	defer suite.cleanup([]string{user.ID}, []string{region.ID}, nil)

	claims := suite.claimsForUser(user)
	body := models.CreateGroupRequest{
		Name:       "Test Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/groups", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Create(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_Create_MissingName(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpcreate_noname", models.TierVouched, false)
	user.VouchVerified = true
	region := suite.createTestRegion("GrpTestRegion3", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, false)

	defer suite.cleanup([]string{user.ID}, []string{region.ID}, nil)

	claims := suite.claimsForUser(user)
	body := map[string]interface{}{
		"name":       "ab", // too short
		"visibility": "unlisted",
		"region_ids": []string{region.ID},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/groups", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_Create_NoRegionIDs(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpcreate_noregion", models.TierVouched, false)
	user.VouchVerified = true

	defer suite.cleanup([]string{user.ID}, nil, nil)

	claims := suite.claimsForUser(user)
	body := map[string]interface{}{
		"name":       "Valid Name",
		"visibility": "unlisted",
		"region_ids": []string{},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/groups", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_Create_NotRegionMember(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpcreate_notmember", models.TierVouched, false)
	user.VouchVerified = true
	region := suite.createTestRegion("GrpTestRegion4", models.RegionTypeCity, nil)
	// Deliberately NOT adding user to region

	defer suite.cleanup([]string{user.ID}, []string{region.ID}, nil)

	claims := suite.claimsForUser(user)
	body := models.CreateGroupRequest{
		Name:       "Test Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/groups", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Create(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Get Tests ---

func TestGroupHandler_Get_MemberSuccess(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpget_member", models.TierVouched, false)
	user.VouchVerified = true
	region := suite.createTestRegion("GrpGetRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, false)

	// Create a group via repo
	createReq := &models.CreateGroupRequest{
		Name:       "Get Test Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, user.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{user.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(user)

	req := httptest.NewRequest("GET", "/groups/"+group.ID, nil)
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result models.GroupWithDetails
	_ = json.NewDecoder(rec.Body).Decode(&result)
	if result.Name != "Get Test Group" {
		t.Errorf("Expected name 'Get Test Group', got '%s'", result.Name)
	}
	if !result.IsUserMember {
		t.Error("Expected IsUserMember to be true")
	}
}

func TestGroupHandler_Get_UnlistedNonMember(t *testing.T) {
	suite := setupGroupTestSuite(t)

	creator := suite.createTestUser("grpget_creator", models.TierVouched, false)
	creator.VouchVerified = true
	outsider := suite.createTestUser("grpget_outsider", models.TierVouched, false)
	outsider.VouchVerified = true
	region := suite.createTestRegion("GrpGetRegion2", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, creator.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Secret Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, creator.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{creator.ID, outsider.ID}, []string{region.ID}, []string{group.ID})

	// Outsider tries to view unlisted group
	claims := suite.claimsForUser(outsider)

	req := httptest.NewRequest("GET", "/groups/"+group.ID, nil)
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Get(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for unlisted group non-member, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_Get_ListedActiveNonMember(t *testing.T) {
	suite := setupGroupTestSuite(t)

	creator := suite.createTestUser("grpget_listed_creator", models.TierVouched, false)
	creator.VouchVerified = true
	viewer := suite.createTestUser("grpget_listed_viewer", models.TierVouched, false)
	viewer.VouchVerified = true
	region := suite.createTestRegion("GrpGetRegion3", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, creator.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Public Group",
		Visibility: "listed",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, creator.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Force status to active and visibility to listed for this test
	_, _ = suite.db.ExecContext(ctx, "UPDATE `groups` SET status = 'active', visibility = 'listed' WHERE id = ?", group.ID)

	defer suite.cleanup([]string{creator.ID, viewer.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(viewer)

	req := httptest.NewRequest("GET", "/groups/"+group.ID, nil)
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for listed active group, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Update Tests ---

func TestGroupHandler_Update_AdminSuccess(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpupd_admin", models.TierVouched, false)
	user.VouchVerified = true
	region := suite.createTestRegion("GrpUpdRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Update Me",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, user.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{user.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(user)
	newName := "Updated Group Name"
	updateBody := models.UpdateGroupRequest{
		Name: &newName,
	}
	bodyBytes, _ := json.Marshal(updateBody)

	req := httptest.NewRequest("PUT", "/groups/"+group.ID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_Update_NonAdmin(t *testing.T) {
	suite := setupGroupTestSuite(t)

	creator := suite.createTestUser("grpupd_creator", models.TierVouched, false)
	creator.VouchVerified = true
	outsider := suite.createTestUser("grpupd_outsider", models.TierVouched, false)
	outsider.VouchVerified = true
	region := suite.createTestRegion("GrpUpdRegion2", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, creator.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "No Touch",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, creator.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{creator.ID, outsider.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(outsider)
	newName := "Hacked"
	updateBody := models.UpdateGroupRequest{Name: &newName}
	bodyBytes, _ := json.Marshal(updateBody)

	req := httptest.NewRequest("PUT", "/groups/"+group.ID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Update(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_Update_ProvisionalCannotBeListed(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpupd_prov", models.TierVouched, false)
	user.VouchVerified = true
	region := suite.createTestRegion("GrpUpdRegion3", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Still Provisional",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, user.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{user.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(user)
	listed := "listed"
	updateBody := models.UpdateGroupRequest{Visibility: &listed}
	bodyBytes, _ := json.Marshal(updateBody)

	req := httptest.NewRequest("PUT", "/groups/"+group.ID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Delete Tests ---

func TestGroupHandler_Delete_SuperuserSuccess(t *testing.T) {
	suite := setupGroupTestSuite(t)

	superuser := suite.createTestUser("grpdel_super", models.TierPostcard, true)
	creator := suite.createTestUser("grpdel_creator", models.TierVouched, false)
	creator.VouchVerified = true
	region := suite.createTestRegion("GrpDelRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, creator.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Delete Me",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, creator.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{superuser.ID, creator.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(superuser)

	req := httptest.NewRequest("DELETE", "/groups/"+group.ID, nil)
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Delete(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_Delete_NonSuperuser(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpdel_regular", models.TierVouched, false)
	user.VouchVerified = true
	region := suite.createTestRegion("GrpDelRegion2", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Cant Delete",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, user.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{user.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(user)

	req := httptest.NewRequest("DELETE", "/groups/"+group.ID, nil)
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Delete(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- ListMembers Tests ---

func TestGroupHandler_ListMembers_MemberSuccess(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpmem_member", models.TierVouched, false)
	user.VouchVerified = true
	region := suite.createTestRegion("GrpMemRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Members Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, user.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{user.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(user)

	req := httptest.NewRequest("GET", "/groups/"+group.ID+"/members", nil)
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.ListMembers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string][]models.GroupMemberWithUser
	_ = json.NewDecoder(rec.Body).Decode(&result)
	if len(result["members"]) != 1 {
		t.Errorf("Expected 1 member, got %d", len(result["members"]))
	}
}

func TestGroupHandler_ListMembers_NonMember(t *testing.T) {
	suite := setupGroupTestSuite(t)

	creator := suite.createTestUser("grpmem_creator", models.TierVouched, false)
	creator.VouchVerified = true
	outsider := suite.createTestUser("grpmem_outsider", models.TierVouched, false)
	outsider.VouchVerified = true
	region := suite.createTestRegion("GrpMemRegion2", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, creator.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Private Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, creator.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{creator.ID, outsider.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(outsider)

	req := httptest.NewRequest("GET", "/groups/"+group.ID+"/members", nil)
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.ListMembers(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Leave Tests ---

func TestGroupHandler_Leave_Success(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpleave_admin", models.TierVouched, false)
	admin.VouchVerified = true
	member := suite.createTestUser("grpleave_member", models.TierVouched, false)
	member.VouchVerified = true
	region := suite.createTestRegion("GrpLeaveRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Leave Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Add second member
	err = suite.groupRepo.AddMember(ctx, group.ID, member.ID, false, false)
	if err != nil {
		t.Fatalf("Failed to add member: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, member.ID}, []string{region.ID}, []string{group.ID})

	// Member leaves
	claims := suite.claimsForUser(member)

	req := httptest.NewRequest("POST", "/groups/"+group.ID+"/leave", nil)
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Leave(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify member is gone
	isMember, _ := suite.groupRepo.IsUserMember(context.Background(), group.ID, member.ID)
	if isMember {
		t.Error("Expected member to no longer be in group")
	}
}

func TestGroupHandler_Leave_LastAdmin(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpleave_lastadmin", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("GrpLeaveRegion2", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Solo Admin Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(admin)

	req := httptest.NewRequest("POST", "/groups/"+group.ID+"/leave", nil)
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Leave(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "last_admin" {
		t.Errorf("Expected error 'last_admin', got '%s'", body["error"])
	}
}

func TestGroupHandler_Leave_NotMember(t *testing.T) {
	suite := setupGroupTestSuite(t)

	creator := suite.createTestUser("grpleave_creator2", models.TierVouched, false)
	creator.VouchVerified = true
	outsider := suite.createTestUser("grpleave_outsider", models.TierVouched, false)
	outsider.VouchVerified = true
	region := suite.createTestRegion("GrpLeaveRegion3", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, creator.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Not Your Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, creator.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{creator.ID, outsider.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(outsider)

	req := httptest.NewRequest("POST", "/groups/"+group.ID+"/leave", nil)
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Leave(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- List Tests ---

func TestGroupHandler_List_Success(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grplist_user", models.TierVouched, false)
	user.VouchVerified = true
	region := suite.createTestRegion("GrpListRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "My Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, user.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{user.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(user)

	req := httptest.NewRequest("GET", "/groups", nil)
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string][]models.GroupWithDetails
	_ = json.NewDecoder(rec.Body).Decode(&result)
	if len(result["groups"]) < 1 {
		t.Error("Expected at least 1 group in list")
	}
}
