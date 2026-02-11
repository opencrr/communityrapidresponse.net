package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// deletionTestSuite holds all dependencies for deletion proposal handler tests
type deletionTestSuite struct {
	db              *database.DB
	handler         *DeletionProposalHandler
	proposalRepo    *database.DeletionProposalRepository
	signalGroupRepo *database.SignalGroupRepository
	regionRepo      *database.RegionRepository
	userRepo        *database.UserRepository
	regionID        string
	adminID         string
	admin2ID        string
	admin3ID        string
	memberID        string
	superuserID     string
	signalGroupID   string
	childRegionID   string
}

func setupDeletionTestSuite(t *testing.T) *deletionTestSuite {
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

	consensusConfig := &config.ConsensusConfig{
		VotePercent: 50,
		VoteFloor:   3,
	}

	proposalRepo := database.NewDeletionProposalRepository(db)
	signalGroupRepo := database.NewSignalGroupRepository(db)
	regionRepo := database.NewRegionRepository(db)
	userRepo := database.NewUserRepository(db)

	handler := NewDeletionProposalHandler(
		db, proposalRepo, signalGroupRepo, regionRepo, nil, userRepo, nil, consensusConfig,
	)

	// Create test users
	adminID := createDeletionHandlerTestUser(db, "dl-admin1")
	admin2ID := createDeletionHandlerTestUser(db, "dl-admin2")
	admin3ID := createDeletionHandlerTestUser(db, "dl-admin3")
	memberID := createDeletionHandlerTestUser(db, "dl-member")
	superuserID := createDeletionHandlerTestSuperuser(db, "dl-super")

	// Create test region
	regionID := createDeletionHandlerTestRegion(db, adminID, "Test Deletion Region")

	// Add users to region
	addDeletionHandlerUserToRegion(db, adminID, regionID, true)
	addDeletionHandlerUserToRegion(db, admin2ID, regionID, true)
	addDeletionHandlerUserToRegion(db, admin3ID, regionID, true)
	addDeletionHandlerUserToRegion(db, memberID, regionID, false)

	// Create signal group in the region
	signalGroupID := createDeletionHandlerTestSignalGroup(db, regionID, adminID, "dl-test-group")

	// Create child region for sub-region deletion tests
	childRegionID := createDeletionHandlerTestChildRegion(db, adminID, regionID, "Test Deletion Child")
	addDeletionHandlerUserToRegion(db, adminID, childRegionID, true)
	createDeletionHandlerTestSignalGroup(db, childRegionID, adminID, "dl-child-group")

	suite := &deletionTestSuite{
		db:              db,
		handler:         handler,
		proposalRepo:    proposalRepo,
		signalGroupRepo: signalGroupRepo,
		regionRepo:      regionRepo,
		userRepo:        userRepo,
		regionID:        regionID,
		adminID:         adminID,
		admin2ID:        admin2ID,
		admin3ID:        admin3ID,
		memberID:        memberID,
		superuserID:     superuserID,
		signalGroupID:   signalGroupID,
		childRegionID:   childRegionID,
	}

	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM deletion_votes WHERE proposal_id IN (SELECT id FROM deletion_proposals WHERE reason LIKE '%dl-test%')")
		_, _ = db.Exec("DELETE FROM deletion_proposals WHERE reason LIKE '%dl-test%'")
		_, _ = db.Exec("DELETE FROM signal_groups WHERE group_name LIKE 'dl-%'")
		_, _ = db.Exec("DELETE FROM user_regions WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'dl-%')")
		_, _ = db.Exec("DELETE FROM geographic_regions WHERE name LIKE 'Test Deletion%'")
		_, _ = db.Exec("DELETE FROM users WHERE username LIKE 'dl-%'")
		_ = db.Close()
	})

	return suite
}

