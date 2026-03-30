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
