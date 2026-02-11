package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

type schoolTestSuite struct {
	t            *testing.T
	db           *database.DB
	userRepo     *database.UserRepository
	schoolRepo   *database.SchoolRepository
	districtRepo *database.SchoolDistrictRepository
	vouchRepo    *database.SchoolVouchRepository
	groupRepo    *database.SignalGroupRepository
	proposalRepo *database.InviteLinkProposalRepository
	auditRepo    *database.AuditRepository
	handler      *SchoolHandler
}

func setupSchoolTestSuite(t *testing.T) *schoolTestSuite {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	schoolRepo := database.NewSchoolRepository(db)
	districtRepo := database.NewSchoolDistrictRepository(db)
	vouchRepo := database.NewSchoolVouchRepository(db)
	groupRepo := database.NewSignalGroupRepository(db)
	proposalRepo := database.NewInviteLinkProposalRepository(db)
	auditRepo := database.NewAuditRepository(db)
	consensusConfig := &config.ConsensusConfig{VotePercent: 50, VoteFloor: 3}

	handler := NewSchoolHandler(
		db,
		schoolRepo,
		districtRepo,
		vouchRepo,
		groupRepo,
		proposalRepo,
		userRepo,
		auditRepo,
		nil, // ncesService - not needed in tests
		consensusConfig,
		false, // bootstrapCooldownEnabled
		0,     // bootstrapCooldownMinutes
	)

	return &schoolTestSuite{
		t:            t,
		db:           db,
		userRepo:     userRepo,
		schoolRepo:   schoolRepo,
		districtRepo: districtRepo,
		vouchRepo:    vouchRepo,
		groupRepo:    groupRepo,
		proposalRepo: proposalRepo,
		auditRepo:    auditRepo,
		handler:      handler,
	}
}

func (s *schoolTestSuite) createTestUser(username string, tier models.VerificationTier) *models.User {
	user := &models.User{
		Username:         username,
		Email:            username + "@schooltest.com",
		PasswordHash:     "$2a$12$test.hash.for.testing.only",
		VerificationTier: tier,
	}
	if err := s.userRepo.Create(context.Background(), user); err != nil {
		s.t.Fatalf("Failed to create test user: %v", err)
	}
	return user
}

func (s *schoolTestSuite) createTestSchool(name, state string) *models.School {
	school := &models.School{
		ID:     uuid.New().String(),
		NCESID: uuid.New().String()[:12],
		Name:   name,
		State:  state,
	}
	_, err := s.db.ExecContext(context.Background(),
		"INSERT INTO schools (id, nces_id, name, state, created_at) VALUES (?, ?, ?, ?, NOW())",
		school.ID, school.NCESID, school.Name, school.State)
	if err != nil {
		s.t.Fatalf("Failed to create test school: %v", err)
	}
	return school
}

func (s *schoolTestSuite) createTestDistrict(name, state string) *models.SchoolDistrict {
	district := &models.SchoolDistrict{
		ID:           uuid.New().String(),
		NCESID:       uuid.New().String()[:7],
		Name:         name,
		State:        state,
		DistrictType: models.SchoolDistrictTypeUnified,
	}
	_, err := s.db.ExecContext(context.Background(),
		"INSERT INTO school_districts (id, nces_id, name, state, district_type, created_at) VALUES (?, ?, ?, ?, ?, NOW())",
		district.ID, district.NCESID, district.Name, district.State, district.DistrictType)
	if err != nil {
		s.t.Fatalf("Failed to create test district: %v", err)
	}
	return district
}

func (s *schoolTestSuite) addUserToSchool(userID, schoolID string, status models.SchoolVerificationStatus, isAdmin bool) {
	membershipID := uuid.New().String()
	query := "INSERT INTO user_schools (id, user_id, school_id, is_admin, verification_status) VALUES (?, ?, ?, ?, ?)"
	if status == models.SchoolVerificationStatusVerified {
		query = "INSERT INTO user_schools (id, user_id, school_id, is_admin, verification_status, verified_at) VALUES (?, ?, ?, ?, ?, NOW())"
	}
	var err error
	if status == models.SchoolVerificationStatusVerified {
		_, err = s.db.ExecContext(context.Background(), query, membershipID, userID, schoolID, isAdmin, status)
	} else {
		_, err = s.db.ExecContext(context.Background(), query, membershipID, userID, schoolID, isAdmin, status)
	}
	if err != nil {
		s.t.Fatalf("Failed to add user to school: %v", err)
	}
}

func (s *schoolTestSuite) blockUserInSchool(userID, schoolID, blockedBy string) {
	blockID := uuid.New().String()
	_, err := s.db.ExecContext(context.Background(),
		"INSERT INTO school_blocked_users (id, user_id, school_id, blocked_by, created_at) VALUES (?, ?, ?, ?, NOW())",
		blockID, userID, schoolID, blockedBy)
	if err != nil {
		s.t.Fatalf("Failed to block user in school: %v", err)
	}
}

func (s *schoolTestSuite) cleanup(userIDs []string, schoolIDs []string, districtIDs []string) {
	ctx := context.Background()
	for _, schoolID := range schoolIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM school_vouches WHERE school_id = ?", schoolID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE school_id = ?", schoolID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM school_blocked_users WHERE school_id = ?", schoolID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_schools WHERE school_id = ?", schoolID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM schools WHERE id = ?", schoolID)
	}
	for _, districtID := range districtIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE district_id = ?", districtID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM school_districts WHERE id = ?", districtID)
	}
	for _, userID := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM school_vouches WHERE voucher_user_id = ? OR vouched_user_id = ?", userID, userID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_schools WHERE user_id = ?", userID)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", userID)
	}
}

func (s *schoolTestSuite) authenticatedRequest(method, url string, body interface{}, claims *middleware.Claims) (*http.Request, *httptest.ResponseRecorder) {
	var reqBody *bytes.Reader
	if body != nil {
		bodyBytes, _ := json.Marshal(body)
		reqBody = bytes.NewReader(bodyBytes)
	} else {
		reqBody = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, url, reqBody)
	req.Header.Set("Content-Type", "application/json")

	if claims != nil {
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)
	}

	rec := httptest.NewRecorder()
	return req, rec
}

