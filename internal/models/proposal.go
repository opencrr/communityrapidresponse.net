package models

import (
	"time"
)

// ProposalStatus represents the status of any consensus proposal
type ProposalStatus string

const (
	ProposalStatusPending                  ProposalStatus = "pending"
	ProposalStatusApproved                 ProposalStatus = "approved"
	ProposalStatusRejected                 ProposalStatus = "rejected"
	ProposalStatusExpired                  ProposalStatus = "expired"
	ProposalStatusApprovedPendingFinalization ProposalStatus = "approved_pending_finalization"
)

// AssetType represents the type of asset being proposed for deletion
type AssetType string

const (
	AssetTypeSignalGroup AssetType = "signal_group"
	AssetTypeSubRegion   AssetType = "sub_region"
)

// DeletionProposal represents a proposal to delete an asset
type DeletionProposal struct {
	ID         string         `json:"id" db:"id"`
	AssetType  AssetType      `json:"asset_type" db:"asset_type"`
	AssetID    string         `json:"asset_id" db:"asset_id"`
	RegionID   *string        `json:"region_id,omitempty" db:"region_id"`
	SchoolID   *string        `json:"school_id,omitempty" db:"school_id"`
	DistrictID *string        `json:"district_id,omitempty" db:"district_id"`
	ProposedBy *string        `json:"proposed_by,omitempty" db:"proposed_by"`
	Reason     *string        `json:"reason,omitempty" db:"reason"`
	Status     ProposalStatus `json:"status" db:"status"`
	CreatedAt  time.Time      `json:"created_at" db:"created_at"`
	ResolvedAt *time.Time     `json:"resolved_at,omitempty" db:"resolved_at"`
}

// DeletionVote represents a vote on a deletion proposal
type DeletionVote struct {
	ID         string    `json:"id" db:"id"`
	ProposalID string    `json:"proposal_id" db:"proposal_id"`
	VoterID    string    `json:"voter_id" db:"voter_id"`
	Vote       bool      `json:"vote" db:"vote"` // true=approve, false=reject
	VotedAt    time.Time `json:"voted_at" db:"voted_at"`
}

// CreateDeletionProposalRequest represents request to propose deletion
type CreateDeletionProposalRequest struct {
	AssetType AssetType `json:"asset_type" validate:"required,oneof=signal_group sub_region"`
	AssetID   string    `json:"asset_id" validate:"required,uuid"`
	Reason    string    `json:"reason" validate:"required,min=1"`
}

// DeletionProposalResponse represents response for deletion proposals
type DeletionProposalResponse struct {
	ProposalID   string `json:"proposal_id"`
	Status       string `json:"status"`
	VotesNeeded  int    `json:"votes_needed"`
	CurrentVotes int    `json:"current_votes"`
	YourVote     *bool  `json:"your_vote,omitempty"`
}

// DeletionProposalListResponse represents response for listing deletion proposals
type DeletionProposalListResponse struct {
	Proposals []*DeletionProposalSummary `json:"proposals"`
}

