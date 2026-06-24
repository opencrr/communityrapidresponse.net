package database

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// =============================================================================
// Helper Functions
// =============================================================================

func createGroupTestUser(t *testing.T, db *DB, suffix string) string {
	t.Helper()
	userID := uuid.New().String()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO users (id, username, email, password_hash, verification_tier, created_at) VALUES (?, ?, ?, ?, 2, NOW())",
		userID, "groupuser_"+suffix, suffix+"@grouptest.com", "$2a$12$testhashedpassword")
	if err != nil {
		t.Fatalf("Failed to create test user %s: %v", suffix, err)
	}
	return userID
}

func createGroupTestRegion(t *testing.T, db *DB, name string) string {
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

func cleanupGroupTest(t *testing.T, db *DB, groupIDs, userIDs, regionIDs []string) {
	t.Helper()
	ctx := context.Background()

	for _, groupID := range groupIDs {
		_, _ = db.ExecContext(ctx, "DELETE FROM topic_board_postings WHERE group_id = ?", groupID)
		_, _ = db.ExecContext(ctx, "DELETE FROM group_resources WHERE group_id = ?", groupID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE owner_group_id = ?", groupID)
		_, _ = db.ExecContext(ctx, "DELETE FROM group_trust_vouches WHERE group_id = ?", groupID)
		_, _ = db.ExecContext(ctx, "DELETE FROM group_invite_links WHERE group_id = ?", groupID)
		_, _ = db.ExecContext(ctx, "DELETE FROM group_invitations WHERE group_id = ?", groupID)
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
// Create Tests
// =============================================================================

func TestGroupRepository_Create(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "create1")
	regionID := createGroupTestRegion(t, db, "Create Test Region")
	var createdGroupIDs []string

	t.Cleanup(func() {
		cleanupGroupTest(t, db, createdGroupIDs, []string{userID}, []string{regionID})
	})

	t.Run("creates group with all fields", func(t *testing.T) {
		req := &models.CreateGroupRequest{
			Name:        "Test Group",
			Description: "A test group description",
			Visibility:  "listed",
			RegionIDs:   []string{regionID},
			TopicTags:   []string{"safety", "mutual-aid"},
		}

		group, err := repo.Create(ctx, req, userID)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		createdGroupIDs = append(createdGroupIDs, group.ID)

		if group.ID == "" {
			t.Error("Expected group ID to be set")
		}
		if group.Name != "Test Group" {
			t.Errorf("Expected name 'Test Group', got %q", group.Name)
		}
		if group.Status != models.GroupStatusProvisional {
			t.Errorf("Expected status 'provisional', got %q", group.Status)
		}
		// Visibility is forced to unlisted on creation
		if group.Visibility != models.GroupVisibilityUnlisted {
			t.Errorf("Expected visibility 'unlisted', got %q", group.Visibility)
		}
		if group.Description == nil || *group.Description != "A test group description" {
			t.Errorf("Expected description to be set, got %v", group.Description)
		}
		if group.CreatedBy == nil || *group.CreatedBy != userID {
			t.Error("Expected created_by to match creator")
		}

		// Verify creator is a founding admin member
		isMember, err := repo.IsUserMember(ctx, group.ID, userID)
		if err != nil {
			t.Fatalf("IsUserMember failed: %v", err)
		}
		if !isMember {
			t.Error("Creator should be a member")
		}

		isAdmin, err := repo.IsUserAdmin(ctx, group.ID, userID)
		if err != nil {
			t.Fatalf("IsUserAdmin failed: %v", err)
		}
		if !isAdmin {
			t.Error("Creator should be an admin")
		}

		// Verify regions were set
		regions, err := repo.GetRegions(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetRegions failed: %v", err)
		}
		if len(regions) != 1 {
			t.Fatalf("Expected 1 region, got %d", len(regions))
		}
		if regions[0].ID != regionID {
			t.Errorf("Expected region ID %s, got %s", regionID, regions[0].ID)
		}

		// Verify tags were set
		tags, err := repo.GetTopicTags(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetTopicTags failed: %v", err)
		}
		if len(tags) != 2 {
			t.Fatalf("Expected 2 tags, got %d", len(tags))
		}
	})

	t.Run("creates group with empty description", func(t *testing.T) {
		req := &models.CreateGroupRequest{
			Name:      "No Desc Group",
			RegionIDs: []string{regionID},
		}

		group, err := repo.Create(ctx, req, userID)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		createdGroupIDs = append(createdGroupIDs, group.ID)

		if group.Description != nil {
			t.Errorf("Expected nil description, got %v", group.Description)
		}
	})
}

// =============================================================================
// GetByID Tests
// =============================================================================

func TestGroupRepository_GetByID(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "getbyid1")
	regionID := createGroupTestRegion(t, db, "GetByID Region")

	req := &models.CreateGroupRequest{
		Name:      "GetByID Group",
		RegionIDs: []string{regionID},
	}
	group, err := repo.Create(ctx, req, userID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	t.Run("returns group", func(t *testing.T) {
		found, err := repo.GetByID(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if found.ID != group.ID {
			t.Errorf("Expected ID %s, got %s", group.ID, found.ID)
		}
		if found.Name != "GetByID Group" {
			t.Errorf("Expected name 'GetByID Group', got %q", found.Name)
		}
	})

	t.Run("returns ErrGroupNotFound for missing ID", func(t *testing.T) {
		_, err := repo.GetByID(ctx, uuid.New().String())
		if err != ErrGroupNotFound {
			t.Errorf("Expected ErrGroupNotFound, got %v", err)
		}
	})
}

// =============================================================================
// GetByIDWithDetails Tests
// =============================================================================

func TestGroupRepository_GetByIDWithDetails(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "details1")
	user2ID := createGroupTestUser(t, db, "details2")
	regionID := createGroupTestRegion(t, db, "Details Region")

	req := &models.CreateGroupRequest{
		Name:      "Details Group",
		RegionIDs: []string{regionID},
		TopicTags: []string{"tag1", "tag2"},
	}
	group, err := repo.Create(ctx, req, userID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	// Add a second non-admin member
	if err := repo.AddMember(ctx, group.ID, user2ID, false, false); err != nil {
		t.Fatalf("Setup: AddMember failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID, user2ID}, []string{regionID})
	})

	t.Run("returns full details for member", func(t *testing.T) {
		details, err := repo.GetByIDWithDetails(ctx, group.ID, userID)
		if err != nil {
			t.Fatalf("GetByIDWithDetails failed: %v", err)
		}

		if details.MemberCount != 2 {
			t.Errorf("Expected 2 members, got %d", details.MemberCount)
		}
		if details.AdminCount != 1 {
			t.Errorf("Expected 1 admin, got %d", details.AdminCount)
		}
		if !details.IsUserMember {
			t.Error("Expected IsUserMember to be true")
		}
		if !details.IsUserAdmin {
			t.Error("Expected IsUserAdmin to be true for creator")
		}
		if len(details.Regions) != 1 {
			t.Errorf("Expected 1 region, got %d", len(details.Regions))
		}
		if len(details.TopicTags) != 2 {
			t.Errorf("Expected 2 tags, got %d", len(details.TopicTags))
		}
	})

	t.Run("non-member shows not member", func(t *testing.T) {
		outsiderID := createGroupTestUser(t, db, "details_outsider")
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", outsiderID)
		}()

		details, err := repo.GetByIDWithDetails(ctx, group.ID, outsiderID)
		if err != nil {
			t.Fatalf("GetByIDWithDetails failed: %v", err)
		}
		if details.IsUserMember {
			t.Error("Expected IsUserMember to be false for non-member")
		}
		if details.IsUserAdmin {
			t.Error("Expected IsUserAdmin to be false for non-member")
		}
	})

	t.Run("returns ErrGroupNotFound for missing ID", func(t *testing.T) {
		_, err := repo.GetByIDWithDetails(ctx, uuid.New().String(), userID)
		if err != ErrGroupNotFound {
			t.Errorf("Expected ErrGroupNotFound, got %v", err)
		}
	})
}

// =============================================================================
// ListByUser Tests
// =============================================================================

func TestGroupRepository_ListByUser(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "listuser1")
	user2ID := createGroupTestUser(t, db, "listuser2")
	regionID := createGroupTestRegion(t, db, "ListUser Region")

	group1, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "User Group 1",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create group1 failed: %v", err)
	}

	group2, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "User Group 2",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create group2 failed: %v", err)
	}

	// group3 belongs to user2 only
	group3, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Other User Group",
		RegionIDs: []string{regionID},
	}, user2ID)
	if err != nil {
		t.Fatalf("Setup: Create group3 failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group1.ID, group2.ID, group3.ID}, []string{userID, user2ID}, []string{regionID})
	})

	t.Run("returns only user's groups", func(t *testing.T) {
		groups, err := repo.ListByUser(ctx, userID)
		if err != nil {
			t.Fatalf("ListByUser failed: %v", err)
		}
		if len(groups) != 2 {
			t.Fatalf("Expected 2 groups, got %d", len(groups))
		}
		for _, g := range groups {
			if !g.IsUserMember {
				t.Error("Expected IsUserMember true for all returned groups")
			}
		}
	})

	t.Run("returns empty for user with no groups", func(t *testing.T) {
		noGroupUser := createGroupTestUser(t, db, "listuser_none")
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", noGroupUser)
		}()

		groups, err := repo.ListByUser(ctx, noGroupUser)
		if err != nil {
			t.Fatalf("ListByUser failed: %v", err)
		}
		if len(groups) != 0 {
			t.Errorf("Expected 0 groups, got %d", len(groups))
		}
	})
}

// =============================================================================
// ListByRegion Tests
// =============================================================================

func TestGroupRepository_ListByRegion(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "listregion1")
	regionID := createGroupTestRegion(t, db, "ListRegion Region")

	// Create a group — starts as provisional + unlisted
	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Region Group",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	t.Run("does not return provisional/unlisted groups", func(t *testing.T) {
		groups, err := repo.ListByRegion(ctx, regionID)
		if err != nil {
			t.Fatalf("ListByRegion failed: %v", err)
		}
		if len(groups) != 0 {
			t.Errorf("Expected 0 groups for provisional/unlisted, got %d", len(groups))
		}
	})

	t.Run("returns listed active groups", func(t *testing.T) {
		// Promote group to active + listed
		_, err := db.ExecContext(ctx, "UPDATE `groups` SET status = 'active', visibility = 'listed' WHERE id = ?", group.ID)
		if err != nil {
			t.Fatalf("Failed to update group: %v", err)
		}

		groups, err := repo.ListByRegion(ctx, regionID)
		if err != nil {
			t.Fatalf("ListByRegion failed: %v", err)
		}
		if len(groups) != 1 {
			t.Fatalf("Expected 1 group, got %d", len(groups))
		}
		if groups[0].ID != group.ID {
			t.Errorf("Expected group ID %s, got %s", group.ID, groups[0].ID)
		}
	})
}

// =============================================================================
// Update Tests
// =============================================================================

func TestGroupRepository_Update(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "update1")
	regionID := createGroupTestRegion(t, db, "Update Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Before Update",
		RegionIDs: []string{regionID},
		TopicTags: []string{"old-tag"},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	t.Run("updates name only", func(t *testing.T) {
		newName := "After Update"
		err := repo.Update(ctx, group.ID, &models.UpdateGroupRequest{
			Name: &newName,
		})
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		found, err := repo.GetByID(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if found.Name != "After Update" {
			t.Errorf("Expected name 'After Update', got %q", found.Name)
		}
	})

	t.Run("updates topic tags", func(t *testing.T) {
		newTags := []string{"new-tag-1", "new-tag-2"}
		err := repo.Update(ctx, group.ID, &models.UpdateGroupRequest{
			TopicTags: &newTags,
		})
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		tags, err := repo.GetTopicTags(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetTopicTags failed: %v", err)
		}
		if len(tags) != 2 {
			t.Fatalf("Expected 2 tags, got %d", len(tags))
		}
	})

	t.Run("returns ErrGroupNotFound for missing ID", func(t *testing.T) {
		newName := "nope"
		err := repo.Update(ctx, uuid.New().String(), &models.UpdateGroupRequest{
			Name: &newName,
		})
		if err != ErrGroupNotFound {
			t.Errorf("Expected ErrGroupNotFound, got %v", err)
		}
	})
}

// =============================================================================
// Delete Tests
// =============================================================================

func TestGroupRepository_Delete(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "delete1")
	regionID := createGroupTestRegion(t, db, "Delete Region")

	t.Cleanup(func() {
		cleanupGroupTest(t, db, nil, []string{userID}, []string{regionID})
	})

	t.Run("deletes group and cascades", func(t *testing.T) {
		group, err := repo.Create(ctx, &models.CreateGroupRequest{
			Name:      "To Delete",
			RegionIDs: []string{regionID},
			TopicTags: []string{"gone"},
		}, userID)
		if err != nil {
			t.Fatalf("Setup: Create failed: %v", err)
		}

		err = repo.Delete(ctx, group.ID)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		_, err = repo.GetByID(ctx, group.ID)
		if err != ErrGroupNotFound {
			t.Errorf("Expected ErrGroupNotFound after delete, got %v", err)
		}

		// Verify cascade cleaned up members
		members, err := repo.GetMembers(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetMembers failed: %v", err)
		}
		if len(members) != 0 {
			t.Errorf("Expected 0 members after delete, got %d", len(members))
		}
	})

	t.Run("returns ErrGroupNotFound for missing ID", func(t *testing.T) {
		err := repo.Delete(ctx, uuid.New().String())
		if err != ErrGroupNotFound {
			t.Errorf("Expected ErrGroupNotFound, got %v", err)
		}
	})
}

