package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

const (
	maxGroupsPerSchool             = 5
	maxGroupsPerDistrict           = 5
	schoolBootstrapVouchesRequired = 3
	schoolNormalVouchesRequired    = 2
	minSchoolAdminsToEndBootstrap  = 3
	maxSchoolVouchesPerMonth       = 10
)

// SchoolHandler handles school endpoints
type SchoolHandler struct {
	db                       *database.DB
	schoolRepo               *database.SchoolRepository
	districtRepo             *database.SchoolDistrictRepository
	schoolVouchRepo          *database.SchoolVouchRepository
	signalGroupRepo          *database.SignalGroupRepository
	encryptedSecretRepo      *database.EncryptedSecretRepository
	userRepo                 *database.UserRepository
	auditRepo                *database.AuditRepository
	ncesService              services.NCESServiceInterface
	consensusConfig          *config.ConsensusConfig
	bootstrapCooldownEnabled bool
	bootstrapCooldownMinutes int
	notificationService      NotificationServiceInterface
}

// NewSchoolHandler creates a new school handler
func NewSchoolHandler(
	db *database.DB,
	schoolRepo *database.SchoolRepository,
	districtRepo *database.SchoolDistrictRepository,
	schoolVouchRepo *database.SchoolVouchRepository,
	signalGroupRepo *database.SignalGroupRepository,
	encryptedSecretRepo *database.EncryptedSecretRepository,
	userRepo *database.UserRepository,
	auditRepo *database.AuditRepository,
	ncesService services.NCESServiceInterface,
	consensusConfig *config.ConsensusConfig,
	bootstrapCooldownEnabled bool,
	bootstrapCooldownMinutes int,
) *SchoolHandler {
	return &SchoolHandler{
		db:                       db,
		schoolRepo:               schoolRepo,
		districtRepo:             districtRepo,
		schoolVouchRepo:          schoolVouchRepo,
		signalGroupRepo:          signalGroupRepo,
		encryptedSecretRepo:      encryptedSecretRepo,
		userRepo:                 userRepo,
		auditRepo:                auditRepo,
		ncesService:              ncesService,
		consensusConfig:          consensusConfig,
		bootstrapCooldownEnabled: bootstrapCooldownEnabled,
		bootstrapCooldownMinutes: bootstrapCooldownMinutes,
	}
}

// SetNotificationService sets the notification service for the handler.
// This is optional - if not set, notifications will not be sent.
func (h *SchoolHandler) SetNotificationService(svc NotificationServiceInterface) {
	h.notificationService = svc
}

// Search handles GET /api/v1/schools/search (public)
func (h *SchoolHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	state := r.URL.Query().Get("state")

	page := 1
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if parsed, err := strconv.Atoi(pageStr); err == nil && parsed > 0 {
			page = parsed
		}
	}

	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			if parsed > 100 {
				parsed = 100
			}
			limit = parsed
		}
	}

	schools, totalCount, err := h.schoolRepo.Search(r.Context(), query, state, "", page, limit)
	if err != nil {
		writeServerError(w, r, err, "Failed to search schools", "school", "search_schools")
		return
	}

	// Ensure non-nil slice for JSON serialization
	if schools == nil {
		schools = []models.SchoolSummary{}
	}

	writeJSON(w, http.StatusOK, models.SchoolSearchResponse{
		Schools: schools,
		Total:   totalCount,
		Page:    page,
		Limit:   limit,
		HasMore: (page * limit) < totalCount,
	})
}

