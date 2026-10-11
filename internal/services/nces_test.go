package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newTestNCESService returns an NCESService whose three ArcGIS endpoints are
// pointed at the given httptest server. The server's handler is responsible
// for routing on path or query params.
func newTestNCESService(server *httptest.Server) *NCESService {
	return &NCESService{
		client:              server.Client(),
		baseURL:             server.URL + "/schools",
		districtBaseURL:     server.URL + "/districts",
		districtBoundaryURL: server.URL + "/boundaries",
	}
}

// ncesFakeResponse describes a single canned response keyed by the request's
// `returnCountOnly` flag, so a fake server can answer both the count query
// and the page query that every fetch issues.
type ncesFakeResponse struct {
	count int
	page  string
}

// arcGISHandler returns a handler that serves a count payload when
// returnCountOnly=true and the supplied features payload otherwise.
func arcGISHandler(t *testing.T, resp ncesFakeResponse) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("returnCountOnly") == "true" {
			_, _ = w.Write([]byte(`{"count":` + itoa(resp.count) + `}`))
			return
		}
		_, _ = w.Write([]byte(resp.page))
	}
}

// itoa avoids pulling in strconv just for one digit-string conversion in the
// fake response helper above.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestNCESService_FetchSchools(t *testing.T) {
	page := `{"features":[
		{"attributes":{"NCESSCH":"001","NAME":"Alpha High","LEAID":"L1","STREET":"1 A St","CITY":"Town","STATE":"CA","ZIP":"94000","LAT":37.5,"LON":-122.5}},
		{"attributes":{"NCESSCH":"002","NAME":"Beta High","LEAID":"L1","STREET":"2 B St","CITY":"Town","STATE":"CA","ZIP":"94000","LAT":37.6,"LON":-122.6}}
	]}`

	var lastQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastQuery = r.URL.Query()
		arcGISHandler(t, ncesFakeResponse{count: 42, page: page})(w, r)
	}))
	defer server.Close()

	svc := newTestNCESService(server)
	schools, total, err := svc.FetchSchools(context.Background(), 100, 50)
	if err != nil {
		t.Fatalf("FetchSchools: unexpected error: %v", err)
	}
	if total != 42 {
		t.Errorf("total = %d, want 42", total)
	}
	if len(schools) != 2 {
		t.Fatalf("got %d schools, want 2", len(schools))
	}
	if schools[0].NCESSCH != "001" || schools[0].Name != "Alpha High" {
		t.Errorf("school[0] = %+v, want NCESSCH=001 Name=Alpha High", schools[0])
	}
	if schools[1].Lat != 37.6 {
		t.Errorf("school[1].Lat = %v, want 37.6", schools[1].Lat)
	}

	// The final (page) request must carry pagination + outFields + a base where clause.
	if got := lastQuery.Get("resultOffset"); got != "100" {
		t.Errorf("resultOffset = %q, want 100", got)
	}
	if got := lastQuery.Get("resultRecordCount"); got != "50" {
		t.Errorf("resultRecordCount = %q, want 50", got)
	}
	if got := lastQuery.Get("outFields"); got != ncesSchoolOutFields {
		t.Errorf("outFields = %q, want %q", got, ncesSchoolOutFields)
	}
	if got := lastQuery.Get("where"); got != "1=1" {
		t.Errorf("where = %q, want 1=1", got)
	}
}

func TestNCESService_FetchSchoolsByState_WhereClause(t *testing.T) {
	page := `{"features":[{"attributes":{"NCESSCH":"010","NAME":"Gamma","LEAID":"L2","STREET":"","CITY":"","STATE":"CA","ZIP":"","LAT":0,"LON":0}}]}`

	var pageWhere string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("returnCountOnly") != "true" {
			pageWhere = r.URL.Query().Get("where")
		}
		arcGISHandler(t, ncesFakeResponse{count: 1, page: page})(w, r)
	}))
	defer server.Close()

	svc := newTestNCESService(server)
	schools, total, err := svc.FetchSchoolsByState(context.Background(), "CA", 0, 10)
	if err != nil {
		t.Fatalf("FetchSchoolsByState: unexpected error: %v", err)
	}
	if total != 1 || len(schools) != 1 {
		t.Fatalf("got total=%d schools=%d, want 1/1", total, len(schools))
	}
	if pageWhere != "STATE='CA'" {
		t.Errorf("page where = %q, want STATE='CA'", pageWhere)
	}
}

