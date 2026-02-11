package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

func TestBlocklistProposalRepository_Create(t *testing.T) {
	db := setupTestDB(t)
	blocklistConfig := &config.BlocklistConfig{
		AddressBlocklistDuration:  2 * 365 * 24 * time.Hour,
		ProposalRateLimitPerMonth: 5,
	}
	repo := NewBlocklistProposalRepository(db, blocklistConfig)

	// Create test user and region
	userID := createTestUser(t, db, "blocklist-proposer")
	targetUserID := createTestUser(t, db, "blocklist-target")
	regionID := createTestRegion(t, db, userID, "Test Region")

	// Add target user to region
	addUserToRegion(t, db, targetUserID, regionID, false)

	proposal := &models.BlocklistProposal{
		TargetUserID: targetUserID,
		RegionID:     &regionID,
		ProposedBy:   &userID,
		Reason:       "Test reason",
		Evidence:     blocklistStrPtr("Test evidence"),
	}

	err := repo.Create(context.Background(), proposal)
	if err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	if proposal.ID == "" {
		t.Error("Expected proposal ID to be set")
	}
	if proposal.Status != models.ProposalStatusPending {
		t.Errorf("Expected status pending, got %s", proposal.Status)
	}
	if proposal.ExpiresAt.Before(proposal.CreatedAt) {
		t.Error("ExpiresAt should be after CreatedAt")
	}
}

func TestBlocklistProposalRepository_GetByID(t *testing.T) {
	db := setupTestDB(t)
	blocklistConfig := &config.BlocklistConfig{
		AddressBlocklistDuration:  2 * 365 * 24 * time.Hour,
		ProposalRateLimitPerMonth: 5,
	}
	repo := NewBlocklistProposalRepository(db, blocklistConfig)

	// Create test data
	userID := createTestUser(t, db, "blocklist-proposer-get")
	targetUserID := createTestUser(t, db, "blocklist-target-get")
	regionID := createTestRegion(t, db, userID, "Test Region Get")
	addUserToRegion(t, db, targetUserID, regionID, false)

	proposal := &models.BlocklistProposal{
		TargetUserID: targetUserID,
		RegionID:     &regionID,
		ProposedBy:   &userID,
		Reason:       "Test reason",
	}
	if err := repo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Retrieve proposal
	retrieved, err := repo.GetByID(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("Failed to get proposal: %v", err)
	}

	if retrieved.ID != proposal.ID {
		t.Errorf("Expected ID %s, got %s", proposal.ID, retrieved.ID)
	}
	if retrieved.TargetUserID != targetUserID {
		t.Errorf("Expected target user %s, got %s", targetUserID, retrieved.TargetUserID)
	}
	if retrieved.Reason != "Test reason" {
		t.Errorf("Expected reason 'Test reason', got %s", retrieved.Reason)
	}
}

func TestBlocklistProposalRepository_GetPendingByTargetUser(t *testing.T) {
	db := setupTestDB(t)
	blocklistConfig := &config.BlocklistConfig{
		AddressBlocklistDuration:  2 * 365 * 24 * time.Hour,
		ProposalRateLimitPerMonth: 5,
	}
	repo := NewBlocklistProposalRepository(db, blocklistConfig)

	userID := createTestUser(t, db, "blocklist-proposer-pending")
	targetUserID := createTestUser(t, db, "blocklist-target-pending")
	regionID := createTestRegion(t, db, userID, "Test Region Pending")
	addUserToRegion(t, db, targetUserID, regionID, false)

	// Create a pending proposal
	proposal := &models.BlocklistProposal{
		TargetUserID: targetUserID,
		RegionID:     &regionID,
		ProposedBy:   &userID,
		Reason:       "Pending proposal",
	}
	if err := repo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Should find the pending proposal
	found, err := repo.GetPendingByTargetUser(context.Background(), targetUserID, regionID)
	if err != nil {
		t.Fatalf("Failed to get pending proposal: %v", err)
	}
	if found == nil {
		t.Fatal("Expected to find pending proposal")
	}
	if found.ID != proposal.ID {
		t.Errorf("Expected ID %s, got %s", proposal.ID, found.ID)
	}

	// Approve the proposal
	if err := repo.UpdateStatus(context.Background(), proposal.ID, models.ProposalStatusApproved); err != nil {
		t.Fatalf("Failed to update status: %v", err)
	}

	// Should not find the proposal now
	found, err = repo.GetPendingByTargetUser(context.Background(), targetUserID, regionID)
	if err != nil {
		t.Fatalf("Failed to get pending proposal after approval: %v", err)
	}
	if found != nil {
		t.Error("Expected no pending proposal after approval")
	}
}

