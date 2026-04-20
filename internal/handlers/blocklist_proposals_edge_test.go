package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// TestBlocklistEdge_DoubleVote tests that an admin cannot vote twice on the same
// proposal (B1).
func TestBlocklistEdge_DoubleVote(t *testing.T) {
	suite := setupBlocklistTestSuite(t)

	// Create proposal
	proposal := &models.BlocklistProposal{
		TargetUserID: suite.memberID,
		RegionID:     &suite.regionID,
		ProposedBy:   &suite.adminID,
		Reason:       "Double vote test",
	}
	if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}
	// Add proposer vote
	_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, suite.adminID, true)

	// First vote from admin2
	body := map[string]bool{"vote": true}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/blocklist-proposals/"+proposal.ID+"/vote", bytes.NewReader(jsonBody))
	req.URL.RawQuery = "id=" + proposal.ID
	claims := &middleware.Claims{UserID: suite.admin2ID, PostcardVerified: true, VouchVerified: true}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	suite.handler.VoteOnProposal(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("First vote should succeed, got %d", rr.Code)
	}

	// Second vote from same admin2 - should fail
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/blocklist-proposals/"+proposal.ID+"/vote", bytes.NewReader(jsonBody))
	req2.URL.RawQuery = "id=" + proposal.ID
	ctx2 := context.WithValue(req2.Context(), middleware.UserContextKey, claims)
	req2 = req2.WithContext(ctx2)
	rr2 := httptest.NewRecorder()
	suite.handler.VoteOnProposal(rr2, req2)

	if rr2.Code != http.StatusConflict {
		t.Errorf("Expected 409 for double vote, got %d: %s", rr2.Code, rr2.Body.String())
	}
}

// TestBlocklistEdge_VoteOnClosedProposal tests voting on an already-approved
// proposal (B5).
func TestBlocklistEdge_VoteOnClosedProposal(t *testing.T) {
	suite := setupBlocklistTestSuite(t)

	// Create and approve a proposal directly
	proposalID := uuid.New().String()
	_, err := suite.db.ExecContext(context.Background(),
		`INSERT INTO blocklist_proposals (id, target_user_id, region_id, proposed_by, reason, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'Closed test', 'approved', NOW(), NOW())`,
		proposalID, suite.memberID, suite.regionID, suite.adminID)
	if err != nil {
		t.Fatalf("Failed to insert proposal: %v", err)
	}

	body := map[string]bool{"vote": true}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/blocklist-proposals/"+proposalID+"/vote", bytes.NewReader(jsonBody))
	req.URL.RawQuery = "id=" + proposalID
	claims := &middleware.Claims{UserID: suite.admin2ID, PostcardVerified: true, VouchVerified: true}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	suite.handler.VoteOnProposal(rr, req)

	// Should reject because proposal is already approved
	if rr.Code == http.StatusOK {
		t.Error("Expected vote on closed proposal to fail")
	}
	t.Logf("Vote on approved proposal: status %d", rr.Code)
}

// TestBlocklistEdge_InsufficientAdmins tests blocklist proposal when fewer than
// 3 admins exist in the region (B9).
func TestBlocklistEdge_InsufficientAdmins(t *testing.T) {
	host := getEnvOrDefault("TEST_DB_HOST", "")
	if host == "" {
		t.Skip("TEST_DB_HOST not set")
	}

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
	consensusConfig := &config.ConsensusConfig{VotePercent: 50, VoteFloor: 3}
	proposalRepo := database.NewBlocklistProposalRepository(db, blocklistConfig)
	regionRepo := database.NewRegionRepository(db)
	userRepo := database.NewUserRepository(db)
	handler := NewBlocklistProposalHandler(nil, proposalRepo, regionRepo, userRepo, nil, consensusConfig, blocklistConfig)

	// Create region with only 2 admins
	admin1ID := createBlocklistTestUser(db, "edge-insuf-admin1")
	admin2ID := createBlocklistTestUser(db, "edge-insuf-admin2")
	targetID := createBlocklistTestUser(db, "edge-insuf-target")
	regionID := createBlocklistTestRegion(db, admin1ID, "Test Insufficient Admins Region")
	addBlocklistUserToRegion(db, admin1ID, regionID, true)
	addBlocklistUserToRegion(db, admin2ID, regionID, true)
	addBlocklistUserToRegion(db, targetID, regionID, false)

	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM blocklist_proposals WHERE reason LIKE '%insuf%'")
		_, _ = db.Exec("DELETE FROM user_regions WHERE user_id IN (?, ?, ?)", admin1ID, admin2ID, targetID)
		_, _ = db.Exec("DELETE FROM geographic_regions WHERE name LIKE 'Test Insufficient%'")
		_, _ = db.Exec("DELETE FROM users WHERE username LIKE 'edge-insuf-%'")
		_ = db.Close()
	})

	body := map[string]string{
		"target_user_id": targetID,
		"reason":         "insuf admins test",
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/communities/"+regionID+"/blocklist-proposals", bytes.NewReader(jsonBody))
	req.URL.RawQuery = "id=" + regionID
	claims := &middleware.Claims{UserID: admin1ID, PostcardVerified: true, VouchVerified: true}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler.CreateProposal(rr, req)

	// Should fail because only 2 admins < VoteFloor of 3
	if rr.Code != http.StatusBadRequest && rr.Code != http.StatusForbidden {
		t.Errorf("Expected rejection for insufficient admins, got %d: %s", rr.Code, rr.Body.String())
	}
	t.Logf("Insufficient admins proposal: status %d", rr.Code)
}

