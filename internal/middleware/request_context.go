package middleware

import (
	"context"
	"net/http"
)

// Use the existing contextKey type from auth.go
const (
	ipContextKey        contextKey = "client_ip"
	userAgentContextKey contextKey = "user_agent"
)

// RequestContext middleware extracts the client IP and user agent from the request
// and stores them in the context for use by downstream handlers and repositories.
func RequestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Extract client IP
		ip := GetClientIP(r)
		ctx = context.WithValue(ctx, ipContextKey, ip)

		// Extract user agent
		userAgent := r.Header.Get("User-Agent")
		ctx = context.WithValue(ctx, userAgentContextKey, userAgent)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetIPFromContext retrieves the client IP from the context
func GetIPFromContext(ctx context.Context) string {
	if ip, ok := ctx.Value(ipContextKey).(string); ok {
		return ip
	}
	return ""
}

// GetUserAgentFromContext retrieves the user agent from the context
func GetUserAgentFromContext(ctx context.Context) string {
	if ua, ok := ctx.Value(userAgentContextKey).(string); ok {
		return ua
	}
	return ""
}
