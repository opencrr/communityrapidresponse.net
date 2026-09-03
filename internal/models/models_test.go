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

func TestRegisterResponseJSON(t *testing.T) {
	// A false EmailVerificationSent must be serialized explicitly so
	// clients can distinguish "verification email not sent" from a field
	// that was never set. An `omitempty` tag on this field would silently
	// drop it from the wire format, which the frontend depends on to route
	// users to the pending-verification page instead of a misleading
	// "check your inbox" success message.
	resp := RegisterResponse{
		UserID:                "test-id",
		Username:              "testuser",
		Email:                 "test@example.com",
		EmailVerificationSent: false,
		Message:               "The verification email could not be sent.",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal RegisterResponse: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal into map: %v", err)
	}

	sent, present := raw["email_verification_sent"]
	if !present {
		t.Fatalf("expected \"email_verification_sent\" key in JSON output, got: %s", data)
	}
	if sent != false {
		t.Errorf("expected email_verification_sent=false, got %v", sent)
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

func TestSignalGroupJSONSerialization(t *testing.T) {
	regionID := "region-id"
	group := &SignalGroup{
		ID:        "group-id",
		RegionID:  &regionID,
		GroupName: "Test Group",
		IsActive:  true,
	}

	data, err := json.Marshal(group)
	if err != nil {
		t.Fatalf("Failed to marshal signal group: %v", err)
	}

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

// =============================================================================
// SecretType Tests
// =============================================================================

func TestSecretType(t *testing.T) {
	tests := []struct {
		secretType SecretType
		expected   string
	}{
		{SecretTypeSignalInvite, "signal_invite"},
		{SecretTypeMeshtasticChannel, "meshtastic_channel"},
	}

	for _, tt := range tests {
		if string(tt.secretType) != tt.expected {
			t.Errorf("Expected secret type '%s', got '%s'", tt.expected, string(tt.secretType))
		}
	}
}

func TestProposalStatusApprovedPendingFinalization(t *testing.T) {
	if string(ProposalStatusApprovedPendingFinalization) != "approved_pending_finalization" {
		t.Errorf("Expected 'approved_pending_finalization', got '%s'", string(ProposalStatusApprovedPendingFinalization))
	}
}

// =============================================================================
// EncryptedSecret JSON Tests
// =============================================================================

func TestEncryptedSecretJSON(t *testing.T) {
	groupID := "group-123"
	secret := &EncryptedSecret{
		ID:               "secret-id",
		SecretType:       SecretTypeSignalInvite,
		SignalGroupID:    &groupID,
		EncryptedPayload: "base64-encrypted-data",
		EncryptionIV:     "base64-iv",
		UpdatedBy:        "user-123",
		UpdatedAt:        time.Now().UTC(),
	}

	data, err := json.Marshal(secret)
	if err != nil {
		t.Fatalf("Failed to marshal EncryptedSecret: %v", err)
	}

	if !contains(string(data), "signal_invite") {
		t.Error("secret_type should be in JSON output")
	}
	if !contains(string(data), "base64-encrypted-data") {
		t.Error("encrypted_payload should be in JSON output")
	}
	if !contains(string(data), "group-123") {
		t.Error("signal_group_id should be in JSON output")
	}

	// Nil meshtastic_channel_id should be omitted
	if contains(string(data), "meshtastic_channel_id") {
		t.Error("meshtastic_channel_id should be omitted when nil")
	}

	// Round-trip
	var parsed EncryptedSecret
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal EncryptedSecret: %v", err)
	}
	if parsed.ID != "secret-id" {
		t.Errorf("Expected ID 'secret-id', got %q", parsed.ID)
	}
	if parsed.SecretType != SecretTypeSignalInvite {
		t.Errorf("Expected secret_type 'signal_invite', got %q", parsed.SecretType)
	}
}

func TestEncryptedSecretJSON_MeshtasticChannel(t *testing.T) {
	channelID := "channel-456"
	secret := &EncryptedSecret{
		ID:                  "secret-id-2",
		SecretType:          SecretTypeMeshtasticChannel,
		MeshtasticChannelID: &channelID,
		EncryptedPayload:    "channel-encrypted-data",
		EncryptionIV:        "channel-iv",
		UpdatedBy:           "user-456",
		UpdatedAt:           time.Now().UTC(),
	}

	data, err := json.Marshal(secret)
	if err != nil {
		t.Fatalf("Failed to marshal EncryptedSecret: %v", err)
	}

	if !contains(string(data), "meshtastic_channel") {
		t.Error("secret_type should be 'meshtastic_channel'")
	}
	if !contains(string(data), "channel-456") {
		t.Error("meshtastic_channel_id should be in JSON output")
	}
	// Nil signal_group_id should be omitted
	if contains(string(data), "signal_group_id") {
		t.Error("signal_group_id should be omitted when nil")
	}
}

func TestEncryptedSecretKeyJSON(t *testing.T) {
	key := &EncryptedSecretKey{
		SecretID:    "secret-1",
		UserID:      "user-1",
		WrappedDEK:  "base64-wrapped-dek",
		RekeyNeeded: false,
		CreatedAt:   time.Now().UTC(),
	}

	data, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("Failed to marshal EncryptedSecretKey: %v", err)
	}

	if !contains(string(data), "base64-wrapped-dek") {
		t.Error("wrapped_dek should be in JSON output")
	}
	if !contains(string(data), "rekey_needed") {
		t.Error("rekey_needed should be in JSON output")
	}
}

func TestSecretUpdateProposalJSON(t *testing.T) {
	regionID := "region-1"
	reason := "Invite link compromised"
	proposal := &SecretUpdateProposal{
		ID:                "proposal-1",
		EncryptedSecretID: "secret-1",
		RegionID:          &regionID,
		ProposedBy:        "user-123",
		EncryptedPayload:  "new-encrypted-data",
		EncryptionIV:      "new-iv",
		Reason:            &reason,
		Status:            ProposalStatusPending,
		CreatedAt:         time.Now().UTC(),
		ExpiresAt:         time.Now().UTC().Add(7 * 24 * time.Hour),
	}

	data, err := json.Marshal(proposal)
	if err != nil {
		t.Fatalf("Failed to marshal SecretUpdateProposal: %v", err)
	}

	if !contains(string(data), "proposal-1") {
		t.Error("id should be in JSON output")
	}
	if !contains(string(data), "pending") {
		t.Error("status should be in JSON output")
	}
	if !contains(string(data), "Invite link compromised") {
		t.Error("reason should be in JSON output")
	}
	// Nil school_id/district_id should be omitted
	if contains(string(data), "school_id") {
		t.Error("school_id should be omitted when nil")
	}
	if contains(string(data), "district_id") {
		t.Error("district_id should be omitted when nil")
	}
}

func TestSecretProposalResponseJSON(t *testing.T) {
	vote := true
	response := &SecretProposalResponse{
		ProposalID:        "prop-1",
		EncryptedSecretID: "secret-1",
		Status:            "pending",
		VotesNeeded:       3,
		CurrentVotes:      1,
		YourVote:          &vote,
		ConsensusReached:  false,
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal SecretProposalResponse: %v", err)
	}

	if !contains(string(data), "prop-1") {
		t.Error("proposal_id should be in JSON output")
	}
	if !contains(string(data), "votes_needed") {
		t.Error("votes_needed should be in JSON output")
	}

	var parsed SecretProposalResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal SecretProposalResponse: %v", err)
	}
	if parsed.VotesNeeded != 3 {
		t.Errorf("Expected votes_needed=3, got %d", parsed.VotesNeeded)
	}
	if parsed.CurrentVotes != 1 {
		t.Errorf("Expected current_votes=1, got %d", parsed.CurrentVotes)
	}
}

