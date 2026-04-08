package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

func TestPostgridService_ValidateAddress_MockMode(t *testing.T) {
	// Without API key, service runs in mock mode
	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "",
		AddressVerificationBaseURL: "https://api.postgrid.com/v1",
		PrintMailAPIKey:            "",
		PrintMailBaseURL:           "https://api.postgrid.com/print-mail/v1",
	})

	ctx := context.Background()

	t.Run("validates regular address", func(t *testing.T) {
		address := &models.Address{
			Line1:      "123 Main St",
			City:       "San Francisco",
			State:      "CA",
			PostalCode: "94102",
		}

		result, err := service.ValidateAddress(ctx, address)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !result.IsDeliverable {
			t.Error("Expected address to be deliverable in mock mode")
		}
		if result.IsPOBox {
			t.Error("Regular address should not be flagged as PO Box")
		}
	})

	t.Run("detects PO Box in mock mode", func(t *testing.T) {
		address := &models.Address{
			Line1:      "PO Box 123",
			City:       "San Francisco",
			State:      "CA",
			PostalCode: "94102",
		}

		result, err := service.ValidateAddress(ctx, address)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !result.IsPOBox {
			t.Error("Expected PO Box to be detected")
		}
	})

	t.Run("detects P.O. Box variant", func(t *testing.T) {
		address := &models.Address{
			Line1:      "P.O. Box 456",
			City:       "San Francisco",
			State:      "CA",
			PostalCode: "94102",
		}

		result, err := service.ValidateAddress(ctx, address)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !result.IsPOBox {
			t.Error("Expected P.O. Box variant to be detected")
		}
	})
}

func TestPostgridService_ValidateAddress_WithAPI(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/addver/verifications" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test_api_key" {
			t.Error("Missing or incorrect API key")
		}

		// Response matches actual Postgrid Address Verification API format
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"status":          "verified",
				"addressType":     "residential",
				"cmra":            false,
				"line1":           "123 MAIN ST",
				"city":            "SAN FRANCISCO",
				"provinceOrState": "CA",
				"postalOrZip":     "94102",
				"country":         "us",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "test_api_key",
		AddressVerificationBaseURL: server.URL,
		PrintMailAPIKey:            "",
		PrintMailBaseURL:           "https://api.postgrid.com/print-mail/v1",
	})

	address := &models.Address{
		Line1:      "123 Main St",
		City:       "San Francisco",
		State:      "CA",
		PostalCode: "94102",
	}

	result, err := service.ValidateAddress(context.Background(), address)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !result.IsDeliverable {
		t.Error("Expected address to be deliverable")
	}
	if result.IsPOBox {
		t.Error("Should not be flagged as PO Box")
	}
	if result.IsCMRA {
		t.Error("Should not be flagged as CMRA")
	}
}

func TestPostgridService_ValidateAddress_POBox(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"status":      "verified",
				"addressType": "po_box",
				"cmra":        false,
				"line1":       "PO BOX 123",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "test_api_key",
		AddressVerificationBaseURL: server.URL,
		PrintMailAPIKey:            "",
		PrintMailBaseURL:           "https://api.postgrid.com/print-mail/v1",
	})

	address := &models.Address{
		Line1:      "PO Box 123",
		City:       "San Francisco",
		State:      "CA",
		PostalCode: "94102",
	}

	result, err := service.ValidateAddress(context.Background(), address)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !result.IsPOBox {
		t.Error("Expected PO Box to be detected")
	}
}

func TestPostgridService_ValidateAddress_CMRA(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"status":      "verified",
				"addressType": "commercial",
				"cmra":        true,
				"line1":       "123 MAIN ST #PMB456",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "test_api_key",
		AddressVerificationBaseURL: server.URL,
		PrintMailAPIKey:            "",
		PrintMailBaseURL:           "https://api.postgrid.com/print-mail/v1",
	})

	address := &models.Address{
		Line1:      "123 Main St #PMB456",
		City:       "San Francisco",
		State:      "CA",
		PostalCode: "94102",
	}

	result, err := service.ValidateAddress(context.Background(), address)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !result.IsCMRA {
		t.Error("Expected CMRA to be detected")
	}
}

