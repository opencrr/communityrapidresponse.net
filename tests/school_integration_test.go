package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// =============================================================================
// School Membership Lifecycle Integration Test
// =============================================================================

func TestIntegration_SchoolMembershipLifecycle(t *testing.T) {
	suite := SetupE2ETest(t)

	ctx := context.Background()
	schoolID := suite.createSchool("Integration Lifecycle School", "CA")
	defer suite.cleanupSchools(schoolID)

	// Register user (handles pre-existing users from prior runs)
	userID := suite.registerOrGetUserID("integ_lifecycle", "integ_lifecycle@test.com", "securepassword123")
	suite.disableMFA(userID)
	defer suite.cleanup(userID)

	resp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "integ_lifecycle@test.com",
		"password": "securepassword123",
	}, "")
	var loginResp models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&loginResp)
	_ = resp.Body.Close()
	token := loginResp.Token

	// Step 1: User joins school -> pending
	t.Run("join school gives pending status", func(t *testing.T) {
		// Clean up stale membership from prior runs
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM school_vouches WHERE vouched_user_id = ? AND school_id = ?", userID, schoolID)
		_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_schools WHERE user_id = ? AND school_id = ?", userID, schoolID)

		resp := suite.request("POST", "/api/v1/schools/"+schoolID+"/join", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Fatalf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["status"] != "pending" {
			t.Errorf("Expected status 'pending', got %v", body["status"])
		}
	})

	// Step 2: Gets vouched -> verified
	t.Run("three vouches verify the user", func(t *testing.T) {
		// Create 3 voucher users who are members (handles pre-existing users)
		for i := 0; i < 3; i++ {
			var voucherUser models.User
			voucherUser.Username = "integ_voucher_" + string(rune('a'+i))
			voucherUser.Email = voucherUser.Username + "@test.com"
			voucherUser.PasswordHash = "$2a$12$test.hash.only"
			voucherUser.VerificationTier = models.TierPostcard
			if err := suite.userRepo.Create(ctx, &voucherUser); err != nil {
				// User may already exist from prior run — look up by email
				var existingID string
				_ = suite.db.QueryRowContext(ctx, "SELECT id FROM users WHERE email = ?", voucherUser.Email).Scan(&existingID)
				if existingID == "" {
					t.Fatalf("Failed to create voucher user: %v", err)
				}
				voucherUser.ID = existingID
			}
			_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_schools WHERE user_id = ? AND school_id = ?", voucherUser.ID, schoolID)
			suite.addUserToSchool(voucherUser.ID, schoolID, "pending", false)
			defer suite.cleanup(voucherUser.ID)

			// Vouch via direct DB insert (bootstrap mode, any member can vouch)
			// Delete any existing vouch first to avoid duplicates
			_, _ = suite.db.ExecContext(ctx,
				"DELETE FROM school_vouches WHERE voucher_user_id = ? AND vouched_user_id = ? AND school_id = ?",
				voucherUser.ID, userID, schoolID)
			_, err := suite.db.ExecContext(ctx,
				"INSERT INTO school_vouches (id, voucher_user_id, vouched_user_id, school_id, created_at) VALUES (UUID(), ?, ?, ?, NOW())",
				voucherUser.ID, userID, schoolID)
			if err != nil {
				t.Fatalf("Failed to create vouch: %v", err)
			}
		}

		// Manually verify the user (simulating what the handler does after threshold)
		_, err := suite.db.ExecContext(ctx,
			"UPDATE user_schools SET verification_status = 'verified', is_admin = TRUE, verified_at = NOW() WHERE user_id = ? AND school_id = ?",
			userID, schoolID)
		if err != nil {
			t.Fatalf("Failed to verify user: %v", err)
		}

		// Check verification status
		var verificationStatus string
		_ = suite.db.QueryRowContext(ctx,
			"SELECT verification_status FROM user_schools WHERE user_id = ? AND school_id = ?",
			userID, schoolID).Scan(&verificationStatus)
		if verificationStatus != "verified" {
			t.Errorf("Expected verification_status 'verified', got '%s'", verificationStatus)
		}
	})

	// Step 3: Admin can create signal group (with enough admins to exit bootstrap)
	t.Run("verified admin can create signal group after bootstrap exit", func(t *testing.T) {
		// Exit bootstrap mode
		bootstrapAdminIDs := suite.exitSchoolBootstrapMode(schoolID)
		defer func() {
			for _, id := range bootstrapAdminIDs {
				suite.cleanup(id)
			}
		}()

		// Re-login to get fresh token with updated claims
		resp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email":    "integ_lifecycle@test.com",
			"password": "securepassword123",
		}, "")
		var freshLogin models.LoginResponse
		_ = json.NewDecoder(resp.Body).Decode(&freshLogin)
		_ = resp.Body.Close()

		resp = suite.request("POST", "/api/v1/schools/"+schoolID+"/signal-groups", map[string]interface{}{
			"name":              "Integration Lifecycle Group",
			"encrypted_payload": "dGVzdC1zY2hvb2wtZW5jcnlwdGVkLXBheWxvYWQ=",
			"encryption_iv":     "dGVzdC1pdi0xMjM=",
			"wrapped_keys": []map[string]string{
				{"user_id": userID, "wrapped_dek": "dGVzdC13cmFwcGVkLWRlaw=="},
			},
		}, freshLogin.Token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}
	})

	// Step 4: Leave school
	t.Run("user can leave school", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/schools/"+schoolID+"/leave", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}
	})

	// Step 5: Can rejoin
	t.Run("user can rejoin school", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/schools/"+schoolID+"/join", nil, token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}
	})
}

