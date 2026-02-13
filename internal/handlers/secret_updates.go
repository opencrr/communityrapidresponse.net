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

// SecretUpdateHandler handles encrypted secret update proposals
type SecretUpdateHandler struct {
	db                *database.DB
	proposalRepo      *database.SecretUpdateProposalRepository
	secretRepo        *database.EncryptedSecretRepository
	encryptionKeyRepo *database.EncryptionKeyRepository
	regionRepo        *database.RegionRepository
	schoolRepo        *database.SchoolRepository
	auditRepo         *database.AuditRepository
	consensusConfig   *config.ConsensusConfig
	notificationService NotificationServiceInterface
}

// NewSecretUpdateHandler creates a new secret update handler
func NewSecretUpdateHandler(
	db *database.DB,
	proposalRepo *database.SecretUpdateProposalRepository,
	secretRepo *database.EncryptedSecretRepository,
	encryptionKeyRepo *database.EncryptionKeyRepository,
	regionRepo *database.RegionRepository,
	schoolRepo *database.SchoolRepository,
	auditRepo *database.AuditRepository,
	consensusConfig *config.ConsensusConfig,
) *SecretUpdateHandler {
	return &SecretUpdateHandler{
		db:                db,
		proposalRepo:      proposalRepo,
		secretRepo:        secretRepo,
		encryptionKeyRepo: encryptionKeyRepo,
		regionRepo:        regionRepo,
		schoolRepo:        schoolRepo,
		auditRepo:         auditRepo,
		consensusConfig:   consensusConfig,
	}
}

// SetNotificationService sets the notification service
func (h *SecretUpdateHandler) SetNotificationService(svc NotificationServiceInterface) {
	h.notificationService = svc
}

// CreateProposal handles POST /api/v1/signal-groups/:id/secret-proposals
func (h *SecretUpdateHandler) CreateProposal(w http.ResponseWriter, r *http.Request) {
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

	var req models.CreateSecretProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.EncryptedPayload == "" || req.EncryptionIV == "" || len(req.WrappedKeys) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "Encrypted payload, IV, and wrapped keys are required")
		return
	}

	// Get the encrypted secret for this group
	secret, err := h.secretRepo.GetBySignalGroupID(r.Context(), groupID)
	if errors.Is(err, database.ErrEncryptedSecretNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "No encrypted secret found for this group")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get encrypted secret", "secret_update", "get_secret")
		return
	}

	// Determine scope from the signal group and verify admin access
	regionID, schoolID, districtID, adminCount, scopeErr := h.resolveGroupScope(w, r, claims, groupID)
	if scopeErr != nil {
		return // resolveGroupScope writes its own error response
	}

	votesNeeded := h.consensusConfig.RequiredVotes(adminCount)
	if adminCount < h.consensusConfig.VoteFloor {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"error":           "insufficient_admins",
			"message":         "Not enough admins to update secrets",
			"admin_count":     adminCount,
			"admins_required": h.consensusConfig.VoteFloor,
		})
		return
	}

	// Create proposal with admin-scoped wrapped keys
	proposal := &models.SecretUpdateProposal{
		EncryptedSecretID: secret.ID,
		RegionID:          regionID,
		SchoolID:          schoolID,
		DistrictID:        districtID,
		ProposedBy:        claims.UserID,
		EncryptedPayload:  req.EncryptedPayload,
		EncryptionIV:      req.EncryptionIV,
		Reason:            req.Reason,
	}

	err = h.db.Transaction(r.Context(), func(tx *sql.Tx) error {
		existing, txErr := h.proposalRepo.GetPendingBySecretForUpdate(r.Context(), tx, secret.ID)
		if txErr != nil {
			return txErr
		}
		if existing != nil {
			return database.ErrPendingExists
		}
		if txErr := h.proposalRepo.CreateTx(r.Context(), tx, proposal, req.WrappedKeys); txErr != nil {
			return txErr
		}
		// Auto-vote for proposer
		return h.proposalRepo.AddVoteTx(r.Context(), tx, proposal.ID, claims.UserID, true)
	})

	if errors.Is(err, database.ErrPendingExists) {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":   "pending_proposal_exists",
			"message": "A pending secret update proposal already exists for this secret",
		})
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to create proposal", "secret_update", "create_proposal")
		return
	}

	if h.auditRepo != nil {
		resourceType := "secret_update_proposal"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalCreated, &resourceType, &proposal.ID, map[string]interface{}{
			"encrypted_secret_id": secret.ID,
			"reason":              req.Reason,
		}), "secret_proposal_created")
	}

	writeJSON(w, http.StatusCreated, models.SecretProposalResponse{
		ProposalID:        proposal.ID,
		EncryptedSecretID: secret.ID,
		Status:            string(proposal.Status),
		VotesNeeded:       votesNeeded,
		CurrentVotes:      1,
		ExpiresAt:         proposal.ExpiresAt,
	})
}

