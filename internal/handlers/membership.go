package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// MembershipHandler handles sub-region membership endpoints
type MembershipHandler struct {
	db                  *database.DB
	membershipRepo      *database.MembershipRepository
	regionRepo          *database.RegionRepository
	userRepo            *database.UserRepository
	auditRepo           *database.AuditRepository
	notificationService NotificationServiceInterface
}

// NewMembershipHandler creates a new membership handler
func NewMembershipHandler(
	db *database.DB,
	membershipRepo *database.MembershipRepository,
	regionRepo *database.RegionRepository,
	userRepo *database.UserRepository,
	auditRepo *database.AuditRepository,
) *MembershipHandler {
	return &MembershipHandler{
		db:             db,
		membershipRepo: membershipRepo,
		regionRepo:     regionRepo,
		userRepo:       userRepo,
		auditRepo:      auditRepo,
	}
}

// SetNotificationService sets the notification service for the handler.
// This is optional - if not set, notifications will not be sent.
func (h *MembershipHandler) SetNotificationService(svc NotificationServiceInterface) {
	h.notificationService = svc
}

// CreateRequest handles POST /api/v1/communities/:id/membership-requests
// Allows verified users to request membership in a sub-region
func (h *MembershipHandler) CreateRequest(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Must be at least vouch verified (TierVouched or higher)
	if claims.VerificationTier < models.TierVouched {
		writeError(w, http.StatusForbidden, "forbidden", "Verification required to request sub-region membership")
		return
	}

	regionID := getPathParam(r, "id")
	if regionID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Community ID required")
		return
	}

	// Get the target region
	region, err := h.regionRepo.GetByID(r.Context(), regionID)
	if errors.Is(err, database.ErrRegionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Community not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get region", "membership", "get_region")
		return
	}

	// Validate region is a sub-region (has parent)
	if region.ParentRegionID == nil {
		writeError(w, http.StatusBadRequest, "invalid_region", "Can only request membership for sub-regions (city blocks, neighborhoods)")
		return
	}

	// Check if user is already a member of the target region
	isMember, err := h.regionRepo.IsUserInRegion(r.Context(), claims.UserID, regionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check region membership", "membership", "check_region_membership")
		return
	}
	if isMember {
		writeError(w, http.StatusConflict, "already_member", "You are already a member of this region")
		return
	}

	// Check if user is a member of the parent region hierarchy
	isInParent, err := h.regionRepo.IsUserInRegion(r.Context(), claims.UserID, *region.ParentRegionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check parent region membership", "membership", "check_parent_membership")
		return
	}
	if !isInParent {
		writeError(w, http.StatusForbidden, "not_in_parent", "You must be a member of the parent region to request sub-region membership")
		return
	}

	// Check for existing pending request
	existingRequest, err := h.membershipRepo.GetPendingByUserAndRegion(r.Context(), claims.UserID, regionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check existing requests", "membership", "check_existing_requests")
		return
	}
	if existingRequest != nil {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":               "pending_request_exists",
			"message":             "You already have a pending request for this region",
			"existing_request_id": existingRequest.ID,
		})
		return
	}

	// Check rate limit: max pending requests per user
	pendingCount, err := h.membershipRepo.CountPendingByUser(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to count pending requests", "membership", "count_pending_requests")
		return
	}
	if pendingCount >= database.MaxPendingRequestsPerUser {
		writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"error":         "too_many_requests",
			"message":       "Maximum pending requests reached. Please wait for existing requests to be resolved.",
			"pending_count": pendingCount,
			"max_allowed":   database.MaxPendingRequestsPerUser,
		})
		return
	}

	// Create the request
	req := &models.SubRegionMembershipRequest{
		UserID:         claims.UserID,
		RegionID:       regionID,
		ParentRegionID: *region.ParentRegionID,
		RequestType:    models.MembershipRequestTypeRequest,
		InitiatedBy:    claims.UserID,
	}

	if err := h.membershipRepo.Create(r.Context(), req); err != nil {
		writeServerError(w, r, err, "Failed to create membership request", "membership", "create_request")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "membership_request"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSubRegionRequestCreated, &resourceType, &req.ID, map[string]interface{}{
			"region_id":        regionID,
			"parent_region_id": *region.ParentRegionID,
		}), "sub_region_request_created")
	}

	writeJSON(w, http.StatusCreated, models.MembershipRequestResponse{
		RequestID:    req.ID,
		UserID:       req.UserID,
		RegionID:     req.RegionID,
		RequestType:  string(req.RequestType),
		Status:       string(req.Status),
		VotesNeeded:  database.RequiredVotesForApproval,
		CurrentVotes: 0,
		ExpiresAt:    req.ExpiresAt,
		Message:      "Membership request created. Admins will review your request.",
	})
}

