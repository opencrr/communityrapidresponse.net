package models

import (
	"time"
)

// RegionType represents the type of geographic region
// Hierarchy: State -> County -> City -> Locality -> Neighborhood (-> CityBlock for future use)
// Locality is for boroughs/sub-city divisions (e.g., Brooklyn in NYC)
type RegionType string

const (
	RegionTypeState        RegionType = "state"
	RegionTypeCounty       RegionType = "county"
	RegionTypeCity         RegionType = "city"
	RegionTypeLocality     RegionType = "locality"     // Boroughs, sub-city divisions
	RegionTypeNeighborhood RegionType = "neighborhood"
	RegionTypeCityBlock    RegionType = "city_block" // Reserved for future use
)

// GeographicRegion represents a geographic area in the hierarchy
type GeographicRegion struct {
	ID             string     `json:"id" db:"id"`
	Name           string     `json:"name" db:"name"`
	RegionType     RegionType `json:"region_type" db:"region_type"`
	ParentRegionID *string    `json:"parent_region_id,omitempty" db:"parent_region_id"`
	Geometry       []byte     `json:"-" db:"geometry"` // WKB format, not exposed in JSON
	CreatedBy      *string    `json:"created_by,omitempty" db:"created_by"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
}

// RegionWithDetails includes additional computed fields
type RegionWithDetails struct {
	GeographicRegion
	ParentRegion   *RegionSummary   `json:"parent_region,omitempty"`
	SubRegions     []RegionSummary  `json:"sub_regions,omitempty"`
	SignalGroups   []SignalGroup    `json:"signal_groups,omitempty"`
	Admins         []PublicUser     `json:"admins,omitempty"`
	AdminCount     int              `json:"admin_count"`
	MemberCount    int              `json:"member_count"`
	GeoJSON        *GeoJSONGeometry `json:"geometry,omitempty"`
	BootstrapMode  bool             `json:"bootstrap_mode"`
	FullAdminCount int              `json:"full_admin_count"`
	IsMember       bool             `json:"is_member"`
}

// RegionSummary provides minimal region info for listings
type RegionSummary struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	RegionType     RegionType       `json:"type"`
	AdminCount     int              `json:"admin_count,omitempty"`
	MemberCount    int              `json:"member_count,omitempty"`
	FullAdminCount int              `json:"full_admin_count"`
	BootstrapMode  bool             `json:"bootstrap_mode"`
	Geometry       *GeoJSONGeometry `json:"geometry,omitempty"`
}

// GeoJSONGeometry represents a GeoJSON geometry object
// Supports both Polygon and MultiPolygon types
type GeoJSONGeometry struct {
	Type        string      `json:"type"`
	Coordinates interface{} `json:"coordinates"`
}

// CreateRegionRequest represents the request body for creating a region
type CreateRegionRequest struct {
	Name           string           `json:"name" validate:"required,min=1,max=255"`
	Type           RegionType       `json:"type" validate:"required,oneof=state county city locality neighborhood city_block"`
	ParentRegionID *string          `json:"parent_region_id"`
	Geometry       *GeoJSONGeometry `json:"geometry" validate:"required"`
	// Optional fields for auto-creating parent regions
	StateName  string `json:"state_name,omitempty"`  // Used to auto-create state parent for county/city
	CountyName string `json:"county_name,omitempty"` // Used to auto-create county parent for city
}

// UpdateRegionRequest represents the request body for updating a region
type UpdateRegionRequest struct {
	Name string `json:"name" validate:"required,min=1,max=255"`
}

// RegionListResponse represents the response for listing regions
type RegionListResponse struct {
	Regions []RegionSummary `json:"regions"`
}
