package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// testDB returns a database connection for testing
// Skip tests if no database is configured
func testDB(t *testing.T) *DB {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping database tests")
	}

	port := 3306
	if p := os.Getenv("TEST_DB_PORT"); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &port)
	}

	cfg := &config.DatabaseConfig{
		Host:     host,
		Port:     port,
		User:     getEnvOrDefault("TEST_DB_USER", "root"),
		Password: getEnvOrDefault("TEST_DB_PASSWORD", ""),
		Name:     getEnvOrDefault("TEST_DB_NAME", "communityrapidresponse_test"),
		Charset:  "utf8mb4",
	}

	db, err := New(cfg)
	if err != nil {
		t.Skipf("Failed to connect to test database (skipping): %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Helper to create a string pointer
func strPtr(s string) *string {
	return &s
}

func TestDB_Transaction(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	t.Run("commits successful transaction", func(t *testing.T) {
		err := db.Transaction(ctx, func(tx *sql.Tx) error {
			// This would contain actual queries in a real test
			return nil
		})
		if err != nil {
			t.Errorf("Transaction failed: %v", err)
		}
	})
}

func TestUserRepository_Create(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	// Clean up any existing test user
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'test_create@example.com'")

	t.Run("creates user successfully", func(t *testing.T) {
		user := &models.User{
			Username:         "testcreate",
			Email:            "test_create@example.com",
			PasswordHash:     "hashed_password",
			VerificationTier: models.TierUnverified,
		}

		err := repo.Create(ctx, user)
		if err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}

		if user.ID == "" {
			t.Error("Expected user ID to be set")
		}
		if user.CreatedAt.IsZero() {
			t.Error("Expected CreatedAt to be set")
		}

		// Cleanup
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	t.Run("rejects duplicate username", func(t *testing.T) {
		// Cleanup before test to avoid conflicts from previous runs
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE username = 'dupeuser'")

		user1 := &models.User{
			Username:     "dupeuser",
			Email:        "dupe1@example.com",
			PasswordHash: "hash",
		}
		user2 := &models.User{
			Username:     "dupeuser",
			Email:        "dupe2@example.com",
			PasswordHash: "hash",
		}

		if err := repo.Create(ctx, user1); err != nil {
			t.Fatalf("Failed to create first user: %v", err)
		}
		err := repo.Create(ctx, user2)

		if err != ErrUserAlreadyExists {
			t.Errorf("Expected ErrUserAlreadyExists, got %v", err)
		}

		// Cleanup
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE username = 'dupeuser'")
	})

	t.Run("rejects duplicate email", func(t *testing.T) {
		// Cleanup before test to avoid conflicts from previous runs
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'dupeemail@example.com'")

		user1 := &models.User{
			Username:     "emailuser1",
			Email:        "dupeemail@example.com",
			PasswordHash: "hash",
		}
		user2 := &models.User{
			Username:     "emailuser2",
			Email:        "dupeemail@example.com",
			PasswordHash: "hash",
		}

		if err := repo.Create(ctx, user1); err != nil {
			t.Fatalf("Failed to create first user: %v", err)
		}
		err := repo.Create(ctx, user2)

		if err != ErrEmailAlreadyExists {
			t.Errorf("Expected ErrEmailAlreadyExists, got %v", err)
		}

		// Cleanup
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'dupeemail@example.com'")
	})
}

func TestUserRepository_GetByID(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	t.Run("retrieves existing user", func(t *testing.T) {
		// Cleanup before test to avoid conflicts from previous runs
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'getbyid@example.com'")

		user := &models.User{
			Username:         "getbyid",
			Email:            "getbyid@example.com",
			PasswordHash:     "hash",
			VerificationTier: models.TierPostcard,
		}
		if err := repo.Create(ctx, user); err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}

		retrieved, err := repo.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}

		if retrieved.Username != user.Username {
			t.Errorf("Expected username '%s', got '%s'", user.Username, retrieved.Username)
		}
		if retrieved.Email != user.Email {
			t.Errorf("Expected email '%s', got '%s'", user.Email, retrieved.Email)
		}
		if retrieved.VerificationTier != models.TierPostcard {
			t.Errorf("Expected tier %d, got %d", models.TierPostcard, retrieved.VerificationTier)
		}

		// Cleanup
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	t.Run("returns error for non-existent user", func(t *testing.T) {
		_, err := repo.GetByID(ctx, "non-existent-id")
		if err != ErrUserNotFound {
			t.Errorf("Expected ErrUserNotFound, got %v", err)
		}
	})
}

func TestUserRepository_GetByEmail(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	t.Run("retrieves user by email", func(t *testing.T) {
		// Cleanup before test to avoid conflicts from previous runs
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'findbyemail@example.com'")

		user := &models.User{
			Username:     "emailtest",
			Email:        "findbyemail@example.com",
			PasswordHash: "hash",
		}
		if err := repo.Create(ctx, user); err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}

		retrieved, err := repo.GetByEmail(ctx, "findbyemail@example.com")
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}

		if retrieved.ID != user.ID {
			t.Errorf("Expected ID '%s', got '%s'", user.ID, retrieved.ID)
		}

		// Cleanup
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	t.Run("returns error for non-existent email", func(t *testing.T) {
		_, err := repo.GetByEmail(ctx, "nonexistent@example.com")
		if err != ErrUserNotFound {
			t.Errorf("Expected ErrUserNotFound, got %v", err)
		}
	})
}

func TestUserRepository_UpdateVerificationTier(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	// Cleanup before test to avoid conflicts from previous runs
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'tiertest@example.com'")

	user := &models.User{
		Username:         "tiertest",
		Email:            "tiertest@example.com",
		PasswordHash:     "hash",
		VerificationTier: models.TierUnverified,
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	err := repo.UpdateVerificationTier(ctx, user.ID, models.TierVouched)
	if err != nil {
		t.Fatalf("Failed to update tier: %v", err)
	}

	retrieved, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to get user after update: %v", err)
	}
	if retrieved.VerificationTier != models.TierVouched {
		t.Errorf("Expected tier %d, got %d", models.TierVouched, retrieved.VerificationTier)
	}

	// Cleanup
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
}

func TestUserRepository_UpdateLastLogin(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	// Cleanup before test to avoid conflicts from previous runs
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'logintest@example.com'")

	user := &models.User{
		Username:     "logintest",
		Email:        "logintest@example.com",
		PasswordHash: "hash",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	before, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}
	if before.LastLogin != nil {
		t.Error("Expected LastLogin to be nil initially")
	}

	err = repo.UpdateLastLogin(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to update last login: %v", err)
	}

	after, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to get user after update: %v", err)
	}
	if after.LastLogin == nil {
		t.Error("Expected LastLogin to be set")
	}

	// Cleanup
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
}

func TestVerificationRepository_CreateAndGetByCode(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	verifyRepo := NewVerificationRepository(db)
	ctx := context.Background()

	// Create a user first
	user := &models.User{
		Username:     "verifytest",
		Email:        "verifytest@example.com",
		PasswordHash: "hash",
	}
	_ = userRepo.Create(ctx, user)

	t.Run("creates and retrieves verification request", func(t *testing.T) {
		req := &models.VerificationRequest{
			UserID:            user.ID,
			VerificationCode:  "TEST123",
			Status:            models.VerificationStatusPending,
			PostgridRequestID: "postgrid_123",
			BoundaryType:      "city",
			BoundaryName:      "San Francisco",
			BoundaryState:     "California",
		}

		err := verifyRepo.CreateVerificationRequest(ctx, req)
		if err != nil {
			t.Fatalf("Failed to create verification request: %v", err)
		}

		if req.ID == "" {
			t.Error("Expected ID to be set")
		}

		retrieved, err := verifyRepo.GetByCode(ctx, "TEST123")
		if err != nil {
			t.Fatalf("Failed to get by code: %v", err)
		}

		if retrieved.UserID != user.ID {
			t.Errorf("Expected user ID '%s', got '%s'", user.ID, retrieved.UserID)
		}
		if retrieved.Status != models.VerificationStatusPending {
			t.Errorf("Expected status 'pending', got '%s'", retrieved.Status)
		}

		// Cleanup
		_, _ = db.ExecContext(ctx, "DELETE FROM verification_requests WHERE id = ?", req.ID)
	})

	t.Run("returns error for invalid code", func(t *testing.T) {
		_, err := verifyRepo.GetByCode(ctx, "INVALID_CODE")
		if err != ErrVerificationNotFound {
			t.Errorf("Expected ErrVerificationNotFound, got %v", err)
		}
	})

	// Cleanup
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
}

func TestVerificationRepository_UpdateStatus(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	verifyRepo := NewVerificationRepository(db)
	ctx := context.Background()

	user := &models.User{
		Username:     "statustest",
		Email:        "statustest@example.com",
		PasswordHash: "hash",
	}
	_ = userRepo.Create(ctx, user)

	req := &models.VerificationRequest{
		UserID:            user.ID,
		VerificationCode:  "STATUS123",
		Status:            models.VerificationStatusPending,
		PostgridRequestID: "postgrid_456",
		BoundaryType:      "city",
		BoundaryName:      "Test City",
		BoundaryState:     "Test State",
	}
	_ = verifyRepo.CreateVerificationRequest(ctx, req)

	err := verifyRepo.UpdateStatus(ctx, req.ID, models.VerificationStatusMailed)
	if err != nil {
		t.Fatalf("Failed to update status: %v", err)
	}

	retrieved, _ := verifyRepo.GetByCode(ctx, "STATUS123")
	if retrieved.Status != models.VerificationStatusMailed {
		t.Errorf("Expected status 'mailed', got '%s'", retrieved.Status)
	}

	// Cleanup
	_, _ = db.ExecContext(ctx, "DELETE FROM verification_requests WHERE id = ?", req.ID)
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
}

func TestVerificationRepository_MarkVerified(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	verifyRepo := NewVerificationRepository(db)
	ctx := context.Background()

	user := &models.User{
		Username:     "verifiedtest",
		Email:        "verifiedtest@example.com",
		PasswordHash: "hash",
	}
	_ = userRepo.Create(ctx, user)

	req := &models.VerificationRequest{
		UserID:            user.ID,
		VerificationCode:  "VERIFY123",
		Status:            models.VerificationStatusMailed,
		PostgridRequestID: "postgrid_789",
		BoundaryType:      "city",
		BoundaryName:      "Test City",
		BoundaryState:     "Test State",
	}
	_ = verifyRepo.CreateVerificationRequest(ctx, req)

	err := verifyRepo.MarkVerified(ctx, req.ID)
	if err != nil {
		t.Fatalf("Failed to mark verified: %v", err)
	}

	retrieved, _ := verifyRepo.GetByCode(ctx, "VERIFY123")
	if retrieved.Status != models.VerificationStatusVerified {
		t.Errorf("Expected status 'verified', got '%s'", retrieved.Status)
	}
	if retrieved.VerifiedAt == nil {
		t.Error("Expected VerifiedAt to be set")
	}

	// Cleanup
	_, _ = db.ExecContext(ctx, "DELETE FROM verification_requests WHERE id = ?", req.ID)
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
}

func TestVouchRepository(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	vouchRepo := NewVouchRepository(db)
	ctx := context.Background()

	// Create voucher and vouchee
	voucher := &models.User{
		Username:         "voucher",
		Email:            "voucher@example.com",
		PasswordHash:     "hash",
		VerificationTier: models.TierPostcard,
	}
	_ = userRepo.Create(ctx, voucher)

	vouchee := &models.User{
		Username:         "vouchee",
		Email:            "vouchee@example.com",
		PasswordHash:     "hash",
		VerificationTier: models.TierUnverified,
	}
	_ = userRepo.Create(ctx, vouchee)

	t.Run("creates vouch and counts correctly", func(t *testing.T) {
		// Create vouch (without region - foreign key constraint)
		vouch := &models.Vouch{
			VoucherUserID: voucher.ID,
			VouchedUserID: vouchee.ID,
			RegionID:      nil, // No region for this test
		}
		err := vouchRepo.Create(ctx, vouch)
		if err != nil {
			t.Fatalf("Failed to create vouch: %v", err)
		}

		// Check HasVouched
		hasVouched, _ := vouchRepo.HasVouched(ctx, voucher.ID, vouchee.ID)
		if !hasVouched {
			t.Error("Expected HasVouched to return true")
		}

		// Cleanup
		_, _ = db.ExecContext(ctx, "DELETE FROM vouches WHERE id = ?", vouch.ID)
	})

	// Cleanup
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", voucher.ID)
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", vouchee.ID)
}

func TestRegionRepository_AddAndRemoveUserFromRegion(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	ctx := context.Background()

	user := &models.User{
		Username:     "regionuser",
		Email:        "regionuser@example.com",
		PasswordHash: "hash",
	}
	_ = userRepo.Create(ctx, user)

	// Create a test region
	region := &models.GeographicRegion{
		Name:       "Test Region",
		RegionType: models.RegionTypeCity,
		CreatedBy:  strPtr(user.ID),
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = regionRepo.Create(ctx, region, geoJSON)

	t.Run("adds user to region as admin", func(t *testing.T) {
		err := regionRepo.AddUserToRegion(ctx, user.ID, region.ID, true)
		if err != nil {
			t.Fatalf("Failed to add user to region: %v", err)
		}

		isAdmin, err := regionRepo.IsUserAdmin(ctx, user.ID, region.ID)
		if err != nil {
			t.Fatalf("Failed to check admin status: %v", err)
		}
		if !isAdmin {
			t.Error("Expected user to be admin")
		}
	})

	t.Run("removes user from region", func(t *testing.T) {
		err := regionRepo.RemoveUserFromRegion(ctx, user.ID, region.ID)
		if err != nil {
			t.Fatalf("Failed to remove user from region: %v", err)
		}

		isAdmin, _ := regionRepo.IsUserAdmin(ctx, user.ID, region.ID)
		if isAdmin {
			t.Error("Expected user to no longer be admin")
		}
	})

	// Cleanup
	_, _ = db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", user.ID)
	_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
}

func TestRegionRepository_GetAdminCount(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	ctx := context.Background()

	// Create test users
	admin1 := &models.User{Username: "admin1", Email: "admin1@test.com", PasswordHash: "hash"}
	admin2 := &models.User{Username: "admin2", Email: "admin2@test.com", PasswordHash: "hash"}
	member := &models.User{Username: "member", Email: "member@test.com", PasswordHash: "hash"}
	_ = userRepo.Create(ctx, admin1)
	_ = userRepo.Create(ctx, admin2)
	_ = userRepo.Create(ctx, member)

	// Create region
	region := &models.GeographicRegion{
		Name:       "Admin Count Test Region",
		RegionType: models.RegionTypeCity,
		CreatedBy:  strPtr(admin1.ID),
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	_ = regionRepo.Create(ctx, region, geoJSON)

	// Add users
	_ = regionRepo.AddUserToRegion(ctx, admin1.ID, region.ID, true)
	_ = regionRepo.AddUserToRegion(ctx, admin2.ID, region.ID, true)
	_ = regionRepo.AddUserToRegion(ctx, member.ID, region.ID, false)

	count, err := regionRepo.GetAdminCount(ctx, region.ID)
	if err != nil {
		t.Fatalf("Failed to get admin count: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 admins, got %d", count)
	}

	// Cleanup
	_, _ = db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", region.ID)
	_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?, ?)", admin1.ID, admin2.ID, member.ID)
}

// Helper struct for expiration test
func TestVerificationRepository_GetPendingByUserID(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	verifyRepo := NewVerificationRepository(db)
	ctx := context.Background()

	user := &models.User{
		Username:     "pendingtest",
		Email:        "pendingtest@example.com",
		PasswordHash: "hash",
	}
	_ = userRepo.Create(ctx, user)

	// Create multiple verification requests with different statuses
	pending := &models.VerificationRequest{
		UserID:            user.ID,
		VerificationCode:  "PENDING1",
		Status:            models.VerificationStatusPending,
		PostgridRequestID: "pg1",
		BoundaryType:      "city",
		BoundaryName:      "City1",
		BoundaryState:     "State1",
	}
	_ = verifyRepo.CreateVerificationRequest(ctx, pending)

	mailed := &models.VerificationRequest{
		UserID:            user.ID,
		VerificationCode:  "MAILED1",
		Status:            models.VerificationStatusMailed,
		PostgridRequestID: "pg2",
		BoundaryType:      "city",
		BoundaryName:      "City2",
		BoundaryState:     "State2",
	}
	_ = verifyRepo.CreateVerificationRequest(ctx, mailed)

	// Create a verified one (should not be returned)
	verified := &models.VerificationRequest{
		UserID:            user.ID,
		VerificationCode:  "VERIFIED1",
		Status:            models.VerificationStatusVerified,
		PostgridRequestID: "pg3",
		BoundaryType:      "city",
		BoundaryName:      "City3",
		BoundaryState:     "State3",
	}
	_ = verifyRepo.CreateVerificationRequest(ctx, verified)

	requests, err := verifyRepo.GetPendingByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to get pending requests: %v", err)
	}

	if len(requests) != 2 {
		t.Errorf("Expected 2 pending/mailed requests, got %d", len(requests))
	}

	// Cleanup
	_, _ = db.ExecContext(ctx, "DELETE FROM verification_requests WHERE user_id = ?", user.ID)
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
}

func TestVerificationRepository_CountRecentByUser(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	verifyRepo := NewVerificationRepository(db)
	ctx := context.Background()

	user := &models.User{
		Username:     "counttest",
		Email:        "counttest@example.com",
		PasswordHash: "hash",
	}
	_ = userRepo.Create(ctx, user)

	// Create some recent requests
	for i := 0; i < 3; i++ {
		req := &models.VerificationRequest{
			UserID:            user.ID,
			VerificationCode:  "COUNT" + string(rune('A'+i)),
			Status:            models.VerificationStatusPending,
			PostgridRequestID: "pg" + string(rune('A'+i)),
			BoundaryType:      "city",
			BoundaryName:      "City",
			BoundaryState:     "State",
		}
		_ = verifyRepo.CreateVerificationRequest(ctx, req)
	}

	count, err := verifyRepo.CountRecentByUser(ctx, user.ID, 7)
	if err != nil {
		t.Fatalf("Failed to count recent: %v", err)
	}
	if count != 3 {
		t.Errorf("Expected 3 recent requests, got %d", count)
	}

	// Cleanup
	_, _ = db.ExecContext(ctx, "DELETE FROM verification_requests WHERE user_id = ?", user.ID)
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
}

func TestRegionRepository_GetRegionsContainingPoint_Ordering(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	ctx := context.Background()

	// Create a test user
	user := &models.User{
		Username:     "containingpointuser",
		Email:        "containingpoint@example.com",
		PasswordHash: "hash",
	}
	_ = userRepo.Create(ctx, user)

	// Create nested regions all containing the same point (-122.45, 37.75)
	// Each region is slightly larger than the one inside it
	// The point (-122.45, 37.75) should be contained in all of them

	// State - largest region
	stateRegion := &models.GeographicRegion{
		Name:       "Test State",
		RegionType: models.RegionTypeState,
		CreatedBy:  strPtr(user.ID),
	}
	stateGeoJSON := `{"type":"Polygon","coordinates":[[[-123.0,37.0],[-122.0,37.0],[-122.0,38.0],[-123.0,38.0],[-123.0,37.0]]]}`
	_ = regionRepo.Create(ctx, stateRegion, stateGeoJSON)

	// County - smaller than state
	countyRegion := &models.GeographicRegion{
		Name:           "Test County",
		RegionType:     models.RegionTypeCounty,
		ParentRegionID: &stateRegion.ID,
		CreatedBy:      strPtr(user.ID),
	}
	countyGeoJSON := `{"type":"Polygon","coordinates":[[[-122.8,37.4],[-122.2,37.4],[-122.2,37.9],[-122.8,37.9],[-122.8,37.4]]]}`
	_ = regionRepo.Create(ctx, countyRegion, countyGeoJSON)

	// City - smaller than county
	cityRegion := &models.GeographicRegion{
		Name:           "Test City",
		RegionType:     models.RegionTypeCity,
		ParentRegionID: &countyRegion.ID,
		CreatedBy:      strPtr(user.ID),
	}
	cityGeoJSON := `{"type":"Polygon","coordinates":[[[-122.6,37.6],[-122.3,37.6],[-122.3,37.85],[-122.6,37.85],[-122.6,37.6]]]}`
	_ = regionRepo.Create(ctx, cityRegion, cityGeoJSON)

	// Neighborhood - smaller than city
	neighborhoodRegion := &models.GeographicRegion{
		Name:           "Test Neighborhood",
		RegionType:     models.RegionTypeNeighborhood,
		ParentRegionID: &cityRegion.ID,
		CreatedBy:      strPtr(user.ID),
	}
	neighborhoodGeoJSON := `{"type":"Polygon","coordinates":[[[-122.55,37.7],[-122.4,37.7],[-122.4,37.8],[-122.55,37.8],[-122.55,37.7]]]}`
	_ = regionRepo.Create(ctx, neighborhoodRegion, neighborhoodGeoJSON)

	// City Block - smallest region
	cityBlockRegion := &models.GeographicRegion{
		Name:           "Test City Block",
		RegionType:     models.RegionTypeCityBlock,
		ParentRegionID: &neighborhoodRegion.ID,
		CreatedBy:      strPtr(user.ID),
	}
	cityBlockGeoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.74],[-122.44,37.74],[-122.44,37.76],[-122.5,37.76],[-122.5,37.74]]]}`
	_ = regionRepo.Create(ctx, cityBlockRegion, cityBlockGeoJSON)

	t.Run("returns regions ordered from most specific to least specific", func(t *testing.T) {
		// Query for point that should be contained in all regions
		lat := 37.75
		lng := -122.45

		regions, err := regionRepo.GetRegionsContainingPoint(ctx, lat, lng)
		if err != nil {
			t.Fatalf("Failed to get regions containing point: %v", err)
		}

		// Filter to only our test regions (there may be other regions in the test DB)
		testRegionIDs := map[string]models.RegionType{
			cityBlockRegion.ID:    models.RegionTypeCityBlock,
			neighborhoodRegion.ID: models.RegionTypeNeighborhood,
			cityRegion.ID:         models.RegionTypeCity,
			countyRegion.ID:       models.RegionTypeCounty,
			stateRegion.ID:        models.RegionTypeState,
		}

		var testRegions []models.RegionSummary
		for _, r := range regions {
			if _, ok := testRegionIDs[r.ID]; ok {
				testRegions = append(testRegions, r)
			}
		}

		if len(testRegions) != 5 {
			t.Fatalf("Expected 5 test regions, got %d", len(testRegions))
		}

		// Expected order: city_block, neighborhood, city, county, state
		expectedOrder := []struct {
			id         string
			regionType models.RegionType
		}{
			{cityBlockRegion.ID, models.RegionTypeCityBlock},
			{neighborhoodRegion.ID, models.RegionTypeNeighborhood},
			{cityRegion.ID, models.RegionTypeCity},
			{countyRegion.ID, models.RegionTypeCounty},
			{stateRegion.ID, models.RegionTypeState},
		}

		for i, expected := range expectedOrder {
			if testRegions[i].ID != expected.id {
				t.Errorf("Position %d: expected %s (%s), got %s (%s)",
					i, expected.regionType, expected.id,
					testRegions[i].RegionType, testRegions[i].ID)
			}
		}
	})

	t.Run("first region is most specific (city_block)", func(t *testing.T) {
		lat := 37.75
		lng := -122.45

		regions, err := regionRepo.GetRegionsContainingPoint(ctx, lat, lng)
		if err != nil {
			t.Fatalf("Failed to get regions: %v", err)
		}

		if len(regions) == 0 {
			t.Fatal("Expected at least one region")
		}

		if regions[0].ID != cityBlockRegion.ID {
			t.Errorf("Expected first region to be city_block (ID: %s), got ID: %s (type: %s)",
				cityBlockRegion.ID, regions[0].ID, regions[0].RegionType)
		}
	})

	// Cleanup
	_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id IN (?, ?, ?, ?, ?)",
		cityBlockRegion.ID, neighborhoodRegion.ID, cityRegion.ID, countyRegion.ID, stateRegion.ID)
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
}

func TestHelperFunctions(t *testing.T) {
	t.Run("containsString", func(t *testing.T) {
		if !containsString("hello world", "world") {
			t.Error("Expected 'hello world' to contain 'world'")
		}
		if containsString("hello", "world") {
			t.Error("Expected 'hello' to not contain 'world'")
		}
		if !containsString("exact", "exact") {
			t.Error("Expected 'exact' to equal 'exact'")
		}
		if containsString("short", "longer string") {
			t.Error("Expected 'short' to not contain 'longer string'")
		}
	})

	t.Run("isDuplicateKeyError", func(t *testing.T) {
		dupErr := &testError{msg: "Error 1062: Duplicate entry 'test' for key 'username'"}
		if !isDuplicateKeyError(dupErr) {
			t.Error("Expected to detect duplicate key error")
		}

		otherErr := &testError{msg: "Some other error"}
		if isDuplicateKeyError(otherErr) {
			t.Error("Should not detect non-duplicate error as duplicate")
		}
	})
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

func init() {
	// Suppress unused variable warnings
	_ = time.Now()
}

// TestRegionRepository_GetByIDWithDetails_ExcludesAncestors tests that when a child region's
// polygon is larger than its parent (like NYC spanning multiple counties), the parent
// regions are NOT returned as sub-regions via ST_Contains.
func TestRegionRepository_GetByIDWithDetails_ExcludesAncestors(t *testing.T) {
	db := testDB(t)
	userRepo := NewUserRepository(db)
	regionRepo := NewRegionRepository(db)
	ctx := context.Background()

	// Create a test user
	user := &models.User{
		Username:     "ancestorexcludeuser",
		Email:        "ancestorexclude@example.com",
		PasswordHash: "hash",
	}
	_ = userRepo.Create(ctx, user)

	// Scenario: Create a hierarchy where a "city" polygon is LARGER than its parent "county"
	// This simulates the NYC case where the city spans 5 counties.
	//
	// Hierarchy by parent_id: State -> County -> City -> Locality
	// But by geometry: City polygon contains County polygon

	// State - largest region
	stateRegion := &models.GeographicRegion{
		Name:       "Test State Ancestor",
		RegionType: models.RegionTypeState,
		CreatedBy:  strPtr(user.ID),
	}
	stateGeoJSON := `{"type":"Polygon","coordinates":[[[-123.0,37.0],[-122.0,37.0],[-122.0,38.0],[-123.0,38.0],[-123.0,37.0]]]}`
	_ = regionRepo.Create(ctx, stateRegion, stateGeoJSON)

	// County - SMALLER than city (simulating one of NYC's counties)
	countyRegion := &models.GeographicRegion{
		Name:           "Test County Ancestor",
		RegionType:     models.RegionTypeCounty,
		ParentRegionID: &stateRegion.ID,
		CreatedBy:      strPtr(user.ID),
	}
	// Small county polygon: just a small square
	countyGeoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.6],[-122.4,37.6],[-122.4,37.7],[-122.5,37.7],[-122.5,37.6]]]}`
	_ = regionRepo.Create(ctx, countyRegion, countyGeoJSON)

	// City - LARGER than county (simulating NYC which spans multiple counties)
	// City has county as parent (by parent_id), but city's polygon CONTAINS county's polygon
	cityRegion := &models.GeographicRegion{
		Name:           "Test City Ancestor",
		RegionType:     models.RegionTypeCity,
		ParentRegionID: &countyRegion.ID, // Parent is county in hierarchy
		CreatedBy:      strPtr(user.ID),
	}
	// Large city polygon that CONTAINS the county polygon
	cityGeoJSON := `{"type":"Polygon","coordinates":[[[-122.7,37.5],[-122.3,37.5],[-122.3,37.8],[-122.7,37.8],[-122.7,37.5]]]}`
	_ = regionRepo.Create(ctx, cityRegion, cityGeoJSON)

	// Locality - child of city
	localityRegion := &models.GeographicRegion{
		Name:           "Test Locality Ancestor",
		RegionType:     models.RegionTypeLocality,
		ParentRegionID: &cityRegion.ID,
		CreatedBy:      strPtr(user.ID),
	}
	// Locality polygon - smaller, inside city
	localityGeoJSON := `{"type":"Polygon","coordinates":[[[-122.55,37.62],[-122.45,37.62],[-122.45,37.68],[-122.55,37.68],[-122.55,37.62]]]}`
	_ = regionRepo.Create(ctx, localityRegion, localityGeoJSON)

	t.Run("GetByIDWithDetails excludes parent county even when city polygon contains county polygon", func(t *testing.T) {
		// Get details for the city region
		// The city's polygon (large) contains the county's polygon (small)
		// But the county should NOT appear as a sub-region because it's an ancestor in the hierarchy
		details, err := regionRepo.GetByIDWithDetails(ctx, cityRegion.ID, "")
		if err != nil {
			t.Fatalf("Failed to get region details: %v", err)
		}

		// Check that locality IS in sub-regions (correct child)
		foundLocality := false
		for _, sub := range details.SubRegions {
			if sub.ID == localityRegion.ID {
				foundLocality = true
			}
			// County should NOT be in sub-regions (it's a parent, not a child)
			if sub.ID == countyRegion.ID {
				t.Errorf("County should NOT appear as sub-region of city (county is an ancestor)")
			}
			// State should NOT be in sub-regions either
			if sub.ID == stateRegion.ID {
				t.Errorf("State should NOT appear as sub-region of city (state is an ancestor)")
			}
		}

		if !foundLocality {
			t.Error("Expected locality to appear as sub-region of city")
		}
	})

	t.Run("GetByIDWithDetails for county shows city as sub-region via parent_id relationship", func(t *testing.T) {
		// Get details for the county region
		// Even though the city polygon contains the county polygon,
		// the city should appear as a sub-region because it has the county as its parent_region_id
		details, err := regionRepo.GetByIDWithDetails(ctx, countyRegion.ID, "")
		if err != nil {
			t.Fatalf("Failed to get region details: %v", err)
		}

		// City should be in sub-regions (it has this county as parent)
		foundCity := false
		for _, sub := range details.SubRegions {
			if sub.ID == cityRegion.ID {
				foundCity = true
			}
		}

		if !foundCity {
			t.Error("Expected city to appear as sub-region of county (city has county as parent)")
		}
	})

	// Cleanup
	_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id IN (?, ?, ?, ?)",
		localityRegion.ID, cityRegion.ID, countyRegion.ID, stateRegion.ID)
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
}
