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
	maxMeshtasticChannelsPerScope = 5
)

// MeshtasticHandler handles Meshtastic channel endpoints
type MeshtasticHandler struct {
	db                  *database.DB
	channelRepo         *database.MeshtasticChannelRepository
	encryptedSecretRepo *database.EncryptedSecretRepository
	regionRepo          *database.RegionRepository
	schoolRepo          *database.SchoolRepository
	auditRepo           *database.AuditRepository
}

// NewMeshtasticHandler creates a new Meshtastic handler
func NewMeshtasticHandler(
	db *database.DB,
	channelRepo *database.MeshtasticChannelRepository,
	encryptedSecretRepo *database.EncryptedSecretRepository,
	regionRepo *database.RegionRepository,
	schoolRepo *database.SchoolRepository,
	auditRepo *database.AuditRepository,
) *MeshtasticHandler {
	return &MeshtasticHandler{
		db:                  db,
		channelRepo:         channelRepo,
		encryptedSecretRepo: encryptedSecretRepo,
		regionRepo:          regionRepo,
		schoolRepo:          schoolRepo,
		auditRepo:           auditRepo,
	}
}

// Create handles POST /api/v1/meshtastic-channels (admin only)
func (h *MeshtasticHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req models.CreateMeshtasticChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate exactly one scope
	scopeCount := 0
	if req.RegionID != nil && *req.RegionID != "" {
		scopeCount++
	}
	if req.SchoolID != nil && *req.SchoolID != "" {
		scopeCount++
	}
	if req.DistrictID != nil && *req.DistrictID != "" {
		scopeCount++
	}
	if scopeCount != 1 {
		writeError(w, http.StatusBadRequest, "validation_error", "Exactly one of region_id, school_id, or district_id must be provided")
		return
	}

	if req.Name == "" || req.EncryptedPayload == "" || req.EncryptionIV == "" || len(req.WrappedKeys) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "Channel name, encrypted payload, IV, and wrapped keys are required")
		return
	}

	// Handle region-scoped channel
	if req.RegionID != nil && *req.RegionID != "" {
		h.createRegionChannel(w, r, claims, &req)
		return
	}

	// Handle school-scoped channel
	if req.SchoolID != nil && *req.SchoolID != "" {
		h.createSchoolChannel(w, r, claims, &req)
		return
	}

	// Handle district-scoped channel
	h.createDistrictChannel(w, r, claims, &req)
}

func (h *MeshtasticHandler) createRegionChannel(w http.ResponseWriter, r *http.Request, claims *middleware.Claims, req *models.CreateMeshtasticChannelRequest) {
	regionID := *req.RegionID

	if !claims.IsSuperuser {
		isAdmin, err := h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, regionID)
		if err != nil {
			writeServerError(w, r, err, "Your permissions could not be verified. Please try again.", "meshtastic", "verify_admin_status")
			return
		}
		if !isAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "Admin access required for this region")
			return
		}
	}

	if !claims.IsSuperuser {
		bootstrapMode, adminCount, err := h.regionRepo.IsRegionInBootstrapMode(r.Context(), regionID)
		if err != nil {
			writeServerError(w, r, err, "Community status could not be verified. Please try again.", "meshtastic", "check_bootstrap_mode")
			return
		}
		if bootstrapMode {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{
				"error":           "region_in_bootstrap",
				"message":         "Cannot create Meshtastic channels while region is in bootstrap mode. The region needs at least 3 fully verified admins.",
				"bootstrap_mode":  true,
				"admin_count":     adminCount,
				"admins_required": 3,
			})
			return
		}
	}

	channel := &models.MeshtasticChannel{
		RegionID:    &regionID,
		ChannelName: req.Name,
		Description: req.Description,
		CreatedBy:   &claims.UserID,
	}

	if err := h.createChannelWithSecret(r, claims, channel, req); err != nil {
		if errors.Is(err, database.ErrLimitReached) {
			writeError(w, http.StatusConflict, "limit_reached", "Maximum number of Meshtastic channels reached for this region")
			return
		}
		writeServerError(w, r, err, "Meshtastic channel could not be created. Please try again.", "meshtastic", "create_channel")
		return
	}

	h.logAudit(r, claims, channel, map[string]interface{}{
		"channel_name": channel.ChannelName,
		"region_id":    regionID,
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"channel_id":   channel.ID,
		"region_id":    regionID,
		"channel_name": channel.ChannelName,
		"created_at":   channel.CreatedAt,
	})
}

