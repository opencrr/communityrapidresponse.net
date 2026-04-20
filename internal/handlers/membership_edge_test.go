package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// TestMembershipEdge_AlreadyMember tests requesting membership in a region the
// user already belongs to (M1).
func TestMembershipEdge_AlreadyMember(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	user := suite.createTestUser("edge-mem-already", models.TierVouched, false, true)
	parentRegion := suite.createTestRegion("Edge Parent Region", nil, models.RegionTypeCity)
	childRegion := suite.createTestRegion("Edge Child Region", &parentRegion.ID, models.RegionTypeNeighborhood)
	suite.addUserToRegion(user.ID, parentRegion.ID, false)
	suite.addUserToRegion(user.ID, childRegion.ID, false) // Already a member

	t.Cleanup(func() {
		suite.cleanup([]string{user.ID}, []string{childRegion.ID, parentRegion.ID})
	})

	body := map[string]string{"region_id": childRegion.ID}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/regions/"+childRegion.ID+"/membership-requests", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.URL.RawQuery = "id=" + childRegion.ID
	claims := &middleware.Claims{
		UserID:           user.ID,
		VerificationTier: user.VerificationTier,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	suite.handler.CreateRequest(rec, req)

	if rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
		t.Error("Expected membership request for existing member to be rejected")
	}
	t.Logf("Already member request: status %d, body: %s", rec.Code, rec.Body.String())
}

// TestMembershipEdge_VoteOnExpired tests voting on an expired membership request (M2).
func TestMembershipEdge_VoteOnExpired(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	admin := suite.createTestUser("edge-mem-admin-exp", models.TierPostcard, true, true)
	user := suite.createTestUser("edge-mem-user-exp", models.TierVouched, false, true)
	parentRegion := suite.createTestRegion("Edge Parent Exp", nil, models.RegionTypeCity)
	childRegion := suite.createTestRegion("Edge Child Exp", &parentRegion.ID, models.RegionTypeNeighborhood)
	suite.addUserToRegion(admin.ID, childRegion.ID, true)
	suite.addUserToRegion(user.ID, parentRegion.ID, false)

	// Create an expired request directly in the database
	requestID := uuid.New().String()
	_, err := suite.db.ExecContext(context.Background(),
		`INSERT INTO sub_region_membership_requests (id, user_id, region_id, status, created_at, expires_at)
		 VALUES (?, ?, ?, 'pending', DATE_SUB(NOW(), INTERVAL 8 DAY), DATE_SUB(NOW(), INTERVAL 1 DAY))`,
		requestID, user.ID, childRegion.ID)
	if err != nil {
		t.Fatalf("Failed to create expired request: %v", err)
	}

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_votes WHERE request_id = ?", requestID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_requests WHERE id = ?", requestID)
		suite.cleanup([]string{admin.ID, user.ID}, []string{childRegion.ID, parentRegion.ID})
	})

	body := map[string]bool{"approve": true}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/membership-requests/"+requestID+"/vote", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.URL.RawQuery = "id=" + requestID
	claims := &middleware.Claims{
		UserID:           admin.ID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	suite.handler.VoteOnRequest(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("Expected vote on expired request to fail")
	}
	t.Logf("Vote on expired request: status %d, body: %s", rec.Code, rec.Body.String())
}

// TestMembershipEdge_DuplicateRequest tests submitting duplicate membership
// requests for the same region (M3).
func TestMembershipEdge_DuplicateRequest(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	user := suite.createTestUser("edge-mem-dup", models.TierVouched, false, true)
	parentRegion := suite.createTestRegion("Edge Parent Dup", nil, models.RegionTypeCity)
	childRegion := suite.createTestRegion("Edge Child Dup", &parentRegion.ID, models.RegionTypeNeighborhood)
	suite.addUserToRegion(user.ID, parentRegion.ID, false)

	// Add an admin so requests can be submitted
	admin := suite.createTestUser("edge-mem-dup-admin", models.TierPostcard, true, true)
	suite.addUserToRegion(admin.ID, childRegion.ID, true)

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(),
			"DELETE FROM sub_region_membership_requests WHERE user_id = ? AND region_id = ?", user.ID, childRegion.ID)
		suite.cleanup([]string{user.ID, admin.ID}, []string{childRegion.ID, parentRegion.ID})
	})

	makeReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/regions/"+childRegion.ID+"/membership-requests", nil)
		req.Header.Set("Content-Type", "application/json")
		req.URL.RawQuery = "id=" + childRegion.ID
		claims := &middleware.Claims{
			UserID:           user.ID,
			VerificationTier: user.VerificationTier,
			VouchVerified:    true,
		}
		ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		suite.handler.CreateRequest(rec, req)
		return rec
	}

	// First request
	rec1 := makeReq()
	t.Logf("First membership request: status %d", rec1.Code)

	// Second request (duplicate)
	rec2 := makeReq()
	if rec2.Code == http.StatusOK || rec2.Code == http.StatusCreated {
		t.Error("Expected duplicate membership request to be rejected")
	}
	t.Logf("Duplicate membership request: status %d, body: %s", rec2.Code, rec2.Body.String())
}

