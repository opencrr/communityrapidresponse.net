package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// GroupHandler handles group endpoints.
type GroupHandler struct {
	groupRepo       *database.GroupRepository
	signalGroupRepo *database.SignalGroupRepository
	regionRepo      *database.RegionRepository
	userRepo        *database.UserRepository
	auditRepo       *database.AuditRepository
}

// NewGroupHandler creates a new group handler.
func NewGroupHandler(
	groupRepo *database.GroupRepository,
	signalGroupRepo *database.SignalGroupRepository,
	regionRepo *database.RegionRepository,
	userRepo *database.UserRepository,
	auditRepo *database.AuditRepository,
) *GroupHandler {
	return &GroupHandler{
		groupRepo:       groupRepo,
		signalGroupRepo: signalGroupRepo,
		regionRepo:      regionRepo,
		userRepo:        userRepo,
		auditRepo:       auditRepo,
	}
}

// Browse handles GET /api/v1/groups/browse
func (h *GroupHandler) Browse(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	regionID := r.URL.Query().Get("region_id")

	var groups []models.GroupWithDetails
	var err error

	if regionID != "" {
		// Check if user is verified in this region
		includeDiscoverable := false
		if claims.VouchVerified {
			inRegion, regionErr := h.regionRepo.IsUserInRegion(r.Context(), claims.UserID, regionID)
			if regionErr != nil {
				writeServerError(w, r, regionErr, "Failed to check region membership", "group", "browse")
				return
			}
			includeDiscoverable = inRegion
		}
		groups, err = h.groupRepo.BrowseByRegion(r.Context(), regionID, includeDiscoverable)
	} else {
		groups, err = h.groupRepo.BrowseAll(r.Context())
	}

	if err != nil {
		writeServerError(w, r, err, "Failed to browse groups", "group", "browse")
		return
	}

	if groups == nil {
		groups = []models.GroupWithDetails{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"groups": groups,
		"disclaimer": []string{
			"This platform verifies member residency. It does not verify, endorse, or guarantee the quality, safety, or leadership of any group.",
			"Group names, descriptions, and claims are provided by the group's organizers, not by this platform.",
		},
	})
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

// CreateInviteLink handles POST /api/v1/groups/:id/invite-links
func (h *GroupHandler) CreateInviteLink(w http.ResponseWriter, r *http.Request) {
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

	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check permissions", "group", "create_invite_link")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group")
		return
	}

	var req models.CreateInviteLinkRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
			return
		}
	}

	link, err := h.groupRepo.CreateInviteLink(r.Context(), groupID, claims.UserID, &req)
	if err != nil {
		writeServerError(w, r, err, "Failed to create invite link", "group", "create_invite_link")
		return
	}

	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupInviteLinkCreated, &resourceType, &groupID, map[string]interface{}{
			"link_id": link.ID,
		}), "group_invite_link_created")
	}

	writeJSON(w, http.StatusCreated, link)
}

// ListInviteLinks handles GET /api/v1/groups/:id/invite-links
func (h *GroupHandler) ListInviteLinks(w http.ResponseWriter, r *http.Request) {
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

	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check permissions", "group", "list_invite_links")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group")
		return
	}

	links, err := h.groupRepo.ListInviteLinks(r.Context(), groupID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list invite links", "group", "list_invite_links")
		return
	}

	if links == nil {
		links = []models.GroupInviteLink{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"invite_links": links,
	})
}