// =============================================================================
// Membership Tests
// =============================================================================

func TestGroupRepository_Membership(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "member1")
	user2ID := createGroupTestUser(t, db, "member2")
	regionID := createGroupTestRegion(t, db, "Member Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Member Group",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID, user2ID}, []string{regionID})
	})

	t.Run("AddMember and check", func(t *testing.T) {
		err := repo.AddMember(ctx, group.ID, user2ID, false, false)
		if err != nil {
			t.Fatalf("AddMember failed: %v", err)
		}

		isMember, err := repo.IsUserMember(ctx, group.ID, user2ID)
		if err != nil {
			t.Fatalf("IsUserMember failed: %v", err)
		}
		if !isMember {
			t.Error("Expected user to be member")
		}

		isAdmin, err := repo.IsUserAdmin(ctx, group.ID, user2ID)
		if err != nil {
			t.Fatalf("IsUserAdmin failed: %v", err)
		}
		if isAdmin {
			t.Error("Expected user to not be admin")
		}
	})

	t.Run("GetMembers returns all members with user details", func(t *testing.T) {
		members, err := repo.GetMembers(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetMembers failed: %v", err)
		}
		if len(members) != 2 {
			t.Fatalf("Expected 2 members, got %d", len(members))
		}

		// Verify user details are populated
		for _, m := range members {
			if m.Username == "" {
				t.Error("Expected username to be populated")
			}
		}
	})

	t.Run("GetMemberCount", func(t *testing.T) {
		count, err := repo.GetMemberCount(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetMemberCount failed: %v", err)
		}
		if count != 2 {
			t.Errorf("Expected 2, got %d", count)
		}
	})

	t.Run("RemoveMember", func(t *testing.T) {
		err := repo.RemoveMember(ctx, group.ID, user2ID)
		if err != nil {
			t.Fatalf("RemoveMember failed: %v", err)
		}

		isMember, err := repo.IsUserMember(ctx, group.ID, user2ID)
		if err != nil {
			t.Fatalf("IsUserMember failed: %v", err)
		}
		if isMember {
			t.Error("Expected user to no longer be a member")
		}
	})

	t.Run("IsUserMember returns false for non-member", func(t *testing.T) {
		nonMember := uuid.New().String()
		isMember, err := repo.IsUserMember(ctx, group.ID, nonMember)
		if err != nil {
			t.Fatalf("IsUserMember failed: %v", err)
		}
		if isMember {
			t.Error("Expected false for non-existent user")
		}
	})
}

// =============================================================================
// TopicTags Tests
// =============================================================================

func TestGroupRepository_TopicTags(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "tags1")
	regionID := createGroupTestRegion(t, db, "Tags Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Tags Group",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	t.Run("SetTopicTags replaces all", func(t *testing.T) {
		err := repo.SetTopicTags(ctx, group.ID, []string{"a", "b", "c"})
		if err != nil {
			t.Fatalf("SetTopicTags failed: %v", err)
		}

		tags, err := repo.GetTopicTags(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetTopicTags failed: %v", err)
		}
		if len(tags) != 3 {
			t.Fatalf("Expected 3 tags, got %d", len(tags))
		}

		// Replace with different set
		err = repo.SetTopicTags(ctx, group.ID, []string{"x"})
		if err != nil {
			t.Fatalf("SetTopicTags second call failed: %v", err)
		}

		tags, err = repo.GetTopicTags(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetTopicTags failed: %v", err)
		}
		if len(tags) != 1 {
			t.Fatalf("Expected 1 tag, got %d", len(tags))
		}
		if tags[0] != "x" {
			t.Errorf("Expected tag 'x', got %q", tags[0])
		}
	})

	t.Run("SetTopicTags with empty clears all", func(t *testing.T) {
		err := repo.SetTopicTags(ctx, group.ID, []string{})
		if err != nil {
			t.Fatalf("SetTopicTags failed: %v", err)
		}

		tags, err := repo.GetTopicTags(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetTopicTags failed: %v", err)
		}
		if len(tags) != 0 {
			t.Errorf("Expected 0 tags, got %d", len(tags))
		}
	})
}

// =============================================================================
// Regions Tests
// =============================================================================

func TestGroupRepository_Regions(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "regions1")
	region1ID := createGroupTestRegion(t, db, "Group Region 1")
	region2ID := createGroupTestRegion(t, db, "Group Region 2")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Multi Region Group",
		RegionIDs: []string{region1ID},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{region1ID, region2ID})
	})

	t.Run("AddRegion adds a new region", func(t *testing.T) {
		err := repo.AddRegion(ctx, group.ID, region2ID)
		if err != nil {
			t.Fatalf("AddRegion failed: %v", err)
		}

		regions, err := repo.GetRegions(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetRegions failed: %v", err)
		}
		if len(regions) != 2 {
			t.Fatalf("Expected 2 regions, got %d", len(regions))
		}
	})

	t.Run("GetRegions includes region details", func(t *testing.T) {
		regions, err := repo.GetRegions(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetRegions failed: %v", err)
		}
		for _, r := range regions {
			if r.ID == "" || r.Name == "" {
				t.Error("Expected region ID and Name to be populated")
			}
			if r.RegionType == "" {
				t.Error("Expected RegionType to be populated")
			}
		}
	})
}

// =============================================================================
// PlatformConfig and FoundingThreshold Tests
// =============================================================================

func TestGroupRepository_FoundingThreshold(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "threshold1")
	regionID := createGroupTestRegion(t, db, "Threshold Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Threshold Group",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	t.Run("returns platform default when group has no custom threshold", func(t *testing.T) {
		threshold, err := repo.GetFoundingThreshold(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetFoundingThreshold failed: %v", err)
		}
		// Platform default is 3 (from migration 034)
		if threshold != 3 {
			t.Errorf("Expected default threshold 3, got %d", threshold)
		}
	})

	t.Run("returns custom threshold when set", func(t *testing.T) {
		_, err := db.ExecContext(ctx, "UPDATE `groups` SET founding_threshold = 5 WHERE id = ?", group.ID)
		if err != nil {
			t.Fatalf("Failed to set custom threshold: %v", err)
		}

		threshold, err := repo.GetFoundingThreshold(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetFoundingThreshold failed: %v", err)
		}
		if threshold != 5 {
			t.Errorf("Expected custom threshold 5, got %d", threshold)
		}
	})

	t.Run("GetPlatformConfig returns value", func(t *testing.T) {
		value, err := repo.GetPlatformConfig(ctx, "group_founding_threshold")
		if err != nil {
			t.Fatalf("GetPlatformConfig failed: %v", err)
		}
		if value != "3" {
			t.Errorf("Expected '3', got %q", value)
		}
	})

	t.Run("GetPlatformConfig returns error for missing key", func(t *testing.T) {
		_, err := repo.GetPlatformConfig(ctx, "nonexistent_key")
		if err == nil {
			t.Error("Expected error for missing key")
		}
	})

	t.Run("returns ErrGroupNotFound for missing group", func(t *testing.T) {
		_, err := repo.GetFoundingThreshold(ctx, uuid.New().String())
		if err != ErrGroupNotFound {
			t.Errorf("Expected ErrGroupNotFound, got %v", err)
		}
	})
}

// =============================================================================
// Invite Link Tests
// =============================================================================

func TestGroupRepository_CreateInviteLink(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "invlink_create1")
	regionID := createGroupTestRegion(t, db, "InvLink Create Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Invite Link Group",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	t.Run("creates link with defaults", func(t *testing.T) {
		link, err := repo.CreateInviteLink(ctx, group.ID, userID, &models.CreateInviteLinkRequest{})
		if err != nil {
			t.Fatalf("CreateInviteLink failed: %v", err)
		}

		if link.ID == "" {
			t.Error("Expected link ID to be set")
		}
		if len(link.Token) != 64 {
			t.Errorf("Expected 64-char hex token, got %d chars", len(link.Token))
		}
		if link.GroupID != group.ID {
			t.Errorf("Expected group_id %s, got %s", group.ID, link.GroupID)
		}
		if link.CreatedBy == nil || *link.CreatedBy != userID {
			t.Error("Expected created_by to match user")
		}
		if link.ExpiresAt != nil {
			t.Error("Expected nil expires_at for default request")
		}
		if link.MaxUses != nil {
			t.Error("Expected nil max_uses for default request")
		}
		if link.UseCount != 0 {
			t.Errorf("Expected use_count 0, got %d", link.UseCount)
		}
	})

	t.Run("creates link with expiry and max uses", func(t *testing.T) {
		expiresInHours := 48
		maxUses := 10
		link, err := repo.CreateInviteLink(ctx, group.ID, userID, &models.CreateInviteLinkRequest{
			ExpiresInHours: &expiresInHours,
			MaxUses:        &maxUses,
		})
		if err != nil {
			t.Fatalf("CreateInviteLink failed: %v", err)
		}

		if link.ExpiresAt == nil {
			t.Fatal("Expected expires_at to be set")
		}
		if link.MaxUses == nil || *link.MaxUses != 10 {
			t.Errorf("Expected max_uses 10, got %v", link.MaxUses)
		}
	})
}

func TestGroupRepository_GetInviteLinkByToken(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "invlink_get1")
	regionID := createGroupTestRegion(t, db, "InvLink Get Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Get Link Group",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	link, err := repo.CreateInviteLink(ctx, group.ID, userID, &models.CreateInviteLinkRequest{})
	if err != nil {
		t.Fatalf("Setup: CreateInviteLink failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	t.Run("returns link by token", func(t *testing.T) {
		found, err := repo.GetInviteLinkByToken(ctx, link.Token)
		if err != nil {
			t.Fatalf("GetInviteLinkByToken failed: %v", err)
		}
		if found.ID != link.ID {
			t.Errorf("Expected ID %s, got %s", link.ID, found.ID)
		}
		if found.GroupID != group.ID {
			t.Errorf("Expected group_id %s, got %s", group.ID, found.GroupID)
		}
	})

	t.Run("returns ErrInviteLinkNotFound for bad token", func(t *testing.T) {
		_, err := repo.GetInviteLinkByToken(ctx, "nonexistent_token_value")
		if err != ErrInviteLinkNotFound {
			t.Errorf("Expected ErrInviteLinkNotFound, got %v", err)
		}
	})
}

func TestGroupRepository_JoinViaInviteLink(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	creatorID := createGroupTestUser(t, db, "invlink_join_creator")
	regionID := createGroupTestRegion(t, db, "InvLink Join Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Join Link Group",
		RegionIDs: []string{regionID},
	}, creatorID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	// JoinViaInviteLink adds membership, so each successful join needs a distinct
	// user (an already-member is rejected). Track them all for cleanup.
	joiners := []string{}
	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, append([]string{creatorID}, joiners...), []string{regionID})
	})
	newJoiner := func(name string) string {
		id := createGroupTestUser(t, db, name)
		joiners = append(joiners, id)
		return id
	}

	t.Run("join increments use count and adds membership", func(t *testing.T) {
		link, err := repo.CreateInviteLink(ctx, group.ID, creatorID, &models.CreateInviteLinkRequest{})
		if err != nil {
			t.Fatalf("Setup: CreateInviteLink failed: %v", err)
		}

		j1 := newJoiner("invlink_join_1")
		consumed, err := repo.JoinViaInviteLink(ctx, link.Token, j1)
		if err != nil {
			t.Fatalf("JoinViaInviteLink failed: %v", err)
		}
		if consumed.UseCount != 1 {
			t.Errorf("Expected use_count 1, got %d", consumed.UseCount)
		}
		if consumed.GroupID != group.ID {
			t.Errorf("Expected group_id %s, got %s", group.ID, consumed.GroupID)
		}
		if m, _ := repo.IsUserMember(ctx, group.ID, j1); !m {
			t.Error("join must add the user as a member")
		}

		j2 := newJoiner("invlink_join_2")
		consumed2, err := repo.JoinViaInviteLink(ctx, link.Token, j2)
		if err != nil {
			t.Fatalf("Second JoinViaInviteLink failed: %v", err)
		}
		if consumed2.UseCount != 2 {
			t.Errorf("Expected use_count 2, got %d", consumed2.UseCount)
		}
	})

	t.Run("returns ErrInviteLinkExpired for expired link", func(t *testing.T) {
		link, err := repo.CreateInviteLink(ctx, group.ID, creatorID, &models.CreateInviteLinkRequest{})
		if err != nil {
			t.Fatalf("Setup: CreateInviteLink failed: %v", err)
		}
		if _, err := db.ExecContext(ctx, "UPDATE group_invite_links SET expires_at = '2020-01-01 00:00:00' WHERE id = ?", link.ID); err != nil {
			t.Fatalf("Failed to backdate link: %v", err)
		}
		if _, err := repo.JoinViaInviteLink(ctx, link.Token, newJoiner("invlink_join_exp")); err != ErrInviteLinkExpired {
			t.Errorf("Expected ErrInviteLinkExpired, got %v", err)
		}
	})

	t.Run("returns ErrInviteLinkExhausted when max uses reached", func(t *testing.T) {
		maxUses := 1
		link, err := repo.CreateInviteLink(ctx, group.ID, creatorID, &models.CreateInviteLinkRequest{MaxUses: &maxUses})
		if err != nil {
			t.Fatalf("Setup: CreateInviteLink failed: %v", err)
		}
		if _, err := repo.JoinViaInviteLink(ctx, link.Token, newJoiner("invlink_join_x1")); err != nil {
			t.Fatalf("First join failed: %v", err)
		}
		if _, err := repo.JoinViaInviteLink(ctx, link.Token, newJoiner("invlink_join_x2")); err != ErrInviteLinkExhausted {
			t.Errorf("Expected ErrInviteLinkExhausted, got %v", err)
		}
	})

	t.Run("returns ErrInviteLinkNotFound for bad token", func(t *testing.T) {
		if _, err := repo.JoinViaInviteLink(ctx, "nonexistent_token_value", creatorID); err != ErrInviteLinkNotFound {
			t.Errorf("Expected ErrInviteLinkNotFound, got %v", err)
		}
	})
}

