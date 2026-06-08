package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func makeRepeatedByte(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// =============================================================================
// writeValidationError Tests
// =============================================================================

func TestWriteValidationError_PopulatesFieldAndOmitsDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	writeValidationError(rec, "email", "Email is required")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", ct)
	}

	// Decode into ErrorResponse to verify structured fields.
	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp.Error != "validation_error" {
		t.Errorf("Expected error=validation_error, got %q", resp.Error)
	}
	if resp.Field != "email" {
		t.Errorf("Expected field=email, got %q", resp.Field)
	}
	if resp.Message != "Email is required" {
		t.Errorf("Expected message='Email is required', got %q", resp.Message)
	}
	if resp.Details != "" {
		t.Errorf("Expected details to be empty, got %q", resp.Details)
	}
}

func TestWriteValidationError_OmitsEmptyFieldsInJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeValidationError(rec, "username", "Username is required")

	// Decode into a generic map to confirm the JSON wire format omits empty
	// optional fields (no "details" key in the payload).
	var raw map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if _, present := raw["details"]; present {
		t.Errorf("Expected 'details' to be omitted from JSON when empty, got %v", raw)
	}
	if raw["field"] != "username" {
		t.Errorf("Expected field=username in JSON, got %v", raw["field"])
	}
}

// =============================================================================
// isValidUUID Tests
// =============================================================================

func TestIsValidUUID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid lowercase UUID", "550e8400-e29b-41d4-a716-446655440000", true},
		{"valid uppercase UUID", "550E8400-E29B-41D4-A716-446655440000", true},
		{"valid mixed case UUID", "550e8400-E29B-41d4-a716-446655440000", true},
		{"empty string", "", false},
		{"whitespace only", "   ", false},
		{"tab characters", "\t\t", false},
		{"too short", "550e8400-e29b-41d4-a716", false},
		{"too long", "550e8400-e29b-41d4-a716-446655440000-extra", false},
		{"no dashes", "550e8400e29b41d4a716446655440000", false},
		{"wrong dash positions", "550e-8400e29b-41d4a716-446655440000", false},
		{"contains non-hex chars", "550e8400-e29b-41d4-a716-44665544zzzz", false},
		{"SQL injection attempt", "'; DROP TABLE users;--", false},
		{"path traversal", "../../../etc/passwd", false},
		{"very long string", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"null byte", "550e8400-e29b-41d4-a716-4466554400\x00", false},
		{"just dashes", "--------", false},
		{"special characters", "!@#$%^&*()_+", false},
		{"newline in UUID", "550e8400-e29b-41d4\n-a716-446655440000", false},
		{"spaces around valid UUID", " 550e8400-e29b-41d4-a716-446655440000 ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidUUID(tt.input)
			if got != tt.want {
				t.Errorf("isValidUUID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Router UUID Validation Tests (unit tests for path param rejection)
// =============================================================================

func TestRouter_RegionByID_InvalidUUID(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"non-UUID string", "/api/v1/communities/not-a-uuid"},
		{"SQL injection", "/api/v1/communities/'%3BDROP+TABLE+users%3B--"},
		{"very long string", "/api/v1/communities/" + string(makeRepeatedByte('a', 500))},
		{"special characters", "/api/v1/communities/!@%23$%25%5E&*()"},
		{"empty segments", "/api/v1/communities//"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()

			// Create a minimal router just to test the path validation
			router := &Router{}
			router.handleRegionByID(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
			}

			var body map[string]string
			_ = json.NewDecoder(rec.Body).Decode(&body)
			if body["error"] != "invalid_id" && body["error"] != "missing_id" {
				t.Errorf("Expected error 'invalid_id' or 'missing_id', got %q", body["error"])
			}
		})
	}
}

func TestRouter_SignalGroupByID_InvalidUUID(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"non-UUID string", "/api/v1/signal-groups/not-a-uuid"},
		{"SQL injection", "/api/v1/signal-groups/1+OR+1%3D1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("PUT", tt.path, nil)
			rec := httptest.NewRecorder()

			router := &Router{}
			router.handleSignalGroupByID(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRouter_SchoolByID_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/schools/not-a-valid-uuid", nil)
	rec := httptest.NewRecorder()

	router := &Router{}
	router.handleSchoolByID(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "invalid_id" {
		t.Errorf("Expected error 'invalid_id', got %q", body["error"])
	}
}

func TestRouter_SchoolDistrictByID_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/school-districts/xyz-invalid", nil)
	rec := httptest.NewRecorder()

	router := &Router{}
	router.handleSchoolDistrictByID(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRouter_MembershipRequestByID_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/membership-requests/invalid!!!", nil)
	rec := httptest.NewRecorder()

	router := &Router{}
	router.handleMembershipRequestByID(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRouter_InvitationByID_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/invitations/not-uuid/respond", nil)
	rec := httptest.NewRecorder()

	router := &Router{}
	router.handleInvitationByID(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRouter_BlocklistProposal_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/blocklist-proposals/sql-injection", nil)
	rec := httptest.NewRecorder()

	router := &Router{}
	router.handleBlocklistProposal(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRouter_DeletionProposal_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/deletion-proposals/bad-id", nil)
	rec := httptest.NewRecorder()

	router := &Router{}
	router.handleDeletionProposal(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRouter_SecretProposal_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/secret-proposals/not-valid", nil)
	rec := httptest.NewRecorder()

	router := &Router{}
	router.handleSecretProposal(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRouter_EncryptedSecretByID_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/encrypted-secrets/bad/finalize", nil)
	rec := httptest.NewRecorder()

	router := &Router{}
	router.handleEncryptedSecretByID(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRouter_ReportByID_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/reports/not-a-uuid", nil)
	rec := httptest.NewRecorder()

	router := &Router{}
	router.handleReportByID(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRouter_MeshtasticChannelByID_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest("PUT", "/api/v1/meshtastic-channels/invalid", nil)
	rec := httptest.NewRecorder()

	router := &Router{}
	router.handleMeshtasticChannelByID(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRouter_AdminUserByID_InvalidUUID(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"plain invalid", "/api/v1/admin/users/not-a-uuid"},
		{"invalid with grant-vouch suffix", "/api/v1/admin/users/bad-id/grant-vouch"},
		{"invalid with revoke-vouch suffix", "/api/v1/admin/users/bad-id/revoke-vouch"},
		{"invalid with block suffix", "/api/v1/admin/users/bad-id/block"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", tt.path, nil)
			rec := httptest.NewRecorder()

			router := &Router{}
			router.handleAdminUserByID(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
			}

			var body map[string]string
			_ = json.NewDecoder(rec.Body).Decode(&body)
			if body["error"] != "invalid_id" && body["error"] != "missing_id" {
				t.Errorf("Expected error 'invalid_id' or 'missing_id', got %q", body["error"])
			}
		})
	}
}

func TestRouter_VouchStatus_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/verification/vouch/status/not-a-uuid", nil)
	rec := httptest.NewRecorder()

	router := &Router{}
	router.handleVouchStatus(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
