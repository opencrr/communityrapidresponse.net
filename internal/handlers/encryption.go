package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// EncryptionHandler handles encryption key management endpoints
type EncryptionHandler struct {
	encryptionKeyRepo   *database.EncryptionKeyRepository
	encryptedSecretRepo *database.EncryptedSecretRepository
	regionRepo          *database.RegionRepository
	schoolRepo          *database.SchoolRepository
	userRepo            *database.UserRepository
	groupRepo           *database.GroupRepository
	signalGroupRepo     *database.SignalGroupRepository
	connectionRepo      *database.ConnectionRepository
	notificationService NotificationServiceInterface
}

// NewEncryptionHandler creates a new encryption handler
func NewEncryptionHandler(
	encryptionKeyRepo *database.EncryptionKeyRepository,
	encryptedSecretRepo *database.EncryptedSecretRepository,
	regionRepo *database.RegionRepository,
	schoolRepo *database.SchoolRepository,
	userRepo *database.UserRepository,
	groupRepo *database.GroupRepository,
	signalGroupRepo *database.SignalGroupRepository,
	connectionRepo *database.ConnectionRepository,
) *EncryptionHandler {
	return &EncryptionHandler{
		encryptionKeyRepo:   encryptionKeyRepo,
		encryptedSecretRepo: encryptedSecretRepo,
		regionRepo:          regionRepo,
		schoolRepo:          schoolRepo,
		userRepo:            userRepo,
		groupRepo:           groupRepo,
		signalGroupRepo:     signalGroupRepo,
		connectionRepo:      connectionRepo,
	}
}

// SetNotificationService sets the notification service
func (h *EncryptionHandler) SetNotificationService(svc NotificationServiceInterface) {
	h.notificationService = svc
}

// UploadKeys handles POST /api/v1/encryption/keys — upload public key + wrapped backup
func (h *EncryptionHandler) UploadKeys(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req models.CreateEncryptionKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
	}

	if req.PublicKey == "" || req.WrappedPrivateKey == "" || req.KeySalt == "" || req.KeyIV == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "All encryption key fields are required")
		return
	}

	key := &models.UserEncryptionKey{
		UserID:            claims.UserID,
		PublicKey:         req.PublicKey,
		WrappedPrivateKey: req.WrappedPrivateKey,
		KeySalt:           req.KeySalt,
		KeyIV:             req.KeyIV,
	}

	if err := h.encryptionKeyRepo.Create(r.Context(), key); err != nil {
		writeServerError(w, r, err, "Failed to store encryption keys", "encryption", "upload_keys")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"stored": true,
	})
}

// GetKeys handles GET /api/v1/encryption/keys — get own wrapped backup
func (h *EncryptionHandler) GetKeys(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	key, err := h.encryptionKeyRepo.GetByUserID(r.Context(), claims.UserID)
	if errors.Is(err, database.ErrEncryptionKeyNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "No encryption keys found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get encryption keys", "encryption", "get_keys")
		return
	}

	writeJSON(w, http.StatusOK, models.EncryptionKeyResponse{
		PublicKey:         key.PublicKey,
		WrappedPrivateKey: key.WrappedPrivateKey,
		KeySalt:           key.KeySalt,
		KeyIV:             key.KeyIV,
		RotatedAt:         key.RotatedAt,
	})
}

// UpdateKeys handles PUT /api/v1/encryption/keys — re-wrap private key (password change)
func (h *EncryptionHandler) UpdateKeys(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req models.UpdateEncryptionKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
	}

	if req.WrappedPrivateKey == "" || req.KeySalt == "" || req.KeyIV == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "All encryption key fields are required")
		return
	}

	if err := h.encryptionKeyRepo.Update(r.Context(), claims.UserID, req.WrappedPrivateKey, req.KeySalt, req.KeyIV); err != nil {
		if errors.Is(err, database.ErrEncryptionKeyNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "No encryption keys found to update")
			return
		}
		writeServerError(w, r, err, "Failed to update encryption keys", "encryption", "update_keys")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"updated": true,
	})
}

