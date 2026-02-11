package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// BlocklistProposalHandler handles blocklist proposal endpoints
type BlocklistProposalHandler struct {
	db                  *database.DB
	proposalRepo        *database.BlocklistProposalRepository
	regionRepo          *database.RegionRepository
	userRepo            *database.UserRepository
	auditRepo           *database.AuditRepository
	consensusConfig     *config.ConsensusConfig
	blocklistConfig     *config.BlocklistConfig
	notificationService NotificationServiceInterface
}

// NewBlocklistProposalHandler creates a new blocklist proposal handler
func NewBlocklistProposalHandler(
	db *database.DB,
	proposalRepo *database.BlocklistProposalRepository,
	regionRepo *database.RegionRepository,
	userRepo *database.UserRepository,
	auditRepo *database.AuditRepository,
	consensusConfig *config.ConsensusConfig,
	blocklistConfig *config.BlocklistConfig,
) *BlocklistProposalHandler {
	return &BlocklistProposalHandler{
		db:              db,
		proposalRepo:    proposalRepo,
		regionRepo:      regionRepo,
		userRepo:        userRepo,
		auditRepo:       auditRepo,
		consensusConfig: consensusConfig,
		blocklistConfig: blocklistConfig,
	}
}

// SetNotificationService sets the notification service for the handler.
// This is optional - if not set, notifications will not be sent.
func (h *BlocklistProposalHandler) SetNotificationService(svc NotificationServiceInterface) {
	h.notificationService = svc
}

// CreateProposal handles POST /api/v1/communities/:id/blocklist-proposals
func (h *BlocklistProposalHandler) CreateProposal(w http.ResponseWriter, r *http.Request) {
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

	var req models.CreateBlocklistProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.TargetUserID == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Target user ID is required")
		return
	}
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Reason is required")
		return
	}

	// Cannot blocklist yourself
	if req.TargetUserID == claims.UserID {
		writeError(w, http.StatusBadRequest, "cannot_blocklist_self", "You cannot propose to blocklist yourself")
		return
	}

	// Check if user is admin for this region
	isAdmin, err := h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, regionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to verify admin status", "blocklist", "verify_admin_status")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "not_region_admin", "You must be an admin of this region to propose blocklisting")
		return
	}

	// Check admin count meets minimum floor
	adminCount, err := h.regionRepo.GetAdminCount(r.Context(), regionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to count admins", "blocklist", "count_admins")
		return
	}
	votesNeeded := h.consensusConfig.RequiredVotes(adminCount)
	if adminCount < h.consensusConfig.VoteFloor {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"error":           "insufficient_admins",
			"message":         "Community does not have enough admins for blocklist voting",
			"admin_count":     adminCount,
			"admins_required": h.consensusConfig.VoteFloor,
		})
		return
	}

	// Check if target user exists and is not a superuser
	targetUser, err := h.userRepo.GetByID(r.Context(), req.TargetUserID)
	if err != nil {
		if err == database.ErrUserNotFound {
			writeError(w, http.StatusNotFound, "user_not_found", "Target user not found")
			return
		}
		writeServerError(w, r, err, "Failed to get target user", "blocklist", "get_target_user")
		return
	}
	if targetUser.IsSuperuser {
		writeError(w, http.StatusForbidden, "cannot_blocklist_superuser", "Superusers cannot be blocked")
		return
	}

	// Check if target user is in the region
	isInRegion, err := h.proposalRepo.IsUserInRegion(r.Context(), req.TargetUserID, regionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check user region membership", "blocklist", "check_user_region")
		return
	}
	if !isInRegion {
		writeError(w, http.StatusBadRequest, "user_not_in_region", "Target user is not a member of this region")
		return
	}

	// Check rate limit (outside tx - read-only)
	proposalsThisMonth, err := h.proposalRepo.CountProposalsThisMonth(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check rate limit", "blocklist", "check_rate_limit")
		return
	}
	if proposalsThisMonth >= h.blocklistConfig.ProposalRateLimitPerMonth {
		writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"error":          "rate_limit_exceeded",
			"message":        "You have reached the maximum number of blocklist proposals for this month",
			"proposals_used": proposalsThisMonth,
			"proposals_max":  h.blocklistConfig.ProposalRateLimitPerMonth,
		})
		return
	}

	// Check for existing pending proposal + create atomically
	proposal := &models.BlocklistProposal{
		TargetUserID: req.TargetUserID,
		RegionID:     &regionID,
		ProposedBy:   &claims.UserID,
		Reason:       req.Reason,
		Evidence:     req.Evidence,
	}

	if h.db != nil {
		err = h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			existing, txErr := h.proposalRepo.GetPendingByTargetUserForUpdate(r.Context(), tx, req.TargetUserID, regionID)
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
		existing, checkErr := h.proposalRepo.GetPendingByTargetUser(r.Context(), req.TargetUserID, regionID)
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
		existing, _ := h.proposalRepo.GetPendingByTargetUser(r.Context(), req.TargetUserID, regionID)
		existingID := ""
		if existing != nil {
			existingID = existing.ID
		}
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":                "pending_proposal_exists",
			"message":              "A pending blocklist proposal already exists for this user in this region",
			"existing_proposal_id": existingID,
		})
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to create proposal", "blocklist", "create_proposal")
		return
	}

	// Audit log: proposal created
	if h.auditRepo != nil {
		resourceType := "blocklist_proposal"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalCreated, &resourceType, &proposal.ID, map[string]interface{}{
			"target_user_id": req.TargetUserID,
			"region_id":      regionID,
			"reason":         req.Reason,
		}), "proposal_created")
	}

	writeJSON(w, http.StatusCreated, models.BlocklistProposalResponse{
		ProposalID:   proposal.ID,
		Status:       string(proposal.Status),
		VotesNeeded:  votesNeeded,
		CurrentVotes: 1, // Proposer's vote
	})
}

