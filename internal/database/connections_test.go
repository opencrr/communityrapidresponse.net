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
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE connection_id = ?", connID)
		_, _ = db.ExecContext(ctx, "DELETE FROM connection_chat_proposal_votes WHERE proposal_id IN (SELECT id FROM connection_chat_proposals WHERE connection_id = ?)", connID)
		_, _ = db.ExecContext(ctx, "DELETE FROM connection_chat_proposals WHERE connection_id = ?", connID)
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

// =============================================================================
// ProposeSignalChat Tests
// =============================================================================

func TestConnectionRepository_ProposeSignalChat(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "propose_chat1")
	regionID := createConnectionTestRegion(t, db, "Propose Chat Region")
	groupAID := createConnectionTestGroup(t, db, "Propose Chat A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Propose Chat B", userID, regionID)

	// Form connection
	proposal, _ := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	formResult, _ := repo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	connectionID := *formResult.ConnectionID

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID}, []string{userID}, []string{regionID}, []string{connectionID})
	})

	chatProposal, err := repo.ProposeSignalChat(ctx, connectionID, groupAID, &models.ProposeConnectionChatRequest{
		GroupName:   "Test Chat",
		Description: "A test chat",
		AccessLevel: "admin_only",
	})
	if err != nil {
		t.Fatalf("ProposeSignalChat failed: %v", err)
	}
	if chatProposal.Status != "pending" {
		t.Errorf("Expected status=pending, got %s", chatProposal.Status)
	}
	if chatProposal.GroupName != "Test Chat" {
		t.Errorf("Expected group_name=Test Chat, got %s", chatProposal.GroupName)
	}
	if len(chatProposal.Votes) != 2 {
		t.Fatalf("Expected 2 votes, got %d", len(chatProposal.Votes))
	}

	// Proposer should be auto-approved
	for _, v := range chatProposal.Votes {
		if v.GroupID == groupAID && v.Status != "approved" {
			t.Errorf("Expected proposer vote=approved, got %s", v.Status)
		}
		if v.GroupID == groupBID && v.Status != "pending" {
			t.Errorf("Expected target vote=pending, got %s", v.Status)
		}
	}
}

func TestConnectionRepository_VoteOnChatProposal_Approve(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "vote_chat_approve1")
	regionID := createConnectionTestRegion(t, db, "Vote Chat Approve Region")
	groupAID := createConnectionTestGroup(t, db, "Vote Chat Approve A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Vote Chat Approve B", userID, regionID)

	// Form connection
	proposal, _ := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	formResult, _ := repo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	connectionID := *formResult.ConnectionID

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID}, []string{userID}, []string{regionID}, []string{connectionID})
	})

	// Propose chat
	chatProposal, _ := repo.ProposeSignalChat(ctx, connectionID, groupAID, &models.ProposeConnectionChatRequest{
		GroupName:   "Approved Chat",
		AccessLevel: "all_members",
	})

	// B votes approve (should trigger creation since A auto-approved)
	result, err := repo.VoteOnChatProposal(ctx, chatProposal.ID, groupBID, true)
	if err != nil {
		t.Fatalf("VoteOnChatProposal failed: %v", err)
	}
	if result.Status != "approved" {
		t.Errorf("Expected status=approved, got %s", result.Status)
	}

	// Verify signal group was created
	signalGroups, err := repo.ListConnectionSignalGroups(ctx, connectionID)
	if err != nil {
		t.Fatalf("ListConnectionSignalGroups failed: %v", err)
	}
	if len(signalGroups) != 1 {
		t.Fatalf("Expected 1 signal group, got %d", len(signalGroups))
	}
	if signalGroups[0].GroupName != "Approved Chat" {
		t.Errorf("Expected group_name=Approved Chat, got %s", signalGroups[0].GroupName)
	}
	if signalGroups[0].ConnectionID == nil || *signalGroups[0].ConnectionID != connectionID {
		t.Error("Expected connection_id to be set")
	}
}

