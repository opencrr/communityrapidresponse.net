package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// TestSchoolEdge_SelfVouch tests that a user cannot vouch for themselves at a
// school (S1).
func TestSchoolEdge_SelfVouch(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	user := suite.createTestUser("edge-school-selfvouch", models.TierUnverified)
	school := createEdgeTestSchool(t, suite,"Edge Self-Vouch School")
	joinEdgeUserToSchool(t, suite,user.ID, school.ID)

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_schools WHERE user_id = ?", user.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM schools WHERE id = ?", school.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", user.ID)
	})

	body := map[string]string{"identifier": user.Email}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/schools/"+school.ID+"/vouch", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.URL.RawQuery = "id=" + school.ID
	claims := &middleware.Claims{UserID: user.ID}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	suite.handler.Vouch(rec, req)

	if rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
		t.Error("Expected self-vouch to be rejected")
	}
	t.Logf("School self-vouch: status %d, body: %s", rec.Code, rec.Body.String())
}

// TestSchoolEdge_VouchForNonMember tests vouching for a user who hasn't joined
// the school (S2).
func TestSchoolEdge_VouchForNonMember(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	voucher := suite.createTestUser("edge-school-voucher", models.TierUnverified)
	nonMember := suite.createTestUser("edge-school-nonmember", models.TierUnverified)
	school := createEdgeTestSchool(t, suite,"Edge Non-Member School")
	joinEdgeUserToSchool(t, suite,voucher.ID, school.ID)
	// nonMember does NOT join

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_schools WHERE user_id = ?", voucher.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM schools WHERE id = ?", school.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id IN (?, ?)", voucher.ID, nonMember.ID)
	})

	body := map[string]string{"identifier": nonMember.Email}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/schools/"+school.ID+"/vouch", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.URL.RawQuery = "id=" + school.ID
	claims := &middleware.Claims{UserID: voucher.ID}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	suite.handler.Vouch(rec, req)

	if rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
		t.Error("Expected vouch for non-member to be rejected")
	}
	t.Logf("Vouch for non-member: status %d", rec.Code)
}

// TestSchoolEdge_DuplicateJoin tests that a user cannot join a school twice (S7).
func TestSchoolEdge_DuplicateJoin(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	user := suite.createTestUser("edge-school-dupjoin", models.TierUnverified)
	school := createEdgeTestSchool(t, suite,"Edge Duplicate Join School")

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_schools WHERE user_id = ?", user.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM schools WHERE id = ?", school.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", user.ID)
	})

	makeJoinReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/schools/"+school.ID+"/join", nil)
		req.URL.RawQuery = "id=" + school.ID
		claims := &middleware.Claims{UserID: user.ID}
		ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		suite.handler.Join(rec, req)
		return rec
	}

	// First join should succeed
	rec1 := makeJoinReq()
	if rec1.Code != http.StatusOK && rec1.Code != http.StatusCreated {
		t.Fatalf("First join should succeed, got %d: %s", rec1.Code, rec1.Body.String())
	}

	// Second join should be rejected
	rec2 := makeJoinReq()
	if rec2.Code == http.StatusOK || rec2.Code == http.StatusCreated {
		t.Error("Expected duplicate join to be rejected")
	}
	t.Logf("Duplicate join: status %d", rec2.Code)
}

// TestSchoolEdge_DuplicateVouch tests that a user cannot vouch for same member
// twice (S12).
func TestSchoolEdge_DuplicateVouch(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	voucher := suite.createTestUser("edge-school-dupvouch-v", models.TierUnverified)
	vouchee := suite.createTestUser("edge-school-dupvouch-e", models.TierUnverified)
	school := createEdgeTestSchool(t, suite,"Edge Duplicate Vouch School")
	joinEdgeUserToSchool(t, suite,voucher.ID, school.ID)
	joinEdgeUserToSchool(t, suite,vouchee.ID, school.ID)

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM school_vouches WHERE voucher_id = ?", voucher.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_schools WHERE user_id IN (?, ?)", voucher.ID, vouchee.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM schools WHERE id = ?", school.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id IN (?, ?)", voucher.ID, vouchee.ID)
	})

	makeVouchReq := func() *httptest.ResponseRecorder {
		body := map[string]string{"identifier": vouchee.Email}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/v1/schools/"+school.ID+"/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.URL.RawQuery = "id=" + school.ID
		claims := &middleware.Claims{UserID: voucher.ID}
		ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)
		return rec
	}

	// First vouch
	rec1 := makeVouchReq()
	t.Logf("First school vouch: status %d", rec1.Code)

	// Second vouch should fail
	rec2 := makeVouchReq()
	if rec2.Code == http.StatusOK || rec2.Code == http.StatusCreated {
		t.Error("Expected duplicate vouch to be rejected")
	}
	t.Logf("Duplicate school vouch: status %d, body: %s", rec2.Code, rec2.Body.String())
}