// VoteOnProposal handles POST /api/v1/blocklist-proposals/:id/vote
func (h *BlocklistProposalHandler) VoteOnProposal(w http.ResponseWriter, r *http.Request) {
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

	// Pre-transaction: get proposal for admin check
	proposal, err := h.proposalRepo.GetByID(r.Context(), proposalID)
	if err != nil {
		if err == database.ErrBlocklistProposalNotFound {
			writeError(w, http.StatusNotFound, "not_found", "Proposal not found")
			return
		}
		writeServerError(w, r, err, "Failed to get proposal", "blocklist", "get_proposal")
		return
	}

	// Check if user is admin for this region (outside tx)
	if proposal.RegionID == nil {
		writeServerError(w, r, errProposalMissingRegion, "Proposal has no associated region", "blocklist", "verify_admin_status")
		return
	}
	isAdmin, err := h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, *proposal.RegionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to verify admin status", "blocklist", "verify_admin_status")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "not_region_admin", "You must be an admin of this region to vote")
		return
	}

	// Get admin count (outside tx)
	adminCount, err := h.regionRepo.GetAdminCount(r.Context(), *proposal.RegionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to count admins", "blocklist", "count_admins")
		return
	}
	votesNeeded := h.consensusConfig.RequiredVotes(adminCount)

	// Transaction: lock proposal → check vote → add vote → count → maybe apply blocklist
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
				if txErr := h.proposalRepo.ApplyBlocklistTx(r.Context(), tx, lockedProposal.TargetUserID, *lockedProposal.RegionID, proposalID, claims.UserID, lockedProposal.Reason); txErr != nil {
					return txErr
				}
				if txErr := h.proposalRepo.UpdateStatusTx(r.Context(), tx, proposalID, models.ProposalStatusApproved); txErr != nil {
					return txErr
				}
			}
			return nil
		})
	} else {
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
						_ = h.proposalRepo.ApplyBlocklist(r.Context(), proposal.TargetUserID, *proposal.RegionID, proposalID, claims.UserID, proposal.Reason)
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
		writeServerError(w, r, err, "Failed to record vote", "blocklist", "record_vote")
		return
	}

	// Side effects outside transaction
	if h.auditRepo != nil {
		resourceType := "blocklist_proposal"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalVoted, &resourceType, &proposalID, map[string]interface{}{
			"vote": req.Vote,
		}), "proposal_voted")
	}

	response := models.BlocklistProposalResponse{
		ProposalID:   proposalID,
		YourVote:     &req.Vote,
		CurrentVotes: voteCount,
		VotesNeeded:  votesNeeded,
		Status:       string(models.ProposalStatusPending),
	}

	if consensusReached {
		if h.auditRepo != nil {
			resourceType := "blocklist_proposal"
			logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalApproved, &resourceType, &proposalID, map[string]interface{}{
				"target_user_id": proposal.TargetUserID,
			}), "proposal_approved")
			resourceType = "user"
			logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionUserBlocked, &resourceType, &proposal.TargetUserID, map[string]interface{}{
				"proposal_id": proposalID,
				"region_id":   *proposal.RegionID,
				"reason":      proposal.Reason,
			}), "user_blocked")
		}

		response.Status = string(models.ProposalStatusApproved)
	}

	writeJSON(w, http.StatusOK, response)
}

// GetProposal handles GET /api/v1/blocklist-proposals/:id
func (h *BlocklistProposalHandler) GetProposal(w http.ResponseWriter, r *http.Request) {
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
		if err == database.ErrBlocklistProposalNotFound {
			writeError(w, http.StatusNotFound, "not_found", "Proposal not found")
			return
		}
		writeServerError(w, r, err, "Failed to get proposal", "blocklist", "get_proposal_with_votes")
		return
	}

	// Check access: superuser can view any proposal, admins can view their region's proposals
	isAdmin := false
	if !claims.IsSuperuser {
		isAdmin, err = h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, proposal.RegionID)
		if err != nil {
			writeServerError(w, r, err, "Failed to verify admin status", "blocklist", "verify_admin_status")
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
		writeServerError(w, r, err, "Failed to count admins", "blocklist", "count_admins")
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

	writeJSON(w, http.StatusOK, proposal)
}

