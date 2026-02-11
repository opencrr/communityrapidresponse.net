// Package testdata provides test fixtures for OSM and Mapbox API responses
package testdata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// OSMSearchResult represents a single result from OSM Nominatim search
type OSMSearchResult struct {
	PlaceID     int64           `json:"place_id"`
	DisplayName string          `json:"display_name"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Category    string          `json:"category"`
	AddressType string          `json:"addresstype"`
	Lat         string          `json:"lat"`
	Lon         string          `json:"lon"`
	Address     json.RawMessage `json:"address"`
	GeoJSON     json.RawMessage `json:"geojson"`
}

// OSMReverseResult represents a result from OSM Nominatim reverse geocoding
type OSMReverseResult struct {
	PlaceID     int64           `json:"place_id"`
	DisplayName string          `json:"display_name"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	AddressType string          `json:"addresstype"`
	Address     json.RawMessage `json:"address"`
	GeoJSON     json.RawMessage `json:"geojson"`
}

// GetTestDataDir returns the path to the testdata directory
func GetTestDataDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

// LoadOSMSearchFixture loads an OSM search fixture from file
func LoadOSMSearchFixture(name string) ([]OSMSearchResult, error) {
	path := filepath.Join(GetTestDataDir(), "osm", name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read fixture %s: %w", name, err)
	}

	var results []OSMSearchResult
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, fmt.Errorf("failed to parse fixture %s: %w", name, err)
	}

	return results, nil
}

// LoadOSMReverseFixture loads an OSM reverse geocoding fixture from file
func LoadOSMReverseFixture(name string) (*OSMReverseResult, error) {
	path := filepath.Join(GetTestDataDir(), "osm", name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read fixture %s: %w", name, err)
	}

	var result OSMReverseResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse fixture %s: %w", name, err)
	}

	return &result, nil
}

// SimplePolygonGeoJSON returns a simple polygon GeoJSON for testing
func SimplePolygonGeoJSON() string {
	return `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.5,37.8],[-122.4,37.8],[-122.4,37.7],[-122.5,37.7]]]}`
}

// SimpleMultiPolygonGeoJSON returns a simple multipolygon GeoJSON for testing
func SimpleMultiPolygonGeoJSON() string {
	return `{"type":"MultiPolygon","coordinates":[[[[-122.5,37.7],[-122.5,37.8],[-122.4,37.8],[-122.4,37.7],[-122.5,37.7]]]]}`
}

// PointGeoJSON returns a point GeoJSON (should be rejected)
func PointGeoJSON() string {
	return `{"type":"Point","coordinates":[-122.4,37.7]}`
}
