package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"
)

// csrfCookieName is the name of the CSRF token cookie.
const csrfCookieName = "csrf_token"

// csrfHeaderName is the name of the HTTP header that carries the CSRF token.
const csrfHeaderName = "X-CSRF-Token"

// csrfCookieMaxAge is the max age for the CSRF cookie in seconds (24 hours).
// Matches the JWT token expiration so the cookie persists for returning users.
const csrfCookieMaxAge = 86400

// csrfExemptPaths are exact paths exempt from CSRF validation.
// Only unauthenticated/pre-auth endpoints are listed here. Authenticated
// endpoints like change-password and resend-verification require CSRF
// as defense-in-depth.
var csrfExemptPaths = map[string]bool{
	"/api/v1/auth/register":        true,
	"/api/v1/auth/login":           true,
	"/api/v1/auth/logout":          true,
	"/api/v1/auth/forgot-password": true,
	"/api/v1/auth/reset-password":  true,
	"/api/v1/auth/verify-email":    true,
	"/api/v1/mfa/setup":            true,
	"/api/v1/mfa/setup/complete":   true,
	"/api/v1/mfa/verify":           true,
}

// deriveCSRFSecret derives a dedicated CSRF signing key from the application
// secret using HMAC-SHA256, following the key separation principle.
func deriveCSRFSecret(appSecret string) string {
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte("csrf"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// CSRFProtection returns middleware that implements the Double Submit Cookie pattern.
// On safe methods (GET/HEAD/OPTIONS), it ensures a csrf_token cookie is set.
// On unsafe methods (POST/PUT/PATCH/DELETE), it validates the X-CSRF-Token header
// matches the csrf_token cookie and has a valid HMAC signature.
// The provided secret is used to derive a dedicated CSRF signing key.
func CSRFProtection(secret string, secureCookies bool) func(http.Handler) http.Handler {
	csrfSecret := deriveCSRFSecret(secret)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method := r.Method

			// Safe methods: ensure cookie exists, then pass through
			if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
				ensureCSRFCookie(w, r, csrfSecret, secureCookies)
				next.ServeHTTP(w, r)
				return
			}

			// Unsafe methods: check exempt paths first
			if csrfExemptPaths[r.URL.Path] {
				ensureCSRFCookie(w, r, csrfSecret, secureCookies)
				next.ServeHTTP(w, r)
				return
			}

			// Validate CSRF token
			cookieToken := ""
			if cookie, err := r.Cookie(csrfCookieName); err == nil {
				cookieToken = cookie.Value
			}

			headerToken := r.Header.Get(csrfHeaderName)

			if cookieToken == "" || headerToken == "" {
				http.Error(w, `{"error":"csrf_validation_failed","message":"CSRF token missing"}`, http.StatusForbidden)
				return
			}

			if subtle.ConstantTimeCompare([]byte(cookieToken), []byte(headerToken)) != 1 {
				http.Error(w, `{"error":"csrf_validation_failed","message":"CSRF token mismatch"}`, http.StatusForbidden)
				return
			}

			if !verifyCSRFToken(cookieToken, csrfSecret) {
				http.Error(w, `{"error":"csrf_validation_failed","message":"CSRF token invalid"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// generateCSRFToken creates a new CSRF token in the format:
// base64url(32 random bytes) + "." + base64url(HMAC-SHA256(random_bytes, secret))
func generateCSRFToken(secret string) (string, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	encodedRandom := base64.RawURLEncoding.EncodeToString(randomBytes)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(randomBytes)
	signature := mac.Sum(nil)
	encodedSignature := base64.RawURLEncoding.EncodeToString(signature)

	return encodedRandom + "." + encodedSignature, nil
}

// verifyCSRFToken validates that a token has a valid HMAC signature.
func verifyCSRFToken(token, secret string) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}

	randomBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}

	expectedSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(randomBytes)
	actualSignature := mac.Sum(nil)

	return hmac.Equal(expectedSignature, actualSignature)
}

// ensureCSRFCookie sets a csrf_token cookie if one is not already present.
func ensureCSRFCookie(w http.ResponseWriter, r *http.Request, secret string, secureCookies bool) {
	if _, err := r.Cookie(csrfCookieName); err == nil {
		return // Cookie already exists
	}

	token, err := generateCSRFToken(secret)
	if err != nil {
		slog.Error("failed to generate csrf token", "error", err)
		return
	}

	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- HttpOnly intentionally false (JS reads CSRF token); Secure set dynamically
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   csrfCookieMaxAge,
		HttpOnly: false, // JS must read this cookie
		Secure:   secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}
