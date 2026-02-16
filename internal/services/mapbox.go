package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
)

// MapboxService handles Mapbox API interactions
type MapboxService struct {
	publicToken string
	secretToken string
	client      *http.Client
}

// NewMapboxService creates a new Mapbox service
func NewMapboxService(cfg *config.MapboxConfig) *MapboxService {
	return &MapboxService{
		publicToken: cfg.PublicToken,
		secretToken: cfg.SecretToken,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GeocodeResult represents the result of geocoding an address
type GeocodeResult struct {
	Latitude      float64
	Longitude     float64
	BoundaryType  string // 'city', 'town', 'county'
	BoundaryName  string
	BoundaryState string
	PlaceID       string
	// Extended hierarchy fields from Mapbox context
	CountyName       string // from district.* context
	LocalityName     string // from locality.* context (boroughs, sub-cities like Brooklyn)
	NeighborhoodName string // from neighborhood.* context
}

// GeocodeAddress geocodes an address using Mapbox
// NOTE: Address is processed in memory only and NEVER stored
func (s *MapboxService) GeocodeAddress(ctx context.Context, address *models.Address) (*GeocodeResult, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "GET mapbox /geocoding"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	if s.secretToken == "" {
		// Return mock result for development
		return &GeocodeResult{
			Latitude:         37.7749,
			Longitude:        -122.4194,
			BoundaryType:     "city",
			BoundaryName:     "San Francisco",
			BoundaryState:    "California",
			PlaceID:          "mock_place_id",
			CountyName:       "San Francisco County",
			LocalityName:     "",
			NeighborhoodName: "",
		}, nil
	}

	// Build search query
	searchText := fmt.Sprintf("%s, %s, %s %s",
		address.Line1,
		address.City,
		address.State,
		address.PostalCode,
	)

	// URL encode the search text
	encodedSearch := url.QueryEscape(searchText)

	// Build Mapbox Geocoding API URL
	apiURL := fmt.Sprintf(
		"https://api.mapbox.com/geocoding/v5/mapbox.places/%s.json?access_token=%s&country=US&types=address",
		encodedSearch,
		s.secretToken,
	)

	return s.geocodeAddressWithURL(ctx, apiURL, address)
}

// geocodeAddressWithURL geocodes an address using a specific URL (for testing)
func (s *MapboxService) geocodeAddressWithURL(ctx context.Context, apiURL string, address *models.Address) (*GeocodeResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s", string(body))
	}

	// Parse response
	var apiResp struct {
		Features []struct {
			ID       string    `json:"id"`
			Center   []float64 `json:"center"` // [longitude, latitude]
			Context  []struct {
				ID        string `json:"id"`
				Text      string `json:"text"`
				ShortCode string `json:"short_code"`
			} `json:"context"`
		} `json:"features"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(apiResp.Features) == 0 {
		return nil, fmt.Errorf("geocoding returned no results for address")
	}

	feature := apiResp.Features[0]

	result := &GeocodeResult{
		Longitude: feature.Center[0],
		Latitude:  feature.Center[1],
		PlaceID:   feature.ID,
	}

	// Extract boundary information from context
	// Process all context items to extract the full hierarchy
	for _, ctx := range feature.Context {
		switch {
		case strings.HasPrefix(ctx.ID, "neighborhood"):
			// Neighborhood (e.g., "Williamsburg" in NYC)
			result.NeighborhoodName = ctx.Text
		case strings.HasPrefix(ctx.ID, "locality"):
			// Locality - boroughs/sub-cities (e.g., "Brooklyn" in NYC)
			result.LocalityName = ctx.Text
		case strings.HasPrefix(ctx.ID, "place"):
			// City/town
			result.BoundaryType = "city"
			result.BoundaryName = ctx.Text
		case strings.HasPrefix(ctx.ID, "district"):
			// County
			result.CountyName = ctx.Text
			// Only use as boundary if no city found
			if result.BoundaryType == "" {
				result.BoundaryType = "county"
				result.BoundaryName = ctx.Text
			}
		case strings.HasPrefix(ctx.ID, "region"):
			// State
			result.BoundaryState = ctx.Text
		}
	}

	// Default to county if no city found (unincorporated area)
	if result.BoundaryType == "" {
		result.BoundaryType = "county"
	}

	return result, nil
}

// ReverseGeocode performs reverse geocoding to get place information from coordinates
func (s *MapboxService) ReverseGeocode(ctx context.Context, lat, lng float64) (*GeocodeResult, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "GET mapbox /reverse-geocoding"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	if s.secretToken == "" {
		// Return mock result for development
		return &GeocodeResult{
			Latitude:      lat,
			Longitude:     lng,
			BoundaryType:  "city",
			BoundaryName:  "San Francisco",
			BoundaryState: "California",
			PlaceID:       "mock_place_id",
		}, nil
	}

	// Build Mapbox Reverse Geocoding API URL
	apiURL := fmt.Sprintf(
		"https://api.mapbox.com/geocoding/v5/mapbox.places/%f,%f.json?access_token=%s&types=place,district,region",
		lng, lat,
		s.secretToken,
	)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s", string(body))
	}

	// Parse response
	var apiResp struct {
		Features []struct {
			ID        string    `json:"id"`
			PlaceType []string  `json:"place_type"`
			Text      string    `json:"text"`
			Center    []float64 `json:"center"`
			Context   []struct {
				ID   string `json:"id"`
				Text string `json:"text"`
			} `json:"context"`
		} `json:"features"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	result := &GeocodeResult{
		Latitude:  lat,
		Longitude: lng,
	}

	// Find the place (city) or district (county)
	for _, feature := range apiResp.Features {
		for _, placeType := range feature.PlaceType {
			if placeType == "place" && result.BoundaryType == "" {
				result.BoundaryType = "city"
				result.BoundaryName = feature.Text
				result.PlaceID = feature.ID
			} else if placeType == "district" && result.BoundaryType == "" {
				result.BoundaryType = "county"
				result.BoundaryName = feature.Text
				result.PlaceID = feature.ID
			} else if placeType == "region" {
				result.BoundaryState = feature.Text
			}
		}

		// Also check context for state
		for _, ctx := range feature.Context {
			if len(ctx.ID) > 6 && ctx.ID[:6] == "region" {
				result.BoundaryState = ctx.Text
			}
		}
	}

	return result, nil
}

// GetCityBoundary returns a boundary polygon for the city/place containing the given coordinates
// Uses OpenStreetMap Nominatim API to get actual administrative boundary polygons
// Falls back to circular buffer if actual boundary cannot be retrieved
// If geocoding returns county but address has a city, tries to look up city boundary by name
func (s *MapboxService) GetCityBoundary(ctx context.Context, lat, lng float64, geocodeResult *GeocodeResult, address *models.Address) (*CityBoundary, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "GET nominatim /city-boundary"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	addressCity := ""
	if address != nil {
		addressCity = address.City
	}
	slog.Debug("get city boundary called", "boundary_type", geocodeResult.BoundaryType, "boundary_name", geocodeResult.BoundaryName, "address_city", addressCity)

	// Always try city name lookup first if we have a city in the address
	// This is more reliable than coordinate-based lookup which may return county boundaries
	if address != nil && address.City != "" {
		cityBoundary, err := s.fetchCityBoundaryByName(ctx, address.City, address.State, lat, lng)
		if err == nil && cityBoundary != nil {
			slog.Debug("found city boundary via name lookup", "city", address.City)
			return cityBoundary, nil
		}
		if err != nil {
			slog.Warn("city name lookup failed, trying coordinate-based lookup", "city", address.City, "error", err)
		}
	}

	// Fall back to coordinate-based OSM lookup
	boundary, err := s.fetchOSMBoundary(ctx, lat, lng, geocodeResult)
	if err == nil && boundary != nil {
		// Check if OSM returned a county when we expected a city
		if geocodeResult.BoundaryType == "city" && boundary.Type == "county" {
			slog.Warn("osm returned county boundary for city-type geocode result")
		}
		return boundary, nil
	}

	// Log the error but fall back to circular buffer
	if err != nil {
		slog.Warn("failed to fetch osm boundary, falling back to circular buffer", "error", err)
	}

	// Fall back to circular buffer polygon around the point
	radiusKm := 5.0
	if geocodeResult.BoundaryType == "county" {
		radiusKm = 20.0
	}

	polygon := createCircularPolygon(lat, lng, radiusKm)

	return &CityBoundary{
		PlaceID: geocodeResult.PlaceID,
		Name:    geocodeResult.BoundaryName,
		State:   geocodeResult.BoundaryState,
		Type:    geocodeResult.BoundaryType,
		GeoJSON: polygon,
		Center:  [2]float64{lng, lat},
	}, nil
}

// fetchOSMBoundary fetches the actual administrative boundary from OpenStreetMap Nominatim
func (s *MapboxService) fetchOSMBoundary(ctx context.Context, lat, lng float64, geocodeResult *GeocodeResult) (*CityBoundary, error) {
	// Use Nominatim reverse geocoding with polygon output
	// zoom level 10 = city, 8 = county
	zoom := 10
	if geocodeResult.BoundaryType == "county" {
		zoom = 8
	}

	apiURL := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/reverse?format=jsonv2&lat=%f&lon=%f&zoom=%d&polygon_geojson=1",
		lat, lng, zoom,
	)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Nominatim requires a User-Agent header
	req.Header.Set("User-Agent", "CommunityRapidResponse/1.0 (https://communityrapidresponse.net)")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var osmResp struct {
		PlaceID     int64  `json:"place_id"`
		DisplayName string `json:"display_name"`
		Name        string `json:"name"`
		Type        string `json:"type"`
		AddressType string `json:"addresstype"`
		Address     struct {
			City    string `json:"city"`
			Town    string `json:"town"`
			Village string `json:"village"`
			County  string `json:"county"`
			State   string `json:"state"`
		} `json:"address"`
		GeoJSON json.RawMessage `json:"geojson"`
	}

	if err := json.Unmarshal(body, &osmResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if we got a valid polygon
	if len(osmResp.GeoJSON) == 0 || string(osmResp.GeoJSON) == "null" {
		return nil, fmt.Errorf("no boundary polygon returned")
	}

	// Parse the GeoJSON to validate it and potentially convert MultiPolygon to Polygon
	var geoType struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(osmResp.GeoJSON, &geoType); err != nil {
		return nil, fmt.Errorf("failed to parse geojson type: %w", err)
	}

	// We only support Polygon and MultiPolygon
	if geoType.Type != "Polygon" && geoType.Type != "MultiPolygon" {
		return nil, fmt.Errorf("unsupported geometry type: %s", geoType.Type)
	}

	// Determine the name from the response
	// The type should stay as what was requested (based on geocodeResult.BoundaryType)
	// since we used the appropriate zoom level for that type
	boundaryName := osmResp.Name
	boundaryType := geocodeResult.BoundaryType
	boundaryState := geocodeResult.BoundaryState

	// Fall back to address fields if name is empty, but DON'T change the type
	// The type is determined by the zoom level we used, not the address fields
	if boundaryName == "" {
		if boundaryType == "county" && osmResp.Address.County != "" {
			boundaryName = osmResp.Address.County
		} else if osmResp.Address.City != "" {
			boundaryName = osmResp.Address.City
		} else if osmResp.Address.Town != "" {
			boundaryName = osmResp.Address.Town
		} else if osmResp.Address.Village != "" {
			boundaryName = osmResp.Address.Village
		} else if osmResp.Address.County != "" {
			boundaryName = osmResp.Address.County
		}
	}

	// Use address state if not set
	if boundaryState == "" && osmResp.Address.State != "" {
		boundaryState = osmResp.Address.State
	}

	// Use the original geocode result name if OSM didn't give us one
	if boundaryName == "" {
		boundaryName = geocodeResult.BoundaryName
	}

	slog.Debug("osm boundary lookup result", "name", boundaryName, "type", boundaryType, "state", boundaryState)

	return &CityBoundary{
		PlaceID: fmt.Sprintf("osm_%d", osmResp.PlaceID),
		Name:    boundaryName,
		State:   boundaryState,
		Type:    boundaryType,
		GeoJSON: string(osmResp.GeoJSON),
		Center:  [2]float64{lng, lat},
	}, nil
}

// fetchCityBoundaryByName looks up a city boundary by name using OSM Nominatim search API
// This is used as a fallback when coordinate-based lookup returns county instead of city
func (s *MapboxService) fetchCityBoundaryByName(ctx context.Context, cityName, stateName string, lat, lng float64) (*CityBoundary, error) {
	// Build search query: "City, State, USA"
	// Use the state name as-is (could be abbreviation or full name)
	var searchQuery string
	if stateName != "" {
		searchQuery = cityName + ", " + stateName + ", USA"
	} else {
		searchQuery = cityName + ", USA"
	}

	slog.Debug("searching osm for city boundary", "query", searchQuery)

	// Use Nominatim search API with polygon output and address details
	// Don't use featuretype filter as it's too restrictive - let OSM find the best match
	apiURL := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/search?format=jsonv2&q=%s&polygon_geojson=1&limit=5&countrycodes=us&addressdetails=1",
		url.QueryEscape(searchQuery),
	)

	return s.fetchCityBoundaryByNameWithURL(ctx, apiURL, cityName, stateName, lat, lng)
}

// fetchCityBoundaryByNameWithURL performs a city boundary search with a custom URL (for testing)
func (s *MapboxService) fetchCityBoundaryByNameWithURL(ctx context.Context, apiURL, cityName, stateName string, lat, lng float64) (*CityBoundary, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "CommunityRapidResponse/1.0 (https://communityrapidresponse.net)")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response - search returns an array
	// Note: jsonv2 format uses "category" instead of "class"
	var osmResults []struct {
		PlaceID     int64           `json:"place_id"`
		DisplayName string          `json:"display_name"`
		Name        string          `json:"name"`
		Type        string          `json:"type"`
		Category    string          `json:"category"`
		AddressType string          `json:"addresstype"`
		Lat         string          `json:"lat"`
		Lon         string          `json:"lon"`
		GeoJSON     json.RawMessage `json:"geojson"`
	}

	if err := json.Unmarshal(body, &osmResults); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	slog.Debug("osm search returned results", "count", len(osmResults))

	if len(osmResults) == 0 {
		return nil, fmt.Errorf("no results found for: %s", cityName)
	}

	// Find the first result that is a city/town/village with a valid polygon
	var result *struct {
		PlaceID     int64           `json:"place_id"`
		DisplayName string          `json:"display_name"`
		Name        string          `json:"name"`
		Type        string          `json:"type"`
		Category    string          `json:"category"`
		AddressType string          `json:"addresstype"`
		Lat         string          `json:"lat"`
		Lon         string          `json:"lon"`
		GeoJSON     json.RawMessage `json:"geojson"`
	}

	// Look for a result with polygon geometry
	for i := range osmResults {
		r := &osmResults[i]
		slog.Debug("osm result", "index", i, "name", r.Name, "type", r.Type, "category", r.Category, "address_type", r.AddressType, "has_geojson", len(r.GeoJSON) > 0 && string(r.GeoJSON) != "null")

		if len(r.GeoJSON) == 0 || string(r.GeoJSON) == "null" {
			continue
		}

		// Skip state and county level boundaries - we only want city-level
		if r.AddressType == "state" || r.AddressType == "county" {
			slog.Debug("skipping result, not a city", "index", i, "address_type", r.AddressType)
			continue
		}

		// Check geometry type - only accept Polygon or MultiPolygon
		var geoType struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(r.GeoJSON, &geoType); err != nil {
			continue
		}

		if geoType.Type != "Polygon" && geoType.Type != "MultiPolygon" {
			slog.Debug("skipping result with unsupported geometry type", "geometry_type", geoType.Type)
			continue
		}

		// Accept city, town, village, census (CDP), administrative, municipality
		isCityType := r.Type == "city" || r.Type == "town" || r.Type == "village" ||
			r.Type == "census" || r.Type == "administrative" || r.Type == "municipality"
		isValidCategory := r.Category == "boundary" || r.Category == "place"

		if isCityType || isValidCategory {
			result = r
			slog.Debug("found polygon result", "name", r.Name, "type", r.Type, "category", r.Category, "address_type", r.AddressType)
			break
		}
	}

	if result == nil {
		return nil, fmt.Errorf("no valid polygon boundary found for: %s", cityName)
	}

	slog.Debug("using city boundary", "name", result.Name, "type", result.Type, "category", result.Category)

	// Use the result's name if available, otherwise use the search city name
	boundaryName := result.Name
	if boundaryName == "" {
		boundaryName = cityName
	}

	return &CityBoundary{
		PlaceID: fmt.Sprintf("osm_%d", result.PlaceID),
		Name:    boundaryName,
		State:   stateName,
		Type:    "city",
		GeoJSON: string(result.GeoJSON),
		Center:  [2]float64{lng, lat},
	}, nil
}

