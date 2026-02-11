package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// DeletionProposalHandler handles deletion proposal endpoints
type DeletionProposalHandler struct {
	db              *database.DB
	proposalRepo    *database.DeletionProposalRepository
	signalGroupRepo *database.SignalGroupRepository
	regionRepo      *database.RegionRepository
	schoolRepo      *database.SchoolRepository
	userRepo        *database.UserRepository
	auditRepo       *database.AuditRepository
	consensusConfig *config.ConsensusConfig
}

// NewDeletionProposalHandler creates a new deletion proposal handler
func NewDeletionProposalHandler(
	db *database.DB,
	proposalRepo *database.DeletionProposalRepository,
	signalGroupRepo *database.SignalGroupRepository,
	regionRepo *database.RegionRepository,
	schoolRepo *database.SchoolRepository,
	userRepo *database.UserRepository,
	auditRepo *database.AuditRepository,
	consensusConfig *config.ConsensusConfig,
) *DeletionProposalHandler {
	return &DeletionProposalHandler{
		db:              db,
		proposalRepo:    proposalRepo,
		signalGroupRepo: signalGroupRepo,
		regionRepo:      regionRepo,
		schoolRepo:      schoolRepo,
		userRepo:        userRepo,
		auditRepo:       auditRepo,
		consensusConfig: consensusConfig,
	}
}

// isAdminForProposal checks if a user is an admin in the scope of the proposal
func (h *DeletionProposalHandler) isAdminForProposal(ctx context.Context, userID string, proposal *models.DeletionProposal) (bool, error) {
	if proposal.RegionID != nil {
		return h.regionRepo.IsUserAdmin(ctx, userID, *proposal.RegionID)
	}
	if proposal.SchoolID != nil {
		userSchool, err := h.schoolRepo.GetUserSchool(ctx, userID, *proposal.SchoolID)
		if err != nil {
			return false, err
		}
		return userSchool != nil && userSchool.IsAdmin && userSchool.VerificationStatus == models.SchoolVerificationStatusVerified, nil
	}
	if proposal.DistrictID != nil {
		return h.isDistrictAdmin(ctx, userID, *proposal.DistrictID)
	}
	return false, nil
}

// isDistrictAdmin checks if a user is a verified admin of any school in the district
func (h *DeletionProposalHandler) isDistrictAdmin(ctx context.Context, userID, districtID string) (bool, error) {
	districtSchools, err := h.schoolRepo.ListByDistrict(ctx, districtID)
	if err != nil {
		return false, err
	}
	for _, schoolSummary := range districtSchools {
		userSchool, err := h.schoolRepo.GetUserSchool(ctx, userID, schoolSummary.ID)
		if err == nil && userSchool != nil && userSchool.IsAdmin && userSchool.VerificationStatus == models.SchoolVerificationStatusVerified {
			return true, nil
		}
	}
	return false, nil
}

// getAdminCount returns the admin count for the proposal's scope
func (h *DeletionProposalHandler) getAdminCount(ctx context.Context, proposal *models.DeletionProposal) (int, error) {
	if proposal.RegionID != nil {
		return h.regionRepo.GetAdminCount(ctx, *proposal.RegionID)
	}
	if proposal.SchoolID != nil {
		schoolDetails, err := h.schoolRepo.GetByIDWithDetails(ctx, *proposal.SchoolID)
		if err != nil {
			return 0, err
		}
		return schoolDetails.AdminCount, nil
	}
	if proposal.DistrictID != nil {
		// Sum admin counts across schools in the district
		districtSchools, err := h.schoolRepo.ListByDistrict(ctx, *proposal.DistrictID)
		if err != nil {
			return 0, err
		}
		totalAdmins := 0
		for _, schoolSummary := range districtSchools {
			schoolDetails, err := h.schoolRepo.GetByIDWithDetails(ctx, schoolSummary.ID)
			if err != nil {
				continue
			}
			totalAdmins += schoolDetails.AdminCount
		}
		return totalAdmins, nil
	}
	return 0, nil
}