// DeletionProposalSummary represents a deletion proposal summary
type DeletionProposalSummary struct {
	ID          string    `json:"id"`
	AssetType   string    `json:"asset_type"`
	AssetName   string    `json:"asset_name"`
	Reason      *string   `json:"reason,omitempty"`
	ProposedBy  string    `json:"proposed_by"`
	Votes       int       `json:"votes"`
	VotesNeeded int       `json:"votes_needed"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

// DeletionProposalDetailResponse represents detailed proposal info with votes
type DeletionProposalDetailResponse struct {
	ID           string              `json:"id"`
	AssetType    string              `json:"asset_type"`
	AssetID      string              `json:"asset_id"`
	AssetName    string              `json:"asset_name"`
	RegionID     string              `json:"region_id,omitempty"`
	RegionName   string              `json:"region_name,omitempty"`
	SchoolID     string              `json:"school_id,omitempty"`
	SchoolName   string              `json:"school_name,omitempty"`
	DistrictID   string              `json:"district_id,omitempty"`
	DistrictName string              `json:"district_name,omitempty"`
	ProposedBy   ProposalUser        `json:"proposed_by"`
	Reason       string              `json:"reason"`
	Status       string              `json:"status"`
	VotesNeeded  int                 `json:"votes_needed"`
	CurrentVotes int                 `json:"current_votes"`
	CanVote      bool                `json:"can_vote"`
	HasVoted     bool                `json:"has_voted"`
	Votes        []DeletionVoteDetail `json:"votes"`
	CreatedAt    time.Time           `json:"created_at"`
	ResolvedAt   *time.Time          `json:"resolved_at,omitempty"`
}

// DeletionVoteDetail represents a vote on a deletion proposal
type DeletionVoteDetail struct {
	VoterID  string    `json:"voter_id"`
	Username string    `json:"username"`
	Vote     bool      `json:"vote"`
	VotedAt  time.Time `json:"voted_at"`
}

// BlocklistProposal represents a proposal to block a user
type BlocklistProposal struct {
	ID           string         `json:"id" db:"id"`
	TargetUserID string         `json:"target_user_id" db:"target_user_id"`
	RegionID     *string        `json:"region_id,omitempty" db:"region_id"`
	ProposedBy   *string        `json:"proposed_by,omitempty" db:"proposed_by"`
	Reason       string         `json:"reason" db:"reason"`
	Evidence     *string        `json:"evidence,omitempty" db:"evidence"`
	Status       ProposalStatus `json:"status" db:"status"`
	CreatedAt    time.Time      `json:"created_at" db:"created_at"`
	ExpiresAt    time.Time      `json:"expires_at" db:"expires_at"`
	ResolvedAt   *time.Time     `json:"resolved_at,omitempty" db:"resolved_at"`
}

// BlocklistVote represents a vote on a blocklist proposal
type BlocklistVote struct {
	ID         string    `json:"id" db:"id"`
	ProposalID string    `json:"proposal_id" db:"proposal_id"`
	VoterID    string    `json:"voter_id" db:"voter_id"`
	Vote       bool      `json:"vote" db:"vote"` // true=approve block, false=reject
	VotedAt    time.Time `json:"voted_at" db:"voted_at"`
}

// BlockedUser represents a blocked user in a region
type BlockedUser struct {
	ID           string     `json:"id" db:"id"`
	UserID       string     `json:"user_id" db:"user_id"`
	RegionID     string     `json:"region_id" db:"region_id"`
	ProposalID   *string    `json:"proposal_id,omitempty" db:"proposal_id"`
	BlockedAt    time.Time  `json:"blocked_at" db:"blocked_at"`
	BlockedUntil *time.Time `json:"blocked_until,omitempty" db:"blocked_until"`
}

// CreateBlocklistProposalRequest represents request to propose blocking
type CreateBlocklistProposalRequest struct {
	TargetUserID string  `json:"target_user_id" validate:"required,uuid"`
	RegionID     string  `json:"region_id" validate:"required,uuid"`
	Reason       string  `json:"reason" validate:"required,min=1"`
	Evidence     *string `json:"evidence,omitempty"`
}

// BlocklistProposalResponse represents response for blocklist proposals
type BlocklistProposalResponse struct {
	ProposalID   string `json:"proposal_id"`
	Status       string `json:"status"`
	VotesNeeded  int    `json:"votes_needed"`
	CurrentVotes int    `json:"current_votes"`
	YourVote     *bool  `json:"your_vote,omitempty"`
}

// BlocklistProposalListResponse represents response for listing blocklist proposals
type BlocklistProposalListResponse struct {
	Proposals []*BlocklistProposalSummary `json:"proposals"`
}

// BlocklistProposalSummary represents a blocklist proposal summary
type BlocklistProposalSummary struct {
	ID          string     `json:"id"`
	TargetUser  PublicUser `json:"target_user"`
	RegionName  string     `json:"region_name"`
	Reason      string     `json:"reason"`
	ProposedBy  string     `json:"proposed_by"`
	Votes       int        `json:"votes"`
	VotesNeeded int        `json:"votes_needed"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
}

// BlocklistListResponse represents response for listing blocked users
type BlocklistListResponse struct {
	BlockedUsers []BlockedUserInfo `json:"blocked_users"`
}

// BlockedUserInfo represents blocked user info
type BlockedUserInfo struct {
	UserID       string     `json:"user_id"`
	Username     string     `json:"username"`
	BlockedAt    time.Time  `json:"blocked_at"`
	BlockedUntil *time.Time `json:"blocked_until,omitempty"`
	Reason       string     `json:"reason"`
}

// BlocklistProposalDetailResponse represents detailed proposal info with votes
type BlocklistProposalDetailResponse struct {
	ID           string               `json:"id"`
	TargetUser   PublicUser           `json:"target_user"`
	RegionID     string               `json:"region_id"`
	RegionName   string               `json:"region_name"`
	ProposedBy   ProposalUser         `json:"proposed_by"`
	Reason       string               `json:"reason"`
	Evidence     string               `json:"evidence,omitempty"`
	Status       string               `json:"status"`
	VotesNeeded  int                  `json:"votes_needed"`
	CurrentVotes int                  `json:"current_votes"`
	CanVote      bool                 `json:"can_vote"`
	HasVoted     bool                 `json:"has_voted"`
	Votes        []BlocklistVoteDetail `json:"votes"`
	CreatedAt    time.Time            `json:"created_at"`
	ExpiresAt    time.Time            `json:"expires_at"`
	ResolvedAt   *time.Time           `json:"resolved_at,omitempty"`
}

// BlocklistVoteDetail represents a vote on a blocklist proposal
type BlocklistVoteDetail struct {
	VoterID  string    `json:"voter_id"`
	Username string    `json:"username"`
	Vote     bool      `json:"vote"`
	VotedAt  time.Time `json:"voted_at"`
}

// BlockedAddress represents a blocked address hash
type BlockedAddress struct {
	ID          string    `json:"id" db:"id"`
	AddressHash string    `json:"address_hash" db:"address_hash"`
	UserID      string    `json:"user_id" db:"user_id"`
	Username    string    `json:"username,omitempty"` // Joined from users table
	ProposalID  *string   `json:"proposal_id,omitempty" db:"proposal_id"`
	BlockedAt   time.Time `json:"blocked_at" db:"blocked_at"`
	ExpiresAt   time.Time `json:"expires_at" db:"expires_at"`
	IsActive    bool      `json:"is_active"` // Computed: expires_at > NOW()
}

// BlockedAddressListResponse represents response for listing blocked addresses
type BlockedAddressListResponse struct {
	Addresses []*BlockedAddress `json:"addresses"`
}
