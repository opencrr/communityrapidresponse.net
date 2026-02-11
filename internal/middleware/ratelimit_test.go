package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockRateLimiter is a test implementation of RateLimiter
type mockRateLimiter struct {
	allowFunc func(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Time, error)
}

func (m *mockRateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
	if m.allowFunc != nil {
		return m.allowFunc(ctx, key, limit, window)
	}
	return true, limit, time.Now().Add(window), nil
}

func (m *mockRateLimiter) Reset(_ context.Context, _ string) error {
	return nil
}

func (m *mockRateLimiter) Cleanup(_ context.Context, _ time.Duration) error {
	return nil
}

func TestRateLimitMiddleware_AllowsRequests(t *testing.T) {
	limiter := &mockRateLimiter{
		allowFunc: func(_ context.Context, _ string, limit int, window time.Duration) (bool, int, time.Time, error) {
			return true, 99, time.Now().Add(window), nil
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	middleware := RateLimitMiddleware(limiter, 100, time.Minute)
	wrappedHandler := middleware(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	// Check rate limit headers
	if rec.Header().Get("X-RateLimit-Limit") != "100" {
		t.Errorf("Expected X-RateLimit-Limit=100, got %s", rec.Header().Get("X-RateLimit-Limit"))
	}
	if rec.Header().Get("X-RateLimit-Remaining") != "99" {
		t.Errorf("Expected X-RateLimit-Remaining=99, got %s", rec.Header().Get("X-RateLimit-Remaining"))
	}
	if rec.Header().Get("X-RateLimit-Reset") == "" {
		t.Error("Expected X-RateLimit-Reset header to be set")
	}
}

func TestRateLimitMiddleware_BlocksExcessRequests(t *testing.T) {
	resetTime := time.Now().Add(30 * time.Second)
	limiter := &mockRateLimiter{
		allowFunc: func(_ context.Context, _ string, _ int, _ time.Duration) (bool, int, time.Time, error) {
			return false, 0, resetTime, nil
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called when rate limited")
	})

	middleware := RateLimitMiddleware(limiter, 100, time.Minute)
	wrappedHandler := middleware(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429, got %d", rec.Code)
	}

	// Check Retry-After header
	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("Expected Retry-After header to be set")
	}

	// Check response body
	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}
	if body["error"] != "rate_limit_exceeded" {
		t.Errorf("Expected error=rate_limit_exceeded, got %v", body["error"])
	}
}

func TestGetClientIP_NoTrustedProxies(t *testing.T) {
	// Clear trusted proxies — should always return RemoteAddr, ignoring headers
	SetTrustedProxies(nil)

	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		expectedIP string
	}{
		{
			name:       "returns RemoteAddr when no headers",
			remoteAddr: "192.168.1.1:12345",
			headers:    nil,
			expectedIP: "192.168.1.1",
		},
		{
			name:       "ignores X-Forwarded-For without trusted proxies",
			remoteAddr: "192.168.1.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.195, 70.41.3.18"},
			expectedIP: "192.168.1.1",
		},
		{
			name:       "ignores X-Real-IP without trusted proxies",
			remoteAddr: "192.168.1.1:12345",
			headers:    map[string]string{"X-Real-IP": "203.0.113.195"},
			expectedIP: "192.168.1.1",
		},
		{
			name:       "handles RemoteAddr without port",
			remoteAddr: "192.168.1.1",
			headers:    nil,
			expectedIP: "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			ip := GetClientIP(req)
			if ip != tt.expectedIP {
				t.Errorf("Expected IP %s, got %s", tt.expectedIP, ip)
			}
		})
	}
}

func TestGetClientIP_WithTrustedProxies(t *testing.T) {
	// Set up trusted proxy range: 10.0.0.0/8 and 127.0.0.1/32
	SetTrustedProxies([]string{"10.0.0.0/8", "127.0.0.1/32"})
	defer SetTrustedProxies(nil) // cleanup

	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		expectedIP string
	}{
		{
			name:       "extracts rightmost untrusted IP from XFF",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.50, 10.0.0.5"},
			expectedIP: "203.0.113.50",
		},
		{
			name:       "single client IP in XFF",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.50"},
			expectedIP: "203.0.113.50",
		},
		{
			name:       "multi-hop chain returns rightmost untrusted",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "1.2.3.4, 203.0.113.50, 10.0.0.5"},
			expectedIP: "203.0.113.50",
		},
		{
			name:       "spoofed XFF from untrusted source returns RemoteAddr",
			remoteAddr: "203.0.113.99:12345",
			headers:    map[string]string{"X-Forwarded-For": "1.2.3.4, 5.6.7.8"},
			expectedIP: "203.0.113.99",
		},
		{
			name:       "X-Real-IP trusted source with no XFF",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Real-IP": "203.0.113.50"},
			expectedIP: "203.0.113.50",
		},
		{
			name:       "X-Real-IP ignored from untrusted source",
			remoteAddr: "203.0.113.99:12345",
			headers:    map[string]string{"X-Real-IP": "1.2.3.4"},
			expectedIP: "203.0.113.99",
		},
		{
			name:       "all XFF IPs are trusted proxies falls back to RemoteAddr",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "10.0.0.2, 10.0.0.3"},
			expectedIP: "10.0.0.1",
		},
		{
			name:       "empty XFF falls back to RemoteAddr from trusted proxy",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": ""},
			expectedIP: "10.0.0.1",
		},
		{
			name:       "localhost trusted proxy extracts client IP",
			remoteAddr: "127.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.50"},
			expectedIP: "203.0.113.50",
		},
		{
			name:       "no headers from trusted proxy returns RemoteAddr",
			remoteAddr: "10.0.0.1:12345",
			headers:    nil,
			expectedIP: "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			ip := GetClientIP(req)
			if ip != tt.expectedIP {
				t.Errorf("Expected IP %s, got %s", tt.expectedIP, ip)
			}
		})
	}
}

func TestSetTrustedProxies(t *testing.T) {
	// Invalid CIDRs should be silently skipped
	SetTrustedProxies([]string{"not-a-cidr", "", "10.0.0.0/8"})
	defer SetTrustedProxies(nil)

	// Should have parsed one valid CIDR
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	ip := GetClientIP(req)
	if ip != "203.0.113.50" {
		t.Errorf("Expected 203.0.113.50, got %s", ip)
	}
}

func TestRateLimitKey(t *testing.T) {
	key := RateLimitKey("user-123", "vouch")
	expected := "user:user-123:vouch"
	if key != expected {
		t.Errorf("Expected key %s, got %s", expected, key)
	}
}
