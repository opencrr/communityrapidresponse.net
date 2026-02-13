package models

import (
	"time"
)

// SchoolVerificationStatus represents the verification status in a school
type SchoolVerificationStatus string

const (
	SchoolVerificationStatusPending  SchoolVerificationStatus = "pending"
	SchoolVerificationStatusVerified SchoolVerificationStatus = "verified"
)

// SchoolDistrictType represents the type of school district
type SchoolDistrictType string

const (
	SchoolDistrictTypeUnified     SchoolDistrictType = "unified"
	SchoolDistrictTypeElementary  SchoolDistrictType = "elementary"
	SchoolDistrictTypeSecondary   SchoolDistrictType = "secondary"
)

// SchoolDistrict represents a school district from NCES data
type SchoolDistrict struct {
	ID           string             `json:"id" db:"id"`
	NCESID       string             `json:"nces_id" db:"nces_id"`
	Name         string             `json:"name" db:"name"`
	State        string             `json:"state" db:"state"`
	DistrictType SchoolDistrictType `json:"district_type" db:"district_type"`
	Geometry     *GeoJSONGeometry   `json:"geometry,omitempty" db:"geometry"`
	CreatedAt    time.Time          `json:"created_at" db:"created_at"`
}

// School represents a school from NCES data
type School struct {
	ID            string    `json:"id" db:"id"`
	NCESID        string    `json:"nces_id" db:"nces_id"`
	DistrictID    *string   `json:"district_id,omitempty" db:"district_id"`
	Name          string    `json:"name" db:"name"`
	StreetAddress *string   `json:"street_address,omitempty" db:"street_address"`
	City          *string   `json:"city,omitempty" db:"city"`
	State         string    `json:"state" db:"state"`
	Zip           *string   `json:"zip,omitempty" db:"zip"`
	Latitude      *float64  `json:"latitude,omitempty" db:"latitude"`
	Longitude     *float64  `json:"longitude,omitempty" db:"longitude"`
	RegionID      *string   `json:"region_id,omitempty" db:"region_id"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

// SchoolWithDetails includes additional computed fields from joins
type SchoolWithDetails struct {
	School
	DistrictName  string              `json:"district_name,omitempty"`
	RegionName    string              `json:"region_name,omitempty"`
	MemberCount   int                 `json:"member_count"`
	AdminCount    int                 `json:"admin_count"`
	VerifiedCount int                 `json:"verified_count"`
	BootstrapMode bool                `json:"bootstrap_mode"`
	SignalGroups  []SignalGroupPublic  `json:"signal_groups,omitempty"`
	Admins        []SchoolMemberBasic `json:"admins,omitempty"`
	// Per-user membership fields (set when an authenticated user views the school)
	IsMember   bool `json:"is_member"`
	IsVerified bool `json:"is_verified"`
	IsAdmin    bool `json:"is_admin"`
}

// SchoolSummary is a lightweight representation for listings
type SchoolSummary struct {
	ID            string   `json:"id"`
	NCESID        string   `json:"nces_id"`
	Name          string   `json:"name"`
	City          string   `json:"city,omitempty"`
	State         string   `json:"state"`
	DistrictID    *string  `json:"district_id,omitempty"`
	DistrictName  string   `json:"district_name,omitempty"`
	Latitude      *float64 `json:"latitude,omitempty"`
	Longitude     *float64 `json:"longitude,omitempty"`
	MemberCount   int      `json:"member_count"`
	VerifiedCount int      `json:"verified_count"`
	BootstrapMode bool     `json:"bootstrap_mode"`
}

// MySchoolSummary extends SchoolSummary with the current user's membership details
type MySchoolSummary struct {
	SchoolSummary
	VerificationStatus SchoolVerificationStatus `json:"verification_status"`
	IsAdmin            bool                     `json:"is_admin"`
	VouchCount         int                      `json:"vouch_count"`
	VouchesNeeded      int                      `json:"vouches_needed"`
}

// UserSchool represents a user's membership in a school
type UserSchool struct {
	ID                 string                   `json:"id" db:"id"`
	UserID             string                   `json:"user_id" db:"user_id"`
	SchoolID           string                   `json:"school_id" db:"school_id"`
	IsAdmin            bool                     `json:"is_admin" db:"is_admin"`
	VerificationStatus SchoolVerificationStatus `json:"verification_status" db:"verification_status"`
	VerifiedAt         *time.Time               `json:"verified_at,omitempty" db:"verified_at"`
}

// SchoolVouch represents a vouch given within a school context
type SchoolVouch struct {
	ID            string    `json:"id" db:"id"`
	VoucherUserID string    `json:"voucher_user_id" db:"voucher_user_id"`
	VouchedUserID string    `json:"vouched_user_id" db:"vouched_user_id"`
	SchoolID      string    `json:"school_id" db:"school_id"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

// SchoolBlockedUser represents a blocked user at the school level
type SchoolBlockedUser struct {
	ID        string    `json:"id" db:"id"`
	UserID    string    `json:"user_id" db:"user_id"`
	SchoolID  string    `json:"school_id" db:"school_id"`
	BlockedBy string    `json:"blocked_by" db:"blocked_by"`
	Reason    *string   `json:"reason,omitempty" db:"reason"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// SchoolMember represents a school member with user details
type SchoolMember struct {
	UserID             string                   `json:"user_id"`
	Username           string                   `json:"username"`
	Email              string                   `json:"email"`
	IsAdmin            bool                     `json:"is_admin"`
	VerificationStatus SchoolVerificationStatus `json:"verification_status"`
	VerifiedAt         *time.Time               `json:"verified_at,omitempty"`
	VouchCount         int                      `json:"vouch_count"`
}

// SchoolMemberBasic is a minimal member representation for admin lists
type SchoolMemberBasic struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

// SchoolSearchRequest represents search parameters for schools
type SchoolSearchRequest struct {
	Query      string `json:"query"`
	State      string `json:"state"`
	DistrictID string `json:"district_id"`
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
}

// SchoolSearchResponse represents search results
type SchoolSearchResponse struct {
	Schools []SchoolSummary `json:"schools"`
	Total   int             `json:"total"`
	Page    int             `json:"page"`
	Limit   int             `json:"limit"`
	HasMore bool            `json:"has_more"`
}

// SchoolVouchRequest represents a request to vouch for a school member
type SchoolVouchRequest struct {
	UserIdentifier string `json:"user_identifier"` // email, username, or user ID
}

// SchoolVouchResponse represents the response after vouching
type SchoolVouchResponse struct {
	VouchID         string `json:"vouch_id"`
	VouchesNeeded   int    `json:"vouches_needed"`
	TotalVouches    int    `json:"total_vouches"`
	BootstrapMode   bool   `json:"bootstrap_mode"`
	AdminCount      int    `json:"admin_count"`
	VouchesRequired int    `json:"vouches_required"`
}

// SchoolVouchStatusResponse represents vouch progress for a user
type SchoolVouchStatusResponse struct {
	UserID          string       `json:"user_id"`
	SchoolID        string       `json:"school_id"`
	VouchesReceived int          `json:"vouches_received"`
	VouchesNeeded   int          `json:"vouches_needed"`
	Vouchers        []VoucherInfo `json:"vouchers"`
	BootstrapMode   bool         `json:"bootstrap_mode"`
	AdminCount      int          `json:"admin_count"`
	VouchesRequired int          `json:"vouches_required"`
}

// VoucherInfo represents info about a voucher (reused from existing codebase)
// Already defined in verification models; referenced here for school vouch context

// PendingSchoolVouchUser represents a user awaiting vouches in a school
type PendingSchoolVouchUser struct {
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	SchoolID   string `json:"school_id"`
	SchoolName string `json:"school_name"`
	VouchCount int    `json:"vouch_count"`
}

// CreateSchoolSignalGroupRequest represents request to create a school signal group
type CreateSchoolSignalGroupRequest struct {
	Name             string            `json:"name" validate:"required,min=1,max=255"`
	Description      *string           `json:"description,omitempty"`
	EncryptedPayload string            `json:"encrypted_payload" validate:"required"`
	EncryptionIV     string            `json:"encryption_iv" validate:"required"`
	WrappedKeys      []WrappedKeyEntry `json:"wrapped_keys" validate:"required"`
}

// DistrictSearchResponse represents district search results
type DistrictSearchResponse struct {
	Districts []SchoolDistrict `json:"districts"`
}

// DistrictMember represents a member of a school district with school context
type DistrictMember struct {
	UserID             string                   `json:"user_id"`
	Username           string                   `json:"username"`
	SchoolName         string                   `json:"school_name"`
	IsAdmin            bool                     `json:"is_admin"`
	VerificationStatus SchoolVerificationStatus `json:"verification_status"`
}

// DistrictWithDetails includes schools and other computed fields
type DistrictWithDetails struct {
	SchoolDistrict
	Schools      []SchoolSummary     `json:"schools"`
	SignalGroups []SignalGroupPublic  `json:"signal_groups,omitempty"`
}