func TestBlocklistProposalRepository_AddVote_CountVotes_HasVoted(t *testing.T) {
	db := setupTestDB(t)
	blocklistConfig := &config.BlocklistConfig{
		AddressBlocklistDuration:  2 * 365 * 24 * time.Hour,
		ProposalRateLimitPerMonth: 5,
	}
	repo := NewBlocklistProposalRepository(db, blocklistConfig)

	userID := createTestUser(t, db, "blocklist-proposer-vote")
	voter2ID := createTestUser(t, db, "blocklist-voter2")
	voter3ID := createTestUser(t, db, "blocklist-voter3")
	targetUserID := createTestUser(t, db, "blocklist-target-vote")
	regionID := createTestRegion(t, db, userID, "Test Region Vote")
	addUserToRegion(t, db, targetUserID, regionID, false)

	proposal := &models.BlocklistProposal{
		TargetUserID: targetUserID,
		RegionID:     &regionID,
		ProposedBy:   &userID,
		Reason:       "Vote test",
	}
	if err := repo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Initially no votes
	count, err := repo.CountApprovalVotes(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("Failed to count votes: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 votes, got %d", count)
	}

	// Add approval vote
	if err := repo.AddVote(context.Background(), proposal.ID, userID, true); err != nil {
		t.Fatalf("Failed to add vote: %v", err)
	}

	// Check vote count
	count, err = repo.CountApprovalVotes(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("Failed to count votes: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 vote, got %d", count)
	}

	// Check HasVoted
	hasVoted, err := repo.HasVoted(context.Background(), proposal.ID, userID)
	if err != nil {
		t.Fatalf("Failed to check HasVoted: %v", err)
	}
	if !hasVoted {
		t.Error("Expected user to have voted")
	}

	hasVoted, err = repo.HasVoted(context.Background(), proposal.ID, voter2ID)
	if err != nil {
		t.Fatalf("Failed to check HasVoted for voter2: %v", err)
	}
	if hasVoted {
		t.Error("Expected voter2 to not have voted")
	}

	// Add rejection vote - should not count toward approval
	if err := repo.AddVote(context.Background(), proposal.ID, voter2ID, false); err != nil {
		t.Fatalf("Failed to add rejection vote: %v", err)
	}

	count, err = repo.CountApprovalVotes(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("Failed to count votes after rejection: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected still 1 approval vote, got %d", count)
	}

	// Add another approval vote
	if err := repo.AddVote(context.Background(), proposal.ID, voter3ID, true); err != nil {
		t.Fatalf("Failed to add third vote: %v", err)
	}

	count, err = repo.CountApprovalVotes(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("Failed to count final votes: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 approval votes, got %d", count)
	}
}

func TestBlocklistProposalRepository_CountProposalsThisMonth(t *testing.T) {
	db := setupTestDB(t)
	blocklistConfig := &config.BlocklistConfig{
		AddressBlocklistDuration:  2 * 365 * 24 * time.Hour,
		ProposalRateLimitPerMonth: 5,
	}
	repo := NewBlocklistProposalRepository(db, blocklistConfig)

	userID := createTestUser(t, db, "blocklist-proposer-month")
	targetUserID := createTestUser(t, db, "blocklist-target-month")
	regionID := createTestRegion(t, db, userID, "Test Region Month")
	addUserToRegion(t, db, targetUserID, regionID, false)

	// Initially no proposals
	count, err := repo.CountProposalsThisMonth(context.Background(), userID)
	if err != nil {
		t.Fatalf("Failed to count proposals: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 proposals, got %d", count)
	}

	// Create a proposal
	proposal := &models.BlocklistProposal{
		TargetUserID: targetUserID,
		RegionID:     &regionID,
		ProposedBy:   &userID,
		Reason:       "Month test",
	}
	if err := repo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Should now have 1 proposal
	count, err = repo.CountProposalsThisMonth(context.Background(), userID)
	if err != nil {
		t.Fatalf("Failed to count proposals after create: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 proposal, got %d", count)
	}
}

