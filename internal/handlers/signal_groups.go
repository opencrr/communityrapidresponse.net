package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
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
	proposalRepo        *database.InviteLinkProposalRepository
	regionRepo          *database.RegionRepository
	auditRepo           *database.AuditRepository
	consensusConfig     *config.ConsensusConfig
	notificationService NotificationServiceInterface
}

// NewSignalGroupHandler creates a new signal group handler
func NewSignalGroupHandler(
	db *database.DB,
	groupRepo *database.SignalGroupRepository,
	proposalRepo *database.InviteLinkProposalRepository,
	regionRepo *database.RegionRepository,
	auditRepo *database.AuditRepository,
	consensusConfig *config.ConsensusConfig,
) *SignalGroupHandler {
	return &SignalGroupHandler{
		db:              db,
		groupRepo:       groupRepo,
		proposalRepo:    proposalRepo,
		regionRepo:      regionRepo,
		auditRepo:       auditRepo,
		consensusConfig: consensusConfig,
	}
}

// SetNotificationService sets the notification service for the handler.
// This is optional - if not set, notifications will not be sent.
func (h *SignalGroupHandler) SetNotificationService(svc NotificationServiceInterface) {
	h.notificationService = svc
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

	// Validate request - require region_id for region-based groups
	if req.RegionID == nil || *req.RegionID == "" || req.Name == "" || req.InviteLink == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Community ID, group name, and invite link are required")
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

	// Check group limit + create atomically to prevent race condition
	group := &models.SignalGroup{
		RegionID:            &regionID,
		GroupName:           req.Name,
		InviteLink:          req.InviteLink,
		InviteLinkUpdatedBy: &claims.UserID,
		Description:         req.Description,
		CreatedBy:           &claims.UserID,
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
			return h.groupRepo.CreateGroupTx(r.Context(), tx, group)
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

	// Only verified users (vouched or postcard) can see invite links
	if claims.VerificationTier < models.TierVouched {
		writeError(w, http.StatusForbidden, "forbidden", "Verification required to view Signal groups")
		return
	}

	regionID := r.URL.Query().Get("region_id")

	var groups []*models.SignalGroup
	var err error

	if regionID != "" {
		// List groups for a specific region
		groups, err = h.groupRepo.ListByRegion(r.Context(), regionID)
	} else {
		// List all groups for user's accessible regions
		groups, err = h.groupRepo.ListByUser(r.Context(), claims.UserID)
	}

	if err != nil {
		writeServerError(w, r, err, "Failed to list Signal groups", "signal_group", "list_groups")
		return
	}

	// Convert to response format with invite links
	// Initialize as empty slice (not nil) so JSON serializes as [] instead of null
	response := []models.SignalGroupWithInvite{}
	for _, g := range groups {
		response = append(response, models.SignalGroupWithInvite{
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
			InviteLink: g.InviteLink,
		})
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

	// Convert to response format with invite links and scope names
	response := []models.SignalGroupWithInvite{}
	for _, g := range groups {
		response = append(response, models.SignalGroupWithInvite{
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
			InviteLink: g.InviteLink,
		})
	}

	writeJSON(w, http.StatusOK, models.SignalGroupListResponse{Groups: response})
}

// Update handles PUT /api/v1/signal-groups/:id
// Note: invite link updates require consensus - use proposal endpoints
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

	// Update (name and description only - invite link requires consensus)
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
		"updated_at": group.CreatedAt, // Would be new timestamp in real impl
	})
}