// ListProposals handles GET /api/v1/blocklist-proposals
func (h *BlocklistProposalHandler) ListProposals(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Parse filter query params
	filter := models.ProposalListFilter{
		Status:   r.URL.Query().Get("status"),
		RegionID: r.URL.Query().Get("region_id"),
	}

	var proposals []*models.BlocklistProposalSummary
	var err error

	if claims.IsSuperuser {
		// Superusers can see all proposals
		proposals, err = h.proposalRepo.ListAll(r.Context(), filter)
	} else {
		// Regular users see only proposals for regions where they are admin
		proposals, err = h.proposalRepo.ListByUser(r.Context(), claims.UserID, filter)
	}

	if err != nil {
		writeServerError(w, r, err, "Failed to list proposals", "blocklist", "list_proposals")
		return
	}

	// Calculate votes_needed for each proposal using default floor
	for _, p := range proposals {
		p.VotesNeeded = h.consensusConfig.VoteFloor
	}

	writeJSON(w, http.StatusOK, models.BlocklistProposalListResponse{Proposals: proposals})
}

// ExpireProposal handles POST /api/v1/blocklist-proposals/:id/expire
// Allows superusers to immediately expire a pending proposal
func (h *BlocklistProposalHandler) ExpireProposal(w http.ResponseWriter, r *http.Request) {
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
		if err == database.ErrBlocklistProposalNotFound {
			writeError(w, http.StatusNotFound, "not_found", "Proposal not found")
			return
		}
		writeServerError(w, r, err, "Failed to get proposal", "blocklist", "get_proposal")
		return
	}

	// Can only expire pending proposals
	if proposal.Status != models.ProposalStatusPending {
		writeError(w, http.StatusConflict, "proposal_closed", "Only pending proposals can be expired")
		return
	}

	// Update status to expired
	if err := h.proposalRepo.UpdateStatus(r.Context(), proposalID, models.ProposalStatusExpired); err != nil {
		writeServerError(w, r, err, "Failed to expire proposal", "blocklist", "expire_proposal")
		return
	}

	// Audit log: proposal expired by superuser
	if h.auditRepo != nil {
		resourceType := "blocklist_proposal"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalExpired, &resourceType, &proposalID, map[string]interface{}{
			"target_user_id": proposal.TargetUserID,
			"expired_by":     "superuser",
		}), "proposal_expired")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"proposal_id": proposalID,
		"status":      "expired",
		"message":     "Proposal has been expired",
	})
}

// ListBlockedAddresses handles GET /api/v1/admin/blocked-addresses
// Superuser only - lists all blocked addresses
func (h *BlocklistProposalHandler) ListBlockedAddresses(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Only superusers can view blocked addresses
	if !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "Only superusers can view blocked addresses")
		return
	}

	activeOnly := r.URL.Query().Get("active") == "true"

	addresses, err := h.proposalRepo.ListBlockedAddresses(r.Context(), activeOnly)
	if err != nil {
		writeServerError(w, r, err, "Failed to list blocked addresses", "blocklist", "list_blocked_addresses")
		return
	}

	// Truncate address hashes for display (show first 8 and last 8 characters)
	for _, a := range addresses {
		if len(a.AddressHash) > 16 {
			a.AddressHash = a.AddressHash[:8] + "..." + a.AddressHash[len(a.AddressHash)-8:]
		}
	}

	writeJSON(w, http.StatusOK, models.BlockedAddressListResponse{Addresses: addresses})
}

// ExpireAddress handles POST /api/v1/admin/blocked-addresses/:hash/expire
// Superuser only - expires a blocked address early
func (h *BlocklistProposalHandler) ExpireAddress(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Only superusers can expire addresses
	if !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "Only superusers can expire blocked addresses")
		return
	}

	addressHash := getPathParam(r, "hash")
	if addressHash == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Address hash required")
		return
	}

	if err := h.proposalRepo.ExpireAddress(r.Context(), addressHash); err != nil {
		if err.Error() == "address not found" {
			writeError(w, http.StatusNotFound, "not_found", "Blocked address not found")
			return
		}
		writeServerError(w, r, err, "Failed to expire address", "blocklist", "expire_address")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "blocked_address"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, "address_block_expired", &resourceType, &addressHash, map[string]interface{}{
			"expired_by": claims.UserID,
		}), "address_block_expired")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"address_hash": addressHash,
		"status":       "expired",
		"message":      "Address block has been expired",
	})
}