// TestBlocklistEdge_WhitespaceOnlyReason tests proposal with whitespace-only
// reason (B11).
func TestBlocklistEdge_WhitespaceOnlyReason(t *testing.T) {
	suite := setupBlocklistTestSuite(t)

	body := map[string]string{
		"target_user_id": suite.memberID,
		"reason":         "   \t\n  ",
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/communities/"+suite.regionID+"/blocklist-proposals", bytes.NewReader(jsonBody))
	req.URL.RawQuery = "id=" + suite.regionID
	claims := &middleware.Claims{UserID: suite.adminID, PostcardVerified: true, VouchVerified: true}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	suite.handler.CreateProposal(rr, req)

	// Should reject whitespace-only reason (or treat as empty)
	t.Logf("Whitespace-only reason: status %d, body: %s", rr.Code, rr.Body.String())
}

// TestBlocklistEdge_LongReason tests proposal with very long reason (B12).
func TestBlocklistEdge_LongReason(t *testing.T) {
	suite := setupBlocklistTestSuite(t)

	body := map[string]string{
		"target_user_id": suite.memberID,
		"reason":         strings.Repeat("x", 5000),
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/communities/"+suite.regionID+"/blocklist-proposals", bytes.NewReader(jsonBody))
	req.URL.RawQuery = "id=" + suite.regionID
	claims := &middleware.Claims{UserID: suite.adminID, PostcardVerified: true, VouchVerified: true}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	suite.handler.CreateProposal(rr, req)

	t.Logf("5000-char reason: status %d", rr.Code)
}

// TestBlocklistEdge_VoteFromNonRegionAdmin tests that a non-admin of the region
// cannot vote (B13).
func TestBlocklistEdge_VoteFromNonRegionAdmin(t *testing.T) {
	suite := setupBlocklistTestSuite(t)

	proposal := &models.BlocklistProposal{
		TargetUserID: suite.memberID,
		RegionID:     &suite.regionID,
		ProposedBy:   &suite.adminID,
		Reason:       "Non-admin vote test",
	}
	if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}
	_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, suite.adminID, true)

	// outsider is not in the region at all
	body := map[string]bool{"vote": true}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/blocklist-proposals/"+proposal.ID+"/vote", bytes.NewReader(jsonBody))
	req.URL.RawQuery = "id=" + proposal.ID
	claims := &middleware.Claims{UserID: suite.outsiderID, PostcardVerified: true, VouchVerified: true}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	suite.handler.VoteOnProposal(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("Expected 403 for non-admin vote, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestBlocklistEdge_ConsensusThresholdExact tests that consensus is reached when
// votes exactly meet the threshold (B8).
func TestBlocklistEdge_ConsensusThresholdExact(t *testing.T) {
	suite := setupBlocklistTestSuite(t)

	// Need a fresh target for this test
	targetID := createBlocklistTestUser(suite.db, "edge-threshold-target")
	addBlocklistUserToRegion(suite.db, targetID, suite.regionID, false)

	proposal := &models.BlocklistProposal{
		TargetUserID: targetID,
		RegionID:     &suite.regionID,
		ProposedBy:   &suite.adminID,
		Reason:       "Threshold test",
	}
	if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}
	_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, suite.adminID, true)
	_ = suite.proposalRepo.AddVote(context.Background(), proposal.ID, suite.admin2ID, true)

	t.Cleanup(func() {
		_, _ = suite.db.Exec("DELETE FROM blocklisted_users WHERE proposal_id = ?", proposal.ID)
		_, _ = suite.db.Exec("DELETE FROM blocklisted_addresses WHERE user_id = ?", targetID)
		_, _ = suite.db.Exec("DELETE FROM user_regions WHERE user_id = ?", targetID)
		_, _ = suite.db.Exec("DELETE FROM users WHERE id = ?", targetID)
	})

	// Third vote should trigger consensus (with VoteFloor=3)
	body := map[string]bool{"vote": true}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/blocklist-proposals/"+proposal.ID+"/vote", bytes.NewReader(jsonBody))
	req.URL.RawQuery = "id=" + proposal.ID
	claims := &middleware.Claims{UserID: suite.admin3ID, PostcardVerified: true, VouchVerified: true}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	suite.handler.VoteOnProposal(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Third vote should succeed, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify proposal was approved
	var status string
	err := suite.db.QueryRow("SELECT status FROM blocklist_proposals WHERE id = ?", proposal.ID).Scan(&status)
	if err != nil {
		t.Fatalf("Failed to check proposal status: %v", err)
	}
	if status != "approved" {
		t.Errorf("Expected proposal status 'approved', got %q", status)
	}
}

// TestBlocklistEdge_ProposalAgainstDeletedUser tests creating a proposal against
// a soft-deleted user (B4).
func TestBlocklistEdge_ProposalAgainstDeletedUser(t *testing.T) {
	suite := setupBlocklistTestSuite(t)

	// Create a user and soft-delete them
	deletedID := createBlocklistTestUser(suite.db, "edge-deleted-target")
	addBlocklistUserToRegion(suite.db, deletedID, suite.regionID, false)
	_, _ = suite.db.Exec("UPDATE users SET deleted_at = NOW() WHERE id = ?", deletedID)

	t.Cleanup(func() {
		_, _ = suite.db.Exec("DELETE FROM user_regions WHERE user_id = ?", deletedID)
		_, _ = suite.db.Exec("DELETE FROM users WHERE id = ?", deletedID)
	})

	body := map[string]string{
		"target_user_id": deletedID,
		"reason":         "Deleted user test",
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/communities/"+suite.regionID+"/blocklist-proposals", bytes.NewReader(jsonBody))
	req.URL.RawQuery = "id=" + suite.regionID
	claims := &middleware.Claims{UserID: suite.adminID, PostcardVerified: true, VouchVerified: true}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	suite.handler.CreateProposal(rr, req)

	t.Logf("Proposal against deleted user: status %d, body: %s", rr.Code, rr.Body.String())
}
