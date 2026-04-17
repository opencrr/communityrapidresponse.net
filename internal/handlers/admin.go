package handlers

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// AdminHandler handles admin-only endpoints
type AdminHandler struct {
	userRepo    *database.UserRepository
	regionRepo  *database.RegionRepository
	auditRepo   *database.AuditRepository
	statusCache *middleware.UserStatusCache
}

// NewAdminHandler creates a new admin handler
func NewAdminHandler(userRepo *database.UserRepository, regionRepo *database.RegionRepository, auditRepo *database.AuditRepository) *AdminHandler {
	return &AdminHandler{
		userRepo:   userRepo,
		regionRepo: regionRepo,
		auditRepo:  auditRepo,
	}
}

// SetStatusCache sets the user status cache for immediate cache eviction
// on privilege changes. Optional - if not set, cache eviction is skipped.
func (h *AdminHandler) SetStatusCache(cache *middleware.UserStatusCache) {
	h.statusCache = cache
}

// requireSuperuser re-checks superuser status from the database as a defense-in-depth
// measure against stale JWT claims. Returns true if the user is a superuser.
func (h *AdminHandler) requireSuperuser(w http.ResponseWriter, r *http.Request, claims *middleware.Claims) bool {
	user, err := h.userRepo.GetByID(r.Context(), claims.UserID)
	if err != nil || user == nil || !user.IsSuperuser {
		if err != nil && !errors.Is(err, database.ErrUserNotFound) {
			slog.ErrorContext(r.Context(), "failed to re-check superuser status", "user_id", claims.UserID, "error", err)
		}
		writeError(w, http.StatusForbidden, "forbidden", "Superuser access required")
		return false
	}
	return true
}

// ListUsers handles GET /api/v1/admin/users
// Supports search via ?q= parameter and pagination via ?page= and ?limit=
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !h.requireSuperuser(w, r, claims) {
		return
	}

	// Parse query parameters
	query := r.URL.Query().Get("q")
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 || page > 1000000 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}

	users, total, err := h.userRepo.Search(r.Context(), query, page, limit)
	if err != nil {
		writeServerError(w, r, err, "User search could not be completed. Please try again.", "admin", "search_users")
		return
	}

	// Audit log: superuser user search (only if searching, not just listing)
	if h.auditRepo != nil && query != "" {
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSuperuserUserSearch, nil, nil, map[string]interface{}{
			"query":        query,
			"results_count": len(users),
		}), "superuser_user_search")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"users": users,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// GetUser handles GET /api/v1/admin/users/:id
func (h *AdminHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !h.requireSuperuser(w, r, claims) {
		return
	}

	userID := getPathParam(r, "id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "User ID required")
		return
	}

	user, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		if err == database.ErrUserNotFound {
			writeError(w, http.StatusNotFound, "not_found", "User not found")
			return
		}
		writeServerError(w, r, err, "User information could not be retrieved. Please try again.", "admin", "get_user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user": map[string]interface{}{
			"id":                user.ID,
			"username":          user.Username,
			"email":             user.Email,
			"verification_tier": user.VerificationTier,
			"postcard_verified": user.PostcardVerified,
			"vouch_verified":    user.VouchVerified,
			"is_superuser":      user.IsSuperuser,
			"created_at":        user.CreatedAt,
			"last_login":        user.LastLogin,
		},
	})
}

