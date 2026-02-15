package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

func TestAuthHandler_CheckAuthRateLimit(t *testing.T) {
	userRepo := database.NewUserRepository(testDB(t))
	jwtAuth := testJWTAuth()
	handler := NewAuthHandler(userRepo, jwtAuth)

	t.Run("allows request when no rate limiter set", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
		rec := httptest.NewRecorder()

		allowed := handler.checkAuthRateLimit(rec, req, "login", 10, 5*time.Minute)
		if !allowed {
			t.Error("Expected request to be allowed when rate limiter is nil")
		}
	})

	t.Run("allows request within limit", func(t *testing.T) {
		limiter := services.NewInMemoryRateLimiter()
		handler.SetRateLimiter(limiter)
		defer func() { handler.rateLimiter = nil }()

		req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()

		allowed := handler.checkAuthRateLimit(rec, req, "login", 10, 5*time.Minute)
		if !allowed {
			t.Error("Expected request to be allowed")
		}
	})

	t.Run("blocks request over limit", func(t *testing.T) {
		limiter := services.NewInMemoryRateLimiter()
		handler.SetRateLimiter(limiter)
		defer func() { handler.rateLimiter = nil }()

		// Exhaust the limit
		for i := 0; i < 3; i++ {
			req := httptest.NewRequest("POST", "/api/v1/auth/register", nil)
			req.RemoteAddr = "10.0.0.1:12345"
			rec := httptest.NewRecorder()
			allowed := handler.checkAuthRateLimit(rec, req, "register", 3, 1*time.Hour)
			if !allowed {
				t.Fatalf("Expected request %d to be allowed", i+1)
			}
		}

		// This should be blocked
		req := httptest.NewRequest("POST", "/api/v1/auth/register", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		rec := httptest.NewRecorder()
		allowed := handler.checkAuthRateLimit(rec, req, "register", 3, 1*time.Hour)
		if allowed {
			t.Error("Expected request to be rate limited")
		}
		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("Expected status 429, got %d", rec.Code)
		}

		var resp ErrorResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp.Error != "rate_limited" {
			t.Errorf("Expected error code 'rate_limited', got '%s'", resp.Error)
		}
	})

	t.Run("different actions have separate limits", func(t *testing.T) {
		limiter := services.NewInMemoryRateLimiter()
		handler.SetRateLimiter(limiter)
		defer func() { handler.rateLimiter = nil }()

		// Exhaust login limit
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
			req.RemoteAddr = "10.0.0.2:12345"
			rec := httptest.NewRecorder()
			handler.checkAuthRateLimit(rec, req, "login", 2, 5*time.Minute)
		}

		// Register from same IP should still work
		req := httptest.NewRequest("POST", "/api/v1/auth/register", nil)
		req.RemoteAddr = "10.0.0.2:12345"
		rec := httptest.NewRecorder()
		allowed := handler.checkAuthRateLimit(rec, req, "register", 3, 1*time.Hour)
		if !allowed {
			t.Error("Expected register to be allowed (separate from login limit)")
		}
	})

	t.Run("different IPs have separate limits", func(t *testing.T) {
		limiter := services.NewInMemoryRateLimiter()
		handler.SetRateLimiter(limiter)
		defer func() { handler.rateLimiter = nil }()

		// Exhaust limit for IP1
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
			req.RemoteAddr = "10.0.0.10:12345"
			rec := httptest.NewRecorder()
			handler.checkAuthRateLimit(rec, req, "login", 2, 5*time.Minute)
		}

		// IP1 should be blocked
		req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
		req.RemoteAddr = "10.0.0.10:12345"
		rec := httptest.NewRecorder()
		blocked := handler.checkAuthRateLimit(rec, req, "login", 2, 5*time.Minute)
		if blocked {
			t.Error("Expected IP1 to be rate limited")
		}

		// IP2 should still be allowed
		req2 := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
		req2.RemoteAddr = "10.0.0.11:12345"
		rec2 := httptest.NewRecorder()
		allowed := handler.checkAuthRateLimit(rec2, req2, "login", 2, 5*time.Minute)
		if !allowed {
			t.Error("Expected IP2 to be allowed (separate from IP1)")
		}
	})
}

