package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// mfaLockoutThreshold is the number of failed MFA attempts before lockout
const mfaLockoutThreshold = 5

// MFAHandler handles MFA-related endpoints
type MFAHandler struct {
	db            *database.DB
	userRepo      *database.UserRepository
	mfaService    *services.MFAService
	jwtAuth       *middleware.JWTAuth
	secureCookies bool
	mfaRequired   bool
	auditRepo     *database.AuditRepository
}

// NewMFAHandler creates a new MFA handler
func NewMFAHandler(db *database.DB, userRepo *database.UserRepository, mfaService *services.MFAService, jwtAuth *middleware.JWTAuth, secureCookies bool, auditRepo *database.AuditRepository) *MFAHandler {
	return &MFAHandler{
		db:            db,
		userRepo:      userRepo,
		mfaService:    mfaService,
		jwtAuth:       jwtAuth,
		secureCookies: secureCookies,
		mfaRequired:   mfaService != nil, // MFA is required if service is configured
		auditRepo:     auditRepo,
	}
}

// SetupMFARequest is the request body for MFA setup initiation
type SetupMFARequest struct{}

// SetupMFAResponse is the response for MFA setup initiation
type SetupMFAResponse struct {
	QRCode string `json:"qr_code"` // Base64 PNG data URL
	Secret string `json:"secret"`  // Manual entry secret
}

// CompleteMFASetupRequest is the request to complete MFA setup
type CompleteMFASetupRequest struct {
	Code string `json:"code"` // TOTP code to verify setup
}

// CompleteMFASetupResponse is the response after completing MFA setup
type CompleteMFASetupResponse struct {
	BackupCodes []string `json:"backup_codes"`
}

// VerifyMFARequest is the request to verify MFA during login
type VerifyMFARequest struct {
	Code       string `json:"code,omitempty"`        // TOTP code
	BackupCode string `json:"backup_code,omitempty"` // Backup code (alternative)
}

// InitSetup initiates MFA setup for a user
// Requires MFA setup token
func (h *MFAHandler) InitSetup(w http.ResponseWriter, r *http.Request) {
	if !h.mfaRequired {
		writeError(w, http.StatusServiceUnavailable, "mfa_disabled", "MFA is disabled in this environment")
		return
	}

	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		slog.WarnContext(r.Context(), "mfa init setup: no claims in context")
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Get user to verify they exist
	user, err := h.userRepo.GetByID(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Your account information could not be retrieved. Please try again.", "mfa", "get_user")
		return
	}

	// Generate TOTP secret
	key, err := h.mfaService.GenerateSecret(user.Email)
	if err != nil {
		writeServerError(w, r, err, "MFA setup could not be completed. Please try again.", "mfa", "generate_secret")
		return
	}

	// Encrypt and store the secret
	encryptedSecret, err := h.mfaService.EncryptSecret(key.Secret())
	if err != nil {
		writeServerError(w, r, err, "MFA setup could not be completed. Please try again.", "mfa", "encrypt_secret")
		return
	}

	if err := h.userRepo.SetMFASecret(r.Context(), user.ID, encryptedSecret); err != nil {
		writeServerError(w, r, err, "MFA setup could not be completed. Please try again.", "mfa", "store_mfa_secret")
		return
	}

	// Generate QR code
	qrCode, err := h.mfaService.GenerateQRCode(key)
	if err != nil {
		writeServerError(w, r, err, "MFA setup could not be completed. Please try again.", "mfa", "generate_qr_code")
		return
	}

	// Audit log: MFA setup started
	if h.auditRepo != nil {
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionMFASetupStarted, nil, nil, nil), "mfa_setup_started")
	}

	writeJSON(w, http.StatusOK, SetupMFAResponse{
		QRCode: qrCode,
		Secret: key.Secret(),
	})
}

