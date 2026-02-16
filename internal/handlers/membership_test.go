package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

type membershipTestSuite struct {
	t              *testing.T
	db             *database.DB
	userRepo       *database.UserRepository
	regionRepo     *database.RegionRepository
	membershipRepo *database.MembershipRepository
	handler        *MembershipHandler
}

func setupMembershipTestSuite(t *testing.T) *membershipTestSuite {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	regionRepo := database.NewRegionRepository(db)
	membershipRepo := database.NewMembershipRepository(db)
	handler := NewMembershipHandler(nil, membershipRepo, regionRepo, userRepo, nil)

	return &membershipTestSuite{
		t:              t,
		db:             db,
		userRepo:       userRepo,
		regionRepo:     regionRepo,
		membershipRepo: membershipRepo,
		handler:        handler,
	}
}

func (s *membershipTestSuite) createTestUser(username string, tier models.VerificationTier, postcard, vouch bool) *models.User {
	user := &models.User{
		Username:         username,
		Email:            username + "@membershiptest.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: tier,
		PostcardVerified: postcard,
		VouchVerified:    vouch,
		EmailVerified:    true,
	}
	if err := s.userRepo.Create(context.Background(), user); err != nil {
		s.t.Fatalf("Failed to create test user: %v", err)
	}
	return user
}

func (s *membershipTestSuite) createTestRegion(name string, parentID *string, regionType models.RegionType) *models.GeographicRegion {
	region := &models.GeographicRegion{
		Name:           name,
		RegionType:     regionType,
		ParentRegionID: parentID,
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	if err := s.regionRepo.Create(context.Background(), region, geoJSON); err != nil {
		s.t.Fatalf("Failed to create test region: %v", err)
	}
	return region
}

func (s *membershipTestSuite) addUserToRegion(userID, regionID string, isAdmin bool) {
	if err := s.regionRepo.AddUserToRegion(context.Background(), userID, regionID, isAdmin); err != nil {
		s.t.Fatalf("Failed to add user to region: %v", err)
	}
}

func (s *membershipTestSuite) cleanup(userIDs []string, regionIDs []string) {
	ctx := context.Background()
	for _, id := range regionIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM sub_region_membership_votes WHERE request_id IN (SELECT id FROM sub_region_membership_requests WHERE region_id = ?)", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE region_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", id)
	}
	// Delete regions in reverse order (children first)
	for i := len(regionIDs) - 1; i >= 0; i-- {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", regionIDs[i])
	}
	for _, id := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM sub_region_membership_votes WHERE voter_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM sub_region_membership_requests WHERE user_id = ? OR initiated_by = ?", id, id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	}
}

// =============================================================================
// CreateRequest Tests
// =============================================================================

