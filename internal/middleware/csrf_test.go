package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const testCSRFSecret = "test-csrf-secret-key-for-testing"

// testCSRFDerivedSecret is the derived key that the middleware uses internally.
// Tests that manually create tokens for middleware validation must use this.
var testCSRFDerivedSecret = deriveCSRFSecret(testCSRFSecret)

func TestCSRFProtection_SafeMethodsPassThrough(t *testing.T) {
	handler := CSRFProtection(testCSRFSecret, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		req := httptest.NewRequest(method, "/api/v1/regions", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", method, rr.Code)
		}
	}
}

func TestCSRFProtection_SetsCookieOnGET(t *testing.T) {
	handler := CSRFProtection(testCSRFSecret, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/regions", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	cookies := rr.Result().Cookies()
	var csrfCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == csrfCookieName {
			csrfCookie = c
			break
		}
	}

	if csrfCookie == nil {
		t.Fatal("expected csrf_token cookie to be set")
	}

	if csrfCookie.HttpOnly {
		t.Error("csrf_token cookie should NOT be HttpOnly (JS must read it)")
	}

	if csrfCookie.Path != "/" {
		t.Errorf("expected cookie path '/', got '%s'", csrfCookie.Path)
	}

	if csrfCookie.MaxAge != csrfCookieMaxAge {
		t.Errorf("expected MaxAge %d, got %d", csrfCookieMaxAge, csrfCookie.MaxAge)
	}

	if !verifyCSRFToken(csrfCookie.Value, testCSRFDerivedSecret) {
		t.Error("csrf_token cookie value should have valid HMAC signature")
	}
}

func TestCSRFProtection_DoesNotOverwriteExistingCookie(t *testing.T) {
	handler := CSRFProtection(testCSRFSecret, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	existingToken, _ := generateCSRFToken(testCSRFDerivedSecret)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/regions", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: existingToken})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	cookies := rr.Result().Cookies()
	for _, c := range cookies {
		if c.Name == csrfCookieName {
			t.Error("should not set a new csrf_token cookie when one already exists")
		}
	}
}

func TestCSRFProtection_ValidTokenPassesPOST(t *testing.T) {
	handler := CSRFProtection(testCSRFSecret, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	token, _ := generateCSRFToken(testCSRFDerivedSecret)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/regions", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	req.Header.Set(csrfHeaderName, token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCSRFProtection_MissingHeader403(t *testing.T) {
	handler := CSRFProtection(testCSRFSecret, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	token, _ := generateCSRFToken(testCSRFDerivedSecret)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/regions", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	// No X-CSRF-Token header
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestCSRFProtection_MissingCookie403(t *testing.T) {
	handler := CSRFProtection(testCSRFSecret, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	token, _ := generateCSRFToken(testCSRFDerivedSecret)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/regions", nil)
	// No cookie
	req.Header.Set(csrfHeaderName, token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestCSRFProtection_MismatchedTokens403(t *testing.T) {
	handler := CSRFProtection(testCSRFSecret, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cookieToken, _ := generateCSRFToken(testCSRFDerivedSecret)
	headerToken, _ := generateCSRFToken(testCSRFDerivedSecret)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/regions", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: cookieToken})
	req.Header.Set(csrfHeaderName, headerToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestCSRFProtection_TamperedSignature403(t *testing.T) {
	handler := CSRFProtection(testCSRFSecret, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	token, _ := generateCSRFToken(testCSRFDerivedSecret)
	tamperedToken := token + "tampered"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/regions", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: tamperedToken})
	req.Header.Set(csrfHeaderName, tamperedToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestCSRFProtection_ExemptPathsBypass(t *testing.T) {
	handler := CSRFProtection(testCSRFSecret, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	exemptPaths := []string{
		"/api/v1/auth/login",
		"/api/v1/auth/register",
		"/api/v1/auth/logout",
		"/api/v1/auth/forgot-password",
		"/api/v1/auth/reset-password",
		"/api/v1/auth/verify-email",
		"/api/v1/mfa/setup",
		"/api/v1/mfa/setup/complete",
		"/api/v1/mfa/verify",
	}

	for _, path := range exemptPaths {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		// No cookie or header — would fail if not exempt
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("exempt path %s: expected 200, got %d", path, rr.Code)
		}
	}
}

func TestCSRFProtection_NonExemptPathRequiresToken(t *testing.T) {
	handler := CSRFProtection(testCSRFSecret, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	nonExemptPaths := []string{
		"/api/v1/regions",
		"/api/v1/signal-groups",
		"/api/v1/verification/postcard/request",
		"/api/v1/deletion-proposals",
		"/api/v1/auth/change-password",
		"/api/v1/auth/resend-verification",
	}

	for _, path := range nonExemptPaths {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("non-exempt path %s: expected 403, got %d", path, rr.Code)
		}
	}
}

func TestCSRFProtection_SecureCookieFlag(t *testing.T) {
	handler := CSRFProtection(testCSRFSecret, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/regions", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	cookies := rr.Result().Cookies()
	var csrfCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == csrfCookieName {
			csrfCookie = c
			break
		}
	}

	if csrfCookie == nil {
		t.Fatal("expected csrf_token cookie to be set")
	}

	if !csrfCookie.Secure {
		t.Error("expected Secure flag to be true when secureCookies=true")
	}
}

func TestCSRFProtection_UnsafeMethods(t *testing.T) {
	handler := CSRFProtection(testCSRFSecret, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/v1/regions", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("%s without token: expected 403, got %d", method, rr.Code)
		}
	}
}

func TestCSRFProtection_ValidTokenAllMethods(t *testing.T) {
	handler := CSRFProtection(testCSRFSecret, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		token, _ := generateCSRFToken(testCSRFDerivedSecret)
		req := httptest.NewRequest(method, "/api/v1/regions", nil)
		req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
		req.Header.Set(csrfHeaderName, token)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("%s with valid token: expected 200, got %d", method, rr.Code)
		}
	}
}

func TestVerifyCSRFToken_InvalidFormat(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"empty string", ""},
		{"no dot separator", "abcdef"},
		{"wrong secret", ""},
	}

	for _, tt := range tests {
		if verifyCSRFToken(tt.token, testCSRFSecret) {
			t.Errorf("%s: expected false, got true", tt.name)
		}
	}

	// Token signed with different secret
	token, _ := generateCSRFToken("different-secret")
	if verifyCSRFToken(token, testCSRFSecret) {
		t.Error("token signed with different secret should not verify")
	}
}

func TestDeriveCSRFSecret_DifferentFromInput(t *testing.T) {
	derived := deriveCSRFSecret(testCSRFSecret)
	if derived == testCSRFSecret {
		t.Error("derived secret should differ from the input secret")
	}
}

func TestDeriveCSRFSecret_Deterministic(t *testing.T) {
	d1 := deriveCSRFSecret(testCSRFSecret)
	d2 := deriveCSRFSecret(testCSRFSecret)
	if d1 != d2 {
		t.Error("same input should produce same derived secret")
	}
}

func TestDeriveCSRFSecret_DifferentInputsDifferentOutputs(t *testing.T) {
	d1 := deriveCSRFSecret("secret-a")
	d2 := deriveCSRFSecret("secret-b")
	if d1 == d2 {
		t.Error("different inputs should produce different derived secrets")
	}
}

func TestGenerateCSRFToken_UniqueTokens(t *testing.T) {
	token1, err1 := generateCSRFToken(testCSRFSecret)
	token2, err2 := generateCSRFToken(testCSRFSecret)

	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}

	if token1 == token2 {
		t.Error("generated tokens should be unique")
	}
}