// ListUserRequests handles GET /api/v1/membership-requests
// Lists the current user's own membership requests
func (h *MembershipHandler) ListUserRequests(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Parse filter query params
	filter := models.MembershipRequestListFilter{
		Status:      r.URL.Query().Get("status"),
		RequestType: r.URL.Query().Get("type"),
	}

	requests, err := h.membershipRepo.ListUserRequests(r.Context(), claims.UserID, filter)
	if err != nil {
		writeServerError(w, r, err, "Failed to list membership requests", "membership", "list_user_requests")
		return
	}

	writeJSON(w, http.StatusOK, models.MembershipRequestListResponse{Requests: requests})
}

// CancelRequest handles DELETE /api/v1/membership-requests/:id
// Allows users to cancel their own pending requests
func (h *MembershipHandler) CancelRequest(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	requestID := getPathParam(r, "id")
	if requestID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Request ID required")
		return
	}

	// Get the request to verify ownership
	request, err := h.membershipRepo.GetByID(r.Context(), requestID)
	if errors.Is(err, database.ErrMembershipRequestNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Membership request not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get membership request", "membership", "get_request")
		return
	}

	// Only the request owner can cancel
	if request.UserID != claims.UserID {
		writeError(w, http.StatusForbidden, "forbidden", "You can only cancel your own requests")
		return
	}

	// Can only cancel pending requests
	if request.Status != models.MembershipRequestStatusPending {
		writeError(w, http.StatusConflict, "request_closed", "Only pending requests can be cancelled")
		return
	}

	// Cancel the request
	if err := h.membershipRepo.Cancel(r.Context(), requestID, claims.UserID); err != nil {
		writeServerError(w, r, err, "Failed to cancel request", "membership", "cancel_request")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"request_id": requestID,
		"status":     "cancelled",
		"message":    "Membership request cancelled",
	})
}

// ListPendingForRegion handles GET /api/v1/communities/:id/membership-requests
// Lists pending membership requests for a region (admin view)
func (h *MembershipHandler) ListPendingForRegion(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	regionID := getPathParam(r, "id")
	if regionID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Community ID required")
		return
	}

	// Check if user is admin for this region (or superuser)
	if !claims.IsSuperuser {
		isAdmin, err := h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, regionID)
		if err != nil {
			writeServerError(w, r, err, "Failed to verify admin status", "membership", "verify_admin_status")
			return
		}
		if !isAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "Admin access required for this region")
			return
		}
	}

	requests, err := h.membershipRepo.ListPendingByRegion(r.Context(), regionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list membership requests", "membership", "list_pending_by_region")
		return
	}

	writeJSON(w, http.StatusOK, models.MembershipRequestListResponse{Requests: requests})
}

// ListPendingForAdmin handles GET /api/v1/membership-requests/admin
// Lists all pending membership requests for regions where user is admin
func (h *MembershipHandler) ListPendingForAdmin(w http.ResponseWriter, r *http.Request) {
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

	requests, err := h.membershipRepo.ListPendingForAdminUser(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list membership requests", "membership", "list_pending_for_admin")
		return
	}

	writeJSON(w, http.StatusOK, models.MembershipRequestListResponse{Requests: requests})
}

