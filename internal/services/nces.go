package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
)

const ncesDefaultBaseURL = "https://nces.ed.gov/opengis/rest/services/K12_School_Locations/EDGE_GEOCODE_PUBLICSCH_2324/MapServer/0/query"

// ncesSchoolOutFields is the set of fields requested from the school ArcGIS API
const ncesSchoolOutFields = "NCESSCH,NAME,LEAID,STREET,CITY,STATE,ZIP,LAT,LON"

// ncesDistrictOutFields is the set of fields requested from the district (LEA) ArcGIS API
const ncesDistrictOutFields = "LEAID,NAME,STATE"

// ncesDistrictBaseURL is the ArcGIS endpoint for school district (LEA) data
const ncesDistrictBaseURL = "https://nces.ed.gov/opengis/rest/services/K12_School_Locations/EDGE_GEOCODE_PUBLICLEA_2324/MapServer/0/query"

// ncesDistrictBoundaryBaseURL is the ArcGIS endpoint for school district boundary polygons
const ncesDistrictBoundaryBaseURL = "https://nces.ed.gov/opengis/rest/services/School_District_Boundaries/EDGE_SCHOOLDISTRICT_TL24_SY2324/MapServer/0/query"

// NCESSchool represents a school record from the NCES ArcGIS API
type NCESSchool struct {
	NCESSCH string  `json:"NCESSCH"`
	Name    string  `json:"NAME"`
	LEAID   string  `json:"LEAID"`
	Street  string  `json:"STREET"`
	City    string  `json:"CITY"`
	State   string  `json:"STATE"`
	Zip     string  `json:"ZIP"`
	Lat     float64 `json:"LAT"`
	Lon     float64 `json:"LON"`
}

// NCESDistrict represents a district (LEA) record from the NCES ArcGIS API
type NCESDistrict struct {
	LEAID string `json:"LEAID"`
	Name  string `json:"NAME"`
	State string `json:"STATE"`
}

// NCESServiceInterface defines the interface for NCES API operations
type NCESServiceInterface interface {
	// FetchSchools fetches a page of schools from the NCES ArcGIS API.
	// Returns the schools, total record count, and any error.
	FetchSchools(ctx context.Context, offset, limit int) ([]NCESSchool, int, error)

	// FetchSchoolsByState fetches a page of schools filtered by state abbreviation.
	// Returns the schools, total record count, and any error.
	FetchSchoolsByState(ctx context.Context, state string, offset, limit int) ([]NCESSchool, int, error)

	// FetchDistricts fetches a page of districts from the NCES ArcGIS LEA API.
	// Returns the districts, total record count, and any error.
	FetchDistricts(ctx context.Context, offset, limit int) ([]NCESDistrict, int, error)

	// FetchDistrictsByState fetches a page of districts filtered by state abbreviation.
	FetchDistrictsByState(ctx context.Context, state string, offset, limit int) ([]NCESDistrict, int, error)

	// FetchDistrictBoundary fetches a single district's boundary polygon by GEOID (LEAID/nces_id).
	// Returns the GeoJSON geometry string, or empty string if no boundary found.
	FetchDistrictBoundary(ctx context.Context, geoid string) (string, error)
}

// NCESService handles NCES ArcGIS API interactions
type NCESService struct {
	client  *http.Client
	baseURL string
}

// Compile-time assertion that NCESService implements NCESServiceInterface
var _ NCESServiceInterface = (*NCESService)(nil)

// NewNCESService creates a new NCES service with the default ArcGIS base URL
func NewNCESService() *NCESService {
	return &NCESService{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: ncesDefaultBaseURL,
	}
}

// arcGISCountResponse represents the JSON response from ArcGIS count queries
type arcGISCountResponse struct {
	Count int `json:"count"`
}

// FetchSchools fetches a page of schools from the NCES ArcGIS API.
func (s *NCESService) FetchSchools(ctx context.Context, offset, limit int) ([]NCESSchool, int, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "GET nces /schools"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	return s.fetchSchools(ctx, "1=1", offset, limit)
}

// FetchSchoolsByState fetches a page of schools filtered by state abbreviation.
func (s *NCESService) FetchSchoolsByState(ctx context.Context, state string, offset, limit int) ([]NCESSchool, int, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "GET nces /schools-by-state"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	whereClause := fmt.Sprintf("STATE='%s'", state)
	return s.fetchSchools(ctx, whereClause, offset, limit)
}

// FetchDistricts fetches a page of districts from the NCES ArcGIS LEA API.
func (s *NCESService) FetchDistricts(ctx context.Context, offset, limit int) ([]NCESDistrict, int, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "GET nces /districts"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	return s.fetchDistricts(ctx, "1=1", offset, limit)
}