func createDeletionHandlerTestUser(db *database.DB, username string) string {
	userID := uuid.New().String()
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO users (id, username, email, password_hash, verification_tier, postcard_verified, vouch_verified, is_superuser, email_verified, created_at)
		VALUES (?, ?, ?, 'hash', 1, TRUE, TRUE, FALSE, TRUE, NOW())
	`, userID, username, username+"@test.com")
	return userID
}

func createDeletionHandlerTestSuperuser(db *database.DB, username string) string {
	userID := uuid.New().String()
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO users (id, username, email, password_hash, verification_tier, postcard_verified, vouch_verified, is_superuser, email_verified, created_at)
		VALUES (?, ?, ?, 'hash', 1, TRUE, TRUE, TRUE, TRUE, NOW())
	`, userID, username, username+"@test.com")
	return userID
}

func createDeletionHandlerTestRegion(db *database.DB, creatorID, name string) string {
	regionID := uuid.New().String()
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO geographic_regions (id, name, region_type, created_by, created_at)
		VALUES (?, ?, 'neighborhood', ?, NOW())
	`, regionID, name, creatorID)
	return regionID
}

func createDeletionHandlerTestChildRegion(db *database.DB, creatorID, parentID, name string) string {
	regionID := uuid.New().String()
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO geographic_regions (id, name, region_type, parent_region_id, created_by, created_at)
		VALUES (?, ?, 'city_block', ?, ?, NOW())
	`, regionID, name, parentID, creatorID)
	return regionID
}

func addDeletionHandlerUserToRegion(db *database.DB, userID, regionID string, isAdmin bool) {
	urID := uuid.New().String()
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO user_regions (id, user_id, region_id, is_admin, verified_at)
		VALUES (?, ?, ?, ?, NOW())
	`, urID, userID, regionID, isAdmin)
}

func createDeletionHandlerTestSignalGroup(db *database.DB, regionID, creatorID, name string) string {
	groupID := uuid.New().String()
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO signal_groups (id, region_id, group_name, invite_link, created_by, created_at, is_active)
		VALUES (?, ?, ?, 'https://signal.group/test', ?, NOW(), TRUE)
	`, groupID, regionID, name, creatorID)
	return groupID
}

func deletionVoteAsAdmin(t *testing.T, suite *deletionTestSuite, proposalID, userID string, vote bool) {
	t.Helper()

	body := map[string]bool{"vote": vote}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deletion-proposals/"+proposalID+"/vote", bytes.NewReader(jsonBody))
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
		t.Fatalf("Vote failed for user %s: %s", userID, rr.Body.String())
	}
}

// =============================================================================
// CreateProposal Tests
// =============================================================================