// GetStateBoundary fetches the state boundary polygon from OSM Nominatim
func (s *MapboxService) GetStateBoundary(ctx context.Context, stateName string, lat, lng float64) (*StateBoundary, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "GET nominatim /state-boundary"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	// Try multiple search strategies to find the state boundary
	searchQueries := []string{
		stateName + ", United States",           // Most specific
		"State of " + stateName + ", USA",       // Alternative format
		stateName + ", USA",                     // Simple format
	}

	for _, searchQuery := range searchQueries {
		slog.Debug("searching osm for state boundary", "query", searchQuery)

		result, err := s.searchStateBoundary(ctx, searchQuery, stateName, lat, lng)
		if err == nil && result != nil {
			return result, nil
		}
		if err != nil {
			slog.Debug("state boundary search failed", "query", searchQuery, "error", err)
		} else {
			slog.Debug("state boundary search did not find valid result, trying next", "query", searchQuery)
		}
	}

	return nil, fmt.Errorf("no valid state boundary found for: %s", stateName)
}

// searchStateBoundary performs a single search for a state boundary
func (s *MapboxService) searchStateBoundary(ctx context.Context, searchQuery, stateName string, lat, lng float64) (*StateBoundary, error) {
	apiURL := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/search?format=jsonv2&q=%s&polygon_geojson=1&limit=10&countrycodes=us&addressdetails=1",
		url.QueryEscape(searchQuery),
	)
	return s.searchStateBoundaryWithURL(ctx, apiURL, stateName, lat, lng)
}

