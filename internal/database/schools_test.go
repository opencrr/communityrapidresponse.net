package database

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// =============================================================================
// Helper Functions
// =============================================================================

func createSchoolTestUser(t *testing.T, db *DB, suffix string) string {
	t.Helper()
	userID := uuid.New().String()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO users (id, username, email, password_hash, verification_tier, created_at) VALUES (?, ?, ?, ?, 2, NOW())",
		userID, "schooluser_"+suffix, suffix+"@schooltest.com", "$2a$12$testhashedpasswordforschool")
	if err != nil {
		t.Fatalf("Failed to create test user %s: %v", suffix, err)
	}
	return userID
}

func createSchoolTestDistrict(t *testing.T, db *DB, districtRepo *SchoolDistrictRepository, ncesID, name, state string) *models.SchoolDistrict {
	t.Helper()
	districtType := models.SchoolDistrictTypeUnified
	district := &models.SchoolDistrict{
		NCESID:       ncesID,
		Name:         name,
		State:        state,
		DistrictType: districtType,
	}
	if err := districtRepo.UpsertByNCESID(context.Background(), district); err != nil {
		t.Fatalf("Failed to create test district %s: %v", name, err)
	}
	return district
}

func createSchoolTestSchool(t *testing.T, db *DB, schoolRepo *SchoolRepository, ncesID, name, state string, districtID *string) *models.School {
	t.Helper()
	city := "Test City"
	school := &models.School{
		NCESID:     ncesID,
		DistrictID: districtID,
		Name:       name,
		City:       &city,
		State:      state,
	}
	if err := schoolRepo.UpsertByNCESID(context.Background(), school); err != nil {
		t.Fatalf("Failed to create test school %s: %v", name, err)
	}
	return school
}

func addVerifiedAdminToSchool(t *testing.T, db *DB, userID, schoolID string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO user_schools (id, user_id, school_id, is_admin, verification_status, verified_at) VALUES (?, ?, ?, TRUE, 'verified', NOW())",
		uuid.New().String(), userID, schoolID)
	if err != nil {
		t.Fatalf("Failed to add verified admin to school: %v", err)
	}
}

func addPendingMemberToSchool(t *testing.T, db *DB, userID, schoolID string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO user_schools (id, user_id, school_id, is_admin, verification_status) VALUES (?, ?, ?, FALSE, 'pending')",
		uuid.New().String(), userID, schoolID)
	if err != nil {
		t.Fatalf("Failed to add pending member to school: %v", err)
	}
}

func cleanupSchoolTest(t *testing.T, db *DB, userIDs []string, schoolIDs []string, districtIDs []string) {
	t.Helper()
	ctx := context.Background()

	for _, schoolID := range schoolIDs {
		_, _ = db.ExecContext(ctx, "DELETE FROM school_vouches WHERE school_id = ?", schoolID)
		_, _ = db.ExecContext(ctx, "DELETE FROM school_blocked_users WHERE school_id = ?", schoolID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE school_id = ?", schoolID)
		_, _ = db.ExecContext(ctx, "DELETE FROM user_schools WHERE school_id = ?", schoolID)
	}

	for _, schoolID := range schoolIDs {
		_, _ = db.ExecContext(ctx, "DELETE FROM schools WHERE id = ?", schoolID)
	}

	for _, districtID := range districtIDs {
		_, _ = db.ExecContext(ctx, "DELETE FROM school_districts WHERE id = ?", districtID)
	}

	for _, userID := range userIDs {
		_, _ = db.ExecContext(ctx, "DELETE FROM school_vouches WHERE voucher_user_id = ? OR vouched_user_id = ?", userID, userID)
		_, _ = db.ExecContext(ctx, "DELETE FROM school_blocked_users WHERE user_id = ? OR blocked_by = ?", userID, userID)
		_, _ = db.ExecContext(ctx, "DELETE FROM user_schools WHERE user_id = ?", userID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", userID)
	}
}

// =============================================================================
// SchoolDistrictRepository Tests
// =============================================================================

func TestDistrictUpsertByNCESID(t *testing.T) {
	db := testDB(t)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	t.Run("creates new district", func(t *testing.T) {
		ncesID := uuid.New().String()[:7]
		districtType := models.SchoolDistrictTypeUnified
		district := &models.SchoolDistrict{
			NCESID:       ncesID,
			Name:         "New Test District",
			State:        "CA",
			DistrictType: districtType,
		}

		err := districtRepo.UpsertByNCESID(ctx, district)
		if err != nil {
			t.Fatalf("Failed to upsert district: %v", err)
		}

		if district.ID == "" {
			t.Error("Expected district ID to be set")
		}
		if district.CreatedAt.IsZero() {
			t.Error("Expected CreatedAt to be set")
		}

		// Verify it was persisted
		retrieved, err := districtRepo.GetByNCESID(ctx, ncesID)
		if err != nil {
			t.Fatalf("Failed to get district by NCES ID: %v", err)
		}
		if retrieved.Name != "New Test District" {
			t.Errorf("Expected name 'New Test District', got '%s'", retrieved.Name)
		}
		if retrieved.State != "CA" {
			t.Errorf("Expected state 'CA', got '%s'", retrieved.State)
		}

		// Cleanup
		_, _ = db.ExecContext(ctx, "DELETE FROM school_districts WHERE id = ?", district.ID)
	})

	t.Run("upsert existing district updates name", func(t *testing.T) {
		ncesID := uuid.New().String()[:7]
		districtType := models.SchoolDistrictTypeElementary
		district := &models.SchoolDistrict{
			NCESID:       ncesID,
			Name:         "Original District Name",
			State:        "NY",
			DistrictType: districtType,
		}

		err := districtRepo.UpsertByNCESID(ctx, district)
		if err != nil {
			t.Fatalf("Failed to create district: %v", err)
		}
		originalID := district.ID

		// Upsert with same NCES ID but updated name
		updatedDistrict := &models.SchoolDistrict{
			NCESID:       ncesID,
			Name:         "Updated District Name",
			State:        "NY",
			DistrictType: models.SchoolDistrictTypeSecondary,
		}

		err = districtRepo.UpsertByNCESID(ctx, updatedDistrict)
		if err != nil {
			t.Fatalf("Failed to upsert district: %v", err)
		}

		// Verify the name was updated
		retrieved, err := districtRepo.GetByNCESID(ctx, ncesID)
		if err != nil {
			t.Fatalf("Failed to get district: %v", err)
		}
		if retrieved.Name != "Updated District Name" {
			t.Errorf("Expected updated name 'Updated District Name', got '%s'", retrieved.Name)
		}
		if retrieved.DistrictType != models.SchoolDistrictTypeSecondary {
			t.Errorf("Expected district type 'secondary', got '%s'", retrieved.DistrictType)
		}

		// Cleanup
		_, _ = db.ExecContext(ctx, "DELETE FROM school_districts WHERE id = ?", originalID)
	})
}

func TestDistrictGetByID(t *testing.T) {
	db := testDB(t)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	t.Run("gets existing district", func(t *testing.T) {
		ncesID := uuid.New().String()[:7]
		district := &models.SchoolDistrict{
			NCESID:       ncesID,
			Name:         "GetByID Test District",
			State:        "TX",
			DistrictType: models.SchoolDistrictTypeUnified,
		}
		_ = districtRepo.UpsertByNCESID(ctx, district)
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM school_districts WHERE id = ?", district.ID)
		}()

		retrieved, err := districtRepo.GetByID(ctx, district.ID)
		if err != nil {
			t.Fatalf("Failed to get district by ID: %v", err)
		}
		if retrieved.Name != "GetByID Test District" {
			t.Errorf("Expected name 'GetByID Test District', got '%s'", retrieved.Name)
		}
		if retrieved.State != "TX" {
			t.Errorf("Expected state 'TX', got '%s'", retrieved.State)
		}
		if retrieved.NCESID != ncesID {
			t.Errorf("Expected NCES ID '%s', got '%s'", ncesID, retrieved.NCESID)
		}
	})

	t.Run("returns ErrDistrictNotFound for non-existent ID", func(t *testing.T) {
		_, err := districtRepo.GetByID(ctx, "non-existent-district-id")
		if err != ErrDistrictNotFound {
			t.Errorf("Expected ErrDistrictNotFound, got %v", err)
		}
	})
}

func TestDistrictGetByNCESID(t *testing.T) {
	db := testDB(t)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	t.Run("gets district by NCES ID", func(t *testing.T) {
		ncesID := uuid.New().String()[:7]
		district := &models.SchoolDistrict{
			NCESID:       ncesID,
			Name:         "NCES ID Test District",
			State:        "FL",
			DistrictType: models.SchoolDistrictTypeUnified,
		}
		_ = districtRepo.UpsertByNCESID(ctx, district)
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM school_districts WHERE id = ?", district.ID)
		}()

		retrieved, err := districtRepo.GetByNCESID(ctx, ncesID)
		if err != nil {
			t.Fatalf("Failed to get district by NCES ID: %v", err)
		}
		if retrieved.ID != district.ID {
			t.Errorf("Expected ID '%s', got '%s'", district.ID, retrieved.ID)
		}
		if retrieved.Name != "NCES ID Test District" {
			t.Errorf("Expected name 'NCES ID Test District', got '%s'", retrieved.Name)
		}
	})

	t.Run("returns ErrDistrictNotFound for non-existent NCES ID", func(t *testing.T) {
		_, err := districtRepo.GetByNCESID(ctx, "non-existent-nces-id")
		if err != ErrDistrictNotFound {
			t.Errorf("Expected ErrDistrictNotFound, got %v", err)
		}
	})
}

