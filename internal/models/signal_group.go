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
	OwnerGroupID        *string    `json:"owner_group_id,omitempty" db:"owner_group_id"`
	ConnectionID        *string    `json:"connection_id,omitempty" db:"connection_id"`
	RegionName          string     `json:"region_name,omitempty" db:"region_name"`     // Populated by joins
	SchoolName          string     `json:"school_name,omitempty" db:"school_name"`     // Populated by joins
	DistrictName        string     `json:"district_name,omitempty" db:"district_name"` // Populated by joins
	GroupName           string     `json:"group_name" db:"group_name"`
	Description         *string    `json:"description,omitempty" db:"description"`
	CreatedBy           *string    `json:"created_by,omitempty" db:"created_by"`
	CreatedAt           time.Time  `json:"created_at" db:"created_at"`
	IsActive            bool       `json:"is_active" db:"is_active"`
	AccessTier          AccessTier `json:"access_tier" db:"access_tier"`
	HasPendingDeletion  bool       `json:"-" db:"has_pending_deletion"`
}

// SignalGroupPublic represents a signal group without sensitive data
type SignalGroupPublic struct {
	ID                  string    `json:"id"`
	RegionID            *string   `json:"region_id,omitempty"`
	SchoolID            *string   `json:"school_id,omitempty"`
	DistrictID          *string   `json:"district_id,omitempty"`
	OwnerGroupID        *string   `json:"owner_group_id,omitempty"`
	ConnectionID        *string   `json:"connection_id,omitempty"`
	RegionName          string    `json:"region_name,omitempty"`
	SchoolName          string    `json:"school_name,omitempty"`
	DistrictName        string    `json:"district_name,omitempty"`
	Name                string    `json:"name"`
	Description         *string   `json:"description,omitempty"`
	MemberCountEstimate string    `json:"member_count_estimate,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	AccessTier          string    `json:"access_tier"`
	HasPendingDeletion  bool      `json:"has_pending_deletion"`
}

// SignalGroupWithSecret includes encrypted secret for verified users
type SignalGroupWithSecret struct {
	SignalGroupPublic
	EncryptedSecret *EncryptedSecretResponse `json:"encrypted_secret,omitempty"`
}

// CreateSignalGroupRequest represents request to create a signal group
// Exactly one of RegionID, SchoolID, or DistrictID must be set
type CreateSignalGroupRequest struct {
	RegionID         *string           `json:"region_id,omitempty"`
	SchoolID         *string           `json:"school_id,omitempty"`
	DistrictID       *string           `json:"district_id,omitempty"`
	Name             string            `json:"name" validate:"required,min=1,max=255"`
	Description      *string           `json:"description,omitempty"`
	EncryptedPayload string            `json:"encrypted_payload" validate:"required"`
	EncryptionIV     string            `json:"encryption_iv" validate:"required"`
	WrappedKeys      []WrappedKeyEntry `json:"wrapped_keys" validate:"required"`
}

// UpdateSignalGroupRequest represents request to update a signal group
// Note: secret updates require consensus - use secret update proposal endpoints
type UpdateSignalGroupRequest struct {
	Name        *string `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	Description *string `json:"description,omitempty"`
}

// SignalGroupListResponse represents response for listing signal groups
type SignalGroupListResponse struct {
	Groups []SignalGroupWithSecret `json:"groups"`
}

// VoteRequest represents a vote on a proposal
type VoteRequest struct {
	Vote bool `json:"vote"` // true=approve, false=reject
}

// ProposalUser represents a user in proposal context
type ProposalUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// ProposalListFilter represents filter options for listing proposals (shared by blocklist, deletion)
type ProposalListFilter struct {
	Status        string
	SignalGroupID string
	RegionID      string
}