func TestCreateSecretProposalRequestJSON(t *testing.T) {
	reason := "Link expired"
	reqJSON := `{
		"encrypted_payload": "enc-data",
		"encryption_iv": "iv-data",
		"wrapped_keys": [
			{"user_id": "user-1", "wrapped_dek": "dek-1"},
			{"user_id": "user-2", "wrapped_dek": "dek-2"}
		],
		"reason": "Link expired"
	}`

	var parsed CreateSecretProposalRequest
	if err := json.Unmarshal([]byte(reqJSON), &parsed); err != nil {
		t.Fatalf("Failed to unmarshal CreateSecretProposalRequest: %v", err)
	}

	if parsed.EncryptedPayload != "enc-data" {
		t.Errorf("Expected encrypted_payload 'enc-data', got %q", parsed.EncryptedPayload)
	}
	if len(parsed.WrappedKeys) != 2 {
		t.Errorf("Expected 2 wrapped keys, got %d", len(parsed.WrappedKeys))
	}
	if *parsed.Reason != reason {
		t.Errorf("Expected reason %q, got %q", reason, *parsed.Reason)
	}
}

func TestFinalizeSecretUpdateRequestJSON(t *testing.T) {
	reqJSON := `{
		"encrypted_payload": "final-enc",
		"encryption_iv": "final-iv",
		"wrapped_keys": [
			{"user_id": "u1", "wrapped_dek": "d1"}
		]
	}`

	var parsed FinalizeSecretUpdateRequest
	if err := json.Unmarshal([]byte(reqJSON), &parsed); err != nil {
		t.Fatalf("Failed to unmarshal FinalizeSecretUpdateRequest: %v", err)
	}

	if parsed.EncryptedPayload != "final-enc" {
		t.Errorf("Expected encrypted_payload 'final-enc', got %q", parsed.EncryptedPayload)
	}
	if len(parsed.WrappedKeys) != 1 {
		t.Errorf("Expected 1 wrapped key, got %d", len(parsed.WrappedKeys))
	}
}