// searchStateBoundaryWithURL performs a state boundary search with a custom URL (for testing)
func (s *MapboxService) searchStateBoundaryWithURL(ctx context.Context, apiURL, stateName string, lat, lng float64) (*StateBoundary, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "CommunityRapidResponse/1.0 (https://communityrapidresponse.net)")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Note: jsonv2 format uses "category" instead of "class"
	var osmResults []struct {
		PlaceID     int64  `json:"place_id"`
		DisplayName string `json:"display_name"`
		Name        string `json:"name"`
		Type        string `json:"type"`
		Category    string `json:"category"`
		AddressType string `json:"addresstype"`
		Address     struct {
			State   string `json:"state"`
			Country string `json:"country"`
		} `json:"address"`
		GeoJSON json.RawMessage `json:"geojson"`
	}

	if err := json.Unmarshal(body, &osmResults); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	slog.Debug("osm state search returned results", "count", len(osmResults))

	// Find first result with valid polygon geometry that is actually a state
	for i, result := range osmResults {
		slog.Debug("osm state result", "index", i, "name", result.Name, "type", result.Type, "category", result.Category, "address_type", result.AddressType, "display_name", result.DisplayName)

		if len(result.GeoJSON) == 0 || string(result.GeoJSON) == "null" {
			slog.Debug("skipping result, no geometry", "index", i)
			continue
		}

		var geoType struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(result.GeoJSON, &geoType); err != nil {
			continue
		}

		if geoType.Type != "Polygon" && geoType.Type != "MultiPolygon" {
			slog.Debug("skipping result, unsupported geometry type", "index", i, "geometry_type", geoType.Type)
			continue
		}

		// Check that this is actually a state-level boundary
		// addresstype=state is the definitive indicator for US states
		// We must NOT accept generic administrative boundaries as they can be cities/counties
		if result.AddressType != "state" {
			slog.Debug("skipping result, not a state", "index", i, "address_type", result.AddressType)
			continue
		}

		// Verify the name matches what we're looking for (case-insensitive)
		resultName := strings.ToLower(result.Name)
		searchName := strings.ToLower(stateName)
		if !strings.Contains(resultName, searchName) && !strings.Contains(searchName, resultName) {
			slog.Debug("skipping result, name mismatch", "index", i, "result_name", result.Name, "expected_name", stateName)
			continue
		}

		// Valid state boundary found
		boundaryName := result.Name
		if boundaryName == "" {
			boundaryName = stateName
		}

		slog.Debug("found state boundary", "name", boundaryName, "type", result.Type, "category", result.Category)
		return &StateBoundary{
			PlaceID: fmt.Sprintf("osm_%d", result.PlaceID),
			Name:    boundaryName,
			GeoJSON: string(result.GeoJSON),
			Center:  [2]float64{lng, lat},
		}, nil
	}

	return nil, fmt.Errorf("no valid state boundary found in this search")
}

