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

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
	}

	// The proposer group is derived from the request — need to know which group the user is proposing from.
	// We'll use a "proposer_group_id" field in the query params or require it in the body.
	proposerGroupID := r.URL.Query().Get("proposer_group_id")
	if proposerGroupID == "" {
		writeError(w, http.StatusBadRequest, "missing_proposer_group", "proposer_group_id query parameter required")
		return
	}

	// Any member can propose connections (acceptance still requires consensus)
	isMember, err := h.groupRepo.IsUserMember(r.Context(), proposerGroupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check membership", "connection", "propose")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "forbidden", "Must be a member of the proposing group")
		return
	}

	// Validate all target groups exist and are not blocked
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

		// Check if either group has blocked the other
		blocked, blockErr := h.groupRepo.IsGroupBlocked(r.Context(), targetGroupID, proposerGroupID)
		if blockErr != nil {
			writeServerError(w, r, blockErr, "Failed to check block status", "connection", "propose")
			return
		}
		if blocked {
			writeError(w, http.StatusForbidden, "group_blocked", "Cannot propose connection to a group that has blocked your group")
			return
		}

		blockedReverse, blockRevErr := h.groupRepo.IsGroupBlocked(r.Context(), proposerGroupID, targetGroupID)
		if blockRevErr != nil {
			writeServerError(w, r, blockRevErr, "Failed to check block status", "connection", "propose")
			return
		}
		if blockedReverse {
			writeError(w, http.StatusForbidden, "group_blocked", "Cannot propose connection to a blocked group")
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

	// Any member of a connected group may view connection detail (name + member
	// roster) so the detail page can render. Admin-only content (admin_only chats
	// and resources) is filtered by its own endpoints. Non-members are rejected.
	hasAccess := false
	for _, member := range connection.MemberGroups {
		isMember, memberErr := h.groupRepo.IsUserMember(r.Context(), member.GroupID, claims.UserID)
		if memberErr != nil {
			writeServerError(w, r, memberErr, "Failed to check membership", "connection", "get")
			return
		}
		if isMember {
			hasAccess = true
			break
		}
	}

	if !hasAccess {
		writeError(w, http.StatusForbidden, "forbidden", "Must be a member of a connected group")
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

	// Mirror the formation-path block check across the whole membership: an
	// expansion must not pull in a group that is blocked by, or blocks, any
	// current connection member, otherwise blocking is bypassed via invitation.
	connection, err := h.connectionRepo.GetConnection(r.Context(), connectionID)
	if err != nil {
		if errors.Is(err, database.ErrConnectionNotFound) {
			writeError(w, http.StatusNotFound, "connection_not_found", "Connection not found")
			return
		}
		writeServerError(w, r, err, "Failed to load connection", "connection", "invite")
		return
	}
	for _, member := range connection.MemberGroups {
		blocked, blockErr := h.groupRepo.IsGroupBlocked(r.Context(), member.GroupID, req.GroupID)
		if blockErr != nil {
			writeServerError(w, r, blockErr, "Failed to check block status", "connection", "invite")
			return
		}
		blockedReverse, blockRevErr := h.groupRepo.IsGroupBlocked(r.Context(), req.GroupID, member.GroupID)
		if blockRevErr != nil {
			writeServerError(w, r, blockRevErr, "Failed to check block status", "connection", "invite")
			return
		}
		if blocked || blockedReverse {
			writeError(w, http.StatusForbidden, "group_blocked", "Cannot invite a group that is blocked by, or has blocked, a current connection member")
			return
		}
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

// ProposeSignalChat handles POST /api/v1/connections/{id}/signal-group-proposals
func (h *ConnectionHandler) ProposeSignalChat(w http.ResponseWriter, r *http.Request) {
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

	proposerGroupID := r.URL.Query().Get("proposer_group_id")
	if proposerGroupID == "" {
		writeError(w, http.StatusBadRequest, "missing_proposer_group", "proposer_group_id query parameter required")
		return
	}

	// Verify user is admin of proposer group
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), proposerGroupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check admin status", "connection", "propose_chat")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "Must be admin of a member group")
		return
	}

	var req models.ProposeConnectionChatRequest
	if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
	}

	proposal, err := h.connectionRepo.ProposeSignalChat(r.Context(), connectionID, proposerGroupID, &req)
	if err != nil {
		if errors.Is(err, database.ErrConnectionNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Connection not found")
			return
		}
		if errors.Is(err, database.ErrNotConnectionMember) {
			writeError(w, http.StatusForbidden, "not_member", "Group is not a member of this connection")
			return
		}
		if errors.Is(err, database.ErrConnectionSignalGroupLimitReached) {
			writeError(w, http.StatusConflict, "limit_reached", "Connection signal group limit reached (max 5)")
			return
		}
		writeServerError(w, r, err, "Failed to propose signal chat", "connection", "propose_chat")
		return
	}

	resourceType := "connection_chat_proposal"
	logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionConnectionChatProposed,
		&resourceType, &proposal.ID, map[string]interface{}{
			"connection_id":    connectionID,
			"proposer_group_id": proposerGroupID,
			"group_name":       req.GroupName,
			"access_level":     req.AccessLevel,
		}), models.AuditActionConnectionChatProposed)

	writeJSON(w, http.StatusCreated, proposal)
}