func (h *MeshtasticHandler) createSchoolChannel(w http.ResponseWriter, r *http.Request, claims *middleware.Claims, req *models.CreateMeshtasticChannelRequest) {
	schoolID := *req.SchoolID

	if !claims.IsSuperuser {
		userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, schoolID)
		if err != nil || userSchool == nil {
			writeError(w, http.StatusForbidden, "forbidden", "You are not a member of this school")
			return
		}
		if !userSchool.IsAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "Admin access required for this school")
			return
		}
	}

	channel := &models.MeshtasticChannel{
		SchoolID:    &schoolID,
		ChannelName: req.Name,
		Description: req.Description,
		CreatedBy:   &claims.UserID,
	}

	if err := h.createChannelWithSecret(r, claims, channel, req); err != nil {
		if errors.Is(err, database.ErrLimitReached) {
			writeError(w, http.StatusConflict, "limit_reached", "Maximum number of Meshtastic channels reached for this school")
			return
		}
		writeServerError(w, r, err, "Meshtastic channel could not be created. Please try again.", "meshtastic", "create_channel")
		return
	}

	h.logAudit(r, claims, channel, map[string]interface{}{
		"channel_name": channel.ChannelName,
		"school_id":    schoolID,
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"channel_id":   channel.ID,
		"school_id":    schoolID,
		"channel_name": channel.ChannelName,
		"created_at":   channel.CreatedAt,
	})
}

func (h *MeshtasticHandler) createDistrictChannel(w http.ResponseWriter, r *http.Request, claims *middleware.Claims, req *models.CreateMeshtasticChannelRequest) {
	districtID := *req.DistrictID

	if !claims.IsSuperuser {
		// District admin check: user must be admin of at least one school in the district
		schools, err := h.schoolRepo.ListByDistrict(r.Context(), districtID)
		if err != nil {
			writeServerError(w, r, err, "Your permissions could not be verified. Please try again.", "meshtastic", "verify_district_admin")
			return
		}
		isDistrictAdmin := false
		for _, school := range schools {
			userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, school.ID)
			if err == nil && userSchool != nil && userSchool.IsAdmin {
				isDistrictAdmin = true
				break
			}
		}
		if !isDistrictAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "Admin access required for this district (must be admin of a school in the district)")
			return
		}
	}

	channel := &models.MeshtasticChannel{
		DistrictID:  &districtID,
		ChannelName: req.Name,
		Description: req.Description,
		CreatedBy:   &claims.UserID,
	}

	if err := h.createChannelWithSecret(r, claims, channel, req); err != nil {
		if errors.Is(err, database.ErrLimitReached) {
			writeError(w, http.StatusConflict, "limit_reached", "Maximum number of Meshtastic channels reached for this district")
			return
		}
		writeServerError(w, r, err, "Meshtastic channel could not be created. Please try again.", "meshtastic", "create_channel")
		return
	}

	h.logAudit(r, claims, channel, map[string]interface{}{
		"channel_name": channel.ChannelName,
		"district_id":  districtID,
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"channel_id":   channel.ID,
		"district_id":  districtID,
		"channel_name": channel.ChannelName,
		"created_at":   channel.CreatedAt,
	})
}

