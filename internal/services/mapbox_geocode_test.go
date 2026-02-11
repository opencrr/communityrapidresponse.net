package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// getMapboxTestDataDir returns the path to the mapbox testdata directory
func getMapboxTestDataDir() string {
	return filepath.Join(getTestDataDir(), "..", "mapbox")
}

// loadMapboxFixture loads a Mapbox JSON fixture file
func loadMapboxFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(getMapboxTestDataDir(), name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to load fixture %s: %v", name, err)
	}
	return data
}

// TestGeocodeAddress_SanFrancisco tests geocoding a San Francisco address
func TestGeocodeAddress_SanFrancisco(t *testing.T) {
	fixture := loadMapboxFixture(t, "geocode_sf_address.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request path
		if !strings.Contains(r.URL.Path, "/geocoding/v5/mapbox.places/") {
			t.Errorf("Expected geocoding path, got %s", r.URL.Path)
		}

		// Verify query params
		if r.URL.Query().Get("country") != "US" {
			t.Errorf("Expected country=US, got %s", r.URL.Query().Get("country"))
		}
		if r.URL.Query().Get("types") != "address" {
			t.Errorf("Expected types=address, got %s", r.URL.Query().Get("types"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	svc := &MapboxService{
		secretToken: "test_token",
		client:      server.Client(),
	}

	address := &models.Address{
		Line1:      "100 Market Street",
		City:       "San Francisco",
		State:      "CA",
		PostalCode: "94105",
	}

	result, err := svc.geocodeAddressWithURL(
		context.Background(),
		server.URL+"/geocoding/v5/mapbox.places/100%20Market%20Street%2C%20San%20Francisco%2C%20CA%2094105.json?access_token=test&country=US&types=address",
		address,
	)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	// Verify coordinates
	if result.Latitude < 37.7 || result.Latitude > 37.8 {
		t.Errorf("Expected latitude around 37.79, got %f", result.Latitude)
	}
	if result.Longitude < -122.5 || result.Longitude > -122.3 {
		t.Errorf("Expected longitude around -122.39, got %f", result.Longitude)
	}

	// Verify state
	if result.BoundaryState != "California" {
		t.Errorf("Expected state 'California', got '%s'", result.BoundaryState)
	}

	// Verify county
	if result.CountyName != "San Francisco County" {
		t.Errorf("Expected county 'San Francisco County', got '%s'", result.CountyName)
	}

	// Verify city (boundary name)
	if result.BoundaryName != "San Francisco" {
		t.Errorf("Expected boundary name 'San Francisco', got '%s'", result.BoundaryName)
	}

	// Verify neighborhood
	if result.NeighborhoodName != "Financial District" {
		t.Errorf("Expected neighborhood 'Financial District', got '%s'", result.NeighborhoodName)
	}
}

// TestGeocodeAddress_Brooklyn tests geocoding a Brooklyn address with locality
func TestGeocodeAddress_Brooklyn(t *testing.T) {
	fixture := loadMapboxFixture(t, "geocode_brooklyn_address.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	svc := &MapboxService{
		secretToken: "test_token",
		client:      server.Client(),
	}

	address := &models.Address{
		Line1:      "123 Bedford Avenue",
		City:       "Brooklyn",
		State:      "NY",
		PostalCode: "11211",
	}

	result, err := svc.geocodeAddressWithURL(
		context.Background(),
		server.URL+"/geocoding/v5/mapbox.places/test.json?access_token=test&country=US&types=address",
		address,
	)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	// Verify state
	if result.BoundaryState != "New York" {
		t.Errorf("Expected state 'New York', got '%s'", result.BoundaryState)
	}

	// Verify county (Kings County is Brooklyn)
	if result.CountyName != "Kings County" {
		t.Errorf("Expected county 'Kings County', got '%s'", result.CountyName)
	}

	// Verify city
	if result.BoundaryName != "New York" {
		t.Errorf("Expected boundary name 'New York', got '%s'", result.BoundaryName)
	}

	// Verify locality (Brooklyn)
	if result.LocalityName != "Brooklyn" {
		t.Errorf("Expected locality 'Brooklyn', got '%s'", result.LocalityName)
	}

	// Verify neighborhood
	if result.NeighborhoodName != "Williamsburg" {
		t.Errorf("Expected neighborhood 'Williamsburg', got '%s'", result.NeighborhoodName)
	}
}

// TestGeocodeAddress_Seattle tests geocoding a Seattle address
func TestGeocodeAddress_Seattle(t *testing.T) {
	fixture := loadMapboxFixture(t, "geocode_seattle_address.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	svc := &MapboxService{
		secretToken: "test_token",
		client:      server.Client(),
	}

	address := &models.Address{
		Line1:      "100 Pike Street",
		City:       "Seattle",
		State:      "WA",
		PostalCode: "98101",
	}

	result, err := svc.geocodeAddressWithURL(
		context.Background(),
		server.URL+"/geocoding/v5/mapbox.places/test.json?access_token=test&country=US&types=address",
		address,
	)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	// Verify state
	if result.BoundaryState != "Washington" {
		t.Errorf("Expected state 'Washington', got '%s'", result.BoundaryState)
	}

	// Verify county
	if result.CountyName != "King County" {
		t.Errorf("Expected county 'King County', got '%s'", result.CountyName)
	}

	// Verify city
	if result.BoundaryName != "Seattle" {
		t.Errorf("Expected boundary name 'Seattle', got '%s'", result.BoundaryName)
	}

	// Verify neighborhood
	if result.NeighborhoodName != "Downtown" {
		t.Errorf("Expected neighborhood 'Downtown', got '%s'", result.NeighborhoodName)
	}
}

// TestGeocodeAddress_Rural tests geocoding a rural address without city
func TestGeocodeAddress_Rural(t *testing.T) {
	fixture := loadMapboxFixture(t, "geocode_rural_address.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	svc := &MapboxService{
		secretToken: "test_token",
		client:      server.Client(),
	}

	address := &models.Address{
		Line1:      "1000 Highway 101",
		City:       "",
		State:      "CA",
		PostalCode: "95521",
	}

	result, err := svc.geocodeAddressWithURL(
		context.Background(),
		server.URL+"/geocoding/v5/mapbox.places/test.json?access_token=test&country=US&types=address",
		address,
	)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	// Verify state
	if result.BoundaryState != "California" {
		t.Errorf("Expected state 'California', got '%s'", result.BoundaryState)
	}

	// Verify county (no city, so county is main boundary)
	if result.CountyName != "Humboldt County" {
		t.Errorf("Expected county 'Humboldt County', got '%s'", result.CountyName)
	}

	// Rural addresses may not have a city - boundary type should be county
	if result.BoundaryType != "county" {
		t.Errorf("Expected boundary type 'county', got '%s'", result.BoundaryType)
	}
}

// TestGeocodeAddress_NoResults tests handling of no geocoding results
func TestGeocodeAddress_NoResults(t *testing.T) {
	fixture := loadMapboxFixture(t, "geocode_no_results.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	svc := &MapboxService{
		secretToken: "test_token",
		client:      server.Client(),
	}

	address := &models.Address{
		Line1:      "Invalid Address",
		City:       "Nowhere",
		State:      "XX",
		PostalCode: "00000",
	}

	result, err := svc.geocodeAddressWithURL(
		context.Background(),
		server.URL+"/geocoding/v5/mapbox.places/test.json?access_token=test&country=US&types=address",
		address,
	)

	// Should return an error for no results
	if err == nil {
		t.Error("Expected error for no results, got nil")
	}

	if result != nil {
		t.Errorf("Expected nil result, got %+v", result)
	}

	if !strings.Contains(err.Error(), "no results") {
		t.Errorf("Expected 'no results' error, got: %v", err)
	}
}

// TestGeocodeAddress_ContextExtraction tests that context fields are extracted correctly
func TestGeocodeAddress_ContextExtraction(t *testing.T) {
	tests := []struct {
		name           string
		fixture        string
		expectState    string
		expectCounty   string
		expectCity     string
		expectLocality string
		expectNeighbor string
	}{
		{
			name:           "San Francisco - no locality",
			fixture:        "geocode_sf_address.json",
			expectState:    "California",
			expectCounty:   "San Francisco County",
			expectCity:     "San Francisco",
			expectLocality: "",
			expectNeighbor: "Financial District",
		},
		{
			name:           "Brooklyn - has locality",
			fixture:        "geocode_brooklyn_address.json",
			expectState:    "New York",
			expectCounty:   "Kings County",
			expectCity:     "New York",
			expectLocality: "Brooklyn",
			expectNeighbor: "Williamsburg",
		},
		{
			name:           "Seattle - no locality",
			fixture:        "geocode_seattle_address.json",
			expectState:    "Washington",
			expectCounty:   "King County",
			expectCity:     "Seattle",
			expectLocality: "",
			expectNeighbor: "Downtown",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := loadMapboxFixture(t, tc.fixture)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(fixture)
			}))
			defer server.Close()

			svc := &MapboxService{
				secretToken: "test_token",
				client:      server.Client(),
			}

			result, err := svc.geocodeAddressWithURL(
				context.Background(),
				server.URL+"/geocoding/v5/mapbox.places/test.json?access_token=test&country=US&types=address",
				&models.Address{Line1: "Test", City: "Test", State: "TS", PostalCode: "12345"},
			)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.BoundaryState != tc.expectState {
				t.Errorf("Expected state '%s', got '%s'", tc.expectState, result.BoundaryState)
			}
			if result.CountyName != tc.expectCounty {
				t.Errorf("Expected county '%s', got '%s'", tc.expectCounty, result.CountyName)
			}
			if result.BoundaryName != tc.expectCity {
				t.Errorf("Expected city '%s', got '%s'", tc.expectCity, result.BoundaryName)
			}
			if result.LocalityName != tc.expectLocality {
				t.Errorf("Expected locality '%s', got '%s'", tc.expectLocality, result.LocalityName)
			}
			if result.NeighborhoodName != tc.expectNeighbor {
				t.Errorf("Expected neighborhood '%s', got '%s'", tc.expectNeighbor, result.NeighborhoodName)
			}
		})
	}
}