// RotateKeys handles POST /api/v1/encryption/keys/rotate — upload new keypair (password reset)
func (h *EncryptionHandler) RotateKeys(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req models.RotateEncryptionKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
	}

	if req.PublicKey == "" || req.WrappedPrivateKey == "" || req.KeySalt == "" || req.KeyIV == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "All encryption key fields are required")
		return
	}

	key := &models.UserEncryptionKey{
		UserID:            claims.UserID,
		PublicKey:         req.PublicKey,
		WrappedPrivateKey: req.WrappedPrivateKey,
		KeySalt:           req.KeySalt,
		KeyIV:             req.KeyIV,
	}

	if err := h.encryptionKeyRepo.Create(r.Context(), key); err != nil {
		writeServerError(w, r, err, "Failed to rotate encryption keys", "encryption", "rotate_keys")
		return
	}

	// Flag all existing wrapped DEKs for this user as needing re-key
	if h.encryptedSecretRepo != nil {
		if err := h.encryptedSecretRepo.FlagRekeyForUser(r.Context(), claims.UserID); err != nil {
			// Log but don't fail - the key rotation itself succeeded
			slog.ErrorContext(r.Context(), "failed to flag re-keys after key rotation", "user_id", claims.UserID, "error", err)
		} else if h.notificationService != nil {
			if err := h.notificationService.QueueRekeyingNeededEvent(r.Context(), claims.UserID); err != nil {
				slog.ErrorContext(r.Context(), "failed to queue rekey notification", "user_id", claims.UserID, "error", err)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rotated": true,
	})
}

