package database

import (
	"context"
	"testing"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// =============================================================================
// Helper Functions
// =============================================================================

func createMembershipTestUser(t *testing.T, db *DB, userRepo *UserRepository, suffix string, postcard, vouch bool) *models.User {
	user := &models.User{
		Username:         "memtest_" + suffix,
		Email:            "memtest_" + suffix + "@test.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		PostcardVerified: postcard,
		VouchVerified:    vouch,
		EmailVerified:    true,
	}
	// Set verification tier based on verification status
	if postcard {
		user.VerificationTier = models.TierPostcard
	}
	if vouch && !postcard {
		user.VerificationTier = models.TierVouched
	}
	// Note: Admin status is determined by having both PostcardVerified and VouchVerified = true
	// There is no TierAdmin; use TierPostcard for fully verified users

	if err := userRepo.Create(context.Background(), user); err != nil {
		t.Fatalf("Failed to create test user %s: %v", suffix, err)
	}
	return user
}

func createMembershipTestRegion(t *testing.T, db *DB, regionRepo *RegionRepository, name string, parentID *string, regionType models.RegionType) *models.GeographicRegion {
	region := &models.GeographicRegion{
		Name:           name,
		RegionType:     regionType,
		ParentRegionID: parentID,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	if err := regionRepo.Create(context.Background(), region, geoJSON); err != nil {
		t.Fatalf("Failed to create test region %s: %v", name, err)
	}
	return region
}

func cleanupMembershipTest(t *testing.T, db *DB, userIDs, regionIDs []string) {
	ctx := context.Background()

	// Clean up votes and requests first
	for _, id := range regionIDs {
		_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_votes WHERE request_id IN (SELECT id FROM sub_region_membership_requests WHERE region_id = ?)", id)
		_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE region_id = ?", id)
		_, _ = db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", id)
	}

	// Delete regions in reverse order (children first)
	for i := len(regionIDs) - 1; i >= 0; i-- {
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", regionIDs[i])
	}

	// Clean up users
	for _, id := range userIDs {
		_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_votes WHERE voter_id = ?", id)
		_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE user_id = ? OR initiated_by = ?", id, id)
		_, _ = db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", id)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	}
}

// =============================================================================
// Create Tests
// =============================================================================

func TestMembershipRepository_Create(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	user := createMembershipTestUser(t, db, userRepo, "create", true, false)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "Membership Create Parent", nil, models.RegionTypeCity)
	subRegion := createMembershipTestRegion(t, db, regionRepo, "Membership Create Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	defer cleanupMembershipTest(t, db, []string{user.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("creates membership request successfully", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}

		err := membershipRepo.Create(ctx, req)
		if err != nil {
			t.Fatalf("Failed to create membership request: %v", err)
		}

		if req.ID == "" {
			t.Error("Expected ID to be set")
		}
		if req.Status != models.MembershipRequestStatusPending {
			t.Errorf("Expected status 'pending', got %s", req.Status)
		}
		if req.CreatedAt.IsZero() {
			t.Error("Expected CreatedAt to be set")
		}
		if req.ExpiresAt.IsZero() {
			t.Error("Expected ExpiresAt to be set")
		}

		// Verify expiration is set correctly (7 days from creation)
		expectedExpiration := req.CreatedAt.Add(7 * 24 * time.Hour)
		if req.ExpiresAt.Sub(expectedExpiration) > time.Second {
			t.Errorf("Expected expiration %v, got %v", expectedExpiration, req.ExpiresAt)
		}

		// Cleanup
		_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE id = ?", req.ID)
	})

	t.Run("creates invitation request successfully", func(t *testing.T) {
		admin := createMembershipTestUser(t, db, userRepo, "create_admin", true, true)
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", admin.ID)
		}()

		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeInvitation,
			InitiatedBy:    admin.ID,
		}

		err := membershipRepo.Create(ctx, req)
		if err != nil {
			t.Fatalf("Failed to create invitation: %v", err)
		}

		if req.RequestType != models.MembershipRequestTypeInvitation {
			t.Errorf("Expected request type 'invitation', got %s", req.RequestType)
		}

		// Cleanup
		_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE id = ?", req.ID)
	})
}

// =============================================================================
// GetByID Tests
// =============================================================================

