package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

func testMFAService(t *testing.T) *services.MFAService {
	cfg := &config.MFAConfig{
		EncryptionKey: "01234567890123456789012345678901",
		Issuer:        "Test MFA",
	}
	service, err := services.NewMFAService(cfg)
	if err != nil {
		t.Fatalf("Failed to create MFA service: %v", err)
	}
	return service
}

func createTestUserForMFA(t *testing.T, db *database.DB, userRepo *database.UserRepository, suffix string) *models.User {
	user := &models.User{
		Username:         "mfatest" + suffix,
		Email:            "mfa" + suffix + "@test.com",
		PasswordHash:     "$2a$12$test", // Dummy hash
		VerificationTier: models.TierUnverified,
		MFASetupRequired: true,
		MFAEnabled:       false,
	}

	err := userRepo.Create(context.Background(), user)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", user.ID)
	})

	return user
}

func TestMFAHandler_InitSetup(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	mfaService := testMFAService(t)
	handler := NewMFAHandler(nil, userRepo, mfaService, jwtAuth, false, nil)

	user := createTestUserForMFA(t, db, userRepo, "init")

	t.Run("returns QR code and secret", func(t *testing.T) {
		// Generate MFA setup token
		token, err := jwtAuth.GenerateTokenWithType(user, middleware.TokenTypeMFASetup)
		if err != nil {
			t.Fatalf("Failed to generate token: %v", err)
		}

		req := httptest.NewRequest("POST", "/api/v1/mfa/setup", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		// Apply auth middleware
		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.InitSetup), middleware.TokenTypeMFASetup).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp SetupMFAResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if resp.QRCode == "" {
			t.Error("Expected QR code in response")
		}
		if resp.Secret == "" {
			t.Error("Expected secret in response")
		}

		// Verify secret was stored in database
		updatedUser, _ := userRepo.GetByID(context.Background(), user.ID)
		if updatedUser.MFASecret == nil || *updatedUser.MFASecret == "" {
			t.Error("Expected MFA secret to be stored in database")
		}
	})

	t.Run("rejects full token", func(t *testing.T) {
		token, _ := jwtAuth.GenerateToken(user) // Full token, not MFA setup

		req := httptest.NewRequest("POST", "/api/v1/mfa/setup", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.InitSetup), middleware.TokenTypeMFASetup).ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/mfa/setup", nil)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.InitSetup), middleware.TokenTypeMFASetup).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})
}