// CreateInviteLinkProposal handles POST /api/v1/signal-groups/:id/invite-link-proposals
func (h *SignalGroupHandler) CreateInviteLinkProposal(w http.ResponseWriter, r *http.Request) {
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

	var req models.CreateInviteLinkProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.NewInviteLink == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "New invite link is required")
		return
	}

	// Get the group
	group, err := h.groupRepo.GetByID(r.Context(), groupID)
	if errors.Is(err, database.ErrSignalGroupNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Signal group not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get Signal group", "signal_group", "get_group")
		return
	}

	// Check group has a region
	if group.RegionID == nil {
		writeError(w, http.StatusBadRequest, "invalid_group", "Invite link proposals are only supported for region-based signal groups")
		return
	}

	// Check if user is admin for this region
	isAdmin, err := h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, *group.RegionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to verify admin status", "signal_group", "verify_admin_status")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "not_region_admin", "You must be an admin of this region to propose changes")
		return
	}

	// Check admin count meets minimum floor
	adminCount, err := h.regionRepo.GetAdminCount(r.Context(), *group.RegionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to count admins", "signal_group", "count_admins")
		return
	}
	votesNeeded := h.consensusConfig.RequiredVotes(adminCount)
	if adminCount < h.consensusConfig.VoteFloor {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"error":          "insufficient_admins",
			"message":        "Community does not have enough admins to update invite links",
			"admin_count":    adminCount,
			"admins_required": h.consensusConfig.VoteFloor,
			"suggestion":     "Contact an Application Manager for assistance",
		})
		return
	}

	// Check for existing pending proposal + create atomically
	proposal := &models.InviteLinkUpdateProposal{
		SignalGroupID: groupID,
		RegionID:      group.RegionID,
		ProposedBy:    &claims.UserID,
		NewInviteLink: req.NewInviteLink,
		Reason:        req.Reason,
	}

	if h.db != nil {
		err = h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			existing, txErr := h.proposalRepo.GetPendingBySignalGroupForUpdate(r.Context(), tx, groupID)
			if txErr != nil {
				return txErr
			}
			if existing != nil {
				return database.ErrPendingExists
			}
			if txErr := h.proposalRepo.CreateTx(r.Context(), tx, proposal); txErr != nil {
				return txErr
			}
			return h.proposalRepo.AddVoteTx(r.Context(), tx, proposal.ID, claims.UserID, true)
		})
	} else {
		existing, checkErr := h.proposalRepo.GetPendingBySignalGroup(r.Context(), groupID)
		if checkErr != nil {
			err = checkErr
		} else if existing != nil {
			err = database.ErrPendingExists
		} else {
			if createErr := h.proposalRepo.Create(r.Context(), proposal); createErr != nil {
				err = createErr
			} else {
				err = h.proposalRepo.AddVote(r.Context(), proposal.ID, claims.UserID, true)
			}
		}
	}
	if errors.Is(err, database.ErrPendingExists) {
		// Re-fetch outside tx for the response
		existing, _ := h.proposalRepo.GetPendingBySignalGroup(r.Context(), groupID)
		existingID := ""
		if existing != nil {
			existingID = existing.ID
		}
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":                "pending_proposal_exists",
			"message":              "A pending invite link update proposal already exists for this group",
			"existing_proposal_id": existingID,
		})
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to create proposal", "signal_group", "create_invite_link_proposal")
		return
	}

	// Audit log: proposal created
	if h.auditRepo != nil {
		resourceType := "invite_link_proposal"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalCreated, &resourceType, &proposal.ID, map[string]interface{}{
			"signal_group_id": groupID,
			"reason":          req.Reason,
		}), "proposal_created")
	}

	writeJSON(w, http.StatusCreated, models.InviteLinkProposalResponse{
		ProposalID:    proposal.ID,
		SignalGroupID: groupID,
		Status:        string(proposal.Status),
		VotesNeeded:   votesNeeded,
		CurrentVotes:  1, // Proposer's vote
		ExpiresAt:     proposal.ExpiresAt,
	})
}

