package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

type signalGroupTestSuite struct {
	t            *testing.T
	db           *database.DB
	userRepo     *database.UserRepository
	regionRepo   *database.RegionRepository
	groupRepo    *database.SignalGroupRepository
	proposalRepo *database.InviteLinkProposalRepository
	handler      *SignalGroupHandler
}

func setupSignalGroupTestSuite(t *testing.T) *signalGroupTestSuite {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	regionRepo := database.NewRegionRepository(db)
	groupRepo := database.NewSignalGroupRepository(db)
	proposalRepo := database.NewInviteLinkProposalRepository(db)
	consensusConfig := &config.ConsensusConfig{VotePercent: 50, VoteFloor: 3}
	handler := NewSignalGroupHandler(nil, groupRepo, proposalRepo, regionRepo, nil, consensusConfig)

	return &signalGroupTestSuite{
		t:            t,
		db:           db,
		userRepo:     userRepo,
		regionRepo:   regionRepo,
		groupRepo:    groupRepo,
		proposalRepo: proposalRepo,
		handler:      handler,
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
		_, _ = s.db.ExecContext(ctx, "DELETE FROM invite_link_update_votes WHERE proposal_id IN (SELECT id FROM invite_link_update_proposals WHERE signal_group_id = ?)", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM invite_link_update_proposals WHERE signal_group_id = ?", id)
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
		body := map[string]string{
			"region_id":   region.ID,
			"name":        "Test Signal Group",
			"invite_link": "https://signal.group/test123",
			"description": "A test group",
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
		body := map[string]string{
			"region_id":   region.ID,
			"name":        "Unauthorized Group",
			"invite_link": "https://signal.group/unauth",
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
			// Missing group_name and invite_link
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
		body := map[string]string{
			"region_id":   region.ID,
			"name":        "Unauth Group",
			"invite_link": "https://signal.group/unauth",
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
				RegionID:   &region.ID,
				GroupName:  "Limit Test Group",
				InviteLink: "https://signal.group/limit",
				CreatedBy:  &adminUser.ID,
			}
			_ = suite.groupRepo.Create(context.Background(), group)
		}

		body := map[string]string{
			"region_id":   region.ID,
			"name":        "Over Limit Group",
			"invite_link": "https://signal.group/overlimit",
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
		RegionID:   &region.ID,
		GroupName:  "List Test Group",
		InviteLink: "https://signal.group/listtest",
		CreatedBy:  &verifiedUser.ID,
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
		RegionID:   &region.ID,
		GroupName:  "Original Name",
		InviteLink: "https://signal.group/original",
		CreatedBy:  &adminUser.ID,
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
			"name":       "Unauthorized Update",
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
			"name":       "Update Non-existent",
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
// CreateInviteLinkProposal Tests
// =============================================================================

func TestSignalGroupHandler_CreateInviteLinkProposal(t *testing.T) {
	suite := setupSignalGroupTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@signalgrouptest.com'")

	// Create 3 admin users for consensus
	admin1 := suite.createTestUser("sg_proposal_admin1", models.TierPostcard)
	admin2 := suite.createTestUser("sg_proposal_admin2", models.TierPostcard)
	admin3 := suite.createTestUser("sg_proposal_admin3", models.TierPostcard)
	regularUser := suite.createTestUser("sg_proposal_regular", models.TierPostcard)
	region := suite.createTestRegion("Proposal Test Region")

	suite.addUserToRegion(admin1.ID, region.ID, true)
	suite.addUserToRegion(admin2.ID, region.ID, true)
	suite.addUserToRegion(admin3.ID, region.ID, true)
	suite.addUserToRegion(regularUser.ID, region.ID, false)

	group := &models.SignalGroup{
		RegionID:   &region.ID,
		GroupName:  "Proposal Test Group",
		InviteLink: "https://signal.group/original",
		CreatedBy:  &admin1.ID,
	}
	_ = suite.groupRepo.Create(context.Background(), group)

	defer suite.cleanup([]string{admin1.ID, admin2.ID, admin3.ID, regularUser.ID}, []string{region.ID}, []string{group.ID})

	t.Run("admin can create invite link proposal", func(t *testing.T) {
		body := map[string]string{
			"new_invite_link": "https://signal.group/newlink",
			"reason":          "Old link was compromised",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/"+group.ID+"/invite-link-proposals", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", group.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateInviteLinkProposal(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.InviteLinkProposalResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody.ProposalID == "" {
			t.Error("Expected proposal_id in response")
		}
		if respBody.VotesNeeded != 3 {
			t.Errorf("Expected votes_needed to be 3, got %d", respBody.VotesNeeded)
		}
		if respBody.CurrentVotes != 1 {
			t.Errorf("Expected current_votes to be 1 (proposer's vote), got %d", respBody.CurrentVotes)
		}

		// Clean up proposal
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", respBody.ProposalID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", respBody.ProposalID)
	})

	t.Run("non-admin cannot create proposal", func(t *testing.T) {
		body := map[string]string{
			"new_invite_link": "https://signal.group/unauthorized",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/"+group.ID+"/invite-link-proposals", bytes.NewReader(bodyBytes))
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
		suite.handler.CreateInviteLinkProposal(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})

	t.Run("missing invite link fails", func(t *testing.T) {
		body := map[string]string{
			"reason": "No link provided",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/"+group.ID+"/invite-link-proposals", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", group.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateInviteLinkProposal(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("insufficient admins prevents proposal", func(t *testing.T) {
		// Create a region with only 1 admin
		smallRegion := suite.createTestRegion("Small Region")
		singleAdmin := suite.createTestUser("sg_single_admin", models.TierPostcard)
		suite.addUserToRegion(singleAdmin.ID, smallRegion.ID, true)

		smallGroup := &models.SignalGroup{
			RegionID:   &smallRegion.ID,
			GroupName:  "Small Group",
			InviteLink: "https://signal.group/small",
			CreatedBy:  &singleAdmin.ID,
		}
		_ = suite.groupRepo.Create(context.Background(), smallGroup)

		defer suite.cleanup([]string{singleAdmin.ID}, []string{smallRegion.ID}, []string{smallGroup.ID})

		body := map[string]string{
			"new_invite_link": "https://signal.group/newsmall",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/"+smallGroup.ID+"/invite-link-proposals", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", smallGroup.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           singleAdmin.ID,
			Email:            singleAdmin.Email,
			VerificationTier: singleAdmin.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateInviteLinkProposal(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["error"] != "insufficient_admins" {
			t.Errorf("Expected error 'insufficient_admins', got %v", respBody["error"])
		}
	})
}

// =============================================================================
// VoteOnInviteLinkProposal Tests
// =============================================================================

func TestSignalGroupHandler_VoteOnInviteLinkProposal(t *testing.T) {
	suite := setupSignalGroupTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@signalgrouptest.com'")

	admin1 := suite.createTestUser("sg_vote_admin1", models.TierPostcard)
	admin2 := suite.createTestUser("sg_vote_admin2", models.TierPostcard)
	admin3 := suite.createTestUser("sg_vote_admin3", models.TierPostcard)
	regularUser := suite.createTestUser("sg_vote_regular", models.TierPostcard)
	region := suite.createTestRegion("Vote Test Region")

	suite.addUserToRegion(admin1.ID, region.ID, true)
	suite.addUserToRegion(admin2.ID, region.ID, true)
	suite.addUserToRegion(admin3.ID, region.ID, true)
	suite.addUserToRegion(regularUser.ID, region.ID, false)

	group := &models.SignalGroup{
		RegionID:   &region.ID,
		GroupName:  "Vote Test Group",
		InviteLink: "https://signal.group/votetest",
		CreatedBy:  &admin1.ID,
	}
	_ = suite.groupRepo.Create(context.Background(), group)

	defer suite.cleanup([]string{admin1.ID, admin2.ID, admin3.ID, regularUser.ID}, []string{region.ID}, []string{group.ID})

	t.Run("admin can vote on proposal", func(t *testing.T) {
		// Create a proposal first
		reason := "Test proposal"
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/voted",
			Reason:        &reason,
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		body := map[string]bool{
			"vote": true,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin2.ID,
			Email:            admin2.Email,
			VerificationTier: admin2.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnInviteLinkProposal(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.InviteLinkProposalResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody.CurrentVotes < 1 {
			t.Error("Expected at least 1 vote")
		}
	})

	t.Run("non-admin cannot vote", func(t *testing.T) {
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/nonadmin",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		body := map[string]bool{
			"vote": true,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           regularUser.ID,
			Email:            regularUser.Email,
			VerificationTier: regularUser.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnInviteLinkProposal(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})

	t.Run("cannot vote twice", func(t *testing.T) {
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/doublevote",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin2.ID, true)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		body := map[string]bool{
			"vote": true,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin2.ID,
			Email:            admin2.Email,
			VerificationTier: admin2.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnInviteLinkProposal(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d", rec.Code)
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["error"] != "already_voted" {
			t.Errorf("Expected error 'already_voted', got %v", respBody["error"])
		}
	})

	t.Run("consensus updates invite link", func(t *testing.T) {
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/consensus",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		// Add 2 approval votes (proposer + 1 more = 2, need 3rd)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin1.ID, true)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin2.ID, true)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		body := map[string]bool{
			"vote": true,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin3.ID,
			Email:            admin3.Email,
			VerificationTier: admin3.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnInviteLinkProposal(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.InviteLinkProposalResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody.Status != string(models.ProposalStatusApproved) {
			t.Errorf("Expected status 'approved', got %s", respBody.Status)
		}
		if !respBody.InviteLinkUpdated {
			t.Error("Expected invite_link_updated to be true")
		}
	})

	t.Run("consensus vote updates signal group invite link in database", func(t *testing.T) {
		// Create a fresh group for this test
		testGroup := &models.SignalGroup{
			RegionID:   &region.ID,
			GroupName:  "Consensus DB Test Group",
			InviteLink: "https://signal.group/original-link",
			CreatedBy:  &admin1.ID,
		}
		_ = suite.groupRepo.Create(context.Background(), testGroup)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM signal_groups WHERE id = ?", testGroup.ID)
		}()

		newInviteLink := "https://signal.group/updated-after-consensus"
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: testGroup.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: newInviteLink,
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin1.ID, true)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin2.ID, true)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		// Cast the deciding vote
		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin3.ID,
			Email:            admin3.Email,
			VerificationTier: admin3.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnInviteLinkProposal(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify the signal group's invite link was updated in the database
		updatedGroup, err := suite.groupRepo.GetByID(context.Background(), testGroup.ID)
		if err != nil {
			t.Fatalf("Failed to get updated group: %v", err)
		}
		if updatedGroup.InviteLink != newInviteLink {
			t.Errorf("Expected invite link to be '%s', got '%s'", newInviteLink, updatedGroup.InviteLink)
		}

		// Verify invite_link_updated_by was set to the voter who triggered consensus
		if updatedGroup.InviteLinkUpdatedBy == nil || *updatedGroup.InviteLinkUpdatedBy != admin3.ID {
			t.Errorf("Expected invite_link_updated_by to be '%s', got '%v'", admin3.ID, updatedGroup.InviteLinkUpdatedBy)
		}
	})

	t.Run("consensus vote updates proposal status to approved in database", func(t *testing.T) {
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/status-check",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin1.ID, true)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin2.ID, true)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		// Verify proposal is pending before the deciding vote
		pendingProposal, _ := suite.proposalRepo.GetByID(context.Background(), proposal.ID)
		if pendingProposal.Status != models.ProposalStatusPending {
			t.Errorf("Expected proposal status to be 'pending' before consensus, got '%s'", pendingProposal.Status)
		}

		// Cast the deciding vote
		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin3.ID,
			Email:            admin3.Email,
			VerificationTier: admin3.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnInviteLinkProposal(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify proposal status is approved in database
		approvedProposal, err := suite.proposalRepo.GetByID(context.Background(), proposal.ID)
		if err != nil {
			t.Fatalf("Failed to get proposal: %v", err)
		}
		if approvedProposal.Status != models.ProposalStatusApproved {
			t.Errorf("Expected proposal status to be 'approved', got '%s'", approvedProposal.Status)
		}

		// Verify resolved_at timestamp is set
		if approvedProposal.ResolvedAt == nil {
			t.Error("Expected resolved_at to be set after approval")
		}
	})

	t.Run("consensus vote records all three votes in database", func(t *testing.T) {
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/vote-count-check",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin1.ID, true)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin2.ID, true)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		// Cast the deciding vote
		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin3.ID,
			Email:            admin3.Email,
			VerificationTier: admin3.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnInviteLinkProposal(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify all 3 votes are recorded
		voteCount, err := suite.proposalRepo.CountApprovalVotes(context.Background(), proposal.ID)
		if err != nil {
			t.Fatalf("Failed to count votes: %v", err)
		}
		if voteCount != 3 {
			t.Errorf("Expected 3 approval votes, got %d", voteCount)
		}

		// Verify each admin's vote is recorded
		for _, adminID := range []string{admin1.ID, admin2.ID, admin3.ID} {
			hasVoted, err := suite.proposalRepo.HasVoted(context.Background(), proposal.ID, adminID)
			if err != nil {
				t.Fatalf("Failed to check vote for admin %s: %v", adminID, err)
			}
			if !hasVoted {
				t.Errorf("Expected admin %s to have voted", adminID)
			}
		}
	})

	t.Run("proposal cannot receive votes after approval", func(t *testing.T) {
		// Create a 4th admin for this test
		admin4 := suite.createTestUser("sg_vote_admin4", models.TierPostcard)
		suite.addUserToRegion(admin4.ID, region.ID, true)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", admin4.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", admin4.ID)
		}()

		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/no-more-votes",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin1.ID, true)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin2.ID, true)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin3.ID, true)
		// Manually set status to approved (simulating consensus was reached)
		_ = suite.proposalRepo.UpdateStatus(context.Background(), proposal.ID, models.ProposalStatusApproved)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		// Try to vote on the already-approved proposal
		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin4.ID,
			Email:            admin4.Email,
			VerificationTier: admin4.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnInviteLinkProposal(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["error"] != "proposal_closed" {
			t.Errorf("Expected error 'proposal_closed', got %v", respBody["error"])
		}
	})

	t.Run("rejection vote does not trigger consensus", func(t *testing.T) {
		testGroup := &models.SignalGroup{
			RegionID:   &region.ID,
			GroupName:  "Rejection Test Group",
			InviteLink: "https://signal.group/should-not-change",
			CreatedBy:  &admin1.ID,
		}
		_ = suite.groupRepo.Create(context.Background(), testGroup)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM signal_groups WHERE id = ?", testGroup.ID)
		}()

		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: testGroup.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/rejected-link",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin1.ID, true)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin2.ID, true)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		// Cast a rejection vote (vote: false)
		body := map[string]bool{"vote": false}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin3.ID,
			Email:            admin3.Email,
			VerificationTier: admin3.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnInviteLinkProposal(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.InviteLinkProposalResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		// Proposal should still be pending (rejection doesn't approve)
		if respBody.Status != string(models.ProposalStatusPending) {
			t.Errorf("Expected status 'pending' after rejection vote, got '%s'", respBody.Status)
		}
		if respBody.InviteLinkUpdated {
			t.Error("Expected invite_link_updated to be false after rejection vote")
		}

		// Verify signal group invite link was NOT changed
		unchangedGroup, _ := suite.groupRepo.GetByID(context.Background(), testGroup.ID)
		if unchangedGroup.InviteLink != "https://signal.group/should-not-change" {
			t.Errorf("Expected invite link to remain unchanged, got '%s'", unchangedGroup.InviteLink)
		}
	})
}

// =============================================================================
// ExpireInviteLinkProposal Tests
// =============================================================================

func TestSignalGroupHandler_ExpireInviteLinkProposal(t *testing.T) {
	suite := setupSignalGroupTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@signalgrouptest.com'")

	admin1 := suite.createTestUser("sg_expire_admin1", models.TierPostcard)
	admin2 := suite.createTestUser("sg_expire_admin2", models.TierPostcard)
	admin3 := suite.createTestUser("sg_expire_admin3", models.TierPostcard)
	superuser := suite.createTestUser("sg_expire_superuser", models.TierPostcard)
	_, _ = suite.db.ExecContext(context.Background(), "UPDATE users SET is_superuser = TRUE WHERE id = ?", superuser.ID)

	region := suite.createTestRegion("Expire Proposal Test Region")

	suite.addUserToRegion(admin1.ID, region.ID, true)
	suite.addUserToRegion(admin2.ID, region.ID, true)
	suite.addUserToRegion(admin3.ID, region.ID, true)

	group := &models.SignalGroup{
		RegionID:   &region.ID,
		GroupName:  "Expire Test Group",
		InviteLink: "https://signal.group/expiretest",
		CreatedBy:  &admin1.ID,
	}
	_ = suite.groupRepo.Create(context.Background(), group)

	defer suite.cleanup([]string{admin1.ID, admin2.ID, admin3.ID, superuser.ID}, []string{region.ID}, []string{group.ID})

	t.Run("superuser can expire pending proposal", func(t *testing.T) {
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/to-be-expired",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/expire", nil)
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           superuser.ID,
			Email:            superuser.Email,
			VerificationTier: superuser.VerificationTier,
			IsSuperuser:      true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ExpireInviteLinkProposal(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["status"] != "expired" {
			t.Errorf("Expected status 'expired', got %v", respBody["status"])
		}

		// Verify proposal is expired in database
		expiredProposal, _ := suite.proposalRepo.GetByID(context.Background(), proposal.ID)
		if expiredProposal.Status != models.ProposalStatusExpired {
			t.Errorf("Expected proposal status to be 'expired' in DB, got '%s'", expiredProposal.Status)
		}
		if expiredProposal.ResolvedAt == nil {
			t.Error("Expected resolved_at to be set")
		}
	})

	t.Run("non-superuser cannot expire proposal", func(t *testing.T) {
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/admin-cannot-expire",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/expire", nil)
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		// Admin (not superuser) tries to expire
		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
			IsSuperuser:      false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ExpireInviteLinkProposal(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify proposal is still pending
		stillPending, _ := suite.proposalRepo.GetByID(context.Background(), proposal.ID)
		if stillPending.Status != models.ProposalStatusPending {
			t.Errorf("Expected proposal to still be 'pending', got '%s'", stillPending.Status)
		}
	})

	t.Run("cannot expire already approved proposal", func(t *testing.T) {
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/already-approved",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		_ = suite.proposalRepo.UpdateStatus(context.Background(), proposal.ID, models.ProposalStatusApproved)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/expire", nil)
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           superuser.ID,
			Email:            superuser.Email,
			VerificationTier: superuser.VerificationTier,
			IsSuperuser:      true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ExpireInviteLinkProposal(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["error"] != "proposal_closed" {
			t.Errorf("Expected error 'proposal_closed', got %v", respBody["error"])
		}
	})

	t.Run("cannot expire already expired proposal", func(t *testing.T) {
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/already-expired",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		_ = suite.proposalRepo.UpdateStatus(context.Background(), proposal.ID, models.ProposalStatusExpired)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		req := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/expire", nil)
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           superuser.ID,
			Email:            superuser.Email,
			VerificationTier: superuser.VerificationTier,
			IsSuperuser:      true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ExpireInviteLinkProposal(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("expire non-existent proposal returns 404", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/nonexistent/expire", nil)
		q := req.URL.Query()
		q.Set("id", "nonexistent")
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           superuser.ID,
			Email:            superuser.Email,
			VerificationTier: superuser.VerificationTier,
			IsSuperuser:      true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ExpireInviteLinkProposal(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("expired proposal cannot receive votes", func(t *testing.T) {
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/expired-no-votes",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		// Superuser expires the proposal
		expireReq := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/expire", nil)
		q := expireReq.URL.Query()
		q.Set("id", proposal.ID)
		expireReq.URL.RawQuery = q.Encode()

		superuserClaims := &middleware.Claims{
			UserID:           superuser.ID,
			Email:            superuser.Email,
			VerificationTier: superuser.VerificationTier,
			IsSuperuser:      true,
		}
		expireReq = expireReq.WithContext(middleware.ContextWithUser(expireReq.Context(), superuserClaims))

		expireRec := httptest.NewRecorder()
		suite.handler.ExpireInviteLinkProposal(expireRec, expireReq)

		if expireRec.Code != http.StatusOK {
			t.Fatalf("Failed to expire proposal: %s", expireRec.Body.String())
		}

		// Admin tries to vote on expired proposal
		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		voteReq := httptest.NewRequest("POST", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID+"/vote", bytes.NewReader(bodyBytes))
		voteReq.Header.Set("Content-Type", "application/json")
		q = voteReq.URL.Query()
		q.Set("id", proposal.ID)
		voteReq.URL.RawQuery = q.Encode()

		adminClaims := &middleware.Claims{
			UserID:           admin2.ID,
			Email:            admin2.Email,
			VerificationTier: admin2.VerificationTier,
		}
		voteReq = voteReq.WithContext(middleware.ContextWithUser(voteReq.Context(), adminClaims))

		voteRec := httptest.NewRecorder()
		suite.handler.VoteOnInviteLinkProposal(voteRec, voteReq)

		if voteRec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", voteRec.Code, voteRec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(voteRec.Body).Decode(&respBody)

		if respBody["error"] != "proposal_closed" {
			t.Errorf("Expected error 'proposal_closed', got %v", respBody["error"])
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
		RegionID:   &cityRegion.ID,
		GroupName:  "City Group",
		InviteLink: "https://signal.group/city",
		CreatedBy:  &adminUser.ID,
	}
	_ = suite.groupRepo.Create(context.Background(), cityGroup)

	countyGroup := &models.SignalGroup{
		RegionID:   &countyRegion.ID,
		GroupName:  "County Group",
		InviteLink: "https://signal.group/county",
		CreatedBy:  &adminUser.ID,
	}
	_ = suite.groupRepo.Create(context.Background(), countyGroup)

	stateGroup := &models.SignalGroup{
		RegionID:   &stateRegion.ID,
		GroupName:  "State Group",
		InviteLink: "https://signal.group/state",
		CreatedBy:  &adminUser.ID,
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
		body := map[string]string{
			"region_id":   region.ID,
			"name":        "Test Group Name",
			"invite_link": "https://signal.group/nametest",
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
		body := map[string]string{
			"region_id":   region.ID,
			"name":        "Superuser Group",
			"invite_link": "https://signal.group/superuser",
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
		body := map[string]string{
			"region_id":   countyRegion.ID,
			"name":        "County Group via Hierarchy",
			"invite_link": "https://signal.group/hierarchy-county",
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
		body := map[string]string{
			"region_id":   stateRegion.ID,
			"name":        "State Group via Hierarchy",
			"invite_link": "https://signal.group/hierarchy-state",
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
		RegionID:   &region.ID,
		GroupName:  "User's Region Group",
		InviteLink: "https://signal.group/userregion",
		CreatedBy:  &verifiedUser.ID,
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

// =============================================================================
// GetInviteLinkProposal Tests
// =============================================================================

func TestSignalGroupHandler_GetInviteLinkProposal(t *testing.T) {
	suite := setupSignalGroupTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@signalgrouptest.com'")

	admin1 := suite.createTestUser("sg_get_admin1", models.TierPostcard)
	admin2 := suite.createTestUser("sg_get_admin2", models.TierPostcard)
	admin3 := suite.createTestUser("sg_get_admin3", models.TierPostcard)
	regularUser := suite.createTestUser("sg_get_regular", models.TierPostcard)
	superuser := suite.createTestUser("sg_get_superuser", models.TierPostcard)
	_, _ = suite.db.ExecContext(context.Background(), "UPDATE users SET is_superuser = TRUE WHERE id = ?", superuser.ID)

	region := suite.createTestRegion("Get Proposal Test Region")

	suite.addUserToRegion(admin1.ID, region.ID, true)
	suite.addUserToRegion(admin2.ID, region.ID, true)
	suite.addUserToRegion(admin3.ID, region.ID, true)
	suite.addUserToRegion(regularUser.ID, region.ID, false)

	group := &models.SignalGroup{
		RegionID:   &region.ID,
		GroupName:  "Get Proposal Test Group",
		InviteLink: "https://signal.group/gettest",
		CreatedBy:  &admin1.ID,
	}
	_ = suite.groupRepo.Create(context.Background(), group)

	defer suite.cleanup([]string{admin1.ID, admin2.ID, admin3.ID, regularUser.ID, superuser.ID}, []string{region.ID}, []string{group.ID})

	t.Run("admin can get proposal details", func(t *testing.T) {
		// Create a proposal
		reason := "Test reason"
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/newlink",
			Reason:        &reason,
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin1.ID, true)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		req := httptest.NewRequest("GET", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID, nil)
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin2.ID,
			Email:            admin2.Email,
			VerificationTier: admin2.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetInviteLinkProposal(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.InviteLinkProposalDetailResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody.ID != proposal.ID {
			t.Errorf("Expected proposal ID %s, got %s", proposal.ID, respBody.ID)
		}
		if respBody.SignalGroupName != group.GroupName {
			t.Errorf("Expected group name %s, got %s", group.GroupName, respBody.SignalGroupName)
		}
		if respBody.NewInviteLink != "https://signal.group/newlink" {
			t.Errorf("Expected new_invite_link to be visible for admin, got %s", respBody.NewInviteLink)
		}
		if respBody.CurrentVotes != 1 {
			t.Errorf("Expected 1 vote, got %d", respBody.CurrentVotes)
		}
		if len(respBody.Votes) != 1 {
			t.Errorf("Expected 1 vote detail, got %d", len(respBody.Votes))
		}
		if !respBody.CanVote {
			t.Error("Expected can_vote to be true for admin who hasn't voted")
		}
		if respBody.HasVoted {
			t.Error("Expected has_voted to be false for admin2")
		}
	})

	t.Run("superuser can get any proposal", func(t *testing.T) {
		// Create a proposal
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/superusertest",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		req := httptest.NewRequest("GET", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID, nil)
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           superuser.ID,
			Email:            superuser.Email,
			VerificationTier: superuser.VerificationTier,
			IsSuperuser:      true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetInviteLinkProposal(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.InviteLinkProposalDetailResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody.NewInviteLink != "https://signal.group/superusertest" {
			t.Errorf("Expected new_invite_link to be visible for superuser, got %s", respBody.NewInviteLink)
		}
		// Superuser who is not admin of region cannot vote
		if respBody.CanVote {
			t.Error("Expected can_vote to be false for superuser who is not region admin")
		}
	})

	t.Run("non-admin cannot get proposal", func(t *testing.T) {
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/nonadmintest",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		req := httptest.NewRequest("GET", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID, nil)
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           regularUser.ID,
			Email:            regularUser.Email,
			VerificationTier: regularUser.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetInviteLinkProposal(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})

	t.Run("non-existent proposal returns 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/signal-groups/invite-link-proposals/nonexistent", nil)
		q := req.URL.Query()
		q.Set("id", "nonexistent")
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetInviteLinkProposal(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rec.Code)
		}
	})

	t.Run("has_voted is true for user who voted", func(t *testing.T) {
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group.ID,
			RegionID:      &region.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/hasvotedtest",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, admin1.ID, true)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		req := httptest.NewRequest("GET", "/api/v1/signal-groups/invite-link-proposals/"+proposal.ID, nil)
		q := req.URL.Query()
		q.Set("id", proposal.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetInviteLinkProposal(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.InviteLinkProposalDetailResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if !respBody.HasVoted {
			t.Error("Expected has_voted to be true for admin1 who voted")
		}
		if respBody.CanVote {
			t.Error("Expected can_vote to be false for user who already voted")
		}
	})
}

// =============================================================================
// ListInviteLinkProposals Tests
// =============================================================================

func TestSignalGroupHandler_ListInviteLinkProposals(t *testing.T) {
	suite := setupSignalGroupTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@signalgrouptest.com'")

	admin1 := suite.createTestUser("sg_list_admin1", models.TierPostcard)
	admin2 := suite.createTestUser("sg_list_admin2", models.TierPostcard)
	admin3 := suite.createTestUser("sg_list_admin3", models.TierPostcard)
	regularUser := suite.createTestUser("sg_list_regular", models.TierPostcard)
	superuser := suite.createTestUser("sg_list_superuser", models.TierPostcard)
	_, _ = suite.db.ExecContext(context.Background(), "UPDATE users SET is_superuser = TRUE WHERE id = ?", superuser.ID)

	region1 := suite.createTestRegion("List Proposal Test Region 1")
	region2 := suite.createTestRegion("List Proposal Test Region 2")

	suite.addUserToRegion(admin1.ID, region1.ID, true)
	suite.addUserToRegion(admin2.ID, region1.ID, true)
	suite.addUserToRegion(admin3.ID, region1.ID, true)
	suite.addUserToRegion(regularUser.ID, region1.ID, false)
	// admin1 is NOT admin of region2

	group1 := &models.SignalGroup{
		RegionID:   &region1.ID,
		GroupName:  "List Test Group 1",
		InviteLink: "https://signal.group/listtest1",
		CreatedBy:  &admin1.ID,
	}
	_ = suite.groupRepo.Create(context.Background(), group1)

	group2 := &models.SignalGroup{
		RegionID:   &region2.ID,
		GroupName:  "List Test Group 2",
		InviteLink: "https://signal.group/listtest2",
		CreatedBy:  &superuser.ID,
	}
	_ = suite.groupRepo.Create(context.Background(), group2)

	defer suite.cleanup(
		[]string{admin1.ID, admin2.ID, admin3.ID, regularUser.ID, superuser.ID},
		[]string{region1.ID, region2.ID},
		[]string{group1.ID, group2.ID},
	)

	t.Run("admin sees only their region's proposals", func(t *testing.T) {
		// Create proposals in both regions
		proposal1 := &models.InviteLinkUpdateProposal{
			SignalGroupID: group1.ID,
			RegionID:      &region1.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/list1",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal1)

		proposal2 := &models.InviteLinkUpdateProposal{
			SignalGroupID: group2.ID,
			RegionID:      &region2.ID,
			ProposedBy:    &superuser.ID,
			NewInviteLink: "https://signal.group/list2",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal2)

		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id IN (?, ?)", proposal1.ID, proposal2.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id IN (?, ?)", proposal1.ID, proposal2.ID)
		}()

		req := httptest.NewRequest("GET", "/api/v1/signal-groups/invite-link-proposals", nil)

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListInviteLinkProposals(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.InviteLinkProposalListResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		// Admin1 should only see proposal1 (region1)
		if len(respBody.Proposals) != 1 {
			t.Errorf("Expected 1 proposal, got %d", len(respBody.Proposals))
		}
		if len(respBody.Proposals) > 0 && respBody.Proposals[0].ID != proposal1.ID {
			t.Errorf("Expected proposal %s, got %s", proposal1.ID, respBody.Proposals[0].ID)
		}
	})

	t.Run("superuser sees all proposals", func(t *testing.T) {
		// Create proposals in both regions
		proposal1 := &models.InviteLinkUpdateProposal{
			SignalGroupID: group1.ID,
			RegionID:      &region1.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/superlist1",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal1)

		proposal2 := &models.InviteLinkUpdateProposal{
			SignalGroupID: group2.ID,
			RegionID:      &region2.ID,
			ProposedBy:    &superuser.ID,
			NewInviteLink: "https://signal.group/superlist2",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal2)

		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id IN (?, ?)", proposal1.ID, proposal2.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id IN (?, ?)", proposal1.ID, proposal2.ID)
		}()

		req := httptest.NewRequest("GET", "/api/v1/signal-groups/invite-link-proposals", nil)

		claims := &middleware.Claims{
			UserID:           superuser.ID,
			Email:            superuser.Email,
			VerificationTier: superuser.VerificationTier,
			IsSuperuser:      true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListInviteLinkProposals(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.InviteLinkProposalListResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		// Superuser should see both proposals
		if len(respBody.Proposals) < 2 {
			t.Errorf("Expected at least 2 proposals, got %d", len(respBody.Proposals))
		}
	})

	t.Run("filter by status works", func(t *testing.T) {
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group1.ID,
			RegionID:      &region1.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/statusfilter",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		req := httptest.NewRequest("GET", "/api/v1/signal-groups/invite-link-proposals?status=pending", nil)

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListInviteLinkProposals(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.InviteLinkProposalListResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		for _, p := range respBody.Proposals {
			if p.Status != "pending" {
				t.Errorf("Expected all proposals to have status 'pending', got %s", p.Status)
			}
		}
	})

	t.Run("non-admin gets empty list", func(t *testing.T) {
		proposal := &models.InviteLinkUpdateProposal{
			SignalGroupID: group1.ID,
			RegionID:      &region1.ID,
			ProposedBy:    &admin1.ID,
			NewInviteLink: "https://signal.group/nonadminlist",
		}
		_ = suite.proposalRepo.Create(context.Background(), proposal)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_votes WHERE proposal_id = ?", proposal.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM invite_link_update_proposals WHERE id = ?", proposal.ID)
		}()

		req := httptest.NewRequest("GET", "/api/v1/signal-groups/invite-link-proposals", nil)

		claims := &middleware.Claims{
			UserID:           regularUser.ID,
			Email:            regularUser.Email,
			VerificationTier: regularUser.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListInviteLinkProposals(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.InviteLinkProposalListResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		// Regular user (not admin) should see empty list
		if len(respBody.Proposals) != 0 {
			t.Errorf("Expected 0 proposals for non-admin, got %d", len(respBody.Proposals))
		}
	})

	t.Run("empty list returns empty array not null", func(t *testing.T) {
		// Use a user with no proposals
		otherAdmin := suite.createTestUser("sg_list_otheradmin", models.TierPostcard)
		otherRegion := suite.createTestRegion("Other Region For Empty List")
		suite.addUserToRegion(otherAdmin.ID, otherRegion.ID, true)
		defer suite.cleanup([]string{otherAdmin.ID}, []string{otherRegion.ID}, []string{})

		req := httptest.NewRequest("GET", "/api/v1/signal-groups/invite-link-proposals", nil)

		claims := &middleware.Claims{
			UserID:           otherAdmin.ID,
			Email:            otherAdmin.Email,
			VerificationTier: otherAdmin.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListInviteLinkProposals(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.InviteLinkProposalListResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody.Proposals == nil {
			t.Error("Expected proposals to be empty array, not nil")
		}
	})
}