func TestBlocklistProposalRepository_ApplyBlocklist(t *testing.T) {
	db := setupTestDB(t)
	blocklistConfig := &config.BlocklistConfig{
		AddressBlocklistDuration:  2 * 365 * 24 * time.Hour,
		ProposalRateLimitPerMonth: 5,
	}
	repo := NewBlocklistProposalRepository(db, blocklistConfig)
	userRepo := NewUserRepository(db)

	adminID := createTestUser(t, db, "blocklist-admin-apply")
	targetUserID := createTestUser(t, db, "blocklist-target-apply")
	regionID := createTestRegion(t, db, adminID, "Test Region Apply")
	region2ID := createTestRegion(t, db, adminID, "Test Region Apply 2")

	// Add target user to both regions
	addUserToRegion(t, db, targetUserID, regionID, false)
	addUserToRegion(t, db, targetUserID, region2ID, false)

	// Set an address hash for the user
	addressHash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := userRepo.SetAddressHash(context.Background(), targetUserID, addressHash); err != nil {
		t.Fatalf("Failed to set address hash: %v", err)
	}

	proposal := &models.BlocklistProposal{
		TargetUserID: targetUserID,
		RegionID:     &regionID,
		ProposedBy:   &adminID,
		Reason:       "Apply test",
	}
	if err := repo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Apply the blocklist
	err := repo.ApplyBlocklist(context.Background(), targetUserID, regionID, proposal.ID, adminID, "Test blocklist reason")
	if err != nil {
		t.Fatalf("Failed to apply blocklist: %v", err)
	}

	// Verify user is blocklisted
	user, err := userRepo.GetByID(context.Background(), targetUserID)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}
	if !user.IsBlocked {
		t.Error("Expected user to be blocklisted")
	}
	if user.BlockReason == nil || *user.BlockReason != "Test blocklist reason" {
		t.Error("Expected blocklist reason to be set")
	}

	// Verify user is removed from ALL regions
	inRegion1, err := repo.IsUserInRegion(context.Background(), targetUserID, regionID)
	if err != nil {
		t.Fatalf("Failed to check region 1: %v", err)
	}
	if inRegion1 {
		t.Error("Expected user to be removed from region 1")
	}

	inRegion2, err := repo.IsUserInRegion(context.Background(), targetUserID, region2ID)
	if err != nil {
		t.Fatalf("Failed to check region 2: %v", err)
	}
	if inRegion2 {
		t.Error("Expected user to be removed from region 2")
	}

	// Verify address is blocklisted
	isBlocklisted, err := repo.IsAddressBlocked(context.Background(), addressHash)
	if err != nil {
		t.Fatalf("Failed to check address blocklist: %v", err)
	}
	if !isBlocklisted {
		t.Error("Expected address to be blocklisted")
	}
}

func TestBlocklistProposalRepository_IsAddressBlocked(t *testing.T) {
	db := setupTestDB(t)
	blocklistConfig := &config.BlocklistConfig{
		AddressBlocklistDuration:  2 * 365 * 24 * time.Hour,
		ProposalRateLimitPerMonth: 5,
	}
	repo := NewBlocklistProposalRepository(db, blocklistConfig)

	addressHash := "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

	// Initially not blocklisted
	isBlocklisted, err := repo.IsAddressBlocked(context.Background(), addressHash)
	if err != nil {
		t.Fatalf("Failed to check blocklist: %v", err)
	}
	if isBlocklisted {
		t.Error("Expected address to not be blocklisted initially")
	}

	// Add to blocklist
	userID := createTestUser(t, db, "blocklist-addr-user")
	blocklistID := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour)
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO blocklisted_addresses (id, address_hash, user_id, blocklisted_at, expires_at)
		VALUES (?, ?, ?, NOW(), ?)
	`, blocklistID, addressHash, userID, expiresAt)
	if err != nil {
		t.Fatalf("Failed to insert blocklisted address: %v", err)
	}

	// Should now be blocklisted
	isBlocklisted, err = repo.IsAddressBlocked(context.Background(), addressHash)
	if err != nil {
		t.Fatalf("Failed to check blocklist after insert: %v", err)
	}
	if !isBlocklisted {
		t.Error("Expected address to be blocklisted")
	}
}

func TestBlocklistProposalRepository_ExpireAddress(t *testing.T) {
	db := setupTestDB(t)
	blocklistConfig := &config.BlocklistConfig{
		AddressBlocklistDuration:  2 * 365 * 24 * time.Hour,
		ProposalRateLimitPerMonth: 5,
	}
	repo := NewBlocklistProposalRepository(db, blocklistConfig)

	addressHash := "1111111122222222333333334444444455555555666666667777777788888888"
	userID := createTestUser(t, db, "blocklist-expire-user")

	// Add to blocklist
	blocklistID := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour)
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO blocklisted_addresses (id, address_hash, user_id, blocklisted_at, expires_at)
		VALUES (?, ?, ?, NOW(), ?)
	`, blocklistID, addressHash, userID, expiresAt)
	if err != nil {
		t.Fatalf("Failed to insert blocklisted address: %v", err)
	}

	// Verify it's blocklisted
	isBlocklisted, err := repo.IsAddressBlocked(context.Background(), addressHash)
	if err != nil {
		t.Fatalf("Failed to check blocklist: %v", err)
	}
	if !isBlocklisted {
		t.Error("Expected address to be blocklisted before expire")
	}

	// Expire the address
	if err := repo.ExpireAddress(context.Background(), addressHash); err != nil {
		t.Fatalf("Failed to expire address: %v", err)
	}

	// Should no longer be blocklisted
	isBlocklisted, err = repo.IsAddressBlocked(context.Background(), addressHash)
	if err != nil {
		t.Fatalf("Failed to check blocklist after expire: %v", err)
	}
	if isBlocklisted {
		t.Error("Expected address to not be blocklisted after expire")
	}
}