// TestGroupRepository_BlockGroup_EvictsSharedConnection verifies a single block
// severs the connection edge directly (issue #10): blocking a group removes it
// from connections the blocker shares with it, dissolving 2-member connections.
func TestGroupRepository_BlockGroup_EvictsSharedConnection(t *testing.T) {
	db := testDB(t)
	connRepo := NewConnectionRepository(db)
	groupRepo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "block_evict_user")
	regionID := createConnectionTestRegion(t, db, "Block Evict Region")
	groupA := createConnectionTestGroup(t, db, "Block Evict A", userID, regionID)
	groupB := createConnectionTestGroup(t, db, "Block Evict B", userID, regionID)
	groupC := createConnectionTestGroup(t, db, "Block Evict C", userID, regionID)

	// 3-group connection so it survives one eviction (drops to 2, not dissolved).
	proposal, _ := connRepo.ProposeConnection(ctx, groupA, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupB, groupC},
	})
	_, _ = connRepo.RespondToProposal(ctx, proposal.ID, groupB, true)
	formResult, _ := connRepo.RespondToProposal(ctx, proposal.ID, groupC, true)
	connectionID := *formResult.ConnectionID
	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupA, groupB, groupC}, []string{userID}, []string{regionID}, []string{connectionID})
	})

	// A blocks C — C must be evicted from the shared connection.
	if err := groupRepo.BlockGroup(ctx, groupA, groupC); err != nil {
		t.Fatalf("BlockGroup failed: %v", err)
	}

	conn, err := connRepo.GetConnection(ctx, connectionID)
	if err != nil {
		t.Fatalf("GetConnection failed: %v", err)
	}
	for _, m := range conn.MemberGroups {
		if m.GroupID == groupC {
			t.Fatalf("blocked group C was not evicted from the shared connection")
		}
	}
	if len(conn.MemberGroups) != 2 {
		t.Errorf("expected 2 remaining members after eviction, got %d", len(conn.MemberGroups))
	}
}

func TestConnectionRepository_VoteOnChatProposal_AdminOnlyTier(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "vote_chat_adminonly1")
	regionID := createConnectionTestRegion(t, db, "Vote Chat AdminOnly Region")
	groupAID := createConnectionTestGroup(t, db, "Vote Chat AdminOnly A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Vote Chat AdminOnly B", userID, regionID)

	proposal, _ := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	formResult, _ := repo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	connectionID := *formResult.ConnectionID

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID}, []string{userID}, []string{regionID}, []string{connectionID})
	})

	// Propose an admin_only chat.
	chatProposal, _ := repo.ProposeSignalChat(ctx, connectionID, groupAID, &models.ProposeConnectionChatRequest{
		GroupName:   "Admin Only Chat",
		AccessLevel: "admin_only",
	})

	result, err := repo.VoteOnChatProposal(ctx, chatProposal.ID, groupBID, true)
	if err != nil {
		t.Fatalf("VoteOnChatProposal failed: %v", err)
	}
	if result.Status != "approved" {
		t.Fatalf("Expected status=approved, got %s", result.Status)
	}

	signalGroups, err := repo.ListConnectionSignalGroups(ctx, connectionID)
	if err != nil {
		t.Fatalf("ListConnectionSignalGroups failed: %v", err)
	}
	if len(signalGroups) != 1 {
		t.Fatalf("Expected 1 signal group, got %d", len(signalGroups))
	}
	// The approved admin_only proposal must NOT become member-visible.
	if signalGroups[0].AccessTier == models.AccessTierMember {
		t.Errorf("admin_only proposal created a member-tier signal group; access level was discarded")
	}
	if signalGroups[0].AccessTier != models.AccessTierAdminOnly {
		t.Errorf("Expected access_tier=admin_only, got %s", signalGroups[0].AccessTier)
	}
}