// VoteOnChatProposal handles POST /api/v1/connection-chat-proposals/{id}/vote
func (h *ConnectionHandler) VoteOnChatProposal(w http.ResponseWriter, r *http.Request) {
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
		Approve bool   `json:"approve"`
		GroupID string `json:"group_id"`
	}
	if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.GroupID == "" {
		writeError(w, http.StatusBadRequest, "missing_group_id", "group_id is required")
		return
	}

	// Verify user is admin of voting group
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), req.GroupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check admin status", "connection", "vote_chat")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "Must be admin of the voting group")
		return
	}

	result, err := h.connectionRepo.VoteOnChatProposal(r.Context(), proposalID, req.GroupID, req.Approve)
	if err != nil {
		if errors.Is(err, database.ErrChatProposalNotFound) {
			writeError(w, http.StatusNotFound, "proposal_not_found", "Chat proposal not found")
			return
		}
		if errors.Is(err, database.ErrChatProposalNotPending) {
			writeError(w, http.StatusConflict, "proposal_not_pending", "Chat proposal is no longer pending")
			return
		}
		if errors.Is(err, database.ErrChatProposalVoteNotFound) {
			writeError(w, http.StatusForbidden, "not_in_proposal", "Group is not part of this proposal")
			return
		}
		if errors.Is(err, database.ErrChatProposalAlreadyVoted) {
			writeError(w, http.StatusConflict, "already_voted", "Group has already voted")
			return
		}
		writeServerError(w, r, err, "Failed to vote on chat proposal", "connection", "vote_chat")
		return
	}

	resourceType := "connection_chat_proposal"
	logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionConnectionChatVoted,
		&resourceType, &proposalID, map[string]interface{}{
			"group_id": req.GroupID,
			"approve":  req.Approve,
		}), models.AuditActionConnectionChatVoted)

	writeJSON(w, http.StatusOK, result)
}

