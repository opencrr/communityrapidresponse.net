package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// getTestDataDir returns the path to the testdata directory
func getTestDataDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "testdata", "osm")
}

// loadFixture loads a JSON fixture file
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(getTestDataDir(), name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to load fixture %s: %v", name, err)
	}
	return data
}

// TestGetStateBoundary_Washington tests that Washington state is correctly found
// even when Washington DC (a city) is returned first
func TestGetStateBoundary_Washington(t *testing.T) {
	fixture := loadFixture(t, "state_washington_simplified.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request
		if !strings.Contains(r.URL.Path, "/search") {
			t.Errorf("Expected /search path, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("format") != "jsonv2" {
			t.Errorf("Expected format=jsonv2, got %s", r.URL.Query().Get("format"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	// Create service with test server
	svc := &MapboxService{
		client: server.Client(),
	}

	// Use the internal method that accepts a custom URL
	result, err := svc.searchStateBoundaryWithURL(
		context.Background(),
		server.URL+"/search?format=jsonv2&q=Washington,+United+States&polygon_geojson=1&limit=10&countrycodes=us&addressdetails=1",
		"Washington",
		47.6, -122.3,
	)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	// Should find Washington STATE, not Washington DC
	if result.Name != "Washington" {
		t.Errorf("Expected name 'Washington', got '%s'", result.Name)
	}

	// Verify it's the state boundary by checking placeID
	if result.PlaceID != "osm_299671221" {
		t.Errorf("Expected state placeID 'osm_299671221', got '%s'", result.PlaceID)
	}

	// Verify GeoJSON is present
	if result.GeoJSON == "" {
		t.Error("Expected GeoJSON to be present")
	}

	// Verify it's a polygon
	var geo struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(result.GeoJSON), &geo); err != nil {
		t.Errorf("Failed to parse GeoJSON: %v", err)
	}
	if geo.Type != "Polygon" && geo.Type != "MultiPolygon" {
		t.Errorf("Expected Polygon or MultiPolygon, got %s", geo.Type)
	}
}

// TestGetStateBoundary_California tests a clean state result
func TestGetStateBoundary_California(t *testing.T) {
	fixture := loadFixture(t, "state_california_simplified.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	svc := &MapboxService{
		client: server.Client(),
	}

	result, err := svc.searchStateBoundaryWithURL(
		context.Background(),
		server.URL+"/search?format=jsonv2&q=California,+United+States&polygon_geojson=1&limit=10&countrycodes=us&addressdetails=1",
		"California",
		36.7, -118.8,
	)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	if result.Name != "California" {
		t.Errorf("Expected name 'California', got '%s'", result.Name)
	}

	// Verify it's a MultiPolygon (California has islands)
	var geo struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(result.GeoJSON), &geo); err != nil {
		t.Errorf("Failed to parse GeoJSON: %v", err)
	}
	if geo.Type != "MultiPolygon" {
		t.Errorf("Expected MultiPolygon, got %s", geo.Type)
	}
}

// TestGetStateBoundary_SkipsBusinesses tests that business results with Point geometry are skipped
func TestGetStateBoundary_SkipsBusinesses(t *testing.T) {
	fixture := loadFixture(t, "businesses_not_states.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	svc := &MapboxService{
		client: server.Client(),
	}

	result, err := svc.searchStateBoundaryWithURL(
		context.Background(),
		server.URL+"/search?format=jsonv2&q=State+of+Washington,+USA&polygon_geojson=1&limit=10&countrycodes=us&addressdetails=1",
		"Washington",
		47.6, -122.3,
	)

	// Should return error because no valid state was found
	if err == nil {
		t.Error("Expected error when no state found, got nil")
	}

	if result != nil {
		t.Errorf("Expected nil result, got %+v", result)
	}

	if !strings.Contains(err.Error(), "no valid state boundary") {
		t.Errorf("Expected 'no valid state boundary' error, got: %v", err)
	}
}

// TestGetStateBoundary_SkipsCityAddressType tests that results with addresstype="city" are skipped
func TestGetStateBoundary_SkipsCityAddressType(t *testing.T) {
	// Fixture with only a city result (Washington DC)
	fixture := []byte(`[
		{
			"place_id": 394370670,
			"display_name": "Washington, District of Columbia, United States",
			"name": "Washington",
			"type": "city",
			"category": "place",
			"addresstype": "city",
			"lat": "38.8950368",
			"lon": "-77.0365427",
			"address": {"city": "Washington", "state": "District of Columbia"},
			"geojson": {"type": "Polygon", "coordinates": [[[-77.1,38.8],[-77.1,38.9],[-77.0,38.9],[-77.0,38.8],[-77.1,38.8]]]}
		}
	]`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	svc := &MapboxService{
		client: server.Client(),
	}

	result, err := svc.searchStateBoundaryWithURL(
		context.Background(),
		server.URL+"/search",
		"Washington",
		47.6, -122.3,
	)

	// Should return error because city should be skipped
	if err == nil {
		t.Error("Expected error when only city found, got nil")
	}

	if result != nil {
		t.Errorf("Expected nil result, got %+v", result)
	}
}

// TestGetCountyBoundary tests county boundary lookup
func TestGetCountyBoundary(t *testing.T) {
	fixture := loadFixture(t, "county_kings_ny_simplified.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify county is in query
		query := r.URL.Query().Get("q")
		if !strings.Contains(strings.ToLower(query), "kings") {
			t.Errorf("Expected query to contain 'kings', got %s", query)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	svc := &MapboxService{
		client: server.Client(),
	}

	// Use the internal method (lowercase) for testing
	result, err := svc.getCountyBoundaryWithURL(
		context.Background(),
		server.URL+"/search?format=jsonv2&q=Kings+County,+New+York,+USA&polygon_geojson=1&limit=5&countrycodes=us&addressdetails=1",
		"Kings County",
		"New York",
		40.65, -73.95,
	)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	if result.Name != "Kings County" {
		t.Errorf("Expected name 'Kings County', got '%s'", result.Name)
	}

	if result.GeoJSON == "" {
		t.Error("Expected GeoJSON to be present")
	}
}

// TestGetCityBoundary tests city boundary lookup via search
func TestGetCityBoundary_Seattle(t *testing.T) {
	fixture := loadFixture(t, "city_seattle_simplified.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	svc := &MapboxService{
		client: server.Client(),
	}

	result, err := svc.fetchCityBoundaryByNameWithURL(
		context.Background(),
		server.URL+"/search?format=jsonv2&q=Seattle,+Washington,+USA&polygon_geojson=1&limit=5&countrycodes=us&addressdetails=1",
		"Seattle",
		"Washington",
		47.6, -122.3,
	)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	if result.Name != "Seattle" {
		t.Errorf("Expected name 'Seattle', got '%s'", result.Name)
	}

	if result.Type != "city" {
		t.Errorf("Expected type 'city', got '%s'", result.Type)
	}
}

// TestGetLocalityBoundary tests locality (borough/suburb) boundary lookup
func TestGetLocalityBoundary_Brooklyn(t *testing.T) {
	fixture := loadFixture(t, "locality_brooklyn_simplified.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	svc := &MapboxService{
		client: server.Client(),
	}

	result, err := svc.getLocalityBoundaryWithURL(
		context.Background(),
		server.URL+"/search?format=jsonv2&q=Brooklyn,+New+York,+New+York,+USA&polygon_geojson=1&limit=5&countrycodes=us&addressdetails=1",
		"Brooklyn",
		"New York",
		"New York",
		40.65, -73.95,
	)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	if result.Name != "Brooklyn" {
		t.Errorf("Expected name 'Brooklyn', got '%s'", result.Name)
	}
}

// TestOSMSearchResultParsing tests that OSM results are parsed correctly
func TestOSMSearchResultParsing(t *testing.T) {
	tests := []struct {
		name          string
		fixture       string
		expectCount   int
		expectFirst   string
		expectType    string
		expectAddrTyp string
	}{
		{
			name:          "Washington with mixed results",
			fixture:       "state_washington_simplified.json",
			expectCount:   3,
			expectFirst:   "Washington",
			expectType:    "city",
			expectAddrTyp: "city",
		},
		{
			name:          "California state only",
			fixture:       "state_california_simplified.json",
			expectCount:   1,
			expectFirst:   "California",
			expectType:    "administrative",
			expectAddrTyp: "state",
		},
		{
			name:          "Seattle city",
			fixture:       "city_seattle_simplified.json",
			expectCount:   1,
			expectFirst:   "Seattle",
			expectType:    "city",
			expectAddrTyp: "city",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := loadFixture(t, tc.fixture)

			var results []struct {
				PlaceID     int64  `json:"place_id"`
				Name        string `json:"name"`
				Type        string `json:"type"`
				AddressType string `json:"addresstype"`
			}

			if err := json.Unmarshal(data, &results); err != nil {
				t.Fatalf("Failed to parse fixture: %v", err)
			}

			if len(results) != tc.expectCount {
				t.Errorf("Expected %d results, got %d", tc.expectCount, len(results))
			}

			if len(results) > 0 {
				if results[0].Name != tc.expectFirst {
					t.Errorf("Expected first name '%s', got '%s'", tc.expectFirst, results[0].Name)
				}
				if results[0].Type != tc.expectType {
					t.Errorf("Expected first type '%s', got '%s'", tc.expectType, results[0].Type)
				}
				if results[0].AddressType != tc.expectAddrTyp {
					t.Errorf("Expected first addresstype '%s', got '%s'", tc.expectAddrTyp, results[0].AddressType)
				}
			}
		})
	}
}

// TestRealOSMFixtures tests against the real (large) fixtures if available
func TestRealOSMFixtures(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real fixture tests in short mode")
	}

	// Test real Washington state fixture
	path := filepath.Join(getTestDataDir(), "state_washington.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("Real fixture state_washington.json not found")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	var results []struct {
		PlaceID     int64           `json:"place_id"`
		Name        string          `json:"name"`
		Type        string          `json:"type"`
		AddressType string          `json:"addresstype"`
		GeoJSON     json.RawMessage `json:"geojson"`
	}

	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("Failed to parse fixture: %v", err)
	}

	// Verify Washington state is in results
	foundState := false
	for _, r := range results {
		if r.AddressType == "state" && r.Name == "Washington" {
			foundState = true

			// Verify GeoJSON is valid
			var geo struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(r.GeoJSON, &geo); err != nil {
				t.Errorf("Failed to parse state GeoJSON: %v", err)
			}
			if geo.Type != "Polygon" && geo.Type != "MultiPolygon" {
				t.Errorf("Expected Polygon or MultiPolygon for state, got %s", geo.Type)
			}

			break
		}
	}

	if !foundState {
		t.Error("Washington state not found in real fixture")
	}
}