func TestPostgridService_ValidateAddress_Commercial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"status":      "verified",
				"addressType": "commercial",
				"cmra":        false,
				"line1":       "456 CORPORATE BLVD",
				"city":        "SAN FRANCISCO",
				"provinceOrState": "CA",
				"postalOrZip": "94102",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "test_api_key",
		AddressVerificationBaseURL: server.URL,
		PrintMailAPIKey:            "",
		PrintMailBaseURL:           "https://api.postgrid.com/print-mail/v1",
	})

	address := &models.Address{
		Line1:      "456 Corporate Blvd",
		City:       "San Francisco",
		State:      "CA",
		PostalCode: "94102",
	}

	result, err := service.ValidateAddress(context.Background(), address)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !result.IsCommercial {
		t.Error("Expected commercial address to be detected")
	}
	if result.AddressType != "commercial" {
		t.Errorf("Expected AddressType 'commercial', got '%s'", result.AddressType)
	}
	if result.IsCMRA {
		t.Error("Did not expect CMRA flag for regular commercial address")
	}
}

func TestPostgridService_ValidateAddress_Residential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"status":      "verified",
				"addressType": "residential",
				"cmra":        false,
				"line1":       "123 MAIN ST",
				"city":        "SAN FRANCISCO",
				"provinceOrState": "CA",
				"postalOrZip": "94102",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "test_api_key",
		AddressVerificationBaseURL: server.URL,
		PrintMailAPIKey:            "",
		PrintMailBaseURL:           "https://api.postgrid.com/print-mail/v1",
	})

	address := &models.Address{
		Line1:      "123 Main St",
		City:       "San Francisco",
		State:      "CA",
		PostalCode: "94102",
	}

	result, err := service.ValidateAddress(context.Background(), address)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.IsCommercial {
		t.Error("Did not expect residential address to be marked commercial")
	}
	if result.AddressType != "residential" {
		t.Errorf("Expected AddressType 'residential', got '%s'", result.AddressType)
	}
}

func TestPostgridService_ValidateAddress_Undeliverable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"status":      "failed",
				"addressType": "",
				"cmra":        false,
				"errors": map[string][]string{
					"address": {"Address not found"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "test_api_key",
		AddressVerificationBaseURL: server.URL,
		PrintMailAPIKey:            "",
		PrintMailBaseURL:           "https://api.postgrid.com/print-mail/v1",
	})

	address := &models.Address{
		Line1:      "99999 Nonexistent St",
		City:       "Fake City",
		State:      "XX",
		PostalCode: "00000",
	}

	result, err := service.ValidateAddress(context.Background(), address)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.IsDeliverable {
		t.Error("Expected address to be undeliverable")
	}
}

func TestPostgridService_SendPostcard_MockMode(t *testing.T) {
	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "",
		AddressVerificationBaseURL: "https://api.postgrid.com/v1",
		PrintMailAPIKey:            "",
		PrintMailBaseURL:           "https://api.postgrid.com/print-mail/v1",
		ReturnName:                 "Test Org",
		ReturnLine1:                "123 Test St",
		ReturnCity:                 "Test City",
		ReturnState:                "CA",
		ReturnZip:                  "94102",
		ReturnCountry:              "US",
	})

	address := &models.Address{
		Line1:      "123 Main St",
		City:       "San Francisco",
		State:      "CA",
		PostalCode: "94102",
	}

	requestID, err := service.SendPostcard(context.Background(), address, "ABC123", "REF01")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if requestID == "" {
		t.Error("Expected non-empty request ID")
	}
	if requestID != "mock_postgrid_ABC123" {
		t.Errorf("Expected mock request ID format, got '%s'", requestID)
	}
}

func TestPostgridService_SendPostcard_WithAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/postcards" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test_print_api_key" {
			t.Error("Missing or incorrect Print Mail API key")
		}

		// Verify request body contains ref and verification code in template
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		frontHTML, _ := reqBody["frontHTML"].(string)
		if !strings.Contains(frontHTML, "REF: REF02") {
			t.Error("Front template missing postcard reference code")
		}
		if !strings.Contains(frontHTML, "VERIFY123") {
			t.Error("Front template missing verification code")
		}

		response := map[string]interface{}{
			"data": map[string]interface{}{
				"id": "postcard_test123",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "",
		AddressVerificationBaseURL: "https://api.postgrid.com/v1",
		PrintMailAPIKey:            "test_print_api_key",
		PrintMailBaseURL:           server.URL,
		ReturnName:                 "Test Org",
		ReturnLine1:                "123 Test St",
		ReturnCity:                 "Test City",
		ReturnState:                "CA",
		ReturnZip:                  "94102",
		ReturnCountry:              "US",
	})

	address := &models.Address{
		Line1:      "123 Main St",
		City:       "San Francisco",
		State:      "CA",
		PostalCode: "94102",
	}

	requestID, err := service.SendPostcard(context.Background(), address, "VERIFY123", "REF02")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if requestID != "postcard_test123" {
		t.Errorf("Expected 'postcard_test123', got '%s'", requestID)
	}
}