func TestMFAHandler_CompleteSetup(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	mfaService := testMFAService(t)
	handler := NewMFAHandler(nil, userRepo, mfaService, jwtAuth, false, nil)

	user := createTestUserForMFA(t, db, userRepo, "complete")

	// First, initialize MFA setup
	setupToken, _ := jwtAuth.GenerateTokenWithType(user, middleware.TokenTypeMFASetup)

	initReq := httptest.NewRequest("POST", "/api/v1/mfa/setup", nil)
	initReq.Header.Set("Authorization", "Bearer "+setupToken)
	initRec := httptest.NewRecorder()
	jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.InitSetup), middleware.TokenTypeMFASetup).ServeHTTP(initRec, initReq)

	var setupResp SetupMFAResponse
	_ = json.NewDecoder(initRec.Body).Decode(&setupResp)

	t.Run("rejects invalid TOTP code", func(t *testing.T) {
		body := map[string]string{"code": "000000"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/mfa/setup/complete", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+setupToken)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.CompleteSetup), middleware.TokenTypeMFASetup).ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects empty code", func(t *testing.T) {
		body := map[string]string{"code": ""}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/mfa/setup/complete", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+setupToken)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.CompleteSetup), middleware.TokenTypeMFASetup).ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("rejects without MFA being initialized", func(t *testing.T) {
		// Create a new user without MFA setup
		newUser := createTestUserForMFA(t, db, userRepo, "noinit")
		newToken, _ := jwtAuth.GenerateTokenWithType(newUser, middleware.TokenTypeMFASetup)

		body := map[string]string{"code": "123456"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/mfa/setup/complete", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+newToken)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.CompleteSetup), middleware.TokenTypeMFASetup).ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestMFAHandler_Verify(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	mfaService := testMFAService(t)
	handler := NewMFAHandler(nil, userRepo, mfaService, jwtAuth, false, nil)

	// Create user with MFA enabled
	user := createTestUserForMFA(t, db, userRepo, "verify")

	// Setup MFA for the user
	key, _ := mfaService.GenerateSecret(user.Email)
	encryptedSecret, _ := mfaService.EncryptSecret(key.Secret())
	backupCodes, _ := mfaService.GenerateBackupCodes(10)
	hashedCodes, _ := mfaService.HashBackupCodes(backupCodes)

	_ = userRepo.SetMFASecret(context.Background(), user.ID, encryptedSecret)
	_ = userRepo.EnableMFA(context.Background(), user.ID, hashedCodes)

	// Update local user object
	user.MFAEnabled = true
	user.MFASetupRequired = false

	t.Run("rejects invalid TOTP code", func(t *testing.T) {
		pendingToken, _ := jwtAuth.GenerateTokenWithType(user, middleware.TokenTypePendingMFA)

		body := map[string]string{"code": "000000"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/mfa/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+pendingToken)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.Verify), middleware.TokenTypePendingMFA).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("accepts valid backup code", func(t *testing.T) {
		pendingToken, _ := jwtAuth.GenerateTokenWithType(user, middleware.TokenTypePendingMFA)

		body := map[string]string{"backup_code": backupCodes[0]}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/mfa/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+pendingToken)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.Verify), middleware.TokenTypePendingMFA).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp["token"] == nil || resp["token"] == "" {
			t.Error("Expected full token in response")
		}

		if resp["user"] == nil {
			t.Error("Expected user in response")
		}

		// Verify backup code was consumed (count decreased)
		if resp["backup_codes_count"] != nil {
			backupCount := int(resp["backup_codes_count"].(float64))
			if backupCount != 9 {
				t.Errorf("Expected 9 remaining backup codes, got %d", backupCount)
			}
		}
	})

	t.Run("rejects invalid backup code", func(t *testing.T) {
		pendingToken, _ := jwtAuth.GenerateTokenWithType(user, middleware.TokenTypePendingMFA)

		body := map[string]string{"backup_code": "XXXX-YYYY"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/mfa/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+pendingToken)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.Verify), middleware.TokenTypePendingMFA).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})

	t.Run("rejects already used backup code", func(t *testing.T) {
		pendingToken, _ := jwtAuth.GenerateTokenWithType(user, middleware.TokenTypePendingMFA)

		// backupCodes[0] was already used in the earlier test
		body := map[string]string{"backup_code": backupCodes[0]}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/mfa/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+pendingToken)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.Verify), middleware.TokenTypePendingMFA).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401 for used backup code, got %d", rec.Code)
		}
	})

	t.Run("rejects empty request", func(t *testing.T) {
		pendingToken, _ := jwtAuth.GenerateTokenWithType(user, middleware.TokenTypePendingMFA)

		body := map[string]string{}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/mfa/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+pendingToken)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.Verify), middleware.TokenTypePendingMFA).ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("rejects full token", func(t *testing.T) {
		fullToken, _ := jwtAuth.GenerateToken(user)

		body := map[string]string{"code": "123456"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/mfa/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+fullToken)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.Verify), middleware.TokenTypePendingMFA).ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})
}