func TestMembershipHandler_CreateRequest(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	verifiedUser := suite.createTestUser("mem_verified", models.TierPostcard, true, false)
	unverifiedUser := suite.createTestUser("mem_unverified", models.TierUnverified, false, false)

	// Create parent region and sub-region
	parentRegion := suite.createTestRegion("Membership Test City", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("Membership Test Neighborhood", &parentRegion.ID, models.RegionTypeNeighborhood)

	// Add verified user to parent region
	suite.addUserToRegion(verifiedUser.ID, parentRegion.ID, false)

	defer suite.cleanup([]string{verifiedUser.ID, unverifiedUser.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("verified user can request sub-region membership", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/communities/"+subRegion.ID+"/membership-requests", nil)
		req.Header.Set("Content-Type", "application/json")

		// Set region ID in query params (as the router does)
		q := req.URL.Query()
		q.Set("id", subRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: verifiedUser.VerificationTier,
			PostcardVerified: verifiedUser.PostcardVerified,
			VouchVerified:    verifiedUser.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateRequest(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["request_id"] == nil {
			t.Error("Expected request_id in response")
		}
		if respBody["status"] != "pending" {
			t.Errorf("Expected status 'pending', got %v", respBody["status"])
		}
	})

	t.Run("unverified user cannot request membership", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/communities/"+subRegion.ID+"/membership-requests", nil)
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", subRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           unverifiedUser.ID,
			Email:            unverifiedUser.Email,
			VerificationTier: unverifiedUser.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateRequest(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("cannot request membership in non-sub-region", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/communities/"+parentRegion.ID+"/membership-requests", nil)
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", parentRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: verifiedUser.VerificationTier,
			PostcardVerified: verifiedUser.PostcardVerified,
			VouchVerified:    verifiedUser.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateRequest(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("user not in parent region cannot request", func(t *testing.T) {
		// Create another user not in parent region
		outsiderUser := suite.createTestUser("mem_outsider", models.TierPostcard, true, false)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", outsiderUser.ID)
		}()

		req := httptest.NewRequest("POST", "/api/v1/communities/"+subRegion.ID+"/membership-requests", nil)
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", subRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           outsiderUser.ID,
			Email:            outsiderUser.Email,
			VerificationTier: outsiderUser.VerificationTier,
			PostcardVerified: outsiderUser.PostcardVerified,
			VouchVerified:    outsiderUser.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateRequest(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// =============================================================================
// VoteOnRequest Tests
// =============================================================================

func TestMembershipHandler_VoteOnRequest(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	requester := suite.createTestUser("mem_requester", models.TierPostcard, true, false)
	admin1 := suite.createTestUser("mem_admin1", models.TierPostcard, true, true)
	admin2 := suite.createTestUser("mem_admin2", models.TierPostcard, true, true)
	nonAdmin := suite.createTestUser("mem_nonadmin", models.TierPostcard, true, false)

	// Create parent region and sub-region
	parentRegion := suite.createTestRegion("Vote Test City", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("Vote Test Neighborhood", &parentRegion.ID, models.RegionTypeNeighborhood)

	// Add users to regions
	suite.addUserToRegion(requester.ID, parentRegion.ID, false)
	suite.addUserToRegion(admin1.ID, subRegion.ID, true)
	suite.addUserToRegion(admin2.ID, subRegion.ID, true)
	suite.addUserToRegion(nonAdmin.ID, parentRegion.ID, false)

	defer suite.cleanup([]string{requester.ID, admin1.ID, admin2.ID, nonAdmin.ID}, []string{subRegion.ID, parentRegion.ID})

	// Create a membership request
	membershipRequest := &models.SubRegionMembershipRequest{
		UserID:         requester.ID,
		RegionID:       subRegion.ID,
		ParentRegionID: parentRegion.ID,
		RequestType:    models.MembershipRequestTypeRequest,
		InitiatedBy:    requester.ID,
	}
	if err := suite.membershipRepo.Create(context.Background(), membershipRequest); err != nil {
		t.Fatalf("Failed to create test membership request: %v", err)
	}

	t.Run("admin can vote on request", func(t *testing.T) {
		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/membership-requests/"+membershipRequest.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", membershipRequest.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
			PostcardVerified: admin1.PostcardVerified,
			VouchVerified:    admin1.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["current_votes"].(float64) != 1 {
			t.Errorf("Expected current_votes 1, got %v", respBody["current_votes"])
		}
	})

	t.Run("admin cannot vote twice", func(t *testing.T) {
		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/membership-requests/"+membershipRequest.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", membershipRequest.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
			PostcardVerified: admin1.PostcardVerified,
			VouchVerified:    admin1.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("non-admin cannot vote", func(t *testing.T) {
		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/membership-requests/"+membershipRequest.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", membershipRequest.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           nonAdmin.ID,
			Email:            nonAdmin.Email,
			VerificationTier: nonAdmin.VerificationTier,
			PostcardVerified: nonAdmin.PostcardVerified,
			VouchVerified:    nonAdmin.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("second vote approves membership", func(t *testing.T) {
		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/membership-requests/"+membershipRequest.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", membershipRequest.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin2.ID,
			Email:            admin2.Email,
			VerificationTier: admin2.VerificationTier,
			PostcardVerified: admin2.PostcardVerified,
			VouchVerified:    admin2.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["status"] != "approved" {
			t.Errorf("Expected status 'approved', got %v", respBody["status"])
		}
		if respBody["membership_granted"] != true {
			t.Errorf("Expected membership_granted true, got %v", respBody["membership_granted"])
		}
	})
}

// =============================================================================
// ListUserRequests Tests
// =============================================================================

func TestMembershipHandler_ListUserRequests(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test user
	user := suite.createTestUser("mem_lister", models.TierPostcard, true, false)
	parentRegion := suite.createTestRegion("List Test City", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("List Test Neighborhood", &parentRegion.ID, models.RegionTypeNeighborhood)

	suite.addUserToRegion(user.ID, parentRegion.ID, false)

	defer suite.cleanup([]string{user.ID}, []string{subRegion.ID, parentRegion.ID})

	// Create a membership request
	membershipRequest := &models.SubRegionMembershipRequest{
		UserID:         user.ID,
		RegionID:       subRegion.ID,
		ParentRegionID: parentRegion.ID,
		RequestType:    models.MembershipRequestTypeRequest,
		InitiatedBy:    user.ID,
	}
	if err := suite.membershipRepo.Create(context.Background(), membershipRequest); err != nil {
		t.Fatalf("Failed to create test membership request: %v", err)
	}

	t.Run("user can list their requests", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/membership-requests", nil)

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: user.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListUserRequests(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.MembershipRequestListResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if len(respBody.Requests) != 1 {
			t.Errorf("Expected 1 request, got %d", len(respBody.Requests))
		}
	})
}

// =============================================================================
// CancelRequest Tests
// =============================================================================

func TestMembershipHandler_CancelRequest(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	owner := suite.createTestUser("mem_owner", models.TierPostcard, true, false)
	other := suite.createTestUser("mem_other", models.TierPostcard, true, false)
	parentRegion := suite.createTestRegion("Cancel Test City", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("Cancel Test Neighborhood", &parentRegion.ID, models.RegionTypeNeighborhood)

	suite.addUserToRegion(owner.ID, parentRegion.ID, false)
	suite.addUserToRegion(other.ID, parentRegion.ID, false)

	defer suite.cleanup([]string{owner.ID, other.ID}, []string{subRegion.ID, parentRegion.ID})

	// Create a membership request
	membershipRequest := &models.SubRegionMembershipRequest{
		UserID:         owner.ID,
		RegionID:       subRegion.ID,
		ParentRegionID: parentRegion.ID,
		RequestType:    models.MembershipRequestTypeRequest,
		InitiatedBy:    owner.ID,
	}
	if err := suite.membershipRepo.Create(context.Background(), membershipRequest); err != nil {
		t.Fatalf("Failed to create test membership request: %v", err)
	}

	t.Run("owner can cancel their request", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v1/membership-requests/"+membershipRequest.ID, nil)

		q := req.URL.Query()
		q.Set("id", membershipRequest.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           owner.ID,
			Email:            owner.Email,
			VerificationTier: owner.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CancelRequest(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("other user cannot cancel someone else's request", func(t *testing.T) {
		// Create a new request to test
		newRequest := &models.SubRegionMembershipRequest{
			UserID:         owner.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    owner.ID,
		}
		// Use raw SQL to bypass the duplicate check
		_, _ = suite.db.ExecContext(context.Background(),
			"DELETE FROM sub_region_membership_requests WHERE user_id = ? AND region_id = ?",
			owner.ID, subRegion.ID)
		if err := suite.membershipRepo.Create(context.Background(), newRequest); err != nil {
			t.Fatalf("Failed to create test membership request: %v", err)
		}

		req := httptest.NewRequest("DELETE", "/api/v1/membership-requests/"+newRequest.ID, nil)

		q := req.URL.Query()
		q.Set("id", newRequest.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           other.ID,
			Email:            other.Email,
			VerificationTier: other.VerificationTier,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CancelRequest(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// =============================================================================
// CreateInvitation Tests
// =============================================================================

func TestMembershipHandler_CreateInvitation(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	admin := suite.createTestUser("mem_inviter", models.TierPostcard, true, true)
	invitee := suite.createTestUser("mem_invitee", models.TierPostcard, true, false)
	nonAdmin := suite.createTestUser("mem_nonadmin_inv", models.TierPostcard, true, false)
	parentRegion := suite.createTestRegion("Invite Test City", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("Invite Test Neighborhood", &parentRegion.ID, models.RegionTypeNeighborhood)

	suite.addUserToRegion(admin.ID, subRegion.ID, true)
	suite.addUserToRegion(invitee.ID, parentRegion.ID, false)
	suite.addUserToRegion(nonAdmin.ID, parentRegion.ID, false)

	defer suite.cleanup([]string{admin.ID, invitee.ID, nonAdmin.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("admin can invite user", func(t *testing.T) {
		body := map[string]string{"user_id": invitee.ID}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/communities/"+subRegion.ID+"/invitations", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", subRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin.ID,
			Email:            admin.Email,
			VerificationTier: admin.VerificationTier,
			PostcardVerified: admin.PostcardVerified,
			VouchVerified:    admin.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateInvitation(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["request_type"] != "invitation" {
			t.Errorf("Expected request_type 'invitation', got %v", respBody["request_type"])
		}
	})

	t.Run("non-admin cannot invite user", func(t *testing.T) {
		body := map[string]string{"user_id": invitee.ID}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/communities/"+subRegion.ID+"/invitations", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", subRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           nonAdmin.ID,
			Email:            nonAdmin.Email,
			VerificationTier: nonAdmin.VerificationTier,
			PostcardVerified: nonAdmin.PostcardVerified,
			VouchVerified:    nonAdmin.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateInvitation(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// =============================================================================
// RespondToInvitation Tests
// =============================================================================

func TestMembershipHandler_RespondToInvitation(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	admin := suite.createTestUser("mem_resp_admin", models.TierPostcard, true, true)
	invitee := suite.createTestUser("mem_resp_invitee", models.TierPostcard, true, false)
	parentRegion := suite.createTestRegion("Respond Test City", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("Respond Test Neighborhood", &parentRegion.ID, models.RegionTypeNeighborhood)

	suite.addUserToRegion(admin.ID, subRegion.ID, true)
	suite.addUserToRegion(invitee.ID, parentRegion.ID, false)

	defer suite.cleanup([]string{admin.ID, invitee.ID}, []string{subRegion.ID, parentRegion.ID})

	// Create an invitation
	invitation := &models.SubRegionMembershipRequest{
		UserID:         invitee.ID,
		RegionID:       subRegion.ID,
		ParentRegionID: parentRegion.ID,
		RequestType:    models.MembershipRequestTypeInvitation,
		InitiatedBy:    admin.ID,
	}
	if err := suite.membershipRepo.Create(context.Background(), invitation); err != nil {
		t.Fatalf("Failed to create test invitation: %v", err)
	}

	t.Run("invitee can accept invitation", func(t *testing.T) {
		body := map[string]bool{"accept": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/invitations/"+invitation.ID+"/respond", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", invitation.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           invitee.ID,
			Email:            invitee.Email,
			VerificationTier: invitee.VerificationTier,
			PostcardVerified: invitee.PostcardVerified,
			VouchVerified:    invitee.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RespondToInvitation(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["membership_granted"] != true {
			t.Errorf("Expected membership_granted true, got %v", respBody["membership_granted"])
		}
	})

	t.Run("invitee can decline invitation", func(t *testing.T) {
		// Create another invitation
		invitation2 := &models.SubRegionMembershipRequest{
			UserID:         invitee.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeInvitation,
			InitiatedBy:    admin.ID,
		}
		// Remove user from region to allow another invitation
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ? AND region_id = ?", invitee.ID, subRegion.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_requests WHERE user_id = ? AND region_id = ?", invitee.ID, subRegion.ID)

		if err := suite.membershipRepo.Create(context.Background(), invitation2); err != nil {
			t.Fatalf("Failed to create test invitation: %v", err)
		}

		body := map[string]bool{"accept": false}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/invitations/"+invitation2.ID+"/respond", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", invitation2.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           invitee.ID,
			Email:            invitee.Email,
			VerificationTier: invitee.VerificationTier,
			PostcardVerified: invitee.PostcardVerified,
			VouchVerified:    invitee.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RespondToInvitation(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["status"] != "rejected" {
			t.Errorf("Expected status 'rejected', got %v", respBody["status"])
		}
	})
}

// =============================================================================
// Additional Edge Case Tests
// =============================================================================

func TestMembershipHandler_CreateRequest_EdgeCases(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	verifiedUser := suite.createTestUser("mem_edge_verified", models.TierPostcard, true, false)
	vouchOnlyUser := suite.createTestUser("mem_edge_vouch", models.TierVouched, false, true)
	fullyVerifiedUser := suite.createTestUser("mem_edge_full", models.TierPostcard, true, true)

	// Create region hierarchy: City -> Neighborhood -> City Block
	cityRegion := suite.createTestRegion("Edge Test City", nil, models.RegionTypeCity)
	neighborhoodRegion := suite.createTestRegion("Edge Test Neighborhood", &cityRegion.ID, models.RegionTypeNeighborhood)
	blockRegion := suite.createTestRegion("Edge Test Block", &neighborhoodRegion.ID, models.RegionTypeCityBlock)

	// Add users to regions
	suite.addUserToRegion(verifiedUser.ID, cityRegion.ID, false)
	suite.addUserToRegion(vouchOnlyUser.ID, cityRegion.ID, false)
	suite.addUserToRegion(fullyVerifiedUser.ID, neighborhoodRegion.ID, false)

	defer suite.cleanup(
		[]string{verifiedUser.ID, vouchOnlyUser.ID, fullyVerifiedUser.ID},
		[]string{blockRegion.ID, neighborhoodRegion.ID, cityRegion.ID},
	)

	t.Run("vouch-only verified user can request membership", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/communities/"+neighborhoodRegion.ID+"/membership-requests", nil)
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", neighborhoodRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           vouchOnlyUser.ID,
			Email:            vouchOnlyUser.Email,
			VerificationTier: vouchOnlyUser.VerificationTier,
			PostcardVerified: vouchOnlyUser.PostcardVerified,
			VouchVerified:    vouchOnlyUser.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateRequest(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("user cannot request deep nested sub-region without being in immediate parent", func(t *testing.T) {
		// verifiedUser is only in cityRegion, but tries to request blockRegion (2 levels down)
		req := httptest.NewRequest("POST", "/api/v1/communities/"+blockRegion.ID+"/membership-requests", nil)
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", blockRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: verifiedUser.VerificationTier,
			PostcardVerified: verifiedUser.PostcardVerified,
			VouchVerified:    verifiedUser.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateRequest(rec, req)

		// Should fail because user is not in neighborhood (immediate parent of block)
		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("user can request nested sub-region when in immediate parent", func(t *testing.T) {
		// fullyVerifiedUser is in neighborhoodRegion, requests blockRegion
		req := httptest.NewRequest("POST", "/api/v1/communities/"+blockRegion.ID+"/membership-requests", nil)
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", blockRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           fullyVerifiedUser.ID,
			Email:            fullyVerifiedUser.Email,
			VerificationTier: fullyVerifiedUser.VerificationTier,
			PostcardVerified: fullyVerifiedUser.PostcardVerified,
			VouchVerified:    fullyVerifiedUser.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateRequest(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("duplicate request returns conflict with existing request ID", func(t *testing.T) {
		// verifiedUser has a pending request for neighborhoodRegion already
		// Try to create another
		req := httptest.NewRequest("POST", "/api/v1/communities/"+neighborhoodRegion.ID+"/membership-requests", nil)
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", neighborhoodRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           vouchOnlyUser.ID,
			Email:            vouchOnlyUser.Email,
			VerificationTier: vouchOnlyUser.VerificationTier,
			PostcardVerified: vouchOnlyUser.PostcardVerified,
			VouchVerified:    vouchOnlyUser.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateRequest(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["existing_request_id"] == nil {
			t.Error("Expected existing_request_id in response")
		}
	})

	t.Run("user already in region gets conflict", func(t *testing.T) {
		// fullyVerifiedUser is already in neighborhoodRegion
		req := httptest.NewRequest("POST", "/api/v1/communities/"+neighborhoodRegion.ID+"/membership-requests", nil)
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", neighborhoodRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           fullyVerifiedUser.ID,
			Email:            fullyVerifiedUser.Email,
			VerificationTier: fullyVerifiedUser.VerificationTier,
			PostcardVerified: fullyVerifiedUser.PostcardVerified,
			VouchVerified:    fullyVerifiedUser.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateRequest(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("request to non-existent region returns 404", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/communities/non-existent-id/membership-requests", nil)
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", "non-existent-id")
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: verifiedUser.VerificationTier,
			PostcardVerified: verifiedUser.PostcardVerified,
			VouchVerified:    verifiedUser.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateRequest(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestMembershipHandler_RateLimiting(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create user and regions
	user := suite.createTestUser("mem_ratelimit", models.TierPostcard, true, false)
	parentRegion := suite.createTestRegion("RateLimit Test City", nil, models.RegionTypeCity)

	// Create 6 sub-regions (exceeds limit of 5)
	subRegions := make([]*models.GeographicRegion, 6)
	regionIDs := make([]string, 7) // parent + 6 sub-regions
	for i := 0; i < 6; i++ {
		subRegions[i] = suite.createTestRegion("RateLimit Neighborhood "+string(rune('A'+i)), &parentRegion.ID, models.RegionTypeNeighborhood)
		regionIDs[i] = subRegions[i].ID
	}
	regionIDs[6] = parentRegion.ID

	// Add user to parent region
	suite.addUserToRegion(user.ID, parentRegion.ID, false)

	defer suite.cleanup([]string{user.ID}, regionIDs)

	t.Run("rate limits after max pending requests", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: user.VerificationTier,
			PostcardVerified: user.PostcardVerified,
			VouchVerified:    user.VouchVerified,
		}

		// Create 5 requests (at the limit)
		for i := 0; i < 5; i++ {
			req := httptest.NewRequest("POST", "/api/v1/communities/"+subRegions[i].ID+"/membership-requests", nil)
			req.Header.Set("Content-Type", "application/json")

			q := req.URL.Query()
			q.Set("id", subRegions[i].ID)
			req.URL.RawQuery = q.Encode()

			ctx := middleware.ContextWithUser(req.Context(), claims)
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()
			suite.handler.CreateRequest(rec, req)

			if rec.Code != http.StatusCreated {
				t.Errorf("Request %d: Expected status 201, got %d: %s", i+1, rec.Code, rec.Body.String())
			}
		}

		// 6th request should be rate limited
		req := httptest.NewRequest("POST", "/api/v1/communities/"+subRegions[5].ID+"/membership-requests", nil)
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", subRegions[5].ID)
		req.URL.RawQuery = q.Encode()

		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateRequest(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("Expected status 429 for rate limit, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody["pending_count"] == nil {
			t.Error("Expected pending_count in response")
		}
		if respBody["max_allowed"] == nil {
			t.Error("Expected max_allowed in response")
		}
	})
}

func TestMembershipHandler_VoteOnRequest_EdgeCases(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	requester := suite.createTestUser("mem_vote_req", models.TierPostcard, true, false)
	admin1 := suite.createTestUser("mem_vote_admin1", models.TierPostcard, true, true)
	admin2 := suite.createTestUser("mem_vote_admin2", models.TierPostcard, true, true)
	admin3 := suite.createTestUser("mem_vote_admin3", models.TierPostcard, true, true)
	parentAdmin := suite.createTestUser("mem_vote_parent_admin", models.TierPostcard, true, true)

	// Create region hierarchy
	parentRegion := suite.createTestRegion("Vote Edge Parent", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("Vote Edge Sub", &parentRegion.ID, models.RegionTypeNeighborhood)

	// Add users to regions
	suite.addUserToRegion(requester.ID, parentRegion.ID, false)
	suite.addUserToRegion(admin1.ID, subRegion.ID, true)
	suite.addUserToRegion(admin2.ID, subRegion.ID, true)
	suite.addUserToRegion(admin3.ID, subRegion.ID, true)
	suite.addUserToRegion(parentAdmin.ID, parentRegion.ID, true)

	defer suite.cleanup(
		[]string{requester.ID, admin1.ID, admin2.ID, admin3.ID, parentAdmin.ID},
		[]string{subRegion.ID, parentRegion.ID},
	)

	t.Run("rejection vote is recorded correctly", func(t *testing.T) {
		membershipRequest := &models.SubRegionMembershipRequest{
			UserID:         requester.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    requester.ID,
		}
		if err := suite.membershipRepo.Create(context.Background(), membershipRequest); err != nil {
			t.Fatalf("Failed to create test membership request: %v", err)
		}

		body := map[string]bool{"vote": false}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/membership-requests/"+membershipRequest.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", membershipRequest.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
			PostcardVerified: admin1.PostcardVerified,
			VouchVerified:    admin1.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		// Rejection votes don't count toward approval
		if respBody["current_votes"].(float64) != 0 {
			t.Errorf("Expected current_votes 0 (rejection), got %v", respBody["current_votes"])
		}
		if respBody["status"] != "pending" {
			t.Errorf("Expected status 'pending', got %v", respBody["status"])
		}

		// Cleanup
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_votes WHERE request_id = ?", membershipRequest.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_requests WHERE id = ?", membershipRequest.ID)
	})

	t.Run("voting on non-existent request returns 404", func(t *testing.T) {
		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/membership-requests/non-existent-id/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", "non-existent-id")
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
			PostcardVerified: admin1.PostcardVerified,
			VouchVerified:    admin1.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("voting on closed request returns conflict", func(t *testing.T) {
		membershipRequest := &models.SubRegionMembershipRequest{
			UserID:         requester.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    requester.ID,
		}
		if err := suite.membershipRepo.Create(context.Background(), membershipRequest); err != nil {
			t.Fatalf("Failed to create test membership request: %v", err)
		}

		// Approve the request
		_ = suite.membershipRepo.UpdateStatus(context.Background(), membershipRequest.ID, models.MembershipRequestStatusApproved)

		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/membership-requests/"+membershipRequest.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", membershipRequest.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
			PostcardVerified: admin1.PostcardVerified,
			VouchVerified:    admin1.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}

		// Cleanup
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_requests WHERE id = ?", membershipRequest.ID)
	})

	t.Run("voting on expired request returns conflict", func(t *testing.T) {
		membershipRequest := &models.SubRegionMembershipRequest{
			UserID:         requester.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    requester.ID,
		}
		if err := suite.membershipRepo.Create(context.Background(), membershipRequest); err != nil {
			t.Fatalf("Failed to create test membership request: %v", err)
		}

		// Set expires_at to the past (status stays 'pending' to simulate the race window)
		_, _ = suite.db.ExecContext(context.Background(),
			"UPDATE sub_region_membership_requests SET expires_at = DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE id = ?",
			membershipRequest.ID)

		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/membership-requests/"+membershipRequest.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", membershipRequest.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
			PostcardVerified: admin1.PostcardVerified,
			VouchVerified:    admin1.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}

		// Cleanup
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_requests WHERE id = ?", membershipRequest.ID)
	})

	t.Run("multiple approvals trigger membership grant", func(t *testing.T) {
		membershipRequest := &models.SubRegionMembershipRequest{
			UserID:         requester.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    requester.ID,
		}
		if err := suite.membershipRepo.Create(context.Background(), membershipRequest); err != nil {
			t.Fatalf("Failed to create test membership request: %v", err)
		}

		// First admin votes
		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/membership-requests/"+membershipRequest.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", membershipRequest.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
			PostcardVerified: admin1.PostcardVerified,
			VouchVerified:    admin1.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("First vote: Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Second admin votes
		req2 := httptest.NewRequest("POST", "/api/v1/membership-requests/"+membershipRequest.ID+"/vote", bytes.NewReader(bodyBytes))
		req2.Header.Set("Content-Type", "application/json")
		q2 := req2.URL.Query()
		q2.Set("id", membershipRequest.ID)
		req2.URL.RawQuery = q2.Encode()

		claims2 := &middleware.Claims{
			UserID:           admin2.ID,
			Email:            admin2.Email,
			VerificationTier: admin2.VerificationTier,
			PostcardVerified: admin2.PostcardVerified,
			VouchVerified:    admin2.VouchVerified,
		}
		ctx2 := middleware.ContextWithUser(req2.Context(), claims2)
		req2 = req2.WithContext(ctx2)

		rec2 := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec2, req2)

		if rec2.Code != http.StatusOK {
			t.Errorf("Second vote: Expected status 200, got %d: %s", rec2.Code, rec2.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec2.Body).Decode(&respBody)

		if respBody["status"] != "approved" {
			t.Errorf("Expected status 'approved', got %v", respBody["status"])
		}
		if respBody["membership_granted"] != true {
			t.Errorf("Expected membership_granted true, got %v", respBody["membership_granted"])
		}

		// Verify user was added to region
		isMember, _ := suite.regionRepo.IsUserInRegion(context.Background(), requester.ID, subRegion.ID)
		if !isMember {
			t.Error("Expected user to be added to region after approval")
		}

		// Cleanup
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ? AND region_id = ?", requester.ID, subRegion.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_votes WHERE request_id = ?", membershipRequest.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_requests WHERE id = ?", membershipRequest.ID)
	})

	t.Run("admin status is granted to fully verified user on approval", func(t *testing.T) {
		fullyVerifiedRequester := suite.createTestUser("mem_vote_fullreq", models.TierPostcard, true, true)
		suite.addUserToRegion(fullyVerifiedRequester.ID, parentRegion.ID, false)

		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", fullyVerifiedRequester.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", fullyVerifiedRequester.ID)
		}()

		membershipRequest := &models.SubRegionMembershipRequest{
			UserID:         fullyVerifiedRequester.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    fullyVerifiedRequester.ID,
		}
		if err := suite.membershipRepo.Create(context.Background(), membershipRequest); err != nil {
			t.Fatalf("Failed to create test membership request: %v", err)
		}

		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		// Admin 1 votes
		req := httptest.NewRequest("POST", "/api/v1/membership-requests/"+membershipRequest.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", membershipRequest.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
			PostcardVerified: admin1.PostcardVerified,
			VouchVerified:    admin1.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec, req)

		// Admin 2 votes (approval threshold reached)
		req2 := httptest.NewRequest("POST", "/api/v1/membership-requests/"+membershipRequest.ID+"/vote", bytes.NewReader(bodyBytes))
		req2.Header.Set("Content-Type", "application/json")
		q2 := req2.URL.Query()
		q2.Set("id", membershipRequest.ID)
		req2.URL.RawQuery = q2.Encode()

		claims2 := &middleware.Claims{
			UserID:           admin2.ID,
			Email:            admin2.Email,
			VerificationTier: admin2.VerificationTier,
			PostcardVerified: admin2.PostcardVerified,
			VouchVerified:    admin2.VouchVerified,
		}
		ctx2 := middleware.ContextWithUser(req2.Context(), claims2)
		req2 = req2.WithContext(ctx2)

		rec2 := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec2, req2)

		// Verify fully verified user was granted admin status
		isAdmin, _ := suite.regionRepo.IsUserAdmin(context.Background(), fullyVerifiedRequester.ID, subRegion.ID)
		if !isAdmin {
			t.Error("Expected fully verified user to be granted admin status on approval")
		}

		// Cleanup
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_votes WHERE request_id = ?", membershipRequest.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_requests WHERE id = ?", membershipRequest.ID)
	})
}

func TestMembershipHandler_GetRequest(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	requester := suite.createTestUser("mem_get_requester", models.TierPostcard, true, false)
	admin := suite.createTestUser("mem_get_admin", models.TierPostcard, true, true)
	otherUser := suite.createTestUser("mem_get_other", models.TierPostcard, true, false)

	// Create regions
	parentRegion := suite.createTestRegion("Get Test City", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("Get Test Neighborhood", &parentRegion.ID, models.RegionTypeNeighborhood)

	// Add users to regions
	suite.addUserToRegion(requester.ID, parentRegion.ID, false)
	suite.addUserToRegion(admin.ID, subRegion.ID, true)
	suite.addUserToRegion(otherUser.ID, parentRegion.ID, false)

	defer suite.cleanup([]string{requester.ID, admin.ID, otherUser.ID}, []string{subRegion.ID, parentRegion.ID})

	// Create a membership request
	membershipRequest := &models.SubRegionMembershipRequest{
		UserID:         requester.ID,
		RegionID:       subRegion.ID,
		ParentRegionID: parentRegion.ID,
		RequestType:    models.MembershipRequestTypeRequest,
		InitiatedBy:    requester.ID,
	}
	if err := suite.membershipRepo.Create(context.Background(), membershipRequest); err != nil {
		t.Fatalf("Failed to create test membership request: %v", err)
	}

	t.Run("owner can view their own request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/membership-requests/"+membershipRequest.ID, nil)

		q := req.URL.Query()
		q.Set("id", membershipRequest.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           requester.ID,
			Email:            requester.Email,
			VerificationTier: requester.VerificationTier,
			PostcardVerified: requester.PostcardVerified,
			VouchVerified:    requester.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetRequest(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.MembershipRequestDetailResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if respBody.ID != membershipRequest.ID {
			t.Errorf("Expected request ID %s, got %s", membershipRequest.ID, respBody.ID)
		}
		if respBody.Username != requester.Username {
			t.Errorf("Expected username %s, got %s", requester.Username, respBody.Username)
		}
	})

	t.Run("admin can view request for their region", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/membership-requests/"+membershipRequest.ID, nil)

		q := req.URL.Query()
		q.Set("id", membershipRequest.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin.ID,
			Email:            admin.Email,
			VerificationTier: admin.VerificationTier,
			PostcardVerified: admin.PostcardVerified,
			VouchVerified:    admin.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetRequest(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.MembershipRequestDetailResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		// Admin can vote and hasn't voted yet
		if !respBody.CanVote {
			t.Error("Expected CanVote to be true for admin")
		}
		if respBody.HasVoted {
			t.Error("Expected HasVoted to be false")
		}
	})

	t.Run("non-admin non-owner cannot view request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/membership-requests/"+membershipRequest.ID, nil)

		q := req.URL.Query()
		q.Set("id", membershipRequest.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           otherUser.ID,
			Email:            otherUser.Email,
			VerificationTier: otherUser.VerificationTier,
			PostcardVerified: otherUser.PostcardVerified,
			VouchVerified:    otherUser.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetRequest(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("non-existent request returns 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/membership-requests/non-existent-id", nil)

		q := req.URL.Query()
		q.Set("id", "non-existent-id")
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin.ID,
			Email:            admin.Email,
			VerificationTier: admin.VerificationTier,
			PostcardVerified: admin.PostcardVerified,
			VouchVerified:    admin.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetRequest(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestMembershipHandler_ListPendingForRegion(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	requester := suite.createTestUser("mem_listregion_req", models.TierPostcard, true, false)
	admin := suite.createTestUser("mem_listregion_admin", models.TierPostcard, true, true)
	nonAdmin := suite.createTestUser("mem_listregion_nonadmin", models.TierPostcard, true, false)

	// Create regions
	parentRegion := suite.createTestRegion("ListRegion Test City", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("ListRegion Test Neighborhood", &parentRegion.ID, models.RegionTypeNeighborhood)

	// Add users to regions
	suite.addUserToRegion(requester.ID, parentRegion.ID, false)
	suite.addUserToRegion(admin.ID, subRegion.ID, true)
	suite.addUserToRegion(nonAdmin.ID, parentRegion.ID, false)

	defer suite.cleanup([]string{requester.ID, admin.ID, nonAdmin.ID}, []string{subRegion.ID, parentRegion.ID})

	// Create a membership request
	membershipRequest := &models.SubRegionMembershipRequest{
		UserID:         requester.ID,
		RegionID:       subRegion.ID,
		ParentRegionID: parentRegion.ID,
		RequestType:    models.MembershipRequestTypeRequest,
		InitiatedBy:    requester.ID,
	}
	if err := suite.membershipRepo.Create(context.Background(), membershipRequest); err != nil {
		t.Fatalf("Failed to create test membership request: %v", err)
	}

	t.Run("admin can list pending requests for region", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/communities/"+subRegion.ID+"/membership-requests", nil)

		q := req.URL.Query()
		q.Set("id", subRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin.ID,
			Email:            admin.Email,
			VerificationTier: admin.VerificationTier,
			PostcardVerified: admin.PostcardVerified,
			VouchVerified:    admin.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListPendingForRegion(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.MembershipRequestListResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if len(respBody.Requests) != 1 {
			t.Errorf("Expected 1 request, got %d", len(respBody.Requests))
		}
	})

	t.Run("non-admin cannot list pending requests", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/communities/"+subRegion.ID+"/membership-requests", nil)

		q := req.URL.Query()
		q.Set("id", subRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           nonAdmin.ID,
			Email:            nonAdmin.Email,
			VerificationTier: nonAdmin.VerificationTier,
			PostcardVerified: nonAdmin.PostcardVerified,
			VouchVerified:    nonAdmin.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListPendingForRegion(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestMembershipHandler_ListPendingForAdmin(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	requester := suite.createTestUser("mem_listadmin_req", models.TierPostcard, true, false)
	admin := suite.createTestUser("mem_listadmin_admin", models.TierPostcard, true, true)
	postcardOnlyUser := suite.createTestUser("mem_listadmin_postcard", models.TierPostcard, true, false)

	// Create regions
	parentRegion := suite.createTestRegion("ListAdmin Test City", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("ListAdmin Test Neighborhood", &parentRegion.ID, models.RegionTypeNeighborhood)

	// Add users to regions
	suite.addUserToRegion(requester.ID, parentRegion.ID, false)
	suite.addUserToRegion(admin.ID, subRegion.ID, true)

	defer suite.cleanup([]string{requester.ID, admin.ID, postcardOnlyUser.ID}, []string{subRegion.ID, parentRegion.ID})

	// Create a membership request
	membershipRequest := &models.SubRegionMembershipRequest{
		UserID:         requester.ID,
		RegionID:       subRegion.ID,
		ParentRegionID: parentRegion.ID,
		RequestType:    models.MembershipRequestTypeRequest,
		InitiatedBy:    requester.ID,
	}
	if err := suite.membershipRepo.Create(context.Background(), membershipRequest); err != nil {
		t.Fatalf("Failed to create test membership request: %v", err)
	}

	t.Run("admin can list all pending requests for their regions", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/membership-requests/admin", nil)

		claims := &middleware.Claims{
			UserID:           admin.ID,
			Email:            admin.Email,
			VerificationTier: admin.VerificationTier,
			PostcardVerified: admin.PostcardVerified,
			VouchVerified:    admin.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListPendingForAdmin(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.MembershipRequestListResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if len(respBody.Requests) != 1 {
			t.Errorf("Expected 1 request, got %d", len(respBody.Requests))
		}
	})

	t.Run("postcard-only user cannot list admin view", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/membership-requests/admin", nil)

		claims := &middleware.Claims{
			UserID:           postcardOnlyUser.ID,
			Email:            postcardOnlyUser.Email,
			VerificationTier: postcardOnlyUser.VerificationTier,
			PostcardVerified: postcardOnlyUser.PostcardVerified,
			VouchVerified:    postcardOnlyUser.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListPendingForAdmin(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestMembershipHandler_CreateInvitation_EdgeCases(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	admin := suite.createTestUser("mem_inv_edge_admin", models.TierPostcard, true, true)
	invitee := suite.createTestUser("mem_inv_edge_invitee", models.TierPostcard, true, false)
	outsider := suite.createTestUser("mem_inv_edge_outsider", models.TierPostcard, true, false)

	// Create regions
	parentRegion := suite.createTestRegion("Inv Edge Test City", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("Inv Edge Test Neighborhood", &parentRegion.ID, models.RegionTypeNeighborhood)

	// Add users to regions
	suite.addUserToRegion(admin.ID, subRegion.ID, true)
	suite.addUserToRegion(invitee.ID, parentRegion.ID, false)
	// outsider is not in any region

	defer suite.cleanup([]string{admin.ID, invitee.ID, outsider.ID}, []string{subRegion.ID, parentRegion.ID})

	t.Run("cannot invite user not in parent region", func(t *testing.T) {
		body := map[string]string{"user_id": outsider.ID}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/communities/"+subRegion.ID+"/invitations", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", subRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin.ID,
			Email:            admin.Email,
			VerificationTier: admin.VerificationTier,
			PostcardVerified: admin.PostcardVerified,
			VouchVerified:    admin.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateInvitation(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("cannot invite non-existent user", func(t *testing.T) {
		body := map[string]string{"user_id": "non-existent-user-id"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/communities/"+subRegion.ID+"/invitations", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", subRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin.ID,
			Email:            admin.Email,
			VerificationTier: admin.VerificationTier,
			PostcardVerified: admin.PostcardVerified,
			VouchVerified:    admin.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateInvitation(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("cannot invite user already in region", func(t *testing.T) {
		// Add invitee to sub-region
		suite.addUserToRegion(invitee.ID, subRegion.ID, false)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ? AND region_id = ?", invitee.ID, subRegion.ID)
		}()

		body := map[string]string{"user_id": invitee.ID}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/communities/"+subRegion.ID+"/invitations", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", subRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin.ID,
			Email:            admin.Email,
			VerificationTier: admin.VerificationTier,
			PostcardVerified: admin.PostcardVerified,
			VouchVerified:    admin.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateInvitation(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("cannot invite to non-sub-region", func(t *testing.T) {
		body := map[string]string{"user_id": invitee.ID}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/communities/"+parentRegion.ID+"/invitations", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", parentRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin.ID,
			Email:            admin.Email,
			VerificationTier: admin.VerificationTier,
			PostcardVerified: admin.PostcardVerified,
			VouchVerified:    admin.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateInvitation(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing user_id returns validation error", func(t *testing.T) {
		body := map[string]string{}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/communities/"+subRegion.ID+"/invitations", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", subRegion.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin.ID,
			Email:            admin.Email,
			VerificationTier: admin.VerificationTier,
			PostcardVerified: admin.PostcardVerified,
			VouchVerified:    admin.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.CreateInvitation(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestMembershipHandler_RespondToInvitation_EdgeCases(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	admin := suite.createTestUser("mem_resp_edge_admin", models.TierPostcard, true, true)
	invitee := suite.createTestUser("mem_resp_edge_invitee", models.TierPostcard, true, false)
	otherUser := suite.createTestUser("mem_resp_edge_other", models.TierPostcard, true, false)

	// Create regions
	parentRegion := suite.createTestRegion("Resp Edge Test City", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("Resp Edge Test Neighborhood", &parentRegion.ID, models.RegionTypeNeighborhood)

	// Add users to regions
	suite.addUserToRegion(admin.ID, subRegion.ID, true)
	suite.addUserToRegion(invitee.ID, parentRegion.ID, false)
	suite.addUserToRegion(otherUser.ID, parentRegion.ID, false)

	defer suite.cleanup([]string{admin.ID, invitee.ID, otherUser.ID}, []string{subRegion.ID, parentRegion.ID})

	// Create an invitation
	invitation := &models.SubRegionMembershipRequest{
		UserID:         invitee.ID,
		RegionID:       subRegion.ID,
		ParentRegionID: parentRegion.ID,
		RequestType:    models.MembershipRequestTypeInvitation,
		InitiatedBy:    admin.ID,
	}
	if err := suite.membershipRepo.Create(context.Background(), invitation); err != nil {
		t.Fatalf("Failed to create test invitation: %v", err)
	}

	t.Run("other user cannot respond to someone else's invitation", func(t *testing.T) {
		body := map[string]bool{"accept": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/invitations/"+invitation.ID+"/respond", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", invitation.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           otherUser.ID,
			Email:            otherUser.Email,
			VerificationTier: otherUser.VerificationTier,
			PostcardVerified: otherUser.PostcardVerified,
			VouchVerified:    otherUser.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RespondToInvitation(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("cannot respond to non-existent invitation", func(t *testing.T) {
		body := map[string]bool{"accept": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/invitations/non-existent-id/respond", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", "non-existent-id")
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           invitee.ID,
			Email:            invitee.Email,
			VerificationTier: invitee.VerificationTier,
			PostcardVerified: invitee.PostcardVerified,
			VouchVerified:    invitee.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RespondToInvitation(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("cannot respond to regular request (not invitation)", func(t *testing.T) {
		// Create a regular request (not invitation)
		request := &models.SubRegionMembershipRequest{
			UserID:         invitee.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeRequest,
			InitiatedBy:    invitee.ID,
		}
		// Delete existing
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_requests WHERE user_id = ? AND region_id = ?", invitee.ID, subRegion.ID)
		if err := suite.membershipRepo.Create(context.Background(), request); err != nil {
			t.Fatalf("Failed to create test request: %v", err)
		}

		body := map[string]bool{"accept": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/invitations/"+request.ID+"/respond", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", request.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           invitee.ID,
			Email:            invitee.Email,
			VerificationTier: invitee.VerificationTier,
			PostcardVerified: invitee.PostcardVerified,
			VouchVerified:    invitee.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RespondToInvitation(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("cannot respond to already closed invitation", func(t *testing.T) {
		// Create and close an invitation
		closedInvitation := &models.SubRegionMembershipRequest{
			UserID:         invitee.ID,
			RegionID:       subRegion.ID,
			ParentRegionID: parentRegion.ID,
			RequestType:    models.MembershipRequestTypeInvitation,
			InitiatedBy:    admin.ID,
		}
		// Delete existing
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_requests WHERE user_id = ? AND region_id = ?", invitee.ID, subRegion.ID)
		if err := suite.membershipRepo.Create(context.Background(), closedInvitation); err != nil {
			t.Fatalf("Failed to create test invitation: %v", err)
		}

		// Close it
		_ = suite.membershipRepo.UpdateStatus(context.Background(), closedInvitation.ID, models.MembershipRequestStatusApproved)

		body := map[string]bool{"accept": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/invitations/"+closedInvitation.ID+"/respond", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", closedInvitation.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           invitee.ID,
			Email:            invitee.Email,
			VerificationTier: invitee.VerificationTier,
			PostcardVerified: invitee.PostcardVerified,
			VouchVerified:    invitee.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RespondToInvitation(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestMembershipHandler_ListInvitations(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	admin := suite.createTestUser("mem_listinv_admin", models.TierPostcard, true, true)
	invitee := suite.createTestUser("mem_listinv_invitee", models.TierPostcard, true, false)

	// Create regions
	parentRegion := suite.createTestRegion("ListInv Test City", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("ListInv Test Neighborhood", &parentRegion.ID, models.RegionTypeNeighborhood)

	// Add users to regions
	suite.addUserToRegion(admin.ID, subRegion.ID, true)
	suite.addUserToRegion(invitee.ID, parentRegion.ID, false)

	defer suite.cleanup([]string{admin.ID, invitee.ID}, []string{subRegion.ID, parentRegion.ID})

	// Create an invitation
	invitation := &models.SubRegionMembershipRequest{
		UserID:         invitee.ID,
		RegionID:       subRegion.ID,
		ParentRegionID: parentRegion.ID,
		RequestType:    models.MembershipRequestTypeInvitation,
		InitiatedBy:    admin.ID,
	}
	if err := suite.membershipRepo.Create(context.Background(), invitation); err != nil {
		t.Fatalf("Failed to create test invitation: %v", err)
	}

	t.Run("user can list their pending invitations", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/invitations", nil)

		claims := &middleware.Claims{
			UserID:           invitee.ID,
			Email:            invitee.Email,
			VerificationTier: invitee.VerificationTier,
			PostcardVerified: invitee.PostcardVerified,
			VouchVerified:    invitee.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListInvitations(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.MembershipRequestListResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if len(respBody.Requests) != 1 {
			t.Errorf("Expected 1 invitation, got %d", len(respBody.Requests))
		}
		if len(respBody.Requests) > 0 && respBody.Requests[0].RequestType != "invitation" {
			t.Errorf("Expected request type 'invitation', got %s", respBody.Requests[0].RequestType)
		}
	})

	t.Run("user with no invitations gets empty list", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/invitations", nil)

		claims := &middleware.Claims{
			UserID:           admin.ID,
			Email:            admin.Email,
			VerificationTier: admin.VerificationTier,
			PostcardVerified: admin.PostcardVerified,
			VouchVerified:    admin.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListInvitations(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody models.MembershipRequestListResponse
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		if len(respBody.Requests) != 0 {
			t.Errorf("Expected 0 invitations, got %d", len(respBody.Requests))
		}
	})
}

// =============================================================================
// Stale JWT Claims Bug Tests
// These tests verify that admin status is determined from fresh database values,
// not from potentially stale JWT claims.
// =============================================================================

func TestMembershipHandler_RespondToInvitation_StaleJWTClaims(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	admin := suite.createTestUser("mem_stale_admin", models.TierPostcard, true, true)

	// Create invitee with ONLY postcard verification initially
	// This simulates the state when the user logged in and got their JWT token
	invitee := suite.createTestUser("mem_stale_invitee", models.TierPostcard, true, false) // Note: vouch=false

	// Create regions
	parentRegion := suite.createTestRegion("Stale JWT Test City", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("Stale JWT Test Neighborhood", &parentRegion.ID, models.RegionTypeNeighborhood)

	// Add users to regions
	suite.addUserToRegion(admin.ID, subRegion.ID, true)
	suite.addUserToRegion(invitee.ID, parentRegion.ID, false)

	defer suite.cleanup([]string{admin.ID, invitee.ID}, []string{subRegion.ID, parentRegion.ID})

	// Create an invitation for the invitee
	invitation := &models.SubRegionMembershipRequest{
		UserID:         invitee.ID,
		RegionID:       subRegion.ID,
		ParentRegionID: parentRegion.ID,
		RequestType:    models.MembershipRequestTypeInvitation,
		InitiatedBy:    admin.ID,
	}
	if err := suite.membershipRepo.Create(context.Background(), invitation); err != nil {
		t.Fatalf("Failed to create test invitation: %v", err)
	}

	t.Run("user gets admin when vouch verified after JWT issued", func(t *testing.T) {
		// IMPORTANT: Now update the user in the database to be vouch verified
		// This simulates the user getting vouch verified AFTER they logged in
		// (so their JWT token still has VouchVerified=false)
		if err := suite.userRepo.SetVouchVerified(context.Background(), invitee.ID, true); err != nil {
			t.Fatalf("Failed to set vouch verified: %v", err)
		}

		// Verify the database was updated
		updatedUser, err := suite.userRepo.GetByID(context.Background(), invitee.ID)
		if err != nil {
			t.Fatalf("Failed to get updated user: %v", err)
		}
		if !updatedUser.VouchVerified {
			t.Fatal("Database should have VouchVerified=true")
		}

		body := map[string]bool{"accept": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/invitations/"+invitation.ID+"/respond", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", invitation.ID)
		req.URL.RawQuery = q.Encode()

		// Create JWT claims with STALE data (VouchVerified=false)
		// This simulates what happens when user logged in before getting vouch verified
		claims := &middleware.Claims{
			UserID:           invitee.ID,
			Email:            invitee.Email,
			VerificationTier: models.TierPostcard, // Stale tier
			PostcardVerified: true,
			VouchVerified:    false, // STALE: This was false when JWT was issued
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RespondToInvitation(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify the user was added to the region with admin=TRUE
		// (The fix ensures we use fresh database values, not stale JWT claims)
		userRegion, err := suite.regionRepo.GetUserRegion(context.Background(), invitee.ID, subRegion.ID)
		if err != nil {
			t.Fatalf("Failed to get user region: %v", err)
		}
		if userRegion == nil {
			t.Fatal("User should be in the region")
		}
		if !userRegion.IsAdmin {
			t.Errorf("User should have is_admin=TRUE because they are fully verified in the database, but got is_admin=FALSE. This indicates the bug where stale JWT claims were used instead of fresh database values.")
		}
	})
}

func TestMembershipHandler_VoteOnRequest_StaleJWTClaims(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	// Clean up any existing test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@membershiptest.com'")

	// Create test users
	admin1 := suite.createTestUser("mem_vote_stale_admin1", models.TierPostcard, true, true)
	admin2 := suite.createTestUser("mem_vote_stale_admin2", models.TierPostcard, true, true)

	// Create requester with ONLY postcard verification initially
	requester := suite.createTestUser("mem_vote_stale_requester", models.TierPostcard, true, false)

	// Create regions
	parentRegion := suite.createTestRegion("Vote Stale JWT Test City", nil, models.RegionTypeCity)
	subRegion := suite.createTestRegion("Vote Stale JWT Test Neighborhood", &parentRegion.ID, models.RegionTypeNeighborhood)

	// Add users to regions
	suite.addUserToRegion(admin1.ID, subRegion.ID, true)
	suite.addUserToRegion(admin2.ID, subRegion.ID, true)
	suite.addUserToRegion(requester.ID, parentRegion.ID, false)

	defer suite.cleanup([]string{admin1.ID, admin2.ID, requester.ID}, []string{subRegion.ID, parentRegion.ID})

	// Create a membership request
	request := &models.SubRegionMembershipRequest{
		UserID:         requester.ID,
		RegionID:       subRegion.ID,
		ParentRegionID: parentRegion.ID,
		RequestType:    models.MembershipRequestTypeRequest,
		InitiatedBy:    requester.ID,
	}
	if err := suite.membershipRepo.Create(context.Background(), request); err != nil {
		t.Fatalf("Failed to create test request: %v", err)
	}

	t.Run("requester gets admin when vouch verified before approval", func(t *testing.T) {
		// Update the requester in the database to be vouch verified
		// This happens after they made the request but before it was approved
		if err := suite.userRepo.SetVouchVerified(context.Background(), requester.ID, true); err != nil {
			t.Fatalf("Failed to set vouch verified: %v", err)
		}

		// First vote (note: field is "vote" not "approve")
		body := map[string]bool{"vote": true}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/membership-requests/"+request.ID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("id", request.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
			PostcardVerified: admin1.PostcardVerified,
			VouchVerified:    admin1.VouchVerified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200 for first vote, got %d: %s", rec.Code, rec.Body.String())
		}

		// Second vote (should reach consensus)
		bodyBytes2, _ := json.Marshal(body)
		req2 := httptest.NewRequest("POST", "/api/v1/membership-requests/"+request.ID+"/vote", bytes.NewReader(bodyBytes2))
		req2.Header.Set("Content-Type", "application/json")

		q2 := req2.URL.Query()
		q2.Set("id", request.ID)
		req2.URL.RawQuery = q2.Encode()

		claims2 := &middleware.Claims{
			UserID:           admin2.ID,
			Email:            admin2.Email,
			VerificationTier: admin2.VerificationTier,
			PostcardVerified: admin2.PostcardVerified,
			VouchVerified:    admin2.VouchVerified,
		}
		ctx2 := middleware.ContextWithUser(req2.Context(), claims2)
		req2 = req2.WithContext(ctx2)

		rec2 := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec2, req2)

		if rec2.Code != http.StatusOK {
			t.Fatalf("Expected status 200 for second vote, got %d: %s", rec2.Code, rec2.Body.String())
		}

		// Check if membership was granted (consensus reached)
		var voteResp map[string]interface{}
		_ = json.NewDecoder(rec2.Body).Decode(&voteResp)
		membershipGranted, _ := voteResp["membership_granted"].(bool)
		if !membershipGranted {
			t.Logf("Vote response: %+v", voteResp)
			t.Fatalf("Expected membership_granted=true after 2 votes")
		}

		// Verify the requester was added with admin=TRUE
		userRegion, err := suite.regionRepo.GetUserRegion(context.Background(), requester.ID, subRegion.ID)
		if err != nil {
			t.Fatalf("Failed to get user region: %v", err)
		}
		if userRegion == nil {
			t.Fatal("Requester should be in the region after approval")
		}
		if !userRegion.IsAdmin {
			t.Errorf("Requester should have is_admin=TRUE because they are fully verified in the database at time of approval")
		}
	})
}
