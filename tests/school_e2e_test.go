package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// =============================================================================
// School Search E2E Tests
// =============================================================================

func TestE2E_SchoolSearch(t *testing.T) {
	suite := SetupE2ETest(t)

	schoolA := suite.createSchool("E2E Alpha Elementary", "CA")
	schoolB := suite.createSchool("E2E Beta Middle School", "CA")
	schoolC := suite.createSchool("E2E Gamma High School", "NY")

	defer suite.cleanupSchools(schoolA, schoolB, schoolC)

	t.Run("search by name", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/schools?query=E2E", nil, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var body models.SchoolSearchResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.Total < 3 {
			t.Errorf("Expected at least 3 results, got %d", body.Total)
		}
	})

	t.Run("search with state filter", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/schools?query=E2E&state=CA", nil, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var body models.SchoolSearchResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		for _, s := range body.Schools {
			if s.State != "CA" {
				t.Errorf("Expected state CA, got %s", s.State)
			}
		}
	})

	t.Run("search with pagination", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/schools?query=E2E&limit=1&page=1", nil, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var body models.SchoolSearchResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if len(body.Schools) != 1 {
			t.Errorf("Expected 1 result per page, got %d", len(body.Schools))
		}

		if !body.HasMore {
			t.Error("Expected has_more to be true")
		}
	})

	t.Run("empty search returns empty array", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/schools?query=NonexistentSchoolXYZ999", nil, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var body models.SchoolSearchResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.Schools == nil {
			t.Error("Expected empty array, not nil")
		}
	})
}

// =============================================================================
// School Join and Leave E2E Tests
// =============================================================================

func TestE2E_SchoolJoinAndLeave(t *testing.T) {
	suite := SetupE2ETest(t)

	schoolID := suite.createSchool("E2E Join Leave School", "CA")
	defer suite.cleanupSchools(schoolID)

	// Register user
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "e2eschooluser",
		"email":    "e2eschooluser@test.com",
		"password": "securepassword123",
	}, "")
	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	_ = resp.Body.Close()

	suite.disableMFA(registerResp.UserID)
	defer suite.cleanup(registerResp.UserID)

	// Login
	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "e2eschooluser@test.com",
		"password": "securepassword123",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token := loginResp.Token

	t.Run("join school", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/schools/"+schoolID+"/join", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["status"] != "pending" {
			t.Errorf("Expected status 'pending', got %v", body["status"])
		}
	})

	t.Run("join same school again returns conflict", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/schools/"+schoolID+"/join", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusConflict {
			t.Errorf("Expected status 409, got %d", resp.StatusCode)
		}
	})

	t.Run("leave school", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/schools/"+schoolID+"/leave", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}
	})

	t.Run("leave school not a member of is idempotent", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/schools/"+schoolID+"/leave", nil, token)
		defer func() { _ = resp.Body.Close() }()

		// Leave is idempotent - returns 200 even if not a member
		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200 (idempotent leave), got %d: %v", resp.StatusCode, body)
		}
	})
}

// =============================================================================
// School Vouch Flow E2E Tests
// =============================================================================

