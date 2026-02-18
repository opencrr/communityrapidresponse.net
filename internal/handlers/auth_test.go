package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// testDB returns a database connection for testing
func testDB(t *testing.T) *database.DB {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping handler tests")
		return nil
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

	db, err := database.New(cfg)
	if err != nil {
		t.Skipf("Failed to connect to test database (skipping): %v", err)
		return nil
	}

	t.Cleanup(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	return db
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func testJWTAuth() *middleware.JWTAuth {
	return middleware.NewJWTAuth(&config.JWTConfig{
		Secret:          "test_secret_key_at_least_32_characters_long",
		ExpirationHours: 24,
		Issuer:          "test_issuer",
	})
}

func TestAuthHandler_Register(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	handler := NewAuthHandler(userRepo, jwtAuth)

	// Clean up any existing test user
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@registertest.com'")

	t.Run("registers user successfully", func(t *testing.T) {
		body := map[string]string{
			"username": "registertest",
			"email":    "user@registertest.com",
			"password": "securepassword123",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Register(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp models.RegisterResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp.UserID == "" {
			t.Error("Expected user ID in response")
		}
		if resp.Username != "registertest" {
			t.Errorf("Expected username 'registertest', got '%s'", resp.Username)
		}

		// Cleanup
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", resp.UserID)
	})

	t.Run("rejects missing fields", func(t *testing.T) {
		body := map[string]string{
			"username": "incomplete",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Register(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("rejects short password", func(t *testing.T) {
		body := map[string]string{
			"username": "shortpass",
			"email":    "short@registertest.com",
			"password": "short",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Register(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("rejects too long password", func(t *testing.T) {
		body := map[string]string{
			"username": "longpass",
			"email":    "long@registertest.com",
			"password": strings.Repeat("a", 129),
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Register(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("rejects duplicate username", func(t *testing.T) {
		// First registration
		body1 := map[string]string{
			"username": "dupetest",
			"email":    "dupe1@registertest.com",
			"password": "securepassword123",
		}
		bodyBytes1, _ := json.Marshal(body1)

		req1 := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(bodyBytes1))
		req1.Header.Set("Content-Type", "application/json")
		rec1 := httptest.NewRecorder()
		handler.Register(rec1, req1)

		// Second registration with same username
		body2 := map[string]string{
			"username": "dupetest",
			"email":    "dupe2@registertest.com",
			"password": "securepassword123",
		}
		bodyBytes2, _ := json.Marshal(body2)

		req2 := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(bodyBytes2))
		req2.Header.Set("Content-Type", "application/json")
		rec2 := httptest.NewRecorder()
		handler.Register(rec2, req2)

		if rec2.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d", rec2.Code)
		}

		// Cleanup
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE username = 'dupetest'")
	})

	t.Run("rejects invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader([]byte("invalid json")))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Register(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})
}

func TestAuthHandler_Login(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	handler := NewAuthHandler(userRepo, jwtAuth)

	// Create a test user first
	registerBody := map[string]string{
		"username": "logintest",
		"email":    "login@logintest.com",
		"password": "securepassword123",
	}
	registerBytes, _ := json.Marshal(registerBody)
	registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	handler.Register(registerRec, registerReq)

	var registerResp models.RegisterResponse
	_ = json.NewDecoder(registerRec.Body).Decode(&registerResp)

	t.Run("logs in with valid credentials", func(t *testing.T) {
		body := map[string]string{
			"email":    "login@logintest.com",
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

		var resp models.LoginResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp.Token == "" {
			t.Error("Expected token in response")
		}
		if resp.User == nil {
			t.Error("Expected user in response")
		}
		if resp.User.Username != "logintest" {
			t.Errorf("Expected username 'logintest', got '%s'", resp.User.Username)
		}

		// Check cookie
		cookies := rec.Result().Cookies()
		found := false
		for _, c := range cookies {
			if c.Name == "token" {
				found = true
				if !c.HttpOnly {
					t.Error("Expected HttpOnly cookie")
				}
			}
		}
		if !found {
			t.Error("Expected token cookie to be set")
		}
	})

	t.Run("rejects invalid password", func(t *testing.T) {
		body := map[string]string{
			"email":    "login@logintest.com",
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
	})

	t.Run("rejects non-existent user", func(t *testing.T) {
		body := map[string]string{
			"email":    "nonexistent@logintest.com",
			"password": "anypassword123",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})

	t.Run("rejects missing fields", func(t *testing.T) {
		body := map[string]string{
			"email": "login@logintest.com",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("logs in with username instead of email", func(t *testing.T) {
		body := map[string]string{
			"email":    "logintest", // Using username in the email field
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

		var resp models.LoginResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp.Token == "" {
			t.Error("Expected token in response")
		}
		if resp.User == nil {
			t.Error("Expected user in response")
		}
		if resp.User.Username != "logintest" {
			t.Errorf("Expected username 'logintest', got '%s'", resp.User.Username)
		}
	})

	t.Run("rejects invalid password with username login", func(t *testing.T) {
		body := map[string]string{
			"email":    "logintest", // Using username
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
	})

	t.Run("rejects non-existent username", func(t *testing.T) {
		body := map[string]string{
			"email":    "nonexistentuser",
			"password": "anypassword123",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})

	// Cleanup
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", registerResp.UserID)
}

func TestAuthHandler_Login_MFAFlow(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	handler := NewAuthHandler(userRepo, jwtAuth)

	// Clean up any existing test users
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@mfalogintest.com'")

	// Create a test user - new users have mfa_setup_required = true by default
	registerBody := map[string]string{
		"username": "mfalogintest",
		"email":    "user@mfalogintest.com",
		"password": "securepassword123",
	}
	registerBytes, _ := json.Marshal(registerBody)
	registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	handler.Register(registerRec, registerReq)

	var registerResp models.RegisterResponse
	_ = json.NewDecoder(registerRec.Body).Decode(&registerResp)

	t.Run("returns mfa_action=setup for user needing MFA setup", func(t *testing.T) {
		body := map[string]string{
			"email":    "user@mfalogintest.com",
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

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp["mfa_action"] != "setup" {
			t.Errorf("Expected mfa_action='setup', got '%v'", resp["mfa_action"])
		}

		if resp["token"] == nil || resp["token"] == "" {
			t.Error("Expected MFA setup token in response")
		}
	})

	t.Run("returns mfa_action=verify for user with MFA enabled", func(t *testing.T) {
		// Enable MFA for the user
		_, _ = db.ExecContext(context.Background(),
			"UPDATE users SET mfa_enabled = TRUE, mfa_setup_required = FALSE, mfa_secret = 'encrypted_secret' WHERE id = ?",
			registerResp.UserID)

		body := map[string]string{
			"email":    "user@mfalogintest.com",
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

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp["mfa_action"] != "verify" {
			t.Errorf("Expected mfa_action='verify', got '%v'", resp["mfa_action"])
		}
	})

	// Cleanup
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", registerResp.UserID)
}

func TestAuthHandler_Logout(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	handler := NewAuthHandler(userRepo, jwtAuth)

	req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	rec := httptest.NewRecorder()

	handler.Logout(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	// Check cookie is cleared
	cookies := rec.Result().Cookies()
	for _, c := range cookies {
		if c.Name == "token" {
			if c.MaxAge >= 0 {
				t.Error("Expected MaxAge to be negative (cookie deletion)")
			}
		}
	}
}

func TestAuthHandler_GetCurrentUser(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	handler := NewAuthHandler(userRepo, jwtAuth)

	// Create and login a test user
	registerBody := map[string]string{
		"username": "currenttest",
		"email":    "current@currenttest.com",
		"password": "securepassword123",
	}
	registerBytes, _ := json.Marshal(registerBody)
	registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	handler.Register(registerRec, registerReq)

	var registerResp models.RegisterResponse
	_ = json.NewDecoder(registerRec.Body).Decode(&registerResp)

	// Disable MFA requirement for this user so login returns a full token
	_, _ = db.ExecContext(context.Background(), "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", registerResp.UserID)

	// Login to get token
	loginBody := map[string]string{
		"email":    "current@currenttest.com",
		"password": "securepassword123",
	}
	loginBytes, _ := json.Marshal(loginBody)
	loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBytes))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.Login(loginRec, loginReq)

	var loginResp models.LoginResponse
	_ = json.NewDecoder(loginRec.Body).Decode(&loginResp)

	t.Run("returns current user with valid token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/users/me", nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.Token)

		// Wrap with auth middleware
		rec := httptest.NewRecorder()
		jwtAuth.Authenticate(http.HandlerFunc(handler.GetCurrentUser)).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Response is wrapped in {"user": {...}}
		var resp struct {
			User models.UserWithRegions `json:"user"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp.User.Username != "currenttest" {
			t.Errorf("Expected username 'currenttest', got '%s'", resp.User.Username)
		}
	})

	t.Run("rejects request without token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/users/me", nil)
		rec := httptest.NewRecorder()

		jwtAuth.Authenticate(http.HandlerFunc(handler.GetCurrentUser)).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})

	// Cleanup
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", registerResp.UserID)
}

func TestAuthHandler_ChangePassword(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	passwordResetRepo := database.NewPasswordResetRepository(db)
	jwtAuth := testJWTAuth()
	handler := NewAuthHandlerWithEmailService(
		nil, userRepo, jwtAuth, nil, "test_secret_key_at_least_32_characters_long",
		false, false, nil, passwordResetRepo, nil, "http://localhost:3000", nil,
	)

	// Create a test user
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@changepasstest.com'")
	registerBody := map[string]string{
		"username": "changepasstest",
		"email":    "user@changepasstest.com",
		"password": "oldpassword12345",
	}
	registerBytes, _ := json.Marshal(registerBody)
	registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	handler.Register(registerRec, registerReq)

	var registerResp models.RegisterResponse
	_ = json.NewDecoder(registerRec.Body).Decode(&registerResp)

	// Disable MFA so login returns full token
	_, _ = db.ExecContext(context.Background(), "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", registerResp.UserID)

	// Login
	loginBody := map[string]string{"email": "user@changepasstest.com", "password": "oldpassword12345"}
	loginBytes, _ := json.Marshal(loginBody)
	loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBytes))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.Login(loginRec, loginReq)

	var loginResp models.LoginResponse
	_ = json.NewDecoder(loginRec.Body).Decode(&loginResp)

	t.Run("changes password successfully", func(t *testing.T) {
		body := map[string]string{
			"current_password": "oldpassword12345",
			"new_password":     "newpassword12345",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/change-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+loginResp.Token)
		rec := httptest.NewRecorder()

		jwtAuth.Authenticate(http.HandlerFunc(handler.ChangePassword)).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify new password works
		loginBody2 := map[string]string{"email": "user@changepasstest.com", "password": "newpassword12345"}
		loginBytes2, _ := json.Marshal(loginBody2)
		loginReq2 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBytes2))
		loginReq2.Header.Set("Content-Type", "application/json")
		loginRec2 := httptest.NewRecorder()
		handler.Login(loginRec2, loginReq2)

		if loginRec2.Code != http.StatusOK {
			t.Errorf("Expected login with new password to succeed, got %d", loginRec2.Code)
		}
	})

	t.Run("rejects wrong current password", func(t *testing.T) {
		body := map[string]string{
			"current_password": "wrongpassword12345",
			"new_password":     "anotherpassword12",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/change-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+loginResp.Token)
		rec := httptest.NewRecorder()

		jwtAuth.Authenticate(http.HandlerFunc(handler.ChangePassword)).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})

	t.Run("rejects too short new password", func(t *testing.T) {
		body := map[string]string{
			"current_password": "newpassword12345",
			"new_password":     "short",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/change-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+loginResp.Token)
		rec := httptest.NewRecorder()

		jwtAuth.Authenticate(http.HandlerFunc(handler.ChangePassword)).ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("rejects too long new password", func(t *testing.T) {
		body := map[string]string{
			"current_password": "newpassword12345",
			"new_password":     strings.Repeat("a", 129),
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/change-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+loginResp.Token)
		rec := httptest.NewRecorder()

		jwtAuth.Authenticate(http.HandlerFunc(handler.ChangePassword)).ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		body := map[string]string{
			"current_password": "newpassword12345",
			"new_password":     "anotherpassword12",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/change-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		jwtAuth.Authenticate(http.HandlerFunc(handler.ChangePassword)).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})

	// Cleanup
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", registerResp.UserID)
}

func TestAuthHandler_ForgotPassword(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	passwordResetRepo := database.NewPasswordResetRepository(db)
	handler := NewAuthHandlerWithEmailService(
		nil, userRepo, testJWTAuth(), nil, "test_secret_key_at_least_32_characters_long",
		false, false, nil, passwordResetRepo, nil, "http://localhost:3000", nil,
	)

	// Create a test user
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@forgottest.com'")
	registerBody := map[string]string{
		"username": "forgottest",
		"email":    "user@forgottest.com",
		"password": "securepassword123",
	}
	registerBytes, _ := json.Marshal(registerBody)
	registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	handler.Register(registerRec, registerReq)

	var forgotRegisterResp models.RegisterResponse
	_ = json.NewDecoder(registerRec.Body).Decode(&forgotRegisterResp)

	t.Run("returns success for valid email", func(t *testing.T) {
		body := map[string]string{"email": "user@forgottest.com"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/forgot-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ForgotPassword(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("returns success for nonexistent email (no enumeration)", func(t *testing.T) {
		body := map[string]string{"email": "nonexistent@forgottest.com"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/forgot-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ForgotPassword(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200 (same as valid email), got %d", rec.Code)
		}
	})

	// Cleanup
	_, _ = db.ExecContext(context.Background(), "DELETE FROM password_reset_tokens WHERE user_id = ?", forgotRegisterResp.UserID)
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", forgotRegisterResp.UserID)
}

func TestAuthHandler_ResetPassword(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	passwordResetRepo := database.NewPasswordResetRepository(db)
	jwtAuth := testJWTAuth()
	handler := NewAuthHandlerWithEmailService(
		nil, userRepo, jwtAuth, nil, "test_secret_key_at_least_32_characters_long",
		false, false, nil, passwordResetRepo, nil, "http://localhost:3000", nil,
	)

	// Create a test user
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@resetpasstest.com'")
	registerBody := map[string]string{
		"username": "resetpasstest",
		"email":    "user@resetpasstest.com",
		"password": "securepassword123",
	}
	registerBytes, _ := json.Marshal(registerBody)
	registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	handler.Register(registerRec, registerReq)

	var resetRegisterResp models.RegisterResponse
	_ = json.NewDecoder(registerRec.Body).Decode(&resetRegisterResp)

	// Disable MFA
	_, _ = db.ExecContext(context.Background(), "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", resetRegisterResp.UserID)

	t.Run("resets password with valid token", func(t *testing.T) {
		rawToken := "valid_reset_token_for_test_abc123"
		tokenHash := fmt.Sprintf("%x", sha256Of(rawToken))
		_, err := passwordResetRepo.Create(context.Background(), resetRegisterResp.UserID, tokenHash, timeInFuture(1))
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		body := map[string]string{
			"token":    rawToken,
			"password": "brandnewpassword",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/reset-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ResetPassword(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify new password works
		loginBody := map[string]string{"email": "user@resetpasstest.com", "password": "brandnewpassword"}
		loginBytes, _ := json.Marshal(loginBody)
		loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBytes))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		handler.Login(loginRec, loginReq)

		if loginRec.Code != http.StatusOK {
			t.Errorf("Expected login with new password to succeed, got %d", loginRec.Code)
		}
	})

	t.Run("rejects invalid token", func(t *testing.T) {
		body := map[string]string{
			"token":    "nonexistent_token",
			"password": "securepassword123",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/reset-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ResetPassword(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("rejects expired token", func(t *testing.T) {
		rawToken := "expired_reset_token_for_test"
		tokenHash := fmt.Sprintf("%x", sha256Of(rawToken))
		_, err := passwordResetRepo.Create(context.Background(), resetRegisterResp.UserID, tokenHash, timeInPast(1))
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		body := map[string]string{
			"token":    rawToken,
			"password": "securepassword123",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/reset-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ResetPassword(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects too short password", func(t *testing.T) {
		rawToken := "short_password_reset_token"
		tokenHash := fmt.Sprintf("%x", sha256Of(rawToken))
		_, err := passwordResetRepo.Create(context.Background(), resetRegisterResp.UserID, tokenHash, timeInFuture(1))
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		body := map[string]string{
			"token":    rawToken,
			"password": "short",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/reset-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ResetPassword(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("rejects too long password", func(t *testing.T) {
		body := map[string]string{
			"token":    "long_password_reset_token",
			"password": strings.Repeat("a", 129),
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/reset-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ResetPassword(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	t.Run("returns 500 when user deleted before reset (tx rollback)", func(t *testing.T) {
		// Create a handler with db set, so the transactional path is taken
		txHandler := NewAuthHandlerWithEmailService(
			db, userRepo, jwtAuth, nil, "test_secret_key_at_least_32_characters_long",
			false, false, nil, passwordResetRepo, nil, "http://localhost:3000", nil,
		)

		// Create a throwaway user and a valid token for them
		regBody := map[string]string{
			"username": "resetghost",
			"email":    "ghost@resetpasstest.com",
			"password": "securepassword123",
		}
		regBytes, _ := json.Marshal(regBody)
		regReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(regBytes))
		regReq.Header.Set("Content-Type", "application/json")
		regRec := httptest.NewRecorder()
		txHandler.Register(regRec, regReq)

		var ghostResp models.RegisterResponse
		_ = json.NewDecoder(regRec.Body).Decode(&ghostResp)

		rawToken := "ghost_user_reset_token_test"
		tokenHash := fmt.Sprintf("%x", sha256Of(rawToken))
		_, err := passwordResetRepo.Create(context.Background(), ghostResp.UserID, tokenHash, timeInFuture(1))
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		// Delete the user — CASCADE removes reset tokens too, but the handler
		// does GetByTokenHash (non-tx) BEFORE the transaction. To simulate a
		// race where the user is deleted between token lookup and password update,
		// we disable FK checks, re-insert the orphaned token, then re-enable.
		// Must use a single connection since FOREIGN_KEY_CHECKS is session-scoped.
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", ghostResp.UserID)
		conn, connErr := db.Conn(context.Background())
		if connErr != nil {
			t.Fatalf("Failed to get DB connection: %v", connErr)
		}
		_, _ = conn.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS=0")
		_, _ = conn.ExecContext(context.Background(),
			"INSERT INTO password_reset_tokens (id, user_id, token_hash, created_at, expires_at) VALUES (?, ?, ?, NOW(), ?)",
			"ghost-token-id", ghostResp.UserID, tokenHash, timeInFuture(1))
		_, _ = conn.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS=1")
		_ = conn.Close()

		body := map[string]string{
			"token":    rawToken,
			"password": "securepassword123",
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/v1/auth/reset-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		txHandler.ResetPassword(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("Expected 500 for deleted user tx rollback, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify the token was NOT consumed (tx rolled back)
		_, lookupErr := passwordResetRepo.GetByTokenHash(context.Background(), tokenHash)
		if lookupErr != nil {
			t.Errorf("Expected token to still be valid after rollback, got error: %v", lookupErr)
		}

		// Cleanup orphaned token (single connection for FK checks)
		cleanConn, _ := db.Conn(context.Background())
		if cleanConn != nil {
			_, _ = cleanConn.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS=0")
			_, _ = cleanConn.ExecContext(context.Background(), "DELETE FROM password_reset_tokens WHERE user_id = ?", ghostResp.UserID)
			_, _ = cleanConn.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS=1")
			_ = cleanConn.Close()
		}
	})

	// Cleanup
	_, _ = db.ExecContext(context.Background(), "DELETE FROM password_reset_tokens WHERE user_id = ?", resetRegisterResp.UserID)
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", resetRegisterResp.UserID)
}

func TestAuthHandler_ValidateResetToken(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	passwordResetRepo := database.NewPasswordResetRepository(db)
	handler := NewAuthHandlerWithEmailService(
		nil, userRepo, testJWTAuth(), nil, "test_secret_key_at_least_32_characters_long",
		false, false, nil, passwordResetRepo, nil, "http://localhost:3000", nil,
	)

	// Create a test user
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@validatetokentest.com'")
	registerBody := map[string]string{
		"username": "validatetokentest",
		"email":    "user@validatetokentest.com",
		"password": "securepassword123",
	}
	registerBytes, _ := json.Marshal(registerBody)
	registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	handler.Register(registerRec, registerReq)

	var validateRegisterResp models.RegisterResponse
	_ = json.NewDecoder(registerRec.Body).Decode(&validateRegisterResp)

	t.Run("validates valid token", func(t *testing.T) {
		rawToken := "valid_validate_token_test"
		tokenHash := fmt.Sprintf("%x", sha256Of(rawToken))
		_, err := passwordResetRepo.Create(context.Background(), validateRegisterResp.UserID, tokenHash, timeInFuture(1))
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		req := httptest.NewRequest("GET", "/api/v1/auth/validate-reset-token?token="+rawToken, nil)
		rec := httptest.NewRecorder()

		handler.ValidateResetToken(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects invalid token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/auth/validate-reset-token?token=nonexistent_token", nil)
		rec := httptest.NewRecorder()

		handler.ValidateResetToken(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rec.Code)
		}
	})

	// Cleanup
	_, _ = db.ExecContext(context.Background(), "DELETE FROM password_reset_tokens WHERE user_id = ?", validateRegisterResp.UserID)
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", validateRegisterResp.UserID)
}

func TestAuthHandler_DeleteAccount(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	auditRepo := database.NewAuditRepository(db)
	jwtAuth := testJWTAuth()
	handler := NewAuthHandlerWithEmailService(
		db, userRepo, jwtAuth, nil, "test_secret_key_at_least_32_characters_long",
		false, false, auditRepo, nil, nil, "http://localhost:3000", nil,
	)

	t.Run("hard deletes non-blocked user", func(t *testing.T) {
		// Create a test user
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@deletehardtest.com'")
		registerBody := map[string]string{
			"username": "deletehardtest",
			"email":    "user@deletehardtest.com",
			"password": "securepassword123",
		}
		registerBytes, _ := json.Marshal(registerBody)
		registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
		registerReq.Header.Set("Content-Type", "application/json")
		registerRec := httptest.NewRecorder()
		handler.Register(registerRec, registerReq)

		var registerResp models.RegisterResponse
		_ = json.NewDecoder(registerRec.Body).Decode(&registerResp)

		// Disable MFA so login returns full token
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?", registerResp.UserID)

		// Login
		loginBody := map[string]string{"email": "user@deletehardtest.com", "password": "securepassword123"}
		loginBytes, _ := json.Marshal(loginBody)
		loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBytes))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		handler.Login(loginRec, loginReq)

		var loginResp models.LoginResponse
		_ = json.NewDecoder(loginRec.Body).Decode(&loginResp)

		// Delete account
		deleteBody := map[string]string{"password": "securepassword123"}
		deleteBytes, _ := json.Marshal(deleteBody)
		deleteReq := httptest.NewRequest("DELETE", "/api/v1/users/me", bytes.NewReader(deleteBytes))
		deleteReq.Header.Set("Content-Type", "application/json")
		deleteReq.Header.Set("Authorization", "Bearer "+loginResp.Token)
		deleteRec := httptest.NewRecorder()

		jwtAuth.Authenticate(http.HandlerFunc(handler.DeleteAccount)).ServeHTTP(deleteRec, deleteReq)

		if deleteRec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", deleteRec.Code, deleteRec.Body.String())
		}

		// Verify user is gone (hard delete)
		_, err := userRepo.GetByID(context.Background(), registerResp.UserID)
		if err == nil {
			t.Error("Expected user to be deleted")
		}

		// Verify cookie is cleared
		cookies := deleteRec.Result().Cookies()
		for _, c := range cookies {
			if c.Name == "token" && c.MaxAge >= 0 {
				t.Error("Expected token cookie to be cleared")
			}
		}
	})

	t.Run("soft deletes blocked user", func(t *testing.T) {
		// Create a test user
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@deletesofttest.com'")
		registerBody := map[string]string{
			"username": "deletesofttest",
			"email":    "user@deletesofttest.com",
			"password": "securepassword123",
		}
		registerBytes, _ := json.Marshal(registerBody)
		registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
		registerReq.Header.Set("Content-Type", "application/json")
		registerRec := httptest.NewRecorder()
		handler.Register(registerRec, registerReq)

		var registerResp models.RegisterResponse
		_ = json.NewDecoder(registerRec.Body).Decode(&registerResp)

		// Disable MFA and login BEFORE blocking (blocked users can't login)
		_, _ = db.ExecContext(context.Background(),
			"UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?",
			registerResp.UserID)

		// Login first to get a token
		loginBody := map[string]string{"email": "user@deletesofttest.com", "password": "securepassword123"}
		loginBytes, _ := json.Marshal(loginBody)
		loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBytes))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		handler.Login(loginRec, loginReq)

		var loginResp models.LoginResponse
		_ = json.NewDecoder(loginRec.Body).Decode(&loginResp)

		// NOW block the user (after obtaining the token)
		_, _ = db.ExecContext(context.Background(),
			"UPDATE users SET is_blocked = TRUE, blocked_at = NOW(), block_reason = 'test' WHERE id = ?",
			registerResp.UserID)

		// Delete account using pre-obtained token
		// (no status cache in handler unit tests, so middleware won't block the token)
		deleteBody := map[string]string{"password": "securepassword123"}
		deleteBytes, _ := json.Marshal(deleteBody)
		deleteReq := httptest.NewRequest("DELETE", "/api/v1/users/me", bytes.NewReader(deleteBytes))
		deleteReq.Header.Set("Content-Type", "application/json")
		deleteReq.Header.Set("Authorization", "Bearer "+loginResp.Token)
		deleteRec := httptest.NewRecorder()

		jwtAuth.Authenticate(http.HandlerFunc(handler.DeleteAccount)).ServeHTTP(deleteRec, deleteReq)

		if deleteRec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", deleteRec.Code, deleteRec.Body.String())
		}

		// Verify user row still exists (soft delete) but PII is scrubbed
		user, err := userRepo.GetByID(context.Background(), registerResp.UserID)
		if err != nil {
			t.Fatalf("Expected soft-deleted user row to still exist: %v", err)
		}
		if user.DeletedAt == nil {
			t.Error("Expected deleted_at to be set")
		}
		if user.Email == "user@deletesofttest.com" {
			t.Error("Expected email to be scrubbed")
		}
		if user.Username == "deletesofttest" {
			t.Error("Expected username to be scrubbed")
		}
		if user.PasswordHash != "" {
			t.Error("Expected password_hash to be empty")
		}
		if user.MFASecret != nil {
			t.Error("Expected mfa_secret to be NULL")
		}
		// Block data should be preserved
		if !user.IsBlocked {
			t.Error("Expected is_blocked to still be true")
		}

		// Cleanup
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", registerResp.UserID)
	})

	t.Run("soft deletes user with blocked_users row", func(t *testing.T) {
		// Create a test user (not directly blocked via is_blocked, but in blocked_users)
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@deleteblacklist.com'")
		registerBody := map[string]string{
			"username": "deleteblacklist",
			"email":    "user@deleteblacklist.com",
			"password": "securepassword123",
		}
		registerBytes, _ := json.Marshal(registerBody)
		registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
		registerReq.Header.Set("Content-Type", "application/json")
		registerRec := httptest.NewRecorder()
		handler.Register(registerRec, registerReq)

		var registerResp models.RegisterResponse
		_ = json.NewDecoder(registerRec.Body).Decode(&registerResp)

		// Disable MFA (is_blocked stays FALSE)
		_, _ = db.ExecContext(context.Background(),
			"UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?",
			registerResp.UserID)

		// Create a region and blocked_users entry
		regionID := "test-region-blacklist-" + registerResp.UserID[:8]
		_, _ = db.ExecContext(context.Background(),
			"INSERT INTO geographic_regions (id, name, region_type) VALUES (?, 'Test Region Blacklist', 'city') ON DUPLICATE KEY UPDATE name=name",
			regionID)
		_, _ = db.ExecContext(context.Background(),
			"INSERT INTO blocked_users (id, user_id, region_id) VALUES (?, ?, ?)",
			"bl-"+registerResp.UserID[:8], registerResp.UserID, regionID)
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), "DELETE FROM blocked_users WHERE user_id = ?", registerResp.UserID)
			_, _ = db.ExecContext(context.Background(), "DELETE FROM geographic_regions WHERE id = ?", regionID)
			_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", registerResp.UserID)
		})

		// Login
		loginBody := map[string]string{"email": "user@deleteblacklist.com", "password": "securepassword123"}
		loginBytes, _ := json.Marshal(loginBody)
		loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBytes))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		handler.Login(loginRec, loginReq)

		var loginResp models.LoginResponse
		_ = json.NewDecoder(loginRec.Body).Decode(&loginResp)

		// Delete account
		deleteBody := map[string]string{"password": "securepassword123"}
		deleteBytes, _ := json.Marshal(deleteBody)
		deleteReq := httptest.NewRequest("DELETE", "/api/v1/users/me", bytes.NewReader(deleteBytes))
		deleteReq.Header.Set("Content-Type", "application/json")
		deleteReq.Header.Set("Authorization", "Bearer "+loginResp.Token)
		deleteRec := httptest.NewRecorder()

		jwtAuth.Authenticate(http.HandlerFunc(handler.DeleteAccount)).ServeHTTP(deleteRec, deleteReq)

		if deleteRec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", deleteRec.Code, deleteRec.Body.String())
		}

		// Verify soft delete (user row still exists, PII scrubbed)
		user, err := userRepo.GetByID(context.Background(), registerResp.UserID)
		if err != nil {
			t.Fatalf("Expected soft-deleted user row to still exist: %v", err)
		}
		if user.DeletedAt == nil {
			t.Error("Expected deleted_at to be set")
		}
		if user.Email == "user@deleteblacklist.com" {
			t.Error("Expected email to be scrubbed")
		}
		if user.Username == "deleteblacklist" {
			t.Error("Expected username to be scrubbed")
		}
	})

	t.Run("rejects wrong password", func(t *testing.T) {
		// Create a test user
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@deletewrongpass.com'")
		registerBody := map[string]string{
			"username": "deletewrongpass",
			"email":    "user@deletewrongpass.com",
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

		// Login
		loginBody := map[string]string{"email": "user@deletewrongpass.com", "password": "securepassword123"}
		loginBytes, _ := json.Marshal(loginBody)
		loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBytes))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		handler.Login(loginRec, loginReq)

		var loginResp models.LoginResponse
		_ = json.NewDecoder(loginRec.Body).Decode(&loginResp)

		// Try delete with wrong password
		deleteBody := map[string]string{"password": "wrongpassword123"}
		deleteBytes, _ := json.Marshal(deleteBody)
		deleteReq := httptest.NewRequest("DELETE", "/api/v1/users/me", bytes.NewReader(deleteBytes))
		deleteReq.Header.Set("Content-Type", "application/json")
		deleteReq.Header.Set("Authorization", "Bearer "+loginResp.Token)
		deleteRec := httptest.NewRecorder()

		jwtAuth.Authenticate(http.HandlerFunc(handler.DeleteAccount)).ServeHTTP(deleteRec, deleteReq)

		if deleteRec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", deleteRec.Code)
		}

		// Cleanup
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", registerResp.UserID)
	})

	t.Run("rejects missing password", func(t *testing.T) {
		// Create a test user
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@deletenopass.com'")
		registerBody := map[string]string{
			"username": "deletenopass",
			"email":    "user@deletenopass.com",
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

		// Login
		loginBody := map[string]string{"email": "user@deletenopass.com", "password": "securepassword123"}
		loginBytes, _ := json.Marshal(loginBody)
		loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBytes))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		handler.Login(loginRec, loginReq)

		var loginResp models.LoginResponse
		_ = json.NewDecoder(loginRec.Body).Decode(&loginResp)

		// Try delete with empty body
		deleteBody := map[string]string{"password": ""}
		deleteBytes, _ := json.Marshal(deleteBody)
		deleteReq := httptest.NewRequest("DELETE", "/api/v1/users/me", bytes.NewReader(deleteBytes))
		deleteReq.Header.Set("Content-Type", "application/json")
		deleteReq.Header.Set("Authorization", "Bearer "+loginResp.Token)
		deleteRec := httptest.NewRecorder()

		jwtAuth.Authenticate(http.HandlerFunc(handler.DeleteAccount)).ServeHTTP(deleteRec, deleteReq)

		if deleteRec.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", deleteRec.Code)
		}

		// Cleanup
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", registerResp.UserID)
	})

	t.Run("rejects superuser self-deletion", func(t *testing.T) {
		// Create a test user
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@deletesuperuser.com'")
		registerBody := map[string]string{
			"username": "deletesuperuser",
			"email":    "user@deletesuperuser.com",
			"password": "securepassword123",
		}
		registerBytes, _ := json.Marshal(registerBody)
		registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
		registerReq.Header.Set("Content-Type", "application/json")
		registerRec := httptest.NewRecorder()
		handler.Register(registerRec, registerReq)

		var registerResp models.RegisterResponse
		_ = json.NewDecoder(registerRec.Body).Decode(&registerResp)

		// Make superuser
		_, _ = db.ExecContext(context.Background(), "UPDATE users SET mfa_setup_required = FALSE, mfa_enabled = FALSE, is_superuser = TRUE WHERE id = ?", registerResp.UserID)

		// Login
		loginBody := map[string]string{"email": "user@deletesuperuser.com", "password": "securepassword123"}
		loginBytes, _ := json.Marshal(loginBody)
		loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBytes))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		handler.Login(loginRec, loginReq)

		var loginResp models.LoginResponse
		_ = json.NewDecoder(loginRec.Body).Decode(&loginResp)

		// Try delete
		deleteBody := map[string]string{"password": "securepassword123"}
		deleteBytes, _ := json.Marshal(deleteBody)
		deleteReq := httptest.NewRequest("DELETE", "/api/v1/users/me", bytes.NewReader(deleteBytes))
		deleteReq.Header.Set("Content-Type", "application/json")
		deleteReq.Header.Set("Authorization", "Bearer "+loginResp.Token)
		deleteRec := httptest.NewRecorder()

		jwtAuth.Authenticate(http.HandlerFunc(handler.DeleteAccount)).ServeHTTP(deleteRec, deleteReq)

		if deleteRec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", deleteRec.Code, deleteRec.Body.String())
		}

		// Cleanup
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", registerResp.UserID)
	})

	t.Run("soft-deleted user cannot login", func(t *testing.T) {
		// Create a test user
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@deletedlogin.com'")
		registerBody := map[string]string{
			"username": "deletedlogin",
			"email":    "user@deletedlogin.com",
			"password": "securepassword123",
		}
		registerBytes, _ := json.Marshal(registerBody)
		registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
		registerReq.Header.Set("Content-Type", "application/json")
		registerRec := httptest.NewRecorder()
		handler.Register(registerRec, registerReq)

		var registerResp models.RegisterResponse
		_ = json.NewDecoder(registerRec.Body).Decode(&registerResp)

		// Soft-delete the user directly
		_, _ = db.ExecContext(context.Background(),
			"UPDATE users SET deleted_at = NOW(), mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?",
			registerResp.UserID)

		// Try to login
		loginBody := map[string]string{"email": "user@deletedlogin.com", "password": "securepassword123"}
		loginBytes, _ := json.Marshal(loginBody)
		loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBytes))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		handler.Login(loginRec, loginReq)

		if loginRec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d: %s", loginRec.Code, loginRec.Body.String())
		}

		// Cleanup
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", registerResp.UserID)
	})

	t.Run("preflight returns sole-admin warnings", func(t *testing.T) {
		// Create a test user
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@preflighttest.com'")
		registerBody := map[string]string{
			"username": "preflighttest",
			"email":    "user@preflighttest.com",
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

		// Login
		loginBody := map[string]string{"email": "user@preflighttest.com", "password": "securepassword123"}
		loginBytes, _ := json.Marshal(loginBody)
		loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBytes))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		handler.Login(loginRec, loginReq)

		var loginResp models.LoginResponse
		_ = json.NewDecoder(loginRec.Body).Decode(&loginResp)

		// Call preflight endpoint
		preflightReq := httptest.NewRequest("GET", "/api/v1/users/me/deletion-preflight", nil)
		preflightReq.Header.Set("Authorization", "Bearer "+loginResp.Token)
		preflightRec := httptest.NewRecorder()

		jwtAuth.Authenticate(http.HandlerFunc(handler.DeletionPreflight)).ServeHTTP(preflightRec, preflightReq)

		if preflightRec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", preflightRec.Code, preflightRec.Body.String())
		}

		var preflightResp map[string]interface{}
		_ = json.NewDecoder(preflightRec.Body).Decode(&preflightResp)

		// User has no regions, so warnings should be empty
		warnings, ok := preflightResp["warnings"].([]interface{})
		if !ok {
			t.Fatal("Expected warnings array in response")
		}
		if len(warnings) != 0 {
			t.Errorf("Expected 0 warnings for user with no regions, got %d", len(warnings))
		}

		// Cleanup
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", registerResp.UserID)
	})
}

// Helper functions for tests
func sha256Of(s string) [32]byte {
	return sha256.Sum256([]byte(s))
}

func timeInFuture(hours int) time.Time {
	return time.Now().UTC().Add(time.Duration(hours) * time.Hour)
}

func timeInPast(hours int) time.Time {
	return time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
}

// =============================================================================
// Blocked User Login Guard Tests
// =============================================================================

func TestAuthHandler_Login_BlockedUser(t *testing.T) {
	db := testDB(t)
	userRepo := database.NewUserRepository(db)
	jwtAuth := testJWTAuth()
	handler := NewAuthHandler(userRepo, jwtAuth)

	// Create a test user
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE email LIKE '%@blockedlogintest.com'")
	registerBody := map[string]string{
		"username": "blockedlogintest",
		"email":    "user@blockedlogintest.com",
		"password": "securepassword123",
	}
	registerBytes, _ := json.Marshal(registerBody)
	registerReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(registerBytes))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	handler.Register(registerRec, registerReq)

	var registerResp models.RegisterResponse
	_ = json.NewDecoder(registerRec.Body).Decode(&registerResp)

	// Block the user
	_, _ = db.ExecContext(context.Background(),
		"UPDATE users SET is_blocked = TRUE, blocked_at = NOW(), block_reason = 'test block' WHERE id = ?",
		registerResp.UserID)

	t.Run("blocked user cannot login", func(t *testing.T) {
		body := map[string]string{
			"email":    "user@blockedlogintest.com",
			"password": "securepassword123",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Login(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp["error"] != "account_blocked" {
			t.Errorf("Expected error 'account_blocked', got %v", resp["error"])
		}
	})

	t.Run("unblocked user can login again", func(t *testing.T) {
		// Unblock the user
		_, _ = db.ExecContext(context.Background(),
			"UPDATE users SET is_blocked = FALSE, blocked_at = NULL, block_reason = NULL, mfa_setup_required = FALSE, mfa_enabled = FALSE WHERE id = ?",
			registerResp.UserID)

		body := map[string]string{
			"email":    "user@blockedlogintest.com",
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

	// Cleanup
	_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", registerResp.UserID)
}