func TestDeletionProposalHandler_CreateProposal(t *testing.T) {
	suite := setupDeletionTestSuite(t)

	tests := []struct {
		name           string
		userID         string
		isSuperuser    bool
		assetType      string
		assetID        string
		reason         string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "admin can create signal group proposal",
			userID:         suite.adminID,
			assetType:      "signal_group",
			assetID:        suite.signalGroupID,
			reason:         "dl-test admin create",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "non-admin cannot create proposal",
			userID:         suite.memberID,
			assetType:      "signal_group",
			assetID:        suite.signalGroupID,
			reason:         "dl-test member create",
			expectedStatus: http.StatusForbidden,
			expectedError:  "not_admin",
		},
		{
			name:           "invalid asset type returns 400",
			userID:         suite.adminID,
			assetType:      "invalid_type",
			assetID:        suite.signalGroupID,
			reason:         "dl-test invalid type",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "validation_error",
		},
		{
			name:           "non-existent signal group returns 404",
			userID:         suite.adminID,
			assetType:      "signal_group",
			assetID:        uuid.New().String(),
			reason:         "dl-test missing asset",
			expectedStatus: http.StatusNotFound,
			expectedError:  "not_found",
		},
		{
			name:           "reason required",
			userID:         suite.adminID,
			assetType:      "signal_group",
			assetID:        suite.signalGroupID,
			reason:         "",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "validation_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]string{
				"asset_type": tt.assetType,
				"asset_id":   tt.assetID,
				"reason":     tt.reason,
			}
			jsonBody, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/deletion-proposals", bytes.NewReader(jsonBody))

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

func TestDeletionProposalHandler_CreateProposal_DuplicatePending(t *testing.T) {
	suite := setupDeletionTestSuite(t)

	// Create first proposal directly via repo
	reason := "dl-test duplicate pending"
	proposal := &models.DeletionProposal{
		AssetType:  models.AssetTypeSignalGroup,
		AssetID:    suite.signalGroupID,
		RegionID:   &suite.regionID,
		ProposedBy: &suite.adminID,
		Reason:     &reason,
	}
	if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Try to create a second proposal for the same asset
	body := map[string]string{
		"asset_type": "signal_group",
		"asset_id":   suite.signalGroupID,
		"reason":     "dl-test duplicate attempt",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deletion-proposals", bytes.NewReader(jsonBody))
	claims := &middleware.Claims{
		UserID:           suite.adminID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.CreateProposal(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("Expected status 409, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if resp["error"] != "pending_proposal_exists" {
		t.Errorf("Expected error pending_proposal_exists, got %v", resp["error"])
	}
}

func TestDeletionProposalHandler_CreateProposal_SubRegion(t *testing.T) {
	suite := setupDeletionTestSuite(t)

	body := map[string]string{
		"asset_type": "sub_region",
		"asset_id":   suite.childRegionID,
		"reason":     "dl-test sub-region create",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deletion-proposals", bytes.NewReader(jsonBody))
	claims := &middleware.Claims{
		UserID:           suite.adminID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.CreateProposal(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp models.DeletionProposalResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if resp.ProposalID == "" {
		t.Error("Expected proposal ID to be set")
	}
	if resp.Status != "pending" {
		t.Errorf("Expected status pending, got %s", resp.Status)
	}
	if resp.CurrentVotes != 1 {
		t.Errorf("Expected 1 current vote (proposer auto-vote), got %d", resp.CurrentVotes)
	}
}

// =============================================================================
// VoteOnProposal Tests
// =============================================================================

func TestDeletionProposalHandler_VoteOnProposal(t *testing.T) {
	suite := setupDeletionTestSuite(t)

	// Create a proposal
	reason := "dl-test vote reason"
	proposal := &models.DeletionProposal{
		AssetType:  models.AssetTypeSignalGroup,
		AssetID:    suite.signalGroupID,
		RegionID:   &suite.regionID,
		ProposedBy: &suite.adminID,
		Reason:     &reason,
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
			expectedError:  "not_admin",
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

			req := httptest.NewRequest(http.MethodPost, "/api/v1/deletion-proposals/"+proposal.ID+"/vote", bytes.NewReader(jsonBody))
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

func TestDeletionProposalHandler_VoteOnNonPending(t *testing.T) {
	suite := setupDeletionTestSuite(t)

	// Create a proposal and expire it
	reason := "dl-test vote non-pending"
	proposal := &models.DeletionProposal{
		AssetType:  models.AssetTypeSignalGroup,
		AssetID:    suite.signalGroupID,
		RegionID:   &suite.regionID,
		ProposedBy: &suite.adminID,
		Reason:     &reason,
	}
	if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}
	if err := suite.proposalRepo.UpdateStatus(context.Background(), proposal.ID, models.ProposalStatusExpired); err != nil {
		t.Fatalf("Failed to expire proposal: %v", err)
	}

	body := map[string]bool{"vote": true}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deletion-proposals/"+proposal.ID+"/vote", bytes.NewReader(jsonBody))
	req.URL.RawQuery = "id=" + proposal.ID

	claims := &middleware.Claims{
		UserID:           suite.admin2ID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.VoteOnProposal(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("Expected status 409, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if resp["error"] != "proposal_closed" {
		t.Errorf("Expected error proposal_closed, got %v", resp["error"])
	}
}

// =============================================================================
// Consensus Tests
// =============================================================================

func TestDeletionProposalHandler_ConsensusSignalGroup(t *testing.T) {
	suite := setupDeletionTestSuite(t)

	// Create a new signal group for this test (we'll deactivate it)
	groupID := createDeletionHandlerTestSignalGroup(suite.db, suite.regionID, suite.adminID, "dl-consensus-sg")

	// Create proposal
	reason := "dl-test consensus signal group"
	proposal := &models.DeletionProposal{
		AssetType:  models.AssetTypeSignalGroup,
		AssetID:    groupID,
		RegionID:   &suite.regionID,
		ProposedBy: &suite.adminID,
		Reason:     &reason,
	}
	if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Add proposer's vote
	if err := suite.proposalRepo.AddVote(context.Background(), proposal.ID, suite.adminID, true); err != nil {
		t.Fatalf("Failed to add proposer vote: %v", err)
	}

	// Vote from admin2
	deletionVoteAsAdmin(t, suite, proposal.ID, suite.admin2ID, true)

	// Vote from admin3 — this should trigger consensus (3 votes)
	body := map[string]bool{"vote": true}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deletion-proposals/"+proposal.ID+"/vote", bytes.NewReader(jsonBody))
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

	var resp models.DeletionProposalResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Status != "approved" {
		t.Errorf("Expected status 'approved', got %s", resp.Status)
	}

	// Verify signal group is deactivated
	var isActive bool
	err := suite.db.QueryRowContext(context.Background(), `SELECT is_active FROM signal_groups WHERE id = ?`, groupID).Scan(&isActive)
	if err != nil {
		t.Fatalf("Failed to check signal group status: %v", err)
	}
	if isActive {
		t.Error("Expected signal group to be deactivated after consensus")
	}
}

func TestDeletionProposalHandler_ConsensusSubRegion(t *testing.T) {
	suite := setupDeletionTestSuite(t)

	// Create a child region dedicated to this test
	childID := createDeletionHandlerTestChildRegion(suite.db, suite.adminID, suite.regionID, "Test Deletion Consensus Child")
	addDeletionHandlerUserToRegion(suite.db, suite.adminID, childID, true)
	createDeletionHandlerTestSignalGroup(suite.db, childID, suite.adminID, "dl-consensus-child-sg")

	// Create proposal — the parent region is the governing scope
	reason := "dl-test consensus sub-region"
	proposal := &models.DeletionProposal{
		AssetType:  models.AssetTypeSubRegion,
		AssetID:    childID,
		RegionID:   &suite.regionID,
		ProposedBy: &suite.adminID,
		Reason:     &reason,
	}
	if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Add proposer's vote
	if err := suite.proposalRepo.AddVote(context.Background(), proposal.ID, suite.adminID, true); err != nil {
		t.Fatalf("Failed to add proposer vote: %v", err)
	}

	// Vote from admin2
	deletionVoteAsAdmin(t, suite, proposal.ID, suite.admin2ID, true)

	// Vote from admin3 — triggers consensus
	body := map[string]bool{"vote": true}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deletion-proposals/"+proposal.ID+"/vote", bytes.NewReader(jsonBody))
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

	var resp models.DeletionProposalResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Status != "approved" {
		t.Errorf("Expected status 'approved', got %s", resp.Status)
	}

	// Verify child region is deleted
	var count int
	err := suite.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM geographic_regions WHERE id = ?`, childID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check child region: %v", err)
	}
	if count != 0 {
		t.Error("Expected child region to be cascade-deleted after consensus")
	}

	// Verify signal groups in child are also gone
	err = suite.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM signal_groups WHERE region_id = ?`, childID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check child signal groups: %v", err)
	}
	if count != 0 {
		t.Error("Expected signal groups in child region to be deleted")
	}

	// Verify parent region still exists
	err = suite.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM geographic_regions WHERE id = ?`, suite.regionID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check parent region: %v", err)
	}
	if count != 1 {
		t.Error("Expected parent region to still exist")
	}
}

// =============================================================================
// GetProposal Tests
// =============================================================================

func TestDeletionProposalHandler_GetProposal(t *testing.T) {
	suite := setupDeletionTestSuite(t)

	// Create a proposal
	reason := "dl-test get reason"
	proposal := &models.DeletionProposal{
		AssetType:  models.AssetTypeSignalGroup,
		AssetID:    suite.signalGroupID,
		RegionID:   &suite.regionID,
		ProposedBy: &suite.adminID,
		Reason:     &reason,
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
			req := httptest.NewRequest(http.MethodGet, "/api/v1/deletion-proposals/"+proposal.ID, nil)
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

func TestDeletionProposalHandler_GetProposal_NotFound(t *testing.T) {
	suite := setupDeletionTestSuite(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/deletion-proposals/"+uuid.New().String(), nil)
	req.URL.RawQuery = "id=" + uuid.New().String()

	claims := &middleware.Claims{
		UserID:           suite.adminID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.GetProposal(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

// =============================================================================
// ListProposals Tests
// =============================================================================

func TestDeletionProposalHandler_ListProposals(t *testing.T) {
	suite := setupDeletionTestSuite(t)

	// Create several proposals
	for i := 0; i < 3; i++ {
		groupID := createDeletionHandlerTestSignalGroup(suite.db, suite.regionID, suite.adminID, "dl-list-"+string(rune('a'+i)))
		reason := "dl-test list reason"
		proposal := &models.DeletionProposal{
			AssetType:  models.AssetTypeSignalGroup,
			AssetID:    groupID,
			RegionID:   &suite.regionID,
			ProposedBy: &suite.adminID,
			Reason:     &reason,
		}
		if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
			t.Fatalf("Failed to create proposal %d: %v", i, err)
		}
	}

	t.Run("admin sees scoped proposals", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/deletion-proposals", nil)
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

		var resp models.DeletionProposalListResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if len(resp.Proposals) < 3 {
			t.Errorf("Expected at least 3 proposals, got %d", len(resp.Proposals))
		}
	})

	t.Run("superuser sees all proposals", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/deletion-proposals", nil)
		claims := &middleware.Claims{
			UserID:           suite.superuserID,
			IsSuperuser:      true,
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

		var resp models.DeletionProposalListResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if len(resp.Proposals) < 3 {
			t.Errorf("Expected at least 3 proposals, got %d", len(resp.Proposals))
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/deletion-proposals?status=pending", nil)
		claims := &middleware.Claims{
			UserID:           suite.superuserID,
			IsSuperuser:      true,
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

		var resp models.DeletionProposalListResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		for _, p := range resp.Proposals {
			if p.Status != "pending" {
				t.Errorf("Expected status pending, got %s", p.Status)
			}
		}
	})
}

// =============================================================================
// ExpireProposal Tests
// =============================================================================

func TestDeletionProposalHandler_ExpireProposal(t *testing.T) {
	suite := setupDeletionTestSuite(t)

	// Create a proposal
	reason := "dl-test expire reason"
	proposal := &models.DeletionProposal{
		AssetType:  models.AssetTypeSignalGroup,
		AssetID:    suite.signalGroupID,
		RegionID:   &suite.regionID,
		ProposedBy: &suite.adminID,
		Reason:     &reason,
	}
	if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	t.Run("superuser can expire", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/deletion-proposals/"+proposal.ID+"/expire", nil)
		req.URL.RawQuery = "id=" + proposal.ID

		claims := &middleware.Claims{
			UserID:           suite.superuserID,
			IsSuperuser:      true,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.ExpireProposal(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}
		if resp["status"] != "expired" {
			t.Errorf("Expected status expired, got %v", resp["status"])
		}
	})

	t.Run("admin cannot expire", func(t *testing.T) {
		// Create another proposal to try to expire as admin
		reason2 := "dl-test admin expire"
		proposal2 := &models.DeletionProposal{
			AssetType:  models.AssetTypeSignalGroup,
			AssetID:    suite.signalGroupID,
			RegionID:   &suite.regionID,
			ProposedBy: &suite.adminID,
			Reason:     &reason2,
		}
		// Need to clear the pending proposal first since signalGroupID already has one (expired)
		if err := suite.proposalRepo.Create(context.Background(), proposal2); err != nil {
			t.Fatalf("Failed to create proposal2: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/deletion-proposals/"+proposal2.ID+"/expire", nil)
		req.URL.RawQuery = "id=" + proposal2.ID

		claims := &middleware.Claims{
			UserID:           suite.adminID,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.ExpireProposal(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("cannot expire already-expired proposal", func(t *testing.T) {
		// The first proposal was already expired — try again
		req := httptest.NewRequest(http.MethodPost, "/api/v1/deletion-proposals/"+proposal.ID+"/expire", nil)
		req.URL.RawQuery = "id=" + proposal.ID

		claims := &middleware.Claims{
			UserID:           suite.superuserID,
			IsSuperuser:      true,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.ExpireProposal(rr, req)

		if rr.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}
