package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

var ErrGroupNotFound = errors.New("group not found")

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
