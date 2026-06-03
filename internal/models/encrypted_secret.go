package models

import "time"

// SecretType represents the type of encrypted secret
type SecretType string

const (
	SecretTypeSignalInvite      SecretType = "signal_invite"
	SecretTypeMeshtasticChannel SecretType = "meshtastic_channel" // #nosec G101 -- not a credential, enum value
)

// EncryptedSecret represents an encrypted secret stored for a signal group or meshtastic channel
type EncryptedSecret struct {
	ID                  string     `json:"id" db:"id"`
	SecretType          SecretType `json:"secret_type" db:"secret_type"`
	SignalGroupID       *string    `json:"signal_group_id,omitempty" db:"signal_group_id"`
	MeshtasticChannelID *string    `json:"meshtastic_channel_id,omitempty" db:"meshtastic_channel_id"`
	EncryptedPayload    string     `json:"encrypted_payload" db:"encrypted_payload"`
	EncryptionIV        string     `json:"encryption_iv" db:"encryption_iv"`
	UpdatedBy           string     `json:"updated_by" db:"updated_by"`
	UpdatedAt           time.Time  `json:"updated_at" db:"updated_at"`
}

// EncryptedSecretKey represents a user's wrapped DEK for an encrypted secret
type EncryptedSecretKey struct {
	SecretID    string    `json:"secret_id" db:"secret_id"`
	UserID      string    `json:"user_id" db:"user_id"`
	WrappedDEK  string    `json:"wrapped_dek" db:"wrapped_dek"`
	RekeyNeeded bool      `json:"rekey_needed" db:"rekey_needed"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

// WrappedKeyEntry represents a wrapped DEK for a single user (used in requests)
type WrappedKeyEntry struct {
	UserID     string `json:"user_id"`
	WrappedDEK string `json:"wrapped_dek"`
}

// SecretUpdateProposal represents a proposal to update an encrypted secret
type SecretUpdateProposal struct {
	ID                string         `json:"id" db:"id"`
	EncryptedSecretID string         `json:"encrypted_secret_id" db:"encrypted_secret_id"`
	RegionID          *string        `json:"region_id,omitempty" db:"region_id"`
	SchoolID          *string        `json:"school_id,omitempty" db:"school_id"`
	DistrictID        *string        `json:"district_id,omitempty" db:"district_id"`
	ProposedBy        string         `json:"proposed_by" db:"proposed_by"`
	EncryptedPayload  string         `json:"encrypted_payload" db:"encrypted_payload"`
	EncryptionIV      string         `json:"encryption_iv" db:"encryption_iv"`
	Reason            *string        `json:"reason,omitempty" db:"reason"`
	Status            ProposalStatus `json:"status" db:"status"`
	CreatedAt         time.Time      `json:"created_at" db:"created_at"`
	ExpiresAt         time.Time      `json:"expires_at" db:"expires_at"`
	ResolvedAt        *time.Time     `json:"resolved_at,omitempty" db:"resolved_at"`
	FinalizedAt       *time.Time     `json:"finalized_at,omitempty" db:"finalized_at"`
}

// SecretUpdateVote represents a vote on a secret update proposal
type SecretUpdateVote struct {
	ID         string    `json:"id" db:"id"`
	ProposalID string    `json:"proposal_id" db:"proposal_id"`
	VoterID    string    `json:"voter_id" db:"voter_id"`
	Vote       bool      `json:"vote" db:"vote"`
	VotedAt    time.Time `json:"voted_at" db:"voted_at"`
}

// ProposalWrappedKey represents an admin's wrapped DEK for a proposal
type ProposalWrappedKey struct {
	ProposalID string    `json:"proposal_id" db:"proposal_id"`
	UserID     string    `json:"user_id" db:"user_id"`
	WrappedDEK string    `json:"wrapped_dek" db:"wrapped_dek"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// --- Request types ---

// CreateEncryptedSecretRequest is used when creating a signal group with encrypted invite link
type CreateEncryptedSecretRequest struct {
	EncryptedPayload string            `json:"encrypted_payload"`
	EncryptionIV     string            `json:"encryption_iv"`
	WrappedKeys      []WrappedKeyEntry `json:"wrapped_keys"`
}

// CreateSecretProposalRequest represents a request to propose a secret update
type CreateSecretProposalRequest struct {
	EncryptedPayload string            `json:"encrypted_payload"`
	EncryptionIV     string            `json:"encryption_iv"`
	WrappedKeys      []WrappedKeyEntry `json:"wrapped_keys"`
	Reason           *string           `json:"reason,omitempty"`
}

// FinalizeSecretUpdateRequest represents a request to finalize a secret update after consensus
type FinalizeSecretUpdateRequest struct {
	EncryptedPayload string            `json:"encrypted_payload"`
	EncryptionIV     string            `json:"encryption_iv"`
	WrappedKeys      []WrappedKeyEntry `json:"wrapped_keys"`
}

// --- Response types ---

// EncryptedSecretResponse includes the encrypted payload and the requesting user's wrapped DEK
type EncryptedSecretResponse struct {
	SecretID         string `json:"secret_id"`
	EncryptedPayload string `json:"encrypted_payload"`
	EncryptionIV     string `json:"encryption_iv"`
	WrappedDEK       string `json:"wrapped_dek,omitempty"`
}

// SecretProposalResponse represents the response after creating/voting on a proposal
type SecretProposalResponse struct {
	ProposalID        string    `json:"proposal_id"`
	EncryptedSecretID string    `json:"encrypted_secret_id,omitempty"`
	Status            string    `json:"status"`
	VotesNeeded       int       `json:"votes_needed"`
	CurrentVotes      int       `json:"current_votes"`
	ExpiresAt         time.Time `json:"expires_at,omitempty"`
	YourVote          *bool     `json:"your_vote,omitempty"`
	ConsensusReached  bool      `json:"consensus_reached,omitempty"`
	Message           string    `json:"message,omitempty"`
}

// SecretProposalSummary represents a proposal summary for listings
type SecretProposalSummary struct {
	ID                string    `json:"id"`
	EncryptedSecretID string    `json:"encrypted_secret_id"`
	SecretType        string    `json:"secret_type"`
	ScopeName         string    `json:"scope_name"`
	GroupName         string    `json:"group_name"`
	ProposedBy        string    `json:"proposed_by"`
	Reason            *string   `json:"reason,omitempty"`
	Votes             int       `json:"votes"`
	VotesNeeded       int       `json:"votes_needed"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	ExpiresAt         time.Time `json:"expires_at"`
}

// SecretProposalDetailResponse represents full proposal details
type SecretProposalDetailResponse struct {
	ID                string             `json:"id"`
	EncryptedSecretID string             `json:"encrypted_secret_id"`
	SecretType        string             `json:"secret_type"`
	ScopeName         string             `json:"scope_name"`
	GroupName         string             `json:"group_name"`
	ProposedBy        ProposalUser       `json:"proposed_by"`
	EncryptedPayload  string             `json:"encrypted_payload,omitempty"`
	EncryptionIV      string             `json:"encryption_iv,omitempty"`
	WrappedDEK        string             `json:"wrapped_dek,omitempty"`
	Reason            string             `json:"reason,omitempty"`
	Status            string             `json:"status"`
	VotesNeeded       int                `json:"votes_needed"`
	CurrentVotes      int                `json:"current_votes"`
	Votes             []SecretVoteDetail `json:"votes"`
	CreatedAt         time.Time          `json:"created_at"`
	ExpiresAt         time.Time          `json:"expires_at"`
	ResolvedAt        *time.Time         `json:"resolved_at,omitempty"`
	FinalizedAt       *time.Time         `json:"finalized_at,omitempty"`
	CanVote           bool               `json:"can_vote"`
	HasVoted          bool               `json:"has_voted"`
}

// SecretVoteDetail represents a vote with user details
type SecretVoteDetail struct {
	VoterID  string    `json:"voter_id"`
	Username string    `json:"username"`
	Vote     bool      `json:"vote"`
	VotedAt  time.Time `json:"voted_at"`
}

// SecretProposalListFilter represents filter options for listing proposals
type SecretProposalListFilter struct {
	Status            string
	EncryptedSecretID string
	RegionID          string
	SchoolID          string
	DistrictID        string
}