// GetPublicKeys handles GET /api/v1/encryption/public-keys?region_id=X (or school_id, district_id, group_id, connection_id)
func (h *EncryptionHandler) GetPublicKeys(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	regionID := r.URL.Query().Get("region_id")
	schoolID := r.URL.Query().Get("school_id")
	districtID := r.URL.Query().Get("district_id")
	groupID := r.URL.Query().Get("group_id")
	connectionID := r.URL.Query().Get("connection_id")

	// Exactly one scope must be provided
	scopeCount := 0
	if regionID != "" {
		scopeCount++
	}
	if schoolID != "" {
		scopeCount++
	}
	if districtID != "" {
		scopeCount++
	}
	if groupID != "" {
		scopeCount++
	}
	if connectionID != "" {
		scopeCount++
	}
	if scopeCount != 1 {
		writeError(w, http.StatusBadRequest, "validation_error", "Exactly one of region_id, school_id, district_id, group_id, or connection_id must be provided")
		return
	}

	// Re-check superuser from DB for authorization
	isSuperuser, err := isSuperuserFromDB(r.Context(), h.userRepo, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to verify superuser status", "encryption", "verify_superuser")
		return
	}

	// Verify caller is a member of the requested scope (superusers bypass)
	if !isSuperuser {
		if regionID != "" {
			isMember, memberErr := h.regionRepo.IsUserInRegion(r.Context(), claims.UserID, regionID)
			if memberErr != nil {
				writeServerError(w, r, memberErr, "Failed to verify membership", "encryption", "check_membership")
				return
			}
			if !isMember {
				writeError(w, http.StatusForbidden, "forbidden", "You must be a member of this scope to view public keys")
				return
			}
		} else if schoolID != "" {
			userSchool, memberErr := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, schoolID)
			if memberErr != nil || userSchool == nil {
				writeError(w, http.StatusForbidden, "forbidden", "You must be a member of this scope to view public keys")
				return
			}
		} else if districtID != "" {
			schools, memberErr := h.schoolRepo.ListByDistrict(r.Context(), districtID)
			if memberErr != nil {
				writeServerError(w, r, memberErr, "Failed to verify membership", "encryption", "check_membership")
				return
			}
			isMember := false
			for _, school := range schools {
				userSchool, sErr := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, school.ID)
				if sErr == nil && userSchool != nil {
					isMember = true
					break
				}
			}
			if !isMember {
				writeError(w, http.StatusForbidden, "forbidden", "You must be a member of this scope to view public keys")
				return
			}
		} else if groupID != "" {
			signalGroup, sgErr := h.signalGroupRepo.GetByID(r.Context(), groupID)
			if sgErr != nil {
				if errors.Is(sgErr, database.ErrSignalGroupNotFound) {
					writeError(w, http.StatusNotFound, "not_found", "Signal group not found")
				} else {
					writeServerError(w, r, sgErr, "Failed to get signal group", "encryption", "get_signal_group")
				}
				return
			}
			if signalGroup.OwnerGroupID == nil {
				writeError(w, http.StatusBadRequest, "invalid_group_type", "Signal group does not have an owner group")
				return
			}
			isMember, memberErr := h.groupRepo.IsUserMember(r.Context(), *signalGroup.OwnerGroupID, claims.UserID)
			if memberErr != nil {
				writeServerError(w, r, memberErr, "Failed to verify membership", "encryption", "check_membership")
				return
			}
			if !isMember {
				writeError(w, http.StatusForbidden, "forbidden", "You must be a member of this scope to view public keys")
				return
			}
		} else if connectionID != "" {
			isMember, memberErr := h.connectionRepo.IsUserInConnection(r.Context(), claims.UserID, connectionID)
			if memberErr != nil {
				writeServerError(w, r, memberErr, "Failed to verify membership", "encryption", "check_membership")
				return
			}
			if !isMember {
				writeError(w, http.StatusForbidden, "forbidden", "You must be a member of this scope to view public keys")
				return
			}
		}
	}

	var keys []models.PublicKeyEntry
	var signalGroupForKeys *models.SignalGroup

	if regionID != "" {
		keys, err = h.encryptionKeyRepo.GetPublicKeysForRegion(r.Context(), regionID)
	} else if schoolID != "" {
		keys, err = h.encryptionKeyRepo.GetPublicKeysForSchool(r.Context(), schoolID)
	} else if districtID != "" {
		keys, err = h.encryptionKeyRepo.GetPublicKeysForDistrict(r.Context(), districtID)
	} else if groupID != "" {
		signalGroupForKeys, err = h.signalGroupRepo.GetByID(r.Context(), groupID)
		if err == nil && signalGroupForKeys != nil && signalGroupForKeys.OwnerGroupID != nil {
			keys, err = h.encryptionKeyRepo.GetPublicKeysForGroup(r.Context(), *signalGroupForKeys.OwnerGroupID, signalGroupForKeys.AccessTier)
		}
	} else if connectionID != "" {
		// Restricted (admin_only) connection chats encrypt their secret for
		// connection admins only; everything else defaults to all members.
		accessLevel := r.URL.Query().Get("access_level")
		if accessLevel != string(models.ConnectionAccessLevelAdminOnly) {
			accessLevel = string(models.ConnectionAccessLevelAllMembers)
		}
		keys, err = h.encryptionKeyRepo.GetPublicKeysForConnection(r.Context(), connectionID, accessLevel)
	}

	if err != nil {
		writeServerError(w, r, err, "Failed to get public keys", "encryption", "get_public_keys")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"keys": keys,
	})
}

// GetPendingRekeys handles GET /api/v1/encryption/pending-rekeys
// Returns secrets where another user needs re-keying and the caller has a valid key
func (h *EncryptionHandler) GetPendingRekeys(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if h.encryptedSecretRepo == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"pending_rekeys": []interface{}{},
		})
		return
	}

	rekeys, err := h.encryptedSecretRepo.GetPendingRekeys(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get pending re-keys", "encryption", "get_pending_rekeys")
		return
	}

	if rekeys == nil {
		rekeys = []database.PendingRekey{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"pending_rekeys": rekeys,
	})
}

