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
	TrustedVouchThreshold    int             `json:"trusted_vouch_threshold" db:"trusted_vouch_threshold"`
	DiscoverableByUnverified bool            `json:"discoverable_by_unverified" db:"discoverable_by_unverified"`
	CreatedBy         *string         `json:"created_by" db:"created_by"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at" db:"updated_at"`
	GraduatedAt       *time.Time      `json:"graduated_at,omitempty" db:"graduated_at"`
}

// GroupWithDetails includes membership counts and related data.
type GroupWithDetails struct {
	Group
	MemberCount  int                `json:"member_count"`
	AdminCount   int                `json:"admin_count"`
	Regions      []RegionSummary    `json:"regions"`
	TopicTags    []string           `json:"topic_tags"`
	SignalGroups []SignalGroupPublic `json:"signal_groups"`
	IsUserMember bool               `json:"is_user_member"`
	IsUserAdmin  bool               `json:"is_user_admin"`
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
	TopicTags                *[]string `json:"topic_tags" validate:"omitempty,max=10,dive,max=100"`
	DiscoverableByUnverified *bool     `json:"discoverable_by_unverified"`
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

// CreateGroupSignalGroupRequest is the request body for creating a signal group under a group.
type CreateGroupSignalGroupRequest struct {
	GroupName   string `json:"group_name" validate:"required,min=1,max=255"`
	Description string `json:"description" validate:"max=1000"`
	AccessTier  string `json:"access_tier" validate:"required,oneof=open resident member trusted admin_only"`
}

// GroupResource represents a resource link attached to a group.
type GroupResource struct {
	ID          string     `json:"id" db:"id"`
	GroupID     string     `json:"group_id" db:"group_id"`
	Title       string     `json:"title" db:"title"`
	URL         string     `json:"url" db:"url"`
	Description *string    `json:"description" db:"description"`
	AccessTier  AccessTier `json:"access_tier" db:"access_tier"`
	CreatedBy   *string    `json:"created_by" db:"created_by"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
}

// CreateResourceRequest is the request body for creating a group resource link.
type CreateResourceRequest struct {
	Title       string `json:"title" validate:"required,min=1,max=255"`
	URL         string `json:"url" validate:"required,max=2048"`
	Description string `json:"description" validate:"max=500"`
	AccessTier  string `json:"access_tier" validate:"required,oneof=open resident member trusted admin_only"`
}

// UpdateResourceRequest is the request body for updating a group resource link.
type UpdateResourceRequest struct {
	Title       *string `json:"title" validate:"omitempty,min=1,max=255"`
	URL         *string `json:"url" validate:"omitempty,max=2048"`
	Description *string `json:"description" validate:"omitempty,max=500"`
	AccessTier  *string `json:"access_tier" validate:"omitempty,oneof=open resident member trusted admin_only"`
}

// BlockGroupRequest is the request body for blocking a group.
type BlockGroupRequest struct {
	GroupID string `json:"group_id" validate:"required"`
}

// Connection represents a group of groups.
type Connection struct {
	ID        string    `json:"id" db:"id"`
	Name      *string   `json:"name" db:"name"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// ConnectionWithDetails includes member groups.
type ConnectionWithDetails struct {
	Connection
	MemberGroups []ConnectionMemberGroup `json:"member_groups"`
}

// ConnectionMemberGroup is a group's info within a connection.
type ConnectionMemberGroup struct {
	GroupID   string    `json:"group_id" db:"group_id"`
	GroupName string    `json:"group_name" db:"group_name"`
	JoinedAt  time.Time `json:"joined_at" db:"joined_at"`
}

// ConnectionProposal tracks formation or expansion proposals.
type ConnectionProposal struct {
	ID              string     `json:"id" db:"id"`
	ConnectionID    *string    `json:"connection_id" db:"connection_id"`
	ProposerGroupID string    `json:"proposer_group_id" db:"proposer_group_id"`
	ProposalType    string     `json:"proposal_type" db:"proposal_type"`
	Status          string     `json:"status" db:"status"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty" db:"expires_at"`
}

// ConnectionProposalGroup tracks per-group status on a proposal.
type ConnectionProposalGroup struct {
	ID          string     `json:"id" db:"id"`
	ProposalID  string     `json:"proposal_id" db:"proposal_id"`
	GroupID     string     `json:"group_id" db:"group_id"`
	Status      string     `json:"status" db:"status"`
	RespondedAt *time.Time `json:"responded_at,omitempty" db:"responded_at"`
}

// ConnectionProposalWithGroups includes the per-group statuses.
type ConnectionProposalWithGroups struct {
	ConnectionProposal
	Groups []ConnectionProposalGroup `json:"groups"`
}

// ProposeConnectionRequest is the request for proposing a new connection.
type ProposeConnectionRequest struct {
	Name     string   `json:"name" validate:"max=255"`
	GroupIDs []string `json:"group_ids" validate:"required,min=1"`
}

// InviteToConnectionRequest is the request for expanding a connection.
type InviteToConnectionRequest struct {
	GroupID string `json:"group_id" validate:"required"`
}

// ConnectionAccessLevel for connection signal chats.
type ConnectionAccessLevel string

const (
	ConnectionAccessLevelAdminOnly  ConnectionAccessLevel = "admin_only"
	ConnectionAccessLevelAllMembers ConnectionAccessLevel = "all_members"
)

// ConnectionChatProposal tracks proposals to create connection signal chats.
type ConnectionChatProposal struct {
	ID              string     `json:"id" db:"id"`
	ConnectionID    string     `json:"connection_id" db:"connection_id"`
	ProposerGroupID string    `json:"proposer_group_id" db:"proposer_group_id"`
	GroupName       string     `json:"group_name" db:"group_name"`
	Description     *string    `json:"description" db:"description"`
	AccessLevel     string     `json:"access_level" db:"access_level"`
	Status          string     `json:"status" db:"status"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty" db:"expires_at"`
}

// ConnectionChatProposalVote tracks per-group votes on a connection chat proposal.
type ConnectionChatProposalVote struct {
	ID          string     `json:"id" db:"id"`
	ProposalID  string     `json:"proposal_id" db:"proposal_id"`
	GroupID     string     `json:"group_id" db:"group_id"`
	Status      string     `json:"status" db:"status"`
	RespondedAt *time.Time `json:"responded_at,omitempty" db:"responded_at"`
}

// ConnectionChatProposalWithVotes includes per-group vote statuses.
type ConnectionChatProposalWithVotes struct {
	ConnectionChatProposal
	Votes []ConnectionChatProposalVote `json:"votes"`
}

// ProposeConnectionChatRequest is the request for proposing a connection signal chat.
type ProposeConnectionChatRequest struct {
	GroupName   string `json:"group_name" validate:"required,min=1,max=255"`
	Description string `json:"description" validate:"max=1000"`
	AccessLevel string `json:"access_level" validate:"required,oneof=admin_only all_members"`
}

// ConnectionSharedResource represents a group resource shared into a connection.
type ConnectionSharedResource struct {
	ID              string    `json:"id" db:"id"`
	ResourceID      string    `json:"resource_id" db:"resource_id"`
	ConnectionID    string    `json:"connection_id" db:"connection_id"`
	SharedByGroupID string    `json:"shared_by_group_id" db:"shared_by_group_id"`
	Visibility      string    `json:"visibility" db:"visibility"`
	SharedAt        time.Time `json:"shared_at" db:"shared_at"`
}

// ConnectionSharedResourceWithDetails includes the resource content.
type ConnectionSharedResourceWithDetails struct {
	ConnectionSharedResource
	Title       string  `json:"title" db:"title"`
	URL         string  `json:"url" db:"url"`
	Description *string `json:"description" db:"description"`
	GroupName   string  `json:"group_name" db:"group_name"`
}

// ShareResourceRequest is the request for sharing a resource into a connection.
type ShareResourceRequest struct {
	ResourceID string `json:"resource_id" validate:"required"`
	Visibility string `json:"visibility" validate:"required,oneof=admin_only all_members"`
}

// TopicBoardPosting represents a group's presence on the topic board.
type TopicBoardPosting struct {
	ID              string    `json:"id" db:"id"`
	GroupID         string    `json:"group_id" db:"group_id"`
	RegionLabel     string    `json:"region_label" db:"region_label"`
	AutoRegionLabel *string   `json:"auto_region_label,omitempty" db:"auto_region_label"`
	Description     string    `json:"description" db:"description"`
	IsActive        bool      `json:"is_active" db:"is_active"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

// TopicBoardPostingWithTags includes the tags for display.
type TopicBoardPostingWithTags struct {
	TopicBoardPosting
	Tags []string `json:"tags"`
}

// CreateTopicBoardPostingRequest is the request for publishing to the topic board.
type CreateTopicBoardPostingRequest struct {
	RegionLabel *string  `json:"region_label" validate:"omitempty,max=255"`
	Description string   `json:"description" validate:"required,min=10,max=500"`
	Tags        []string `json:"tags" validate:"required,min=1,max=5,dive,min=2,max=100"`
}
