package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// Rate limit constants for group operations.
const (
	inviteLinkCreateLimit  = 10
	inviteLinkCreateWindow = 1 * time.Hour
	joinViaLinkLimit       = 20
	joinViaLinkWindow      = 1 * time.Hour
)

// GroupHandler handles group endpoints.
type GroupHandler struct {
	db                    *database.DB
	groupRepo            *database.GroupRepository
	signalGroupRepo      *database.SignalGroupRepository
	meshtasticChannelRepo *database.MeshtasticChannelRepository
	regionRepo           *database.RegionRepository
	userRepo             *database.UserRepository
	auditRepo            *database.AuditRepository
	rateLimiter          services.RateLimiter
}

// NewGroupHandler creates a new group handler.
func NewGroupHandler(
	db *database.DB,
	groupRepo *database.GroupRepository,
	signalGroupRepo *database.SignalGroupRepository,
	meshtasticChannelRepo *database.MeshtasticChannelRepository,
	regionRepo *database.RegionRepository,
	userRepo *database.UserRepository,
	auditRepo *database.AuditRepository,
) *GroupHandler {
	return &GroupHandler{
		db:                    db,
		groupRepo:            groupRepo,
		signalGroupRepo:      signalGroupRepo,
		meshtasticChannelRepo: meshtasticChannelRepo,
		regionRepo:           regionRepo,
		userRepo:             userRepo,
		auditRepo:            auditRepo,
	}
}

// SetRateLimiter sets the rate limiter for group-specific rate limiting.
func (h *GroupHandler) SetRateLimiter(limiter services.RateLimiter) {
	h.rateLimiter = limiter
}

// checkGroupRateLimit checks per-endpoint group rate limits.
// Returns true if allowed, false if rate-limited (429 already written).
func (h *GroupHandler) checkGroupRateLimit(w http.ResponseWriter, r *http.Request, action, scopeID string, limit int, window time.Duration) bool {
	if h.rateLimiter == nil {
		return true
	}

	key := fmt.Sprintf("group:%s:%s", action, scopeID)
	allowed, _, _, err := h.rateLimiter.Allow(r.Context(), key, limit, window)
	if err != nil {
		slog.WarnContext(r.Context(), "group rate limit check failed", "key", key, "error", err)
		return true
	}

	if !allowed {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests. Please try again later.")
		return false
	}
	return true
}

// verifySuperuser re-checks superuser status from the database and writes
// a 500 error if the check fails. Returns the superuser boolean and whether
// the caller should continue (false = error already written).
func (h *GroupHandler) verifySuperuser(w http.ResponseWriter, r *http.Request, userID string) (bool, bool) {
	isSuperuser, err := isSuperuserFromDB(r.Context(), h.userRepo, userID)
	if err != nil {
		writeServerError(w, r, err, "Failed to verify superuser status", "group", "verify_superuser")
		return false, false
	}
	return isSuperuser, true
}