// GetCountyBoundary fetches the county boundary polygon from OSM Nominatim
func (s *MapboxService) GetCountyBoundary(ctx context.Context, countyName, stateName string, lat, lng float64) (*CountyBoundary, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "GET nominatim /county-boundary"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	// Build search query
	var searchQuery string
	if stateName != "" {
		searchQuery = countyName + ", " + stateName + ", USA"
	} else {
		searchQuery = countyName + ", USA"
	}
	slog.Debug("searching osm for county boundary", "query", searchQuery)

	apiURL := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/search?format=jsonv2&q=%s&polygon_geojson=1&limit=5&countrycodes=us&addressdetails=1",
		url.QueryEscape(searchQuery),
	)

	return s.getCountyBoundaryWithURL(ctx, apiURL, countyName, stateName, lat, lng)
}

// getCountyBoundaryWithURL fetches a county boundary with a custom URL (for testing)
func (s *MapboxService) getCountyBoundaryWithURL(ctx context.Context, apiURL, countyName, stateName string, lat, lng float64) (*CountyBoundary, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "CommunityRapidResponse/1.0 (https://communityrapidresponse.net)")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Note: jsonv2 format uses "category" instead of "class"
	var osmResults []struct {
		PlaceID     int64           `json:"place_id"`
		DisplayName string          `json:"display_name"`
		Name        string          `json:"name"`
		Type        string          `json:"type"`
		Category    string          `json:"category"`
		AddressType string          `json:"addresstype"`
		GeoJSON     json.RawMessage `json:"geojson"`
	}

	if err := json.Unmarshal(body, &osmResults); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	slog.Debug("osm county search returned results", "count", len(osmResults))

	// Find first result with valid polygon geometry that is a county
	for i, result := range osmResults {
		slog.Debug("osm county result", "index", i, "name", result.Name, "type", result.Type, "category", result.Category, "address_type", result.AddressType, "has_geojson", len(result.GeoJSON) > 0 && string(result.GeoJSON) != "null")

		if len(result.GeoJSON) == 0 || string(result.GeoJSON) == "null" {
			continue
		}

		var geoType struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(result.GeoJSON, &geoType); err != nil {
			continue
		}

		if geoType.Type != "Polygon" && geoType.Type != "MultiPolygon" {
			continue
		}

		// Must be addresstype=county to be a county boundary
		if result.AddressType != "county" {
			slog.Debug("skipping result, not a county", "index", i, "address_type", result.AddressType)
			continue
		}

		// Valid county boundary found
		boundaryName := result.Name
		if boundaryName == "" {
			boundaryName = countyName
		}

		slog.Debug("found county boundary", "name", boundaryName)
		return &CountyBoundary{
			PlaceID: fmt.Sprintf("osm_%d", result.PlaceID),
			Name:    boundaryName,
			State:   stateName,
			GeoJSON: string(result.GeoJSON),
			Center:  [2]float64{lng, lat},
		}, nil
	}

	return nil, fmt.Errorf("no valid county boundary found for: %s", countyName)
}