// CreateProposal handles POST /api/v1/deletion-proposals
func (h *DeletionProposalHandler) CreateProposal(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req models.CreateDeletionProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.AssetID == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Asset ID is required")
		return
	}
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Reason is required")
		return
	}
	if req.AssetType != models.AssetTypeSignalGroup && req.AssetType != models.AssetTypeSubRegion {
		writeError(w, http.StatusBadRequest, "validation_error", "Asset type must be signal_group or sub_region")
		return
	}

	// Build proposal based on asset type
	proposal := &models.DeletionProposal{
		AssetType:  req.AssetType,
		AssetID:    req.AssetID,
		ProposedBy: &claims.UserID,
		Reason:     &req.Reason,
	}

	// Look up asset to determine scope
	switch req.AssetType {
	case models.AssetTypeSignalGroup:
		signalGroup, err := h.signalGroupRepo.GetByID(r.Context(), req.AssetID)
		if err != nil {
			if err == database.ErrSignalGroupNotFound {
				writeError(w, http.StatusNotFound, "not_found", "Signal group not found")
				return
			}
			writeServerError(w, r, err, "Failed to get signal group", "deletion", "get_signal_group")
			return
		}
		if !signalGroup.IsActive {
			writeError(w, http.StatusBadRequest, "already_deleted", "Signal group is already deactivated")
			return
		}
		proposal.RegionID = signalGroup.RegionID
		proposal.SchoolID = signalGroup.SchoolID
		proposal.DistrictID = signalGroup.DistrictID

	case models.AssetTypeSubRegion:
		region, err := h.regionRepo.GetByID(r.Context(), req.AssetID)
		if err != nil {
			if err == database.ErrRegionNotFound {
				writeError(w, http.StatusNotFound, "not_found", "Region not found")
				return
			}
			writeServerError(w, r, err, "Failed to get region", "deletion", "get_region")
			return
		}
		// The governing region is the parent of the sub-region being deleted
		if region.ParentRegionID == nil {
			writeError(w, http.StatusBadRequest, "invalid_asset", "Top-level regions cannot be deleted via proposals")
			return
		}
		proposal.RegionID = region.ParentRegionID
	}

	// Validate caller is admin in the appropriate scope
	isAdmin, err := h.isAdminForProposal(r.Context(), claims.UserID, proposal)
	if err != nil {
		writeServerError(w, r, err, "Failed to verify admin status", "deletion", "verify_admin_status")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "not_admin", "You must be an admin to propose deletions")
		return
	}

	// Check admin count meets minimum floor
	adminCount, err := h.getAdminCount(r.Context(), proposal)
	if err != nil {
		writeServerError(w, r, err, "Failed to count admins", "deletion", "count_admins")
		return
	}
	votesNeeded := h.consensusConfig.RequiredVotes(adminCount)
	if adminCount < h.consensusConfig.VoteFloor {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"error":           "insufficient_admins",
			"message":         "Not enough admins for deletion voting",
			"admin_count":     adminCount,
			"admins_required": h.consensusConfig.VoteFloor,
		})
		return
	}

	// Check for existing pending proposal + create atomically
	if h.db != nil {
		err = h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
			existing, txErr := h.proposalRepo.GetPendingByAssetForUpdate(r.Context(), tx, req.AssetType, req.AssetID)
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
		// Non-transactional fallback for tests without a DB connection.
		existing, checkErr := h.proposalRepo.GetPendingByAsset(r.Context(), req.AssetType, req.AssetID)
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
		existing, _ := h.proposalRepo.GetPendingByAsset(r.Context(), req.AssetType, req.AssetID)
		existingID := ""
		if existing != nil {
			existingID = existing.ID
		}
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":                "pending_proposal_exists",
			"message":              "A pending deletion proposal already exists for this asset",
			"existing_proposal_id": existingID,
		})
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to create proposal", "deletion", "create_proposal")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "deletion_proposal"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalCreated, &resourceType, &proposal.ID, map[string]interface{}{
			"asset_type": string(req.AssetType),
			"asset_id":   req.AssetID,
			"reason":     req.Reason,
		}), "proposal_created")
	}

	writeJSON(w, http.StatusCreated, models.DeletionProposalResponse{
		ProposalID:   proposal.ID,
		Status:       string(proposal.Status),
		VotesNeeded:  votesNeeded,
		CurrentVotes: 1, // Proposer's vote
	})
}

