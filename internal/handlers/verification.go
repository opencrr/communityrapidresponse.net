package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

const (
	maxVerificationRequestsPer30Days = 3

	// Verification code lockout: max failed attempts before code is permanently locked
	verificationCodeLockoutThreshold = 5
)

// VerificationHandler handles verification endpoints
type VerificationHandler struct {
	db                  *database.DB
	verificationRepo    *database.VerificationRepository
	userRepo            *database.UserRepository
	regionRepo          *database.RegionRepository
	postgridService     services.PostgridServiceInterface
	mapboxService       services.MapboxServiceInterface
	auditRepo           *database.AuditRepository
	notificationService NotificationServiceInterface
	statusCache         *middleware.UserStatusCache
	jwtAuth             *middleware.JWTAuth
	secureCookies       bool
}

// NewVerificationHandler creates a new verification handler
func NewVerificationHandler(
	db *database.DB,
	verificationRepo *database.VerificationRepository,
	userRepo *database.UserRepository,
	regionRepo *database.RegionRepository,
	postgridService services.PostgridServiceInterface,
	mapboxService services.MapboxServiceInterface,
	auditRepo *database.AuditRepository,
) *VerificationHandler {
	return &VerificationHandler{
		db:               db,
		verificationRepo: verificationRepo,
		userRepo:         userRepo,
		regionRepo:       regionRepo,
		postgridService:  postgridService,
		mapboxService:    mapboxService,
		auditRepo:        auditRepo,
	}
}

// SetNotificationService sets the notification service for the handler.
// This is optional - if not set, notifications will not be sent.
func (h *VerificationHandler) SetNotificationService(svc NotificationServiceInterface) {
	h.notificationService = svc
}

// SetStatusCache sets the user status cache for immediate cache eviction
// on verification changes. Optional - if not set, cache eviction is skipped.
func (h *VerificationHandler) SetStatusCache(cache *middleware.UserStatusCache) {
	h.statusCache = cache
}

// SetJWTAuth sets the JWT auth for issuing fresh tokens after verification.
// Also needs secureCookies to set the httpOnly cookie correctly.
// Optional - if not set, no fresh token is returned after verification.
func (h *VerificationHandler) SetJWTAuth(jwtAuth *middleware.JWTAuth, secureCookies bool) {
	h.jwtAuth = jwtAuth
	h.secureCookies = secureCookies
}

