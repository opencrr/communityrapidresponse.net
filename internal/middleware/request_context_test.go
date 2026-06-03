package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestContext_PopulatesIPAndUserAgent(t *testing.T) {
	SetTrustedProxies(nil)

	var gotIP, gotUA string
	var gotCtx context.Context

	handler := RequestContext(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		gotCtx = ctx
		gotIP = GetIPFromContext(ctx)
		gotUA = GetUserAgentFromContext(ctx)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.5:54321"
	req.Header.Set("User-Agent", "test-agent/1.0")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotIP != "203.0.113.5" {
		t.Errorf("client_ip = %q, want %q", gotIP, "203.0.113.5")
	}
	if gotUA != "test-agent/1.0" {
		t.Errorf("user_agent = %q, want %q", gotUA, "test-agent/1.0")
	}
	if gotCtx == nil {
		t.Fatal("downstream context was nil")
	}
}

func TestRequestContext_EmptyUserAgent(t *testing.T) {
	SetTrustedProxies(nil)

	var gotUA string
	handler := RequestContext(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = GetUserAgentFromContext(r.Context())
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Del("User-Agent")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotUA != "" {
		t.Errorf("user_agent = %q, want empty string", gotUA)
	}
}

func TestRequestContext_PropagatesParentContextValues(t *testing.T) {
	SetTrustedProxies(nil)

	type parentKey struct{}
	var sawParent any
	var sawIP string

	handler := RequestContext(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawParent = r.Context().Value(parentKey{})
		sawIP = GetIPFromContext(r.Context())
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "198.51.100.7:8080"
	req.Header.Set("User-Agent", "ua")
	req = req.WithContext(context.WithValue(req.Context(), parentKey{}, "preserved"))

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if sawParent != "preserved" {
		t.Errorf("parent context value = %v, want \"preserved\"", sawParent)
	}
	if sawIP != "198.51.100.7" {
		t.Errorf("client_ip = %q, want %q", sawIP, "198.51.100.7")
	}
}

func TestGetIPFromContext_Missing(t *testing.T) {
	if got := GetIPFromContext(context.Background()); got != "" {
		t.Errorf("GetIPFromContext on bare ctx = %q, want \"\"", got)
	}
}

func TestGetIPFromContext_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), ipContextKey, 12345)
	if got := GetIPFromContext(ctx); got != "" {
		t.Errorf("GetIPFromContext with non-string value = %q, want \"\"", got)
	}
}

func TestGetUserAgentFromContext_Missing(t *testing.T) {
	if got := GetUserAgentFromContext(context.Background()); got != "" {
		t.Errorf("GetUserAgentFromContext on bare ctx = %q, want \"\"", got)
	}
}

func TestGetUserAgentFromContext_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), userAgentContextKey, struct{}{})
	if got := GetUserAgentFromContext(ctx); got != "" {
		t.Errorf("GetUserAgentFromContext with non-string value = %q, want \"\"", got)
	}
}
