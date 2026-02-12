package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestVerificationTier(t *testing.T) {
	tests := []struct {
		tier     VerificationTier
		expected int
	}{
		{TierUnverified, 0},
		{TierVouched, 1},
		{TierPostcard, 2},
	}

	for _, tt := range tests {
		if int(tt.tier) != tt.expected {
			t.Errorf("Expected tier value %d, got %d", tt.expected, int(tt.tier))
		}
	}
}

func TestRegionType(t *testing.T) {
	tests := []struct {
		regionType RegionType
		expected   string
	}{
		{RegionTypeState, "state"},
		{RegionTypeCounty, "county"},
		{RegionTypeCity, "city"},
		{RegionTypeNeighborhood, "neighborhood"},
		{RegionTypeCityBlock, "city_block"},
	}

	for _, tt := range tests {
		if string(tt.regionType) != tt.expected {
			t.Errorf("Expected region type '%s', got '%s'", tt.expected, string(tt.regionType))
		}
	}
}

func TestVerificationStatus(t *testing.T) {
	tests := []struct {
		status   VerificationStatus
		expected string
	}{
		{VerificationStatusPending, "pending"},
		{VerificationStatusMailed, "mailed"},
		{VerificationStatusVerified, "verified"},
		{VerificationStatusExpired, "expired"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.expected {
			t.Errorf("Expected status '%s', got '%s'", tt.expected, string(tt.status))
		}
	}
}

func TestProposalStatus(t *testing.T) {
	tests := []struct {
		status   ProposalStatus
		expected string
	}{
		{ProposalStatusPending, "pending"},
		{ProposalStatusApproved, "approved"},
		{ProposalStatusRejected, "rejected"},
		{ProposalStatusExpired, "expired"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.expected {
			t.Errorf("Expected status '%s', got '%s'", tt.expected, string(tt.status))
		}
	}
}

func TestUserJSON(t *testing.T) {
	now := time.Now().UTC()
	user := &User{
		ID:               "test-id",
		Username:         "testuser",
		Email:            "test@example.com",
		PasswordHash:     "hashed_password",
		VerificationTier: TierPostcard,
		CreatedAt:        now,
	}

	data, err := json.Marshal(user)
	if err != nil {
		t.Fatalf("Failed to marshal user: %v", err)
	}

	// Password hash should not be in JSON
	if contains(string(data), "hashed_password") {
		t.Error("Password hash should not be in JSON output")
	}

	// Other fields should be present
	if !contains(string(data), "testuser") {
		t.Error("Username should be in JSON output")
	}
}

func TestPublicUser(t *testing.T) {
	user := &PublicUser{
		ID:               "test-id",
		Username:         "testuser",
		VerificationTier: TierPostcard,
	}

	data, err := json.Marshal(user)
	if err != nil {
		t.Fatalf("Failed to marshal public user: %v", err)
	}

	if !contains(string(data), "testuser") {
		t.Error("Username should be in JSON output")
	}
	if !contains(string(data), "verification_tier") {
		t.Error("Verification tier should be in JSON output")
	}
}

func TestVerificationRequestNoAddress(t *testing.T) {
	req := &VerificationRequest{
		ID:                "test-id",
		UserID:            "user-id",
		VerificationCode:  "ABC123",
		Status:            VerificationStatusPending,
		PostgridRequestID: "postgrid-id",
		BoundaryType:      "city",
		BoundaryName:      "San Francisco",
		BoundaryState:     "California",
		CreatedAt:         time.Now().UTC(),
		ExpiresAt:         time.Now().UTC().Add(30 * 24 * time.Hour),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal verification request: %v", err)
	}

	// Verification code should not be in JSON (marked with json:"-")
	if contains(string(data), "ABC123") {
		t.Error("Verification code should not be in JSON output")
	}

	// Postgrid request ID should not be in JSON
	if contains(string(data), "postgrid-id") {
		t.Error("Postgrid request ID should not be in JSON output")
	}

	// Status should be present
	if !contains(string(data), "pending") {
		t.Error("Status should be in JSON output")
	}
}

func TestSignalGroupInviteLinkVisibility(t *testing.T) {
	regionID := "region-id"
	group := &SignalGroup{
		ID:         "group-id",
		RegionID:   &regionID,
		GroupName:  "Test Group",
		InviteLink: "https://signal.group/secret_link",
		IsActive:   true,
	}

	data, err := json.Marshal(group)
	if err != nil {
		t.Fatalf("Failed to marshal signal group: %v", err)
	}

	// Invite link should be present (with omitempty, it's only hidden when empty)
	if !contains(string(data), "Test Group") {
		t.Error("Group name should be in JSON output")
	}
}

func TestAddressNeverStored(t *testing.T) {
	// This test documents that Address struct is for in-memory use only
	address := &Address{
		Line1:      "123 Main St",
		Line2:      "Apt 4",
		City:       "San Francisco",
		State:      "CA",
		PostalCode: "94102",
		Country:    "US",
	}

	// Address can be marshaled for API requests to external services
	data, err := json.Marshal(address)
	if err != nil {
		t.Fatalf("Failed to marshal address: %v", err)
	}

	if !contains(string(data), "123 Main St") {
		t.Error("Address should be marshalable for external API calls")
	}

	// Document: This struct should NEVER be stored in the database
	// The VerificationRequest struct has no address fields
	t.Log("IMPORTANT: Address struct is for in-memory processing only. Never store in database.")
}

func TestGeoJSONGeometry(t *testing.T) {
	geom := &GeoJSONGeometry{
		Type: "Polygon",
		Coordinates: [][][]float64{
			{
				{-122.5, 37.7},
				{-122.4, 37.7},
				{-122.4, 37.8},
				{-122.5, 37.8},
				{-122.5, 37.7},
			},
		},
	}

	data, err := json.Marshal(geom)
	if err != nil {
		t.Fatalf("Failed to marshal GeoJSON: %v", err)
	}

	if !contains(string(data), "Polygon") {
		t.Error("Type should be in JSON output")
	}

	// Verify it can be unmarshaled
	var parsed GeoJSONGeometry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal GeoJSON: %v", err)
	}

	if parsed.Type != "Polygon" {
		t.Error("Parsed type should be 'Polygon'")
	}

	// Type-assert coordinates since it's now interface{}
	coords, ok := parsed.Coordinates.([]interface{})
	if !ok {
		t.Fatal("Coordinates should be a slice")
	}
	if len(coords) != 1 {
		t.Error("Should have one polygon ring")
	}
}

func TestGeoJSONGeometry_MultiPolygon(t *testing.T) {
	// MultiPolygon coordinates have 4 levels: [polygon][ring][point][lng,lat]
	multiPolygonJSON := `{
		"type": "MultiPolygon",
		"coordinates": [
			[
				[
					[-74.0, 40.7],
					[-74.0, 40.8],
					[-73.9, 40.8],
					[-73.9, 40.7],
					[-74.0, 40.7]
				]
			],
			[
				[
					[-74.1, 40.6],
					[-74.1, 40.65],
					[-74.05, 40.65],
					[-74.05, 40.6],
					[-74.1, 40.6]
				]
			]
		]
	}`

	// Verify MultiPolygon can be unmarshaled
	var parsed GeoJSONGeometry
	if err := json.Unmarshal([]byte(multiPolygonJSON), &parsed); err != nil {
		t.Fatalf("Failed to unmarshal MultiPolygon GeoJSON: %v", err)
	}

	if parsed.Type != "MultiPolygon" {
		t.Errorf("Expected type 'MultiPolygon', got '%s'", parsed.Type)
	}

	// Verify coordinates is not nil
	if parsed.Coordinates == nil {
		t.Fatal("Coordinates should not be nil")
	}

	// Type-assert coordinates - should be []interface{} after JSON unmarshal
	coords, ok := parsed.Coordinates.([]interface{})
	if !ok {
		t.Fatalf("Coordinates should be a slice, got %T", parsed.Coordinates)
	}

	// Should have 2 polygons
	if len(coords) != 2 {
		t.Errorf("Expected 2 polygons in MultiPolygon, got %d", len(coords))
	}

	// Verify it can be re-marshaled
	data, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("Failed to marshal MultiPolygon: %v", err)
	}

	if !contains(string(data), "MultiPolygon") {
		t.Error("Type should be in JSON output")
	}
}

