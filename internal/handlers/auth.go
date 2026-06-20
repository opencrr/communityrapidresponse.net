package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// Auth rate limit constants
const (
	authLoginLimit             = 10
	authLoginWindow            = 5 * time.Minute
	authRegisterLimit          = 3
	authRegisterWindow         = 1 * time.Hour
	authForgotPasswordLimit    = 5
	authForgotPasswordWindow   = 15 * time.Minute
	authResetPasswordLimit     = 10
	authResetPasswordWindow    = 15 * time.Minute
	authResendVerifyLimit      = 3
	authResendVerifyWindow     = 15 * time.Minute
	accountLockoutThreshold    = 10
	accountLockoutDuration     = 15 * time.Minute
)

// EmailVerificationClaims represents JWT claims for email verification tokens
type EmailVerificationClaims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	db                *database.DB
	userRepo          *database.UserRepository
	jwtAuth           *middleware.JWTAuth
	emailService      services.EmailServiceInterface
	jwtSecret         string
	secureCookies     bool
	mfaRequired       bool
	auditRepo         *database.AuditRepository
	passwordResetRepo *database.PasswordResetRepository
	emailTemplates    *services.EmailTemplates
	baseURL           string
	encryptionKeyRepo *database.EncryptionKeyRepository
	rateLimiter       services.RateLimiter
}

// SetRateLimiter sets the rate limiter for auth-specific rate limiting.
// This is optional - if not set, auth rate limits are not enforced.
func (h *AuthHandler) SetRateLimiter(limiter services.RateLimiter) {
	h.rateLimiter = limiter
}

