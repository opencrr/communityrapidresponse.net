package database

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// =============================================================================
// Helper Functions
// =============================================================================

func createConnectionTestUser(t *testing.T, db *DB, suffix string) string {
	t.Helper()
	userID := uuid.New().String()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO users (id, username, email, password_hash, verification_tier, created_at) VALUES (?, ?, ?, ?, 2, NOW())",
		userID, "connuser_"+suffix, suffix+"@conntest.com", "$2a$12$testhashedpassword")
	if err != nil {
		t.Fatalf("Failed to create test user %s: %v", suffix, err)
	}
	return userID
}

func createConnectionTestRegion(t *testing.T, db *DB, name string) string {
	t.Helper()
	regionID := uuid.New().String()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO geographic_regions (id, name, region_type, created_at) VALUES (?, ?, 'city', NOW())",
		regionID, name)
	if err != nil {
		t.Fatalf("Failed to create test region %s: %v", name, err)
	}
	return regionID
}

func createConnectionTestGroup(t *testing.T, db *DB, name string, userID, regionID string) string {
	t.Helper()
	groupRepo := NewGroupRepository(db)
	group, err := groupRepo.Create(context.Background(), &models.CreateGroupRequest{
		Name:       name,
		Visibility: "listed",
		RegionIDs:  []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Failed to create test group %s: %v", name, err)
	}
	return group.ID
}

func cleanupConnectionTest(t *testing.T, db *DB, groupIDs, userIDs, regionIDs, connectionIDs []string) {
	t.Helper()
	ctx := context.Background()

	for _, connID := range connectionIDs {
		_, _ = db.ExecContext(ctx, "DELETE FROM connection_proposal_groups WHERE proposal_id IN (SELECT id FROM connection_proposals WHERE connection_id = ?)", connID)
		_, _ = db.ExecContext(ctx, "DELETE FROM connection_proposals WHERE connection_id = ?", connID)
		_, _ = db.ExecContext(ctx, "DELETE FROM connection_members WHERE connection_id = ?", connID)
		_, _ = db.ExecContext(ctx, "DELETE FROM connections WHERE id = ?", connID)
	}
	// Clean orphaned proposals
	_, _ = db.ExecContext(ctx, "DELETE FROM connection_proposal_groups WHERE proposal_id IN (SELECT id FROM connection_proposals WHERE connection_id IS NULL)")
	_, _ = db.ExecContext(ctx, "DELETE FROM connection_proposals WHERE connection_id IS NULL")

	for _, groupID := range groupIDs {
		_, _ = db.ExecContext(ctx, "DELETE FROM group_blocks WHERE blocker_group_id = ? OR blocked_group_id = ?", groupID, groupID)
		_, _ = db.ExecContext(ctx, "DELETE FROM group_topic_tags WHERE group_id = ?", groupID)
		_, _ = db.ExecContext(ctx, "DELETE FROM group_regions WHERE group_id = ?", groupID)
		_, _ = db.ExecContext(ctx, "DELETE FROM group_members WHERE group_id = ?", groupID)
		_, _ = db.ExecContext(ctx, "DELETE FROM `groups` WHERE id = ?", groupID)
	}
	for _, regionID := range regionIDs {
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", regionID)
	}
	for _, userID := range userIDs {
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", userID)
	}
}

// =============================================================================
// ProposeConnection Tests
// =============================================================================

func TestConnectionRepository_ProposeConnection(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "propose1")
	regionID := createConnectionTestRegion(t, db, "Propose Region")
	groupAID := createConnectionTestGroup(t, db, "Propose A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Propose B", userID, regionID)

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID}, []string{userID}, []string{regionID}, nil)
	})

	proposal, err := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		Name:     "Test Connection",
		GroupIDs: []string{groupBID},
	})
	if err != nil {
		t.Fatalf("ProposeConnection failed: %v", err)
	}

	if proposal.ProposalType != "formation" {
		t.Errorf("Expected proposal_type=formation, got %s", proposal.ProposalType)
	}
	if proposal.Status != "pending" {
		t.Errorf("Expected status=pending, got %s", proposal.Status)
	}
	if len(proposal.Groups) != 2 {
		t.Fatalf("Expected 2 group entries, got %d", len(proposal.Groups))
	}

	// Proposer should be accepted, target should be pending
	for _, g := range proposal.Groups {
		if g.GroupID == groupAID && g.Status != "accepted" {
			t.Errorf("Expected proposer status=accepted, got %s", g.Status)
		}
		if g.GroupID == groupBID && g.Status != "pending" {
			t.Errorf("Expected target status=pending, got %s", g.Status)
		}
	}
}

