package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

func testJWTConfig() *config.JWTConfig {
	return &config.JWTConfig{
		Secret:          "test_secret_key_at_least_32_characters",
		ExpirationHours: 24,
		Issuer:          "test_issuer",
	}
}

func TestJWTAuth_GenerateToken(t *testing.T) {
	auth := NewJWTAuth(testJWTConfig())

	user := &models.User{
		ID:               "test-user-id",
		Username:         "testuser",
		Email:            "test@example.com",
		VerificationTier: models.TierPostcard,
	}

	token, err := auth.GenerateToken(user)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	if token == "" {
		t.Error("Expected non-empty token")
	}
}

func TestJWTAuth_ValidateToken(t *testing.T) {
	auth := NewJWTAuth(testJWTConfig())

	user := &models.User{
		ID:               "test-user-id",
		Username:         "testuser",
		Email:            "test@example.com",
		VerificationTier: models.TierPostcard,
	}

	token, err := auth.GenerateToken(user)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	t.Run("validates valid token", func(t *testing.T) {
		claims, err := auth.ValidateToken(token)
		if err != nil {
			t.Fatalf("Failed to validate token: %v", err)
		}

		if claims.UserID != user.ID {
			t.Errorf("Expected user ID '%s', got '%s'", user.ID, claims.UserID)
		}
		if claims.Username != user.Username {
			t.Errorf("Expected username '%s', got '%s'", user.Username, claims.Username)
		}
		if claims.Email != user.Email {
			t.Errorf("Expected email '%s', got '%s'", user.Email, claims.Email)
		}
		if claims.VerificationTier != user.VerificationTier {
			t.Errorf("Expected tier %d, got %d", user.VerificationTier, claims.VerificationTier)
		}
	})

	t.Run("rejects invalid token", func(t *testing.T) {
		_, err := auth.ValidateToken("invalid.token.here")
		if err == nil {
			t.Error("Expected error for invalid token")
		}
	})

	t.Run("rejects token with wrong secret", func(t *testing.T) {
		wrongAuth := NewJWTAuth(&config.JWTConfig{
			Secret:          "different_secret_key_at_least_32_chars",
			ExpirationHours: 24,
			Issuer:          "test_issuer",
		})

		_, err := wrongAuth.ValidateToken(token)
		if err == nil {
			t.Error("Expected error for token with wrong secret")
		}
	})
}

func TestJWTAuth_Authenticate(t *testing.T) {
	auth := NewJWTAuth(testJWTConfig())

	user := &models.User{
		ID:               "test-user-id",
		Username:         "testuser",
		Email:            "test@example.com",
		VerificationTier: models.TierPostcard,
	}

	token, _ := auth.GenerateToken(user)

	t.Run("allows request with valid token", func(t *testing.T) {
		handler := auth.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserFromContext(r.Context())
			if claims == nil {
				t.Error("Expected claims in context")
				return
			}
			if claims.UserID != user.ID {
				t.Errorf("Expected user ID '%s', got '%s'", user.ID, claims.UserID)
			}
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})

	t.Run("rejects request without token", func(t *testing.T) {
		handler := auth.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("Handler should not be called")
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})

	t.Run("rejects request with invalid token", func(t *testing.T) {
		handler := auth.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("Handler should not be called")
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer invalid_token")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rec.Code)
		}
	})

	t.Run("reads token from cookie", func(t *testing.T) {
		handler := auth.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserFromContext(r.Context())
			if claims == nil {
				t.Error("Expected claims in context")
				return
			}
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{Name: "token", Value: token})
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})
}

func TestRequireTier(t *testing.T) {
	auth := NewJWTAuth(testJWTConfig())

	createUserWithTier := func(tier models.VerificationTier) string {
		user := &models.User{
			ID:               "test-user-id",
			Username:         "testuser",
			Email:            "test@example.com",
			VerificationTier: tier,
		}
		token, _ := auth.GenerateToken(user)
		return token
	}

	t.Run("allows access for sufficient tier", func(t *testing.T) {
		token := createUserWithTier(models.TierPostcard)

		handler := auth.Authenticate(RequireTier(models.TierPostcard)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})

	t.Run("denies access for insufficient tier", func(t *testing.T) {
		token := createUserWithTier(models.TierUnverified)

		handler := auth.Authenticate(RequireTier(models.TierPostcard)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("Handler should not be called")
		})))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})
}

func TestExtractToken(t *testing.T) {
	t.Run("extracts from Authorization header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer my_token")

		token := extractToken(req)
		if token != "my_token" {
			t.Errorf("Expected 'my_token', got '%s'", token)
		}
	})

	t.Run("extracts from cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{Name: "token", Value: "cookie_token"})

		token := extractToken(req)
		if token != "cookie_token" {
			t.Errorf("Expected 'cookie_token', got '%s'", token)
		}
	})

	t.Run("prefers header over cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer header_token")
		req.AddCookie(&http.Cookie{Name: "token", Value: "cookie_token"})

		token := extractToken(req)
		if token != "header_token" {
			t.Errorf("Expected 'header_token', got '%s'", token)
		}
	})

	t.Run("returns empty for missing token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)

		token := extractToken(req)
		if token != "" {
			t.Errorf("Expected empty string, got '%s'", token)
		}
	})
}

