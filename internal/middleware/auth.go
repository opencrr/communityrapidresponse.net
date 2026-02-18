package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
)

type contextKey string

const (
	UserContextKey contextKey = "user"
)

// TokenType represents the type of JWT token
type TokenType string

const (
	TokenTypeFull            TokenType = "full"             // Fully authenticated token
	TokenTypePendingMFA      TokenType = "pending_mfa"      // Awaiting MFA verification
	TokenTypeMFASetup        TokenType = "mfa_setup"        // Needs to set up MFA
	TokenTypeEmailUnverified TokenType = "email_unverified" // Email not yet verified
)

var (
	ErrMissingToken = errors.New("missing authorization token")
	ErrInvalidToken = errors.New("invalid authorization token")
	ErrExpiredToken = errors.New("token has expired")
)

// JWTAuth provides JWT authentication middleware
type JWTAuth struct {
	config      *config.JWTConfig
	statusCache *UserStatusCache
}

// NewJWTAuth creates a new JWT auth middleware
func NewJWTAuth(cfg *config.JWTConfig) *JWTAuth {
	return &JWTAuth{config: cfg}
}

// SetStatusCache sets the user status cache for token revocation checks.
// If nil (default), revocation checks are skipped (backward compat for tests).
func (a *JWTAuth) SetStatusCache(cache *UserStatusCache) {
	a.statusCache = cache
}

// InvalidateUserCache evicts a user's entry from the status cache,
// forcing the next request to re-fetch from the database.
func (a *JWTAuth) InvalidateUserCache(userID string) {
	if a.statusCache != nil {
		a.statusCache.Invalidate(userID)
	}
}