func TestConnectionRepository_VoteOnChatProposal_AllMembersTier(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "vote_chat_allmembers1")
	regionID := createConnectionTestRegion(t, db, "Vote Chat AllMembers Region")
	groupAID := createConnectionTestGroup(t, db, "Vote Chat AllMembers A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Vote Chat AllMembers B", userID, regionID)

	proposal, _ := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	formResult, _ := repo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	connectionID := *formResult.ConnectionID

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID}, []string{userID}, []string{regionID}, []string{connectionID})
	})

	chatProposal, _ := repo.ProposeSignalChat(ctx, connectionID, groupAID, &models.ProposeConnectionChatRequest{
		GroupName:   "All Members Chat",
		AccessLevel: "all_members",
	})

	if _, err := repo.VoteOnChatProposal(ctx, chatProposal.ID, groupBID, true); err != nil {
		t.Fatalf("VoteOnChatProposal failed: %v", err)
	}

	signalGroups, err := repo.ListConnectionSignalGroups(ctx, connectionID)
	if err != nil {
		t.Fatalf("ListConnectionSignalGroups failed: %v", err)
	}
	if len(signalGroups) != 1 {
		t.Fatalf("Expected 1 signal group, got %d", len(signalGroups))
	}
	if signalGroups[0].AccessTier != models.AccessTierMember {
		t.Errorf("Expected all_members to map to access_tier=member, got %s", signalGroups[0].AccessTier)
	}
}

func TestConnectionRepository_VoteOnChatProposal_Decline(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "vote_chat_decline1")
	regionID := createConnectionTestRegion(t, db, "Vote Chat Decline Region")
	groupAID := createConnectionTestGroup(t, db, "Vote Chat Decline A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Vote Chat Decline B", userID, regionID)

	// Form connection
	proposal, _ := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	formResult, _ := repo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	connectionID := *formResult.ConnectionID

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID}, []string{userID}, []string{regionID}, []string{connectionID})
	})

	// Propose chat
	chatProposal, _ := repo.ProposeSignalChat(ctx, connectionID, groupAID, &models.ProposeConnectionChatRequest{
		GroupName:   "Declined Chat",
		AccessLevel: "admin_only",
	})

	// B declines
	result, err := repo.VoteOnChatProposal(ctx, chatProposal.ID, groupBID, false)
	if err != nil {
		t.Fatalf("VoteOnChatProposal failed: %v", err)
	}
	if result.Status != "declined" {
		t.Errorf("Expected status=declined, got %s", result.Status)
	}

	// No signal group should be created
	signalGroups, err := repo.ListConnectionSignalGroups(ctx, connectionID)
	if err != nil {
		t.Fatalf("ListConnectionSignalGroups failed: %v", err)
	}
	if len(signalGroups) != 0 {
		t.Errorf("Expected 0 signal groups, got %d", len(signalGroups))
	}
}

func TestConnectionRepository_ListChatProposals(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "list_chat1")
	regionID := createConnectionTestRegion(t, db, "List Chat Region")
	groupAID := createConnectionTestGroup(t, db, "List Chat A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "List Chat B", userID, regionID)

	// Form connection
	proposal, _ := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	formResult, _ := repo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	connectionID := *formResult.ConnectionID

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID}, []string{userID}, []string{regionID}, []string{connectionID})
	})

	// Propose a chat
	_, _ = repo.ProposeSignalChat(ctx, connectionID, groupAID, &models.ProposeConnectionChatRequest{
		GroupName:   "Listed Chat",
		AccessLevel: "admin_only",
	})

	proposals, err := repo.ListChatProposals(ctx, connectionID)
	if err != nil {
		t.Fatalf("ListChatProposals failed: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("Expected 1 proposal, got %d", len(proposals))
	}
	if proposals[0].GroupName != "Listed Chat" {
		t.Errorf("Expected group_name=Listed Chat, got %s", proposals[0].GroupName)
	}
	if len(proposals[0].Votes) != 2 {
		t.Errorf("Expected 2 votes, got %d", len(proposals[0].Votes))
	}
}

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

// =============================================================================
// Connection Chat User Access Predicate Tests
// =============================================================================

func TestConnectionChatUserAccessPredicate_AllMembers(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "chat_access_allmembers")
	regionID := createConnectionTestRegion(t, db, "Chat Access AllMembers Region")
	groupAID := createConnectionTestGroup(t, db, "Chat Access AllMembers A", userID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Chat Access AllMembers B", userID, regionID)

	// Form connection
	proposal, _ := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	formResult, _ := repo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	connectionID := *formResult.ConnectionID

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID}, []string{userID}, []string{regionID}, []string{connectionID})
	})

	// Get the predicate for all_members access level
	predicate := ConnectionChatUserAccessPredicate(string(models.ConnectionAccessLevelAllMembers))

	// Query to find users who can access
	var count int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM ("+predicate+") AS eligible_users",
		connectionID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Both groups have the same user, so expect 1 eligible user
	if count != 1 {
		t.Errorf("Expected 1 eligible user for all_members, got %d", count)
	}
}