// Vote handles POST /api/v1/secret-proposals/:id/vote
func (h *SecretUpdateHandler) Vote(w http.ResponseWriter, r *http.Request) {
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

	// Get proposal for scope check
	proposal, err := h.proposalRepo.GetByID(r.Context(), proposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Proposal not found")
		return
	}

	// Verify admin access to the scope
	adminCount, scopeErr := h.verifyAdminAccess(w, r, claims, proposal.RegionID, proposal.SchoolID, proposal.DistrictID)
	if scopeErr != nil {
		return
	}
	votesNeeded := h.consensusConfig.RequiredVotes(adminCount)

	// Vote within transaction
	var voteCount int
	var consensusReached bool

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
			return h.proposalRepo.UpdateStatusTx(r.Context(), tx, proposalID, models.ProposalStatusApprovedPendingFinalization)
		}
		return nil
	})

	if errors.Is(err, database.ErrProposalClosed) {
		writeError(w, http.StatusConflict, "proposal_closed", "This proposal is no longer open for voting")
		return
	}
	if errors.Is(err, database.ErrAlreadyVoted) {
		writeError(w, http.StatusConflict, "already_voted", "You have already voted on this proposal")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to record vote", "secret_update", "record_vote")
		return
	}

	if h.auditRepo != nil {
		resourceType := "secret_update_proposal"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalVoted, &resourceType, &proposalID, map[string]interface{}{
			"vote": req.Vote,
		}), "secret_proposal_voted")
	}

	response := models.SecretProposalResponse{
		ProposalID:   proposalID,
		YourVote:     &req.Vote,
		CurrentVotes: voteCount,
		VotesNeeded:  votesNeeded,
		Status:       string(models.ProposalStatusPending),
	}

	if consensusReached {
		if h.auditRepo != nil {
			resourceType := "secret_update_proposal"
			logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalApproved, &resourceType, &proposalID, nil), "secret_proposal_approved")
		}
		response.Status = string(models.ProposalStatusApprovedPendingFinalization)
		response.ConsensusReached = true
		response.Message = "Consensus reached. Finalize the update by re-encrypting for all members."
	}

	writeJSON(w, http.StatusOK, response)
}

// Finalize handles POST /api/v1/encrypted-secrets/:id/finalize
func (h *SecretUpdateHandler) Finalize(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	secretID := getPathParam(r, "id")
	if secretID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Secret ID required")
		return
	}

	var req models.FinalizeSecretUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.EncryptedPayload == "" || req.EncryptionIV == "" || len(req.WrappedKeys) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "Encrypted payload, IV, and wrapped keys are required")
		return
	}

	if err := h.secretRepo.UpdatePayloadAndKeys(r.Context(), secretID, req.EncryptedPayload, req.EncryptionIV, claims.UserID, req.WrappedKeys); err != nil {
		writeServerError(w, r, err, "Failed to finalize secret update", "secret_update", "finalize")
		return
	}

	if h.auditRepo != nil {
		resourceType := "encrypted_secret"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionInviteLinkUpdated, &resourceType, &secretID, nil), "secret_finalized")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"secret_id": secretID,
		"status":    "finalized",
		"message":   "Secret updated and encrypted for all members",
	})
}

