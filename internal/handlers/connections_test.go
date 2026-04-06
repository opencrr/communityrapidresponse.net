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
	connectionHandler := NewConnectionHandler(connectionRepo, groupRepo, auditRepo)
	groupHandler := NewGroupHandler(groupRepo, signalGroupRepo, regionRepo, userRepo, auditRepo)

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
	defer s.cleanup(nil, nil, nil, []string{connectionID})
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
		AccessLevel: "admin_only",
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