func TestConnectionChatUserAccessPredicate_AdminOnly(t *testing.T) {
	db := testDB(t)
	repo := NewConnectionRepository(db)
	ctx := context.Background()

	// Create two users: one admin, one regular member
	adminUserID := createConnectionTestUser(t, db, "chat_access_admin")
	memberUserID := createConnectionTestUser(t, db, "chat_access_member")
	regionID := createConnectionTestRegion(t, db, "Chat Access AdminOnly Region")

	// Group A with admin user
	groupAID := createConnectionTestGroup(t, db, "Chat Access AdminOnly A", adminUserID, regionID)

	// Group B with member user
	groupBID := uuid.New().String()
	_, _ = db.ExecContext(ctx,
		"INSERT INTO `groups` (id, name, status, visibility, created_at, updated_at) VALUES (?, ?, 'active', 'listed', NOW(), NOW())",
		groupBID, "Chat Access AdminOnly B",
	)
	_, _ = db.ExecContext(ctx,
		"INSERT INTO group_regions (id, group_id, region_id) VALUES (?, ?, ?)",
		uuid.New().String(), groupBID, regionID,
	)
	// Add member user as regular member (not admin)
	_, _ = db.ExecContext(ctx,
		"INSERT INTO group_members (id, group_id, user_id, is_admin, joined_at) VALUES (?, ?, ?, FALSE, NOW())",
		uuid.New().String(), groupBID, memberUserID,
	)

	// Form connection with both groups
	proposal, _ := repo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID},
	})
	formResult, _ := repo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	connectionID := *formResult.ConnectionID

	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupAID, groupBID}, []string{adminUserID, memberUserID}, []string{regionID}, []string{connectionID})
	})

	// Get the predicate for admin_only access level
	predicate := ConnectionChatUserAccessPredicate(string(models.ConnectionAccessLevelAdminOnly))

	// Query to find admins who can access
	var count int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM ("+predicate+") AS eligible_users",
		connectionID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Only the admin user should be eligible
	if count != 1 {
		t.Errorf("Expected 1 admin eligible user, got %d", count)
	}

	// Verify it's the admin user
	var foundUserID string
	err = db.QueryRowContext(ctx,
		"SELECT user_id FROM ("+predicate+") AS eligible_users LIMIT 1",
		connectionID,
	).Scan(&foundUserID)
	if err != nil {
		t.Fatalf("Query to verify user failed: %v", err)
	}
	if foundUserID != adminUserID {
		t.Errorf("Expected admin user %s, got %s", adminUserID, foundUserID)
	}
}

