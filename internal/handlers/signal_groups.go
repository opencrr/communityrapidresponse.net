package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

const (
	maxGroupsPerRegion = 5
)

// SignalGroupHandler handles signal group endpoints
type SignalGroupHandler struct {
	db                  *database.DB
	groupRepo           *database.SignalGroupRepository
	encryptedSecretRepo *database.EncryptedSecretRepository
	regionRepo          *database.RegionRepository
	auditRepo           *database.AuditRepository
}

// NewSignalGroupHandler creates a new signal group handler
func NewSignalGroupHandler(
	db *database.DB,
	groupRepo *database.SignalGroupRepository,
	encryptedSecretRepo *database.EncryptedSecretRepository,
	regionRepo *database.RegionRepository,
	auditRepo *database.AuditRepository,
) *SignalGroupHandler {
	return &SignalGroupHandler{
		db:                  db,
		groupRepo:           groupRepo,
		encryptedSecretRepo: encryptedSecretRepo,
		regionRepo:          regionRepo,
		auditRepo:           auditRepo,
	}
}

// Create handles POST /api/v1/signal-groups (admin only)
func (h *SignalGroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req models.CreateSignalGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate request - require region_id, name, and encrypted payload
	if req.RegionID == nil || *req.RegionID == "" || req.Name == "" || req.EncryptedPayload == "" || req.EncryptionIV == "" || len(req.WrappedKeys) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "Community ID, group name, encrypted payload, IV, and wrapped keys are required")
		return
	}

	regionID := *req.RegionID

	// Check if user is admin for this region (or superuser)
	if !claims.IsSuperuser {
		isAdmin, err := h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, regionID)
		if err != nil {
			writeServerError(w, r, err, "Failed to verify admin status", "signal_group", "verify_admin_status")
			return
		}
		if !isAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "Admin access required for this region")
			return
		}
	}

	// Check if region is in bootstrap mode - cannot create groups until region has enough admins
	// Superusers can bypass this restriction
	if !claims.IsSuperuser {
		bootstrapMode, adminCount, err := h.regionRepo.IsRegionInBootstrapMode(r.Context(), regionID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check region status", "signal_group", "check_bootstrap_mode")
			return
		}
		if bootstrapMode {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{
				"error":           "region_in_bootstrap",
				"message":         "Cannot create Signal groups while region is in bootstrap mode. The region needs at least 3 fully verified admins.",
				"bootstrap_mode":  true,
				"admin_count":     adminCount,
				"admins_required": 3,
			})
			return
		}
	}

	// Check group limit + create group + encrypted secret atomically
	group := &models.SignalGroup{
		RegionID:    &regionID,
		GroupName:   req.Name,
		Description: req.Description,
		CreatedBy:   &claims.UserID,
	}

	if h.db != nil {
		err := h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			count, txErr := h.groupRepo.CountByRegionForUpdate(r.Context(), tx, regionID)
			if txErr != nil {
				return txErr
			}
			if count >= maxGroupsPerRegion {
				return database.ErrLimitReached
			}
			if txErr := h.groupRepo.CreateGroupTx(r.Context(), tx, group); txErr != nil {
				return txErr
			}
			// Create encrypted secret for the group
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
			writeError(w, http.StatusConflict, "limit_reached", "Maximum number of Signal groups reached for this region")
			return
		}
		if err != nil {
			writeServerError(w, r, err, "Failed to create Signal group", "signal_group", "create_group")
			return
		}
	} else {
		count, err := h.groupRepo.CountByRegion(r.Context(), regionID)
		if err != nil {
			writeServerError(w, r, err, "Failed to create Signal group", "signal_group", "count_by_region")
			return
		}
		if count >= maxGroupsPerRegion {
			writeError(w, http.StatusConflict, "limit_reached", "Maximum number of Signal groups reached for this region")
			return
		}
		if err := h.groupRepo.Create(r.Context(), group); err != nil {
			writeServerError(w, r, err, "Failed to create Signal group", "signal_group", "create_group")
			return
		}
		// Create encrypted secret (non-transactional fallback)
		secret := &models.EncryptedSecret{
			SecretType:       models.SecretTypeSignalInvite,
			SignalGroupID:    &group.ID,
			EncryptedPayload: req.EncryptedPayload,
			EncryptionIV:     req.EncryptionIV,
			UpdatedBy:        claims.UserID,
		}
		if err := h.encryptedSecretRepo.Create(r.Context(), secret, req.WrappedKeys); err != nil {
			writeServerError(w, r, err, "Failed to create encrypted secret", "signal_group", "create_encrypted_secret")
			return
		}
	}

	// Audit log: signal group created
	if h.auditRepo != nil {
		resourceType := "signal_group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSignalGroupCreated, &resourceType, &group.ID, map[string]interface{}{
			"group_name": group.GroupName,
			"region_id":  regionID,
		}), "signal_group_created")
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"group_id":   group.ID,
		"region_id":  regionID,
		"group_name": group.GroupName,
		"created_at": group.CreatedAt,
	})
}