func TestDistrictSearch(t *testing.T) {
	db := testDB(t)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	// Create test districts
	uniqueSuffix := uuid.New().String()[:8]
	districtCA := &models.SchoolDistrict{
		NCESID:       "A" + uniqueSuffix[:6],
		Name:         "SearchTest Unified " + uniqueSuffix,
		State:        "CA",
		DistrictType: models.SchoolDistrictTypeUnified,
	}
	if err := districtRepo.UpsertByNCESID(ctx, districtCA); err != nil {
		t.Fatalf("Failed to create CA district: %v", err)
	}

	districtNY := &models.SchoolDistrict{
		NCESID:       "B" + uniqueSuffix[:6],
		Name:         "SearchTest Elementary " + uniqueSuffix,
		State:        "NY",
		DistrictType: models.SchoolDistrictTypeElementary,
	}
	if err := districtRepo.UpsertByNCESID(ctx, districtNY); err != nil {
		t.Fatalf("Failed to create NY district: %v", err)
	}

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM school_districts WHERE id IN (?, ?)", districtCA.ID, districtNY.ID)
	}()

	t.Run("searches by name query", func(t *testing.T) {
		results, err := districtRepo.Search(ctx, uniqueSuffix, "")
		if err != nil {
			t.Fatalf("Failed to search districts: %v", err)
		}
		if len(results) < 2 {
			t.Errorf("Expected at least 2 results, got %d", len(results))
		}
	})

	t.Run("searches by state filter", func(t *testing.T) {
		results, err := districtRepo.Search(ctx, "SearchTest", "CA")
		if err != nil {
			t.Fatalf("Failed to search districts: %v", err)
		}

		foundCA := false
		foundNY := false
		for _, district := range results {
			if district.ID == districtCA.ID {
				foundCA = true
			}
			if district.ID == districtNY.ID {
				foundNY = true
			}
		}

		if !foundCA {
			t.Error("Expected to find CA district in results")
		}
		if foundNY {
			t.Error("Did not expect to find NY district when filtering by CA")
		}
	})
}

// =============================================================================
// SchoolRepository Tests
// =============================================================================

func TestSchoolUpsertByNCESID(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	// Create a district for the school
	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "Upsert Test District", "CA")
	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM school_districts WHERE id = ?", district.ID)
	}()

	t.Run("creates new school", func(t *testing.T) {
		ncesID := "SCH_" + uuid.New().String()[:8]
		city := "San Francisco"
		school := &models.School{
			NCESID:     ncesID,
			DistrictID: &district.ID,
			Name:       "Upsert New School",
			City:       &city,
			State:      "CA",
		}

		err := schoolRepo.UpsertByNCESID(ctx, school)
		if err != nil {
			t.Fatalf("Failed to upsert school: %v", err)
		}

		if school.ID == "" {
			t.Error("Expected school ID to be set")
		}
		if school.CreatedAt.IsZero() {
			t.Error("Expected CreatedAt to be set")
		}

		// Verify persistence
		retrieved, err := schoolRepo.GetByNCESID(ctx, ncesID)
		if err != nil {
			t.Fatalf("Failed to get school: %v", err)
		}
		if retrieved.Name != "Upsert New School" {
			t.Errorf("Expected name 'Upsert New School', got '%s'", retrieved.Name)
		}

		// Cleanup
		_, _ = db.ExecContext(ctx, "DELETE FROM schools WHERE id = ?", school.ID)
	})

	t.Run("upsert existing school updates fields", func(t *testing.T) {
		ncesID := "SCH_" + uuid.New().String()[:8]
		city := "Los Angeles"
		school := &models.School{
			NCESID:     ncesID,
			DistrictID: &district.ID,
			Name:       "Original School Name",
			City:       &city,
			State:      "CA",
		}
		err := schoolRepo.UpsertByNCESID(ctx, school)
		if err != nil {
			t.Fatalf("Failed to create school: %v", err)
		}
		originalID := school.ID

		// Upsert with same NCES ID but updated name
		updatedCity := "Sacramento"
		updatedSchool := &models.School{
			NCESID:     ncesID,
			DistrictID: &district.ID,
			Name:       "Updated School Name",
			City:       &updatedCity,
			State:      "CA",
		}
		err = schoolRepo.UpsertByNCESID(ctx, updatedSchool)
		if err != nil {
			t.Fatalf("Failed to upsert school: %v", err)
		}

		// Verify update
		retrieved, err := schoolRepo.GetByNCESID(ctx, ncesID)
		if err != nil {
			t.Fatalf("Failed to get school: %v", err)
		}
		if retrieved.Name != "Updated School Name" {
			t.Errorf("Expected name 'Updated School Name', got '%s'", retrieved.Name)
		}
		if retrieved.City == nil || *retrieved.City != "Sacramento" {
			t.Errorf("Expected city 'Sacramento', got '%v'", retrieved.City)
		}

		// Cleanup
		_, _ = db.ExecContext(ctx, "DELETE FROM schools WHERE id = ?", originalID)
	})
}