// checkAuthRateLimit checks per-endpoint auth rate limits.
// Returns true if the request is allowed, false if rate-limited (429 already written).
func (h *AuthHandler) checkAuthRateLimit(w http.ResponseWriter, r *http.Request, action string, limit int, window time.Duration) bool {
	if h.rateLimiter == nil {
		return true
	}

	clientIP := middleware.GetClientIP(r)
	key := fmt.Sprintf("auth:%s:%s", action, clientIP)

	allowed, _, _, err := h.rateLimiter.Allow(r.Context(), key, limit, window)
	if err != nil {
		// On error, allow the request rather than blocking legitimate users
		slog.WarnContext(r.Context(), "auth rate limit check failed", "key", key, "error", err)
		return true
	}

	if !allowed {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests. Please try again later.")
		return false
	}
	return true
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(userRepo *database.UserRepository, jwtAuth *middleware.JWTAuth) *AuthHandler {
	return &AuthHandler{
		userRepo:      userRepo,
		jwtAuth:       jwtAuth,
		secureCookies: false, // Default for backwards compatibility
		mfaRequired:   true,  // Default to requiring MFA
	}
}

// NewAuthHandlerWithConfig creates a new auth handler with configuration
func NewAuthHandlerWithConfig(userRepo *database.UserRepository, jwtAuth *middleware.JWTAuth, secureCookies bool, mfaRequired bool) *AuthHandler {
	return &AuthHandler{
		userRepo:      userRepo,
		jwtAuth:       jwtAuth,
		secureCookies: secureCookies,
		mfaRequired:   mfaRequired,
	}
}

// NewAuthHandlerWithEmailService creates a new auth handler with email service
func NewAuthHandlerWithEmailService(
	db *database.DB,
	userRepo *database.UserRepository,
	jwtAuth *middleware.JWTAuth,
	emailService services.EmailServiceInterface,
	jwtSecret string,
	secureCookies bool,
	mfaRequired bool,
	auditRepo *database.AuditRepository,
	passwordResetRepo *database.PasswordResetRepository,
	emailTemplates *services.EmailTemplates,
	baseURL string,
	encryptionKeyRepo *database.EncryptionKeyRepository,
) *AuthHandler {
	return &AuthHandler{
		db:                db,
		userRepo:          userRepo,
		jwtAuth:           jwtAuth,
		emailService:      emailService,
		jwtSecret:         jwtSecret,
		secureCookies:     secureCookies,
		mfaRequired:       mfaRequired,
		auditRepo:         auditRepo,
		passwordResetRepo: passwordResetRepo,
		emailTemplates:    emailTemplates,
		baseURL:           baseURL,
		encryptionKeyRepo: encryptionKeyRepo,
	}
}

// Register handles user registration
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuthRateLimit(w, r, "register", authRegisterLimit, authRegisterWindow) {
		return
	}

	var req models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Struct-level validation from tags
	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
	}

	// Username format: letters, numbers, underscores only
	for _, c := range req.Username {
		if !isUsernameChar(c) {
			writeError(w, http.StatusBadRequest, "validation_error", "Username must contain only letters, numbers, and underscores")
			return
		}
	}

	if len(req.Password) > 128 {
		writeError(w, http.StatusBadRequest, "validation_error", "Password must not exceed 128 characters")
		return
	}

	// Check for '+' alias in email (reject outright)
	if services.ContainsEmailAlias(req.Email) {
		writeError(w, http.StatusBadRequest, "validation_error", "Email aliases with '+' are not allowed")
		return
	}

	// Normalize email for duplicate detection
	normalizedEmail, err := services.NormalizeEmail(req.Email)
	if err != nil {
		if errors.Is(err, services.ErrAliasedEmailNotAllowed) {
			writeError(w, http.StatusBadRequest, "validation_error", "Email aliases with '+' are not allowed")
			return
		}
		writeError(w, http.StatusBadRequest, "validation_error", "Invalid email format")
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		writeServerError(w, r, err, "Failed to process registration", "auth", "register")
		return
	}

	// Determine if email should be verified based on email service availability
	emailVerificationEnabled := h.emailService != nil && h.emailService.IsEnabled()

	// Create user
	user := &models.User{
		Username:         req.Username,
		Email:            req.Email,
		PasswordHash:     string(hashedPassword),
		VerificationTier: models.TierUnverified,
		EmailVerified:    !emailVerificationEnabled, // If email verification is disabled, mark as verified
		EmailNormalized:  &normalizedEmail,
	}

	if err := h.userRepo.Create(r.Context(), user); err != nil {
		if errors.Is(err, database.ErrUserAlreadyExists) ||
			errors.Is(err, database.ErrEmailAlreadyExists) ||
			errors.Is(err, database.ErrNormalizedEmailAlreadyExists) {
			// Anti-enumeration: same response as success.
			// Perform a dummy bcrypt comparison so the timing roughly matches
			// the success path (which already ran bcrypt.GenerateFromPassword).
			// This prevents an attacker from distinguishing duplicate accounts
			// via response latency.
			dummyHash := "$2a$12$000000000000000000000uGWDRhGTsKOQJF5K5CO1MJvVf6bhn6Ey"
			_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(req.Password))
			writeJSON(w, http.StatusCreated, models.RegisterResponse{
				Message: "Registration successful. Please check your email for verification.",
			})
			return
		}
		writeServerError(w, r, err, "Failed to create user", "auth", "register")
		return
	}

	// Audit log: user registered
	if h.auditRepo != nil {
		logAuditError(r, h.auditRepo.Log(r.Context(), &user.ID, models.AuditActionUserRegistered, nil, nil, map[string]string{
			"username": user.Username,
			"email":    user.Email,
		}), "user_registered")
	}

	// Build response
	resp := models.RegisterResponse{
		UserID:   user.ID,
		Username: user.Username,
		Email:    user.Email,
	}

	// Send verification email if email verification is enabled
	if emailVerificationEnabled {
		verificationToken, err := h.generateEmailVerificationToken(user.ID, user.Email)
		if err != nil {
			// Log error but don't fail registration
			// User can request resend
			resp.Message = "Registration successful. Please check your email to verify your account."
		} else {
			if err := h.emailService.SendVerificationEmail(r.Context(), user.Email, verificationToken); err != nil {
				// Log error but don't fail registration
				resp.Message = "Registration successful. Please check your email to verify your account."
			} else {
				resp.EmailVerificationSent = true
				resp.Message = "Registration successful. Please check your email to verify your account."
			}
		}
	}

	writeJSON(w, http.StatusCreated, resp)
}

