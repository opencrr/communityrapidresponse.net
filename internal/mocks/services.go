package mocks

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// MockPostgridService is a mock implementation of PostgridService for testing
type MockPostgridService struct {
	mu sync.Mutex

	// Control mock behavior - custom functions for fine-grained control
	ValidateAddressFunc func(ctx context.Context, address *models.Address) (*services.AddressValidationResult, error)
	SendPostcardFunc    func(ctx context.Context, address *models.Address, verificationCode, postcardRef string) (string, error)

	// Track calls for assertions
	ValidateAddressCalls []ValidateAddressCall
	SendPostcardCalls    []SendPostcardCall

	// Default responses
	DefaultDeliverable  bool
	DefaultIsPOBox      bool
	DefaultIsCMRA       bool
	DefaultIsCommercial bool
	ShouldFail          bool
	FailError           error
}

// ValidateAddressCall records a call to ValidateAddress
type ValidateAddressCall struct {
	Address *models.Address
}

// SendPostcardCall records a call to SendPostcard
type SendPostcardCall struct {
	Address          *models.Address
	VerificationCode string
	PostcardRef      string
}

// NewMockPostgridService creates a new mock Postgrid service with sensible defaults
func NewMockPostgridService() *MockPostgridService {
	return &MockPostgridService{
		DefaultDeliverable:   true,
		DefaultIsPOBox:       false,
		DefaultIsCMRA:        false,
		DefaultIsCommercial:  false,
		ValidateAddressCalls: make([]ValidateAddressCall, 0),
		SendPostcardCalls:    make([]SendPostcardCall, 0),
	}
}

// ValidateAddress validates an address (mock implementation)
func (m *MockPostgridService) ValidateAddress(ctx context.Context, address *models.Address) (*services.AddressValidationResult, error) {
	m.mu.Lock()
	m.ValidateAddressCalls = append(m.ValidateAddressCalls, ValidateAddressCall{Address: address})
	m.mu.Unlock()

	// Use custom function if provided
	if m.ValidateAddressFunc != nil {
		return m.ValidateAddressFunc(ctx, address)
	}

	// Return error if configured to fail
	if m.ShouldFail {
		if m.FailError != nil {
			return nil, m.FailError
		}
		return nil, fmt.Errorf("mock postgrid error")
	}

	// Detect PO Box from address
	isPOBox := m.DefaultIsPOBox
	if address != nil && address.Line1 != "" {
		normalized := strings.ToLower(address.Line1)
		normalized = strings.ReplaceAll(normalized, ".", "")
		if strings.Contains(normalized, "po box") || strings.Contains(normalized, "pobox") {
			isPOBox = true
		}
	}

	// Determine address type for response
	addressType := "residential"
	if m.DefaultIsCommercial {
		addressType = "commercial"
	}

	return &services.AddressValidationResult{
		IsDeliverable:       m.DefaultDeliverable,
		Deliverability:      "deliverable",
		IsPOBox:             isPOBox,
		IsCMRA:              m.DefaultIsCMRA,
		IsCommercial:        m.DefaultIsCommercial,
		AddressType:         addressType,
		StandardizedAddress: address,
	}, nil
}

// SendPostcard sends a postcard (mock implementation)
func (m *MockPostgridService) SendPostcard(ctx context.Context, address *models.Address, verificationCode, postcardRef string) (string, error) {
	m.mu.Lock()
	m.SendPostcardCalls = append(m.SendPostcardCalls, SendPostcardCall{
		Address:          address,
		VerificationCode: verificationCode,
		PostcardRef:      postcardRef,
	})
	m.mu.Unlock()

	// Use custom function if provided
	if m.SendPostcardFunc != nil {
		return m.SendPostcardFunc(ctx, address, verificationCode, postcardRef)
	}

	// Return error if configured to fail
	if m.ShouldFail {
		if m.FailError != nil {
			return "", m.FailError
		}
		return "", fmt.Errorf("mock postgrid error")
	}

	return fmt.Sprintf("mock_postcard_%s", verificationCode), nil
}

// Reset clears all recorded calls
func (m *MockPostgridService) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ValidateAddressCalls = make([]ValidateAddressCall, 0)
	m.SendPostcardCalls = make([]SendPostcardCall, 0)
	m.ShouldFail = false
	m.FailError = nil
}