func TestSchoolGetByID(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "GetByID District", "CA")
	school := createSchoolTestSchool(t, db, schoolRepo, "SCH_"+uniqueSuffix, "GetByID School", "CA", &district.ID)
	defer cleanupSchoolTest(t, db, nil, []string{school.ID}, []string{district.ID})

	t.Run("gets existing school", func(t *testing.T) {
		retrieved, err := schoolRepo.GetByID(ctx, school.ID)
		if err != nil {
			t.Fatalf("Failed to get school by ID: %v", err)
		}
		if retrieved.Name != "GetByID School" {
			t.Errorf("Expected name 'GetByID School', got '%s'", retrieved.Name)
		}
		if retrieved.State != "CA" {
			t.Errorf("Expected state 'CA', got '%s'", retrieved.State)
		}
	})

	t.Run("returns ErrSchoolNotFound for non-existent ID", func(t *testing.T) {
		_, err := schoolRepo.GetByID(ctx, "non-existent-school-id")
		if err != ErrSchoolNotFound {
			t.Errorf("Expected ErrSchoolNotFound, got %v", err)
		}
	})
}

func TestSchoolGetByIDWithDetails(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "Details District", "CA")
	school := createSchoolTestSchool(t, db, schoolRepo, "SCH_"+uniqueSuffix, "Details School", "CA", &district.ID)

	// Create users and add them to the school
	userID1 := createSchoolTestUser(t, db, "detail1_"+uniqueSuffix)
	userID2 := createSchoolTestUser(t, db, "detail2_"+uniqueSuffix)
	addVerifiedAdminToSchool(t, db, userID1, school.ID)
	addPendingMemberToSchool(t, db, userID2, school.ID)

	defer cleanupSchoolTest(t, db, []string{userID1, userID2}, []string{school.ID}, []string{district.ID})

	t.Run("returns school with counts and district name", func(t *testing.T) {
		details, err := schoolRepo.GetByIDWithDetails(ctx, school.ID)
		if err != nil {
			t.Fatalf("Failed to get school with details: %v", err)
		}
		if details.Name != "Details School" {
			t.Errorf("Expected name 'Details School', got '%s'", details.Name)
		}
		if details.DistrictName != "Details District" {
			t.Errorf("Expected district name 'Details District', got '%s'", details.DistrictName)
		}
		if details.MemberCount != 2 {
			t.Errorf("Expected member count 2, got %d", details.MemberCount)
		}
		if details.AdminCount != 1 {
			t.Errorf("Expected admin count 1, got %d", details.AdminCount)
		}
		if details.VerifiedCount != 1 {
			t.Errorf("Expected verified count 1, got %d", details.VerifiedCount)
		}
		// With only 1 verified admin, should be in bootstrap mode
		if !details.BootstrapMode {
			t.Error("Expected bootstrap mode to be true with 1 admin")
		}
	})

	t.Run("returns ErrSchoolNotFound for non-existent ID", func(t *testing.T) {
		_, err := schoolRepo.GetByIDWithDetails(ctx, "non-existent-school-id")
		if err != ErrSchoolNotFound {
			t.Errorf("Expected ErrSchoolNotFound, got %v", err)
		}
	})
}

func TestSchoolSearch(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	districtCA := createSchoolTestDistrict(t, db, districtRepo, "A"+uniqueSuffix[:6], "Search District CA", "CA")
	districtNY := createSchoolTestDistrict(t, db, districtRepo, "B"+uniqueSuffix[:6], "Search District NY", "NY")

	school1 := createSchoolTestSchool(t, db, schoolRepo, "SCH1"+uniqueSuffix, "SearchSchool Alpha "+uniqueSuffix, "CA", &districtCA.ID)
	school2 := createSchoolTestSchool(t, db, schoolRepo, "SCH2"+uniqueSuffix, "SearchSchool Beta "+uniqueSuffix, "CA", &districtCA.ID)
	school3 := createSchoolTestSchool(t, db, schoolRepo, "SCH3"+uniqueSuffix, "SearchSchool Gamma "+uniqueSuffix, "NY", &districtNY.ID)

	defer cleanupSchoolTest(t, db, nil, []string{school1.ID, school2.ID, school3.ID}, []string{districtCA.ID, districtNY.ID})

	t.Run("searches by name", func(t *testing.T) {
		results, totalCount, err := schoolRepo.Search(ctx, uniqueSuffix, "", "", 1, 20)
		if err != nil {
			t.Fatalf("Failed to search schools: %v", err)
		}
		if totalCount < 3 {
			t.Errorf("Expected at least 3 results, got %d", totalCount)
		}
		if len(results) < 3 {
			t.Errorf("Expected at least 3 result items, got %d", len(results))
		}
	})

	t.Run("searches by state filter", func(t *testing.T) {
		results, totalCount, err := schoolRepo.Search(ctx, uniqueSuffix, "CA", "", 1, 20)
		if err != nil {
			t.Fatalf("Failed to search schools: %v", err)
		}
		if totalCount < 2 {
			t.Errorf("Expected at least 2 CA results, got %d", totalCount)
		}
		for _, result := range results {
			if result.State != "CA" {
				t.Errorf("Expected all results to be in CA, got '%s'", result.State)
			}
		}
		// Verify NY school is not in results
		for _, result := range results {
			if result.ID == school3.ID {
				t.Error("Did not expect NY school in CA-filtered results")
			}
		}
	})

	t.Run("pagination works correctly", func(t *testing.T) {
		// Get page 1 with limit 2
		resultsPage1, totalCount, err := schoolRepo.Search(ctx, uniqueSuffix, "", "", 1, 2)
		if err != nil {
			t.Fatalf("Failed to search schools page 1: %v", err)
		}
		if totalCount < 3 {
			t.Errorf("Expected at least 3 total results, got %d", totalCount)
		}
		if len(resultsPage1) != 2 {
			t.Errorf("Expected 2 results on page 1, got %d", len(resultsPage1))
		}

		// Get page 2 with limit 2
		resultsPage2, _, err := schoolRepo.Search(ctx, uniqueSuffix, "", "", 2, 2)
		if err != nil {
			t.Fatalf("Failed to search schools page 2: %v", err)
		}
		if len(resultsPage2) < 1 {
			t.Errorf("Expected at least 1 result on page 2, got %d", len(resultsPage2))
		}

		// Verify no overlap between pages
		page1IDs := make(map[string]bool)
		for _, result := range resultsPage1 {
			page1IDs[result.ID] = true
		}
		for _, result := range resultsPage2 {
			if page1IDs[result.ID] {
				t.Errorf("Found duplicate school '%s' across pages", result.ID)
			}
		}
	})

	t.Run("searches by district ID filter", func(t *testing.T) {
		results, totalCount, err := schoolRepo.Search(ctx, uniqueSuffix, "", districtCA.ID, 1, 20)
		if err != nil {
			t.Fatalf("Failed to search schools by district: %v", err)
		}
		if totalCount < 2 {
			t.Errorf("Expected at least 2 results in CA district, got %d", totalCount)
		}
		for _, result := range results {
			if result.ID == school3.ID {
				t.Error("Did not expect NY school in CA district-filtered results")
			}
		}
	})
}

