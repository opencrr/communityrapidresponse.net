package models

import "time"

// Group status
type GroupStatus string

const (
	GroupStatusProvisional GroupStatus = "provisional"
	GroupStatusActive      GroupStatus = "active"
)

// Group visibility
type GroupVisibility string

const (
	GroupVisibilityListed   GroupVisibility = "listed"
	GroupVisibilityUnlisted GroupVisibility = "unlisted"
)

// Group is an independent organization with geographic scope.
type Group struct {
	ID                string          `json:"id" db:"id"`
	Name              string          `json:"name" db:"name"`
	Description       *string         `json:"description" db:"description"`
	Status            GroupStatus     `json:"status" db:"status"`
	Visibility        GroupVisibility `json:"visibility" db:"visibility"`
	FoundingThreshold *int            `json:"founding_threshold,omitempty" db:"founding_threshold"`
	CreatedBy         *string         `json:"created_by" db:"created_by"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at" db:"updated_at"`
	GraduatedAt       *time.Time      `json:"graduated_at,omitempty" db:"graduated_at"`
}

// GroupWithDetails includes membership counts and related data.
type GroupWithDetails struct {
	Group
	MemberCount  int             `json:"member_count"`
	AdminCount   int             `json:"admin_count"`
	Regions      []RegionSummary `json:"regions"`
	TopicTags    []string        `json:"topic_tags"`
	IsUserMember bool            `json:"is_user_member"`
	IsUserAdmin  bool            `json:"is_user_admin"`
}

// GroupMember represents a user's membership in a group.
type GroupMember struct {
	ID               string    `json:"id" db:"id"`
	GroupID          string    `json:"group_id" db:"group_id"`
	UserID           string    `json:"user_id" db:"user_id"`
	IsAdmin          bool      `json:"is_admin" db:"is_admin"`
	IsFoundingMember bool      `json:"is_founding_member" db:"is_founding_member"`
	TrustLevel       string    `json:"trust_level" db:"trust_level"`
	JoinedAt         time.Time `json:"joined_at" db:"joined_at"`
}

// GroupMemberWithUser includes user details for display.
type GroupMemberWithUser struct {
	GroupMember
	Username         string `json:"username" db:"username"`
	VerificationTier int    `json:"verification_tier" db:"verification_tier"`
}

// CreateGroupRequest is the request body for creating a new group.
type CreateGroupRequest struct {
	Name        string   `json:"name" validate:"required,min=3,max=255"`
	Description string   `json:"description" validate:"max=2000"`
	Visibility  string   `json:"visibility" validate:"required,oneof=listed unlisted"`
	RegionIDs   []string `json:"region_ids" validate:"required,min=1"`
	TopicTags   []string `json:"topic_tags" validate:"max=10,dive,max=100"`
}

// UpdateGroupRequest is the request body for updating a group.
type UpdateGroupRequest struct {
	Name        *string   `json:"name" validate:"omitempty,min=3,max=255"`
	Description *string   `json:"description" validate:"omitempty,max=2000"`
	Visibility  *string   `json:"visibility" validate:"omitempty,oneof=listed unlisted"`
	TopicTags   *[]string `json:"topic_tags" validate:"omitempty,max=10,dive,max=100"`
}