// MockMapboxService is a mock implementation of MapboxService for testing
type MockMapboxService struct {
	mu sync.Mutex

	// Control mock behavior - custom functions for fine-grained control
	GeocodeAddressFunc          func(ctx context.Context, address *models.Address) (*services.GeocodeResult, error)
	ReverseGeocodeFunc          func(ctx context.Context, lat, lng float64) (*services.GeocodeResult, error)
	GetCityBoundaryFunc         func(ctx context.Context, lat, lng float64, geocodeResult *services.GeocodeResult, address *models.Address) (*services.CityBoundary, error)
	GetStateBoundaryFunc        func(ctx context.Context, stateName string, lat, lng float64) (*services.StateBoundary, error)
	GetCountyBoundaryFunc       func(ctx context.Context, countyName, stateName string, lat, lng float64) (*services.CountyBoundary, error)
	GetCountyForCoordinatesFunc func(ctx context.Context, lat, lng float64, stateName string) (*services.CountyBoundary, error)
	GetLocalityBoundaryFunc     func(ctx context.Context, localityName, cityName, stateName string, lat, lng float64) (*services.LocalityBoundary, error)
	GetNeighborhoodBoundaryFunc func(ctx context.Context, neighborhoodName, localityName, cityName, stateName string, lat, lng float64) (*services.NeighborhoodBoundary, error)

	// Track calls for assertions
	GeocodeAddressCalls []GeocodeAddressCall
	ReverseGeocodeCalls []ReverseGeocodeCall

	// Default responses
	DefaultLatitude         float64
	DefaultLongitude        float64
	DefaultBoundaryType     string
	DefaultBoundaryName     string
	DefaultBoundaryState    string
	DefaultPlaceID          string
	DefaultCountyName       string
	DefaultLocalityName     string
	DefaultNeighborhoodName string
	PublicToken             string
	ShouldFail              bool
	FailError               error
}

// GeocodeAddressCall records a call to GeocodeAddress
type GeocodeAddressCall struct {
	Address *models.Address
}

// ReverseGeocodeCall records a call to ReverseGeocode
type ReverseGeocodeCall struct {
	Lat float64
	Lng float64
}

// NewMockMapboxService creates a new mock Mapbox service with sensible defaults
func NewMockMapboxService() *MockMapboxService {
	return &MockMapboxService{
		DefaultLatitude:         37.7749,
		DefaultLongitude:        -122.4194,
		DefaultBoundaryType:     "city",
		DefaultBoundaryName:     "San Francisco",
		DefaultBoundaryState:    "California",
		DefaultPlaceID:          "mock_place_id",
		DefaultCountyName:       "San Francisco County",
		DefaultLocalityName:     "",
		DefaultNeighborhoodName: "",
		PublicToken:             "pk.mock_public_token", // #nosec G101 -- test mock value
		GeocodeAddressCalls:     make([]GeocodeAddressCall, 0),
		ReverseGeocodeCalls:     make([]ReverseGeocodeCall, 0),
	}
}

// GeocodeAddress geocodes an address (mock implementation)
func (m *MockMapboxService) GeocodeAddress(ctx context.Context, address *models.Address) (*services.GeocodeResult, error) {
	m.mu.Lock()
	m.GeocodeAddressCalls = append(m.GeocodeAddressCalls, GeocodeAddressCall{Address: address})
	m.mu.Unlock()

	// Use custom function if provided
	if m.GeocodeAddressFunc != nil {
		return m.GeocodeAddressFunc(ctx, address)
	}

	// Return error if configured to fail
	if m.ShouldFail {
		if m.FailError != nil {
			return nil, m.FailError
		}
		return nil, fmt.Errorf("mock mapbox error")
	}

	return &services.GeocodeResult{
		Latitude:         m.DefaultLatitude,
		Longitude:        m.DefaultLongitude,
		BoundaryType:     m.DefaultBoundaryType,
		BoundaryName:     m.DefaultBoundaryName,
		BoundaryState:    m.DefaultBoundaryState,
		PlaceID:          m.DefaultPlaceID,
		CountyName:       m.DefaultCountyName,
		LocalityName:     m.DefaultLocalityName,
		NeighborhoodName: m.DefaultNeighborhoodName,
	}, nil
}

// ReverseGeocode performs reverse geocoding (mock implementation)
func (m *MockMapboxService) ReverseGeocode(ctx context.Context, lat, lng float64) (*services.GeocodeResult, error) {
	m.mu.Lock()
	m.ReverseGeocodeCalls = append(m.ReverseGeocodeCalls, ReverseGeocodeCall{Lat: lat, Lng: lng})
	m.mu.Unlock()

	// Use custom function if provided
	if m.ReverseGeocodeFunc != nil {
		return m.ReverseGeocodeFunc(ctx, lat, lng)
	}

	// Return error if configured to fail
	if m.ShouldFail {
		if m.FailError != nil {
			return nil, m.FailError
		}
		return nil, fmt.Errorf("mock mapbox error")
	}

	return &services.GeocodeResult{
		Latitude:         lat,
		Longitude:        lng,
		BoundaryType:     m.DefaultBoundaryType,
		BoundaryName:     m.DefaultBoundaryName,
		BoundaryState:    m.DefaultBoundaryState,
		PlaceID:          m.DefaultPlaceID,
		CountyName:       m.DefaultCountyName,
		LocalityName:     m.DefaultLocalityName,
		NeighborhoodName: m.DefaultNeighborhoodName,
	}, nil
}