// TestGroupRepository_JoinViaInviteLink_InsertFailureRollsBack verifies the join
// is fully transactional: when the membership insert fails (here a FK violation
// from a non-existent user, standing in for a duplicate-member race), the whole
// transaction rolls back and use_count is NOT incremented.
func TestGroupRepository_JoinViaInviteLink_InsertFailureRollsBack(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "join_tx_rollback")
	regionID := createGroupTestRegion(t, db, "Join Tx Rollback Region")
	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name: "Join Tx Group", RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}
	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	link, err := repo.CreateInviteLink(ctx, group.ID, userID, &models.CreateInviteLinkRequest{})
	if err != nil {
		t.Fatalf("Setup: CreateInviteLink failed: %v", err)
	}

	// A user_id that does not exist passes the block/already-member EXISTS checks
	// but fails the group_members FK on insert — forcing the insert to error after
	// the link would otherwise be consumed.
	_, err = repo.JoinViaInviteLink(ctx, link.Token, "nonexistent-user-id")
	if err == nil {
		t.Fatal("expected JoinViaInviteLink to fail on the membership insert")
	}

	after, err := repo.GetInviteLinkByToken(ctx, link.Token)
	if err != nil {
		t.Fatalf("GetInviteLinkByToken failed: %v", err)
	}
	if after.UseCount != 0 {
		t.Errorf("failed membership insert must roll back the use_count increment; got %d", after.UseCount)
	}
}

func TestGroupRepository_ListInviteLinks(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "invlink_list1")
	regionID := createGroupTestRegion(t, db, "InvLink List Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "List Links Group",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	// Create a second group to verify isolation
	group2, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Other Links Group",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create group2 failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID, group2.ID}, []string{userID}, []string{regionID})
	})

	// Create links for both groups
	_, err = repo.CreateInviteLink(ctx, group.ID, userID, &models.CreateInviteLinkRequest{})
	if err != nil {
		t.Fatalf("Setup: CreateInviteLink 1 failed: %v", err)
	}
	_, err = repo.CreateInviteLink(ctx, group.ID, userID, &models.CreateInviteLinkRequest{})
	if err != nil {
		t.Fatalf("Setup: CreateInviteLink 2 failed: %v", err)
	}
	_, err = repo.CreateInviteLink(ctx, group2.ID, userID, &models.CreateInviteLinkRequest{})
	if err != nil {
		t.Fatalf("Setup: CreateInviteLink for group2 failed: %v", err)
	}

	t.Run("returns links for specific group only", func(t *testing.T) {
		links, err := repo.ListInviteLinks(ctx, group.ID)
		if err != nil {
			t.Fatalf("ListInviteLinks failed: %v", err)
		}
		if len(links) != 2 {
			t.Fatalf("Expected 2 links, got %d", len(links))
		}
		for _, link := range links {
			if link.GroupID != group.ID {
				t.Errorf("Expected group_id %s, got %s", group.ID, link.GroupID)
			}
		}
	})
}

// =============================================================================
// Invitation Tests
// =============================================================================

func TestGroupRepository_CreateInvitation(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	adminID := createGroupTestUser(t, db, "inv_admin1")
	inviteeID := createGroupTestUser(t, db, "inv_invitee1")
	regionID := createGroupTestRegion(t, db, "Invitation Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Invitation Group",
		RegionIDs: []string{regionID},
	}, adminID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{adminID, inviteeID}, []string{regionID})
	})

	t.Run("creates invitation", func(t *testing.T) {
		invitation, err := repo.CreateInvitation(ctx, group.ID, inviteeID, adminID)
		if err != nil {
			t.Fatalf("CreateInvitation failed: %v", err)
		}

		if invitation.ID == "" {
			t.Error("Expected invitation ID to be set")
		}
		if invitation.GroupID != group.ID {
			t.Errorf("Expected group_id %s, got %s", group.ID, invitation.GroupID)
		}
		if invitation.UserID != inviteeID {
			t.Errorf("Expected user_id %s, got %s", inviteeID, invitation.UserID)
		}
		if invitation.Status != models.InvitationStatusPending {
			t.Errorf("Expected status 'pending', got %q", invitation.Status)
		}
		if invitation.ExpiresAt == nil {
			t.Error("Expected expires_at to be set")
		}
		if invitation.InvitedBy == nil || *invitation.InvitedBy != adminID {
			t.Error("Expected invited_by to match admin")
		}
	})

	t.Run("rejects duplicate pending invitation", func(t *testing.T) {
		_, err := repo.CreateInvitation(ctx, group.ID, inviteeID, adminID)
		if err != ErrInvitationAlreadyPending {
			t.Errorf("Expected ErrInvitationAlreadyPending, got %v", err)
		}
	})
}

func TestGroupRepository_ListPendingInvitationsForUser(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	adminID := createGroupTestUser(t, db, "inv_listuser_admin")
	inviteeID := createGroupTestUser(t, db, "inv_listuser_invitee")
	otherUserID := createGroupTestUser(t, db, "inv_listuser_other")
	regionID := createGroupTestRegion(t, db, "ListInv Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "ListInv Group",
		RegionIDs: []string{regionID},
	}, adminID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	// Create invitation for invitee
	_, err = repo.CreateInvitation(ctx, group.ID, inviteeID, adminID)
	if err != nil {
		t.Fatalf("Setup: CreateInvitation failed: %v", err)
	}

	// Create invitation for other user (should not appear)
	_, err = repo.CreateInvitation(ctx, group.ID, otherUserID, adminID)
	if err != nil {
		t.Fatalf("Setup: CreateInvitation for other failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{adminID, inviteeID, otherUserID}, []string{regionID})
	})

	t.Run("returns pending invitations with details", func(t *testing.T) {
		invitations, err := repo.ListPendingInvitationsForUser(ctx, inviteeID)
		if err != nil {
			t.Fatalf("ListPendingInvitationsForUser failed: %v", err)
		}
		if len(invitations) != 1 {
			t.Fatalf("Expected 1 invitation, got %d", len(invitations))
		}

		inv := invitations[0]
		if inv.GroupName != "ListInv Group" {
			t.Errorf("Expected group_name 'ListInv Group', got %q", inv.GroupName)
		}
		if inv.InviterName == nil || *inv.InviterName != "groupuser_inv_listuser_admin" {
			t.Errorf("Expected inviter_name to be set, got %v", inv.InviterName)
		}
		if inv.UserID != inviteeID {
			t.Errorf("Expected user_id %s, got %s", inviteeID, inv.UserID)
		}
	})

	t.Run("excludes expired invitations", func(t *testing.T) {
		// Backdate the invitation to expire
		_, err := db.ExecContext(ctx,
			"UPDATE group_invitations SET expires_at = '2020-01-01 00:00:00' WHERE user_id = ? AND group_id = ?",
			inviteeID, group.ID)
		if err != nil {
			t.Fatalf("Failed to backdate invitation: %v", err)
		}

		invitations, err := repo.ListPendingInvitationsForUser(ctx, inviteeID)
		if err != nil {
			t.Fatalf("ListPendingInvitationsForUser failed: %v", err)
		}
		if len(invitations) != 0 {
			t.Errorf("Expected 0 invitations for expired, got %d", len(invitations))
		}
	})
}

func TestGroupRepository_InvitationResponse(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	adminID := createGroupTestUser(t, db, "inv_respond_admin")
	inviteeID := createGroupTestUser(t, db, "inv_respond_invitee")
	invitee2ID := createGroupTestUser(t, db, "inv_respond_invitee2")
	regionID := createGroupTestRegion(t, db, "Respond Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Respond Group",
		RegionIDs: []string{regionID},
	}, adminID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{adminID, inviteeID, invitee2ID}, []string{regionID})
	})

	// AcceptInvitation is the only path that marks an invitation accepted, and it
	// adds membership in the same transaction.
	t.Run("accept adds membership transactionally", func(t *testing.T) {
		invitation, err := repo.CreateInvitation(ctx, group.ID, inviteeID, adminID)
		if err != nil {
			t.Fatalf("Setup: CreateInvitation failed: %v", err)
		}

		result, err := repo.AcceptInvitation(ctx, invitation.ID, inviteeID)
		if err != nil {
			t.Fatalf("AcceptInvitation failed: %v", err)
		}
		if result.Status != models.InvitationStatusAccepted {
			t.Errorf("Expected status 'accepted', got %q", result.Status)
		}
		if isMember, _ := repo.IsUserMember(ctx, group.ID, inviteeID); !isMember {
			t.Error("accept must add the user as a member")
		}
	})

	// DeclineInvitation marks declined and never adds membership.
	t.Run("decline marks declined without membership", func(t *testing.T) {
		invitation, err := repo.CreateInvitation(ctx, group.ID, invitee2ID, adminID)
		if err != nil {
			t.Fatalf("Setup: CreateInvitation failed: %v", err)
		}

		result, err := repo.DeclineInvitation(ctx, invitation.ID, invitee2ID)
		if err != nil {
			t.Fatalf("DeclineInvitation failed: %v", err)
		}
		if result.Status != models.InvitationStatusDeclined {
			t.Errorf("Expected status 'declined', got %q", result.Status)
		}
		if isMember, _ := repo.IsUserMember(ctx, group.ID, invitee2ID); isMember {
			t.Error("decline must not add the user as a member")
		}
	})

	t.Run("decline returns ErrInvitationNotFound for wrong user", func(t *testing.T) {
		if _, err := repo.DeclineInvitation(ctx, uuid.New().String(), uuid.New().String()); err != ErrInvitationNotFound {
			t.Errorf("Expected ErrInvitationNotFound, got %v", err)
		}
	})

	t.Run("accept returns ErrInvitationExpired for expired invitation", func(t *testing.T) {
		invitee3ID := createGroupTestUser(t, db, "inv_respond_invitee3")
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", invitee3ID)
		}()

		invitation, err := repo.CreateInvitation(ctx, group.ID, invitee3ID, adminID)
		if err != nil {
			t.Fatalf("Setup: CreateInvitation failed: %v", err)
		}

		// Backdate to expire
		if _, err := db.ExecContext(ctx, "UPDATE group_invitations SET expires_at = '2020-01-01 00:00:00' WHERE id = ?", invitation.ID); err != nil {
			t.Fatalf("Failed to backdate: %v", err)
		}

		if _, err := repo.AcceptInvitation(ctx, invitation.ID, invitee3ID); err != ErrInvitationExpired {
			t.Errorf("Expected ErrInvitationExpired, got %v", err)
		}
	})
}

// =============================================================================
// Graduation Tests
// =============================================================================

