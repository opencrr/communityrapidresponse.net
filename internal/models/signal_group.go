package models

import (
	"time"
)

// SignalGroup represents a Signal group associated with a region, school, or district
type SignalGroup struct {
	ID                  string     `json:"id" db:"id"`
	RegionID            *string    `json:"region_id,omitempty" db:"region_id"`
	SchoolID            *string    `json:"school_id,omitempty" db:"school_id"`
	DistrictID          *string    `json:"district_id,omitempty" db:"district_id"`
	RegionName          string     `json:"region_name,omitempty" db:"region_name"`     // Populated by joins
	SchoolName          string     `json:"school_name,omitempty" db:"school_name"`     // Populated by joins
	DistrictName        string     `json:"district_name,omitempty" db:"district_name"` // Populated by joins
	GroupName           string     `json:"group_name" db:"group_name"`
	InviteLink          string     `json:"invite_link,omitempty" db:"invite_link"` // Only shown to verified users
	InviteLinkUpdatedAt time.Time  `json:"invite_link_updated_at" db:"invite_link_updated_at"`
	InviteLinkUpdatedBy *string    `json:"invite_link_updated_by,omitempty" db:"invite_link_updated_by"`
	Description         *string    `json:"description,omitempty" db:"description"`
	CreatedBy           *string    `json:"created_by,omitempty" db:"created_by"`
	CreatedAt           time.Time  `json:"created_at" db:"created_at"`
	IsActive            bool       `json:"is_active" db:"is_active"`
	HasPendingDeletion  bool       `json:"-" db:"has_pending_deletion"`
}

// SignalGroupPublic represents a signal group without sensitive data
type SignalGroupPublic struct {
	ID                  string    `json:"id"`
	RegionID            *string   `json:"region_id,omitempty"`
	SchoolID            *string   `json:"school_id,omitempty"`
	DistrictID          *string   `json:"district_id,omitempty"`
	RegionName          string    `json:"region_name,omitempty"`
	SchoolName          string    `json:"school_name,omitempty"`
	DistrictName        string    `json:"district_name,omitempty"`
	Name                string    `json:"name"`
	Description         *string   `json:"description,omitempty"`
	MemberCountEstimate string    `json:"member_count_estimate,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	HasPendingDeletion  bool      `json:"has_pending_deletion"`
}

// SignalGroupWithInvite includes invite link for verified users
type SignalGroupWithInvite struct {
	SignalGroupPublic
	InviteLink string `json:"invite_link"`
}

// CreateSignalGroupRequest represents request to create a signal group
// Exactly one of RegionID, SchoolID, or DistrictID must be set
type CreateSignalGroupRequest struct {
	RegionID    *string `json:"region_id,omitempty"`
	SchoolID    *string `json:"school_id,omitempty"`
	DistrictID  *string `json:"district_id,omitempty"`
	Name        string  `json:"name" validate:"required,min=1,max=255"`
	InviteLink  string  `json:"invite_link" validate:"required,url"`
	Description *string `json:"description,omitempty"`
}

// UpdateSignalGroupRequest represents request to update a signal group
// Note: invite_link updates require consensus - use proposal endpoints
type UpdateSignalGroupRequest struct {
	Name        *string `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	Description *string `json:"description,omitempty"`
}

// SignalGroupListResponse represents response for listing signal groups
type SignalGroupListResponse struct {
	Groups []SignalGroupWithInvite `json:"groups"`
}

// InviteLinkUpdateProposal represents a proposal to update an invite link
type InviteLinkUpdateProposal struct {
	ID            string         `json:"id" db:"id"`
	SignalGroupID string         `json:"signal_group_id" db:"signal_group_id"`
	RegionID      *string        `json:"region_id,omitempty" db:"region_id"`
	ProposedBy    *string        `json:"proposed_by,omitempty" db:"proposed_by"`
	NewInviteLink string         `json:"new_invite_link,omitempty" db:"new_invite_link"` // Only shown to admins
	Reason        *string        `json:"reason,omitempty" db:"reason"`
	Status        ProposalStatus `json:"status" db:"status"`
	CreatedAt     time.Time      `json:"created_at" db:"created_at"`
	ExpiresAt     time.Time      `json:"expires_at" db:"expires_at"`
	ResolvedAt    *time.Time     `json:"resolved_at,omitempty" db:"resolved_at"`
}