// TestConnectionRepository_LeaveConnection_RevokesSecrets verifies that leaving a connection
// revokes secret keys for the leaving group and flags rekey for survivors.
func TestConnectionRepository_LeaveConnection_RevokesSecrets(t *testing.T) {
	db := testDB(t)
	secretRepo := NewEncryptedSecretRepository(db)
	connRepo := NewConnectionRepository(db)
	ctx := context.Background()

	user1 := encCreateUser(t, db, "leave_secret_u1")
	user2 := encCreateUser(t, db, "leave_secret_u2")
	user3 := encCreateUser(t, db, "leave_secret_u3")

	regionID := createConnectionTestRegion(t, db, "Secret Leave Region")
	groupAID := createConnectionTestGroup(t, db, "Secret Leave GA", user1.ID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Secret Leave GB", user2.ID, regionID)
	groupCID := createConnectionTestGroup(t, db, "Secret Leave GC", user3.ID, regionID)

	// Form connection with all three groups
	proposal, _ := connRepo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID, groupCID},
	})
	_, _ = connRepo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	result, _ := connRepo.RespondToProposal(ctx, proposal.ID, groupCID, true)
	connectionID := *result.ConnectionID

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE connection_id = ?", connectionID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id IN (SELECT id FROM encrypted_secrets WHERE signal_group_id IS NOT NULL)")
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE signal_group_id IS NOT NULL")
		cleanupConnectionTest(t, db, []string{groupAID, groupBID, groupCID}, []string{user1.ID, user2.ID, user3.ID}, []string{regionID}, []string{connectionID})
	})

	// Create a signal group and encrypted secret for this connection
	sigGroupID := uuid.New().String()
	_, _ = db.ExecContext(ctx, `INSERT INTO signal_groups (id, connection_id, group_name, created_by, created_at, is_active)
		VALUES (?, ?, 'test_sig_group', ?, NOW(), TRUE)`, sigGroupID, connectionID, user1.ID)

	secret := &models.EncryptedSecret{
		SecretType:       models.SecretTypeSignalInvite,
		SignalGroupID:    &sigGroupID,
		EncryptedPayload: "secret_payload",
		EncryptionIV:     "secret_iv_123456",
		UpdatedBy:        user1.ID,
	}
	wrappedKeys := []models.WrappedKeyEntry{
		{UserID: user1.ID, WrappedDEK: "dek_u1"},
		{UserID: user2.ID, WrappedDEK: "dek_u2"},
		{UserID: user3.ID, WrappedDEK: "dek_u3"},
	}
	if err := secretRepo.Create(ctx, secret, wrappedKeys); err != nil {
		t.Fatalf("Failed to create secret: %v", err)
	}

	// Group B leaves the connection
	err := connRepo.LeaveConnection(ctx, connectionID, groupBID)
	if err != nil {
		t.Fatalf("LeaveConnection failed: %v", err)
	}

	// Verify user1 (groupA, survivor) still has key but rekey_needed is true
	dek1, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user1.ID)
	if err != nil {
		t.Fatalf("Failed to get user1 DEK: %v", err)
	}
	if dek1 != "dek_u1" {
		t.Errorf("user1 (survivor) key should be unchanged, got '%s'", dek1)
	}

	var rekeyNeeded bool
	err = db.QueryRowContext(ctx, `
		SELECT rekey_needed FROM encrypted_secret_keys
		WHERE secret_id = ? AND user_id = ?
	`, secret.ID, user1.ID).Scan(&rekeyNeeded)
	if err != nil {
		t.Fatalf("Failed to query rekey flag for user1: %v", err)
	}
	if !rekeyNeeded {
		t.Error("user1 (survivor) should have rekey_needed=true")
	}

	// Verify user3 (groupC, survivor) still has key but rekey_needed is true
	dek3, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user3.ID)
	if err != nil {
		t.Fatalf("Failed to get user3 DEK: %v", err)
	}
	if dek3 != "dek_u3" {
		t.Errorf("user3 (survivor) key should be unchanged, got '%s'", dek3)
	}

	var rekey3Needed bool
	err = db.QueryRowContext(ctx, `
		SELECT rekey_needed FROM encrypted_secret_keys
		WHERE secret_id = ? AND user_id = ?
	`, secret.ID, user3.ID).Scan(&rekey3Needed)
	if err != nil {
		t.Fatalf("Failed to query rekey flag for user3: %v", err)
	}
	if !rekey3Needed {
		t.Error("user3 (survivor) should have rekey_needed=true")
	}

	// Verify user2 (groupB, leaving) key is deleted
	_, err = secretRepo.GetWrappedDEK(ctx, secret.ID, user2.ID)
	if err == nil {
		t.Error("user2 (leaving) key should be deleted")
	}
}

