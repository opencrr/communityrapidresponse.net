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
	FoundingThreshold     *int            `json:"founding_threshold,omitempty" db:"founding_threshold"`
	TrustedVouchThreshold int             `json:"trusted_vouch_threshold" db:"trusted_vouch_threshold"`
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

// AccessTier defines the access level required to see a signal chat's invite link.
type AccessTier string

const (
	AccessTierOpen      AccessTier = "open"
	AccessTierResident  AccessTier = "resident"
	AccessTierMember    AccessTier = "member"
	AccessTierTrusted   AccessTier = "trusted"
	AccessTierAdminOnly AccessTier = "admin_only"
)

// GroupTrustVouch represents a trust vouch from one member to another within a group.
type GroupTrustVouch struct {
	ID            string    `json:"id" db:"id"`
	GroupID       string    `json:"group_id" db:"group_id"`
	VoucherUserID string    `json:"voucher_user_id" db:"voucher_user_id"`
	VouchedUserID string    `json:"vouched_user_id" db:"vouched_user_id"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

// CreateTrustVouchRequest is the request body for vouching for a group member.
type CreateTrustVouchRequest struct {
	UserID string `json:"user_id" validate:"required"`
}

// Invitation status
type InvitationStatus string

const (
	InvitationStatusPending  InvitationStatus = "pending"
	InvitationStatusAccepted InvitationStatus = "accepted"
	InvitationStatusDeclined InvitationStatus = "declined"
	InvitationStatusExpired  InvitationStatus = "expired"
)

// GroupInviteLink is a shareable link for joining a group.
type GroupInviteLink struct {
	ID        string     `json:"id" db:"id"`
	GroupID   string     `json:"group_id" db:"group_id"`
	Token     string     `json:"token" db:"token"`
	CreatedBy *string    `json:"created_by" db:"created_by"`
	ExpiresAt *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	MaxUses   *int       `json:"max_uses,omitempty" db:"max_uses"`
	UseCount  int        `json:"use_count" db:"use_count"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
}

// CreateInviteLinkRequest is the request body for creating an invite link.
type CreateInviteLinkRequest struct {
	ExpiresInHours *int `json:"expires_in_hours" validate:"omitempty,min=1,max=720"`
	MaxUses        *int `json:"max_uses" validate:"omitempty,min=1,max=1000"`
}

// GroupInvitation is a direct invitation from an admin to a specific user.
type GroupInvitation struct {
	ID          string           `json:"id" db:"id"`
	GroupID     string           `json:"group_id" db:"group_id"`
	UserID      string           `json:"user_id" db:"user_id"`
	InvitedBy   *string          `json:"invited_by" db:"invited_by"`
	Status      InvitationStatus `json:"status" db:"status"`
	CreatedAt   time.Time        `json:"created_at" db:"created_at"`
	ExpiresAt   *time.Time       `json:"expires_at,omitempty" db:"expires_at"`
	RespondedAt *time.Time       `json:"responded_at,omitempty" db:"responded_at"`
}

// GroupInvitationWithDetails includes group and user info for display.
type GroupInvitationWithDetails struct {
	GroupInvitation
	GroupName   string  `json:"group_name" db:"group_name"`
	InviterName *string `json:"inviter_name,omitempty" db:"inviter_name"`
}

// CreateGroupInvitationRequest is the request body for inviting a user to a group.
type CreateGroupInvitationRequest struct {
	UserID string `json:"user_id" validate:"required"`
}

// RespondToGroupInvitationRequest is the request body for accepting/declining a group invitation.
type RespondToGroupInvitationRequest struct {
	Accept bool `json:"accept"`
}