func TestMembershipRepository_GetByID(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	user := createMembershipTestUser(t, db, userRepo, "getbyid", true, false)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "Membership GetByID Parent", nil, models.RegionTypeCity)
	subRegion := createMembershipTestRegion(t, db, regionRepo, "Membership GetByID Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	defer cleanupMembershipTest(t, db, []string{user.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("retrieves existing request", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		_ = membershipRepo.Create(ctx, req)
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE id = ?", req.ID)
		}()

		retrieved, err := membershipRepo.GetByID(ctx, req.ID)
		if err != nil {
			t.Fatalf("Failed to get request: %v", err)
		}

		if retrieved.ID != req.ID {
			t.Errorf("Expected ID %s, got %s", req.ID, retrieved.ID)
		}
		if retrieved.UserID != user.ID {
			t.Errorf("Expected UserID %s, got %s", user.ID, retrieved.UserID)
		}
		if retrieved.RegionID != subRegion.ID {
			t.Errorf("Expected RegionID %s, got %s", subRegion.ID, retrieved.RegionID)
		}
		if retrieved.Status != models.MembershipRequestStatusPending {
			t.Errorf("Expected status 'pending', got %s", retrieved.Status)
		}
	})

	t.Run("returns error for non-existent request", func(t *testing.T) {
		_, err := membershipRepo.GetByID(ctx, "non-existent-id")
		if err != ErrMembershipRequestNotFound {
			t.Errorf("Expected ErrMembershipRequestNotFound, got %v", err)
		}
	})
}

// =============================================================================
// GetByIDWithVotes Tests
// =============================================================================

func TestMembershipRepository_GetByIDWithVotes(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	user := createMembershipTestUser(t, db, userRepo, "withvotes_user", true, false)
	admin1 := createMembershipTestUser(t, db, userRepo, "withvotes_admin1", true, true)
	admin2 := createMembershipTestUser(t, db, userRepo, "withvotes_admin2", true, true)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "Membership WithVotes Parent", nil, models.RegionTypeCity)
	subRegion := createMembershipTestRegion(t, db, regionRepo, "Membership WithVotes Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	defer cleanupMembershipTest(t, db, []string{user.ID, admin1.ID, admin2.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("retrieves request with votes", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		_ = membershipRepo.Create(ctx, req)

		// Add votes
		_ = membershipRepo.AddVote(ctx, req.ID, admin1.ID, true)
		_ = membershipRepo.AddVote(ctx, req.ID, admin2.ID, false)

		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_votes WHERE request_id = ?", req.ID)
			_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE id = ?", req.ID)
		}()

		retrieved, err := membershipRepo.GetByIDWithVotes(ctx, req.ID)
		if err != nil {
			t.Fatalf("Failed to get request with votes: %v", err)
		}

		if retrieved.Username != user.Username {
			t.Errorf("Expected username %s, got %s", user.Username, retrieved.Username)
		}
		if retrieved.RegionName != subRegion.Name {
			t.Errorf("Expected region name %s, got %s", subRegion.Name, retrieved.RegionName)
		}
		if len(retrieved.Votes) != 2 {
			t.Errorf("Expected 2 votes, got %d", len(retrieved.Votes))
		}
		if retrieved.CurrentVotes != 1 {
			t.Errorf("Expected 1 approval vote, got %d", retrieved.CurrentVotes)
		}
		if retrieved.VotesNeeded != RequiredVotesForApproval {
			t.Errorf("Expected VotesNeeded %d, got %d", RequiredVotesForApproval, retrieved.VotesNeeded)
		}
	})

	t.Run("returns error for non-existent request", func(t *testing.T) {
		_, err := membershipRepo.GetByIDWithVotes(ctx, "non-existent-id")
		if err != ErrMembershipRequestNotFound {
			t.Errorf("Expected ErrMembershipRequestNotFound, got %v", err)
		}
	})
}

// =============================================================================
// GetPendingByUserAndRegion Tests
// =============================================================================