func TestE2E_SchoolVouchFlow(t *testing.T) {
	suite := SetupE2ETest(t)

	ctx := context.Background()
	schoolID := suite.createSchool("E2E Vouch Flow School", "CA")
	defer suite.cleanupSchools(schoolID)

	// Create 4 users: 3 vouchers + 1 vouchee
	var userIDs []string
	var tokens []string

	for i, name := range []string{"e2e_sv_a", "e2e_sv_b", "e2e_sv_c", "e2e_sv_target"} {
		resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
			"username": name,
			"email":    name + "@test.com",
			"password": "securepassword123",
		}, "")
		var reg models.RegisterResponse
		_ = json.NewDecoder(resp.Body).Decode(&reg)
		_ = resp.Body.Close()

		suite.disableMFA(reg.UserID)
		userIDs = append(userIDs, reg.UserID)

		resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email":    name + "@test.com",
			"password": "securepassword123",
		}, "")
		var login models.LoginResponse
		_ = json.NewDecoder(resp.Body).Decode(&login)
		_ = resp.Body.Close()
		tokens = append(tokens, login.Token)

		// All users join the school
		resp = suite.request("POST", "/api/v1/schools/"+schoolID+"/join", nil, login.Token)
		_ = resp.Body.Close()

		_ = i
	}

	defer func() {
		for _, id := range userIDs {
			suite.cleanup(id)
		}
	}()

	targetEmail := "e2e_sv_target@test.com"

	t.Run("first vouch in bootstrap mode", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/schools/"+schoolID+"/vouch", map[string]string{
			"user_identifier": targetEmail,
		}, tokens[0])
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Fatalf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}

		var body models.SchoolVouchResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if !body.BootstrapMode {
			t.Error("Expected bootstrap_mode true")
		}
		if body.TotalVouches != 1 {
			t.Errorf("Expected 1 total vouch, got %d", body.TotalVouches)
		}
		if body.VouchesRequired != 3 {
			t.Errorf("Expected 3 vouches required (bootstrap), got %d", body.VouchesRequired)
		}
	})

	t.Run("second vouch", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/schools/"+schoolID+"/vouch", map[string]string{
			"user_identifier": targetEmail,
		}, tokens[1])
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Fatalf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}

		var body models.SchoolVouchResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.TotalVouches != 2 {
			t.Errorf("Expected 2 total vouches, got %d", body.TotalVouches)
		}
	})

	t.Run("third vouch triggers verification", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/schools/"+schoolID+"/vouch", map[string]string{
			"user_identifier": targetEmail,
		}, tokens[2])
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Fatalf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}

		var body models.SchoolVouchResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.TotalVouches != 3 {
			t.Errorf("Expected 3 total vouches, got %d", body.TotalVouches)
		}
		if body.VouchesNeeded != 0 {
			t.Errorf("Expected 0 vouches needed, got %d", body.VouchesNeeded)
		}
	})

	t.Run("verify target user is now verified", func(t *testing.T) {
		var verificationStatus string
		err := suite.db.QueryRowContext(ctx,
			"SELECT verification_status FROM user_schools WHERE user_id = ? AND school_id = ?",
			userIDs[3], schoolID).Scan(&verificationStatus)
		if err != nil {
			t.Fatalf("Failed to query verification status: %v", err)
		}
		if verificationStatus != "verified" {
			t.Errorf("Expected verification_status 'verified', got '%s'", verificationStatus)
		}
	})
}

// =============================================================================
// School Signal Group Flow E2E Tests
// =============================================================================

func TestE2E_SchoolSignalGroupFlow(t *testing.T) {
	suite := SetupE2ETest(t)

	schoolID := suite.createSchool("E2E SG Flow School", "CA")
	defer suite.cleanupSchools(schoolID)

	// Exit bootstrap mode with 3 verified admins
	adminIDs := suite.exitSchoolBootstrapMode(schoolID)
	defer func() {
		for _, id := range adminIDs {
			suite.cleanup(id)
		}
	}()

	// Create an admin user with login
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "e2e_sg_admin",
		"email":    "e2e_sg_admin@test.com",
		"password": "securepassword123",
	}, "")
	var registerResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&registerResp)
	_ = resp.Body.Close()

	ctx := context.Background()
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, verification_tier = 3, postcard_verified = TRUE, vouch_verified = TRUE WHERE id = ?", registerResp.UserID)
	suite.addUserToSchool(registerResp.UserID, schoolID, "verified", true)
	defer suite.cleanup(registerResp.UserID)

	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "e2e_sg_admin@test.com",
		"password": "securepassword123",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	adminToken := loginResp.Token

	// Create a verified member user
	resp = suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "e2e_sg_member",
		"email":    "e2e_sg_member@test.com",
		"password": "securepassword123",
	}, "")
	var memberRegResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&memberRegResp)
	_ = resp.Body.Close()

	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", memberRegResp.UserID)
	suite.addUserToSchool(memberRegResp.UserID, schoolID, "verified", false)
	defer suite.cleanup(memberRegResp.UserID)

	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "e2e_sg_member@test.com",
		"password": "securepassword123",
	}, "")
	var memberLoginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&memberLoginResp)
	_ = resp.Body.Close()
	memberToken := memberLoginResp.Token

	// Create an unverified member
	resp = suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "e2e_sg_pending",
		"email":    "e2e_sg_pending@test.com",
		"password": "securepassword123",
	}, "")
	var pendingRegResp models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&pendingRegResp)
	_ = resp.Body.Close()

	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", pendingRegResp.UserID)
	suite.addUserToSchool(pendingRegResp.UserID, schoolID, "pending", false)
	defer suite.cleanup(pendingRegResp.UserID)

	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "e2e_sg_pending@test.com",
		"password": "securepassword123",
	}, "")
	var pendingLoginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&pendingLoginResp)
	_ = resp.Body.Close()
	pendingToken := pendingLoginResp.Token

	t.Run("admin creates signal group", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/schools/"+schoolID+"/signal-groups", map[string]interface{}{
			"name":        "E2E School Signal Group",
			"invite_link": "https://signal.group/e2e123",
		}, adminToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}
	})

	t.Run("verified member can list signal groups", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/schools/"+schoolID+"/signal-groups", nil, memberToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}

		var body models.SignalGroupListResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if len(body.Groups) < 1 {
			t.Error("Expected at least 1 signal group")
		}
	})

	t.Run("non-verified member cannot list signal groups", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/schools/"+schoolID+"/signal-groups", nil, pendingToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", resp.StatusCode)
		}
	})
}

