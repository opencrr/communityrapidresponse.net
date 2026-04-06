package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

var (
	ErrConnectionNotFound         = errors.New("connection not found")
	ErrConnectionProposalNotFound = errors.New("connection proposal not found")
	ErrNotConnectionMember        = errors.New("group is not a member of this connection")
	ErrAlreadyConnected           = errors.New("group is already a member of this connection")
	ErrAlreadyResponded           = errors.New("group has already responded to this proposal")
	ErrProposalNotPending         = errors.New("proposal is not pending")
	ErrGroupNotInProposal         = errors.New("group is not part of this proposal")
)

// ConnectionRepository handles connection database operations.
type ConnectionRepository struct {
	db *DB
}

// NewConnectionRepository creates a new connection repository.
func NewConnectionRepository(db *DB) *ConnectionRepository {
	return &ConnectionRepository{db: db}
}

// ProposeConnection creates a formation proposal for a new connection between groups.
func (r *ConnectionRepository) ProposeConnection(ctx context.Context, proposerGroupID string, req *models.ProposeConnectionRequest) (*models.ConnectionProposalWithGroups, error) {
	proposalID := uuid.New().String()
	now := time.Now().UTC()
	expiresAt := now.Add(7 * 24 * time.Hour)

	var proposalName *string
	if req.Name != "" {
		proposalName = &req.Name
	}

	proposal := &models.ConnectionProposalWithGroups{
		ConnectionProposal: models.ConnectionProposal{
			ID:              proposalID,
			ConnectionID:    nil,
			ProposerGroupID: proposerGroupID,
			ProposalType:    "formation",
			Status:          "pending",
			CreatedAt:       now,
			ExpiresAt:       &expiresAt,
		},
	}

	err := r.db.Transaction(ctx, func(tx *sql.Tx) error {
		// Insert the proposal (store the name in the connection later, but we need it in memory)
		proposalQuery := `
			INSERT INTO connection_proposals (id, connection_id, proposer_group_id, proposal_type, status, created_at, expires_at)
			VALUES (?, NULL, ?, 'formation', 'pending', ?, ?)
		`
		_, err := tx.ExecContext(ctx, proposalQuery, proposalID, proposerGroupID, now, expiresAt)
		if err != nil {
			return fmt.Errorf("insert connection proposal: %w", err)
		}

		// Create proposal group entry for proposer (auto-accepted)
		proposerEntryID := uuid.New().String()
		proposerEntry := models.ConnectionProposalGroup{
			ID:          proposerEntryID,
			ProposalID:  proposalID,
			GroupID:     proposerGroupID,
			Status:      "accepted",
			RespondedAt: &now,
		}
		_, err = tx.ExecContext(ctx,
			"INSERT INTO connection_proposal_groups (id, proposal_id, group_id, status, responded_at) VALUES (?, ?, ?, 'accepted', ?)",
			proposerEntryID, proposalID, proposerGroupID, now,
		)
		if err != nil {
			return fmt.Errorf("insert proposer group entry: %w", err)
		}
		proposal.Groups = append(proposal.Groups, proposerEntry)

		// Create proposal group entries for target groups (pending)
		for _, targetGroupID := range req.GroupIDs {
			if targetGroupID == proposerGroupID {
				continue // skip if proposer is in the list
			}
			entryID := uuid.New().String()
			entry := models.ConnectionProposalGroup{
				ID:         entryID,
				ProposalID: proposalID,
				GroupID:    targetGroupID,
				Status:     "pending",
			}
			_, err = tx.ExecContext(ctx,
				"INSERT INTO connection_proposal_groups (id, proposal_id, group_id, status) VALUES (?, ?, ?, 'pending')",
				entryID, proposalID, targetGroupID,
			)
			if err != nil {
				return fmt.Errorf("insert target group entry: %w", err)
			}
			proposal.Groups = append(proposal.Groups, entry)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Store the name in memory for the response (it will be used when connection is formed)
	_ = proposalName

	return proposal, nil
}

// RespondToProposal handles a group's accept/decline response to a connection proposal.
func (r *ConnectionRepository) RespondToProposal(ctx context.Context, proposalID, groupID string, accept bool) (*models.ConnectionProposalWithGroups, error) {
	var result *models.ConnectionProposalWithGroups

	err := r.db.Transaction(ctx, func(tx *sql.Tx) error {
		// Load the proposal
		var proposal models.ConnectionProposal
		err := tx.QueryRowContext(ctx,
			"SELECT id, connection_id, proposer_group_id, proposal_type, status, created_at, expires_at FROM connection_proposals WHERE id = ?",
			proposalID,
		).Scan(&proposal.ID, &proposal.ConnectionID, &proposal.ProposerGroupID, &proposal.ProposalType, &proposal.Status, &proposal.CreatedAt, &proposal.ExpiresAt)
		if err == sql.ErrNoRows {
			return ErrConnectionProposalNotFound
		}
		if err != nil {
			return fmt.Errorf("get proposal: %w", err)
		}

		if proposal.Status != "pending" {
			return ErrProposalNotPending
		}

		// Check expiration
		if proposal.ExpiresAt != nil && time.Now().UTC().After(*proposal.ExpiresAt) {
			_, _ = tx.ExecContext(ctx, "UPDATE connection_proposals SET status = 'expired' WHERE id = ?", proposalID)
			return ErrProposalNotPending
		}

		// Verify group is part of this proposal
		var groupEntryID string
		var groupStatus string
		err = tx.QueryRowContext(ctx,
			"SELECT id, status FROM connection_proposal_groups WHERE proposal_id = ? AND group_id = ?",
			proposalID, groupID,
		).Scan(&groupEntryID, &groupStatus)
		if err == sql.ErrNoRows {
			return ErrGroupNotInProposal
		}
		if err != nil {
			return fmt.Errorf("get group entry: %w", err)
		}

		if groupStatus != "pending" {
			return ErrAlreadyResponded
		}

		// Update group's response
		now := time.Now().UTC()
		newStatus := "declined"
		if accept {
			newStatus = "accepted"
		}
		_, err = tx.ExecContext(ctx,
			"UPDATE connection_proposal_groups SET status = ?, responded_at = ? WHERE id = ?",
			newStatus, now, groupEntryID,
		)
		if err != nil {
			return fmt.Errorf("update group response: %w", err)
		}

		if !accept {
			// Decline the whole proposal
			_, err = tx.ExecContext(ctx, "UPDATE connection_proposals SET status = 'declined' WHERE id = ?", proposalID)
			if err != nil {
				return fmt.Errorf("decline proposal: %w", err)
			}
			proposal.Status = "declined"
		} else {
			// Check if all groups have accepted
			var pendingCount int
			err = tx.QueryRowContext(ctx,
				"SELECT COUNT(*) FROM connection_proposal_groups WHERE proposal_id = ? AND status = 'pending'",
				proposalID,
			).Scan(&pendingCount)
			if err != nil {
				return fmt.Errorf("count pending: %w", err)
			}

			if pendingCount == 0 {
				// All accepted — form or expand the connection
				if proposal.ProposalType == "formation" {
					connectionID, formErr := r.formConnection(ctx, tx, proposalID, proposal.ProposerGroupID)
					if formErr != nil {
						return formErr
					}
					proposal.ConnectionID = &connectionID
				} else {
					// expansion: add the new group(s) to existing connection
					if proposal.ConnectionID != nil {
						expandErr := r.expandConnection(ctx, tx, proposalID, *proposal.ConnectionID)
						if expandErr != nil {
							return expandErr
						}
					}
				}
				_, err = tx.ExecContext(ctx, "UPDATE connection_proposals SET status = 'accepted' WHERE id = ?", proposalID)
				if err != nil {
					return fmt.Errorf("accept proposal: %w", err)
				}
				proposal.Status = "accepted"
			}
		}

		// Load all group entries for the response
		groups, loadErr := r.loadProposalGroups(ctx, tx, proposalID)
		if loadErr != nil {
			return loadErr
		}

		result = &models.ConnectionProposalWithGroups{
			ConnectionProposal: proposal,
			Groups:             groups,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// formConnection creates a new connection from a fully-accepted formation proposal.
func (r *ConnectionRepository) formConnection(ctx context.Context, tx *sql.Tx, proposalID, proposerGroupID string) (string, error) {
	connectionID := uuid.New().String()
	now := time.Now().UTC()

	// Try to get the name from the proposer group's name (connection name is optional)
	_, err := tx.ExecContext(ctx,
		"INSERT INTO connections (id, name, created_at) VALUES (?, NULL, ?)",
		connectionID, now,
	)
	if err != nil {
		return "", fmt.Errorf("insert connection: %w", err)
	}

	// Update the proposal with the new connection ID
	_, err = tx.ExecContext(ctx,
		"UPDATE connection_proposals SET connection_id = ? WHERE id = ?",
		connectionID, proposalID,
	)
	if err != nil {
		return "", fmt.Errorf("update proposal connection_id: %w", err)
	}

	// Add all accepted groups as members
	rows, err := tx.QueryContext(ctx,
		"SELECT group_id FROM connection_proposal_groups WHERE proposal_id = ? AND status = 'accepted'",
		proposalID,
	)
	if err != nil {
		return "", fmt.Errorf("get accepted groups: %w", err)
	}
	defer rows.Close()

	var groupIDs []string
	for rows.Next() {
		var groupID string
		if scanErr := rows.Scan(&groupID); scanErr != nil {
			return "", fmt.Errorf("scan group id: %w", scanErr)
		}
		groupIDs = append(groupIDs, groupID)
	}
	if err = rows.Err(); err != nil {
		return "", fmt.Errorf("iterate group ids: %w", err)
	}

	for _, groupID := range groupIDs {
		memberID := uuid.New().String()
		_, err = tx.ExecContext(ctx,
			"INSERT INTO connection_members (id, connection_id, group_id, joined_at) VALUES (?, ?, ?, ?)",
			memberID, connectionID, groupID, now,
		)
		if err != nil {
			return "", fmt.Errorf("insert connection member: %w", err)
		}
	}

	return connectionID, nil
}

// expandConnection adds new groups from an accepted expansion proposal to an existing connection.
func (r *ConnectionRepository) expandConnection(ctx context.Context, tx *sql.Tx, proposalID, connectionID string) error {
	now := time.Now().UTC()

	// Find groups in the proposal that are not already members
	rows, err := tx.QueryContext(ctx, `
		SELECT cpg.group_id FROM connection_proposal_groups cpg
		WHERE cpg.proposal_id = ? AND cpg.status = 'accepted'
		AND NOT EXISTS (
			SELECT 1 FROM connection_members cm WHERE cm.connection_id = ? AND cm.group_id = cpg.group_id
		)
	`, proposalID, connectionID)
	if err != nil {
		return fmt.Errorf("find new groups: %w", err)
	}
	defer rows.Close()

	var newGroupIDs []string
	for rows.Next() {
		var groupID string
		if scanErr := rows.Scan(&groupID); scanErr != nil {
			return fmt.Errorf("scan new group id: %w", scanErr)
		}
		newGroupIDs = append(newGroupIDs, groupID)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate new group ids: %w", err)
	}

	for _, groupID := range newGroupIDs {
		memberID := uuid.New().String()
		_, err = tx.ExecContext(ctx,
			"INSERT INTO connection_members (id, connection_id, group_id, joined_at) VALUES (?, ?, ?, ?)",
			memberID, connectionID, groupID, now,
		)
		if err != nil {
			if strings.Contains(err.Error(), "Duplicate entry") {
				continue // already a member, skip
			}
			return fmt.Errorf("insert connection member: %w", err)
		}
	}

	return nil
}

// loadProposalGroups loads all connection_proposal_groups for a proposal within a transaction.
func (r *ConnectionRepository) loadProposalGroups(ctx context.Context, tx *sql.Tx, proposalID string) ([]models.ConnectionProposalGroup, error) {
	rows, err := tx.QueryContext(ctx,
		"SELECT id, proposal_id, group_id, status, responded_at FROM connection_proposal_groups WHERE proposal_id = ?",
		proposalID,
	)
	if err != nil {
		return nil, fmt.Errorf("load proposal groups: %w", err)
	}
	defer rows.Close()

	var groups []models.ConnectionProposalGroup
	for rows.Next() {
		var g models.ConnectionProposalGroup
		if scanErr := rows.Scan(&g.ID, &g.ProposalID, &g.GroupID, &g.Status, &g.RespondedAt); scanErr != nil {
			return nil, fmt.Errorf("scan proposal group: %w", scanErr)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// ListConnectionsForGroup returns all connections where the given group is a member.
func (r *ConnectionRepository) ListConnectionsForGroup(ctx context.Context, groupID string) ([]models.ConnectionWithDetails, error) {
	connectionRows, err := r.db.QueryContext(ctx, `
		SELECT c.id, c.name, c.created_at
		FROM connections c
		JOIN connection_members cm ON c.id = cm.connection_id
		WHERE cm.group_id = ?
		ORDER BY c.created_at DESC
	`, groupID)
	if err != nil {
		return nil, fmt.Errorf("list connections for group: %w", err)
	}
	defer connectionRows.Close()

	var connections []models.ConnectionWithDetails
	for connectionRows.Next() {
		var cwd models.ConnectionWithDetails
		if scanErr := connectionRows.Scan(&cwd.ID, &cwd.Name, &cwd.CreatedAt); scanErr != nil {
			return nil, fmt.Errorf("scan connection: %w", scanErr)
		}
		connections = append(connections, cwd)
	}
	if err = connectionRows.Err(); err != nil {
		return nil, err
	}

	// Load member groups for each connection
	for i := range connections {
		members, memberErr := r.getConnectionMembers(ctx, connections[i].ID)
		if memberErr != nil {
			return nil, memberErr
		}
		connections[i].MemberGroups = members
	}

	return connections, nil
}

// GetConnection returns a connection with its member groups.
func (r *ConnectionRepository) GetConnection(ctx context.Context, connectionID string) (*models.ConnectionWithDetails, error) {
	var cwd models.ConnectionWithDetails
	err := r.db.QueryRowContext(ctx,
		"SELECT id, name, created_at FROM connections WHERE id = ?",
		connectionID,
	).Scan(&cwd.ID, &cwd.Name, &cwd.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrConnectionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get connection: %w", err)
	}

	members, err := r.getConnectionMembers(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	cwd.MemberGroups = members

	return &cwd, nil
}

// getConnectionMembers returns the member groups for a connection.
func (r *ConnectionRepository) getConnectionMembers(ctx context.Context, connectionID string) ([]models.ConnectionMemberGroup, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT cm.group_id, g.name, cm.joined_at
		FROM connection_members cm
		JOIN `+"`groups`"+` g ON g.id = cm.group_id
		WHERE cm.connection_id = ?
		ORDER BY cm.joined_at
	`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("get connection members: %w", err)
	}
	defer rows.Close()

	var members []models.ConnectionMemberGroup
	for rows.Next() {
		var m models.ConnectionMemberGroup
		if scanErr := rows.Scan(&m.GroupID, &m.GroupName, &m.JoinedAt); scanErr != nil {
			return nil, fmt.Errorf("scan connection member: %w", scanErr)
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// InviteToConnection creates an expansion proposal to add a new group to an existing connection.
func (r *ConnectionRepository) InviteToConnection(ctx context.Context, connectionID, proposerGroupID, targetGroupID string) (*models.ConnectionProposalWithGroups, error) {
	// Verify proposer is a member
	isMember, err := r.isConnectionMember(ctx, connectionID, proposerGroupID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrNotConnectionMember
	}

	// Check if target is already a member
	isTargetMember, err := r.isConnectionMember(ctx, connectionID, targetGroupID)
	if err != nil {
		return nil, err
	}
	if isTargetMember {
		return nil, ErrAlreadyConnected
	}

	proposalID := uuid.New().String()
	now := time.Now().UTC()
	expiresAt := now.Add(7 * 24 * time.Hour)

	proposal := &models.ConnectionProposalWithGroups{
		ConnectionProposal: models.ConnectionProposal{
			ID:              proposalID,
			ConnectionID:    &connectionID,
			ProposerGroupID: proposerGroupID,
			ProposalType:    "expansion",
			Status:          "pending",
			CreatedAt:       now,
			ExpiresAt:       &expiresAt,
		},
	}

	err = r.db.Transaction(ctx, func(tx *sql.Tx) error {
		// Insert the proposal
		_, insertErr := tx.ExecContext(ctx,
			"INSERT INTO connection_proposals (id, connection_id, proposer_group_id, proposal_type, status, created_at, expires_at) VALUES (?, ?, ?, 'expansion', 'pending', ?, ?)",
			proposalID, connectionID, proposerGroupID, now, expiresAt,
		)
		if insertErr != nil {
			return fmt.Errorf("insert expansion proposal: %w", insertErr)
		}

		// Add proposer entry (auto-accepted)
		proposerEntryID := uuid.New().String()
		_, insertErr = tx.ExecContext(ctx,
			"INSERT INTO connection_proposal_groups (id, proposal_id, group_id, status, responded_at) VALUES (?, ?, ?, 'accepted', ?)",
			proposerEntryID, proposalID, proposerGroupID, now,
		)
		if insertErr != nil {
			return fmt.Errorf("insert proposer entry: %w", insertErr)
		}
		proposal.Groups = append(proposal.Groups, models.ConnectionProposalGroup{
			ID:          proposerEntryID,
			ProposalID:  proposalID,
			GroupID:     proposerGroupID,
			Status:      "accepted",
			RespondedAt: &now,
		})

		// Add all existing member groups (pending) except proposer
		memberRows, queryErr := tx.QueryContext(ctx,
			"SELECT group_id FROM connection_members WHERE connection_id = ? AND group_id != ?",
			connectionID, proposerGroupID,
		)
		if queryErr != nil {
			return fmt.Errorf("get existing members: %w", queryErr)
		}
		defer memberRows.Close()

		var existingMemberIDs []string
		for memberRows.Next() {
			var memberGroupID string
			if scanErr := memberRows.Scan(&memberGroupID); scanErr != nil {
				return fmt.Errorf("scan member id: %w", scanErr)
			}
			existingMemberIDs = append(existingMemberIDs, memberGroupID)
		}
		if rowsErr := memberRows.Err(); rowsErr != nil {
			return fmt.Errorf("iterate member ids: %w", rowsErr)
		}

		for _, memberGroupID := range existingMemberIDs {
			entryID := uuid.New().String()
			_, insertErr = tx.ExecContext(ctx,
				"INSERT INTO connection_proposal_groups (id, proposal_id, group_id, status) VALUES (?, ?, ?, 'pending')",
				entryID, proposalID, memberGroupID,
			)
			if insertErr != nil {
				return fmt.Errorf("insert existing member entry: %w", insertErr)
			}
			proposal.Groups = append(proposal.Groups, models.ConnectionProposalGroup{
				ID:         entryID,
				ProposalID: proposalID,
				GroupID:    memberGroupID,
				Status:     "pending",
			})
		}

		// Add target group (pending)
		targetEntryID := uuid.New().String()
		_, insertErr = tx.ExecContext(ctx,
			"INSERT INTO connection_proposal_groups (id, proposal_id, group_id, status) VALUES (?, ?, ?, 'pending')",
			targetEntryID, proposalID, targetGroupID,
		)
		if insertErr != nil {
			return fmt.Errorf("insert target entry: %w", insertErr)
		}
		proposal.Groups = append(proposal.Groups, models.ConnectionProposalGroup{
			ID:         targetEntryID,
			ProposalID: proposalID,
			GroupID:    targetGroupID,
			Status:     "pending",
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	return proposal, nil
}

// LeaveConnection removes a group from a connection. If only 1 or 0 remain, the connection is deleted.
func (r *ConnectionRepository) LeaveConnection(ctx context.Context, connectionID, groupID string) error {
	return r.db.Transaction(ctx, func(tx *sql.Tx) error {
		// Verify membership
		var memberCount int
		err := tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM connection_members WHERE connection_id = ? AND group_id = ?",
			connectionID, groupID,
		).Scan(&memberCount)
		if err != nil {
			return fmt.Errorf("check membership: %w", err)
		}
		if memberCount == 0 {
			return ErrNotConnectionMember
		}

		// Remove the member
		_, err = tx.ExecContext(ctx,
			"DELETE FROM connection_members WHERE connection_id = ? AND group_id = ?",
			connectionID, groupID,
		)
		if err != nil {
			return fmt.Errorf("delete member: %w", err)
		}

		// Count remaining members
		var remainingCount int
		err = tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM connection_members WHERE connection_id = ?",
			connectionID,
		).Scan(&remainingCount)
		if err != nil {
			return fmt.Errorf("count remaining: %w", err)
		}

		if remainingCount <= 1 {
			// Delete the connection (CASCADE will clean up members and proposals)
			_, err = tx.ExecContext(ctx, "DELETE FROM connections WHERE id = ?", connectionID)
			if err != nil {
				return fmt.Errorf("delete connection: %w", err)
			}
		} else {
			// Clean up pending proposals for this connection
			_, err = tx.ExecContext(ctx,
				"DELETE FROM connection_proposals WHERE connection_id = ? AND status = 'pending'",
				connectionID,
			)
			if err != nil {
				return fmt.Errorf("cleanup pending proposals: %w", err)
			}
		}

		return nil
	})
}

// CheckUnanimousBlock checks if ALL other member groups in the connection have blocked the given group.
func (r *ConnectionRepository) CheckUnanimousBlock(ctx context.Context, connectionID, groupID string) (bool, error) {
	// Get all other members in the connection
	rows, err := r.db.QueryContext(ctx,
		"SELECT group_id FROM connection_members WHERE connection_id = ? AND group_id != ?",
		connectionID, groupID,
	)
	if err != nil {
		return false, fmt.Errorf("get other members: %w", err)
	}
	defer rows.Close()

	var otherGroupIDs []string
	for rows.Next() {
		var otherGroupID string
		if scanErr := rows.Scan(&otherGroupID); scanErr != nil {
			return false, fmt.Errorf("scan other group id: %w", scanErr)
		}
		otherGroupIDs = append(otherGroupIDs, otherGroupID)
	}
	if err = rows.Err(); err != nil {
		return false, err
	}

	if len(otherGroupIDs) == 0 {
		return false, nil // no other members, can't be unanimously blocked
	}

	// Check if each other member has blocked this group
	for _, otherGroupID := range otherGroupIDs {
		var blocked bool
		err = r.db.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM group_blocks WHERE blocker_group_id = ? AND blocked_group_id = ?)",
			otherGroupID, groupID,
		).Scan(&blocked)
		if err != nil {
			return false, fmt.Errorf("check block: %w", err)
		}
		if !blocked {
			return false, nil // at least one member hasn't blocked
		}
	}

	return true, nil
}

// ListPendingProposalsForGroup returns pending proposals where the given group needs to respond.
func (r *ConnectionRepository) ListPendingProposalsForGroup(ctx context.Context, groupID string) ([]models.ConnectionProposalWithGroups, error) {
	proposalRows, err := r.db.QueryContext(ctx, `
		SELECT cp.id, cp.connection_id, cp.proposer_group_id, cp.proposal_type, cp.status, cp.created_at, cp.expires_at
		FROM connection_proposals cp
		JOIN connection_proposal_groups cpg ON cp.id = cpg.proposal_id
		WHERE cpg.group_id = ? AND cpg.status = 'pending' AND cp.status = 'pending'
		ORDER BY cp.created_at DESC
	`, groupID)
	if err != nil {
		return nil, fmt.Errorf("list pending proposals: %w", err)
	}
	defer proposalRows.Close()

	var proposals []models.ConnectionProposalWithGroups
	for proposalRows.Next() {
		var p models.ConnectionProposalWithGroups
		if scanErr := proposalRows.Scan(
			&p.ID, &p.ConnectionID, &p.ProposerGroupID, &p.ProposalType, &p.Status, &p.CreatedAt, &p.ExpiresAt,
		); scanErr != nil {
			return nil, fmt.Errorf("scan proposal: %w", scanErr)
		}
		proposals = append(proposals, p)
	}
	if err = proposalRows.Err(); err != nil {
		return nil, err
	}

	// Load group entries for each proposal
	for i := range proposals {
		groupRows, queryErr := r.db.QueryContext(ctx,
			"SELECT id, proposal_id, group_id, status, responded_at FROM connection_proposal_groups WHERE proposal_id = ?",
			proposals[i].ID,
		)
		if queryErr != nil {
			return nil, fmt.Errorf("load proposal groups: %w", queryErr)
		}

		var groups []models.ConnectionProposalGroup
		for groupRows.Next() {
			var g models.ConnectionProposalGroup
			if scanErr := groupRows.Scan(&g.ID, &g.ProposalID, &g.GroupID, &g.Status, &g.RespondedAt); scanErr != nil {
				groupRows.Close()
				return nil, fmt.Errorf("scan proposal group: %w", scanErr)
			}
			groups = append(groups, g)
		}
		groupRows.Close()
		if rowsErr := groupRows.Err(); rowsErr != nil {
			return nil, rowsErr
		}
		proposals[i].Groups = groups
	}

	return proposals, nil
}

// IsConnectionMember checks if a group is a member of the given connection.
func (r *ConnectionRepository) IsConnectionMember(ctx context.Context, connectionID, groupID string) (bool, error) {
	return r.isConnectionMember(ctx, connectionID, groupID)
}

// isConnectionMember checks if a group is a member of the given connection (internal).
func (r *ConnectionRepository) isConnectionMember(ctx context.Context, connectionID, groupID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM connection_members WHERE connection_id = ? AND group_id = ?)",
		connectionID, groupID,
	).Scan(&exists)
	return exists, err
}

// SetConnectionName updates the name of a connection.
func (r *ConnectionRepository) SetConnectionName(ctx context.Context, connectionID, name string) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE connections SET name = ? WHERE id = ?",
		name, connectionID,
	)
	return err
}
