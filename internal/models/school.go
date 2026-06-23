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
	SchoolDistrictTypeUnified    SchoolDistrictType = "unified"
	SchoolDistrictTypeElementary SchoolDistrictType = "elementary"
	SchoolDistrictTypeSecondary  SchoolDistrictType = "secondary"
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

// SchoolWithDetails includes additional computed fields from joins
type SchoolWithDetails struct {
	School
	DistrictName  string             `json:"district_name,omitempty"`
	RegionName    string             `json:"region_name,omitempty"`
	MemberCount   int                `json:"member_count"`
	AdminCount    int                `json:"admin_count"`
	VerifiedCount int                `json:"verified_count"`
	BootstrapMode bool               `json:"bootstrap_mode"`
	SignalGroups  []SignalGroupPublic `json:"signal_groups,omitempty"`
	Admins        []SchoolMemberBasic `json:"admins,omitempty"`
	IsMember      bool               `json:"is_member"`
	IsVerified    bool               `json:"is_verified"`
	IsAdmin       bool               `json:"is_admin"`
}

// SchoolMemberBasic is a minimal member representation for admin lists
type SchoolMemberBasic struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
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

// DistrictSearchResponse represents district search results
type DistrictSearchResponse struct {
	Districts []SchoolDistrict `json:"districts"`
}

// DistrictWithDetails includes schools and other computed fields
type DistrictWithDetails struct {
	SchoolDistrict
	Schools []SchoolSummary `json:"schools"`
}
