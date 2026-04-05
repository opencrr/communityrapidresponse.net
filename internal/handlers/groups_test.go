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
	t               *testing.T
	db              *database.DB
	groupRepo       *database.GroupRepository
	signalGroupRepo *database.SignalGroupRepository
	regionRepo      *database.RegionRepository
	userRepo        *database.UserRepository
	auditRepo       *database.AuditRepository
	handler         *GroupHandler
	jwtAuth         *middleware.JWTAuth
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
	signalGroupRepo := database.NewSignalGroupRepository(db)
	regionRepo := database.NewRegionRepository(db)
	userRepo := database.NewUserRepository(db)
	auditRepo := database.NewAuditRepository(db)
	handler := NewGroupHandler(groupRepo, signalGroupRepo, regionRepo, userRepo, auditRepo)

	jwtConfig := &config.JWTConfig{
		Secret:          "test_secret_key_at_least_32_characters_long",
		ExpirationHours: 24,
		Issuer:          "test_issuer",
	}
	jwtAuth := middleware.NewJWTAuth(jwtConfig)

	suite := &GroupTestSuite{
		t:               t,
		db:              db,
		groupRepo:       groupRepo,
		signalGroupRepo: signalGroupRepo,
		regionRepo:      regionRepo,
		userRepo:        userRepo,
		auditRepo:       auditRepo,
		handler:         handler,
		jwtAuth:         jwtAuth,
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
		_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE owner_group_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_trust_vouches WHERE group_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_invite_links WHERE group_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_invitations WHERE group_id = ?", id)
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
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_invitations WHERE user_id = ?", id)
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

// --- CreateInviteLink Tests ---

func TestGroupHandler_CreateInviteLink_AdminSuccess(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpinvlink_admin", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("GrpInvLinkRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Invite Link Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(admin)

	req := httptest.NewRequest("POST", "/groups/"+group.ID+"/invite-links", nil)
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.CreateInviteLink(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var link models.GroupInviteLink
	_ = json.NewDecoder(rec.Body).Decode(&link)
	if link.Token == "" {
		t.Error("Expected non-empty token in response")
	}
	if link.GroupID != group.ID {
		t.Errorf("Expected group_id %s, got %s", group.ID, link.GroupID)
	}
}

func TestGroupHandler_CreateInviteLink_NonAdmin(t *testing.T) {
	suite := setupGroupTestSuite(t)

	creator := suite.createTestUser("grpinvlink_creator", models.TierVouched, false)
	creator.VouchVerified = true
	outsider := suite.createTestUser("grpinvlink_outsider", models.TierVouched, false)
	outsider.VouchVerified = true
	region := suite.createTestRegion("GrpInvLinkRegion2", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, creator.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "No Invite For You",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, creator.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{creator.ID, outsider.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(outsider)

	req := httptest.NewRequest("POST", "/groups/"+group.ID+"/invite-links", nil)
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.CreateInviteLink(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- JoinViaLink Tests ---

func TestGroupHandler_JoinViaLink_Success(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpjoin_admin", models.TierVouched, false)
	admin.VouchVerified = true
	joiner := suite.createTestUser("grpjoin_joiner", models.TierVouched, false)
	joiner.VouchVerified = true
	region := suite.createTestRegion("GrpJoinRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)
	_ = suite.regionRepo.AddUserToRegion(ctx, joiner.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Join Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	linkReq := &models.CreateInviteLinkRequest{}
	link, err := suite.groupRepo.CreateInviteLink(ctx, group.ID, admin.ID, linkReq)
	if err != nil {
		t.Fatalf("Failed to create invite link: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, joiner.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(joiner)

	req := httptest.NewRequest("POST", "/groups/join/"+link.Token, nil)
	q := req.URL.Query()
	q.Set("token", link.Token)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.JoinViaLink(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify membership
	isMember, _ := suite.groupRepo.IsUserMember(context.Background(), group.ID, joiner.ID)
	if !isMember {
		t.Error("Expected joiner to be a member of the group")
	}
}

func TestGroupHandler_JoinViaLink_UnverifiedUser(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpjoin_admin2", models.TierVouched, false)
	admin.VouchVerified = true
	unverified := suite.createTestUser("grpjoin_unverified", models.TierUnverified, false)
	region := suite.createTestRegion("GrpJoinRegion2", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "No Unverified",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	linkReq := &models.CreateInviteLinkRequest{}
	link, err := suite.groupRepo.CreateInviteLink(ctx, group.ID, admin.ID, linkReq)
	if err != nil {
		t.Fatalf("Failed to create invite link: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, unverified.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(unverified)

	req := httptest.NewRequest("POST", "/groups/join/"+link.Token, nil)
	q := req.URL.Query()
	q.Set("token", link.Token)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.JoinViaLink(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_JoinViaLink_AlreadyMember(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpjoin_admin3", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("GrpJoinRegion3", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Already In",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	linkReq := &models.CreateInviteLinkRequest{}
	link, err := suite.groupRepo.CreateInviteLink(ctx, group.ID, admin.ID, linkReq)
	if err != nil {
		t.Fatalf("Failed to create invite link: %v", err)
	}

	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{group.ID})

	// Admin is already a member, tries to join via link
	claims := suite.claimsForUser(admin)

	req := httptest.NewRequest("POST", "/groups/join/"+link.Token, nil)
	q := req.URL.Query()
	q.Set("token", link.Token)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.JoinViaLink(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("Expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_JoinViaLink_GraduationTrigger(t *testing.T) {
	suite := setupGroupTestSuite(t)

	// Set a low threshold so graduation triggers
	ctx := context.Background()
	_, _ = suite.db.ExecContext(ctx, "INSERT INTO platform_config (config_key, config_value) VALUES ('group_founding_threshold', '3') ON DUPLICATE KEY UPDATE config_value = '3'")

	admin := suite.createTestUser("grpjoin_grad_admin", models.TierVouched, false)
	admin.VouchVerified = true
	member1 := suite.createTestUser("grpjoin_grad_m1", models.TierVouched, false)
	member1.VouchVerified = true
	member2 := suite.createTestUser("grpjoin_grad_m2", models.TierVouched, false)
	member2.VouchVerified = true
	region := suite.createTestRegion("GrpJoinGradRegion", models.RegionTypeCity, nil)

	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)
	_ = suite.regionRepo.AddUserToRegion(ctx, member1.ID, region.ID, false)
	_ = suite.regionRepo.AddUserToRegion(ctx, member2.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Graduation Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Add member1 directly (now 2 members: admin + member1)
	err = suite.groupRepo.AddMember(ctx, group.ID, member1.ID, false, true)
	if err != nil {
		t.Fatalf("Failed to add member1: %v", err)
	}

	linkReq := &models.CreateInviteLinkRequest{}
	link, err := suite.groupRepo.CreateInviteLink(ctx, group.ID, admin.ID, linkReq)
	if err != nil {
		t.Fatalf("Failed to create invite link: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, member1.ID, member2.ID}, []string{region.ID}, []string{group.ID})

	// member2 joins via link (3rd member = threshold)
	claims := suite.claimsForUser(member2)

	req := httptest.NewRequest("POST", "/groups/join/"+link.Token, nil)
	q := req.URL.Query()
	q.Set("token", link.Token)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.JoinViaLink(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&result)
	graduated, ok := result["graduated"].(bool)
	if !ok || !graduated {
		t.Errorf("Expected graduated=true, got %v", result["graduated"])
	}

	// Verify group is now active
	updatedGroup, _ := suite.groupRepo.GetByID(context.Background(), group.ID)
	if updatedGroup.Status != models.GroupStatusActive {
		t.Errorf("Expected group status 'active', got '%s'", updatedGroup.Status)
	}
}

// --- CreateInvitation Tests ---

func TestGroupHandler_CreateInvitation_AdminSuccess(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpinv_admin", models.TierVouched, false)
	admin.VouchVerified = true
	target := suite.createTestUser("grpinv_target", models.TierVouched, false)
	target.VouchVerified = true
	region := suite.createTestRegion("GrpInvRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Invitation Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, target.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(admin)
	invBody := models.CreateGroupInvitationRequest{UserID: target.ID}
	bodyBytes, _ := json.Marshal(invBody)

	req := httptest.NewRequest("POST", "/groups/"+group.ID+"/invitations", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.CreateInvitation(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var invitation models.GroupInvitation
	_ = json.NewDecoder(rec.Body).Decode(&invitation)
	if invitation.UserID != target.ID {
		t.Errorf("Expected user_id %s, got %s", target.ID, invitation.UserID)
	}
}

func TestGroupHandler_CreateInvitation_NonAdmin(t *testing.T) {
	suite := setupGroupTestSuite(t)

	creator := suite.createTestUser("grpinv_creator2", models.TierVouched, false)
	creator.VouchVerified = true
	outsider := suite.createTestUser("grpinv_outsider", models.TierVouched, false)
	outsider.VouchVerified = true
	target := suite.createTestUser("grpinv_target2", models.TierVouched, false)
	target.VouchVerified = true
	region := suite.createTestRegion("GrpInvRegion2", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, creator.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "No Invite For Outsider",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, creator.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{creator.ID, outsider.ID, target.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(outsider)
	invBody := models.CreateGroupInvitationRequest{UserID: target.ID}
	bodyBytes, _ := json.Marshal(invBody)

	req := httptest.NewRequest("POST", "/groups/"+group.ID+"/invitations", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.CreateInvitation(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_CreateInvitation_AlreadyMember(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpinv_admin3", models.TierVouched, false)
	admin.VouchVerified = true
	existingMember := suite.createTestUser("grpinv_existing", models.TierVouched, false)
	existingMember.VouchVerified = true
	region := suite.createTestRegion("GrpInvRegion3", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Already Member Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Add existing member
	err = suite.groupRepo.AddMember(ctx, group.ID, existingMember.ID, false, false)
	if err != nil {
		t.Fatalf("Failed to add member: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, existingMember.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(admin)
	invBody := models.CreateGroupInvitationRequest{UserID: existingMember.ID}
	bodyBytes, _ := json.Marshal(invBody)

	req := httptest.NewRequest("POST", "/groups/"+group.ID+"/invitations", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Set("id", group.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.CreateInvitation(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("Expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- RespondToInvitation Tests ---

func TestGroupHandler_RespondToInvitation_Accept(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpresp_admin", models.TierVouched, false)
	admin.VouchVerified = true
	invitee := suite.createTestUser("grpresp_invitee", models.TierVouched, false)
	invitee.VouchVerified = true
	region := suite.createTestRegion("GrpRespRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Accept Invite Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	invitation, err := suite.groupRepo.CreateInvitation(ctx, group.ID, invitee.ID, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create invitation: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, invitee.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(invitee)
	respondBody := models.RespondToGroupInvitationRequest{Accept: true}
	bodyBytes, _ := json.Marshal(respondBody)

	req := httptest.NewRequest("POST", "/groups/invitations/"+invitation.ID+"/respond", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Set("id", invitation.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.RespondToInvitation(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify membership
	isMember, _ := suite.groupRepo.IsUserMember(context.Background(), group.ID, invitee.ID)
	if !isMember {
		t.Error("Expected invitee to be a member of the group after accepting")
	}
}

func TestGroupHandler_RespondToInvitation_Decline(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpresp_admin2", models.TierVouched, false)
	admin.VouchVerified = true
	invitee := suite.createTestUser("grpresp_invitee2", models.TierVouched, false)
	invitee.VouchVerified = true
	region := suite.createTestRegion("GrpRespRegion2", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Decline Invite Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	invitation, err := suite.groupRepo.CreateInvitation(ctx, group.ID, invitee.ID, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create invitation: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, invitee.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(invitee)
	respondBody := models.RespondToGroupInvitationRequest{Accept: false}
	bodyBytes, _ := json.Marshal(respondBody)

	req := httptest.NewRequest("POST", "/groups/invitations/"+invitation.ID+"/respond", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Set("id", invitation.ID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.RespondToInvitation(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify NOT a member
	isMember, _ := suite.groupRepo.IsUserMember(context.Background(), group.ID, invitee.ID)
	if isMember {
		t.Error("Expected invitee to NOT be a member after declining")
	}
}

// --- ListMyInvitations Tests ---

func TestGroupHandler_ListMyInvitations_ReturnsPending(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grplistinv_admin", models.TierVouched, false)
	admin.VouchVerified = true
	invitee := suite.createTestUser("grplistinv_invitee", models.TierVouched, false)
	invitee.VouchVerified = true
	region := suite.createTestRegion("GrpListInvRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "List Invitations Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	_, err = suite.groupRepo.CreateInvitation(ctx, group.ID, invitee.ID, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create invitation: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, invitee.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(invitee)

	req := httptest.NewRequest("GET", "/groups/invitations", nil)
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.ListMyInvitations(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string][]models.GroupInvitationWithDetails
	_ = json.NewDecoder(rec.Body).Decode(&result)
	if len(result["invitations"]) != 1 {
		t.Errorf("Expected 1 invitation, got %d", len(result["invitations"]))
	}
	if result["invitations"][0].GroupName != "List Invitations Group" {
		t.Errorf("Expected group name 'List Invitations Group', got '%s'", result["invitations"][0].GroupName)
	}
}

// --- Trust Vouch Tests ---

func TestGroupHandler_VouchForMember_Success(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpvouch_admin", models.TierVouched, false)
	admin.VouchVerified = true
	member := suite.createTestUser("grpvouch_member", models.TierVouched, false)
	member.VouchVerified = true
	region := suite.createTestRegion("GrpVouchRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)
	_ = suite.regionRepo.AddUserToRegion(ctx, member.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Vouch Test Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Add member to the group
	err = suite.groupRepo.AddMember(ctx, group.ID, member.ID, false, false)
	if err != nil {
		t.Fatalf("Failed to add member: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, member.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(admin)
	body := models.CreateTrustVouchRequest{UserID: member.ID}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/groups/"+group.ID+"/trust-vouches?id="+group.ID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.VouchForMember(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&result)
	vouchCount, ok := result["vouch_count"].(float64)
	if !ok || vouchCount != 1 {
		t.Errorf("Expected vouch_count 1, got %v", result["vouch_count"])
	}
}

func TestGroupHandler_VouchForMember_SelfVouch(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpvouch_selfadmin", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("GrpVouchRegion2", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Self Vouch Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(admin)
	body := models.CreateTrustVouchRequest{UserID: admin.ID}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/groups/"+group.ID+"/trust-vouches?id="+group.ID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.VouchForMember(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_VouchForMember_NotTrustedOrAdmin(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpvouch_origadmin", models.TierVouched, false)
	admin.VouchVerified = true
	regularMember := suite.createTestUser("grpvouch_regular", models.TierVouched, false)
	regularMember.VouchVerified = true
	targetMember := suite.createTestUser("grpvouch_target", models.TierVouched, false)
	targetMember.VouchVerified = true
	region := suite.createTestRegion("GrpVouchRegion3", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)
	_ = suite.regionRepo.AddUserToRegion(ctx, regularMember.ID, region.ID, false)
	_ = suite.regionRepo.AddUserToRegion(ctx, targetMember.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "NotTrusted Vouch Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Add both as regular (non-admin) members
	_ = suite.groupRepo.AddMember(ctx, group.ID, regularMember.ID, false, false)
	_ = suite.groupRepo.AddMember(ctx, group.ID, targetMember.ID, false, false)

	defer suite.cleanup([]string{admin.ID, regularMember.ID, targetMember.ID}, []string{region.ID}, []string{group.ID})

	// Regular (untrusted) member tries to vouch — should be 403
	claims := suite.claimsForUser(regularMember)
	body := models.CreateTrustVouchRequest{UserID: targetMember.ID}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/groups/"+group.ID+"/trust-vouches?id="+group.ID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.VouchForMember(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_GetTrustVouchStatus_Success(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpvouchst_admin", models.TierVouched, false)
	admin.VouchVerified = true
	member := suite.createTestUser("grpvouchst_member", models.TierVouched, false)
	member.VouchVerified = true
	region := suite.createTestRegion("GrpVouchStRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)
	_ = suite.regionRepo.AddUserToRegion(ctx, member.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Vouch Status Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	_ = suite.groupRepo.AddMember(ctx, group.ID, member.ID, false, false)

	// Admin vouches for member
	err = suite.groupRepo.CreateTrustVouch(ctx, group.ID, admin.ID, member.ID)
	if err != nil {
		t.Fatalf("Failed to create trust vouch: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, member.ID}, []string{region.ID}, []string{group.ID})

	// Admin queries vouch status for member
	claims := suite.claimsForUser(admin)

	req := httptest.NewRequest("GET", "/groups/"+group.ID+"/trust-vouches/"+member.ID+"?id="+group.ID+"&user_id="+member.ID, nil)
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.GetTrustVouchStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&result)

	vouchCount, ok := result["vouch_count"].(float64)
	if !ok || vouchCount != 1 {
		t.Errorf("Expected vouch_count 1, got %v", result["vouch_count"])
	}

	trustLevel, ok := result["trust_level"].(string)
	if !ok {
		t.Errorf("Expected trust_level string, got %v", result["trust_level"])
	}
	// With default threshold of 2 and only 1 vouch, trust_level should still be "member"
	if trustLevel != "member" {
		t.Errorf("Expected trust_level 'member', got '%s'", trustLevel)
	}

	threshold, ok := result["threshold"].(float64)
	if !ok || threshold < 1 {
		t.Errorf("Expected threshold >= 1, got %v", result["threshold"])
	}
}

func TestGroupHandler_GetTrustVouchStatus_NonMember(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpvouchst_admin2", models.TierVouched, false)
	admin.VouchVerified = true
	nonMember := suite.createTestUser("grpvouchst_nonmem", models.TierVouched, false)
	nonMember.VouchVerified = true
	region := suite.createTestRegion("GrpVouchStRegion2", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Vouch Status NonMember Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, nonMember.ID}, []string{region.ID}, []string{group.ID})

	// Non-member tries to get vouch status — should be 403
	claims := suite.claimsForUser(nonMember)

	req := httptest.NewRequest("GET", "/groups/"+group.ID+"/trust-vouches/"+admin.ID+"?id="+group.ID+"&user_id="+admin.ID, nil)
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.GetTrustVouchStatus(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Signal Group Tests ---

// createActiveGroupForSignalTests creates a group, makes it active, and adds the user as admin.
func (s *GroupTestSuite) createActiveGroupForSignalTests(user *models.User, region *models.GeographicRegion) string {
	ctx := context.Background()

	req := &models.CreateGroupRequest{
		Name:       "SG Test Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := s.groupRepo.Create(ctx, req, user.ID)
	if err != nil {
		s.t.Fatalf("Failed to create group: %v", err)
	}

	// Force-graduate the group to active status
	_, err = s.db.ExecContext(ctx, "UPDATE `groups` SET status = 'active', graduated_at = NOW() WHERE id = ?", group.ID)
	if err != nil {
		s.t.Fatalf("Failed to activate group: %v", err)
	}

	return group.ID
}

func TestGroupHandler_CreateSignalGroup_Success(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpsg_create_ok", models.TierVouched, false)
	user.VouchVerified = true
	region := suite.createTestRegion("GrpSGRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, true)

	groupID := suite.createActiveGroupForSignalTests(user, region)
	defer suite.cleanup([]string{user.ID}, []string{region.ID}, []string{groupID})

	claims := suite.claimsForUser(user)
	body := models.CreateGroupSignalGroupRequest{
		GroupName:  "Test Signal Chat",
		AccessTier: "member",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/groups/"+groupID+"/signal-groups", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Set("id", groupID)
	req.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.CreateSignalGroup(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&result)
	if result["id"] == nil || result["id"] == "" {
		t.Error("Expected signal group ID in response")
	}
	if result["group_name"] != "Test Signal Chat" {
		t.Errorf("Expected group_name 'Test Signal Chat', got '%v'", result["group_name"])
	}
	if result["access_tier"] != "member" {
		t.Errorf("Expected access_tier 'member', got '%v'", result["access_tier"])
	}
}

func TestGroupHandler_CreateSignalGroup_ProvisionalGroup(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpsg_provisional", models.TierVouched, false)
	user.VouchVerified = true
	region := suite.createTestRegion("GrpSGRegion2", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, true)

	// Create a group but do NOT graduate it (stays provisional)
	req := &models.CreateGroupRequest{
		Name:       "Provisional SG Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, req, user.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}
	defer suite.cleanup([]string{user.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(user)
	body := models.CreateGroupSignalGroupRequest{
		GroupName:  "Should Fail",
		AccessTier: "member",
	}
	bodyBytes, _ := json.Marshal(body)

	httpReq := httptest.NewRequest("POST", "/api/v1/groups/"+group.ID+"/signal-groups", bytes.NewReader(bodyBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	q := httpReq.URL.Query()
	q.Set("id", group.ID)
	httpReq.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(httpReq.Context(), claims)
	httpReq = httpReq.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.CreateSignalGroup(rec, httpReq)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for provisional group, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_CreateSignalGroup_NonAdmin(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpsg_admin", models.TierVouched, false)
	admin.VouchVerified = true
	nonAdmin := suite.createTestUser("grpsg_nonadmin", models.TierVouched, false)
	nonAdmin.VouchVerified = true
	region := suite.createTestRegion("GrpSGRegion3", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, true)
	_ = suite.regionRepo.AddUserToRegion(ctx, nonAdmin.ID, region.ID, false)

	groupID := suite.createActiveGroupForSignalTests(admin, region)

	// Add non-admin as regular member
	_ = suite.groupRepo.AddMember(ctx, groupID, nonAdmin.ID, false, false)
	defer suite.cleanup([]string{admin.ID, nonAdmin.ID}, []string{region.ID}, []string{groupID})

	claims := suite.claimsForUser(nonAdmin)
	body := models.CreateGroupSignalGroupRequest{
		GroupName:  "Should Fail",
		AccessTier: "member",
	}
	bodyBytes, _ := json.Marshal(body)

	httpReq := httptest.NewRequest("POST", "/api/v1/groups/"+groupID+"/signal-groups", bytes.NewReader(bodyBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	q := httpReq.URL.Query()
	q.Set("id", groupID)
	httpReq.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(httpReq.Context(), claims)
	httpReq = httpReq.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.CreateSignalGroup(rec, httpReq)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 for non-admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_CreateSignalGroup_LimitReached(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpsg_limit", models.TierVouched, false)
	user.VouchVerified = true
	region := suite.createTestRegion("GrpSGRegion4", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, true)

	groupID := suite.createActiveGroupForSignalTests(user, region)
	defer suite.cleanup([]string{user.ID}, []string{region.ID}, []string{groupID})

	// Create 5 signal groups to reach the limit
	for i := 0; i < 5; i++ {
		sg := &models.SignalGroup{
			OwnerGroupID: &groupID,
			GroupName:    "SG " + strconv.Itoa(i),
			AccessTier:   models.AccessTierMember,
			CreatedBy:    &user.ID,
		}
		if err := suite.signalGroupRepo.CreateForOwnerGroup(ctx, sg); err != nil {
			t.Fatalf("Failed to create signal group %d: %v", i, err)
		}
	}

	claims := suite.claimsForUser(user)
	body := models.CreateGroupSignalGroupRequest{
		GroupName:  "One Too Many",
		AccessTier: "member",
	}
	bodyBytes, _ := json.Marshal(body)

	httpReq := httptest.NewRequest("POST", "/api/v1/groups/"+groupID+"/signal-groups", bytes.NewReader(bodyBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	q := httpReq.URL.Query()
	q.Set("id", groupID)
	httpReq.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(httpReq.Context(), claims)
	httpReq = httpReq.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.CreateSignalGroup(rec, httpReq)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for limit reached, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_ListSignalGroups_FilteredByAccessTier(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpsg_list_admin", models.TierVouched, false)
	admin.VouchVerified = true
	member := suite.createTestUser("grpsg_list_member", models.TierVouched, false)
	member.VouchVerified = true
	region := suite.createTestRegion("GrpSGRegion5", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, true)
	_ = suite.regionRepo.AddUserToRegion(ctx, member.ID, region.ID, false)

	groupID := suite.createActiveGroupForSignalTests(admin, region)

	// Add member to group
	_ = suite.groupRepo.AddMember(ctx, groupID, member.ID, false, false)
	defer suite.cleanup([]string{admin.ID, member.ID}, []string{region.ID}, []string{groupID})

	// Create signal groups at different tiers
	tiers := []models.AccessTier{models.AccessTierOpen, models.AccessTierMember, models.AccessTierAdminOnly}
	for i, tier := range tiers {
		sg := &models.SignalGroup{
			OwnerGroupID: &groupID,
			GroupName:    "SG Tier " + strconv.Itoa(i),
			AccessTier:   tier,
			CreatedBy:    &admin.ID,
		}
		if err := suite.signalGroupRepo.CreateForOwnerGroup(ctx, sg); err != nil {
			t.Fatalf("Failed to create signal group: %v", err)
		}
	}

	// Member should see open + member tiers (2 of 3), but NOT admin_only
	claims := suite.claimsForUser(member)
	httpReq := httptest.NewRequest("GET", "/api/v1/groups/"+groupID+"/signal-groups", nil)
	q := httpReq.URL.Query()
	q.Set("id", groupID)
	httpReq.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(httpReq.Context(), claims)
	httpReq = httpReq.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.ListSignalGroups(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&result)
	signalGroups, ok := result["signal_groups"].([]interface{})
	if !ok {
		t.Fatal("Expected signal_groups array in response")
	}
	if len(signalGroups) != 2 {
		t.Errorf("Expected member to see 2 signal groups (open + member), got %d", len(signalGroups))
	}
}

func TestGroupHandler_ListSignalGroups_AdminSeesAll(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpsg_list_alladmin", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("GrpSGRegion6", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, true)

	groupID := suite.createActiveGroupForSignalTests(admin, region)
	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{groupID})

	// Create signal groups at every tier
	tiers := []models.AccessTier{models.AccessTierOpen, models.AccessTierMember, models.AccessTierTrusted, models.AccessTierAdminOnly}
	for i, tier := range tiers {
		sg := &models.SignalGroup{
			OwnerGroupID: &groupID,
			GroupName:    "SG All " + strconv.Itoa(i),
			AccessTier:   tier,
			CreatedBy:    &admin.ID,
		}
		if err := suite.signalGroupRepo.CreateForOwnerGroup(ctx, sg); err != nil {
			t.Fatalf("Failed to create signal group: %v", err)
		}
	}

	claims := suite.claimsForUser(admin)
	httpReq := httptest.NewRequest("GET", "/api/v1/groups/"+groupID+"/signal-groups", nil)
	q := httpReq.URL.Query()
	q.Set("id", groupID)
	httpReq.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(httpReq.Context(), claims)
	httpReq = httpReq.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.ListSignalGroups(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&result)
	signalGroups, ok := result["signal_groups"].([]interface{})
	if !ok {
		t.Fatal("Expected signal_groups array in response")
	}
	if len(signalGroups) != 4 {
		t.Errorf("Expected admin to see all 4 signal groups, got %d", len(signalGroups))
	}
}

func TestGroupHandler_ListSignalGroups_NonMemberSeesOnlyOpen(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpsg_list_nmmadmin", models.TierVouched, false)
	admin.VouchVerified = true
	outsider := suite.createTestUser("grpsg_list_outsider", models.TierVouched, false)
	outsider.VouchVerified = true
	region := suite.createTestRegion("GrpSGRegion7", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, true)

	groupID := suite.createActiveGroupForSignalTests(admin, region)
	defer suite.cleanup([]string{admin.ID, outsider.ID}, []string{region.ID}, []string{groupID})

	// Create signal groups at different tiers
	tiers := []models.AccessTier{models.AccessTierOpen, models.AccessTierMember, models.AccessTierAdminOnly}
	for i, tier := range tiers {
		sg := &models.SignalGroup{
			OwnerGroupID: &groupID,
			GroupName:    "SG NM " + strconv.Itoa(i),
			AccessTier:   tier,
			CreatedBy:    &admin.ID,
		}
		if err := suite.signalGroupRepo.CreateForOwnerGroup(ctx, sg); err != nil {
			t.Fatalf("Failed to create signal group: %v", err)
		}
	}

	// Outsider (not a member of the group) should only see open tier
	claims := suite.claimsForUser(outsider)
	httpReq := httptest.NewRequest("GET", "/api/v1/groups/"+groupID+"/signal-groups", nil)
	q := httpReq.URL.Query()
	q.Set("id", groupID)
	httpReq.URL.RawQuery = q.Encode()
	ctx = middleware.ContextWithUser(httpReq.Context(), claims)
	httpReq = httpReq.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.ListSignalGroups(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&result)
	signalGroups, ok := result["signal_groups"].([]interface{})
	if !ok {
		t.Fatal("Expected signal_groups array in response")
	}
	if len(signalGroups) != 1 {
		t.Errorf("Expected non-member to see only 1 signal group (open), got %d", len(signalGroups))
	}
}