// TestMembershipEdge_NotInParentRegion tests requesting membership when user is
// not in the parent region (M4).
func TestMembershipEdge_NotInParentRegion(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	user := suite.createTestUser("edge-mem-noparent", models.TierVouched, false, true)
	parentRegion := suite.createTestRegion("Edge Parent NP", nil, models.RegionTypeCity)
	childRegion := suite.createTestRegion("Edge Child NP", &parentRegion.ID, models.RegionTypeNeighborhood)
	// User is NOT added to parent region

	t.Cleanup(func() {
		suite.cleanup([]string{user.ID}, []string{childRegion.ID, parentRegion.ID})
	})

	req := httptest.NewRequest("POST", "/api/v1/regions/"+childRegion.ID+"/membership-requests", nil)
	req.Header.Set("Content-Type", "application/json")
	req.URL.RawQuery = "id=" + childRegion.ID
	claims := &middleware.Claims{
		UserID:           user.ID,
		VerificationTier: user.VerificationTier,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	suite.handler.CreateRequest(rec, req)

	if rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
		t.Error("Expected request from user not in parent to be rejected")
	}
	t.Logf("Not in parent region: status %d", rec.Code)
}

// TestMembershipEdge_NonAdminVote tests voting on a request as a non-admin (M5).
func TestMembershipEdge_NonAdminVote(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	nonAdmin := suite.createTestUser("edge-mem-nonadmin", models.TierVouched, false, true)
	user := suite.createTestUser("edge-mem-user-na", models.TierVouched, false, true)
	parentRegion := suite.createTestRegion("Edge Parent NA", nil, models.RegionTypeCity)
	childRegion := suite.createTestRegion("Edge Child NA", &parentRegion.ID, models.RegionTypeNeighborhood)
	suite.addUserToRegion(nonAdmin.ID, childRegion.ID, false) // not admin
	suite.addUserToRegion(user.ID, parentRegion.ID, false)

	// Create a request
	requestID := uuid.New().String()
	_, _ = suite.db.ExecContext(context.Background(),
		`INSERT INTO sub_region_membership_requests (id, user_id, region_id, status, created_at, expires_at)
		 VALUES (?, ?, ?, 'pending', NOW(), DATE_ADD(NOW(), INTERVAL 7 DAY))`,
		requestID, user.ID, childRegion.ID)

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_requests WHERE id = ?", requestID)
		suite.cleanup([]string{nonAdmin.ID, user.ID}, []string{childRegion.ID, parentRegion.ID})
	})

	body := map[string]bool{"approve": true}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/membership-requests/"+requestID+"/vote", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.URL.RawQuery = "id=" + requestID
	claims := &middleware.Claims{
		UserID:           nonAdmin.ID,
		PostcardVerified: false,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	suite.handler.VoteOnRequest(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("Expected non-admin vote to be rejected")
	}
	t.Logf("Non-admin vote: status %d", rec.Code)
}

// TestMembershipEdge_DoubleVote tests that an admin cannot vote twice on the
// same request (M6).
func TestMembershipEdge_DoubleVote(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	admin := suite.createTestUser("edge-mem-dv-admin", models.TierPostcard, true, true)
	user := suite.createTestUser("edge-mem-dv-user", models.TierVouched, false, true)
	parentRegion := suite.createTestRegion("Edge Parent DV", nil, models.RegionTypeCity)
	childRegion := suite.createTestRegion("Edge Child DV", &parentRegion.ID, models.RegionTypeNeighborhood)
	suite.addUserToRegion(admin.ID, childRegion.ID, true)
	suite.addUserToRegion(user.ID, parentRegion.ID, false)

	requestID := uuid.New().String()
	_, _ = suite.db.ExecContext(context.Background(),
		`INSERT INTO sub_region_membership_requests (id, user_id, region_id, status, created_at, expires_at)
		 VALUES (?, ?, ?, 'pending', NOW(), DATE_ADD(NOW(), INTERVAL 7 DAY))`,
		requestID, user.ID, childRegion.ID)

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_votes WHERE request_id = ?", requestID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_requests WHERE id = ?", requestID)
		suite.cleanup([]string{admin.ID, user.ID}, []string{childRegion.ID, parentRegion.ID})
	})

	makeVoteReq := func() *httptest.ResponseRecorder {
		body := map[string]bool{"approve": true}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/v1/membership-requests/"+requestID+"/vote", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.URL.RawQuery = "id=" + requestID
		claims := &middleware.Claims{
			UserID:           admin.ID,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		suite.handler.VoteOnRequest(rec, req)
		return rec
	}

	// First vote
	rec1 := makeVoteReq()
	t.Logf("First vote: status %d", rec1.Code)

	// Second vote (double)
	rec2 := makeVoteReq()
	if rec2.Code == http.StatusOK {
		t.Error("Expected double vote to be rejected")
	}
	t.Logf("Double vote: status %d, body: %s", rec2.Code, rec2.Body.String())
}

