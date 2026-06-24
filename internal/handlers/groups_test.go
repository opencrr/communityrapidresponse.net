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
	"github.com/opencrr/communityrapidresponse.net/internal/services"
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

	groupRepo := database.NewGroupRepository(db, nil)
	signalGroupRepo := database.NewSignalGroupRepository(db)
	regionRepo := database.NewRegionRepository(db)
	userRepo := database.NewUserRepository(db)
	auditRepo := database.NewAuditRepository(db)
	meshtasticChannelRepo := database.NewMeshtasticChannelRepository(db)
	handler := NewGroupHandler(groupRepo, signalGroupRepo, meshtasticChannelRepo, regionRepo, userRepo, auditRepo)

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

func (s *GroupTestSuite) createPostcardVerifiedUser(username string) *models.User {
	ctx := context.Background()
	user := &models.User{
		Username:         username,
		Email:            username + "@test.com",
		PasswordHash:     "hashedpassword",
		VerificationTier: models.TierPostcard,
		PostcardVerified: true,
	}
	err := s.userRepo.Create(ctx, user)
	if err != nil {
		s.t.Fatalf("Failed to create postcard-verified test user: %v", err)
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
		_, _ = s.db.ExecContext(ctx, "DELETE FROM topic_board_postings WHERE group_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_blocks WHERE blocker_group_id = ? OR blocked_group_id = ?", id, id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_resources WHERE group_id = ?", id)
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

	user := suite.createPostcardVerifiedUser("grpcreate_ok")
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

	user := suite.createPostcardVerifiedUser("grpcreate_noname")
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

	user := suite.createPostcardVerifiedUser("grpcreate_noregion")

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

	user := suite.createPostcardVerifiedUser("grpcreate_notmember")
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

func TestGroupHandler_JoinViaLink_UnverifiedUserCanJoin(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpjoin_admin2", models.TierVouched, false)
	admin.VouchVerified = true
	unverified := suite.createTestUser("grpjoin_unverified", models.TierUnverified, false)
	region := suite.createTestRegion("GrpJoinRegion2", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Open To All",
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

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify membership
	isMember, _ := suite.groupRepo.IsUserMember(context.Background(), group.ID, unverified.ID)
	if !isMember {
		t.Error("Expected unverified user to be a member after joining via invite link")
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

	// Listed (discoverable) group: open-tier content is intentionally visible to
	// non-members. Unlisted groups are fully hidden from non-members — that case
	// is covered by TestGroupHandler_ListSignalGroups_UnlistedHiddenFromNonMember.
	listedGroup, err := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name:       "SG NM Listed Group",
		Visibility: "listed",
		RegionIDs:  []string{region.ID},
	}, admin.ID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}
	// Provisional groups are forced unlisted at creation; visibility is applied on
	// graduation. Set both here to simulate a graduated, listed group.
	if _, err := suite.db.ExecContext(ctx, "UPDATE `groups` SET status = 'active', visibility = 'listed', graduated_at = NOW() WHERE id = ?", listedGroup.ID); err != nil {
		t.Fatalf("Failed to activate group: %v", err)
	}
	groupID := listedGroup.ID
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

// --- Browse Tests ---

func TestGroupHandler_Browse_ReturnsListedGroupsWithDisclaimer(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpbrowse_listed", models.TierVouched, false)
	user.VouchVerified = true
	region := suite.createTestRegion("GrpBrowseRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Listed Browse Group",
		Visibility: "listed",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, user.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Force active + listed
	_, _ = suite.db.ExecContext(ctx, "UPDATE `groups` SET status = 'active', visibility = 'listed' WHERE id = ?", group.ID)

	defer suite.cleanup([]string{user.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(user)

	req := httptest.NewRequest("GET", "/api/v1/groups/browse?region_id="+region.ID, nil)
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Browse(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&result)

	// Check disclaimer is present
	disclaimer, ok := result["disclaimer"].([]interface{})
	if !ok || len(disclaimer) != 2 {
		t.Errorf("Expected disclaimer array with 2 entries, got %v", result["disclaimer"])
	}

	// Check groups are present
	groups, ok := result["groups"].([]interface{})
	if !ok {
		t.Fatal("Expected groups array in response")
	}

	found := false
	for _, g := range groups {
		gMap := g.(map[string]interface{})
		if gMap["name"] == "Listed Browse Group" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'Listed Browse Group' in browse results")
	}
}

func TestGroupHandler_Browse_WithRegionID(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpbrowse_region", models.TierVouched, false)
	user.VouchVerified = true
	region1 := suite.createTestRegion("GrpBrowseRegionA", models.RegionTypeCity, nil)
	region2 := suite.createTestRegion("GrpBrowseRegionB", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region1.ID, false)

	// Create group in region1
	createReq1 := &models.CreateGroupRequest{
		Name:       "Region1 Group",
		Visibility: "listed",
		RegionIDs:  []string{region1.ID},
	}
	group1, err := suite.groupRepo.Create(ctx, createReq1, user.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}
	_, _ = suite.db.ExecContext(ctx, "UPDATE `groups` SET status = 'active', visibility = 'listed' WHERE id = ?", group1.ID)

	// Create group in region2
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region2.ID, false)
	createReq2 := &models.CreateGroupRequest{
		Name:       "Region2 Group",
		Visibility: "listed",
		RegionIDs:  []string{region2.ID},
	}
	group2, err := suite.groupRepo.Create(ctx, createReq2, user.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}
	_, _ = suite.db.ExecContext(ctx, "UPDATE `groups` SET status = 'active', visibility = 'listed' WHERE id = ?", group2.ID)

	defer suite.cleanup([]string{user.ID}, []string{region1.ID, region2.ID}, []string{group1.ID, group2.ID})

	// Browse with region1 filter
	claims := suite.claimsForUser(user)

	req := httptest.NewRequest("GET", "/api/v1/groups/browse?region_id="+region1.ID, nil)
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Browse(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&result)

	groups, ok := result["groups"].([]interface{})
	if !ok {
		t.Fatal("Expected groups array in response")
	}

	// Region1 group should appear; Region2 group should not (unless it's discoverable)
	foundRegion1 := false
	foundRegion2 := false
	for _, g := range groups {
		gMap := g.(map[string]interface{})
		if gMap["name"] == "Region1 Group" {
			foundRegion1 = true
		}
		if gMap["name"] == "Region2 Group" {
			foundRegion2 = true
		}
	}
	if !foundRegion1 {
		t.Error("Expected 'Region1 Group' in browse results for region1")
	}
	if foundRegion2 {
		t.Error("Expected 'Region2 Group' NOT in browse results for region1")
	}
}

func TestGroupHandler_Browse_UnverifiedUserSeesOnlyDiscoverable(t *testing.T) {
	suite := setupGroupTestSuite(t)

	// Admin creates groups
	admin := suite.createTestUser("grpbrowse_admin", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("GrpBrowseRegionU", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	// Create a listed active group (non-discoverable)
	createReq := &models.CreateGroupRequest{
		Name:       "Normal Listed Group",
		Visibility: "listed",
		RegionIDs:  []string{region.ID},
	}
	normalGroup, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}
	_, _ = suite.db.ExecContext(ctx, "UPDATE `groups` SET status = 'active', visibility = 'listed' WHERE id = ?", normalGroup.ID)

	// Create a discoverable group with an open signal group
	createReq2 := &models.CreateGroupRequest{
		Name:       "Discoverable Group",
		Visibility: "listed",
		RegionIDs:  []string{region.ID},
	}
	discoverableGroup, err := suite.groupRepo.Create(ctx, createReq2, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}
	_, _ = suite.db.ExecContext(ctx, "UPDATE `groups` SET status = 'active', visibility = 'listed', discoverable_by_unverified = TRUE WHERE id = ?", discoverableGroup.ID)

	// Add an open-tier signal group
	sg := &models.SignalGroup{
		OwnerGroupID: &discoverableGroup.ID,
		GroupName:     "Open Chat",
		AccessTier:   models.AccessTierOpen,
		CreatedBy:    &admin.ID,
	}
	if err := suite.signalGroupRepo.CreateForOwnerGroup(ctx, sg); err != nil {
		t.Fatalf("Failed to create signal group: %v", err)
	}

	// Unverified user browsing without region_id
	unverified := suite.createTestUser("grpbrowse_unv", models.TierUnverified, false)
	defer suite.cleanup([]string{admin.ID, unverified.ID}, []string{region.ID}, []string{normalGroup.ID, discoverableGroup.ID})

	claims := suite.claimsForUser(unverified)

	req := httptest.NewRequest("GET", "/api/v1/groups/browse", nil)
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.Browse(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&result)

	groups, ok := result["groups"].([]interface{})
	if !ok {
		t.Fatal("Expected groups array in response")
	}

	// Only discoverable group should appear
	foundNormal := false
	foundDiscoverable := false
	for _, g := range groups {
		gMap := g.(map[string]interface{})
		if gMap["name"] == "Normal Listed Group" {
			foundNormal = true
		}
		if gMap["name"] == "Discoverable Group" {
			foundDiscoverable = true
		}
	}
	if foundNormal {
		t.Error("Expected non-discoverable group NOT in BrowseAll results")
	}
	if !foundDiscoverable {
		t.Error("Expected discoverable group in BrowseAll results")
	}
}

// --- RespondToInvitation: unverified user can accept ---

func TestGroupHandler_RespondToInvitation_UnverifiedUserCanAccept(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpresp_admin_uv", models.TierVouched, false)
	admin.VouchVerified = true
	unverifiedInvitee := suite.createTestUser("grpresp_uvinvitee", models.TierUnverified, false)
	region := suite.createTestRegion("GrpRespRegionUV", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Invite Unverified Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	invitation, err := suite.groupRepo.CreateInvitation(ctx, group.ID, unverifiedInvitee.ID, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create invitation: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, unverifiedInvitee.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(unverifiedInvitee)
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
	isMember, _ := suite.groupRepo.IsUserMember(context.Background(), group.ID, unverifiedInvitee.ID)
	if !isMember {
		t.Error("Expected unverified invitee to be a member after accepting invitation")
	}
}

// =============================================================================
// Resource Tests
// =============================================================================

func TestGroupHandler_CreateResource_AdminSuccess(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpres_cr_admin", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("GrpResCreateRegion", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	group, err := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name:       "Resource Handler Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}, admin.ID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}

	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(admin)
	body := models.CreateResourceRequest{
		Title:       "Test Resource",
		URL:         "https://resource.example.com",
		Description: "A helpful link",
		AccessTier:  "member",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/resources?id="+group.ID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.CreateResource(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created models.GroupResource
	_ = json.NewDecoder(rec.Body).Decode(&created)
	if created.ID == "" {
		t.Error("Expected resource ID in response")
	}
	if created.Title != "Test Resource" {
		t.Errorf("Expected title 'Test Resource', got %q", created.Title)
	}
}

func TestGroupHandler_CreateResource_MemberCanCreate(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpres_cr_na_admin", models.TierVouched, false)
	admin.VouchVerified = true
	member := suite.createTestUser("grpres_cr_na_member", models.TierVouched, false)
	member.VouchVerified = true
	region := suite.createTestRegion("GrpResNonAdminRegion", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)
	_ = suite.regionRepo.AddUserToRegion(ctx, member.ID, region.ID, false)

	group, err := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name:       "Resource NonAdmin Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}, admin.ID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}

	// Add member as non-admin
	_ = suite.groupRepo.AddMember(ctx, group.ID, member.ID, false, false)

	defer suite.cleanup([]string{admin.ID, member.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(member)
	body := models.CreateResourceRequest{
		Title:      "Member Resource",
		URL:        "https://member-created.example.com",
		AccessTier: "member",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/resources?id="+group.ID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.CreateResource(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected 201 for member creating resource, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_CreateResource_RejectsNonHTTPURL(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpres_xss_admin", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("GrpResXSSRegion", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	group, err := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name:       "Resource XSS Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}, admin.ID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}
	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(admin)
	badURLs := []string{
		"javascript:alert(document.cookie)",
		"data:text/html,<script>alert(1)</script>",
		"  javascript:alert(1)", // leading whitespace must not bypass
		"vbscript:msgbox(1)",
		"notaurl",
		"/relative/path",
	}
	for _, badURL := range badURLs {
		body := models.CreateResourceRequest{
			Title:      "Bad Resource",
			URL:        badURL,
			AccessTier: "member",
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/resources?id="+group.ID, bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(middleware.ContextWithUser(req.Context(), claims))

		rec := httptest.NewRecorder()
		suite.handler.CreateResource(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("URL %q: expected 400, got %d: %s", badURL, rec.Code, rec.Body.String())
		}
	}

	// A valid https URL still succeeds.
	okBody, _ := json.Marshal(models.CreateResourceRequest{
		Title: "Good Resource", URL: "https://example.com/help", AccessTier: "member",
	})
	req := httptest.NewRequest("POST", "/resources?id="+group.ID, bytes.NewReader(okBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.ContextWithUser(req.Context(), claims))
	rec := httptest.NewRecorder()
	suite.handler.CreateResource(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("valid https URL: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_ListSignalGroups_UnlistedHiddenFromNonMember(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpls_unl_admin", models.TierVouched, false)
	admin.VouchVerified = true
	outsider := suite.createTestUser("grpls_unl_outsider", models.TierVouched, false)
	outsider.VouchVerified = true
	region := suite.createTestRegion("GrpLsUnlistedRegion", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)
	// Outsider is a verified resident of the region but NOT a member of the group.
	_ = suite.regionRepo.AddUserToRegion(ctx, outsider.ID, region.ID, false)

	group, err := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name:       "Unlisted Signal Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}, admin.ID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}
	defer suite.cleanup([]string{admin.ID, outsider.ID}, []string{region.ID}, []string{group.ID})

	// Non-member (even a region resident) must not learn the unlisted group exists.
	req := httptest.NewRequest("GET", "/signal-groups?id="+group.ID, nil)
	req = req.WithContext(middleware.ContextWithUser(req.Context(), suite.claimsForUser(outsider)))
	rec := httptest.NewRecorder()
	suite.handler.ListSignalGroups(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-member listing unlisted group, got %d: %s", rec.Code, rec.Body.String())
	}

	// The admin (a member) can list normally.
	req2 := httptest.NewRequest("GET", "/signal-groups?id="+group.ID, nil)
	req2 = req2.WithContext(middleware.ContextWithUser(req2.Context(), suite.claimsForUser(admin)))
	rec2 := httptest.NewRecorder()
	suite.handler.ListSignalGroups(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200 for member listing unlisted group, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

// TestGroupHandler_JoinViaLink_RateLimited verifies the per-user join_via_link
// limit fires once a rate limiter is wired (regression for the limiter never
// being attached in main.go). The rate-limit check runs before token validation,
// so bogus tokens still consume the budget.
func TestGroupHandler_JoinViaLink_RateLimited(t *testing.T) {
	suite := setupGroupTestSuite(t)
	suite.handler.SetRateLimiter(services.NewInMemoryRateLimiter())

	user := suite.createTestUser("grp_join_rl", models.TierVouched, false)
	user.VouchVerified = true
	defer suite.cleanup([]string{user.ID}, nil, nil)

	claims := suite.claimsForUser(user)

	call := func() int {
		req := httptest.NewRequest("POST", "/api/v1/groups/join/bogus-token?token=bogus-token", nil)
		req = req.WithContext(middleware.ContextWithUser(req.Context(), claims))
		rec := httptest.NewRecorder()
		suite.handler.JoinViaLink(rec, req)
		return rec.Code
	}

	// First joinViaLinkLimit calls consume the budget (each 404 for bogus token,
	// but the limiter increments before validation).
	for i := 0; i < joinViaLinkLimit; i++ {
		if code := call(); code == http.StatusTooManyRequests {
			t.Fatalf("hit rate limit early at attempt %d", i+1)
		}
	}

	// The next call must be rejected with 429.
	if code := call(); code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exhausting join_via_link limit, got %d", code)
	}
}

// TestGroupHandler_BlockedUserCannotRejoin verifies the per-group user ban
// (issue #7): an admin blocks a member, which removes them, and the blocked user
// can no longer join via an invite link.
func TestGroupHandler_BlockedUserCannotRejoin(t *testing.T) {
	suite := setupGroupTestSuite(t)
	suite.handler.SetRateLimiter(services.NewInMemoryRateLimiter())
	ctx := context.Background()

	admin := suite.createTestUser("grp_ban_admin", models.TierVouched, false)
	admin.VouchVerified = true
	target := suite.createTestUser("grp_ban_target", models.TierVouched, false)
	target.VouchVerified = true
	region := suite.createTestRegion("GrpBanRegion", models.RegionTypeCity, nil)
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	group, err := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "Ban Test Group", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}
	defer suite.cleanup([]string{admin.ID, target.ID}, []string{region.ID}, []string{group.ID})

	// target joins as a member.
	if err := suite.groupRepo.AddMember(ctx, group.ID, target.ID, false, false); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	// Admin bans target via the handler.
	banBody, _ := json.Marshal(map[string]string{"user_id": target.ID, "reason": "abuse"})
	banReq := httptest.NewRequest("POST", "/api/v1/groups/"+group.ID+"/blocked-users?id="+group.ID, bytes.NewReader(banBody))
	banReq = banReq.WithContext(middleware.ContextWithUser(banReq.Context(), suite.claimsForUser(admin)))
	banRec := httptest.NewRecorder()
	suite.handler.BlockMember(banRec, banReq)
	if banRec.Code != http.StatusOK {
		t.Fatalf("BlockMember expected 200, got %d: %s", banRec.Code, banRec.Body.String())
	}

	// target should no longer be a member.
	if isMember, _ := suite.groupRepo.IsUserMember(ctx, group.ID, target.ID); isMember {
		t.Error("blocked user should have been removed from the group")
	}

	// target tries to rejoin via an invite link → 403 blocked.
	link, err := suite.groupRepo.CreateInviteLink(ctx, group.ID, admin.ID, &models.CreateInviteLinkRequest{})
	if err != nil {
		t.Fatalf("CreateInviteLink failed: %v", err)
	}
	joinReq := httptest.NewRequest("POST", "/api/v1/groups/join/"+link.Token+"?token="+link.Token, nil)
	joinReq = joinReq.WithContext(middleware.ContextWithUser(joinReq.Context(), suite.claimsForUser(target)))
	joinRec := httptest.NewRecorder()
	suite.handler.JoinViaLink(joinRec, joinReq)
	if joinRec.Code != http.StatusForbidden {
		t.Fatalf("blocked user join expected 403, got %d: %s", joinRec.Code, joinRec.Body.String())
	}
}

// TestGroupHandler_RespondToInvitation_RejectedAcceptStaysPending verifies that a
// failed accept (blocked invitee or departed inviter) does NOT mark the invitation
// accepted — it remains pending so it isn't silently consumed.
func TestGroupHandler_RespondToInvitation_RejectedAcceptStaysPending(t *testing.T) {
	suite := setupGroupTestSuite(t)
	ctx := context.Background()

	admin := suite.createTestUser("grp_inv_pend_admin", models.TierVouched, false)
	admin.VouchVerified = true
	inviter := suite.createTestUser("grp_inv_pend_inviter", models.TierVouched, false)
	inviter.VouchVerified = true
	blockedUser := suite.createTestUser("grp_inv_pend_blocked", models.TierVouched, false)
	blockedUser.VouchVerified = true
	staleUser := suite.createTestUser("grp_inv_pend_stale", models.TierVouched, false)
	staleUser.VouchVerified = true
	region := suite.createTestRegion("GrpInvPendRegion", models.RegionTypeCity, nil)
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	group, err := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "Inv Pending Group", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}
	_ = suite.groupRepo.AddMember(ctx, group.ID, inviter.ID, false, false)
	defer suite.cleanup([]string{admin.ID, inviter.ID, blockedUser.ID, staleUser.ID}, []string{region.ID}, []string{group.ID})

	respondAccept := func(invitationID string, u *models.User) int {
		body, _ := json.Marshal(models.RespondToGroupInvitationRequest{Accept: true})
		req := httptest.NewRequest("POST", "/api/v1/group-invitations/"+invitationID+"/respond?id="+invitationID, bytes.NewReader(body))
		req = req.WithContext(middleware.ContextWithUser(req.Context(), suite.claimsForUser(u)))
		rec := httptest.NewRecorder()
		suite.handler.RespondToInvitation(rec, req)
		return rec.Code
	}
	invitationStatus := func(invitationID string) string {
		inv, gErr := suite.groupRepo.GetInvitation(ctx, invitationID)
		if gErr != nil {
			t.Fatalf("GetInvitation failed: %v", gErr)
		}
		return string(inv.Status)
	}

	// Case 1: blocked invitee.
	inv1, err := suite.groupRepo.CreateInvitation(ctx, group.ID, blockedUser.ID, admin.ID)
	if err != nil {
		t.Fatalf("CreateInvitation failed: %v", err)
	}
	if err := suite.groupRepo.BlockUser(ctx, group.ID, blockedUser.ID, &admin.ID, nil); err != nil {
		t.Fatalf("BlockUser failed: %v", err)
	}
	if code := respondAccept(inv1.ID, blockedUser); code != http.StatusForbidden {
		t.Errorf("blocked invitee accept: expected 403, got %d", code)
	}
	if s := invitationStatus(inv1.ID); s != "pending" {
		t.Errorf("blocked invitee: invitation should remain pending, got %q", s)
	}

	// Case 2: stale inviter (inviter leaves before invitee accepts).
	inv2, err := suite.groupRepo.CreateInvitation(ctx, group.ID, staleUser.ID, inviter.ID)
	if err != nil {
		t.Fatalf("CreateInvitation failed: %v", err)
	}
	if err := suite.groupRepo.RemoveMember(ctx, group.ID, inviter.ID); err != nil {
		t.Fatalf("RemoveMember failed: %v", err)
	}
	if code := respondAccept(inv2.ID, staleUser); code != http.StatusConflict {
		t.Errorf("stale inviter accept: expected 409, got %d", code)
	}
	if s := invitationStatus(inv2.ID); s != "pending" {
		t.Errorf("stale inviter: invitation should remain pending, got %q", s)
	}
}

// TestGroupHandler_RespondToInvitation_InsertFailureRollsBack verifies the accept
// is transactional: if the membership insert fails (here the invitee is already a
// member, so the unique key rejects the insert), the invitation is NOT consumed —
// it remains pending and the handler returns 409.
func TestGroupHandler_RespondToInvitation_InsertFailureRollsBack(t *testing.T) {
	suite := setupGroupTestSuite(t)
	ctx := context.Background()

	admin := suite.createTestUser("grp_inv_tx_admin", models.TierVouched, false)
	admin.VouchVerified = true
	invitee := suite.createTestUser("grp_inv_tx_invitee", models.TierVouched, false)
	invitee.VouchVerified = true
	region := suite.createTestRegion("GrpInvTxRegion", models.RegionTypeCity, nil)
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	group, err := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "Inv Tx Group", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}
	defer suite.cleanup([]string{admin.ID, invitee.ID}, []string{region.ID}, []string{group.ID})

	inv, err := suite.groupRepo.CreateInvitation(ctx, group.ID, invitee.ID, admin.ID)
	if err != nil {
		t.Fatalf("CreateInvitation failed: %v", err)
	}
	// Invitee is already a member via another path — the accept's INSERT will hit
	// the unique key and the whole transaction must roll back.
	if err := suite.groupRepo.AddMember(ctx, group.ID, invitee.ID, false, false); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	body, _ := json.Marshal(models.RespondToGroupInvitationRequest{Accept: true})
	req := httptest.NewRequest("POST", "/api/v1/group-invitations/"+inv.ID+"/respond?id="+inv.ID, bytes.NewReader(body))
	req = req.WithContext(middleware.ContextWithUser(req.Context(), suite.claimsForUser(invitee)))
	rec := httptest.NewRecorder()
	suite.handler.RespondToInvitation(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for already-member accept, got %d: %s", rec.Code, rec.Body.String())
	}
	got, err := suite.groupRepo.GetInvitation(ctx, inv.ID)
	if err != nil {
		t.Fatalf("GetInvitation failed: %v", err)
	}
	if string(got.Status) != "pending" {
		t.Errorf("failed accept must roll back; invitation should stay pending, got %q", got.Status)
	}
}

// TestGroupHandler_JoinViaLink_DisallowedJoinDoesNotBurnUse verifies blocked and
// already-member join attempts do not increment the invite link's use_count.
func TestGroupHandler_JoinViaLink_DisallowedJoinDoesNotBurnUse(t *testing.T) {
	suite := setupGroupTestSuite(t)
	suite.handler.SetRateLimiter(services.NewInMemoryRateLimiter())
	ctx := context.Background()

	admin := suite.createTestUser("grp_burn_admin", models.TierVouched, false)
	admin.VouchVerified = true
	blockedUser := suite.createTestUser("grp_burn_blocked", models.TierVouched, false)
	blockedUser.VouchVerified = true
	memberUser := suite.createTestUser("grp_burn_member", models.TierVouched, false)
	memberUser.VouchVerified = true
	region := suite.createTestRegion("GrpBurnRegion", models.RegionTypeCity, nil)
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	group, err := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "Burn Test Group", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}
	defer suite.cleanup([]string{admin.ID, blockedUser.ID, memberUser.ID}, []string{region.ID}, []string{group.ID})

	_ = suite.groupRepo.BlockUser(ctx, group.ID, blockedUser.ID, &admin.ID, nil)
	_ = suite.groupRepo.AddMember(ctx, group.ID, memberUser.ID, false, false)

	link, err := suite.groupRepo.CreateInviteLink(ctx, group.ID, admin.ID, &models.CreateInviteLinkRequest{})
	if err != nil {
		t.Fatalf("CreateInviteLink failed: %v", err)
	}

	join := func(u *models.User) int {
		req := httptest.NewRequest("POST", "/api/v1/groups/join/"+link.Token+"?token="+link.Token, nil)
		req = req.WithContext(middleware.ContextWithUser(req.Context(), suite.claimsForUser(u)))
		rec := httptest.NewRecorder()
		suite.handler.JoinViaLink(rec, req)
		return rec.Code
	}
	useCount := func() int {
		l, gErr := suite.groupRepo.GetInviteLinkByToken(ctx, link.Token)
		if gErr != nil {
			t.Fatalf("GetInviteLinkByToken failed: %v", gErr)
		}
		return l.UseCount
	}

	if uc := useCount(); uc != 0 {
		t.Fatalf("precondition: expected use_count 0, got %d", uc)
	}

	if code := join(blockedUser); code != http.StatusForbidden {
		t.Errorf("blocked join: expected 403, got %d", code)
	}
	if uc := useCount(); uc != 0 {
		t.Errorf("blocked join must not burn a use; use_count=%d", uc)
	}

	if code := join(memberUser); code != http.StatusConflict {
		t.Errorf("already-member join: expected 409, got %d", code)
	}
	if uc := useCount(); uc != 0 {
		t.Errorf("already-member join must not burn a use; use_count=%d", uc)
	}

	// Sanity: an allowed join DOES consume one use.
	newUser := suite.createTestUser("grp_burn_ok", models.TierVouched, false)
	newUser.VouchVerified = true
	defer suite.cleanup([]string{newUser.ID}, nil, nil)
	if code := join(newUser); code != http.StatusOK {
		t.Fatalf("allowed join: expected 200, got %d", code)
	}
	if uc := useCount(); uc != 1 {
		t.Errorf("allowed join should increment use_count to 1, got %d", uc)
	}
}

func TestGroupHandler_ListResources_FilteredByAccessTier(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpres_ls_admin", models.TierVouched, false)
	admin.VouchVerified = true
	member := suite.createTestUser("grpres_ls_member", models.TierVouched, false)
	member.VouchVerified = true
	region := suite.createTestRegion("GrpResListRegion", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)
	_ = suite.regionRepo.AddUserToRegion(ctx, member.ID, region.ID, false)

	group, err := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name:       "Resource List Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}, admin.ID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}

	// Add member as non-admin
	_ = suite.groupRepo.AddMember(ctx, group.ID, member.ID, false, false)

	// Create resources with different tiers
	_, _ = suite.groupRepo.CreateResource(ctx, group.ID, admin.ID, &models.CreateResourceRequest{
		Title: "Open Resource", URL: "https://open.example.com", AccessTier: "open",
	})
	_, _ = suite.groupRepo.CreateResource(ctx, group.ID, admin.ID, &models.CreateResourceRequest{
		Title: "Member Resource", URL: "https://member.example.com", AccessTier: "member",
	})
	_, _ = suite.groupRepo.CreateResource(ctx, group.ID, admin.ID, &models.CreateResourceRequest{
		Title: "Admin Resource", URL: "https://admin.example.com", AccessTier: "admin_only",
	})

	defer suite.cleanup([]string{admin.ID, member.ID}, []string{region.ID}, []string{group.ID})

	// Member should see open + member but NOT admin_only
	memberClaims := suite.claimsForUser(member)
	req := httptest.NewRequest("GET", "/resources?id="+group.ID, nil)
	ctx = middleware.ContextWithUser(req.Context(), memberClaims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.ListResources(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string][]models.GroupResource
	_ = json.NewDecoder(rec.Body).Decode(&response)

	resources := response["resources"]
	if len(resources) != 2 {
		t.Fatalf("Expected 2 resources for member, got %d", len(resources))
	}

	for _, r := range resources {
		if r.AccessTier == models.AccessTierAdminOnly {
			t.Error("Member should not see admin_only resources")
		}
	}
}

func TestGroupHandler_UpdateResource_AdminSuccess(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpres_upd_admin", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("GrpResUpdateRegion", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	group, err := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name:       "Resource Update Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}, admin.ID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}

	resource, err := suite.groupRepo.CreateResource(ctx, group.ID, admin.ID, &models.CreateResourceRequest{
		Title: "Original", URL: "https://orig.example.com", AccessTier: "member",
	})
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}

	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(admin)
	newTitle := "Updated"
	updateBody := models.UpdateResourceRequest{Title: &newTitle}
	bodyBytes, _ := json.Marshal(updateBody)

	req := httptest.NewRequest("PUT", "/resources?id="+group.ID+"&rid="+resource.ID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.UpdateResource(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify update
	updated, _ := suite.groupRepo.GetResource(ctx, resource.ID)
	if updated.Title != "Updated" {
		t.Errorf("Expected title 'Updated', got %q", updated.Title)
	}
}

func TestGroupHandler_DeleteResource_AdminSuccess(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("grpres_del_admin", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("GrpResDeleteRegion", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	group, err := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name:       "Resource Delete Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}, admin.ID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}

	resource, err := suite.groupRepo.CreateResource(ctx, group.ID, admin.ID, &models.CreateResourceRequest{
		Title: "To Delete", URL: "https://delete.example.com", AccessTier: "member",
	})
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}

	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(admin)
	req := httptest.NewRequest("DELETE", "/resources?id="+group.ID+"&rid="+resource.ID, nil)
	ctx = middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	suite.handler.DeleteResource(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify deleted
	_, err = suite.groupRepo.GetResource(ctx, resource.ID)
	if err != database.ErrResourceNotFound {
		t.Errorf("Expected ErrResourceNotFound after delete, got %v", err)
	}
}

// --- Group Blocking: Repository Tests ---

func TestGroupRepo_BlockGroup(t *testing.T) {
	suite := setupGroupTestSuite(t)
	ctx := context.Background()

	admin := suite.createTestUser("blk_repo_admin", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("BlkRepoRegion", models.RegionTypeCity, nil)
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	groupA, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "Block Repo Group A", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)
	groupB, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "Block Repo Group B", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)

	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID})

	// Block
	err := suite.groupRepo.BlockGroup(ctx, groupA.ID, groupB.ID)
	if err != nil {
		t.Fatalf("BlockGroup failed: %v", err)
	}

	// IsGroupBlocked
	blocked, err := suite.groupRepo.IsGroupBlocked(ctx, groupA.ID, groupB.ID)
	if err != nil {
		t.Fatalf("IsGroupBlocked failed: %v", err)
	}
	if !blocked {
		t.Error("Expected group to be blocked")
	}

	// Not blocked in reverse direction
	blockedReverse, err := suite.groupRepo.IsGroupBlocked(ctx, groupB.ID, groupA.ID)
	if err != nil {
		t.Fatalf("IsGroupBlocked reverse failed: %v", err)
	}
	if blockedReverse {
		t.Error("Expected reverse direction to not be blocked")
	}

	// List blocked
	blockedGroups, err := suite.groupRepo.ListBlockedGroups(ctx, groupA.ID)
	if err != nil {
		t.Fatalf("ListBlockedGroups failed: %v", err)
	}
	if len(blockedGroups) != 1 {
		t.Fatalf("Expected 1 blocked group, got %d", len(blockedGroups))
	}
	if blockedGroups[0].ID != groupB.ID {
		t.Errorf("Expected blocked group ID %s, got %s", groupB.ID, blockedGroups[0].ID)
	}

	// Duplicate block
	err = suite.groupRepo.BlockGroup(ctx, groupA.ID, groupB.ID)
	if err != database.ErrGroupAlreadyBlocked {
		t.Errorf("Expected ErrGroupAlreadyBlocked, got %v", err)
	}

	// Self-block
	err = suite.groupRepo.BlockGroup(ctx, groupA.ID, groupA.ID)
	if err != database.ErrCannotBlockSelf {
		t.Errorf("Expected ErrCannotBlockSelf, got %v", err)
	}

	// Unblock
	err = suite.groupRepo.UnblockGroup(ctx, groupA.ID, groupB.ID)
	if err != nil {
		t.Fatalf("UnblockGroup failed: %v", err)
	}

	// Verify unblocked
	blocked, _ = suite.groupRepo.IsGroupBlocked(ctx, groupA.ID, groupB.ID)
	if blocked {
		t.Error("Expected group to be unblocked")
	}

	// Unblock nonexistent
	err = suite.groupRepo.UnblockGroup(ctx, groupA.ID, groupB.ID)
	if err != database.ErrGroupNotFound {
		t.Errorf("Expected ErrGroupNotFound for unblocking non-blocked, got %v", err)
	}
}

// --- Group Blocking: Handler Tests ---

func TestGroupHandler_BlockGroup_Success(t *testing.T) {
	suite := setupGroupTestSuite(t)
	ctx := context.Background()

	admin := suite.createTestUser("blk_h_admin", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("BlkHandlerRegion", models.RegionTypeCity, nil)
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	groupA, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "Block Handler A", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)
	groupB, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "Block Handler B", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)

	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID})

	claims := suite.claimsForUser(admin)
	body, _ := json.Marshal(map[string]string{"group_id": groupB.ID})
	req := httptest.NewRequest("POST", "/blocks?id="+groupA.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(reqCtx)

	rec := httptest.NewRecorder()
	suite.handler.BlockGroup(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify blocked
	blocked, _ := suite.groupRepo.IsGroupBlocked(ctx, groupA.ID, groupB.ID)
	if !blocked {
		t.Error("Expected group to be blocked after handler call")
	}
}

func TestGroupHandler_UnblockGroup_Success(t *testing.T) {
	suite := setupGroupTestSuite(t)
	ctx := context.Background()

	admin := suite.createTestUser("unblk_h_admin", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("UnblkHandlerRegion", models.RegionTypeCity, nil)
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	groupA, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "Unblock Handler A", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)
	groupB, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "Unblock Handler B", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)

	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID})

	// Block first
	_ = suite.groupRepo.BlockGroup(ctx, groupA.ID, groupB.ID)

	claims := suite.claimsForUser(admin)
	req := httptest.NewRequest("DELETE", "/blocks?id="+groupA.ID+"&gid="+groupB.ID, nil)
	reqCtx := middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(reqCtx)

	rec := httptest.NewRecorder()
	suite.handler.UnblockGroup(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify unblocked
	blocked, _ := suite.groupRepo.IsGroupBlocked(ctx, groupA.ID, groupB.ID)
	if blocked {
		t.Error("Expected group to be unblocked after handler call")
	}
}

func TestGroupHandler_ListBlockedGroups_Success(t *testing.T) {
	suite := setupGroupTestSuite(t)
	ctx := context.Background()

	admin := suite.createTestUser("lstblk_h_admin", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("LstBlkHandlerRegion", models.RegionTypeCity, nil)
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	groupA, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "List Block Handler A", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)
	groupB, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "List Block Handler B", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)

	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID})

	_ = suite.groupRepo.BlockGroup(ctx, groupA.ID, groupB.ID)

	claims := suite.claimsForUser(admin)
	req := httptest.NewRequest("GET", "/blocks?id="+groupA.ID, nil)
	reqCtx := middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(reqCtx)

	rec := httptest.NewRecorder()
	suite.handler.ListBlockedGroups(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&body)
	blockedGroups := body["blocked_groups"].([]interface{})
	if len(blockedGroups) != 1 {
		t.Fatalf("Expected 1 blocked group, got %d", len(blockedGroups))
	}
}

func TestGroupHandler_BlockGroup_NonAdminRejected(t *testing.T) {
	suite := setupGroupTestSuite(t)
	ctx := context.Background()

	admin := suite.createTestUser("blk_admin_ok", models.TierVouched, false)
	admin.VouchVerified = true
	nonAdmin := suite.createTestUser("blk_nonadmin", models.TierVouched, false)
	nonAdmin.VouchVerified = true
	region := suite.createTestRegion("BlkNonAdminRegion", models.RegionTypeCity, nil)
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)
	_ = suite.regionRepo.AddUserToRegion(ctx, nonAdmin.ID, region.ID, false)

	groupA, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "Block NonAdmin A", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)
	groupB, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "Block NonAdmin B", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)

	// Add nonAdmin as regular member
	_ = suite.groupRepo.AddMember(ctx, groupA.ID, nonAdmin.ID, false, false)

	defer suite.cleanup([]string{admin.ID, nonAdmin.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID})

	claims := suite.claimsForUser(nonAdmin)
	body, _ := json.Marshal(map[string]string{"group_id": groupB.ID})
	req := httptest.NewRequest("POST", "/blocks?id="+groupA.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(reqCtx)

	rec := httptest.NewRecorder()
	suite.handler.BlockGroup(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_BlockGroup_SelfBlockRejected(t *testing.T) {
	suite := setupGroupTestSuite(t)
	ctx := context.Background()

	admin := suite.createTestUser("blk_self_admin", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("BlkSelfRegion", models.RegionTypeCity, nil)
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	groupA, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "Block Self Group", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)

	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{groupA.ID})

	claims := suite.claimsForUser(admin)
	body, _ := json.Marshal(map[string]string{"group_id": groupA.ID})
	req := httptest.NewRequest("POST", "/blocks?id="+groupA.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(reqCtx)

	rec := httptest.NewRecorder()
	suite.handler.BlockGroup(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// Topic Board Handler Tests
// =============================================================================

func TestGroupHandler_TopicBoard_CreatePosting(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("tb_h_create", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("TBHandlerRegion", models.RegionTypeState, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	groupReq := &models.CreateGroupRequest{
		Name: "TB Handler Group", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, groupReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{group.ID})

	t.Run("admin can create posting", func(t *testing.T) {
		claims := suite.claimsForUser(admin)
		body, _ := json.Marshal(models.CreateTopicBoardPostingRequest{
			Description: "Looking for mutual aid partners nearby",
			Tags:        []string{"mutual-aid", "safety"},
		})
		req := httptest.NewRequest("POST", "/topic-board?id="+group.ID, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		reqCtx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(reqCtx)

		rec := httptest.NewRecorder()
		suite.handler.CreateOrUpdatePosting(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var posting models.TopicBoardPostingWithTags
		_ = json.NewDecoder(rec.Body).Decode(&posting)
		if posting.ID == "" {
			t.Error("Expected posting ID")
		}
		if posting.Description != "Looking for mutual aid partners nearby" {
			t.Errorf("Unexpected description: %s", posting.Description)
		}
		if len(posting.Tags) != 2 {
			t.Errorf("Expected 2 tags, got %d", len(posting.Tags))
		}
	})
}

func TestGroupHandler_TopicBoard_CreatePosting_NonAdmin(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("tb_h_nonadm_a", models.TierVouched, false)
	admin.VouchVerified = true
	member := suite.createTestUser("tb_h_nonadm_m", models.TierVouched, false)
	member.VouchVerified = true
	region := suite.createTestRegion("TBNonAdminRegion", models.RegionTypeState, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	group, err := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "TB NonAdmin Group", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Add member as non-admin
	_ = suite.groupRepo.AddMember(ctx, group.ID, member.ID, false, false)

	defer suite.cleanup([]string{admin.ID, member.ID}, []string{region.ID}, []string{group.ID})

	claims := suite.claimsForUser(member)
	body, _ := json.Marshal(models.CreateTopicBoardPostingRequest{
		Description: "Non-admin trying to post something",
		Tags:        []string{"test"},
	})
	req := httptest.NewRequest("POST", "/topic-board?id="+group.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(reqCtx)

	rec := httptest.NewRecorder()
	suite.handler.CreateOrUpdatePosting(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_TopicBoard_BrowseByTag(t *testing.T) {
	suite := setupGroupTestSuite(t)

	adminA := suite.createTestUser("tb_h_br_a", models.TierVouched, false)
	adminA.VouchVerified = true
	adminB := suite.createTestUser("tb_h_br_b", models.TierVouched, false)
	adminB.VouchVerified = true
	region := suite.createTestRegion("TBBrowseRegion", models.RegionTypeState, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, adminA.ID, region.ID, false)
	_ = suite.regionRepo.AddUserToRegion(ctx, adminB.ID, region.ID, false)

	groupA, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "TB Browse A", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, adminA.ID)
	groupB, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "TB Browse B", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, adminB.ID)

	defer suite.cleanup([]string{adminA.ID, adminB.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID})

	// Create postings
	_, _ = suite.groupRepo.CreateOrUpdatePosting(ctx, groupB.ID, &models.CreateTopicBoardPostingRequest{
		Description: "Group B is ready for mutual aid",
		Tags:        []string{"mutual-aid"},
	})

	t.Run("browse returns matching postings", func(t *testing.T) {
		claims := suite.claimsForUser(adminA)
		req := httptest.NewRequest("GET", "/topic-board?tag=mutual-aid&group_id="+groupA.ID, nil)
		reqCtx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(reqCtx)

		rec := httptest.NewRecorder()
		suite.handler.BrowsePostings(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result map[string][]models.TopicBoardPostingWithTags
		_ = json.NewDecoder(rec.Body).Decode(&result)
		postings := result["postings"]
		if len(postings) != 1 {
			t.Fatalf("Expected 1 posting, got %d", len(postings))
		}
		if postings[0].GroupID != groupB.ID {
			t.Errorf("Expected group B, got %s", postings[0].GroupID)
		}
	})

	t.Run("browse without tag returns all postings", func(t *testing.T) {
		claims := suite.claimsForUser(adminA)
		req := httptest.NewRequest("GET", "/topic-board?group_id="+groupA.ID, nil)
		reqCtx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(reqCtx)

		rec := httptest.NewRecorder()
		suite.handler.BrowsePostings(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestGroupHandler_TopicBoard_BrowseExcludesBlocked(t *testing.T) {
	suite := setupGroupTestSuite(t)

	adminA := suite.createTestUser("tb_h_blk_a", models.TierVouched, false)
	adminA.VouchVerified = true
	adminB := suite.createTestUser("tb_h_blk_b", models.TierVouched, false)
	adminB.VouchVerified = true
	region := suite.createTestRegion("TBBlockRegion", models.RegionTypeState, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, adminA.ID, region.ID, false)
	_ = suite.regionRepo.AddUserToRegion(ctx, adminB.ID, region.ID, false)

	groupA, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "TB Block A", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, adminA.ID)
	groupB, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "TB Block B", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, adminB.ID)

	defer suite.cleanup([]string{adminA.ID, adminB.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID})

	// Create posting for group B
	_, _ = suite.groupRepo.CreateOrUpdatePosting(ctx, groupB.ID, &models.CreateTopicBoardPostingRequest{
		Description: "Group B offers mutual aid services",
		Tags:        []string{"mutual-aid"},
	})

	// Block group B from group A
	_ = suite.groupRepo.BlockGroup(ctx, groupA.ID, groupB.ID)

	claims := suite.claimsForUser(adminA)
	req := httptest.NewRequest("GET", "/topic-board?tag=mutual-aid&group_id="+groupA.ID, nil)
	reqCtx := middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(reqCtx)

	rec := httptest.NewRecorder()
	suite.handler.BrowsePostings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string][]models.TopicBoardPostingWithTags
	_ = json.NewDecoder(rec.Body).Decode(&result)
	if len(result["postings"]) != 0 {
		t.Errorf("Expected 0 postings (blocked group excluded), got %d", len(result["postings"]))
	}
}

func TestGroupHandler_TopicBoard_RemovePosting(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createTestUser("tb_h_remove", models.TierVouched, false)
	admin.VouchVerified = true
	region := suite.createTestRegion("TBRemoveRegion", models.RegionTypeState, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	group, _ := suite.groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name: "TB Remove Group", Visibility: "unlisted", RegionIDs: []string{region.ID},
	}, admin.ID)

	defer suite.cleanup([]string{admin.ID}, []string{region.ID}, []string{group.ID})

	// Create posting
	_, _ = suite.groupRepo.CreateOrUpdatePosting(ctx, group.ID, &models.CreateTopicBoardPostingRequest{
		Description: "Posting that will be removed later",
		Tags:        []string{"test"},
	})

	claims := suite.claimsForUser(admin)
	req := httptest.NewRequest("DELETE", "/topic-board?id="+group.ID, nil)
	reqCtx := middleware.ContextWithUser(req.Context(), claims)
	req = req.WithContext(reqCtx)

	rec := httptest.NewRecorder()
	suite.handler.RemovePosting(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify posting is gone
	_, err := suite.groupRepo.GetPosting(ctx, group.ID)
	if err == nil {
		t.Error("Expected posting to be removed")
	}
}

// --- Verification Simplification Tests ---

func TestGroupHandler_Create_PostcardVerifiedSuccess(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createPostcardVerifiedUser("grpcreate_pcard_ok")
	region := suite.createTestRegion("GrpPcardRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, false)

	defer suite.cleanup([]string{user.ID}, []string{region.ID}, nil)

	claims := suite.claimsForUser(user)
	body := models.CreateGroupRequest{
		Name:       "Postcard Group",
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

	suite.cleanup(nil, nil, []string{created.ID})
}

func TestGroupHandler_Create_VouchOnlyCannotCreate(t *testing.T) {
	suite := setupGroupTestSuite(t)

	user := suite.createTestUser("grpcreate_vouchonly", models.TierVouched, false)
	user.VouchVerified = true
	// PostcardVerified is false by default
	region := suite.createTestRegion("GrpVouchOnlyRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, user.ID, region.ID, false)

	defer suite.cleanup([]string{user.ID}, []string{region.ID}, nil)

	claims := suite.claimsForUser(user)
	body := models.CreateGroupRequest{
		Name:       "Should Fail",
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

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 for vouch-only user, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupHandler_ListMembers_AddressVerifiedVisible(t *testing.T) {
	suite := setupGroupTestSuite(t)

	// Create postcard-verified user (will show address_verified = true)
	admin := suite.createPostcardVerifiedUser("grpmem_addrvis_admin")
	member := suite.createPostcardVerifiedUser("grpmem_addrvis_member")
	region := suite.createTestRegion("GrpAddrVisRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Address Visible Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// show_address_verification defaults to true
	err = suite.groupRepo.AddMember(ctx, group.ID, member.ID, false, false)
	if err != nil {
		t.Fatalf("Failed to add member: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, member.ID}, []string{region.ID}, []string{group.ID})

	// Member (non-admin) lists members
	claims := suite.claimsForUser(member)

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
	members := result["members"]
	if len(members) != 2 {
		t.Fatalf("Expected 2 members, got %d", len(members))
	}

	// Both members are postcard-verified, so address_verified should be true
	for _, m := range members {
		if !m.AddressVerified {
			t.Errorf("Expected address_verified=true for user %s, got false", m.Username)
		}
	}
}

func TestGroupHandler_ListMembers_AddressVerifiedHidden(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createPostcardVerifiedUser("grpmem_addrhid_admin")
	member := suite.createPostcardVerifiedUser("grpmem_addrhid_member")
	region := suite.createTestRegion("GrpAddrHidRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Address Hidden Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Disable show_address_verification
	showFalse := false
	err = suite.groupRepo.Update(ctx, group.ID, &models.UpdateGroupRequest{
		ShowAddressVerification: &showFalse,
	})
	if err != nil {
		t.Fatalf("Failed to update group: %v", err)
	}

	err = suite.groupRepo.AddMember(ctx, group.ID, member.ID, false, false)
	if err != nil {
		t.Fatalf("Failed to add member: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, member.ID}, []string{region.ID}, []string{group.ID})

	// Non-admin member lists members -- address_verified should be hidden
	claims := suite.claimsForUser(member)

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
	members := result["members"]
	if len(members) != 2 {
		t.Fatalf("Expected 2 members, got %d", len(members))
	}

	// address_verified should be false for all members since setting is off and caller is non-admin
	for _, m := range members {
		if m.AddressVerified {
			t.Errorf("Expected address_verified=false for user %s when setting is off, got true", m.Username)
		}
	}
}

func TestGroupHandler_ListMembers_AddressVerifiedVisibleToAdmin(t *testing.T) {
	suite := setupGroupTestSuite(t)

	admin := suite.createPostcardVerifiedUser("grpmem_addradm_admin")
	member := suite.createPostcardVerifiedUser("grpmem_addradm_member")
	region := suite.createTestRegion("GrpAddrAdmRegion1", models.RegionTypeCity, nil)

	ctx := context.Background()
	_ = suite.regionRepo.AddUserToRegion(ctx, admin.ID, region.ID, false)

	createReq := &models.CreateGroupRequest{
		Name:       "Address Admin Group",
		Visibility: "unlisted",
		RegionIDs:  []string{region.ID},
	}
	group, err := suite.groupRepo.Create(ctx, createReq, admin.ID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Disable show_address_verification
	showFalse := false
	err = suite.groupRepo.Update(ctx, group.ID, &models.UpdateGroupRequest{
		ShowAddressVerification: &showFalse,
	})
	if err != nil {
		t.Fatalf("Failed to update group: %v", err)
	}

	err = suite.groupRepo.AddMember(ctx, group.ID, member.ID, false, false)
	if err != nil {
		t.Fatalf("Failed to add member: %v", err)
	}

	defer suite.cleanup([]string{admin.ID, member.ID}, []string{region.ID}, []string{group.ID})

	// Admin lists members -- should still see address_verified even with setting off
	claims := suite.claimsForUser(admin)

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
	members := result["members"]
	if len(members) != 2 {
		t.Fatalf("Expected 2 members, got %d", len(members))
	}

	// Admin should see the real address_verified values (both true since postcard-verified)
	for _, m := range members {
		if !m.AddressVerified {
			t.Errorf("Expected address_verified=true for admin view of user %s, got false", m.Username)
		}
	}
}
