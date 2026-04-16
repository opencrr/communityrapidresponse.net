package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/logging"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
)

// Logger logs HTTP requests with structured fields including request ID.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Generate request ID and add to context
		requestID := uuid.New().String()
		ctx := logging.WithRequestID(r.Context(), requestID)
		r = r.WithContext(ctx)

		// Add request ID to response headers
		w.Header().Set("X-Request-ID", requestID)

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_addr", r.RemoteAddr,
			"request_id", requestID,
		)
	})
}

// Recoverer recovers from panics and logs them
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				appSentry.RecoverAndReport(err)
				slog.Error("panic recovered",
					"error", err,
					"stack", string(debug.Stack()),
					"request_id", logging.RequestID(r.Context()),
				)
				http.Error(w, `{"error":"internal_error","message":"Internal server error"}`, http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// CORS adds CORS headers.
//
// Security note: when the allowed-origins list contains "*" and a cross-origin
// request arrives, we respond with a literal "*" origin and OMIT the
// Access-Control-Allow-Credentials header. Browsers reject credentialed
// requests against a wildcard origin, which prevents the classic
// "reflect arbitrary origin + credentials=true" footgun that would otherwise
// allow any site to make authenticated cross-origin requests.
// For exact-match origins (the production path) we continue to echo the
// origin back with credentials enabled.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			allowed := false
			wildcard := false
			for _, o := range allowedOrigins {
				if o == "*" {
					allowed = true
					wildcard = true
					continue
				}
				if o == origin {
					allowed = true
					wildcard = false
					break
				}
			}

			if allowed {
				if wildcard {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")
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

// MaxBodySize limits the size of request bodies to prevent memory exhaustion.
// Returns 413 Request Entity Too Large if the body exceeds the limit.
func MaxBodySize(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
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
			// Disable powerful browser APIs we never use. Narrows the blast
			// radius of any future XSS and blocks passive fingerprinting vectors.
			w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=(), usb=(), magnetometer=(), gyroscope=(), accelerometer=(), interest-cohort=()")

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