// RequestPostcardVerification handles POST /api/v1/verification/postcard/request
// Requires vouch verification first — postcard upgrades a vouched user to admin.
func (h *VerificationHandler) RequestPostcardVerification(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !claims.VouchVerified {
		writeError(w, http.StatusForbidden, "vouch_required",
			"You must be vouch-verified before requesting address verification")
		return
	}

	var req models.PostcardVerificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate address fields (region_id is now optional)
	if req.Address.Line1 == "" || req.Address.City == "" || req.Address.State == "" || req.Address.PostalCode == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Complete address is required")
		return
	}

	// Pre-check rate limiting (non-locking, for fast rejection before external API calls)
	recentCount, err := h.verificationRepo.CountRecentByUser(r.Context(), claims.UserID, 30)
	if err != nil {
		writeServerError(w, r, err, "Failed to check rate limit", "verification", "request_verification")
		return
	}
	if recentCount >= maxVerificationRequestsPer30Days {
		writeError(w, http.StatusTooManyRequests, "rate_limit", "Maximum verification requests exceeded. Please wait before requesting again.")
		return
	}

	// Step 1: Validate address with Postgrid (address in memory only)
	validationResult, err := h.postgridService.ValidateAddress(r.Context(), &req.Address)
	if err != nil {
		slog.ErrorContext(r.Context(), "postgrid address validation failed", "error", err)
		_ = appSentry.CaptureErrorWithContext(r.Context(), err, "component", "postgrid", "operation", "address_validation")
		writeError(w, http.StatusBadGateway, "address_validation_failed", "Failed to validate address")
		return
	}

	if !validationResult.IsDeliverable {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":   "invalid_address",
			"message": "Address validation failed",
			"validation_result": map[string]interface{}{
				"deliverability": validationResult.Deliverability,
				"reason":         validationResult.Reason,
			},
		})
		return
	}

	if validationResult.IsPOBox {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":   "po_box_not_allowed",
			"message": "PO Box addresses cannot be used for verification. Please provide a residential street address.",
			"validation_result": map[string]interface{}{
				"address_type":   "po_box",
				"deliverability": "deliverable",
			},
		})
		return
	}

	if validationResult.IsCMRA {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":   "cmra_not_allowed",
			"message": "Commercial mail receiving agencies (e.g., UPS Store mailboxes) cannot be used for verification. Please provide your actual residential address.",
			"validation_result": map[string]interface{}{
				"address_type": "street",
				"cmra":         true,
			},
		})
		return
	}

	if validationResult.IsCommercial {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":   "commercial_not_allowed",
			"message": "Business addresses cannot be used for verification. Please provide a residential address where you live.",
			"validation_result": map[string]interface{}{
				"address_type": validationResult.AddressType,
				"commercial":   true,
			},
		})
		return
	}

	// Step 2: Geocode address with Mapbox (address still in memory only)
	geocodeResult, err := h.mapboxService.GeocodeAddress(r.Context(), &req.Address)
	if err != nil {
		slog.ErrorContext(r.Context(), "mapbox geocoding failed", "error", err)
		_ = appSentry.CaptureErrorWithContext(r.Context(), err, "component", "mapbox", "operation", "geocoding")
		writeError(w, http.StatusBadGateway, "geocoding_failed", "Failed to geocode address")
		return
	}

	// Step 3: Determine or create the region for this address
	var regionID string
	var regionCreated bool

	if req.RegionID != "" {
		// User specified a region - verify address is within it
		isInRegion, err := h.regionRepo.IsPointInRegion(r.Context(), req.RegionID, geocodeResult.Latitude, geocodeResult.Longitude)
		if err != nil {
			if errors.Is(err, database.ErrRegionNotFound) {
				writeError(w, http.StatusNotFound, "region_not_found", "Community not found")
				return
			}
			writeServerError(w, r, err, "Failed to verify address location", "verification", "request_verification")
			return
		}
		if !isInRegion {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error":   "address_outside_region",
				"message": "The provided address is not within the selected geographic region",
				"validated_coordinates": map[string]float64{
					"lat": geocodeResult.Latitude,
					"lng": geocodeResult.Longitude,
				},
			})
			return
		}
		regionID = req.RegionID
	} else {
		// No region specified - find existing or create new
		existingRegions, err := h.regionRepo.GetRegionsContainingPoint(r.Context(), geocodeResult.Latitude, geocodeResult.Longitude)
		if err != nil {
			writeServerError(w, r, err, "Failed to find regions", "verification", "request_verification")
			return
		}

		// Check if a city-level or more specific region exists
		// (existingRegions are ordered: city_block, neighborhood, locality, city, county, state)
		var cityOrMoreSpecificRegion *models.RegionSummary
		for i := range existingRegions {
			rt := existingRegions[i].RegionType
			if rt == models.RegionTypeCityBlock ||
				rt == models.RegionTypeNeighborhood ||
				rt == models.RegionTypeLocality ||
				rt == models.RegionTypeCity {
				cityOrMoreSpecificRegion = &existingRegions[i]
				break
			}
		}

		if cityOrMoreSpecificRegion != nil {
			// Use the existing city-level or more specific region
			regionID = cityOrMoreSpecificRegion.ID
		} else {
			// No city-level region exists - create the city hierarchy
			// (even if state/county regions exist)
			createdRegionID, err := h.createRegionHierarchy(r.Context(), geocodeResult, &req.Address, claims.UserID)
			if err != nil {
				writeServerError(w, r, err, "Failed to create region", "verification", "create_hierarchy")
				return
			}
			regionID = createdRegionID
			regionCreated = true
		}
	}

	// Step 4: Generate verification code and postcard reference
	code, err := generateVerificationCode()
	if err != nil {
		writeServerError(w, r, err, "Failed to generate verification code", "verification", "request_verification")
		return
	}
	postcardRef, err := generatePostcardRef()
	if err != nil {
		writeServerError(w, r, err, "Failed to generate postcard reference", "verification", "request_verification")
		return
	}

	// Step 5: Atomic rate-limit check + insert BEFORE sending postcard.
	// The FOR UPDATE lock serializes concurrent requests per user, preventing
	// race conditions even across multiple application instances.
	// We insert with a placeholder postgrid_request_id and update it after sending.
	verificationReq := &models.VerificationRequest{
		UserID:            claims.UserID,
		RegionID:          &regionID,
		VerificationCode:  code,
		Status:            models.VerificationStatusMailed,
		PostgridRequestID: "pending",
		BoundaryType:      geocodeResult.BoundaryType,
		BoundaryName:      geocodeResult.BoundaryName,
		BoundaryState:     geocodeResult.BoundaryState,
		PostcardRef:       &postcardRef,
	}

	if h.db != nil {
		txErr := h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			lockedCount, err := h.verificationRepo.CountRecentByUserForUpdate(r.Context(), tx, claims.UserID, 30)
			if err != nil {
				return err
			}
			if lockedCount >= maxVerificationRequestsPer30Days {
				return database.ErrRateLimited
			}
			return h.verificationRepo.CreateVerificationRequestTx(r.Context(), tx, verificationReq)
		})
		if txErr != nil {
			if errors.Is(txErr, database.ErrRateLimited) {
				writeError(w, http.StatusTooManyRequests, "rate_limit", "Maximum verification requests exceeded. Please wait before requesting again.")
				return
			}
			writeServerError(w, r, txErr, "Failed to create verification request", "verification", "request_verification")
			return
		}
	} else {
		if err := h.verificationRepo.CreateVerificationRequest(r.Context(), verificationReq); err != nil {
			writeServerError(w, r, err, "Failed to create verification request", "verification", "request_verification")
			return
		}
	}

	// Step 6: Send postcard via Postgrid (address sent to Postgrid, not stored by us)
	postgridRequestID, err := h.postgridService.SendPostcard(r.Context(), &req.Address, code, postcardRef)
	if err != nil {
		slog.ErrorContext(r.Context(), "postgrid postcard sending failed", "error", err)
		_ = appSentry.CaptureErrorWithContext(r.Context(), err, "component", "postgrid", "operation", "send_postcard")
		// Clean up the reserved row since the postcard was never sent
		_ = h.verificationRepo.Delete(r.Context(), verificationReq.ID)
		writeError(w, http.StatusBadGateway, "mail_service_error", "Failed to send verification postcard")
		return
	}

	// Step 7: Update the verification request with the real postgrid_request_id.
	// Non-fatal: the postcard was sent and the DB row exists, so the user's
	// verification flow works regardless. Nothing branches on this column's value.
	if err := h.verificationRepo.UpdatePostgridRequestID(r.Context(), verificationReq.ID, postgridRequestID); err != nil {
		slog.ErrorContext(r.Context(), "failed to update postgrid_request_id", "verification_id", verificationReq.ID, "error", err)
		_ = appSentry.CaptureErrorWithContext(r.Context(), err, "component", "verification", "operation", "update_postgrid_id")
	}

	// Audit log: postcard requested
	if h.auditRepo != nil {
		resourceType := "verification_request"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionPostcardRequested, &resourceType, &verificationReq.ID, map[string]interface{}{
			"region_id":      regionID,
			"region_created": regionCreated,
		}), "postcard_requested")
	}

	// Get region details for response
	region, _ := h.regionRepo.GetByID(r.Context(), regionID)
	var regionInfo *models.RegionInfo
	if region != nil {
		regionInfo = &models.RegionInfo{
			ID:      region.ID,
			Name:    region.Name,
			Type:    string(region.RegionType),
			Created: regionCreated,
		}
	}

	// Address is now discarded from memory - NEVER written to database

	writeJSON(w, http.StatusCreated, models.PostcardVerificationResponse{
		VerificationID:    verificationReq.ID,
		Status:            "mailed",
		ExpiresAt:         verificationReq.ExpiresAt,
		EstimatedDelivery: "3-5 business days",
		PrivacyNotice:     "Your address has been sent to our mailing partner and was not stored in our system.",
		PostcardRef:       postcardRef,
		DetectedBoundary: &models.BoundaryInfo{
			Type:  geocodeResult.BoundaryType,
			Name:  geocodeResult.BoundaryName,
			State: geocodeResult.BoundaryState,
		},
		Region: regionInfo,
	})
}