func TestAuthHandler_LoginRateLimit(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	handler := NewAuthHandler(userRepo, jwtAuth)

	limiter := services.NewInMemoryRateLimiter()
	handler.SetRateLimiter(limiter)

	// Make 10 login requests (at the limit) - use non-existent emails to avoid lockout
	for i := 0; i < 10; i++ {
		body := map[string]string{
			"email":    "nonexistent@loginrltest.com",
			"password": "anypassword12345",
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "172.16.1.1:12345"
		rec := httptest.NewRecorder()
		handler.Login(rec, req)
		// These should return 401 (invalid credentials), not 429
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("Request %d: expected 401 (within limit), got 429", i+1)
		}
	}

	// 11th request should be rate limited
	body := map[string]string{
		"email":    "nonexistent@loginrltest.com",
		"password": "anypassword12345",
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "172.16.1.1:12345"
	rec := httptest.NewRecorder()
	handler.Login(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429 for rate-limited login, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ErrorResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != "rate_limited" {
		t.Errorf("Expected error 'rate_limited', got '%s'", resp.Error)
	}
}

func TestAuthHandler_ForgotPasswordRateLimit(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	// Construct with nil passwordResetRepo - requests will pass rate limit
	// but fail with 503. We just verify the 6th gets 429 before reaching handler logic.
	handler := NewAuthHandler(userRepo, jwtAuth)

	limiter := services.NewInMemoryRateLimiter()
	handler.SetRateLimiter(limiter)

	// Make 5 requests (at the limit)
	for i := 0; i < 5; i++ {
		body := map[string]string{"email": "test@forgotrltest.com"}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/v1/auth/forgot-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "172.16.2.1:12345"
		rec := httptest.NewRecorder()
		handler.ForgotPassword(rec, req)
		// These will return 503 (password reset not configured) but rate limit is consumed
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("Request %d: should not be rate limited yet", i+1)
		}
	}

	// 6th request should be rate limited
	body := map[string]string{"email": "test@forgotrltest.com"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/auth/forgot-password", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "172.16.2.1:12345"
	rec := httptest.NewRecorder()
	handler.ForgotPassword(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429 for rate-limited forgot-password, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthHandler_ResetPasswordRateLimit(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	handler := NewAuthHandler(userRepo, jwtAuth)

	limiter := services.NewInMemoryRateLimiter()
	handler.SetRateLimiter(limiter)

	// Make 10 requests (at the limit)
	for i := 0; i < 10; i++ {
		body := map[string]string{
			"token":    "invalid_token",
			"password": "newpassword12345",
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/v1/auth/reset-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "172.16.3.1:12345"
		rec := httptest.NewRecorder()
		handler.ResetPassword(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("Request %d: should not be rate limited yet", i+1)
		}
	}

	// 11th request should be rate limited
	body := map[string]string{
		"token":    "invalid_token",
		"password": "newpassword12345",
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/auth/reset-password", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "172.16.3.1:12345"
	rec := httptest.NewRecorder()
	handler.ResetPassword(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429 for rate-limited reset-password, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthHandler_AccountLockout(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	auditRepo := database.NewAuditRepository(db)
	jwtAuth := testJWTAuth()
	handler := NewAuthHandlerWithEmailService(
		db, userRepo, jwtAuth, nil, "test_secret_key_at_least_32_characters_long",
		false, false, auditRepo, nil, nil, "http://localhost:3000", nil,
	)

	// Create a test user
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@lockouttest.com'")
	registerBody := map[string]string{
		"username": "lockouttest",
		"email":    "user@lockouttest.com",
		"password": "securepassword123",
	}
	registerBytes, _ := json.Marshal(registerBody)
	registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	handler.Register(registerRec, registerReq)

	var registerResp models.RegisterResponse
	_ = json.NewDecoder(registerRec.Body).Decode(&registerResp)

	// Disable MFA
	_, _ = db.ExecContext(context.Background(), "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", registerResp.UserID)

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", registerResp.UserID)
	})

	t.Run("increments failed attempts on wrong password", func(t *testing.T) {
		// Reset counter first
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET failed_login_attempts = 0, locked_until = NULL WHERE id = ?", registerResp.UserID)

		body := map[string]string{
			"email":    "user@lockouttest.com",
			"password": "wrongpassword123",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.Login(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}

		// Check that failed_login_attempts was incremented
		user, err := userRepo.GetByID(context.Background(), registerResp.UserID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if user.FailedLoginAttempts != 1 {
			t.Errorf("Expected FailedLoginAttempts=1, got %d", user.FailedLoginAttempts)
		}
	})

	t.Run("locks account after threshold", func(t *testing.T) {
		// Set counter to just below threshold
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET failed_login_attempts = 9, locked_until = NULL WHERE id = ?", registerResp.UserID)

		body := map[string]string{
			"email":    "user@lockouttest.com",
			"password": "wrongpassword123",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.Login(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}

		// Check that account is now locked
		user, err := userRepo.GetByID(context.Background(), registerResp.UserID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if user.LockedUntil == nil {
			t.Error("Expected account to be locked after 10 failed attempts")
		}
		if user.FailedLoginAttempts != 10 {
			t.Errorf("Expected FailedLoginAttempts=10, got %d", user.FailedLoginAttempts)
		}
	})

	t.Run("rejects login for locked account", func(t *testing.T) {
		// Ensure account is locked
		lockUntil := time.Now().Add(15 * time.Minute)
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET failed_login_attempts = 10, locked_until = ? WHERE id = ?", lockUntil, registerResp.UserID)

		body := map[string]string{
			"email":    "user@lockouttest.com",
			"password": "securepassword123", // Correct password
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.Login(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("Expected status 429, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp ErrorResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp.Error != "account_locked" {
			t.Errorf("Expected error 'account_locked', got '%s'", resp.Error)
		}
	})

	t.Run("allows login after lock expires", func(t *testing.T) {
		// Set lock to the past
		lockUntil := time.Now().Add(-1 * time.Minute)
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET failed_login_attempts = 10, locked_until = ? WHERE id = ?", lockUntil, registerResp.UserID)

		body := map[string]string{
			"email":    "user@lockouttest.com",
			"password": "securepassword123",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.Login(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("resets failed attempts on successful login", func(t *testing.T) {
		// Set some failed attempts
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET failed_login_attempts = 5, locked_until = NULL WHERE id = ?", registerResp.UserID)

		body := map[string]string{
			"email":    "user@lockouttest.com",
			"password": "securepassword123",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.Login(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify counter was reset
		user, err := userRepo.GetByID(context.Background(), registerResp.UserID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if user.FailedLoginAttempts != 0 {
			t.Errorf("Expected FailedLoginAttempts=0 after successful login, got %d", user.FailedLoginAttempts)
		}
	})

	t.Run("no lockout increment for nonexistent user", func(t *testing.T) {
		body := map[string]string{
			"email":    "doesnotexist@lockouttest.com",
			"password": "anypassword12345",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.Login(rec, req)

		// Should return 401 without incrementing any counter (no user to increment)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})

	t.Run("lockout works with username login", func(t *testing.T) {
		// Reset counter
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET failed_login_attempts = 9, locked_until = NULL WHERE id = ?", registerResp.UserID)

		body := map[string]string{
			"email":    "lockouttest", // Using username
			"password": "wrongpassword123",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.Login(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}

		// Verify lockout was triggered
		user, err := userRepo.GetByID(context.Background(), registerResp.UserID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if user.LockedUntil == nil {
			t.Error("Expected account to be locked via username login")
		}
		if user.FailedLoginAttempts != 10 {
			t.Errorf("Expected FailedLoginAttempts=10, got %d", user.FailedLoginAttempts)
		}
	})

	t.Run("locked account rejects wrong password with account_locked", func(t *testing.T) {
		// Lock the account
		lockUntil := time.Now().Add(15 * time.Minute)
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET failed_login_attempts = 10, locked_until = ? WHERE id = ?", lockUntil, registerResp.UserID)

		body := map[string]string{
			"email":    "user@lockouttest.com",
			"password": "wrongpassword123", // Wrong password on locked account
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.Login(rec, req)

		// Should return 429 account_locked (lockout check is before password check)
		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("Expected status 429 for locked account with wrong password, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp ErrorResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp.Error != "account_locked" {
			t.Errorf("Expected error 'account_locked', got '%s'", resp.Error)
		}
	})

	t.Run("multiple lockout cycles", func(t *testing.T) {
		// Reset everything
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET failed_login_attempts = 0, locked_until = NULL WHERE id = ?", registerResp.UserID)

		// First cycle: fail 10 times to trigger lockout
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET failed_login_attempts = 9 WHERE id = ?", registerResp.UserID)

		body := map[string]string{
			"email":    "user@lockouttest.com",
			"password": "wrongpassword123",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.Login(rec, req)

		// Verify locked
		user, _ := userRepo.GetByID(context.Background(), registerResp.UserID)
		if user.LockedUntil == nil {
			t.Fatal("Expected account to be locked after first cycle")
		}

		// Expire lock, then login successfully to reset
		lockPast := time.Now().Add(-1 * time.Minute)
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET locked_until = ? WHERE id = ?", lockPast, registerResp.UserID)

		body["password"] = "securepassword123"
		bodyBytes, _ = json.Marshal(body)
		req = httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		handler.Login(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected successful login after lock expired, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify counter was reset
		user, _ = userRepo.GetByID(context.Background(), registerResp.UserID)
		if user.FailedLoginAttempts != 0 {
			t.Errorf("Expected FailedLoginAttempts=0 after successful login, got %d", user.FailedLoginAttempts)
		}
		if user.LockedUntil != nil {
			t.Error("Expected LockedUntil to be nil after successful login")
		}

		// Second cycle: fail 10 times again
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET failed_login_attempts = 9 WHERE id = ?", registerResp.UserID)

		body["password"] = "wrongpassword123"
		bodyBytes, _ = json.Marshal(body)
		req = httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		handler.Login(rec, req)

		// Verify locked again
		user, _ = userRepo.GetByID(context.Background(), registerResp.UserID)
		if user.LockedUntil == nil {
			t.Error("Expected account to be locked after second cycle")
		}
		if user.FailedLoginAttempts != 10 {
			t.Errorf("Expected FailedLoginAttempts=10 in second cycle, got %d", user.FailedLoginAttempts)
		}
	})

	t.Run("creates audit log entry on lockout", func(t *testing.T) {
		// Reset counter
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET failed_login_attempts = 9, locked_until = NULL WHERE id = ?", registerResp.UserID)
		// Delete existing audit entries for clean test
		_, _ = db.ExecContext(context.Background(), "DELETE FROM audit_log WHERE user_id = ? AND action = ?", registerResp.UserID, models.AuditActionAccountLocked)

		body := map[string]string{
			"email":    "user@lockouttest.com",
			"password": "wrongpassword123",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.Login(rec, req)

		// Verify audit log entry was created
		var auditCount int
		err := db.QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM audit_log WHERE user_id = ? AND action = ?",
			registerResp.UserID, models.AuditActionAccountLocked,
		).Scan(&auditCount)
		if err != nil {
			t.Fatalf("Failed to query audit log: %v", err)
		}
		if auditCount == 0 {
			t.Error("Expected audit log entry for account_locked action")
		}
	})
}

func TestAuthHandler_RegisterRateLimit(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	handler := NewAuthHandler(userRepo, jwtAuth)

	limiter := services.NewInMemoryRateLimiter()
	handler.SetRateLimiter(limiter)

	// Clean up test users
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@ratelimittest.com'")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@ratelimittest.com'")
	})

	// Make 3 registration requests (at the limit)
	for i := 0; i < 3; i++ {
		body := map[string]string{
			"username": "invalid", // Will fail validation but rate limit still counts
			"email":    "",
			"password": "",
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "172.16.0.1:12345"
		rec := httptest.NewRecorder()
		handler.Register(rec, req)
		// Don't check status - we just want to consume rate limit tokens
	}

	// 4th request should be rate limited
	body := map[string]string{
		"username": "ratelimited",
		"email":    "ratelimited@ratelimittest.com",
		"password": "securepassword123",
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "172.16.0.1:12345"
	rec := httptest.NewRecorder()
	handler.Register(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429 for rate-limited register, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthHandler_LockoutAndRateLimitIndependent(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	auditRepo := database.NewAuditRepository(db)
	jwtAuth := testJWTAuth()
	handler := NewAuthHandlerWithEmailService(
		db, userRepo, jwtAuth, nil, "test_secret_key_at_least_32_characters_long",
		false, false, auditRepo, nil, nil, "http://localhost:3000", nil,
	)

	limiter := services.NewInMemoryRateLimiter()
	handler.SetRateLimiter(limiter)

	// Create a test user
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@lockrl.com'")
	registerBody := map[string]string{
		"username": "lockrltest",
		"email":    "user@lockrl.com",
		"password": "securepassword123",
	}
	registerBytes, _ := json.Marshal(registerBody)
	registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	handler.Register(registerRec, registerReq)

	var registerResp models.RegisterResponse
	_ = json.NewDecoder(registerRec.Body).Decode(&registerResp)

	_, _ = db.ExecContext(context.Background(), "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", registerResp.UserID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", registerResp.UserID)
	})

	t.Run("lockout triggers before IP rate limit is exhausted", func(t *testing.T) {
		// Reset state
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET failed_login_attempts = 0, locked_until = NULL WHERE id = ?", registerResp.UserID)

		// Lockout threshold is 10, login rate limit is also 10
		// After 10 failed attempts: account is locked, and rate limit is exhausted
		for i := 0; i < 10; i++ {
			body := map[string]string{
				"email":    "user@lockrl.com",
				"password": "wrongpassword123",
			}
			bodyBytes, _ := json.Marshal(body)
			req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			req.RemoteAddr = "172.16.5.1:12345"
			rec := httptest.NewRecorder()
			handler.Login(rec, req)

			if rec.Code == http.StatusTooManyRequests {
				t.Fatalf("Request %d: got rate limited before lockout, expected 401", i+1)
			}
		}

		// Verify account is locked
		user, err := userRepo.GetByID(context.Background(), registerResp.UserID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if user.LockedUntil == nil {
			t.Error("Expected account to be locked after 10 wrong attempts")
		}
	})
}
