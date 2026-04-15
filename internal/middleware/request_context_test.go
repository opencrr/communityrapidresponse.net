package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestContextMiddleware(t *testing.T) {
	t.Run("sets IP and user agent in context", func(t *testing.T) {
		var gotIP, gotUA string

		handler := RequestContext(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotIP = GetIPFromContext(r.Context())
			gotUA = GetUserAgentFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("User-Agent", "TestBrowser/1.0")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		if gotIP != "192.168.1.100" {
			t.Errorf("expected IP 192.168.1.100, got %q", gotIP)
		}
		if gotUA != "TestBrowser/1.0" {
			t.Errorf("expected user agent TestBrowser/1.0, got %q", gotUA)
		}
	})

	t.Run("empty user agent", func(t *testing.T) {
		var gotUA string

		handler := RequestContext(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUA = GetUserAgentFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Del("User-Agent")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if gotUA != "" {
			t.Errorf("expected empty user agent, got %q", gotUA)
		}
	})

	t.Run("passes request to next handler", func(t *testing.T) {
		called := false
		handler := RequestContext(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if !called {
			t.Error("next handler was not called")
		}
	})
}

func TestGetIPFromContext(t *testing.T) {
	t.Run("returns IP when set", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ipContextKey, "10.0.0.1")
		if got := GetIPFromContext(ctx); got != "10.0.0.1" {
			t.Errorf("expected 10.0.0.1, got %q", got)
		}
	})

	t.Run("returns empty string for missing value", func(t *testing.T) {
		if got := GetIPFromContext(context.Background()); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("returns empty string for wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ipContextKey, 12345)
		if got := GetIPFromContext(ctx); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
}

func TestGetUserAgentFromContext(t *testing.T) {
	t.Run("returns user agent when set", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), userAgentContextKey, "Mozilla/5.0")
		if got := GetUserAgentFromContext(ctx); got != "Mozilla/5.0" {
			t.Errorf("expected Mozilla/5.0, got %q", got)
		}
	})

	t.Run("returns empty string for missing value", func(t *testing.T) {
		if got := GetUserAgentFromContext(context.Background()); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("returns empty string for wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), userAgentContextKey, 12345)
		if got := GetUserAgentFromContext(ctx); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
}