// List handles GET /api/v1/signal-groups
func (h *SignalGroupHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Only verified users (vouched or postcard) can see groups
	if claims.VerificationTier < models.TierVouched {
		writeError(w, http.StatusForbidden, "forbidden", "Verification required to view Signal groups")
		return
	}

	regionID := r.URL.Query().Get("region_id")

	var groups []*models.SignalGroup
	var err error

	if regionID != "" {
		// Verify the caller is a member of the requested region (superusers bypass)
		if !claims.IsSuperuser {
			isMember, memberErr := h.regionRepo.IsUserInRegion(r.Context(), claims.UserID, regionID)
			if memberErr != nil {
				writeServerError(w, r, memberErr, "Failed to check region access", "signal_group", "check_region_access")
				return
			}
			if !isMember {
				writeError(w, http.StatusForbidden, "forbidden", "You do not have access to this region")
				return
			}
		}
		groups, err = h.groupRepo.ListByRegion(r.Context(), regionID)
	} else {
		groups, err = h.groupRepo.ListByUser(r.Context(), claims.UserID)
	}

	if err != nil {
		writeServerError(w, r, err, "Failed to list Signal groups", "signal_group", "list_groups")
		return
	}

	// Convert to response format with encrypted secrets
	response := []models.SignalGroupWithSecret{}
	for _, g := range groups {
		sgws := models.SignalGroupWithSecret{
			SignalGroupPublic: models.SignalGroupPublic{
				ID:                 g.ID,
				RegionID:           g.RegionID,
				SchoolID:           g.SchoolID,
				DistrictID:         g.DistrictID,
				RegionName:         g.RegionName,
				SchoolName:         g.SchoolName,
				DistrictName:       g.DistrictName,
				Name:               g.GroupName,
				Description:        g.Description,
				CreatedAt:          g.CreatedAt,
				HasPendingDeletion: g.HasPendingDeletion,
			},
		}
		// Fetch encrypted secret + user's wrapped DEK
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

// ListAdmin handles GET /api/v1/signal-groups/admin
// Returns groups for regions where the user is an admin
func (h *SignalGroupHandler) ListAdmin(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Must be an admin (both postcard and vouch verified)
	if !claims.PostcardVerified || !claims.VouchVerified {
		writeError(w, http.StatusForbidden, "forbidden", "Admin access required (both postcard and vouch verification needed)")
		return
	}

	groups, err := h.groupRepo.ListByAdminUser(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list Signal groups", "signal_group", "list_admin_groups")
		return
	}

	// Convert to response format with encrypted secrets
	response := []models.SignalGroupWithSecret{}
	for _, g := range groups {
		sgws := models.SignalGroupWithSecret{
			SignalGroupPublic: models.SignalGroupPublic{
				ID:                 g.ID,
				RegionID:           g.RegionID,
				SchoolID:           g.SchoolID,
				DistrictID:         g.DistrictID,
				RegionName:         g.RegionName,
				SchoolName:         g.SchoolName,
				DistrictName:       g.DistrictName,
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

// Update handles PUT /api/v1/signal-groups/:id
// Note: secret updates require consensus - use secret proposal endpoints
func (h *SignalGroupHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	groupID := getPathParam(r, "id")
	if groupID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Group ID required")
		return
	}

	var req models.UpdateSignalGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Get the group to verify region
	group, err := h.groupRepo.GetByID(r.Context(), groupID)
	if errors.Is(err, database.ErrSignalGroupNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Signal group not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get Signal group", "signal_group", "get_group")
		return
	}

	// Check if user is admin for this region (or superuser)
	if group.RegionID == nil {
		writeError(w, http.StatusBadRequest, "invalid_group", "This signal group is not associated with a region")
		return
	}
	if !claims.IsSuperuser {
		isAdmin, err := h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, *group.RegionID)
		if err != nil {
			writeServerError(w, r, err, "Failed to verify admin status", "signal_group", "verify_admin_status")
			return
		}
		if !isAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "Admin access required for this region")
			return
		}
	}

	// Update (name and description only - secret requires consensus)
	if err := h.groupRepo.Update(r.Context(), groupID, req.Name, req.Description); err != nil {
		writeServerError(w, r, err, "Failed to update Signal group", "signal_group", "update_group")
		return
	}

	// Audit log: signal group updated
	if h.auditRepo != nil {
		resourceType := "signal_group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSignalGroupUpdated, &resourceType, &groupID, map[string]interface{}{
			"new_name":        req.Name,
			"new_description": req.Description,
		}), "signal_group_updated")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"group_id":   groupID,
		"group_name": req.Name,
		"updated_at": group.CreatedAt,
	})
}