func TestMembershipRepository_GetPendingByUserAndRegion(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	user := createMembershipTestUser(t, db, userRepo, "pending_user", true, false)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "Membership Pending Parent", nil, models.RegionTypeCity)
	subRegion := createMembershipTestRegion(t, db, regionRepo, "Membership Pending Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	defer cleanupMembershipTest(t, db, []string{user.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("returns pending request if exists", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		_ = membershipRepo.Create(ctx, req)
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE id = ?", req.ID)
		}()

		found, err := membershipRepo.GetPendingByUserAndRegion(ctx, user.ID, subRegion.ID)
		if err != nil {
			t.Fatalf("Failed to get pending request: %v", err)
		}
		if found == nil {
			t.Error("Expected to find pending request")
		}
		if found != nil && found.ID != req.ID {
			t.Errorf("Expected request ID %s, got %s", req.ID, found.ID)
		}
	})

	t.Run("returns nil for approved requests", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		_ = membershipRepo.Create(ctx, req)
		_ = membershipRepo.UpdateStatus(ctx, req.ID, models.MembershipRequestStatusApproved)
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE id = ?", req.ID)
		}()

		found, err := membershipRepo.GetPendingByUserAndRegion(ctx, user.ID, subRegion.ID)
		if err != nil {
			t.Fatalf("Failed to check pending request: %v", err)
		}
		if found != nil {
			t.Error("Expected no pending request for approved status")
		}
	})

	t.Run("returns nil for expired requests", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		_ = membershipRepo.Create(ctx, req)
		// Manually set expires_at to the past
		_, _ = db.ExecContext(ctx, "UPDATE sub_region_membership_requests SET expires_at = ? WHERE id = ?",
			time.Now().Add(-1*time.Hour), req.ID)
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE id = ?", req.ID)
		}()

		found, err := membershipRepo.GetPendingByUserAndRegion(ctx, user.ID, subRegion.ID)
		if err != nil {
			t.Fatalf("Failed to check pending request: %v", err)
		}
		if found != nil {
			t.Error("Expected no pending request for expired request")
		}
	})

	t.Run("returns nil when no pending request exists", func(t *testing.T) {
		found, err := membershipRepo.GetPendingByUserAndRegion(ctx, user.ID, "non-existent-region")
		if err != nil {
			t.Fatalf("Failed to check pending request: %v", err)
		}
		if found != nil {
			t.Error("Expected nil for non-existent request")
		}
	})
}

// =============================================================================
// CountPendingByUser Tests
// =============================================================================

func TestMembershipRepository_CountPendingByUser(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	user := createMembershipTestUser(t, db, userRepo, "count_user", true, false)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "Membership Count Parent", nil, models.RegionTypeCity)
	subRegion1 := createMembershipTestRegion(t, db, regionRepo, "Membership Count Sub1", &parentRegion.ID, models.RegionTypeNeighborhood)
	subRegion2 := createMembershipTestRegion(t, db, regionRepo, "Membership Count Sub2", &parentRegion.ID, models.RegionTypeNeighborhood)
	subRegion3 := createMembershipTestRegion(t, db, regionRepo, "Membership Count Sub3", &parentRegion.ID, models.RegionTypeNeighborhood)

	defer cleanupMembershipTest(t, db, []string{user.ID}, []string{subRegion3.ID, subRegion2.ID, subRegion1.ID, parentRegion.ID})

	t.Run("counts pending requests correctly", func(t *testing.T) {
		// Create 3 pending requests
		for i, regionID := range []string{subRegion1.ID, subRegion2.ID, subRegion3.ID} {
			req := &models.SubRegionMembershipRequest{
				UserID:         user.ID,
				RegionID:       regionID,
				ParentRegionID: parentRegion.ID,
				RequestType:    models.MembershipRequestTypeRequest,
				InitiatedBy:    user.ID,
			}
			_ = membershipRepo.Create(ctx, req)

			// Approve one of them
			if i == 2 {
				_ = membershipRepo.UpdateStatus(ctx, req.ID, models.MembershipRequestStatusApproved)
			}
		}

		count, err := membershipRepo.CountPendingByUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to count pending: %v", err)
		}

		// Should only count pending ones (2 pending, 1 approved)
		if count != 2 {
			t.Errorf("Expected 2 pending requests, got %d", count)
		}
	})

	t.Run("excludes expired requests", func(t *testing.T) {
		// Expire all pending requests
		_, _ = db.ExecContext(ctx, "UPDATE sub_region_membership_requests SET expires_at = ? WHERE user_id = ? AND status = 'pending'",
			time.Now().Add(-1*time.Hour), user.ID)

		count, err := membershipRepo.CountPendingByUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to count pending: %v", err)
		}

		if count != 0 {
			t.Errorf("Expected 0 pending requests after expiration, got %d", count)
		}
	})
}