// JoinViaLink handles POST /api/v1/groups/join/:token
func (h *GroupHandler) JoinViaLink(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	token := getPathParam(r, "token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Invite token required")
		return
	}

	// Validate and consume the invite link
	link, err := h.groupRepo.ConsumeInviteLink(r.Context(), token)
	if err != nil {
		if errors.Is(err, database.ErrInviteLinkNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Invite link not found")
			return
		}
		if errors.Is(err, database.ErrInviteLinkExpired) {
			writeError(w, http.StatusGone, "expired", "Invite link has expired")
			return
		}
		if errors.Is(err, database.ErrInviteLinkExhausted) {
			writeError(w, http.StatusGone, "exhausted", "Invite link has reached its maximum uses")
			return
		}
		writeServerError(w, r, err, "Failed to validate invite link", "group", "join_via_link")
		return
	}

	// Check if user is already a member
	isMember, err := h.groupRepo.IsUserMember(r.Context(), link.GroupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check membership", "group", "join_via_link")
		return
	}
	if isMember {
		writeError(w, http.StatusConflict, "already_member", "You are already a member of this group")
		return
	}

	// Add user as regular member
	if err := h.groupRepo.AddMember(r.Context(), link.GroupID, claims.UserID, false, false); err != nil {
		writeServerError(w, r, err, "Failed to join group", "group", "join_via_link")
		return
	}

	// Check if the group should graduate
	graduated, err := h.groupRepo.CheckAndGraduate(r.Context(), link.GroupID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check graduation", "group", "join_via_link")
		return
	}

	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupMemberAdded, &resourceType, &link.GroupID, map[string]interface{}{
			"method":    "invite_link",
			"link_id":   link.ID,
			"graduated": graduated,
		}), "group_member_added")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":   "Successfully joined the group",
		"group_id":  link.GroupID,
		"graduated": graduated,
	})
}

// CreateInvitation handles POST /api/v1/groups/:id/invitations
func (h *GroupHandler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
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

	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check permissions", "group", "create_invitation")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group")
		return
	}

	var req models.CreateGroupInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "User ID is required")
		return
	}

	// Verify target user exists
	_, err = h.userRepo.GetByID(r.Context(), req.UserID)
	if errors.Is(err, database.ErrUserNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "User not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to verify user", "group", "create_invitation")
		return
	}

	// Check if target user is already a member
	isMember, err := h.groupRepo.IsUserMember(r.Context(), groupID, req.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check membership", "group", "create_invitation")
		return
	}
	if isMember {
		writeError(w, http.StatusConflict, "already_member", "User is already a member of this group")
		return
	}

	invitation, err := h.groupRepo.CreateInvitation(r.Context(), groupID, req.UserID, claims.UserID)
	if err != nil {
		if errors.Is(err, database.ErrInvitationAlreadyPending) {
			writeError(w, http.StatusConflict, "invitation_pending", "An invitation is already pending for this user")
			return
		}
		writeServerError(w, r, err, "Failed to create invitation", "group", "create_invitation")
		return
	}

	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupInvitationSent, &resourceType, &groupID, map[string]interface{}{
			"invitation_id": invitation.ID,
			"target_user":   req.UserID,
		}), "group_invitation_sent")
	}

	writeJSON(w, http.StatusCreated, invitation)
}

// ListMyInvitations handles GET /api/v1/groups/invitations
func (h *GroupHandler) ListMyInvitations(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	invitations, err := h.groupRepo.ListPendingInvitationsForUser(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list invitations", "group", "list_invitations")
		return
	}

	if invitations == nil {
		invitations = []models.GroupInvitationWithDetails{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"invitations": invitations,
	})
}

// VouchForMember handles POST /api/v1/groups/:id/trust-vouches
func (h *GroupHandler) VouchForMember(w http.ResponseWriter, r *http.Request) {
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

	var req models.CreateTrustVouchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "User ID is required")
		return
	}

	err := h.groupRepo.CreateTrustVouch(r.Context(), groupID, claims.UserID, req.UserID)
	if err != nil {
		if errors.Is(err, database.ErrSelfVouch) {
			writeError(w, http.StatusBadRequest, "self_vouch", "Cannot vouch for yourself")
			return
		}
		if errors.Is(err, database.ErrNotTrustedOrAdmin) {
			writeError(w, http.StatusForbidden, "forbidden", "You must be a trusted member or admin to vouch")
			return
		}
		if errors.Is(err, database.ErrGroupNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Group or target member not found")
			return
		}
		writeServerError(w, r, err, "Failed to create trust vouch", "group", "trust_vouch")
		return
	}

	vouchCount, err := h.groupRepo.GetTrustVouchCount(r.Context(), groupID, req.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get vouch count", "group", "trust_vouch")
		return
	}

	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupTrustVouchCreated, &resourceType, &groupID, map[string]interface{}{
			"vouched_user_id": req.UserID,
			"vouch_count":     vouchCount,
		}), "group_trust_vouch_created")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"vouch_count": vouchCount,
	})
}