// InviteLinkUpdateVote represents a vote on an invite link update proposal
type InviteLinkUpdateVote struct {
	ID         string    `json:"id" db:"id"`
	ProposalID string    `json:"proposal_id" db:"proposal_id"`
	VoterID    string    `json:"voter_id" db:"voter_id"`
	Vote       bool      `json:"vote" db:"vote"`
	VotedAt    time.Time `json:"voted_at" db:"voted_at"`
}

// CreateInviteLinkProposalRequest represents request to propose invite link update
type CreateInviteLinkProposalRequest struct {
	NewInviteLink string  `json:"new_invite_link" validate:"required,url"`
	Reason        *string `json:"reason,omitempty"`
}

// InviteLinkProposalResponse represents response after creating/voting on proposal
type InviteLinkProposalResponse struct {
	ProposalID          string    `json:"proposal_id"`
	SignalGroupID       string    `json:"signal_group_id,omitempty"`
	Status              string    `json:"status"`
	VotesNeeded         int       `json:"votes_needed"`
	CurrentVotes        int       `json:"current_votes"`
	ExpiresAt           time.Time `json:"expires_at,omitempty"`
	YourVote            *bool     `json:"your_vote,omitempty"`
	InviteLinkUpdated   bool      `json:"invite_link_updated,omitempty"`
	NotificationsQueued int       `json:"notifications_queued,omitempty"`
	Message             string    `json:"message,omitempty"`
}

// VoteRequest represents a vote on a proposal
type VoteRequest struct {
	Vote bool `json:"vote"` // true=approve, false=reject
}

// InviteLinkProposalListResponse represents response for listing proposals
type InviteLinkProposalListResponse struct {
	Proposals []*InviteLinkProposalSummary `json:"proposals"`
}

// InviteLinkProposalSummary represents a proposal summary for listings
type InviteLinkProposalSummary struct {
	ID            string    `json:"id"`
	SignalGroupID string    `json:"signal_group_id"`
	GroupName     string    `json:"group_name"`
	RegionName    string    `json:"region_name"`
	ProposedBy    string    `json:"proposed_by"`
	Reason        *string   `json:"reason,omitempty"`
	Votes         int       `json:"votes"`
	VotesNeeded   int       `json:"votes_needed"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// InviteLinkProposalDetailResponse represents full proposal details for GET endpoint
type InviteLinkProposalDetailResponse struct {
	ID              string                 `json:"id"`
	SignalGroupID   string                 `json:"signal_group_id"`
	SignalGroupName string                 `json:"signal_group_name"`
	RegionID        string                 `json:"region_id"`
	RegionName      string                 `json:"region_name"`
	ProposedBy      ProposalUser           `json:"proposed_by"`
	NewInviteLink   string                 `json:"new_invite_link,omitempty"`
	Reason          string                 `json:"reason,omitempty"`
	Status          string                 `json:"status"`
	VotesNeeded     int                    `json:"votes_needed"`
	CurrentVotes    int                    `json:"current_votes"`
	Votes           []InviteLinkVoteDetail `json:"votes"`
	CreatedAt       time.Time              `json:"created_at"`
	ExpiresAt       time.Time              `json:"expires_at"`
	ResolvedAt      *time.Time             `json:"resolved_at,omitempty"`
	CanVote         bool                   `json:"can_vote"`
	HasVoted        bool                   `json:"has_voted"`
}

// ProposalUser represents a user in proposal context
type ProposalUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// InviteLinkVoteDetail represents a vote on an invite link proposal with user details
type InviteLinkVoteDetail struct {
	VoterID  string    `json:"voter_id"`
	Username string    `json:"username"`
	Vote     bool      `json:"vote"`
	VotedAt  time.Time `json:"voted_at"`
}

// ProposalListFilter represents filter options for listing proposals
type ProposalListFilter struct {
	Status        string // "pending", "approved", "expired", "rejected", or "" for all
	SignalGroupID string
	RegionID      string
}