// CompleteSetup completes MFA setup by verifying a code and generating backup codes
// Requires MFA setup token
func (h *MFAHandler) CompleteSetup(w http.ResponseWriter, r *http.Request) {
	if !h.mfaRequired {
		writeError(w, http.StatusServiceUnavailable, "mfa_disabled", "MFA is disabled in this environment")
		return
	}

	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req CompleteMFASetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "TOTP code is required")
		return
	}

	// Get user
	user, err := h.userRepo.GetByID(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Your account information could not be retrieved. Please try again.", "mfa", "get_user")
		return
	}

	if user.MFASecret == nil || *user.MFASecret == "" {
		writeError(w, http.StatusBadRequest, "mfa_not_initialized", "MFA setup has not been initiated")
		return
	}

	// Validate the code
	valid, err := h.mfaService.ValidateCode(*user.MFASecret, req.Code)
	if err != nil {
		writeServerError(w, r, err, "Code verification could not be completed. Please try again.", "mfa", "validate_code")
		return
	}

	if !valid {
		writeError(w, http.StatusBadRequest, "invalid_code", "Invalid TOTP code")
		return
	}

	// Generate backup codes
	backupCodes, err := h.mfaService.GenerateBackupCodes(10)
	if err != nil {
		writeServerError(w, r, err, "MFA setup could not be completed. Please try again.", "mfa", "generate_backup_codes")
		return
	}

	// Hash and store backup codes
	hashedCodes, err := h.mfaService.HashBackupCodes(backupCodes)
	if err != nil {
		writeServerError(w, r, err, "MFA setup could not be completed. Please try again.", "mfa", "hash_backup_codes")
		return
	}

	// Enable MFA
	if err := h.userRepo.EnableMFA(r.Context(), user.ID, hashedCodes); err != nil {
		writeServerError(w, r, err, "MFA setup could not be completed. Please try again.", "mfa", "enable_mfa")
		return
	}

	// Audit log: MFA setup completed
	if h.auditRepo != nil {
		logAuditError(r, h.auditRepo.Log(r.Context(), &user.ID, models.AuditActionMFASetupCompleted, nil, nil, nil), "mfa_setup_completed")
	}

	// Update last login
	_ = h.userRepo.UpdateLastLogin(r.Context(), user.ID)

	// Generate full token now that MFA is complete
	user.MFAEnabled = true
	user.MFASetupRequired = false
	fullToken, err := h.jwtAuth.GenerateToken(user)
	if err != nil {
		writeServerError(w, r, err, "Authentication could not be completed. Please try again.", "mfa", "generate_token")
		return
	}

	// Set cookie with full token
	sameSite := http.SameSiteLaxMode
	if h.secureCookies {
		sameSite = http.SameSiteStrictMode
	}
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure and SameSite set dynamically via h.secureCookies
		Name:     "token",
		Value:    fullToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: sameSite,
		MaxAge:   86400, // 24 hours
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"backup_codes": backupCodes,
		"token":        fullToken,
		"user": map[string]interface{}{
			"id":                user.ID,
			"username":          user.Username,
			"email":             user.Email,
			"verification_tier": user.VerificationTier,
			"postcard_verified": user.PostcardVerified,
			"vouch_verified":    user.VouchVerified,
			"is_superuser":      user.IsSuperuser,
			"mfa_enabled":       true,
		},
	})
}

