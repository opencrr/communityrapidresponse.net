package middleware

import (
	"log"
	"net/http"
	"runtime/debug"
	"time"

	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
)

// Logger logs HTTP requests
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		log.Printf(
			"%s %s %d %s %s",
			r.Method,
			r.URL.Path,
			wrapped.status,
			time.Since(start),
			r.RemoteAddr,
		)
	})
}

// Recoverer recovers from panics and logs them
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				appSentry.RecoverAndReport(err)
				log.Printf("panic: %v\n%s", err, debug.Stack())
				http.Error(w, `{"error":"internal_error","message":"Internal server error"}`, http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// CORS adds CORS headers
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed
			allowed := false
			for _, o := range allowedOrigins {
				if o == "*" || o == origin {
					allowed = true
					break
				}
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			// Handle preflight
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ContentType sets the Content-Type header for JSON responses
func ContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

// SecurityConfig holds configuration for security headers middleware.
type SecurityConfig struct {
	SecureCookies bool   // When true, enables HSTS
	CSPDirectives string // Content-Security-Policy header value
}

// SecurityHeaders adds security-related headers.
// When cfg is nil, only the base 4 headers are set (backward compat for tests).
func SecurityHeadersWithConfig(cfg *SecurityConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			if cfg != nil {
				if cfg.CSPDirectives != "" {
					w.Header().Set("Content-Security-Policy", cfg.CSPDirectives)
				}
				if cfg.SecureCookies {
					w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders adds base security headers (backward-compatible, no CSP/HSTS).
func SecurityHeaders(next http.Handler) http.Handler {
	return SecurityHeadersWithConfig(nil)(next)
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}