// Login handles user login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuthRateLimit(w, r, "login", authLoginLimit, authLoginWindow) {
		return
	}

	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
	}

	// Validate request
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Email or username and password are required")
		return
	}

	// Get user by email or username
	user, err := h.userRepo.GetByEmailOrUsername(r.Context(), req.Email)
	if errors.Is(err, database.ErrUserNotFound) {
		// Audit log: login failed (user not found)
		if h.auditRepo != nil {
			logAuditError(r, h.auditRepo.Log(r.Context(), nil, models.AuditActionUserLoginFailed, nil, nil, map[string]string{
				"email":  req.Email,
				"reason": "user_not_found",
			}), "user_login_failed")
		}
		appSentry.IncrementCounter("auth.login_failure", 1, "reason", "user_not_found")
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid credentials")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to process login", "auth", "login")
		return
	}

	// Check if user has been soft-deleted
	if user.DeletedAt != nil {
		appSentry.IncrementCounter("auth.login_failure", 1, "reason", "account_deleted")
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid credentials")
		return
	}

	// Check if user is blocked
	if user.IsBlocked {
		appSentry.IncrementCounter("auth.login_failure", 1, "reason", "account_blocked")
		writeError(w, http.StatusForbidden, "account_blocked", "This account has been blocked")
		return
	}

	// Check if account is locked
	if user.LockedUntil != nil && time.Now().Before(*user.LockedUntil) {
		appSentry.IncrementCounter("auth.login_failure", 1, "reason", "account_locked")
		writeError(w, http.StatusTooManyRequests, "account_locked", "Account is temporarily locked due to too many failed login attempts. Please try again later.")
		return
	}

	// Check password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		// Increment failed login counter
		failedCount, incrErr := h.userRepo.IncrementFailedLoginAttempts(r.Context(), user.ID)
		if incrErr != nil {
			slog.WarnContext(r.Context(), "failed to increment login attempts", "user_id", user.ID, "error", incrErr)
		}

		// Lock account if threshold exceeded
		if failedCount >= accountLockoutThreshold {
			lockUntil := time.Now().Add(accountLockoutDuration)
			if lockErr := h.userRepo.LockAccount(r.Context(), user.ID, lockUntil); lockErr != nil {
				slog.WarnContext(r.Context(), "failed to lock account", "user_id", user.ID, "error", lockErr)
			}
			if h.auditRepo != nil {
				logAuditError(r, h.auditRepo.Log(r.Context(), &user.ID, models.AuditActionAccountLocked, nil, nil, map[string]string{
					"failed_attempts": fmt.Sprintf("%d", failedCount),
				}), "account_locked")
			}
		}

		// Audit log: login failed (wrong password)
		if h.auditRepo != nil {
			logAuditError(r, h.auditRepo.Log(r.Context(), &user.ID, models.AuditActionUserLoginFailed, nil, nil, map[string]string{
				"reason": "invalid_password",
			}), "user_login_failed")
		}
		appSentry.IncrementCounter("auth.login_failure", 1, "reason", "invalid_password")
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid credentials")
		return
	}

	// Reset failed login attempts on successful password verification
	if user.FailedLoginAttempts > 0 {
		if resetErr := h.userRepo.ResetFailedLoginAttempts(r.Context(), user.ID); resetErr != nil {
			slog.WarnContext(r.Context(), "failed to reset login attempts", "user_id", user.ID, "error", resetErr)
		}
	}

	// Reset failed MFA attempts on new login (gives user fresh attempts)
	if user.FailedMFAAttempts > 0 {
		if resetErr := h.userRepo.ResetFailedMFAAttempts(r.Context(), user.ID); resetErr != nil {
			slog.WarnContext(r.Context(), "failed to reset MFA attempts", "user_id", user.ID, "error", resetErr)
		}
	}

	// Determine token type based on email verification and MFA status
	var tokenType middleware.TokenType
	var mfaAction string
	var emailAction string

	// Check email verification first (if email service is enabled)
	emailVerificationEnabled := h.emailService != nil && h.emailService.IsEnabled()
	if emailVerificationEnabled && !user.EmailVerified {
		tokenType = middleware.TokenTypeEmailUnverified
		emailAction = "verify_email"
	} else if !h.mfaRequired {
		// MFA is disabled (development mode) - issue full token immediately
		tokenType = middleware.TokenTypeFull
	} else if user.MFASetupRequired && !user.MFAEnabled {
		// User needs to set up MFA
		tokenType = middleware.TokenTypeMFASetup
		mfaAction = "setup"
	} else if user.MFAEnabled {
		// User has MFA enabled, needs to verify
		tokenType = middleware.TokenTypePendingMFA
		mfaAction = "verify"
	} else {
		// MFA is enabled and not required (shouldn't happen, but handle gracefully)
		tokenType = middleware.TokenTypeFull
	}

	// Generate token
	token, err := h.jwtAuth.GenerateTokenWithType(user, tokenType)
	if err != nil {
		writeServerError(w, r, err, "Failed to generate token", "auth", "login")
		return
	}

	// Set cookie
	sameSite := http.SameSiteLaxMode
	if h.secureCookies {
		sameSite = http.SameSiteStrictMode
	}

	// MFA and email unverified tokens have shorter cookie expiry (10 minutes)
	maxAge := 86400 // 24 hours
	if tokenType != middleware.TokenTypeFull {
		maxAge = 600 // 10 minutes
	}

	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure and SameSite set dynamically via h.secureCookies
		Name:     "token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: sameSite,
		MaxAge:   maxAge,
	})

	// NOTE: Token is included in response body for programmatic API clients.
	// Browser clients should use the httpOnly cookie set above as their primary auth mechanism.

	// If email verification required, return different response
	if emailAction != "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"token":        token,
			"email_action": emailAction,
			"message":      "Please verify your email address to continue",
			"user": map[string]interface{}{
				"id":             user.ID,
				"username":       user.Username,
				"email":          user.Email,
				"email_verified": user.EmailVerified,
			},
		})
		return
	}

	// If MFA action required, return different response
	if mfaAction != "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"token":      token,
			"mfa_action": mfaAction,
			"user": map[string]interface{}{
				"id":       user.ID,
				"username": user.Username,
				"email":    user.Email,
			},
		})
		return
	}

	// Update last login only after full authentication
	loginTime := time.Now()
	_ = h.userRepo.UpdateLastLogin(r.Context(), user.ID)

	// Audit log: successful login
	if h.auditRepo != nil {
		logAuditError(r, h.auditRepo.Log(r.Context(), &user.ID, models.AuditActionUserLogin, nil, nil, nil), "user_login")
	}

	// Check if user has encryption keys
	hasEncryptionKeys := false
	if h.encryptionKeyRepo != nil {
		_, keyErr := h.encryptionKeyRepo.GetByUserID(r.Context(), user.ID)
		if keyErr == nil {
			hasEncryptionKeys = true
		}
	}

	// Return response with full user info
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":                  user.ID,
			"username":            user.Username,
			"email":               user.Email,
			"verification_tier":   user.VerificationTier,
			"postcard_verified":   user.PostcardVerified,
			"vouch_verified":      user.VouchVerified,
			"is_superuser":        user.IsSuperuser,
			"mfa_enabled":         user.MFAEnabled,
			"email_verified":      user.EmailVerified,
			"is_blocked":          user.IsBlocked,
			"block_reason":        user.BlockReason,
			"created_at":          user.CreatedAt,
			"last_login":          loginTime,
			"has_encryption_keys": hasEncryptionKeys,
		},
	})
}