// =============================================================================
// RespondToProposal Tests
// =============================================================================

func TestConnectionRepository_RespondToProposal_Accept(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "respond_accept1")
	regionID := createConnectionTestRegion(t, db, "Respond Accept Region")
	groupAID := createConnectionTestGroup(t, db, "Respond Accept A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Respond Accept B", userID, regionID)

	proposal, err := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	if err != nil {
		t.Fatalf("ProposeConnection failed: %v", err)
	}

	result, err := repo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	if err != nil {
		t.Fatalf("RespondToProposal failed: %v", err)
	}

	if result.Status != "accepted" {
		t.Errorf("Expected status=accepted, got %s", result.Status)
	}
	if result.ConnectionID == nil {
		t.Fatal("Expected connection_id to be set")
	}

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID}, []string{userID}, []string{regionID}, []string{*result.ConnectionID})
	})

	// Verify connection exists with 2 members
	conn, err := repo.GetConnection(ctx, *result.ConnectionID)
	if err != nil {
		t.Fatalf("GetConnection failed: %v", err)
	}
	if len(conn.MemberGroups) != 2 {
		t.Errorf("Expected 2 member groups, got %d", len(conn.MemberGroups))
	}
}

func TestConnectionRepository_RespondToProposal_Decline(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "respond_decline1")
	regionID := createConnectionTestRegion(t, db, "Respond Decline Region")
	groupAID := createConnectionTestGroup(t, db, "Respond Decline A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Respond Decline B", userID, regionID)

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID}, []string{userID}, []string{regionID}, nil)
	})

	proposal, err := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	if err != nil {
		t.Fatalf("ProposeConnection failed: %v", err)
	}

	result, err := repo.RespondToProposal(ctx, proposal.ID, groupBID, false)
	if err != nil {
		t.Fatalf("RespondToProposal failed: %v", err)
	}

	if result.Status != "declined" {
		t.Errorf("Expected status=declined, got %s", result.Status)
	}
	if result.ConnectionID != nil {
		t.Error("Expected connection_id to be nil for declined proposal")
	}
}

// =============================================================================
// ListConnectionsForGroup Tests
// =============================================================================

func TestConnectionRepository_ListConnectionsForGroup(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "list_conn1")
	regionID := createConnectionTestRegion(t, db, "List Conn Region")
	groupAID := createConnectionTestGroup(t, db, "List Conn A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "List Conn B", userID, regionID)

	proposal, _ := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	result, _ := repo.RespondToProposal(ctx, proposal.ID, groupBID, true)

	t.Cleanup(func() {
		var connIDs []string
		if result.ConnectionID != nil {
			connIDs = []string{*result.ConnectionID}
		}
		cleanupConnectionTest(t, db, []string{groupAID, groupBID}, []string{userID}, []string{regionID}, connIDs)
	})

	connections, err := repo.ListConnectionsForGroup(ctx, groupAID)
	if err != nil {
		t.Fatalf("ListConnectionsForGroup failed: %v", err)
	}
	if len(connections) != 1 {
		t.Fatalf("Expected 1 connection, got %d", len(connections))
	}
	if len(connections[0].MemberGroups) != 2 {
		t.Errorf("Expected 2 member groups, got %d", len(connections[0].MemberGroups))
	}
}