func TestGroupRepository_CheckAndGraduate(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	creatorID := createGroupTestUser(t, db, "grad_creator")
	member2ID := createGroupTestUser(t, db, "grad_member2")
	member3ID := createGroupTestUser(t, db, "grad_member3")
	regionID := createGroupTestRegion(t, db, "Graduation Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Graduation Group",
		RegionIDs: []string{regionID},
	}, creatorID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{creatorID, member2ID, member3ID}, []string{regionID})
	})

	t.Run("does not graduate below threshold", func(t *testing.T) {
		// Group has 1 member (creator), threshold is 3
		graduated, err := repo.CheckAndGraduate(ctx, group.ID)
		if err != nil {
			t.Fatalf("CheckAndGraduate failed: %v", err)
		}
		if graduated {
			t.Error("Expected not graduated with 1 member")
		}

		// Verify still provisional
		g, err := repo.GetByID(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if g.Status != models.GroupStatusProvisional {
			t.Errorf("Expected status provisional, got %q", g.Status)
		}
	})

	t.Run("graduates at threshold and promotes founding members", func(t *testing.T) {
		// Add two more founding members (non-admin initially)
		if err := repo.AddMember(ctx, group.ID, member2ID, false, true); err != nil {
			t.Fatalf("AddMember 2 failed: %v", err)
		}
		if err := repo.AddMember(ctx, group.ID, member3ID, false, true); err != nil {
			t.Fatalf("AddMember 3 failed: %v", err)
		}

		// Now at 3 members, threshold is 3
		graduated, err := repo.CheckAndGraduate(ctx, group.ID)
		if err != nil {
			t.Fatalf("CheckAndGraduate failed: %v", err)
		}
		if !graduated {
			t.Error("Expected graduated with 3 members")
		}

		// Verify group is now active
		g, err := repo.GetByID(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if g.Status != models.GroupStatusActive {
			t.Errorf("Expected status active, got %q", g.Status)
		}
		if g.GraduatedAt == nil {
			t.Error("Expected graduated_at to be set")
		}

		// Verify founding members are now admins
		members, err := repo.GetMembers(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetMembers failed: %v", err)
		}
		for _, m := range members {
			if m.IsFoundingMember && !m.IsAdmin {
				t.Errorf("Founding member %s should be admin after graduation", m.UserID)
			}
		}
	})

	t.Run("no-op when already active", func(t *testing.T) {
		graduated, err := repo.CheckAndGraduate(ctx, group.ID)
		if err != nil {
			t.Fatalf("CheckAndGraduate failed: %v", err)
		}
		if graduated {
			t.Error("Expected not graduated for already-active group")
		}
	})
}

// =============================================================================
// GetMember Tests
// =============================================================================

func TestGroupRepository_GetMember(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "getmember1")
	nonMemberID := createGroupTestUser(t, db, "getmember_none")
	regionID := createGroupTestRegion(t, db, "GetMember Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "GetMember Group",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID, nonMemberID}, []string{regionID})
	})

	t.Run("returns member when found", func(t *testing.T) {
		member, err := repo.GetMember(ctx, group.ID, userID)
		if err != nil {
			t.Fatalf("GetMember failed: %v", err)
		}
		if member == nil {
			t.Fatal("Expected member, got nil")
			return
		}
		if member.UserID != userID {
			t.Errorf("Expected user_id %s, got %s", userID, member.UserID)
		}
		if member.GroupID != group.ID {
			t.Errorf("Expected group_id %s, got %s", group.ID, member.GroupID)
		}
		if !member.IsAdmin {
			t.Error("Expected creator to be admin")
		}
		if member.TrustLevel == "" {
			t.Error("Expected trust_level to be set")
		}
	})

	t.Run("returns nil for non-member", func(t *testing.T) {
		member, err := repo.GetMember(ctx, group.ID, nonMemberID)
		if err != nil {
			t.Fatalf("GetMember failed: %v", err)
		}
		if member != nil {
			t.Errorf("Expected nil for non-member, got %+v", member)
		}
	})

	t.Run("returns nil for nonexistent group", func(t *testing.T) {
		member, err := repo.GetMember(ctx, uuid.New().String(), userID)
		if err != nil {
			t.Fatalf("GetMember failed: %v", err)
		}
		if member != nil {
			t.Errorf("Expected nil for nonexistent group, got %+v", member)
		}
	})
}

// =============================================================================
// Trust Vouch Tests
// =============================================================================

func TestGroupRepository_CreateTrustVouch(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	adminID := createGroupTestUser(t, db, "vouch_admin")
	memberID := createGroupTestUser(t, db, "vouch_member")
	member2ID := createGroupTestUser(t, db, "vouch_member2")
	nonMemberID := createGroupTestUser(t, db, "vouch_nonmember")
	regionID := createGroupTestRegion(t, db, "Vouch Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Vouch Group",
		RegionIDs: []string{regionID},
	}, adminID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	// Add regular members
	if err := repo.AddMember(ctx, group.ID, memberID, false, false); err != nil {
		t.Fatalf("Setup: AddMember failed: %v", err)
	}
	if err := repo.AddMember(ctx, group.ID, member2ID, false, false); err != nil {
		t.Fatalf("Setup: AddMember 2 failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{adminID, memberID, member2ID, nonMemberID}, []string{regionID})
	})

	t.Run("self-vouch rejected", func(t *testing.T) {
		err := repo.CreateTrustVouch(ctx, group.ID, adminID, adminID)
		if err != ErrSelfVouch {
			t.Errorf("Expected ErrSelfVouch, got %v", err)
		}
	})

	t.Run("non-trusted non-admin rejected", func(t *testing.T) {
		// memberID has trust_level='member', is_admin=false
		err := repo.CreateTrustVouch(ctx, group.ID, memberID, member2ID)
		if err != ErrNotTrustedOrAdmin {
			t.Errorf("Expected ErrNotTrustedOrAdmin, got %v", err)
		}
	})

	t.Run("non-member voucher rejected", func(t *testing.T) {
		err := repo.CreateTrustVouch(ctx, group.ID, nonMemberID, memberID)
		if err != ErrNotTrustedOrAdmin {
			t.Errorf("Expected ErrNotTrustedOrAdmin, got %v", err)
		}
	})

	t.Run("admin can vouch successfully", func(t *testing.T) {
		err := repo.CreateTrustVouch(ctx, group.ID, adminID, memberID)
		if err != nil {
			t.Fatalf("CreateTrustVouch failed: %v", err)
		}

		count, err := repo.GetTrustVouchCount(ctx, group.ID, memberID)
		if err != nil {
			t.Fatalf("GetTrustVouchCount failed: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 vouch, got %d", count)
		}
	})

	t.Run("auto-promotion at threshold", func(t *testing.T) {
		// Default trusted_vouch_threshold is 2. memberID already has 1 vouch from admin.
		// We need a second trusted/admin voucher. Promote member2 to trusted manually.
		_, err := db.ExecContext(ctx,
			"UPDATE group_members SET trust_level = 'trusted' WHERE group_id = ? AND user_id = ?",
			group.ID, member2ID)
		if err != nil {
			t.Fatalf("Failed to promote member2: %v", err)
		}

		err = repo.CreateTrustVouch(ctx, group.ID, member2ID, memberID)
		if err != nil {
			t.Fatalf("CreateTrustVouch failed: %v", err)
		}

		// Verify memberID was promoted to trusted
		member, err := repo.GetMember(ctx, group.ID, memberID)
		if err != nil {
			t.Fatalf("GetMember failed: %v", err)
		}
		if member.TrustLevel != "trusted" {
			t.Errorf("Expected trust_level 'trusted' after promotion, got %q", member.TrustLevel)
		}

		count, err := repo.GetTrustVouchCount(ctx, group.ID, memberID)
		if err != nil {
			t.Fatalf("GetTrustVouchCount failed: %v", err)
		}
		if count != 2 {
			t.Errorf("Expected 2 vouches, got %d", count)
		}
	})
}

func TestGroupRepository_GetTrustVouchCount(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "vouchcount1")
	regionID := createGroupTestRegion(t, db, "VouchCount Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "VouchCount Group",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	t.Run("returns 0 for user with no vouches", func(t *testing.T) {
		count, err := repo.GetTrustVouchCount(ctx, group.ID, userID)
		if err != nil {
			t.Fatalf("GetTrustVouchCount failed: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected 0, got %d", count)
		}
	})
}

func TestGroupRepository_ListTrustVouchesForUser(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	adminID := createGroupTestUser(t, db, "listvouches_admin")
	memberID := createGroupTestUser(t, db, "listvouches_member")
	regionID := createGroupTestRegion(t, db, "ListVouches Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      "ListVouches Group",
		RegionIDs: []string{regionID},
	}, adminID)
	if err != nil {
		t.Fatalf("Setup: Create failed: %v", err)
	}

	if err := repo.AddMember(ctx, group.ID, memberID, false, false); err != nil {
		t.Fatalf("Setup: AddMember failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{adminID, memberID}, []string{regionID})
	})

	t.Run("returns empty for user with no vouches", func(t *testing.T) {
		vouches, err := repo.ListTrustVouchesForUser(ctx, group.ID, memberID)
		if err != nil {
			t.Fatalf("ListTrustVouchesForUser failed: %v", err)
		}
		if len(vouches) != 0 {
			t.Errorf("Expected 0 vouches, got %d", len(vouches))
		}
	})

	t.Run("returns vouches after vouch created", func(t *testing.T) {
		err := repo.CreateTrustVouch(ctx, group.ID, adminID, memberID)
		if err != nil {
			t.Fatalf("CreateTrustVouch failed: %v", err)
		}

		vouches, err := repo.ListTrustVouchesForUser(ctx, group.ID, memberID)
		if err != nil {
			t.Fatalf("ListTrustVouchesForUser failed: %v", err)
		}
		if len(vouches) != 1 {
			t.Fatalf("Expected 1 vouch, got %d", len(vouches))
		}
		if vouches[0].VoucherUserID != adminID {
			t.Errorf("Expected voucher %s, got %s", adminID, vouches[0].VoucherUserID)
		}
		if vouches[0].VouchedUserID != memberID {
			t.Errorf("Expected vouched %s, got %s", memberID, vouches[0].VouchedUserID)
		}
		if vouches[0].GroupID != group.ID {
			t.Errorf("Expected group_id %s, got %s", group.ID, vouches[0].GroupID)
		}
	})
}

// =============================================================================
// UserMeetsAccessTier Tests
// =============================================================================