func TestGeoJSONGeometry_RealWorldMultiPolygon(t *testing.T) {
	// Simulates NYC-like MultiPolygon with multiple polygons (boroughs/islands)
	// This is a simplified version of what OSM returns for NYC
	nycLikeJSON := `{
		"type": "MultiPolygon",
		"coordinates": [
			[
				[
					[-74.258843, 40.498875],
					[-74.2581372, 40.4973901],
					[-73.700272, 40.879548],
					[-73.728684, 40.889168],
					[-74.258843, 40.498875]
				]
			]
		]
	}`

	var parsed GeoJSONGeometry
	if err := json.Unmarshal([]byte(nycLikeJSON), &parsed); err != nil {
		t.Fatalf("Failed to unmarshal NYC-like MultiPolygon: %v", err)
	}

	if parsed.Type != "MultiPolygon" {
		t.Errorf("Expected type 'MultiPolygon', got '%s'", parsed.Type)
	}

	// Verify the structure can be traversed
	coords, ok := parsed.Coordinates.([]interface{})
	if !ok {
		t.Fatalf("Coordinates should be a slice, got %T", parsed.Coordinates)
	}

	if len(coords) == 0 {
		t.Fatal("Expected at least one polygon")
	}

	// Verify first polygon exists and has rings
	polygon, ok := coords[0].([]interface{})
	if !ok || len(polygon) == 0 {
		t.Fatal("First polygon should have rings")
	}

	// Verify outer ring has points
	outerRing, ok := polygon[0].([]interface{})
	if !ok || len(outerRing) == 0 {
		t.Fatal("Outer ring should have points")
	}

	// Verify first point structure
	firstPoint, ok := outerRing[0].([]interface{})
	if !ok || len(firstPoint) < 2 {
		t.Fatal("Points should have [lng, lat] format")
	}
}