// =============================================================================
// Encryption Key Model Tests
// =============================================================================

func TestUserEncryptionKeyJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	key := &UserEncryptionKey{
		UserID:            "user-123",
		PublicKey:         "RSA-pub-key",
		WrappedPrivateKey: "wrapped-priv-key",
		KeySalt:           "pbkdf2-salt",
		KeyIV:             "aes-iv",
		CreatedAt:         now,
		RotatedAt:         now,
	}

	data, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("Failed to marshal UserEncryptionKey: %v", err)
	}

	if !contains(string(data), "RSA-pub-key") {
		t.Error("public_key should be in JSON output")
	}
	if !contains(string(data), "wrapped-priv-key") {
		t.Error("wrapped_private_key should be in JSON output")
	}
	if !contains(string(data), "pbkdf2-salt") {
		t.Error("key_salt should be in JSON output")
	}

	var parsed UserEncryptionKey
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal UserEncryptionKey: %v", err)
	}
	if parsed.UserID != "user-123" {
		t.Errorf("Expected user_id 'user-123', got %q", parsed.UserID)
	}
}

func TestEncryptionKeyResponseJSON(t *testing.T) {
	resp := &EncryptionKeyResponse{
		PublicKey:         "pub-key",
		WrappedPrivateKey: "wrapped-key",
		KeySalt:           "salt",
		KeyIV:             "iv",
		RotatedAt:         time.Now().UTC(),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal EncryptionKeyResponse: %v", err)
	}

	if !contains(string(data), "pub-key") {
		t.Error("public_key should be in JSON output")
	}
	if !contains(string(data), "rotated_at") {
		t.Error("rotated_at should be in JSON output")
	}
}

func TestPublicKeyEntryJSON(t *testing.T) {
	entry := &PublicKeyEntry{
		UserID:    "user-1",
		PublicKey: "pub-key-1",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Failed to marshal PublicKeyEntry: %v", err)
	}

	var parsed PublicKeyEntry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal PublicKeyEntry: %v", err)
	}
	if parsed.UserID != "user-1" {
		t.Errorf("Expected user_id 'user-1', got %q", parsed.UserID)
	}
	if parsed.PublicKey != "pub-key-1" {
		t.Errorf("Expected public_key 'pub-key-1', got %q", parsed.PublicKey)
	}
}