// VoteOnProposal handles POST /api/v1/deletion-proposals/:id/vote
func (h *DeletionProposalHandler) VoteOnProposal(w http.ResponseWriter, r *http.Request) {
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
		if err == database.ErrDeletionProposalNotFound {
			writeError(w, http.StatusNotFound, "not_found", "Proposal not found")
			return
		}
		writeServerError(w, r, err, "Failed to get proposal", "deletion", "get_proposal")
		return
	}

	// Check if user is admin in the proposal's scope
	isAdmin, err := h.isAdminForProposal(r.Context(), claims.UserID, proposal)
	if err != nil {
		writeServerError(w, r, err, "Failed to verify admin status", "deletion", "verify_admin_status")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "not_admin", "You must be an admin to vote on deletion proposals")
		return
	}

	// Get admin count for votes needed calculation
	adminCount, err := h.getAdminCount(r.Context(), proposal)
	if err != nil {
		writeServerError(w, r, err, "Failed to count admins", "deletion", "count_admins")
		return
	}
	votesNeeded := h.consensusConfig.RequiredVotes(adminCount)

	// Transaction: lock proposal → check vote → add vote → count → maybe apply deletion
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
				// Apply the deletion based on asset type
				switch lockedProposal.AssetType {
				case models.AssetTypeSignalGroup:
					if txErr := h.proposalRepo.ApplySignalGroupDeletionTx(r.Context(), tx, lockedProposal.AssetID); txErr != nil {
						return txErr
					}
				case models.AssetTypeSubRegion:
					if txErr := h.proposalRepo.ApplySubRegionDeletionTx(r.Context(), tx, lockedProposal.AssetID); txErr != nil {
						return txErr
					}
				}
				if txErr := h.proposalRepo.UpdateStatusTx(r.Context(), tx, proposalID, models.ProposalStatusApproved); txErr != nil {
					return txErr
				}
			}
			return nil
		})
	} else {
		// Non-transactional fallback for tests without a DB connection.
		// Note: asset deletion is not applied here — only status is updated.
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
		writeServerError(w, r, err, "Failed to record vote", "deletion", "record_vote")
		return
	}

	// Audit logging
	if h.auditRepo != nil {
		resourceType := "deletion_proposal"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalVoted, &resourceType, &proposalID, map[string]interface{}{
			"vote": req.Vote,
		}), "proposal_voted")
	}

	response := models.DeletionProposalResponse{
		ProposalID:   proposalID,
		YourVote:     &req.Vote,
		CurrentVotes: voteCount,
		VotesNeeded:  votesNeeded,
		Status:       string(models.ProposalStatusPending),
	}

	if consensusReached {
		if h.auditRepo != nil {
			resourceType := "deletion_proposal"
			logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalApproved, &resourceType, &proposalID, map[string]interface{}{
				"asset_type": string(proposal.AssetType),
				"asset_id":   proposal.AssetID,
			}), "proposal_approved")

			// Log the specific asset deletion
			switch proposal.AssetType {
			case models.AssetTypeSignalGroup:
				resourceType = "signal_group"
				logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSignalGroupDeleted, &resourceType, &proposal.AssetID, map[string]interface{}{
					"proposal_id": proposalID,
				}), "signal_group_deleted")
			case models.AssetTypeSubRegion:
				resourceType = "region"
				logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionRegionDeleted, &resourceType, &proposal.AssetID, map[string]interface{}{
					"proposal_id": proposalID,
				}), "region_deleted")
			}
		}
		response.Status = string(models.ProposalStatusApproved)
	}

	writeJSON(w, http.StatusOK, response)
}

