package services

import (
	"context"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// PostgridServiceInterface defines the interface for Postgrid operations
type PostgridServiceInterface interface {
	// ValidateAddress validates an address using Postgrid
	// NOTE: Address is processed in memory only and NEVER stored
	ValidateAddress(ctx context.Context, address *models.Address) (*AddressValidationResult, error)

	// SendPostcard sends a verification postcard to the given address
	// NOTE: Address is sent to Postgrid but not stored by us
	SendPostcard(ctx context.Context, address *models.Address, verificationCode, postcardRef string) (string, error)
}

// CityBoundary represents a city/place boundary polygon
type CityBoundary struct {
	PlaceID   string
	Name      string
	State     string
	Type      string // "city", "county"
	GeoJSON   string // GeoJSON Polygon string
	Center    [2]float64
}

// StateBoundary represents a state boundary polygon
type StateBoundary struct {
	PlaceID string
	Name    string
	GeoJSON string
	Center  [2]float64
}

// CountyBoundary represents a county boundary polygon
type CountyBoundary struct {
	PlaceID string
	Name    string
	State   string
	GeoJSON string
	Center  [2]float64
}

// LocalityBoundary represents a locality (borough/sub-city) boundary polygon
type LocalityBoundary struct {
	PlaceID string
	Name    string
	State   string
	City    string
	GeoJSON string     // May be empty if no boundary available
	Center  [2]float64
}

// NeighborhoodBoundary represents a neighborhood boundary polygon
type NeighborhoodBoundary struct {
	PlaceID  string
	Name     string
	State    string
	City     string
	Locality string     // Parent locality if applicable
	GeoJSON  string     // Usually empty (~92% of neighborhoods lack polygon boundaries)
	Center   [2]float64
}

// MapboxServiceInterface defines the interface for Mapbox operations
type MapboxServiceInterface interface {
	// GeocodeAddress geocodes an address using Mapbox
	// NOTE: Address is processed in memory only and NEVER stored
	GeocodeAddress(ctx context.Context, address *models.Address) (*GeocodeResult, error)

	// ReverseGeocode performs reverse geocoding to get place information from coordinates
	ReverseGeocode(ctx context.Context, lat, lng float64) (*GeocodeResult, error)

	// GetCityBoundary returns a boundary polygon for the city/place containing the given coordinates
	// If actual boundaries aren't available, returns a circular buffer around the point
	// The address parameter is used as a fallback to look up city by name if geocoding returns county
	GetCityBoundary(ctx context.Context, lat, lng float64, geocodeResult *GeocodeResult, address *models.Address) (*CityBoundary, error)

	// GetStateBoundary fetches the state boundary polygon from OSM Nominatim
	GetStateBoundary(ctx context.Context, stateName string, lat, lng float64) (*StateBoundary, error)

	// GetCountyBoundary fetches the county boundary polygon from OSM Nominatim by name
	GetCountyBoundary(ctx context.Context, countyName, stateName string, lat, lng float64) (*CountyBoundary, error)

	// GetCountyForCoordinates uses reverse geocoding to find the county containing the given coordinates
	GetCountyForCoordinates(ctx context.Context, lat, lng float64, stateName string) (*CountyBoundary, error)

	// GetLocalityBoundary fetches a locality (borough/sub-city) boundary from OSM
	// Returns boundary with empty GeoJSON if no polygon boundary is available
	GetLocalityBoundary(ctx context.Context, localityName, cityName, stateName string, lat, lng float64) (*LocalityBoundary, error)

	// GetNeighborhoodBoundary fetches a neighborhood boundary from OSM
	// Most neighborhoods (~92%) lack polygon boundaries, so GeoJSON is often empty
	GetNeighborhoodBoundary(ctx context.Context, neighborhoodName, localityName, cityName, stateName string, lat, lng float64) (*NeighborhoodBoundary, error)

	// GetPublicToken returns the public token for frontend use
	GetPublicToken() string
}

// EmailMessage represents an email to be sent
type EmailMessage struct {
	To          string
	Subject     string
	TextContent string
	HTMLContent string
}

// EmailServiceInterface defines the interface for email operations
type EmailServiceInterface interface {
	// SendVerificationEmail sends an email verification link to the user
	SendVerificationEmail(ctx context.Context, toEmail, token string) error

	// Send sends a generic email message
	Send(ctx context.Context, msg *EmailMessage) error

	// IsEnabled returns whether the email service is enabled
	IsEnabled() bool

	// Backend returns the name of the email backend being used
	Backend() string
}

// Ensure concrete types implement interfaces
var _ PostgridServiceInterface = (*PostgridService)(nil)
var _ PostgridServiceInterface = (*LobService)(nil) // LobService also implements this interface
var _ MapboxServiceInterface = (*MapboxService)(nil)