// GetRequest handles GET /api/v1/membership-requests/:id
// Gets details of a specific membership request
func (h *MembershipHandler) GetRequest(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	requestID := getPathParam(r, "id")
	if requestID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Request ID required")
		return
	}

	// Get request with votes
	request, err := h.membershipRepo.GetByIDWithVotes(r.Context(), requestID)
	if errors.Is(err, database.ErrMembershipRequestNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Membership request not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get membership request", "membership", "get_request_with_votes")
		return
	}

	// Check access: owner can see their own request, admins can see requests for their regions
	isOwner := request.UserID == claims.UserID
	isAdmin := false

	if !isOwner && !claims.IsSuperuser {
		isAdmin, err = h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, request.RegionID)
		if err != nil {
			writeServerError(w, r, err, "Failed to verify admin status", "membership", "verify_admin_status")
			return
		}
		if !isAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "You don't have access to view this request")
			return
		}
	}

	// Calculate can_vote and has_voted
	request.HasVoted = false
	for _, vote := range request.Votes {
		if vote.VoterID == claims.UserID {
			request.HasVoted = true
			break
		}
	}

	// Determine if user is admin if not already checked
	if claims.IsSuperuser && !isAdmin {
		isAdmin, _ = h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, request.RegionID)
	}

	// Can vote if: is admin, hasn't voted, and request is pending
	request.CanVote = isAdmin && !request.HasVoted && request.Status == string(models.MembershipRequestStatusPending)

	writeJSON(w, http.StatusOK, request)
}