// VoteOnInviteLinkProposal handles POST /api/v1/signal-groups/invite-link-proposals/:id/vote
func (h *SignalGroupHandler) VoteOnInviteLinkProposal(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	proposalID := getPathParam(r, "id")
	if proposalID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Proposal ID required")
		return
	}

	var req models.VoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Pre-transaction: get proposal (non-locking) for admin check
	proposal, err := h.proposalRepo.GetByID(r.Context(), proposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Proposal not found")
		return
	}

	// Check if user is admin for this region (outside tx - read-only check)
	if proposal.RegionID == nil {
		writeServerError(w, r, errProposalMissingRegion, "Proposal has no associated region", "signal_group", "verify_admin_status")
		return
	}
	isAdmin, err := h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, *proposal.RegionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to verify admin status", "signal_group", "verify_admin_status")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "not_region_admin", "You must be an admin of this region to vote")
		return
	}

	// Get admin count (outside tx - used for threshold calculation)
	adminCount, err := h.regionRepo.GetAdminCount(r.Context(), *proposal.RegionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to count admins", "signal_group", "count_admins")
		return
	}
	votesNeeded := h.consensusConfig.RequiredVotes(adminCount)

	// Transaction: lock proposal → check vote → add vote → count → maybe apply consensus
	var voteCount int
	var consensusReached bool

	if h.db != nil {
		err = h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			lockedProposal, txErr := h.proposalRepo.GetByIDForUpdate(r.Context(), tx, proposalID)
			if txErr != nil {
				return txErr
			}
			if lockedProposal.Status != models.ProposalStatusPending {
				return database.ErrProposalClosed
			}
			hasVoted, txErr := h.proposalRepo.HasVotedTx(r.Context(), tx, proposalID, claims.UserID)
			if txErr != nil {
				return txErr
			}
			if hasVoted {
				return database.ErrAlreadyVoted
			}
			if txErr := h.proposalRepo.AddVoteTx(r.Context(), tx, proposalID, claims.UserID, req.Vote); txErr != nil {
				return txErr
			}
			voteCount, txErr = h.proposalRepo.CountApprovalVotesTx(r.Context(), tx, proposalID)
			if txErr != nil {
				return txErr
			}
			if voteCount >= votesNeeded {
				consensusReached = true
				if txErr := h.groupRepo.UpdateInviteLinkTx(r.Context(), tx, lockedProposal.SignalGroupID, lockedProposal.NewInviteLink, claims.UserID); txErr != nil {
					return txErr
				}
				if txErr := h.proposalRepo.UpdateStatusTx(r.Context(), tx, proposalID, models.ProposalStatusApproved); txErr != nil {
					return txErr
				}
			}
			return nil
		})
	} else {
		// Fallback for when db is not available (e.g., tests)
		if proposal.Status != models.ProposalStatusPending {
			err = database.ErrProposalClosed
		} else {
			hasVoted, checkErr := h.proposalRepo.HasVoted(r.Context(), proposalID, claims.UserID)
			if checkErr != nil {
				err = checkErr
			} else if hasVoted {
				err = database.ErrAlreadyVoted
			} else {
				if addErr := h.proposalRepo.AddVote(r.Context(), proposalID, claims.UserID, req.Vote); addErr != nil {
					err = addErr
				} else {
					voteCount, err = h.proposalRepo.CountApprovalVotes(r.Context(), proposalID)
					if err == nil && voteCount >= votesNeeded {
						consensusReached = true
						_ = h.groupRepo.UpdateInviteLink(r.Context(), proposal.SignalGroupID, proposal.NewInviteLink, claims.UserID)
						_ = h.proposalRepo.UpdateStatus(r.Context(), proposalID, models.ProposalStatusApproved)
					}
				}
			}
		}
	}

	if errors.Is(err, database.ErrProposalClosed) {
		writeError(w, http.StatusConflict, "proposal_closed", "This proposal is no longer open for voting")
		return
	}
	if errors.Is(err, database.ErrAlreadyVoted) {
		writeError(w, http.StatusConflict, "already_voted", "You have already voted on this proposal")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to record vote", "signal_group", "record_vote")
		return
	}

	// Side effects outside transaction: audit logs + notifications
	if h.auditRepo != nil {
		resourceType := "invite_link_proposal"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalVoted, &resourceType, &proposalID, map[string]interface{}{
			"vote": req.Vote,
		}), "proposal_voted")
	}

	response := models.InviteLinkProposalResponse{
		ProposalID:   proposalID,
		YourVote:     &req.Vote,
		CurrentVotes: voteCount,
		VotesNeeded:  votesNeeded,
		Status:       string(models.ProposalStatusPending),
	}

	if consensusReached {
		// Audit logs for consensus
		if h.auditRepo != nil {
			resourceType := "invite_link_proposal"
			logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalApproved, &resourceType, &proposalID, map[string]interface{}{
				"signal_group_id": proposal.SignalGroupID,
			}), "proposal_approved")
			resourceType = "signal_group"
			logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionInviteLinkUpdated, &resourceType, &proposal.SignalGroupID, map[string]interface{}{
				"proposal_id": proposalID,
			}), "invite_link_updated")
		}

		// Queue notifications
		if h.notificationService != nil && proposal.RegionID != nil {
			if err := h.notificationService.QueueInviteLinkUpdatedEvent(r.Context(), proposal.SignalGroupID, *proposal.RegionID); err != nil {
				log.Printf("WARN: Failed to queue invite link update notifications: %v", err)
			}
		}

		response.Status = string(models.ProposalStatusApproved)
		response.InviteLinkUpdated = true
		response.NotificationsQueued = 1
		response.Message = "Invite link updated. Email notifications will be sent to verified users."
	}

	writeJSON(w, http.StatusOK, response)
}