// SubmitRekeys handles POST /api/v1/encryption/rekey
// Accepts a batch of re-keyed wrapped DEKs
func (h *EncryptionHandler) SubmitRekeys(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req models.SubmitRekeysRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if len(req.Rekeys) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "At least one re-key entry is required")
		return
	}

	if h.encryptedSecretRepo == nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Re-keying not available")
		return
	}

	// Authorize against the exact set of (secret, target) pairs the server
	// advertised to this caller as pending. GetPendingRekeys guarantees the
	// caller holds a valid (non-rekey-needed) key for the secret AND the target
	// genuinely needs re-keying. Without this, any member holding a secret could
	// overwrite an arbitrary entitled member's wrapped DEK with a forged blob.
	pending, err := h.encryptedSecretRepo.GetPendingRekeys(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to load pending re-keys", "encryption", "submit_rekeys")
		return
	}
	allowed := make(map[string]bool, len(pending))
	for _, p := range pending {
		allowed[p.SecretID+"\x00"+p.TargetUserID] = true
	}

	successCount := 0
	for _, entry := range req.Rekeys {
		if entry.SecretID == "" || entry.TargetUserID == "" || entry.WrappedDEK == "" {
			continue
		}
		if !allowed[entry.SecretID+"\x00"+entry.TargetUserID] {
			slog.WarnContext(r.Context(), "rejecting re-key: pair not in caller's pending set", "secret_id", entry.SecretID, "target_user_id", entry.TargetUserID, "caller_id", claims.UserID)
			continue
		}
		if err := h.encryptedSecretRepo.SubmitRekey(r.Context(), entry.SecretID, entry.TargetUserID, entry.WrappedDEK); err != nil {
			slog.ErrorContext(r.Context(), "failed to submit re-key", "secret_id", entry.SecretID, "target_user_id", entry.TargetUserID, "error", err)
			continue
		}
		successCount++
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rekeyed": successCount,
	})
}

// GetPendingGroupRotations handles GET /api/v1/encryption/pending-group-rotations
// Returns secrets that require a full DEK rotation (member was removed from group/connection).
// Unlike pending-rekeys (user key-pair rotation), these require the caller to generate a fresh DEK,
// re-encrypt the payload, and re-wrap for all surviving recipients.
func (h *EncryptionHandler) GetPendingGroupRotations(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if h.encryptedSecretRepo == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"pending_group_rotations": []interface{}{},
		})
		return
	}

	rotations, err := h.encryptedSecretRepo.GetPendingGroupRotations(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get pending group rotations", "encryption", "get_pending_group_rotations")
		return
	}

	if rotations == nil {
		rotations = []database.PendingGroupRotation{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"pending_group_rotations": rotations,
	})
}

// SubmitGroupRotation handles POST /api/v1/encryption/group-rekey
// Accepts a fully re-encrypted payload and wrapped keys for all surviving recipients.
// The caller must be in wrapped_keys to retain their own decrypt access after the rotation.
func (h *EncryptionHandler) SubmitGroupRotation(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req models.SubmitGroupRotationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.SecretID == "" || req.EncryptedPayload == "" || req.EncryptionIV == "" || len(req.WrappedKeys) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "secret_id, encrypted_payload, encryption_iv, and wrapped_keys are required")
		return
	}

	callerIncluded := false
	for _, wk := range req.WrappedKeys {
		if wk.UserID == claims.UserID {
			callerIncluded = true
			break
		}
	}
	if !callerIncluded {
		writeError(w, http.StatusBadRequest, "validation_error", "wrapped_keys must include an entry for the caller")
		return
	}

	if h.encryptedSecretRepo == nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Encryption not available")
		return
	}

	if err := h.encryptedSecretRepo.SubmitGroupRotation(r.Context(), req.SecretID, claims.UserID, req.EncryptedPayload, req.EncryptionIV, req.WrappedKeys); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "no pending group rotation") {
			writeError(w, http.StatusForbidden, "forbidden", "No pending group rotation found for this secret")
			return
		}
		// The submitted wrapped_keys set must exactly match the surviving recipients; mismatches are
		// client errors, not server faults.
		if strings.Contains(msg, "wrapped_keys") || strings.Contains(msg, "current recipient") ||
			strings.Contains(msg, "surviving recipient") || strings.Contains(msg, "duplicate wrapped_key") {
			writeError(w, http.StatusBadRequest, "validation_error", "wrapped_keys must include exactly the current surviving recipients")
			return
		}
		writeServerError(w, r, err, "Failed to submit group rotation", "encryption", "submit_group_rotation")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rotated": true,
	})
}