// GetCountyForCoordinates uses reverse geocoding to find the county containing the given coordinates
func (s *MapboxService) GetCountyForCoordinates(ctx context.Context, lat, lng float64, stateName string) (*CountyBoundary, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "GET nominatim /county-reverse"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	// Use Nominatim reverse geocoding at county zoom level (zoom 8)
	apiURL := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/reverse?format=jsonv2&lat=%f&lon=%f&zoom=8&polygon_geojson=1",
		lat, lng,
	)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "CommunityRapidResponse/1.0 (https://communityrapidresponse.net)")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var osmResp struct {
		PlaceID     int64  `json:"place_id"`
		DisplayName string `json:"display_name"`
		Name        string `json:"name"`
		Address     struct {
			County string `json:"county"`
			State  string `json:"state"`
		} `json:"address"`
		GeoJSON json.RawMessage `json:"geojson"`
	}

	if err := json.Unmarshal(body, &osmResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if we got a valid polygon
	if len(osmResp.GeoJSON) == 0 || string(osmResp.GeoJSON) == "null" {
		return nil, fmt.Errorf("no county boundary polygon returned")
	}

	var geoType struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(osmResp.GeoJSON, &geoType); err != nil {
		return nil, fmt.Errorf("failed to parse geojson type: %w", err)
	}

	if geoType.Type != "Polygon" && geoType.Type != "MultiPolygon" {
		return nil, fmt.Errorf("unsupported geometry type: %s", geoType.Type)
	}

	countyName := osmResp.Address.County
	if countyName == "" {
		countyName = osmResp.Name
	}
	if countyName == "" {
		return nil, fmt.Errorf("could not determine county name")
	}

	state := stateName
	if state == "" {
		state = osmResp.Address.State
	}

	slog.Debug("found county via reverse geocoding", "county", countyName, "state", state)

	return &CountyBoundary{
		PlaceID: fmt.Sprintf("osm_%d", osmResp.PlaceID),
		Name:    countyName,
		State:   state,
		GeoJSON: string(osmResp.GeoJSON),
		Center:  [2]float64{lng, lat},
	}, nil
}