// GetCurrentUser returns the current authenticated user
func (h *AuthHandler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	user, err := h.userRepo.GetUserWithRegions(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get user", "auth", "get_user")
		return
	}

	// Return user with verification fields (never expose JWT claims)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user": map[string]interface{}{
			"id":                    user.ID,
			"username":              user.Username,
			"email":                 user.Email,
			"verification_tier":     user.VerificationTier,
			"postcard_verified":     user.PostcardVerified,
			"vouch_verified":        user.VouchVerified,
			"is_superuser":          user.IsSuperuser,
			"email_verified":        user.EmailVerified,
			"is_blocked":            user.IsBlocked,
			"block_reason":          user.BlockReason,
			"created_at":            user.CreatedAt,
			"last_login":            user.LastLogin,
			"regions":               user.Regions,
			"authorized_boundaries": user.AuthorizedBoundaries,
		},
	})
}

// Logout handles user logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// Get user from context for audit logging
	claims := middleware.GetUserFromContext(r.Context())

	// Invalidate all tokens for this user so stolen JWTs cannot be reused
	if claims != nil && h.userRepo != nil {
		if err := h.userRepo.InvalidateTokens(r.Context(), claims.UserID); err != nil {
			slog.WarnContext(r.Context(), "failed to invalidate tokens on logout", "error", err, "user_id", claims.UserID)
		}
	}

	// Clear the cookie
	sameSite := http.SameSiteLaxMode
	if h.secureCookies {
		sameSite = http.SameSiteStrictMode
	}
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure and SameSite set dynamically via h.secureCookies
		Name:     "token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: sameSite,
		MaxAge:   -1,
	})

	// Audit log: user logout
	if h.auditRepo != nil && claims != nil {
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionUserLogout, nil, nil, nil), "user_logout")
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// VerifyEmail handles email verification via token
func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "missing_token", "Verification token is required")
		return
	}

	// Validate the token
	claims, err := h.validateEmailVerificationToken(token)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			writeError(w, http.StatusBadRequest, "expired_token", "Verification link has expired. Please request a new one.")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_token", "Invalid verification token")
		return
	}

	// Get the user
	user, err := h.userRepo.GetByID(r.Context(), claims.UserID)
	if errors.Is(err, database.ErrUserNotFound) {
		writeError(w, http.StatusBadRequest, "invalid_token", "Invalid verification token")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to verify email", "auth", "verify_email")
		return
	}

	// Check if already verified
	if user.EmailVerified {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Email already verified",
		})
		return
	}

	// Mark email as verified
	if err := h.userRepo.SetEmailVerified(r.Context(), user.ID, true); err != nil {
		writeServerError(w, r, err, "Failed to verify email", "auth", "verify_email")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Email verified successfully",
	})
}