// VerifyCode handles POST /api/v1/verification/postcard/verify
func (h *VerificationHandler) VerifyCode(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req models.VerifyCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.VerificationCode == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Verification code is required")
		return
	}

	// Normalize code to lowercase (codes are stored as lowercase hex)
	code := strings.ToLower(strings.TrimSpace(req.VerificationCode))

	// Variables populated inside or outside the transaction
	var verificationReq *models.VerificationRequest
	var isAdmin bool

	if h.db != nil {
		txErr := h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			// Lock verification request by code
			var err error
			verificationReq, err = h.verificationRepo.GetByCodeForUpdate(r.Context(), tx, code)
			if err != nil {
				return err
			}

			// Check if code is already locked out from too many failed attempts
			if verificationReq.FailedVerificationAttempts >= verificationCodeLockoutThreshold {
				return database.ErrVerificationLocked
			}

			// Verify the request belongs to this user
			if verificationReq.UserID != claims.UserID {
				// Increment failed attempts — fail-closed on DB error
				newCount, incrementErr := h.verificationRepo.IncrementFailedVerificationAttemptsTx(r.Context(), tx, verificationReq.ID)
				if incrementErr != nil || newCount >= verificationCodeLockoutThreshold {
					return database.ErrVerificationLocked
				}
				return database.ErrVerificationNotFound // Treat as not found for security
			}

			// Check if expired
			if time.Now().After(verificationReq.ExpiresAt) {
				return fmt.Errorf("code_expired")
			}

			// Check if already verified
			if verificationReq.Status == models.VerificationStatusVerified {
				return database.ErrAlreadyVerified
			}

			// Mark as verified
			if err := h.verificationRepo.MarkVerifiedTx(r.Context(), tx, verificationReq.ID); err != nil {
				return err
			}

			// Update user tier to Tier 1 (postcard verified)
			if err := h.userRepo.UpdateVerificationTierTx(r.Context(), tx, claims.UserID, models.TierPostcard); err != nil {
				return err
			}

			// Mark user as postcard verified
			if err := h.userRepo.SetPostcardVerifiedTx(r.Context(), tx, claims.UserID, true); err != nil {
				return err
			}

			// Get user to determine admin status
			user, err := h.userRepo.GetByIDForUpdate(r.Context(), tx, claims.UserID)
			if err != nil {
				return err
			}
			isAdmin = user.VouchVerified

			// Add user to region
			if verificationReq.RegionID != nil {
				if err := h.regionRepo.AddUserToRegionTx(r.Context(), tx, claims.UserID, *verificationReq.RegionID, isAdmin); err != nil {
					slog.WarnContext(r.Context(), "failed to add user to region", "error", err)
				}
			}

			// If user is now a full admin, upgrade is_admin on all existing regions
			if isAdmin {
				adminUpgradeCount, err := h.regionRepo.UpgradeVerifiedUserRegionsToAdminTx(r.Context(), tx, claims.UserID)
				if err != nil {
					slog.WarnContext(r.Context(), "failed to upgrade verified user_regions to admin", "user_id", claims.UserID, "error", err)
				} else if adminUpgradeCount > 0 {
					slog.InfoContext(r.Context(), "upgraded verified user_regions to admin after postcard verification", "count", adminUpgradeCount, "user_id", claims.UserID)
				}
			}

			return nil
		})
		if txErr != nil {
			if errors.Is(txErr, database.ErrVerificationLocked) {
				writeError(w, http.StatusTooManyRequests, "verification_code_locked",
					"Too many failed attempts. This verification code has been locked. Please request a new postcard.")
				return
			}
			if errors.Is(txErr, database.ErrVerificationNotFound) {
				writeError(w, http.StatusNotFound, "invalid_code", "Invalid verification code")
				return
			}
			if txErr.Error() == "code_expired" {
				writeError(w, http.StatusGone, "code_expired", "Verification code has expired. Please request a new postcard.")
				return
			}
			if errors.Is(txErr, database.ErrAlreadyVerified) {
				writeError(w, http.StatusConflict, "already_verified", "This code has already been used")
				return
			}
			writeServerError(w, r, txErr, "Failed to verify code", "verification", "confirm_verification")
			return
		}
	} else {
		// Fallback for when db is not available (e.g., tests)
		var err error
		verificationReq, err = h.verificationRepo.GetByCode(r.Context(), code)
		if errors.Is(err, database.ErrVerificationNotFound) {
			writeError(w, http.StatusNotFound, "invalid_code", "Invalid verification code")
			return
		}
		if err != nil {
			writeServerError(w, r, err, "Failed to verify code", "verification", "confirm_verification")
			return
		}
		// Check if code is already locked out
		if verificationReq.FailedVerificationAttempts >= verificationCodeLockoutThreshold {
			writeError(w, http.StatusTooManyRequests, "verification_code_locked",
				"Too many failed attempts. This verification code has been locked. Please request a new postcard.")
			return
		}
		if verificationReq.UserID != claims.UserID {
			// Increment failed attempts — fail-closed on DB error
			newCount, incrementErr := h.verificationRepo.IncrementFailedVerificationAttempts(r.Context(), verificationReq.ID)
			if incrementErr != nil || newCount >= verificationCodeLockoutThreshold {
				writeError(w, http.StatusTooManyRequests, "verification_code_locked",
					"Too many failed attempts. This verification code has been locked. Please request a new postcard.")
				return
			}
			writeError(w, http.StatusForbidden, "invalid_code", "Invalid verification code")
			return
		}
		if time.Now().After(verificationReq.ExpiresAt) {
			writeError(w, http.StatusGone, "code_expired", "Verification code has expired. Please request a new postcard.")
			return
		}
		if verificationReq.Status == models.VerificationStatusVerified {
			writeError(w, http.StatusConflict, "already_verified", "This code has already been used")
			return
		}
		if err := h.verificationRepo.MarkVerified(r.Context(), verificationReq.ID); err != nil {
			writeServerError(w, r, err, "Failed to complete verification", "verification", "confirm_verification")
			return
		}
		if err := h.userRepo.UpdateVerificationTier(r.Context(), claims.UserID, models.TierPostcard); err != nil {
			writeServerError(w, r, err, "Failed to update user tier", "verification", "confirm_verification")
			return
		}
		_ = h.userRepo.SetPostcardVerified(r.Context(), claims.UserID, true)
		user, err := h.userRepo.GetByID(r.Context(), claims.UserID)
		isAdmin = err == nil && user != nil && user.VouchVerified
		if verificationReq.RegionID != nil {
			_ = h.regionRepo.AddUserToRegion(r.Context(), claims.UserID, *verificationReq.RegionID, isAdmin)
		}
		if isAdmin {
			_, _ = h.regionRepo.UpgradeVerifiedUserRegionsToAdmin(r.Context(), claims.UserID)
		}
	}

	// Invalidate status cache so any cached state is refreshed.
	// We intentionally skip InvalidateTokens here: postcard verification is a
	// privilege upgrade, so stale claims in old tokens only grant less access.
	// Calling InvalidateTokens would reject the fresh token we issue below
	// due to second-precision timestamp equality (iat == token_invalidated_at).
	if h.statusCache != nil {
		h.statusCache.Invalidate(claims.UserID)
	}

	// Generate fresh token with updated claims so the SPA can continue without re-login
	var freshToken string
	if h.jwtAuth != nil {
		freshUser, err := h.userRepo.GetByID(r.Context(), claims.UserID)
		if err == nil && freshUser != nil {
			freshToken, err = h.jwtAuth.GenerateToken(freshUser)
			if err != nil {
				slog.WarnContext(r.Context(), "failed to generate fresh token after postcard verification", "user_id", claims.UserID, "error", err)
			} else {
				sameSite := http.SameSiteLaxMode
				if h.secureCookies {
					sameSite = http.SameSiteStrictMode
				}
				http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure and SameSite set dynamically via h.secureCookies
					Name:     "token",
					Value:    freshToken,
					Path:     "/",
					HttpOnly: true,
					Secure:   h.secureCookies,
					SameSite: sameSite,
					MaxAge:   86400,
				})
			}
		}
	}

	// Side effects outside transaction
	// Audit log: postcard verified
	if h.auditRepo != nil {
		resourceType := "user"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionPostcardVerified, &resourceType, &claims.UserID, map[string]interface{}{
			"region_id": verificationReq.RegionID,
		}), "postcard_verified")
	}

	// Queue verification complete notification
	if h.notificationService != nil && verificationReq.RegionID != nil {
		if err := h.notificationService.QueueVerificationComplete(r.Context(), claims.UserID, *verificationReq.RegionID); err != nil {
			slog.WarnContext(r.Context(), "failed to queue verification complete notification", "error", err)
		}
	}

	// Delete the verification request (code no longer needed)
	_ = h.verificationRepo.Delete(r.Context(), verificationReq.ID)

	var adminRegions []string
	if verificationReq.RegionID != nil {
		adminRegions = append(adminRegions, *verificationReq.RegionID)
	}

	writeJSON(w, http.StatusOK, models.VerifyCodeResponse{
		Success: true,
		Token:   freshToken,
		User: &models.UserVerificationResult{
			VerificationTier: models.TierPostcard,
			AdminRegions:     adminRegions,
		},
	})
}