func TestJWTAuth_GenerateTokenWithType(t *testing.T) {
	auth := NewJWTAuth(testJWTConfig())

	user := &models.User{
		ID:               "test-user-id",
		Username:         "testuser",
		Email:            "test@example.com",
		VerificationTier: models.TierPostcard,
	}

	t.Run("generates full token", func(t *testing.T) {
		token, err := auth.GenerateTokenWithType(user, TokenTypeFull)
		if err != nil {
			t.Fatalf("Failed to generate token: %v", err)
		}

		claims, _ := auth.ValidateToken(token)
		if claims.TokenType != TokenTypeFull {
			t.Errorf("Expected token type 'full', got '%s'", claims.TokenType)
		}
	})

	t.Run("generates MFA setup token", func(t *testing.T) {
		token, err := auth.GenerateTokenWithType(user, TokenTypeMFASetup)
		if err != nil {
			t.Fatalf("Failed to generate token: %v", err)
		}

		claims, _ := auth.ValidateToken(token)
		if claims.TokenType != TokenTypeMFASetup {
			t.Errorf("Expected token type 'mfa_setup', got '%s'", claims.TokenType)
		}
	})

	t.Run("generates pending MFA token", func(t *testing.T) {
		token, err := auth.GenerateTokenWithType(user, TokenTypePendingMFA)
		if err != nil {
			t.Fatalf("Failed to generate token: %v", err)
		}

		claims, _ := auth.ValidateToken(token)
		if claims.TokenType != TokenTypePendingMFA {
			t.Errorf("Expected token type 'pending_mfa', got '%s'", claims.TokenType)
		}
	})

	t.Run("MFA tokens have shorter expiration", func(t *testing.T) {
		fullToken, _ := auth.GenerateTokenWithType(user, TokenTypeFull)
		mfaToken, _ := auth.GenerateTokenWithType(user, TokenTypeMFASetup)

		fullClaims, _ := auth.ValidateToken(fullToken)
		mfaClaims, _ := auth.ValidateToken(mfaToken)

		fullExpiry := fullClaims.ExpiresAt.Time
		mfaExpiry := mfaClaims.ExpiresAt.Time

		// MFA token should expire much sooner than full token
		if !mfaExpiry.Before(fullExpiry) {
			t.Error("MFA token should expire before full token")
		}
	})
}

func TestJWTAuth_AuthenticateWithTypes(t *testing.T) {
	auth := NewJWTAuth(testJWTConfig())

	user := &models.User{
		ID:       "test-user-id",
		Username: "testuser",
		Email:    "test@example.com",
	}

	fullToken, _ := auth.GenerateTokenWithType(user, TokenTypeFull)
	mfaSetupToken, _ := auth.GenerateTokenWithType(user, TokenTypeMFASetup)
	pendingMFAToken, _ := auth.GenerateTokenWithType(user, TokenTypePendingMFA)

	t.Run("allows matching token type", func(t *testing.T) {
		handler := auth.AuthenticateWithTypes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}), TokenTypeMFASetup)

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+mfaSetupToken)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})

	t.Run("rejects mismatched token type", func(t *testing.T) {
		handler := auth.AuthenticateWithTypes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("Handler should not be called")
		}), TokenTypeMFASetup)

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+fullToken)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
	})

	t.Run("allows multiple token types", func(t *testing.T) {
		handler := auth.AuthenticateWithTypes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}), TokenTypeFull, TokenTypeMFASetup)

		// Test with full token
		req1 := httptest.NewRequest("GET", "/test", nil)
		req1.Header.Set("Authorization", "Bearer "+fullToken)
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Errorf("Expected status 200 for full token, got %d", rec1.Code)
		}

		// Test with MFA setup token
		req2 := httptest.NewRequest("GET", "/test", nil)
		req2.Header.Set("Authorization", "Bearer "+mfaSetupToken)
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Errorf("Expected status 200 for MFA setup token, got %d", rec2.Code)
		}
	})

	t.Run("default Authenticate only allows full tokens", func(t *testing.T) {
		handler := auth.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		// Full token should work
		req1 := httptest.NewRequest("GET", "/test", nil)
		req1.Header.Set("Authorization", "Bearer "+fullToken)
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Errorf("Expected status 200 for full token, got %d", rec1.Code)
		}

		// MFA setup token should be rejected
		req2 := httptest.NewRequest("GET", "/test", nil)
		req2.Header.Set("Authorization", "Bearer "+mfaSetupToken)
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusForbidden {
			t.Errorf("Expected status 403 for MFA setup token, got %d", rec2.Code)
		}

		// Pending MFA token should be rejected
		req3 := httptest.NewRequest("GET", "/test", nil)
		req3.Header.Set("Authorization", "Bearer "+pendingMFAToken)
		rec3 := httptest.NewRecorder()
		handler.ServeHTTP(rec3, req3)
		if rec3.Code != http.StatusForbidden {
			t.Errorf("Expected status 403 for pending MFA token, got %d", rec3.Code)
		}
	})

	t.Run("returns appropriate error message for MFA tokens", func(t *testing.T) {
		handler := auth.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("Handler should not be called")
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+mfaSetupToken)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		body := rec.Body.String()
		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rec.Code)
		}
		if body == "" {
			t.Error("Expected error message in response body")
		}
	})
}

func TestTokenExpiration(t *testing.T) {
	// Create auth with 0 hour expiration (already expired)
	auth := NewJWTAuth(&config.JWTConfig{
		Secret:          "test_secret_key_at_least_32_characters",
		ExpirationHours: 0,
		Issuer:          "test_issuer",
	})

	user := &models.User{
		ID:       "test-user-id",
		Username: "testuser",
		Email:    "test@example.com",
	}

	token, _ := auth.GenerateToken(user)

	// Wait a moment to ensure expiration
	time.Sleep(time.Second)

	_, err := auth.ValidateToken(token)
	if err != ErrExpiredToken {
		t.Errorf("Expected ErrExpiredToken, got %v", err)
	}
}