// GetProposal handles GET /api/v1/secret-proposals/:id
func (h *SecretUpdateHandler) GetProposal(w http.ResponseWriter, r *http.Request) {
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

	proposal, err := h.proposalRepo.GetByIDWithVotes(r.Context(), proposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Proposal not found")
		return
	}

	adminCount := h.consensusConfig.VoteFloor
	proposal.VotesNeeded = h.consensusConfig.RequiredVotes(adminCount)

	wrappedDEK, dErr := h.proposalRepo.GetWrappedDEK(r.Context(), proposalID, claims.UserID)
	if dErr == nil {
		proposal.WrappedDEK = wrappedDEK
	}

	proposal.HasVoted = false
	for _, vote := range proposal.Votes {
		if vote.VoterID == claims.UserID {
			proposal.HasVoted = true
			break
		}
	}
	proposal.CanVote = !proposal.HasVoted && proposal.Status == string(models.ProposalStatusPending)

	writeJSON(w, http.StatusOK, proposal)
}

// ListProposals handles GET /api/v1/secret-proposals
func (h *SecretUpdateHandler) ListProposals(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	filter := models.SecretProposalListFilter{
		Status:            r.URL.Query().Get("status"),
		EncryptedSecretID: r.URL.Query().Get("encrypted_secret_id"),
		RegionID:          r.URL.Query().Get("region_id"),
		SchoolID:          r.URL.Query().Get("school_id"),
		DistrictID:        r.URL.Query().Get("district_id"),
	}

	proposals, err := h.proposalRepo.List(r.Context(), claims.UserID, claims.IsSuperuser, filter)
	if err != nil {
		writeServerError(w, r, err, "Failed to list proposals", "secret_update", "list_proposals")
		return
	}

	for _, p := range proposals {
		p.VotesNeeded = h.consensusConfig.RequiredVotes(h.consensusConfig.VoteFloor)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"proposals": proposals,
	})
}

// ExpireProposal handles POST /api/v1/secret-proposals/:id/expire
func (h *SecretUpdateHandler) ExpireProposal(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusNotFound, "not_found", "Proposal not found")
		return
	}

	if proposal.Status != models.ProposalStatusPending && proposal.Status != models.ProposalStatusApprovedPendingFinalization {
		writeError(w, http.StatusConflict, "proposal_closed", "Only pending proposals can be expired")
		return
	}

	if err := h.proposalRepo.UpdateStatus(r.Context(), proposalID, models.ProposalStatusExpired); err != nil {
		writeServerError(w, r, err, "Failed to expire proposal", "secret_update", "expire_proposal")
		return
	}

	if h.auditRepo != nil {
		resourceType := "secret_update_proposal"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionProposalExpired, &resourceType, &proposalID, map[string]interface{}{
			"expired_by": "superuser",
		}), "secret_proposal_expired")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"proposal_id": proposalID,
		"status":      "expired",
		"message":     "Proposal has been expired",
	})
}

// resolveGroupScope determines the scope of a signal group and verifies admin access
func (h *SecretUpdateHandler) resolveGroupScope(w http.ResponseWriter, r *http.Request, claims *middleware.Claims, groupID string) (regionID, schoolID, districtID *string, adminCount int, err error) {
	query := `SELECT region_id, school_id, district_id FROM signal_groups WHERE id = ? AND is_active = TRUE`
	var rID, sID, dID sql.NullString
	queryErr := h.db.QueryRowContext(r.Context(), query, groupID).Scan(&rID, &sID, &dID)
	if queryErr != nil {
		writeError(w, http.StatusNotFound, "not_found", "Signal group not found")
		return nil, nil, nil, 0, queryErr
	}

	if rID.Valid {
		regionID = &rID.String
		adminCount, err = h.verifyRegionAdmin(w, r, claims, rID.String)
	} else if sID.Valid {
		schoolID = &sID.String
		adminCount, err = h.verifySchoolAdmin(w, r, claims, sID.String)
	} else if dID.Valid {
		districtID = &dID.String
		adminCount, err = h.verifyDistrictAdmin(w, r, claims, dID.String)
	}

	return
}