// guardUnlistedGroup enforces the same visibility invariant as the Get handler
// for sub-resource list endpoints: an unlisted group must not disclose its
// existence or contents to non-member, non-superuser callers. Returns true if
// the caller may proceed; otherwise it writes the response and returns false.
func (h *GroupHandler) guardUnlistedGroup(w http.ResponseWriter, r *http.Request, groupID, userID string, isSuperuser bool, op string) bool {
	if isSuperuser {
		return true
	}
	group, err := h.groupRepo.GetByID(r.Context(), groupID)
	if errors.Is(err, database.ErrGroupNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Group not found")
		return false
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get group", "group", op)
		return false
	}
	if group.Visibility != models.GroupVisibilityUnlisted {
		return true
	}
	isMember, err := h.groupRepo.IsUserMember(r.Context(), groupID, userID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check membership", "group", op)
		return false
	}
	if !isMember {
		writeError(w, http.StatusNotFound, "not_found", "Group not found")
		return false
	}
	return true
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
			inRegion, regionErr := h.regionRepo.IsUserVerifiedInRegion(r.Context(), claims.UserID, regionID)
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

	if !claims.PostcardVerified {
		writeError(w, http.StatusForbidden, "verification_required", "Address verification required to create groups")
		return
	}

	var req models.CreateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
	}

	// Validate at least one region
	if len(req.RegionIDs) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "At least one region_id is required")
		return
	}

	// Verify user is a *verified* member of each region (pending memberships,
	// created the moment someone types an address, must not qualify).
	for _, regionID := range req.RegionIDs {
		isMember, err := h.regionRepo.IsUserVerifiedInRegion(r.Context(), claims.UserID, regionID)
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

	// Filter signal groups by access tier and clear plaintext_invite_link for non-open tiers
	isSuperuser, err := isSuperuserFromDB(r.Context(), h.userRepo, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to verify superuser status", "group", "get_group")
		return
	}

	memberInfo, err := h.groupRepo.GetMember(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get membership info", "group", "get_group")
		return
	}

	isVerifiedResident := false
	for _, region := range groupDetails.Regions {
		inRegion, regionErr := h.regionRepo.IsUserVerifiedInRegion(r.Context(), claims.UserID, region.ID)
		if regionErr != nil {
			writeServerError(w, r, regionErr, "Failed to check region membership", "group", "get_group")
			return
		}
		if inRegion {
			isVerifiedResident = true
			break
		}
	}

	var filteredSignalGroups []models.SignalGroupPublic
	for _, sg := range groupDetails.SignalGroups {
		if isSuperuser || database.UserMeetsAccessTier(models.AccessTier(sg.AccessTier), true, isVerifiedResident, memberInfo) {
			// Clear plaintext_invite_link for non-open tiers
			if sg.AccessTier != "open" {
				sg.PlaintextInviteLink = nil
			}
			filteredSignalGroups = append(filteredSignalGroups, sg)
		}
	}
	groupDetails.SignalGroups = filteredSignalGroups

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
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isAdmin && !isSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group to update it")
		return
	}

	var req models.UpdateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
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

	// Re-check superuser status from DB (defense-in-depth against stale JWT claims)
	callerUser, err := h.userRepo.GetByID(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to verify permissions", "group", "delete_group")
		return
	}
	if !callerUser.IsSuperuser {
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

	// Re-check superuser from DB
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}

	// Check membership (superusers bypass)
	if !isSuperuser {
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

	// Hide address_verified field if the group setting is off and caller is not an admin
	group, err := h.groupRepo.GetByID(r.Context(), groupID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get group", "group", "list_members")
		return
	}
	if !group.ShowAddressVerification {
		isAdmin, _ := h.groupRepo.IsUserAdmin(r.Context(), groupID, claims.UserID)
		if !isAdmin && !isSuperuser {
			for i := range members {
				members[i].AddressVerified = false
			}
		}
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

// BlockMember handles POST /api/v1/groups/:id/blocked-users — bans a user from the
// group (admin only) and removes any current membership, so they cannot rejoin via
// link, invitation, or vouch.
func (h *GroupHandler) BlockMember(w http.ResponseWriter, r *http.Request) {
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

	if !h.requireGroupAdmin(w, r, groupID, claims.UserID, "block_member") {
		return
	}

	var req struct {
		UserID string `json:"user_id" validate:"required"`
		Reason string `json:"reason" validate:"max=500"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}
	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
	}
	if req.UserID == claims.UserID {
		writeError(w, http.StatusBadRequest, "invalid_request", "You cannot block yourself")
		return
	}

	var reason *string
	if req.Reason != "" {
		reason = &req.Reason
	}
	// Ban + remove in one transaction so a ban can't leave the user
	// blocked-but-still-a-member if the removal fails.
	if err := h.groupRepo.BlockAndRemoveMember(r.Context(), groupID, req.UserID, &claims.UserID, reason); err != nil {
		writeServerError(w, r, err, "Failed to block user", "group", "block_member")
		return
	}

	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionGroupMemberRemoved, &resourceType, &groupID, map[string]interface{}{
			"user_id": req.UserID,
			"action":  "block",
		}), "group_member_blocked")
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "User blocked from group"})
}

// UnblockMember handles DELETE /api/v1/groups/:id/blocked-users/:userID — lifts a
// user ban (admin only).
func (h *GroupHandler) UnblockMember(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	groupID := getPathParam(r, "id")
	targetUserID := getPathParam(r, "user_id")
	if groupID == "" || targetUserID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Group ID and user ID required")
		return
	}

	if !h.requireGroupAdmin(w, r, groupID, claims.UserID, "unblock_member") {
		return
	}

	if err := h.groupRepo.UnblockUser(r.Context(), groupID, targetUserID); err != nil {
		writeServerError(w, r, err, "Failed to unblock user", "group", "unblock_member")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "User unblocked"})
}

// requireGroupAdmin verifies the caller is an admin of the group or a superuser.
// Writes the appropriate error and returns false if not.
func (h *GroupHandler) requireGroupAdmin(w http.ResponseWriter, r *http.Request, groupID, userID, op string) bool {
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), groupID, userID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check admin status", "group", op)
		return false
	}
	if isAdmin {
		return true
	}
	isSuperuser, ok := h.verifySuperuser(w, r, userID)
	if !ok {
		return false
	}
	if !isSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be a group admin")
		return false
	}
	return true
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

	// Rate limit: 10 invite links per group per hour
	if !h.checkGroupRateLimit(w, r, "create_invite_link", groupID, inviteLinkCreateLimit, inviteLinkCreateWindow) {
		return
	}

	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check permissions", "group", "create_invite_link")
		return
	}
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isAdmin && !isSuperuser {
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

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
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
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isAdmin && !isSuperuser {
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

	// Rate limit: 20 join attempts per user per hour
	if !h.checkGroupRateLimit(w, r, "join_via_link", claims.UserID, joinViaLinkLimit, joinViaLinkWindow) {
		return
	}

	token := getPathParam(r, "token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Invite token required")
		return
	}

	// The entire join — validate/lock the link, reject blocked/already-member
	// users, insert the membership, and increment use_count — happens in one
	// transaction. A failure at any step (including a duplicate-member race on the
	// insert) rolls back, so a failed join never burns a link use.
	link, err := h.groupRepo.JoinViaInviteLink(r.Context(), token, claims.UserID)
	if err != nil {
		switch {
		case errors.Is(err, database.ErrInviteLinkNotFound), errors.Is(err, database.ErrGroupNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Invite link not found")
		case errors.Is(err, database.ErrInviteLinkExpired):
			writeError(w, http.StatusGone, "expired", "Invite link has expired")
		case errors.Is(err, database.ErrInviteLinkExhausted):
			writeError(w, http.StatusGone, "exhausted", "Invite link has reached its maximum uses")
		case errors.Is(err, database.ErrUserBlockedFromGroup):
			writeError(w, http.StatusForbidden, "blocked", "You cannot join this group")
		case errors.Is(err, database.ErrGroupAlreadyMember):
			writeError(w, http.StatusConflict, "already_member", "You are already a member of this group")
		default:
			writeServerError(w, r, err, "Failed to join group", "group", "join_via_link")
		}
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

	// Any member can invite others (invitees still need to be accepted through the normal process)
	isMember, err := h.groupRepo.IsUserMember(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check permissions", "group", "create_invitation")
		return
	}
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isMember && !isSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be a member of this group")
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

	// Refuse to invite a banned user.
	if blocked, blockErr := h.groupRepo.IsUserBlockedFromGroup(r.Context(), groupID, req.UserID); blockErr != nil {
		writeServerError(w, r, blockErr, "Failed to check block status", "group", "create_invitation")
		return
	} else if blocked {
		writeError(w, http.StatusForbidden, "blocked", "This user cannot be invited to the group")
		return
	}

	// Check if target user is already a member
	targetIsMember, err := h.groupRepo.IsUserMember(r.Context(), groupID, req.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check membership", "group", "create_invitation")
		return
	}
	if targetIsMember {
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

	// Refuse to vouch for a banned user (no path to elevate a blocked member).
	if blocked, blockErr := h.groupRepo.IsUserBlockedFromGroup(r.Context(), groupID, req.UserID); blockErr != nil {
		writeServerError(w, r, blockErr, "Failed to check block status", "group", "trust_vouch")
		return
	} else if blocked {
		writeError(w, http.StatusForbidden, "blocked", "This user cannot be vouched for")
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
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isSuperuser {
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

	// Decline: simple status update, no membership change.
	if !req.Accept {
		if _, err := h.groupRepo.DeclineInvitation(r.Context(), invitationID, claims.UserID); err != nil {
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
		writeJSON(w, http.StatusOK, map[string]string{
			"message": "Invitation declined",
		})
		return
	}

	// Accept: validate/lock the invitation, run the gating checks (banned invitee,
	// departed inviter), mark it accepted, and insert the membership — all in one
	// transaction. A failure (including a duplicate-member race) rolls back, so a
	// rejected or failed accept never consumes the invitation.
	invitation, err := h.groupRepo.AcceptInvitation(r.Context(), invitationID, claims.UserID)
	if err != nil {
		switch {
		case errors.Is(err, database.ErrInvitationNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Invitation not found")
		case errors.Is(err, database.ErrInvitationExpired):
			writeError(w, http.StatusGone, "expired", "Invitation has expired")
		case errors.Is(err, database.ErrUserBlockedFromGroup):
			writeError(w, http.StatusForbidden, "blocked", "You cannot join this group")
		case errors.Is(err, database.ErrInviterLeft):
			writeError(w, http.StatusConflict, "inviter_left", "The member who invited you is no longer in this group")
		case errors.Is(err, database.ErrGroupAlreadyMember):
			writeError(w, http.StatusConflict, "already_member", "You are already a member of this group")
		default:
			writeServerError(w, r, err, "Failed to accept invitation", "group", "respond_to_invitation")
		}
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
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isAdmin && !isSuperuser {
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

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
	}

	accessTier := models.AccessTier(req.AccessTier)

	if req.InviteLink != nil && accessTier != models.AccessTierOpen {
		writeError(w, http.StatusBadRequest, "validation_error", "invite_link is only allowed for open-tier signal groups")
		return
	}

	// Validate encrypted bundle for restricted tiers
	if accessTier != models.AccessTierOpen {
		// For restricted tiers, encryption fields are required
		if req.EncryptedPayload == nil || *req.EncryptedPayload == "" {
			writeError(w, http.StatusBadRequest, "validation_error", "encrypted_payload is required for restricted-tier signal groups")
			return
		}
		if req.EncryptionIV == nil || *req.EncryptionIV == "" {
			writeError(w, http.StatusBadRequest, "validation_error", "encryption_iv is required for restricted-tier signal groups")
			return
		}
		if len(req.WrappedKeys) == 0 {
			writeError(w, http.StatusBadRequest, "validation_error", "wrapped_keys is required for restricted-tier signal groups")
			return
		}
	} else if req.EncryptedPayload != nil || req.EncryptionIV != nil || len(req.WrappedKeys) > 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "encrypted bundle is not allowed for open-tier signal groups")
		return
	}

	var description *string
	if req.Description != "" {
		description = &req.Description
	}

	signalGroup := &models.SignalGroup{
		OwnerGroupID:        &groupID,
		GroupName:           req.GroupName,
		Description:         description,
		AccessTier:          accessTier,
		CreatedBy:           &claims.UserID,
		PlaintextInviteLink: req.InviteLink,
	}

	if err := h.signalGroupRepo.CreateForOwnerGroup(r.Context(), signalGroup); err != nil {
		writeServerError(w, r, err, "Failed to create signal group", "group", "create_signal_group")
		return
	}

	// Create encrypted secret for restricted-tier groups with a bundle
	if accessTier != models.AccessTierOpen &&
		req.EncryptedPayload != nil && *req.EncryptedPayload != "" &&
		req.EncryptionIV != nil && *req.EncryptionIV != "" &&
		len(req.WrappedKeys) > 0 {
		secretRepo := database.NewEncryptedSecretRepository(h.db)
		secret := &models.EncryptedSecret{
			SecretType:       models.SecretTypeSignalInvite,
			SignalGroupID:    &signalGroup.ID,
			EncryptedPayload: *req.EncryptedPayload,
			EncryptionIV:     *req.EncryptionIV,
			UpdatedBy:        claims.UserID,
		}
		if createErr := secretRepo.Create(r.Context(), secret, req.WrappedKeys); createErr != nil {
			writeServerError(w, r, createErr, "Failed to create encrypted secret", "group", "create_signal_group")
			return
		}
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
		"id":                   signalGroup.ID,
		"group_name":           signalGroup.GroupName,
		"access_tier":          string(signalGroup.AccessTier),
		"owner_group_id":       groupID,
		"plaintext_invite_link": signalGroup.PlaintextInviteLink,
		"created_at":           signalGroup.CreatedAt,
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

	// Re-check superuser from DB
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}

	// Don't disclose unlisted groups (or their contents) to non-members
	if !h.guardUnlistedGroup(w, r, groupID, claims.UserID, isSuperuser, "list_signal_groups") {
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
		inRegion, regionErr := h.regionRepo.IsUserVerifiedInRegion(r.Context(), claims.UserID, region.ID)
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
		if isSuperuser || database.UserMeetsAccessTier(sg.AccessTier, true, isVerifiedResident, memberInfo) {
			publicGroup := models.SignalGroupPublic{
				ID:                 sg.ID,
				OwnerGroupID:      sg.OwnerGroupID,
				Name:               sg.GroupName,
				Description:        sg.Description,
				AccessTier:         string(sg.AccessTier),
				CreatedAt:          sg.CreatedAt,
				HasPendingDeletion: sg.HasPendingDeletion,
			}
			// Include plaintext_invite_link for open-tier chats
			if sg.AccessTier == models.AccessTierOpen {
				publicGroup.PlaintextInviteLink = sg.PlaintextInviteLink
			}
			filteredGroups = append(filteredGroups, publicGroup)
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

	// Check user is a member (any member can propose resources)
	isMember, err := h.groupRepo.IsUserMember(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check permissions", "group", "create_resource")
		return
	}
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isMember && !isSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be a member of this group")
		return
	}

	var req models.CreateResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
	}

	if !isValidHTTPURL(req.URL) {
		writeError(w, http.StatusBadRequest, "validation_error", "URL must be a valid http or https URL")
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

	// Re-check superuser from DB
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}

	// Don't disclose unlisted groups (or their contents) to non-members
	if !h.guardUnlistedGroup(w, r, groupID, claims.UserID, isSuperuser, "list_resources") {
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
		inRegion, regionErr := h.regionRepo.IsUserVerifiedInRegion(r.Context(), claims.UserID, region.ID)
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
		if isSuperuser || database.UserMeetsAccessTier(resource.AccessTier, true, isVerifiedResident, memberInfo) {
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
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isAdmin && !isSuperuser {
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

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
	}

	if req.URL != nil && !isValidHTTPURL(*req.URL) {
		writeError(w, http.StatusBadRequest, "validation_error", "URL must be a valid http or https URL")
		return
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
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isAdmin && !isSuperuser {
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
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isAdmin && !isSuperuser {
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
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isAdmin && !isSuperuser {
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
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isAdmin && !isSuperuser {
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
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isAdmin && !isSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group to post to the topic board")
		return
	}

	var req models.CreateTopicBoardPostingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
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
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isAdmin && !isSuperuser {
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
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isAdmin && !isSuperuser {
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
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isAdmin && !isSuperuser {
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

const maxMeshtasticChannelsPerOwnerGroup = 5

// CreateMeshtasticChannel handles POST /api/v1/groups/:id/meshtastic-channels
func (h *GroupHandler) CreateMeshtasticChannel(w http.ResponseWriter, r *http.Request) {
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
		writeServerError(w, r, err, "Failed to get group", "group", "create_meshtastic_channel")
		return
	}
	if group.Status == models.GroupStatusProvisional {
		writeError(w, http.StatusBadRequest, "group_provisional", "Cannot create meshtastic channels for a provisional group")
		return
	}

	// Check user is admin of this group
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check permissions", "group", "create_meshtastic_channel")
		return
	}
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}
	if !isAdmin && !isSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this group")
		return
	}

	// Check meshtastic channel limit
	count, err := h.meshtasticChannelRepo.CountByOwnerGroup(r.Context(), groupID)
	if err != nil {
		writeServerError(w, r, err, "Failed to count meshtastic channels", "group", "create_meshtastic_channel")
		return
	}
	if count >= maxMeshtasticChannelsPerOwnerGroup {
		writeError(w, http.StatusBadRequest, "limit_reached", "Maximum number of meshtastic channels reached for this group")
		return
	}

	var req models.CreateGroupMeshtasticChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
	}

	accessTier := models.AccessTier(req.AccessTier)

	var description *string
	if req.Description != "" {
		description = &req.Description
	}

	meshtasticChannel := &models.MeshtasticChannel{
		OwnerGroupID: &groupID,
		ChannelName:  req.ChannelName,
		Description:  description,
		AccessTier:   accessTier,
		CreatedBy:    &claims.UserID,
	}

	if err := h.meshtasticChannelRepo.CreateForOwnerGroup(r.Context(), meshtasticChannel); err != nil {
		writeServerError(w, r, err, "Failed to create meshtastic channel", "group", "create_meshtastic_channel")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionMeshtasticChannelCreated, &resourceType, &groupID, map[string]interface{}{
			"meshtastic_channel_id":   meshtasticChannel.ID,
			"meshtastic_channel_name": meshtasticChannel.ChannelName,
			"access_tier":             string(meshtasticChannel.AccessTier),
		}), "group_meshtastic_channel_created")
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":             meshtasticChannel.ID,
		"channel_name":   meshtasticChannel.ChannelName,
		"access_tier":    string(meshtasticChannel.AccessTier),
		"owner_group_id": groupID,
		"created_at":     meshtasticChannel.CreatedAt,
	})
}

// ListMeshtasticChannels handles GET /api/v1/groups/:id/meshtastic-channels
func (h *GroupHandler) ListMeshtasticChannels(w http.ResponseWriter, r *http.Request) {
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

	// Re-check superuser from DB
	isSuperuser, ok := h.verifySuperuser(w, r, claims.UserID)
	if !ok {
		return
	}

	// Don't disclose unlisted groups (or their contents) to non-members
	if !h.guardUnlistedGroup(w, r, groupID, claims.UserID, isSuperuser, "list_meshtastic_channels") {
		return
	}

	// Get all meshtastic channels for this owner group
	meshtasticChannels, err := h.meshtasticChannelRepo.ListByOwnerGroup(r.Context(), groupID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list meshtastic channels", "group", "list_meshtastic_channels")
		return
	}

	// Get user's membership info for access tier filtering
	memberInfo, err := h.groupRepo.GetMember(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get membership info", "group", "list_meshtastic_channels")
		return
	}

	// Determine if user is a verified resident in any of the group's regions
	isVerifiedResident := false
	regions, err := h.groupRepo.GetRegions(r.Context(), groupID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get group regions", "group", "list_meshtastic_channels")
		return
	}
	for _, region := range regions {
		inRegion, regionErr := h.regionRepo.IsUserVerifiedInRegion(r.Context(), claims.UserID, region.ID)
		if regionErr != nil {
			writeServerError(w, r, regionErr, "Failed to check region membership", "group", "list_meshtastic_channels")
			return
		}
		if inRegion {
			isVerifiedResident = true
			break
		}
	}

	// Filter meshtastic channels by access tier
	var filteredChannels []models.MeshtasticChannelPublic
	for _, mc := range meshtasticChannels {
		if isSuperuser || database.UserMeetsAccessTier(mc.AccessTier, true, isVerifiedResident, memberInfo) {
			filteredChannels = append(filteredChannels, models.MeshtasticChannelPublic{
				ID:                 mc.ID,
				OwnerGroupID:       mc.OwnerGroupID,
				Name:               mc.ChannelName,
				Description:        mc.Description,
				AccessTier:         string(mc.AccessTier),
				CreatedAt:          mc.CreatedAt,
				HasPendingDeletion: mc.HasPendingDeletion,
			})
		}
	}

	if filteredChannels == nil {
		filteredChannels = []models.MeshtasticChannelPublic{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"meshtastic_channels": filteredChannels,
	})
}