func TestNCESService_FetchDistricts(t *testing.T) {
	page := `{"features":[
		{"attributes":{"LEAID":"L1","NAME":"Unified","STATE":"CA"}},
		{"attributes":{"LEAID":"L2","NAME":"Elementary","STATE":"NY"}}
	]}`

	var hitDistrictsEndpoint bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/districts") {
			hitDistrictsEndpoint = true
		}
		arcGISHandler(t, ncesFakeResponse{count: 2, page: page})(w, r)
	}))
	defer server.Close()

	svc := newTestNCESService(server)
	districts, total, err := svc.FetchDistricts(context.Background(), 0, 25)
	if err != nil {
		t.Fatalf("FetchDistricts: unexpected error: %v", err)
	}
	if !hitDistrictsEndpoint {
		t.Errorf("expected request against /districts endpoint, got none")
	}
	if total != 2 || len(districts) != 2 {
		t.Fatalf("got total=%d districts=%d, want 2/2", total, len(districts))
	}
	if districts[0].LEAID != "L1" || districts[0].Name != "Unified" {
		t.Errorf("districts[0] = %+v, want LEAID=L1 Name=Unified", districts[0])
	}
}

func TestNCESService_FetchDistrictsByState(t *testing.T) {
	page := `{"features":[{"attributes":{"LEAID":"L9","NAME":"Solo","STATE":"WA"}}]}`

	var pageWhere string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("returnCountOnly") != "true" {
			pageWhere = r.URL.Query().Get("where")
		}
		arcGISHandler(t, ncesFakeResponse{count: 1, page: page})(w, r)
	}))
	defer server.Close()

	svc := newTestNCESService(server)
	districts, _, err := svc.FetchDistrictsByState(context.Background(), "WA", 0, 10)
	if err != nil {
		t.Fatalf("FetchDistrictsByState: unexpected error: %v", err)
	}
	if len(districts) != 1 || districts[0].State != "WA" {
		t.Errorf("districts = %+v, want one WA district", districts)
	}
	if pageWhere != "STATE='WA'" {
		t.Errorf("page where = %q, want STATE='WA'", pageWhere)
	}
}