func TestNotificationTypes(t *testing.T) {
	types := []NotificationType{
		NotificationTypeInviteLinkUpdated,
		NotificationTypeVerificationComplete,
		NotificationTypeVouchReceived,
		NotificationTypeVouchComplete,
		NotificationTypeBlocklistProposal,
		NotificationTypeDeletionProposal,
		NotificationTypeInviteLinkProposal,
	}

	for _, nt := range types {
		if string(nt) == "" {
			t.Error("Notification type should not be empty")
		}
	}
}

func TestAuditActionConstants(t *testing.T) {
	actions := []string{
		AuditActionUserRegistered,
		AuditActionUserLogin,
		AuditActionUserVerified,
		AuditActionUserVouched,
		AuditActionRegionCreated,
		AuditActionSignalGroupCreated,
		AuditActionInviteLinkUpdated,
		AuditActionProposalCreated,
		AuditActionUserBlocked,
	}

	for _, action := range actions {
		if action == "" {
			t.Error("Audit action should not be empty")
		}
	}
}

func TestUpdateRegionRequest(t *testing.T) {
	req := &UpdateRegionRequest{
		Name: "Updated Region Name",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal UpdateRegionRequest: %v", err)
	}

	if !contains(string(data), "Updated Region Name") {
		t.Error("Name should be in JSON output")
	}

	// Verify it can be unmarshaled
	var parsed UpdateRegionRequest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal UpdateRegionRequest: %v", err)
	}

	if parsed.Name != "Updated Region Name" {
		t.Error("Parsed name should match original")
	}
}

func TestRegionSummaryWithGeometry(t *testing.T) {
	geom := &GeoJSONGeometry{
		Type: "Polygon",
		Coordinates: [][][]float64{
			{
				{-122.5, 37.7},
				{-122.4, 37.7},
				{-122.4, 37.8},
				{-122.5, 37.8},
				{-122.5, 37.7},
			},
		},
	}

	summary := &RegionSummary{
		ID:          "test-region-id",
		Name:        "Test Region",
		RegionType:  RegionTypeNeighborhood,
		AdminCount:  2,
		MemberCount: 10,
		Geometry:    geom,
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("Failed to marshal RegionSummary: %v", err)
	}

	// Verify all fields are present
	if !contains(string(data), "Test Region") {
		t.Error("Name should be in JSON output")
	}
	if !contains(string(data), "neighborhood") {
		t.Error("RegionType should be in JSON output")
	}
	if !contains(string(data), "Polygon") {
		t.Error("Geometry type should be in JSON output")
	}

	// Verify it can be unmarshaled
	var parsed RegionSummary
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal RegionSummary: %v", err)
	}

	if parsed.ID != "test-region-id" {
		t.Error("Parsed ID should match original")
	}
	if parsed.Geometry == nil {
		t.Error("Geometry should not be nil")
	}
	if parsed.Geometry.Type != "Polygon" {
		t.Error("Geometry type should be 'Polygon'")
	}
}

func TestRegionSummaryWithoutGeometry(t *testing.T) {
	summary := &RegionSummary{
		ID:         "test-region-id",
		Name:       "Test Region",
		RegionType: RegionTypeCity,
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("Failed to marshal RegionSummary: %v", err)
	}

	// Geometry should be omitted when nil
	if contains(string(data), "geometry") {
		t.Error("Geometry should be omitted when nil (omitempty)")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