func TestSchoolMembership(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "Membership District", "CA")
	school := createSchoolTestSchool(t, db, schoolRepo, "SCH_"+uniqueSuffix, "Membership School", "CA", &district.ID)
	userID := createSchoolTestUser(t, db, "mem_"+uniqueSuffix)

	defer cleanupSchoolTest(t, db, []string{userID}, []string{school.ID}, []string{district.ID})

	t.Run("AddUserToSchool creates pending membership", func(t *testing.T) {
		membershipID, err := schoolRepo.AddUserToSchool(ctx, userID, school.ID)
		if err != nil {
			t.Fatalf("Failed to add user to school: %v", err)
		}
		if membershipID == "" {
			t.Error("Expected membership ID to be set")
		}

		// Verify the membership was created with pending status
		userSchool, err := schoolRepo.GetUserSchool(ctx, userID, school.ID)
		if err != nil {
			t.Fatalf("Failed to get user school: %v", err)
		}
		if userSchool == nil {
			t.Fatal("Expected user school to exist")
		}
		if userSchool.VerificationStatus != models.SchoolVerificationStatusPending {
			t.Errorf("Expected verification status 'pending', got '%s'", userSchool.VerificationStatus)
		}
		if userSchool.IsAdmin {
			t.Error("Expected is_admin to be false for new membership")
		}
	})

	t.Run("GetUserSchool returns membership", func(t *testing.T) {
		userSchool, err := schoolRepo.GetUserSchool(ctx, userID, school.ID)
		if err != nil {
			t.Fatalf("Failed to get user school: %v", err)
		}
		if userSchool == nil {
			t.Fatal("Expected user school to exist")
		}
		if userSchool.UserID != userID {
			t.Errorf("Expected user ID '%s', got '%s'", userID, userSchool.UserID)
		}
		if userSchool.SchoolID != school.ID {
			t.Errorf("Expected school ID '%s', got '%s'", school.ID, userSchool.SchoolID)
		}
	})

	t.Run("GetUserSchool returns nil for non-existent membership", func(t *testing.T) {
		userSchool, err := schoolRepo.GetUserSchool(ctx, "non-existent-user", school.ID)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if userSchool != nil {
			t.Error("Expected nil for non-existent membership")
		}
	})

	t.Run("IsUserInSchool returns true for member", func(t *testing.T) {
		isMember, err := schoolRepo.IsUserInSchool(ctx, userID, school.ID)
		if err != nil {
			t.Fatalf("Failed to check membership: %v", err)
		}
		if !isMember {
			t.Error("Expected IsUserInSchool to return true")
		}
	})

	t.Run("IsUserInSchool returns false for non-member", func(t *testing.T) {
		isMember, err := schoolRepo.IsUserInSchool(ctx, "non-existent-user", school.ID)
		if err != nil {
			t.Fatalf("Failed to check membership: %v", err)
		}
		if isMember {
			t.Error("Expected IsUserInSchool to return false")
		}
	})

	t.Run("RemoveUserFromSchool deletes membership", func(t *testing.T) {
		err := schoolRepo.RemoveUserFromSchool(ctx, userID, school.ID)
		if err != nil {
			t.Fatalf("Failed to remove user from school: %v", err)
		}

		isMember, err := schoolRepo.IsUserInSchool(ctx, userID, school.ID)
		if err != nil {
			t.Fatalf("Failed to check membership after removal: %v", err)
		}
		if isMember {
			t.Error("Expected IsUserInSchool to return false after removal")
		}

		userSchool, err := schoolRepo.GetUserSchool(ctx, userID, school.ID)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if userSchool != nil {
			t.Error("Expected GetUserSchool to return nil after removal")
		}
	})
}

func TestSchoolBootstrapMode(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "Bootstrap District", "CA")
	school := createSchoolTestSchool(t, db, schoolRepo, "SCH_"+uniqueSuffix, "Bootstrap School", "CA", &district.ID)

	adminIDs := make([]string, 4)
	for i := 0; i < 4; i++ {
		adminIDs[i] = createSchoolTestUser(t, db, "bootadmin"+string(rune('a'+i))+"_"+uniqueSuffix)
	}

	defer cleanupSchoolTest(t, db, adminIDs, []string{school.ID}, []string{district.ID})

	t.Run("school with fewer than 3 verified admins is in bootstrap mode", func(t *testing.T) {
		// Add 2 verified admins
		addVerifiedAdminToSchool(t, db, adminIDs[0], school.ID)
		addVerifiedAdminToSchool(t, db, adminIDs[1], school.ID)

		isBootstrap, adminCount, err := schoolRepo.IsSchoolInBootstrapMode(ctx, school.ID)
		if err != nil {
			t.Fatalf("Failed to check bootstrap mode: %v", err)
		}
		if !isBootstrap {
			t.Error("Expected bootstrap mode to be true with 2 admins")
		}
		if adminCount != 2 {
			t.Errorf("Expected admin count 2, got %d", adminCount)
		}
	})

	t.Run("school with 3 or more verified admins is not in bootstrap mode", func(t *testing.T) {
		// Add a third verified admin
		addVerifiedAdminToSchool(t, db, adminIDs[2], school.ID)

		isBootstrap, adminCount, err := schoolRepo.IsSchoolInBootstrapMode(ctx, school.ID)
		if err != nil {
			t.Fatalf("Failed to check bootstrap mode: %v", err)
		}
		if isBootstrap {
			t.Error("Expected bootstrap mode to be false with 3 admins")
		}
		if adminCount != 3 {
			t.Errorf("Expected admin count 3, got %d", adminCount)
		}
	})

	t.Run("GetVerifiedAdminCount returns correct count", func(t *testing.T) {
		// Add a fourth verified admin
		addVerifiedAdminToSchool(t, db, adminIDs[3], school.ID)

		count, err := schoolRepo.GetVerifiedAdminCount(ctx, school.ID)
		if err != nil {
			t.Fatalf("Failed to get verified admin count: %v", err)
		}
		if count != 4 {
			t.Errorf("Expected verified admin count 4, got %d", count)
		}
	})

	t.Run("pending members do not count as verified admins", func(t *testing.T) {
		pendingUserID := createSchoolTestUser(t, db, "bootpending_"+uniqueSuffix)
		addPendingMemberToSchool(t, db, pendingUserID, school.ID)
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM user_schools WHERE user_id = ?", pendingUserID)
			_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", pendingUserID)
		}()

		count, err := schoolRepo.GetVerifiedAdminCount(ctx, school.ID)
		if err != nil {
			t.Fatalf("Failed to get verified admin count: %v", err)
		}
		// Should still be 4 (the pending member is not counted)
		if count != 4 {
			t.Errorf("Expected verified admin count 4 (pending not counted), got %d", count)
		}
	})
}

