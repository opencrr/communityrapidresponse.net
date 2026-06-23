package models

import (
	"time"
)

// VerificationStatus represents the status of a verification request
type VerificationStatus string

const (
	VerificationStatusPending  VerificationStatus = "pending"
	VerificationStatusMailed   VerificationStatus = "mailed"
	VerificationStatusVerified VerificationStatus = "verified"
	VerificationStatusExpired  VerificationStatus = "expired"
)

// VerificationRequest represents a postcard verification request
// NOTE: Address is NEVER stored - only tracking info
type VerificationRequest struct {
	ID                         string             `json:"id" db:"id"`
	UserID                     string             `json:"user_id" db:"user_id"`
	RegionID                   *string            `json:"region_id,omitempty" db:"region_id"`
	VerificationCode           string             `json:"-" db:"verification_code"` // Never expose in API
	Status                     VerificationStatus `json:"status" db:"status"`
	PostgridRequestID          string             `json:"-" db:"postgrid_request_id"`
	BoundaryType               string             `json:"boundary_type" db:"boundary_type"`
	BoundaryName               string             `json:"boundary_name" db:"boundary_name"`
	BoundaryState              string             `json:"boundary_state" db:"boundary_state"`
	PostcardRef                *string            `json:"postcard_ref,omitempty" db:"postcard_ref"`
	CreatedAt                  time.Time          `json:"created_at" db:"created_at"`
	ExpiresAt                  time.Time          `json:"expires_at" db:"expires_at"`
	VerifiedAt                 *time.Time         `json:"verified_at,omitempty" db:"verified_at"`
	FailedVerificationAttempts int                `json:"-" db:"failed_verification_attempts"`
}

// Address represents a mailing address (NEVER stored in database)
// This struct exists only in memory during verification processing
type Address struct {
	Line1      string `json:"line1" validate:"required"`
	Line2      string `json:"line2,omitempty"`
	City       string `json:"city" validate:"required"`
	State      string `json:"state" validate:"required"`
	PostalCode string `json:"postal_code" validate:"required"`
	Country    string `json:"country,omitempty"`
}

// PostcardVerificationRequest represents request to initiate postcard verification
type PostcardVerificationRequest struct {
	RegionID string  `json:"region_id,omitempty"` // Optional - if not provided, will find or create region
	Address  Address `json:"address" validate:"required"`
}

// PostcardVerificationResponse represents response after initiating verification
type PostcardVerificationResponse struct {
	VerificationID    string        `json:"verification_id"`
	Status            string        `json:"status"`
	ExpiresAt         time.Time     `json:"expires_at"`
	EstimatedDelivery string        `json:"estimated_delivery"`
	PrivacyNotice     string        `json:"privacy_notice"`
	PostcardRef       string        `json:"postcard_ref,omitempty"`
	DetectedBoundary  *BoundaryInfo `json:"detected_boundary,omitempty"`
	Region            *RegionInfo   `json:"region,omitempty"`
}

// RegionInfo represents info about the region used for verification
type RegionInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Created bool   `json:"created"` // True if region was auto-created
}

// BoundaryInfo represents detected administrative boundary
type BoundaryInfo struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// VerifyCodeRequest represents request to verify a postcard code
type VerifyCodeRequest struct {
	VerificationCode string `json:"verification_code" validate:"required,min=8,max=32"`
}

// VerifyCodeResponse represents response after successful code verification
type VerifyCodeResponse struct {
	Success bool                    `json:"success"`
	Token   string                  `json:"token,omitempty"`
	User    *UserVerificationResult `json:"user,omitempty"`
}

// UserVerificationResult represents user data after verification
type UserVerificationResult struct {
	VerificationTier VerificationTier `json:"verification_tier"`
	AdminRegions     []string         `json:"admin_regions"`
}

// Vouch represents a vouch from one user for another
type Vouch struct {
	ID            string    `json:"id" db:"id"`
	VoucherUserID string    `json:"voucher_user_id" db:"voucher_user_id"`
	VouchedUserID string    `json:"vouched_user_id" db:"vouched_user_id"`
	RegionID      *string   `json:"region_id,omitempty" db:"region_id"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

// VoucherInfo represents info about a voucher
type VoucherInfo struct {
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	VouchedAt time.Time `json:"vouched_at"`
}

// UserRegionStatus represents the verification status of a user in a region
type UserRegionStatus string

const (
	UserRegionStatusPending  UserRegionStatus = "pending"
	UserRegionStatusVerified UserRegionStatus = "verified"
)
