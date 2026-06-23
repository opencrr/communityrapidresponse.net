package models

import "time"

// MeshtasticChannel represents a Meshtastic mesh networking channel associated with a region, school, district, or group
type MeshtasticChannel struct {
	ID                 string     `json:"id" db:"id"`
	RegionID           *string    `json:"region_id,omitempty" db:"region_id"`
	SchoolID           *string    `json:"school_id,omitempty" db:"school_id"`
	DistrictID         *string    `json:"district_id,omitempty" db:"district_id"`
	OwnerGroupID       *string    `json:"owner_group_id,omitempty" db:"owner_group_id"`
	RegionName         string     `json:"region_name,omitempty" db:"region_name"`
	SchoolName         string     `json:"school_name,omitempty" db:"school_name"`
	DistrictName       string     `json:"district_name,omitempty" db:"district_name"`
	ChannelName        string     `json:"channel_name" db:"channel_name"`
	Description        *string    `json:"description,omitempty" db:"description"`
	AccessTier         AccessTier `json:"access_tier" db:"access_tier"`
	CreatedBy          *string    `json:"created_by,omitempty" db:"created_by"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	IsActive           bool       `json:"is_active" db:"is_active"`
	HasPendingDeletion bool       `json:"-" db:"has_pending_deletion"`
}

// MeshtasticChannelPublic represents a Meshtastic channel without sensitive data
type MeshtasticChannelPublic struct {
	ID                 string    `json:"id"`
	RegionID           *string   `json:"region_id,omitempty"`
	SchoolID           *string   `json:"school_id,omitempty"`
	DistrictID         *string   `json:"district_id,omitempty"`
	OwnerGroupID       *string   `json:"owner_group_id,omitempty"`
	RegionName         string    `json:"region_name,omitempty"`
	SchoolName         string    `json:"school_name,omitempty"`
	DistrictName       string    `json:"district_name,omitempty"`
	Name               string    `json:"name"`
	Description        *string   `json:"description,omitempty"`
	AccessTier         string    `json:"access_tier"`
	CreatedAt          time.Time `json:"created_at"`
	HasPendingDeletion bool      `json:"has_pending_deletion"`
}

// MeshtasticChannelWithSecret includes encrypted secret for verified users
type MeshtasticChannelWithSecret struct {
	MeshtasticChannelPublic
	EncryptedSecret *EncryptedSecretResponse `json:"encrypted_secret,omitempty"`
}

// CreateMeshtasticChannelRequest represents request to create a Meshtastic channel
// Exactly one of RegionID, SchoolID, or DistrictID must be set
type CreateMeshtasticChannelRequest struct {
	RegionID         *string           `json:"region_id,omitempty"`
	SchoolID         *string           `json:"school_id,omitempty"`
	DistrictID       *string           `json:"district_id,omitempty"`
	Name             string            `json:"name" validate:"required,min=1,max=255"`
	Description      *string           `json:"description,omitempty"`
	EncryptedPayload string            `json:"encrypted_payload" validate:"required"`
	EncryptionIV     string            `json:"encryption_iv" validate:"required"`
	WrappedKeys      []WrappedKeyEntry `json:"wrapped_keys" validate:"required"`
}

// UpdateMeshtasticChannelRequest represents request to update a Meshtastic channel
// Note: secret updates require consensus - use secret update proposal endpoints
type UpdateMeshtasticChannelRequest struct {
	Name        *string `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	Description *string `json:"description,omitempty"`
}

// MeshtasticChannelListResponse represents response for listing Meshtastic channels
type MeshtasticChannelListResponse struct {
	Channels []MeshtasticChannelWithSecret `json:"channels"`
}

// CreateGroupMeshtasticChannelRequest is the request body for creating a meshtastic channel under a group.
type CreateGroupMeshtasticChannelRequest struct {
	ChannelName string `json:"channel_name" validate:"required,min=1,max=255"`
	Description string `json:"description" validate:"max=1000"`
	AccessTier  string `json:"access_tier" validate:"required,oneof=open resident member trusted admin_only"`
}