func TestSubmitRekeysRequestJSON(t *testing.T) {
	reqJSON := `{
		"rekeys": [
			{"secret_id": "s1", "target_user_id": "u1", "wrapped_dek": "d1"},
			{"secret_id": "s2", "target_user_id": "u2", "wrapped_dek": "d2"}
		]
	}`

	var parsed SubmitRekeysRequest
	if err := json.Unmarshal([]byte(reqJSON), &parsed); err != nil {
		t.Fatalf("Failed to unmarshal SubmitRekeysRequest: %v", err)
	}

	if len(parsed.Rekeys) != 2 {
		t.Errorf("Expected 2 rekeys, got %d", len(parsed.Rekeys))
	}
	if parsed.Rekeys[0].SecretID != "s1" {
		t.Errorf("Expected first rekey secret_id 's1', got %q", parsed.Rekeys[0].SecretID)
	}
}

func TestCreateEncryptionKeyRequestJSON(t *testing.T) {
	reqJSON := `{
		"public_key": "pub",
		"wrapped_private_key": "wrapped",
		"key_salt": "salt",
		"key_iv": "iv"
	}`

	var parsed CreateEncryptionKeyRequest
	if err := json.Unmarshal([]byte(reqJSON), &parsed); err != nil {
		t.Fatalf("Failed to unmarshal CreateEncryptionKeyRequest: %v", err)
	}

	if parsed.PublicKey != "pub" {
		t.Errorf("Expected public_key 'pub', got %q", parsed.PublicKey)
	}
	if parsed.WrappedPrivateKey != "wrapped" {
		t.Errorf("Expected wrapped_private_key 'wrapped', got %q", parsed.WrappedPrivateKey)
	}
}

// =============================================================================
// Meshtastic Model Tests
// =============================================================================

func TestMeshtasticChannelJSON(t *testing.T) {
	regionID := "region-1"
	channel := &MeshtasticChannel{
		ID:          "ch-1",
		RegionID:    &regionID,
		ChannelName: "Emergency Channel",
		IsActive:    true,
		CreatedAt:   time.Now().UTC(),
	}

	data, err := json.Marshal(channel)
	if err != nil {
		t.Fatalf("Failed to marshal MeshtasticChannel: %v", err)
	}

	if !contains(string(data), "Emergency Channel") {
		t.Error("channel_name should be in JSON output")
	}
	if !contains(string(data), "region-1") {
		t.Error("region_id should be in JSON output")
	}
	// Nil school_id/district_id should be omitted
	if contains(string(data), "school_id") {
		t.Error("school_id should be omitted when nil")
	}
	if contains(string(data), "district_id") {
		t.Error("district_id should be omitted when nil")
	}
	// has_pending_deletion has json:"-", should not appear
	if contains(string(data), "has_pending_deletion") {
		t.Error("has_pending_deletion should not be in JSON output (json:\"-\")")
	}
}

func TestMeshtasticChannelJSON_SchoolScope(t *testing.T) {
	schoolID := "school-1"
	channel := &MeshtasticChannel{
		ID:          "ch-2",
		SchoolID:    &schoolID,
		SchoolName:  "Lincoln High",
		ChannelName: "School Channel",
		IsActive:    true,
		CreatedAt:   time.Now().UTC(),
	}

	data, err := json.Marshal(channel)
	if err != nil {
		t.Fatalf("Failed to marshal MeshtasticChannel: %v", err)
	}

	if !contains(string(data), "school-1") {
		t.Error("school_id should be in JSON output")
	}
	if !contains(string(data), "Lincoln High") {
		t.Error("school_name should be in JSON output")
	}
	if contains(string(data), "region_id") {
		t.Error("region_id should be omitted when nil")
	}
}

func TestMeshtasticChannelPublicJSON(t *testing.T) {
	regionID := "region-1"
	pub := &MeshtasticChannelPublic{
		ID:                 "ch-1",
		RegionID:           &regionID,
		RegionName:         "Downtown",
		Name:               "Emergency Mesh",
		HasPendingDeletion: true,
		CreatedAt:          time.Now().UTC(),
	}

	data, err := json.Marshal(pub)
	if err != nil {
		t.Fatalf("Failed to marshal MeshtasticChannelPublic: %v", err)
	}

	// has_pending_deletion should be visible (no json:"-")
	if !contains(string(data), "has_pending_deletion") {
		t.Error("has_pending_deletion should be in JSON output for public type")
	}
	if !contains(string(data), "Emergency Mesh") {
		t.Error("name should be in JSON output")
	}
}

