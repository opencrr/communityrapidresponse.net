package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

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
	notificationService NotificationServiceInterface
}

// NewEncryptionHandler creates a new encryption handler
func NewEncryptionHandler(
	encryptionKeyRepo *database.EncryptionKeyRepository,
	encryptedSecretRepo *database.EncryptedSecretRepository,
	regionRepo *database.RegionRepository,
	schoolRepo *database.SchoolRepository,
	userRepo *database.UserRepository,
) *EncryptionHandler {
	return &EncryptionHandler{
		encryptionKeyRepo:   encryptionKeyRepo,
		encryptedSecretRepo: encryptedSecretRepo,
		regionRepo:          regionRepo,
		schoolRepo:          schoolRepo,
		userRepo:            userRepo,
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

// GetPublicKeys handles GET /api/v1/encryption/public-keys?region_id=X (or school_id, district_id)
func (h *EncryptionHandler) GetPublicKeys(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	regionID := r.URL.Query().Get("region_id")
	schoolID := r.URL.Query().Get("school_id")
	districtID := r.URL.Query().Get("district_id")

	// SCOPE LIMITATION: only region/school/district recipient enumeration exists.
	// Group- and connection-owned signal chats are intentionally NOT supported here
	// yet (they are metadata-only — see DESIGN/PR "Known limitations"). Do NOT add
	// group_id/connection_id branches without also landing DEK revocation
	// (delete encrypted_secret_keys + rotate the DEK) on member removal / ban /
	// connection-leave / tier-downgrade. Enumeration without revocation lets a
	// removed member who cached the unwrapped DEK keep decrypting.
	// Tracked: github.com/opencrr/communityrapidresponse.net/issues/91.

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
	if scopeCount != 1 {
		writeError(w, http.StatusBadRequest, "validation_error", "Exactly one of region_id, school_id, or district_id must be provided")
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
		} else {
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
		}
	}

	var keys []models.PublicKeyEntry

	if regionID != "" {
		keys, err = h.encryptionKeyRepo.GetPublicKeysForRegion(r.Context(), regionID)
	} else if schoolID != "" {
		keys, err = h.encryptionKeyRepo.GetPublicKeysForSchool(r.Context(), schoolID)
	} else {
		keys, err = h.encryptionKeyRepo.GetPublicKeysForDistrict(r.Context(), districtID)
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
// If encrypted_payload and encryption_iv are provided, performs a group/connection rotation
// (generates fresh DEK, re-encrypts payload, wraps for survivors)
// Otherwise, performs user key rotation (re-wraps existing DEK for new public key)
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

	// Check if this is a group rotation (payload update)
	isGroupRotation := req.EncryptedPayload != "" && req.EncryptionIV != ""

	successCount := 0
	if isGroupRotation {
		// Group rotation: update payload and all wrapped keys in one batch
		if len(req.Rekeys) == 0 {
			writeError(w, http.StatusBadRequest, "validation_error", "At least one survivor required for group rotation")
			return
		}

		// Verify all rekeys are for the same secret
		secretID := req.Rekeys[0].SecretID
		for _, entry := range req.Rekeys {
			if entry.SecretID != secretID {
				writeError(w, http.StatusBadRequest, "validation_error", "All rekeys in a group rotation must be for the same secret")
				return
			}
		}

		// Prepare wrapped keys for update
		wrappedKeys := make([]models.WrappedKeyEntry, 0, len(req.Rekeys))
		for _, entry := range req.Rekeys {
			if entry.SecretID == "" || entry.TargetUserID == "" || entry.WrappedDEK == "" {
				continue
			}
			if !allowed[entry.SecretID+"\x00"+entry.TargetUserID] {
				slog.WarnContext(r.Context(), "rejecting re-key: pair not in caller's pending set", "secret_id", entry.SecretID, "target_user_id", entry.TargetUserID, "caller_id", claims.UserID)
				continue
			}
			wrappedKeys = append(wrappedKeys, models.WrappedKeyEntry{
				UserID:     entry.TargetUserID,
				WrappedDEK: entry.WrappedDEK,
			})
		}

		if len(wrappedKeys) == 0 {
			writeError(w, http.StatusBadRequest, "validation_error", "No valid wrapped keys for group rotation")
			return
		}

		// Update the secret with new payload and wrapped keys
		if err := h.encryptedSecretRepo.UpdatePayloadAndKeys(r.Context(), secretID, req.EncryptedPayload, req.EncryptionIV, claims.UserID, wrappedKeys); err != nil {
			writeServerError(w, r, err, "Failed to update secret during group rotation", "encryption", "group_rotation")
			return
		}

		successCount = len(wrappedKeys)
	} else {
		// User key rotation: re-wrap existing DEK for new public key
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
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rekeyed": successCount,
	})
}