// handleMFAFailure increments the MFA failure counter and returns the appropriate response.
// Returns 429 if the lockout threshold is reached, otherwise 401 with remaining attempts.
func (h *MFAHandler) handleMFAFailure(w http.ResponseWriter, r *http.Request, userID, errorCode, errorMessage string) {
	failedCount, incrErr := h.userRepo.IncrementFailedMFAAttempts(r.Context(), userID)
	if incrErr != nil {
		slog.WarnContext(r.Context(), "failed to increment MFA attempts", "user_id", userID, "error", incrErr)
		// Fail closed: treat DB errors as lockout to prevent bypass
		writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"error":              "mfa_locked",
			"message":            "Too many failed MFA attempts. Please log in again to retry.",
			"remaining_attempts": 0,
		})
		return
	}

	if h.auditRepo != nil {
		logAuditError(r, h.auditRepo.Log(r.Context(), &userID, models.AuditActionMFAVerifyFailed, nil, nil, map[string]string{
			"failed_attempts": fmt.Sprintf("%d", failedCount),
		}), "mfa_verify_failed")
	}

	if failedCount >= mfaLockoutThreshold {
		if h.auditRepo != nil {
			logAuditError(r, h.auditRepo.Log(r.Context(), &userID, models.AuditActionMFALocked, nil, nil, map[string]string{
				"failed_attempts": fmt.Sprintf("%d", failedCount),
			}), "mfa_locked")
		}
		writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"error":              "mfa_locked",
			"message":            "Too many failed MFA attempts. Please log in again to retry.",
			"remaining_attempts": 0,
		})
		return
	}

	remaining := mfaLockoutThreshold - failedCount
	writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
		"error":              errorCode,
		"message":            errorMessage,
		"remaining_attempts": remaining,
	})
}