// GetLocalityBoundary fetches a locality (borough/sub-city) boundary from OSM Nominatim
// Returns boundary with empty GeoJSON if no polygon boundary is available
func (s *MapboxService) GetLocalityBoundary(ctx context.Context, localityName, cityName, stateName string, lat, lng float64) (*LocalityBoundary, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "GET nominatim /locality-boundary"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	// Build search query: "LocalityName, CityName, StateName, USA"
	var searchQuery string
	if cityName != "" && stateName != "" {
		searchQuery = localityName + ", " + cityName + ", " + stateName + ", USA"
	} else if stateName != "" {
		searchQuery = localityName + ", " + stateName + ", USA"
	} else {
		searchQuery = localityName + ", USA"
	}

	slog.Debug("searching osm for locality boundary", "query", searchQuery)

	apiURL := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/search?format=jsonv2&q=%s&polygon_geojson=1&limit=5&countrycodes=us&addressdetails=1",
		url.QueryEscape(searchQuery),
	)

	return s.getLocalityBoundaryWithURL(ctx, apiURL, localityName, cityName, stateName, lat, lng)
}

// getLocalityBoundaryWithURL fetches a locality boundary with a custom URL (for testing)
func (s *MapboxService) getLocalityBoundaryWithURL(ctx context.Context, apiURL, localityName, cityName, stateName string, lat, lng float64) (*LocalityBoundary, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "CommunityRapidResponse/1.0 (https://communityrapidresponse.net)")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var osmResults []struct {
		PlaceID     int64           `json:"place_id"`
		DisplayName string          `json:"display_name"`
		Name        string          `json:"name"`
		Type        string          `json:"type"`
		Category    string          `json:"category"`
		AddressType string          `json:"addresstype"`
		GeoJSON     json.RawMessage `json:"geojson"`
	}

	if err := json.Unmarshal(body, &osmResults); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	slog.Debug("osm locality search returned results", "count", len(osmResults))

	// Find the first result that is a locality type with valid polygon (if any)
	var bestResult *struct {
		PlaceID     int64
		Name        string
		GeoJSON     string
		HasGeometry bool
	}

	for i := range osmResults {
		r := &osmResults[i]
		slog.Debug("osm locality result", "index", i, "name", r.Name, "type", r.Type, "category", r.Category, "address_type", r.AddressType, "has_geojson", len(r.GeoJSON) > 0 && string(r.GeoJSON) != "null")

		// Skip state, county, and city level boundaries - we only want locality-level (borough, suburb)
		if r.AddressType == "state" || r.AddressType == "county" || r.AddressType == "city" {
			slog.Debug("skipping result, not a locality", "index", i, "address_type", r.AddressType)
			continue
		}

		// Accept suburb, borough, quarter types for localities
		isLocalityType := r.Type == "suburb" || r.Type == "borough" || r.Type == "quarter" ||
			r.Type == "administrative" || r.Type == "city_district"
		isValidCategory := r.Category == "boundary" || r.Category == "place"

		if !isLocalityType && !isValidCategory {
			continue
		}

		hasGeometry := len(r.GeoJSON) > 0 && string(r.GeoJSON) != "null"
		geoJSONStr := ""

		if hasGeometry {
			// Check geometry type - only accept Polygon or MultiPolygon
			var geoType struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(r.GeoJSON, &geoType); err == nil {
				if geoType.Type == "Polygon" || geoType.Type == "MultiPolygon" {
					geoJSONStr = string(r.GeoJSON)
				} else {
					hasGeometry = false
				}
			} else {
				hasGeometry = false
			}
		}

		// Prefer results with geometry, but accept without
		if bestResult == nil || (!bestResult.HasGeometry && hasGeometry) {
			bestResult = &struct {
				PlaceID     int64
				Name        string
				GeoJSON     string
				HasGeometry bool
			}{
				PlaceID:     r.PlaceID,
				Name:        r.Name,
				GeoJSON:     geoJSONStr,
				HasGeometry: hasGeometry,
			}

			if hasGeometry {
				break // Found one with geometry, use it
			}
		}
	}

	// Return result even without geometry (name/center only)
	if bestResult != nil {
		slog.Debug("found locality", "name", bestResult.Name, "has_geometry", bestResult.HasGeometry)
		return &LocalityBoundary{
			PlaceID: fmt.Sprintf("osm_%d", bestResult.PlaceID),
			Name:    bestResult.Name,
			State:   stateName,
			City:    cityName,
			GeoJSON: bestResult.GeoJSON,
			Center:  [2]float64{lng, lat},
		}, nil
	}

	// No OSM result found - return boundary with just name/center (no geometry)
	slog.Debug("no osm result for locality, returning without geometry", "locality", localityName)
	return &LocalityBoundary{
		PlaceID: "",
		Name:    localityName,
		State:   stateName,
		City:    cityName,
		GeoJSON: "",
		Center:  [2]float64{lng, lat},
	}, nil
}