// Claims represents JWT claims
type Claims struct {
	UserID           string                  `json:"user_id"`
	Username         string                  `json:"username"`
	Email            string                  `json:"email"`
	VerificationTier models.VerificationTier `json:"verification_tier"`
	PostcardVerified bool                    `json:"postcard_verified"`
	VouchVerified    bool                    `json:"vouch_verified"`
	IsSuperuser      bool                    `json:"is_superuser"`
	EmailVerified    bool                    `json:"email_verified"`
	TokenType        TokenType               `json:"token_type,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken generates a new fully authenticated JWT token for a user
func (a *JWTAuth) GenerateToken(user *models.User) (string, error) {
	return a.GenerateTokenWithType(user, TokenTypeFull)
}

// GenerateTokenWithType generates a JWT token with a specific type
func (a *JWTAuth) GenerateTokenWithType(user *models.User, tokenType TokenType) (string, error) {
	now := time.Now()

	// MFA and email unverified tokens have shorter expiration (10 minutes)
	expiration := time.Duration(a.config.ExpirationHours) * time.Hour
	if tokenType == TokenTypePendingMFA || tokenType == TokenTypeMFASetup || tokenType == TokenTypeEmailUnverified {
		expiration = 10 * time.Minute
	}

	claims := &Claims{
		UserID:           user.ID,
		Username:         user.Username,
		Email:            user.Email,
		VerificationTier: user.VerificationTier,
		PostcardVerified: user.PostcardVerified,
		VouchVerified:    user.VouchVerified,
		IsSuperuser:      user.IsSuperuser,
		EmailVerified:    user.EmailVerified,
		TokenType:        tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(expiration)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    a.config.Issuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(a.config.Secret))
}

// ValidateToken validates a JWT token and returns the claims
func (a *JWTAuth) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(a.config.Secret), nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// Authenticate is middleware that validates JWT tokens and requires full authentication
func (a *JWTAuth) Authenticate(next http.Handler) http.Handler {
	return a.AuthenticateWithTypes(next, TokenTypeFull)
}

// OptionalAuthenticate tries to parse a JWT token and set claims in context,
// but passes through to the next handler even if no token is present or invalid.
func (a *JWTAuth) OptionalAuthenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenString := extractToken(r)
		if tokenString != "" {
			claims, err := a.ValidateToken(tokenString)
			if err == nil && claims != nil {
				if claims.TokenType == "" || claims.TokenType == TokenTypeFull {
					// Check user status — if revoked, treat as unauthenticated
					if a.statusCache != nil && a.checkUserRevoked(r.Context(), claims) {
						// Don't set claims; pass through as unauthenticated
					} else {
						ctx := context.WithValue(r.Context(), UserContextKey, claims)
						appSentry.SetUser(ctx, claims.UserID)
						r = r.WithContext(ctx)
					}
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// AuthenticateWithTypes validates JWT tokens and allows specific token types
func (a *JWTAuth) AuthenticateWithTypes(next http.Handler, allowedTypes ...TokenType) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenString := extractToken(r)
		if tokenString == "" {
			appSentry.IncrementCounter("auth.middleware_rejection", 1, "reason", "missing_token")
			http.Error(w, `{"error":"unauthorized","message":"Missing authorization token"}`, http.StatusUnauthorized)
			return
		}

		claims, err := a.ValidateToken(tokenString)
		if err != nil {
			status := http.StatusUnauthorized
			message := "Invalid authorization token"
			reason := "invalid_token"
			if errors.Is(err, ErrExpiredToken) {
				message = "Token has expired"
				reason = "expired_token"
			}
			appSentry.IncrementCounter("auth.middleware_rejection", 1, "reason", reason)
			http.Error(w, `{"error":"unauthorized","message":"`+message+`"}`, status)
			return
		}

		// Check token type
		tokenType := claims.TokenType
		if tokenType == "" {
			tokenType = TokenTypeFull // Legacy tokens without type are treated as full
		}

		allowed := false
		for _, t := range allowedTypes {
			if t == tokenType {
				allowed = true
				break
			}
		}

		if !allowed {
			appSentry.IncrementCounter("auth.middleware_rejection", 1, "reason", "wrong_token_type")
			var message string
			var errorCode string
			switch tokenType {
			case TokenTypePendingMFA:
				message = "MFA verification required"
				errorCode = "mfa_required"
			case TokenTypeMFASetup:
				message = "MFA setup required"
				errorCode = "mfa_required"
			case TokenTypeEmailUnverified:
				message = "Email verification required"
				errorCode = "email_verification_required"
			default:
				message = "Invalid token type for this endpoint"
				errorCode = "invalid_token_type"
			}
			http.Error(w, `{"error":"`+errorCode+`","message":"`+message+`","token_type":"`+string(tokenType)+`"}`, http.StatusForbidden)
			return
		}

		// Check user status (revocation, blocked, deleted) if cache is configured
		if a.statusCache != nil {
			if revoked := a.checkUserRevoked(r.Context(), claims); revoked {
				appSentry.IncrementCounter("auth.middleware_rejection", 1, "reason", "token_revoked")
				http.Error(w, `{"error":"token_revoked","message":"Your session has been invalidated. Please log in again."}`, http.StatusUnauthorized)
				return
			}
		}

		// Add claims to context
		ctx := context.WithValue(r.Context(), UserContextKey, claims)
		appSentry.SetUser(ctx, claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireTier creates middleware that requires a minimum verification tier
func RequireTier(minTier models.VerificationTier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserFromContext(r.Context())
			if claims == nil {
				http.Error(w, `{"error":"unauthorized","message":"Authentication required"}`, http.StatusUnauthorized)
				return
			}

			if claims.VerificationTier < minTier {
				http.Error(w, `{"error":"forbidden","message":"Insufficient verification tier"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdmin creates middleware that requires admin status for a region
// Note: This requires the regionID to be available in the request
func RequireAdmin(getRegionID func(*http.Request) string, isAdmin func(context.Context, string, string) (bool, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserFromContext(r.Context())
			if claims == nil {
				http.Error(w, `{"error":"unauthorized","message":"Authentication required"}`, http.StatusUnauthorized)
				return
			}

			regionID := getRegionID(r)
			if regionID == "" {
				http.Error(w, `{"error":"bad_request","message":"Community ID required"}`, http.StatusBadRequest)
				return
			}

			admin, err := isAdmin(r.Context(), claims.UserID, regionID)
			if err != nil {
				http.Error(w, `{"error":"internal_error","message":"Failed to check admin status"}`, http.StatusInternalServerError)
				return
			}

			if !admin {
				http.Error(w, `{"error":"forbidden","message":"Admin access required"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// GetUserFromContext retrieves user claims from context
func GetUserFromContext(ctx context.Context) *Claims {
	claims, ok := ctx.Value(UserContextKey).(*Claims)
	if !ok {
		return nil
	}
	return claims
}

// ContextWithUser adds user claims to a context (primarily for testing)
func ContextWithUser(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, UserContextKey, claims)
}

// checkUserRevoked checks whether the user's token should be rejected based on
// their current status (blocked, deleted, or token invalidated). Fails closed
// on DB/cache errors (returns true to reject the request).
func (a *JWTAuth) checkUserRevoked(ctx context.Context, claims *Claims) bool {
	status, err := a.statusCache.GetUserStatus(ctx, claims.UserID)
	if err != nil {
		slog.ErrorContext(ctx, "user status check failed, failing closed", "user_id", claims.UserID, "error", err)
		return true
	}

	if status.IsBlocked {
		return true
	}
	if status.DeletedAt != nil {
		return true
	}
	if status.TokenInvalidatedAt != nil && claims.IssuedAt != nil {
		if !claims.IssuedAt.Time.After(*status.TokenInvalidatedAt) {
			return true
		}
	}

	return false
}

// extractToken extracts the JWT token from the Authorization header
func extractToken(r *http.Request) string {
	// Check Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
	}

	// Check cookie as fallback
	cookie, err := r.Cookie("token")
	if err == nil {
		return cookie.Value
	}

	return ""
}
