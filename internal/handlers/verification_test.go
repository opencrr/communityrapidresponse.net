package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/mocks"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

type verificationTestSuite struct {
	t                *testing.T
	db               *database.DB
	userRepo         *database.UserRepository
	regionRepo       *database.RegionRepository
	verificationRepo *database.VerificationRepository
	vouchRepo        *database.VouchRepository
	postgridService  *mocks.MockPostgridService
	mapboxService    *mocks.MockMapboxService
	handler          *VerificationHandler
}

func setupVerificationTestSuite(t *testing.T) *verificationTestSuite {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	regionRepo := database.NewRegionRepository(db)
	verificationRepo := database.NewVerificationRepository(db)
	vouchRepo := database.NewVouchRepository(db)
	postgridService := mocks.NewMockPostgridService()
	mapboxService := mocks.NewMockMapboxService()

	handler := NewVerificationHandler(
		nil,
		verificationRepo,
		vouchRepo,
		userRepo,
		regionRepo,
		postgridService,
		mapboxService,
		nil,
		false, 30, // Bootstrap cooldown disabled for tests
	)

	return &verificationTestSuite{
		t:                t,
		db:               db,
		userRepo:         userRepo,
		regionRepo:       regionRepo,
		verificationRepo: verificationRepo,
		vouchRepo:        vouchRepo,
		postgridService:  postgridService,
		mapboxService:    mapboxService,
		handler:          handler,
	}
}

func (s *verificationTestSuite) createTestUser(username string, tier models.VerificationTier) *models.User {
	user := &models.User{
		Username:         username,
		Email:            username + "@verificationtest.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: tier,
	}
	if err := s.userRepo.Create(context.Background(), user); err != nil {
		s.t.Fatalf("Failed to create test user: %v", err)
	}
	return user
}

func (s *verificationTestSuite) createTestRegion(name string, regionType models.RegionType, parentID *string, createdBy string) *models.GeographicRegion {
	region := &models.GeographicRegion{
		ID:             uuid.New().String(),
		Name:           name,
		RegionType:     regionType,
		ParentRegionID: parentID,
		CreatedBy:      &createdBy,
		CreatedAt:      time.Now().UTC(),
	}

	// Create a simple polygon around the default mock coordinates (San Francisco area)
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.3,37.7],[-122.3,37.85],[-122.5,37.85],[-122.5,37.7]]]}`

	query := `
		INSERT INTO geographic_regions (id, name, region_type, parent_region_id, geometry, created_by, created_at)
		VALUES (?, ?, ?, ?, ST_GeomFromGeoJSON(?), ?, ?)
	`
	_, err := s.db.ExecContext(context.Background(), query,
		region.ID, region.Name, region.RegionType, region.ParentRegionID, geoJSON, region.CreatedBy, region.CreatedAt)
	if err != nil {
		s.t.Fatalf("Failed to create test region: %v", err)
	}

	return region
}

func (s *verificationTestSuite) createVerificationRequest(userID, regionID string, code string, status models.VerificationStatus) *models.VerificationRequest {
	req := &models.VerificationRequest{
		ID:                uuid.New().String(),
		UserID:            userID,
		RegionID:          &regionID,
		VerificationCode:  code,
		Status:            status,
		PostgridRequestID: fmt.Sprintf("mock_postcard_%s", code),
		BoundaryType:      "city",
		BoundaryName:      "San Francisco",
		BoundaryState:     "California",
		CreatedAt:         time.Now().UTC(),
		ExpiresAt:         time.Now().UTC().Add(30 * 24 * time.Hour), // 30 days
	}

	query := `
		INSERT INTO verification_requests (id, user_id, region_id, verification_code, status,
			postgrid_request_id, boundary_type, boundary_name, boundary_state, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(context.Background(), query,
		req.ID, req.UserID, req.RegionID, req.VerificationCode, req.Status,
		req.PostgridRequestID, req.BoundaryType, req.BoundaryName, req.BoundaryState, req.CreatedAt, req.ExpiresAt)
	if err != nil {
		s.t.Fatalf("Failed to create verification request: %v", err)
	}

	return req
}