// ResendVerificationEmail resends the verification email
func (h *AuthHandler) ResendVerificationEmail(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuthRateLimit(w, r, "resend-verification", authResendVerifyLimit, authResendVerifyWindow) {
		return
	}

	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Get the user
	user, err := h.userRepo.GetByID(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get user", "auth", "resend_verification")
		return
	}

	// Check if already verified
	if user.EmailVerified {
		writeError(w, http.StatusBadRequest, "already_verified", "Email is already verified")
		return
	}

	// Check if email service is available
	if h.emailService == nil || !h.emailService.IsEnabled() {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "Email verification is not available")
		return
	}

	// Generate and send verification email
	verificationToken, err := h.generateEmailVerificationToken(user.ID, user.Email)
	if err != nil {
		writeServerError(w, r, err, "Failed to generate verification token", "auth", "resend_verification")
		return
	}

	if err := h.emailService.SendVerificationEmail(r.Context(), user.Email, verificationToken); err != nil {
		writeServerError(w, r, err, "Failed to send verification email", "auth", "resend_verification")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Verification email sent",
	})
}

// ChangePassword handles password change for authenticated users
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Current password and new password are required")
		return
	}

	if len(req.NewPassword) < 12 {
		writeError(w, http.StatusBadRequest, "validation_error", "New password must be at least 12 characters")
		return
	}

	if len(req.NewPassword) > 128 {
		writeError(w, http.StatusBadRequest, "validation_error", "Password must not exceed 128 characters")
		return
	}

	// Get user to verify current password
	user, err := h.userRepo.GetByID(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get user", "auth", "change_password")
		return
	}

	// Verify current password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_password", "Current password is incorrect")
		return
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 12)
	if err != nil {
		writeServerError(w, r, err, "Failed to process password change", "auth", "change_password")
		return
	}

	// Update password
	if err := h.userRepo.UpdatePassword(r.Context(), user.ID, string(hashedPassword)); err != nil {
		writeServerError(w, r, err, "Failed to update password", "auth", "change_password")
		return
	}

	// Invalidate existing tokens so other sessions must re-login
	if err := h.userRepo.InvalidateTokens(r.Context(), user.ID); err != nil {
		slog.WarnContext(r.Context(), "failed to invalidate tokens after password change", "user_id", user.ID, "error", err)
	}
	if h.jwtAuth != nil {
		h.jwtAuth.InvalidateUserCache(user.ID)
	}

	// Invalidate any active reset tokens
	if h.passwordResetRepo != nil {
		_ = h.passwordResetRepo.InvalidateForUser(r.Context(), user.ID)
	}

	// Audit log
	if h.auditRepo != nil {
		logAuditError(r, h.auditRepo.Log(r.Context(), &user.ID, models.AuditActionPasswordChanged, nil, nil, nil), "password_changed")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Password changed successfully",
	})
}

