package database

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

func setupDeletionTestDB(t *testing.T) *DB {
	t.Helper()

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
		_, _ = db.Exec("DELETE FROM deletion_votes WHERE proposal_id IN (SELECT id FROM deletion_proposals WHERE reason LIKE '%deltest%')")
		_, _ = db.Exec("DELETE FROM deletion_proposals WHERE reason LIKE '%deltest%'")
		_, _ = db.Exec("DELETE FROM signal_groups WHERE group_name LIKE 'deltest-%'")
		_, _ = db.Exec("DELETE FROM user_regions WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'deltest-%')")
		_, _ = db.Exec("DELETE FROM geographic_regions WHERE name LIKE 'deltest-%'")
		_, _ = db.Exec("DELETE FROM users WHERE username LIKE 'deltest-%'")
		_ = db.Close()
	})

	return db
}

func createDeletionTestUser(t *testing.T, db *DB, prefix string) string {
	t.Helper()
	userID := uuid.New().String()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO users (id, username, email, password_hash, verification_tier, postcard_verified, vouch_verified, is_superuser, email_verified, created_at)
		VALUES (?, ?, ?, 'hash', 1, TRUE, FALSE, FALSE, TRUE, NOW())
	`, userID, "deltest-"+prefix+"-"+userID[:8], "deltest-"+prefix+"-"+userID[:8]+"@test.com")
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	return userID
}

func createDeletionTestRegion(t *testing.T, db *DB, creatorID, name string) string {
	t.Helper()
	regionID := uuid.New().String()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO geographic_regions (id, name, region_type, created_by, created_at)
		VALUES (?, ?, 'neighborhood', ?, NOW())
	`, regionID, "deltest-"+name+"-"+regionID[:8], creatorID)
	if err != nil {
		t.Fatalf("Failed to create test region: %v", err)
	}
	addDeletionUserToRegion(t, db, creatorID, regionID, true)
	return regionID
}

func createDeletionTestChildRegion(t *testing.T, db *DB, creatorID, parentID, name string) string {
	t.Helper()
	regionID := uuid.New().String()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO geographic_regions (id, name, region_type, parent_region_id, created_by, created_at)
		VALUES (?, ?, 'city_block', ?, ?, NOW())
	`, regionID, "deltest-"+name+"-"+regionID[:8], parentID, creatorID)
	if err != nil {
		t.Fatalf("Failed to create child region: %v", err)
	}
	return regionID
}

func addDeletionUserToRegion(t *testing.T, db *DB, userID, regionID string, isAdmin bool) {
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

func createDeletionTestSignalGroup(t *testing.T, db *DB, regionID, creatorID, name string) string {
	t.Helper()
	groupID := uuid.New().String()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO signal_groups (id, region_id, group_name, invite_link, created_by, created_at, is_active)
		VALUES (?, ?, ?, 'https://signal.group/test', ?, NOW(), TRUE)
	`, groupID, regionID, "deltest-"+name, creatorID)
	if err != nil {
		t.Fatalf("Failed to create test signal group: %v", err)
	}
	return groupID
}

