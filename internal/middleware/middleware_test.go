package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogger(t *testing.T) {
	handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestRecoverer(t *testing.T) {
	t.Run("recovers from panic", func(t *testing.T) {
		handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("test panic")
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()

		// Should not panic
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rec.Code)
		}
	})

	t.Run("passes through normal requests", func(t *testing.T) {
		handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})
}

func TestCORS(t *testing.T) {
	t.Run("adds CORS headers for allowed origin", func(t *testing.T) {
		handler := CORS([]string{"https://example.com"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
			t.Error("Expected Access-Control-Allow-Origin header")
		}
	})

	t.Run("wildcard responds with literal * and no credentials", func(t *testing.T) {
		handler := CORS([]string{"*"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "https://any-origin.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("Wildcard must respond with literal '*', got %q", rec.Header().Get("Access-Control-Allow-Origin"))
		}
		if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Error("Wildcard must not set Access-Control-Allow-Credentials (browsers reject the combination)")
		}
	})

	t.Run("exact-match origin sets credentials", func(t *testing.T) {
		handler := CORS([]string{"https://example.com"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Error("Exact-match origin must enable credentials")
		}
	})

	t.Run("does not add header for disallowed origin", func(t *testing.T) {
		handler := CORS([]string{"https://allowed.com"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "https://not-allowed.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("Should not add header for disallowed origin")
		}
	})

	t.Run("empty origins denies all cross-origin requests", func(t *testing.T) {
		handler := CORS(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("Should not add CORS headers when origins list is empty")
		}
	})

	t.Run("handles preflight OPTIONS request", func(t *testing.T) {
		handler := CORS([]string{"*"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("Handler should not be called for OPTIONS")
		}))

		req := httptest.NewRequest("OPTIONS", "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200 for OPTIONS, got %d", rec.Code)
		}
	})
}

func TestMaxBodySize(t *testing.T) {
	t.Run("allows body within limit", func(t *testing.T) {
		handler := MaxBodySize(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body := make([]byte, 512)
			_, err := r.Body.Read(body)
			if err != nil && err.Error() == "http: request body too large" {
				t.Error("Body should be within limit")
			}
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("POST", "/test", strings.NewReader(strings.Repeat("a", 512)))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})

	t.Run("rejects body exceeding limit", func(t *testing.T) {
		handler := MaxBodySize(64)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body := make([]byte, 128)
			_, err := r.Body.Read(body)
			if err == nil {
				t.Error("Expected error reading oversized body")
			}
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
		}))

		req := httptest.NewRequest("POST", "/test", strings.NewReader(strings.Repeat("a", 128)))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("Expected status 413, got %d", rec.Code)
		}
	})

	t.Run("nil body passes through", func(t *testing.T) {
		handler := MaxBodySize(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})
}

func TestContentType(t *testing.T) {
	handler := ContentType(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	expectedHeaders := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"X-XSS-Protection":       "1; mode=block",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}

	if pp := rec.Header().Get("Permissions-Policy"); pp == "" {
		t.Error("Expected Permissions-Policy header to be set")
	}

	for header, expectedValue := range expectedHeaders {
		actualValue := rec.Header().Get(header)
		if actualValue != expectedValue {
			t.Errorf("Expected %s: '%s', got '%s'", header, expectedValue, actualValue)
		}
	}

	// nil config should NOT set CSP or HSTS
	if rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("nil config should not set Content-Security-Policy")
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("nil config should not set Strict-Transport-Security")
	}
}

func TestSecurityHeaders_WithCSP(t *testing.T) {
	cspDirectives := "default-src 'self'; script-src 'self' https://api.mapbox.com"
	cfg := &SecurityConfig{
		SecureCookies: false,
		CSPDirectives: cspDirectives,
	}

	handler := SecurityHeadersWithConfig(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	actualCSP := rec.Header().Get("Content-Security-Policy")
	if actualCSP != cspDirectives {
		t.Errorf("Expected Content-Security-Policy '%s', got '%s'", cspDirectives, actualCSP)
	}

	// Base headers should still be present
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("Expected X-Content-Type-Options: nosniff")
	}
}

func TestSecurityHeaders_HSTS_OnlyWhenSecure(t *testing.T) {
	t.Run("HSTS present when SecureCookies=true", func(t *testing.T) {
		cfg := &SecurityConfig{
			SecureCookies: true,
			CSPDirectives: "default-src 'self'",
		}

		handler := SecurityHeadersWithConfig(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		hsts := rec.Header().Get("Strict-Transport-Security")
		if hsts != "max-age=31536000; includeSubDomains" {
			t.Errorf("Expected HSTS header, got '%s'", hsts)
		}
	})

	t.Run("HSTS absent when SecureCookies=false", func(t *testing.T) {
		cfg := &SecurityConfig{
			SecureCookies: false,
			CSPDirectives: "default-src 'self'",
		}

		handler := SecurityHeadersWithConfig(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		hsts := rec.Header().Get("Strict-Transport-Security")
		if hsts != "" {
			t.Errorf("Expected no HSTS header when SecureCookies=false, got '%s'", hsts)
		}
	})
}

func TestResponseWriter(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		wrapped.WriteHeader(http.StatusCreated)
		_, _ = wrapped.Write([]byte("test"))

		if wrapped.status != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", wrapped.status)
		}
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "test") {
		t.Error("Expected body to contain 'test'")
	}
}