// GrantVouchVerification handles POST /api/v1/admin/users/:id/grant-vouch
func (h *AdminHandler) GrantVouchVerification(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !h.requireSuperuser(w, r, claims) {
		return
	}

	userID := getPathParam(r, "id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "User ID required")
		return
	}

	// Verify user exists
	user, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		if err == database.ErrUserNotFound {
			writeError(w, http.StatusNotFound, "not_found", "User not found")
			return
		}
		writeServerError(w, r, err, "User information could not be retrieved. Please try again.", "admin", "grant_vouch")
		return
	}

	// Grant vouch verification
	if err := h.userRepo.SetVouchVerified(r.Context(), userID, true); err != nil {
		writeServerError(w, r, err, "Vouch verification could not be granted. Please try again.", "admin", "grant_vouch")
		return
	}

	// Invalidate tokens so user gets fresh claims with updated verification status
	if err := h.userRepo.InvalidateTokens(r.Context(), userID); err != nil {
		slog.WarnContext(r.Context(), "failed to invalidate tokens after grant vouch", "user_id", userID, "error", err)
	}
	if h.statusCache != nil {
		h.statusCache.Invalidate(userID)
	}

	// Upgrade any pending user_regions to verified status
	// Admin status depends on whether user also has postcard verification
	isAdmin := user.PostcardVerified
	upgradedCount, err := h.regionRepo.UpgradeAllPendingUserRegions(r.Context(), userID, isAdmin)
	if err != nil {
		slog.WarnContext(r.Context(), "failed to upgrade pending user_regions", "user_id", userID, "error", err)
		// Don't fail the request - the main vouch verification was granted
	} else if upgradedCount > 0 {
		slog.InfoContext(r.Context(), "upgraded pending user_regions to verified", "count", upgradedCount, "user_id", userID, "is_admin", isAdmin)
	}

	// If user now has both verifications, also upgrade is_admin on already-verified regions
	if isAdmin {
		adminUpgradeCount, err := h.regionRepo.UpgradeVerifiedUserRegionsToAdmin(r.Context(), userID)
		if err != nil {
			slog.WarnContext(r.Context(), "failed to upgrade verified user_regions to admin", "user_id", userID, "error", err)
		} else if adminUpgradeCount > 0 {
			slog.InfoContext(r.Context(), "upgraded verified user_regions to admin", "count", adminUpgradeCount, "user_id", userID)
			upgradedCount += adminUpgradeCount
		}
	}

	// Audit log: superuser grant vouch
	if h.auditRepo != nil {
		resourceType := "user"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSuperuserGrantVouch, &resourceType, &userID, map[string]interface{}{
			"target_username":   user.Username,
			"target_email":      user.Email,
			"regions_upgraded":  upgradedCount,
		}), "superuser_grant_vouch")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Vouch verification granted",
		"user": map[string]interface{}{
			"id":                user.ID,
			"username":          user.Username,
			"email":             user.Email,
			"postcard_verified": user.PostcardVerified,
			"vouch_verified":    true,
		},
		"regions_upgraded": upgradedCount,
	})
}

// RevokeVouchVerification handles POST /api/v1/admin/users/:id/revoke-vouch
func (h *AdminHandler) RevokeVouchVerification(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !h.requireSuperuser(w, r, claims) {
		return
	}

	userID := getPathParam(r, "id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "User ID required")
		return
	}

	// Verify user exists
	user, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		if err == database.ErrUserNotFound {
			writeError(w, http.StatusNotFound, "not_found", "User not found")
			return
		}
		writeServerError(w, r, err, "User information could not be retrieved. Please try again.", "admin", "revoke_vouch")
		return
	}

	// Revoke vouch verification
	if err := h.userRepo.SetVouchVerified(r.Context(), userID, false); err != nil {
		writeServerError(w, r, err, "Vouch verification could not be revoked. Please try again.", "admin", "revoke_vouch")
		return
	}

	// Invalidate tokens so user's stale claims are rejected
	if err := h.userRepo.InvalidateTokens(r.Context(), userID); err != nil {
		slog.WarnContext(r.Context(), "failed to invalidate tokens after revoke vouch", "user_id", userID, "error", err)
	}
	if h.statusCache != nil {
		h.statusCache.Invalidate(userID)
	}

	// Audit log: superuser revoke vouch
	if h.auditRepo != nil {
		resourceType := "user"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSuperuserRevokeVouch, &resourceType, &userID, map[string]interface{}{
			"target_username": user.Username,
			"target_email":    user.Email,
		}), "superuser_revoke_vouch")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Vouch verification revoked",
		"user": map[string]interface{}{
			"id":                user.ID,
			"username":          user.Username,
			"email":             user.Email,
			"postcard_verified": user.PostcardVerified,
			"vouch_verified":    false,
		},
	})
}