// GetProposal handles GET /api/v1/deletion-proposals/:id
func (h *DeletionProposalHandler) GetProposal(w http.ResponseWriter, r *http.Request) {
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

	// Fetch raw proposal once for scope checks and admin count
	rawProposal, err := h.proposalRepo.GetByID(r.Context(), proposalID)
	if err != nil {
		if err == database.ErrDeletionProposalNotFound {
			writeError(w, http.StatusNotFound, "not_found", "Proposal not found")
			return
		}
		writeServerError(w, r, err, "Failed to get proposal", "deletion", "get_proposal")
		return
	}

	// Check access
	isAdmin, err := h.isAdminForProposal(r.Context(), claims.UserID, rawProposal)
	if err != nil {
		writeServerError(w, r, err, "Failed to verify admin status", "deletion", "verify_admin_status")
		return
	}
	if !isAdmin && !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "You must be an admin to view proposal details")
		return
	}

	// Get proposal with votes for the detail response
	detail, err := h.proposalRepo.GetByIDWithVotes(r.Context(), proposalID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get proposal", "deletion", "get_proposal_with_votes")
		return
	}

	// Compute votes needed from actual admin count
	adminCount, err := h.getAdminCount(r.Context(), rawProposal)
	if err == nil {
		detail.VotesNeeded = h.consensusConfig.RequiredVotes(adminCount)
	}

	// Calculate can_vote and has_voted
	detail.HasVoted = false
	for _, vote := range detail.Votes {
		if vote.VoterID == claims.UserID {
			detail.HasVoted = true
			break
		}
	}
	detail.CanVote = isAdmin && !detail.HasVoted && detail.Status == string(models.ProposalStatusPending)

	writeJSON(w, http.StatusOK, detail)
}

// ListProposals handles GET /api/v1/deletion-proposals
func (h *DeletionProposalHandler) ListProposals(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	filter := models.ProposalListFilter{
		Status:   r.URL.Query().Get("status"),
		RegionID: r.URL.Query().Get("region_id"),
	}

	var proposals []*models.DeletionProposalSummary
	var err error

	if claims.IsSuperuser {
		proposals, err = h.proposalRepo.ListAll(r.Context(), filter)
	} else {
		proposals, err = h.proposalRepo.ListByUser(r.Context(), claims.UserID, filter)
	}

	if err != nil {
		writeServerError(w, r, err, "Failed to list proposals", "deletion", "list_proposals")
		return
	}

	for _, p := range proposals {
		rawProposal, fetchErr := h.proposalRepo.GetByID(r.Context(), p.ID)
		if fetchErr == nil {
			adminCount, countErr := h.getAdminCount(r.Context(), rawProposal)
			if countErr == nil {
				p.VotesNeeded = h.consensusConfig.RequiredVotes(adminCount)
				continue
			}
		}
		p.VotesNeeded = h.consensusConfig.VoteFloor
	}

	writeJSON(w, http.StatusOK, models.DeletionProposalListResponse{Proposals: proposals})
}

// ExpireProposal handles POST /api/v1/deletion-proposals/:id/expire
func (h *DeletionProposalHandler) ExpireProposal(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "Only superusers can expire proposals")
		return
	}

	proposalID := getPathParam(r, "id")
	if proposalID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Proposal ID required")
		return
	}

	proposal, err := h.proposalRepo.GetByID(r.Context(), proposalID)
	if err != nil {
		if err == database.ErrDeletionProposalNotFound {
			writeError(w, http.StatusNotFound, "not_found", "Proposal not found")
			return
		}
		writeServerError(w, r, err, "Failed to get proposal", "deletion", "get_proposal")
		return
	}

	if proposal.Status != models.ProposalStatusPending {
		writeError(w, http.StatusConflict, "proposal_closed", "Only pending proposals can be expired")
		return
	}

	if err := h.proposalRepo.UpdateStatus(r.Context(), proposalID, models.ProposalStatusExpired); err != nil {
		writeServerError(w, r, err, "Failed to expire proposal", "deletion", "expire_proposal")
		return
	}

	if h.auditRepo != nil {
		resourceType := "deletion_proposal"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalExpired, &resourceType, &proposalID, map[string]interface{}{
			"asset_type": string(proposal.AssetType),
			"asset_id":   proposal.AssetID,
			"expired_by": "superuser",
		}), "proposal_expired")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"proposal_id": proposalID,
		"status":      "expired",
		"message":     "Proposal has been expired",
	})
}