// GetPublicToken returns the public token
func (m *MockMapboxService) GetPublicToken() string {
	return m.PublicToken
}

// GetCityBoundary returns a mock city boundary
func (m *MockMapboxService) GetCityBoundary(ctx context.Context, lat, lng float64, geocodeResult *services.GeocodeResult, address *models.Address) (*services.CityBoundary, error) {
	// Use custom function if provided
	if m.GetCityBoundaryFunc != nil {
		return m.GetCityBoundaryFunc(ctx, lat, lng, geocodeResult, address)
	}

	if m.ShouldFail {
		if m.FailError != nil {
			return nil, m.FailError
		}
		return nil, fmt.Errorf("mock mapbox error")
	}

	// Create a simple square polygon around the point
	delta := 0.05 // roughly 5km
	geoJSON := fmt.Sprintf(
		`{"type":"Polygon","coordinates":[[[%f,%f],[%f,%f],[%f,%f],[%f,%f],[%f,%f]]]}`,
		lng-delta, lat-delta,
		lng+delta, lat-delta,
		lng+delta, lat+delta,
		lng-delta, lat+delta,
		lng-delta, lat-delta,
	)

	name := m.DefaultBoundaryName
	state := m.DefaultBoundaryState
	boundaryType := m.DefaultBoundaryType
	placeID := m.DefaultPlaceID

	if geocodeResult != nil {
		name = geocodeResult.BoundaryName
		state = geocodeResult.BoundaryState
		boundaryType = geocodeResult.BoundaryType
		placeID = geocodeResult.PlaceID
	}

	// If geocoding returned county but address has a city, use the city name
	if boundaryType == "county" && address != nil && address.City != "" {
		name = address.City
		boundaryType = "city"
		if address.State != "" {
			state = address.State
		}
	}

	return &services.CityBoundary{
		PlaceID: placeID,
		Name:    name,
		State:   state,
		Type:    boundaryType,
		GeoJSON: geoJSON,
		Center:  [2]float64{lng, lat},
	}, nil
}

// GetStateBoundary returns a mock state boundary
func (m *MockMapboxService) GetStateBoundary(ctx context.Context, stateName string, lat, lng float64) (*services.StateBoundary, error) {
	// Use custom function if provided
	if m.GetStateBoundaryFunc != nil {
		return m.GetStateBoundaryFunc(ctx, stateName, lat, lng)
	}

	if m.ShouldFail {
		if m.FailError != nil {
			return nil, m.FailError
		}
		return nil, fmt.Errorf("mock mapbox error")
	}

	// Create a simple rectangle around the coordinates for the state
	delta := 2.0 // roughly 200km for a state
	geoJSON := fmt.Sprintf(
		`{"type":"Polygon","coordinates":[[[%f,%f],[%f,%f],[%f,%f],[%f,%f],[%f,%f]]]}`,
		lng-delta, lat-delta,
		lng+delta, lat-delta,
		lng+delta, lat+delta,
		lng-delta, lat+delta,
		lng-delta, lat-delta,
	)

	return &services.StateBoundary{
		PlaceID: "mock_state_place_id",
		Name:    stateName,
		GeoJSON: geoJSON,
		Center:  [2]float64{lng, lat},
	}, nil
}

// GetCountyBoundary returns a mock county boundary
func (m *MockMapboxService) GetCountyBoundary(ctx context.Context, countyName, stateName string, lat, lng float64) (*services.CountyBoundary, error) {
	// Use custom function if provided
	if m.GetCountyBoundaryFunc != nil {
		return m.GetCountyBoundaryFunc(ctx, countyName, stateName, lat, lng)
	}

	if m.ShouldFail {
		if m.FailError != nil {
			return nil, m.FailError
		}
		return nil, fmt.Errorf("mock mapbox error")
	}

	// Create a simple rectangle around the coordinates for the county
	delta := 0.5 // roughly 50km for a county
	geoJSON := fmt.Sprintf(
		`{"type":"Polygon","coordinates":[[[%f,%f],[%f,%f],[%f,%f],[%f,%f],[%f,%f]]]}`,
		lng-delta, lat-delta,
		lng+delta, lat-delta,
		lng+delta, lat+delta,
		lng-delta, lat+delta,
		lng-delta, lat-delta,
	)

	return &services.CountyBoundary{
		PlaceID: "mock_county_place_id",
		Name:    countyName,
		State:   stateName,
		GeoJSON: geoJSON,
		Center:  [2]float64{lng, lat},
	}, nil
}