// BlockUser handles POST /api/v1/admin/users/:id/block
func (h *AdminHandler) BlockUser(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !h.requireSuperuser(w, r, claims) {
		return
	}

	userID := getPathParam(r, "id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "User ID required")
		return
	}

	// Can't block yourself
	if userID == claims.UserID {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cannot block yourself")
		return
	}

	// Parse request body for reason
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Reason is optional, continue without it
		req.Reason = ""
	}

	// Verify user exists
	user, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		if err == database.ErrUserNotFound {
			writeError(w, http.StatusNotFound, "not_found", "User not found")
			return
		}
		writeServerError(w, r, err, "User information could not be retrieved. Please try again.", "admin", "block_user")
		return
	}

	// Can't block another superuser
	if user.IsSuperuser {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cannot block a superuser")
		return
	}

	// Block the user
	if err := h.userRepo.Block(r.Context(), userID, claims.UserID, req.Reason); err != nil {
		writeServerError(w, r, err, "User could not be blocked. Please try again.", "admin", "block_user")
		return
	}

	// Invalidate existing tokens so the blocked user is rejected immediately
	if err := h.userRepo.InvalidateTokens(r.Context(), userID); err != nil {
		slog.WarnContext(r.Context(), "failed to invalidate tokens after block", "user_id", userID, "error", err)
	}
	if h.statusCache != nil {
		h.statusCache.Invalidate(userID)
	}

	// Audit log: user blocked
	if h.auditRepo != nil {
		resourceType := "user"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionUserBlocked, &resourceType, &userID, map[string]interface{}{
			"target_username": user.Username,
			"target_email":    user.Email,
			"reason":          req.Reason,
		}), "user_blocked")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "User has been blocked",
		"user": map[string]interface{}{
			"id":         user.ID,
			"username":   user.Username,
			"email":      user.Email,
			"is_blocked": true,
		},
	})
}

// UnblockUser handles POST /api/v1/admin/users/:id/unblock
func (h *AdminHandler) UnblockUser(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !h.requireSuperuser(w, r, claims) {
		return
	}

	userID := getPathParam(r, "id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "User ID required")
		return
	}

	// Verify user exists
	user, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		if err == database.ErrUserNotFound {
			writeError(w, http.StatusNotFound, "not_found", "User not found")
			return
		}
		writeServerError(w, r, err, "User information could not be retrieved. Please try again.", "admin", "unblock_user")
		return
	}

	// Unblock the user
	if err := h.userRepo.Unblock(r.Context(), userID); err != nil {
		writeServerError(w, r, err, "User could not be unblocked. Please try again.", "admin", "unblock_user")
		return
	}

	// Invalidate tokens so unblocked user must re-login with fresh claims
	if err := h.userRepo.InvalidateTokens(r.Context(), userID); err != nil {
		slog.WarnContext(r.Context(), "failed to invalidate tokens after unblock", "user_id", userID, "error", err)
	}
	if h.statusCache != nil {
		h.statusCache.Invalidate(userID)
	}

	// Audit log: user unblocked
	if h.auditRepo != nil {
		resourceType := "user"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionUserUnblocked, &resourceType, &userID, map[string]interface{}{
			"target_username": user.Username,
			"target_email":    user.Email,
		}), "user_unblocked")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "User has been unblocked",
		"user": map[string]interface{}{
			"id":         user.ID,
			"username":   user.Username,
			"email":      user.Email,
			"is_blocked": false,
		},
	})
}

// DeleteUser handles DELETE /api/v1/admin/users/:id
func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !h.requireSuperuser(w, r, claims) {
		return
	}

	userID := getPathParam(r, "id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "User ID required")
		return
	}

	// Can't delete yourself
	if userID == claims.UserID {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cannot delete yourself")
		return
	}

	// Verify user exists
	user, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		if err == database.ErrUserNotFound {
			writeError(w, http.StatusNotFound, "not_found", "User not found")
			return
		}
		writeServerError(w, r, err, "User information could not be retrieved. Please try again.", "admin", "delete_user")
		return
	}

	// Can't delete another superuser
	if user.IsSuperuser {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cannot delete a superuser")
		return
	}

	// Invalidate tokens before deletion so any in-flight requests are rejected
	if err := h.userRepo.InvalidateTokens(r.Context(), userID); err != nil {
		slog.WarnContext(r.Context(), "failed to invalidate tokens before delete", "user_id", userID, "error", err)
	}
	if h.statusCache != nil {
		h.statusCache.Invalidate(userID)
	}

	// Delete the user
	if err := h.userRepo.Delete(r.Context(), userID); err != nil {
		writeServerError(w, r, err, "User could not be deleted. Please try again.", "admin", "delete_user")
		return
	}

	// Audit log: user deleted
	if h.auditRepo != nil {
		resourceType := "user"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSuperuserUserDelete, &resourceType, &userID, map[string]interface{}{
			"target_username": user.Username,
			"target_email":    user.Email,
		}), "superuser_user_delete")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "User has been permanently deleted",
		"user_id": userID,
	})
}

