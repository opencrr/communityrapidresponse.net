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
	ID                string             `json:"id" db:"id"`
	UserID            string             `json:"user_id" db:"user_id"`
	RegionID          *string            `json:"region_id,omitempty" db:"region_id"`
	VerificationCode  string             `json:"-" db:"verification_code"` // Never expose in API
	Status            VerificationStatus `json:"status" db:"status"`
	PostgridRequestID string             `json:"-" db:"postgrid_request_id"`
	BoundaryType      string             `json:"boundary_type" db:"boundary_type"`
	BoundaryName      string             `json:"boundary_name" db:"boundary_name"`
	BoundaryState     string             `json:"boundary_state" db:"boundary_state"`
	PostcardRef       *string            `json:"postcard_ref,omitempty" db:"postcard_ref"`
	CreatedAt         time.Time          `json:"created_at" db:"created_at"`
	ExpiresAt         time.Time          `json:"expires_at" db:"expires_at"`
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
	Success bool                   `json:"success"`
	Token   string                 `json:"token,omitempty"`
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

// VouchRequest represents request to vouch for a user
// UserIdentifier can be a user ID (UUID), email, or username
// RegionID is optional - if not provided, the user's pending vouch region is used
type VouchRequest struct {
	VouchedUserID  string `json:"vouched_user_id"`  // Deprecated: use UserIdentifier
	UserIdentifier string `json:"user_id"`          // Can be UUID, email, or username
	RegionID       string `json:"region_id"`        // Optional: auto-detected from pending request
}

// VouchResponse represents response after vouching
type VouchResponse struct {
	VouchID         string `json:"vouch_id"`
	VouchesNeeded   int    `json:"vouches_needed"`
	TotalVouches    int    `json:"total_vouches"`
	BootstrapMode   bool   `json:"bootstrap_mode"`
	AdminCount      int    `json:"admin_count"`
	VouchesRequired int    `json:"vouches_required"`
}

// VouchStatusResponse represents vouch status for a user
type VouchStatusResponse struct {
	UserID          string        `json:"user_id"`
	RegionID        string        `json:"region_id"`
	VouchesReceived int           `json:"vouches_received"`
	VouchesNeeded   int           `json:"vouches_needed"`
	Vouchers        []VoucherInfo `json:"vouchers"`
	BootstrapMode   bool          `json:"bootstrap_mode"`
	AdminCount      int           `json:"admin_count"`
	VouchesRequired int           `json:"vouches_required"`
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

// VouchVerificationRequest represents request to initiate vouch verification
// User provides their address to identify their community; address is never stored
type VouchVerificationRequest struct {
	Address Address `json:"address" validate:"required"`
}

// VouchVerificationResponse represents response after requesting vouch verification
type VouchVerificationResponse struct {
	UserRegionID     string        `json:"user_region_id"`
	RegionID         string        `json:"region_id"`
	RegionName       string        `json:"region_name"`
	Status           string        `json:"status"`
	VouchesNeeded    int           `json:"vouches_needed"`
	Message          string        `json:"message"`
	BootstrapMode    bool          `json:"bootstrap_mode,omitempty"`
	AdminCount       int           `json:"admin_count,omitempty"`
	VouchesRequired  int           `json:"vouches_required,omitempty"`
	PrivacyNotice    string        `json:"privacy_notice"`
	DetectedBoundary *BoundaryInfo `json:"detected_boundary,omitempty"`
	Region           *RegionInfo   `json:"region,omitempty"`
}