// GetTrustVouchStatus handles GET /api/v1/groups/:id/trust-vouches/:user_id
func (h *GroupHandler) GetTrustVouchStatus(w http.ResponseWriter, r *http.Request) {
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

	targetUserID := getPathParam(r, "user_id")
	if targetUserID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "User ID required")
		return
	}

	// Check caller is a member
	if !claims.IsSuperuser {
		isMember, err := h.groupRepo.IsUserMember(r.Context(), groupID, claims.UserID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check membership", "group", "trust_vouch_status")
			return
		}
		if !isMember {
			writeError(w, http.StatusForbidden, "forbidden", "You must be a member of this group")
			return
		}
	}

	vouchCount, err := h.groupRepo.GetTrustVouchCount(r.Context(), groupID, targetUserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get vouch count", "group", "trust_vouch_status")
		return
	}

	member, err := h.groupRepo.GetMember(r.Context(), groupID, targetUserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get member", "group", "trust_vouch_status")
		return
	}

	trustLevel := ""
	if member != nil {
		trustLevel = member.TrustLevel
	}

	group, err := h.groupRepo.GetByID(r.Context(), groupID)
	if errors.Is(err, database.ErrGroupNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Group not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get group", "group", "trust_vouch_status")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"vouch_count":  vouchCount,
		"trust_level":  trustLevel,
		"threshold":    group.TrustedVouchThreshold,
	})
}

// RespondToInvitation handles POST /api/v1/groups/invitations/:id/respond
func (h *GroupHandler) RespondToInvitation(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	invitationID := getPathParam(r, "id")
	if invitationID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Invitation ID required")
		return
	}

	var req models.RespondToGroupInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	invitation, err := h.groupRepo.RespondToInvitation(r.Context(), invitationID, claims.UserID, req.Accept)
	if err != nil {
		if errors.Is(err, database.ErrInvitationNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Invitation not found")
			return
		}
		if errors.Is(err, database.ErrInvitationExpired) {
			writeError(w, http.StatusGone, "expired", "Invitation has expired")
			return
		}
		writeServerError(w, r, err, "Failed to respond to invitation", "group", "respond_to_invitation")
		return
	}

	if req.Accept {
		if err := h.groupRepo.AddMember(r.Context(), invitation.GroupID, claims.UserID, false, false); err != nil {
			writeServerError(w, r, err, "Failed to add member", "group", "respond_to_invitation")
			return
		}

		graduated, err := h.groupRepo.CheckAndGraduate(r.Context(), invitation.GroupID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check graduation", "group", "respond_to_invitation")
			return
		}

		if h.auditRepo != nil {
			resourceType := "group"
			logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupMemberAdded, &resourceType, &invitation.GroupID, map[string]interface{}{
				"method":        "invitation",
				"invitation_id": invitation.ID,
				"graduated":     graduated,
			}), "group_member_added")
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message":   "Invitation accepted",
			"group_id":  invitation.GroupID,
			"graduated": graduated,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Invitation declined",
	})
}

const maxSignalGroupsPerOwnerGroup = 5