// =============================================================================
// InviteToConnection (Expansion) Tests
// =============================================================================

func TestConnectionRepository_InviteToConnection(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "invite_conn1")
	regionID := createConnectionTestRegion(t, db, "Invite Conn Region")
	groupAID := createConnectionTestGroup(t, db, "Invite Conn A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Invite Conn B", userID, regionID)
	groupCID := createConnectionTestGroup(t, db, "Invite Conn C", userID, regionID)

	proposal, _ := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	formResult, _ := repo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	if formResult.ConnectionID == nil {
		t.Fatal("Expected connection to be formed")
	}
	connectionID := *formResult.ConnectionID

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID, groupCID}, []string{userID}, []string{regionID}, []string{connectionID})
	})

	// Invite group C
	expandProposal, err := repo.InviteToConnection(ctx, connectionID, groupAID, groupCID)
	if err != nil {
		t.Fatalf("InviteToConnection failed: %v", err)
	}
	if expandProposal.ProposalType != "expansion" {
		t.Errorf("Expected proposal_type=expansion, got %s", expandProposal.ProposalType)
	}
	// Should have 3 group entries: proposer (accepted), existing member B (pending), target C (pending)
	if len(expandProposal.Groups) != 3 {
		t.Fatalf("Expected 3 group entries, got %d", len(expandProposal.Groups))
	}
}

func TestConnectionRepository_RespondToProposal_Expansion(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "expand1")
	regionID := createConnectionTestRegion(t, db, "Expand Region")
	groupAID := createConnectionTestGroup(t, db, "Expand A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Expand B", userID, regionID)
	groupCID := createConnectionTestGroup(t, db, "Expand C", userID, regionID)

	// Form A+B connection
	proposal, _ := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	formResult, _ := repo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	connectionID := *formResult.ConnectionID

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID, groupCID}, []string{userID}, []string{regionID}, []string{connectionID})
	})

	// Invite C
	expandProposal, _ := repo.InviteToConnection(ctx, connectionID, groupAID, groupCID)

	// B accepts
	_, err := repo.RespondToProposal(ctx, expandProposal.ID, groupBID, true)
	if err != nil {
		t.Fatalf("B accept failed: %v", err)
	}

	// C accepts
	result, err := repo.RespondToProposal(ctx, expandProposal.ID, groupCID, true)
	if err != nil {
		t.Fatalf("C accept failed: %v", err)
	}
	if result.Status != "accepted" {
		t.Errorf("Expected status=accepted, got %s", result.Status)
	}

	// Verify 3 members
	conn, err := repo.GetConnection(ctx, connectionID)
	if err != nil {
		t.Fatalf("GetConnection failed: %v", err)
	}
	if len(conn.MemberGroups) != 3 {
		t.Errorf("Expected 3 member groups, got %d", len(conn.MemberGroups))
	}
}

// =============================================================================
// LeaveConnection Tests
// =============================================================================

func TestConnectionRepository_LeaveConnection(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "leave1")
	regionID := createConnectionTestRegion(t, db, "Leave Region")
	groupAID := createConnectionTestGroup(t, db, "Leave A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Leave B", userID, regionID)
	groupCID := createConnectionTestGroup(t, db, "Leave C", userID, regionID)

	// Form A+B+C
	proposal, _ := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID, groupCID},
	})
	_, _ = repo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	result, _ := repo.RespondToProposal(ctx, proposal.ID, groupCID, true)
	connectionID := *result.ConnectionID

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID, groupCID}, []string{userID}, []string{regionID}, []string{connectionID})
	})

	// A leaves
	err := repo.LeaveConnection(ctx, connectionID, groupAID)
	if err != nil {
		t.Fatalf("LeaveConnection failed: %v", err)
	}

	// Connection should still exist with 2 members
	conn, err := repo.GetConnection(ctx, connectionID)
	if err != nil {
		t.Fatalf("Expected connection to still exist: %v", err)
	}
	if len(conn.MemberGroups) != 2 {
		t.Errorf("Expected 2 remaining members, got %d", len(conn.MemberGroups))
	}
}