// ForgotPassword handles password reset request (sends email with reset link)
func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuthRateLimit(w, r, "forgot-password", authForgotPasswordLimit, authForgotPasswordWindow) {
		return
	}

	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Email is required")
		return
	}

	// Always return success to prevent email enumeration
	successResponse := map[string]interface{}{
		"success": true,
		"message": "If an account with that email exists, a password reset link has been sent.",
	}

	// Look up user by email
	user, err := h.userRepo.GetByEmail(r.Context(), req.Email)
	if err != nil {
		// User not found - return success anyway to prevent enumeration
		writeJSON(w, http.StatusOK, successResponse)
		return
	}

	// Generate random token (32 bytes = 64 hex chars) before transaction
	rawTokenBytes := make([]byte, 32)
	if _, err := rand.Read(rawTokenBytes); err != nil {
		slog.WarnContext(r.Context(), "failed to generate reset token", "error", err)
		writeJSON(w, http.StatusOK, successResponse)
		return
	}
	rawToken := hex.EncodeToString(rawTokenBytes)

	// Store SHA-256 hash of token
	tokenHashBytes := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(tokenHashBytes[:])

	expiresAt := time.Now().UTC().Add(1 * time.Hour)

	// Rate-limit check + create within a transaction to prevent race conditions
	if h.passwordResetRepo != nil && h.db != nil {
		txErr := h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			// Lock and count active tokens for this user
			activeCount, err := h.passwordResetRepo.CountActiveForUserForUpdate(r.Context(), tx, user.ID)
			if err != nil {
				return err
			}
			if activeCount >= 3 {
				return database.ErrRateLimited
			}

			_, err = h.passwordResetRepo.CreateTx(r.Context(), tx, user.ID, tokenHash, expiresAt)
			return err
		})
		if txErr != nil {
			if !errors.Is(txErr, database.ErrRateLimited) {
				slog.WarnContext(r.Context(), "failed to create reset token", "error", txErr)
			}
			// Silently succeed to prevent enumeration
			writeJSON(w, http.StatusOK, successResponse)
			return
		}
	} else if h.passwordResetRepo != nil {
		// Fallback for when db is not available (e.g., tests)
		activeCount, err := h.passwordResetRepo.CountActiveForUser(r.Context(), user.ID)
		if err != nil {
			slog.WarnContext(r.Context(), "failed to count active reset tokens", "error", err)
			writeJSON(w, http.StatusOK, successResponse)
			return
		}
		if activeCount >= 3 {
			writeJSON(w, http.StatusOK, successResponse)
			return
		}
		_, err = h.passwordResetRepo.Create(r.Context(), user.ID, tokenHash, expiresAt)
		if err != nil {
			slog.WarnContext(r.Context(), "failed to create reset token", "error", err)
			writeJSON(w, http.StatusOK, successResponse)
			return
		}
	}

	// Send reset email
	if h.emailService != nil && h.emailService.IsEnabled() && h.emailTemplates != nil {
		resetURL := fmt.Sprintf("%s/reset-password?token=%s", h.baseURL, rawToken)
		emailMessage := h.emailTemplates.BuildPasswordReset(user.Email, resetURL)
		if err := h.emailService.Send(r.Context(), emailMessage); err != nil {
			slog.WarnContext(r.Context(), "failed to send password reset email", "error", err)
		}
	} else {
		// In development/mock mode, log the token
		slog.DebugContext(r.Context(), "password reset token generated (mock mode)", "user_id", user.ID)
	}

	// Audit log
	if h.auditRepo != nil {
		logAuditError(r, h.auditRepo.Log(r.Context(), &user.ID, models.AuditActionPasswordResetRequested, nil, nil, nil), "password_reset_requested")
	}

	writeJSON(w, http.StatusOK, successResponse)
}

