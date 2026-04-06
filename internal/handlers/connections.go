package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// ConnectionHandler handles connection endpoints.
type ConnectionHandler struct {
	connectionRepo *database.ConnectionRepository
	groupRepo      *database.GroupRepository
	auditRepo      *database.AuditRepository
}

// NewConnectionHandler creates a new connection handler.
func NewConnectionHandler(
	connectionRepo *database.ConnectionRepository,
	groupRepo *database.GroupRepository,
	auditRepo *database.AuditRepository,
) *ConnectionHandler {
	return &ConnectionHandler{
		connectionRepo: connectionRepo,
		groupRepo:      groupRepo,
		auditRepo:      auditRepo,
	}
}

// ProposeConnection handles POST /api/v1/connections
func (h *ConnectionHandler) ProposeConnection(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req models.ProposeConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if len(req.GroupIDs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "At least one target group ID is required")
		return
	}

	// The proposer group is derived from the request — need to know which group the user is proposing from.
	// We'll use a "proposer_group_id" field in the query params or require it in the body.
	proposerGroupID := r.URL.Query().Get("proposer_group_id")
	if proposerGroupID == "" {
		writeError(w, http.StatusBadRequest, "missing_proposer_group", "proposer_group_id query parameter required")
		return
	}

	// Verify user is admin of proposer group
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), proposerGroupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check admin status", "connection", "propose")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "Must be admin of the proposing group")
		return
	}

	// Validate all target groups exist
	for _, targetGroupID := range req.GroupIDs {
		_, getErr := h.groupRepo.GetByID(r.Context(), targetGroupID)
		if getErr != nil {
			if errors.Is(getErr, database.ErrGroupNotFound) {
				writeError(w, http.StatusNotFound, "group_not_found", "Target group not found: "+targetGroupID)
				return
			}
			writeServerError(w, r, getErr, "Failed to validate target group", "connection", "propose")
			return
		}
	}

	proposal, err := h.connectionRepo.ProposeConnection(r.Context(), proposerGroupID, &req)
	if err != nil {
		writeServerError(w, r, err, "Failed to propose connection", "connection", "propose")
		return
	}

	// Audit log
	resourceType := "connection_proposal"
	logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionConnectionProposed,
		&resourceType, &proposal.ID, map[string]interface{}{
			"proposer_group_id": proposerGroupID,
			"target_group_ids":  req.GroupIDs,
		}), models.AuditActionConnectionProposed)

	writeJSON(w, http.StatusCreated, proposal)
}