// =============================================================================
// ListPendingByRegion Tests
// =============================================================================

func TestMembershipRepository_ListPendingByRegion(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	user1 := createMembershipTestUser(t, db, userRepo, "list_user1", true, false)
	user2 := createMembershipTestUser(t, db, userRepo, "list_user2", true, false)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "Membership List Parent", nil, models.RegionTypeCity)
	subRegion := createMembershipTestRegion(t, db, regionRepo, "Membership List Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	defer cleanupMembershipTest(t, db, []string{user1.ID, user2.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("lists pending requests for region", func(t *testing.T) {
		req1 := &models.SubRegionMembershipRequest{
			UserID:         user1.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user1.ID,
		}
		_ = membershipRepo.Create(ctx, req1)

		req2 := &models.SubRegionMembershipRequest{
			UserID:         user2.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user2.ID,
		}
		_ = membershipRepo.Create(ctx, req2)

		requests, err := membershipRepo.ListPendingByRegion(ctx, subRegion.ID)
		if err != nil {
			t.Fatalf("Failed to list pending: %v", err)
		}

		if len(requests) != 2 {
			t.Errorf("Expected 2 requests, got %d", len(requests))
		}

		// Verify fields are populated
		for _, r := range requests {
			if r.Username == "" {
				t.Error("Expected username to be populated")
			}
			if r.RegionName == "" {
				t.Error("Expected region name to be populated")
			}
			if r.VotesNeeded != RequiredVotesForApproval {
				t.Errorf("Expected VotesNeeded %d, got %d", RequiredVotesForApproval, r.VotesNeeded)
			}
		}
	})

	t.Run("excludes non-pending requests", func(t *testing.T) {
		// Approve one request
		requests, _ := membershipRepo.ListPendingByRegion(ctx, subRegion.ID)
		if len(requests) > 0 {
			_ = membershipRepo.UpdateStatus(ctx, requests[0].ID, models.MembershipRequestStatusApproved)
		}

		updatedRequests, err := membershipRepo.ListPendingByRegion(ctx, subRegion.ID)
		if err != nil {
			t.Fatalf("Failed to list pending: %v", err)
		}

		if len(updatedRequests) != 1 {
			t.Errorf("Expected 1 pending request after approval, got %d", len(updatedRequests))
		}
	})

	t.Run("returns empty list for region with no pending requests", func(t *testing.T) {
		requests, err := membershipRepo.ListPendingByRegion(ctx, "non-existent-region")
		if err != nil {
			t.Fatalf("Failed to list pending: %v", err)
		}

		if len(requests) != 0 {
			t.Errorf("Expected 0 requests, got %d", len(requests))
		}
	})
}

// =============================================================================
// ListPendingForAdminUser Tests
// =============================================================================

func TestMembershipRepository_ListPendingForAdminUser(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	admin := createMembershipTestUser(t, db, userRepo, "admin_list", true, true)
	user := createMembershipTestUser(t, db, userRepo, "user_list", true, false)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "Admin List Parent", nil, models.RegionTypeCity)
	subRegion := createMembershipTestRegion(t, db, regionRepo, "Admin List Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	// Make admin an admin of the sub-region
	_ = regionRepo.AddUserToRegion(ctx, admin.ID, subRegion.ID, true)

	defer cleanupMembershipTest(t, db, []string{admin.ID, user.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("lists pending requests for admin's regions", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		_ = membershipRepo.Create(ctx, req)

		requests, err := membershipRepo.ListPendingForAdminUser(ctx, admin.ID)
		if err != nil {
			t.Fatalf("Failed to list pending for admin: %v", err)
		}

		if len(requests) != 1 {
			t.Errorf("Expected 1 request, got %d", len(requests))
		}
	})

	t.Run("returns empty list for non-admin user", func(t *testing.T) {
		requests, err := membershipRepo.ListPendingForAdminUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to list pending for non-admin: %v", err)
		}

		if len(requests) != 0 {
			t.Errorf("Expected 0 requests for non-admin, got %d", len(requests))
		}
	})
}