// TestGroupRepository_BlockGroup_RevokesSecrets verifies that blocking a group evicts it from
// connections and revokes its secret keys.
func TestGroupRepository_BlockGroup_RevokesSecrets(t *testing.T) {
	db := testDB(t)
	secretRepo := NewEncryptedSecretRepository(db)
	groupRepo := NewGroupRepository(db)
	connRepo := NewConnectionRepository(db)
	ctx := context.Background()

	user1 := encCreateUser(t, db, "block_secret_u1")
	user2 := encCreateUser(t, db, "block_secret_u2")
	user3 := encCreateUser(t, db, "block_secret_u3")

	regionID := createConnectionTestRegion(t, db, "Secret Block Region")
	groupAID := createConnectionTestGroup(t, db, "Secret Block GA", user1.ID, regionID)
	groupBID := createConnectionTestGroup(t, db, "Secret Block GB", user2.ID, regionID)
	groupCID := createConnectionTestGroup(t, db, "Secret Block GC", user3.ID, regionID)

	// Form connection with all three groups
	proposal, _ := connRepo.ProposeConnection(ctx, groupAID, &models.ProposeConnectionRequest{
		GroupIDs: []string{groupBID, groupCID},
	})
	_, _ = connRepo.RespondToProposal(ctx, proposal.ID, groupBID, true)
	result, _ := connRepo.RespondToProposal(ctx, proposal.ID, groupCID, true)
	connectionID := *result.ConnectionID

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE connection_id = ?", connectionID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id IN (SELECT id FROM encrypted_secrets WHERE signal_group_id IS NOT NULL)")
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE signal_group_id IS NOT NULL")
		cleanupConnectionTest(t, db, []string{groupAID, groupBID, groupCID}, []string{user1.ID, user2.ID, user3.ID}, []string{regionID}, []string{connectionID})
	})

	// Create a signal group and encrypted secret for this connection
	sigGroupID := uuid.New().String()
	_, _ = db.ExecContext(ctx, `INSERT INTO signal_groups (id, connection_id, group_name, created_by, created_at, is_active)
		VALUES (?, ?, 'test_sig_group', ?, NOW(), TRUE)`, sigGroupID, connectionID, user1.ID)

	secret := &models.EncryptedSecret{
		SecretType:       models.SecretTypeSignalInvite,
		SignalGroupID:    &sigGroupID,
		EncryptedPayload: "block_payload",
		EncryptionIV:     "block_iv_123456",
		UpdatedBy:        user1.ID,
	}
	wrappedKeys := []models.WrappedKeyEntry{
		{UserID: user1.ID, WrappedDEK: "dek_u1"},
		{UserID: user2.ID, WrappedDEK: "dek_u2"},
		{UserID: user3.ID, WrappedDEK: "dek_u3"},
	}
	if err := secretRepo.Create(ctx, secret, wrappedKeys); err != nil {
		t.Fatalf("Failed to create secret: %v", err)
	}

	// Group A blocks Group C (evicting C from connection)
	err := groupRepo.BlockGroup(ctx, groupAID, groupCID)
	if err != nil {
		t.Fatalf("BlockGroup failed: %v", err)
	}

	// Verify group C is no longer a member of the connection
	isMember, err := connRepo.IsConnectionMember(ctx, connectionID, groupCID)
	if err != nil {
		t.Fatalf("Failed to check membership: %v", err)
	}
	if isMember {
		t.Error("Group C should have been evicted from connection")
	}

	// Verify user1 and user2 (surviving groups) still have keys but rekey_needed is true
	dek1, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user1.ID)
	if err != nil {
		t.Fatalf("Failed to get user1 DEK: %v", err)
	}
	if dek1 != "dek_u1" {
		t.Errorf("user1 (survivor) key should be unchanged, got '%s'", dek1)
	}

	dek2, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user2.ID)
	if err != nil {
		t.Fatalf("Failed to get user2 DEK: %v", err)
	}
	if dek2 != "dek_u2" {
		t.Errorf("user2 (survivor) key should be unchanged, got '%s'", dek2)
	}

	var rekeyNeeded bool
	err = db.QueryRowContext(ctx, `
		SELECT rekey_needed FROM encrypted_secret_keys
		WHERE secret_id = ? AND user_id = ?
	`, secret.ID, user1.ID).Scan(&rekeyNeeded)
	if err != nil {
		t.Fatalf("Failed to query rekey flag for user1: %v", err)
	}
	if !rekeyNeeded {
		t.Error("user1 (survivor) should have rekey_needed=true")
	}

	// Verify user3 (blocked group) key is deleted
	_, err = secretRepo.GetWrappedDEK(ctx, secret.ID, user3.ID)
	if err == nil {
		t.Error("user3 (blocked) key should be deleted")
	}
}