// CreateSignalGroup handles POST /api/v1/groups/:id/signal-groups
func (h *GroupHandler) CreateSignalGroup(w http.ResponseWriter, r *http.Request) {
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

	// Check group exists and is active
	group, err := h.groupRepo.GetByID(r.Context(), groupID)
	if errors.Is(err, database.ErrGroupNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Group not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get group", "group", "create_signal_group")
		return
	}
	if group.Status == models.GroupStatusProvisional {
		writeError(w, http.StatusBadRequest, "group_provisional", "Cannot create signal groups for a provisional group")
		return
	}

	// Check user is admin of this group
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check permissions", "group", "create_signal_group")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group")
		return
	}

	// Check signal group limit
	count, err := h.signalGroupRepo.CountByOwnerGroup(r.Context(), groupID)
	if err != nil {
		writeServerError(w, r, err, "Failed to count signal groups", "group", "create_signal_group")
		return
	}
	if count >= maxSignalGroupsPerOwnerGroup {
		writeError(w, http.StatusBadRequest, "limit_reached", "Maximum number of signal groups reached for this group")
		return
	}

	var req models.CreateGroupSignalGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate group_name
	if req.GroupName == "" || len(req.GroupName) > 255 {
		writeError(w, http.StatusBadRequest, "validation_error", "Group name must be between 1 and 255 characters")
		return
	}

	// Validate access_tier
	accessTier := models.AccessTier(req.AccessTier)
	switch accessTier {
	case models.AccessTierOpen, models.AccessTierResident, models.AccessTierMember, models.AccessTierTrusted, models.AccessTierAdminOnly:
		// valid
	default:
		writeError(w, http.StatusBadRequest, "validation_error", "Invalid access tier; must be one of: open, resident, member, trusted, admin_only")
		return
	}

	var description *string
	if req.Description != "" {
		description = &req.Description
	}

	signalGroup := &models.SignalGroup{
		OwnerGroupID: &groupID,
		GroupName:    req.GroupName,
		Description:  description,
		AccessTier:   accessTier,
		CreatedBy:    &claims.UserID,
	}

	if err := h.signalGroupRepo.CreateForOwnerGroup(r.Context(), signalGroup); err != nil {
		writeServerError(w, r, err, "Failed to create signal group", "group", "create_signal_group")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupSignalGroupCreated, &resourceType, &groupID, map[string]interface{}{
			"signal_group_id":   signalGroup.ID,
			"signal_group_name": signalGroup.GroupName,
			"access_tier":       string(signalGroup.AccessTier),
		}), "group_signal_group_created")
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":           signalGroup.ID,
		"group_name":   signalGroup.GroupName,
		"access_tier":  string(signalGroup.AccessTier),
		"owner_group_id": groupID,
		"created_at":   signalGroup.CreatedAt,
	})
}

// ListSignalGroups handles GET /api/v1/groups/:id/signal-groups
func (h *GroupHandler) ListSignalGroups(w http.ResponseWriter, r *http.Request) {
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

	// Get all signal groups for this owner group
	signalGroups, err := h.signalGroupRepo.ListByOwnerGroup(r.Context(), groupID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list signal groups", "group", "list_signal_groups")
		return
	}

	// Get user's membership info for access tier filtering
	memberInfo, err := h.groupRepo.GetMember(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get membership info", "group", "list_signal_groups")
		return
	}

	// Determine if user is a verified resident in any of the group's regions
	isVerifiedResident := false
	regions, err := h.groupRepo.GetRegions(r.Context(), groupID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get group regions", "group", "list_signal_groups")
		return
	}
	for _, region := range regions {
		inRegion, regionErr := h.regionRepo.IsUserInRegion(r.Context(), claims.UserID, region.ID)
		if regionErr != nil {
			writeServerError(w, r, regionErr, "Failed to check region membership", "group", "list_signal_groups")
			return
		}
		if inRegion {
			isVerifiedResident = true
			break
		}
	}

	// Filter signal groups by access tier
	var filteredGroups []models.SignalGroupPublic
	for _, sg := range signalGroups {
		if claims.IsSuperuser || database.UserMeetsAccessTier(sg.AccessTier, true, isVerifiedResident, memberInfo) {
			filteredGroups = append(filteredGroups, models.SignalGroupPublic{
				ID:                 sg.ID,
				OwnerGroupID:      sg.OwnerGroupID,
				Name:               sg.GroupName,
				Description:        sg.Description,
				AccessTier:         string(sg.AccessTier),
				CreatedAt:          sg.CreatedAt,
				HasPendingDeletion: sg.HasPendingDeletion,
			})
		}
	}

	if filteredGroups == nil {
		filteredGroups = []models.SignalGroupPublic{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"signal_groups": filteredGroups,
	})
}