func TestNCESService_FetchDistrictBoundary(t *testing.T) {
	geom := `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]]]}`
	body := `{"features":[{"geometry":` + geom + `}]}`

	var requestedWhere string
	var requestedF string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedWhere = r.URL.Query().Get("where")
		requestedF = r.URL.Query().Get("f")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	svc := newTestNCESService(server)
	geo, err := svc.FetchDistrictBoundary(context.Background(), "0612345")
	if err != nil {
		t.Fatalf("FetchDistrictBoundary: unexpected error: %v", err)
	}
	if requestedWhere != "GEOID='0612345'" {
		t.Errorf("where = %q, want GEOID='0612345'", requestedWhere)
	}
	if requestedF != "geojson" {
		t.Errorf("f = %q, want geojson", requestedF)
	}

	// The returned geometry should round-trip back to the same JSON object.
	var got, want map[string]any
	if err := json.Unmarshal([]byte(geo), &got); err != nil {
		t.Fatalf("returned geometry is not JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(geom), &want); err != nil {
		t.Fatalf("expected geometry is not JSON: %v", err)
	}
	if !equalJSON(got, want) {
		t.Errorf("geometry mismatch:\n got=%v\nwant=%v", got, want)
	}
}

func TestNCESService_FetchDistrictBoundary_NoFeatures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"features":[]}`))
	}))
	defer server.Close()

	svc := newTestNCESService(server)
	geo, err := svc.FetchDistrictBoundary(context.Background(), "9999999")
	if err != nil {
		t.Fatalf("FetchDistrictBoundary: unexpected error: %v", err)
	}
	if geo != "" {
		t.Errorf("expected empty geometry for no-features response, got %q", geo)
	}
}

func TestNCESService_FetchSchools_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`upstream broken`))
	}))
	defer server.Close()

	svc := newTestNCESService(server)
	_, _, err := svc.FetchSchools(context.Background(), 0, 10)
	if err == nil {
		t.Fatal("expected error from 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "count API error") {
		t.Errorf("error = %q, want it to mention count API error", err.Error())
	}
}

func TestNCESService_FetchSchools_MalformedCountJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("returnCountOnly") == "true" {
			_, _ = w.Write([]byte(`not-json`))
			return
		}
		_, _ = w.Write([]byte(`{"features":[]}`))
	}))
	defer server.Close()

	svc := newTestNCESService(server)
	_, _, err := svc.FetchSchools(context.Background(), 0, 10)
	if err == nil {
		t.Fatal("expected JSON parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parse count response") {
		t.Errorf("error = %q, want it to mention parse count response", err.Error())
	}
}

func TestNCESService_FetchSchools_MalformedPageJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("returnCountOnly") == "true" {
			_, _ = w.Write([]byte(`{"count":1}`))
			return
		}
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer server.Close()

	svc := newTestNCESService(server)
	_, _, err := svc.FetchSchools(context.Background(), 0, 10)
	if err == nil {
		t.Fatal("expected JSON parse error on page, got nil")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("error = %q, want it to mention parse response", err.Error())
	}
}

func TestNCESService_FetchSchools_BadRecord(t *testing.T) {
	// Page parses successfully but the per-record JSON has a type mismatch
	// (LAT is a string instead of a number), which should bubble up as a
	// "failed to parse school record" error.
	page := `{"features":[{"attributes":{"NCESSCH":"001","NAME":"Bad","LEAID":"","STREET":"","CITY":"","STATE":"","ZIP":"","LAT":"not-a-number","LON":0}}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arcGISHandler(t, ncesFakeResponse{count: 1, page: page})(w, r)
	}))
	defer server.Close()

	svc := newTestNCESService(server)
	_, _, err := svc.FetchSchools(context.Background(), 0, 10)
	if err == nil {
		t.Fatal("expected per-record parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parse school record") {
		t.Errorf("error = %q, want it to mention parse school record", err.Error())
	}
}

func TestNCESService_FetchDistrictBoundary_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`gateway down`))
	}))
	defer server.Close()

	svc := newTestNCESService(server)
	_, err := svc.FetchDistrictBoundary(context.Background(), "0612345")
	if err == nil {
		t.Fatal("expected error from 502 response, got nil")
	}
	if !strings.Contains(err.Error(), "boundary API error") {
		t.Errorf("error = %q, want it to mention boundary API error", err.Error())
	}
}

func TestNewNCESService_DefaultsToProductionURLs(t *testing.T) {
	svc := NewNCESService()
	if svc.baseURL != ncesDefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", svc.baseURL, ncesDefaultBaseURL)
	}
	if svc.districtBaseURL != ncesDistrictBaseURL {
		t.Errorf("districtBaseURL = %q, want %q", svc.districtBaseURL, ncesDistrictBaseURL)
	}
	if svc.districtBoundaryURL != ncesDistrictBoundaryBaseURL {
		t.Errorf("districtBoundaryURL = %q, want %q", svc.districtBoundaryURL, ncesDistrictBoundaryBaseURL)
	}
	if svc.client == nil || svc.client.Timeout == 0 {
		t.Errorf("expected an HTTP client with a non-zero timeout, got %+v", svc.client)
	}
}

// equalJSON does a shallow structural comparison sufficient for the small
// polygon fixtures used here.
func equalJSON(a, b map[string]any) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ab) == string(bb)
}