func TestUserMeetsAccessTier(t *testing.T) {
	adminMember := &models.GroupMember{
		IsAdmin:    true,
		TrustLevel: "trusted",
	}
	trustedMember := &models.GroupMember{
		IsAdmin:    false,
		TrustLevel: "trusted",
	}
	regularMember := &models.GroupMember{
		IsAdmin:    false,
		TrustLevel: "member",
	}

	tests := []struct {
		name               string
		tier               models.AccessTier
		isAuthenticated    bool
		isVerifiedResident bool
		memberInfo         *models.GroupMember
		expected           bool
	}{
		// Open tier
		{"open_authenticated", models.AccessTierOpen, true, false, nil, true},
		{"open_unauthenticated", models.AccessTierOpen, false, false, nil, false},

		// Resident tier
		{"resident_verified", models.AccessTierResident, true, true, nil, true},
		{"resident_not_verified", models.AccessTierResident, true, false, nil, false},
		{"resident_unauthenticated", models.AccessTierResident, false, false, nil, false},

		// Member tier
		{"member_is_member", models.AccessTierMember, true, false, regularMember, true},
		{"member_not_member", models.AccessTierMember, true, true, nil, false},

		// Trusted tier
		{"trusted_admin", models.AccessTierTrusted, true, false, adminMember, true},
		{"trusted_trusted_member", models.AccessTierTrusted, true, false, trustedMember, true},
		{"trusted_regular_member", models.AccessTierTrusted, true, false, regularMember, false},
		{"trusted_not_member", models.AccessTierTrusted, true, true, nil, false},

		// Admin-only tier
		{"admin_only_admin", models.AccessTierAdminOnly, true, false, adminMember, true},
		{"admin_only_trusted", models.AccessTierAdminOnly, true, false, trustedMember, false},
		{"admin_only_regular", models.AccessTierAdminOnly, true, false, regularMember, false},
		{"admin_only_not_member", models.AccessTierAdminOnly, true, true, nil, false},

		// Unknown tier
		{"unknown_tier", models.AccessTier("unknown"), true, true, adminMember, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UserMeetsAccessTier(tt.tier, tt.isAuthenticated, tt.isVerifiedResident, tt.memberInfo)
			if result != tt.expected {
				t.Errorf("UserMeetsAccessTier(%q, auth=%v, resident=%v, member=%v) = %v, want %v",
					tt.tier, tt.isAuthenticated, tt.isVerifiedResident, tt.memberInfo != nil, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// GroupTierMembershipPredicate Tests
// =============================================================================

func TestGroupTierMembershipPredicate(t *testing.T) {
	tests := []struct {
		name          string
		tier          models.AccessTier
		expectError   bool
		expectSuccess bool
		predicateSub  string // substring to check in predicate
	}{
		// Restricted tiers - should return valid predicates
		{
			name:           "resident_tier",
			tier:           models.AccessTierResident,
			expectSuccess:  true,
			predicateSub:   "postcard_verified = TRUE",
		},
		{
			name:           "member_tier",
			tier:           models.AccessTierMember,
			expectSuccess:  true,
			predicateSub:   "user_id IS NOT NULL",
		},
		{
			name:           "trusted_tier",
			tier:           models.AccessTierTrusted,
			expectSuccess:  true,
			predicateSub:   "trust_level = 'trusted'",
		},
		{
			name:           "admin_only_tier",
			tier:           models.AccessTierAdminOnly,
			expectSuccess:  true,
			predicateSub:   "is_admin = TRUE",
		},
		// Open tier - should error (not encrypted)
		{
			name:        "open_tier_error",
			tier:        models.AccessTierOpen,
			expectError: true,
		},
		// Unknown tier - should error
		{
			name:        "unknown_tier_error",
			tier:        models.AccessTier("invalid"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate, err := GroupTierMembershipPredicate(tt.tier)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for tier %q, got nil", tt.tier)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error for tier %q: %v", tt.tier, err)
				return
			}

			if !tt.expectSuccess {
				t.Errorf("Expected success for tier %q but test marked expectSuccess=false", tt.tier)
				return
			}

			if predicate == "" {
				t.Errorf("Expected non-empty predicate for tier %q", tt.tier)
				return
			}

			if tt.predicateSub != "" && !strings.Contains(predicate, tt.predicateSub) {
				t.Errorf("Expected predicate for tier %q to contain %q, got: %s", tt.tier, tt.predicateSub, predicate)
			}
		})
	}
}

// TestGroupTierMembershipPredicateInclusionSet verifies each tier's membership predicate
// correctly includes/excludes members based on their group membership status.
func TestGroupTierMembershipPredicateInclusionSet(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	// Create test users
	adminUserID := createGroupTestUser(t, db, "tier_admin")
	trustedUserID := createGroupTestUser(t, db, "tier_trusted")
	regularUserID := createGroupTestUser(t, db, "tier_regular")
	verifiedResidentID := createGroupTestUser(t, db, "tier_resident")
	nonMemberUserID := createGroupTestUser(t, db, "tier_nonmember")

	// Mark one user as postcard-verified (resident)
	_, err := db.ExecContext(ctx,
		"UPDATE users SET postcard_verified = TRUE WHERE id = ?",
		verifiedResidentID,
	)
	if err != nil {
		t.Fatalf("Failed to mark user as verified resident: %v", err)
	}

	regionID := createGroupTestRegion(t, db, "Tier Test Region")
	var groupIDs []string

	t.Cleanup(func() {
		cleanupGroupTest(t, db, groupIDs, []string{adminUserID, trustedUserID, regularUserID, verifiedResidentID, nonMemberUserID}, []string{regionID})
	})

	// Create a group
	req := &models.CreateGroupRequest{
		Name:      "Tier Test Group",
		RegionIDs: []string{regionID},
	}
	group, err := repo.Create(ctx, req, adminUserID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}
	groupIDs = append(groupIDs, group.ID)

	// Add group creator as admin (already added by Create)
	// Add trusted member
	err = repo.AddMember(ctx, group.ID, trustedUserID, false, false)
	if err != nil {
		t.Fatalf("Add trusted member failed: %v", err)
	}

	// Add regular member
	err = repo.AddMember(ctx, group.ID, regularUserID, false, false)
	if err != nil {
		t.Fatalf("Add regular member failed: %v", err)
	}

	// Add verified resident member
	err = repo.AddMember(ctx, group.ID, verifiedResidentID, false, false)
	if err != nil {
		t.Fatalf("Add verified resident member failed: %v", err)
	}

	// Promote trusted member to trusted status
	_, err = db.ExecContext(ctx,
		"UPDATE group_members SET trust_level = 'trusted' WHERE group_id = ? AND user_id = ?",
		group.ID, trustedUserID,
	)
	if err != nil {
		t.Fatalf("Failed to promote member to trusted: %v", err)
	}

	// Test resident tier - should include verified resident
	t.Run("resident_tier_inclusion", func(t *testing.T) {
		predicate, _ := GroupTierMembershipPredicate(models.AccessTierResident)
		query := `
			SELECT gm.user_id FROM group_members gm
			WHERE gm.group_id = ? AND ` + predicate + `
			ORDER BY gm.user_id
		`
		rows, err := db.QueryContext(ctx, query, group.ID)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		defer rows.Close()

		var resultIDs []string
		for rows.Next() {
			var userID string
			if err := rows.Scan(&userID); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			resultIDs = append(resultIDs, userID)
		}

		if len(resultIDs) != 1 || resultIDs[0] != verifiedResidentID {
			t.Errorf("Resident tier predicate: expected [%s], got %v", verifiedResidentID, resultIDs)
		}
	})

	// Test member tier - should include all members (admin, trusted, regular, verified resident)
	t.Run("member_tier_inclusion", func(t *testing.T) {
		predicate, _ := GroupTierMembershipPredicate(models.AccessTierMember)
		query := `
			SELECT gm.user_id FROM group_members gm
			WHERE gm.group_id = ? AND ` + predicate + `
		`
		rows, err := db.QueryContext(ctx, query, group.ID)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		defer rows.Close()

		var resultIDs []string
		for rows.Next() {
			var userID string
			if err := rows.Scan(&userID); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			resultIDs = append(resultIDs, userID)
		}

		expectedCount := 4 // admin, trusted, regular, verified resident
		if len(resultIDs) != expectedCount {
			t.Errorf("Member tier predicate: expected %d members, got %d", expectedCount, len(resultIDs))
		}
	})

	// Test trusted tier - should include admin and trusted members only
	t.Run("trusted_tier_inclusion", func(t *testing.T) {
		predicate, _ := GroupTierMembershipPredicate(models.AccessTierTrusted)
		query := `
			SELECT gm.user_id FROM group_members gm
			WHERE gm.group_id = ? AND ` + predicate + `
			ORDER BY gm.user_id
		`
		rows, err := db.QueryContext(ctx, query, group.ID)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		defer rows.Close()

		var resultIDs []string
		for rows.Next() {
			var userID string
			if err := rows.Scan(&userID); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			resultIDs = append(resultIDs, userID)
		}

		// Should include admin and trusted member
		expected := 2
		if len(resultIDs) != expected {
			t.Errorf("Trusted tier predicate: expected %d members, got %d: %v", expected, len(resultIDs), resultIDs)
		}

		// Verify admin and trusted are included
		foundAdmin := false
		foundTrusted := false
		for _, uid := range resultIDs {
			if uid == adminUserID {
				foundAdmin = true
			}
			if uid == trustedUserID {
				foundTrusted = true
			}
		}
		if !foundAdmin {
			t.Error("Trusted tier predicate: admin user not included")
		}
		if !foundTrusted {
			t.Error("Trusted tier predicate: trusted user not included")
		}
	})

	// Test admin-only tier - should include admin only
	t.Run("admin_only_tier_inclusion", func(t *testing.T) {
		predicate, _ := GroupTierMembershipPredicate(models.AccessTierAdminOnly)
		query := `
			SELECT gm.user_id FROM group_members gm
			WHERE gm.group_id = ? AND ` + predicate + `
			ORDER BY gm.user_id
		`
		rows, err := db.QueryContext(ctx, query, group.ID)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		defer rows.Close()

		var resultIDs []string
		for rows.Next() {
			var userID string
			if err := rows.Scan(&userID); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			resultIDs = append(resultIDs, userID)
		}

		if len(resultIDs) != 1 || resultIDs[0] != adminUserID {
			t.Errorf("Admin-only tier predicate: expected [%s], got %v", adminUserID, resultIDs)
		}
	})

	// Test that non-member is excluded from all tiers
	t.Run("non_member_excluded", func(t *testing.T) {
		for _, tier := range []models.AccessTier{models.AccessTierMember, models.AccessTierTrusted, models.AccessTierAdminOnly} {
			predicate, _ := GroupTierMembershipPredicate(tier)
			query := `
				SELECT EXISTS(
					SELECT 1 FROM group_members gm
					WHERE gm.group_id = ? AND gm.user_id = ? AND ` + predicate + `
				)
			`
			var exists bool
			err := db.QueryRowContext(ctx, query, group.ID, nonMemberUserID).Scan(&exists)
			if err != nil {
				t.Fatalf("Query failed: %v", err)
			}
			if exists {
				t.Errorf("Non-member should not be included in tier %s", tier)
			}
		}
	})
}

// =============================================================================
// Signal Group under Group Tests
// =============================================================================

func TestSignalGroupRepository_ListByOwnerGroup(t *testing.T) {
	db := testDB(t)
	groupRepo := NewGroupRepository(db)
	sgRepo := NewSignalGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "sglist1")
	regionID := createGroupTestRegion(t, db, "SG List Region")
	var createdGroupIDs []string

	t.Cleanup(func() {
		cleanupGroupTest(t, db, createdGroupIDs, []string{userID}, []string{regionID})
	})

	// Create a group
	req := &models.CreateGroupRequest{
		Name:      "SG List Test Group",
		RegionIDs: []string{regionID},
	}
	group, err := groupRepo.Create(ctx, req, userID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}
	createdGroupIDs = append(createdGroupIDs, group.ID)

	t.Run("returns empty list when no signal groups", func(t *testing.T) {
		signalGroups, err := sgRepo.ListByOwnerGroup(ctx, group.ID)
		if err != nil {
			t.Fatalf("ListByOwnerGroup failed: %v", err)
		}
		if len(signalGroups) != 0 {
			t.Errorf("Expected 0 signal groups, got %d", len(signalGroups))
		}
	})

	t.Run("returns signal groups owned by group", func(t *testing.T) {
		sg := &models.SignalGroup{
			OwnerGroupID: &group.ID,
			GroupName:    "Test Signal Chat",
			AccessTier:   models.AccessTierMember,
			CreatedBy:    &userID,
		}
		err := sgRepo.CreateForOwnerGroup(ctx, sg)
		if err != nil {
			t.Fatalf("CreateForOwnerGroup failed: %v", err)
		}

		signalGroups, err := sgRepo.ListByOwnerGroup(ctx, group.ID)
		if err != nil {
			t.Fatalf("ListByOwnerGroup failed: %v", err)
		}
		if len(signalGroups) != 1 {
			t.Fatalf("Expected 1 signal group, got %d", len(signalGroups))
		}
		if signalGroups[0].GroupName != "Test Signal Chat" {
			t.Errorf("Expected name 'Test Signal Chat', got %q", signalGroups[0].GroupName)
		}
		if signalGroups[0].OwnerGroupID == nil || *signalGroups[0].OwnerGroupID != group.ID {
			t.Error("Expected owner_group_id to match group ID")
		}
		if signalGroups[0].RegionID != nil {
			t.Error("Expected region_id to be nil")
		}
		if signalGroups[0].SchoolID != nil {
			t.Error("Expected school_id to be nil")
		}
		if signalGroups[0].DistrictID != nil {
			t.Error("Expected district_id to be nil")
		}
	})

	t.Run("does not return deactivated signal groups", func(t *testing.T) {
		sg := &models.SignalGroup{
			OwnerGroupID: &group.ID,
			GroupName:    "Deactivated Chat",
			AccessTier:   models.AccessTierOpen,
			CreatedBy:    &userID,
		}
		err := sgRepo.CreateForOwnerGroup(ctx, sg)
		if err != nil {
			t.Fatalf("CreateForOwnerGroup failed: %v", err)
		}

		err = sgRepo.Deactivate(ctx, sg.ID)
		if err != nil {
			t.Fatalf("Deactivate failed: %v", err)
		}

		signalGroups, err := sgRepo.ListByOwnerGroup(ctx, group.ID)
		if err != nil {
			t.Fatalf("ListByOwnerGroup failed: %v", err)
		}
		// Should only have the one from previous subtest
		if len(signalGroups) != 1 {
			t.Errorf("Expected 1 active signal group, got %d", len(signalGroups))
		}
	})
}

func TestSignalGroupRepository_CountByOwnerGroup(t *testing.T) {
	db := testDB(t)
	groupRepo := NewGroupRepository(db)
	sgRepo := NewSignalGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "sgcount1")
	regionID := createGroupTestRegion(t, db, "SG Count Region")
	var createdGroupIDs []string

	t.Cleanup(func() {
		cleanupGroupTest(t, db, createdGroupIDs, []string{userID}, []string{regionID})
	})

	group, err := groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name:      "SG Count Test Group",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}
	createdGroupIDs = append(createdGroupIDs, group.ID)

	t.Run("returns zero when no signal groups", func(t *testing.T) {
		count, err := sgRepo.CountByOwnerGroup(ctx, group.ID)
		if err != nil {
			t.Fatalf("CountByOwnerGroup failed: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected count 0, got %d", count)
		}
	})

	t.Run("returns correct count", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			sg := &models.SignalGroup{
				OwnerGroupID: &group.ID,
				GroupName:    "Chat " + string(rune('A'+i)),
				AccessTier:   models.AccessTierMember,
				CreatedBy:    &userID,
			}
			if err := sgRepo.CreateForOwnerGroup(ctx, sg); err != nil {
				t.Fatalf("CreateForOwnerGroup failed: %v", err)
			}
		}

		count, err := sgRepo.CountByOwnerGroup(ctx, group.ID)
		if err != nil {
			t.Fatalf("CountByOwnerGroup failed: %v", err)
		}
		if count != 3 {
			t.Errorf("Expected count 3, got %d", count)
		}
	})
}

