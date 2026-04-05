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

	if !claims.VouchVerified {
		writeError(w, http.StatusForbidden, "forbidden", "Vouch verification required to join groups")
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

	// Check user is verified in at least one of the group's regions
	regions, err := h.groupRepo.GetRegions(r.Context(), link.GroupID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get group regions", "group", "join_via_link")
		return
	}

	inRegion := false
	for _, region := range regions {
		isMemberOfRegion, err := h.regionRepo.IsUserInRegion(r.Context(), claims.UserID, region.ID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check region membership", "group", "join_via_link")
			return
		}
		if isMemberOfRegion {
			inRegion = true
			break
		}
	}

	if !inRegion {
		writeError(w, http.StatusForbidden, "forbidden", "You must be a verified member of at least one of the group's regions")
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
