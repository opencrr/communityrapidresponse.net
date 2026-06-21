package database

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// TestMeshtasticChannel_GroupOwnedInsert is a regression guard for migration 047.
// The original 3-way XOR CHECK (migration 027) was unnamed; if 047 failed to drop
// it (its auto-generated name is MariaDB-version-dependent), the surviving 3-way
// constraint AND-ed with the new 4-way and rejected every owner_group_id insert.
// This test exercises the insert path directly so a broken constraint fails CI.
func TestMeshtasticChannel_GroupOwnedInsert(t *testing.T) {
	db := testDB(t)
	repo := NewMeshtasticChannelRepository(db)
	ctx := context.Background()

	userID := createConnectionTestUser(t, db, "mesh_grp_owner")
	regionID := createConnectionTestRegion(t, db, "Mesh Group Owner Region")
	groupID := createConnectionTestGroup(t, db, "Mesh Group Owner Group", userID, regionID)
	t.Cleanup(func() {
		cleanupConnectionTest(t, db, []string{groupID}, []string{userID}, []string{regionID}, nil)
	})

	// A group-owned channel (owner_group_id set, all other owners nil) must insert
	// cleanly under the 4-way XOR constraint.
	channel := &models.MeshtasticChannel{
		OwnerGroupID: &groupID,
		ChannelName:  "Group Mesh Channel",
		AccessTier:   models.AccessTierMember,
		CreatedBy:    &userID,
	}
	if err := repo.CreateForOwnerGroup(ctx, channel); err != nil {
		t.Fatalf("group-owned meshtastic insert failed (migration 047 likely left the old 3-way CHECK in place): %v", err)
	}

	got, err := repo.GetByID(ctx, channel.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.OwnerGroupID == nil || *got.OwnerGroupID != groupID {
		t.Errorf("expected owner_group_id=%s, got %v", groupID, got.OwnerGroupID)
	}

	// The XOR must still reject a row with two owners set (region_id + owner_group_id).
	_, err = db.ExecContext(ctx,
		`INSERT INTO meshtastic_channels (id, region_id, school_id, district_id, owner_group_id, channel_name, access_tier, created_by, created_at, is_active)
		 VALUES (?, ?, NULL, NULL, ?, ?, 'member', ?, NOW(), TRUE)`,
		uuid.New().String(), regionID, groupID, "Invalid Two-Owner Channel", userID,
	)
	if err == nil {
		t.Error("expected the 4-way XOR CHECK to reject a channel with both region_id and owner_group_id set, but the insert succeeded")
	}
}