// VoteOnRequest handles POST /api/v1/membership-requests/:id/vote
// Allows admins to vote on membership requests
func (h *MembershipHandler) VoteOnRequest(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	requestID := getPathParam(r, "id")
	if requestID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Request ID required")
		return
	}

	var voteReq models.VoteMembershipRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&voteReq); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Pre-transaction: get request for admin check
	request, err := h.membershipRepo.GetByID(r.Context(), requestID)
	if errors.Is(err, database.ErrMembershipRequestNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Membership request not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get membership request", "membership", "get_request")
		return
	}

	// Check if user is admin for this region (outside tx)
	isAdmin, err := h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, request.RegionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to verify admin status", "membership", "verify_admin_status")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "not_region_admin", "You must be an admin of this region to vote")
		return
	}

	// Transaction: lock request → check vote → add vote → count → maybe add user to region
	var voteCount int
	var consensusReached bool
	var isUserAdmin bool

	if h.db != nil {
		err = h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			lockedRequest, txErr := h.membershipRepo.GetByIDForUpdate(r.Context(), tx, requestID)
			if txErr != nil {
				return txErr
			}
			if lockedRequest.Status != models.MembershipRequestStatusPending {
				return database.ErrProposalClosed
			}
			hasVoted, txErr := h.membershipRepo.HasVotedTx(r.Context(), tx, requestID, claims.UserID)
			if txErr != nil {
				return txErr
			}
			if hasVoted {
				return database.ErrAlreadyVoted
			}
			if txErr := h.membershipRepo.AddVoteTx(r.Context(), tx, requestID, claims.UserID, voteReq.Vote); txErr != nil {
				return txErr
			}
			voteCount, txErr = h.membershipRepo.CountApprovalVotesTx(r.Context(), tx, requestID)
			if txErr != nil {
				return txErr
			}
			if voteCount >= database.RequiredVotesForApproval {
				consensusReached = true
				user, txErr := h.userRepo.GetByIDForUpdate(r.Context(), tx, lockedRequest.UserID)
				if txErr != nil {
					slog.Warn("failed to get user for admin check", "error", txErr)
				}
				if user != nil {
					isUserAdmin = user.PostcardVerified && user.VouchVerified
				}
				if txErr := h.regionRepo.AddUserToRegionTx(r.Context(), tx, lockedRequest.UserID, lockedRequest.RegionID, isUserAdmin); txErr != nil {
					return txErr
				}
				if txErr := h.membershipRepo.UpdateStatusTx(r.Context(), tx, requestID, models.MembershipRequestStatusApproved); txErr != nil {
					return txErr
				}
			}
			return nil
		})
	} else {
		if request.Status != models.MembershipRequestStatusPending {
			err = database.ErrProposalClosed
		} else {
			hasVoted, checkErr := h.membershipRepo.HasVoted(r.Context(), requestID, claims.UserID)
			if checkErr != nil {
				err = checkErr
			} else if hasVoted {
				err = database.ErrAlreadyVoted
			} else {
				if addErr := h.membershipRepo.AddVote(r.Context(), requestID, claims.UserID, voteReq.Vote); addErr != nil {
					err = addErr
				} else {
					voteCount, err = h.membershipRepo.CountApprovalVotes(r.Context(), requestID)
					if err == nil && voteCount >= database.RequiredVotesForApproval {
						consensusReached = true
						user, _ := h.userRepo.GetByID(r.Context(), request.UserID)
						if user != nil {
							isUserAdmin = user.PostcardVerified && user.VouchVerified
						}
						_ = h.regionRepo.AddUserToRegion(r.Context(), request.UserID, request.RegionID, isUserAdmin)
						_ = h.membershipRepo.UpdateStatus(r.Context(), requestID, models.MembershipRequestStatusApproved)
					}
				}
			}
		}
	}

	if errors.Is(err, database.ErrProposalClosed) {
		writeError(w, http.StatusConflict, "request_closed", "This request is no longer open for voting")
		return
	}
	if errors.Is(err, database.ErrAlreadyVoted) {
		writeError(w, http.StatusConflict, "already_voted", "You have already voted on this request")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to record vote", "membership", "record_vote")
		return
	}

	// Side effects outside transaction
	if h.auditRepo != nil {
		resourceType := "membership_request"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSubRegionRequestVoted, &resourceType, &requestID, map[string]interface{}{
			"vote":      voteReq.Vote,
			"region_id": request.RegionID,
		}), "sub_region_request_voted")
	}

	response := models.MembershipRequestResponse{
		RequestID:    requestID,
		UserID:       request.UserID,
		RegionID:     request.RegionID,
		RequestType:  string(request.RequestType),
		Status:       string(models.MembershipRequestStatusPending),
		VotesNeeded:  database.RequiredVotesForApproval,
		CurrentVotes: voteCount,
		YourVote:     &voteReq.Vote,
	}

	if consensusReached {
		if h.auditRepo != nil {
			resourceType := "membership_request"
			logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSubRegionRequestApproved, &resourceType, &requestID, map[string]interface{}{
				"user_id":   request.UserID,
				"region_id": request.RegionID,
				"is_admin":  isUserAdmin,
			}), "sub_region_request_approved")
		}

		response.Status = string(models.MembershipRequestStatusApproved)
		response.MembershipGranted = true
		response.Message = "Membership approved. User has been added to the region."
	}

	writeJSON(w, http.StatusOK, response)
}