// GetNeighborhoodBoundary fetches a neighborhood boundary from OSM Nominatim
// Most neighborhoods (~92%) lack polygon boundaries, so GeoJSON is often empty
func (s *MapboxService) GetNeighborhoodBoundary(ctx context.Context, neighborhoodName, localityName, cityName, stateName string, lat, lng float64) (*NeighborhoodBoundary, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "GET nominatim /neighborhood-boundary"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	// Build search query including locality if present
	var searchQuery string
	if localityName != "" {
		searchQuery = neighborhoodName + ", " + localityName + ", " + stateName + ", USA"
	} else if cityName != "" {
		searchQuery = neighborhoodName + ", " + cityName + ", " + stateName + ", USA"
	} else {
		searchQuery = neighborhoodName + ", " + stateName + ", USA"
	}

	slog.Debug("searching osm for neighborhood boundary", "query", searchQuery)

	apiURL := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/search?format=jsonv2&q=%s&polygon_geojson=1&limit=5&countrycodes=us",
		url.QueryEscape(searchQuery),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "CommunityRapidResponse/1.0 (https://communityrapidresponse.net)")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var osmResults []struct {
		PlaceID     int64           `json:"place_id"`
		DisplayName string          `json:"display_name"`
		Name        string          `json:"name"`
		Type        string          `json:"type"`
		Category    string          `json:"category"`
		GeoJSON     json.RawMessage `json:"geojson"`
	}

	if err := json.Unmarshal(body, &osmResults); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	slog.Debug("osm neighborhood search returned results", "count", len(osmResults))

	// Find the first result that is a neighborhood type
	var bestResult *struct {
		PlaceID     int64
		Name        string
		GeoJSON     string
		HasGeometry bool
	}

	for i := range osmResults {
		r := &osmResults[i]
		slog.Debug("osm neighborhood result", "index", i, "name", r.Name, "type", r.Type, "category", r.Category, "has_geojson", len(r.GeoJSON) > 0 && string(r.GeoJSON) != "null")

		// Accept neighbourhood, suburb, quarter types for neighborhoods
		isNeighborhoodType := r.Type == "neighbourhood" || r.Type == "suburb" || r.Type == "quarter" ||
			r.Type == "residential" || r.Type == "administrative"
		isValidCategory := r.Category == "boundary" || r.Category == "place"

		if !isNeighborhoodType && !isValidCategory {
			continue
		}

		hasGeometry := len(r.GeoJSON) > 0 && string(r.GeoJSON) != "null"
		geoJSONStr := ""

		if hasGeometry {
			// Check geometry type - only accept Polygon or MultiPolygon
			var geoType struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(r.GeoJSON, &geoType); err == nil {
				if geoType.Type == "Polygon" || geoType.Type == "MultiPolygon" {
					geoJSONStr = string(r.GeoJSON)
				} else {
					hasGeometry = false
				}
			} else {
				hasGeometry = false
			}
		}

		// Prefer results with geometry, but accept without
		if bestResult == nil || (!bestResult.HasGeometry && hasGeometry) {
			bestResult = &struct {
				PlaceID     int64
				Name        string
				GeoJSON     string
				HasGeometry bool
			}{
				PlaceID:     r.PlaceID,
				Name:        r.Name,
				GeoJSON:     geoJSONStr,
				HasGeometry: hasGeometry,
			}

			if hasGeometry {
				break // Found one with geometry, use it
			}
		}
	}

	// Return result even without geometry (name/center only)
	if bestResult != nil {
		slog.Debug("found neighborhood", "name", bestResult.Name, "has_geometry", bestResult.HasGeometry)
		return &NeighborhoodBoundary{
			PlaceID:  fmt.Sprintf("osm_%d", bestResult.PlaceID),
			Name:     bestResult.Name,
			State:    stateName,
			City:     cityName,
			Locality: localityName,
			GeoJSON:  bestResult.GeoJSON,
			Center:   [2]float64{lng, lat},
		}, nil
	}

	// No OSM result found - return boundary with just name/center (no geometry)
	// This is expected for most neighborhoods
	slog.Debug("no osm result for neighborhood, returning without geometry", "neighborhood", neighborhoodName)
	return &NeighborhoodBoundary{
		PlaceID:  "",
		Name:     neighborhoodName,
		State:    stateName,
		City:     cityName,
		Locality: localityName,
		GeoJSON:  "",
		Center:   [2]float64{lng, lat},
	}, nil
}

// createCircularPolygon creates a GeoJSON polygon approximating a circle
// lat, lng are the center coordinates, radiusKm is the radius in kilometers
func createCircularPolygon(lat, lng, radiusKm float64) string {
	// Convert radius from km to approximate degrees
	// At the equator, 1 degree ≈ 111 km
	// Adjust for latitude
	latRadius := radiusKm / 111.0
	lngRadius := radiusKm / (111.0 * math.Cos(lat*math.Pi/180.0))

	// Create a 32-point polygon approximating a circle
	numPoints := 32
	coordinates := make([]string, numPoints+1)

	for i := 0; i < numPoints; i++ {
		angle := float64(i) * 2.0 * math.Pi / float64(numPoints)
		pointLng := lng + lngRadius*math.Cos(angle)
		pointLat := lat + latRadius*math.Sin(angle)
		coordinates[i] = fmt.Sprintf("[%f,%f]", pointLng, pointLat)
	}
	// Close the polygon
	coordinates[numPoints] = coordinates[0]

	return fmt.Sprintf(`{"type":"Polygon","coordinates":[[%s]]}`, strings.Join(coordinates, ","))
}

// GetPublicToken returns the public token for frontend use
func (s *MapboxService) GetPublicToken() string {
	return s.publicToken
}