func TestSchoolBlocklist(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "Blocklist District", "CA")
	school := createSchoolTestSchool(t, db, schoolRepo, "SCH_"+uniqueSuffix, "Blocklist School", "CA", &district.ID)
	blockedUserID := createSchoolTestUser(t, db, "blocked_"+uniqueSuffix)
	adminUserID := createSchoolTestUser(t, db, "blockadmin_"+uniqueSuffix)

	defer cleanupSchoolTest(t, db, []string{blockedUserID, adminUserID}, []string{school.ID}, []string{district.ID})

	t.Run("IsUserBlockedInSchool returns false for non-blocked user", func(t *testing.T) {
		isBlocked, err := schoolRepo.IsUserBlockedInSchool(ctx, blockedUserID, school.ID)
		if err != nil {
			t.Fatalf("Failed to check block status: %v", err)
		}
		if isBlocked {
			t.Error("Expected IsUserBlockedInSchool to return false for non-blocked user")
		}
	})

	t.Run("after blocking, IsUserBlockedInSchool returns true", func(t *testing.T) {
		reason := "Test block reason"
		err := db.Transaction(ctx, func(tx *sql.Tx) error {
			return schoolRepo.BlockUserInSchoolTx(ctx, tx, blockedUserID, school.ID, adminUserID, reason)
		})
		if err != nil {
			t.Fatalf("Failed to block user: %v", err)
		}

		isBlocked, err := schoolRepo.IsUserBlockedInSchool(ctx, blockedUserID, school.ID)
		if err != nil {
			t.Fatalf("Failed to check block status: %v", err)
		}
		if !isBlocked {
			t.Error("Expected IsUserBlockedInSchool to return true after blocking")
		}
	})

	t.Run("ListBlockedUsers returns blocked user", func(t *testing.T) {
		blockedUsers, err := schoolRepo.ListBlockedUsers(ctx, school.ID)
		if err != nil {
			t.Fatalf("Failed to list blocked users: %v", err)
		}
		if len(blockedUsers) != 1 {
			t.Fatalf("Expected 1 blocked user, got %d", len(blockedUsers))
		}
		if blockedUsers[0].UserID != blockedUserID {
			t.Errorf("Expected blocked user ID '%s', got '%s'", blockedUserID, blockedUsers[0].UserID)
		}
		if blockedUsers[0].BlockedBy != adminUserID {
			t.Errorf("Expected blocked_by '%s', got '%s'", adminUserID, blockedUsers[0].BlockedBy)
		}
	})

	t.Run("UnblockUserInSchool removes block", func(t *testing.T) {
		err := schoolRepo.UnblockUserInSchool(ctx, blockedUserID, school.ID)
		if err != nil {
			t.Fatalf("Failed to unblock user: %v", err)
		}

		isBlocked, err := schoolRepo.IsUserBlockedInSchool(ctx, blockedUserID, school.ID)
		if err != nil {
			t.Fatalf("Failed to check block status after unblock: %v", err)
		}
		if isBlocked {
			t.Error("Expected IsUserBlockedInSchool to return false after unblocking")
		}
	})
}

func TestSchoolListByDistrict(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "ListByDistrict District", "CA")
	school1 := createSchoolTestSchool(t, db, schoolRepo, "SCH1"+uniqueSuffix, "ListDistrict Alpha "+uniqueSuffix, "CA", &district.ID)
	school2 := createSchoolTestSchool(t, db, schoolRepo, "SCH2"+uniqueSuffix, "ListDistrict Beta "+uniqueSuffix, "CA", &district.ID)

	defer cleanupSchoolTest(t, db, nil, []string{school1.ID, school2.ID}, []string{district.ID})

	t.Run("lists all schools in a district", func(t *testing.T) {
		results, err := schoolRepo.ListByDistrict(ctx, district.ID)
		if err != nil {
			t.Fatalf("Failed to list by district: %v", err)
		}
		if len(results) < 2 {
			t.Errorf("Expected at least 2 schools in district, got %d", len(results))
		}

		foundSchool1 := false
		foundSchool2 := false
		for _, result := range results {
			if result.ID == school1.ID {
				foundSchool1 = true
			}
			if result.ID == school2.ID {
				foundSchool2 = true
			}
		}
		if !foundSchool1 {
			t.Error("Expected to find school1 in results")
		}
		if !foundSchool2 {
			t.Error("Expected to find school2 in results")
		}
	})
}

func TestSchoolListUserSchools(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "ListUserSchools District", "CA")
	school1 := createSchoolTestSchool(t, db, schoolRepo, "SCH1"+uniqueSuffix, "UserSchool Alpha "+uniqueSuffix, "CA", &district.ID)
	school2 := createSchoolTestSchool(t, db, schoolRepo, "SCH2"+uniqueSuffix, "UserSchool Beta "+uniqueSuffix, "CA", &district.ID)
	userID := createSchoolTestUser(t, db, "listuser_"+uniqueSuffix)

	// Add user to both schools
	addVerifiedAdminToSchool(t, db, userID, school1.ID)
	addPendingMemberToSchool(t, db, userID, school2.ID)

	defer cleanupSchoolTest(t, db, []string{userID}, []string{school1.ID, school2.ID}, []string{district.ID})

	t.Run("lists all schools for a user", func(t *testing.T) {
		results, err := schoolRepo.ListUserSchools(ctx, userID)
		if err != nil {
			t.Fatalf("Failed to list user schools: %v", err)
		}
		if len(results) != 2 {
			t.Errorf("Expected 2 schools, got %d", len(results))
		}
	})

	t.Run("returns empty list for user with no schools", func(t *testing.T) {
		results, err := schoolRepo.ListUserSchools(ctx, "non-existent-user")
		if err != nil {
			t.Fatalf("Failed to list user schools: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Expected 0 schools, got %d", len(results))
		}
	})
}