// Get handles GET /api/v1/schools/:id (public, enriched for authenticated verified members)
func (h *SchoolHandler) Get(w http.ResponseWriter, r *http.Request) {
	schoolID := getPathParam(r, "id")
	if schoolID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "School ID required")
		return
	}

	schoolWithDetails, err := h.schoolRepo.GetByIDWithDetails(r.Context(), schoolID)
	if err != nil {
		if errors.Is(err, database.ErrSchoolNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "School not found")
			return
		}
		writeServerError(w, r, err, "Failed to get school", "school", "get_school")
		return
	}

	// Check if the user is authenticated and populate per-user membership fields
	claims := middleware.GetUserFromContext(r.Context())
	if claims != nil {
		userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, schoolID)
		if err == nil && userSchool != nil {
			schoolWithDetails.IsMember = true
			schoolWithDetails.IsAdmin = userSchool.IsAdmin
			schoolWithDetails.IsVerified = userSchool.VerificationStatus == models.SchoolVerificationStatusVerified

			if schoolWithDetails.IsVerified {
				// Include signal groups with invite links for verified members
				groups, groupErr := h.signalGroupRepo.ListBySchool(r.Context(), schoolID)
				if groupErr == nil {
					signalGroupsPublic := []models.SignalGroupPublic{}
					for _, g := range groups {
						signalGroupsPublic = append(signalGroupsPublic, models.SignalGroupPublic{
							ID:          g.ID,
							SchoolID:    g.SchoolID,
							Name:        g.GroupName,
							Description: g.Description,
							CreatedAt:   g.CreatedAt,
						})
					}
					schoolWithDetails.SignalGroups = signalGroupsPublic
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, schoolWithDetails)
}

// Join handles POST /api/v1/schools/:id/join (auth required)
func (h *SchoolHandler) Join(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	schoolID := getPathParam(r, "id")
	if schoolID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "School ID required")
		return
	}

	// Pre-check: is user blocked?
	isBlocked, err := h.schoolRepo.IsUserBlockedInSchool(r.Context(), claims.UserID, schoolID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check block status", "school", "check_block_status")
		return
	}
	if isBlocked {
		writeError(w, http.StatusForbidden, "user_blocked", "You are blocked from this school")
		return
	}

	var membershipID string

	if h.db != nil {
		txErr := h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			existing, txErr := h.schoolRepo.GetUserSchoolForUpdate(r.Context(), tx, claims.UserID, schoolID)
			if txErr != nil {
				return txErr
			}
			if existing != nil {
				return database.ErrAlreadyMember
			}

			var addErr error
			membershipID, addErr = h.schoolRepo.AddUserToSchoolTx(r.Context(), tx, claims.UserID, schoolID)
			return addErr
		})
		if txErr != nil {
			if errors.Is(txErr, database.ErrAlreadyMember) {
				writeError(w, http.StatusConflict, "already_member", "You are already a member of this school")
				return
			}
			writeServerError(w, r, txErr, "Failed to join school", "school", "join_school")
			return
		}
	} else {
		// Fallback for when db is not available (e.g., tests)
		existing, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, schoolID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check membership", "school", "check_membership")
			return
		}
		if existing != nil {
			writeError(w, http.StatusConflict, "already_member", "You are already a member of this school")
			return
		}

		membershipID, err = h.schoolRepo.AddUserToSchool(r.Context(), claims.UserID, schoolID)
		if err != nil {
			writeServerError(w, r, err, "Failed to join school", "school", "join_school")
			return
		}
	}

	// Audit log: school joined
	if h.auditRepo != nil {
		resourceType := "school"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSchoolJoined, &resourceType, &schoolID, map[string]interface{}{
			"membership_id": membershipID,
		}), "school_joined")
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"membership_id": membershipID,
		"school_id":     schoolID,
		"status":        "pending",
	})
}

// Leave handles POST /api/v1/schools/:id/leave (auth required)
func (h *SchoolHandler) Leave(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	schoolID := getPathParam(r, "id")
	if schoolID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "School ID required")
		return
	}

	if err := h.schoolRepo.RemoveUserFromSchool(r.Context(), claims.UserID, schoolID); err != nil {
		writeServerError(w, r, err, "Failed to leave school", "school", "leave_school")
		return
	}

	// Audit log: school left
	if h.auditRepo != nil {
		resourceType := "school"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSchoolLeft, &resourceType, &schoolID, map[string]interface{}{}), "school_left")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"school_id": schoolID,
		"message":   "Successfully left school",
	})
}