// =============================================================================
// ListUserRequests Tests
// =============================================================================

func TestMembershipRepository_ListUserRequests(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	user := createMembershipTestUser(t, db, userRepo, "userrequests", true, false)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "User Requests Parent", nil, models.RegionTypeCity)
	subRegion := createMembershipTestRegion(t, db, regionRepo, "User Requests Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	defer cleanupMembershipTest(t, db, []string{user.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("lists all requests for user without filters", func(t *testing.T) {
		// Create request
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		_ = membershipRepo.Create(ctx, req)

		filter := models.MembershipRequestListFilter{}
		requests, err := membershipRepo.ListUserRequests(ctx, user.ID, filter)
		if err != nil {
			t.Fatalf("Failed to list user requests: %v", err)
		}

		if len(requests) != 1 {
			t.Errorf("Expected 1 request, got %d", len(requests))
		}
	})

	t.Run("filters by status", func(t *testing.T) {
		// Get existing pending request
		filter := models.MembershipRequestListFilter{Status: "pending"}
		pendingRequests, _ := membershipRepo.ListUserRequests(ctx, user.ID, filter)
		if len(pendingRequests) > 0 {
			// Approve it
			_ = membershipRepo.UpdateStatus(ctx, pendingRequests[0].ID, models.MembershipRequestStatusApproved)
		}

		// Query for pending should return 0
		pendingFilter := models.MembershipRequestListFilter{Status: "pending"}
		pending, err := membershipRepo.ListUserRequests(ctx, user.ID, pendingFilter)
		if err != nil {
			t.Fatalf("Failed to list user requests: %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("Expected 0 pending requests, got %d", len(pending))
		}

		// Query for approved should return 1
		approvedFilter := models.MembershipRequestListFilter{Status: "approved"}
		approved, err := membershipRepo.ListUserRequests(ctx, user.ID, approvedFilter)
		if err != nil {
			t.Fatalf("Failed to list user requests: %v", err)
		}
		if len(approved) != 1 {
			t.Errorf("Expected 1 approved request, got %d", len(approved))
		}
	})

	t.Run("filters by request type", func(t *testing.T) {
		admin := createMembershipTestUser(t, db, userRepo, "userrequests_admin", true, true)
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE initiated_by = ?", admin.ID)
			_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", admin.ID)
		}()

		// Create an invitation
		invitation := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeInvitation,
			InitiatedBy:    admin.ID,
		}
		// Delete existing requests to avoid duplicates
		_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE user_id = ? AND region_id = ?", user.ID, subRegion.ID)
		_ = membershipRepo.Create(ctx, invitation)

		// Filter for invitations
		invitationFilter := models.MembershipRequestListFilter{RequestType: "invitation"}
		invitations, err := membershipRepo.ListUserRequests(ctx, user.ID, invitationFilter)
		if err != nil {
			t.Fatalf("Failed to list user requests: %v", err)
		}
		if len(invitations) != 1 {
			t.Errorf("Expected 1 invitation, got %d", len(invitations))
		}
	})
}

// =============================================================================
// ListInvitationsForUser Tests
// =============================================================================

func TestMembershipRepository_ListInvitationsForUser(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	user := createMembershipTestUser(t, db, userRepo, "invitations", true, false)
	admin := createMembershipTestUser(t, db, userRepo, "invitations_admin", true, true)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "Invitations Parent", nil, models.RegionTypeCity)
	subRegion := createMembershipTestRegion(t, db, regionRepo, "Invitations Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	defer cleanupMembershipTest(t, db, []string{user.ID, admin.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("lists pending invitations for user", func(t *testing.T) {
		invitation := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeInvitation,
			InitiatedBy:    admin.ID,
		}
		_ = membershipRepo.Create(ctx, invitation)

		invitations, err := membershipRepo.ListInvitationsForUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to list invitations: %v", err)
		}

		if len(invitations) != 1 {
			t.Errorf("Expected 1 invitation, got %d", len(invitations))
		}
		if invitations[0].RequestType != "invitation" {
			t.Errorf("Expected request type 'invitation', got %s", invitations[0].RequestType)
		}
	})

	t.Run("excludes regular requests", func(t *testing.T) {
		// Create a regular request (not invitation)
		request := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest, // Not invitation
			InitiatedBy:    user.ID,
		}
		// Delete existing to avoid duplicates
		_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE user_id = ? AND region_id = ?", user.ID, subRegion.ID)
		_ = membershipRepo.Create(ctx, request)

		invitations, err := membershipRepo.ListInvitationsForUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to list invitations: %v", err)
		}

		if len(invitations) != 0 {
			t.Errorf("Expected 0 invitations (only requests exist), got %d", len(invitations))
		}
	})
}