func TestConnectionRepository_LeaveConnection_Dissolves(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "dissolve1")
	regionID := createConnectionTestRegion(t, db, "Dissolve Region")
	groupAID := createConnectionTestGroup(t, db, "Dissolve A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Dissolve B", userID, regionID)

	proposal, _ := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	result, _ := repo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	connectionID := *result.ConnectionID

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID}, []string{userID}, []string{regionID}, []string{connectionID})
	})

	// A leaves — only B remains, connection should be deleted
	err := repo.LeaveConnection(ctx, connectionID, groupAID)
	if err != nil {
		t.Fatalf("LeaveConnection failed: %v", err)
	}

	_, err = repo.GetConnection(ctx, connectionID)
	if err != ErrConnectionNotFound {
		t.Errorf("Expected ErrConnectionNotFound, got %v", err)
	}
}

// =============================================================================
// CheckUnanimousBlock Tests
// =============================================================================

func TestConnectionRepository_CheckUnanimousBlock(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	groupRepo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "ublock1")
	regionID := createConnectionTestRegion(t, db, "UBlock Region")
	groupAID := createConnectionTestGroup(t, db, "UBlock A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "UBlock B", userID, regionID)
	groupCID := createConnectionTestGroup(t, db, "UBlock C", userID, regionID)

	// Form A+B+C connection
	proposal, _ := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID, groupCID},
	})
	_, _ = repo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	result, _ := repo.RespondToProposal(ctx, proposal.ID, groupCID, true)
	connectionID := *result.ConnectionID

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID, groupCID}, []string{userID}, []string{regionID}, []string{connectionID})
	})

	// Only A blocks C — not unanimous
	_ = groupRepo.BlockGroup(ctx, groupAID, groupCID)
	shouldRemove, err := repo.CheckUnanimousBlock(ctx, connectionID, groupCID)
	if err != nil {
		t.Fatalf("CheckUnanimousBlock failed: %v", err)
	}
	if shouldRemove {
		t.Error("Expected false when only one group blocks")
	}

	// B also blocks C — now unanimous
	_ = groupRepo.BlockGroup(ctx, groupBID, groupCID)
	shouldRemove, err = repo.CheckUnanimousBlock(ctx, connectionID, groupCID)
	if err != nil {
		t.Fatalf("CheckUnanimousBlock failed: %v", err)
	}
	if !shouldRemove {
		t.Error("Expected true when all other groups block")
	}
}

// =============================================================================
// ListPendingProposalsForGroup Tests
// =============================================================================

func TestConnectionRepository_ListPendingProposalsForGroup(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "pending1")
	regionID := createConnectionTestRegion(t, db, "Pending Region")
	groupAID := createConnectionTestGroup(t, db, "Pending A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Pending B", userID, regionID)

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID}, []string{userID}, []string{regionID}, nil)
	})

	_, err := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	if err != nil {
		t.Fatalf("ProposeConnection failed: %v", err)
	}

	// B should have a pending proposal
	proposals, err := repo.ListPendingProposalsForGroup(ctx, groupBID)
	if err != nil {
		t.Fatalf("ListPendingProposalsForGroup failed: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("Expected 1 pending proposal, got %d", len(proposals))
	}
	if proposals[0].ProposalType != "formation" {
		t.Errorf("Expected proposal_type=formation, got %s", proposals[0].ProposalType)
	}

	// A should NOT have a pending proposal (already accepted)
	proposalsA, err := repo.ListPendingProposalsForGroup(ctx, groupAID)
	if err != nil {
		t.Fatalf("ListPendingProposalsForGroup for A failed: %v", err)
	}
	if len(proposalsA) != 0 {
		t.Errorf("Expected 0 pending proposals for proposer, got %d", len(proposalsA))
	}
}