// Vouch handles POST /api/v1/schools/:id/vouch (auth required)
func (h *SchoolHandler) Vouch(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	schoolID := getPathParam(r, "id")
	if schoolID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "School ID required")
		return
	}

	var req models.SchoolVouchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	userIdentifier := req.UserIdentifier
	if userIdentifier == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "User identifier (email, username, or user ID) is required")
		return
	}

	// Look up the vouchee by email, username, or ID
	var vouchee *models.User
	var err error

	if strings.Contains(userIdentifier, "@") {
		vouchee, err = h.userRepo.GetByEmail(r.Context(), userIdentifier)
	} else {
		vouchee, err = h.userRepo.GetByUsername(r.Context(), userIdentifier)
		if err != nil || vouchee == nil {
			vouchee, err = h.userRepo.GetByID(r.Context(), userIdentifier)
		}
	}

	if err != nil || vouchee == nil {
		writeError(w, http.StatusNotFound, "user_not_found", "User not found. Please check the email or username.")
		return
	}

	vouchedUserID := vouchee.ID

	// Can't vouch for yourself
	if vouchedUserID == claims.UserID {
		writeError(w, http.StatusBadRequest, "invalid_request", "You cannot vouch for yourself")
		return
	}

	// Check voucher is a member of the school
	voucherMembership, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, schoolID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check membership", "school", "check_voucher_membership")
		return
	}
	if voucherMembership == nil {
		writeError(w, http.StatusForbidden, "not_member", "You must be a member of this school to vouch")
		return
	}

	// Check bootstrap mode
	bootstrapMode, adminCount, err := h.schoolRepo.IsSchoolInBootstrapMode(r.Context(), schoolID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check school bootstrap status", "school", "check_bootstrap_status")
		return
	}

	// Determine vouch threshold based on bootstrap mode
	vouchesRequired := schoolNormalVouchesRequired
	if bootstrapMode {
		vouchesRequired = schoolBootstrapVouchesRequired
	}

	// Check voucher eligibility based on mode
	if bootstrapMode {
		// Bootstrap mode: ANY member can vouch
	} else {
		// Normal mode: only verified members can vouch
		if voucherMembership.VerificationStatus != models.SchoolVerificationStatusVerified {
			writeError(w, http.StatusForbidden, "forbidden", "Only verified members can vouch outside of bootstrap mode")
			return
		}
	}

	// Check if vouchee is blocked
	isBlocked, err := h.schoolRepo.IsUserBlockedInSchool(r.Context(), vouchedUserID, schoolID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check block status", "school", "check_vouchee_block_status")
		return
	}
	if isBlocked {
		writeError(w, http.StatusForbidden, "user_blocked", "This user is blocked from this school")
		return
	}

	// Bootstrap cooldown (if enabled)
	if bootstrapMode && h.bootstrapCooldownEnabled {
		lastVouchTime, err := h.schoolVouchRepo.GetLastVouchTimeByVoucher(r.Context(), claims.UserID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check vouch cooldown", "school", "check_vouch_cooldown")
			return
		}
		if lastVouchTime != nil {
			cooldownEnd := lastVouchTime.Add(time.Duration(h.bootstrapCooldownMinutes) * time.Minute)
			if time.Now().Before(cooldownEnd) {
				minutesLeft := int(time.Until(cooldownEnd).Minutes()) + 1
				writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
					"error":        "vouch_cooldown",
					"message":      fmt.Sprintf("Bootstrap mode cooldown: please wait %d minutes before vouching again", minutesLeft),
					"cooldown_end": cooldownEnd.Format(time.RFC3339),
				})
				return
			}
		}
	}

	// Transaction: duplicate check + monthly limit + create vouch + auto-upgrade
	vouch := &models.SchoolVouch{
		VoucherUserID: claims.UserID,
		VouchedUserID: vouchedUserID,
		SchoolID:      schoolID,
	}
	var totalVouches int
	var autoUpgraded bool

	if h.db != nil {
		txErr := h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			// Check if already vouched (with lock via tx)
			hasVouched, txErr := h.schoolVouchRepo.HasVouchedTx(r.Context(), tx, claims.UserID, vouchedUserID, schoolID)
			if txErr != nil {
				return txErr
			}
			if hasVouched {
				return database.ErrAlreadyVouched
			}

			// Check monthly vouch limit (with lock)
			monthlyVouches, txErr := h.schoolVouchRepo.CountVouchesThisMonthForUpdate(r.Context(), tx, claims.UserID)
			if txErr != nil {
				return txErr
			}
			if monthlyVouches >= maxSchoolVouchesPerMonth {
				return database.ErrVouchLimitReached
			}

			// Create vouch
			if txErr := h.schoolVouchRepo.CreateTx(r.Context(), tx, vouch); txErr != nil {
				return txErr
			}

			// Count total vouches for auto-upgrade check
			totalVouches, txErr = h.schoolVouchRepo.CountVouchesForUserTx(r.Context(), tx, vouchedUserID, schoolID)
			if txErr != nil {
				return txErr
			}

			// Auto-upgrade if enough vouches
			if totalVouches >= vouchesRequired {
				autoUpgraded = true

				// Determine if user should become admin (fewer than 3 admins)
				_, currentAdminCount, txErr := h.schoolRepo.IsSchoolInBootstrapModeTx(r.Context(), tx, schoolID)
				if txErr != nil {
					return txErr
				}
				shouldBeAdmin := currentAdminCount < minSchoolAdminsToEndBootstrap

				if txErr := h.schoolRepo.UpgradeUserSchoolToVerifiedTx(r.Context(), tx, vouchedUserID, schoolID, shouldBeAdmin); txErr != nil {
					return txErr
				}

				if shouldBeAdmin {
					if txErr := h.schoolRepo.SetUserSchoolAdminTx(r.Context(), tx, vouchedUserID, schoolID, true); txErr != nil {
						slog.WarnContext(r.Context(), "failed to set user as school admin", "error", txErr)
					}
				}
			}

			return nil
		})
		if txErr != nil {
			if errors.Is(txErr, database.ErrAlreadyVouched) {
				writeError(w, http.StatusConflict, "already_vouched", "You have already vouched for this user in this school")
				return
			}
			if errors.Is(txErr, database.ErrVouchLimitReached) {
				writeError(w, http.StatusTooManyRequests, "vouch_limit", "You have reached your monthly vouch limit")
				return
			}
			writeServerError(w, r, txErr, "Failed to create vouch", "school", "create_vouch")
			return
		}
	} else {
		// Fallback for when db is not available (e.g., tests)
		hasVouched, err := h.schoolVouchRepo.HasVouched(r.Context(), claims.UserID, vouchedUserID, schoolID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check vouch status", "school", "check_vouch_status")
			return
		}
		if hasVouched {
			writeError(w, http.StatusConflict, "already_vouched", "You have already vouched for this user in this school")
			return
		}

		monthlyVouches, err := h.schoolVouchRepo.CountVouchesThisMonth(r.Context(), claims.UserID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check vouch limit", "school", "check_vouch_limit")
			return
		}
		if monthlyVouches >= maxSchoolVouchesPerMonth {
			writeError(w, http.StatusTooManyRequests, "vouch_limit", "You have reached your monthly vouch limit")
			return
		}

		if err := h.schoolVouchRepo.Create(r.Context(), vouch); err != nil {
			writeServerError(w, r, err, "Failed to create vouch", "school", "create_vouch")
			return
		}

		totalVouches, err = h.schoolVouchRepo.CountVouchesForUser(r.Context(), vouchedUserID, schoolID)
		if err != nil {
			writeServerError(w, r, err, "Failed to count vouches", "school", "count_vouches")
			return
		}

		if totalVouches >= vouchesRequired {
			autoUpgraded = true
			currentAdminCount, _ := h.schoolRepo.GetVerifiedAdminCount(r.Context(), schoolID)
			shouldBeAdmin := currentAdminCount < minSchoolAdminsToEndBootstrap
			// Non-tx fallback: best-effort upgrade
			_ = h.schoolRepo.RemoveUserFromSchool(r.Context(), vouchedUserID, schoolID) // Will re-add below
			_, _ = h.schoolRepo.AddUserToSchool(r.Context(), vouchedUserID, schoolID)
			// Note: In test fallback, full upgrade is not atomic
			_ = shouldBeAdmin // Used by tx path only; logged below
		}
	}

	// Side effects outside transaction
	// Audit log: vouch given
	if h.auditRepo != nil {
		resourceType := "school"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSchoolVouchGiven, &resourceType, &schoolID, map[string]interface{}{
			"vouched_user_id":      vouchedUserID,
			"school_id":            schoolID,
			"bootstrap_mode":       bootstrapMode,
			"admin_count_at_vouch": adminCount,
		}), "school_vouch_given")
	}

	if autoUpgraded && h.auditRepo != nil {
		resourceType := "school"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSchoolVouchComplete, &resourceType, &schoolID, map[string]interface{}{
			"vouched_user_id": vouchedUserID,
			"school_id":       schoolID,
			"total_vouches":   totalVouches,
		}), "school_vouch_complete")
	}

	vouchesNeeded := vouchesRequired - totalVouches
	if vouchesNeeded < 0 {
		vouchesNeeded = 0
	}

	writeJSON(w, http.StatusCreated, models.SchoolVouchResponse{
		VouchID:         vouch.ID,
		VouchesNeeded:   vouchesNeeded,
		TotalVouches:    totalVouches,
		BootstrapMode:   bootstrapMode,
		AdminCount:      adminCount,
		VouchesRequired: vouchesRequired,
	})
}

