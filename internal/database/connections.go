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
	ErrAlreadyShared              = errors.New("resource already shared in this connection")
	ErrResourceNotInGroup         = errors.New("resource does not belong to the specified group")
)

// ConnectionRepository handles connection database operations.
type ConnectionRepository struct {
	db                  *DB
	encryptedSecretRepo *EncryptedSecretRepository
}

// NewConnectionRepository creates a new connection repository.
func NewConnectionRepository(db *DB, encryptedSecretRepo *EncryptedSecretRepository) *ConnectionRepository {
	return &ConnectionRepository{
		db:                  db,
		encryptedSecretRepo: encryptedSecretRepo,
	}
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
	defer func() { _ = rows.Close() }()

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
	defer func() { _ = rows.Close() }()

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
		// Re-check blocks at finalize time (TOCTOU): a current member may have
		// blocked this group, or vice-versa, after voting to accept. Skip rather
		// than add a group that is now block-incompatible with the connection.
		var blocked bool
		blockErr := tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM connection_members cm
				JOIN group_blocks gb
					ON (gb.blocker_group_id = cm.group_id AND gb.blocked_group_id = ?)
					OR (gb.blocker_group_id = ? AND gb.blocked_group_id = cm.group_id)
				WHERE cm.connection_id = ?
			)`,
			groupID, groupID, connectionID,
		).Scan(&blocked)
		if blockErr != nil {
			return fmt.Errorf("check expansion block: %w", blockErr)
		}
		if blocked {
			continue
		}

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
	defer func() { _ = rows.Close() }()

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
	defer func() { _ = connectionRows.Close() }()

	var connections []models.ConnectionWithDetails
	for connectionRows.Next() {
		var cwd models.ConnectionWithDetails
		if scanErr := connectionRows.Scan(&cwd.ID, &cwd.Name, &cwd.CreatedAt); scanErr != nil {
			return nil, fmt.Errorf("scan connection: %w", scanErr)
		}
		connections = append(connections, cwd)
	}
	if err := connectionRows.Err(); err != nil {
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
	defer func() { _ = rows.Close() }()

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
		defer func() { _ = memberRows.Close() }()

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

		// Revoke secret keys for the leaving group
		if r.encryptedSecretRepo != nil {
			if err := r.encryptedSecretRepo.RevokeConnectionSecretKeysForGroup(ctx, tx, connectionID, groupID); err != nil {
				return err
			}
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
	defer func() { _ = rows.Close() }()

	var otherGroupIDs []string
	for rows.Next() {
		var otherGroupID string
		if scanErr := rows.Scan(&otherGroupID); scanErr != nil {
			return false, fmt.Errorf("scan other group id: %w", scanErr)
		}
		otherGroupIDs = append(otherGroupIDs, otherGroupID)
	}
	if err := rows.Err(); err != nil {
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
	defer func() { _ = proposalRows.Close() }()

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
	if err := proposalRows.Err(); err != nil {
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
				_ = groupRows.Close()
				return nil, fmt.Errorf("scan proposal group: %w", scanErr)
			}
			groups = append(groups, g)
		}
		_ = groupRows.Close()
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

// =============================================================================
// Connection Signal Chat Proposals
// =============================================================================

var (
	ErrChatProposalNotFound              = errors.New("chat proposal not found")
	ErrChatProposalNotPending            = errors.New("chat proposal is not pending")
	ErrChatProposalVoteNotFound          = errors.New("vote not found for this group")
	ErrChatProposalAlreadyVoted          = errors.New("group has already voted on this proposal")
	ErrConnectionSignalGroupLimitReached = errors.New("connection signal group limit reached (max 5)")
)

// ProposeSignalChat creates a proposal to add a signal chat to a connection.
func (r *ConnectionRepository) ProposeSignalChat(ctx context.Context, connectionID, proposerGroupID string, req *models.ProposeConnectionChatRequest) (*models.ConnectionChatProposalWithVotes, error) {
	var result *models.ConnectionChatProposalWithVotes

	err := r.db.Transaction(ctx, func(tx *sql.Tx) error {
		// Verify connection exists
		var connExists bool
		checkErr := tx.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM connections WHERE id = ?)", connectionID,
		).Scan(&connExists)
		if checkErr != nil {
			return fmt.Errorf("check connection: %w", checkErr)
		}
		if !connExists {
			return ErrConnectionNotFound
		}

		// Verify proposer is a member
		var isMember bool
		checkErr = tx.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM connection_members WHERE connection_id = ? AND group_id = ?)",
			connectionID, proposerGroupID,
		).Scan(&isMember)
		if checkErr != nil {
			return fmt.Errorf("check membership: %w", checkErr)
		}
		if !isMember {
			return ErrNotConnectionMember
		}

		// Check signal group count for this connection
		var signalGroupCount int
		checkErr = tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM signal_groups WHERE connection_id = ? AND is_active = TRUE",
			connectionID,
		).Scan(&signalGroupCount)
		if checkErr != nil {
			return fmt.Errorf("count signal groups: %w", checkErr)
		}
		if signalGroupCount >= 5 {
			return ErrConnectionSignalGroupLimitReached
		}

		proposalID := uuid.New().String()
		now := time.Now().UTC()
		expiresAt := now.Add(7 * 24 * time.Hour)

		var description *string
		if req.Description != "" {
			description = &req.Description
		}

		// Insert the proposal
		_, insertErr := tx.ExecContext(ctx,
			`INSERT INTO connection_chat_proposals (id, connection_id, proposer_group_id, group_name, description, access_level, status, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
			proposalID, connectionID, proposerGroupID, req.GroupName, description, req.AccessLevel, now, expiresAt,
		)
		if insertErr != nil {
			return fmt.Errorf("insert chat proposal: %w", insertErr)
		}

		// Get all member groups
		memberRows, queryErr := tx.QueryContext(ctx,
			"SELECT group_id FROM connection_members WHERE connection_id = ?",
			connectionID,
		)
		if queryErr != nil {
			return fmt.Errorf("get member groups: %w", queryErr)
		}
		defer func() { _ = memberRows.Close() }()

		var memberGroupIDs []string
		for memberRows.Next() {
			var memberGroupID string
			if scanErr := memberRows.Scan(&memberGroupID); scanErr != nil {
				return fmt.Errorf("scan member group id: %w", scanErr)
			}
			memberGroupIDs = append(memberGroupIDs, memberGroupID)
		}
		if rowsErr := memberRows.Err(); rowsErr != nil {
			return fmt.Errorf("iterate member groups: %w", rowsErr)
		}

		// Create votes for all member groups
		var votes []models.ConnectionChatProposalVote
		for _, memberGroupID := range memberGroupIDs {
			voteID := uuid.New().String()
			voteStatus := "pending"
			var respondedAt *time.Time
			if memberGroupID == proposerGroupID {
				voteStatus = "approved"
				respondedAt = &now
			}
			_, insertErr = tx.ExecContext(ctx,
				"INSERT INTO connection_chat_proposal_votes (id, proposal_id, group_id, status, responded_at) VALUES (?, ?, ?, ?, ?)",
				voteID, proposalID, memberGroupID, voteStatus, respondedAt,
			)
			if insertErr != nil {
				return fmt.Errorf("insert vote: %w", insertErr)
			}
			votes = append(votes, models.ConnectionChatProposalVote{
				ID:          voteID,
				ProposalID:  proposalID,
				GroupID:     memberGroupID,
				Status:      voteStatus,
				RespondedAt: respondedAt,
			})
		}

		proposal := models.ConnectionChatProposal{
			ID:              proposalID,
			ConnectionID:    connectionID,
			ProposerGroupID: proposerGroupID,
			GroupName:       req.GroupName,
			Description:     description,
			AccessLevel:     req.AccessLevel,
			Status:          "pending",
			CreatedAt:       now,
			ExpiresAt:       &expiresAt,
		}

		result = &models.ConnectionChatProposalWithVotes{
			ConnectionChatProposal: proposal,
			Votes:                  votes,
		}

		// If only 1 member group (edge case), auto-create the signal group
		if len(memberGroupIDs) == 1 {
			createErr := r.createConnectionSignalGroup(ctx, tx, connectionID, proposerGroupID, req)
			if createErr != nil {
				return createErr
			}
			_, updateErr := tx.ExecContext(ctx,
				"UPDATE connection_chat_proposals SET status = 'approved' WHERE id = ?", proposalID,
			)
			if updateErr != nil {
				return fmt.Errorf("approve single-member proposal: %w", updateErr)
			}
			result.Status = "approved"
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// VoteOnChatProposal handles a group's vote on a connection chat proposal.
func (r *ConnectionRepository) VoteOnChatProposal(ctx context.Context, proposalID, groupID string, approve bool) (*models.ConnectionChatProposalWithVotes, error) {
	var result *models.ConnectionChatProposalWithVotes

	err := r.db.Transaction(ctx, func(tx *sql.Tx) error {
		// Load the proposal
		var proposal models.ConnectionChatProposal
		loadErr := tx.QueryRowContext(ctx,
			`SELECT id, connection_id, proposer_group_id, group_name, description, access_level, status, created_at, expires_at
			FROM connection_chat_proposals WHERE id = ?`,
			proposalID,
		).Scan(&proposal.ID, &proposal.ConnectionID, &proposal.ProposerGroupID, &proposal.GroupName,
			&proposal.Description, &proposal.AccessLevel, &proposal.Status, &proposal.CreatedAt, &proposal.ExpiresAt)
		if loadErr == sql.ErrNoRows {
			return ErrChatProposalNotFound
		}
		if loadErr != nil {
			return fmt.Errorf("load chat proposal: %w", loadErr)
		}

		if proposal.Status != "pending" {
			return ErrChatProposalNotPending
		}

		// Check expiration
		if proposal.ExpiresAt != nil && time.Now().UTC().After(*proposal.ExpiresAt) {
			_, _ = tx.ExecContext(ctx, "UPDATE connection_chat_proposals SET status = 'expired' WHERE id = ?", proposalID)
			return ErrChatProposalNotPending
		}

		// Find the vote for this group
		var voteID string
		var voteStatus string
		loadErr = tx.QueryRowContext(ctx,
			"SELECT id, status FROM connection_chat_proposal_votes WHERE proposal_id = ? AND group_id = ?",
			proposalID, groupID,
		).Scan(&voteID, &voteStatus)
		if loadErr == sql.ErrNoRows {
			return ErrChatProposalVoteNotFound
		}
		if loadErr != nil {
			return fmt.Errorf("load vote: %w", loadErr)
		}

		if voteStatus != "pending" {
			return ErrChatProposalAlreadyVoted
		}

		// Update vote
		now := time.Now().UTC()
		newVoteStatus := "declined"
		if approve {
			newVoteStatus = "approved"
		}
		_, updateErr := tx.ExecContext(ctx,
			"UPDATE connection_chat_proposal_votes SET status = ?, responded_at = ? WHERE id = ?",
			newVoteStatus, now, voteID,
		)
		if updateErr != nil {
			return fmt.Errorf("update vote: %w", updateErr)
		}

		if !approve {
			// Decline the whole proposal
			_, updateErr = tx.ExecContext(ctx,
				"UPDATE connection_chat_proposals SET status = 'declined' WHERE id = ?", proposalID,
			)
			if updateErr != nil {
				return fmt.Errorf("decline proposal: %w", updateErr)
			}
			proposal.Status = "declined"
		} else {
			// Check if all groups have approved (unanimous)
			var pendingCount int
			countErr := tx.QueryRowContext(ctx,
				"SELECT COUNT(*) FROM connection_chat_proposal_votes WHERE proposal_id = ? AND status = 'pending'",
				proposalID,
			).Scan(&pendingCount)
			if countErr != nil {
				return fmt.Errorf("count pending votes: %w", countErr)
			}

			if pendingCount == 0 {
				// All approved — create the signal group
				req := &models.ProposeConnectionChatRequest{
					GroupName:   proposal.GroupName,
					AccessLevel: proposal.AccessLevel,
				}
				if proposal.Description != nil {
					req.Description = *proposal.Description
				}
				createErr := r.createConnectionSignalGroup(ctx, tx, proposal.ConnectionID, proposal.ProposerGroupID, req)
				if createErr != nil {
					return createErr
				}
				_, updateErr = tx.ExecContext(ctx,
					"UPDATE connection_chat_proposals SET status = 'approved' WHERE id = ?", proposalID,
				)
				if updateErr != nil {
					return fmt.Errorf("approve proposal: %w", updateErr)
				}
				proposal.Status = "approved"
			}
		}

		// Load all votes for the response
		votes, loadVotesErr := r.loadChatProposalVotes(ctx, tx, proposalID)
		if loadVotesErr != nil {
			return loadVotesErr
		}

		result = &models.ConnectionChatProposalWithVotes{
			ConnectionChatProposal: proposal,
			Votes:                  votes,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ListConnectionSignalGroups returns active signal groups for a connection.
func (r *ConnectionRepository) ListConnectionSignalGroups(ctx context.Context, connectionID string) ([]*models.SignalGroup, error) {
	query := `
		SELECT sg.id, sg.region_id, sg.school_id, sg.district_id, sg.owner_group_id, sg.connection_id, sg.group_name,
			sg.description, sg.access_tier, sg.plaintext_invite_link, sg.created_by, sg.created_at, sg.is_active
		FROM signal_groups sg
		WHERE sg.connection_id = ? AND sg.is_active = TRUE
		ORDER BY sg.created_at
	`

	rows, err := r.db.QueryContext(ctx, query, connectionID)
	if err != nil {
		return nil, fmt.Errorf("list connection signal groups: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var groups []*models.SignalGroup
	for rows.Next() {
		group := &models.SignalGroup{}
		if scanErr := rows.Scan(
			&group.ID,
			&group.RegionID,
			&group.SchoolID,
			&group.DistrictID,
			&group.OwnerGroupID,
			&group.ConnectionID,
			&group.GroupName,
			&group.Description,
			&group.AccessTier,
			&group.PlaintextInviteLink,
			&group.CreatedBy,
			&group.CreatedAt,
			&group.IsActive,
		); scanErr != nil {
			return nil, fmt.Errorf("scan signal group: %w", scanErr)
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

// ListChatProposals returns pending chat proposals for a connection.
func (r *ConnectionRepository) ListChatProposals(ctx context.Context, connectionID string) ([]models.ConnectionChatProposalWithVotes, error) {
	proposalRows, err := r.db.QueryContext(ctx,
		`SELECT id, connection_id, proposer_group_id, group_name, description, access_level, status, created_at, expires_at
		FROM connection_chat_proposals WHERE connection_id = ? AND status = 'pending' ORDER BY created_at DESC`,
		connectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list chat proposals: %w", err)
	}
	defer func() { _ = proposalRows.Close() }()

	var proposals []models.ConnectionChatProposalWithVotes
	for proposalRows.Next() {
		var p models.ConnectionChatProposalWithVotes
		if scanErr := proposalRows.Scan(
			&p.ID, &p.ConnectionID, &p.ProposerGroupID, &p.GroupName,
			&p.Description, &p.AccessLevel, &p.Status, &p.CreatedAt, &p.ExpiresAt,
		); scanErr != nil {
			return nil, fmt.Errorf("scan chat proposal: %w", scanErr)
		}
		proposals = append(proposals, p)
	}
	if rowsErr := proposalRows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	// Load votes for each proposal
	for i := range proposals {
		voteRows, queryErr := r.db.QueryContext(ctx,
			"SELECT id, proposal_id, group_id, status, responded_at FROM connection_chat_proposal_votes WHERE proposal_id = ?",
			proposals[i].ID,
		)
		if queryErr != nil {
			return nil, fmt.Errorf("load chat proposal votes: %w", queryErr)
		}

		var votes []models.ConnectionChatProposalVote
		for voteRows.Next() {
			var v models.ConnectionChatProposalVote
			if scanErr := voteRows.Scan(&v.ID, &v.ProposalID, &v.GroupID, &v.Status, &v.RespondedAt); scanErr != nil {
				_ = voteRows.Close()
				return nil, fmt.Errorf("scan vote: %w", scanErr)
			}
			votes = append(votes, v)
		}
		_ = voteRows.Close()
		if rowsErr := voteRows.Err(); rowsErr != nil {
			return nil, rowsErr
		}
		proposals[i].Votes = votes
	}

	return proposals, nil
}

// createConnectionSignalGroup creates a signal group owned by a connection.
func (r *ConnectionRepository) createConnectionSignalGroup(ctx context.Context, tx *sql.Tx, connectionID, _ string, req *models.ProposeConnectionChatRequest) error {
	signalGroupID := uuid.New().String()
	now := time.Now().UTC()

	var description *string
	if req.Description != "" {
		description = &req.Description
	}

	// Map the proposal's connection access level onto a signal group access tier.
	// admin_only stays admin-only; all_members maps to the open tier (unrestricted,
	// unencrypted). Default to admin_only (most restrictive) if the value is
	// missing/unexpected.
	accessTier := models.AccessTierAdminOnly
	var plaintextInviteLink *string
	if req.AccessLevel == string(models.ConnectionAccessLevelAllMembers) {
		accessTier = models.AccessTierOpen
		link := uuid.New().String()
		plaintextInviteLink = &link
	}

	_, err := tx.ExecContext(ctx,
		`INSERT INTO signal_groups (id, region_id, school_id, district_id, owner_group_id, connection_id, group_name, description, access_tier, plaintext_invite_link, created_by, created_at, is_active)
		VALUES (?, NULL, NULL, NULL, NULL, ?, ?, ?, ?, ?, NULL, ?, TRUE)`,
		signalGroupID, connectionID, req.GroupName, description, string(accessTier), plaintextInviteLink, now,
	)
	if err != nil {
		return fmt.Errorf("create connection signal group: %w", err)
	}
	return nil
}

// loadChatProposalVotes loads all votes for a chat proposal within a transaction.
func (r *ConnectionRepository) loadChatProposalVotes(ctx context.Context, tx *sql.Tx, proposalID string) ([]models.ConnectionChatProposalVote, error) {
	rows, err := tx.QueryContext(ctx,
		"SELECT id, proposal_id, group_id, status, responded_at FROM connection_chat_proposal_votes WHERE proposal_id = ?",
		proposalID,
	)
	if err != nil {
		return nil, fmt.Errorf("load chat proposal votes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var votes []models.ConnectionChatProposalVote
	for rows.Next() {
		var v models.ConnectionChatProposalVote
		if scanErr := rows.Scan(&v.ID, &v.ProposalID, &v.GroupID, &v.Status, &v.RespondedAt); scanErr != nil {
			return nil, fmt.Errorf("scan vote: %w", scanErr)
		}
		votes = append(votes, v)
	}
	return votes, rows.Err()
}

// =============================================================================
// Connection Chat Access Predicates
// =============================================================================

// ConnectionChatUserAccessPredicate generates a SQL subquery that identifies
// users eligible to access a connection signal chat based on access level.
// For admin_only: returns only admins of member groups
// For all_members: returns all members of connection groups (open-tier access)
// The subquery uses IN syntax: WHERE user_id IN (...)
func ConnectionChatUserAccessPredicate(accessLevel string) string {
	adminOnlyFilter := ""
	if accessLevel == string(models.ConnectionAccessLevelAdminOnly) {
		adminOnlyFilter = "AND gm.is_admin = TRUE"
	}

	return fmt.Sprintf(`
		SELECT DISTINCT gm.user_id
		FROM group_members gm
		JOIN connection_members cm ON gm.group_id = cm.group_id
		WHERE cm.connection_id = ? %s
	`, adminOnlyFilter)
}

// =============================================================================
// Connection Shared Resources
// =============================================================================

// ShareResource shares a group resource into a connection.
func (r *ConnectionRepository) ShareResource(ctx context.Context, connectionID, groupID string, req *models.ShareResourceRequest) (*models.ConnectionSharedResource, error) {
	// Verify group is a member of the connection
	isMember, err := r.isConnectionMember(ctx, connectionID, groupID)
	if err != nil {
		return nil, fmt.Errorf("check connection membership: %w", err)
	}
	if !isMember {
		return nil, ErrNotConnectionMember
	}

	// Verify the resource belongs to the group
	var resourceGroupID string
	err = r.db.QueryRowContext(ctx,
		"SELECT group_id FROM group_resources WHERE id = ?",
		req.ResourceID,
	).Scan(&resourceGroupID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrResourceNotInGroup
		}
		return nil, fmt.Errorf("check resource ownership: %w", err)
	}
	if resourceGroupID != groupID {
		return nil, ErrResourceNotInGroup
	}

	shareID := uuid.New().String()
	now := time.Now().UTC()

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO connection_shared_resources (id, resource_id, connection_id, shared_by_group_id, visibility, shared_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		shareID, req.ResourceID, connectionID, groupID, req.Visibility, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") || strings.Contains(err.Error(), "uk_csr") {
			return nil, ErrAlreadyShared
		}
		return nil, fmt.Errorf("share resource: %w", err)
	}

	return &models.ConnectionSharedResource{
		ID:              shareID,
		ResourceID:      req.ResourceID,
		ConnectionID:    connectionID,
		SharedByGroupID: groupID,
		Visibility:      req.Visibility,
		SharedAt:        now,
	}, nil
}

// UnshareResource removes a shared resource from a connection.
func (r *ConnectionRepository) UnshareResource(ctx context.Context, connectionID, resourceID string) error {
	result, err := r.db.ExecContext(ctx,
		"DELETE FROM connection_shared_resources WHERE connection_id = ? AND resource_id = ?",
		connectionID, resourceID,
	)
	if err != nil {
		return fmt.Errorf("unshare resource: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrConnectionNotFound
	}
	return nil
}

// ListConnectionResources returns shared resources for a connection, filtered by visibility.
func (r *ConnectionRepository) ListConnectionResources(ctx context.Context, connectionID string, isAdmin bool) ([]models.ConnectionSharedResourceWithDetails, error) {
	query := `
		SELECT csr.id, csr.resource_id, csr.connection_id, csr.shared_by_group_id,
			csr.visibility, csr.shared_at,
			gr.title, gr.url, gr.description,
			g.name AS group_name
		FROM connection_shared_resources csr
		JOIN group_resources gr ON csr.resource_id = gr.id
		JOIN ` + "`groups`" + ` g ON csr.shared_by_group_id = g.id
		WHERE csr.connection_id = ?
	`
	var args []interface{}
	args = append(args, connectionID)

	if !isAdmin {
		query += " AND csr.visibility = 'all_members'"
	}
	query += " ORDER BY csr.shared_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list connection resources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var resources []models.ConnectionSharedResourceWithDetails
	for rows.Next() {
		var resource models.ConnectionSharedResourceWithDetails
		if scanErr := rows.Scan(
			&resource.ID, &resource.ResourceID, &resource.ConnectionID,
			&resource.SharedByGroupID, &resource.Visibility, &resource.SharedAt,
			&resource.Title, &resource.URL, &resource.Description,
			&resource.GroupName,
		); scanErr != nil {
			return nil, fmt.Errorf("scan shared resource: %w", scanErr)
		}
		resources = append(resources, resource)
	}
	return resources, rows.Err()
}