// =============================================================================
// Search Tests
// =============================================================================

func TestSchoolHandler_Search(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	// Clean up any previous test data
	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	schoolAlpha := suite.createTestSchool("Alpha Elementary School", "CA")
	schoolBeta := suite.createTestSchool("Beta Middle School", "CA")
	schoolGamma := suite.createTestSchool("Gamma High School", "NY")

	defer suite.cleanup(nil, []string{schoolAlpha.ID, schoolBeta.ID, schoolGamma.ID}, nil)

	t.Run("search returns results", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/search?query=School", nil, nil)
		suite.handler.Search(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.SchoolSearchResponse
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if responseBody.Total < 3 {
			t.Errorf("Expected at least 3 results, got %d", responseBody.Total)
		}
	})

	t.Run("search with state filter", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/search?query=School&state=CA", nil, nil)
		suite.handler.Search(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.SchoolSearchResponse
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		for _, schoolSummary := range responseBody.Schools {
			if schoolSummary.State != "CA" {
				t.Errorf("Expected all results to be in CA, got state %s for school %s", schoolSummary.State, schoolSummary.Name)
			}
		}

		if responseBody.Total < 2 {
			t.Errorf("Expected at least 2 CA schools, got %d", responseBody.Total)
		}
	})

	t.Run("search with pagination", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/search?query=School&limit=1&page=1", nil, nil)
		suite.handler.Search(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.SchoolSearchResponse
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if len(responseBody.Schools) != 1 {
			t.Errorf("Expected 1 school per page, got %d", len(responseBody.Schools))
		}

		if responseBody.Limit != 1 {
			t.Errorf("Expected limit 1, got %d", responseBody.Limit)
		}

		if responseBody.Page != 1 {
			t.Errorf("Expected page 1, got %d", responseBody.Page)
		}

		if !responseBody.HasMore {
			t.Error("Expected has_more to be true when total exceeds page size")
		}
	})
}

// =============================================================================
// Get Tests
// =============================================================================