// Verify verifies a TOTP code or backup code during login
// Requires pending MFA token
func (h *MFAHandler) Verify(w http.ResponseWriter, r *http.Request) {
	if !h.mfaRequired {
		writeError(w, http.StatusServiceUnavailable, "mfa_disabled", "MFA is disabled in this environment")
		return
	}

	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req VerifyMFARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Code == "" && req.BackupCode == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "TOTP code or backup code is required")
		return
	}

	// Get user
	user, err := h.userRepo.GetByID(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Your account information could not be retrieved. Please try again.", "mfa", "get_user")
		return
	}

	if !user.MFAEnabled {
		writeError(w, http.StatusBadRequest, "mfa_not_enabled", "MFA is not enabled for this user")
		return
	}

	if user.MFASecret == nil || *user.MFASecret == "" {
		writeServerError(w, r, fmt.Errorf("MFA secret is nil or empty for user %s", user.ID), "MFA verification could not be completed. Please try again.", "mfa", "mfa_config_check")
		return
	}

	// Check if MFA attempts are already exhausted
	if user.FailedMFAAttempts >= mfaLockoutThreshold {
		if h.auditRepo != nil {
			logAuditError(r, h.auditRepo.Log(r.Context(), &user.ID, models.AuditActionMFALocked, nil, nil, map[string]string{
				"failed_attempts": fmt.Sprintf("%d", user.FailedMFAAttempts),
				"reason":          "already_locked",
			}), "mfa_locked")
		}
		writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"error":              "mfa_locked",
			"message":            "Too many failed MFA attempts. Please log in again to retry.",
			"remaining_attempts": 0,
		})
		return
	}

	var authenticated bool
	var currentBackupCodes string

	if req.Code != "" {
		// Validate TOTP code
		valid, err := h.mfaService.ValidateCode(*user.MFASecret, req.Code)
		if err != nil {
			writeServerError(w, r, err, "Code verification could not be completed. Please try again.", "mfa", "validate_totp_code")
			return
		}
		authenticated = valid
		if user.MFABackupCodes != nil {
			currentBackupCodes = *user.MFABackupCodes
		}
	} else if req.BackupCode != "" {
		// Validate backup code within a transaction to prevent race conditions
		if h.db != nil {
			txErr := h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
				// Lock user row and get fresh backup codes
				lockedUser, err := h.userRepo.GetByIDForUpdate(r.Context(), tx, user.ID)
				if err != nil {
					return err
				}

				if lockedUser.MFABackupCodes == nil || *lockedUser.MFABackupCodes == "" {
					return services.ErrNoBackupCodes
				}

				updatedCodes, err := h.mfaService.ValidateBackupCode(*lockedUser.MFABackupCodes, req.BackupCode)
				if err != nil {
					return err
				}

				if err := h.userRepo.UpdateBackupCodesTx(r.Context(), tx, user.ID, updatedCodes); err != nil {
					return err
				}

				currentBackupCodes = updatedCodes
				return nil
			})
			if txErr != nil {
				if txErr == services.ErrInvalidBackupCode {
					h.handleMFAFailure(w, r, user.ID, "invalid_backup_code", "Invalid backup code")
					return
				}
				if txErr == services.ErrNoBackupCodes {
					writeError(w, http.StatusBadRequest, "no_backup_codes", "No backup codes remaining")
					return
				}
				writeServerError(w, r, txErr, "Backup code verification could not be completed. Please try again.", "mfa", "validate_backup_code")
				return
			}
			authenticated = true
		} else {
			// Fallback for when db is not available (e.g., tests)
			if user.MFABackupCodes == nil || *user.MFABackupCodes == "" {
				writeError(w, http.StatusBadRequest, "no_backup_codes", "No backup codes remaining")
				return
			}

			updatedCodes, err := h.mfaService.ValidateBackupCode(*user.MFABackupCodes, req.BackupCode)
			if err != nil {
				if err == services.ErrInvalidBackupCode {
					h.handleMFAFailure(w, r, user.ID, "invalid_backup_code", "Invalid backup code")
					return
				}
				if err == services.ErrNoBackupCodes {
					writeError(w, http.StatusBadRequest, "no_backup_codes", "No backup codes remaining")
					return
				}
				writeServerError(w, r, err, "Backup code verification could not be completed. Please try again.", "mfa", "validate_backup_code")
				return
			}

			if err := h.userRepo.UpdateBackupCodes(r.Context(), user.ID, updatedCodes); err != nil {
				writeServerError(w, r, err, "Backup code verification could not be completed. Please try again.", "mfa", "update_backup_codes")
				return
			}

			authenticated = true
			currentBackupCodes = updatedCodes
		}
	}

	if !authenticated {
		h.handleMFAFailure(w, r, user.ID, "invalid_code", "Invalid MFA code")
		return
	}

	// Reset MFA attempt counter on successful verification
	if user.FailedMFAAttempts > 0 {
		if resetErr := h.userRepo.ResetFailedMFAAttempts(r.Context(), user.ID); resetErr != nil {
			slog.WarnContext(r.Context(), "failed to reset MFA attempts", "user_id", user.ID, "error", resetErr)
		}
	}

	// Audit log: MFA verification succeeded
	if h.auditRepo != nil {
		logAuditError(r, h.auditRepo.Log(r.Context(), &user.ID, models.AuditActionMFAVerified, nil, nil, nil), "mfa_verified")
	}

	// Update last login
	_ = h.userRepo.UpdateLastLogin(r.Context(), user.ID)

	// Generate full token
	fullToken, err := h.jwtAuth.GenerateToken(user)
	if err != nil {
		writeServerError(w, r, err, "Authentication could not be completed. Please try again.", "mfa", "generate_token")
		return
	}

	// Set cookie with full token
	sameSite := http.SameSiteLaxMode
	if h.secureCookies {
		sameSite = http.SameSiteStrictMode
	}
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure and SameSite set dynamically via h.secureCookies
		Name:     "token",
		Value:    fullToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: sameSite,
		MaxAge:   86400, // 24 hours
	})

	// Get remaining backup code count
	backupCodeCount := h.mfaService.GetBackupCodeCount(currentBackupCodes)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":              fullToken,
		"backup_codes_count": backupCodeCount,
		"user": map[string]interface{}{
			"id":                user.ID,
			"username":          user.Username,
			"email":             user.Email,
			"verification_tier": user.VerificationTier,
			"postcard_verified": user.PostcardVerified,
			"vouch_verified":    user.VouchVerified,
			"is_superuser":      user.IsSuperuser,
			"mfa_enabled":       user.MFAEnabled,
		},
	})
}