// GetCountyForCoordinates returns a mock county for given coordinates
func (m *MockMapboxService) GetCountyForCoordinates(ctx context.Context, lat, lng float64, stateName string) (*services.CountyBoundary, error) {
	// Use custom function if provided
	if m.GetCountyForCoordinatesFunc != nil {
		return m.GetCountyForCoordinatesFunc(ctx, lat, lng, stateName)
	}

	if m.ShouldFail {
		if m.FailError != nil {
			return nil, m.FailError
		}
		return nil, fmt.Errorf("mock mapbox error")
	}

	// Create a simple rectangle around the coordinates for the county
	delta := 0.5 // roughly 50km for a county
	geoJSON := fmt.Sprintf(
		`{"type":"Polygon","coordinates":[[[%f,%f],[%f,%f],[%f,%f],[%f,%f],[%f,%f]]]}`,
		lng-delta, lat-delta,
		lng+delta, lat-delta,
		lng+delta, lat+delta,
		lng-delta, lat+delta,
		lng-delta, lat-delta,
	)

	return &services.CountyBoundary{
		PlaceID: "mock_county_place_id",
		Name:    "Mock County",
		State:   stateName,
		GeoJSON: geoJSON,
		Center:  [2]float64{lng, lat},
	}, nil
}

// GetLocalityBoundary returns a mock locality boundary
// Returns boundary without geometry to simulate the common case of no OSM polygon
func (m *MockMapboxService) GetLocalityBoundary(ctx context.Context, localityName, cityName, stateName string, lat, lng float64) (*services.LocalityBoundary, error) {
	// Use custom function if provided
	if m.GetLocalityBoundaryFunc != nil {
		return m.GetLocalityBoundaryFunc(ctx, localityName, cityName, stateName, lat, lng)
	}

	if m.ShouldFail {
		if m.FailError != nil {
			return nil, m.FailError
		}
		return nil, fmt.Errorf("mock mapbox error")
	}

	// Most localities may not have polygon boundaries
	// Return with empty GeoJSON to simulate typical case
	return &services.LocalityBoundary{
		PlaceID: "mock_locality_place_id",
		Name:    localityName,
		State:   stateName,
		City:    cityName,
		GeoJSON: "", // No geometry (typical for many localities)
		Center:  [2]float64{lng, lat},
	}, nil
}

// GetNeighborhoodBoundary returns a mock neighborhood boundary
// Returns boundary without geometry since ~92% of neighborhoods lack OSM polygons
func (m *MockMapboxService) GetNeighborhoodBoundary(ctx context.Context, neighborhoodName, localityName, cityName, stateName string, lat, lng float64) (*services.NeighborhoodBoundary, error) {
	// Use custom function if provided
	if m.GetNeighborhoodBoundaryFunc != nil {
		return m.GetNeighborhoodBoundaryFunc(ctx, neighborhoodName, localityName, cityName, stateName, lat, lng)
	}

	if m.ShouldFail {
		if m.FailError != nil {
			return nil, m.FailError
		}
		return nil, fmt.Errorf("mock mapbox error")
	}

	// Most neighborhoods (~92%) lack polygon boundaries
	// Return with empty GeoJSON to simulate typical case
	return &services.NeighborhoodBoundary{
		PlaceID:  "mock_neighborhood_place_id",
		Name:     neighborhoodName,
		State:    stateName,
		City:     cityName,
		Locality: localityName,
		GeoJSON:  "", // No geometry (typical for ~92% of neighborhoods)
		Center:   [2]float64{lng, lat},
	}, nil
}

// Reset clears all recorded calls and custom functions
func (m *MockMapboxService) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GeocodeAddressCalls = make([]GeocodeAddressCall, 0)
	m.ReverseGeocodeCalls = make([]ReverseGeocodeCall, 0)
	m.ShouldFail = false
	m.FailError = nil
	// Clear custom functions
	m.GeocodeAddressFunc = nil
	m.ReverseGeocodeFunc = nil
	m.GetCityBoundaryFunc = nil
	m.GetStateBoundaryFunc = nil
	m.GetCountyBoundaryFunc = nil
	m.GetCountyForCoordinatesFunc = nil
	m.GetLocalityBoundaryFunc = nil
	m.GetNeighborhoodBoundaryFunc = nil
}

// Ensure mocks implement interfaces
var _ services.PostgridServiceInterface = (*MockPostgridService)(nil)
var _ services.MapboxServiceInterface = (*MockMapboxService)(nil)