// RespondToProposal handles POST /api/v1/connection-proposals/{id}/respond
func (h *ConnectionHandler) RespondToProposal(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	proposalID := r.URL.Query().Get("id")
	if proposalID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Proposal ID required")
		return
	}

	var req struct {
		Accept bool   `json:"accept"`
		GroupID string `json:"group_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.GroupID == "" {
		writeError(w, http.StatusBadRequest, "missing_group_id", "group_id is required")
		return
	}

	// Verify user is admin of responding group
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), req.GroupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check admin status", "connection", "respond")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "Must be admin of the responding group")
		return
	}

	result, err := h.connectionRepo.RespondToProposal(r.Context(), proposalID, req.GroupID, req.Accept)
	if err != nil {
		if errors.Is(err, database.ErrConnectionProposalNotFound) {
			writeError(w, http.StatusNotFound, "proposal_not_found", "Proposal not found")
			return
		}
		if errors.Is(err, database.ErrProposalNotPending) {
			writeError(w, http.StatusConflict, "proposal_not_pending", "Proposal is no longer pending")
			return
		}
		if errors.Is(err, database.ErrGroupNotInProposal) {
			writeError(w, http.StatusForbidden, "not_in_proposal", "Group is not part of this proposal")
			return
		}
		if errors.Is(err, database.ErrAlreadyResponded) {
			writeError(w, http.StatusConflict, "already_responded", "Group has already responded")
			return
		}
		writeServerError(w, r, err, "Failed to respond to proposal", "connection", "respond")
		return
	}

	if result.Status == "accepted" {
		resourceType := "connection"
		connectionID := ""
		if result.ConnectionID != nil {
			connectionID = *result.ConnectionID
		}
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionConnectionAccepted,
			&resourceType, &connectionID, map[string]interface{}{
				"proposal_id": proposalID,
			}), models.AuditActionConnectionAccepted)
	}

	writeJSON(w, http.StatusOK, result)
}

// ListMyConnections handles GET /api/v1/connections
func (h *ConnectionHandler) ListMyConnections(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Get user's admin groups
	adminGroups, err := h.groupRepo.ListByUser(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list user groups", "connection", "list")
		return
	}

	// Collect connections from all admin groups, deduplicating
	seenConnectionIDs := make(map[string]bool)
	var allConnections []models.ConnectionWithDetails

	for _, group := range adminGroups {
		if !group.IsUserAdmin {
			continue
		}
		connections, listErr := h.connectionRepo.ListConnectionsForGroup(r.Context(), group.ID)
		if listErr != nil {
			writeServerError(w, r, listErr, "Failed to list connections", "connection", "list")
			return
		}
		for _, conn := range connections {
			if !seenConnectionIDs[conn.ID] {
				seenConnectionIDs[conn.ID] = true
				allConnections = append(allConnections, conn)
			}
		}
	}

	if allConnections == nil {
		allConnections = []models.ConnectionWithDetails{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"connections": allConnections,
	})
}

// GetConnection handles GET /api/v1/connections/{id}
func (h *ConnectionHandler) GetConnection(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	connectionID := r.URL.Query().Get("id")
	if connectionID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Connection ID required")
		return
	}

	connection, err := h.connectionRepo.GetConnection(r.Context(), connectionID)
	if err != nil {
		if errors.Is(err, database.ErrConnectionNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Connection not found")
			return
		}
		writeServerError(w, r, err, "Failed to get connection", "connection", "get")
		return
	}

	// Verify user is admin of at least one member group
	hasAccess := false
	for _, member := range connection.MemberGroups {
		isAdmin, adminErr := h.groupRepo.IsUserAdmin(r.Context(), member.GroupID, claims.UserID)
		if adminErr != nil {
			writeServerError(w, r, adminErr, "Failed to check admin status", "connection", "get")
			return
		}
		if isAdmin {
			hasAccess = true
			break
		}
	}

	if !hasAccess {
		writeError(w, http.StatusForbidden, "forbidden", "Must be admin of a member group")
		return
	}

	writeJSON(w, http.StatusOK, connection)
}

// InviteToConnection handles POST /api/v1/connections/{id}/invite
func (h *ConnectionHandler) InviteToConnection(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	connectionID := r.URL.Query().Get("id")
	if connectionID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Connection ID required")
		return
	}

	var req models.InviteToConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.GroupID == "" {
		writeError(w, http.StatusBadRequest, "missing_group_id", "group_id is required")
		return
	}

	// Determine which member group the user is admin of
	proposerGroupID := r.URL.Query().Get("proposer_group_id")
	if proposerGroupID == "" {
		writeError(w, http.StatusBadRequest, "missing_proposer_group", "proposer_group_id query parameter required")
		return
	}

	// Verify user is admin of proposer group
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), proposerGroupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check admin status", "connection", "invite")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "Must be admin of a member group")
		return
	}

	// Validate target group exists
	_, err = h.groupRepo.GetByID(r.Context(), req.GroupID)
	if err != nil {
		if errors.Is(err, database.ErrGroupNotFound) {
			writeError(w, http.StatusNotFound, "group_not_found", "Target group not found")
			return
		}
		writeServerError(w, r, err, "Failed to validate target group", "connection", "invite")
		return
	}

	proposal, err := h.connectionRepo.InviteToConnection(r.Context(), connectionID, proposerGroupID, req.GroupID)
	if err != nil {
		if errors.Is(err, database.ErrNotConnectionMember) {
			writeError(w, http.StatusForbidden, "not_member", "Proposer group is not a member of this connection")
			return
		}
		if errors.Is(err, database.ErrAlreadyConnected) {
			writeError(w, http.StatusConflict, "already_connected", "Target group is already a member")
			return
		}
		writeServerError(w, r, err, "Failed to invite to connection", "connection", "invite")
		return
	}

	resourceType := "connection_proposal"
	logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionConnectionProposed,
		&resourceType, &proposal.ID, map[string]interface{}{
			"connection_id":  connectionID,
			"target_group_id": req.GroupID,
			"proposal_type":  "expansion",
		}), models.AuditActionConnectionProposed)

	writeJSON(w, http.StatusCreated, proposal)
}

// LeaveConnection handles POST /api/v1/connections/{id}/leave
func (h *ConnectionHandler) LeaveConnection(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	connectionID := r.URL.Query().Get("id")
	if connectionID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Connection ID required")
		return
	}

	groupID := r.URL.Query().Get("group_id")
	if groupID == "" {
		writeError(w, http.StatusBadRequest, "missing_group_id", "group_id query parameter required")
		return
	}

	// Verify user is admin of the leaving group
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), groupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check admin status", "connection", "leave")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "Must be admin of the leaving group")
		return
	}

	err = h.connectionRepo.LeaveConnection(r.Context(), connectionID, groupID)
	if err != nil {
		if errors.Is(err, database.ErrNotConnectionMember) {
			writeError(w, http.StatusNotFound, "not_member", "Group is not a member of this connection")
			return
		}
		writeServerError(w, r, err, "Failed to leave connection", "connection", "leave")
		return
	}

	resourceType := "connection"
	logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionConnectionLeft,
		&resourceType, &connectionID, map[string]interface{}{
			"group_id": groupID,
		}), models.AuditActionConnectionLeft)

	// After leaving, check unanimous block for remaining members
	connection, getErr := h.connectionRepo.GetConnection(r.Context(), connectionID)
	if getErr == nil && connection != nil {
		for _, member := range connection.MemberGroups {
			shouldRemove, checkErr := h.connectionRepo.CheckUnanimousBlock(r.Context(), connectionID, member.GroupID)
			if checkErr == nil && shouldRemove {
				_ = h.connectionRepo.LeaveConnection(r.Context(), connectionID, member.GroupID)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Left connection successfully"})
}

// ListPendingProposals handles GET /api/v1/connection-proposals
func (h *ConnectionHandler) ListPendingProposals(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Get user's admin groups
	adminGroups, err := h.groupRepo.ListByUser(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list user groups", "connection", "list_proposals")
		return
	}

	// Collect pending proposals from all admin groups, deduplicating
	seenProposalIDs := make(map[string]bool)
	var allProposals []models.ConnectionProposalWithGroups

	for _, group := range adminGroups {
		if !group.IsUserAdmin {
			continue
		}
		proposals, listErr := h.connectionRepo.ListPendingProposalsForGroup(r.Context(), group.ID)
		if listErr != nil {
			writeServerError(w, r, listErr, "Failed to list proposals", "connection", "list_proposals")
			return
		}
		for _, proposal := range proposals {
			if !seenProposalIDs[proposal.ID] {
				seenProposalIDs[proposal.ID] = true
				allProposals = append(allProposals, proposal)
			}
		}
	}

	if allProposals == nil {
		allProposals = []models.ConnectionProposalWithGroups{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"proposals": allProposals,
	})
}
