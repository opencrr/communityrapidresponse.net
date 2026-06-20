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
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

type verificationTestSuite struct {
	t                *testing.T
	db               *database.DB
	userRepo         *database.UserRepository
	regionRepo       *database.RegionRepository
	verificationRepo *database.VerificationRepository
	postgridService  *mocks.MockPostgridService
	mapboxService    *mocks.MockMapboxService
	handler          *VerificationHandler
}

func setupVerificationTestSuite(t *testing.T) *verificationTestSuite {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	regionRepo := database.NewRegionRepository(db)
	verificationRepo := database.NewVerificationRepository(db)
	postgridService := mocks.NewMockPostgridService()
	mapboxService := mocks.NewMockMapboxService()

	handler := NewVerificationHandler(
		nil,
		verificationRepo,
		userRepo,
		regionRepo,
		postgridService,
		mapboxService,
		nil,
	)

	return &verificationTestSuite{
		t:                t,
		db:               db,
		userRepo:         userRepo,
		regionRepo:       regionRepo,
		verificationRepo: verificationRepo,
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
func (s *verificationTestSuite) createNestedRegionHierarchy(prefix, createdBy string) (state, county, city *models.GeographicRegion, cleanup func()) {
	stateGeoJSON := `{"type":"Polygon","coordinates":[[[-120.0,38.0],[-114.0,38.0],[-114.0,42.0],[-120.0,42.0],[-120.0,38.0]]]}`
	state = s.createTestRegionWithCustomGeometry(prefix+" State", models.RegionTypeState, nil, createdBy, stateGeoJSON)

	countyGeoJSON := `{"type":"Polygon","coordinates":[[[-118.0,39.0],[-116.0,39.0],[-116.0,41.0],[-118.0,41.0],[-118.0,39.0]]]}`
	county = s.createTestRegionWithCustomGeometry(prefix+" County", models.RegionTypeCounty, &state.ID, createdBy, countyGeoJSON)

	cityGeoJSON := `{"type":"Polygon","coordinates":[[[-117.5,39.5],[-116.5,39.5],[-116.5,40.5],[-117.5,40.5],[-117.5,39.5]]]}`
	city = s.createTestRegionWithCustomGeometry(prefix+" City", models.RegionTypeCity, &county.ID, createdBy, cityGeoJSON)

	cleanup = func() {
		s.cleanupRegions(city.ID)
		s.cleanupRegions(county.ID)
		s.cleanupRegions(state.ID)
	}

	return state, county, city, cleanup
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
			suite.userRepo,
			suite.regionRepo,
			suite.postgridService,
			suite.mapboxService,
			nil,
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
			suite.userRepo,
			suite.regionRepo,
			suite.postgridService,
			suite.mapboxService,
			nil,
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
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("Code contains non-hex character: %c in %q", c, code)
		}
	}
}

// =============================================================================
// RequestPostcardVerification Edge Cases
// =============================================================================

func TestVerificationHandler_RequestPostcardVerification_InvalidJSON(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@invalidjsontest.com'")

	user := &models.User{
		Username:         "invalidjsontest",
		Email:            "user@invalidjsontest.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: models.TierVouched,
		VouchVerified:    true,
	}
	if err := suite.userRepo.Create(context.Background(), user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	_, _ = suite.db.ExecContext(context.Background(), "UPDATE users SET vouch_verified = TRUE WHERE id = ?", user.ID)
	defer suite.cleanup(user.ID)

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/request", bytes.NewReader([]byte("not json")))
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
	})
}