// ListConnectionSignalGroups handles GET /api/v1/connections/{id}/signal-groups
func (h *ConnectionHandler) ListConnectionSignalGroups(w http.ResponseWriter, r *http.Request) {
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

	// Get connection to verify access
	connection, err := h.connectionRepo.GetConnection(r.Context(), connectionID)
	if err != nil {
		if errors.Is(err, database.ErrConnectionNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Connection not found")
			return
		}
		writeServerError(w, r, err, "Failed to get connection", "connection", "list_signal_groups")
		return
	}

	// Any member of a connected group may list connection chats; admins of a
	// connected group additionally see admin_only chats. Non-members are rejected.
	hasAccess := false
	isAdminOfAny := false
	for _, member := range connection.MemberGroups {
		isAdmin, adminErr := h.groupRepo.IsUserAdmin(r.Context(), member.GroupID, claims.UserID)
		if adminErr != nil {
			writeServerError(w, r, adminErr, "Failed to check admin status", "connection", "list_signal_groups")
			return
		}
		if isAdmin {
			hasAccess = true
			isAdminOfAny = true
			break
		}
		isMember, memberErr := h.groupRepo.IsUserMember(r.Context(), member.GroupID, claims.UserID)
		if memberErr != nil {
			writeServerError(w, r, memberErr, "Failed to check membership", "connection", "list_signal_groups")
			return
		}
		if isMember {
			hasAccess = true
		}
	}

	if !hasAccess {
		writeError(w, http.StatusForbidden, "forbidden", "Must be a member of a connected group")
		return
	}

	groups, err := h.connectionRepo.ListConnectionSignalGroups(r.Context(), connectionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list signal groups", "connection", "list_signal_groups")
		return
	}

	// Non-admins only see all_members (open-tier) chats; admin_only chats are
	// withheld unless the caller is an admin of a connected group.
	filtered := make([]*models.SignalGroup, 0, len(groups))
	for _, g := range groups {
		if isAdminOfAny || g.AccessTier != models.AccessTierAdminOnly {
			filtered = append(filtered, g)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"signal_groups": filtered,
	})
}

// ListChatProposals handles GET /api/v1/connections/{id}/signal-group-proposals
func (h *ConnectionHandler) ListChatProposals(w http.ResponseWriter, r *http.Request) {
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

	// Get connection to verify access
	connection, err := h.connectionRepo.GetConnection(r.Context(), connectionID)
	if err != nil {
		if errors.Is(err, database.ErrConnectionNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Connection not found")
			return
		}
		writeServerError(w, r, err, "Failed to get connection", "connection", "list_chat_proposals")
		return
	}

	// Verify user is admin of at least one member group
	hasAccess := false
	for _, member := range connection.MemberGroups {
		isAdmin, adminErr := h.groupRepo.IsUserAdmin(r.Context(), member.GroupID, claims.UserID)
		if adminErr != nil {
			writeServerError(w, r, adminErr, "Failed to check admin status", "connection", "list_chat_proposals")
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

	proposals, err := h.connectionRepo.ListChatProposals(r.Context(), connectionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list chat proposals", "connection", "list_chat_proposals")
		return
	}

	if proposals == nil {
		proposals = []models.ConnectionChatProposalWithVotes{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"proposals": proposals,
	})
}

// ShareResource handles POST /api/v1/connections/{id}/shared-resources
func (h *ConnectionHandler) ShareResource(w http.ResponseWriter, r *http.Request) {
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

	proposerGroupID := r.URL.Query().Get("proposer_group_id")
	if proposerGroupID == "" {
		writeError(w, http.StatusBadRequest, "missing_proposer_group", "proposer_group_id query parameter required")
		return
	}

	// Verify user is admin of the owning group
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), proposerGroupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check admin status", "connection", "share_resource")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "Must be admin of the owning group")
		return
	}

	var req models.ShareResourceRequest
	if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if validationMsg := validateStruct(&req); validationMsg != "" {
		writeError(w, http.StatusBadRequest, "validation_error", validationMsg)
		return
	}

	shared, err := h.connectionRepo.ShareResource(r.Context(), connectionID, proposerGroupID, &req)
	if err != nil {
		if errors.Is(err, database.ErrNotConnectionMember) {
			writeError(w, http.StatusForbidden, "not_member", "Group is not a member of this connection")
			return
		}
		if errors.Is(err, database.ErrResourceNotInGroup) {
			writeError(w, http.StatusBadRequest, "resource_not_in_group", "Resource does not belong to the specified group")
			return
		}
		if errors.Is(err, database.ErrAlreadyShared) {
			writeError(w, http.StatusConflict, "already_shared", "Resource is already shared in this connection")
			return
		}
		writeServerError(w, r, err, "Failed to share resource", "connection", "share_resource")
		return
	}

	resourceType := "connection_shared_resource"
	logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionConnectionResourceShared,
		&resourceType, &shared.ID, map[string]interface{}{
			"connection_id": connectionID,
			"resource_id":   req.ResourceID,
			"group_id":      proposerGroupID,
			"visibility":    req.Visibility,
		}), models.AuditActionConnectionResourceShared)

	writeJSON(w, http.StatusCreated, shared)
}