// =============================================================================
// Vote Tests
// =============================================================================

func TestMembershipRepository_AddVote(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	user := createMembershipTestUser(t, db, userRepo, "addvote_user", true, false)
	admin := createMembershipTestUser(t, db, userRepo, "addvote_admin", true, true)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "AddVote Parent", nil, models.RegionTypeCity)
	subRegion := createMembershipTestRegion(t, db, regionRepo, "AddVote Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	defer cleanupMembershipTest(t, db, []string{user.ID, admin.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("adds vote successfully", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		_ = membershipRepo.Create(ctx, req)

		err := membershipRepo.AddVote(ctx, req.ID, admin.ID, true)
		if err != nil {
			t.Fatalf("Failed to add vote: %v", err)
		}

		// Verify vote was recorded
		count, _ := membershipRepo.CountApprovalVotes(ctx, req.ID)
		if count != 1 {
			t.Errorf("Expected 1 approval vote, got %d", count)
		}
	})

	t.Run("adds rejection vote", func(t *testing.T) {
		admin2 := createMembershipTestUser(t, db, userRepo, "addvote_admin2", true, true)
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", admin2.ID)
		}()

		// Get existing request
		requests, _ := membershipRepo.ListPendingByRegion(ctx, subRegion.ID)
		if len(requests) == 0 {
			t.Skip("No pending requests to vote on")
		}

		err := membershipRepo.AddVote(ctx, requests[0].ID, admin2.ID, false)
		if err != nil {
			t.Fatalf("Failed to add rejection vote: %v", err)
		}

		// Approval count should still be 1
		count, _ := membershipRepo.CountApprovalVotes(ctx, requests[0].ID)
		if count != 1 {
			t.Errorf("Expected 1 approval vote, got %d", count)
		}
	})
}

func TestMembershipRepository_HasVoted(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	user := createMembershipTestUser(t, db, userRepo, "hasvoted_user", true, false)
	admin := createMembershipTestUser(t, db, userRepo, "hasvoted_admin", true, true)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "HasVoted Parent", nil, models.RegionTypeCity)
	subRegion := createMembershipTestRegion(t, db, regionRepo, "HasVoted Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	defer cleanupMembershipTest(t, db, []string{user.ID, admin.ID}, []string{subRegion.ID, parentRegion.ID})

	req := &models.SubRegionMembershipRequest{
		UserID:         user.ID,
		RegionID:       subRegion.ID,
		ParentRegionID: parentRegion.ID,
		RequestType:    models.MembershipRequestTypeRequest,
		InitiatedBy:    user.ID,
	}
	_ = membershipRepo.Create(ctx, req)

	t.Run("returns false before voting", func(t *testing.T) {
		hasVoted, err := membershipRepo.HasVoted(ctx, req.ID, admin.ID)
		if err != nil {
			t.Fatalf("Failed to check vote: %v", err)
		}
		if hasVoted {
			t.Error("Expected HasVoted to return false")
		}
	})

	t.Run("returns true after voting", func(t *testing.T) {
		_ = membershipRepo.AddVote(ctx, req.ID, admin.ID, true)

		hasVoted, err := membershipRepo.HasVoted(ctx, req.ID, admin.ID)
		if err != nil {
			t.Fatalf("Failed to check vote: %v", err)
		}
		if !hasVoted {
			t.Error("Expected HasVoted to return true")
		}
	})
}