// FetchDistrictsByState fetches a page of districts filtered by state abbreviation.
func (s *NCESService) FetchDistrictsByState(ctx context.Context, state string, offset, limit int) ([]NCESDistrict, int, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "GET nces /districts-by-state"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	whereClause := fmt.Sprintf("STATE='%s'", state)
	return s.fetchDistricts(ctx, whereClause, offset, limit)
}

// fetchSchools is the internal method that handles both all-school and state-filtered queries.
func (s *NCESService) fetchSchools(ctx context.Context, whereClause string, offset, limit int) ([]NCESSchool, int, error) {
	totalCount, err := s.fetchCount(ctx, s.baseURL, whereClause)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch school count: %w", err)
	}

	schools, err := s.fetchPage(ctx, s.baseURL, whereClause, ncesSchoolOutFields, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch schools: %w", err)
	}

	results := make([]NCESSchool, len(schools))
	for i, raw := range schools {
		if err := json.Unmarshal(raw, &results[i]); err != nil {
			return nil, 0, fmt.Errorf("failed to parse school record: %w", err)
		}
	}

	return results, totalCount, nil
}

// fetchDistricts is the internal method for district queries.
func (s *NCESService) fetchDistricts(ctx context.Context, whereClause string, offset, limit int) ([]NCESDistrict, int, error) {
	totalCount, err := s.fetchCount(ctx, ncesDistrictBaseURL, whereClause)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch district count: %w", err)
	}

	records, err := s.fetchPage(ctx, ncesDistrictBaseURL, whereClause, ncesDistrictOutFields, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch districts: %w", err)
	}

	results := make([]NCESDistrict, len(records))
	for i, raw := range records {
		if err := json.Unmarshal(raw, &results[i]); err != nil {
			return nil, 0, fmt.Errorf("failed to parse district record: %w", err)
		}
	}

	return results, totalCount, nil
}

// fetchCount queries an ArcGIS endpoint for the total number of matching records.
func (s *NCESService) fetchCount(ctx context.Context, endpoint, whereClause string) (int, error) {
	params := url.Values{}
	params.Set("where", whereClause)
	params.Set("returnCountOnly", "true")
	params.Set("f", "json")

	requestURL := endpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create count request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send count request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read count response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("count API error (status %d): %s", resp.StatusCode, string(body))
	}

	var countResp arcGISCountResponse
	if err := json.Unmarshal(body, &countResp); err != nil {
		return 0, fmt.Errorf("failed to parse count response: %w", err)
	}

	return countResp.Count, nil
}

// FetchDistrictBoundary fetches a single district's boundary polygon from the NCES boundary service.
func (s *NCESService) FetchDistrictBoundary(ctx context.Context, geoid string) (string, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "GET nces /district-boundary"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	params := url.Values{}
	params.Set("where", fmt.Sprintf("GEOID='%s'", geoid))
	params.Set("returnGeometry", "true")
	params.Set("outSR", "4326")
	params.Set("f", "geojson")

	requestURL := ncesDistrictBoundaryBaseURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create boundary request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send boundary request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read boundary response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("boundary API error (status %d): %s", resp.StatusCode, string(body))
	}

	var geoJSONResp struct {
		Features []struct {
			Geometry json.RawMessage `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(body, &geoJSONResp); err != nil {
		return "", fmt.Errorf("failed to parse boundary response: %w", err)
	}

	if len(geoJSONResp.Features) == 0 || geoJSONResp.Features[0].Geometry == nil {
		return "", nil
	}

	return string(geoJSONResp.Features[0].Geometry), nil
}

// fetchPage queries an ArcGIS endpoint for a page of records, returning raw JSON attributes.
func (s *NCESService) fetchPage(ctx context.Context, endpoint, whereClause, outFields string, offset, limit int) ([]json.RawMessage, error) {
	params := url.Values{}
	params.Set("where", whereClause)
	params.Set("outFields", outFields)
	params.Set("f", "json")
	params.Set("resultOffset", fmt.Sprintf("%d", offset))
	params.Set("resultRecordCount", fmt.Sprintf("%d", limit))

	requestURL := endpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
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
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var featureResp struct {
		Features []struct {
			Attributes json.RawMessage `json:"attributes"`
		} `json:"features"`
	}
	if err := json.Unmarshal(body, &featureResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	results := make([]json.RawMessage, len(featureResp.Features))
	for i, feature := range featureResp.Features {
		results[i] = feature.Attributes
	}

	return results, nil
}
