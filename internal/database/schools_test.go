package database

import (
	"context"
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
