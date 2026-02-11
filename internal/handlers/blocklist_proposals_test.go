package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

func TestBlocklistProposalHandler_CreateProposal(t *testing.T) {
	suite := setupBlocklistTestSuite(t)

	tests := []struct {
		name           string
		userID         string
		isSuperuser    bool
		isAdmin        bool
		targetUserID   string
		reason         string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "admin can create proposal",
			userID:         suite.adminID,
			isAdmin:        true,
			targetUserID:   suite.memberID,
			reason:         "Test reason",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "non-admin cannot create proposal",
			userID:         suite.memberID,
			isAdmin:        false,
			targetUserID:   suite.adminID,
			reason:         "Test reason",
			expectedStatus: http.StatusForbidden,
			expectedError:  "not_region_admin",
		},
		{
			name:           "cannot blocklist self",
			userID:         suite.adminID,
			isAdmin:        true,
			targetUserID:   suite.adminID,
			reason:         "Test reason",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "cannot_blocklist_self",
		},
		{
			name:           "cannot blocklist superuser",
			userID:         suite.adminID,
			isAdmin:        true,
			targetUserID:   suite.superuserID,
			reason:         "Test reason",
			expectedStatus: http.StatusForbidden,
			expectedError:  "cannot_blocklist_superuser",
		},
		{
			name:           "cannot blocklist user not in region",
			userID:         suite.adminID,
			isAdmin:        true,
			targetUserID:   suite.outsiderID,
			reason:         "Test reason",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "user_not_in_region",
		},
		{
			name:           "reason required",
			userID:         suite.adminID,
			isAdmin:        true,
			targetUserID:   suite.memberID,
			reason:         "",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "validation_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]string{
				"target_user_id": tt.targetUserID,
				"reason":         tt.reason,
			}
			jsonBody, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/communities/"+suite.regionID+"/blocklist-proposals", bytes.NewReader(jsonBody))
			req.URL.RawQuery = "id=" + suite.regionID

			claims := &middleware.Claims{
				UserID:           tt.userID,
				IsSuperuser:      tt.isSuperuser,
				PostcardVerified: true,
				VouchVerified:    true,
			}
			ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			suite.handler.CreateProposal(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d: %s", tt.expectedStatus, rr.Code, rr.Body.String())
			}

			if tt.expectedError != "" {
				var resp map[string]interface{}
				if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}
				if resp["error"] != tt.expectedError {
					t.Errorf("Expected error %s, got %v", tt.expectedError, resp["error"])
				}
			}
		})
	}
}

func TestBlocklistProposalHandler_VoteOnProposal(t *testing.T) {
	suite := setupBlocklistTestSuite(t)

	// Create a proposal
	proposal := &models.BlocklistProposal{
		TargetUserID: suite.memberID,
		RegionID:     &suite.regionID,
		ProposedBy:   &suite.adminID,
		Reason:       "Vote test",
	}
	if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}
	// Add proposer's vote
	if err := suite.proposalRepo.AddVote(context.Background(), proposal.ID, suite.adminID, true); err != nil {
		t.Fatalf("Failed to add proposer vote: %v", err)
	}

	tests := []struct {
		name           string
		userID         string
		vote           bool
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "admin can vote",
			userID:         suite.admin2ID,
			vote:           true,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "non-admin cannot vote",
			userID:         suite.memberID,
			vote:           true,
			expectedStatus: http.StatusForbidden,
			expectedError:  "not_region_admin",
		},
		{
			name:           "cannot vote twice",
			userID:         suite.admin2ID,
			vote:           true,
			expectedStatus: http.StatusConflict,
			expectedError:  "already_voted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]bool{"vote": tt.vote}
			jsonBody, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/blocklist-proposals/"+proposal.ID+"/vote", bytes.NewReader(jsonBody))
			req.URL.RawQuery = "id=" + proposal.ID

			claims := &middleware.Claims{
				UserID:           tt.userID,
				PostcardVerified: true,
				VouchVerified:    true,
			}
			ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			suite.handler.VoteOnProposal(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d: %s", tt.expectedStatus, rr.Code, rr.Body.String())
			}

			if tt.expectedError != "" {
				var resp map[string]interface{}
				if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}
				if resp["error"] != tt.expectedError {
					t.Errorf("Expected error %s, got %v", tt.expectedError, resp["error"])
				}
			}
		})
	}
}