// UnshareResource handles DELETE /api/v1/connections/{id}/shared-resources/{rid}
func (h *ConnectionHandler) UnshareResource(w http.ResponseWriter, r *http.Request) {
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

	resourceID := r.URL.Query().Get("resource_id")
	if resourceID == "" {
		writeError(w, http.StatusBadRequest, "missing_resource_id", "Resource ID required")
		return
	}

	// Look up the shared resource to find the owning group
	sharedResources, err := h.connectionRepo.ListConnectionResources(r.Context(), connectionID, true)
	if err != nil {
		writeServerError(w, r, err, "Failed to look up shared resource", "connection", "unshare_resource")
		return
	}

	var owningGroupID string
	for _, sr := range sharedResources {
		if sr.ResourceID == resourceID {
			owningGroupID = sr.SharedByGroupID
			break
		}
	}
	if owningGroupID == "" {
		writeError(w, http.StatusNotFound, "not_found", "Shared resource not found")
		return
	}

	// Verify user is admin of the owning group
	isAdmin, err := h.groupRepo.IsUserAdmin(r.Context(), owningGroupID, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check admin status", "connection", "unshare_resource")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "Must be admin of the owning group")
		return
	}

	err = h.connectionRepo.UnshareResource(r.Context(), connectionID, resourceID)
	if err != nil {
		if errors.Is(err, database.ErrConnectionNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Shared resource not found")
			return
		}
		writeServerError(w, r, err, "Failed to unshare resource", "connection", "unshare_resource")
		return
	}

	logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionConnectionResourceUnshared,
		nil, nil, map[string]interface{}{
			"connection_id": connectionID,
			"resource_id":   resourceID,
			"group_id":      owningGroupID,
		}), models.AuditActionConnectionResourceUnshared)

	writeJSON(w, http.StatusOK, map[string]string{"message": "Resource unshared successfully"})
}

// ListConnectionResources handles GET /api/v1/connections/{id}/shared-resources
func (h *ConnectionHandler) ListConnectionResources(w http.ResponseWriter, r *http.Request) {
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

	// Get connection to verify access
	connection, err := h.connectionRepo.GetConnection(r.Context(), connectionID)
	if err != nil {
		if errors.Is(err, database.ErrConnectionNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Connection not found")
			return
		}
		writeServerError(w, r, err, "Failed to get connection", "connection", "list_resources")
		return
	}

	// Any member of a connected group may view shared resources; admins of a
	// connected group additionally see admin_only resources. Non-members are
	// rejected. The repository filters to all_members-only when isAdminOfAny is
	// false.
	hasAccess := false
	isAdminOfAny := false
	for _, member := range connection.MemberGroups {
		isAdmin, adminErr := h.groupRepo.IsUserAdmin(r.Context(), member.GroupID, claims.UserID)
		if adminErr != nil {
			writeServerError(w, r, adminErr, "Failed to check admin status", "connection", "list_resources")
			return
		}
		if isAdmin {
			hasAccess = true
			isAdminOfAny = true
			break
		}
		isMember, memberErr := h.groupRepo.IsUserMember(r.Context(), member.GroupID, claims.UserID)
		if memberErr != nil {
			writeServerError(w, r, memberErr, "Failed to check membership", "connection", "list_resources")
			return
		}
		if isMember {
			hasAccess = true
		}
	}

	if !hasAccess {
		writeError(w, http.StatusForbidden, "forbidden", "Must be a member of a connected group")
		return
	}

	resources, err := h.connectionRepo.ListConnectionResources(r.Context(), connectionID, isAdminOfAny)
	if err != nil {
		writeServerError(w, r, err, "Failed to list shared resources", "connection", "list_resources")
		return
	}

	if resources == nil {
		resources = []models.ConnectionSharedResourceWithDetails{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"shared_resources": resources,
	})
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
