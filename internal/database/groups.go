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
	ErrGroupNotFound            = errors.New("group not found")
	ErrInviteLinkNotFound       = errors.New("invite link not found")
	ErrInviteLinkExpired        = errors.New("invite link expired")
	ErrInviteLinkExhausted      = errors.New("invite link max uses reached")
	ErrInvitationNotFound       = errors.New("invitation not found")
	ErrInvitationAlreadyPending = errors.New("invitation already pending")
	ErrInvitationExpired        = errors.New("invitation expired")
	ErrGroupAlreadyMember       = errors.New("user is already a member")
	ErrNotTrustedOrAdmin        = errors.New("user is not trusted or admin in this group")
	ErrSelfVouch                = errors.New("cannot vouch for yourself")
	ErrResourceNotFound         = errors.New("resource not found")
	ErrCannotBlockSelf          = errors.New("cannot block own group")
	ErrGroupAlreadyBlocked      = errors.New("group already blocked")
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
			trusted_vouch_threshold, discoverable_by_unverified,
			created_by, created_at, updated_at, graduated_at
		FROM ` + "`groups`" + `
		WHERE id = ?
	`

	group := &models.Group{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&group.ID, &group.Name, &group.Description,
		&group.Status, &group.Visibility, &group.FoundingThreshold,
		&group.TrustedVouchThreshold, &group.DiscoverableByUnverified,
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

	// Get signal groups owned by this group
	signalGroups, err := r.GetSignalGroups(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get signal groups: %w", err)
	}
	result.SignalGroups = signalGroups

	return result, nil
}

// ListByUser returns all groups where the given user is a member, with details.
func (r *GroupRepository) ListByUser(ctx context.Context, userID string) ([]models.GroupWithDetails, error) {
	query := `
		SELECT g.id, g.name, g.description, g.status, g.visibility, g.founding_threshold,
			g.trusted_vouch_threshold, g.discoverable_by_unverified,
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
			&gwd.TrustedVouchThreshold, &gwd.DiscoverableByUnverified,
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
			g.trusted_vouch_threshold, g.discoverable_by_unverified,
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
			&gwd.TrustedVouchThreshold, &gwd.DiscoverableByUnverified,
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

// groupBrowseColumns is the shared SELECT column list for browse queries.
const groupBrowseColumns = `
	g.id, g.name, g.description, g.status, g.visibility, g.founding_threshold,
	g.trusted_vouch_threshold, g.discoverable_by_unverified,
	g.created_by, g.created_at, g.updated_at, g.graduated_at,
	(SELECT COUNT(*) FROM group_members WHERE group_id = g.id) as member_count,
	(SELECT SUM(CASE WHEN is_admin = TRUE THEN 1 ELSE 0 END) FROM group_members WHERE group_id = g.id) as admin_count
`

// scanBrowseGroup scans a row from a browse query into a GroupWithDetails.
func scanBrowseGroup(rows *sql.Rows) (models.GroupWithDetails, error) {
	var gwd models.GroupWithDetails
	err := rows.Scan(
		&gwd.ID, &gwd.Name, &gwd.Description,
		&gwd.Status, &gwd.Visibility, &gwd.FoundingThreshold,
		&gwd.TrustedVouchThreshold, &gwd.DiscoverableByUnverified,
		&gwd.CreatedBy, &gwd.CreatedAt, &gwd.UpdatedAt, &gwd.GraduatedAt,
		&gwd.MemberCount, &gwd.AdminCount,
	)
	return gwd, err
}

// enrichBrowseGroup populates regions and topic tags on a scanned GroupWithDetails.
func (r *GroupRepository) enrichBrowseGroup(ctx context.Context, gwd *models.GroupWithDetails) error {
	var err error
	gwd.Regions, err = r.GetRegions(ctx, gwd.ID)
	if err != nil {
		return fmt.Errorf("get regions for group %s: %w", gwd.ID, err)
	}

	gwd.TopicTags, err = r.GetTopicTags(ctx, gwd.ID)
	if err != nil {
		return fmt.Errorf("get tags for group %s: %w", gwd.ID, err)
	}

	return nil
}