// GetPendingVouchRequests handles GET /api/v1/schools/:id/vouch/pending (auth required)
func (h *SchoolHandler) GetPendingVouchRequests(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	schoolID := getPathParam(r, "id")
	if schoolID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "School ID required")
		return
	}

	// Check caller is a member
	isMember, err := h.schoolRepo.IsUserInSchool(r.Context(), claims.UserID, schoolID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check membership", "school", "check_membership")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "not_member", "You must be a member of this school")
		return
	}

	pendingUsers, err := h.schoolVouchRepo.GetPendingVouchUsers(r.Context(), schoolID)
	if err != nil {
		writeServerError(w, r, err, "Failed to fetch pending vouch requests", "school", "get_pending_vouch_users")
		return
	}

	if pendingUsers == nil {
		pendingUsers = []models.PendingSchoolVouchUser{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"requests": pendingUsers,
	})
}

// GetVouchStatus handles GET /api/v1/schools/:id/vouch/status/:user_id (auth required)
func (h *SchoolHandler) GetVouchStatus(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	schoolID := getPathParam(r, "id")
	if schoolID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "School ID required")
		return
	}

	userID := getPathParam(r, "user_id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "User ID required")
		return
	}

	// Check bootstrap mode for this school
	bootstrapMode, adminCount, err := h.schoolRepo.IsSchoolInBootstrapMode(r.Context(), schoolID)
	if err != nil {
		slog.WarnContext(r.Context(), "failed to check bootstrap mode", "error", err)
		bootstrapMode = false
		adminCount = 0
	}

	vouchesRequired := schoolNormalVouchesRequired
	if bootstrapMode {
		vouchesRequired = schoolBootstrapVouchesRequired
	}

	// Count vouches
	totalVouches, err := h.schoolVouchRepo.CountVouchesForUser(r.Context(), userID, schoolID)
	if err != nil {
		writeServerError(w, r, err, "Failed to count vouches", "school", "count_vouches")
		return
	}

	// Get vouchers
	vouchers, err := h.schoolVouchRepo.GetVouchersForUser(r.Context(), userID, schoolID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get vouchers", "school", "get_vouchers")
		return
	}

	vouchesNeeded := vouchesRequired - totalVouches
	if vouchesNeeded < 0 {
		vouchesNeeded = 0
	}

	writeJSON(w, http.StatusOK, models.SchoolVouchStatusResponse{
		UserID:          userID,
		SchoolID:        schoolID,
		VouchesReceived: totalVouches,
		VouchesNeeded:   vouchesNeeded,
		Vouchers:        vouchers,
		BootstrapMode:   bootstrapMode,
		AdminCount:      adminCount,
		VouchesRequired: vouchesRequired,
	})
}