func TestMeshtasticChannelWithSecretJSON(t *testing.T) {
	regionID := "region-1"
	channelWithSecret := &MeshtasticChannelWithSecret{
		MeshtasticChannelPublic: MeshtasticChannelPublic{
			ID:         "ch-1",
			RegionID:   &regionID,
			RegionName: "Downtown",
			Name:       "Mesh Channel",
			CreatedAt:  time.Now().UTC(),
		},
		EncryptedSecret: &EncryptedSecretResponse{
			SecretID:         "secret-1",
			EncryptedPayload: "enc-payload",
			EncryptionIV:     "enc-iv",
			WrappedDEK:       "user-dek",
		},
	}

	data, err := json.Marshal(channelWithSecret)
	if err != nil {
		t.Fatalf("Failed to marshal MeshtasticChannelWithSecret: %v", err)
	}

	if !contains(string(data), "enc-payload") {
		t.Error("encrypted_payload should be in JSON output")
	}
	if !contains(string(data), "user-dek") {
		t.Error("wrapped_dek should be in JSON output")
	}

	// Round-trip
	var parsed MeshtasticChannelWithSecret
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal MeshtasticChannelWithSecret: %v", err)
	}
	if parsed.EncryptedSecret == nil {
		t.Fatal("Expected encrypted_secret to be present")
	}
	if parsed.EncryptedSecret.WrappedDEK != "user-dek" {
		t.Errorf("Expected wrapped_dek 'user-dek', got %q", parsed.EncryptedSecret.WrappedDEK)
	}
}

func TestMeshtasticChannelWithSecret_NoSecret(t *testing.T) {
	regionID := "region-1"
	channelWithoutSecret := &MeshtasticChannelWithSecret{
		MeshtasticChannelPublic: MeshtasticChannelPublic{
			ID:        "ch-1",
			RegionID:  &regionID,
			Name:      "Mesh Channel",
			CreatedAt: time.Now().UTC(),
		},
		EncryptedSecret: nil,
	}

	data, err := json.Marshal(channelWithoutSecret)
	if err != nil {
		t.Fatalf("Failed to marshal MeshtasticChannelWithSecret: %v", err)
	}

	// encrypted_secret should be omitted when nil
	if contains(string(data), "encrypted_secret") {
		t.Error("encrypted_secret should be omitted when nil")
	}
}

func TestCreateMeshtasticChannelRequestJSON(t *testing.T) {
	reqJSON := `{
		"region_id": "region-1",
		"name": "Emergency Mesh",
		"description": "For emergencies",
		"encrypted_payload": "enc-data",
		"encryption_iv": "iv-data",
		"wrapped_keys": [
			{"user_id": "user-1", "wrapped_dek": "dek-1"}
		]
	}`

	var parsed CreateMeshtasticChannelRequest
	if err := json.Unmarshal([]byte(reqJSON), &parsed); err != nil {
		t.Fatalf("Failed to unmarshal CreateMeshtasticChannelRequest: %v", err)
	}

	if parsed.RegionID == nil || *parsed.RegionID != "region-1" {
		t.Error("Expected region_id 'region-1'")
	}
	if parsed.Name != "Emergency Mesh" {
		t.Errorf("Expected name 'Emergency Mesh', got %q", parsed.Name)
	}
	if parsed.SchoolID != nil {
		t.Error("Expected school_id to be nil")
	}
	if parsed.DistrictID != nil {
		t.Error("Expected district_id to be nil")
	}
	if len(parsed.WrappedKeys) != 1 {
		t.Errorf("Expected 1 wrapped key, got %d", len(parsed.WrappedKeys))
	}
}