// BrowseByRegion returns listed active groups visible for a given region.
// When includeUnverifiedDiscoverable is true, it also includes discoverable_by_unverified
// groups from any region that have at least one open-tier signal group.
func (r *GroupRepository) BrowseByRegion(ctx context.Context, regionID string, includeUnverifiedDiscoverable bool) ([]models.GroupWithDetails, error) {
	var query string

	if includeUnverifiedDiscoverable {
		query = `
			SELECT ` + groupBrowseColumns + `
			FROM ` + "`groups`" + ` g
			JOIN group_regions gr ON g.id = gr.group_id
			WHERE gr.region_id = ?
				AND g.visibility = 'listed'
				AND g.status = 'active'

			UNION

			SELECT ` + groupBrowseColumns + `
			FROM ` + "`groups`" + ` g
			WHERE g.visibility = 'listed'
				AND g.status = 'active'
				AND g.discoverable_by_unverified = TRUE
				AND EXISTS (
					SELECT 1 FROM signal_groups sg
					WHERE sg.owner_group_id = g.id
						AND sg.access_tier = 'open'
						AND sg.is_active = TRUE
				)

			ORDER BY RAND()
		`
	} else {
		query = `
			SELECT ` + groupBrowseColumns + `
			FROM ` + "`groups`" + ` g
			JOIN group_regions gr ON g.id = gr.group_id
			WHERE gr.region_id = ?
				AND g.visibility = 'listed'
				AND g.status = 'active'
			ORDER BY RAND()
		`
	}

	rows, err := r.db.QueryContext(ctx, query, regionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []models.GroupWithDetails
	for rows.Next() {
		gwd, err := scanBrowseGroup(rows)
		if err != nil {
			return nil, err
		}

		if err := r.enrichBrowseGroup(ctx, &gwd); err != nil {
			return nil, err
		}

		groups = append(groups, gwd)
	}

	return groups, rows.Err()
}

// BrowseAll returns all listed active discoverable groups that have at least one
// open-tier signal group. Intended for unverified users or users without a region.
func (r *GroupRepository) BrowseAll(ctx context.Context) ([]models.GroupWithDetails, error) {
	query := `
		SELECT ` + groupBrowseColumns + `
		FROM ` + "`groups`" + ` g
		WHERE g.visibility = 'listed'
			AND g.status = 'active'
			AND g.discoverable_by_unverified = TRUE
			AND EXISTS (
				SELECT 1 FROM signal_groups sg
				WHERE sg.owner_group_id = g.id
					AND sg.access_tier = 'open'
					AND sg.is_active = TRUE
			)
		ORDER BY RAND()
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []models.GroupWithDetails
	for rows.Next() {
		gwd, err := scanBrowseGroup(rows)
		if err != nil {
			return nil, err
		}

		if err := r.enrichBrowseGroup(ctx, &gwd); err != nil {
			return nil, err
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
	if req.DiscoverableByUnverified != nil {
		setClauses = append(setClauses, "discoverable_by_unverified = ?")
		args = append(args, *req.DiscoverableByUnverified)
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
// Signal Groups
// =============================================================================

// GetSignalGroups returns public signal group data for groups owned by this group.
func (r *GroupRepository) GetSignalGroups(ctx context.Context, groupID string) ([]models.SignalGroupPublic, error) {
	query := `
		SELECT id, owner_group_id, group_name, description, access_tier, created_at,
			EXISTS (SELECT 1 FROM deletion_proposals WHERE asset_type = 'signal_group' AND asset_id = sg.id AND status = 'pending') AS has_pending_deletion
		FROM signal_groups sg
		WHERE sg.owner_group_id = ? AND sg.is_active = TRUE
		ORDER BY sg.created_at
	`

	rows, err := r.db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var signalGroups []models.SignalGroupPublic
	for rows.Next() {
		var sg models.SignalGroupPublic
		if err := rows.Scan(
			&sg.ID, &sg.OwnerGroupID, &sg.Name, &sg.Description,
			&sg.AccessTier, &sg.CreatedAt, &sg.HasPendingDeletion,
		); err != nil {
			return nil, err
		}
		signalGroups = append(signalGroups, sg)
	}

	return signalGroups, rows.Err()
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

// =============================================================================
// GetMember
// =============================================================================

// GetMember retrieves a single group member record.
// Returns nil, nil if the user is not a member (not an error).
func (r *GroupRepository) GetMember(ctx context.Context, groupID, userID string) (*models.GroupMember, error) {
	query := `
		SELECT id, group_id, user_id, is_admin, is_founding_member, trust_level, joined_at
		FROM group_members
		WHERE group_id = ? AND user_id = ?
	`

	member := &models.GroupMember{}
	err := r.db.QueryRowContext(ctx, query, groupID, userID).Scan(
		&member.ID, &member.GroupID, &member.UserID,
		&member.IsAdmin, &member.IsFoundingMember,
		&member.TrustLevel, &member.JoinedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return member, nil
}

// =============================================================================
// Trust Vouches
// =============================================================================

// CreateTrustVouch records a trust vouch from one member to another.
// If the vouched user reaches the group's trusted_vouch_threshold, they are
// automatically promoted to trust_level='trusted'.
func (r *GroupRepository) CreateTrustVouch(ctx context.Context, groupID, voucherUserID, vouchedUserID string) error {
	if voucherUserID == vouchedUserID {
		return ErrSelfVouch
	}

	var promoted bool

	err := r.db.Transaction(ctx, func(tx *sql.Tx) error {
		// Verify both users are members and voucher is trusted or admin
		var voucherTrustLevel string
		var voucherIsAdmin bool
		err := tx.QueryRowContext(ctx,
			"SELECT trust_level, is_admin FROM group_members WHERE group_id = ? AND user_id = ?",
			groupID, voucherUserID,
		).Scan(&voucherTrustLevel, &voucherIsAdmin)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotTrustedOrAdmin
		}
		if err != nil {
			return fmt.Errorf("check voucher membership: %w", err)
		}

		if voucherTrustLevel != "trusted" && !voucherIsAdmin {
			return ErrNotTrustedOrAdmin
		}

		// Verify vouched user is a member
		var vouchedExists bool
		err = tx.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM group_members WHERE group_id = ? AND user_id = ?)",
			groupID, vouchedUserID,
		).Scan(&vouchedExists)
		if err != nil {
			return fmt.Errorf("check vouched membership: %w", err)
		}
		if !vouchedExists {
			return ErrGroupNotFound
		}

		// Insert the vouch
		vouchID := uuid.New().String()
		_, err = tx.ExecContext(ctx,
			"INSERT INTO group_trust_vouches (id, group_id, voucher_user_id, vouched_user_id, created_at) VALUES (?, ?, ?, ?, ?)",
			vouchID, groupID, voucherUserID, vouchedUserID, time.Now().UTC(),
		)
		if err != nil {
			return fmt.Errorf("insert trust vouch: %w", err)
		}

		// Count vouches for the vouched user
		var vouchCount int
		err = tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM group_trust_vouches WHERE group_id = ? AND vouched_user_id = ?",
			groupID, vouchedUserID,
		).Scan(&vouchCount)
		if err != nil {
			return fmt.Errorf("count vouches: %w", err)
		}

		// Check threshold from the group
		var threshold int
		err = tx.QueryRowContext(ctx,
			"SELECT trusted_vouch_threshold FROM `groups` WHERE id = ?",
			groupID,
		).Scan(&threshold)
		if err != nil {
			return fmt.Errorf("get threshold: %w", err)
		}

		// Promote if threshold met
		if vouchCount >= threshold {
			_, err = tx.ExecContext(ctx,
				"UPDATE group_members SET trust_level = 'trusted' WHERE group_id = ? AND user_id = ?",
				groupID, vouchedUserID,
			)
			if err != nil {
				return fmt.Errorf("promote to trusted: %w", err)
			}
			promoted = true
		}

		return nil
	})

	_ = promoted // available for future use (e.g., returning promotion status)
	return err
}

// GetTrustVouchCount returns the number of trust vouches a user has received in a group.
func (r *GroupRepository) GetTrustVouchCount(ctx context.Context, groupID, userID string) (int, error) {
	query := "SELECT COUNT(*) FROM group_trust_vouches WHERE group_id = ? AND vouched_user_id = ?"
	var count int
	err := r.db.QueryRowContext(ctx, query, groupID, userID).Scan(&count)
	return count, err
}

// ListTrustVouchesForUser returns all trust vouches received by a user in a group.
func (r *GroupRepository) ListTrustVouchesForUser(ctx context.Context, groupID, userID string) ([]models.GroupTrustVouch, error) {
	query := `
		SELECT id, group_id, voucher_user_id, vouched_user_id, created_at
		FROM group_trust_vouches
		WHERE group_id = ? AND vouched_user_id = ?
		ORDER BY created_at
	`

	rows, err := r.db.QueryContext(ctx, query, groupID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vouches []models.GroupTrustVouch
	for rows.Next() {
		var vouch models.GroupTrustVouch
		if err := rows.Scan(
			&vouch.ID, &vouch.GroupID, &vouch.VoucherUserID,
			&vouch.VouchedUserID, &vouch.CreatedAt,
		); err != nil {
			return nil, err
		}
		vouches = append(vouches, vouch)
	}

	return vouches, rows.Err()
}

// =============================================================================
// Access Tier Enforcement
// =============================================================================

// UserMeetsAccessTier checks if a user meets the access tier requirement for a signal group.
// memberInfo should be nil if user is not a member.
func UserMeetsAccessTier(
	tier models.AccessTier,
	isAuthenticated bool,
	isVerifiedResident bool,
	memberInfo *models.GroupMember,
) bool {
	switch tier {
	case models.AccessTierOpen:
		return isAuthenticated
	case models.AccessTierResident:
		return isVerifiedResident
	case models.AccessTierMember:
		return memberInfo != nil
	case models.AccessTierTrusted:
		return memberInfo != nil && (memberInfo.TrustLevel == "trusted" || memberInfo.IsAdmin)
	case models.AccessTierAdminOnly:
		return memberInfo != nil && memberInfo.IsAdmin
	default:
		return false
	}
}

// =============================================================================
// Group Resources
// =============================================================================

// CreateResource creates a new resource link for a group.
func (r *GroupRepository) CreateResource(ctx context.Context, groupID, createdBy string, req *models.CreateResourceRequest) (*models.GroupResource, error) {
	resourceID := uuid.New().String()

	var description *string
	if req.Description != "" {
		description = &req.Description
	}

	query := `
		INSERT INTO group_resources (id, group_id, title, url, description, access_tier, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	_, err := r.db.ExecContext(ctx, query,
		resourceID, groupID, req.Title, req.URL, description, req.AccessTier, createdBy,
	)
	if err != nil {
		return nil, fmt.Errorf("insert group resource: %w", err)
	}

	return r.GetResource(ctx, resourceID)
}

// GetResource retrieves a single resource by ID.
func (r *GroupRepository) GetResource(ctx context.Context, id string) (*models.GroupResource, error) {
	query := `
		SELECT id, group_id, title, url, description, access_tier, created_by, created_at, updated_at
		FROM group_resources
		WHERE id = ?
	`
	var resource models.GroupResource
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&resource.ID, &resource.GroupID, &resource.Title, &resource.URL,
		&resource.Description, &resource.AccessTier, &resource.CreatedBy,
		&resource.CreatedAt, &resource.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrResourceNotFound
		}
		return nil, fmt.Errorf("get group resource: %w", err)
	}
	return &resource, nil
}