func TestIsPOBoxAddress(t *testing.T) {
	tests := []struct {
		line     string
		expected bool
	}{
		{"PO Box 123", true},
		{"P.O. Box 456", true},
		{"P.O.Box 789", true},
		{"po box 101", true},
		{"P O Box 202", true},
		{"123 Main St", false},
		{"123 Postal Road", false},
		{"Box Canyon Drive", false},
		{"POBox 303", true},
	}

	for _, tt := range tests {
		result := isPOBoxAddress(tt.line)
		if result != tt.expected {
			t.Errorf("isPOBoxAddress(%q) = %v, expected %v", tt.line, result, tt.expected)
		}
	}
}

func TestPostgridService_SendPostcard_MissingReturnAddress(t *testing.T) {
	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "",
		AddressVerificationBaseURL: "https://api.postgrid.com/v1",
		PrintMailAPIKey:            "test_print_api_key",
		PrintMailBaseURL:           "https://api.postgrid.com/print-mail/v1",
		// No return address configured
	})

	address := &models.Address{
		Line1:      "123 Main St",
		City:       "San Francisco",
		State:      "CA",
		PostalCode: "94102",
	}

	_, err := service.SendPostcard(context.Background(), address, "ABC123", "REF03")
	if err == nil {
		t.Error("Expected error when return address is not configured")
	}
}