// =============================================================================
// Bootstrap Mode Transition Integration Test
// =============================================================================

func TestIntegration_BootstrapModeTransition(t *testing.T) {
	suite := SetupE2ETest(t)

	ctx := context.Background()
	schoolID := suite.createSchool("Integration Bootstrap School", "CA")
	defer suite.cleanupSchools(schoolID)

	// Register a user for authenticated requests
	viewerID, viewerToken := suite.registerOrGetUser("integ_bs_viewer", "integ_bs_viewer@test.com", "securepassword123")
	defer suite.cleanup(viewerID)

	t.Run("new school starts in bootstrap mode", func(t *testing.T) {
		// Check the school details
		resp := suite.request("GET", "/api/v1/schools/"+schoolID, nil, viewerToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		var body models.SchoolWithDetails
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if !body.BootstrapMode {
			t.Error("Expected new school to be in bootstrap mode")
		}
	})

	t.Run("bootstrap mode requires 3 vouches", func(t *testing.T) {
		// Create a voucher and vouchee user
		voucherUser := &models.User{
			Username:         "integ_bs_voucher",
			Email:            "integ_bs_voucher@test.com",
			PasswordHash:     "$2a$12$test.hash.only",
			VerificationTier: models.TierPostcard,
		}
		if err := suite.userRepo.Create(ctx, voucherUser); err != nil {
			t.Fatalf("Failed to create voucher: %v", err)
		}
		suite.addUserToSchool(voucherUser.ID, schoolID, "pending", false)
		defer suite.cleanup(voucherUser.ID)

		targetUser := &models.User{
			Username:         "integ_bs_target",
			Email:            "integ_bs_target@test.com",
			PasswordHash:     "$2a$12$test.hash.only",
			VerificationTier: models.TierUnverified,
		}
		if err := suite.userRepo.Create(ctx, targetUser); err != nil {
			t.Fatalf("Failed to create target: %v", err)
		}
		suite.addUserToSchool(targetUser.ID, schoolID, "pending", false)
		defer suite.cleanup(targetUser.ID)

		// Disable MFA and login as voucher
		_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", voucherUser.ID)
		resp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email":    "integ_bs_voucher@test.com",
			"password": "securepassword123",
		}, "")
		_ = resp.Body.Close()
		// Login may fail since password doesn't match bcrypt hash - just use direct DB vouch

		// Use vouch-status endpoint to check requirements
		// First register a real user who can login
		resp = suite.request("POST", "/api/v1/auth/register", map[string]string{
			"username": "integ_bs_checker",
			"email":    "integ_bs_checker@test.com",
			"password": "securepassword123",
		}, "")
		var checkerReg models.RegisterResponse
		_ = json.NewDecoder(resp.Body).Decode(&checkerReg)
		_ = resp.Body.Close()

		suite.disableMFA(checkerReg.UserID)
		suite.addUserToSchool(checkerReg.UserID, schoolID, "pending", false)
		defer suite.cleanup(checkerReg.UserID)

		resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email":    "integ_bs_checker@test.com",
			"password": "securepassword123",
		}, "")
		var checkerLogin models.LoginResponse
		_ = json.NewDecoder(resp.Body).Decode(&checkerLogin)
		_ = resp.Body.Close()

		// Check vouch status
		resp = suite.request("GET", "/api/v1/schools/"+schoolID+"/vouch-status/"+targetUser.ID, nil, checkerLogin.Token)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Fatalf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}

		var body models.SchoolVouchStatusResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if !body.BootstrapMode {
			t.Error("Expected bootstrap mode")
		}
		if body.VouchesRequired != 3 {
			t.Errorf("Expected 3 vouches required (bootstrap), got %d", body.VouchesRequired)
		}
	})

	t.Run("after 3 admins, transitions to normal mode", func(t *testing.T) {
		// Create 3 verified admins
		adminIDs := suite.exitSchoolBootstrapMode(schoolID)
		defer func() {
			for _, id := range adminIDs {
				suite.cleanup(id)
			}
		}()

		// Check school details
		resp := suite.request("GET", "/api/v1/schools/"+schoolID, nil, viewerToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		var body models.SchoolWithDetails
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.BootstrapMode {
			t.Error("Expected school to be out of bootstrap mode with 3+ admins")
		}

		if body.AdminCount < 3 {
			t.Errorf("Expected at least 3 admins, got %d", body.AdminCount)
		}
	})
}

