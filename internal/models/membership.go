package models

import (
	"time"
)

// MembershipRequestType represents the type of membership request
type MembershipRequestType string

const (
	MembershipRequestTypeRequest    MembershipRequestType = "request"    // User requests to join
	MembershipRequestTypeInvitation MembershipRequestType = "invitation" // Admin invites user
)

// MembershipRequestStatus represents the status of a membership request
type MembershipRequestStatus string

const (
	MembershipRequestStatusPending   MembershipRequestStatus = "pending"
	MembershipRequestStatusApproved  MembershipRequestStatus = "approved"
	MembershipRequestStatusRejected  MembershipRequestStatus = "rejected"
	MembershipRequestStatusExpired   MembershipRequestStatus = "expired"
	MembershipRequestStatusCancelled MembershipRequestStatus = "cancelled"
)

// SubRegionMembershipRequest represents a request to join a sub-region
type SubRegionMembershipRequest struct {
	ID             string                  `json:"id" db:"id"`
	UserID         string                  `json:"user_id" db:"user_id"`
	RegionID       string                  `json:"region_id" db:"region_id"`
	ParentRegionID string                  `json:"parent_region_id" db:"parent_region_id"`
	RequestType    MembershipRequestType   `json:"request_type" db:"request_type"`
	Status         MembershipRequestStatus `json:"status" db:"status"`
	InitiatedBy    string                  `json:"initiated_by" db:"initiated_by"`
	CreatedAt      time.Time               `json:"created_at" db:"created_at"`
	ExpiresAt      time.Time               `json:"expires_at" db:"expires_at"`
	ResolvedAt     *time.Time              `json:"resolved_at,omitempty" db:"resolved_at"`
}

// SubRegionMembershipVote represents a vote on a membership request
type SubRegionMembershipVote struct {
	ID        string    `json:"id" db:"id"`
	RequestID string    `json:"request_id" db:"request_id"`
	VoterID   string    `json:"voter_id" db:"voter_id"`
	Vote      bool      `json:"vote" db:"vote"` // true=approve, false=reject
	VotedAt   time.Time `json:"voted_at" db:"voted_at"`
}

// CreateMembershipRequestRequest represents the request body for creating a membership request
type CreateMembershipRequestRequest struct {
	RegionID string `json:"region_id" validate:"required,uuid"`
}

// VoteMembershipRequestRequest represents the request body for voting on a membership request
type VoteMembershipRequestRequest struct {
	Vote bool `json:"vote"` // true=approve, false=reject
}

// CreateInvitationRequest represents the request body for inviting a user to a sub-region
type CreateInvitationRequest struct {
	UserID string `json:"user_id" validate:"required,uuid"`
}

// RespondToInvitationRequest represents the request body for responding to an invitation
type RespondToInvitationRequest struct {
	Accept bool `json:"accept"` // true=accept, false=decline
}

// MembershipRequestResponse represents the response after creating/voting on a request
type MembershipRequestResponse struct {
	RequestID         string    `json:"request_id"`
	UserID            string    `json:"user_id"`
	RegionID          string    `json:"region_id"`
	RequestType       string    `json:"request_type"`
	Status            string    `json:"status"`
	VotesNeeded       int       `json:"votes_needed"`
	CurrentVotes      int       `json:"current_votes"`
	ExpiresAt         time.Time `json:"expires_at"`
	YourVote          *bool     `json:"your_vote,omitempty"`
	MembershipGranted bool      `json:"membership_granted,omitempty"`
	Message           string    `json:"message,omitempty"`
}

// MembershipRequestListResponse represents the response for listing membership requests
type MembershipRequestListResponse struct {
	Requests []MembershipRequestSummary `json:"requests"`
}

// MembershipRequestSummary represents a summary of a membership request for listings
type MembershipRequestSummary struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Username    string    `json:"username"`
	RegionID    string    `json:"region_id"`
	RegionName  string    `json:"region_name"`
	RequestType string    `json:"request_type"`
	Status      string    `json:"status"`
	Votes       int       `json:"votes"`
	VotesNeeded int       `json:"votes_needed"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// MembershipRequestDetailResponse represents full request details
type MembershipRequestDetailResponse struct {
	ID               string                 `json:"id"`
	UserID           string                 `json:"user_id"`
	Username         string                 `json:"username"`
	RegionID         string                 `json:"region_id"`
	RegionName       string                 `json:"region_name"`
	ParentRegionID   string                 `json:"parent_region_id"`
	ParentRegionName string                 `json:"parent_region_name"`
	RequestType      string                 `json:"request_type"`
	InitiatedBy      ProposalUser           `json:"initiated_by"`
	Status           string                 `json:"status"`
	VotesNeeded      int                    `json:"votes_needed"`
	CurrentVotes     int                    `json:"current_votes"`
	Votes            []MembershipVoteDetail `json:"votes"`
	CreatedAt        time.Time              `json:"created_at"`
	ExpiresAt        time.Time              `json:"expires_at"`
	ResolvedAt       *time.Time             `json:"resolved_at,omitempty"`
	CanVote          bool                   `json:"can_vote"`
	HasVoted         bool                   `json:"has_voted"`
}

// MembershipVoteDetail represents a vote with user details
type MembershipVoteDetail struct {
	VoterID  string    `json:"voter_id"`
	Username string    `json:"username"`
	Vote     bool      `json:"vote"`
	VotedAt  time.Time `json:"voted_at"`
}

// MembershipRequestListFilter represents filter options for listing membership requests
type MembershipRequestListFilter struct {
	Status      string // "pending", "approved", "rejected", "expired", "cancelled", or "" for all
	RequestType string // "request", "invitation", or "" for all
	RegionID    string
	UserID      string
}
