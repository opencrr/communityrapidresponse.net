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

type ConnectionTestSuite struct {
	t              *testing.T
	db             *database.DB
	groupRepo      *database.GroupRepository
	connectionRepo *database.ConnectionRepository
	regionRepo     *database.RegionRepository
	userRepo       *database.UserRepository
	auditRepo      *database.AuditRepository
	handler        *ConnectionHandler
	groupHandler   *GroupHandler
	jwtAuth        *middleware.JWTAuth
}

func setupConnectionTestSuite(t *testing.T) *ConnectionTestSuite {
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
	connectionRepo := database.NewConnectionRepository(db)
	signalGroupRepo := database.NewSignalGroupRepository(db)
	regionRepo := database.NewRegionRepository(db)
	userRepo := database.NewUserRepository(db)
	auditRepo := database.NewAuditRepository(db)
	encryptedSecretRepo := database.NewEncryptedSecretRepository(db)
	connectionHandler := NewConnectionHandler(connectionRepo, groupRepo, auditRepo, encryptedSecretRepo, userRepo)
	meshtasticChannelRepo := database.NewMeshtasticChannelRepository(db)
	groupHandler := NewGroupHandler(db, groupRepo, signalGroupRepo, meshtasticChannelRepo, regionRepo, userRepo, auditRepo, encryptedSecretRepo)

	jwtConfig := &config.JWTConfig{
		Secret:          "test_secret_key_at_least_32_characters_long",
		ExpirationHours: 24,
		Issuer:          "test_issuer",
	}
	jwtAuth := middleware.NewJWTAuth(jwtConfig)

	suite := &ConnectionTestSuite{
		t:              t,
		db:             db,
		groupRepo:      groupRepo,
		connectionRepo: connectionRepo,
		regionRepo:     regionRepo,
		userRepo:       userRepo,
		auditRepo:      auditRepo,
		handler:        connectionHandler,
		groupHandler:   groupHandler,
		jwtAuth:        jwtAuth,
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return suite
}

func (s *ConnectionTestSuite) createTestUser(username string, tier models.VerificationTier, isSuperuser bool) *models.User {
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

func (s *ConnectionTestSuite) createTestRegion(name string, regionType models.RegionType, parentID *string) *models.GeographicRegion {
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

func (s *ConnectionTestSuite) createTestGroup(name string, creatorID string, regionIDs []string) *models.Group {
	ctx := context.Background()
	req := &models.CreateGroupRequest{
		Name:       name,
		Visibility: "listed",
		RegionIDs:  regionIDs,
	}
	group, err := s.groupRepo.Create(ctx, req, creatorID)
	if err != nil {
		s.t.Fatalf("Failed to create test group: %v", err)
	}
	return group
}

func (s *ConnectionTestSuite) claimsForUser(user *models.User) *middleware.Claims {
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

func (s *ConnectionTestSuite) cleanup(userIDs, regionIDs, groupIDs, connectionIDs []string) {
	ctx := context.Background()
	for _, id := range connectionIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM connection_shared_resources WHERE connection_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE connection_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM connection_chat_proposal_votes WHERE proposal_id IN (SELECT id FROM connection_chat_proposals WHERE connection_id = ?)", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM connection_chat_proposals WHERE connection_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM connection_proposal_groups WHERE proposal_id IN (SELECT id FROM connection_proposals WHERE connection_id = ?)", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM connection_proposals WHERE connection_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM connection_members WHERE connection_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM connections WHERE id = ?", id)
	}
	// Also clean orphaned proposals (formation proposals with no connection_id)
	_, _ = s.db.ExecContext(ctx, "DELETE FROM connection_proposal_groups WHERE proposal_id IN (SELECT id FROM connection_proposals WHERE connection_id IS NULL)")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM connection_proposals WHERE connection_id IS NULL")

	for _, id := range groupIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM connection_shared_resources WHERE shared_by_group_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_resources WHERE group_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM group_blocks WHERE blocker_group_id = ? OR blocked_group_id = ?", id, id)
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

// --- Propose Tests ---

func TestConnectionHandler_Propose_Success(t *testing.T) {
	s := setupConnectionTestSuite(t)
	user := s.createTestUser("conn_propose_user", 1, false)
	user.VouchVerified = true
	region := s.createTestRegion("Conn Propose Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group A", user.ID, []string{region.ID})
	groupB := s.createTestGroup("Group B", user.ID, []string{region.ID})
	defer s.cleanup([]string{user.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, nil)

	body, _ := json.Marshal(models.ProposeConnectionRequest{
		Name:     "Test Connection",
		GroupIDs: []string{groupB.ID},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections?proposer_group_id="+groupA.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.ProposeConnection(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var result models.ConnectionProposalWithGroups
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if result.ProposalType != "formation" {
		t.Errorf("Expected proposal_type=formation, got %s", result.ProposalType)
	}
	if len(result.Groups) != 2 {
		t.Errorf("Expected 2 group entries, got %d", len(result.Groups))
	}
}

func TestConnectionHandler_Propose_NonAdmin(t *testing.T) {
	s := setupConnectionTestSuite(t)
	admin := s.createTestUser("conn_propose_admin", 1, false)
	admin.VouchVerified = true
	nonAdmin := s.createTestUser("conn_propose_nonadmin", 1, false)
	nonAdmin.VouchVerified = true
	region := s.createTestRegion("Conn NonAdmin Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group NonAdmin A", admin.ID, []string{region.ID})
	groupB := s.createTestGroup("Group NonAdmin B", admin.ID, []string{region.ID})
	defer s.cleanup([]string{admin.ID, nonAdmin.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, nil)

	body, _ := json.Marshal(models.ProposeConnectionRequest{
		GroupIDs: []string{groupB.ID},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections?proposer_group_id="+groupA.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(nonAdmin))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.ProposeConnection(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("Expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- Respond Tests ---

func TestConnectionHandler_Respond_AcceptFormsConnection(t *testing.T) {
	s := setupConnectionTestSuite(t)
	user := s.createTestUser("conn_resp_accept_user", 1, false)
	user.VouchVerified = true
	region := s.createTestRegion("Conn Resp Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group Resp A", user.ID, []string{region.ID})
	groupB := s.createTestGroup("Group Resp B", user.ID, []string{region.ID})
	defer s.cleanup([]string{user.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, nil)

	// Propose
	proposal, err := s.connectionRepo.ProposeConnection(context.Background(), groupA.ID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupB.ID},
	})
	if err != nil {
		t.Fatalf("Failed to propose: %v", err)
	}

	// Respond (accept)
	body, _ := json.Marshal(map[string]interface{}{
		"accept":   true,
		"group_id": groupB.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connection-proposals/"+proposal.ID+"/respond?id="+proposal.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.RespondToProposal(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var result models.ConnectionProposalWithGroups
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if result.Status != "accepted" {
		t.Errorf("Expected status=accepted, got %s", result.Status)
	}
	if result.ConnectionID == nil {
		t.Error("Expected connection_id to be set after formation")
	}

	// Cleanup connection if formed
	if result.ConnectionID != nil {
		defer s.cleanup(nil, nil, nil, []string{*result.ConnectionID})
	}
}

func TestConnectionHandler_Respond_Decline(t *testing.T) {
	s := setupConnectionTestSuite(t)
	user := s.createTestUser("conn_resp_decline_user", 1, false)
	user.VouchVerified = true
	region := s.createTestRegion("Conn Decline Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group Decline A", user.ID, []string{region.ID})
	groupB := s.createTestGroup("Group Decline B", user.ID, []string{region.ID})
	defer s.cleanup([]string{user.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, nil)

	proposal, err := s.connectionRepo.ProposeConnection(context.Background(), groupA.ID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupB.ID},
	})
	if err != nil {
		t.Fatalf("Failed to propose: %v", err)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"accept":   false,
		"group_id": groupB.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connection-proposals/"+proposal.ID+"/respond?id="+proposal.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.RespondToProposal(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var result models.ConnectionProposalWithGroups
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if result.Status != "declined" {
		t.Errorf("Expected status=declined, got %s", result.Status)
	}
}

// --- List Tests ---

func TestConnectionHandler_ListConnections(t *testing.T) {
	s := setupConnectionTestSuite(t)
	user := s.createTestUser("conn_list_user", 1, false)
	user.VouchVerified = true
	region := s.createTestRegion("Conn List Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group List A", user.ID, []string{region.ID})
	groupB := s.createTestGroup("Group List B", user.ID, []string{region.ID})
	defer s.cleanup([]string{user.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, nil)

	// Create connection via proposal + accept
	proposal, _ := s.connectionRepo.ProposeConnection(context.Background(), groupA.ID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupB.ID},
	})
	result, _ := s.connectionRepo.RespondToProposal(context.Background(), proposal.ID, groupB.ID, true)
	if result.ConnectionID != nil {
		defer s.cleanup(nil, nil, nil, []string{*result.ConnectionID})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.ListMyConnections(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	connections, ok := response["connections"].([]interface{})
	if !ok {
		t.Fatal("Expected connections array in response")
	}
	if len(connections) == 0 {
		t.Error("Expected at least one connection")
	}
}

// --- Get Tests ---

func TestConnectionHandler_Get_MemberCanView(t *testing.T) {
	s := setupConnectionTestSuite(t)
	user := s.createTestUser("conn_get_user", 1, false)
	user.VouchVerified = true
	region := s.createTestRegion("Conn Get Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group Get A", user.ID, []string{region.ID})
	groupB := s.createTestGroup("Group Get B", user.ID, []string{region.ID})
	defer s.cleanup([]string{user.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, nil)

	proposal, _ := s.connectionRepo.ProposeConnection(context.Background(), groupA.ID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupB.ID},
	})
	result, _ := s.connectionRepo.RespondToProposal(context.Background(), proposal.ID, groupB.ID, true)
	if result.ConnectionID == nil {
		t.Fatal("Expected connection to be formed")
	}
	defer s.cleanup(nil, nil, nil, []string{*result.ConnectionID})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/"+*result.ConnectionID+"?id="+*result.ConnectionID, nil)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.GetConnection(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestConnectionHandler_Get_NonMemberRejected(t *testing.T) {
	s := setupConnectionTestSuite(t)
	adminUser := s.createTestUser("conn_get_admin", 1, false)
	adminUser.VouchVerified = true
	outsideUser := s.createTestUser("conn_get_outside", 1, false)
	outsideUser.VouchVerified = true
	region := s.createTestRegion("Conn Get NM Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group Get NM A", adminUser.ID, []string{region.ID})
	groupB := s.createTestGroup("Group Get NM B", adminUser.ID, []string{region.ID})
	defer s.cleanup([]string{adminUser.ID, outsideUser.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, nil)

	proposal, _ := s.connectionRepo.ProposeConnection(context.Background(), groupA.ID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupB.ID},
	})
	result, _ := s.connectionRepo.RespondToProposal(context.Background(), proposal.ID, groupB.ID, true)
	if result.ConnectionID == nil {
		t.Fatal("Expected connection to be formed")
	}
	defer s.cleanup(nil, nil, nil, []string{*result.ConnectionID})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/"+*result.ConnectionID+"?id="+*result.ConnectionID, nil)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(outsideUser))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.GetConnection(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("Expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- Invite Tests ---

func TestConnectionHandler_Invite_Success(t *testing.T) {
	s := setupConnectionTestSuite(t)
	user := s.createTestUser("conn_invite_user", 1, false)
	user.VouchVerified = true
	region := s.createTestRegion("Conn Invite Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group Invite A", user.ID, []string{region.ID})
	groupB := s.createTestGroup("Group Invite B", user.ID, []string{region.ID})
	groupC := s.createTestGroup("Group Invite C", user.ID, []string{region.ID})
	defer s.cleanup([]string{user.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID, groupC.ID}, nil)

	// Form connection between A and B
	proposal, _ := s.connectionRepo.ProposeConnection(context.Background(), groupA.ID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupB.ID},
	})
	result, _ := s.connectionRepo.RespondToProposal(context.Background(), proposal.ID, groupB.ID, true)
	if result.ConnectionID == nil {
		t.Fatal("Expected connection to be formed")
	}
	defer s.cleanup(nil, nil, nil, []string{*result.ConnectionID})

	// Invite group C
	body, _ := json.Marshal(models.InviteToConnectionRequest{
		GroupID: groupC.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/"+*result.ConnectionID+"/invite?id="+*result.ConnectionID+"&proposer_group_id="+groupA.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.InviteToConnection(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var inviteResult models.ConnectionProposalWithGroups
	if err := json.NewDecoder(rr.Body).Decode(&inviteResult); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if inviteResult.ProposalType != "expansion" {
		t.Errorf("Expected proposal_type=expansion, got %s", inviteResult.ProposalType)
	}
}

// --- Leave Tests ---

func TestConnectionHandler_Leave_Success(t *testing.T) {
	s := setupConnectionTestSuite(t)
	user := s.createTestUser("conn_leave_user", 1, false)
	user.VouchVerified = true
	region := s.createTestRegion("Conn Leave Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group Leave A", user.ID, []string{region.ID})
	groupB := s.createTestGroup("Group Leave B", user.ID, []string{region.ID})
	groupC := s.createTestGroup("Group Leave C", user.ID, []string{region.ID})
	defer s.cleanup([]string{user.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID, groupC.ID}, nil)

	// Form 3-group connection: A+B first, then expand with C
	proposal, _ := s.connectionRepo.ProposeConnection(context.Background(), groupA.ID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupB.ID, groupC.ID},
	})
	_, _ = s.connectionRepo.RespondToProposal(context.Background(), proposal.ID, groupB.ID, true)
	result, _ := s.connectionRepo.RespondToProposal(context.Background(), proposal.ID, groupC.ID, true)
	if result.ConnectionID == nil {
		t.Fatal("Expected connection to be formed")
	}
	connectionID := *result.ConnectionID
	defer s.cleanup(nil, nil, nil, []string{connectionID})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/"+connectionID+"/leave?id="+connectionID+"&group_id="+groupA.ID, nil)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.LeaveConnection(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify connection still exists with 2 members
	conn, err := s.connectionRepo.GetConnection(context.Background(), connectionID)
	if err != nil {
		t.Fatalf("Expected connection to still exist: %v", err)
	}
	if len(conn.MemberGroups) != 2 {
		t.Errorf("Expected 2 remaining members, got %d", len(conn.MemberGroups))
	}
}

// --- Signal Chat Proposal Tests ---

func TestConnectionHandler_ProposeSignalChat_Success(t *testing.T) {
	s := setupConnectionTestSuite(t)
	user := s.createTestUser("conn_chat_propose_user", 1, false)
	user.VouchVerified = true
	region := s.createTestRegion("Conn Chat Propose Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group Chat A", user.ID, []string{region.ID})
	groupB := s.createTestGroup("Group Chat B", user.ID, []string{region.ID})
	defer s.cleanup([]string{user.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, nil)

	// Form connection
	proposal, _ := s.connectionRepo.ProposeConnection(context.Background(), groupA.ID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupB.ID},
	})
	result, _ := s.connectionRepo.RespondToProposal(context.Background(), proposal.ID, groupB.ID, true)
	if result.ConnectionID == nil {
		t.Fatal("Expected connection to be formed")
	}
	defer s.cleanup(nil, nil, nil, []string{*result.ConnectionID})

	body, _ := json.Marshal(models.ProposeConnectionChatRequest{
		GroupName:   "Test Chat",
		Description: "A test chat",
		AccessLevel: "all_members",
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/"+*result.ConnectionID+"/signal-group-proposals?id="+*result.ConnectionID+"&proposer_group_id="+groupA.ID,
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.ProposeSignalChat(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var chatProposal models.ConnectionChatProposalWithVotes
	if err := json.NewDecoder(rr.Body).Decode(&chatProposal); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if chatProposal.GroupName != "Test Chat" {
		t.Errorf("Expected group_name=Test Chat, got %s", chatProposal.GroupName)
	}
	if len(chatProposal.Votes) != 2 {
		t.Errorf("Expected 2 votes, got %d", len(chatProposal.Votes))
	}
}

func TestConnectionHandler_ProposeSignalChat_AdminOnly_RequiresEncryption(t *testing.T) {
	s := setupConnectionTestSuite(t)
	user := s.createTestUser("conn_chat_encrypt_user", 1, false)
	user.VouchVerified = true
	region := s.createTestRegion("Conn Chat Encrypt Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group Encrypt A", user.ID, []string{region.ID})
	groupB := s.createTestGroup("Group Encrypt B", user.ID, []string{region.ID})
	defer s.cleanup([]string{user.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, nil)

	// Form connection
	proposal, _ := s.connectionRepo.ProposeConnection(context.Background(), groupA.ID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupB.ID},
	})
	result, _ := s.connectionRepo.RespondToProposal(context.Background(), proposal.ID, groupB.ID, true)
	if result.ConnectionID == nil {
		t.Fatal("Expected connection to be formed")
	}
	connectionID := *result.ConnectionID
	defer s.cleanup(nil, nil, nil, []string{connectionID})

	// Test: admin_only without encryption fields should fail
	body, _ := json.Marshal(models.ProposeConnectionChatRequest{
		GroupName:   "Admin Chat",
		Description: "An admin chat",
		AccessLevel: "admin_only",
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/"+connectionID+"/signal-group-proposals?id="+connectionID+"&proposer_group_id="+groupA.ID,
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.ProposeSignalChat(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for missing encryption fields, got %d: %s", rr.Code, rr.Body.String())
	}

	// Test: admin_only with encryption fields should succeed
	payload := "encrypted_payload_data"
	iv := "initialization_vector"
	wrappedKeys := []models.WrappedKeyEntry{
		{UserID: "user1", WrappedDEK: "wrapped_dek_1"},
	}
	body, _ = json.Marshal(models.ProposeConnectionChatRequest{
		GroupName:        "Admin Chat",
		Description:      "An admin chat",
		AccessLevel:      "admin_only",
		EncryptedPayload: &payload,
		EncryptionIV:     &iv,
		WrappedKeys:      wrappedKeys,
	})
	req = httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/"+connectionID+"/signal-group-proposals?id="+connectionID+"&proposer_group_id="+groupA.ID,
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx = context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr = httptest.NewRecorder()
	s.handler.ProposeSignalChat(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Expected 201 with encryption fields, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestConnectionHandler_VoteOnChatProposal_Success(t *testing.T) {
	s := setupConnectionTestSuite(t)
	user := s.createTestUser("conn_chat_vote_user", 1, false)
	user.VouchVerified = true
	region := s.createTestRegion("Conn Chat Vote Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group Vote A", user.ID, []string{region.ID})
	groupB := s.createTestGroup("Group Vote B", user.ID, []string{region.ID})
	defer s.cleanup([]string{user.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, nil)

	// Form connection
	proposal, _ := s.connectionRepo.ProposeConnection(context.Background(), groupA.ID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupB.ID},
	})
	formResult, _ := s.connectionRepo.RespondToProposal(context.Background(), proposal.ID, groupB.ID, true)
	if formResult.ConnectionID == nil {
		t.Fatal("Expected connection to be formed")
	}
	connectionID := *formResult.ConnectionID
	defer s.cleanup(nil, nil, nil, []string{connectionID})

	// Propose chat
	chatProposal, err := s.connectionRepo.ProposeSignalChat(context.Background(), connectionID, groupA.ID, &models.ProposeConnectionChatRequest{
		GroupName:   "Vote Test Chat",
		AccessLevel: "all_members",
	})
	if err != nil {
		t.Fatalf("ProposeSignalChat failed: %v", err)
	}

	// Vote approve as B
	body, _ := json.Marshal(map[string]interface{}{
		"approve":  true,
		"group_id": groupB.ID,
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/connection-chat-proposals/"+chatProposal.ID+"/vote?id="+chatProposal.ID,
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.VoteOnChatProposal(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var voteResult models.ConnectionChatProposalWithVotes
	if err := json.NewDecoder(rr.Body).Decode(&voteResult); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if voteResult.Status != "approved" {
		t.Errorf("Expected status=approved, got %s", voteResult.Status)
	}

	// Verify signal group was created
	signalGroups, err := s.connectionRepo.ListConnectionSignalGroups(context.Background(), connectionID)
	if err != nil {
		t.Fatalf("ListConnectionSignalGroups failed: %v", err)
	}
	if len(signalGroups) != 1 {
		t.Fatalf("Expected 1 signal group, got %d", len(signalGroups))
	}
}

func TestConnectionHandler_ListConnectionSignalGroups(t *testing.T) {
	s := setupConnectionTestSuite(t)
	user := s.createTestUser("conn_list_sg_user", 1, false)
	user.VouchVerified = true
	region := s.createTestRegion("Conn List SG Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group List SG A", user.ID, []string{region.ID})
	groupB := s.createTestGroup("Group List SG B", user.ID, []string{region.ID})
	defer s.cleanup([]string{user.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, nil)

	// Form connection
	proposal, _ := s.connectionRepo.ProposeConnection(context.Background(), groupA.ID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupB.ID},
	})
	formResult, _ := s.connectionRepo.RespondToProposal(context.Background(), proposal.ID, groupB.ID, true)
	if formResult.ConnectionID == nil {
		t.Fatal("Expected connection to be formed")
	}
	connectionID := *formResult.ConnectionID
	defer s.cleanup(nil, nil, nil, []string{connectionID})

	// Propose and approve chat
	chatProposal, _ := s.connectionRepo.ProposeSignalChat(context.Background(), connectionID, groupA.ID, &models.ProposeConnectionChatRequest{
		GroupName:   "List Test Chat",
		AccessLevel: "admin_only",
	})
	_, _ = s.connectionRepo.VoteOnChatProposal(context.Background(), chatProposal.ID, groupB.ID, true)

	// List signal groups via handler
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/"+connectionID+"/signal-groups?id="+connectionID, nil)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.ListConnectionSignalGroups(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	signalGroups, ok := response["signal_groups"].([]interface{})
	if !ok {
		t.Fatal("Expected signal_groups array in response")
	}
	if len(signalGroups) != 1 {
		t.Errorf("Expected 1 signal group, got %d", len(signalGroups))
	}
}

// --- Shared Resource Tests ---

func (s *ConnectionTestSuite) createTestResource(groupID, userID, title string) *models.GroupResource {
	ctx := context.Background()
	req := &models.CreateResourceRequest{
		Title:      title,
		URL:        "https://example.com/" + title,
		AccessTier: "member",
	}
	resource, err := s.groupRepo.CreateResource(ctx, groupID, userID, req)
	if err != nil {
		s.t.Fatalf("Failed to create test resource: %v", err)
	}
	return resource
}

func (s *ConnectionTestSuite) formConnection(groupAID, groupBID string) string {
	ctx := context.Background()
	proposal, err := s.connectionRepo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	if err != nil {
		s.t.Fatalf("Failed to propose connection: %v", err)
	}
	result, err := s.connectionRepo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	if err != nil {
		s.t.Fatalf("Failed to accept proposal: %v", err)
	}
	if result.ConnectionID == nil {
		s.t.Fatal("Expected connection to be formed")
	}
	return *result.ConnectionID
}

func TestConnectionHandler_ShareResource_Success(t *testing.T) {
	s := setupConnectionTestSuite(t)
	user := s.createTestUser("conn_share_res_user", 1, false)
	user.VouchVerified = true
	region := s.createTestRegion("Conn Share Res Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group Share Res A", user.ID, []string{region.ID})
	groupB := s.createTestGroup("Group Share Res B", user.ID, []string{region.ID})
	connectionID := s.formConnection(groupA.ID, groupB.ID)
	resource := s.createTestResource(groupA.ID, user.ID, "Shared Resource")
	defer s.cleanup([]string{user.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, []string{connectionID})

	body, _ := json.Marshal(models.ShareResourceRequest{
		ResourceID: resource.ID,
		Visibility: "all_members",
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/"+connectionID+"/shared-resources?id="+connectionID+"&proposer_group_id="+groupA.ID,
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.ShareResource(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var result models.ConnectionSharedResource
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if result.ResourceID != resource.ID {
		t.Errorf("Expected resource_id=%s, got %s", resource.ID, result.ResourceID)
	}
	if result.Visibility != "all_members" {
		t.Errorf("Expected visibility=all_members, got %s", result.Visibility)
	}
}

func TestConnectionHandler_ShareResource_Duplicate(t *testing.T) {
	s := setupConnectionTestSuite(t)
	user := s.createTestUser("conn_share_dup_user", 1, false)
	user.VouchVerified = true
	region := s.createTestRegion("Conn Share Dup Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group Share Dup A", user.ID, []string{region.ID})
	groupB := s.createTestGroup("Group Share Dup B", user.ID, []string{region.ID})
	connectionID := s.formConnection(groupA.ID, groupB.ID)
	resource := s.createTestResource(groupA.ID, user.ID, "Dup Resource")
	defer s.cleanup([]string{user.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, []string{connectionID})

	// Share once
	_, err := s.connectionRepo.ShareResource(context.Background(), connectionID, groupA.ID, &models.ShareResourceRequest{
		ResourceID: resource.ID,
		Visibility: "admin_only",
	})
	if err != nil {
		t.Fatalf("First share failed: %v", err)
	}

	// Attempt duplicate via handler
	body, _ := json.Marshal(models.ShareResourceRequest{
		ResourceID: resource.ID,
		Visibility: "admin_only",
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/"+connectionID+"/shared-resources?id="+connectionID+"&proposer_group_id="+groupA.ID,
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.ShareResource(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("Expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestConnectionHandler_ShareResource_NonMemberRejected(t *testing.T) {
	s := setupConnectionTestSuite(t)
	admin := s.createTestUser("conn_share_nm_admin", 1, false)
	admin.VouchVerified = true
	outsider := s.createTestUser("conn_share_nm_outsider", 1, false)
	outsider.VouchVerified = true
	region := s.createTestRegion("Conn Share NM Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group Share NM A", admin.ID, []string{region.ID})
	groupB := s.createTestGroup("Group Share NM B", admin.ID, []string{region.ID})
	outsiderGroup := s.createTestGroup("Group Share NM Out", outsider.ID, []string{region.ID})
	connectionID := s.formConnection(groupA.ID, groupB.ID)
	resource := s.createTestResource(outsiderGroup.ID, outsider.ID, "NM Resource")
	defer s.cleanup([]string{admin.ID, outsider.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID, outsiderGroup.ID}, []string{connectionID})

	body, _ := json.Marshal(models.ShareResourceRequest{
		ResourceID: resource.ID,
		Visibility: "all_members",
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/"+connectionID+"/shared-resources?id="+connectionID+"&proposer_group_id="+outsiderGroup.ID,
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(outsider))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.ShareResource(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("Expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestConnectionHandler_UnshareResource_Success(t *testing.T) {
	s := setupConnectionTestSuite(t)
	user := s.createTestUser("conn_unshare_user", 1, false)
	user.VouchVerified = true
	region := s.createTestRegion("Conn Unshare Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group Unshare A", user.ID, []string{region.ID})
	groupB := s.createTestGroup("Group Unshare B", user.ID, []string{region.ID})
	connectionID := s.formConnection(groupA.ID, groupB.ID)
	resource := s.createTestResource(groupA.ID, user.ID, "Unshare Resource")
	defer s.cleanup([]string{user.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, []string{connectionID})

	// Share first
	_, err := s.connectionRepo.ShareResource(context.Background(), connectionID, groupA.ID, &models.ShareResourceRequest{
		ResourceID: resource.ID,
		Visibility: "all_members",
	})
	if err != nil {
		t.Fatalf("Share failed: %v", err)
	}

	// Unshare via handler
	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/connections/"+connectionID+"/shared-resources/"+resource.ID+"?id="+connectionID+"&resource_id="+resource.ID,
		nil)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(user))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.UnshareResource(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify it's gone
	resources, err := s.connectionRepo.ListConnectionResources(context.Background(), connectionID, true)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("Expected 0 resources after unshare, got %d", len(resources))
	}
}

func TestConnectionHandler_ListConnectionResources_VisibilityFiltering(t *testing.T) {
	s := setupConnectionTestSuite(t)
	adminUser := s.createTestUser("conn_list_res_admin", 1, false)
	adminUser.VouchVerified = true
	region := s.createTestRegion("Conn List Res Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group List Res A", adminUser.ID, []string{region.ID})
	groupB := s.createTestGroup("Group List Res B", adminUser.ID, []string{region.ID})
	connectionID := s.formConnection(groupA.ID, groupB.ID)
	adminResource := s.createTestResource(groupA.ID, adminUser.ID, "Admin Only Resource")
	memberResource := s.createTestResource(groupA.ID, adminUser.ID, "All Members Resource")
	defer s.cleanup([]string{adminUser.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, []string{connectionID})

	// Share both resources
	_, err := s.connectionRepo.ShareResource(context.Background(), connectionID, groupA.ID, &models.ShareResourceRequest{
		ResourceID: adminResource.ID,
		Visibility: "admin_only",
	})
	if err != nil {
		t.Fatalf("Share admin resource failed: %v", err)
	}
	_, err = s.connectionRepo.ShareResource(context.Background(), connectionID, groupA.ID, &models.ShareResourceRequest{
		ResourceID: memberResource.ID,
		Visibility: "all_members",
	})
	if err != nil {
		t.Fatalf("Share member resource failed: %v", err)
	}

	// Admin should see both
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/"+connectionID+"/shared-resources?id="+connectionID, nil)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(adminUser))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.ListConnectionResources(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	resources, ok := response["shared_resources"].([]interface{})
	if !ok {
		t.Fatal("Expected shared_resources array in response")
	}
	if len(resources) != 2 {
		t.Errorf("Admin should see 2 resources, got %d", len(resources))
	}

	// Test repo-level visibility filtering directly (non-admin sees only all_members)
	nonAdminResources, err := s.connectionRepo.ListConnectionResources(context.Background(), connectionID, false)
	if err != nil {
		t.Fatalf("ListConnectionResources (non-admin) failed: %v", err)
	}
	if len(nonAdminResources) != 1 {
		t.Errorf("Non-admin should see 1 resource, got %d", len(nonAdminResources))
	}
	if len(nonAdminResources) > 0 && nonAdminResources[0].Visibility != "all_members" {
		t.Errorf("Non-admin resource should be all_members, got %s", nonAdminResources[0].Visibility)
	}
}

// TestConnectionHandler_GetConnection_NonAdminMemberAllowed verifies the access
// model is consistent: a non-admin member of a connected group can fetch
// connection detail (so the detail page renders), while a non-member gets 403.
func TestConnectionHandler_GetConnection_NonAdminMemberAllowed(t *testing.T) {
	s := setupConnectionTestSuite(t)
	admin := s.createTestUser("conn_get_nm_admin", 1, false)
	admin.VouchVerified = true
	member := s.createTestUser("conn_get_nm_member", 1, false)
	member.VouchVerified = true
	outsider := s.createTestUser("conn_get_nm_outsider", 1, false)
	outsider.VouchVerified = true
	region := s.createTestRegion("Conn Get NM Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group Get NM A", admin.ID, []string{region.ID})
	groupB := s.createTestGroup("Group Get NM B", admin.ID, []string{region.ID})
	connectionID := s.formConnection(groupA.ID, groupB.ID)
	defer s.cleanup([]string{admin.ID, member.ID, outsider.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, []string{connectionID})

	if err := s.groupRepo.AddMember(context.Background(), groupA.ID, member.ID, false, false); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	// Non-admin member of a connected group: 200.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/"+connectionID+"?id="+connectionID, nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(member)))
	rr := httptest.NewRecorder()
	s.handler.GetConnection(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("non-admin member expected 200 from GetConnection, got %d: %s", rr.Code, rr.Body.String())
	}

	// Non-member of any connected group: 403.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/connections/"+connectionID+"?id="+connectionID, nil)
	req2 = req2.WithContext(context.WithValue(req2.Context(), middleware.UserContextKey, s.claimsForUser(outsider)))
	rr2 := httptest.NewRecorder()
	s.handler.GetConnection(rr2, req2)
	if rr2.Code != http.StatusForbidden {
		t.Fatalf("non-member expected 403 from GetConnection, got %d", rr2.Code)
	}
}

// TestConnectionHandler_ListConnectionSignalGroups_TierFiltering verifies a
// non-admin member sees only all_members (member-tier) chats, while an admin sees
// admin_only chats too.
func TestConnectionHandler_ListConnectionSignalGroups_TierFiltering(t *testing.T) {
	s := setupConnectionTestSuite(t)
	ctx := context.Background()
	admin := s.createTestUser("conn_sg_tier_admin", 1, false)
	admin.VouchVerified = true
	member := s.createTestUser("conn_sg_tier_member", 1, false)
	member.VouchVerified = true
	region := s.createTestRegion("Conn SG Tier Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group SG Tier A", admin.ID, []string{region.ID})
	groupB := s.createTestGroup("Group SG Tier B", admin.ID, []string{region.ID})
	connectionID := s.formConnection(groupA.ID, groupB.ID)
	defer s.cleanup([]string{admin.ID, member.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, []string{connectionID})

	if err := s.groupRepo.AddMember(ctx, groupA.ID, member.ID, false, false); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	// Two connection chats: one member-tier (all_members), one admin_only.
	for _, c := range []struct{ name, tier string }{
		{"Members Chat", "member"},
		{"Admins Chat", "admin_only"},
	} {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO signal_groups (id, region_id, school_id, district_id, owner_group_id, connection_id, group_name, access_tier, created_by, created_at, is_active)
			 VALUES (UUID(), NULL, NULL, NULL, NULL, ?, ?, ?, NULL, NOW(), TRUE)`,
			connectionID, c.name, c.tier)
		if err != nil {
			t.Fatalf("insert connection chat %q failed: %v", c.name, err)
		}
	}

	listAs := func(u *models.User) []interface{} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/"+connectionID+"/signal-groups?id="+connectionID, nil)
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(u)))
		rr := httptest.NewRecorder()
		s.handler.ListConnectionSignalGroups(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var body map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&body)
		sgs, _ := body["signal_groups"].([]interface{})
		return sgs
	}

	// Admin sees both chats.
	if got := len(listAs(admin)); got != 2 {
		t.Errorf("admin should see 2 connection chats, got %d", got)
	}
	// Non-admin member sees only the member-tier chat.
	memberChats := listAs(member)
	if len(memberChats) != 1 {
		t.Fatalf("non-admin member should see 1 (member-tier) chat, got %d", len(memberChats))
	}
	if tier := memberChats[0].(map[string]interface{})["access_tier"]; tier == "admin_only" {
		t.Errorf("non-admin member must not see admin_only chat")
	}
}

// TestConnectionHandler_ListConnectionResources_NonAdminMemberAllowed verifies a
// regular (non-admin) member of a connected group reaches the handler (not 403)
// and receives only all_members resources.
func TestConnectionHandler_ListConnectionResources_NonAdminMemberAllowed(t *testing.T) {
	s := setupConnectionTestSuite(t)
	adminUser := s.createTestUser("conn_list_res_nm_admin", 1, false)
	adminUser.VouchVerified = true
	memberUser := s.createTestUser("conn_list_res_nm_member", 1, false)
	memberUser.VouchVerified = true
	region := s.createTestRegion("Conn List Res NM Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group List Res NM A", adminUser.ID, []string{region.ID})
	groupB := s.createTestGroup("Group List Res NM B", adminUser.ID, []string{region.ID})
	connectionID := s.formConnection(groupA.ID, groupB.ID)
	adminResource := s.createTestResource(groupA.ID, adminUser.ID, "Admin Only Resource NM")
	memberResource := s.createTestResource(groupA.ID, adminUser.ID, "All Members Resource NM")
	defer s.cleanup([]string{adminUser.ID, memberUser.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, []string{connectionID})

	// memberUser is a non-admin member of connected group A.
	if err := s.groupRepo.AddMember(context.Background(), groupA.ID, memberUser.ID, false, false); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	_, err := s.connectionRepo.ShareResource(context.Background(), connectionID, groupA.ID, &models.ShareResourceRequest{
		ResourceID: adminResource.ID,
		Visibility: "admin_only",
	})
	if err != nil {
		t.Fatalf("Share admin resource failed: %v", err)
	}
	_, err = s.connectionRepo.ShareResource(context.Background(), connectionID, groupA.ID, &models.ShareResourceRequest{
		ResourceID: memberResource.ID,
		Visibility: "all_members",
	})
	if err != nil {
		t.Fatalf("Share member resource failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/"+connectionID+"/shared-resources?id="+connectionID, nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(memberUser)))

	rr := httptest.NewRecorder()
	s.handler.ListConnectionResources(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Non-admin member expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	resources, ok := response["shared_resources"].([]interface{})
	if !ok {
		t.Fatal("Expected shared_resources array in response")
	}
	if len(resources) != 1 {
		t.Fatalf("Non-admin member should see 1 (all_members) resource, got %d", len(resources))
	}
	res := resources[0].(map[string]interface{})
	if res["visibility"] != "all_members" {
		t.Errorf("Non-admin member should only see all_members resource, got visibility=%v", res["visibility"])
	}
}

// TestConnectionHandler_ListConnectionResources_NonMemberRejected verifies a user
// who belongs to no connected group is rejected with 403.
func TestConnectionHandler_ListConnectionResources_NonMemberRejected(t *testing.T) {
	s := setupConnectionTestSuite(t)
	adminUser := s.createTestUser("conn_list_res_nonmem_admin", 1, false)
	adminUser.VouchVerified = true
	outsider := s.createTestUser("conn_list_res_outsider", 1, false)
	outsider.VouchVerified = true
	region := s.createTestRegion("Conn List Res Outsider Region", models.RegionTypeState, nil)
	groupA := s.createTestGroup("Group List Res Outsider A", adminUser.ID, []string{region.ID})
	groupB := s.createTestGroup("Group List Res Outsider B", adminUser.ID, []string{region.ID})
	connectionID := s.formConnection(groupA.ID, groupB.ID)
	defer s.cleanup([]string{adminUser.ID, outsider.ID}, []string{region.ID}, []string{groupA.ID, groupB.ID}, []string{connectionID})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/"+connectionID+"/shared-resources?id="+connectionID, nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, s.claimsForUser(outsider)))

	rr := httptest.NewRecorder()
	s.handler.ListConnectionResources(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("Non-member expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}
