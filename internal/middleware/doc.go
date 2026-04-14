// Package middleware provides HTTP middleware for authentication, security,
// and request lifecycle management.
//
// [JWTAuth] handles token generation and validation with support for
// multiple token types (full, mfa_setup, pending_mfa). [Auth] extracts
// and validates JWT claims on protected routes; [GetUserFromContext]
// retrieves the authenticated [Claims] from the request context.
//
// Additional middleware includes [RateLimit] (per-IP request throttling),
// [CSRFProtection] (double-submit cookie pattern), [CORS], [Security]
// (response headers), [Logger] (structured request logging with request
// IDs), [Recoverer] (panic recovery), and [MaxBodySize].
package middleware