func TestVerificationHandler_VerifyCode_InvalidJSON(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@verifyjsontest.com'")

	user := &models.User{
		Username:         "verifyjsontest",
		Email:            "user@verifyjsontest.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: models.TierUnverified,
	}
	if err := suite.userRepo.Create(context.Background(), user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer suite.cleanup(user.ID)

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/verification/postcard/verify", bytes.NewReader([]byte("not json")))
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

func TestVerificationHandler_GetStatus_PostcardVerifiedUser(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@statuspctest.com'")

	user := &models.User{
		Username:         "statuspctest",
		Email:            "user@statuspctest.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: models.TierPostcard,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	if err := suite.userRepo.Create(context.Background(), user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	_, _ = suite.db.ExecContext(context.Background(),
		"UPDATE users SET postcard_verified = TRUE, vouch_verified = TRUE WHERE id = ?", user.ID)
	defer suite.cleanup(user.ID)

	t.Run("postcard verified user shows correct tier", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/verification/status", nil)

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierPostcard,
			PostcardVerified: true,
			VouchVerified:    true,
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

		postcardVerified, ok := resp["postcard_verified"].(bool)
		if !ok || !postcardVerified {
			t.Errorf("Expected postcard_verified = true, got %v", resp["postcard_verified"])
		}

		vouchVerified, ok := resp["vouch_verified"].(bool)
		if !ok || !vouchVerified {
			t.Errorf("Expected vouch_verified = true, got %v", resp["vouch_verified"])
		}
	})
}

// =============================================================================
// Mapbox Geocode Failure Test
// =============================================================================

func TestVerificationHandler_RequestPostcardVerification_MapboxFailure(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@mapboxfailtest.com'")

	user := &models.User{
		Username:         "mapboxfailtest",
		Email:            "user@mapboxfailtest.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: models.TierVouched,
		VouchVerified:    true,
	}
	if err := suite.userRepo.Create(context.Background(), user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	_, _ = suite.db.ExecContext(context.Background(),
		"UPDATE users SET vouch_verified = TRUE WHERE id = ?", user.ID)
	defer suite.cleanup(user.ID)

	t.Run("mapbox service failure returns 502", func(t *testing.T) {
		suite.postgridService.Reset()
		suite.mapboxService.Reset()
		suite.mapboxService.ShouldFail = true

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

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp["error"] != "geocoding_failed" {
			t.Errorf("Expected error 'geocoding_failed', got %v", resp["error"])
		}

		suite.mapboxService.ShouldFail = false
	})
}

// =============================================================================
// Commercial Address Rejection Test
// =============================================================================

func TestVerificationHandler_RequestPostcardVerification_CommercialAddress(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@commercialtest.com'")

	user := &models.User{
		Username:         "commercialtest",
		Email:            "user@commercialtest.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: models.TierVouched,
		VouchVerified:    true,
	}
	if err := suite.userRepo.Create(context.Background(), user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	_, _ = suite.db.ExecContext(context.Background(),
		"UPDATE users SET vouch_verified = TRUE WHERE id = ?", user.ID)
	defer suite.cleanup(user.ID)

	t.Run("commercial address is rejected", func(t *testing.T) {
		suite.postgridService.Reset()
		suite.postgridService.DefaultIsCommercial = true

		body := map[string]interface{}{
			"address": map[string]string{
				"line1":       "100 Business Park Dr",
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
		if resp["error"] != "commercial_not_allowed" {
			t.Errorf("Expected error 'commercial_not_allowed', got %v", resp["error"])
		}

		suite.postgridService.DefaultIsCommercial = false
	})
}

// =============================================================================
// GetStatus Vouch Verified Only User Test
// =============================================================================

func TestVerificationHandler_GetStatus_VouchVerifiedUser(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@statusvouchtest.com'")

	user := &models.User{
		Username:         "statusvouchtest",
		Email:            "user@statusvouchtest.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: models.TierVouched,
		VouchVerified:    true,
		PostcardVerified: false,
	}
	if err := suite.userRepo.Create(context.Background(), user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	_, _ = suite.db.ExecContext(context.Background(),
		"UPDATE users SET vouch_verified = TRUE, postcard_verified = FALSE WHERE id = ?", user.ID)
	defer suite.cleanup(user.ID)

	t.Run("vouch verified user shows correct status", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/verification/status", nil)

		claims := &middleware.Claims{
			UserID:           user.ID,
			Email:            user.Email,
			VerificationTier: models.TierVouched,
			VouchVerified:    true,
			PostcardVerified: false,
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

		tier, ok := resp["verification_tier"].(float64)
		if !ok || int(tier) != int(models.TierVouched) {
			t.Errorf("Expected verification_tier = %d, got %v", models.TierVouched, resp["verification_tier"])
		}

		vouchVerified, ok := resp["vouch_verified"].(bool)
		if !ok || !vouchVerified {
			t.Errorf("Expected vouch_verified = true, got %v", resp["vouch_verified"])
		}

		postcardVerified, ok := resp["postcard_verified"].(bool)
		if !ok || postcardVerified {
			t.Errorf("Expected postcard_verified = false, got %v", resp["postcard_verified"])
		}
	})
}

// =============================================================================
// RequestPostcardVerification Region Not Found Test
// =============================================================================

// =============================================================================
// Full Integration Flow Test - traces data through Postgrid + Mapbox mocks
// =============================================================================

func TestVerificationHandler_FullFlow_ServiceInteraction(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@fullflowtest.com'")
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM geographic_regions WHERE name LIKE '%FullFlow%'")

	user := &models.User{
		Username:         "fullflowtest",
		Email:            "user@fullflowtest.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: models.TierVouched,
		VouchVerified:    true,
	}
	if err := suite.userRepo.Create(context.Background(), user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	_, _ = suite.db.ExecContext(context.Background(),
		"UPDATE users SET vouch_verified = TRUE WHERE id = ?", user.ID)
	defer suite.cleanup(user.ID)

	region := suite.createTestRegion("FullFlow Test Region", models.RegionTypeCity, nil, user.ID)
	defer suite.cleanupRegions(region.ID)

	t.Run("custom mocks trace exact address through service chain", func(t *testing.T) {
		suite.postgridService.Reset()
		suite.mapboxService.Reset()

		// Track which address was validated and sent
		var validatedAddress *models.Address
		var sentAddress *models.Address
		var sentVerificationCode string
		var geocodedAddress *models.Address

		suite.postgridService.ValidateAddressFunc = func(ctx context.Context, address *models.Address) (*services.AddressValidationResult, error) {
			validatedAddress = address
			return &services.AddressValidationResult{
				IsDeliverable:       true,
				Deliverability:      "deliverable",
				IsPOBox:             false,
				IsCMRA:              false,
				IsCommercial:        false,
				AddressType:         "residential",
				StandardizedAddress: address,
			}, nil
		}

		suite.postgridService.SendPostcardFunc = func(ctx context.Context, address *models.Address, verificationCode, postcardRef string) (string, error) {
			sentAddress = address
			sentVerificationCode = verificationCode
			return "postcard_flow_test_001", nil
		}

		suite.mapboxService.GeocodeAddressFunc = func(ctx context.Context, address *models.Address) (*services.GeocodeResult, error) {
			geocodedAddress = address
			return &services.GeocodeResult{
				Latitude:      37.7749,
				Longitude:     -122.4194,
				BoundaryType:  "city",
				BoundaryName:  "San Francisco",
				BoundaryState: "California",
				PlaceID:       "place.test_flow",
				CountyName:    "San Francisco County",
			}, nil
		}

		inputAddress := map[string]string{
			"line1":       "742 Evergreen Terrace",
			"city":        "San Francisco",
			"state":       "CA",
			"postal_code": "94102",
		}

		body := map[string]interface{}{
			"region_id": region.ID,
			"address":   inputAddress,
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

		if rec.Code != http.StatusCreated {
			t.Fatalf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify the exact address flowed through ValidateAddress
		if validatedAddress == nil {
			t.Fatal("ValidateAddress was not called")
		}
		if validatedAddress.Line1 != "742 Evergreen Terrace" {
			t.Errorf("ValidateAddress received wrong line1: %s", validatedAddress.Line1)
		}

		// Verify the exact address flowed through GeocodeAddress
		if geocodedAddress == nil {
			t.Fatal("GeocodeAddress was not called")
		}
		if geocodedAddress.City != "San Francisco" {
			t.Errorf("GeocodeAddress received wrong city: %s", geocodedAddress.City)
		}

		// Verify the address flowed through SendPostcard
		if sentAddress == nil {
			t.Fatal("SendPostcard was not called")
		}
		if sentAddress.Line1 != "742 Evergreen Terrace" {
			t.Errorf("SendPostcard received wrong line1: %s", sentAddress.Line1)
		}

		// Verify a verification code was generated
		if sentVerificationCode == "" {
			t.Error("SendPostcard received empty verification code")
		}
		// Verification codes should be 16 hex chars (8 random bytes)
		if len(sentVerificationCode) != 16 {
			t.Errorf("Expected 16-char verification code, got %d chars: %s", len(sentVerificationCode), sentVerificationCode)
		}

		// Clean up verification request
		_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM verification_requests WHERE user_id = ?", user.ID)

		// Reset custom functions
		suite.postgridService.ValidateAddressFunc = nil
		suite.postgridService.SendPostcardFunc = nil
		suite.mapboxService.GeocodeAddressFunc = nil
	})

	t.Run("validate address failure prevents geocode and postcard send", func(t *testing.T) {
		suite.postgridService.Reset()
		suite.mapboxService.Reset()

		geocodeCalled := false
		sendPostcardCalled := false

		suite.postgridService.ValidateAddressFunc = func(ctx context.Context, address *models.Address) (*services.AddressValidationResult, error) {
			return nil, fmt.Errorf("postgrid service unavailable")
		}

		suite.mapboxService.GeocodeAddressFunc = func(ctx context.Context, address *models.Address) (*services.GeocodeResult, error) {
			geocodeCalled = true
			return nil, fmt.Errorf("should not be called")
		}

		suite.postgridService.SendPostcardFunc = func(ctx context.Context, address *models.Address, verificationCode, postcardRef string) (string, error) {
			sendPostcardCalled = true
			return "", fmt.Errorf("should not be called")
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

		// Verify the service call chain was short-circuited
		if geocodeCalled {
			t.Error("GeocodeAddress should not be called when ValidateAddress fails")
		}
		if sendPostcardCalled {
			t.Error("SendPostcard should not be called when ValidateAddress fails")
		}

		// Reset custom functions
		suite.postgridService.ValidateAddressFunc = nil
		suite.postgridService.SendPostcardFunc = nil
		suite.mapboxService.GeocodeAddressFunc = nil
	})
}

func TestVerificationHandler_RequestPostcardVerification_RegionNotFound(t *testing.T) {
	suite := setupVerificationTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@regionnotfound.com'")

	user := &models.User{
		Username:         "regionnotfound",
		Email:            "user@regionnotfound.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: models.TierVouched,
		VouchVerified:    true,
	}
	if err := suite.userRepo.Create(context.Background(), user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	_, _ = suite.db.ExecContext(context.Background(),
		"UPDATE users SET vouch_verified = TRUE WHERE id = ?", user.ID)
	defer suite.cleanup(user.ID)

	t.Run("nonexistent region_id returns 404", func(t *testing.T) {
		suite.postgridService.Reset()
		suite.mapboxService.Reset()

		body := map[string]interface{}{
			"region_id": "nonexistent-region-id-12345",
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

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

