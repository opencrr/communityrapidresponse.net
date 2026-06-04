package middleware

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// RateLimitMiddleware creates middleware that rate limits requests by IP
func RateLimitMiddleware(limiter services.RateLimiter, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := GetClientIP(r)
			ctx := r.Context()

			allowed, remaining, resetTime, err := limiter.Allow(ctx, ip, limit, window)
			if err != nil {
				// On error, allow the request but log it
				// This prevents rate limiting failures from breaking the API
				_ = appSentry.CaptureError(err, "component", "rate_limiter", "operation", "check")
				next.ServeHTTP(w, r)
				return
			}

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))

			if !allowed {
				appSentry.IncrementCounter("http.rate_limited", 1)
				retryAfter := int(time.Until(resetTime).Seconds())
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)

				response := map[string]interface{}{
					"error":       "rate_limit_exceeded",
					"message":     "Too many requests. Please slow down.",
					"retry_after": retryAfter,
				}
				_ = json.NewEncoder(w).Encode(response)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// trustedNets holds parsed CIDR ranges of trusted reverse proxies.
// When empty, forwarding headers (X-Forwarded-For, X-Real-IP) are never trusted
// and GetClientIP always returns RemoteAddr.
var trustedNets []*net.IPNet

// SetTrustedProxies parses CIDR strings and stores them for use by GetClientIP.
// Call this once at startup. Invalid CIDRs are silently skipped.
func SetTrustedProxies(cidrs []string) {
	var nets []*net.IPNet
	for _, cidr := range cidrs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		nets = append(nets, ipNet)
	}
	trustedNets = nets
}

// isTrustedProxy checks whether an IP falls within a trusted proxy CIDR.
func isTrustedProxy(ip net.IP) bool {
	for _, n := range trustedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// extractIP parses the IP portion from an address that may include a port.
func extractIP(addr string) net.IP {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return net.ParseIP(strings.TrimSpace(host))
}

// GetClientIP extracts the real client IP from the request.
//
// When no trusted proxies are configured, it returns RemoteAddr directly —
// forwarding headers are never trusted in this mode.
//
// When trusted proxies are configured and RemoteAddr is from a trusted proxy,
// it uses the rightmost-untrusted IP from X-Forwarded-For (or X-Real-IP as
// fallback). This prevents spoofing by untrusted clients injecting IPs into
// the left side of the header.
func GetClientIP(r *http.Request) string {
	remoteIP := extractIP(r.RemoteAddr)
	if remoteIP == nil {
		return r.RemoteAddr
	}

	// If no trusted proxies configured, never trust forwarding headers
	if len(trustedNets) == 0 {
		return remoteIP.String()
	}

	// Only trust forwarding headers if the immediate connection is from a trusted proxy
	if !isTrustedProxy(remoteIP) {
		return remoteIP.String()
	}

	// Parse X-Forwarded-For right-to-left, returning the rightmost untrusted IP
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		for i := len(ips) - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(ips[i])
			if candidate == "" {
				continue
			}
			parsed := net.ParseIP(candidate)
			if parsed == nil {
				continue
			}
			if !isTrustedProxy(parsed) {
				return parsed.String()
			}
		}
	}

	// Fallback: check X-Real-IP (set by some reverse proxies like nginx)
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		parsed := net.ParseIP(strings.TrimSpace(xri))
		if parsed != nil {
			return parsed.String()
		}
	}

	// All forwarded IPs are trusted proxies (or headers empty/invalid) — use RemoteAddr
	return remoteIP.String()
}

// RateLimitKey generates a rate limit key for user-specific limits
func RateLimitKey(userID string, action string) string {
	return fmt.Sprintf("user:%s:%s", userID, action)
}