// ResetPassword handles password reset with a valid token
func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuthRateLimit(w, r, "reset-password", authResetPasswordLimit, authResetPasswordWindow) {
		return
	}

	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Token == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Token and password are required")
		return
	}

	if len(req.Password) < 12 {
		writeError(w, http.StatusBadRequest, "validation_error", "Password must be at least 12 characters")
		return
	}

	if len(req.Password) > 128 {
		writeError(w, http.StatusBadRequest, "validation_error", "Password must not exceed 128 characters")
		return
	}

	if h.passwordResetRepo == nil {
		writeServerError(w, r, errPasswordResetNotConfigured, "Password reset is not configured", "auth", "reset_password")
		return
	}

	// Hash the provided token to look up in DB
	tokenHashBytes := sha256.Sum256([]byte(req.Token))
	tokenHash := hex.EncodeToString(tokenHashBytes[:])

	// Look up and validate token
	resetToken, err := h.passwordResetRepo.GetByTokenHash(r.Context(), tokenHash)
	if err != nil {
		if errors.Is(err, database.ErrResetTokenNotFound) || errors.Is(err, database.ErrResetTokenUsed) {
			writeError(w, http.StatusBadRequest, "invalid_token", "Invalid or expired reset token")
			return
		}
		if errors.Is(err, database.ErrResetTokenExpired) {
			writeError(w, http.StatusBadRequest, "expired_token", "This reset link has expired. Please request a new one.")
			return
		}
		writeServerError(w, r, err, "Failed to validate reset token", "auth", "reset_password")
		return
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		writeServerError(w, r, err, "Failed to process password reset", "auth", "reset_password")
		return
	}

	// Update password, invalidate tokens, and mark reset token used — all atomically
	if h.db != nil {
		txErr := h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			if err := h.userRepo.UpdatePasswordTx(r.Context(), tx, resetToken.UserID, string(hashedPassword)); err != nil {
				return err
			}
			if err := h.userRepo.InvalidateTokensTx(r.Context(), tx, resetToken.UserID); err != nil {
				return err
			}
			if err := h.passwordResetRepo.MarkUsedTx(r.Context(), tx, resetToken.ID); err != nil {
				return err
			}
			return h.passwordResetRepo.InvalidateForUserTx(r.Context(), tx, resetToken.UserID)
		})
		if txErr != nil {
			writeServerError(w, r, txErr, "Failed to reset password", "auth", "reset_password")
			return
		}
	} else {
		// Non-transactional fallback (unit tests where h.db is nil)
		if err := h.userRepo.UpdatePassword(r.Context(), resetToken.UserID, string(hashedPassword)); err != nil {
			writeServerError(w, r, err, "Failed to update password", "auth", "reset_password")
			return
		}
		if err := h.userRepo.InvalidateTokens(r.Context(), resetToken.UserID); err != nil {
			slog.WarnContext(r.Context(), "failed to invalidate tokens after password reset", "user_id", resetToken.UserID, "error", err)
		}
		_ = h.passwordResetRepo.MarkUsed(r.Context(), resetToken.ID)
		_ = h.passwordResetRepo.InvalidateForUser(r.Context(), resetToken.UserID)
	}

	// Evict status cache so middleware picks up the invalidated epoch immediately
	if h.jwtAuth != nil {
		h.jwtAuth.InvalidateUserCache(resetToken.UserID)
	}

	// Audit log
	if h.auditRepo != nil {
		logAuditError(r, h.auditRepo.Log(r.Context(), &resetToken.UserID, models.AuditActionPasswordResetCompleted, nil, nil, nil), "password_reset_completed")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Password has been reset successfully. You can now log in with your new password.",
	})
}

// ValidateResetToken checks if a password reset token is valid
func (h *AuthHandler) ValidateResetToken(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "missing_token", "Reset token is required")
		return
	}

	if h.passwordResetRepo == nil {
		writeServerError(w, r, errPasswordResetNotConfigured, "Password reset is not configured", "auth", "validate_reset_token")
		return
	}

	// Hash the provided token
	tokenHashBytes := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(tokenHashBytes[:])

	_, err := h.passwordResetRepo.GetByTokenHash(r.Context(), tokenHash)
	if err != nil {
		if errors.Is(err, database.ErrResetTokenExpired) {
			writeError(w, http.StatusBadRequest, "expired_token", "This reset link has expired. Please request a new one.")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_token", "Invalid or expired reset token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid": true,
	})
}