// CreateResource handles POST /api/v1/groups/:id/resources
func (h *GroupHandler) CreateResource(w http.ResponseWriter, r *http.Request) {
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

	// Check group exists
	_, err := h.groupRepo.GetByID(r.Context(), groupID)
	if errors.Is(err, database.ErrGroupNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Group not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get group", "group", "create_resource")
		return
	}

	// Check user is admin
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check permissions", "group", "create_resource")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group")
		return
	}

	var req models.CreateResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate title
	if req.Title == "" || len(req.Title) > 255 {
		writeError(w, http.StatusBadRequest, "validation_error", "Title must be between 1 and 255 characters")
		return
	}

	// Validate URL
	if req.URL == "" || len(req.URL) > 2048 {
		writeError(w, http.StatusBadRequest, "validation_error", "URL must be between 1 and 2048 characters")
		return
	}

	// Validate description length
	if len(req.Description) > 500 {
		writeError(w, http.StatusBadRequest, "validation_error", "Description must be at most 500 characters")
		return
	}

	// Validate access tier
	accessTier := models.AccessTier(req.AccessTier)
	switch accessTier {
	case models.AccessTierOpen, models.AccessTierResident, models.AccessTierMember, models.AccessTierTrusted, models.AccessTierAdminOnly:
		// valid
	default:
		writeError(w, http.StatusBadRequest, "validation_error", "Invalid access tier; must be one of: open, resident, member, trusted, admin_only")
		return
	}

	resource, err := h.groupRepo.CreateResource(r.Context(), groupID, claims.UserID, &req)
	if err != nil {
		writeServerError(w, r, err, "Failed to create resource", "group", "create_resource")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupResourceCreated, &resourceType, &groupID, map[string]interface{}{
			"resource_id":   resource.ID,
			"resource_title": resource.Title,
			"access_tier":   string(resource.AccessTier),
		}), "group_resource_created")
	}

	writeJSON(w, http.StatusCreated, resource)
}

// ListResources handles GET /api/v1/groups/:id/resources
func (h *GroupHandler) ListResources(w http.ResponseWriter, r *http.Request) {
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

	// Get all resources for this group
	allResources, err := h.groupRepo.ListResources(r.Context(), groupID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list resources", "group", "list_resources")
		return
	}

	// Get user's membership info for access tier filtering
	memberInfo, err := h.groupRepo.GetMember(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get membership info", "group", "list_resources")
		return
	}

	// Determine if user is a verified resident in any of the group's regions
	isVerifiedResident := false
	regions, err := h.groupRepo.GetRegions(r.Context(), groupID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get group regions", "group", "list_resources")
		return
	}
	for _, region := range regions {
		inRegion, regionErr := h.regionRepo.IsUserInRegion(r.Context(), claims.UserID, region.ID)
		if regionErr != nil {
			writeServerError(w, r, regionErr, "Failed to check region membership", "group", "list_resources")
			return
		}
		if inRegion {
			isVerifiedResident = true
			break
		}
	}

	// Filter resources by access tier
	var filteredResources []models.GroupResource
	for _, resource := range allResources {
		if claims.IsSuperuser || database.UserMeetsAccessTier(resource.AccessTier, true, isVerifiedResident, memberInfo) {
			filteredResources = append(filteredResources, resource)
		}
	}

	if filteredResources == nil {
		filteredResources = []models.GroupResource{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"resources": filteredResources,
	})
}

// UpdateResource handles PUT /api/v1/groups/:id/resources/:rid
func (h *GroupHandler) UpdateResource(w http.ResponseWriter, r *http.Request) {
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

	resourceID := getPathParam(r, "rid")
	if resourceID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Resource ID required")
		return
	}

	// Check user is admin
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check permissions", "group", "update_resource")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group")
		return
	}

	// Verify the resource belongs to this group
	existingResource, err := h.groupRepo.GetResource(r.Context(), resourceID)
	if errors.Is(err, database.ErrResourceNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Resource not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get resource", "group", "update_resource")
		return
	}
	if existingResource.GroupID != groupID {
		writeError(w, http.StatusNotFound, "not_found", "Resource not found")
		return
	}

	var req models.UpdateResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate fields if present
	if req.Title != nil && (*req.Title == "" || len(*req.Title) > 255) {
		writeError(w, http.StatusBadRequest, "validation_error", "Title must be between 1 and 255 characters")
		return
	}
	if req.URL != nil && (*req.URL == "" || len(*req.URL) > 2048) {
		writeError(w, http.StatusBadRequest, "validation_error", "URL must be between 1 and 2048 characters")
		return
	}
	if req.Description != nil && len(*req.Description) > 500 {
		writeError(w, http.StatusBadRequest, "validation_error", "Description must be at most 500 characters")
		return
	}
	if req.AccessTier != nil {
		tier := models.AccessTier(*req.AccessTier)
		switch tier {
		case models.AccessTierOpen, models.AccessTierResident, models.AccessTierMember, models.AccessTierTrusted, models.AccessTierAdminOnly:
			// valid
		default:
			writeError(w, http.StatusBadRequest, "validation_error", "Invalid access tier; must be one of: open, resident, member, trusted, admin_only")
			return
		}
	}

	if err := h.groupRepo.UpdateResource(r.Context(), resourceID, &req); err != nil {
		writeServerError(w, r, err, "Failed to update resource", "group", "update_resource")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupResourceUpdated, &resourceType, &groupID, map[string]interface{}{
			"resource_id": resourceID,
		}), "group_resource_updated")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Resource updated",
	})
}