// ListResources returns all resources for a group, ordered by created_at.
func (r *GroupRepository) ListResources(ctx context.Context, groupID string) ([]models.GroupResource, error) {
	query := `
		SELECT id, group_id, title, url, description, access_tier, created_by, created_at, updated_at
		FROM group_resources
		WHERE group_id = ?
		ORDER BY created_at, id
	`
	rows, err := r.db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group resources: %w", err)
	}
	defer rows.Close()

	var resources []models.GroupResource
	for rows.Next() {
		var resource models.GroupResource
		if err := rows.Scan(
			&resource.ID, &resource.GroupID, &resource.Title, &resource.URL,
			&resource.Description, &resource.AccessTier, &resource.CreatedBy,
			&resource.CreatedAt, &resource.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan group resource: %w", err)
		}
		resources = append(resources, resource)
	}
	return resources, rows.Err()
}

// UpdateResource updates a resource's fields. Only non-nil fields are updated.
func (r *GroupRepository) UpdateResource(ctx context.Context, id string, req *models.UpdateResourceRequest) error {
	var setClauses []string
	var args []interface{}

	if req.Title != nil {
		setClauses = append(setClauses, "title = ?")
		args = append(args, *req.Title)
	}
	if req.URL != nil {
		setClauses = append(setClauses, "url = ?")
		args = append(args, *req.URL)
	}
	if req.Description != nil {
		setClauses = append(setClauses, "description = ?")
		args = append(args, *req.Description)
	}
	if req.AccessTier != nil {
		setClauses = append(setClauses, "access_tier = ?")
		args = append(args, *req.AccessTier)
	}

	if len(setClauses) == 0 {
		return nil
	}

	args = append(args, id)
	query := "UPDATE group_resources SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update group resource: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update group resource rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrResourceNotFound
	}
	return nil
}