func TestMembershipRepository_CountApprovalVotes(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	user := createMembershipTestUser(t, db, userRepo, "count_votes_user", true, false)
	admin1 := createMembershipTestUser(t, db, userRepo, "count_votes_admin1", true, true)
	admin2 := createMembershipTestUser(t, db, userRepo, "count_votes_admin2", true, true)
	admin3 := createMembershipTestUser(t, db, userRepo, "count_votes_admin3", true, true)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "Count Votes Parent", nil, models.RegionTypeCity)
	subRegion := createMembershipTestRegion(t, db, regionRepo, "Count Votes Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	defer cleanupMembershipTest(t, db, []string{user.ID, admin1.ID, admin2.ID, admin3.ID}, []string{subRegion.ID, parentRegion.ID})

	req := &models.SubRegionMembershipRequest{
		UserID:         user.ID,
		RegionID:       subRegion.ID,
		ParentRegionID: parentRegion.ID,
		RequestType:    models.MembershipRequestTypeRequest,
		InitiatedBy:    user.ID,
	}
	_ = membershipRepo.Create(ctx, req)

	t.Run("counts only approval votes", func(t *testing.T) {
		// Add mixed votes
		_ = membershipRepo.AddVote(ctx, req.ID, admin1.ID, true)  // Approve
		_ = membershipRepo.AddVote(ctx, req.ID, admin2.ID, false) // Reject
		_ = membershipRepo.AddVote(ctx, req.ID, admin3.ID, true)  // Approve

		count, err := membershipRepo.CountApprovalVotes(ctx, req.ID)
		if err != nil {
			t.Fatalf("Failed to count votes: %v", err)
		}

		// Should count only approvals (2)
		if count != 2 {
			t.Errorf("Expected 2 approval votes, got %d", count)
		}
	})
}

// =============================================================================
// UpdateStatus Tests
// =============================================================================

func TestMembershipRepository_UpdateStatus(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	user := createMembershipTestUser(t, db, userRepo, "update_status", true, false)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "Update Status Parent", nil, models.RegionTypeCity)
	subRegion := createMembershipTestRegion(t, db, regionRepo, "Update Status Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	defer cleanupMembershipTest(t, db, []string{user.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("updates status to approved", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		_ = membershipRepo.Create(ctx, req)

		err := membershipRepo.UpdateStatus(ctx, req.ID, models.MembershipRequestStatusApproved)
		if err != nil {
			t.Fatalf("Failed to update status: %v", err)
		}

		retrieved, _ := membershipRepo.GetByID(ctx, req.ID)
		if retrieved.Status != models.MembershipRequestStatusApproved {
			t.Errorf("Expected status 'approved', got %s", retrieved.Status)
		}
		if retrieved.ResolvedAt == nil {
			t.Error("Expected ResolvedAt to be set")
		}
	})

	t.Run("updates status to rejected", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		// Delete existing to avoid duplicate
		_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE user_id = ? AND region_id = ?", user.ID, subRegion.ID)
		_ = membershipRepo.Create(ctx, req)

		err := membershipRepo.UpdateStatus(ctx, req.ID, models.MembershipRequestStatusRejected)
		if err != nil {
			t.Fatalf("Failed to update status: %v", err)
		}

		retrieved, _ := membershipRepo.GetByID(ctx, req.ID)
		if retrieved.Status != models.MembershipRequestStatusRejected {
			t.Errorf("Expected status 'rejected', got %s", retrieved.Status)
		}
	})

	t.Run("returns error for non-existent request", func(t *testing.T) {
		err := membershipRepo.UpdateStatus(ctx, "non-existent-id", models.MembershipRequestStatusApproved)
		if err != ErrMembershipRequestNotFound {
			t.Errorf("Expected ErrMembershipRequestNotFound, got %v", err)
		}
	})
}

// =============================================================================
// Cancel Tests
// =============================================================================