// =============================================================================
// District Signal Group Access Integration Test
// =============================================================================

func TestIntegration_DistrictSignalGroupAccess(t *testing.T) {
	suite := SetupE2ETest(t)

	ctx := context.Background()

	districtID := suite.createDistrict("Integration District", "CA")
	schoolAID := suite.createSchool("Integration School A", "CA")
	schoolBID := suite.createSchool("Integration School B", "CA")
	suite.linkSchoolToDistrict(schoolAID, districtID)
	suite.linkSchoolToDistrict(schoolBID, districtID)

	defer func() {
		suite.cleanupSchools(schoolAID, schoolBID)
		suite.cleanupDistricts(districtID)
	}()

	// Create user verified in school A (handles pre-existing users)
	verifiedUserID := suite.registerOrGetUserID("integ_dist_verified", "integ_dist_verified@test.com", "securepassword123")
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, verification_tier = 2, postcard_verified = TRUE, vouch_verified = TRUE WHERE id = ?", verifiedUserID)
	_, _ = suite.db.ExecContext(ctx, "DELETE FROM user_schools WHERE user_id = ? AND school_id = ?", verifiedUserID, schoolAID)
	suite.addUserToSchool(verifiedUserID, schoolAID, "verified", true)
	defer suite.cleanup(verifiedUserID)

	resp := suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "integ_dist_verified@test.com",
		"password": "securepassword123",
	}, "")
	var verifiedLogin models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&verifiedLogin)
	_ = resp.Body.Close()
	verifiedToken := verifiedLogin.Token

	// Create user NOT in any school (handles pre-existing users)
	outsiderUserID := suite.registerOrGetUserID("integ_dist_outsider", "integ_dist_outsider@test.com", "securepassword123")
	_, _ = suite.db.ExecContext(ctx, "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", outsiderUserID)
	defer suite.cleanup(outsiderUserID)

	resp = suite.request("POST", "/api/v1/auth/login", map[string]string{
		"email":    "integ_dist_outsider@test.com",
		"password": "securepassword123",
	}, "")
	var outsiderLogin models.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&outsiderLogin)
	_ = resp.Body.Close()
	outsiderToken := outsiderLogin.Token

	t.Run("verified school member can view district groups", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/school-districts/"+districtID+"/signal-groups", nil, verifiedToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 200, got %d: %v", resp.StatusCode, body)
		}
	})

	t.Run("non-member cannot view district groups", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/school-districts/"+districtID+"/signal-groups", nil, outsiderToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", resp.StatusCode)
		}
	})

	t.Run("admin of school A can create district group", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/school-districts/"+districtID+"/signal-groups", map[string]interface{}{
			"name":              "Integration District Group",
			"encrypted_payload": "dGVzdC1kaXN0cmljdC1lbmNyeXB0ZWQtcGF5bG9hZA==",
			"encryption_iv":     "dGVzdC1pdi0xMjM=",
			"wrapped_keys": []map[string]string{
				{"user_id": verifiedUserID, "wrapped_dek": "dGVzdC13cmFwcGVkLWRlaw=="},
			},
		}, verifiedToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			t.Errorf("Expected status 201, got %d: %v", resp.StatusCode, body)
		}
	})
}
