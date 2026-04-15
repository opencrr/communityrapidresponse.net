package middleware

import "context"

// TestContextWithIPAndUA creates a context with IP and user agent values set,
// for use in tests outside this package.
func TestContextWithIPAndUA(ctx context.Context, ip, ua string) context.Context {
	if ip != "" {
		ctx = context.WithValue(ctx, ipContextKey, ip)
	}
	if ua != "" {
		ctx = context.WithValue(ctx, userAgentContextKey, ua)
	}
	return ctx
}