// DeleteAccount handles self-service account deletion
func (h *AuthHandler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Password is required")
		return
	}

	// Get user to verify password
	user, err := h.userRepo.GetByID(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get user", "auth", "delete_account")
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_password", "Password is incorrect")
		return
	}

	// Block superuser self-deletion
	if user.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "Superusers cannot delete their own accounts")
		return
	}

	// Determine deletion strategy
	isBlocked, err := h.userRepo.IsUserBlockedAnywhere(r.Context(), user.ID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check block status", "auth", "delete_account")
		return
	}

	deletionType := "hard_delete"
	if isBlocked {
		deletionType = "soft_delete"
	}

	// Audit log before deletion (hard delete removes the user row)
	if h.auditRepo != nil {
		logAuditError(r, h.auditRepo.Log(r.Context(), &user.ID, models.AuditActionAccountDeleted, nil, nil, map[string]string{
			"deletion_type": deletionType,
		}), "account_deleted")
	}

	if isBlocked {
		scrubUUID := uuid.New().String()
		if err := h.userRepo.SoftDelete(r.Context(), user.ID, scrubUUID); err != nil {
			writeServerError(w, r, err, "Failed to delete account", "auth", "delete_account")
			return
		}
	} else {
		if err := h.userRepo.Delete(r.Context(), user.ID); err != nil {
			writeServerError(w, r, err, "Failed to delete account", "auth", "delete_account")
			return
		}
	}

	// Clear auth cookie
	sameSite := http.SameSiteLaxMode
	if h.secureCookies {
		sameSite = http.SameSiteStrictMode
	}
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure and SameSite set dynamically via h.secureCookies
		Name:     "token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: sameSite,
		MaxAge:   -1,
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Account deleted successfully",
	})
}

// DeletionPreflight returns warnings about the consequences of account deletion
func (h *AuthHandler) DeletionPreflight(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	warnings := make([]map[string]string, 0)

	// Check for sole-admin regions
	soleAdminRegions, err := h.userRepo.GetSoleAdminRegions(r.Context(), claims.UserID)
	if err != nil {
		slog.WarnContext(r.Context(), "failed to check sole admin regions", "error", err)
	} else {
		for _, region := range soleAdminRegions {
			warnings = append(warnings, map[string]string{
				"type":    "sole_admin_region",
				"id":      region.ID,
				"name":    region.Name,
				"message": fmt.Sprintf("You are the only admin of %s. Deleting your account will leave this community without an admin.", region.Name),
			})
		}
	}

	// Check for sole-verified schools
	soleVerifiedSchools, err := h.userRepo.GetSoleVerifiedSchools(r.Context(), claims.UserID)
	if err != nil {
		slog.WarnContext(r.Context(), "failed to check sole verified schools", "error", err)
	} else {
		for _, school := range soleVerifiedSchools {
			warnings = append(warnings, map[string]string{
				"type":    "sole_verified_school",
				"id":      school.ID,
				"name":    school.Name,
				"message": fmt.Sprintf("You are the only verified member of %s. Deleting your account will leave this school without a verified member.", school.Name),
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"warnings": warnings,
	})
}

// generateEmailVerificationToken generates a JWT token for email verification
func (h *AuthHandler) generateEmailVerificationToken(userID, email string) (string, error) {
	now := time.Now()
	claims := &EmailVerificationClaims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   "email_verification",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(h.jwtSecret))
}

// validateEmailVerificationToken validates an email verification JWT token
func (h *AuthHandler) validateEmailVerificationToken(tokenString string) (*EmailVerificationClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &EmailVerificationClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("invalid signing method")
		}
		return []byte(h.jwtSecret), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*EmailVerificationClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	// Verify the subject is email_verification
	if claims.Subject != "email_verification" {
		return nil, errors.New("invalid token type")
	}

	return claims, nil
}