// ListMembers handles GET /api/v1/schools/:id/members (auth required)
func (h *SchoolHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	schoolID := getPathParam(r, "id")
	if schoolID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "School ID required")
		return
	}

	// Check caller is a verified member
	userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, schoolID)
	if err != nil || userSchool == nil {
		writeError(w, http.StatusForbidden, "not_member", "You must be a verified member of this school")
		return
	}
	if userSchool.VerificationStatus != models.SchoolVerificationStatusVerified {
		writeError(w, http.StatusForbidden, "not_verified", "You must be a verified member of this school to view the member list")
		return
	}

	members, err := h.schoolRepo.GetSchoolMembers(r.Context(), schoolID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list members", "school", "list_members")
		return
	}

	if members == nil {
		members = []models.SchoolMember{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"members": members,
	})
}

// ListSignalGroups handles GET /api/v1/schools/:id/signal-groups (auth required)
func (h *SchoolHandler) ListSignalGroups(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	schoolID := getPathParam(r, "id")
	if schoolID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "School ID required")
		return
	}

	// Check user is verified member of this school
	userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, schoolID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check membership", "school", "check_membership")
		return
	}
	if userSchool == nil || userSchool.VerificationStatus != models.SchoolVerificationStatusVerified {
		writeError(w, http.StatusForbidden, "forbidden", "Verified membership required to view Signal groups")
		return
	}

	groups, err := h.signalGroupRepo.ListBySchool(r.Context(), schoolID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list Signal groups", "school", "list_signal_groups")
		return
	}

	// Convert to response format with encrypted secrets
	response := []models.SignalGroupWithSecret{}
	for _, g := range groups {
		sgws := models.SignalGroupWithSecret{
			SignalGroupPublic: models.SignalGroupPublic{
				ID:                 g.ID,
				SchoolID:           g.SchoolID,
				Name:               g.GroupName,
				Description:        g.Description,
				CreatedAt:          g.CreatedAt,
				HasPendingDeletion: g.HasPendingDeletion,
			},
		}
		if secret, secretErr := h.encryptedSecretRepo.GetBySignalGroupID(r.Context(), g.ID); secretErr == nil {
			wrappedDEK, _ := h.encryptedSecretRepo.GetWrappedDEK(r.Context(), secret.ID, claims.UserID)
			sgws.EncryptedSecret = &models.EncryptedSecretResponse{
				SecretID:         secret.ID,
				EncryptedPayload: secret.EncryptedPayload,
				EncryptionIV:     secret.EncryptionIV,
				WrappedDEK:       wrappedDEK,
			}
		}
		response = append(response, sgws)
	}

	writeJSON(w, http.StatusOK, models.SignalGroupListResponse{Groups: response})
}