// DeleteResource handles DELETE /api/v1/groups/:id/resources/:rid
func (h *GroupHandler) DeleteResource(w http.ResponseWriter, r *http.Request) {
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

	resourceID := getPathParam(r, "rid")
	if resourceID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Resource ID required")
		return
	}

	// Check user is admin
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check permissions", "group", "delete_resource")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group")
		return
	}

	// Verify the resource belongs to this group
	existingResource, err := h.groupRepo.GetResource(r.Context(), resourceID)
	if errors.Is(err, database.ErrResourceNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Resource not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get resource", "group", "delete_resource")
		return
	}
	if existingResource.GroupID != groupID {
		writeError(w, http.StatusNotFound, "not_found", "Resource not found")
		return
	}

	if err := h.groupRepo.DeleteResource(r.Context(), resourceID); err != nil {
		writeServerError(w, r, err, "Failed to delete resource", "group", "delete_resource")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupResourceDeleted, &resourceType, &groupID, map[string]interface{}{
			"resource_id": resourceID,
		}), "group_resource_deleted")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Resource deleted",
	})
}

// BlockGroup handles POST /api/v1/groups/{id}/blocks
func (h *GroupHandler) BlockGroup(w http.ResponseWriter, r *http.Request) {
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
		writeServerError(w, r, err, "Failed to check permissions", "group", "block_group")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group to block other groups")
		return
	}

	var req models.BlockGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.GroupID == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "group_id is required")
		return
	}

	err = h.groupRepo.BlockGroup(r.Context(), groupID, req.GroupID)
	if errors.Is(err, database.ErrCannotBlockSelf) {
		writeError(w, http.StatusBadRequest, "cannot_block_self", "Cannot block your own group")
		return
	}
	if errors.Is(err, database.ErrGroupAlreadyBlocked) {
		writeError(w, http.StatusConflict, "already_blocked", "Group is already blocked")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to block group", "group", "block_group")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupBlocked, &resourceType, &groupID, map[string]interface{}{
			"blocked_group_id": req.GroupID,
		}), "group_blocked")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Group blocked",
	})
}

// UnblockGroup handles DELETE /api/v1/groups/{id}/blocks/{gid}
func (h *GroupHandler) UnblockGroup(w http.ResponseWriter, r *http.Request) {
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

	blockedGroupID := getPathParam(r, "gid")
	if blockedGroupID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Blocked group ID required")
		return
	}

	// Check admin permission
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check permissions", "group", "unblock_group")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group to unblock groups")
		return
	}

	err = h.groupRepo.UnblockGroup(r.Context(), groupID, blockedGroupID)
	if errors.Is(err, database.ErrGroupNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Block not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to unblock group", "group", "unblock_group")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupUnblocked, &resourceType, &groupID, map[string]interface{}{
			"unblocked_group_id": blockedGroupID,
		}), "group_unblocked")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Group unblocked",
	})
}

// ListBlockedGroups handles GET /api/v1/groups/{id}/blocks
func (h *GroupHandler) ListBlockedGroups(w http.ResponseWriter, r *http.Request) {
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
		writeServerError(w, r, err, "Failed to check permissions", "group", "list_blocked_groups")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group to view blocked groups")
		return
	}

	groups, err := h.groupRepo.ListBlockedGroups(r.Context(), groupID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list blocked groups", "group", "list_blocked_groups")
		return
	}

	if groups == nil {
		groups = []models.Group{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"blocked_groups": groups,
	})
}