func TestPostgridService_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "Internal server error"}`))
	}))
	defer server.Close()

	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "test_api_key",
		AddressVerificationBaseURL: server.URL,
		PrintMailAPIKey:            "",
		PrintMailBaseURL:           "https://api.postgrid.com/print-mail/v1",
	})

	address := &models.Address{
		Line1: "123 Main St",
		City:  "San Francisco",
		State: "CA",
	}

	_, err := service.ValidateAddress(context.Background(), address)
	if err == nil {
		t.Error("Expected error for API failure")
	}
}

func TestPostgridService_ValidateAddress_CorrectedStatus(t *testing.T) {
	// Postgrid returns "corrected" for addresses that are deliverable but were adjusted
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"status":          "corrected",
				"addressType":     "residential",
				"cmra":            false,
				"line1":           "123 MAIN STREET",
				"city":            "SAN FRANCISCO",
				"provinceOrState": "CA",
				"postalOrZip":     "94102-1234",
				"country":         "us",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "test_api_key",
		AddressVerificationBaseURL: server.URL,
		PrintMailAPIKey:            "",
		PrintMailBaseURL:           "https://api.postgrid.com/print-mail/v1",
	})

	address := &models.Address{
		Line1:      "123 Main St",
		City:       "San Francisco",
		State:      "CA",
		PostalCode: "94102",
	}

	result, err := service.ValidateAddress(context.Background(), address)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !result.IsDeliverable {
		t.Error("Expected corrected address to be deliverable")
	}
	if result.Deliverability != "corrected" {
		t.Errorf("Expected deliverability 'corrected', got '%s'", result.Deliverability)
	}
	// Standardized address should reflect the corrected values
	if result.StandardizedAddress.Line1 != "123 MAIN STREET" {
		t.Errorf("Expected standardized line1 '123 MAIN STREET', got '%s'", result.StandardizedAddress.Line1)
	}
	if result.StandardizedAddress.PostalCode != "94102-1234" {
		t.Errorf("Expected corrected postal code '94102-1234', got '%s'", result.StandardizedAddress.PostalCode)
	}
}

func TestPostgridService_ValidateAddress_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer server.Close()

	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "test_api_key",
		AddressVerificationBaseURL: server.URL,
		PrintMailAPIKey:            "",
		PrintMailBaseURL:           "https://api.postgrid.com/print-mail/v1",
	})

	address := &models.Address{
		Line1: "123 Main St",
		City:  "San Francisco",
		State: "CA",
	}

	_, err := service.ValidateAddress(context.Background(), address)
	if err == nil {
		t.Error("Expected error for malformed JSON response")
	}
	if !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("Expected 'failed to parse response' error, got: %v", err)
	}
}

func TestPostgridService_SendPostcard_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "",
		AddressVerificationBaseURL: "https://api.postgrid.com/v1",
		PrintMailAPIKey:            "test_print_api_key",
		PrintMailBaseURL:           server.URL,
		ReturnName:                 "Test Org",
		ReturnLine1:                "123 Test St",
		ReturnCity:                 "Test City",
		ReturnState:                "CA",
		ReturnZip:                  "94102",
		ReturnCountry:              "US",
	})

	address := &models.Address{
		Line1:      "123 Main St",
		City:       "San Francisco",
		State:      "CA",
		PostalCode: "94102",
	}

	_, err := service.SendPostcard(context.Background(), address, "ABC123", "REF04")
	if err == nil {
		t.Error("Expected error for API failure")
	}
	if !strings.Contains(err.Error(), "API error") {
		t.Errorf("Expected 'API error' in error message, got: %v", err)
	}
}

func TestPostgridService_SendPostcard_InvalidAPIKeyFallback(t *testing.T) {
	// When Postgrid returns invalid_api_key, the service falls back to mock mode
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "invalid_api_key"}`))
	}))
	defer server.Close()

	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "",
		AddressVerificationBaseURL: "https://api.postgrid.com/v1",
		PrintMailAPIKey:            "test_print_api_key",
		PrintMailBaseURL:           server.URL,
		ReturnName:                 "Test Org",
		ReturnLine1:                "123 Test St",
		ReturnCity:                 "Test City",
		ReturnState:                "CA",
		ReturnZip:                  "94102",
		ReturnCountry:              "US",
	})

	address := &models.Address{
		Line1:      "123 Main St",
		City:       "San Francisco",
		State:      "CA",
		PostalCode: "94102",
	}

	requestID, err := service.SendPostcard(context.Background(), address, "ABC123", "REF05")
	if err != nil {
		t.Fatalf("Expected fallback to mock mode, got error: %v", err)
	}
	if requestID != "mock_postgrid_ABC123" {
		t.Errorf("Expected mock request ID, got '%s'", requestID)
	}
}

func TestPostgridService_SendPostcard_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer server.Close()

	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "",
		AddressVerificationBaseURL: "https://api.postgrid.com/v1",
		PrintMailAPIKey:            "test_print_api_key",
		PrintMailBaseURL:           server.URL,
		ReturnName:                 "Test Org",
		ReturnLine1:                "123 Test St",
		ReturnCity:                 "Test City",
		ReturnState:                "CA",
		ReturnZip:                  "94102",
		ReturnCountry:              "US",
	})

	address := &models.Address{
		Line1:      "123 Main St",
		City:       "San Francisco",
		State:      "CA",
		PostalCode: "94102",
	}

	_, err := service.SendPostcard(context.Background(), address, "ABC123", "REF06")
	if err == nil {
		t.Error("Expected error for malformed JSON response")
	}
	if !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("Expected 'failed to parse response' error, got: %v", err)
	}
}

func TestPostgridService_ValidateAddress_ErrorReasonExtraction(t *testing.T) {
	// Test that specific error reasons are extracted from Postgrid's error response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"status":      "failed",
				"addressType": "",
				"cmra":        false,
				"errors": map[string][]string{
					"city": {"City not found in state"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewPostgridService(&config.PostgridConfig{
		AddressVerificationAPIKey:  "test_api_key",
		AddressVerificationBaseURL: server.URL,
		PrintMailAPIKey:            "",
		PrintMailBaseURL:           "https://api.postgrid.com/print-mail/v1",
	})

	result, err := service.ValidateAddress(context.Background(), &models.Address{
		Line1:      "123 Main St",
		City:       "Faketown",
		State:      "CA",
		PostalCode: "00000",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.IsDeliverable {
		t.Error("Expected address to be undeliverable")
	}
	if !strings.Contains(result.Reason, "City not found") {
		t.Errorf("Expected reason to contain 'City not found', got '%s'", result.Reason)
	}
}