// TestSchoolEdge_VouchForBlockedUser tests vouching for a user who is blocked
// in the school (S4).
func TestSchoolEdge_VouchForBlockedUser(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	voucher := suite.createTestUser("edge-school-vouchblk-v", models.TierUnverified)
	blocked := suite.createTestUser("edge-school-vouchblk-b", models.TierUnverified)
	school := createEdgeTestSchool(t, suite,"Edge Blocked Vouch School")
	joinEdgeUserToSchool(t, suite,voucher.ID, school.ID)
	joinEdgeUserToSchool(t, suite,blocked.ID, school.ID)

	// Block the user in the school
	_, _ = suite.db.ExecContext(context.Background(),
		`INSERT INTO school_blocked_users (id, school_id, user_id, blocked_by, reason, created_at)
		 VALUES (?, ?, ?, ?, 'test block', NOW())`,
		uuid.New().String(), school.ID, blocked.ID, voucher.ID)

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM school_blocked_users WHERE school_id = ?", school.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_schools WHERE user_id IN (?, ?)", voucher.ID, blocked.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM schools WHERE id = ?", school.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id IN (?, ?)", voucher.ID, blocked.ID)
	})

	body := map[string]string{"identifier": blocked.Email}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/schools/"+school.ID+"/vouch", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.URL.RawQuery = "id=" + school.ID
	claims := &middleware.Claims{UserID: voucher.ID}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	suite.handler.Vouch(rec, req)

	if rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
		t.Error("Expected vouch for blocked user to be rejected")
	}
	t.Logf("Vouch for blocked user: status %d", rec.Code)
}

// TestSchoolEdge_VouchLookupMethods tests vouch lookup by email, username, and
// user ID (S11).
func TestSchoolEdge_VouchLookupMethods(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	voucher := suite.createTestUser("edge-school-lookup-v", models.TierUnverified)
	vouchee := suite.createTestUser("edge-school-lookup-e", models.TierUnverified)
	school := createEdgeTestSchool(t, suite,"Edge Lookup School")
	joinEdgeUserToSchool(t, suite,voucher.ID, school.ID)
	joinEdgeUserToSchool(t, suite,vouchee.ID, school.ID)

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM school_vouches WHERE voucher_id = ?", voucher.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_schools WHERE user_id IN (?, ?)", voucher.ID, vouchee.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM schools WHERE id = ?", school.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id IN (?, ?)", voucher.ID, vouchee.ID)
	})

	identifiers := []struct {
		name string
		id   string
	}{
		{"by email", vouchee.Email},
		{"by username", vouchee.Username},
		{"by user ID", vouchee.ID},
	}

	for _, tt := range identifiers {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]string{"identifier": tt.id}
			bodyBytes, _ := json.Marshal(body)
			req := httptest.NewRequest("POST", "/api/v1/schools/"+school.ID+"/vouch", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			req.URL.RawQuery = "id=" + school.ID
			claims := &middleware.Claims{UserID: voucher.ID}
			ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			suite.handler.Vouch(rec, req)

			t.Logf("School vouch %s: status %d", tt.name, rec.Code)
		})
	}
}

// Helper functions for school edge tests

func createEdgeTestSchool(t *testing.T, suite *schoolTestSuite, name string) *models.School {
	school := &models.School{
		ID:     uuid.New().String(),
		NCESID: uuid.New().String()[:12],
		Name:   name,
		State:  "CA",
	}
	_, err := suite.db.ExecContext(context.Background(),
		`INSERT INTO schools (id, nces_id, name, state, created_at) VALUES (?, ?, ?, ?, NOW())`,
		school.ID, school.NCESID, school.Name, school.State)
	if err != nil {
		t.Fatalf("Failed to create test school: %v", err)
	}
	return school
}

func joinEdgeUserToSchool(t *testing.T, suite *schoolTestSuite, userID, schoolID string) {
	_, err := suite.db.ExecContext(context.Background(),
		`INSERT INTO user_schools (id, user_id, school_id, status, created_at) VALUES (?, ?, ?, 'pending', NOW())`,
		uuid.New().String(), userID, schoolID)
	if err != nil {
		t.Fatalf("Failed to join user to school: %v", err)
	}
}

// Suppress unused import warnings
var _ = database.NewSchoolRepository