func (s *verificationTestSuite) createVouch(voucherID, vouchedID, regionID string) *models.Vouch {
	vouch := &models.Vouch{
		ID:            uuid.New().String(),
		VoucherUserID: voucherID,
		VouchedUserID: vouchedID,
		RegionID:      &regionID,
		CreatedAt:     time.Now().UTC(),
	}

	query := `INSERT INTO vouches (id, voucher_user_id, vouched_user_id, region_id, created_at) VALUES (?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(context.Background(), query,
		vouch.ID, vouch.VoucherUserID, vouch.VouchedUserID, vouch.RegionID, vouch.CreatedAt)
	if err != nil {
		s.t.Fatalf("Failed to create vouch: %v", err)
	}

	return vouch
}

func (s *verificationTestSuite) addUserToRegion(userID, regionID string, isAdmin bool) {
	id := uuid.New().String()
	query := `INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (?, ?, ?, ?, 'verified', ?)`
	_, err := s.db.ExecContext(context.Background(), query, id, userID, regionID, isAdmin, time.Now().UTC())
	if err != nil {
		s.t.Fatalf("Failed to add user to region: %v", err)
	}
}

func (s *verificationTestSuite) addUserToRegionPending(userID, regionID string) {
	id := uuid.New().String()
	query := `INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at) VALUES (?, ?, ?, FALSE, 'pending', ?)`
	_, err := s.db.ExecContext(context.Background(), query, id, userID, regionID, time.Now().UTC())
	if err != nil {
		s.t.Fatalf("Failed to add user to region as pending: %v", err)
	}
}

// exitBootstrapMode creates 3 full admins (postcard + vouch verified) in a region
// to take it out of bootstrap mode. Returns the user IDs for cleanup.
func (s *verificationTestSuite) exitBootstrapMode(regionID string) []string {
	var userIDs []string
	for i := 0; i < 3; i++ {
		user := s.createTestUserWithFlags(fmt.Sprintf("fulladmin%d_%s", i, regionID[:8]), true, true)
		s.addUserToRegion(user.ID, regionID, true)
		userIDs = append(userIDs, user.ID)
	}
	return userIDs
}

func (s *verificationTestSuite) createTestUserWithFlags(username string, postcardVerified, vouchVerified bool) *models.User {
	tier := models.TierUnverified
	if postcardVerified && vouchVerified {
		tier = models.TierPostcard // Has both, will be treated as admin
	} else if postcardVerified {
		tier = models.TierPostcard
	} else if vouchVerified {
		tier = models.TierVouched
	}

	user := &models.User{
		Username:         username,
		Email:            username + "@verificationtest.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: tier,
		PostcardVerified: postcardVerified,
		VouchVerified:    vouchVerified,
	}
	if err := s.userRepo.Create(context.Background(), user); err != nil {
		s.t.Fatalf("Failed to create test user: %v", err)
	}

	// Update the flags directly in case Create doesn't set them
	_, err := s.db.ExecContext(context.Background(),
		"UPDATE users SET postcard_verified = ?, vouch_verified = ? WHERE id = ?",
		postcardVerified, vouchVerified, user.ID)
	if err != nil {
		s.t.Fatalf("Failed to update user flags: %v", err)
	}

	return user
}

func (s *verificationTestSuite) blockUserInRegion(userID, regionID string) {
	id := uuid.New().String()
	query := `INSERT INTO blocked_users (id, user_id, region_id, blocked_at) VALUES (?, ?, ?, ?)`
	_, err := s.db.ExecContext(context.Background(), query, id, userID, regionID, time.Now().UTC())
	if err != nil {
		s.t.Fatalf("Failed to block user: %v", err)
	}
}

func (s *verificationTestSuite) cleanupBlockedUsers(userID string) {
	_, _ = s.db.ExecContext(context.Background(), "DELETE FROM blocked_users WHERE user_id = ?", userID)
}

func (s *verificationTestSuite) cleanup(userIDs ...string) {
	ctx := context.Background()
	for _, id := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM vouches WHERE voucher_user_id = ? OR vouched_user_id = ?", id, id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM verification_requests WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	}
}

func (s *verificationTestSuite) cleanupRegions(regionIDs ...string) {
	ctx := context.Background()
	for _, id := range regionIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE region_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM vouches WHERE region_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM verification_requests WHERE region_id = ?", id)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", id)
	}
}

// =============================================================================
// RequestPostcardVerification Tests
// =============================================================================

func TestVerificationHandler_RequestPostcardVerification(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	// Clean up test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@verificationtest.com'")
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM geographic_regions WHERE name LIKE '%Test Region%' OR name LIKE '%San Francisco%'")

	user := suite.createTestUser("verif_user1", models.TierUnverified)
	region := suite.createTestRegion("Test Region City", models.RegionTypeCity, nil, user.ID)

	defer suite.cleanup(user.ID)
	defer suite.cleanupRegions(region.ID)

	t.Run("postcard request requires vouch verification", func(t *testing.T) {
		suite.postgridService.Reset()
		suite.mapboxService.Reset()

		body := map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 Main St",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierUnverified,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403 for non-vouch-verified user, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("successful postcard verification request", func(t *testing.T) {
		suite.postgridService.Reset()
		suite.mapboxService.Reset()

		body := map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 Main St",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		// Vouch-verified user (postcard requires vouch first)
		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierVouched,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if _, ok := resp["verification_id"]; !ok {
			t.Error("Expected 'verification_id' in response")
		}
		if status, ok := resp["status"].(string); !ok || status != "mailed" {
			t.Errorf("Expected status 'mailed', got %v", resp["status"])
		}
		if _, ok := resp["privacy_notice"]; !ok {
			t.Error("Expected 'privacy_notice' in response")
		}

		// Verify mock services were called
		if len(suite.postgridService.ValidateAddressCalls) != 1 {
			t.Errorf("Expected 1 ValidateAddress call, got %d", len(suite.postgridService.ValidateAddressCalls))
		}
		if len(suite.postgridService.SendPostcardCalls) != 1 {
			t.Errorf("Expected 1 SendPostcard call, got %d", len(suite.postgridService.SendPostcardCalls))
		}
		if len(suite.mapboxService.GeocodeAddressCalls) != 1 {
			t.Errorf("Expected 1 GeocodeAddress call, got %d", len(suite.mapboxService.GeocodeAddressCalls))
		}

		// Clean up verification request
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE user_id = ?", user.ID)
	})

	t.Run("unauthenticated request fails", func(t *testing.T) {
		body := map[string]interface{}{
			"address": map[string]string{
				"line1":       "123 Main St",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		suite.handler.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})

	t.Run("invalid address fails validation", func(t *testing.T) {
		body := map[string]interface{}{
			"address": map[string]string{
				"line1": "123 Main St",
				// Missing required fields
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierVouched,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("PO Box address is rejected", func(t *testing.T) {
		suite.postgridService.Reset()

		body := map[string]interface{}{
			"address": map[string]string{
				"line1":       "PO Box 123",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierVouched,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp["error"] != "po_box_not_allowed" {
			t.Errorf("Expected error 'po_box_not_allowed', got %v", resp["error"])
		}
	})

	t.Run("CMRA address is rejected", func(t *testing.T) {
		suite.postgridService.Reset()
		suite.postgridService.DefaultIsCMRA = true

		body := map[string]interface{}{
			"address": map[string]string{
				"line1":       "123 Main St #456",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierVouched,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp["error"] != "cmra_not_allowed" {
			t.Errorf("Expected error 'cmra_not_allowed', got %v", resp["error"])
		}

		suite.postgridService.DefaultIsCMRA = false
	})

	t.Run("non-deliverable address is rejected", func(t *testing.T) {
		suite.postgridService.Reset()
		suite.postgridService.DefaultDeliverable = false

		body := map[string]interface{}{
			"address": map[string]string{
				"line1":       "999 Fake St",
				"city":        "Nowhere",
				"state":       "XX",
				"postal_code": "00000",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierVouched,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp["error"] != "invalid_address" {
			t.Errorf("Expected error 'invalid_address', got %v", resp["error"])
		}

		suite.postgridService.DefaultDeliverable = true
	})

	t.Run("rate limit enforced", func(t *testing.T) {
		rateLimitUser := suite.createTestUser("verif_ratelimit", models.TierUnverified)
		defer suite.cleanup(rateLimitUser.ID)

		// Create 3 verification requests (the limit)
		for i := 0; i < 3; i++ {
			suite.createVerificationRequest(rateLimitUser.ID, region.ID, fmt.Sprintf("code%d", i), models.VerificationStatusMailed)
		}

		suite.postgridService.Reset()
		body := map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 Main St",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           rateLimitUser.ID,
			Email:            rateLimitUser.Email,
			VerificationTier: models.TierVouched,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("Expected status 429, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify SendPostcard was NOT called (rate limit should reject before sending)
		if len(suite.postgridService.SendPostcardCalls) != 0 {
			t.Errorf("Expected 0 SendPostcard calls when rate limited, got %d", len(suite.postgridService.SendPostcardCalls))
		}

		// Clean up
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE user_id = ?", rateLimitUser.ID)
	})

	t.Run("rate limit with locked check prevents postcard send", func(t *testing.T) {
		// Create a handler with db set so the locked rate-limit check runs
		handlerWithDB := NewVerificationHandler(
			suite.db,
			suite.verificationRepo,
			suite.vouchRepo,
			suite.userRepo,
			suite.regionRepo,
			suite.postgridService,
			suite.mapboxService,
			nil,
			false, 30,
		)

		rateLimitUser := suite.createTestUser("verif_ratelimit_locked", models.TierUnverified)
		defer suite.cleanup(rateLimitUser.ID)

		// Create 3 verification requests (the limit)
		for i := 0; i < 3; i++ {
			suite.createVerificationRequest(rateLimitUser.ID, region.ID, fmt.Sprintf("locked_code%d", i), models.VerificationStatusMailed)
		}

		suite.postgridService.Reset()
		body := map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 Main St",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           rateLimitUser.ID,
			Email:            rateLimitUser.Email,
			VerificationTier: models.TierVouched,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handlerWithDB.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("Expected status 429, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify SendPostcard was NOT called (locked rate limit should reject before sending)
		if len(suite.postgridService.SendPostcardCalls) != 0 {
			t.Errorf("Expected 0 SendPostcard calls when rate limited, got %d", len(suite.postgridService.SendPostcardCalls))
		}

		// Clean up
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE user_id = ?", rateLimitUser.ID)
	})

	t.Run("postgrid service failure handled", func(t *testing.T) {
		suite.postgridService.Reset()
		suite.postgridService.ShouldFail = true

		body := map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 Main St",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierVouched,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusBadGateway {
			t.Errorf("Expected status 502, got %d: %s", rec.Code, rec.Body.String())
		}

		suite.postgridService.ShouldFail = false
	})

	t.Run("send postcard failure cleans up reserved row", func(t *testing.T) {
		// Use a handler with db so the atomic insert runs
		handlerWithDB := NewVerificationHandler(
			suite.db,
			suite.verificationRepo,
			suite.vouchRepo,
			suite.userRepo,
			suite.regionRepo,
			suite.postgridService,
			suite.mapboxService,
			nil,
			false, 30,
		)

		sendFailUser := suite.createTestUser("verif_sendfail", models.TierUnverified)
		defer suite.cleanup(sendFailUser.ID)

		suite.postgridService.Reset()
		// Only fail SendPostcard, not ValidateAddress
		defer func() { suite.postgridService.SendPostcardFunc = nil }()
		suite.postgridService.SendPostcardFunc = func(ctx context.Context, address *models.Address, verificationCode, postcardRef string) (string, error) {
			return "", fmt.Errorf("mock send postcard error")
		}

		body := map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 Main St",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           sendFailUser.ID,
			Email:            sendFailUser.Email,
			VerificationTier: models.TierVouched,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handlerWithDB.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusBadGateway {
			t.Errorf("Expected status 502, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify the reserved row was cleaned up
		var count int
		err := suite.db.QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM verification_requests WHERE user_id = ?", sendFailUser.ID).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to query verification_requests: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected 0 verification_requests after SendPostcard failure cleanup, got %d", count)
		}

		// Clean up (defensive, in case the delete failed)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE user_id = ?", sendFailUser.ID)
	})

	t.Run("address outside region rejected", func(t *testing.T) {
		suite.postgridService.Reset()
		suite.mapboxService.Reset()
		// Set mock coordinates that are outside the test region
		suite.mapboxService.DefaultLatitude = 40.7128  // New York
		suite.mapboxService.DefaultLongitude = -74.006

		body := map[string]interface{}{
			"region_id": region.ID,
			"address": map[string]string{
				"line1":       "123 Broadway",
				"city":        "New York",
				"state":       "NY",
				"postal_code": "10001",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierVouched,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp["error"] != "address_outside_region" {
			t.Errorf("Expected error 'address_outside_region', got %v", resp["error"])
		}

		// Reset mock coordinates
		suite.mapboxService.DefaultLatitude = 37.7749
		suite.mapboxService.DefaultLongitude = -122.4194
	})
}

// =============================================================================
// VerifyCode Tests
// =============================================================================

func TestVerificationHandler_VerifyCode(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	// Clean up test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@verificationtest.com'")

	user := suite.createTestUser("verif_code_user", models.TierUnverified)
	region := suite.createTestRegion("Test Region For Verify", models.RegionTypeCity, nil, user.ID)

	defer suite.cleanup(user.ID)
	defer suite.cleanupRegions(region.ID)

	t.Run("successful code verification", func(t *testing.T) {
		verificationCode := "abc12345"
		verReq := suite.createVerificationRequest(user.ID, region.ID, verificationCode, models.VerificationStatusMailed)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE id = ?", verReq.ID) }()

		body := map[string]interface{}{
			"verification_code": verificationCode,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierUnverified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VerifyCode(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if success, ok := resp["success"].(bool); !ok || !success {
			t.Error("Expected success: true")
		}

		// Verify user tier was updated
		updatedUser, _ := suite.userRepo.GetByID(context.Background(), user.ID)
		if updatedUser.VerificationTier != models.TierPostcard {
			t.Errorf("Expected user tier %d, got %d", models.TierPostcard, updatedUser.VerificationTier)
		}

		// Reset user tier for other tests
		_, _ = suite.db.ExecContext(context.Background(), "UPDATE users SET verification_tier = ? WHERE id = ?", models.TierUnverified, user.ID)
	})

	t.Run("unauthenticated request fails", func(t *testing.T) {
		body := map[string]interface{}{
			"verification_code": "abc12345",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		suite.handler.VerifyCode(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})

	t.Run("invalid code fails", func(t *testing.T) {
		body := map[string]interface{}{
			"verification_code": "invalid_code_xyz",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierUnverified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VerifyCode(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rec.Code)
		}
	})

	t.Run("code belonging to different user fails", func(t *testing.T) {
		otherUser := suite.createTestUser("verif_other_user", models.TierUnverified)
		defer suite.cleanup(otherUser.ID)

		verificationCode := "other123"
		verReq := suite.createVerificationRequest(otherUser.ID, region.ID, verificationCode, models.VerificationStatusMailed)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE id = ?", verReq.ID) }()

		body := map[string]interface{}{
			"verification_code": verificationCode,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		// Using the original user's claims to try verifying the other user's code
		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierUnverified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VerifyCode(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("expired code fails", func(t *testing.T) {
		verificationCode := "expired1"
		verReq := &models.VerificationRequest{
			ID:                uuid.New().String(),
			UserID:            user.ID,
			RegionID:          &region.ID,
			VerificationCode:  verificationCode,
			Status:            models.VerificationStatusMailed,
			PostgridRequestID: fmt.Sprintf("mock_postcard_%s", verificationCode),
			BoundaryType:      "city",
			BoundaryName:      "San Francisco",
			BoundaryState:     "California",
			CreatedAt:         time.Now().UTC().Add(-60 * 24 * time.Hour), // 60 days ago
			ExpiresAt:         time.Now().UTC().Add(-30 * 24 * time.Hour), // Expired 30 days ago
		}

		query := `
			INSERT INTO verification_requests (id, user_id, region_id, verification_code, status,
				postgrid_request_id, boundary_type, boundary_name, boundary_state, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`
		_, _ = suite.db.ExecContext(context.Background(), query,
			verReq.ID, verReq.UserID, verReq.RegionID, verReq.VerificationCode, verReq.Status,
			verReq.PostgridRequestID, verReq.BoundaryType, verReq.BoundaryName, verReq.BoundaryState, verReq.CreatedAt, verReq.ExpiresAt)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE id = ?", verReq.ID) }()

		body := map[string]interface{}{
			"verification_code": verificationCode,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierUnverified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VerifyCode(rec, req)

		if rec.Code != http.StatusGone {
			t.Errorf("Expected status 410, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("already verified code fails", func(t *testing.T) {
		verificationCode := "alreadyv"
		verReq := suite.createVerificationRequest(user.ID, region.ID, verificationCode, models.VerificationStatusVerified)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE id = ?", verReq.ID) }()

		body := map[string]interface{}{
			"verification_code": verificationCode,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierUnverified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VerifyCode(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing code fails", func(t *testing.T) {
		body := map[string]interface{}{
			"verification_code": "",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierUnverified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VerifyCode(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// =============================================================================
// Vouch Tests
// =============================================================================

func TestVerificationHandler_Vouch(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	// Clean up test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@verificationtest.com'")

	// Create a fully verified user (both postcard AND vouch verified) for vouching
	verifiedUser := suite.createTestUserWithFlags("vouch_verified", true, true)
	unverifiedUser := suite.createTestUser("vouch_unverified", models.TierUnverified)
	targetUser := suite.createTestUser("vouch_target", models.TierUnverified)
	region := suite.createTestRegion("Test Vouch Region", models.RegionTypeCity, nil, verifiedUser.ID)

	// Exit bootstrap mode so tests use normal 2-vouch threshold
	adminIDs := suite.exitBootstrapMode(region.ID)

	defer suite.cleanup(verifiedUser.ID, unverifiedUser.ID, targetUser.ID)
	defer suite.cleanupRegions(region.ID)
	defer func() {
		for _, id := range adminIDs {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", id)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", id)
		}
	}()

	t.Run("successful vouch", func(t *testing.T) {
		// Add voucher to region as admin, target as pending (requesting vouch)
		suite.addUserToRegion(verifiedUser.ID, region.ID, true)
		suite.addUserToRegionPending(targetUser.ID, region.ID)

		body := map[string]interface{}{
			"vouched_user_id": targetUser.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if _, ok := resp["vouch_id"]; !ok {
			t.Error("Expected 'vouch_id' in response")
		}
		if totalVouches, ok := resp["total_vouches"].(float64); !ok || totalVouches != 1 {
			t.Errorf("Expected total_vouches = 1, got %v", resp["total_vouches"])
		}

		// Clean up vouch and user_regions
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE voucher_user_id = ?", verifiedUser.ID)
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", verifiedUser.ID, targetUser.ID)
	})

	t.Run("unverified user cannot vouch", func(t *testing.T) {
		body := map[string]interface{}{
			"vouched_user_id": targetUser.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           unverifiedUser.ID,
			Email:            unverifiedUser.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("cannot vouch for self", func(t *testing.T) {
		// Add self as pending to test self-vouch rejection
		suite.addUserToRegionPending(verifiedUser.ID, region.ID)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", verifiedUser.ID) }()

		body := map[string]interface{}{
			"vouched_user_id": verifiedUser.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("cannot double vouch", func(t *testing.T) {
		// Bootstrap mode is already exited at function level
		// Add voucher and target to region, target as pending
		suite.addUserToRegion(verifiedUser.ID, region.ID, true)
		suite.addUserToRegionPending(targetUser.ID, region.ID)
		// Create initial vouch
		suite.createVouch(verifiedUser.ID, targetUser.ID, region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE voucher_user_id = ?", verifiedUser.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", verifiedUser.ID, targetUser.ID)
		}()

		body := map[string]interface{}{
			"vouched_user_id": targetUser.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unauthenticated request fails", func(t *testing.T) {
		body := map[string]interface{}{
			"vouched_user_id": targetUser.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})

	t.Run("missing fields fails", func(t *testing.T) {
		body := map[string]interface{}{
			"vouched_user_id": targetUser.ID,
			// Missing region_id
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("vouch limit enforced", func(t *testing.T) {
		// Add voucher to region
		suite.addUserToRegion(verifiedUser.ID, region.ID, true)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", verifiedUser.ID) }()

		// Create 10 vouches (the monthly limit)
		for i := 0; i < 10; i++ {
			targetUserN := suite.createTestUser(fmt.Sprintf("vouch_target_%d", i), models.TierUnverified)
			suite.createVouch(verifiedUser.ID, targetUserN.ID, region.ID)
			defer suite.cleanup(targetUserN.ID)
		}
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE voucher_user_id = ?", verifiedUser.ID) }()

		newTarget := suite.createTestUser("vouch_over_limit", models.TierUnverified)
		suite.addUserToRegionPending(newTarget.ID, region.ID)
		defer suite.cleanup(newTarget.ID)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", newTarget.ID) }()

		body := map[string]interface{}{
			"vouched_user_id": newTarget.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("Expected status 429, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("circular vouch pattern rejected", func(t *testing.T) {
		// User A vouches for User B, then User B tries to vouch for User A - should fail
		userA := suite.createTestUserWithFlags("circular_a", true, true)
		userB := suite.createTestUserWithFlags("circular_b", true, true)
		defer suite.cleanup(userA.ID, userB.ID)

		// Add both users to the region as verified, but also add them as pending for the vouch test
		suite.addUserToRegion(userA.ID, region.ID, true)
		suite.addUserToRegion(userB.ID, region.ID, true)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", userA.ID, userB.ID) }()

		// User A vouches for User B
		suite.createVouch(userA.ID, userB.ID, region.ID)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE voucher_user_id = ? OR vouched_user_id = ?", userA.ID, userA.ID) }()

		// Create a pending entry for User A so User B can attempt to vouch
		// First remove A's verified entry and add pending
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", userA.ID)
		suite.addUserToRegionPending(userA.ID, region.ID)

		// Now User B tries to vouch for User A - should be rejected as circular
		body := map[string]interface{}{
			"vouched_user_id": userA.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           userB.ID,
			Email:            userB.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp["error"] != "circular_vouch" {
			t.Errorf("Expected error 'circular_vouch', got %v", resp["error"])
		}
	})

	t.Run("voucher not in target region cannot vouch", func(t *testing.T) {
		// Create users where voucher is NOT in the target region
		voucher := suite.createTestUserWithFlags("no_region_voucher", true, true)
		vouchee := suite.createTestUser("no_region_vouchee", models.TierUnverified)
		defer suite.cleanup(voucher.ID, vouchee.ID)

		// Add vouchee as pending in region, but voucher is NOT in any region
		suite.addUserToRegionPending(vouchee.ID, region.ID)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", vouchee.ID) }()

		body := map[string]interface{}{
			"vouched_user_id": vouchee.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           voucher.ID,
			Email:            voucher.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp["error"] != "not_in_region" {
			t.Errorf("Expected error 'not_in_region', got %v", resp["error"])
		}
	})

	t.Run("users sharing region can vouch", func(t *testing.T) {
		// Create users who share a region
		voucher := suite.createTestUserWithFlags("shared_voucher", true, true)
		vouchee := suite.createTestUser("shared_vouchee", models.TierUnverified)
		defer suite.cleanup(voucher.ID, vouchee.ID)

		// Add both users to the same region - voucher verified, vouchee pending
		suite.addUserToRegion(voucher.ID, region.ID, true)
		suite.addUserToRegionPending(vouchee.ID, region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE voucher_user_id = ?", voucher.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", voucher.ID, vouchee.ID)
		}()

		body := map[string]interface{}{
			"vouched_user_id": vouchee.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           voucher.ID,
			Email:            voucher.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("cannot vouch for blocked user", func(t *testing.T) {
		voucher := suite.createTestUserWithFlags("bl_voucher", true, true)
		blockedUser := suite.createTestUser("bl_vouchee", models.TierUnverified)
		defer suite.cleanup(voucher.ID, blockedUser.ID)

		// Add both users to region - vouchee as pending
		suite.addUserToRegion(voucher.ID, region.ID, true)
		suite.addUserToRegionPending(blockedUser.ID, region.ID)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", voucher.ID, blockedUser.ID) }()

		// Block the vouchee in this region
		suite.blockUserInRegion(blockedUser.ID, region.ID)
		defer suite.cleanupBlockedUsers(blockedUser.ID)

		body := map[string]interface{}{
			"vouched_user_id": blockedUser.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           voucher.ID,
			Email:            voucher.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp["error"] != "user_blocked" {
			t.Errorf("Expected error 'user_blocked', got %v", resp["error"])
		}
	})

	t.Run("auto-upgrade on enough vouches", func(t *testing.T) {
		autoUpgradeTarget := suite.createTestUser("vouch_auto_upgrade", models.TierUnverified)
		voucher2 := suite.createTestUserWithFlags("vouch_voucher2", true, true)
		defer suite.cleanup(autoUpgradeTarget.ID, voucher2.ID)

		// Add all users to the region - vouchers verified, target pending
		suite.addUserToRegion(verifiedUser.ID, region.ID, true)
		suite.addUserToRegion(voucher2.ID, region.ID, true)
		suite.addUserToRegionPending(autoUpgradeTarget.ID, region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?, ?)", verifiedUser.ID, voucher2.ID, autoUpgradeTarget.ID)
		}()

		// Create first vouch
		suite.createVouch(verifiedUser.ID, autoUpgradeTarget.ID, region.ID)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE vouched_user_id = ?", autoUpgradeTarget.ID) }()

		// Second vouch should trigger auto-upgrade
		body := map[string]interface{}{
			"vouched_user_id": autoUpgradeTarget.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           voucher2.ID,
			Email:            voucher2.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify user was upgraded
		updatedUser, _ := suite.userRepo.GetByID(context.Background(), autoUpgradeTarget.ID)
		if updatedUser.VerificationTier != models.TierVouched {
			t.Errorf("Expected user tier %d, got %d", models.TierVouched, updatedUser.VerificationTier)
		}
		if !updatedUser.VouchVerified {
			t.Error("Expected vouch_verified to be true")
		}
	})
}

// =============================================================================
// GetVouchStatus Tests
// =============================================================================

func TestVerificationHandler_GetVouchStatus(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	// Clean up test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@verificationtest.com'")

	targetUser := suite.createTestUser("status_target", models.TierUnverified)
	voucher1 := suite.createTestUser("status_voucher1", models.TierPostcard)
	voucher2 := suite.createTestUser("status_voucher2", models.TierPostcard)
	region := suite.createTestRegion("Test Status Region", models.RegionTypeCity, nil, voucher1.ID)

	// Exit bootstrap mode so tests use normal 2-vouch threshold
	adminIDs := suite.exitBootstrapMode(region.ID)

	defer suite.cleanup(targetUser.ID, voucher1.ID, voucher2.ID)
	defer suite.cleanupRegions(region.ID)
	defer func() {
		for _, id := range adminIDs {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", id)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", id)
		}
	}()

	t.Run("get vouch status with vouches", func(t *testing.T) {
		// Create vouches
		suite.createVouch(voucher1.ID, targetUser.ID, region.ID)
		suite.createVouch(voucher2.ID, targetUser.ID, region.ID)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE vouched_user_id = ?", targetUser.ID) }()

		// getPathParam uses query params, so include user_id as query param
		path := fmt.Sprintf("/api/v1/verification/vouch/status?user_id=%s&region_id=%s", targetUser.ID, region.ID)
		req := httptest.NewRequest("GET", path, nil)
		ctx := context.WithValue(req.Context(), middleware.UserContextKey, &middleware.Claims{UserID: voucher1.ID})
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetVouchStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp["user_id"] != targetUser.ID {
			t.Errorf("Expected user_id %s, got %v", targetUser.ID, resp["user_id"])
		}
		if resp["region_id"] != region.ID {
			t.Errorf("Expected region_id %s, got %v", region.ID, resp["region_id"])
		}
		if vouchesReceived, ok := resp["vouches_received"].(float64); !ok || vouchesReceived != 2 {
			t.Errorf("Expected vouches_received = 2, got %v", resp["vouches_received"])
		}
		if vouchesNeeded, ok := resp["vouches_needed"].(float64); !ok || vouchesNeeded != 0 {
			t.Errorf("Expected vouches_needed = 0, got %v", resp["vouches_needed"])
		}
	})

	t.Run("get vouch status with no vouches", func(t *testing.T) {
		noVouchUser := suite.createTestUser("no_vouch_user", models.TierUnverified)
		defer suite.cleanup(noVouchUser.ID)

		path := fmt.Sprintf("/api/v1/verification/vouch/status?user_id=%s&region_id=%s", noVouchUser.ID, region.ID)
		req := httptest.NewRequest("GET", path, nil)
		ctx := context.WithValue(req.Context(), middleware.UserContextKey, &middleware.Claims{UserID: voucher1.ID})
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetVouchStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if vouchesReceived, ok := resp["vouches_received"].(float64); !ok || vouchesReceived != 0 {
			t.Errorf("Expected vouches_received = 0, got %v", resp["vouches_received"])
		}
		if vouchesNeeded, ok := resp["vouches_needed"].(float64); !ok || vouchesNeeded != 2 {
			t.Errorf("Expected vouches_needed = 2, got %v", resp["vouches_needed"])
		}
	})

	t.Run("missing user_id fails", func(t *testing.T) {
		path := fmt.Sprintf("/api/v1/verification/vouch/status?region_id=%s", region.ID)
		req := httptest.NewRequest("GET", path, nil)
		ctx := context.WithValue(req.Context(), middleware.UserContextKey, &middleware.Claims{UserID: voucher1.ID})
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetVouchStatus(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing region_id fails", func(t *testing.T) {
		path := fmt.Sprintf("/api/v1/verification/vouch/status?user_id=%s", targetUser.ID)
		req := httptest.NewRequest("GET", path, nil)
		ctx := context.WithValue(req.Context(), middleware.UserContextKey, &middleware.Claims{UserID: voucher1.ID})
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetVouchStatus(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unauthenticated request returns 401", func(t *testing.T) {
		path := fmt.Sprintf("/api/v1/verification/vouch/status?user_id=%s&region_id=%s", targetUser.ID, region.ID)
		req := httptest.NewRequest("GET", path, nil)
		// No claims in context

		rec := httptest.NewRecorder()
		suite.handler.GetVouchStatus(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// =============================================================================
// GetStatus Tests
// =============================================================================

func TestVerificationHandler_GetStatus(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	// Clean up test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@verificationtest.com'")

	user := suite.createTestUser("get_status_user", models.TierUnverified)
	region := suite.createTestRegion("Test GetStatus Region", models.RegionTypeCity, nil, user.ID)

	defer suite.cleanup(user.ID)
	defer suite.cleanupRegions(region.ID)

	t.Run("get status with no pending request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/verification/status", nil)

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierUnverified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if tier, ok := resp["verification_tier"].(float64); !ok || int(tier) != int(models.TierUnverified) {
			t.Errorf("Expected verification_tier = %d, got %v", models.TierUnverified, resp["verification_tier"])
		}
		if pending, ok := resp["pending_request"].(bool); !ok || pending {
			t.Errorf("Expected pending_request = false, got %v", resp["pending_request"])
		}
	})

	t.Run("get status with pending request", func(t *testing.T) {
		verReq := suite.createVerificationRequest(user.ID, region.ID, "pending12", models.VerificationStatusMailed)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE id = ?", verReq.ID) }()

		req := httptest.NewRequest("GET", "/api/v1/verification/status", nil)

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierUnverified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if pending, ok := resp["pending_request"].(bool); !ok || !pending {
			t.Errorf("Expected pending_request = true, got %v", resp["pending_request"])
		}
		if _, ok := resp["request_id"]; !ok {
			t.Error("Expected 'request_id' in response")
		}
		if _, ok := resp["request_expires_at"]; !ok {
			t.Error("Expected 'request_expires_at' in response")
		}
	})

	t.Run("get status for verified user", func(t *testing.T) {
		verifiedUser := suite.createTestUser("status_verified", models.TierPostcard)
		defer suite.cleanup(verifiedUser.ID)

		req := httptest.NewRequest("GET", "/api/v1/verification/status", nil)

		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: models.TierPostcard,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.GetStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if tier, ok := resp["verification_tier"].(float64); !ok || int(tier) != int(models.TierPostcard) {
			t.Errorf("Expected verification_tier = %d, got %v", models.TierPostcard, resp["verification_tier"])
		}
	})

	t.Run("unauthenticated request fails", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/verification/status", nil)

		rec := httptest.NewRecorder()
		suite.handler.GetStatus(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})
}

// =============================================================================
// RequestVouchVerification Tests
// =============================================================================

func TestVerificationHandler_RequestVouchVerification(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	// Clean up test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@vouchreqtest.com'")

	user := suite.createTestUser("vouchreq_user", models.TierUnverified)

	// Create full hierarchy with unique geometry in rural Montana area (47.0, -110.0)
	// This avoids interference from other test regions that share the default SF polygon.
	stateGeoJSON := `{"type":"Polygon","coordinates":[[[-112.0,45.0],[-108.0,45.0],[-108.0,49.0],[-112.0,49.0],[-112.0,45.0]]]}`
	vouchState := suite.createTestRegionWithCustomGeometry("VouchReq Oregon", models.RegionTypeState, nil, user.ID, stateGeoJSON)

	countyGeoJSON := `{"type":"Polygon","coordinates":[[[-111.0,46.0],[-109.0,46.0],[-109.0,48.0],[-111.0,48.0],[-111.0,46.0]]]}`
	vouchCounty := suite.createTestRegionWithCustomGeometry("VouchReq Multnomah County", models.RegionTypeCounty, &vouchState.ID, user.ID, countyGeoJSON)

	cityGeoJSON := `{"type":"Polygon","coordinates":[[[-110.5,46.5],[-109.5,46.5],[-109.5,47.5],[-110.5,47.5],[-110.5,46.5]]]}`
	region := suite.createTestRegionWithCustomGeometry("VouchReq Portland", models.RegionTypeCity, &vouchCounty.ID, user.ID, cityGeoJSON)

	// Exit bootstrap mode so unverified users can request vouch verification
	adminIDs := suite.exitBootstrapMode(region.ID)

	defer suite.cleanup(user.ID)
	defer func() {
		suite.cleanupRegions(region.ID)
		suite.cleanupRegions(vouchCounty.ID)
		suite.cleanupRegions(vouchState.ID)
	}()
	defer func() {
		for _, id := range adminIDs {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", id)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", id)
		}
	}()

	// Helper to build address payload for vouch request
	makeAddressPayload := func(line1, city, state, postalCode string) []byte {
		body := map[string]interface{}{
			"address": map[string]interface{}{
				"line1":       line1,
				"city":        city,
				"state":       state,
				"postal_code": postalCode,
			},
		}
		bodyBytes, _ := json.Marshal(body)
		return bodyBytes
	}

	t.Run("successful vouch request", func(t *testing.T) {
		// Configure mock Mapbox to return coordinates within the test city polygon
		suite.mapboxService.DefaultLatitude = 47.0
		suite.mapboxService.DefaultLongitude = -110.0
		defer func() {
			suite.mapboxService.DefaultLatitude = 37.7749
			suite.mapboxService.DefaultLongitude = -122.4194
		}()

		bodyBytes := makeAddressPayload("123 Main St", "Portland", "OR", "97204")

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierUnverified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestVouchVerification(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp["status"] != "pending" {
			t.Errorf("Expected status 'pending', got %v", resp["status"])
		}
		if resp["region_id"] != region.ID {
			t.Errorf("Expected region_id '%s', got %v", region.ID, resp["region_id"])
		}
		if resp["privacy_notice"] == nil || resp["privacy_notice"] == "" {
			t.Error("Expected privacy_notice in response")
		}

		// Cleanup
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", user.ID)
	})

	t.Run("duplicate pending request fails", func(t *testing.T) {
		// Configure mock Mapbox to return coordinates within the test city polygon
		suite.mapboxService.DefaultLatitude = 47.0
		suite.mapboxService.DefaultLongitude = -110.0
		defer func() {
			suite.mapboxService.DefaultLatitude = 37.7749
			suite.mapboxService.DefaultLongitude = -122.4194
		}()

		// Create initial pending request
		suite.addUserToRegionPending(user.ID, region.ID)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", user.ID) }()

		bodyBytes := makeAddressPayload("123 Main St", "Portland", "OR", "97204")

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierUnverified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestVouchVerification(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("already verified member fails", func(t *testing.T) {
		// Configure mock Mapbox to return coordinates within the test city polygon
		suite.mapboxService.DefaultLatitude = 47.0
		suite.mapboxService.DefaultLongitude = -110.0
		defer func() {
			suite.mapboxService.DefaultLatitude = 37.7749
			suite.mapboxService.DefaultLongitude = -122.4194
		}()

		// Create verified membership
		suite.addUserToRegion(user.ID, region.ID, false)
		defer func() { _, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", user.ID) }()

		bodyBytes := makeAddressPayload("123 Main St", "Portland", "OR", "97204")

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierUnverified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestVouchVerification(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing address fields fails validation", func(t *testing.T) {
		// Missing line1
		body := map[string]interface{}{
			"address": map[string]interface{}{
				"city":        "Portland",
				"state":       "OR",
				"postal_code": "97204",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierUnverified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestVouchVerification(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("missing postal code fails validation", func(t *testing.T) {
		body := map[string]interface{}{
			"address": map[string]interface{}{
				"line1": "123 Main St",
				"city":  "Portland",
				"state": "OR",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierUnverified,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestVouchVerification(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("unauthenticated request fails", func(t *testing.T) {
		bodyBytes := makeAddressPayload("123 Main St", "Portland", "OR", "97204")

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		suite.handler.RequestVouchVerification(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})
}

// =============================================================================
// Vouch Tests - Fully Verified Voucher Required
// =============================================================================

func TestVerificationHandler_Vouch_FullyVerifiedRequired(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	// Clean up test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@vouchfulltest.com'")

	// Create users with specific verification flags
	fullyVerifiedUser := suite.createTestUserWithFlags("full_verified", true, true)   // Both postcard and vouch verified
	postcardOnlyUser := suite.createTestUserWithFlags("postcard_only", true, false)   // Postcard only
	vouchOnlyUser := suite.createTestUserWithFlags("vouch_only", false, true)         // Vouch only
	targetUser := suite.createTestUser("vouch_target_full", models.TierUnverified)
	region := suite.createTestRegion("Test Full Vouch Region", models.RegionTypeCity, nil, fullyVerifiedUser.ID)

	// Exit bootstrap mode so "postcard-only cannot vouch" test works
	// (in bootstrap mode, postcard-only users CAN vouch)
	adminIDs := suite.exitBootstrapMode(region.ID)

	defer suite.cleanup(fullyVerifiedUser.ID, postcardOnlyUser.ID, vouchOnlyUser.ID, targetUser.ID)
	defer suite.cleanupRegions(region.ID)
	defer func() {
		for _, id := range adminIDs {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", id)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", id)
		}
	}()

	t.Run("fully verified user can vouch", func(t *testing.T) {
		// Add users to region - voucher as admin, target as pending
		suite.addUserToRegion(fullyVerifiedUser.ID, region.ID, true)
		suite.addUserToRegionPending(targetUser.ID, region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE voucher_user_id = ?", fullyVerifiedUser.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", fullyVerifiedUser.ID, targetUser.ID)
		}()

		body := map[string]interface{}{
			"vouched_user_id": targetUser.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           fullyVerifiedUser.ID,
			Email:            fullyVerifiedUser.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unverified user cannot vouch in normal mode", func(t *testing.T) {
		// Add users to region
		unverifiedUser := suite.createTestUserWithFlags("unverified_voucher", false, false)
		suite.addUserToRegion(unverifiedUser.ID, region.ID, false)
		suite.addUserToRegionPending(targetUser.ID, region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", unverifiedUser.ID, targetUser.ID)
		}()
		defer suite.cleanup(unverifiedUser.ID)

		body := map[string]interface{}{
			"vouched_user_id": targetUser.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           unverifiedUser.ID,
			Email:            unverifiedUser.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("vouch-only user cannot vouch", func(t *testing.T) {
		// Add users to region
		suite.addUserToRegion(vouchOnlyUser.ID, region.ID, false)
		suite.addUserToRegionPending(targetUser.ID, region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", vouchOnlyUser.ID, targetUser.ID)
		}()

		body := map[string]interface{}{
			"vouched_user_id": targetUser.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           vouchOnlyUser.ID,
			Email:            vouchOnlyUser.Email,
			VerificationTier: models.TierVouched,
			PostcardVerified: false,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("vouchee without pending request cannot be vouched", func(t *testing.T) {
		// Add voucher to region but don't add target as pending
		suite.addUserToRegion(fullyVerifiedUser.ID, region.ID, true)
		// Note: targetUser is NOT added to region with pending status
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", fullyVerifiedUser.ID)
		}()

		body := map[string]interface{}{
			"vouched_user_id": targetUser.ID,
			"region_id":       region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           fullyVerifiedUser.ID,
			Email:            fullyVerifiedUser.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// =============================================================================
// Postcard Verification - Region Assignment Tests
// =============================================================================

// createTestRegionWithCustomGeometry creates a region with a specific geometry
func (s *verificationTestSuite) createTestRegionWithCustomGeometry(name string, regionType models.RegionType, parentID *string, createdBy string, geoJSON string) *models.GeographicRegion {
	region := &models.GeographicRegion{
		ID:             uuid.New().String(),
		Name:           name,
		RegionType:     regionType,
		ParentRegionID: parentID,
		CreatedBy:      &createdBy,
		CreatedAt:      time.Now().UTC(),
	}

	query := `
		INSERT INTO geographic_regions (id, name, region_type, parent_region_id, geometry, created_by, created_at)
		VALUES (?, ?, ?, ?, ST_GeomFromGeoJSON(?), ?, ?)
	`
	_, err := s.db.ExecContext(context.Background(), query,
		region.ID, region.Name, region.RegionType, region.ParentRegionID, geoJSON, region.CreatedBy, region.CreatedAt)
	if err != nil {
		s.t.Fatalf("Failed to create test region: %v", err)
	}

	return region
}

// createNestedRegionHierarchy creates state -> county -> city with nested geometries
// City is smallest, county contains city, state contains county
// Uses unique coordinates (rural Nevada area) to avoid interference from other test regions
// Test coordinates: 40.0, -117.0
func (s *verificationTestSuite) createNestedRegionHierarchy(prefix, createdBy string) (state, county, city *models.GeographicRegion, cleanup func()) {
	// State: Large area (rural Nevada)
	stateGeoJSON := `{"type":"Polygon","coordinates":[[[-120.0,38.0],[-114.0,38.0],[-114.0,42.0],[-120.0,42.0],[-120.0,38.0]]]}`
	state = s.createTestRegionWithCustomGeometry(prefix+" State", models.RegionTypeState, nil, createdBy, stateGeoJSON)

	// County: Medium area - contains test point 40.0, -117.0
	countyGeoJSON := `{"type":"Polygon","coordinates":[[[-118.0,39.0],[-116.0,39.0],[-116.0,41.0],[-118.0,41.0],[-118.0,39.0]]]}`
	county = s.createTestRegionWithCustomGeometry(prefix+" County", models.RegionTypeCounty, &state.ID, createdBy, countyGeoJSON)

	// City: Smallest area - contains test point 40.0, -117.0
	cityGeoJSON := `{"type":"Polygon","coordinates":[[[-117.5,39.5],[-116.5,39.5],[-116.5,40.5],[-117.5,40.5],[-117.5,39.5]]]}`
	city = s.createTestRegionWithCustomGeometry(prefix+" City", models.RegionTypeCity, &county.ID, createdBy, cityGeoJSON)

	cleanup = func() {
		s.cleanupRegions(city.ID)
		s.cleanupRegions(county.ID)
		s.cleanupRegions(state.ID)
	}

	return state, county, city, cleanup
}

func TestVerificationHandler_PostcardVerification_RegionAssignment(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	// Clean up test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@postcardregiontest.com'")
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM geographic_regions WHERE name LIKE '%PostcardRegion%'")

	t.Run("user is added to most specific region (city) on postcard verification", func(t *testing.T) {
		user := &models.User{
			Username:         "postcard_region_user",
			Email:            "postcard_region_user@postcardregiontest.com",
			PasswordHash:     "$2a$12$test.hash.for.testing.only",
			VerificationTier: models.TierUnverified,
		}
		if err := suite.userRepo.Create(context.Background(), user); err != nil {
			t.Fatalf("Failed to create test user: %v", err)
		}
		defer suite.cleanup(user.ID)

		// Create nested region hierarchy (state -> county -> city)
		state, county, city, cleanupHierarchy := suite.createNestedRegionHierarchy("PostcardRegion", user.ID)
		defer cleanupHierarchy()

		// Reset mock services and set coordinates that fall within all three test regions
		suite.postgridService.Reset()
		suite.mapboxService.Reset()
		// Set mock coordinates to 40.0, -117.0 (within our test city polygon in Nevada area)
		suite.mapboxService.DefaultLatitude = 40.0
		suite.mapboxService.DefaultLongitude = -117.0

		// Step 1: Request postcard verification (no region_id - let it auto-detect)
		reqBody := map[string]interface{}{
			"address": map[string]string{
				"line1":       "123 Market St",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierVouched,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var postcardResp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&postcardResp)

		// Verify the region associated with verification is the city (most specific)
		regionInfo, ok := postcardResp["region"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected 'region' in response")
		}
		if regionInfo["id"] != city.ID {
			t.Errorf("Expected region to be city (ID: %s), got %v", city.ID, regionInfo["id"])
		}

		// Get the verification code from the mock postcard service
		if len(suite.postgridService.SendPostcardCalls) == 0 {
			t.Fatal("Expected SendPostcard to be called")
		}
		verificationCode := suite.postgridService.SendPostcardCalls[0].VerificationCode

		// Step 2: Verify the code
		verifyBody := map[string]interface{}{
			"verification_code": verificationCode,
		}
		verifyBytes, _ := json.Marshal(verifyBody)

		verifyReq := httptest.NewRequest("POST", "/api/v1/verification/postcard/verify", bytes.NewReader(verifyBytes))
		verifyReq.Header.Set("Content-Type", "application/json")
		verifyReq = verifyReq.WithContext(middleware.ContextWithUser(verifyReq.Context(), claims))

		verifyRec := httptest.NewRecorder()
		suite.handler.VerifyCode(verifyRec, verifyReq)

		if verifyRec.Code != http.StatusOK {
			t.Fatalf("Expected verify status 200, got %d: %s", verifyRec.Code, verifyRec.Body.String())
		}

		// Step 3: Verify user was added to the city region (not state or county)
		var userRegionID string
		err := suite.db.QueryRowContext(context.Background(),
			"SELECT region_id FROM user_regions WHERE user_id = ?", user.ID).Scan(&userRegionID)
		if err != nil {
			t.Fatalf("Failed to query user_regions: %v", err)
		}

		if userRegionID != city.ID {
			t.Errorf("User was added to wrong region. Expected city ID %s, got %s", city.ID, userRegionID)

			// Provide additional context for debugging
			switch userRegionID {
			case state.ID:
				t.Error("User was incorrectly added to STATE region instead of CITY")
			case county.ID:
				t.Error("User was incorrectly added to COUNTY region instead of CITY")
			}
		}

		// Also verify the user is NOT directly added to state or county
		var count int
		err = suite.db.QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM user_regions WHERE user_id = ?", user.ID).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to count user_regions: %v", err)
		}

		if count != 1 {
			t.Errorf("User should only be in 1 region (city), but found %d user_region entries", count)
		}
	})

	t.Run("GetRegionsContainingPoint returns regions ordered from most to least specific", func(t *testing.T) {
		user := &models.User{
			Username:         "region_order_user",
			Email:            "region_order_user@postcardregiontest.com",
			PasswordHash:     "$2a$12$test.hash.for.testing.only",
			VerificationTier: models.TierUnverified,
		}
		if err := suite.userRepo.Create(context.Background(), user); err != nil {
			t.Fatalf("Failed to create test user: %v", err)
		}
		defer suite.cleanup(user.ID)

		// Create nested hierarchy with a unique prefix
		state, county, city, cleanupHierarchy := suite.createNestedRegionHierarchy("OrderTest", user.ID)
		defer cleanupHierarchy()

		// Query regions containing the test coordinates
		lat := 40.0  // Within all three nested test regions (rural Nevada)
		lng := -117.0

		regions, err := suite.regionRepo.GetRegionsContainingPoint(context.Background(), lat, lng)
		if err != nil {
			t.Fatalf("GetRegionsContainingPoint failed: %v", err)
		}

		// Filter to only our test regions
		var testRegions []models.RegionSummary
		testRegionIDs := map[string]bool{state.ID: true, county.ID: true, city.ID: true}
		for _, r := range regions {
			if testRegionIDs[r.ID] {
				testRegions = append(testRegions, r)
			}
		}

		if len(testRegions) < 3 {
			t.Fatalf("Expected at least 3 test regions containing point, got %d", len(testRegions))
		}

		// Verify ordering: city (most specific) should be first, then county, then state
		if testRegions[0].ID != city.ID {
			t.Errorf("First region should be city (ID: %s), got %s (type: %s)", city.ID, testRegions[0].ID, testRegions[0].RegionType)
		}
		if testRegions[1].ID != county.ID {
			t.Errorf("Second region should be county (ID: %s), got %s (type: %s)", county.ID, testRegions[1].ID, testRegions[1].RegionType)
		}
		if testRegions[2].ID != state.ID {
			t.Errorf("Third region should be state (ID: %s), got %s (type: %s)", state.ID, testRegions[2].ID, testRegions[2].RegionType)
		}
	})

	t.Run("verification with specified region_id uses that region", func(t *testing.T) {
		user := &models.User{
			Username:         "specified_region_user",
			Email:            "specified_region_user@postcardregiontest.com",
			PasswordHash:     "$2a$12$test.hash.for.testing.only",
			VerificationTier: models.TierUnverified,
		}
		if err := suite.userRepo.Create(context.Background(), user); err != nil {
			t.Fatalf("Failed to create test user: %v", err)
		}
		defer suite.cleanup(user.ID)

		// Create a simple city region for this test (unique area - rural Arizona)
		// Contains test point 35.0, -111.5
		cityGeoJSON := `{"type":"Polygon","coordinates":[[[-112.0,34.5],[-111.0,34.5],[-111.0,35.5],[-112.0,35.5],[-112.0,34.5]]]}`
		city := suite.createTestRegionWithCustomGeometry("SpecifiedCity", models.RegionTypeCity, nil, user.ID, cityGeoJSON)
		defer suite.cleanupRegions(city.ID)

		suite.postgridService.Reset()
		suite.mapboxService.Reset()
		// Set coordinates within the test city polygon
		suite.mapboxService.DefaultLatitude = 35.0
		suite.mapboxService.DefaultLongitude = -111.5

		// Request verification with specific region_id
		reqBody := map[string]interface{}{
			"region_id": city.ID,
			"address": map[string]string{
				"line1":       "456 Market St",
				"city":        "San Francisco",
				"state":       "CA",
				"postal_code": "94102",
			},
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierVouched,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var postcardResp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&postcardResp)

		// Verify the specified region is used
		regionInfo, ok := postcardResp["region"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected 'region' in response")
		}
		if regionInfo["id"] != city.ID {
			t.Errorf("Expected specified region ID %s, got %v", city.ID, regionInfo["id"])
		}

		// Verify and check user is added to specified region
		verificationCode := suite.postgridService.SendPostcardCalls[0].VerificationCode

		verifyBody := map[string]interface{}{
			"verification_code": verificationCode,
		}
		verifyBytes, _ := json.Marshal(verifyBody)

		verifyReq := httptest.NewRequest("POST", "/api/v1/verification/postcard/verify", bytes.NewReader(verifyBytes))
		verifyReq.Header.Set("Content-Type", "application/json")
		verifyReq = verifyReq.WithContext(middleware.ContextWithUser(verifyReq.Context(), claims))

		verifyRec := httptest.NewRecorder()
		suite.handler.VerifyCode(verifyRec, verifyReq)

		if verifyRec.Code != http.StatusOK {
			t.Fatalf("Expected verify status 200, got %d: %s", verifyRec.Code, verifyRec.Body.String())
		}

		var userRegionID string
		err := suite.db.QueryRowContext(context.Background(),
			"SELECT region_id FROM user_regions WHERE user_id = ?", user.ID).Scan(&userRegionID)
		if err != nil {
			t.Fatalf("Failed to query user_regions: %v", err)
		}

		if userRegionID != city.ID {
			t.Errorf("User should be added to specified region %s, got %s", city.ID, userRegionID)
		}
	})

	t.Run("creates city region when only state/county exist", func(t *testing.T) {
		// This tests the fix for the bug where a user was assigned to state/county
		// when the address coordinates fell outside any existing city polygon
		user := &models.User{
			Username:         "state_only_user",
			Email:            "state_only_user@postcardregiontest.com",
			PasswordHash:     "$2a$12$test.hash.for.testing.only",
			VerificationTier: models.TierUnverified,
		}
		if err := suite.userRepo.Create(context.Background(), user); err != nil {
			t.Fatalf("Failed to create test user: %v", err)
		}
		defer suite.cleanup(user.ID)

		// Create ONLY state and county regions (no city) at unique coordinates
		// Rural Idaho area: 44.0, -115.0
		stateGeoJSON := `{"type":"Polygon","coordinates":[[[-116.0,43.5],[-114.0,43.5],[-114.0,44.5],[-116.0,44.5],[-116.0,43.5]]]}`
		state := suite.createTestRegionWithCustomGeometry("StateOnly_Idaho", models.RegionTypeState, nil, user.ID, stateGeoJSON)
		defer suite.cleanupRegions(state.ID)

		countyGeoJSON := `{"type":"Polygon","coordinates":[[[-115.5,43.75],[-114.5,43.75],[-114.5,44.25],[-115.5,44.25],[-115.5,43.75]]]}`
		county := suite.createTestRegionWithCustomGeometry("StateOnly_County", models.RegionTypeCounty, &state.ID, user.ID, countyGeoJSON)
		defer suite.cleanupRegions(county.ID)

		// Note: No city region is created - coordinates 44.0, -115.0 are only in state/county

		suite.postgridService.Reset()
		suite.mapboxService.Reset()
		// Set coordinates within the state/county but no city
		suite.mapboxService.DefaultLatitude = 44.0
		suite.mapboxService.DefaultLongitude = -115.0
		suite.mapboxService.DefaultBoundaryName = "NewTestCity"
		suite.mapboxService.DefaultBoundaryState = "Idaho"

		reqBody := map[string]interface{}{
			"address": map[string]string{
				"line1":       "789 Rural Rd",
				"city":        "NewTestCity",
				"state":       "ID",
				"postal_code": "83701",
			},
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierVouched,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.RequestPostcardVerification(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var postcardResp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&postcardResp)

		// Verify a new city region was created (not assigned to state/county)
		regionInfo, ok := postcardResp["region"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected 'region' in response")
		}

		regionID := regionInfo["id"].(string)
		regionCreated, _ := regionInfo["created"].(bool)

		// The region should be newly created
		if !regionCreated {
			t.Error("Expected region to be newly created, but it wasn't")
		}

		// The region should NOT be the state or county
		if regionID == state.ID {
			t.Errorf("User should NOT be assigned to state region. A new city should be created.")
		}
		if regionID == county.ID {
			t.Errorf("User should NOT be assigned to county region. A new city should be created.")
		}

		// Verify the new region is a city
		var regionType string
		err := suite.db.QueryRowContext(context.Background(),
			"SELECT region_type FROM geographic_regions WHERE id = ?", regionID).Scan(&regionType)
		if err != nil {
			t.Fatalf("Failed to query new region: %v", err)
		}
		if regionType != "city" {
			t.Errorf("Expected newly created region to be type 'city', got '%s'", regionType)
		}

		// Clean up the newly created city (and its parent hierarchy if created)
		suite.cleanupRegions(regionID)
	})
}

// =============================================================================
// Bootstrap Mode Tests
// =============================================================================

func TestVerificationHandler_BootstrapMode(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	// Clean up test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@bootstraptest.com'")
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM geographic_regions WHERE name LIKE '%Bootstrap Test%'")

	t.Run("any member can vouch in bootstrap mode", func(t *testing.T) {
		// Create a region with no full admins (bootstrap mode)
		creator := suite.createTestUserWithFlags("bootstrap_creator", false, false) // unverified
		region := suite.createTestRegion("Bootstrap Test Region", models.RegionTypeCity, nil, creator.ID)
		suite.addUserToRegion(creator.ID, region.ID, true) // admin but not full admin

		// Create another unverified user who will vouch (any member can in bootstrap)
		voucher := suite.createTestUserWithFlags("bootstrap_voucher", false, false) // unverified
		suite.addUserToRegion(voucher.ID, region.ID, false)

		// Create a user to be vouched for with pending status
		vouchee := suite.createTestUserWithFlags("bootstrap_vouchee", false, false)
		suite.addUserToRegionPending(vouchee.ID, region.ID)

		defer suite.cleanup(creator.ID, voucher.ID, vouchee.ID)
		defer suite.cleanupRegions(region.ID)

		// Verify region is in bootstrap mode (no full admins)
		bootstrapMode, adminCount, err := suite.regionRepo.IsRegionInBootstrapMode(context.Background(), region.ID)
		if err != nil {
			t.Fatalf("Failed to check bootstrap mode: %v", err)
		}
		if !bootstrapMode {
			t.Errorf("Expected region to be in bootstrap mode, but it's not")
		}
		if adminCount != 0 {
			t.Errorf("Expected 0 full admins, got %d", adminCount)
		}

		// Any member should be able to vouch in bootstrap mode
		body := map[string]interface{}{
			"user_id":   vouchee.ID,
			"region_id": region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           voucher.ID,
			Email:            voucher.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.Vouch(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var response models.VouchResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !response.BootstrapMode {
			t.Errorf("Expected bootstrap_mode=true in response")
		}
		if response.VouchesRequired != 3 {
			t.Errorf("Expected vouches_required=3 in bootstrap mode, got %d", response.VouchesRequired)
		}
	})

	t.Run("bootstrap mode requires 3 vouches instead of 2", func(t *testing.T) {
		// Create a region with no full admins (bootstrap mode)
		creator := suite.createTestUserWithFlags("bootstrap_creator2", false, false)
		region := suite.createTestRegion("Bootstrap Test Region 2", models.RegionTypeCity, nil, creator.ID)
		suite.addUserToRegion(creator.ID, region.ID, true)

		// Create three vouchers (unverified members — any member can vouch in bootstrap)
		voucher1 := suite.createTestUserWithFlags("bootstrap_voucher2_1", false, false)
		voucher2 := suite.createTestUserWithFlags("bootstrap_voucher2_2", false, false)
		voucher3 := suite.createTestUserWithFlags("bootstrap_voucher2_3", false, false)
		suite.addUserToRegion(voucher1.ID, region.ID, false)
		suite.addUserToRegion(voucher2.ID, region.ID, false)
		suite.addUserToRegion(voucher3.ID, region.ID, false)

		// Create a user to be vouched for
		vouchee := suite.createTestUserWithFlags("bootstrap_vouchee2", false, false) // unverified
		suite.addUserToRegionPending(vouchee.ID, region.ID)

		defer suite.cleanup(creator.ID, voucher1.ID, voucher2.ID, voucher3.ID, vouchee.ID)
		defer suite.cleanupRegions(region.ID)

		// Helper to vouch with cooldown bypass (set vouch time in past)
		vouchWithCooldownBypass := func(voucher *models.User, waitMinutes int) {
			body := map[string]interface{}{
				"user_id":   vouchee.ID,
				"region_id": region.ID,
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			claims := &middleware.Claims{
				UserID:           voucher.ID,
				Email:            voucher.Email,
				VerificationTier: models.TierUnverified,
				PostcardVerified: false,
				VouchVerified:    false,
			}
			ctx := middleware.ContextWithUser(req.Context(), claims)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			suite.handler.Vouch(rr, req)

			if rr.Code != http.StatusCreated {
				t.Fatalf("Vouch failed with status %d: %s", rr.Code, rr.Body.String())
			}

			// Update vouch time to bypass cooldown for next voucher
			if waitMinutes > 0 {
				_, _ = suite.db.ExecContext(context.Background(),
					"UPDATE vouches SET created_at = DATE_SUB(NOW(), INTERVAL ? MINUTE) WHERE voucher_user_id = ?",
					waitMinutes+61, voucher.ID)
			}
		}

		// First vouch - should succeed, vouchee not yet verified
		vouchWithCooldownBypass(voucher1, 70)

		// Check vouchee is not yet vouch_verified (needs 3 in bootstrap mode)
		var vouchVerified bool
		err := suite.db.QueryRowContext(context.Background(),
			"SELECT vouch_verified FROM users WHERE id = ?", vouchee.ID).Scan(&vouchVerified)
		if err != nil {
			t.Fatalf("Failed to check vouch status: %v", err)
		}
		if vouchVerified {
			t.Error("User should not be vouch_verified after 1 vouch in bootstrap mode")
		}

		// Second vouch
		vouchWithCooldownBypass(voucher2, 70)

		// Check still not verified (needs 3)
		err = suite.db.QueryRowContext(context.Background(),
			"SELECT vouch_verified FROM users WHERE id = ?", vouchee.ID).Scan(&vouchVerified)
		if err != nil {
			t.Fatalf("Failed to check vouch status: %v", err)
		}
		if vouchVerified {
			t.Error("User should not be vouch_verified after 2 vouches in bootstrap mode (needs 3)")
		}

		// Third vouch - should trigger verification
		vouchWithCooldownBypass(voucher3, 0)

		// Now user should be vouch_verified
		err = suite.db.QueryRowContext(context.Background(),
			"SELECT vouch_verified FROM users WHERE id = ?", vouchee.ID).Scan(&vouchVerified)
		if err != nil {
			t.Fatalf("Failed to check vouch status: %v", err)
		}
		if !vouchVerified {
			t.Error("User should be vouch_verified after 3 vouches in bootstrap mode")
		}
	})

	t.Run("cooldown enforced in bootstrap mode", func(t *testing.T) {
		// Create a handler with cooldown ENABLED for this test
		handlerWithCooldown := NewVerificationHandler(
			nil,
			suite.verificationRepo,
			suite.vouchRepo,
			suite.userRepo,
			suite.regionRepo,
			suite.postgridService,
			mocks.NewMockMapboxService(),
			nil,
			true, 30, // Bootstrap cooldown ENABLED
		)

		// Create a region with no full admins (bootstrap mode)
		creator := suite.createTestUserWithFlags("bootstrap_creator3", false, false)
		region := suite.createTestRegion("Bootstrap Test Region 3", models.RegionTypeCity, nil, creator.ID)
		suite.addUserToRegion(creator.ID, region.ID, true)

		// Create a voucher (unverified member — any member can vouch in bootstrap)
		voucher := suite.createTestUserWithFlags("bootstrap_voucher3", false, false)
		suite.addUserToRegion(voucher.ID, region.ID, false)

		// Create two users to be vouched for
		vouchee1 := suite.createTestUserWithFlags("bootstrap_vouchee3_1", false, false)
		vouchee2 := suite.createTestUserWithFlags("bootstrap_vouchee3_2", false, false)
		suite.addUserToRegionPending(vouchee1.ID, region.ID)
		suite.addUserToRegionPending(vouchee2.ID, region.ID)

		defer suite.cleanup(creator.ID, voucher.ID, vouchee1.ID, vouchee2.ID)
		defer suite.cleanupRegions(region.ID)

		// First vouch should succeed
		body := map[string]interface{}{
			"user_id":   vouchee1.ID,
			"region_id": region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		claims := &middleware.Claims{
			UserID:           voucher.ID,
			Email:            voucher.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		handlerWithCooldown.Vouch(rr, req)

		if rr.Code != http.StatusCreated {
			t.Fatalf("First vouch failed: %d - %s", rr.Code, rr.Body.String())
		}

		// Second vouch (for different user) should be blocked by cooldown
		body2 := map[string]interface{}{
			"user_id":   vouchee2.ID,
			"region_id": region.ID,
		}
		bodyBytes2, _ := json.Marshal(body2)

		req2 := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes2))
		req2.Header.Set("Content-Type", "application/json")
		ctx2 := middleware.ContextWithUser(req2.Context(), claims)
		req2 = req2.WithContext(ctx2)

		rr2 := httptest.NewRecorder()
		handlerWithCooldown.Vouch(rr2, req2)

		if rr2.Code != http.StatusTooManyRequests {
			t.Errorf("Expected cooldown error (429), got %d: %s", rr2.Code, rr2.Body.String())
		}

		var errorResp map[string]interface{}
		if err := json.Unmarshal(rr2.Body.Bytes(), &errorResp); err == nil {
			if errorResp["error"] != "vouch_cooldown" {
				t.Errorf("Expected error=vouch_cooldown, got %v", errorResp["error"])
			}
		}
	})

	t.Run("vouch-only user cannot vouch in normal mode", func(t *testing.T) {
		// Create a region with 3 full admins (not in bootstrap mode)
		admin1 := suite.createTestUserWithFlags("normal_admin1", true, true) // full admin
		admin2 := suite.createTestUserWithFlags("normal_admin2", true, true) // full admin
		admin3 := suite.createTestUserWithFlags("normal_admin3", true, true) // full admin
		region := suite.createTestRegion("Normal Mode Region", models.RegionTypeCity, nil, admin1.ID)
		suite.addUserToRegion(admin1.ID, region.ID, true)
		suite.addUserToRegion(admin2.ID, region.ID, true)
		suite.addUserToRegion(admin3.ID, region.ID, true)

		// Create a vouch-only user who tries to vouch (not fully verified)
		voucher := suite.createTestUserWithFlags("normal_voucher", false, true) // vouch only
		suite.addUserToRegion(voucher.ID, region.ID, false)

		// Create a user to be vouched for
		vouchee := suite.createTestUserWithFlags("normal_vouchee", false, false)
		suite.addUserToRegionPending(vouchee.ID, region.ID)

		defer suite.cleanup(admin1.ID, admin2.ID, admin3.ID, voucher.ID, vouchee.ID)
		defer suite.cleanupRegions(region.ID)

		// Verify region is NOT in bootstrap mode
		bootstrapMode, adminCount, err := suite.regionRepo.IsRegionInBootstrapMode(context.Background(), region.ID)
		if err != nil {
			t.Fatalf("Failed to check bootstrap mode: %v", err)
		}
		if bootstrapMode {
			t.Errorf("Expected region to NOT be in bootstrap mode, but it is. adminCount=%d", adminCount)
		}
		if adminCount != 3 {
			t.Errorf("Expected 3 full admins, got %d", adminCount)
		}

		// Vouch-only user should NOT be able to vouch in normal mode (needs both verifications)
		body := map[string]interface{}{
			"user_id":   vouchee.ID,
			"region_id": region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           voucher.ID,
			Email:            voucher.Email,
			VerificationTier: models.TierVouched,
			PostcardVerified: false,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.Vouch(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403 (forbidden), got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("region API returns bootstrap_mode flag", func(t *testing.T) {
		// Create a region with no full admins (bootstrap mode)
		creator := suite.createTestUserWithFlags("bootstrap_api_creator", false, false)
		region := suite.createTestRegion("Bootstrap API Region", models.RegionTypeCity, nil, creator.ID)
		suite.addUserToRegion(creator.ID, region.ID, true)

		defer suite.cleanup(creator.ID)
		defer suite.cleanupRegions(region.ID)

		// Check bootstrap mode via database
		bootstrapMode, fullAdminCount, err := suite.regionRepo.IsRegionInBootstrapMode(context.Background(), region.ID)
		if err != nil {
			t.Fatalf("Failed to check bootstrap mode: %v", err)
		}
		if !bootstrapMode {
			t.Error("Expected region to be in bootstrap mode")
		}
		if fullAdminCount != 0 {
			t.Errorf("Expected 0 full admins, got %d", fullAdminCount)
		}
	})
}

// =============================================================================
// Vouch Tests: Pending Request Required
// =============================================================================
// These tests verify that vouching requires the vouchee to have a pending
// vouch request (created via address submission in the vouch request flow).

func TestVerificationHandler_Vouch_PendingRequestRequired(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	// Clean up test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@vouchverifiedtest.com'")
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM geographic_regions WHERE name LIKE 'VouchVerified%'")

	t.Run("vouch succeeds for user with pending request in bootstrap mode", func(t *testing.T) {
		// Create an unverified voucher (any member can vouch in bootstrap mode)
		voucher := suite.createTestUserWithFlags("vv_voucher", false, false)

		// Create an unverified vouchee who submitted a vouch request
		vouchee := suite.createTestUserWithFlags("vv_vouchee", false, false)

		// Create region (will be in bootstrap mode with no full admins)
		region := suite.createTestRegion("VouchVerified City", models.RegionTypeCity, nil, voucher.ID)

		// Add voucher as verified member, vouchee as pending (from vouch request)
		suite.addUserToRegion(voucher.ID, region.ID, false)
		suite.addUserToRegionPending(vouchee.ID, region.ID)

		defer suite.cleanup(voucher.ID, vouchee.ID)
		defer suite.cleanupRegions(region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE voucher_user_id = ?", voucher.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", voucher.ID, vouchee.ID)
		}()

		body := map[string]interface{}{
			"user_id":   vouchee.ID,
			"region_id": region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           voucher.ID,
			Email:            voucher.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.Vouch(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("Expected status 201 (created), got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Verify response contains expected fields
		if _, ok := resp["vouch_id"]; !ok {
			t.Error("Expected 'vouch_id' in response")
		}
		if totalVouches, ok := resp["total_vouches"].(float64); !ok || totalVouches != 1 {
			t.Errorf("Expected total_vouches = 1, got %v", resp["total_vouches"])
		}
		if bootstrapMode, ok := resp["bootstrap_mode"].(bool); !ok || !bootstrapMode {
			t.Errorf("Expected bootstrap_mode = true, got %v", resp["bootstrap_mode"])
		}
	})

	t.Run("vouch fails if vouchee has no pending request", func(t *testing.T) {
		// Ensure we get the proper error when vouchee has no pending region request
		// Vouchee must share a region with voucher (verified status, not pending)
		// so we pass the shared-region check and hit the not_eligible check.

		voucher := suite.createTestUserWithFlags("vv_voucher2", false, false)
		vouchee := suite.createTestUserWithFlags("vv_vouchee2", false, false)
		region := suite.createTestRegion("VouchVerified City2", models.RegionTypeCity, nil, voucher.ID)

		// Add both voucher and vouchee to region as verified members (not pending)
		suite.addUserToRegion(voucher.ID, region.ID, false)
		suite.addUserToRegion(vouchee.ID, region.ID, false)

		defer suite.cleanup(voucher.ID, vouchee.ID)
		defer suite.cleanupRegions(region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", voucher.ID, vouchee.ID)
		}()

		body := map[string]interface{}{
			"user_id":   vouchee.ID,
			"region_id": region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           voucher.ID,
			Email:            voucher.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.Vouch(rr, req)

		// Should fail since vouchee has no pending request
		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400 (bad request), got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&resp)
		if resp["error"] != "not_eligible" {
			t.Errorf("Expected error 'not_eligible', got '%v'", resp["error"])
		}
	})

	t.Run("vouch fails for verified-status user (requires pending)", func(t *testing.T) {
		// Users with verified status should not be vouch-eligible
		// Only pending users (from vouch request) can receive vouches

		voucher := suite.createTestUserWithFlags("vv_voucher3", false, false)
		vouchee := suite.createTestUserWithFlags("vv_vouchee3", false, false)
		region := suite.createTestRegion("VouchVerified City3", models.RegionTypeCity, nil, voucher.ID)

		suite.addUserToRegion(voucher.ID, region.ID, false)
		suite.addUserToRegion(vouchee.ID, region.ID, false) // verified, not pending

		defer suite.cleanup(voucher.ID, vouchee.ID)
		defer suite.cleanupRegions(region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", voucher.ID, vouchee.ID)
		}()

		body := map[string]interface{}{
			"user_id":   vouchee.ID,
			"region_id": region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           voucher.ID,
			Email:            voucher.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.Vouch(rr, req)

		// Should fail since vouchee has verified (not pending) status
		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400 (bad request), got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&resp)
		if resp["error"] != "not_eligible" {
			t.Errorf("Expected error 'not_eligible', got '%v'", resp["error"])
		}
	})
}

// =============================================================================
// Bug Fix Tests: GetStatus Uses Most Specific Region for Vouch Count
// =============================================================================
// This tests the fix for the bug where GetStatus returned vouch_count = 0
// even though the user had received vouches. The bug was caused by using
// regions[0] (the state, least specific) instead of regions[len(regions)-1]
// (the city, most specific) when calculating vouch count.

func TestVerificationHandler_GetStatus_MostSpecificRegion(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	// Clean up test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@statusregiontest.com'")
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM geographic_regions WHERE name LIKE 'StatusRegion%'")

	t.Run("GetStatus returns vouch count from most specific region (city)", func(t *testing.T) {
		// This tests the bug fix: previously, GetStatus used regions[0] which
		// was the state (least specific), causing vouch_count to be 0 even when
		// the user had vouches in their city region.

		// Create users
		admin := suite.createTestUserWithFlags("sr_admin", true, true)
		vouchee := suite.createTestUserWithFlags("sr_vouchee", false, false) // unverified, awaiting vouches

		// Create region hierarchy: state -> county -> city
		state, _, city, cleanupHierarchy := suite.createNestedRegionHierarchy("StatusRegion", admin.ID)
		defer cleanupHierarchy()

		// Add vouchee to the city with pending status (from vouch request)
		suite.addUserToRegionPending(vouchee.ID, city.ID)

		// Add admin to city as well (for shared region requirement)
		suite.addUserToRegion(admin.ID, city.ID, true)

		defer suite.cleanup(admin.ID, vouchee.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE voucher_user_id = ?", admin.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", admin.ID, vouchee.ID)
		}()

		// Create a vouch in the CITY region
		suite.createVouch(admin.ID, vouchee.ID, city.ID)

		// Get status - should show vouch_count = 1 for the city region
		req := httptest.NewRequest("GET", "/api/v1/verification/status", nil)
		claims := &middleware.Claims{
			UserID:           vouchee.ID,
			Email:            vouchee.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.GetStatus(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Before the fix: vouch_count was 0 because it looked at state region
		// After the fix: vouch_count should be 1 because it looks at city region
		vouchCount, ok := resp["vouch_count"].(float64)
		if !ok {
			t.Fatalf("Expected vouch_count in response, got: %v", resp)
		}
		if vouchCount != 1 {
			t.Errorf("Expected vouch_count = 1, got %v", vouchCount)
		}

		// Verify it's using the city region, not the state
		pendingRegion, ok := resp["pending_vouch_region"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected pending_vouch_region in response, got: %v", resp)
		}
		regionName, _ := pendingRegion["name"].(string)
		if regionName != city.Name {
			t.Errorf("Expected pending_vouch_region.name = '%s' (city), got '%s'", city.Name, regionName)
		}

		// Verify it's NOT using the state
		if regionName == state.Name {
			t.Errorf("Bug: GetStatus is still using state ('%s') instead of city ('%s')", state.Name, city.Name)
		}
	})

	t.Run("GetStatus with vouch in state region shows 0 for city-based user", func(t *testing.T) {
		// Ensure vouches in parent regions don't incorrectly count for the child

		admin := suite.createTestUserWithFlags("sr_admin2", true, true)
		vouchee := suite.createTestUserWithFlags("sr_vouchee2", false, false) // unverified, awaiting vouches

		state, _, city, cleanupHierarchy := suite.createNestedRegionHierarchy("StatusRegion2", admin.ID)
		defer cleanupHierarchy()

		// Add vouchee to city with pending status (from vouch request)
		suite.addUserToRegionPending(vouchee.ID, city.ID)
		suite.addUserToRegion(admin.ID, city.ID, true)

		defer suite.cleanup(admin.ID, vouchee.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE voucher_user_id = ?", admin.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", admin.ID, vouchee.ID)
		}()

		// Create vouch in STATE region (not city)
		suite.createVouch(admin.ID, vouchee.ID, state.ID)

		// Get status - should show vouch_count = 0 since the vouch is in state, not city
		req := httptest.NewRequest("GET", "/api/v1/verification/status", nil)
		claims := &middleware.Claims{
			UserID:           vouchee.ID,
			Email:            vouchee.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.GetStatus(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&resp)

		// Vouch count should be 0 since the vouch was in state, not city
		vouchCount, _ := resp["vouch_count"].(float64)
		if vouchCount != 0 {
			t.Errorf("Expected vouch_count = 0 (vouch in wrong region), got %v", vouchCount)
		}
	})

	t.Run("GetStatus returns correct region for single-region user", func(t *testing.T) {
		// Test that users with only one region still work correctly

		admin := suite.createTestUserWithFlags("sr_admin3", true, true)
		vouchee := suite.createTestUserWithFlags("sr_vouchee3", false, false) // unverified, awaiting vouches

		// Create just a city (no hierarchy)
		city := suite.createTestRegion("StatusRegion Solo City", models.RegionTypeCity, nil, admin.ID)

		suite.addUserToRegionPending(vouchee.ID, city.ID)
		suite.addUserToRegion(admin.ID, city.ID, true)

		defer suite.cleanup(admin.ID, vouchee.ID)
		defer suite.cleanupRegions(city.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE voucher_user_id = ?", admin.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", admin.ID, vouchee.ID)
		}()

		suite.createVouch(admin.ID, vouchee.ID, city.ID)

		req := httptest.NewRequest("GET", "/api/v1/verification/status", nil)
		claims := &middleware.Claims{
			UserID:           vouchee.ID,
			Email:            vouchee.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.GetStatus(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&resp)

		vouchCount, _ := resp["vouch_count"].(float64)
		if vouchCount != 1 {
			t.Errorf("Expected vouch_count = 1, got %v", vouchCount)
		}

		pendingRegion, _ := resp["pending_vouch_region"].(map[string]interface{})
		regionName, _ := pendingRegion["name"].(string)
		if regionName != city.Name {
			t.Errorf("Expected region name '%s', got '%s'", city.Name, regionName)
		}
	})
}

// =============================================================================
// Bug Fix Tests: Circular Vouch Constraint in Pending List
// =============================================================================
// This tests the fix for the bug where the pending vouch requests list showed
// users who had already vouched for the caller, which would violate the circular
// vouch constraint if the caller tried to vouch for them.

func TestVerificationHandler_GetPendingVouchRequests_CircularVouchFilter(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	// Clean up test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@circulartest.com'")
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM geographic_regions WHERE name LIKE 'CircularTest%'")

	t.Run("pending list excludes users who have vouched for the caller", func(t *testing.T) {
		// This tests the bug fix: previously, the pending vouch list included
		// users who had already vouched for the caller, which would create a
		// circular vouch pattern if the caller tried to vouch for them.

		// Create users - unverified members in bootstrap mode
		caller := suite.createTestUserWithFlags("circ_caller", false, false)
		userWhoVouchedForCaller := suite.createTestUserWithFlags("circ_vouched_caller", false, false)
		eligibleUser := suite.createTestUserWithFlags("circ_eligible", false, false)

		// Create region (bootstrap mode - no full admins)
		region := suite.createTestRegion("CircularTest City", models.RegionTypeCity, nil, caller.ID)

		// Add caller as verified member, others as pending (from vouch request)
		suite.addUserToRegion(caller.ID, region.ID, false)
		suite.addUserToRegionPending(userWhoVouchedForCaller.ID, region.ID)
		suite.addUserToRegionPending(eligibleUser.ID, region.ID)

		// userWhoVouchedForCaller vouches for caller - this creates the constraint
		suite.createVouch(userWhoVouchedForCaller.ID, caller.ID, region.ID)

		defer suite.cleanup(caller.ID, userWhoVouchedForCaller.ID, eligibleUser.ID)
		defer suite.cleanupRegions(region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE region_id = ?", region.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE region_id = ?", region.ID)
		}()

		// Get pending vouch requests as caller
		req := httptest.NewRequest("GET", "/api/v1/verification/vouch/pending", nil)
		claims := &middleware.Claims{
			UserID:           caller.ID,
			Email:            caller.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.GetPendingVouchRequests(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		requests, ok := resp["requests"].([]interface{})
		if !ok {
			t.Fatalf("Expected 'requests' array in response, got: %v", resp)
		}

		// Check that userWhoVouchedForCaller is NOT in the list (circular constraint)
		// and eligibleUser IS in the list
		foundCircularUser := false
		foundEligibleUser := false

		for _, r := range requests {
			reqMap, ok := r.(map[string]interface{})
			if !ok {
				continue
			}
			userID, _ := reqMap["user_id"].(string)
			if userID == userWhoVouchedForCaller.ID {
				foundCircularUser = true
			}
			if userID == eligibleUser.ID {
				foundEligibleUser = true
			}
		}

		// Before the fix: foundCircularUser would be true
		// After the fix: foundCircularUser should be false
		if foundCircularUser {
			t.Errorf("Bug: Pending list includes user who vouched for caller (would create circular vouch)")
		}

		if !foundEligibleUser {
			t.Errorf("Expected eligible user to be in pending list")
		}
	})

	t.Run("pending list still excludes users caller has already vouched for", func(t *testing.T) {
		// Verify that the original filter (caller already vouched for user) still works

		caller := suite.createTestUserWithFlags("circ_caller2", false, false)
		alreadyVouchedFor := suite.createTestUserWithFlags("circ_already_vouched", false, false)
		eligibleUser := suite.createTestUserWithFlags("circ_eligible2", false, false)

		region := suite.createTestRegion("CircularTest City2", models.RegionTypeCity, nil, caller.ID)

		suite.addUserToRegion(caller.ID, region.ID, false)
		suite.addUserToRegionPending(alreadyVouchedFor.ID, region.ID)
		suite.addUserToRegionPending(eligibleUser.ID, region.ID)

		// Caller has already vouched for alreadyVouchedFor
		suite.createVouch(caller.ID, alreadyVouchedFor.ID, region.ID)

		defer suite.cleanup(caller.ID, alreadyVouchedFor.ID, eligibleUser.ID)
		defer suite.cleanupRegions(region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE region_id = ?", region.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE region_id = ?", region.ID)
		}()

		req := httptest.NewRequest("GET", "/api/v1/verification/vouch/pending", nil)
		claims := &middleware.Claims{
			UserID:           caller.ID,
			Email:            caller.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.GetPendingVouchRequests(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&resp)

		requests, _ := resp["requests"].([]interface{})

		foundAlreadyVouched := false
		foundEligibleUser := false

		for _, r := range requests {
			reqMap, _ := r.(map[string]interface{})
			userID, _ := reqMap["user_id"].(string)
			if userID == alreadyVouchedFor.ID {
				foundAlreadyVouched = true
			}
			if userID == eligibleUser.ID {
				foundEligibleUser = true
			}
		}

		if foundAlreadyVouched {
			t.Errorf("Pending list should not include users caller has already vouched for")
		}

		if !foundEligibleUser {
			t.Errorf("Expected eligible user to be in pending list")
		}
	})

	t.Run("full admin query also filters circular vouches", func(t *testing.T) {
		// Test that the admin query (GetPendingVouchUsersForAdmin) also filters circular vouches

		admin := suite.createTestUserWithFlags("circ_admin", true, true)
		userWhoVouchedForAdmin := suite.createTestUserWithFlags("circ_vouched_admin", true, false)
		eligibleUser := suite.createTestUserWithFlags("circ_eligible3", true, false)

		region := suite.createTestRegion("CircularTest City3", models.RegionTypeCity, nil, admin.ID)

		// Exit bootstrap mode so admin query is used
		adminIDs := suite.exitBootstrapMode(region.ID)

		suite.addUserToRegion(admin.ID, region.ID, true)
		suite.addUserToRegion(userWhoVouchedForAdmin.ID, region.ID, false)
		suite.addUserToRegion(eligibleUser.ID, region.ID, false)

		// userWhoVouchedForAdmin vouches for admin
		suite.createVouch(userWhoVouchedForAdmin.ID, admin.ID, region.ID)

		defer suite.cleanup(admin.ID, userWhoVouchedForAdmin.ID, eligibleUser.ID)
		defer suite.cleanupRegions(region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE region_id = ?", region.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE region_id = ?", region.ID)
			for _, id := range adminIDs {
				_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", id)
				_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", id)
			}
		}()

		req := httptest.NewRequest("GET", "/api/v1/verification/vouch/pending", nil)
		claims := &middleware.Claims{
			UserID:           admin.ID,
			Email:            admin.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.GetPendingVouchRequests(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&resp)

		requests, _ := resp["requests"].([]interface{})

		foundCircularUser := false
		for _, r := range requests {
			reqMap, _ := r.(map[string]interface{})
			userID, _ := reqMap["user_id"].(string)
			if userID == userWhoVouchedForAdmin.ID {
				foundCircularUser = true
			}
		}

		if foundCircularUser {
			t.Errorf("Bug: Admin pending list includes user who vouched for admin (would create circular vouch)")
		}
	})
}

// =============================================================================
// Security Fix Tests: Voucher Must Be in Target Region
// =============================================================================
// These tests verify that vouching requires the voucher to be a verified
// member of the specific target region. (Security audit #14)

func TestVerificationHandler_Vouch_VoucherMustBeInTargetRegion(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	// Clean up test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@targetregiontest.com'")
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM geographic_regions WHERE name LIKE 'TargetRegion%'")

	t.Run("vouch fails when voucher is not in target region (bootstrap mode)", func(t *testing.T) {
		// Scenario: voucher is verified in Region A, vouchee has a pending
		// vouch request in Region B where voucher has no membership at all.

		voucher := suite.createTestUserWithFlags("tr_voucher1", false, false)
		vouchee := suite.createTestUserWithFlags("tr_vouchee1", false, false)

		// Create two separate regions
		regionA := suite.createTestRegion("TargetRegion CityA", models.RegionTypeCity, nil, voucher.ID)
		regionB := suite.createTestRegion("TargetRegion CityB", models.RegionTypeCity, nil, vouchee.ID)

		// Both users in Region A (so they share a region)
		suite.addUserToRegion(voucher.ID, regionA.ID, false)
		suite.addUserToRegion(vouchee.ID, regionA.ID, false)

		// Vouchee has pending request in Region B (voucher is NOT in Region B)
		suite.addUserToRegionPending(vouchee.ID, regionB.ID)

		defer suite.cleanup(voucher.ID, vouchee.ID)
		defer suite.cleanupRegions(regionA.ID, regionB.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", voucher.ID, vouchee.ID)
		}()

		body := map[string]interface{}{
			"user_id":   vouchee.ID,
			"region_id": regionB.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           voucher.ID,
			Email:            voucher.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.Vouch(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403 (forbidden), got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&resp)
		if resp["error"] != "not_in_region" {
			t.Errorf("Expected error 'not_in_region', got '%v'", resp["error"])
		}
	})

	t.Run("vouch succeeds when voucher is in target region (bootstrap mode)", func(t *testing.T) {
		// Positive case: voucher IS in the target region

		voucher := suite.createTestUserWithFlags("tr_voucher2", false, false)
		vouchee := suite.createTestUserWithFlags("tr_vouchee2", false, false)

		region := suite.createTestRegion("TargetRegion CityC", models.RegionTypeCity, nil, voucher.ID)

		// Both users in the same region; vouchee has pending request there
		suite.addUserToRegion(voucher.ID, region.ID, false)
		suite.addUserToRegionPending(vouchee.ID, region.ID)

		defer suite.cleanup(voucher.ID, vouchee.ID)
		defer suite.cleanupRegions(region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE voucher_user_id = ?", voucher.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", voucher.ID, vouchee.ID)
		}()

		body := map[string]interface{}{
			"user_id":   vouchee.ID,
			"region_id": region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           voucher.ID,
			Email:            voucher.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.Vouch(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("Expected status 201 (created), got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("vouch succeeds when voucher has pending membership in bootstrap mode", func(t *testing.T) {
		// In bootstrap mode, pending members can vouch for each other
		// (they're all bootstrapping together).

		voucher := suite.createTestUserWithFlags("tr_voucher3", false, false)
		vouchee := suite.createTestUserWithFlags("tr_vouchee3", false, false)

		region := suite.createTestRegion("TargetRegion CityD", models.RegionTypeCity, nil, voucher.ID)

		// Both users have pending status in the same region (bootstrap mode)
		suite.addUserToRegionPending(voucher.ID, region.ID)
		suite.addUserToRegionPending(vouchee.ID, region.ID)

		defer suite.cleanup(voucher.ID, vouchee.ID)
		defer suite.cleanupRegions(region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM vouches WHERE voucher_user_id = ?", voucher.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", voucher.ID, vouchee.ID)
		}()

		body := map[string]interface{}{
			"user_id":   vouchee.ID,
			"region_id": region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           voucher.ID,
			Email:            voucher.Email,
			VerificationTier: models.TierUnverified,
			PostcardVerified: false,
			VouchVerified:    false,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.Vouch(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("Expected status 201 (created), got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("vouch fails when voucher has only pending membership in normal mode", func(t *testing.T) {
		// In normal mode, only verified members can vouch. A pending member
		// with fully-verified user flags should still be rejected by the
		// region membership check.

		voucher := suite.createTestUserWithFlags("tr_voucher4", true, true)
		vouchee := suite.createTestUserWithFlags("tr_vouchee4", false, false)

		region := suite.createTestRegion("TargetRegion CityE", models.RegionTypeCity, nil, voucher.ID)

		// Exit bootstrap mode
		adminIDs := suite.exitBootstrapMode(region.ID)

		// Voucher has only pending membership in the target region
		suite.addUserToRegionPending(voucher.ID, region.ID)
		suite.addUserToRegionPending(vouchee.ID, region.ID)

		defer suite.cleanup(voucher.ID, vouchee.ID)
		defer suite.cleanupRegions(region.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", voucher.ID, vouchee.ID)
			for _, id := range adminIDs {
				_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", id)
				_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", id)
			}
		}()

		body := map[string]interface{}{
			"user_id":   vouchee.ID,
			"region_id": region.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           voucher.ID,
			Email:            voucher.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.Vouch(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403 (forbidden), got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&resp)
		if resp["error"] != "not_in_region" {
			t.Errorf("Expected error 'not_in_region', got '%v'", resp["error"])
		}
	})

	t.Run("vouch fails when voucher is not in target region (normal mode)", func(t *testing.T) {
		// Cross-region scenario in normal mode (>= 3 admins)

		voucher := suite.createTestUserWithFlags("tr_voucher5", true, true)
		vouchee := suite.createTestUserWithFlags("tr_vouchee5", false, false)

		regionA := suite.createTestRegion("TargetRegion CityF", models.RegionTypeCity, nil, voucher.ID)
		regionB := suite.createTestRegion("TargetRegion CityG", models.RegionTypeCity, nil, voucher.ID)

		// Exit bootstrap mode in Region B
		adminIDs := suite.exitBootstrapMode(regionB.ID)

		// Both users share Region A
		suite.addUserToRegion(voucher.ID, regionA.ID, true)
		suite.addUserToRegion(vouchee.ID, regionA.ID, false)

		// Vouchee has pending request in Region B; voucher is NOT in Region B
		suite.addUserToRegionPending(vouchee.ID, regionB.ID)

		defer suite.cleanup(voucher.ID, vouchee.ID)
		defer suite.cleanupRegions(regionA.ID, regionB.ID)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id IN (?, ?)", voucher.ID, vouchee.ID)
			for _, id := range adminIDs {
				_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_regions WHERE user_id = ?", id)
				_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", id)
			}
		}()

		body := map[string]interface{}{
			"user_id":   vouchee.ID,
			"region_id": regionB.ID,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/verification/vouch", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		claims := &middleware.Claims{
			UserID:           voucher.ID,
			Email:            voucher.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
		}
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		suite.handler.Vouch(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403 (forbidden), got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&resp)
		if resp["error"] != "not_in_region" {
			t.Errorf("Expected error 'not_in_region', got '%v'", resp["error"])
		}
	})
}

func TestVerificationHandler_VerifyCode_Lockout(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@verificationtest.com'")

	userA := suite.createTestUser("lockout_user_a", models.TierUnverified)
	userB := suite.createTestUser("lockout_user_b", models.TierUnverified)
	region := suite.createTestRegion("Test Region Lockout", models.RegionTypeCity, nil, userA.ID)

	defer suite.cleanup(userA.ID, userB.ID)
	defer suite.cleanupRegions(region.ID)

	t.Run("lockout after 5 failed attempts from wrong user", func(t *testing.T) {
		verificationCode := "lockouttest1234"
		verReq := suite.createVerificationRequest(userA.ID, region.ID, verificationCode, models.VerificationStatusMailed)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE id = ?", verReq.ID)
		}()

		claimsB := &middleware.Claims{
			UserID:           userB.ID,
			Email:            userB.Email,
			VerificationTier: models.TierUnverified,
		}

		// Attempt 5 times as wrong user — should get 403 for first 4, then 429 on the 5th
		for i := 1; i <= verificationCodeLockoutThreshold; i++ {
			body := map[string]interface{}{"verification_code": verificationCode}
			bodyBytes, _ := json.Marshal(body)
			req := httptest.NewRequest("POST", "/api/v1/verification/postcard/verify", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			ctx := middleware.ContextWithUser(req.Context(), claimsB)
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()
			suite.handler.VerifyCode(rec, req)

			if i < verificationCodeLockoutThreshold {
				if rec.Code != http.StatusForbidden {
					t.Errorf("Attempt %d: expected 403, got %d: %s", i, rec.Code, rec.Body.String())
				}
			} else {
				if rec.Code != http.StatusTooManyRequests {
					t.Errorf("Attempt %d: expected 429, got %d: %s", i, rec.Code, rec.Body.String())
				}
			}
		}

		// 6th attempt — even the correct user should get 429
		claimsA := &middleware.Claims{
			UserID:           userA.ID,
			Email:            userA.Email,
			VerificationTier: models.TierUnverified,
		}
		body := map[string]interface{}{"verification_code": verificationCode}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx := middleware.ContextWithUser(req.Context(), claimsA)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VerifyCode(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("Post-lockout attempt by correct user: expected 429, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp["error"] != "verification_code_locked" {
			t.Errorf("Expected error 'verification_code_locked', got '%v'", resp["error"])
		}
	})

	t.Run("under threshold still succeeds for correct user", func(t *testing.T) {
		verificationCode := "underthreshold12"
		verReq := suite.createVerificationRequest(userA.ID, region.ID, verificationCode, models.VerificationStatusMailed)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE id = ?", verReq.ID)
		}()

		claimsB := &middleware.Claims{
			UserID:           userB.ID,
			Email:            userB.Email,
			VerificationTier: models.TierUnverified,
		}

		// 4 failed attempts (under threshold of 5)
		for i := 1; i <= verificationCodeLockoutThreshold-1; i++ {
			body := map[string]interface{}{"verification_code": verificationCode}
			bodyBytes, _ := json.Marshal(body)
			req := httptest.NewRequest("POST", "/api/v1/verification/postcard/verify", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			ctx := middleware.ContextWithUser(req.Context(), claimsB)
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()
			suite.handler.VerifyCode(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Errorf("Attempt %d: expected 403, got %d: %s", i, rec.Code, rec.Body.String())
			}
		}

		// Correct user should still succeed
		claimsA := &middleware.Claims{
			UserID:           userA.ID,
			Email:            userA.Email,
			VerificationTier: models.TierUnverified,
		}
		body := map[string]interface{}{"verification_code": verificationCode}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx := middleware.ContextWithUser(req.Context(), claimsA)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		suite.handler.VerifyCode(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Correct user after %d failed attempts: expected 200, got %d: %s",
				verificationCodeLockoutThreshold-1, rec.Code, rec.Body.String())
		}

		// Reset user tier for other tests
		_, _ = suite.db.ExecContext(context.Background(), "UPDATE users SET verification_tier = ?, postcard_verified = false WHERE id = ?", models.TierUnverified, userA.ID)
	})
}

func TestGenerateVerificationCode_Length(t *testing.T) {
	code, err := generateVerificationCode()
	if err != nil {
		t.Fatalf("generateVerificationCode() returned error: %v", err)
	}
	if len(code) != 16 {
		t.Errorf("Expected 16-char hex code, got %d chars: %q", len(code), code)
	}
	// Verify it's valid hex
	for _, c := range code {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("Code contains non-hex character: %c in %q", c, code)
		}
	}
}