// GetStatus handles GET /api/v1/verification/status
// Returns the current user's verification status including any pending requests
func (h *VerificationHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Get user to check verification tier
	user, err := h.userRepo.GetByID(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get user", "verification", "get_status")
		return
	}

	// Check for pending postcard verification requests
	pendingRequests, err := h.verificationRepo.GetPendingByUserID(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check verification status", "verification", "get_status")
		return
	}

	response := map[string]interface{}{
		"verification_tier": user.VerificationTier,
		"postcard_verified": user.PostcardVerified,
		"vouch_verified":    user.VouchVerified,
		"pending_request":   len(pendingRequests) > 0,
	}

	if len(pendingRequests) > 0 {
		// Return info about the most recent pending postcard request
		latestRequest := pendingRequests[0]
		response["request_id"] = latestRequest.ID
		response["request_created_at"] = latestRequest.CreatedAt
		response["request_expires_at"] = latestRequest.ExpiresAt
		if latestRequest.PostcardRef != nil && *latestRequest.PostcardRef != "" {
			response["postcard_ref"] = *latestRequest.PostcardRef
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// createRegionHierarchy creates the full region hierarchy (state -> county -> city -> locality -> neighborhood)
// and returns the ID of the most specific region. If parent regions already exist, they are reused.
func (h *VerificationHandler) createRegionHierarchy(ctx context.Context, geocodeResult *services.GeocodeResult, address *models.Address, userID string) (string, error) {
	lat := geocodeResult.Latitude
	lng := geocodeResult.Longitude
	stateName := geocodeResult.BoundaryState
	if stateName == "" && address != nil {
		stateName = address.State
	}

	// Step 1: Get or create State region
	stateRegionID, err := h.getOrCreateStateRegion(ctx, stateName, lat, lng, userID)
	if err != nil {
		return "", err
	}

	// Step 2: Get or create County region (with state as parent)
	countyRegionID, err := h.getOrCreateCountyRegion(ctx, stateName, lat, lng, stateRegionID, userID)
	if err != nil {
		return "", err
	}

	// Step 3: Get or create City region (with county as parent)
	cityRegionID, err := h.getOrCreateCityRegion(ctx, geocodeResult, address, countyRegionID, userID)
	if err != nil {
		return "", err
	}

	// Step 4: Get or create Locality region (if present, with city as parent)
	// Localities are boroughs/sub-city divisions like Brooklyn in NYC
	var localityRegionID string
	if geocodeResult.LocalityName != "" {
		localityRegionID, err = h.getOrCreateLocalityRegion(ctx, geocodeResult, cityRegionID, userID)
		if err != nil {
			// Non-fatal: log and continue without locality
			slog.Warn("failed to create locality region", "error", err)
		}
	}

	// Step 5: Get or create Neighborhood region (if present)
	// Parent is locality if exists, otherwise city
	var neighborhoodRegionID string
	if geocodeResult.NeighborhoodName != "" {
		parentID := localityRegionID
		if parentID == "" {
			parentID = cityRegionID
		}
		neighborhoodRegionID, err = h.getOrCreateNeighborhoodRegion(ctx, geocodeResult, parentID, userID)
		if err != nil {
			// Non-fatal: log and continue without neighborhood
			slog.Warn("failed to create neighborhood region", "error", err)
		}
	}

	// Return the most specific region ID
	if neighborhoodRegionID != "" {
		return neighborhoodRegionID, nil
	}
	if localityRegionID != "" {
		return localityRegionID, nil
	}
	return cityRegionID, nil
}

// getOrCreateStateRegion finds an existing state region or creates a new one
func (h *VerificationHandler) getOrCreateStateRegion(ctx context.Context, stateName string, lat, lng float64, userID string) (string, error) {
	// Quick check outside transaction
	existingState, err := h.regionRepo.GetByNameAndType(ctx, stateName, models.RegionTypeState)
	if err == nil && existingState != nil {
		slog.Info("found existing state region", "name", stateName, "region_id", existingState.ID)
		return existingState.ID, nil
	}

	// State doesn't exist - fetch boundary (external API call before transaction)
	slog.Info("creating new state region", "name", stateName)
	stateBoundary, err := h.mapboxService.GetStateBoundary(ctx, stateName, lat, lng)
	if err != nil {
		return "", err
	}

	newState := &models.GeographicRegion{
		Name:       stateBoundary.Name,
		RegionType: models.RegionTypeState,
		CreatedBy:  &userID,
	}

	if h.db != nil {
		var resultID string
		txErr := h.db.Transaction(ctx, func(tx *sql.Tx) error {
			// Re-check with lock inside transaction
			existing, err := h.regionRepo.GetByNameAndTypeForUpdate(ctx, tx, stateBoundary.Name, models.RegionTypeState)
			if err == nil && existing != nil {
				resultID = existing.ID
				return nil
			}
			if err := h.regionRepo.CreateTx(ctx, tx, newState, stateBoundary.GeoJSON); err != nil {
				return err
			}
			resultID = newState.ID
			return nil
		})
		if txErr != nil {
			return "", txErr
		}
		slog.Info("state region", "name", stateBoundary.Name, "region_id", resultID)
		return resultID, nil
	}

	if err := h.regionRepo.Create(ctx, newState, stateBoundary.GeoJSON); err != nil {
		return "", err
	}
	slog.Info("created state region", "name", newState.Name, "region_id", newState.ID)
	return newState.ID, nil
}

// getOrCreateCountyRegion finds an existing county region or creates a new one
func (h *VerificationHandler) getOrCreateCountyRegion(ctx context.Context, stateName string, lat, lng float64, stateRegionID string, userID string) (string, error) {
	// External API call before transaction
	countyBoundary, err := h.mapboxService.GetCountyForCoordinates(ctx, lat, lng, stateName)
	if err != nil {
		return "", err
	}

	countyName := countyBoundary.Name

	// Quick check outside transaction
	existingCounty, err := h.regionRepo.GetByNameAndType(ctx, countyName, models.RegionTypeCounty)
	if err == nil && existingCounty != nil {
		slog.Info("found existing county region", "name", countyName, "region_id", existingCounty.ID)
		return existingCounty.ID, nil
	}

	slog.Info("creating new county region", "name", countyName)
	newCounty := &models.GeographicRegion{
		Name:           countyName,
		RegionType:     models.RegionTypeCounty,
		ParentRegionID: &stateRegionID,
		CreatedBy:      &userID,
	}

	if h.db != nil {
		var resultID string
		txErr := h.db.Transaction(ctx, func(tx *sql.Tx) error {
			existing, err := h.regionRepo.GetByNameAndTypeForUpdate(ctx, tx, countyName, models.RegionTypeCounty)
			if err == nil && existing != nil {
				resultID = existing.ID
				return nil
			}
			if err := h.regionRepo.CreateTx(ctx, tx, newCounty, countyBoundary.GeoJSON); err != nil {
				return err
			}
			resultID = newCounty.ID
			return nil
		})
		if txErr != nil {
			return "", txErr
		}
		slog.Info("county region", "name", countyName, "region_id", resultID)
		return resultID, nil
	}

	if err := h.regionRepo.Create(ctx, newCounty, countyBoundary.GeoJSON); err != nil {
		return "", err
	}
	slog.Info("created county region", "name", newCounty.Name, "region_id", newCounty.ID)
	return newCounty.ID, nil
}

// getOrCreateCityRegion finds an existing city region or creates a new one
func (h *VerificationHandler) getOrCreateCityRegion(ctx context.Context, geocodeResult *services.GeocodeResult, address *models.Address, countyRegionID string, userID string) (string, error) {
	lat := geocodeResult.Latitude
	lng := geocodeResult.Longitude

	// When a locality is present (e.g., "Brooklyn"), the address.City field may contain
	// the locality name rather than the actual city. In this case, use the geocoded
	// BoundaryName (from Mapbox "place" context) which is the true city (e.g., "New York").
	effectiveAddress := address
	if geocodeResult.LocalityName != "" && geocodeResult.BoundaryName != "" {
		effectiveAddress = &models.Address{
			Line1:      address.Line1,
			Line2:      address.Line2,
			City:       geocodeResult.BoundaryName,
			State:      address.State,
			PostalCode: address.PostalCode,
			Country:    address.Country,
		}
		slog.Info("locality detected, using geocoded city name", "locality", geocodeResult.LocalityName, "city_name", geocodeResult.BoundaryName)
	}

	// External API call before transaction
	cityBoundary, err := h.mapboxService.GetCityBoundary(ctx, lat, lng, geocodeResult, effectiveAddress)
	if err != nil {
		return "", err
	}

	cityName := cityBoundary.Name
	if cityBoundary.State != "" {
		cityName = cityBoundary.Name + ", " + cityBoundary.State
	}

	// Quick check outside transaction
	existingCity, err := h.regionRepo.GetByNameAndType(ctx, cityName, models.RegionTypeCity)
	if err == nil && existingCity != nil {
		slog.Info("found existing city region", "name", cityName, "region_id", existingCity.ID)
		return existingCity.ID, nil
	}

	slog.Info("creating new city region", "name", cityName)
	newCity := &models.GeographicRegion{
		Name:           cityName,
		RegionType:     models.RegionTypeCity,
		ParentRegionID: &countyRegionID,
		CreatedBy:      &userID,
	}

	if h.db != nil {
		var resultID string
		txErr := h.db.Transaction(ctx, func(tx *sql.Tx) error {
			existing, err := h.regionRepo.GetByNameAndTypeForUpdate(ctx, tx, cityName, models.RegionTypeCity)
			if err == nil && existing != nil {
				resultID = existing.ID
				return nil
			}
			if err := h.regionRepo.CreateTx(ctx, tx, newCity, cityBoundary.GeoJSON); err != nil {
				return err
			}
			resultID = newCity.ID
			return nil
		})
		if txErr != nil {
			return "", txErr
		}
		slog.Info("city region", "name", cityName, "region_id", resultID)
		return resultID, nil
	}

	if err := h.regionRepo.Create(ctx, newCity, cityBoundary.GeoJSON); err != nil {
		return "", err
	}
	slog.Info("created city region", "name", newCity.Name, "region_id", newCity.ID)
	return newCity.ID, nil
}

// getOrCreateLocalityRegion finds an existing locality region or creates a new one
// Localities are boroughs/sub-city divisions (e.g., Brooklyn in NYC)
func (h *VerificationHandler) getOrCreateLocalityRegion(ctx context.Context, geocodeResult *services.GeocodeResult, cityRegionID string, userID string) (string, error) {
	localityName := geocodeResult.LocalityName
	if localityName == "" {
		return "", nil
	}

	lat := geocodeResult.Latitude
	lng := geocodeResult.Longitude
	stateName := geocodeResult.BoundaryState
	cityName := geocodeResult.BoundaryName

	// Quick check outside transaction
	existingLocality, err := h.regionRepo.GetByNameTypeAndParent(ctx, localityName, models.RegionTypeLocality, cityRegionID)
	if err == nil && existingLocality != nil {
		slog.Info("found existing locality region", "name", localityName, "region_id", existingLocality.ID)
		return existingLocality.ID, nil
	}

	// External API call before transaction
	slog.Info("creating new locality region", "name", localityName)
	localityBoundary, err := h.mapboxService.GetLocalityBoundary(ctx, localityName, cityName, stateName, lat, lng)
	if err != nil {
		return "", err
	}

	newLocality := &models.GeographicRegion{
		Name:           localityBoundary.Name,
		RegionType:     models.RegionTypeLocality,
		ParentRegionID: &cityRegionID,
		CreatedBy:      &userID,
	}

	if h.db != nil {
		var resultID string
		txErr := h.db.Transaction(ctx, func(tx *sql.Tx) error {
			existing, err := h.regionRepo.GetByNameTypeAndParentForUpdate(ctx, tx, localityName, models.RegionTypeLocality, cityRegionID)
			if err == nil && existing != nil {
				resultID = existing.ID
				return nil
			}
			// CreateTx handles both empty and non-empty geoJSON
			if err := h.regionRepo.CreateTx(ctx, tx, newLocality, localityBoundary.GeoJSON); err != nil {
				return err
			}
			resultID = newLocality.ID
			return nil
		})
		if txErr != nil {
			return "", txErr
		}
		hasGeometry := localityBoundary.GeoJSON != ""
		slog.Info("locality region", "name", localityBoundary.Name, "region_id", resultID, "has_geometry", hasGeometry)
		return resultID, nil
	}

	if err := h.regionRepo.CreateWithOptionalGeometry(ctx, newLocality, localityBoundary.GeoJSON); err != nil {
		return "", err
	}
	hasGeometry := localityBoundary.GeoJSON != ""
	slog.Info("created locality region", "name", newLocality.Name, "region_id", newLocality.ID, "has_geometry", hasGeometry)
	return newLocality.ID, nil
}

// getOrCreateNeighborhoodRegion finds an existing neighborhood region or creates a new one
// Most neighborhoods (~92%) lack polygon boundaries from OSM
func (h *VerificationHandler) getOrCreateNeighborhoodRegion(ctx context.Context, geocodeResult *services.GeocodeResult, parentRegionID string, userID string) (string, error) {
	neighborhoodName := geocodeResult.NeighborhoodName
	if neighborhoodName == "" {
		return "", nil
	}

	lat := geocodeResult.Latitude
	lng := geocodeResult.Longitude
	stateName := geocodeResult.BoundaryState
	cityName := geocodeResult.BoundaryName
	localityName := geocodeResult.LocalityName

	// Quick check outside transaction
	existingNeighborhood, err := h.regionRepo.GetByNameTypeAndParent(ctx, neighborhoodName, models.RegionTypeNeighborhood, parentRegionID)
	if err == nil && existingNeighborhood != nil {
		slog.Info("found existing neighborhood region", "name", neighborhoodName, "region_id", existingNeighborhood.ID)
		return existingNeighborhood.ID, nil
	}

	// External API call before transaction
	slog.Info("creating new neighborhood region", "name", neighborhoodName)
	neighborhoodBoundary, err := h.mapboxService.GetNeighborhoodBoundary(ctx, neighborhoodName, localityName, cityName, stateName, lat, lng)
	if err != nil {
		return "", err
	}

	newNeighborhood := &models.GeographicRegion{
		Name:           neighborhoodBoundary.Name,
		RegionType:     models.RegionTypeNeighborhood,
		ParentRegionID: &parentRegionID,
		CreatedBy:      &userID,
	}

	if h.db != nil {
		var resultID string
		txErr := h.db.Transaction(ctx, func(tx *sql.Tx) error {
			existing, err := h.regionRepo.GetByNameTypeAndParentForUpdate(ctx, tx, neighborhoodName, models.RegionTypeNeighborhood, parentRegionID)
			if err == nil && existing != nil {
				resultID = existing.ID
				return nil
			}
			// CreateTx handles both empty and non-empty geoJSON
			if err := h.regionRepo.CreateTx(ctx, tx, newNeighborhood, neighborhoodBoundary.GeoJSON); err != nil {
				return err
			}
			resultID = newNeighborhood.ID
			return nil
		})
		if txErr != nil {
			return "", txErr
		}
		hasGeometry := neighborhoodBoundary.GeoJSON != ""
		slog.Info("neighborhood region", "name", neighborhoodBoundary.Name, "region_id", resultID, "has_geometry", hasGeometry)
		return resultID, nil
	}

	if err := h.regionRepo.CreateWithOptionalGeometry(ctx, newNeighborhood, neighborhoodBoundary.GeoJSON); err != nil {
		return "", err
	}
	hasGeometry := neighborhoodBoundary.GeoJSON != ""
	slog.Info("created neighborhood region", "name", newNeighborhood.Name, "region_id", newNeighborhood.ID, "has_geometry", hasGeometry)
	return newNeighborhood.ID, nil
}

// generateVerificationCode generates a random 16-character hex code (8 bytes of entropy)
func generateVerificationCode() (string, error) {
	codeBytes := make([]byte, 8)
	if _, err := rand.Read(codeBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(codeBytes), nil
}

// postcardRefCharset excludes ambiguous characters: 0/O, 1/I/L
const postcardRefCharset = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// generatePostcardRef generates a 5-character uppercase alphanumeric reference code
// printed on the postcard exterior so household members can identify their card.
// Uses rejection sampling to avoid modulo bias (charset length 30 doesn't divide 256 evenly).
func generatePostcardRef() (string, error) {
	// 240 is the largest multiple of 30 that fits in a byte (30*8=240)
	const maxUnbiased = 240
	result := make([]byte, 5)
	buf := make([]byte, 1)
	for i := range result {
		for {
			if _, err := rand.Read(buf); err != nil {
				return "", err
			}
			if int(buf[0]) < maxUnbiased {
				result[i] = postcardRefCharset[int(buf[0])%len(postcardRefCharset)] // #nosec G602 -- buf[0] safe: rand.Read fills exactly 1 byte
				break
			}
		}
	}
	return string(result), nil
}