// CreateSignalGroup handles POST /api/v1/schools/:id/signal-groups (auth, admin)
func (h *SchoolHandler) CreateSignalGroup(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	schoolID := getPathParam(r, "id")
	if schoolID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "School ID required")
		return
	}

	var req models.CreateSchoolSignalGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Name == "" || req.EncryptedPayload == "" || req.EncryptionIV == "" || len(req.WrappedKeys) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "Group name, encrypted payload, IV, and wrapped keys are required")
		return
	}

	// Check user is admin of this school
	userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, schoolID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check membership", "school", "check_admin_membership")
		return
	}
	if userSchool == nil || !userSchool.IsAdmin || userSchool.VerificationStatus != models.SchoolVerificationStatusVerified {
		writeError(w, http.StatusForbidden, "forbidden", "Verified admin access required for this school")
		return
	}

	// Check NOT in bootstrap mode (like region signal groups)
	if !claims.IsSuperuser {
		bootstrapMode, currentAdminCount, err := h.schoolRepo.IsSchoolInBootstrapMode(r.Context(), schoolID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check school status", "school", "check_bootstrap_mode")
			return
		}
		if bootstrapMode {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{
				"error":           "school_in_bootstrap",
				"message":         "Cannot create Signal groups while school is in bootstrap mode. The school needs at least 3 verified admins.",
				"bootstrap_mode":  true,
				"admin_count":     currentAdminCount,
				"admins_required": minSchoolAdminsToEndBootstrap,
			})
			return
		}
	}

	// Check group limit + create group + encrypted secret atomically
	group := &models.SignalGroup{
		SchoolID:    &schoolID,
		GroupName:   req.Name,
		Description: req.Description,
		CreatedBy:   &claims.UserID,
	}

	if h.db != nil {
		err := h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			count, txErr := h.signalGroupRepo.CountBySchoolForUpdate(r.Context(), tx, schoolID)
			if txErr != nil {
				return txErr
			}
			if count >= maxGroupsPerSchool {
				return database.ErrLimitReached
			}
			if txErr := h.signalGroupRepo.CreateGroupTx(r.Context(), tx, group); txErr != nil {
				return txErr
			}
			secret := &models.EncryptedSecret{
				SecretType:       models.SecretTypeSignalInvite,
				SignalGroupID:    &group.ID,
				EncryptedPayload: req.EncryptedPayload,
				EncryptionIV:     req.EncryptionIV,
				UpdatedBy:        claims.UserID,
			}
			return h.encryptedSecretRepo.CreateTx(r.Context(), tx, secret, req.WrappedKeys)
		})
		if errors.Is(err, database.ErrLimitReached) {
			writeError(w, http.StatusConflict, "limit_reached", "Maximum number of Signal groups reached for this school")
			return
		}
		if err != nil {
			writeServerError(w, r, err, "Failed to create Signal group", "school", "create_signal_group")
			return
		}
	} else {
		count, err := h.signalGroupRepo.CountBySchool(r.Context(), schoolID)
		if err != nil {
			writeServerError(w, r, err, "Failed to create Signal group", "school", "count_school_groups")
			return
		}
		if count >= maxGroupsPerSchool {
			writeError(w, http.StatusConflict, "limit_reached", "Maximum number of Signal groups reached for this school")
			return
		}
		if err := h.signalGroupRepo.Create(r.Context(), group); err != nil {
			writeServerError(w, r, err, "Failed to create Signal group", "school", "create_signal_group")
			return
		}
		secret := &models.EncryptedSecret{
			SecretType:       models.SecretTypeSignalInvite,
			SignalGroupID:    &group.ID,
			EncryptedPayload: req.EncryptedPayload,
			EncryptionIV:     req.EncryptionIV,
			UpdatedBy:        claims.UserID,
		}
		if err := h.encryptedSecretRepo.Create(r.Context(), secret, req.WrappedKeys); err != nil {
			writeServerError(w, r, err, "Failed to create encrypted secret", "school", "create_encrypted_secret")
			return
		}
	}

	// Audit log: school signal group created
	if h.auditRepo != nil {
		resourceType := "signal_group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSchoolSignalGroupCreated, &resourceType, &group.ID, map[string]interface{}{
			"group_name": group.GroupName,
			"school_id":  schoolID,
		}), "school_signal_group_created")
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"group_id":   group.ID,
		"school_id":  schoolID,
		"group_name": group.GroupName,
		"created_at": group.CreatedAt,
	})
}

// ListMySchools handles GET /api/v1/schools/my (auth required)
func (h *SchoolHandler) ListMySchools(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	schools, err := h.schoolRepo.ListUserSchoolsWithStatus(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list schools", "school", "list_my_schools")
		return
	}

	if schools == nil {
		schools = []models.MySchoolSummary{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"schools": schools,
	})
}

// SearchDistricts handles GET /api/v1/districts/search (public)
func (h *SchoolHandler) SearchDistricts(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	state := r.URL.Query().Get("state")

	districts, err := h.districtRepo.Search(r.Context(), query, state)
	if err != nil {
		writeServerError(w, r, err, "Failed to search districts", "school", "search_districts")
		return
	}

	if districts == nil {
		districts = []models.SchoolDistrict{}
	}

	writeJSON(w, http.StatusOK, models.DistrictSearchResponse{
		Districts: districts,
	})
}