// CreateInvitation handles POST /api/v1/communities/:id/invitations
// Allows admins to invite a user to join a sub-region
func (h *MembershipHandler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	regionID := getPathParam(r, "id")
	if regionID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Community ID required")
		return
	}

	var req models.CreateInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "User ID is required")
		return
	}

	// Check if caller is admin for this region (or superuser)
	if !claims.IsSuperuser {
		isAdmin, err := h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, regionID)
		if err != nil {
			writeServerError(w, r, err, "Failed to verify admin status", "membership", "verify_admin_status")
			return
		}
		if !isAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "Admin access required for this region")
			return
		}
	}

	// Get the target region
	region, err := h.regionRepo.GetByID(r.Context(), regionID)
	if errors.Is(err, database.ErrRegionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Community not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get region", "membership", "get_region")
		return
	}

	// Validate region is a sub-region (has parent)
	if region.ParentRegionID == nil {
		writeError(w, http.StatusBadRequest, "invalid_region", "Can only invite users to sub-regions")
		return
	}

	// Check if target user exists
	targetUser, err := h.userRepo.GetByID(r.Context(), req.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user_not_found", "User not found")
		return
	}

	// Check if target user is a member of the parent region hierarchy
	isInParent, err := h.regionRepo.IsUserInRegion(r.Context(), req.UserID, *region.ParentRegionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check parent region membership", "membership", "check_parent_membership")
		return
	}
	if !isInParent {
		writeError(w, http.StatusBadRequest, "user_not_in_parent", "User must be a member of the parent region")
		return
	}

	// Check if user is already a member
	isMember, err := h.regionRepo.IsUserInRegion(r.Context(), req.UserID, regionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check region membership", "membership", "check_region_membership")
		return
	}
	if isMember {
		writeError(w, http.StatusConflict, "already_member", "User is already a member of this region")
		return
	}

	// Check for existing pending invitation or request
	existingRequest, err := h.membershipRepo.GetPendingByUserAndRegion(r.Context(), req.UserID, regionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check existing requests", "membership", "check_existing_requests")
		return
	}
	if existingRequest != nil {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":               "pending_request_exists",
			"message":             "A pending request or invitation already exists for this user and region",
			"existing_request_id": existingRequest.ID,
		})
		return
	}

	// Create the invitation
	invitation := &models.SubRegionMembershipRequest{
		UserID:         req.UserID,
		RegionID:       regionID,
		ParentRegionID: *region.ParentRegionID,
		RequestType:    models.MembershipRequestTypeInvitation,
		InitiatedBy:    claims.UserID,
	}

	if err := h.membershipRepo.Create(r.Context(), invitation); err != nil {
		writeServerError(w, r, err, "Failed to create invitation", "membership", "create_invitation")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "membership_invitation"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSubRegionInviteSent, &resourceType, &invitation.ID, map[string]interface{}{
			"user_id":          req.UserID,
			"user_username":    targetUser.Username,
			"region_id":        regionID,
			"parent_region_id": *region.ParentRegionID,
		}), "sub_region_invite_sent")
	}

	// Queue email notification to invited user
	if h.notificationService != nil {
		if err := h.notificationService.QueueSubRegionInvitation(r.Context(), req.UserID, claims.UserID, regionID); err != nil {
			slog.Warn("failed to queue sub-region invitation notification", "error", err)
		}
	}

	writeJSON(w, http.StatusCreated, models.MembershipRequestResponse{
		RequestID:    invitation.ID,
		UserID:       invitation.UserID,
		RegionID:     invitation.RegionID,
		RequestType:  string(invitation.RequestType),
		Status:       string(invitation.Status),
		VotesNeeded:  0, // Invitations don't need votes
		CurrentVotes: 0,
		ExpiresAt:    invitation.ExpiresAt,
		Message:      "Invitation sent. User will be notified.",
	})
}

// ListInvitations handles GET /api/v1/invitations
// Lists pending invitations for the current user
func (h *MembershipHandler) ListInvitations(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	invitations, err := h.membershipRepo.ListInvitationsForUser(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list invitations", "membership", "list_invitations")
		return
	}

	writeJSON(w, http.StatusOK, models.MembershipRequestListResponse{Requests: invitations})
}