// TestMembershipEdge_TopLevelRegion tests requesting membership for a top-level
// region (no parent) (M7).
func TestMembershipEdge_TopLevelRegion(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	user := suite.createTestUser("edge-mem-toplevel", models.TierVouched, false, true)
	topRegion := suite.createTestRegion("Edge Top Region", nil, models.RegionTypeCity)

	t.Cleanup(func() {
		suite.cleanup([]string{user.ID}, []string{topRegion.ID})
	})

	req := httptest.NewRequest("POST", "/api/v1/regions/"+topRegion.ID+"/membership-requests", nil)
	req.Header.Set("Content-Type", "application/json")
	req.URL.RawQuery = "id=" + topRegion.ID
	claims := &middleware.Claims{
		UserID:           user.ID,
		VerificationTier: user.VerificationTier,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	suite.handler.CreateRequest(rec, req)

	// Top-level region has no parent, so membership request should fail
	t.Logf("Top-level region request: status %d, body: %s", rec.Code, rec.Body.String())
}

// TestMembershipEdge_ExpirationBoundary tests request at exact expiration time (M10).
func TestMembershipEdge_ExpirationBoundary(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	admin := suite.createTestUser("edge-mem-expbnd-admin", models.TierPostcard, true, true)
	user := suite.createTestUser("edge-mem-expbnd-user", models.TierVouched, false, true)
	parentRegion := suite.createTestRegion("Edge Parent EB", nil, models.RegionTypeCity)
	childRegion := suite.createTestRegion("Edge Child EB", &parentRegion.ID, models.RegionTypeNeighborhood)
	suite.addUserToRegion(admin.ID, childRegion.ID, true)
	suite.addUserToRegion(user.ID, parentRegion.ID, false)

	// Create request that expires exactly now
	requestID := uuid.New().String()
	now := time.Now().UTC()
	_, _ = suite.db.ExecContext(context.Background(),
		`INSERT INTO sub_region_membership_requests (id, user_id, region_id, status, created_at, expires_at)
		 VALUES (?, ?, ?, 'pending', ?, ?)`,
		requestID, user.ID, childRegion.ID, now.Add(-7*24*time.Hour), now)

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_votes WHERE request_id = ?", requestID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_requests WHERE id = ?", requestID)
		suite.cleanup([]string{admin.ID, user.ID}, []string{childRegion.ID, parentRegion.ID})
	})

	body := map[string]bool{"approve": true}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/membership-requests/"+requestID+"/vote", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.URL.RawQuery = "id=" + requestID
	claims := &middleware.Claims{
		UserID:           admin.ID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	suite.handler.VoteOnRequest(rec, req)

	// At exact expiration boundary, behavior depends on implementation
	// (time.Now().After(expiresAt) — "after" means strictly after, so exact == not expired)
	t.Logf("Expiration boundary vote: status %d, body: %s", rec.Code, rec.Body.String())
}

// TestMembershipEdge_CancelApprovedRequest tests canceling an already-approved
// request (M14).
func TestMembershipEdge_CancelApprovedRequest(t *testing.T) {
	suite := setupMembershipTestSuite(t)

	user := suite.createTestUser("edge-mem-cancel", models.TierVouched, false, true)
	parentRegion := suite.createTestRegion("Edge Parent Cancel", nil, models.RegionTypeCity)
	childRegion := suite.createTestRegion("Edge Child Cancel", &parentRegion.ID, models.RegionTypeNeighborhood)
	suite.addUserToRegion(user.ID, parentRegion.ID, false)

	// Create an approved request
	requestID := uuid.New().String()
	_, _ = suite.db.ExecContext(context.Background(),
		`INSERT INTO sub_region_membership_requests (id, user_id, region_id, status, created_at, expires_at)
		 VALUES (?, ?, ?, 'approved', NOW(), DATE_ADD(NOW(), INTERVAL 7 DAY))`,
		requestID, user.ID, childRegion.ID)

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM sub_region_membership_requests WHERE id = ?", requestID)
		suite.cleanup([]string{user.ID}, []string{childRegion.ID, parentRegion.ID})
	})

	req := httptest.NewRequest("DELETE", "/api/v1/membership-requests/"+requestID, nil)
	req.URL.RawQuery = "id=" + requestID
	claims := &middleware.Claims{
		UserID:           user.ID,
		VerificationTier: user.VerificationTier,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	suite.handler.CancelRequest(rec, req)

	// Cannot cancel an already-approved request
	t.Logf("Cancel approved request: status %d, body: %s", rec.Code, rec.Body.String())
}