func TestSignalGroupRepository_CreateForOwnerGroup(t *testing.T) {
	db := testDB(t)
	groupRepo := NewGroupRepository(db)
	sgRepo := NewSignalGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "sgcreate1")
	regionID := createGroupTestRegion(t, db, "SG Create Region")
	var createdGroupIDs []string

	t.Cleanup(func() {
		cleanupGroupTest(t, db, createdGroupIDs, []string{userID}, []string{regionID})
	})

	group, err := groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name:      "SG Create Test Group",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}
	createdGroupIDs = append(createdGroupIDs, group.ID)

	t.Run("creates signal group with owner_group_id", func(t *testing.T) {
		sg := &models.SignalGroup{
			OwnerGroupID: &group.ID,
			GroupName:    "Created Chat",
			Description:  strPtr("A test chat"),
			AccessTier:   models.AccessTierTrusted,
			CreatedBy:    &userID,
		}
		err := sgRepo.CreateForOwnerGroup(ctx, sg)
		if err != nil {
			t.Fatalf("CreateForOwnerGroup failed: %v", err)
		}

		if sg.ID == "" {
			t.Error("Expected ID to be set")
		}
		if !sg.IsActive {
			t.Error("Expected IsActive to be true")
		}

		// Verify via GetByID
		fetched, err := sgRepo.GetByID(ctx, sg.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if fetched.OwnerGroupID == nil || *fetched.OwnerGroupID != group.ID {
			t.Error("Expected owner_group_id to match")
		}
		if fetched.RegionID != nil {
			t.Error("Expected region_id to be nil")
		}
		if fetched.SchoolID != nil {
			t.Error("Expected school_id to be nil")
		}
		if fetched.DistrictID != nil {
			t.Error("Expected district_id to be nil")
		}
		if string(fetched.AccessTier) != string(models.AccessTierTrusted) {
			t.Errorf("Expected access_tier 'trusted', got %q", fetched.AccessTier)
		}
	})

	t.Run("fails without owner_group_id", func(t *testing.T) {
		sg := &models.SignalGroup{
			GroupName:  "No Owner Chat",
			AccessTier: models.AccessTierMember,
			CreatedBy:  &userID,
		}
		err := sgRepo.CreateForOwnerGroup(ctx, sg)
		if err == nil {
			t.Fatal("Expected error when owner_group_id is nil")
		}
	})

	t.Run("clears region_id/school_id/district_id", func(t *testing.T) {
		// Even if these are set, CreateForOwnerGroup should nil them out
		sg := &models.SignalGroup{
			OwnerGroupID: &group.ID,
			RegionID:     &regionID,
			GroupName:    "Cleaned Chat",
			AccessTier:   models.AccessTierMember,
			CreatedBy:    &userID,
		}
		err := sgRepo.CreateForOwnerGroup(ctx, sg)
		if err != nil {
			t.Fatalf("CreateForOwnerGroup failed: %v", err)
		}

		fetched, err := sgRepo.GetByID(ctx, sg.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if fetched.RegionID != nil {
			t.Error("Expected region_id to be nil after CreateForOwnerGroup")
		}
	})
}

func TestGroupRepository_GetByIDWithDetails_IncludesSignalGroups(t *testing.T) {
	db := testDB(t)
	groupRepo := NewGroupRepository(db)
	sgRepo := NewSignalGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "sgdetails1")
	regionID := createGroupTestRegion(t, db, "SG Details Region")
	var createdGroupIDs []string

	t.Cleanup(func() {
		cleanupGroupTest(t, db, createdGroupIDs, []string{userID}, []string{regionID})
	})

	group, err := groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name:      "SG Details Test Group",
		RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}
	createdGroupIDs = append(createdGroupIDs, group.ID)

	// Create two signal groups
	for _, name := range []string{"Chat Alpha", "Chat Beta"} {
		sg := &models.SignalGroup{
			OwnerGroupID: &group.ID,
			GroupName:    name,
			AccessTier:   models.AccessTierMember,
			CreatedBy:    &userID,
		}
		if err := sgRepo.CreateForOwnerGroup(ctx, sg); err != nil {
			t.Fatalf("CreateForOwnerGroup failed: %v", err)
		}
	}

	details, err := groupRepo.GetByIDWithDetails(ctx, group.ID, userID)
	if err != nil {
		t.Fatalf("GetByIDWithDetails failed: %v", err)
	}

	if len(details.SignalGroups) != 2 {
		t.Fatalf("Expected 2 signal groups, got %d", len(details.SignalGroups))
	}

	// Verify the signal groups have the expected fields populated
	for _, sg := range details.SignalGroups {
		if sg.ID == "" {
			t.Error("Expected signal group ID to be set")
		}
		if sg.OwnerGroupID == nil || *sg.OwnerGroupID != group.ID {
			t.Error("Expected owner_group_id to match group ID")
		}
		if sg.Name == "" {
			t.Error("Expected signal group name to be set")
		}
		if sg.AccessTier == "" {
			t.Error("Expected access_tier to be set")
		}
	}
}

// =============================================================================
// Browse Tests
// =============================================================================

// makeActiveListedGroup creates a group, promotes it to active+listed, and returns its ID.
func makeActiveListedGroup(t *testing.T, repo *GroupRepository, db *DB, ctx context.Context, name, regionID, userID string) string {
	t.Helper()
	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:      name,
		RegionIDs: []string{regionID},
		TopicTags: []string{"tag1"},
	}, userID)
	if err != nil {
		t.Fatalf("Create group %q failed: %v", name, err)
	}
	_, err = db.ExecContext(ctx, "UPDATE `groups` SET status = 'active', visibility = 'listed' WHERE id = ?", group.ID)
	if err != nil {
		t.Fatalf("Promote group %q failed: %v", name, err)
	}
	return group.ID
}

func TestGroupRepository_BrowseByRegion(t *testing.T) {
	db := testDB(t)
	groupRepo := NewGroupRepository(db)
	sgRepo := NewSignalGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "browse_region1")
	regionA := createGroupTestRegion(t, db, "Browse Region A")
	regionB := createGroupTestRegion(t, db, "Browse Region B")
	var createdGroupIDs []string

	t.Cleanup(func() {
		cleanupGroupTest(t, db, createdGroupIDs, []string{userID}, []string{regionA, regionB})
	})

	// Group 1: listed+active in regionA
	groupListedActive := makeActiveListedGroup(t, groupRepo, db, ctx, "Listed Active Group", regionA, userID)
	createdGroupIDs = append(createdGroupIDs, groupListedActive)

	// Group 2: unlisted+active in regionA
	groupUnlisted, err := groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name:       "Unlisted Group",
		Visibility: "unlisted",
		RegionIDs:  []string{regionA},
	}, userID)
	if err != nil {
		t.Fatalf("Create unlisted group failed: %v", err)
	}
	createdGroupIDs = append(createdGroupIDs, groupUnlisted.ID)
	_, _ = db.ExecContext(ctx, "UPDATE `groups` SET status = 'active' WHERE id = ?", groupUnlisted.ID)

	// Group 3: listed+provisional in regionA
	groupProvisional, err := groupRepo.Create(ctx, &models.CreateGroupRequest{
		Name:      "Provisional Group",
		RegionIDs: []string{regionA},
	}, userID)
	if err != nil {
		t.Fatalf("Create provisional group failed: %v", err)
	}
	createdGroupIDs = append(createdGroupIDs, groupProvisional.ID)
	// Set listed but keep provisional
	_, _ = db.ExecContext(ctx, "UPDATE `groups` SET visibility = 'listed' WHERE id = ?", groupProvisional.ID)

	// Group 4: discoverable in regionB with open signal group
	groupDiscoverable := makeActiveListedGroup(t, groupRepo, db, ctx, "Discoverable Group", regionB, userID)
	createdGroupIDs = append(createdGroupIDs, groupDiscoverable)
	_, _ = db.ExecContext(ctx, "UPDATE `groups` SET discoverable_by_unverified = TRUE WHERE id = ?", groupDiscoverable)
	sgOpen := &models.SignalGroup{
		OwnerGroupID: &groupDiscoverable,
		GroupName:    "Open Chat",
		AccessTier:   models.AccessTierOpen,
		CreatedBy:    &userID,
	}
	if err := sgRepo.CreateForOwnerGroup(ctx, sgOpen); err != nil {
		t.Fatalf("Create open signal group failed: %v", err)
	}

	// Group 5: discoverable in regionB but NO open signal group
	groupDiscoverableNoOpen := makeActiveListedGroup(t, groupRepo, db, ctx, "Discoverable No Open", regionB, userID)
	createdGroupIDs = append(createdGroupIDs, groupDiscoverableNoOpen)
	_, _ = db.ExecContext(ctx, "UPDATE `groups` SET discoverable_by_unverified = TRUE WHERE id = ?", groupDiscoverableNoOpen)
	sgMember := &models.SignalGroup{
		OwnerGroupID: &groupDiscoverableNoOpen,
		GroupName:    "Member Chat",
		AccessTier:   models.AccessTierMember,
		CreatedBy:    &userID,
	}
	if err := sgRepo.CreateForOwnerGroup(ctx, sgMember); err != nil {
		t.Fatalf("Create member signal group failed: %v", err)
	}

	t.Run("returns listed active groups in region", func(t *testing.T) {
		groups, err := groupRepo.BrowseByRegion(ctx, regionA, false)
		if err != nil {
			t.Fatalf("BrowseByRegion failed: %v", err)
		}
		if len(groups) != 1 {
			t.Fatalf("Expected 1 group, got %d", len(groups))
		}
		if groups[0].ID != groupListedActive {
			t.Errorf("Expected group %s, got %s", groupListedActive, groups[0].ID)
		}
		// Verify enrichment
		if len(groups[0].Regions) == 0 {
			t.Error("Expected regions to be populated")
		}
		if len(groups[0].TopicTags) == 0 {
			t.Error("Expected topic tags to be populated")
		}
		// IsUserMember/IsUserAdmin should be false (browse, not user-specific)
		if groups[0].IsUserMember || groups[0].IsUserAdmin {
			t.Error("Expected IsUserMember and IsUserAdmin to be false for browse")
		}
	})

	t.Run("excludes unlisted groups", func(t *testing.T) {
		groups, err := groupRepo.BrowseByRegion(ctx, regionA, false)
		if err != nil {
			t.Fatalf("BrowseByRegion failed: %v", err)
		}
		for _, g := range groups {
			if g.ID == groupUnlisted.ID {
				t.Error("Unlisted group should not appear in browse results")
			}
		}
	})

	t.Run("excludes provisional groups", func(t *testing.T) {
		groups, err := groupRepo.BrowseByRegion(ctx, regionA, false)
		if err != nil {
			t.Fatalf("BrowseByRegion failed: %v", err)
		}
		for _, g := range groups {
			if g.ID == groupProvisional.ID {
				t.Error("Provisional group should not appear in browse results")
			}
		}
	})

	t.Run("with includeUnverifiedDiscoverable includes discoverable groups from other regions", func(t *testing.T) {
		groups, err := groupRepo.BrowseByRegion(ctx, regionA, true)
		if err != nil {
			t.Fatalf("BrowseByRegion failed: %v", err)
		}

		foundListed := false
		foundDiscoverable := false
		for _, g := range groups {
			if g.ID == groupListedActive {
				foundListed = true
			}
			if g.ID == groupDiscoverable {
				foundDiscoverable = true
			}
		}
		if !foundListed {
			t.Error("Expected listed active group from regionA")
		}
		if !foundDiscoverable {
			t.Error("Expected discoverable group from regionB")
		}
	})

	t.Run("discoverable group without open-tier signal group is excluded", func(t *testing.T) {
		groups, err := groupRepo.BrowseByRegion(ctx, regionA, true)
		if err != nil {
			t.Fatalf("BrowseByRegion failed: %v", err)
		}
		for _, g := range groups {
			if g.ID == groupDiscoverableNoOpen {
				t.Error("Discoverable group without open signal group should not appear")
			}
		}
	})

	t.Run("deduplicates groups in region that are also discoverable", func(t *testing.T) {
		// Make the discoverable group also belong to regionA
		_ = groupRepo.AddRegion(ctx, groupDiscoverable, regionA)
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM group_regions WHERE group_id = ? AND region_id = ?", groupDiscoverable, regionA)
		}()

		groups, err := groupRepo.BrowseByRegion(ctx, regionA, true)
		if err != nil {
			t.Fatalf("BrowseByRegion failed: %v", err)
		}

		discoverableCount := 0
		for _, g := range groups {
			if g.ID == groupDiscoverable {
				discoverableCount++
			}
		}
		if discoverableCount != 1 {
			t.Errorf("Expected discoverable group to appear once, got %d", discoverableCount)
		}
	})
}

func TestGroupRepository_BrowseAll(t *testing.T) {
	db := testDB(t)
	groupRepo := NewGroupRepository(db)
	sgRepo := NewSignalGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "browse_all1")
	regionID := createGroupTestRegion(t, db, "Browse All Region")
	var createdGroupIDs []string

	t.Cleanup(func() {
		cleanupGroupTest(t, db, createdGroupIDs, []string{userID}, []string{regionID})
	})

	t.Run("returns empty for no discoverable groups", func(t *testing.T) {
		// BrowseAll may include pre-existing rows on a shared test DB, so we only
		// assert it returns without error rather than asserting an exact count.
		if _, err := groupRepo.BrowseAll(ctx); err != nil {
			t.Fatalf("BrowseAll failed: %v", err)
		}
	})

	t.Run("returns only discoverable groups with open-tier signal groups", func(t *testing.T) {
		// Create a discoverable group with open signal group
		discoverableID := makeActiveListedGroup(t, groupRepo, db, ctx, "BrowseAll Discoverable", regionID, userID)
		createdGroupIDs = append(createdGroupIDs, discoverableID)
		_, _ = db.ExecContext(ctx, "UPDATE `groups` SET discoverable_by_unverified = TRUE WHERE id = ?", discoverableID)
		sgOpen := &models.SignalGroup{
			OwnerGroupID: &discoverableID,
			GroupName:    "Open Chat All",
			AccessTier:   models.AccessTierOpen,
			CreatedBy:    &userID,
		}
		if err := sgRepo.CreateForOwnerGroup(ctx, sgOpen); err != nil {
			t.Fatalf("Create open signal group failed: %v", err)
		}

		// Create a non-discoverable listed active group (should NOT appear)
		nonDiscoverableID := makeActiveListedGroup(t, groupRepo, db, ctx, "Non Discoverable", regionID, userID)
		createdGroupIDs = append(createdGroupIDs, nonDiscoverableID)

		// Create a discoverable group without open signal group (should NOT appear)
		discoverableNoOpenID := makeActiveListedGroup(t, groupRepo, db, ctx, "Discoverable No Open All", regionID, userID)
		createdGroupIDs = append(createdGroupIDs, discoverableNoOpenID)
		_, _ = db.ExecContext(ctx, "UPDATE `groups` SET discoverable_by_unverified = TRUE WHERE id = ?", discoverableNoOpenID)

		groups, err := groupRepo.BrowseAll(ctx)
		if err != nil {
			t.Fatalf("BrowseAll failed: %v", err)
		}

		foundDiscoverable := false
		for _, g := range groups {
			if g.ID == discoverableID {
				foundDiscoverable = true
				if len(g.Regions) == 0 {
					t.Error("Expected regions to be populated")
				}
			}
			if g.ID == nonDiscoverableID {
				t.Error("Non-discoverable group should not appear in BrowseAll")
			}
			if g.ID == discoverableNoOpenID {
				t.Error("Discoverable group without open signal group should not appear in BrowseAll")
			}
		}
		if !foundDiscoverable {
			t.Error("Expected discoverable group with open signal group to appear")
		}
	})
}