func TestSchoolGetSchoolMembers(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "Members District", "CA")
	school := createSchoolTestSchool(t, db, schoolRepo, "SCH_"+uniqueSuffix, "Members School", "CA", &district.ID)
	userID1 := createSchoolTestUser(t, db, "member1_"+uniqueSuffix)
	userID2 := createSchoolTestUser(t, db, "member2_"+uniqueSuffix)

	addVerifiedAdminToSchool(t, db, userID1, school.ID)
	addPendingMemberToSchool(t, db, userID2, school.ID)

	defer cleanupSchoolTest(t, db, []string{userID1, userID2}, []string{school.ID}, []string{district.ID})

	t.Run("retrieves all members of a school", func(t *testing.T) {
		members, err := schoolRepo.GetSchoolMembers(ctx, school.ID)
		if err != nil {
			t.Fatalf("Failed to get school members: %v", err)
		}
		if len(members) != 2 {
			t.Fatalf("Expected 2 members, got %d", len(members))
		}

		foundAdmin := false
		foundPending := false
		for _, member := range members {
			if member.UserID == userID1 && member.IsAdmin && member.VerificationStatus == models.SchoolVerificationStatusVerified {
				foundAdmin = true
			}
			if member.UserID == userID2 && !member.IsAdmin && member.VerificationStatus == models.SchoolVerificationStatusPending {
				foundPending = true
			}
		}
		if !foundAdmin {
			t.Error("Expected to find verified admin member")
		}
		if !foundPending {
			t.Error("Expected to find pending member")
		}
	})

	t.Run("returns empty list for school with no members", func(t *testing.T) {
		emptySchool := createSchoolTestSchool(t, db, schoolRepo, "SCHE"+uniqueSuffix, "Empty School", "CA", &district.ID)
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM schools WHERE id = ?", emptySchool.ID)
		}()

		members, err := schoolRepo.GetSchoolMembers(ctx, emptySchool.ID)
		if err != nil {
			t.Fatalf("Failed to get school members: %v", err)
		}
		if len(members) != 0 {
			t.Errorf("Expected 0 members, got %d", len(members))
		}
	})
}

// =============================================================================
// SchoolVouchRepository Tests
// =============================================================================

func TestSchoolVouchCreate(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	vouchRepo := NewSchoolVouchRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "Vouch District", "CA")
	school := createSchoolTestSchool(t, db, schoolRepo, "SCH_"+uniqueSuffix, "Vouch School", "CA", &district.ID)
	voucherID := createSchoolTestUser(t, db, "voucher_"+uniqueSuffix)
	vouchedID := createSchoolTestUser(t, db, "vouched_"+uniqueSuffix)

	// Add both users to the school
	addVerifiedAdminToSchool(t, db, voucherID, school.ID)
	addPendingMemberToSchool(t, db, vouchedID, school.ID)

	defer cleanupSchoolTest(t, db, []string{voucherID, vouchedID}, []string{school.ID}, []string{district.ID})

	t.Run("create vouch succeeds", func(t *testing.T) {
		vouch := &models.SchoolVouch{
			VoucherUserID: voucherID,
			VouchedUserID: vouchedID,
			SchoolID:      school.ID,
		}

		err := vouchRepo.Create(ctx, vouch)
		if err != nil {
			t.Fatalf("Failed to create vouch: %v", err)
		}
		if vouch.ID == "" {
			t.Error("Expected vouch ID to be set")
		}
		if vouch.CreatedAt.IsZero() {
			t.Error("Expected CreatedAt to be set")
		}
	})

	t.Run("HasVouched returns true after vouch", func(t *testing.T) {
		hasVouched, err := vouchRepo.HasVouched(ctx, voucherID, vouchedID, school.ID)
		if err != nil {
			t.Fatalf("Failed to check HasVouched: %v", err)
		}
		if !hasVouched {
			t.Error("Expected HasVouched to return true")
		}
	})

	t.Run("HasVouched returns false for non-existent vouch", func(t *testing.T) {
		hasVouched, err := vouchRepo.HasVouched(ctx, vouchedID, voucherID, school.ID)
		if err != nil {
			t.Fatalf("Failed to check HasVouched: %v", err)
		}
		if hasVouched {
			t.Error("Expected HasVouched to return false for reverse direction")
		}
	})
}

func TestSchoolVouchCountForUser(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	vouchRepo := NewSchoolVouchRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "VouchCount District", "CA")
	school := createSchoolTestSchool(t, db, schoolRepo, "SCH_"+uniqueSuffix, "VouchCount School", "CA", &district.ID)
	voucherID1 := createSchoolTestUser(t, db, "vcnt_voucher1_"+uniqueSuffix)
	voucherID2 := createSchoolTestUser(t, db, "vcnt_voucher2_"+uniqueSuffix)
	vouchedID := createSchoolTestUser(t, db, "vcnt_vouched_"+uniqueSuffix)

	addVerifiedAdminToSchool(t, db, voucherID1, school.ID)
	addVerifiedAdminToSchool(t, db, voucherID2, school.ID)
	addPendingMemberToSchool(t, db, vouchedID, school.ID)

	defer cleanupSchoolTest(t, db, []string{voucherID1, voucherID2, vouchedID}, []string{school.ID}, []string{district.ID})

	t.Run("CountVouchesForUser returns correct count", func(t *testing.T) {
		// Initially no vouches
		count, err := vouchRepo.CountVouchesForUser(ctx, vouchedID, school.ID)
		if err != nil {
			t.Fatalf("Failed to count vouches: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected 0 vouches initially, got %d", count)
		}

		// Add vouches
		vouch1 := &models.SchoolVouch{
			VoucherUserID: voucherID1,
			VouchedUserID: vouchedID,
			SchoolID:      school.ID,
		}
		_ = vouchRepo.Create(ctx, vouch1)

		vouch2 := &models.SchoolVouch{
			VoucherUserID: voucherID2,
			VouchedUserID: vouchedID,
			SchoolID:      school.ID,
		}
		_ = vouchRepo.Create(ctx, vouch2)

		count, err = vouchRepo.CountVouchesForUser(ctx, vouchedID, school.ID)
		if err != nil {
			t.Fatalf("Failed to count vouches: %v", err)
		}
		if count != 2 {
			t.Errorf("Expected 2 vouches, got %d", count)
		}
	})
}