// =============================================================================
// District Search and Detail E2E Tests
// =============================================================================

func TestE2E_DistrictSearchAndDetail(t *testing.T) {
	suite := SetupE2ETest(t)

	ctx := context.Background()

	districtID := suite.createDistrict("E2E Test Unified District", "CA")
	schoolID := suite.createSchool("E2E District School", "CA")
	suite.linkSchoolToDistrict(schoolID, districtID)

	defer func() {
		suite.cleanupSchools(schoolID)
		suite.cleanupDistricts(districtID)
	}()

	t.Run("search districts by name", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/school-districts?query=E2E+Test+Unified", nil, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var body models.DistrictSearchResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if len(body.Districts) < 1 {
			t.Error("Expected at least 1 district in results")
		}
	})

	t.Run("get district details includes schools", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/school-districts/"+districtID, nil, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var body models.DistrictWithDetails
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.ID != districtID {
			t.Errorf("Expected district ID %s, got %s", districtID, body.ID)
		}

		if len(body.Schools) != 1 {
			t.Errorf("Expected 1 school in district, got %d", len(body.Schools))
		}
	})

	t.Run("verified member can list district signal groups", func(t *testing.T) {
		// Create a user with verified school membership
		resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
			"username": "e2edistmember",
			"email":    "e2edistmember@test.com",
			"password": "securepassword123",
		}, "")
		var reg models.RegisterResponse
		_ = json.NewDecoder(resp.Body).Decode(&reg)
		_ = resp.Body.Close()

		_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", reg.UserID)
		suite.addUserToSchool(reg.UserID, schoolID, "verified", false)
		defer suite.cleanup(reg.UserID)

		resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email":    "e2edistmember@test.com",
			"password": "securepassword123",
		}, "")
		var login models.LoginResponse
		_ = json.NewDecoder(resp.Body).Decode(&login)
		_ = resp.Body.Close()

		resp = suite.request("GET", "/api/v1/school-districts/"+districtID+"/signal-groups", nil, login.Token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}
	})

	t.Run("admin creates district signal group", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
			"username": "e2edistadmin",
			"email":    "e2edistadmin@test.com",
			"password": "securepassword123",
		}, "")
		var reg models.RegisterResponse
		_ = json.NewDecoder(resp.Body).Decode(&reg)
		_ = resp.Body.Close()

		_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, verification_tier = 3, postcard_verified = TRUE, vouch_verified = TRUE WHERE id = ?", reg.UserID)
		suite.addUserToSchool(reg.UserID, schoolID, "verified", true)
		defer suite.cleanup(reg.UserID)

		resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email":    "e2edistadmin@test.com",
			"password": "securepassword123",
		}, "")
		var login models.LoginResponse
		_ = json.NewDecoder(resp.Body).Decode(&login)
		_ = resp.Body.Close()

		resp = suite.request("POST", "/api/v1/school-districts/"+districtID+"/signal-groups", map[string]interface{}{
			"name":        "E2E District Signal Group",
			"invite_link": "https://signal.group/e2edist",
		}, login.Token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}
	})
}