// =============================================================================
// Group Resource Tests
// =============================================================================

func TestGroupRepository_CreateResource(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "res_create")
	regionID := createGroupTestRegion(t, db, "Resource Create Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:       "Resource Test Group",
		Visibility: "unlisted",
		RegionIDs:  []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	t.Run("creates resource with all fields", func(t *testing.T) {
		req := &models.CreateResourceRequest{
			Title:       "Community Wiki",
			URL:         "https://wiki.example.com",
			Description: "Our shared knowledge base",
			AccessTier:  "member",
		}

		resource, err := repo.CreateResource(ctx, group.ID, userID, req)
		if err != nil {
			t.Fatalf("CreateResource failed: %v", err)
		}

		if resource.ID == "" {
			t.Error("Expected resource ID to be set")
		}
		if resource.GroupID != group.ID {
			t.Errorf("Expected group_id %q, got %q", group.ID, resource.GroupID)
		}
		if resource.Title != "Community Wiki" {
			t.Errorf("Expected title 'Community Wiki', got %q", resource.Title)
		}
		if resource.URL != "https://wiki.example.com" {
			t.Errorf("Expected URL 'https://wiki.example.com', got %q", resource.URL)
		}
		if resource.Description == nil || *resource.Description != "Our shared knowledge base" {
			t.Errorf("Expected description 'Our shared knowledge base', got %v", resource.Description)
		}
		if resource.AccessTier != models.AccessTierMember {
			t.Errorf("Expected access_tier 'member', got %q", resource.AccessTier)
		}
		if resource.CreatedBy == nil || *resource.CreatedBy != userID {
			t.Error("Expected created_by to match creator")
		}
	})
}

func TestGroupRepository_GetResource(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "res_get")
	regionID := createGroupTestRegion(t, db, "Resource Get Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:       "Resource Get Group",
		Visibility: "unlisted",
		RegionIDs:  []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	t.Run("returns resource by ID", func(t *testing.T) {
		created, err := repo.CreateResource(ctx, group.ID, userID, &models.CreateResourceRequest{
			Title:      "Findable Resource",
			URL:        "https://find.example.com",
			AccessTier: "open",
		})
		if err != nil {
			t.Fatalf("CreateResource failed: %v", err)
		}

		found, err := repo.GetResource(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetResource failed: %v", err)
		}
		if found.Title != "Findable Resource" {
			t.Errorf("Expected title 'Findable Resource', got %q", found.Title)
		}
	})

	t.Run("returns not found for missing ID", func(t *testing.T) {
		_, err := repo.GetResource(ctx, "nonexistent-id")
		if err != ErrResourceNotFound {
			t.Errorf("Expected ErrResourceNotFound, got %v", err)
		}
	})
}

func TestGroupRepository_ListResources(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "res_list")
	regionID := createGroupTestRegion(t, db, "Resource List Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:       "Resource List Group",
		Visibility: "unlisted",
		RegionIDs:  []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	// Create multiple resources
	for i, title := range []string{"First", "Second", "Third"} {
		_, err := repo.CreateResource(ctx, group.ID, userID, &models.CreateResourceRequest{
			Title:      title,
			URL:        "https://example.com/" + strconv.Itoa(i),
			AccessTier: "member",
		})
		if err != nil {
			t.Fatalf("CreateResource %q failed: %v", title, err)
		}
	}

	resources, err := repo.ListResources(ctx, group.ID)
	if err != nil {
		t.Fatalf("ListResources failed: %v", err)
	}

	if len(resources) != 3 {
		t.Fatalf("Expected 3 resources, got %d", len(resources))
	}

	// Verify all expected titles are present
	titleSet := make(map[string]bool)
	for _, r := range resources {
		titleSet[r.Title] = true
	}
	for _, expected := range []string{"First", "Second", "Third"} {
		if !titleSet[expected] {
			t.Errorf("Expected resource with title %q to be present", expected)
		}
	}
}

func TestGroupRepository_UpdateResource(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "res_upd")
	regionID := createGroupTestRegion(t, db, "Resource Update Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:       "Resource Update Group",
		Visibility: "unlisted",
		RegionIDs:  []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	created, err := repo.CreateResource(ctx, group.ID, userID, &models.CreateResourceRequest{
		Title:      "Original Title",
		URL:        "https://original.example.com",
		AccessTier: "member",
	})
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}

	t.Run("updates individual fields", func(t *testing.T) {
		newTitle := "Updated Title"
		newTier := "admin_only"
		err := repo.UpdateResource(ctx, created.ID, &models.UpdateResourceRequest{
			Title:      &newTitle,
			AccessTier: &newTier,
		})
		if err != nil {
			t.Fatalf("UpdateResource failed: %v", err)
		}

		updated, err := repo.GetResource(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetResource after update failed: %v", err)
		}
		if updated.Title != "Updated Title" {
			t.Errorf("Expected title 'Updated Title', got %q", updated.Title)
		}
		if updated.AccessTier != models.AccessTierAdminOnly {
			t.Errorf("Expected access_tier 'admin_only', got %q", updated.AccessTier)
		}
		// URL should remain unchanged
		if updated.URL != "https://original.example.com" {
			t.Errorf("Expected URL unchanged, got %q", updated.URL)
		}
	})

	t.Run("returns not found for missing ID", func(t *testing.T) {
		newTitle := "Nope"
		err := repo.UpdateResource(ctx, "nonexistent-id", &models.UpdateResourceRequest{
			Title: &newTitle,
		})
		if err != ErrResourceNotFound {
			t.Errorf("Expected ErrResourceNotFound, got %v", err)
		}
	})
}

func TestGroupRepository_DeleteResource(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "res_del")
	regionID := createGroupTestRegion(t, db, "Resource Delete Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name:       "Resource Delete Group",
		Visibility: "unlisted",
		RegionIDs:  []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	created, err := repo.CreateResource(ctx, group.ID, userID, &models.CreateResourceRequest{
		Title:      "To Delete",
		URL:        "https://delete.example.com",
		AccessTier: "member",
	})
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}

	t.Run("deletes resource", func(t *testing.T) {
		err := repo.DeleteResource(ctx, created.ID)
		if err != nil {
			t.Fatalf("DeleteResource failed: %v", err)
		}

		_, err = repo.GetResource(ctx, created.ID)
		if err != ErrResourceNotFound {
			t.Errorf("Expected ErrResourceNotFound after delete, got %v", err)
		}
	})

	t.Run("returns not found for missing ID", func(t *testing.T) {
		err := repo.DeleteResource(ctx, "nonexistent-id")
		if err != ErrResourceNotFound {
			t.Errorf("Expected ErrResourceNotFound, got %v", err)
		}
	})
}

// =============================================================================
// Topic Board Tests
// =============================================================================

func createGroupTestStateRegion(t *testing.T, db *DB, name string) string {
	t.Helper()
	regionID := uuid.New().String()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO geographic_regions (id, name, region_type, created_at) VALUES (?, ?, 'state', NOW())",
		regionID, name)
	if err != nil {
		t.Fatalf("Failed to create test state region %s: %v", name, err)
	}
	return regionID
}

func createGroupTestCityRegion(t *testing.T, db *DB, name, parentID string) string {
	t.Helper()
	regionID := uuid.New().String()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO geographic_regions (id, name, region_type, parent_region_id, created_at) VALUES (?, ?, 'city', ?, NOW())",
		regionID, name, parentID)
	if err != nil {
		t.Fatalf("Failed to create test city region %s: %v", name, err)
	}
	return regionID
}

func TestGroupRepository_DeriveRegionLabel(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "derive_label")
	stateID := createGroupTestStateRegion(t, db, "Washington")
	cityID := createGroupTestCityRegion(t, db, "Seattle", stateID)

	req := &models.CreateGroupRequest{
		Name:       "Label Test Group",
		Visibility: "unlisted",
		RegionIDs:  []string{cityID},
	}
	group, err := repo.Create(ctx, req, userID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{cityID, stateID})
	})

	t.Run("returns state name for city region", func(t *testing.T) {
		label, err := repo.DeriveRegionLabel(ctx, group.ID)
		if err != nil {
			t.Fatalf("DeriveRegionLabel failed: %v", err)
		}
		if label != "Washington" {
			t.Errorf("Expected 'Washington', got %q", label)
		}
	})

	t.Run("returns Unknown for nonexistent group", func(t *testing.T) {
		label, err := repo.DeriveRegionLabel(ctx, "nonexistent-group-id")
		if err != nil {
			t.Fatalf("DeriveRegionLabel failed: %v", err)
		}
		if label != "Unknown" {
			t.Errorf("Expected 'Unknown', got %q", label)
		}
	})
}

func TestGroupRepository_TopicBoardPosting_CreateAndUpdate(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "tb_create")
	stateID := createGroupTestStateRegion(t, db, "Oregon")
	cityID := createGroupTestCityRegion(t, db, "Portland", stateID)

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name: "Topic Board Group", Visibility: "unlisted", RegionIDs: []string{cityID},
	}, userID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{cityID, stateID})
	})

	t.Run("creates posting with auto label", func(t *testing.T) {
		req := &models.CreateTopicBoardPostingRequest{
			Description: "Looking for mutual aid partners in the area",
			Tags:        []string{"mutual-aid", "safety"},
		}

		posting, err := repo.CreateOrUpdatePosting(ctx, group.ID, req)
		if err != nil {
			t.Fatalf("CreateOrUpdatePosting failed: %v", err)
		}

		if posting.ID == "" {
			t.Error("Expected posting ID")
		}
		if posting.GroupID != group.ID {
			t.Errorf("Expected group ID %s, got %s", group.ID, posting.GroupID)
		}
		if posting.RegionLabel != "Oregon" {
			t.Errorf("Expected auto region label 'Oregon', got %q", posting.RegionLabel)
		}
		if posting.AutoRegionLabel == nil || *posting.AutoRegionLabel != "Oregon" {
			t.Errorf("Expected auto_region_label 'Oregon', got %v", posting.AutoRegionLabel)
		}
		if posting.Description != "Looking for mutual aid partners in the area" {
			t.Errorf("Unexpected description: %s", posting.Description)
		}
		if len(posting.Tags) != 2 {
			t.Fatalf("Expected 2 tags, got %d", len(posting.Tags))
		}
		if !posting.IsActive {
			t.Error("Expected posting to be active")
		}
	})

	t.Run("updates existing posting (upsert)", func(t *testing.T) {
		customLabel := "Pacific Northwest"
		req := &models.CreateTopicBoardPostingRequest{
			RegionLabel: &customLabel,
			Description: "Updated: seeking community defense allies",
			Tags:        []string{"defense", "community"},
		}

		posting, err := repo.CreateOrUpdatePosting(ctx, group.ID, req)
		if err != nil {
			t.Fatalf("CreateOrUpdatePosting (update) failed: %v", err)
		}

		if posting.RegionLabel != "Pacific Northwest" {
			t.Errorf("Expected custom label 'Pacific Northwest', got %q", posting.RegionLabel)
		}
		if posting.Description != "Updated: seeking community defense allies" {
			t.Errorf("Unexpected description: %s", posting.Description)
		}
		if len(posting.Tags) != 2 || posting.Tags[0] != "defense" || posting.Tags[1] != "community" {
			t.Errorf("Expected tags [defense, community], got %v", posting.Tags)
		}
	})
}

func TestGroupRepository_TopicBoardPosting_GetAndRemove(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userID := createGroupTestUser(t, db, "tb_getrem")
	regionID := createGroupTestRegion(t, db, "Get Remove Region")

	group, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name: "Get Remove Group", Visibility: "unlisted", RegionIDs: []string{regionID},
	}, userID)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{group.ID}, []string{userID}, []string{regionID})
	})

	t.Run("get returns not found when no posting exists", func(t *testing.T) {
		_, err := repo.GetPosting(ctx, group.ID)
		if err != ErrTopicBoardPostingNotFound {
			t.Errorf("Expected ErrTopicBoardPostingNotFound, got %v", err)
		}
	})

	t.Run("get returns posting after creation", func(t *testing.T) {
		_, err := repo.CreateOrUpdatePosting(ctx, group.ID, &models.CreateTopicBoardPostingRequest{
			Description: "Test posting for get test scenario",
			Tags:        []string{"test"},
		})
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		posting, err := repo.GetPosting(ctx, group.ID)
		if err != nil {
			t.Fatalf("GetPosting failed: %v", err)
		}
		if posting.Description != "Test posting for get test scenario" {
			t.Errorf("Unexpected description: %s", posting.Description)
		}
		if len(posting.Tags) != 1 || posting.Tags[0] != "test" {
			t.Errorf("Expected [test], got %v", posting.Tags)
		}
	})

	t.Run("remove deletes posting", func(t *testing.T) {
		err := repo.RemovePosting(ctx, group.ID)
		if err != nil {
			t.Fatalf("RemovePosting failed: %v", err)
		}

		_, err = repo.GetPosting(ctx, group.ID)
		if err != ErrTopicBoardPostingNotFound {
			t.Errorf("Expected not found after remove, got %v", err)
		}
	})

	t.Run("remove returns not found when no posting", func(t *testing.T) {
		err := repo.RemovePosting(ctx, group.ID)
		if err != ErrTopicBoardPostingNotFound {
			t.Errorf("Expected ErrTopicBoardPostingNotFound, got %v", err)
		}
	})
}

