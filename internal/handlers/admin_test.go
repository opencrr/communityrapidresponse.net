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

type adminTestSuite struct {
	t          *testing.T
	db         *database.DB
	userRepo   *database.UserRepository
	regionRepo *database.RegionRepository
	handler    *AdminHandler
}

func setupAdminTestSuite(t *testing.T) *adminTestSuite {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	regionRepo := database.NewRegionRepository(db)
	handler := NewAdminHandler(userRepo, regionRepo, nil)

	return &adminTestSuite{
		t:          t,
		db:         db,
		userRepo:   userRepo,
		regionRepo: regionRepo,
		handler:    handler,
	}
}

func (s *adminTestSuite) createTestUser(username string, isSuperuser bool, isBlocked bool) *models.User {
	user := &models.User{
		Username:         username,
		Email:            username + "@admintest.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: models.TierUnverified,
		IsSuperuser:      isSuperuser,
		IsBlocked:        isBlocked,
	}
	if err := s.userRepo.Create(context.Background(), user); err != nil {
		s.t.Fatalf("Failed to create test user: %v", err)
	}
	return user
}

func (s *adminTestSuite) cleanup(userIDs ...string) {
	ctx := context.Background()
	for _, id := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM verification_requests WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM vouches WHERE voucher_user_id = ? OR vouched_user_id = ?", id, id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	}
}

// =============================================================================
// ListUsers Tests
// =============================================================================

func TestAdminHandler_ListUsers(t *testing.T) {
	suite := setupAdminTestSuite(t)

	// Clean up test users
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@admintest.com'")

	superuser := suite.createTestUser("admin_super", true, false)
	regularUser := suite.createTestUser("admin_regular", false, false)
	testUser1 := suite.createTestUser("admin_search1", false, false)
	testUser2 := suite.createTestUser("admin_search2", false, false)

	defer suite.cleanup(superuser.ID, regularUser.ID, testUser1.ID, testUser2.ID)

	t.Run("superuser can list users", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/admin/users", nil)
		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListUsers(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if _, ok := body["users"]; !ok {
			t.Error("Expected 'users' key in response")
		}
		if _, ok := body["total"]; !ok {
			t.Error("Expected 'total' key in response")
		}
	})

	t.Run("regular user cannot list users", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/admin/users", nil)
		claims := &middleware.Claims{
			UserID:      regularUser.ID,
			Email:       regularUser.Email,
			IsSuperuser: false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListUsers(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})

	t.Run("unauthenticated request fails", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/admin/users", nil)
		rec := httptest.NewRecorder()
		suite.handler.ListUsers(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})

	t.Run("search filters users", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/admin/users?q=search1", nil)
		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListUsers(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		users := body["users"].([]interface{})
		if len(users) == 0 {
			t.Error("Expected to find at least one user matching 'search1'")
		}
	})

	t.Run("pagination works correctly", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/admin/users?page=1&limit=2", nil)
		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListUsers(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if body["page"].(float64) != 1 {
			t.Errorf("Expected page 1, got %v", body["page"])
		}
		if body["limit"].(float64) != 2 {
			t.Errorf("Expected limit 2, got %v", body["limit"])
		}
	})

	t.Run("invalid page defaults to 1", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/admin/users?page=0", nil)
		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListUsers(rec, req)

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if body["page"].(float64) != 1 {
			t.Errorf("Expected page to default to 1, got %v", body["page"])
		}
	})

	t.Run("limit over 100 defaults to 20", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/admin/users?limit=500", nil)
		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListUsers(rec, req)

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if body["limit"].(float64) != 20 {
			t.Errorf("Expected limit to default to 20, got %v", body["limit"])
		}
	})
}

// =============================================================================
// GetUser Tests
// =============================================================================

func TestAdminHandler_GetUser(t *testing.T) {
	suite := setupAdminTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@admintest.com'")

	superuser := suite.createTestUser("getuser_super", true, false)
	targetUser := suite.createTestUser("getuser_target", false, false)

	defer suite.cleanup(superuser.ID, targetUser.ID)

	t.Run("superuser can get user details", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/admin/users/"+targetUser.ID, nil)
		q := req.URL.Query()
		q.Set("id", targetUser.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetUser(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		user := body["user"].(map[string]interface{})
		if user["id"] != targetUser.ID {
			t.Errorf("Expected user ID %s, got %v", targetUser.ID, user["id"])
		}
	})

	t.Run("returns 404 for non-existent user", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/admin/users/nonexistent-id", nil)
		q := req.URL.Query()
		q.Set("id", "nonexistent-id")
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetUser(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rec.Code)
		}
	})

	t.Run("returns 400 for missing user ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/admin/users/", nil)
		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetUser(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})
}

