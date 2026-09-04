package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/logging"
)

func makeRepeatedByte(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
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

// =============================================================================
// writeServerError Tests
// =============================================================================

func TestWriteServerError_NormalError(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	oldDefault := slog.Default()
	defer func() { slog.SetDefault(oldDefault) }()
	slog.SetDefault(slog.New(base))

	ctx := logging.WithRequestID(context.Background(), "req-123")
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	testErr := context.DeadlineExceeded
	writeServerError(rec, req, testErr, "Internal server error", "test", "operation")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rec.Code)
	}

	var body ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if body.Error != "internal_error" {
		t.Errorf("Expected error code 'internal_error', got %q", body.Error)
	}
	if body.Message != "Internal server error" {
		t.Errorf("Expected message 'Internal server error', got %q", body.Message)
	}

	var logEntry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to unmarshal log: %v", err)
	}
	if logEntry["level"] != "ERROR" {
		t.Errorf("Expected log level ERROR, got %v", logEntry["level"])
	}
	if logEntry["component"] != "test" {
		t.Errorf("Expected component 'test', got %v", logEntry["component"])
	}
	if logEntry["operation"] != "operation" {
		t.Errorf("Expected operation 'operation', got %v", logEntry["operation"])
	}
}

func TestWriteServerError_ClientDisconnected(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	oldDefault := slog.Default()
	defer func() { slog.SetDefault(oldDefault) }()
	slog.SetDefault(slog.New(base))

	ctxWithCancel, cancel := context.WithCancel(context.Background())
	cancel()

	ctx := logging.WithRequestID(ctxWithCancel, "req-456")
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	testErr := context.Canceled
	writeServerError(rec, req, testErr, "Internal server error", "test", "operation")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rec.Code)
	}

	var body ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if body.Error != "internal_error" {
		t.Errorf("Expected error code 'internal_error', got %q", body.Error)
	}

	var logEntry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to unmarshal log: %v", err)
	}
	if logEntry["level"] != "INFO" {
		t.Errorf("Expected log level INFO for client_disconnected, got %v", logEntry["level"])
	}
	if logEntry["msg"] != "client_disconnected" {
		t.Errorf("Expected msg 'client_disconnected', got %v", logEntry["msg"])
	}
	if logEntry["component"] != "test" {
		t.Errorf("Expected component 'test', got %v", logEntry["component"])
	}
	if logEntry["operation"] != "operation" {
		t.Errorf("Expected operation 'operation', got %v", logEntry["operation"])
	}
}