func TestGroupRepository_BrowsePostings(t *testing.T) {
	db := testDB(t)
	repo := NewGroupRepository(db)
	ctx := context.Background()

	userA := createGroupTestUser(t, db, "tb_browse_a")
	userB := createGroupTestUser(t, db, "tb_browse_b")
	regionID := createGroupTestRegion(t, db, "Browse Region")

	groupA, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name: "Browse Group A", Visibility: "unlisted", RegionIDs: []string{regionID},
	}, userA)
	if err != nil {
		t.Fatalf("Create group A failed: %v", err)
	}
	groupB, err := repo.Create(ctx, &models.CreateGroupRequest{
		Name: "Browse Group B", Visibility: "unlisted", RegionIDs: []string{regionID},
	}, userB)
	if err != nil {
		t.Fatalf("Create group B failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupGroupTest(t, db, []string{groupA.ID, groupB.ID}, []string{userA, userB}, []string{regionID})
	})

	// Create postings for both groups
	_, err = repo.CreateOrUpdatePosting(ctx, groupA.ID, &models.CreateTopicBoardPostingRequest{
		Description: "Group A posting for browsing test",
		Tags:        []string{"mutual-aid", "safety"},
	})
	if err != nil {
		t.Fatalf("Create posting A failed: %v", err)
	}
	_, err = repo.CreateOrUpdatePosting(ctx, groupB.ID, &models.CreateTopicBoardPostingRequest{
		Description: "Group B posting for browsing test",
		Tags:        []string{"mutual-aid", "defense"},
	})
	if err != nil {
		t.Fatalf("Create posting B failed: %v", err)
	}

	t.Run("returns matching postings by tag", func(t *testing.T) {
		// Browse as group A, looking for "mutual-aid" — should see group B
		postings, err := repo.BrowsePostings(ctx, "mutual-aid", groupA.ID, 20, 0)
		if err != nil {
			t.Fatalf("BrowsePostings failed: %v", err)
		}
		if len(postings) != 1 {
			t.Fatalf("Expected 1 posting (group B), got %d", len(postings))
		}
		if postings[0].GroupID != groupB.ID {
			t.Errorf("Expected group B posting, got group %s", postings[0].GroupID)
		}
	})

	t.Run("filters by tag correctly", func(t *testing.T) {
		// Browse as group A for "defense" — should see group B
		postings, err := repo.BrowsePostings(ctx, "defense", groupA.ID, 20, 0)
		if err != nil {
			t.Fatalf("BrowsePostings failed: %v", err)
		}
		if len(postings) != 1 {
			t.Fatalf("Expected 1 posting with 'defense' tag, got %d", len(postings))
		}

		// Browse for a tag no one has
		postings, err = repo.BrowsePostings(ctx, "nonexistent-tag", groupA.ID, 20, 0)
		if err != nil {
			t.Fatalf("BrowsePostings failed: %v", err)
		}
		if len(postings) != 0 {
			t.Errorf("Expected 0 postings for nonexistent tag, got %d", len(postings))
		}
	})

	t.Run("excludes own group posting", func(t *testing.T) {
		// Browse as group A for "safety" — group A has it but should be excluded
		postings, err := repo.BrowsePostings(ctx, "safety", groupA.ID, 20, 0)
		if err != nil {
			t.Fatalf("BrowsePostings failed: %v", err)
		}
		if len(postings) != 0 {
			t.Errorf("Expected 0 postings (own group excluded), got %d", len(postings))
		}
	})

	t.Run("excludes blocked group postings", func(t *testing.T) {
		// Block group B from group A
		err := repo.BlockGroup(ctx, groupA.ID, groupB.ID)
		if err != nil {
			t.Fatalf("BlockGroup failed: %v", err)
		}
		defer func() { _ = repo.UnblockGroup(ctx, groupA.ID, groupB.ID) }()

		postings, err := repo.BrowsePostings(ctx, "mutual-aid", groupA.ID, 20, 0)
		if err != nil {
			t.Fatalf("BrowsePostings failed: %v", err)
		}
		if len(postings) != 0 {
			t.Errorf("Expected 0 postings (blocked group excluded), got %d", len(postings))
		}
	})
}

// =============================================================================
// RemoveMember and BlockAndRemoveMember with Encrypted Keys Tests
// =============================================================================

func TestGroupRepository_RemoveMemberRevokesKeys(t *testing.T) {
	db := testDB(t)
	grpRepo := NewGroupRepository(db)
	secretRepo := NewEncryptedSecretRepository(db)
	sgRepo := NewSignalGroupRepository(db)
	ctx := context.Background()

	user1 := createGroupTestUser(t, db, "rmk_u1")
	user2 := createGroupTestUser(t, db, "rmk_u2")
	user3 := createGroupTestUser(t, db, "rmk_u3")
	regionID := createGroupTestRegion(t, db, "RemoveKeys Region")

	var groupID string
	t.Cleanup(func() {
		if groupID != "" {
			_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE owner_group_id = ?", groupID)
		}
		cleanupGroupTest(t, db, []string{groupID}, []string{user1, user2, user3}, []string{regionID})
	})

	// Create a group with all three users
	req := &models.CreateGroupRequest{
		Name:      "Remove Keys Test Group",
		RegionIDs: []string{regionID},
	}
	group, err := grpRepo.Create(ctx, req, user1)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}
	groupID = group.ID

	if err := grpRepo.AddMember(ctx, groupID, user2, false, false); err != nil {
		t.Fatalf("Add user2 failed: %v", err)
	}
	if err := grpRepo.AddMember(ctx, groupID, user3, false, false); err != nil {
		t.Fatalf("Add user3 failed: %v", err)
	}

	// Create a signal group for this group
	sg := &models.SignalGroup{
		OwnerGroupID: &groupID,
		GroupName:    "Remove Keys Signal Group",
		AccessTier:   models.AccessTierMember,
		CreatedBy:    &user1,
	}
	if err := sgRepo.CreateForOwnerGroup(ctx, sg); err != nil {
		t.Fatalf("Create signal group failed: %v", err)
	}

	// Create an encrypted secret with wrapped keys for all users
	secret := &models.EncryptedSecret{
		SecretType:       models.SecretTypeSignalInvite,
		SignalGroupID:    &sg.ID,
		EncryptedPayload: "rmk_payload",
		EncryptionIV:     "rmk_iv_12345678",
		UpdatedBy:        user1,
	}
	wrappedKeys := []models.WrappedKeyEntry{
		{UserID: user1, WrappedDEK: "dek_rmk_u1"},
		{UserID: user2, WrappedDEK: "dek_rmk_u2"},
		{UserID: user3, WrappedDEK: "dek_rmk_u3"},
	}
	if err := secretRepo.Create(ctx, secret, wrappedKeys); err != nil {
		t.Fatalf("Create secret failed: %v", err)
	}

	// Remove user2
	if err := grpRepo.RemoveMember(ctx, groupID, user2); err != nil {
		t.Fatalf("RemoveMember failed: %v", err)
	}

	// Verify user2 is no longer a member
	isMember, err := grpRepo.IsUserMember(ctx, groupID, user2)
	if err != nil {
		t.Fatalf("IsUserMember check failed: %v", err)
	}
	if isMember {
		t.Error("Expected user2 to not be a member after removal")
	}

	// Verify user2's wrapped key is deleted
	_, err = secretRepo.GetWrappedDEK(ctx, secret.ID, user2)
	if err == nil {
		t.Error("Expected user2's wrapped key to be revoked")
	}

	// Verify survivors' keys still exist
	dek1, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user1)
	if err != nil {
		t.Fatalf("Failed to get user1 DEK: %v", err)
	}
	if dek1 != "dek_rmk_u1" {
		t.Errorf("Expected user1 key unchanged, got %s", dek1)
	}

	dek3, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user3)
	if err != nil {
		t.Fatalf("Failed to get user3 DEK: %v", err)
	}
	if dek3 != "dek_rmk_u3" {
		t.Errorf("Expected user3 key unchanged, got %s", dek3)
	}

	// Verify survivors are flagged for rekey
	for _, uid := range []string{user1, user3} {
		var rekeyNeeded bool
		if err := db.QueryRowContext(ctx, `
			SELECT rekey_needed FROM encrypted_secret_keys
			WHERE secret_id = ? AND user_id = ?
		`, secret.ID, uid).Scan(&rekeyNeeded); err != nil {
			t.Fatalf("Failed to check rekey flag for %s: %v", uid, err)
		}
		if !rekeyNeeded {
			t.Errorf("Expected user %s to have rekey_needed=true", uid)
		}
	}
}

func TestGroupRepository_BlockAndRemoveMemberRevokesKeys(t *testing.T) {
	db := testDB(t)
	grpRepo := NewGroupRepository(db)
	secretRepo := NewEncryptedSecretRepository(db)
	sgRepo := NewSignalGroupRepository(db)
	ctx := context.Background()

	user1 := createGroupTestUser(t, db, "bark_u1")
	user2 := createGroupTestUser(t, db, "bark_u2")
	user3 := createGroupTestUser(t, db, "bark_u3")
	regionID := createGroupTestRegion(t, db, "BlockKeys Region")

	var groupID string
	t.Cleanup(func() {
		if groupID != "" {
			_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE owner_group_id = ?", groupID)
		}
		cleanupGroupTest(t, db, []string{groupID}, []string{user1, user2, user3}, []string{regionID})
	})

	// Create a group with all three users
	req := &models.CreateGroupRequest{
		Name:      "Block Keys Test Group",
		RegionIDs: []string{regionID},
	}
	group, err := grpRepo.Create(ctx, req, user1)
	if err != nil {
		t.Fatalf("Create group failed: %v", err)
	}
	groupID = group.ID

	if err := grpRepo.AddMember(ctx, groupID, user2, false, false); err != nil {
		t.Fatalf("Add user2 failed: %v", err)
	}
	if err := grpRepo.AddMember(ctx, groupID, user3, false, false); err != nil {
		t.Fatalf("Add user3 failed: %v", err)
	}

	// Create a signal group for this group
	sg := &models.SignalGroup{
		OwnerGroupID: &groupID,
		GroupName:    "Block Keys Signal Group",
		AccessTier:   models.AccessTierMember,
		CreatedBy:    &user1,
	}
	if err := sgRepo.CreateForOwnerGroup(ctx, sg); err != nil {
		t.Fatalf("Create signal group failed: %v", err)
	}

	// Create an encrypted secret with wrapped keys for all users
	secret := &models.EncryptedSecret{
		SecretType:       models.SecretTypeSignalInvite,
		SignalGroupID:    &sg.ID,
		EncryptedPayload: "bark_payload",
		EncryptionIV:     "bark_iv_1234567",
		UpdatedBy:        user1,
	}
	wrappedKeys := []models.WrappedKeyEntry{
		{UserID: user1, WrappedDEK: "dek_bark_u1"},
		{UserID: user2, WrappedDEK: "dek_bark_u2"},
		{UserID: user3, WrappedDEK: "dek_bark_u3"},
	}
	if err := secretRepo.Create(ctx, secret, wrappedKeys); err != nil {
		t.Fatalf("Create secret failed: %v", err)
	}

	// Block and remove user2
	if err := grpRepo.BlockAndRemoveMember(ctx, groupID, user2, &user1, nil); err != nil {
		t.Fatalf("BlockAndRemoveMember failed: %v", err)
	}

	// Verify user2 is no longer a member
	isMember, err := grpRepo.IsUserMember(ctx, groupID, user2)
	if err != nil {
		t.Fatalf("IsUserMember check failed: %v", err)
	}
	if isMember {
		t.Error("Expected user2 to not be a member after block")
	}

	// Verify user2 is blocked
	isBlocked, err := grpRepo.IsUserBlockedFromGroup(ctx, groupID, user2)
	if err != nil {
		t.Fatalf("IsUserBlockedFromGroup check failed: %v", err)
	}
	if !isBlocked {
		t.Error("Expected user2 to be blocked")
	}

	// Verify user2's wrapped key is deleted
	_, err = secretRepo.GetWrappedDEK(ctx, secret.ID, user2)
	if err == nil {
		t.Error("Expected user2's wrapped key to be revoked")
	}

	// Verify survivors' keys still exist
	dek1, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user1)
	if err != nil {
		t.Fatalf("Failed to get user1 DEK: %v", err)
	}
	if dek1 != "dek_bark_u1" {
		t.Errorf("Expected user1 key unchanged, got %s", dek1)
	}

	dek3, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user3)
	if err != nil {
		t.Fatalf("Failed to get user3 DEK: %v", err)
	}
	if dek3 != "dek_bark_u3" {
		t.Errorf("Expected user3 key unchanged, got %s", dek3)
	}

	// Verify survivors are flagged for rekey
	for _, uid := range []string{user1, user3} {
		var rekeyNeeded bool
		if err := db.QueryRowContext(ctx, `
			SELECT rekey_needed FROM encrypted_secret_keys
			WHERE secret_id = ? AND user_id = ?
		`, secret.ID, uid).Scan(&rekeyNeeded); err != nil {
			t.Fatalf("Failed to check rekey flag for %s: %v", uid, err)
		}
		if !rekeyNeeded {
			t.Errorf("Expected user %s to have rekey_needed=true", uid)
		}
	}
}