// DeleteResource deletes a resource by ID.
func (r *GroupRepository) DeleteResource(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, "DELETE FROM group_resources WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete group resource: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete group resource rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrResourceNotFound
	}
	return nil
}

// BlockGroup creates a block from blockerGroupID to blockedGroupID.
func (r *GroupRepository) BlockGroup(ctx context.Context, blockerGroupID, blockedGroupID string) error {
	if blockerGroupID == blockedGroupID {
		return ErrCannotBlockSelf
	}

	id := uuid.New().String()
	query := `INSERT INTO group_blocks (id, blocker_group_id, blocked_group_id) VALUES (?, ?, ?)`
	_, err := r.db.ExecContext(ctx, query, id, blockerGroupID, blockedGroupID)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") {
			return ErrGroupAlreadyBlocked
		}
		return fmt.Errorf("block group: %w", err)
	}
	return nil
}

// UnblockGroup removes a block from blockerGroupID to blockedGroupID.
func (r *GroupRepository) UnblockGroup(ctx context.Context, blockerGroupID, blockedGroupID string) error {
	result, err := r.db.ExecContext(ctx,
		"DELETE FROM group_blocks WHERE blocker_group_id = ? AND blocked_group_id = ?",
		blockerGroupID, blockedGroupID,
	)
	if err != nil {
		return fmt.Errorf("unblock group: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("unblock group rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrGroupNotFound
	}
	return nil
}

// IsGroupBlocked checks if blockerGroupID has blocked blockedGroupID.
func (r *GroupRepository) IsGroupBlocked(ctx context.Context, blockerGroupID, blockedGroupID string) (bool, error) {
	query := "SELECT EXISTS(SELECT 1 FROM group_blocks WHERE blocker_group_id = ? AND blocked_group_id = ?)"
	var exists bool
	err := r.db.QueryRowContext(ctx, query, blockerGroupID, blockedGroupID).Scan(&exists)
	return exists, err
}

// ListBlockedGroups returns all groups blocked by the given group.
func (r *GroupRepository) ListBlockedGroups(ctx context.Context, groupID string) ([]models.Group, error) {
	query := `
		SELECT g.id, g.name, g.description, g.status, g.visibility, g.founding_threshold,
			g.trusted_vouch_threshold, g.discoverable_by_unverified,
			g.created_by, g.created_at, g.updated_at, g.graduated_at
		FROM group_blocks gb
		JOIN ` + "`groups`" + ` g ON g.id = gb.blocked_group_id
		WHERE gb.blocker_group_id = ?
		ORDER BY gb.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, fmt.Errorf("list blocked groups: %w", err)
	}
	defer rows.Close()

	var groups []models.Group
	for rows.Next() {
		var g models.Group
		err := rows.Scan(
			&g.ID, &g.Name, &g.Description, &g.Status, &g.Visibility, &g.FoundingThreshold,
			&g.TrustedVouchThreshold, &g.DiscoverableByUnverified,
			&g.CreatedBy, &g.CreatedAt, &g.UpdatedAt, &g.GraduatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan blocked group: %w", err)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}