// RespondToInvitation handles POST /api/v1/invitations/:id/respond
// Allows users to accept or decline invitations
func (h *MembershipHandler) RespondToInvitation(w http.ResponseWriter, r *http.Request) {
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

	var req models.RespondToInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Accept {
		// Transaction: lock invitation → verify → get user → add to region → update status
		var invitationRegionID string
		var respondIsAdmin bool
		var err error

		if h.db != nil {
			err = h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
				lockedInvitation, txErr := h.membershipRepo.GetByIDForUpdate(r.Context(), tx, invitationID)
				if txErr != nil {
					return txErr
				}
				if lockedInvitation.UserID != claims.UserID {
					return errors.New("forbidden")
				}
				if lockedInvitation.RequestType != models.MembershipRequestTypeInvitation {
					return errors.New("invalid_type")
				}
				if lockedInvitation.Status != models.MembershipRequestStatusPending {
					return database.ErrProposalClosed
				}
				invitationRegionID = lockedInvitation.RegionID
				user, txErr := h.userRepo.GetByIDForUpdate(r.Context(), tx, claims.UserID)
				if txErr != nil {
					return txErr
				}
				respondIsAdmin = user.PostcardVerified && user.VouchVerified
				if txErr := h.regionRepo.AddUserToRegionTx(r.Context(), tx, claims.UserID, lockedInvitation.RegionID, respondIsAdmin); txErr != nil {
					return txErr
				}
				return h.membershipRepo.UpdateStatusTx(r.Context(), tx, invitationID, models.MembershipRequestStatusApproved)
			})
		} else {
			invitation, getErr := h.membershipRepo.GetByID(r.Context(), invitationID)
			if getErr != nil {
				err = getErr
			} else if invitation.UserID != claims.UserID {
				err = errors.New("forbidden")
			} else if invitation.RequestType != models.MembershipRequestTypeInvitation {
				err = errors.New("invalid_type")
			} else if invitation.Status != models.MembershipRequestStatusPending {
				err = database.ErrProposalClosed
			} else {
				invitationRegionID = invitation.RegionID
				user, _ := h.userRepo.GetByID(r.Context(), claims.UserID)
				if user != nil {
					respondIsAdmin = user.PostcardVerified && user.VouchVerified
				}
				if addErr := h.regionRepo.AddUserToRegion(r.Context(), claims.UserID, invitation.RegionID, respondIsAdmin); addErr != nil {
					err = addErr
				} else {
					err = h.membershipRepo.UpdateStatus(r.Context(), invitationID, models.MembershipRequestStatusApproved)
				}
			}
		}

		if errors.Is(err, database.ErrMembershipRequestNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Invitation not found")
			return
		}
		if errors.Is(err, database.ErrProposalClosed) {
			writeError(w, http.StatusConflict, "invitation_closed", "This invitation is no longer pending")
			return
		}
		if err != nil {
			errMsg := err.Error()
			if errMsg == "forbidden" {
				writeError(w, http.StatusForbidden, "forbidden", "This invitation is not for you")
				return
			}
			if errMsg == "invalid_type" {
				writeError(w, http.StatusBadRequest, "invalid_type", "This is not an invitation")
				return
			}
			writeServerError(w, r, err, "Failed to accept invitation", "membership", "accept_invitation")
			return
		}

		if h.auditRepo != nil {
			resourceType := "membership_invitation"
			logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSubRegionInviteAccepted, &resourceType, &invitationID, map[string]interface{}{
				"region_id": invitationRegionID,
				"is_admin":  respondIsAdmin,
			}), "sub_region_invite_accepted")
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"invitation_id":      invitationID,
			"status":             "approved",
			"membership_granted": true,
			"message":            "Invitation accepted. You are now a member of the region.",
		})
	} else {
		// Get the invitation for validation
		invitation, err := h.membershipRepo.GetByID(r.Context(), invitationID)
		if errors.Is(err, database.ErrMembershipRequestNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Invitation not found")
			return
		}
		if err != nil {
			writeServerError(w, r, err, "Failed to get invitation", "membership", "get_invitation")
			return
		}

		if invitation.UserID != claims.UserID {
			writeError(w, http.StatusForbidden, "forbidden", "This invitation is not for you")
			return
		}
		if invitation.RequestType != models.MembershipRequestTypeInvitation {
			writeError(w, http.StatusBadRequest, "invalid_type", "This is not an invitation")
			return
		}
		if invitation.Status != models.MembershipRequestStatusPending {
			writeError(w, http.StatusConflict, "invitation_closed", "This invitation is no longer pending")
			return
		}

		if err := h.membershipRepo.UpdateStatus(r.Context(), invitationID, models.MembershipRequestStatusRejected); err != nil {
			writeServerError(w, r, err, "Failed to decline invitation", "membership", "decline_invitation")
			return
		}

		if h.auditRepo != nil {
			resourceType := "membership_invitation"
			logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSubRegionInviteDeclined, &resourceType, &invitationID, map[string]interface{}{
				"region_id": invitation.RegionID,
			}), "sub_region_invite_declined")
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"invitation_id": invitationID,
			"status":        "rejected",
			"message":       "Invitation declined.",
		})
	}
}
