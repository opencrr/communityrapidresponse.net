package models

import (
	"time"
)

// VerificationTier represents user verification levels
type VerificationTier int

const (
	TierUnverified VerificationTier = 0 // No verification
	TierVouched    VerificationTier = 1 // First step: community trust (read-only access)
	TierPostcard   VerificationTier = 2 // Second step: proven residency (admin rights)
)

// User represents a registered user in the system
type User struct {
	ID                  string           `json:"id" db:"id"`
	Username            string           `json:"username" db:"username"`
	Email               string           `json:"email" db:"email"`
	PasswordHash        string           `json:"-" db:"password_hash"`
	VerificationTier    VerificationTier `json:"verification_tier" db:"verification_tier"`
	PostcardVerified    bool             `json:"postcard_verified" db:"postcard_verified"`
	VouchVerified       bool             `json:"vouch_verified" db:"vouch_verified"`
	IsSuperuser         bool             `json:"is_superuser" db:"is_superuser"`
	MFASecret           *string          `json:"-" db:"mfa_secret"` // Encrypted TOTP secret
	MFAEnabled          bool             `json:"mfa_enabled" db:"mfa_enabled"`
	MFABackupCodes      *string          `json:"-" db:"mfa_backup_codes"` // JSON array of hashed backup codes
	MFASetupRequired    bool             `json:"mfa_setup_required" db:"mfa_setup_required"`
	EmailVerified       bool             `json:"email_verified" db:"email_verified"`
	EmailNormalized     *string          `json:"-" db:"email_normalized"` // Normalized email for duplicate detection
	IsBlocked           bool             `json:"is_blocked" db:"is_blocked"`
	BlockedAt           *time.Time       `json:"blocked_at,omitempty" db:"blocked_at"`
	BlockedBy           *string          `json:"blocked_by,omitempty" db:"blocked_by"`
	BlockReason         *string          `json:"block_reason,omitempty" db:"block_reason"`
	AddressHash         *string          `json:"-" db:"address_hash"` // SHA-256 hash of verified address (never exposed)
	CreatedAt           time.Time        `json:"created_at" db:"created_at"`
	LastLogin           *time.Time       `json:"last_login,omitempty" db:"last_login"`
	DeletedAt           *time.Time       `json:"-" db:"deleted_at"`
	FailedLoginAttempts int              `json:"-" db:"failed_login_attempts"`
	LockedUntil         *time.Time       `json:"-" db:"locked_until"`
	FailedMFAAttempts   int              `json:"-" db:"failed_mfa_attempts"`
}

// UserRegion represents a user's membership in a geographic region
type UserRegion struct {
	ID                 string    `json:"id" db:"id"`
	UserID             string    `json:"user_id" db:"user_id"`
	RegionID           string    `json:"region_id" db:"region_id"`
	IsAdmin            bool      `json:"is_admin" db:"is_admin"`
	VerificationStatus string    `json:"verification_status" db:"verification_status"`
	VerifiedAt         time.Time `json:"verified_at" db:"verified_at"`
}

// AdminBoundary represents the administrative boundary where a user can create regions
type AdminBoundary struct {
	ID               string    `json:"id" db:"id"`
	UserID           string    `json:"user_id" db:"user_id"`
	BoundaryType     string    `json:"boundary_type" db:"boundary_type"` // 'city', 'town', 'county'
	BoundaryName     string    `json:"boundary_name" db:"boundary_name"`
	BoundaryState    string    `json:"boundary_state" db:"boundary_state"`
	BoundaryCountry  string    `json:"boundary_country" db:"boundary_country"`
	MapboxPlaceID    *string   `json:"mapbox_place_id,omitempty" db:"mapbox_place_id"`
	BoundaryGeometry []byte    `json:"-" db:"boundary_geometry"` // WKB format
	VerifiedAt       time.Time `json:"verified_at" db:"verified_at"`
}

// UserWithRegions represents a user with their associated regions
type UserWithRegions struct {
	User
	Regions              []UserRegionInfo    `json:"regions,omitempty"`
	AuthorizedBoundaries []AdminBoundaryInfo `json:"authorized_boundaries,omitempty"`
}

// UserRegionInfo provides region details for a user
type UserRegionInfo struct {
	RegionID   string `json:"region_id"`
	RegionName string `json:"region_name"`
	IsAdmin    bool   `json:"is_admin"`
}

// AdminBoundaryInfo provides boundary details for API responses
type AdminBoundaryInfo struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Name       string    `json:"name"`
	State      string    `json:"state"`
	VerifiedAt time.Time `json:"verified_at"`
}

// PublicUser represents user data safe for public API responses
type PublicUser struct {
	ID               string           `json:"id"`
	Username         string           `json:"username"`
	VerificationTier VerificationTier `json:"verification_tier"`
}