func TestSchoolVouchMonthlyLimit(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	vouchRepo := NewSchoolVouchRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "MonthlyLimit District", "CA")
	school := createSchoolTestSchool(t, db, schoolRepo, "SCH_"+uniqueSuffix, "MonthlyLimit School", "CA", &district.ID)
	voucherID := createSchoolTestUser(t, db, "vmon_voucher_"+uniqueSuffix)

	addVerifiedAdminToSchool(t, db, voucherID, school.ID)

	// Create multiple users to vouch for
	vouchedIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		vouchedIDs[i] = createSchoolTestUser(t, db, "vmon_vouched"+string(rune('a'+i))+"_"+uniqueSuffix)
		addPendingMemberToSchool(t, db, vouchedIDs[i], school.ID)
	}

	allUserIDs := append([]string{voucherID}, vouchedIDs...)
	defer cleanupSchoolTest(t, db, allUserIDs, []string{school.ID}, []string{district.ID})

	t.Run("CountVouchesThisMonth counts correctly", func(t *testing.T) {
		// Initially no vouches this month
		count, err := vouchRepo.CountVouchesThisMonth(ctx, voucherID)
		if err != nil {
			t.Fatalf("Failed to count monthly vouches: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected 0 monthly vouches initially, got %d", count)
		}

		// Create vouches
		for _, vouchedID := range vouchedIDs {
			vouch := &models.SchoolVouch{
				VoucherUserID: voucherID,
				VouchedUserID: vouchedID,
				SchoolID:      school.ID,
			}
			_ = vouchRepo.Create(ctx, vouch)
		}

		count, err = vouchRepo.CountVouchesThisMonth(ctx, voucherID)
		if err != nil {
			t.Fatalf("Failed to count monthly vouches: %v", err)
		}
		if count != 3 {
			t.Errorf("Expected 3 monthly vouches, got %d", count)
		}
	})
}

func TestSchoolVouchGetVouchersForUser(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	vouchRepo := NewSchoolVouchRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "GetVouchers District", "CA")
	school := createSchoolTestSchool(t, db, schoolRepo, "SCH_"+uniqueSuffix, "GetVouchers School", "CA", &district.ID)
	voucherID1 := createSchoolTestUser(t, db, "vgfu_voucher1_"+uniqueSuffix)
	voucherID2 := createSchoolTestUser(t, db, "vgfu_voucher2_"+uniqueSuffix)
	vouchedID := createSchoolTestUser(t, db, "vgfu_vouched_"+uniqueSuffix)

	addVerifiedAdminToSchool(t, db, voucherID1, school.ID)
	addVerifiedAdminToSchool(t, db, voucherID2, school.ID)
	addPendingMemberToSchool(t, db, vouchedID, school.ID)

	// Create vouches
	vouch1 := &models.SchoolVouch{
		VoucherUserID: voucherID1,
		VouchedUserID: vouchedID,
		SchoolID:      school.ID,
	}
	_ = vouchRepo.Create(ctx, vouch1)

	vouch2 := &models.SchoolVouch{
		VoucherUserID: voucherID2,
		VouchedUserID: vouchedID,
		SchoolID:      school.ID,
	}
	_ = vouchRepo.Create(ctx, vouch2)

	defer cleanupSchoolTest(t, db, []string{voucherID1, voucherID2, vouchedID}, []string{school.ID}, []string{district.ID})

	t.Run("returns list of vouchers for user", func(t *testing.T) {
		vouchers, err := vouchRepo.GetVouchersForUser(ctx, vouchedID, school.ID)
		if err != nil {
			t.Fatalf("Failed to get vouchers: %v", err)
		}
		if len(vouchers) != 2 {
			t.Fatalf("Expected 2 vouchers, got %d", len(vouchers))
		}

		voucherUserIDs := make(map[string]bool)
		for _, voucherInfo := range vouchers {
			voucherUserIDs[voucherInfo.UserID] = true
			if voucherInfo.Username == "" {
				t.Error("Expected username to be populated")
			}
			if voucherInfo.VouchedAt.IsZero() {
				t.Error("Expected VouchedAt to be set")
			}
		}

		if !voucherUserIDs[voucherID1] {
			t.Error("Expected to find voucher1 in results")
		}
		if !voucherUserIDs[voucherID2] {
			t.Error("Expected to find voucher2 in results")
		}
	})

	t.Run("returns empty list for user with no vouches", func(t *testing.T) {
		vouchers, err := vouchRepo.GetVouchersForUser(ctx, "non-existent-user", school.ID)
		if err != nil {
			t.Fatalf("Failed to get vouchers: %v", err)
		}
		if len(vouchers) != 0 {
			t.Errorf("Expected 0 vouchers, got %d", len(vouchers))
		}
	})
}

func TestSchoolVouchGetPendingUsers(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	vouchRepo := NewSchoolVouchRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "Pending District", "CA")
	school := createSchoolTestSchool(t, db, schoolRepo, "SCH_"+uniqueSuffix, "Pending School", "CA", &district.ID)
	adminID := createSchoolTestUser(t, db, "vpnd_admin_"+uniqueSuffix)
	pendingID1 := createSchoolTestUser(t, db, "vpnd_pend1_"+uniqueSuffix)
	pendingID2 := createSchoolTestUser(t, db, "vpnd_pend2_"+uniqueSuffix)
	verifiedID := createSchoolTestUser(t, db, "vpnd_verif_"+uniqueSuffix)

	addVerifiedAdminToSchool(t, db, adminID, school.ID)
	addPendingMemberToSchool(t, db, pendingID1, school.ID)
	addPendingMemberToSchool(t, db, pendingID2, school.ID)
	addVerifiedAdminToSchool(t, db, verifiedID, school.ID)

	// Give one pending user a vouch
	vouch := &models.SchoolVouch{
		VoucherUserID: adminID,
		VouchedUserID: pendingID1,
		SchoolID:      school.ID,
	}
	_ = vouchRepo.Create(ctx, vouch)

	defer cleanupSchoolTest(t, db, []string{adminID, pendingID1, pendingID2, verifiedID}, []string{school.ID}, []string{district.ID})

	t.Run("returns users with pending status", func(t *testing.T) {
		pendingUsers, err := vouchRepo.GetPendingVouchUsers(ctx, school.ID)
		if err != nil {
			t.Fatalf("Failed to get pending vouch users: %v", err)
		}
		if len(pendingUsers) != 2 {
			t.Fatalf("Expected 2 pending users, got %d", len(pendingUsers))
		}

		foundPending1 := false
		foundPending2 := false
		for _, pendingUser := range pendingUsers {
			if pendingUser.UserID == pendingID1 {
				foundPending1 = true
				if pendingUser.VouchCount != 1 {
					t.Errorf("Expected vouch count 1 for pending user 1, got %d", pendingUser.VouchCount)
				}
			}
			if pendingUser.UserID == pendingID2 {
				foundPending2 = true
				if pendingUser.VouchCount != 0 {
					t.Errorf("Expected vouch count 0 for pending user 2, got %d", pendingUser.VouchCount)
				}
			}
			if pendingUser.SchoolName != "Pending School" {
				t.Errorf("Expected school name 'Pending School', got '%s'", pendingUser.SchoolName)
			}
			if pendingUser.Username == "" {
				t.Error("Expected username to be populated")
			}
			if pendingUser.Email == "" {
				t.Error("Expected email to be populated")
			}
		}

		if !foundPending1 {
			t.Error("Expected to find pending user 1 in results")
		}
		if !foundPending2 {
			t.Error("Expected to find pending user 2 in results")
		}
	})

	t.Run("does not include verified users", func(t *testing.T) {
		pendingUsers, err := vouchRepo.GetPendingVouchUsers(ctx, school.ID)
		if err != nil {
			t.Fatalf("Failed to get pending vouch users: %v", err)
		}
		for _, pendingUser := range pendingUsers {
			if pendingUser.UserID == verifiedID {
				t.Error("Did not expect verified user to appear in pending results")
			}
			if pendingUser.UserID == adminID {
				t.Error("Did not expect admin user to appear in pending results")
			}
		}
	})

	t.Run("returns empty list for school with no pending members", func(t *testing.T) {
		emptySchool := createSchoolTestSchool(t, db, schoolRepo, "SCHE"+uniqueSuffix, "No Pending School", "CA", &district.ID)
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM schools WHERE id = ?", emptySchool.ID)
		}()

		pendingUsers, err := vouchRepo.GetPendingVouchUsers(ctx, emptySchool.ID)
		if err != nil {
			t.Fatalf("Failed to get pending vouch users: %v", err)
		}
		if len(pendingUsers) != 0 {
			t.Errorf("Expected 0 pending users, got %d", len(pendingUsers))
		}
	})
}