func (h *MeshtasticHandler) createChannelWithSecret(r *http.Request, claims *middleware.Claims, channel *models.MeshtasticChannel, req *models.CreateMeshtasticChannelRequest) error {
	if h.db != nil {
		return h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			// Check limit based on scope
			var count int
			var txErr error
			if channel.RegionID != nil {
				count, txErr = h.channelRepo.CountByRegionForUpdate(r.Context(), tx, *channel.RegionID)
			} else if channel.SchoolID != nil {
				count, txErr = h.channelRepo.CountBySchoolForUpdate(r.Context(), tx, *channel.SchoolID)
			} else if channel.DistrictID != nil {
				count, txErr = h.channelRepo.CountByDistrictForUpdate(r.Context(), tx, *channel.DistrictID)
			}
			if txErr != nil {
				return txErr
			}
			if count >= maxMeshtasticChannelsPerScope {
				return database.ErrLimitReached
			}

			if txErr := h.channelRepo.CreateChannelTx(r.Context(), tx, channel); txErr != nil {
				return txErr
			}

			secret := &models.EncryptedSecret{
				SecretType:          models.SecretTypeMeshtasticChannel,
				MeshtasticChannelID: &channel.ID,
				EncryptedPayload:    req.EncryptedPayload,
				EncryptionIV:        req.EncryptionIV,
				UpdatedBy:           claims.UserID,
			}
			return h.encryptedSecretRepo.CreateTx(r.Context(), tx, secret, req.WrappedKeys)
		})
	}

	// Non-transactional fallback
	if err := h.channelRepo.Create(r.Context(), channel); err != nil {
		return err
	}
	secret := &models.EncryptedSecret{
		SecretType:          models.SecretTypeMeshtasticChannel,
		MeshtasticChannelID: &channel.ID,
		EncryptedPayload:    req.EncryptedPayload,
		EncryptionIV:        req.EncryptionIV,
		UpdatedBy:           claims.UserID,
	}
	return h.encryptedSecretRepo.Create(r.Context(), secret, req.WrappedKeys)
}

func (h *MeshtasticHandler) logAudit(r *http.Request, claims *middleware.Claims, channel *models.MeshtasticChannel, details map[string]interface{}) {
	if h.auditRepo != nil {
		resourceType := "meshtastic_channel"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionMeshtasticChannelCreated, &resourceType, &channel.ID, details), "meshtastic_channel_created")
	}
}

// List handles GET /api/v1/meshtastic-channels
func (h *MeshtasticHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if claims.VerificationTier < models.TierVouched {
		writeError(w, http.StatusForbidden, "forbidden", "Verification required to view Meshtastic channels")
		return
	}

	regionID := r.URL.Query().Get("region_id")
	schoolID := r.URL.Query().Get("school_id")
	districtID := r.URL.Query().Get("district_id")

	var channels []*models.MeshtasticChannel
	var err error

	if regionID != "" {
		channels, err = h.channelRepo.ListByRegion(r.Context(), regionID)
	} else if schoolID != "" {
		channels, err = h.channelRepo.ListBySchool(r.Context(), schoolID)
	} else if districtID != "" {
		channels, err = h.channelRepo.ListByDistrict(r.Context(), districtID)
	} else {
		channels, err = h.channelRepo.ListByUser(r.Context(), claims.UserID)
	}

	if err != nil {
		writeServerError(w, r, err, "Meshtastic channels could not be loaded. Please try again.", "meshtastic", "list_channels")
		return
	}

	response := []models.MeshtasticChannelWithSecret{}
	for _, ch := range channels {
		mcws := models.MeshtasticChannelWithSecret{
			MeshtasticChannelPublic: models.MeshtasticChannelPublic{
				ID:                 ch.ID,
				RegionID:           ch.RegionID,
				SchoolID:           ch.SchoolID,
				DistrictID:         ch.DistrictID,
				RegionName:         ch.RegionName,
				SchoolName:         ch.SchoolName,
				DistrictName:       ch.DistrictName,
				Name:               ch.ChannelName,
				Description:        ch.Description,
				CreatedAt:          ch.CreatedAt,
				HasPendingDeletion: ch.HasPendingDeletion,
			},
		}
		if secret, secretErr := h.encryptedSecretRepo.GetByMeshtasticChannelID(r.Context(), ch.ID); secretErr == nil {
			wrappedDEK, _ := h.encryptedSecretRepo.GetWrappedDEK(r.Context(), secret.ID, claims.UserID)
			mcws.EncryptedSecret = &models.EncryptedSecretResponse{
				SecretID:         secret.ID,
				EncryptedPayload: secret.EncryptedPayload,
				EncryptionIV:     secret.EncryptionIV,
				WrappedDEK:       wrappedDEK,
			}
		}
		response = append(response, mcws)
	}

	writeJSON(w, http.StatusOK, models.MeshtasticChannelListResponse{Channels: response})
}