// =============================================================================
// GrantVouchVerification Tests
// =============================================================================

func TestAdminHandler_GrantVouchVerification(t *testing.T) {
	suite := setupAdminTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@admintest.com'")

	superuser := suite.createTestUser("grant_super", true, false)
	targetUser := suite.createTestUser("grant_target", false, false)

	defer suite.cleanup(superuser.ID, targetUser.ID)

	t.Run("superuser can grant vouch verification", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/admin/users/"+targetUser.ID+"/grant-vouch", nil)
		q := req.URL.Query()
		q.Set("id", targetUser.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GrantVouchVerification(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if body["success"] != true {
			t.Error("Expected success to be true")
		}

		user := body["user"].(map[string]interface{})
		if user["vouch_verified"] != true {
			t.Error("Expected vouch_verified to be true")
		}
	})

	t.Run("non-superuser cannot grant vouch", func(t *testing.T) {
		regularUser := suite.createTestUser("grant_regular", false, false)
		defer suite.cleanup(regularUser.ID)

		req := httptest.NewRequest("POST", "/api/v1/admin/users/"+targetUser.ID+"/grant-vouch", nil)
		q := req.URL.Query()
		q.Set("id", targetUser.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      regularUser.ID,
			Email:       regularUser.Email,
			IsSuperuser: false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GrantVouchVerification(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})

	t.Run("returns 404 for non-existent user", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/admin/users/nonexistent/grant-vouch", nil)
		q := req.URL.Query()
		q.Set("id", "nonexistent")
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GrantVouchVerification(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rec.Code)
		}
	})
}

// =============================================================================
// RevokeVouchVerification Tests
// =============================================================================

func TestAdminHandler_RevokeVouchVerification(t *testing.T) {
	suite := setupAdminTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@admintest.com'")

	superuser := suite.createTestUser("revoke_super", true, false)
	targetUser := suite.createTestUser("revoke_target", false, false)
	// Set vouch verified
	_ = suite.userRepo.SetVouchVerified(context.Background(), targetUser.ID, true)

	defer suite.cleanup(superuser.ID, targetUser.ID)

	t.Run("superuser can revoke vouch verification", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/admin/users/"+targetUser.ID+"/revoke-vouch", nil)
		q := req.URL.Query()
		q.Set("id", targetUser.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RevokeVouchVerification(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		user := body["user"].(map[string]interface{})
		if user["vouch_verified"] != false {
			t.Error("Expected vouch_verified to be false")
		}
	})
}

// =============================================================================
// BlockUser Tests
// =============================================================================

func TestAdminHandler_BlockUser(t *testing.T) {
	suite := setupAdminTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@admintest.com'")

	superuser := suite.createTestUser("block_super", true, false)
	targetUser := suite.createTestUser("block_target", false, false)
	anotherSuperuser := suite.createTestUser("block_super2", true, false)

	defer suite.cleanup(superuser.ID, targetUser.ID, anotherSuperuser.ID)

	t.Run("superuser can block regular user", func(t *testing.T) {
		body := map[string]string{"reason": "Test block reason"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/admin/users/"+targetUser.ID+"/block", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		q := req.URL.Query()
		q.Set("id", targetUser.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.BlockUser(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var respBody map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&respBody)

		user := respBody["user"].(map[string]interface{})
		if user["is_blocked"] != true {
			t.Error("Expected is_blocked to be true")
		}
	})

	t.Run("cannot block yourself", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/admin/users/"+superuser.ID+"/block", nil)
		q := req.URL.Query()
		q.Set("id", superuser.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.BlockUser(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["error"] != "invalid_request" {
			t.Errorf("Expected error 'invalid_request', got %v", body["error"])
		}
	})

	t.Run("cannot block another superuser", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/admin/users/"+anotherSuperuser.ID+"/block", nil)
		q := req.URL.Query()
		q.Set("id", anotherSuperuser.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.BlockUser(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("block without reason succeeds", func(t *testing.T) {
		newUser := suite.createTestUser("block_noreason", false, false)
		defer suite.cleanup(newUser.ID)

		req := httptest.NewRequest("POST", "/api/v1/admin/users/"+newUser.ID+"/block", nil)
		q := req.URL.Query()
		q.Set("id", newUser.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.BlockUser(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})
}

// =============================================================================
// UnblockUser Tests
// =============================================================================

func TestAdminHandler_UnblockUser(t *testing.T) {
	suite := setupAdminTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@admintest.com'")

	superuser := suite.createTestUser("unblock_super", true, false)
	blockedUser := suite.createTestUser("unblock_target", false, true)

	defer suite.cleanup(superuser.ID, blockedUser.ID)

	t.Run("superuser can unblock user", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/admin/users/"+blockedUser.ID+"/unblock", nil)
		q := req.URL.Query()
		q.Set("id", blockedUser.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.UnblockUser(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		user := body["user"].(map[string]interface{})
		if user["is_blocked"] != false {
			t.Error("Expected is_blocked to be false")
		}
	})

	t.Run("returns 404 for non-existent user", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/admin/users/nonexistent/unblock", nil)
		q := req.URL.Query()
		q.Set("id", "nonexistent")
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.UnblockUser(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rec.Code)
		}
	})
}

// =============================================================================
// DeleteUser Tests
// =============================================================================

func TestAdminHandler_DeleteUser(t *testing.T) {
	suite := setupAdminTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@admintest.com'")

	superuser := suite.createTestUser("delete_super", true, false)
	anotherSuperuser := suite.createTestUser("delete_super2", true, false)

	defer suite.cleanup(superuser.ID, anotherSuperuser.ID)

	t.Run("superuser can delete regular user", func(t *testing.T) {
		targetUser := suite.createTestUser("delete_target", false, false)

		req := httptest.NewRequest("DELETE", "/api/v1/admin/users/"+targetUser.ID, nil)
		q := req.URL.Query()
		q.Set("id", targetUser.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.DeleteUser(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)

		if body["success"] != true {
			t.Error("Expected success to be true")
		}

		// Verify user was actually deleted
		_, err := suite.userRepo.GetByID(context.Background(), targetUser.ID)
		if err == nil {
			t.Error("Expected user to be deleted")
		}
	})

	t.Run("cannot delete yourself", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v1/admin/users/"+superuser.ID, nil)
		q := req.URL.Query()
		q.Set("id", superuser.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.DeleteUser(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("cannot delete another superuser", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v1/admin/users/"+anotherSuperuser.ID, nil)
		q := req.URL.Query()
		q.Set("id", anotherSuperuser.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.DeleteUser(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("returns 404 for non-existent user", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v1/admin/users/nonexistent", nil)
		q := req.URL.Query()
		q.Set("id", "nonexistent")
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      superuser.ID,
			Email:       superuser.Email,
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.DeleteUser(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rec.Code)
		}
	})

	t.Run("non-superuser cannot delete users", func(t *testing.T) {
		regularUser := suite.createTestUser("delete_regular", false, false)
		targetUser := suite.createTestUser("delete_target2", false, false)
		defer suite.cleanup(regularUser.ID, targetUser.ID)

		req := httptest.NewRequest("DELETE", "/api/v1/admin/users/"+targetUser.ID, nil)
		q := req.URL.Query()
		q.Set("id", targetUser.ID)
		req.URL.RawQuery = q.Encode()

		claims := &middleware.Claims{
			UserID:      regularUser.ID,
			Email:       regularUser.Email,
			IsSuperuser: false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.DeleteUser(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})
}

// =============================================================================
// RequireSuperuser Defense-in-Depth Tests
// =============================================================================

func TestAdminHandler_RequireSuperuser_DBRecheck(t *testing.T) {
	suite := setupAdminTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@admintest.com'")

	// Create a regular user (not superuser in DB)
	regularUser := suite.createTestUser("recheck_regular", false, false)
	defer suite.cleanup(regularUser.ID)

	t.Run("rejects user with stale superuser claim", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/admin/users", nil)
		// Set claims with IsSuperuser: true (stale claim)
		// but the user is NOT a superuser in the database
		claims := &middleware.Claims{
			UserID:      regularUser.ID,
			Email:       regularUser.Email,
			IsSuperuser: true, // Stale claim — DB says false
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListUsers(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403 (DB re-check), got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects deleted superuser with stale claim", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/admin/users", nil)
		// Use a non-existent user ID — simulates deleted user
		claims := &middleware.Claims{
			UserID:      "nonexistent-superuser-id",
			Email:       "gone@example.com",
			IsSuperuser: true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.ListUsers(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403 for deleted superuser, got %d", rec.Code)
		}
	})
}