func TestUpdateMeshtasticChannelRequestJSON(t *testing.T) {
	reqJSON := `{"name": "New Name", "description": "Updated description"}`

	var parsed UpdateMeshtasticChannelRequest
	if err := json.Unmarshal([]byte(reqJSON), &parsed); err != nil {
		t.Fatalf("Failed to unmarshal UpdateMeshtasticChannelRequest: %v", err)
	}

	if parsed.Name == nil || *parsed.Name != "New Name" {
		t.Error("Expected name 'New Name'")
	}
	if parsed.Description == nil || *parsed.Description != "Updated description" {
		t.Error("Expected description 'Updated description'")
	}
}

func TestMeshtasticChannelListResponseJSON(t *testing.T) {
	regionID := "region-1"
	response := &MeshtasticChannelListResponse{
		Channels: []MeshtasticChannelWithSecret{
			{
				MeshtasticChannelPublic: MeshtasticChannelPublic{
					ID:       "ch-1",
					RegionID: &regionID,
					Name:     "Channel 1",
				},
			},
			{
				MeshtasticChannelPublic: MeshtasticChannelPublic{
					ID:       "ch-2",
					RegionID: &regionID,
					Name:     "Channel 2",
				},
			},
		},
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal MeshtasticChannelListResponse: %v", err)
	}

	var parsed MeshtasticChannelListResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal MeshtasticChannelListResponse: %v", err)
	}

	if len(parsed.Channels) != 2 {
		t.Errorf("Expected 2 channels, got %d", len(parsed.Channels))
	}
}

// =============================================================================
// WrappedKeyEntry Tests
// =============================================================================

func TestWrappedKeyEntryJSON(t *testing.T) {
	entry := &WrappedKeyEntry{
		UserID:     "user-1",
		WrappedDEK: "base64-dek",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Failed to marshal WrappedKeyEntry: %v", err)
	}

	var parsed WrappedKeyEntry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal WrappedKeyEntry: %v", err)
	}

	if parsed.UserID != "user-1" {
		t.Errorf("Expected user_id 'user-1', got %q", parsed.UserID)
	}
	if parsed.WrappedDEK != "base64-dek" {
		t.Errorf("Expected wrapped_dek 'base64-dek', got %q", parsed.WrappedDEK)
	}
}

// =============================================================================
// SecretProposalDetailResponse Tests
// =============================================================================

func TestSecretProposalDetailResponseJSON(t *testing.T) {
	createdAt := time.Now().UTC().Truncate(time.Second)
	detail := &SecretProposalDetailResponse{
		ID:                "prop-1",
		EncryptedSecretID: "secret-1",
		SecretType:        "signal_invite",
		ScopeName:         "Region One",
		GroupName:         "Group One",
		ProposedBy: ProposalUser{
			ID:       "user-1",
			Username: "adminuser",
		},
		EncryptedPayload: "enc-data",
		EncryptionIV:     "enc-iv",
		WrappedDEK:       "my-dek",
		Status:           "pending",
		VotesNeeded:      3,
		CurrentVotes:     1,
		Votes: []SecretVoteDetail{
			{VoterID: "user-1", Username: "adminuser", Vote: true, VotedAt: createdAt},
		},
		CreatedAt: createdAt,
		ExpiresAt: createdAt.Add(72 * time.Hour),
		CanVote:   false,
		HasVoted:  true,
	}

	data, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("Failed to marshal SecretProposalDetailResponse: %v", err)
	}

	if !contains(string(data), "Region One") {
		t.Error("scope_name should be in JSON output")
	}
	if !contains(string(data), "my-dek") {
		t.Error("wrapped_dek should be in JSON output")
	}
	if !contains(string(data), "has_voted") {
		t.Error("has_voted should be in JSON output")
	}

	var parsed SecretProposalDetailResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal SecretProposalDetailResponse: %v", err)
	}
	if parsed.ProposedBy.Username != "adminuser" {
		t.Errorf("Expected proposed_by.username 'adminuser', got %q", parsed.ProposedBy.Username)
	}
	if len(parsed.Votes) != 1 {
		t.Errorf("Expected 1 vote, got %d", len(parsed.Votes))
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