// ListAdmin handles GET /api/v1/meshtastic-channels/admin
func (h *MeshtasticHandler) ListAdmin(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !claims.PostcardVerified || !claims.VouchVerified {
		writeError(w, http.StatusForbidden, "forbidden", "Admin access required (both postcard and vouch verification needed)")
		return
	}

	channels, err := h.channelRepo.ListByAdminUser(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Meshtastic channels could not be loaded. Please try again.", "meshtastic", "list_admin_channels")
		return
	}

	response := []models.MeshtasticChannelWithSecret{}
	for _, ch := range channels {
		mcws := models.MeshtasticChannelWithSecret{
			MeshtasticChannelPublic: models.MeshtasticChannelPublic{
				ID:                 ch.ID,
				RegionID:           ch.RegionID,
				SchoolID:           ch.SchoolID,
				DistrictID:         ch.DistrictID,
				RegionName:         ch.RegionName,
				SchoolName:         ch.SchoolName,
				DistrictName:       ch.DistrictName,
				Name:               ch.ChannelName,
				Description:        ch.Description,
				CreatedAt:          ch.CreatedAt,
				HasPendingDeletion: ch.HasPendingDeletion,
			},
		}
		if secret, secretErr := h.encryptedSecretRepo.GetByMeshtasticChannelID(r.Context(), ch.ID); secretErr == nil {
			wrappedDEK, _ := h.encryptedSecretRepo.GetWrappedDEK(r.Context(), secret.ID, claims.UserID)
			mcws.EncryptedSecret = &models.EncryptedSecretResponse{
				SecretID:         secret.ID,
				EncryptedPayload: secret.EncryptedPayload,
				EncryptionIV:     secret.EncryptionIV,
				WrappedDEK:       wrappedDEK,
			}
		}
		response = append(response, mcws)
	}

	writeJSON(w, http.StatusOK, models.MeshtasticChannelListResponse{Channels: response})
}

// Update handles PUT /api/v1/meshtastic-channels/:id
func (h *MeshtasticHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	channelID := getPathParam(r, "id")
	if channelID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Channel ID required")
		return
	}

	var req models.UpdateMeshtasticChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	channel, err := h.channelRepo.GetByID(r.Context(), channelID)
	if errors.Is(err, database.ErrMeshtasticChannelNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Meshtastic channel not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Meshtastic channel could not be retrieved. Please try again.", "meshtastic", "get_channel")
		return
	}

	// Check admin access based on scope
	if !claims.IsSuperuser {
		if channel.RegionID != nil {
			isAdmin, err := h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, *channel.RegionID)
			if err != nil {
				writeServerError(w, r, err, "Your permissions could not be verified. Please try again.", "meshtastic", "verify_admin_status")
				return
			}
			if !isAdmin {
				writeError(w, http.StatusForbidden, "forbidden", "Admin access required for this region")
				return
			}
		} else if channel.SchoolID != nil {
			userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, *channel.SchoolID)
			if err != nil || userSchool == nil || !userSchool.IsAdmin {
				writeError(w, http.StatusForbidden, "forbidden", "Admin access required for this school")
				return
			}
		} else if channel.DistrictID != nil {
			schools, err := h.schoolRepo.ListByDistrict(r.Context(), *channel.DistrictID)
			if err != nil {
				writeServerError(w, r, err, "Your permissions could not be verified. Please try again.", "meshtastic", "verify_district_admin")
				return
			}
			isDistrictAdmin := false
			for _, school := range schools {
				userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, school.ID)
				if err == nil && userSchool != nil && userSchool.IsAdmin {
					isDistrictAdmin = true
					break
				}
			}
			if !isDistrictAdmin {
				writeError(w, http.StatusForbidden, "forbidden", "Admin access required for this district")
				return
			}
		}
	}

	if err := h.channelRepo.Update(r.Context(), channelID, req.Name, req.Description); err != nil {
		writeServerError(w, r, err, "Meshtastic channel could not be updated. Please try again.", "meshtastic", "update_channel")
		return
	}

	if h.auditRepo != nil {
		resourceType := "meshtastic_channel"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionMeshtasticChannelUpdated, &resourceType, &channelID, map[string]interface{}{
			"new_name":        req.Name,
			"new_description": req.Description,
		}), "meshtastic_channel_updated")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"channel_id":   channelID,
		"channel_name": req.Name,
	})
}