func TestMembershipRepository_Cancel(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	user := createMembershipTestUser(t, db, userRepo, "cancel_user", true, false)
	otherUser := createMembershipTestUser(t, db, userRepo, "cancel_other", true, false)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "Cancel Parent", nil, models.RegionTypeCity)
	subRegion := createMembershipTestRegion(t, db, regionRepo, "Cancel Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	defer cleanupMembershipTest(t, db, []string{user.ID, otherUser.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("cancels pending request by owner", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		_ = membershipRepo.Create(ctx, req)

		err := membershipRepo.Cancel(ctx, req.ID, user.ID)
		if err != nil {
			t.Fatalf("Failed to cancel request: %v", err)
		}

		retrieved, _ := membershipRepo.GetByID(ctx, req.ID)
		if retrieved.Status != models.MembershipRequestStatusCancelled {
			t.Errorf("Expected status 'cancelled', got %s", retrieved.Status)
		}
	})

	t.Run("fails when cancelled by non-owner", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		// Delete existing to avoid duplicate
		_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE user_id = ? AND region_id = ?", user.ID, subRegion.ID)
		_ = membershipRepo.Create(ctx, req)

		err := membershipRepo.Cancel(ctx, req.ID, otherUser.ID)
		if err != ErrMembershipRequestNotFound {
			t.Errorf("Expected ErrMembershipRequestNotFound for non-owner, got %v", err)
		}
	})

	t.Run("fails when already approved", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		// Delete existing to avoid duplicate
		_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE user_id = ? AND region_id = ?", user.ID, subRegion.ID)
		_ = membershipRepo.Create(ctx, req)
		_ = membershipRepo.UpdateStatus(ctx, req.ID, models.MembershipRequestStatusApproved)

		err := membershipRepo.Cancel(ctx, req.ID, user.ID)
		if err != ErrMembershipRequestNotFound {
			t.Errorf("Expected ErrMembershipRequestNotFound for approved request, got %v", err)
		}
	})
}

// =============================================================================
// Expiration Tests
// =============================================================================

func TestMembershipRepository_ExpirePendingRequests(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	membershipRepo := NewMembershipRepository(db)
	ctx := context.Background()

	// Setup
	user := createMembershipTestUser(t, db, userRepo, "expire_user", true, false)
	parentRegion := createMembershipTestRegion(t, db, regionRepo, "Expire Parent", nil, models.RegionTypeCity)
	subRegion := createMembershipTestRegion(t, db, regionRepo, "Expire Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	defer cleanupMembershipTest(t, db, []string{user.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("expires requests past expiration date", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		_ = membershipRepo.Create(ctx, req)

		// Manually set expires_at to the past
		_, _ = db.ExecContext(ctx, "UPDATE sub_region_membership_requests SET expires_at = ? WHERE id = ?",
			time.Now().Add(-1*time.Hour), req.ID)

		expired, err := membershipRepo.ExpirePendingRequests(ctx)
		if err != nil {
			t.Fatalf("Failed to expire requests: %v", err)
		}

		if expired != 1 {
			t.Errorf("Expected 1 expired request, got %d", expired)
		}

		// Verify status is now expired
		retrieved, _ := membershipRepo.GetByID(ctx, req.ID)
		if retrieved.Status != models.MembershipRequestStatusExpired {
			t.Errorf("Expected status 'expired', got %s", retrieved.Status)
		}
	})

	t.Run("does not expire non-pending requests", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		// Delete existing to avoid duplicate
		_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE user_id = ? AND region_id = ?", user.ID, subRegion.ID)
		_ = membershipRepo.Create(ctx, req)
		_ = membershipRepo.UpdateStatus(ctx, req.ID, models.MembershipRequestStatusApproved)

		// Manually set expires_at to the past
		_, _ = db.ExecContext(ctx, "UPDATE sub_region_membership_requests SET expires_at = ? WHERE id = ?",
			time.Now().Add(-1*time.Hour), req.ID)

		expired, err := membershipRepo.ExpirePendingRequests(ctx)
		if err != nil {
			t.Fatalf("Failed to expire requests: %v", err)
		}

		if expired != 0 {
			t.Errorf("Expected 0 expired requests (already approved), got %d", expired)
		}
	})

	t.Run("does not expire future requests", func(t *testing.T) {
		req := &models.SubRegionMembershipRequest{
			UserID:         user.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    user.ID,
		}
		// Delete existing to avoid duplicate
		_, _ = db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE user_id = ? AND region_id = ?", user.ID, subRegion.ID)
		_ = membershipRepo.Create(ctx, req)

		// Request should have expiration 7 days in the future
		expired, err := membershipRepo.ExpirePendingRequests(ctx)
		if err != nil {
			t.Fatalf("Failed to expire requests: %v", err)
		}

		if expired != 0 {
			t.Errorf("Expected 0 expired requests (not yet expired), got %d", expired)
		}

		// Verify status is still pending
		retrieved, _ := membershipRepo.GetByID(ctx, req.ID)
		if retrieved.Status != models.MembershipRequestStatusPending {
			t.Errorf("Expected status 'pending', got %s", retrieved.Status)
		}
	})
}
