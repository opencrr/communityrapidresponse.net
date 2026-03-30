package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// GroupHandler handles group endpoints.
type GroupHandler struct {
	groupRepo  *database.GroupRepository
	regionRepo *database.RegionRepository
	userRepo   *database.UserRepository
	auditRepo  *database.AuditRepository
}

// NewGroupHandler creates a new group handler.
func NewGroupHandler(
	groupRepo *database.GroupRepository,
	regionRepo *database.RegionRepository,
	userRepo *database.UserRepository,
	auditRepo *database.AuditRepository,
) *GroupHandler {
	return &GroupHandler{
		groupRepo:  groupRepo,
		regionRepo: regionRepo,
		userRepo:   userRepo,
		auditRepo:  auditRepo,
	}
}

// Create handles POST /api/v1/groups
func (h *GroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !claims.VouchVerified {
		writeError(w, http.StatusForbidden, "forbidden", "Vouch verification required to create groups")
		return
	}

	var req models.CreateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate name
	if len(req.Name) < 3 || len(req.Name) > 255 {
		writeError(w, http.StatusBadRequest, "validation_error", "Group name must be between 3 and 255 characters")
		return
	}

	// Validate at least one region
	if len(req.RegionIDs) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "At least one region_id is required")
		return
	}

	// Validate visibility
	if req.Visibility != "listed" && req.Visibility != "unlisted" {
		writeError(w, http.StatusBadRequest, "validation_error", "Visibility must be 'listed' or 'unlisted'")
		return
	}

	// Verify user is a member of each region
	for _, regionID := range req.RegionIDs {
		isMember, err := h.regionRepo.IsUserInRegion(r.Context(), claims.UserID, regionID)
		if err != nil {
			writeServerError(w, r, err, "Failed to verify region membership", "group", "create_group")
			return
		}
		if !isMember {
			writeError(w, http.StatusForbidden, "forbidden", "You must be a verified member of all specified regions")
			return
		}
	}

	group, err := h.groupRepo.Create(r.Context(), &req, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to create group", "group", "create_group")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupCreated, &resourceType, &group.ID, map[string]interface{}{
			"name":       group.Name,
			"region_ids": req.RegionIDs,
		}), "group_created")
	}

	writeJSON(w, http.StatusCreated, group)
}

// List handles GET /api/v1/groups
func (h *GroupHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	groups, err := h.groupRepo.ListByUser(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list groups", "group", "list_groups")
		return
	}

	if groups == nil {
		groups = []models.GroupWithDetails{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"groups": groups,
	})
}

// Get handles GET /api/v1/groups/:id
func (h *GroupHandler) Get(w http.ResponseWriter, r *http.Request) {
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

	groupDetails, err := h.groupRepo.GetByIDWithDetails(r.Context(), groupID, claims.UserID)
	if errors.Is(err, database.ErrGroupNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Group not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get group", "group", "get_group")
		return
	}

	// If unlisted and user is not a member, don't reveal existence
	if groupDetails.Visibility == models.GroupVisibilityUnlisted && !groupDetails.IsUserMember {
		writeError(w, http.StatusNotFound, "not_found", "Group not found")
		return
	}

	writeJSON(w, http.StatusOK, groupDetails)
}

// Update handles PUT /api/v1/groups/:id
func (h *GroupHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	// Check admin permission
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check permissions", "group", "update_group")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group to update it")
		return
	}

	var req models.UpdateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate name length if provided
	if req.Name != nil && (len(*req.Name) < 3 || len(*req.Name) > 255) {
		writeError(w, http.StatusBadRequest, "validation_error", "Group name must be between 3 and 255 characters")
		return
	}

	// Cannot change visibility to listed if group is provisional
	if req.Visibility != nil && *req.Visibility == "listed" {
		group, err := h.groupRepo.GetByID(r.Context(), groupID)
		if errors.Is(err, database.ErrGroupNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Group not found")
			return
		}
		if err != nil {
			writeServerError(w, r, err, "Failed to get group", "group", "update_group")
			return
		}
		if group.Status == models.GroupStatusProvisional {
			writeError(w, http.StatusBadRequest, "validation_error", "Cannot set visibility to 'listed' while group is provisional")
			return
		}
	}

	if err := h.groupRepo.Update(r.Context(), groupID, &req); err != nil {
		if errors.Is(err, database.ErrGroupNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Group not found")
			return
		}
		writeServerError(w, r, err, "Failed to update group", "group", "update_group")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupUpdated, &resourceType, &groupID, map[string]interface{}{
			"updates": req,
		}), "group_updated")
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Group updated successfully",
		"id":      groupID,
	})
}

// Delete handles DELETE /api/v1/groups/:id
func (h *GroupHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "Only superusers can delete groups")
		return
	}

	groupID := getPathParam(r, "id")
	if groupID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Group ID required")
		return
	}

	// Check group exists before deleting
	group, err := h.groupRepo.GetByID(r.Context(), groupID)
	if errors.Is(err, database.ErrGroupNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Group not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get group", "group", "delete_group")
		return
	}

	if err := h.groupRepo.Delete(r.Context(), groupID); err != nil {
		writeServerError(w, r, err, "Failed to delete group", "group", "delete_group")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupDeleted, &resourceType, &groupID, map[string]interface{}{
			"name": group.Name,
		}), "group_deleted")
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Group deleted successfully",
		"id":      groupID,
	})
}

// ListMembers handles GET /api/v1/groups/:id/members
func (h *GroupHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
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

	// Check membership (superusers bypass)
	if !claims.IsSuperuser {
		isMember, err := h.groupRepo.IsUserMember(r.Context(), groupID, claims.UserID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check membership", "group", "list_members")
			return
		}
		if !isMember {
			writeError(w, http.StatusForbidden, "forbidden", "You must be a member of this group")
			return
		}
	}

	members, err := h.groupRepo.GetMembers(r.Context(), groupID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list members", "group", "list_members")
		return
	}

	if members == nil {
		members = []models.GroupMemberWithUser{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"members": members,
	})
}

// Leave handles POST /api/v1/groups/:id/leave
func (h *GroupHandler) Leave(w http.ResponseWriter, r *http.Request) {
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

	// Check membership
	isMember, err := h.groupRepo.IsUserMember(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check membership", "group", "leave_group")
		return
	}
	if !isMember {
		writeError(w, http.StatusBadRequest, "not_member", "You are not a member of this group")
		return
	}

	// If user is admin, check there are other admins
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check admin status", "group", "leave_group")
		return
	}
	if isAdmin {
		members, err := h.groupRepo.GetMembers(r.Context(), groupID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check group members", "group", "leave_group")
			return
		}
		adminCount := 0
		for _, member := range members {
			if member.IsAdmin {
				adminCount++
			}
		}
		if adminCount <= 1 {
			writeError(w, http.StatusBadRequest, "last_admin", "Cannot leave as the last admin of the group")
			return
		}
	}

	if err := h.groupRepo.RemoveMember(r.Context(), groupID, claims.UserID); err != nil {
		writeServerError(w, r, err, "Failed to leave group", "group", "leave_group")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupMemberRemoved, &resourceType, &groupID, map[string]interface{}{
			"user_id": claims.UserID,
			"action":  "self_leave",
		}), "group_member_removed")
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Successfully left the group",
	})
}