// verifyAdminAccess verifies the user has admin access to the proposal's scope
func (h *SecretUpdateHandler) verifyAdminAccess(w http.ResponseWriter, r *http.Request, claims *middleware.Claims, regionID, schoolID, districtID *string) (int, error) {
	if regionID != nil {
		return h.verifyRegionAdmin(w, r, claims, *regionID)
	}
	if schoolID != nil {
		return h.verifySchoolAdmin(w, r, claims, *schoolID)
	}
	if districtID != nil {
		return h.verifyDistrictAdmin(w, r, claims, *districtID)
	}
	writeError(w, http.StatusBadRequest, "invalid_scope", "Proposal has no valid scope")
	return 0, errors.New("no scope")
}

// verifyRegionAdmin checks if user is admin of a region and returns admin count
func (h *SecretUpdateHandler) verifyRegionAdmin(w http.ResponseWriter, r *http.Request, claims *middleware.Claims, regionID string) (int, error) {
	if !claims.IsSuperuser {
		isAdmin, err := h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, regionID)
		if err != nil {
			writeServerError(w, r, err, "Failed to verify admin status", "secret_update", "verify_admin")
			return 0, err
		}
		if !isAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "Admin access required")
			return 0, errors.New("not admin")
		}
	}
	adminCount, err := h.regionRepo.GetAdminCount(r.Context(), regionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to count admins", "secret_update", "count_admins")
		return 0, err
	}
	return adminCount, nil
}

// verifySchoolAdmin checks if user is admin of a school and returns admin count
func (h *SecretUpdateHandler) verifySchoolAdmin(w http.ResponseWriter, r *http.Request, claims *middleware.Claims, schoolID string) (int, error) {
	if !claims.IsSuperuser {
		userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, schoolID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check school membership", "secret_update", "check_school_admin")
			return 0, err
		}
		if userSchool == nil || !userSchool.IsAdmin || userSchool.VerificationStatus != models.SchoolVerificationStatusVerified {
			writeError(w, http.StatusForbidden, "forbidden", "School admin access required")
			return 0, errors.New("not school admin")
		}
	}
	adminCount, err := h.schoolRepo.GetVerifiedAdminCount(r.Context(), schoolID)
	if err != nil {
		writeServerError(w, r, err, "Failed to count school admins", "secret_update", "count_school_admins")
		return 0, err
	}
	return adminCount, nil
}

// verifyDistrictAdmin checks if user is admin of a school in the district
func (h *SecretUpdateHandler) verifyDistrictAdmin(w http.ResponseWriter, r *http.Request, claims *middleware.Claims, districtID string) (int, error) {
	if !claims.IsSuperuser {
		schools, err := h.schoolRepo.ListByDistrict(r.Context(), districtID)
		if err != nil {
			writeServerError(w, r, err, "Failed to list district schools", "secret_update", "list_district_schools")
			return 0, err
		}
		isDistrictAdmin := false
		for _, school := range schools {
			userSchool, sErr := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, school.ID)
			if sErr != nil {
				continue
			}
			if userSchool != nil && userSchool.IsAdmin && userSchool.VerificationStatus == models.SchoolVerificationStatusVerified {
				isDistrictAdmin = true
				break
			}
		}
		if !isDistrictAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "District admin access required")
			return 0, errors.New("not district admin")
		}
	}
	adminCount := h.consensusConfig.VoteFloor
	return adminCount, nil
}