func TestBlocklistProposalHandler_ConsensusBlocklist(t *testing.T) {
	suite := setupBlocklistTestSuite(t)

	// Create a proposal
	proposal := &models.BlocklistProposal{
		TargetUserID: suite.memberID,
		RegionID:     &suite.regionID,
		ProposedBy:   &suite.adminID,
		Reason:       "Consensus test",
	}
	if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Add proposer's vote
	if err := suite.proposalRepo.AddVote(context.Background(), proposal.ID, suite.adminID, true); err != nil {
		t.Fatalf("Failed to add proposer vote: %v", err)
	}

	// Vote from admin2
	voteAsAdmin(t, suite, proposal.ID, suite.admin2ID, true)

	// Vote from admin3 - this should trigger consensus (3 votes)
	body := map[string]bool{"vote": true}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/blocklist-proposals/"+proposal.ID+"/vote", bytes.NewReader(jsonBody))
	req.URL.RawQuery = "id=" + proposal.ID

	claims := &middleware.Claims{
		UserID:           suite.admin3ID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.VoteOnProposal(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp models.BlocklistProposalResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Status != "approved" {
		t.Errorf("Expected status 'approved', got %s", resp.Status)
	}

	// Verify user is blocklisted
	user, err := suite.userRepo.GetByID(context.Background(), suite.memberID)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}
	if !user.IsBlocked {
		t.Error("Expected user to be blocklisted after consensus")
	}

	// Verify user is removed from region
	inRegion, err := suite.proposalRepo.IsUserInRegion(context.Background(), suite.memberID, suite.regionID)
	if err != nil {
		t.Fatalf("Failed to check region membership: %v", err)
	}
	if inRegion {
		t.Error("Expected user to be removed from region after blocklist")
	}
}

func TestBlocklistProposalHandler_GetProposal(t *testing.T) {
	suite := setupBlocklistTestSuite(t)

	// Create a proposal
	proposal := &models.BlocklistProposal{
		TargetUserID: suite.memberID,
		RegionID:     &suite.regionID,
		ProposedBy:   &suite.adminID,
		Reason:       "Get test",
	}
	if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	tests := []struct {
		name           string
		userID         string
		isSuperuser    bool
		expectedStatus int
	}{
		{
			name:           "admin can view",
			userID:         suite.adminID,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "superuser can view",
			userID:         suite.superuserID,
			isSuperuser:    true,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "non-admin cannot view",
			userID:         suite.memberID,
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/blocklist-proposals/"+proposal.ID, nil)
			req.URL.RawQuery = "id=" + proposal.ID

			claims := &middleware.Claims{
				UserID:           tt.userID,
				IsSuperuser:      tt.isSuperuser,
				PostcardVerified: true,
				VouchVerified:    true,
			}
			ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			suite.handler.GetProposal(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d: %s", tt.expectedStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestBlocklistProposalHandler_ListProposals(t *testing.T) {
	suite := setupBlocklistTestSuite(t)

	// Create proposals
	for i := 0; i < 3; i++ {
		memberID := createBlocklistTestUser(suite.db, "list-target-"+string(rune('a'+i)))
		addBlocklistUserToRegion(suite.db, memberID, suite.regionID, false)

		proposal := &models.BlocklistProposal{
			TargetUserID: memberID,
			RegionID:     &suite.regionID,
			ProposedBy:   &suite.adminID,
			Reason:       "List test",
		}
		if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
			t.Fatalf("Failed to create proposal %d: %v", i, err)
		}
	}

	// Admin should see proposals
	req := httptest.NewRequest(http.MethodGet, "/api/v1/blocklist-proposals", nil)
	claims := &middleware.Claims{
		UserID:           suite.adminID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.ListProposals(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp models.BlocklistProposalListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(resp.Proposals) < 3 {
		t.Errorf("Expected at least 3 proposals, got %d", len(resp.Proposals))
	}
}

func TestBlocklistProposalHandler_RateLimit(t *testing.T) {
	suite := setupBlocklistTestSuite(t)

	// Create 5 proposals (the rate limit)
	for i := 0; i < 5; i++ {
		memberID := createBlocklistTestUser(suite.db, "rate-target-"+string(rune('a'+i)))
		addBlocklistUserToRegion(suite.db, memberID, suite.regionID, false)

		proposal := &models.BlocklistProposal{
			TargetUserID: memberID,
			RegionID:     &suite.regionID,
			ProposedBy:   &suite.adminID,
			Reason:       "Rate limit test",
		}
		if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
			t.Fatalf("Failed to create proposal %d: %v", i, err)
		}
	}

	// 6th proposal should be rate limited
	memberID := createBlocklistTestUser(suite.db, "rate-target-f")
	addBlocklistUserToRegion(suite.db, memberID, suite.regionID, false)

	body := map[string]string{
		"target_user_id": memberID,
		"reason":         "Rate limit exceeded",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/communities/"+suite.regionID+"/blocklist-proposals", bytes.NewReader(jsonBody))
	req.URL.RawQuery = "id=" + suite.regionID

	claims := &middleware.Claims{
		UserID:           suite.adminID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.CreateProposal(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestBlocklistProposalHandler_ListBlockedAddresses(t *testing.T) {
	suite := setupBlocklistTestSuite(t)

	// Only superuser can list
	tests := []struct {
		name           string
		userID         string
		isSuperuser    bool
		expectedStatus int
	}{
		{
			name:           "superuser can list",
			userID:         suite.superuserID,
			isSuperuser:    true,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "admin cannot list",
			userID:         suite.adminID,
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/blocklisted-addresses", nil)

			claims := &middleware.Claims{
				UserID:           tt.userID,
				IsSuperuser:      tt.isSuperuser,
				PostcardVerified: true,
				VouchVerified:    true,
			}
			ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			suite.handler.ListBlockedAddresses(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d: %s", tt.expectedStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

// Test suite setup

type blocklistTestSuite struct {
	db           *database.DB
	handler      *BlocklistProposalHandler
	proposalRepo *database.BlocklistProposalRepository
	regionRepo   *database.RegionRepository
	userRepo     *database.UserRepository
	regionID     string
	adminID      string
	admin2ID     string
	admin3ID     string
	memberID     string
	superuserID  string
	outsiderID   string
}

func setupBlocklistTestSuite(t *testing.T) *blocklistTestSuite {
	t.Helper()

	dsn := "root:@tcp(127.0.0.1:3306)/communityrapidresponse_test?charset=utf8mb4&parseTime=true&loc=UTC"
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skip("Test database not available")
	}

	if err := sqlDB.Ping(); err != nil {
		t.Skip("Test database not available")
	}

	db := &database.DB{DB: sqlDB}

	blocklistConfig := &config.BlocklistConfig{
		AddressBlocklistDuration:  2 * 365 * 24 * time.Hour,
		ProposalRateLimitPerMonth: 5,
	}
	consensusConfig := &config.ConsensusConfig{
		VotePercent: 50,
		VoteFloor:   3,
	}

	proposalRepo := database.NewBlocklistProposalRepository(db, blocklistConfig)
	regionRepo := database.NewRegionRepository(db)
	userRepo := database.NewUserRepository(db)

	handler := NewBlocklistProposalHandler(
		nil,
		proposalRepo, regionRepo, userRepo, nil,
		consensusConfig, blocklistConfig,
	)

	// Create test data
	adminID := createBlocklistTestUser(db, "bl-admin1")
	admin2ID := createBlocklistTestUser(db, "bl-admin2")
	admin3ID := createBlocklistTestUser(db, "bl-admin3")
	memberID := createBlocklistTestUser(db, "bl-member")
	superuserID := createBlocklistTestSuperuser(db, "bl-super")
	outsiderID := createBlocklistTestUser(db, "bl-outsider")

	regionID := createBlocklistTestRegion(db, adminID, "Test Blocklist Region")

	// Add users to region
	addBlocklistUserToRegion(db, adminID, regionID, true)
	addBlocklistUserToRegion(db, admin2ID, regionID, true)
	addBlocklistUserToRegion(db, admin3ID, regionID, true)
	addBlocklistUserToRegion(db, memberID, regionID, false)
	addBlocklistUserToRegion(db, superuserID, regionID, false)

	suite := &blocklistTestSuite{
		db:           db,
		handler:      handler,
		proposalRepo: proposalRepo,
		regionRepo:   regionRepo,
		userRepo:     userRepo,
		regionID:     regionID,
		adminID:      adminID,
		admin2ID:     admin2ID,
		admin3ID:     admin3ID,
		memberID:     memberID,
		superuserID:  superuserID,
		outsiderID:   outsiderID,
	}

	t.Cleanup(func() {
		// Clean up test data
		_, _ = db.Exec("DELETE FROM blocklist_votes WHERE proposal_id IN (SELECT id FROM blocklist_proposals WHERE reason LIKE '%test%')")
		_, _ = db.Exec("DELETE FROM blocklisted_addresses WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'bl-%')")
		_, _ = db.Exec("DELETE FROM blocklisted_users WHERE proposal_id IN (SELECT id FROM blocklist_proposals WHERE reason LIKE '%test%')")
		_, _ = db.Exec("DELETE FROM blocklist_proposals WHERE reason LIKE '%test%'")
		_, _ = db.Exec("DELETE FROM user_regions WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'bl-%')")
		_, _ = db.Exec("DELETE FROM user_regions WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'list-target-%')")
		_, _ = db.Exec("DELETE FROM user_regions WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'rate-target-%')")
		_, _ = db.Exec("DELETE FROM geographic_regions WHERE name LIKE 'Test Blocklist%'")
		_, _ = db.Exec("DELETE FROM users WHERE username LIKE 'bl-%'")
		_, _ = db.Exec("DELETE FROM users WHERE username LIKE 'list-target-%'")
		_, _ = db.Exec("DELETE FROM users WHERE username LIKE 'rate-target-%'")
		_ = db.Close()
	})

	return suite
}

func createBlocklistTestUser(db *database.DB, username string) string {
	userID := uuid.New().String()
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO users (id, username, email, password_hash, verification_tier, postcard_verified, vouch_verified, is_superuser, email_verified, created_at)
		VALUES (?, ?, ?, 'hash', 1, TRUE, TRUE, FALSE, TRUE, NOW())
	`, userID, username, username+"@test.com")
	return userID
}

func createBlocklistTestSuperuser(db *database.DB, username string) string {
	userID := uuid.New().String()
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO users (id, username, email, password_hash, verification_tier, postcard_verified, vouch_verified, is_superuser, email_verified, created_at)
		VALUES (?, ?, ?, 'hash', 1, TRUE, TRUE, TRUE, TRUE, NOW())
	`, userID, username, username+"@test.com")
	return userID
}

func createBlocklistTestRegion(db *database.DB, creatorID, name string) string {
	regionID := uuid.New().String()
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO geographic_regions (id, name, region_type, created_by, created_at)
		VALUES (?, ?, 'neighborhood', ?, NOW())
	`, regionID, name, creatorID)
	return regionID
}

func addBlocklistUserToRegion(db *database.DB, userID, regionID string, isAdmin bool) {
	urID := uuid.New().String()
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO user_regions (id, user_id, region_id, is_admin, verified_at)
		VALUES (?, ?, ?, ?, NOW())
	`, urID, userID, regionID, isAdmin)
}

func voteAsAdmin(t *testing.T, suite *blocklistTestSuite, proposalID, userID string, vote bool) {
	t.Helper()

	body := map[string]bool{"vote": vote}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/blocklist-proposals/"+proposalID+"/vote", bytes.NewReader(jsonBody))
	req.URL.RawQuery = "id=" + proposalID

	claims := &middleware.Claims{
		UserID:           userID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.VoteOnProposal(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Vote failed: %s", rr.Body.String())
	}
}