// =============================================================================
// GetDistrictMembers Tests
// =============================================================================

func TestSchoolGetDistrictMembers(t *testing.T) {
	db := testDB(t)
	schoolRepo := NewSchoolRepository(db)
	districtRepo := NewSchoolDistrictRepository(db)
	ctx := context.Background()

	uniqueSuffix := uuid.New().String()[:8]
	district := createSchoolTestDistrict(t, db, districtRepo, uniqueSuffix[:7], "DistMembers District", "CA")
	schoolAlpha := createSchoolTestSchool(t, db, schoolRepo, "DMA"+uniqueSuffix, "Alpha School", "CA", &district.ID)
	schoolBeta := createSchoolTestSchool(t, db, schoolRepo, "DMB"+uniqueSuffix, "Beta School", "CA", &district.ID)
	userID1 := createSchoolTestUser(t, db, "distmem1_"+uniqueSuffix)
	userID2 := createSchoolTestUser(t, db, "distmem2_"+uniqueSuffix)
	userID3 := createSchoolTestUser(t, db, "distmem3_"+uniqueSuffix)

	// user1: verified admin in Alpha School
	addVerifiedAdminToSchool(t, db, userID1, schoolAlpha.ID)
	// user2: pending member in Beta School
	addPendingMemberToSchool(t, db, userID2, schoolBeta.ID)
	// user3: in both schools (Alpha and Beta) - should appear once with Alpha (alphabetically first)
	addVerifiedAdminToSchool(t, db, userID3, schoolAlpha.ID)
	addPendingMemberToSchool(t, db, userID3, schoolBeta.ID)

	defer cleanupSchoolTest(t, db, []string{userID1, userID2, userID3}, []string{schoolAlpha.ID, schoolBeta.ID}, []string{district.ID})

	t.Run("retrieves all members across schools in district", func(t *testing.T) {
		members, err := schoolRepo.GetDistrictMembers(ctx, district.ID)
		if err != nil {
			t.Fatalf("Failed to get district members: %v", err)
		}
		if len(members) != 3 {
			t.Fatalf("Expected 3 members, got %d", len(members))
		}
	})

	t.Run("deduplicates users in multiple schools", func(t *testing.T) {
		members, err := schoolRepo.GetDistrictMembers(ctx, district.ID)
		if err != nil {
			t.Fatalf("Failed to get district members: %v", err)
		}

		// Count how many times userID3 appears - should be exactly 1
		countUser3 := 0
		for _, member := range members {
			if member.UserID == userID3 {
				countUser3++
				// Should be listed under Alpha School (alphabetically first)
				if member.SchoolName != "Alpha School" {
					t.Errorf("Expected user3 school_name 'Alpha School', got %s", member.SchoolName)
				}
			}
		}
		if countUser3 != 1 {
			t.Errorf("Expected user3 to appear exactly once, appeared %d times", countUser3)
		}
	})

	t.Run("includes correct fields", func(t *testing.T) {
		members, err := schoolRepo.GetDistrictMembers(ctx, district.ID)
		if err != nil {
			t.Fatalf("Failed to get district members: %v", err)
		}

		foundAdmin := false
		foundPending := false
		for _, member := range members {
			if member.UserID == userID1 {
				if !member.IsAdmin || member.VerificationStatus != models.SchoolVerificationStatusVerified {
					t.Errorf("Expected user1 to be verified admin")
				}
				if member.SchoolName != "Alpha School" {
					t.Errorf("Expected user1 school_name 'Alpha School', got %s", member.SchoolName)
				}
				foundAdmin = true
			}
			if member.UserID == userID2 {
				if member.IsAdmin || member.VerificationStatus != models.SchoolVerificationStatusPending {
					t.Errorf("Expected user2 to be pending non-admin")
				}
				if member.SchoolName != "Beta School" {
					t.Errorf("Expected user2 school_name 'Beta School', got %s", member.SchoolName)
				}
				foundPending = true
			}
		}
		if !foundAdmin {
			t.Error("Expected to find verified admin member (user1)")
		}
		if !foundPending {
			t.Error("Expected to find pending member (user2)")
		}
	})

	t.Run("returns empty list for district with no members", func(t *testing.T) {
		emptyDistrict := createSchoolTestDistrict(t, db, districtRepo, "E"+uniqueSuffix[:6], "Empty District", "CA")
		defer func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM school_districts WHERE id = ?", emptyDistrict.ID)
		}()

		members, err := schoolRepo.GetDistrictMembers(ctx, emptyDistrict.ID)
		if err != nil {
			t.Fatalf("Failed to get district members: %v", err)
		}
		if len(members) != 0 {
			t.Errorf("Expected 0 members, got %d", len(members))
		}
	})

	t.Run("ordered by verification status desc then username", func(t *testing.T) {
		members, err := schoolRepo.GetDistrictMembers(ctx, district.ID)
		if err != nil {
			t.Fatalf("Failed to get district members: %v", err)
		}

		// Verified members should come before pending ones
		seenPending := false
		for _, member := range members {
			if member.VerificationStatus == models.SchoolVerificationStatusPending {
				seenPending = true
			}
			if seenPending && member.VerificationStatus == models.SchoolVerificationStatusVerified {
				t.Error("Found verified member after pending member - ordering is wrong")
			}
		}
	})
}