// GetInviteLinkProposal handles GET /api/v1/signal-groups/invite-link-proposals/:id
func (h *SignalGroupHandler) GetInviteLinkProposal(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	proposalID := getPathParam(r, "id")
	if proposalID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Proposal ID required")
		return
	}

	// Get proposal with votes
	proposal, err := h.proposalRepo.GetByIDWithVotes(r.Context(), proposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Proposal not found")
		return
	}

	// Check access: superuser can view any proposal, admins can view their region's proposals
	isAdmin := false
	if !claims.IsSuperuser {
		isAdmin, err = h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, proposal.RegionID)
		if err != nil {
			writeServerError(w, r, err, "Failed to verify admin status", "signal_group", "verify_admin_status")
			return
		}
		if !isAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this region to view proposal details")
			return
		}
	}

	// Get admin count for votes needed calculation
	adminCount, err := h.regionRepo.GetAdminCount(r.Context(), proposal.RegionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to count admins", "signal_group", "count_admins")
		return
	}
	proposal.VotesNeeded = h.consensusConfig.RequiredVotes(adminCount)

	// Superusers cannot vote (only admins can), so can_vote is false for superusers
	if claims.IsSuperuser && !isAdmin {
		// Check if user is also an admin of this region
		isAdmin, _ = h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, proposal.RegionID)
	}

	// Calculate can_vote and has_voted
	proposal.HasVoted = false
	for _, vote := range proposal.Votes {
		if vote.VoterID == claims.UserID {
			proposal.HasVoted = true
			break
		}
	}

	// Can vote if: is admin, hasn't voted, and proposal is pending
	proposal.CanVote = isAdmin && !proposal.HasVoted && proposal.Status == string(models.ProposalStatusPending)

	// Hide invite link if not admin and not superuser
	if !claims.IsSuperuser && !isAdmin {
		proposal.NewInviteLink = ""
	}

	writeJSON(w, http.StatusOK, proposal)
}

// ListInviteLinkProposals handles GET /api/v1/signal-groups/invite-link-proposals
func (h *SignalGroupHandler) ListInviteLinkProposals(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Parse filter query params
	filter := models.ProposalListFilter{
		Status:        r.URL.Query().Get("status"),
		SignalGroupID: r.URL.Query().Get("signal_group_id"),
		RegionID:      r.URL.Query().Get("region_id"),
	}

	var proposals []*models.InviteLinkProposalSummary
	var err error

	if claims.IsSuperuser {
		// Superusers can see all proposals
		proposals, err = h.proposalRepo.ListAll(r.Context(), filter)
	} else {
		// Regular users see only proposals for regions where they are admin
		proposals, err = h.proposalRepo.ListByUser(r.Context(), claims.UserID, filter)
	}

	if err != nil {
		writeServerError(w, r, err, "Failed to list proposals", "signal_group", "list_proposals")
		return
	}

	// Calculate votes_needed for each proposal
	regionAdminCounts := make(map[string]int)
	for _, p := range proposals {
		// Get admin count (with caching to avoid repeated queries)
		if _, ok := regionAdminCounts[p.RegionName]; !ok {
			// We need the region ID to get admin count, but we only have region name in summary
			// For now, use a default votes_needed based on consensus config floor
			regionAdminCounts[p.RegionName] = h.consensusConfig.VoteFloor
		}
		p.VotesNeeded = h.consensusConfig.RequiredVotes(regionAdminCounts[p.RegionName])
	}

	writeJSON(w, http.StatusOK, models.InviteLinkProposalListResponse{Proposals: proposals})
}

// ExpireInviteLinkProposal handles POST /api/v1/signal-groups/invite-link-proposals/:id/expire
// Allows superusers to immediately expire a pending proposal
func (h *SignalGroupHandler) ExpireInviteLinkProposal(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Only superusers can force-expire proposals
	if !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "Only superusers can expire proposals")
		return
	}

	proposalID := getPathParam(r, "id")
	if proposalID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Proposal ID required")
		return
	}

	// Get the proposal
	proposal, err := h.proposalRepo.GetByID(r.Context(), proposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Proposal not found")
		return
	}

	// Can only expire pending proposals
	if proposal.Status != models.ProposalStatusPending {
		writeError(w, http.StatusConflict, "proposal_closed", "Only pending proposals can be expired")
		return
	}

	// Update status to expired
	if err := h.proposalRepo.UpdateStatus(r.Context(), proposalID, models.ProposalStatusExpired); err != nil {
		writeServerError(w, r, err, "Failed to expire proposal", "signal_group", "expire_proposal")
		return
	}

	// Audit log: proposal expired by superuser
	if h.auditRepo != nil {
		resourceType := "invite_link_proposal"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalExpired, &resourceType, &proposalID, map[string]interface{}{
			"signal_group_id": proposal.SignalGroupID,
			"expired_by":      "superuser",
		}), "proposal_expired")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"proposal_id": proposalID,
		"status":      "expired",
		"message":     "Proposal has been expired",
	})
}
