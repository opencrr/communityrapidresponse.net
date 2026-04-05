package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

var (
	ErrGroupNotFound          = errors.New("group not found")
	ErrInviteLinkNotFound     = errors.New("invite link not found")
	ErrInviteLinkExpired      = errors.New("invite link expired")
	ErrInviteLinkExhausted    = errors.New("invite link max uses reached")
	ErrInvitationNotFound     = errors.New("invitation not found")
	ErrInvitationAlreadyPending = errors.New("invitation already pending")
	ErrInvitationExpired      = errors.New("invitation expired")
	ErrGroupAlreadyMember     = errors.New("user is already a member")
)

// GroupRepository handles group database operations
type GroupRepository struct {
	db *DB
}

// NewGroupRepository creates a new group repository
func NewGroupRepository(db *DB) *GroupRepository {
	return &GroupRepository{db: db}
}

// Create creates a new group with its creator as founding admin, regions, and topic tags.
// Status is always provisional and visibility is always unlisted on creation.
func (r *GroupRepository) Create(ctx context.Context, req *models.CreateGroupRequest, creatorID string) (*models.Group, error) {
	groupID := uuid.New().String()
	now := time.Now().UTC()

	var description *string
	if req.Description != "" {
		description = &req.Description
	}

	group := &models.Group{
		ID:          groupID,
		Name:        req.Name,
		Description: description,
		Status:      models.GroupStatusProvisional,
		Visibility:  models.GroupVisibilityUnlisted,
		CreatedBy:   &creatorID,
		CreatedAt:   now,
	}

	err := r.db.Transaction(ctx, func(tx *sql.Tx) error {
		// Insert the group (created_at and updated_at have DEFAULT CURRENT_TIMESTAMP)
		groupQuery := `
			INSERT INTO ` + "`groups`" + ` (id, name, description, status, visibility, created_by)
			VALUES (?, ?, ?, ?, ?, ?)
		`
		_, err := tx.ExecContext(ctx, groupQuery,
			group.ID, group.Name, group.Description,
			group.Status, group.Visibility,
			group.CreatedBy,
		)
		if err != nil {
			return fmt.Errorf("insert group: %w", err)
		}

		// Add creator as founding admin
		memberQuery := `
			INSERT INTO group_members (id, group_id, user_id, is_admin, is_founding_member, joined_at)
			VALUES (?, ?, ?, TRUE, TRUE, ?)
		`
		_, err = tx.ExecContext(ctx, memberQuery,
			uuid.New().String(), groupID, creatorID, now,
		)
		if err != nil {
			return fmt.Errorf("insert founding member: %w", err)
		}

		// Add regions
		for _, regionID := range req.RegionIDs {
			regionQuery := `
				INSERT INTO group_regions (id, group_id, region_id)
				VALUES (?, ?, ?)
			`
			_, err = tx.ExecContext(ctx, regionQuery,
				uuid.New().String(), groupID, regionID,
			)
			if err != nil {
				return fmt.Errorf("insert group region: %w", err)
			}
		}

		// Add topic tags
		for _, tag := range req.TopicTags {
			tagQuery := `
				INSERT INTO group_topic_tags (id, group_id, tag)
				VALUES (?, ?, ?)
			`
			_, err = tx.ExecContext(ctx, tagQuery,
				uuid.New().String(), groupID, tag,
			)
			if err != nil {
				return fmt.Errorf("insert topic tag: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return group, nil
}

// GetByID retrieves a group by ID
func (r *GroupRepository) GetByID(ctx context.Context, id string) (*models.Group, error) {
	query := `
		SELECT id, name, description, status, visibility, founding_threshold,
			created_by, created_at, updated_at, graduated_at
		FROM ` + "`groups`" + `
		WHERE id = ?
	`

	group := &models.Group{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&group.ID, &group.Name, &group.Description,
		&group.Status, &group.Visibility, &group.FoundingThreshold,
		&group.CreatedBy, &group.CreatedAt, &group.UpdatedAt, &group.GraduatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGroupNotFound
	}
	if err != nil {
		return nil, err
	}

	return group, nil
}

// GetByIDWithDetails retrieves a group with membership counts, regions, tags, and user membership status.
func (r *GroupRepository) GetByIDWithDetails(ctx context.Context, id, userID string) (*models.GroupWithDetails, error) {
	group, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	result := &models.GroupWithDetails{
		Group: *group,
	}

	// Get member and admin counts
	countQuery := `
		SELECT
			COUNT(*) as member_count,
			SUM(CASE WHEN is_admin = TRUE THEN 1 ELSE 0 END) as admin_count
		FROM group_members
		WHERE group_id = ?
	`
	err = r.db.QueryRowContext(ctx, countQuery, id).Scan(
		&result.MemberCount, &result.AdminCount,
	)
	if err != nil {
		return nil, fmt.Errorf("count members: %w", err)
	}

	// Check user membership
	if userID != "" {
		memberQuery := `
			SELECT is_admin FROM group_members
			WHERE group_id = ? AND user_id = ?
		`
		var isAdmin bool
		err = r.db.QueryRowContext(ctx, memberQuery, id, userID).Scan(&isAdmin)
		if err == nil {
			result.IsUserMember = true
			result.IsUserAdmin = isAdmin
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("check membership: %w", err)
		}
	}

	// Get regions
	result.Regions, err = r.GetRegions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get regions: %w", err)
	}

	// Get topic tags
	result.TopicTags, err = r.GetTopicTags(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get topic tags: %w", err)
	}

	return result, nil
}

// ListByUser returns all groups where the given user is a member, with details.
func (r *GroupRepository) ListByUser(ctx context.Context, userID string) ([]models.GroupWithDetails, error) {
	query := `
		SELECT g.id, g.name, g.description, g.status, g.visibility, g.founding_threshold,
			g.created_by, g.created_at, g.updated_at, g.graduated_at,
			(SELECT COUNT(*) FROM group_members WHERE group_id = g.id) as member_count,
			(SELECT SUM(CASE WHEN is_admin = TRUE THEN 1 ELSE 0 END) FROM group_members WHERE group_id = g.id) as admin_count,
			gm.is_admin
		FROM ` + "`groups`" + ` g
		JOIN group_members gm ON g.id = gm.group_id
		WHERE gm.user_id = ?
		ORDER BY g.name
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []models.GroupWithDetails
	for rows.Next() {
		var gwd models.GroupWithDetails
		err := rows.Scan(
			&gwd.ID, &gwd.Name, &gwd.Description,
			&gwd.Status, &gwd.Visibility, &gwd.FoundingThreshold,
			&gwd.CreatedBy, &gwd.CreatedAt, &gwd.UpdatedAt, &gwd.GraduatedAt,
			&gwd.MemberCount, &gwd.AdminCount,
			&gwd.IsUserAdmin,
		)
		if err != nil {
			return nil, err
		}
		gwd.IsUserMember = true

		gwd.Regions, err = r.GetRegions(ctx, gwd.ID)
		if err != nil {
			return nil, fmt.Errorf("get regions for group %s: %w", gwd.ID, err)
		}

		gwd.TopicTags, err = r.GetTopicTags(ctx, gwd.ID)
		if err != nil {
			return nil, fmt.Errorf("get tags for group %s: %w", gwd.ID, err)
		}

		groups = append(groups, gwd)
	}

	return groups, rows.Err()
}

// ListByRegion returns listed, active groups associated with the given region.
func (r *GroupRepository) ListByRegion(ctx context.Context, regionID string) ([]models.GroupWithDetails, error) {
	query := `
		SELECT g.id, g.name, g.description, g.status, g.visibility, g.founding_threshold,
			g.created_by, g.created_at, g.updated_at, g.graduated_at,
			(SELECT COUNT(*) FROM group_members WHERE group_id = g.id) as member_count,
			(SELECT SUM(CASE WHEN is_admin = TRUE THEN 1 ELSE 0 END) FROM group_members WHERE group_id = g.id) as admin_count
		FROM ` + "`groups`" + ` g
		JOIN group_regions gr ON g.id = gr.group_id
		WHERE gr.region_id = ?
			AND g.visibility = 'listed'
			AND g.status = 'active'
		ORDER BY g.name
	`

	rows, err := r.db.QueryContext(ctx, query, regionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []models.GroupWithDetails
	for rows.Next() {
		var gwd models.GroupWithDetails
		err := rows.Scan(
			&gwd.ID, &gwd.Name, &gwd.Description,
			&gwd.Status, &gwd.Visibility, &gwd.FoundingThreshold,
			&gwd.CreatedBy, &gwd.CreatedAt, &gwd.UpdatedAt, &gwd.GraduatedAt,
			&gwd.MemberCount, &gwd.AdminCount,
		)
		if err != nil {
			return nil, err
		}

		gwd.Regions, err = r.GetRegions(ctx, gwd.ID)
		if err != nil {
			return nil, fmt.Errorf("get regions for group %s: %w", gwd.ID, err)
		}

		gwd.TopicTags, err = r.GetTopicTags(ctx, gwd.ID)
		if err != nil {
			return nil, fmt.Errorf("get tags for group %s: %w", gwd.ID, err)
		}

		groups = append(groups, gwd)
	}

	return groups, rows.Err()
}

// Update applies partial updates to a group. Only non-nil fields are updated.
func (r *GroupRepository) Update(ctx context.Context, id string, req *models.UpdateGroupRequest) error {
	var setClauses []string
	var args []interface{}

	if req.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, *req.Name)
	}
	if req.Description != nil {
		setClauses = append(setClauses, "description = ?")
		args = append(args, *req.Description)
	}
	if req.Visibility != nil {
		setClauses = append(setClauses, "visibility = ?")
		args = append(args, *req.Visibility)
	}

	if len(setClauses) > 0 {
		query := "UPDATE `groups` SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
		args = append(args, id)

		result, err := r.db.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return ErrGroupNotFound
		}
	}

	if req.TopicTags != nil {
		if err := r.SetTopicTags(ctx, id, *req.TopicTags); err != nil {
			return err
		}
	}

	return nil
}

// Delete removes a group. CASCADE handles child rows.
func (r *GroupRepository) Delete(ctx context.Context, id string) error {
	query := "DELETE FROM `groups` WHERE id = ?"
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrGroupNotFound
	}

	return nil
}

// AddMember adds a user to a group.
func (r *GroupRepository) AddMember(ctx context.Context, groupID, userID string, isAdmin, isFoundingMember bool) error {
	query := `
		INSERT INTO group_members (id, group_id, user_id, is_admin, is_founding_member, joined_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	_, err := r.db.ExecContext(ctx, query,
		uuid.New().String(), groupID, userID, isAdmin, isFoundingMember, time.Now().UTC(),
	)
	return err
}

// RemoveMember removes a user from a group.
func (r *GroupRepository) RemoveMember(ctx context.Context, groupID, userID string) error {
	query := "DELETE FROM group_members WHERE group_id = ? AND user_id = ?"
	_, err := r.db.ExecContext(ctx, query, groupID, userID)
	return err
}

// IsUserMember checks if a user is a member of a group.
func (r *GroupRepository) IsUserMember(ctx context.Context, groupID, userID string) (bool, error) {
	query := "SELECT EXISTS(SELECT 1 FROM group_members WHERE group_id = ? AND user_id = ?)"
	var exists bool
	err := r.db.QueryRowContext(ctx, query, groupID, userID).Scan(&exists)
	return exists, err
}

// IsUserAdmin checks if a user is an admin of a group.
func (r *GroupRepository) IsUserAdmin(ctx context.Context, groupID, userID string) (bool, error) {
	query := "SELECT EXISTS(SELECT 1 FROM group_members WHERE group_id = ? AND user_id = ? AND is_admin = TRUE)"
	var exists bool
	err := r.db.QueryRowContext(ctx, query, groupID, userID).Scan(&exists)
	return exists, err
}

// GetMembers returns all members of a group with their user details.
func (r *GroupRepository) GetMembers(ctx context.Context, groupID string) ([]models.GroupMemberWithUser, error) {
	query := `
		SELECT gm.id, gm.group_id, gm.user_id, gm.is_admin, gm.is_founding_member,
			gm.trust_level, gm.joined_at,
			u.username, u.verification_tier
		FROM group_members gm
		JOIN users u ON gm.user_id = u.id
		WHERE gm.group_id = ?
		ORDER BY gm.joined_at
	`

	rows, err := r.db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []models.GroupMemberWithUser
	for rows.Next() {
		var member models.GroupMemberWithUser
		err := rows.Scan(
			&member.ID, &member.GroupID, &member.UserID,
			&member.IsAdmin, &member.IsFoundingMember,
			&member.TrustLevel, &member.JoinedAt,
			&member.Username, &member.VerificationTier,
		)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}

	return members, rows.Err()
}

// GetMemberCount returns the number of members in a group.
func (r *GroupRepository) GetMemberCount(ctx context.Context, groupID string) (int, error) {
	query := "SELECT COUNT(*) FROM group_members WHERE group_id = ?"
	var count int
	err := r.db.QueryRowContext(ctx, query, groupID).Scan(&count)
	return count, err
}

// SetTopicTags replaces all topic tags for a group.
func (r *GroupRepository) SetTopicTags(ctx context.Context, groupID string, tags []string) error {
	return r.db.Transaction(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DELETE FROM group_topic_tags WHERE group_id = ?", groupID)
		if err != nil {
			return fmt.Errorf("delete existing tags: %w", err)
		}

		for _, tag := range tags {
			_, err = tx.ExecContext(ctx,
				"INSERT INTO group_topic_tags (id, group_id, tag) VALUES (?, ?, ?)",
				uuid.New().String(), groupID, tag,
			)
			if err != nil {
				return fmt.Errorf("insert tag: %w", err)
			}
		}

		return nil
	})
}

// GetTopicTags returns all topic tags for a group.
func (r *GroupRepository) GetTopicTags(ctx context.Context, groupID string) ([]string, error) {
	query := "SELECT tag FROM group_topic_tags WHERE group_id = ? ORDER BY tag"
	rows, err := r.db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}

	return tags, rows.Err()
}

// AddRegion associates a region with a group.
func (r *GroupRepository) AddRegion(ctx context.Context, groupID, regionID string) error {
	query := "INSERT INTO group_regions (id, group_id, region_id) VALUES (?, ?, ?)"
	_, err := r.db.ExecContext(ctx, query, uuid.New().String(), groupID, regionID)
	return err
}

// GetRegions returns the regions associated with a group.
func (r *GroupRepository) GetRegions(ctx context.Context, groupID string) ([]models.RegionSummary, error) {
	query := `
		SELECT gr2.id, gr2.name, gr2.region_type
		FROM group_regions gr
		JOIN geographic_regions gr2 ON gr.region_id = gr2.id
		WHERE gr.group_id = ?
		ORDER BY gr2.name
	`

	rows, err := r.db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var regions []models.RegionSummary
	for rows.Next() {
		var region models.RegionSummary
		if err := rows.Scan(&region.ID, &region.Name, &region.RegionType); err != nil {
			return nil, err
		}
		regions = append(regions, region)
	}

	return regions, rows.Err()
}

// GetPlatformConfig retrieves a config value by key from platform_config.
func (r *GroupRepository) GetPlatformConfig(ctx context.Context, key string) (string, error) {
	query := "SELECT config_value FROM platform_config WHERE config_key = ?"
	var value string
	err := r.db.QueryRowContext(ctx, query, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("platform config key not found: %s", key)
	}
	return value, err
}

// GetFoundingThreshold returns the founding threshold for a group.
// If the group has a custom threshold, it is returned. Otherwise the platform default is used.
func (r *GroupRepository) GetFoundingThreshold(ctx context.Context, groupID string) (int, error) {
	group, err := r.GetByID(ctx, groupID)
	if err != nil {
		return 0, err
	}

	if group.FoundingThreshold != nil {
		return *group.FoundingThreshold, nil
	}

	defaultVal, err := r.GetPlatformConfig(ctx, "group_founding_threshold")
	if err != nil {
		return 0, fmt.Errorf("get default founding threshold: %w", err)
	}

	threshold, err := strconv.Atoi(defaultVal)
	if err != nil {
		return 0, fmt.Errorf("invalid founding threshold value: %w", err)
	}

	return threshold, nil
}

// generateInviteToken creates a cryptographically random 64-character hex token.
func generateInviteToken() (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generate invite token: %w", err)
	}
	return hex.EncodeToString(tokenBytes), nil
}

// =============================================================================
// Invite Links
// =============================================================================

// CreateInviteLink creates a shareable invite link for a group.
func (r *GroupRepository) CreateInviteLink(ctx context.Context, groupID, createdBy string, req *models.CreateInviteLinkRequest) (*models.GroupInviteLink, error) {
	token, err := generateInviteToken()
	if err != nil {
		return nil, err
	}

	linkID := uuid.New().String()
	now := time.Now().UTC()

	link := &models.GroupInviteLink{
		ID:        linkID,
		GroupID:   groupID,
		Token:     token,
		CreatedBy: &createdBy,
		MaxUses:   req.MaxUses,
		UseCount:  0,
		CreatedAt: now,
	}

	if req.ExpiresInHours != nil {
		expiresAt := now.Add(time.Duration(*req.ExpiresInHours) * time.Hour)
		link.ExpiresAt = &expiresAt
	}

	query := `
		INSERT INTO group_invite_links (id, group_id, token, created_by, expires_at, max_uses, use_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?)
	`
	_, err = r.db.ExecContext(ctx, query,
		link.ID, link.GroupID, link.Token, link.CreatedBy,
		link.ExpiresAt, link.MaxUses, link.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert invite link: %w", err)
	}

	return link, nil
}

// GetInviteLinkByToken retrieves an invite link by its token.
func (r *GroupRepository) GetInviteLinkByToken(ctx context.Context, token string) (*models.GroupInviteLink, error) {
	query := `
		SELECT id, group_id, token, created_by, expires_at, max_uses, use_count, created_at
		FROM group_invite_links
		WHERE token = ?
	`

	link := &models.GroupInviteLink{}
	err := r.db.QueryRowContext(ctx, query, token).Scan(
		&link.ID, &link.GroupID, &link.Token, &link.CreatedBy,
		&link.ExpiresAt, &link.MaxUses, &link.UseCount, &link.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInviteLinkNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get invite link by token: %w", err)
	}

	return link, nil
}

// ConsumeInviteLink atomically validates and increments the use count of an invite link.
// Returns the link (with group_id) so the caller can add the member.
func (r *GroupRepository) ConsumeInviteLink(ctx context.Context, token string) (*models.GroupInviteLink, error) {
	var link models.GroupInviteLink

	err := r.db.Transaction(ctx, func(tx *sql.Tx) error {
		query := `
			SELECT id, group_id, token, created_by, expires_at, max_uses, use_count, created_at
			FROM group_invite_links
			WHERE token = ?
			FOR UPDATE
		`
		err := tx.QueryRowContext(ctx, query, token).Scan(
			&link.ID, &link.GroupID, &link.Token, &link.CreatedBy,
			&link.ExpiresAt, &link.MaxUses, &link.UseCount, &link.CreatedAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInviteLinkNotFound
		}
		if err != nil {
			return fmt.Errorf("select invite link for update: %w", err)
		}

		// Check expiration
		if link.ExpiresAt != nil && link.ExpiresAt.Before(time.Now().UTC()) {
			return ErrInviteLinkExpired
		}

		// Check max uses
		if link.MaxUses != nil && link.UseCount >= *link.MaxUses {
			return ErrInviteLinkExhausted
		}

		// Increment use count
		_, err = tx.ExecContext(ctx, "UPDATE group_invite_links SET use_count = use_count + 1 WHERE id = ?", link.ID)
		if err != nil {
			return fmt.Errorf("increment use count: %w", err)
		}

		link.UseCount++
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &link, nil
}

// ListInviteLinks returns all invite links for a group, ordered by creation time descending.
func (r *GroupRepository) ListInviteLinks(ctx context.Context, groupID string) ([]models.GroupInviteLink, error) {
	query := `
		SELECT id, group_id, token, created_by, expires_at, max_uses, use_count, created_at
		FROM group_invite_links
		WHERE group_id = ?
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, fmt.Errorf("list invite links: %w", err)
	}
	defer rows.Close()

	var links []models.GroupInviteLink
	for rows.Next() {
		var link models.GroupInviteLink
		if err := rows.Scan(
			&link.ID, &link.GroupID, &link.Token, &link.CreatedBy,
			&link.ExpiresAt, &link.MaxUses, &link.UseCount, &link.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan invite link: %w", err)
		}
		links = append(links, link)
	}

	return links, rows.Err()
}

// =============================================================================
// Invitations
// =============================================================================

// CreateInvitation creates a direct invitation from an admin to a specific user.
func (r *GroupRepository) CreateInvitation(ctx context.Context, groupID, userID, invitedBy string) (*models.GroupInvitation, error) {
	// Check for existing pending invitation
	checkQuery := `
		SELECT EXISTS(
			SELECT 1 FROM group_invitations
			WHERE group_id = ? AND user_id = ? AND status = 'pending'
			AND (expires_at IS NULL OR expires_at > NOW())
		)
	`
	var exists bool
	if err := r.db.QueryRowContext(ctx, checkQuery, groupID, userID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check existing invitation: %w", err)
	}
	if exists {
		return nil, ErrInvitationAlreadyPending
	}

	invitationID := uuid.New().String()
	now := time.Now().UTC()
	expiresAt := now.Add(7 * 24 * time.Hour)

	invitation := &models.GroupInvitation{
		ID:        invitationID,
		GroupID:   groupID,
		UserID:    userID,
		InvitedBy: &invitedBy,
		Status:    models.InvitationStatusPending,
		CreatedAt: now,
		ExpiresAt: &expiresAt,
	}

	query := `
		INSERT INTO group_invitations (id, group_id, user_id, invited_by, status, created_at, expires_at)
		VALUES (?, ?, ?, ?, 'pending', ?, ?)
	`
	_, err := r.db.ExecContext(ctx, query,
		invitation.ID, invitation.GroupID, invitation.UserID,
		invitation.InvitedBy, invitation.CreatedAt, invitation.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert invitation: %w", err)
	}

	return invitation, nil
}

// ListPendingInvitationsForUser returns pending, non-expired invitations for a user with group and inviter details.
func (r *GroupRepository) ListPendingInvitationsForUser(ctx context.Context, userID string) ([]models.GroupInvitationWithDetails, error) {
	query := `
		SELECT gi.id, gi.group_id, gi.user_id, gi.invited_by, gi.status,
			gi.created_at, gi.expires_at, gi.responded_at,
			g.name AS group_name,
			u.username AS inviter_name
		FROM group_invitations gi
		JOIN ` + "`groups`" + ` g ON gi.group_id = g.id
		LEFT JOIN users u ON gi.invited_by = u.id
		WHERE gi.user_id = ? AND gi.status = 'pending'
			AND (gi.expires_at IS NULL OR gi.expires_at > NOW())
		ORDER BY gi.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list pending invitations: %w", err)
	}
	defer rows.Close()

	var invitations []models.GroupInvitationWithDetails
	for rows.Next() {
		var inv models.GroupInvitationWithDetails
		if err := rows.Scan(
			&inv.ID, &inv.GroupID, &inv.UserID, &inv.InvitedBy, &inv.Status,
			&inv.CreatedAt, &inv.ExpiresAt, &inv.RespondedAt,
			&inv.GroupName, &inv.InviterName,
		); err != nil {
			return nil, fmt.Errorf("scan invitation: %w", err)
		}
		invitations = append(invitations, inv)
	}

	return invitations, rows.Err()
}

// ListPendingInvitationsForGroup returns pending invitations for a group.
func (r *GroupRepository) ListPendingInvitationsForGroup(ctx context.Context, groupID string) ([]models.GroupInvitation, error) {
	query := `
		SELECT id, group_id, user_id, invited_by, status, created_at, expires_at, responded_at
		FROM group_invitations
		WHERE group_id = ? AND status = 'pending'
			AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, fmt.Errorf("list pending invitations for group: %w", err)
	}
	defer rows.Close()

	var invitations []models.GroupInvitation
	for rows.Next() {
		var inv models.GroupInvitation
		if err := rows.Scan(
			&inv.ID, &inv.GroupID, &inv.UserID, &inv.InvitedBy, &inv.Status,
			&inv.CreatedAt, &inv.ExpiresAt, &inv.RespondedAt,
		); err != nil {
			return nil, fmt.Errorf("scan invitation: %w", err)
		}
		invitations = append(invitations, inv)
	}

	return invitations, rows.Err()
}

// RespondToInvitation accepts or declines an invitation. Does NOT add the user to the group.
func (r *GroupRepository) RespondToInvitation(ctx context.Context, invitationID, userID string, accept bool) (*models.GroupInvitation, error) {
	query := `
		SELECT id, group_id, user_id, invited_by, status, created_at, expires_at, responded_at
		FROM group_invitations
		WHERE id = ?
	`

	var invitation models.GroupInvitation
	err := r.db.QueryRowContext(ctx, query, invitationID).Scan(
		&invitation.ID, &invitation.GroupID, &invitation.UserID, &invitation.InvitedBy,
		&invitation.Status, &invitation.CreatedAt, &invitation.ExpiresAt, &invitation.RespondedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvitationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get invitation: %w", err)
	}

	// Verify ownership
	if invitation.UserID != userID {
		return nil, ErrInvitationNotFound
	}

	// Verify pending status
	if invitation.Status != models.InvitationStatusPending {
		return nil, ErrInvitationNotFound
	}

	// Check expiration
	if invitation.ExpiresAt != nil && invitation.ExpiresAt.Before(time.Now().UTC()) {
		return nil, ErrInvitationExpired
	}

	newStatus := models.InvitationStatusDeclined
	if accept {
		newStatus = models.InvitationStatusAccepted
	}

	now := time.Now().UTC()
	updateQuery := `
		UPDATE group_invitations SET status = ?, responded_at = ?
		WHERE id = ?
	`
	_, err = r.db.ExecContext(ctx, updateQuery, string(newStatus), now, invitationID)
	if err != nil {
		return nil, fmt.Errorf("update invitation: %w", err)
	}

	invitation.Status = newStatus
	invitation.RespondedAt = &now

	return &invitation, nil
}

// =============================================================================
// Graduation
// =============================================================================

// CheckAndGraduate checks if a provisional group has met its founding threshold
// and graduates it to active status, promoting founding members to admin.
// Returns true if the group was graduated, false if no action was taken.
func (r *GroupRepository) CheckAndGraduate(ctx context.Context, groupID string) (bool, error) {
	group, err := r.GetByID(ctx, groupID)
	if err != nil {
		return false, err
	}

	if group.Status != models.GroupStatusProvisional {
		return false, nil
	}

	threshold, err := r.GetFoundingThreshold(ctx, groupID)
	if err != nil {
		return false, fmt.Errorf("get founding threshold: %w", err)
	}

	memberCount, err := r.GetMemberCount(ctx, groupID)
	if err != nil {
		return false, fmt.Errorf("get member count: %w", err)
	}

	if memberCount < threshold {
		return false, nil
	}

	err = r.db.Transaction(ctx, func(tx *sql.Tx) error {
		// Graduate the group
		result, err := tx.ExecContext(ctx,
			"UPDATE `groups` SET status = 'active', graduated_at = NOW() WHERE id = ? AND status = 'provisional'",
			groupID,
		)
		if err != nil {
			return fmt.Errorf("graduate group: %w", err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("check rows affected: %w", err)
		}
		if rowsAffected == 0 {
			// Another goroutine already graduated this group
			return nil
		}

		// Promote founding members to admin
		_, err = tx.ExecContext(ctx,
			"UPDATE group_members SET is_admin = TRUE WHERE group_id = ? AND is_founding_member = TRUE",
			groupID,
		)
		if err != nil {
			return fmt.Errorf("promote founding members: %w", err)
		}

		return nil
	})

	if err != nil {
		return false, err
	}

	return true, nil
}