func TestDeletionProposalRepository_Create(t *testing.T) {
	db := setupDeletionTestDB(t)
	repo := NewDeletionProposalRepository(db)

	userID := createDeletionTestUser(t, db, "proposer")
	regionID := createDeletionTestRegion(t, db, userID, "create")
	groupID := createDeletionTestSignalGroup(t, db, regionID, userID, "create")

	reason := "deltest create reason"
	proposal := &models.DeletionProposal{
		AssetType:  models.AssetTypeSignalGroup,
		AssetID:    groupID,
		RegionID:   &regionID,
		ProposedBy: &userID,
		Reason:     &reason,
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
}

func TestDeletionProposalRepository_GetByID(t *testing.T) {
	db := setupDeletionTestDB(t)
	repo := NewDeletionProposalRepository(db)

	userID := createDeletionTestUser(t, db, "getbyid")
	regionID := createDeletionTestRegion(t, db, userID, "getbyid")
	groupID := createDeletionTestSignalGroup(t, db, regionID, userID, "getbyid")

	reason := "deltest getbyid reason"
	proposal := &models.DeletionProposal{
		AssetType:  models.AssetTypeSignalGroup,
		AssetID:    groupID,
		RegionID:   &regionID,
		ProposedBy: &userID,
		Reason:     &reason,
	}
	if err := repo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	retrieved, err := repo.GetByID(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("Failed to get proposal: %v", err)
	}

	if retrieved.ID != proposal.ID {
		t.Errorf("Expected ID %s, got %s", proposal.ID, retrieved.ID)
	}
	if retrieved.AssetID != groupID {
		t.Errorf("Expected asset ID %s, got %s", groupID, retrieved.AssetID)
	}
	if string(retrieved.AssetType) != string(models.AssetTypeSignalGroup) {
		t.Errorf("Expected asset type signal_group, got %s", retrieved.AssetType)
	}
}

func TestDeletionProposalRepository_GetPendingByAsset(t *testing.T) {
	db := setupDeletionTestDB(t)
	repo := NewDeletionProposalRepository(db)

	userID := createDeletionTestUser(t, db, "pending")
	regionID := createDeletionTestRegion(t, db, userID, "pending")
	groupID := createDeletionTestSignalGroup(t, db, regionID, userID, "pending")

	reason := "deltest pending reason"
	proposal := &models.DeletionProposal{
		AssetType:  models.AssetTypeSignalGroup,
		AssetID:    groupID,
		RegionID:   &regionID,
		ProposedBy: &userID,
		Reason:     &reason,
	}
	if err := repo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Should find the pending proposal
	found, err := repo.GetPendingByAsset(context.Background(), models.AssetTypeSignalGroup, groupID)
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
	found, err = repo.GetPendingByAsset(context.Background(), models.AssetTypeSignalGroup, groupID)
	if err != nil {
		t.Fatalf("Failed to get pending proposal after approval: %v", err)
	}
	if found != nil {
		t.Error("Expected no pending proposal after approval")
	}
}

func TestDeletionProposalRepository_AddVote_CountVotes_HasVoted(t *testing.T) {
	db := setupDeletionTestDB(t)
	repo := NewDeletionProposalRepository(db)

	userID := createDeletionTestUser(t, db, "voter1")
	voter2ID := createDeletionTestUser(t, db, "voter2")
	voter3ID := createDeletionTestUser(t, db, "voter3")
	regionID := createDeletionTestRegion(t, db, userID, "votes")
	groupID := createDeletionTestSignalGroup(t, db, regionID, userID, "votes")

	reason := "deltest vote reason"
	proposal := &models.DeletionProposal{
		AssetType:  models.AssetTypeSignalGroup,
		AssetID:    groupID,
		RegionID:   &regionID,
		ProposedBy: &userID,
		Reason:     &reason,
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

func TestDeletionProposalRepository_UpdateStatus(t *testing.T) {
	db := setupDeletionTestDB(t)
	repo := NewDeletionProposalRepository(db)

	userID := createDeletionTestUser(t, db, "status")
	regionID := createDeletionTestRegion(t, db, userID, "status")
	groupID := createDeletionTestSignalGroup(t, db, regionID, userID, "status")

	reason := "deltest status reason"
	proposal := &models.DeletionProposal{
		AssetType:  models.AssetTypeSignalGroup,
		AssetID:    groupID,
		RegionID:   &regionID,
		ProposedBy: &userID,
		Reason:     &reason,
	}
	if err := repo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Update to approved
	if err := repo.UpdateStatus(context.Background(), proposal.ID, models.ProposalStatusApproved); err != nil {
		t.Fatalf("Failed to update status: %v", err)
	}

	updated, err := repo.GetByID(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("Failed to get updated proposal: %v", err)
	}
	if updated.Status != models.ProposalStatusApproved {
		t.Errorf("Expected status approved, got %s", updated.Status)
	}
	if updated.ResolvedAt == nil {
		t.Error("Expected resolved_at to be set")
	}
}

func TestDeletionProposalRepository_ApplySignalGroupDeletion(t *testing.T) {
	db := setupDeletionTestDB(t)
	repo := NewDeletionProposalRepository(db)

	userID := createDeletionTestUser(t, db, "sgdelete")
	regionID := createDeletionTestRegion(t, db, userID, "sgdelete")
	groupID := createDeletionTestSignalGroup(t, db, regionID, userID, "sgdelete")

	// Verify is_active initially
	var isActive bool
	err := db.QueryRowContext(context.Background(), `SELECT is_active FROM signal_groups WHERE id = ?`, groupID).Scan(&isActive)
	if err != nil {
		t.Fatalf("Failed to check is_active: %v", err)
	}
	if !isActive {
		t.Fatal("Expected signal group to be active initially")
	}

	// Apply deletion in transaction
	err = db.Transaction(context.Background(), func(tx *sql.Tx) error {
		return repo.ApplySignalGroupDeletionTx(context.Background(), tx, groupID)
	})
	if err != nil {
		t.Fatalf("Failed to apply signal group deletion: %v", err)
	}

	// Verify is_active is now false
	err = db.QueryRowContext(context.Background(), `SELECT is_active FROM signal_groups WHERE id = ?`, groupID).Scan(&isActive)
	if err != nil {
		t.Fatalf("Failed to check is_active after deletion: %v", err)
	}
	if isActive {
		t.Error("Expected signal group to be inactive after deletion")
	}
}

func TestDeletionProposalRepository_ApplySubRegionDeletion(t *testing.T) {
	db := setupDeletionTestDB(t)
	repo := NewDeletionProposalRepository(db)

	userID := createDeletionTestUser(t, db, "subdelete")
	parentRegionID := createDeletionTestRegion(t, db, userID, "parent")
	childRegionID := createDeletionTestChildRegion(t, db, userID, parentRegionID, "child")

	// Add user to child region
	addDeletionUserToRegion(t, db, userID, childRegionID, true)

	// Add signal group to child region
	createDeletionTestSignalGroup(t, db, childRegionID, userID, "child-sg")

	// Apply sub-region deletion
	err := db.Transaction(context.Background(), func(tx *sql.Tx) error {
		return repo.ApplySubRegionDeletionTx(context.Background(), tx, childRegionID)
	})
	if err != nil {
		t.Fatalf("Failed to apply sub-region deletion: %v", err)
	}

	// Verify child region is deleted
	var count int
	err = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM geographic_regions WHERE id = ?`, childRegionID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check child region: %v", err)
	}
	if count != 0 {
		t.Error("Expected child region to be deleted")
	}

	// Verify user_regions for child are deleted
	err = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM user_regions WHERE region_id = ?`, childRegionID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check user_regions: %v", err)
	}
	if count != 0 {
		t.Error("Expected user_regions for child to be deleted")
	}

	// Verify signal groups for child are deleted
	err = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM signal_groups WHERE region_id = ?`, childRegionID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check signal_groups: %v", err)
	}
	if count != 0 {
		t.Error("Expected signal groups for child to be deleted")
	}

	// Verify parent region still exists
	err = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM geographic_regions WHERE id = ?`, parentRegionID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check parent region: %v", err)
	}
	if count != 1 {
		t.Error("Expected parent region to still exist")
	}
}

func TestDeletionProposalRepository_GetByIDWithVotes(t *testing.T) {
	db := setupDeletionTestDB(t)
	repo := NewDeletionProposalRepository(db)

	userID := createDeletionTestUser(t, db, "withvotes1")
	voter2ID := createDeletionTestUser(t, db, "withvotes2")
	regionID := createDeletionTestRegion(t, db, userID, "withvotes")
	groupID := createDeletionTestSignalGroup(t, db, regionID, userID, "withvotes")

	reason := "deltest withvotes reason"
	proposal := &models.DeletionProposal{
		AssetType:  models.AssetTypeSignalGroup,
		AssetID:    groupID,
		RegionID:   &regionID,
		ProposedBy: &userID,
		Reason:     &reason,
	}
	if err := repo.Create(context.Background(), proposal); err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Add votes
	if err := repo.AddVote(context.Background(), proposal.ID, userID, true); err != nil {
		t.Fatalf("Failed to add vote 1: %v", err)
	}
	if err := repo.AddVote(context.Background(), proposal.ID, voter2ID, true); err != nil {
		t.Fatalf("Failed to add vote 2: %v", err)
	}

	// Get with votes
	detail, err := repo.GetByIDWithVotes(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("Failed to get with votes: %v", err)
	}

	if detail.ID != proposal.ID {
		t.Errorf("Expected ID %s, got %s", proposal.ID, detail.ID)
	}
	if detail.AssetType != string(models.AssetTypeSignalGroup) {
		t.Errorf("Expected asset type signal_group, got %s", detail.AssetType)
	}
	if detail.AssetName == "" || detail.AssetName == "Unknown" {
		t.Error("Expected asset name to be resolved")
	}
	if detail.CurrentVotes != 2 {
		t.Errorf("Expected 2 current votes, got %d", detail.CurrentVotes)
	}
	if len(detail.Votes) != 2 {
		t.Errorf("Expected 2 vote details, got %d", len(detail.Votes))
	}
}