func TestBlocklistProposalRepository_ExpirePendingProposals(t *testing.T) {
	db := setupTestDB(t)
	blocklistConfig := &config.BlocklistConfig{
		AddressBlocklistDuration:  2 * 365 * 24 * time.Hour,
		ProposalRateLimitPerMonth: 5,
	}
	repo := NewBlocklistProposalRepository(db, blocklistConfig)

	userID := createTestUser(t, db, "blocklist-expire-proposer")
	targetUserID := createTestUser(t, db, "blocklist-expire-target")
	regionID := createTestRegion(t, db, userID, "Test Region Expire")
	addUserToRegion(t, db, targetUserID, regionID, false)

	// Create a proposal
	proposal := &models.BlocklistProposal{
		TargetUserID: targetUserID,
		RegionID:     &regionID,
		ProposedBy:   &userID,
		Reason:       "Expire test",
	}
	if err := repo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Manually set expires_at to the past
	_, err := db.ExecContext(context.Background(), `
		UPDATE blocklist_proposals SET expires_at = DATE_SUB(NOW(), INTERVAL 1 DAY) WHERE id = ?
	`, proposal.ID)
	if err != nil {
		t.Fatalf("Failed to backdate proposal: %v", err)
	}

	// Run expiration
	expired, err := repo.ExpirePendingProposals(context.Background())
	if err != nil {
		t.Fatalf("Failed to expire proposals: %v", err)
	}
	if expired != 1 {
		t.Errorf("Expected 1 expired proposal, got %d", expired)
	}

	// Verify proposal status
	updated, err := repo.GetByID(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("Failed to get proposal: %v", err)
	}
	if updated.Status != models.ProposalStatusExpired {
		t.Errorf("Expected status expired, got %s", updated.Status)
	}
}

// Helper functions for tests

func blocklistStrPtr(s string) *string {
	return &s
}

func createTestUser(t *testing.T, db *DB, prefix string) string {
	t.Helper()
	userID := uuid.New().String()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO users (id, username, email, password_hash, verification_tier, postcard_verified, vouch_verified, is_superuser, email_verified, created_at)
		VALUES (?, ?, ?, 'hash', 1, TRUE, FALSE, FALSE, TRUE, NOW())
	`, userID, prefix+"-"+userID[:8], prefix+"-"+userID[:8]+"@test.com")
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	return userID
}

func createTestRegion(t *testing.T, db *DB, creatorID, name string) string {
	t.Helper()
	regionID := uuid.New().String()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO geographic_regions (id, name, region_type, created_by, created_at)
		VALUES (?, ?, 'neighborhood', ?, NOW())
	`, regionID, name+"-"+regionID[:8], creatorID)
	if err != nil {
		t.Fatalf("Failed to create test region: %v", err)
	}
	// Add creator as admin
	addUserToRegion(t, db, creatorID, regionID, true)
	return regionID
}

func addUserToRegion(t *testing.T, db *DB, userID, regionID string, isAdmin bool) {
	t.Helper()
	urID := uuid.New().String()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO user_regions (id, user_id, region_id, is_admin, verified_at)
		VALUES (?, ?, ?, ?, NOW())
	`, urID, userID, regionID, isAdmin)
	if err != nil {
		t.Fatalf("Failed to add user to region: %v", err)
	}
}

func setupTestDB(t *testing.T) *DB {
	t.Helper()

	// Use the test database
	dsn := "root:@tcp(127.0.0.1:3306)/communityrapidresponse_test?charset=utf8mb4&parseTime=true&loc=UTC"
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skip("Test database not available")
	}

	if err := sqlDB.Ping(); err != nil {
		t.Skip("Test database not available")
	}

	db := &DB{sqlDB}

	t.Cleanup(func() {
		// Clean up test data
		_, _ = db.Exec("DELETE FROM blocklist_votes WHERE proposal_id IN (SELECT id FROM blocklist_proposals WHERE reason LIKE '%test%')")
		_, _ = db.Exec("DELETE FROM blocklisted_addresses WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'blocklist-%')")
		_, _ = db.Exec("DELETE FROM blocklisted_users WHERE proposal_id IN (SELECT id FROM blocklist_proposals WHERE reason LIKE '%test%')")
		_, _ = db.Exec("DELETE FROM blocklist_proposals WHERE reason LIKE '%test%'")
		_, _ = db.Exec("DELETE FROM user_regions WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'blocklist-%')")
		_, _ = db.Exec("DELETE FROM geographic_regions WHERE name LIKE 'Test Region%'")
		_, _ = db.Exec("DELETE FROM users WHERE username LIKE 'blocklist-%'")
		_ = db.Close()
	})

	return db
}