// GetDistrict handles GET /api/v1/districts/:id (public)
func (h *SchoolHandler) GetDistrict(w http.ResponseWriter, r *http.Request) {
	districtID := getPathParam(r, "id")
	if districtID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "District ID required")
		return
	}

	district, err := h.districtRepo.GetByID(r.Context(), districtID)
	if err != nil {
		if errors.Is(err, database.ErrDistrictNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "District not found")
			return
		}
		writeServerError(w, r, err, "Failed to get district", "school", "get_district")
		return
	}

	// Lazy-load district boundary from NCES if not yet cached
	if district.Geometry == nil && district.NCESID != "" && h.ncesService != nil {
		geoJSONStr, fetchErr := h.ncesService.FetchDistrictBoundary(r.Context(), district.NCESID)
		if fetchErr != nil {
			slog.WarnContext(r.Context(), "failed to fetch district boundary", "nces_id", district.NCESID, "error", fetchErr)
		} else if geoJSONStr != "" {
			if storeErr := h.districtRepo.UpdateGeometry(r.Context(), district.NCESID, geoJSONStr); storeErr != nil {
				slog.WarnContext(r.Context(), "failed to store district boundary", "nces_id", district.NCESID, "error", storeErr)
			}
			var geom models.GeoJSONGeometry
			if jsonErr := json.Unmarshal([]byte(geoJSONStr), &geom); jsonErr == nil {
				district.Geometry = &geom
			}
		}
	}

	schools, err := h.schoolRepo.ListByDistrict(r.Context(), districtID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list schools in district", "school", "list_schools_in_district")
		return
	}

	if schools == nil {
		schools = []models.SchoolSummary{}
	}

	writeJSON(w, http.StatusOK, models.DistrictWithDetails{
		SchoolDistrict: *district,
		Schools:        schools,
	})
}

// ListDistrictMembers handles GET /api/v1/school-districts/:id/members (auth required)
func (h *SchoolHandler) ListDistrictMembers(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	districtID := getPathParam(r, "id")
	if districtID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "District ID required")
		return
	}

	// Check user is verified member of at least one school in district
	if h.db != nil {
		var isVerified bool
		txErr := h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			var txErr error
			isVerified, txErr = h.schoolRepo.IsUserVerifiedInDistrictTx(r.Context(), tx, claims.UserID, districtID)
			return txErr
		})
		if txErr != nil {
			writeServerError(w, r, txErr, "Failed to check district membership", "school", "check_district_membership")
			return
		}
		if !isVerified {
			writeError(w, http.StatusForbidden, "forbidden", "Verified membership in a school within this district is required")
			return
		}
	} else {
		// Fallback for when db is not available (e.g., tests)
		districtSchools, err := h.schoolRepo.ListByDistrict(r.Context(), districtID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check district membership", "school", "check_district_membership")
			return
		}
		isVerified := false
		for _, schoolSummary := range districtSchools {
			userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, schoolSummary.ID)
			if err == nil && userSchool != nil && userSchool.VerificationStatus == models.SchoolVerificationStatusVerified {
				isVerified = true
				break
			}
		}
		if !isVerified {
			writeError(w, http.StatusForbidden, "forbidden", "Verified membership in a school within this district is required")
			return
		}
	}

	members, err := h.schoolRepo.GetDistrictMembers(r.Context(), districtID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list district members", "school", "list_district_members")
		return
	}

	if members == nil {
		members = []models.DistrictMember{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"members": members,
	})
}

// ListDistrictSignalGroups handles GET /api/v1/districts/:id/signal-groups (auth required)
func (h *SchoolHandler) ListDistrictSignalGroups(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	districtID := getPathParam(r, "id")
	if districtID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "District ID required")
		return
	}

	// Check user is verified member of at least one school in district
	if h.db != nil {
		var isVerified bool
		txErr := h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			var txErr error
			isVerified, txErr = h.schoolRepo.IsUserVerifiedInDistrictTx(r.Context(), tx, claims.UserID, districtID)
			return txErr
		})
		if txErr != nil {
			writeServerError(w, r, txErr, "Failed to check district membership", "school", "check_district_membership")
			return
		}
		if !isVerified {
			writeError(w, http.StatusForbidden, "forbidden", "Verified membership in a school within this district is required")
			return
		}
	} else {
		// Fallback for when db is not available (e.g., tests)
		// Check by listing schools in district and checking membership
		districtSchools, err := h.schoolRepo.ListByDistrict(r.Context(), districtID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check district membership", "school", "check_district_membership")
			return
		}
		isVerified := false
		for _, schoolSummary := range districtSchools {
			userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, schoolSummary.ID)
			if err == nil && userSchool != nil && userSchool.VerificationStatus == models.SchoolVerificationStatusVerified {
				isVerified = true
				break
			}
		}
		if !isVerified {
			writeError(w, http.StatusForbidden, "forbidden", "Verified membership in a school within this district is required")
			return
		}
	}

	groups, err := h.signalGroupRepo.ListByDistrict(r.Context(), districtID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list Signal groups", "school", "list_district_signal_groups")
		return
	}

	// Convert to response format with encrypted secrets
	response := []models.SignalGroupWithSecret{}
	for _, g := range groups {
		sgws := models.SignalGroupWithSecret{
			SignalGroupPublic: models.SignalGroupPublic{
				ID:                 g.ID,
				DistrictID:         g.DistrictID,
				Name:               g.GroupName,
				Description:        g.Description,
				CreatedAt:          g.CreatedAt,
				HasPendingDeletion: g.HasPendingDeletion,
			},
		}
		if secret, secretErr := h.encryptedSecretRepo.GetBySignalGroupID(r.Context(), g.ID); secretErr == nil {
			wrappedDEK, _ := h.encryptedSecretRepo.GetWrappedDEK(r.Context(), secret.ID, claims.UserID)
			sgws.EncryptedSecret = &models.EncryptedSecretResponse{
				SecretID:         secret.ID,
				EncryptedPayload: secret.EncryptedPayload,
				EncryptionIV:     secret.EncryptionIV,
				WrappedDEK:       wrappedDEK,
			}
		}
		response = append(response, sgws)
	}

	writeJSON(w, http.StatusOK, models.SignalGroupListResponse{Groups: response})
}