func TestMFAHandler_VerifyUserWithMFADisabled(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	mfaService := testMFAService(t)
	handler := NewMFAHandler(nil, userRepo, mfaService, jwtAuth, false, nil)

	// Create user without MFA enabled
	user := createTestUserForMFA(t, db, userRepo, "nomfa")

	t.Run("rejects verify for user without MFA", func(t *testing.T) {
		pendingToken, _ := jwtAuth.GenerateTokenWithType(user, middleware.TokenTypePendingMFA)

		body := map[string]string{"code": "123456"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/mfa/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+pendingToken)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.Verify), middleware.TokenTypePendingMFA).ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// setupMFAEnabledUser creates a user with MFA fully enabled and returns the user, handler, and pending MFA token helper
func setupMFAEnabledUser(t *testing.T, suffix string) (*database.DB, *database.UserRepository, *middleware.JWTAuth, *MFAHandler, *models.User) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	mfaService := testMFAService(t)
	auditRepo := database.NewAuditRepository(db)
	handler := NewMFAHandler(nil, userRepo, mfaService, jwtAuth, false, auditRepo)

	user := createTestUserForMFA(t, db, userRepo, suffix)

	key, _ := mfaService.GenerateSecret(user.Email)
	encryptedSecret, _ := mfaService.EncryptSecret(key.Secret())
	backupCodes, _ := mfaService.GenerateBackupCodes(10)
	hashedCodes, _ := mfaService.HashBackupCodes(backupCodes)

	_ = userRepo.SetMFASecret(context.Background(), user.ID, encryptedSecret)
	_ = userRepo.EnableMFA(context.Background(), user.ID, hashedCodes)

	user.MFAEnabled = true
	user.MFASetupRequired = false

	return db, userRepo, jwtAuth, handler, user
}