// GetAuditLogs handles GET /api/v1/admin/audit-logs
func (h *AdminHandler) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !h.requireSuperuser(w, r, claims) {
		return
	}

	filter := h.parseAuditLogFilter(r)

	result, err := h.auditRepo.Query(r.Context(), filter)
	if err != nil {
		writeServerError(w, r, err, "Audit logs could not be retrieved. Please try again.", "admin", "query_audit_logs")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// ExportAuditLogs handles GET /api/v1/admin/audit-logs/export
func (h *AdminHandler) ExportAuditLogs(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !h.requireSuperuser(w, r, claims) {
		return
	}

	filter := h.parseAuditLogFilter(r)
	// For export, we want all matching records (up to a reasonable limit)
	filter.Page = 1
	filter.Limit = 10000 // Max export limit

	result, err := h.auditRepo.Query(r.Context(), filter)
	if err != nil {
		writeServerError(w, r, err, "Audit log export could not be completed. Please try again.", "admin", "export_audit_logs")
		return
	}

	format := r.URL.Query().Get("format")
	if format == "csv" {
		h.exportAuditLogsCSV(w, result.Logs)
	} else {
		h.exportAuditLogsJSON(w, result.Logs)
	}
}

// parseAuditLogFilter parses query parameters into an AuditLogFilter
func (h *AdminHandler) parseAuditLogFilter(r *http.Request) models.AuditLogFilter {
	filter := models.AuditLogFilter{}

	if userID := r.URL.Query().Get("user_id"); userID != "" {
		filter.UserID = &userID
	}

	if actions := r.URL.Query().Get("action"); actions != "" {
		filter.Actions = strings.Split(actions, ",")
	}

	if resourceType := r.URL.Query().Get("resource_type"); resourceType != "" {
		filter.ResourceType = &resourceType
	}

	if resourceID := r.URL.Query().Get("resource_id"); resourceID != "" {
		filter.ResourceID = &resourceID
	}

	if dateFrom := r.URL.Query().Get("date_from"); dateFrom != "" {
		if t, err := time.Parse(time.RFC3339, dateFrom); err == nil {
			filter.DateFrom = &t
		}
	}

	if dateTo := r.URL.Query().Get("date_to"); dateTo != "" {
		if t, err := time.Parse(time.RFC3339, dateTo); err == nil {
			filter.DateTo = &t
		}
	}

	if ipAddress := r.URL.Query().Get("ip_address"); ipAddress != "" {
		filter.IPAddress = &ipAddress
	}

	filter.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if filter.Page < 1 {
		filter.Page = 1
	}

	filter.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 50
	}

	return filter
}

// exportAuditLogsCSV exports audit logs as CSV
func (h *AdminHandler) exportAuditLogsCSV(w http.ResponseWriter, logs []models.AuditLog) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=audit_logs_%s.csv", time.Now().Format("20060102_150405")))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write header
	header := []string{"ID", "User ID", "Action", "Resource Type", "Resource ID", "Details", "IP Address", "User Agent", "Created At"}
	if err := writer.Write(header); err != nil {
		slog.Error("failed to write CSV header", "error", err)
		return
	}

	// Write rows
	for _, logEntry := range logs {
		row := []string{
			logEntry.ID,
			stringOrEmpty(logEntry.UserID),
			logEntry.Action,
			stringOrEmpty(logEntry.ResourceType),
			stringOrEmpty(logEntry.ResourceID),
			string(logEntry.Details),
			stringOrEmpty(logEntry.IPAddress),
			stringOrEmpty(logEntry.UserAgent),
			logEntry.CreatedAt.Format(time.RFC3339),
		}
		if err := writer.Write(row); err != nil {
			slog.Error("failed to write CSV row", "error", err)
			return
		}
	}
}

// exportAuditLogsJSON exports audit logs as JSON
func (h *AdminHandler) exportAuditLogsJSON(w http.ResponseWriter, logs []models.AuditLog) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=audit_logs_%s.json", time.Now().Format("20060102_150405")))

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"logs":       logs,
		"exported_at": time.Now().UTC(),
		"count":      len(logs),
	}); err != nil {
		slog.Error("failed to encode JSON export", "error", err)
	}
}

// stringOrEmpty returns the string value or empty string if nil
func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