func TestSchoolHandler_Get(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	school := suite.createTestSchool("Get Test School", "CA")
	defer suite.cleanup(nil, []string{school.ID}, nil)

	t.Run("get existing school returns details", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+school.ID+"?id="+school.ID, nil, nil)
		suite.handler.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.SchoolWithDetails
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if responseBody.ID != school.ID {
			t.Errorf("Expected school ID %s, got %s", school.ID, responseBody.ID)
		}

		if responseBody.Name != "Get Test School" {
			t.Errorf("Expected school name 'Get Test School', got %s", responseBody.Name)
		}
	})

	t.Run("get non-existent school returns 404", func(t *testing.T) {
		nonExistentID := uuid.New().String()
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+nonExistentID+"?id="+nonExistentID, nil, nil)
		suite.handler.Get(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// =============================================================================
// Join Tests
// =============================================================================

func TestSchoolHandler_Join(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	school := suite.createTestSchool("Join Test School", "CA")
	regularUser := suite.createTestUser("school_join_user", models.TierUnverified)
	blockedUser := suite.createTestUser("school_join_blocked", models.TierUnverified)
	existingMember := suite.createTestUser("school_join_existing", models.TierUnverified)

	// Set up: block one user, make another already a member
	suite.blockUserInSchool(blockedUser.ID, school.ID, regularUser.ID)
	suite.addUserToSchool(existingMember.ID, school.ID, models.SchoolVerificationStatusPending, false)

	defer suite.cleanup(
		[]string{regularUser.ID, blockedUser.ID, existingMember.ID},
		[]string{school.ID},
		nil,
	)

	t.Run("join requires authentication", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/join?id="+school.ID, nil, nil)
		suite.handler.Join(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("join a school successfully", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           regularUser.ID,
			Email:            regularUser.Email,
			VerificationTier: regularUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/join?id="+school.ID, nil, claims)
		suite.handler.Join(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if responseBody["membership_id"] == nil {
			t.Error("Expected membership_id in response")
		}

		if responseBody["status"] != "pending" {
			t.Errorf("Expected status 'pending', got %v", responseBody["status"])
		}

		if responseBody["school_id"] != school.ID {
			t.Errorf("Expected school_id %s, got %v", school.ID, responseBody["school_id"])
		}
	})

	t.Run("join same school twice returns conflict", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           regularUser.ID,
			Email:            regularUser.Email,
			VerificationTier: regularUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/join?id="+school.ID, nil, claims)
		suite.handler.Join(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("join while blocked returns forbidden", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           blockedUser.ID,
			Email:            blockedUser.Email,
			VerificationTier: blockedUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/join?id="+school.ID, nil, claims)
		suite.handler.Join(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// =============================================================================
// Leave Tests
// =============================================================================

func TestSchoolHandler_Leave(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	school := suite.createTestSchool("Leave Test School", "CA")
	memberUser := suite.createTestUser("school_leave_member", models.TierUnverified)

	suite.addUserToSchool(memberUser.ID, school.ID, models.SchoolVerificationStatusPending, false)

	defer suite.cleanup([]string{memberUser.ID}, []string{school.ID}, nil)

	t.Run("leave requires authentication", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/leave?id="+school.ID, nil, nil)
		suite.handler.Leave(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("leave school successfully", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           memberUser.ID,
			Email:            memberUser.Email,
			VerificationTier: memberUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/leave?id="+school.ID, nil, claims)
		suite.handler.Leave(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if responseBody["school_id"] != school.ID {
			t.Errorf("Expected school_id %s, got %v", school.ID, responseBody["school_id"])
		}

		if responseBody["message"] != "Successfully left school" {
			t.Errorf("Expected success message, got %v", responseBody["message"])
		}
	})
}

// =============================================================================
// Vouch Tests
// =============================================================================

func TestSchoolHandler_Vouch(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	school := suite.createTestSchool("Vouch Test School", "CA")
	voucherUser := suite.createTestUser("school_voucher", models.TierPostcard)
	voucheeUser := suite.createTestUser("school_vouchee", models.TierUnverified)
	voucher2User := suite.createTestUser("school_voucher2", models.TierPostcard)
	voucher3User := suite.createTestUser("school_voucher3", models.TierPostcard)

	// All users are members of the school (pending except voucher)
	suite.addUserToSchool(voucherUser.ID, school.ID, models.SchoolVerificationStatusPending, false)
	suite.addUserToSchool(voucheeUser.ID, school.ID, models.SchoolVerificationStatusPending, false)
	suite.addUserToSchool(voucher2User.ID, school.ID, models.SchoolVerificationStatusPending, false)
	suite.addUserToSchool(voucher3User.ID, school.ID, models.SchoolVerificationStatusPending, false)

	defer suite.cleanup(
		[]string{voucherUser.ID, voucheeUser.ID, voucher2User.ID, voucher3User.ID},
		[]string{school.ID},
		nil,
	)

	t.Run("vouch requires authentication", func(t *testing.T) {
		requestBody := models.SchoolVouchRequest{
			UserIdentifier: voucheeUser.Email,
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/vouch?id="+school.ID, requestBody, nil)
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("self-vouch returns error", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           voucherUser.ID,
			Email:            voucherUser.Email,
			VerificationTier: voucherUser.VerificationTier,
		}
		requestBody := models.SchoolVouchRequest{
			UserIdentifier: voucherUser.Email,
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/vouch?id="+school.ID, requestBody, claims)
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("vouch in bootstrap mode succeeds", func(t *testing.T) {
		// In bootstrap mode (no verified admins), any member can vouch
		claims := &middleware.Claims{
			UserID:           voucherUser.ID,
			Email:            voucherUser.Email,
			VerificationTier: voucherUser.VerificationTier,
		}
		requestBody := models.SchoolVouchRequest{
			UserIdentifier: voucheeUser.Email,
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/vouch?id="+school.ID, requestBody, claims)
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.SchoolVouchResponse
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if responseBody.VouchID == "" {
			t.Error("Expected a vouch_id in response")
		}

		if !responseBody.BootstrapMode {
			t.Error("Expected bootstrap_mode to be true")
		}

		if responseBody.TotalVouches != 1 {
			t.Errorf("Expected total_vouches 1, got %d", responseBody.TotalVouches)
		}

		// In bootstrap mode, 3 vouches are required
		if responseBody.VouchesRequired != 3 {
			t.Errorf("Expected vouches_required 3 (bootstrap), got %d", responseBody.VouchesRequired)
		}
	})

	t.Run("second vouch from different user succeeds", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           voucher2User.ID,
			Email:            voucher2User.Email,
			VerificationTier: voucher2User.VerificationTier,
		}
		requestBody := models.SchoolVouchRequest{
			UserIdentifier: voucheeUser.Email,
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/vouch?id="+school.ID, requestBody, claims)
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.SchoolVouchResponse
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if responseBody.TotalVouches != 2 {
			t.Errorf("Expected total_vouches 2, got %d", responseBody.TotalVouches)
		}

		if responseBody.VouchesNeeded != 1 {
			t.Errorf("Expected vouches_needed 1, got %d", responseBody.VouchesNeeded)
		}
	})

	t.Run("third vouch triggers auto-upgrade in bootstrap mode", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           voucher3User.ID,
			Email:            voucher3User.Email,
			VerificationTier: voucher3User.VerificationTier,
		}
		requestBody := models.SchoolVouchRequest{
			UserIdentifier: voucheeUser.Email,
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/vouch?id="+school.ID, requestBody, claims)
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.SchoolVouchResponse
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if responseBody.TotalVouches != 3 {
			t.Errorf("Expected total_vouches 3, got %d", responseBody.TotalVouches)
		}

		if responseBody.VouchesNeeded != 0 {
			t.Errorf("Expected vouches_needed 0, got %d", responseBody.VouchesNeeded)
		}
	})

	t.Run("duplicate vouch returns conflict", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           voucherUser.ID,
			Email:            voucherUser.Email,
			VerificationTier: voucherUser.VerificationTier,
		}
		requestBody := models.SchoolVouchRequest{
			UserIdentifier: voucheeUser.Email,
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/vouch?id="+school.ID, requestBody, claims)
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("monthly vouch limit check", func(t *testing.T) {
		// Create 10 additional users and vouch for each to exhaust the monthly limit
		limitVoucherUser := suite.createTestUser("school_limit_voucher", models.TierPostcard)
		suite.addUserToSchool(limitVoucherUser.ID, school.ID, models.SchoolVerificationStatusPending, false)
		defer func() {
			suite.cleanup([]string{limitVoucherUser.ID}, nil, nil)
		}()

		var additionalTargetUserIDs []string
		for i := 0; i < 10; i++ {
			targetUser := suite.createTestUser("school_limit_target_"+string(rune('a'+i)), models.TierUnverified)
			suite.addUserToSchool(targetUser.ID, school.ID, models.SchoolVerificationStatusPending, false)
			additionalTargetUserIDs = append(additionalTargetUserIDs, targetUser.ID)

			// Create vouches directly in db to fill up the limit
			vouchID := uuid.New().String()
			_, _ = suite.db.ExecContext(context.Background(),
				"INSERT INTO school_vouches (id, voucher_user_id, vouched_user_id, school_id, created_at) VALUES (?, ?, ?, ?, NOW())",
				vouchID, limitVoucherUser.ID, targetUser.ID, school.ID)
		}
		defer func() {
			suite.cleanup(additionalTargetUserIDs, nil, nil)
		}()

		// Now try to vouch for one more person -- should hit monthly limit
		overLimitTarget := suite.createTestUser("school_limit_over", models.TierUnverified)
		suite.addUserToSchool(overLimitTarget.ID, school.ID, models.SchoolVerificationStatusPending, false)
		defer func() {
			suite.cleanup([]string{overLimitTarget.ID}, nil, nil)
		}()

		claims := &middleware.Claims{
			UserID:           limitVoucherUser.ID,
			Email:            limitVoucherUser.Email,
			VerificationTier: limitVoucherUser.VerificationTier,
		}
		requestBody := models.SchoolVouchRequest{
			UserIdentifier: overLimitTarget.Email,
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/vouch?id="+school.ID, requestBody, claims)
		suite.handler.Vouch(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("Expected status 429, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// =============================================================================
// ListMembers Tests
// =============================================================================

func TestSchoolHandler_ListMembers(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	school := suite.createTestSchool("Members Test School", "CA")
	verifiedUser := suite.createTestUser("school_verified_member", models.TierPostcard)
	pendingUser := suite.createTestUser("school_pending_member", models.TierPostcard)
	nonMemberUser := suite.createTestUser("school_nonmember_user", models.TierPostcard)

	suite.addUserToSchool(verifiedUser.ID, school.ID, models.SchoolVerificationStatusVerified, false)
	suite.addUserToSchool(pendingUser.ID, school.ID, models.SchoolVerificationStatusPending, false)

	defer suite.cleanup(
		[]string{verifiedUser.ID, pendingUser.ID, nonMemberUser.ID},
		[]string{school.ID},
		nil,
	)

	t.Run("list members requires authentication", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+school.ID+"/members?id="+school.ID, nil, nil)
		suite.handler.ListMembers(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("list members requires membership", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           nonMemberUser.ID,
			Email:            nonMemberUser.Email,
			VerificationTier: nonMemberUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+school.ID+"/members?id="+school.ID, nil, claims)
		suite.handler.ListMembers(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("pending member cannot list members", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           pendingUser.ID,
			Email:            pendingUser.Email,
			VerificationTier: pendingUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+school.ID+"/members?id="+school.ID, nil, claims)
		suite.handler.ListMembers(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("verified member can list members", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: verifiedUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+school.ID+"/members?id="+school.ID, nil, claims)
		suite.handler.ListMembers(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		membersRaw, ok := responseBody["members"]
		if !ok {
			t.Fatal("Expected 'members' key in response")
		}

		membersList, ok := membersRaw.([]interface{})
		if !ok {
			t.Fatal("Expected 'members' to be an array")
		}

		if len(membersList) < 1 {
			t.Error("Expected at least 1 member in the list")
		}
	})
}

// =============================================================================
// CreateSignalGroup Tests
// =============================================================================

func TestSchoolHandler_CreateSignalGroup(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	school := suite.createTestSchool("SignalGroup Test School", "CA")
	adminUser := suite.createTestUser("school_sg_admin", models.TierPostcard)
	regularUser := suite.createTestUser("school_sg_regular", models.TierUnverified)

	// adminUser is a verified admin; set up enough admins to exit bootstrap mode
	suite.addUserToSchool(adminUser.ID, school.ID, models.SchoolVerificationStatusVerified, true)
	suite.addUserToSchool(regularUser.ID, school.ID, models.SchoolVerificationStatusPending, false)

	// Create two more verified admins to exit bootstrap mode (need 3 total)
	admin2User := suite.createTestUser("school_sg_admin2", models.TierPostcard)
	admin3User := suite.createTestUser("school_sg_admin3", models.TierPostcard)
	suite.addUserToSchool(admin2User.ID, school.ID, models.SchoolVerificationStatusVerified, true)
	suite.addUserToSchool(admin3User.ID, school.ID, models.SchoolVerificationStatusVerified, true)

	defer suite.cleanup(
		[]string{adminUser.ID, regularUser.ID, admin2User.ID, admin3User.ID},
		[]string{school.ID},
		nil,
	)

	t.Run("requires authentication", func(t *testing.T) {
		requestBody := models.CreateSchoolSignalGroupRequest{
			Name:       "Test Group",
			InviteLink: "https://signal.group/test123",
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/signal-groups?id="+school.ID, requestBody, nil)
		suite.handler.CreateSignalGroup(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("non-admin gets forbidden", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           regularUser.ID,
			Email:            regularUser.Email,
			VerificationTier: regularUser.VerificationTier,
		}
		requestBody := models.CreateSchoolSignalGroupRequest{
			Name:       "Unauthorized Group",
			InviteLink: "https://signal.group/unauth",
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/signal-groups?id="+school.ID, requestBody, claims)
		suite.handler.CreateSignalGroup(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("admin can create signal group", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			VerificationTier: adminUser.VerificationTier,
		}
		requestBody := models.CreateSchoolSignalGroupRequest{
			Name:       "School Test Signal Group",
			InviteLink: "https://signal.group/schooltest123",
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/signal-groups?id="+school.ID, requestBody, claims)
		suite.handler.CreateSignalGroup(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if responseBody["group_id"] == nil {
			t.Error("Expected group_id in response")
		}

		if responseBody["group_name"] != "School Test Signal Group" {
			t.Errorf("Expected group_name 'School Test Signal Group', got %v", responseBody["group_name"])
		}

		if responseBody["school_id"] != school.ID {
			t.Errorf("Expected school_id %s, got %v", school.ID, responseBody["school_id"])
		}

		// Clean up created group
		if groupID, ok := responseBody["group_id"].(string); ok {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM signal_groups WHERE id = ?", groupID)
		}
	})

	t.Run("group limit prevents creation", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			VerificationTier: adminUser.VerificationTier,
		}

		// Create 5 groups to hit the limit
		var createdGroupIDs []string
		for i := 0; i < 5; i++ {
			group := &models.SignalGroup{
				SchoolID:            &school.ID,
				GroupName:           "Limit Test Group",
				InviteLink:          "https://signal.group/limit",
				InviteLinkUpdatedBy: &adminUser.ID,
				CreatedBy:           &adminUser.ID,
			}
			if err := suite.groupRepo.Create(context.Background(), group); err != nil {
				t.Fatalf("Failed to create test group: %v", err)
			}
			createdGroupIDs = append(createdGroupIDs, group.ID)
		}

		requestBody := models.CreateSchoolSignalGroupRequest{
			Name:       "Over Limit Group",
			InviteLink: "https://signal.group/overlimit",
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+school.ID+"/signal-groups?id="+school.ID, requestBody, claims)
		suite.handler.CreateSignalGroup(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}

		// Clean up
		for _, groupID := range createdGroupIDs {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM signal_groups WHERE id = ?", groupID)
		}
	})

	t.Run("bootstrap mode prevents non-superuser creation", func(t *testing.T) {
		// Create a new school that stays in bootstrap mode (no verified admins)
		bootstrapSchool := suite.createTestSchool("Bootstrap SG School", "CA")
		suite.addUserToSchool(adminUser.ID, bootstrapSchool.ID, models.SchoolVerificationStatusVerified, true)
		defer func() {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM signal_groups WHERE school_id = ?", bootstrapSchool.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM user_schools WHERE school_id = ?", bootstrapSchool.ID)
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM schools WHERE id = ?", bootstrapSchool.ID)
		}()

		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			VerificationTier: adminUser.VerificationTier,
			IsSuperuser:      false,
		}
		requestBody := models.CreateSchoolSignalGroupRequest{
			Name:       "Bootstrap Group",
			InviteLink: "https://signal.group/bootstrap",
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/schools/"+bootstrapSchool.ID+"/signal-groups?id="+bootstrapSchool.ID, requestBody, claims)
		suite.handler.CreateSignalGroup(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// =============================================================================
// ListMySchools Tests
// =============================================================================

func TestSchoolHandler_ListMySchools(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	school1 := suite.createTestSchool("My School 1", "CA")
	school2 := suite.createTestSchool("My School 2", "NY")
	memberUser := suite.createTestUser("school_my_user", models.TierPostcard)

	suite.addUserToSchool(memberUser.ID, school1.ID, models.SchoolVerificationStatusPending, false)
	suite.addUserToSchool(memberUser.ID, school2.ID, models.SchoolVerificationStatusPending, false)

	defer suite.cleanup(
		[]string{memberUser.ID},
		[]string{school1.ID, school2.ID},
		nil,
	)

	t.Run("requires authentication", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/my", nil, nil)
		suite.handler.ListMySchools(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("returns user school list", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           memberUser.ID,
			Email:            memberUser.Email,
			VerificationTier: memberUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/my", nil, claims)
		suite.handler.ListMySchools(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		schoolsRaw, ok := responseBody["schools"]
		if !ok {
			t.Fatal("Expected 'schools' key in response")
		}

		schoolsList, ok := schoolsRaw.([]interface{})
		if !ok {
			t.Fatal("Expected 'schools' to be an array")
		}

		if len(schoolsList) != 2 {
			t.Errorf("Expected 2 schools, got %d", len(schoolsList))
		}
	})
}

// =============================================================================
// Helper: Create school in district
// =============================================================================

func (s *schoolTestSuite) createTestSchoolInDistrict(name, state, districtID string) *models.School {
	school := s.createTestSchool(name, state)
	_, err := s.db.ExecContext(context.Background(),
		"UPDATE schools SET district_id = ? WHERE id = ?", districtID, school.ID)
	if err != nil {
		s.t.Fatalf("Failed to link school to district: %v", err)
	}
	return school
}

// =============================================================================
// GetPendingVouchRequests Tests
// =============================================================================

func TestSchoolHandler_GetPendingVouchRequests(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	school := suite.createTestSchool("PendingVouch Test School", "CA")
	memberUser := suite.createTestUser("school_pending_member", models.TierUnverified)
	nonMemberUser := suite.createTestUser("school_pending_nonmember", models.TierUnverified)
	pendingUser := suite.createTestUser("school_pending_target", models.TierUnverified)

	suite.addUserToSchool(memberUser.ID, school.ID, models.SchoolVerificationStatusPending, false)
	suite.addUserToSchool(pendingUser.ID, school.ID, models.SchoolVerificationStatusPending, false)

	defer suite.cleanup(
		[]string{memberUser.ID, nonMemberUser.ID, pendingUser.ID},
		[]string{school.ID},
		nil,
	)

	t.Run("requires authentication", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+school.ID+"/vouch/pending?id="+school.ID, nil, nil)
		suite.handler.GetPendingVouchRequests(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("requires membership", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           nonMemberUser.ID,
			Email:            nonMemberUser.Email,
			VerificationTier: nonMemberUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+school.ID+"/vouch/pending?id="+school.ID, nil, claims)
		suite.handler.GetPendingVouchRequests(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("member can see pending vouch requests", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           memberUser.ID,
			Email:            memberUser.Email,
			VerificationTier: memberUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+school.ID+"/vouch/pending?id="+school.ID, nil, claims)
		suite.handler.GetPendingVouchRequests(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		requestsRaw, ok := responseBody["requests"]
		if !ok {
			t.Fatal("Expected 'requests' key in response")
		}

		_, ok = requestsRaw.([]interface{})
		if !ok {
			t.Fatal("Expected 'requests' to be an array")
		}
	})

	t.Run("returns empty array when no pending requests", func(t *testing.T) {
		emptySchool := suite.createTestSchool("Empty Pending School", "CA")
		suite.addUserToSchool(memberUser.ID, emptySchool.ID, models.SchoolVerificationStatusPending, false)
		defer suite.cleanup(nil, []string{emptySchool.ID}, nil)

		claims := &middleware.Claims{
			UserID:           memberUser.ID,
			Email:            memberUser.Email,
			VerificationTier: memberUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+emptySchool.ID+"/vouch/pending?id="+emptySchool.ID, nil, claims)
		suite.handler.GetPendingVouchRequests(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody map[string]json.RawMessage
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		requestsJSON := string(responseBody["requests"])
		if requestsJSON == "null" {
			t.Error("Expected 'requests' to be [] not null")
		}
	})
}

// =============================================================================
// GetVouchStatus Tests
// =============================================================================

func TestSchoolHandler_GetVouchStatus(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	school := suite.createTestSchool("VouchStatus Test School", "CA")
	memberUser := suite.createTestUser("school_vs_member", models.TierPostcard)
	targetUser := suite.createTestUser("school_vs_target", models.TierUnverified)

	suite.addUserToSchool(memberUser.ID, school.ID, models.SchoolVerificationStatusPending, false)
	suite.addUserToSchool(targetUser.ID, school.ID, models.SchoolVerificationStatusPending, false)

	defer suite.cleanup(
		[]string{memberUser.ID, targetUser.ID},
		[]string{school.ID},
		nil,
	)

	t.Run("requires authentication", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+school.ID+"/vouch-status/"+targetUser.ID+"?id="+school.ID+"&user_id="+targetUser.ID, nil, nil)
		suite.handler.GetVouchStatus(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("happy path returns vouch status in bootstrap mode", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           memberUser.ID,
			Email:            memberUser.Email,
			VerificationTier: memberUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+school.ID+"/vouch-status/"+targetUser.ID+"?id="+school.ID+"&user_id="+targetUser.ID, nil, claims)
		suite.handler.GetVouchStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.SchoolVouchStatusResponse
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if responseBody.UserID != targetUser.ID {
			t.Errorf("Expected user_id %s, got %s", targetUser.ID, responseBody.UserID)
		}

		if responseBody.SchoolID != school.ID {
			t.Errorf("Expected school_id %s, got %s", school.ID, responseBody.SchoolID)
		}

		// Bootstrap mode: 3 vouches required
		if !responseBody.BootstrapMode {
			t.Error("Expected bootstrap_mode to be true")
		}

		if responseBody.VouchesRequired != 3 {
			t.Errorf("Expected vouches_required 3 (bootstrap), got %d", responseBody.VouchesRequired)
		}

		if responseBody.VouchesReceived != 0 {
			t.Errorf("Expected vouches_received 0, got %d", responseBody.VouchesReceived)
		}
	})

	t.Run("normal mode requires 2 vouches", func(t *testing.T) {
		// Create a school with 3 verified admins (exits bootstrap mode)
		normalSchool := suite.createTestSchool("Normal Mode School", "CA")
		admin1 := suite.createTestUser("school_vs_admin1", models.TierPostcard)
		admin2 := suite.createTestUser("school_vs_admin2", models.TierPostcard)
		admin3 := suite.createTestUser("school_vs_admin3", models.TierPostcard)
		normalTarget := suite.createTestUser("school_vs_normal_target", models.TierUnverified)

		suite.addUserToSchool(admin1.ID, normalSchool.ID, models.SchoolVerificationStatusVerified, true)
		suite.addUserToSchool(admin2.ID, normalSchool.ID, models.SchoolVerificationStatusVerified, true)
		suite.addUserToSchool(admin3.ID, normalSchool.ID, models.SchoolVerificationStatusVerified, true)
		suite.addUserToSchool(normalTarget.ID, normalSchool.ID, models.SchoolVerificationStatusPending, false)

		defer suite.cleanup(
			[]string{admin1.ID, admin2.ID, admin3.ID, normalTarget.ID},
			[]string{normalSchool.ID},
			nil,
		)

		claims := &middleware.Claims{
			UserID:           admin1.ID,
			Email:            admin1.Email,
			VerificationTier: admin1.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+normalSchool.ID+"/vouch-status/"+normalTarget.ID+"?id="+normalSchool.ID+"&user_id="+normalTarget.ID, nil, claims)
		suite.handler.GetVouchStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.SchoolVouchStatusResponse
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if responseBody.BootstrapMode {
			t.Error("Expected bootstrap_mode to be false with 3 admins")
		}

		if responseBody.VouchesRequired != 2 {
			t.Errorf("Expected vouches_required 2 (normal), got %d", responseBody.VouchesRequired)
		}
	})
}

// =============================================================================
// ListSignalGroups Tests
// =============================================================================

func TestSchoolHandler_ListSignalGroups(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	school := suite.createTestSchool("ListSG Test School", "CA")
	verifiedUser := suite.createTestUser("school_lsg_verified", models.TierPostcard)
	pendingUser := suite.createTestUser("school_lsg_pending", models.TierUnverified)
	nonMemberUser := suite.createTestUser("school_lsg_nonmember", models.TierPostcard)

	suite.addUserToSchool(verifiedUser.ID, school.ID, models.SchoolVerificationStatusVerified, false)
	suite.addUserToSchool(pendingUser.ID, school.ID, models.SchoolVerificationStatusPending, false)

	defer suite.cleanup(
		[]string{verifiedUser.ID, pendingUser.ID, nonMemberUser.ID},
		[]string{school.ID},
		nil,
	)

	t.Run("requires authentication", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+school.ID+"/signal-groups?id="+school.ID, nil, nil)
		suite.handler.ListSignalGroups(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("pending member cannot list signal groups", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           pendingUser.ID,
			Email:            pendingUser.Email,
			VerificationTier: pendingUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+school.ID+"/signal-groups?id="+school.ID, nil, claims)
		suite.handler.ListSignalGroups(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("non-member cannot list signal groups", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           nonMemberUser.ID,
			Email:            nonMemberUser.Email,
			VerificationTier: nonMemberUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+school.ID+"/signal-groups?id="+school.ID, nil, claims)
		suite.handler.ListSignalGroups(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("verified member can list signal groups", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: verifiedUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/schools/"+school.ID+"/signal-groups?id="+school.ID, nil, claims)
		suite.handler.ListSignalGroups(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.SignalGroupListResponse
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Should be empty but not null
		if responseBody.Groups == nil {
			t.Error("Expected groups to be empty array, not nil")
		}
	})
}

// =============================================================================
// SearchDistricts Tests
// =============================================================================

func TestSchoolHandler_SearchDistricts(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	districtAlpha := suite.createTestDistrict("Alpha Unified District", "CA")
	districtBeta := suite.createTestDistrict("Beta Elementary District", "CA")
	districtGamma := suite.createTestDistrict("Gamma High School District", "NY")

	defer suite.cleanup(nil, nil, []string{districtAlpha.ID, districtBeta.ID, districtGamma.ID})

	t.Run("search returns matching districts", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts?query=District", nil, nil)
		suite.handler.SearchDistricts(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.DistrictSearchResponse
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if len(responseBody.Districts) < 3 {
			t.Errorf("Expected at least 3 districts, got %d", len(responseBody.Districts))
		}
	})

	t.Run("search with state filter", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts?query=District&state=CA", nil, nil)
		suite.handler.SearchDistricts(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.DistrictSearchResponse
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		for _, d := range responseBody.Districts {
			if d.State != "CA" {
				t.Errorf("Expected all districts in CA, got state %s", d.State)
			}
		}
	})

	t.Run("empty search returns empty array", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts?query=NonexistentXYZ", nil, nil)
		suite.handler.SearchDistricts(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.DistrictSearchResponse
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if responseBody.Districts == nil {
			t.Error("Expected empty array, not nil")
		}
	})
}

// =============================================================================
// GetDistrict Tests
// =============================================================================

func TestSchoolHandler_GetDistrict(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	district := suite.createTestDistrict("GetDistrict Test District", "CA")
	school1 := suite.createTestSchoolInDistrict("District School 1", "CA", district.ID)
	school2 := suite.createTestSchoolInDistrict("District School 2", "CA", district.ID)

	defer suite.cleanup(nil, []string{school1.ID, school2.ID}, []string{district.ID})

	t.Run("get existing district returns details", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts/"+district.ID+"?id="+district.ID, nil, nil)
		suite.handler.GetDistrict(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.DistrictWithDetails
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if responseBody.ID != district.ID {
			t.Errorf("Expected district ID %s, got %s", district.ID, responseBody.ID)
		}

		if responseBody.Name != "GetDistrict Test District" {
			t.Errorf("Expected district name 'GetDistrict Test District', got %s", responseBody.Name)
		}
	})

	t.Run("not found for non-existent district", func(t *testing.T) {
		nonExistentID := uuid.New().String()
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts/"+nonExistentID+"?id="+nonExistentID, nil, nil)
		suite.handler.GetDistrict(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("district response includes schools", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts/"+district.ID+"?id="+district.ID, nil, nil)
		suite.handler.GetDistrict(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.DistrictWithDetails
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if len(responseBody.Schools) != 2 {
			t.Errorf("Expected 2 schools in district, got %d", len(responseBody.Schools))
		}
	})
}

// =============================================================================
// ListDistrictMembers Tests
// =============================================================================

func TestSchoolHandler_ListDistrictMembers(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	district := suite.createTestDistrict("ListDistrictMembers Test District", "CA")
	schoolAlpha := suite.createTestSchoolInDistrict("Alpha DM School", "CA", district.ID)
	schoolBeta := suite.createTestSchoolInDistrict("Beta DM School", "CA", district.ID)
	verifiedUser := suite.createTestUser("school_ldm_verified", models.TierPostcard)
	pendingUser := suite.createTestUser("school_ldm_pending", models.TierUnverified)
	nonMemberUser := suite.createTestUser("school_ldm_nonmember", models.TierPostcard)
	multiSchoolUser := suite.createTestUser("school_ldm_multi", models.TierPostcard)

	suite.addUserToSchool(verifiedUser.ID, schoolAlpha.ID, models.SchoolVerificationStatusVerified, true)
	suite.addUserToSchool(pendingUser.ID, schoolBeta.ID, models.SchoolVerificationStatusPending, false)
	// multiSchoolUser in both schools - should appear once
	suite.addUserToSchool(multiSchoolUser.ID, schoolAlpha.ID, models.SchoolVerificationStatusVerified, false)
	suite.addUserToSchool(multiSchoolUser.ID, schoolBeta.ID, models.SchoolVerificationStatusPending, false)

	defer suite.cleanup(
		[]string{verifiedUser.ID, pendingUser.ID, nonMemberUser.ID, multiSchoolUser.ID},
		[]string{schoolAlpha.ID, schoolBeta.ID},
		[]string{district.ID},
	)

	t.Run("requires authentication", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts/"+district.ID+"/members?id="+district.ID, nil, nil)
		suite.handler.ListDistrictMembers(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("non-member cannot list district members", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           nonMemberUser.ID,
			Email:            nonMemberUser.Email,
			VerificationTier: nonMemberUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts/"+district.ID+"/members?id="+district.ID, nil, claims)
		suite.handler.ListDistrictMembers(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unverified member cannot list district members", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           pendingUser.ID,
			Email:            pendingUser.Email,
			VerificationTier: pendingUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts/"+district.ID+"/members?id="+district.ID, nil, claims)
		suite.handler.ListDistrictMembers(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("verified school member can list district members", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: verifiedUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts/"+district.ID+"/members?id="+district.ID, nil, claims)
		suite.handler.ListDistrictMembers(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		membersRaw, ok := responseBody["members"]
		if !ok {
			t.Fatal("Expected 'members' key in response")
		}

		membersList, ok := membersRaw.([]interface{})
		if !ok {
			t.Fatal("Expected 'members' to be an array")
		}

		// 3 unique users: verifiedUser, pendingUser, multiSchoolUser
		if len(membersList) != 3 {
			t.Errorf("Expected 3 members, got %d", len(membersList))
		}
	})

	t.Run("members include school_name field", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: verifiedUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts/"+district.ID+"/members?id="+district.ID, nil, claims)
		suite.handler.ListDistrictMembers(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody struct {
			Members []models.DistrictMember `json:"members"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		for _, member := range responseBody.Members {
			if member.SchoolName == "" {
				t.Errorf("Expected school_name to be set for member %s", member.Username)
			}
			if member.UserID == "" {
				t.Error("Expected user_id to be set")
			}
			if member.Username == "" {
				t.Error("Expected username to be set")
			}
		}
	})

	t.Run("deduplicates multi-school users", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: verifiedUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts/"+district.ID+"/members?id="+district.ID, nil, claims)
		suite.handler.ListDistrictMembers(rec, req)

		var responseBody struct {
			Members []models.DistrictMember `json:"members"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		countMultiUser := 0
		for _, member := range responseBody.Members {
			if member.UserID == multiSchoolUser.ID {
				countMultiUser++
				// Should appear under "Alpha DM School" (alphabetically first)
				if member.SchoolName != "Alpha DM School" {
					t.Errorf("Expected multi-school user school_name 'Alpha DM School', got '%s'", member.SchoolName)
				}
			}
		}
		if countMultiUser != 1 {
			t.Errorf("Expected multi-school user to appear exactly once, appeared %d times", countMultiUser)
		}
	})
}

// =============================================================================
// ListDistrictSignalGroups Tests
// =============================================================================

func TestSchoolHandler_ListDistrictSignalGroups(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	district := suite.createTestDistrict("ListDistrictSG Test District", "CA")
	school := suite.createTestSchoolInDistrict("ListDistrictSG School", "CA", district.ID)
	verifiedUser := suite.createTestUser("school_ldsg_verified", models.TierPostcard)
	nonMemberUser := suite.createTestUser("school_ldsg_nonmember", models.TierPostcard)

	suite.addUserToSchool(verifiedUser.ID, school.ID, models.SchoolVerificationStatusVerified, false)

	defer suite.cleanup(
		[]string{verifiedUser.ID, nonMemberUser.ID},
		[]string{school.ID},
		[]string{district.ID},
	)

	t.Run("requires authentication", func(t *testing.T) {
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts/"+district.ID+"/signal-groups?id="+district.ID, nil, nil)
		suite.handler.ListDistrictSignalGroups(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("non-member cannot list district signal groups", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           nonMemberUser.ID,
			Email:            nonMemberUser.Email,
			VerificationTier: nonMemberUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts/"+district.ID+"/signal-groups?id="+district.ID, nil, claims)
		suite.handler.ListDistrictSignalGroups(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("verified school member can list district signal groups", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           verifiedUser.ID,
			Email:            verifiedUser.Email,
			VerificationTier: verifiedUser.VerificationTier,
		}
		req, rec := suite.authenticatedRequest("GET", "/api/v1/school-districts/"+district.ID+"/signal-groups?id="+district.ID, nil, claims)
		suite.handler.ListDistrictSignalGroups(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody models.SignalGroupListResponse
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if responseBody.Groups == nil {
			t.Error("Expected groups to be empty array, not nil")
		}
	})
}

// =============================================================================
// CreateDistrictSignalGroup Tests
// =============================================================================

func TestSchoolHandler_CreateDistrictSignalGroup(t *testing.T) {
	suite := setupSchoolTestSuite(t)

	_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@schooltest.com'")

	district := suite.createTestDistrict("CreateDistrictSG Test District", "CA")
	school := suite.createTestSchoolInDistrict("CreateDistrictSG School", "CA", district.ID)
	adminUser := suite.createTestUser("school_cdsg_admin", models.TierPostcard)
	regularUser := suite.createTestUser("school_cdsg_regular", models.TierUnverified)
	superUser := suite.createTestUser("school_cdsg_super", models.TierPostcard)

	suite.addUserToSchool(adminUser.ID, school.ID, models.SchoolVerificationStatusVerified, true)
	suite.addUserToSchool(regularUser.ID, school.ID, models.SchoolVerificationStatusPending, false)

	defer suite.cleanup(
		[]string{adminUser.ID, regularUser.ID, superUser.ID},
		[]string{school.ID},
		[]string{district.ID},
	)

	t.Run("requires authentication", func(t *testing.T) {
		requestBody := models.CreateSchoolSignalGroupRequest{
			Name:       "Test District Group",
			InviteLink: "https://signal.group/disttest",
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/school-districts/"+district.ID+"/signal-groups?id="+district.ID, requestBody, nil)
		suite.handler.CreateDistrictSignalGroup(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("non-admin cannot create district signal group", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           regularUser.ID,
			Email:            regularUser.Email,
			VerificationTier: regularUser.VerificationTier,
		}
		requestBody := models.CreateSchoolSignalGroupRequest{
			Name:       "Unauthorized District Group",
			InviteLink: "https://signal.group/unauth",
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/school-districts/"+district.ID+"/signal-groups?id="+district.ID, requestBody, claims)
		suite.handler.CreateDistrictSignalGroup(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("admin can create district signal group", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			VerificationTier: adminUser.VerificationTier,
		}
		requestBody := models.CreateSchoolSignalGroupRequest{
			Name:       "Admin District Signal Group",
			InviteLink: "https://signal.group/admindist",
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/school-districts/"+district.ID+"/signal-groups?id="+district.ID, requestBody, claims)
		suite.handler.CreateDistrictSignalGroup(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var responseBody map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if responseBody["group_id"] == nil {
			t.Error("Expected group_id in response")
		}

		if responseBody["district_id"] != district.ID {
			t.Errorf("Expected district_id %s, got %v", district.ID, responseBody["district_id"])
		}

		// Clean up created group
		if groupID, ok := responseBody["group_id"].(string); ok {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM signal_groups WHERE id = ?", groupID)
		}
	})

	t.Run("group limit prevents creation", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           adminUser.ID,
			Email:            adminUser.Email,
			VerificationTier: adminUser.VerificationTier,
		}

		// Create 5 groups to hit the limit
		var createdGroupIDs []string
		for i := 0; i < 5; i++ {
			group := &models.SignalGroup{
				DistrictID:          &district.ID,
				GroupName:           "Limit Test District Group",
				InviteLink:          "https://signal.group/dlimit",
				InviteLinkUpdatedBy: &adminUser.ID,
				CreatedBy:           &adminUser.ID,
			}
			if err := suite.groupRepo.Create(context.Background(), group); err != nil {
				t.Fatalf("Failed to create test group: %v", err)
			}
			createdGroupIDs = append(createdGroupIDs, group.ID)
		}

		requestBody := models.CreateSchoolSignalGroupRequest{
			Name:       "Over Limit District Group",
			InviteLink: "https://signal.group/doverlimit",
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/school-districts/"+district.ID+"/signal-groups?id="+district.ID, requestBody, claims)
		suite.handler.CreateDistrictSignalGroup(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d: %s", rec.Code, rec.Body.String())
		}

		// Clean up
		for _, groupID := range createdGroupIDs {
			_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM signal_groups WHERE id = ?", groupID)
		}
	})

	t.Run("superuser can create regardless of membership", func(t *testing.T) {
		claims := &middleware.Claims{
			UserID:           superUser.ID,
			Email:            superUser.Email,
			VerificationTier: superUser.VerificationTier,
			IsSuperuser:      true,
		}
		requestBody := models.CreateSchoolSignalGroupRequest{
			Name:       "Superuser District Group",
			InviteLink: "https://signal.group/superdist",
		}
		req, rec := suite.authenticatedRequest("POST", "/api/v1/school-districts/"+district.ID+"/signal-groups?id="+district.ID, requestBody, claims)
		suite.handler.CreateDistrictSignalGroup(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		// Clean up created group
		var responseBody map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&responseBody); err == nil {
			if groupID, ok := responseBody["group_id"].(string); ok {
				_, _ = suite.db.ExecContext(context.Background(), "DELETE FROM signal_groups WHERE id = ?", groupID)
			}
		}
	})
}