func TestMFAHandler_BruteForceProtection(t *testing.T) {
	_, userRepo, jwtAuth, handler, user := setupMFAEnabledUser(t, "brute")

	// Reset MFA attempts before test
	_ = userRepo.ResetFailedMFAAttempts(context.Background(), user.ID)

	t.Run("failed TOTP attempts return remaining_attempts", func(t *testing.T) {
		// Reset for clean state
		_ = userRepo.ResetFailedMFAAttempts(context.Background(), user.ID)

		pendingToken, _ := jwtAuth.GenerateTokenWithType(user, middleware.TokenTypePendingMFA)

		body := map[string]string{"code": "000000"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/mfa/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+pendingToken)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.Verify), middleware.TokenTypePendingMFA).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		remaining, ok := resp["remaining_attempts"]
		if !ok {
			t.Error("Expected remaining_attempts in response")
		} else if int(remaining.(float64)) != 4 {
			t.Errorf("Expected 4 remaining attempts, got %v", remaining)
		}
	})

	t.Run("5 failed attempts locks out with 429", func(t *testing.T) {
		// Reset and exhaust attempts
		_ = userRepo.ResetFailedMFAAttempts(context.Background(), user.ID)

		for i := 0; i < 5; i++ {
			pendingToken, _ := jwtAuth.GenerateTokenWithType(user, middleware.TokenTypePendingMFA)
			body := map[string]string{"code": "000000"}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest("POST", "/api/v1/mfa/verify", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+pendingToken)
			rec := httptest.NewRecorder()

			jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.Verify), middleware.TokenTypePendingMFA).ServeHTTP(rec, req)

			if i < 4 {
				if rec.Code != http.StatusUnauthorized {
					t.Errorf("Attempt %d: expected 401, got %d", i+1, rec.Code)
				}
			} else {
				// 5th attempt should return 429
				if rec.Code != http.StatusTooManyRequests {
					t.Errorf("Attempt %d: expected 429, got %d: %s", i+1, rec.Code, rec.Body.String())
				}
				var resp map[string]interface{}
				_ = json.NewDecoder(rec.Body).Decode(&resp)
				if resp["error"] != "mfa_locked" {
					t.Errorf("Expected error 'mfa_locked', got '%v'", resp["error"])
				}
				if int(resp["remaining_attempts"].(float64)) != 0 {
					t.Errorf("Expected 0 remaining attempts, got %v", resp["remaining_attempts"])
				}
			}
		}
	})

	t.Run("already locked user gets 429 before validation", func(t *testing.T) {
		// Set counter to threshold via DB
		_ = userRepo.ResetFailedMFAAttempts(context.Background(), user.ID)
		for i := 0; i < 5; i++ {
			_, _ = userRepo.IncrementFailedMFAAttempts(context.Background(), user.ID)
		}

		pendingToken, _ := jwtAuth.GenerateTokenWithType(user, middleware.TokenTypePendingMFA)
		body := map[string]string{"code": "000000"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/mfa/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+pendingToken)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.Verify), middleware.TokenTypePendingMFA).ServeHTTP(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("Expected 429 for already-locked user, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp["error"] != "mfa_locked" {
			t.Errorf("Expected error 'mfa_locked', got '%v'", resp["error"])
		}
	})

	t.Run("failed backup code counts toward lockout", func(t *testing.T) {
		_ = userRepo.ResetFailedMFAAttempts(context.Background(), user.ID)

		pendingToken, _ := jwtAuth.GenerateTokenWithType(user, middleware.TokenTypePendingMFA)
		body := map[string]string{"backup_code": "INVALID-CODE"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/mfa/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+pendingToken)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.Verify), middleware.TokenTypePendingMFA).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401 for invalid backup code, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify counter was incremented
		retrieved, _ := userRepo.GetByID(context.Background(), user.ID)
		if retrieved.FailedMFAAttempts != 1 {
			t.Errorf("Expected FailedMFAAttempts=1 after invalid backup code, got %d", retrieved.FailedMFAAttempts)
		}
	})

	t.Run("successful verification resets counter", func(t *testing.T) {
		// Set some failed attempts
		_ = userRepo.ResetFailedMFAAttempts(context.Background(), user.ID)
		for i := 0; i < 3; i++ {
			_, _ = userRepo.IncrementFailedMFAAttempts(context.Background(), user.ID)
		}

		// Re-enable MFA with fresh backup codes for this sub-test
		mfaService := testMFAService(t)
		backupCodes, _ := mfaService.GenerateBackupCodes(10)
		hashedCodes, _ := mfaService.HashBackupCodes(backupCodes)
		_ = userRepo.EnableMFA(context.Background(), user.ID, hashedCodes)

		pendingToken, _ := jwtAuth.GenerateTokenWithType(user, middleware.TokenTypePendingMFA)
		body := map[string]string{"backup_code": backupCodes[0]}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/mfa/verify", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+pendingToken)
		rec := httptest.NewRecorder()

		jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.Verify), middleware.TokenTypePendingMFA).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected 200 for valid backup code, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify counter was reset
		retrieved, _ := userRepo.GetByID(context.Background(), user.ID)
		if retrieved.FailedMFAAttempts != 0 {
			t.Errorf("Expected FailedMFAAttempts=0 after success, got %d", retrieved.FailedMFAAttempts)
		}
	})

	t.Run("remaining attempts decrease with each failure", func(t *testing.T) {
		_ = userRepo.ResetFailedMFAAttempts(context.Background(), user.ID)

		expectedRemaining := []int{4, 3, 2, 1}
		for i, expected := range expectedRemaining {
			pendingToken, _ := jwtAuth.GenerateTokenWithType(user, middleware.TokenTypePendingMFA)
			body := map[string]string{"code": "000000"}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest("POST", "/api/v1/mfa/verify", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+pendingToken)
			rec := httptest.NewRecorder()

			jwtAuth.AuthenticateWithTypes(http.HandlerFunc(handler.Verify), middleware.TokenTypePendingMFA).ServeHTTP(rec, req)

			var resp map[string]interface{}
			_ = json.NewDecoder(rec.Body).Decode(&resp)

			remaining := int(resp["remaining_attempts"].(float64))
			if remaining != expected {
				t.Errorf("Attempt %d: expected %d remaining, got %d", i+1, expected, remaining)
			}
		}
	})
}