// =============================================================================
// Topic Board Handlers
// =============================================================================

// CreateOrUpdatePosting handles POST /api/v1/groups/{id}/topic-board
func (h *GroupHandler) CreateOrUpdatePosting(w http.ResponseWriter, r *http.Request) {
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
		writeServerError(w, r, err, "Failed to check permissions", "group", "create_posting")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group to post to the topic board")
		return
	}

	var req models.CreateTopicBoardPostingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate description
	if len(req.Description) < 10 || len(req.Description) > 500 {
		writeError(w, http.StatusBadRequest, "validation_error", "Description must be between 10 and 500 characters")
		return
	}

	// Validate tags
	if len(req.Tags) == 0 || len(req.Tags) > 5 {
		writeError(w, http.StatusBadRequest, "validation_error", "Must provide between 1 and 5 tags")
		return
	}
	for _, tag := range req.Tags {
		if len(tag) < 2 || len(tag) > 100 {
			writeError(w, http.StatusBadRequest, "validation_error", "Each tag must be between 2 and 100 characters")
			return
		}
	}

	// Validate region label if provided
	if req.RegionLabel != nil && len(*req.RegionLabel) > 255 {
		writeError(w, http.StatusBadRequest, "validation_error", "Region label must be at most 255 characters")
		return
	}

	posting, err := h.groupRepo.CreateOrUpdatePosting(r.Context(), groupID, &req)
	if err != nil {
		writeServerError(w, r, err, "Failed to create topic board posting", "group", "create_posting")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionTopicBoardPosted, &resourceType, &groupID, map[string]interface{}{
			"posting_id": posting.ID,
			"tags":       req.Tags,
		}), "topic_board_posted")
	}

	writeJSON(w, http.StatusOK, posting)
}

// GetPosting handles GET /api/v1/groups/{id}/topic-board
func (h *GroupHandler) GetPosting(w http.ResponseWriter, r *http.Request) {
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
		writeServerError(w, r, err, "Failed to check permissions", "group", "get_posting")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group to view topic board postings")
		return
	}

	posting, err := h.groupRepo.GetPosting(r.Context(), groupID)
	if err != nil {
		if errors.Is(err, database.ErrTopicBoardPostingNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "No topic board posting found for this group")
			return
		}
		writeServerError(w, r, err, "Failed to get topic board posting", "group", "get_posting")
		return
	}

	writeJSON(w, http.StatusOK, posting)
}

// RemovePosting handles DELETE /api/v1/groups/{id}/topic-board
func (h *GroupHandler) RemovePosting(w http.ResponseWriter, r *http.Request) {
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
		writeServerError(w, r, err, "Failed to check permissions", "group", "remove_posting")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group to remove topic board postings")
		return
	}

	err = h.groupRepo.RemovePosting(r.Context(), groupID)
	if err != nil {
		if errors.Is(err, database.ErrTopicBoardPostingNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "No topic board posting found for this group")
			return
		}
		writeServerError(w, r, err, "Failed to remove topic board posting", "group", "remove_posting")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionTopicBoardRemoved, &resourceType, &groupID, nil), "topic_board_removed")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Topic board posting removed",
	})
}

// BrowsePostings handles GET /api/v1/topic-board
func (h *GroupHandler) BrowsePostings(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	topicTag := r.URL.Query().Get("tag")
	if topicTag == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "tag query parameter is required")
		return
	}

	browsingGroupID := r.URL.Query().Get("group_id")
	if browsingGroupID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "group_id query parameter is required")
		return
	}

	// Verify the user is an admin of the browsing group
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), browsingGroupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check permissions", "group", "browse_postings")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of the specified group to browse the topic board")
		return
	}

	// Parse pagination
	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	postings, err := h.groupRepo.BrowsePostings(r.Context(), topicTag, browsingGroupID, limit, offset)
	if err != nil {
		writeServerError(w, r, err, "Failed to browse topic board", "group", "browse_postings")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"postings": postings,
	})
}