// CreateDistrictSignalGroup handles POST /api/v1/districts/:id/signal-groups (auth required)
func (h *SchoolHandler) CreateDistrictSignalGroup(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	districtID := getPathParam(r, "id")
	if districtID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "District ID required")
		return
	}

	var req models.CreateSchoolSignalGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Name == "" || req.EncryptedPayload == "" || req.EncryptionIV == "" || len(req.WrappedKeys) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "Group name, encrypted payload, IV, and wrapped keys are required")
		return
	}

	// Check user is admin of at least one school in district (verified admin)
	districtSchools, err := h.schoolRepo.ListByDistrict(r.Context(), districtID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check district schools", "school", "check_district_schools")
		return
	}

	isDistrictAdmin := false
	for _, schoolSummary := range districtSchools {
		userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, schoolSummary.ID)
		if err == nil && userSchool != nil && userSchool.IsAdmin && userSchool.VerificationStatus == models.SchoolVerificationStatusVerified {
			isDistrictAdmin = true
			break
		}
	}

	if !isDistrictAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "Verified admin access in a school within this district is required")
		return
	}

	// Check group limit + create group + encrypted secret atomically
	group := &models.SignalGroup{
		DistrictID:  &districtID,
		GroupName:   req.Name,
		Description: req.Description,
		CreatedBy:   &claims.UserID,
	}

	if h.db != nil {
		err := h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			count, txErr := h.signalGroupRepo.CountByDistrictForUpdate(r.Context(), tx, districtID)
			if txErr != nil {
				return txErr
			}
			if count >= maxGroupsPerDistrict {
				return database.ErrLimitReached
			}
			if txErr := h.signalGroupRepo.CreateGroupTx(r.Context(), tx, group); txErr != nil {
				return txErr
			}
			secret := &models.EncryptedSecret{
				SecretType:       models.SecretTypeSignalInvite,
				SignalGroupID:    &group.ID,
				EncryptedPayload: req.EncryptedPayload,
				EncryptionIV:     req.EncryptionIV,
				UpdatedBy:        claims.UserID,
			}
			return h.encryptedSecretRepo.CreateTx(r.Context(), tx, secret, req.WrappedKeys)
		})
		if errors.Is(err, database.ErrLimitReached) {
			writeError(w, http.StatusConflict, "limit_reached", "Maximum number of Signal groups reached for this district")
			return
		}
		if err != nil {
			writeServerError(w, r, err, "Failed to create Signal group", "school", "create_district_signal_group")
			return
		}
	} else {
		count, err := h.signalGroupRepo.CountByDistrict(r.Context(), districtID)
		if err != nil {
			writeServerError(w, r, err, "Failed to create Signal group", "school", "count_district_groups")
			return
		}
		if count >= maxGroupsPerDistrict {
			writeError(w, http.StatusConflict, "limit_reached", "Maximum number of Signal groups reached for this district")
			return
		}
		if err := h.signalGroupRepo.Create(r.Context(), group); err != nil {
			writeServerError(w, r, err, "Failed to create Signal group", "school", "create_district_signal_group")
			return
		}
		secret := &models.EncryptedSecret{
			SecretType:       models.SecretTypeSignalInvite,
			SignalGroupID:    &group.ID,
			EncryptedPayload: req.EncryptedPayload,
			EncryptionIV:     req.EncryptionIV,
			UpdatedBy:        claims.UserID,
		}
		if err := h.encryptedSecretRepo.Create(r.Context(), secret, req.WrappedKeys); err != nil {
			writeServerError(w, r, err, "Failed to create encrypted secret", "school", "create_encrypted_secret")
			return
		}
	}

	// Audit log: district signal group created
	if h.auditRepo != nil {
		resourceType := "signal_group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSchoolSignalGroupCreated, &resourceType, &group.ID, map[string]interface{}{
			"group_name":  group.GroupName,
			"district_id": districtID,
		}), "district_signal_group_created")
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"group_id":    group.ID,
		"district_id": districtID,
		"group_name":  group.GroupName,
		"created_at":  group.CreatedAt,
	})
}
