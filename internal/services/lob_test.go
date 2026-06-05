package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// getLobTestDataDir returns the path to the lob testdata directory
func getLobTestDataDir() string {
	return filepath.Join(getTestDataDir(), "..", "lob")
}

// loadLobFixture loads a Lob JSON fixture file
func loadLobFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(getLobTestDataDir(), name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to load fixture %s: %v", name, err)
	}
	return data
}

func TestLobService_ValidateAddress_MockMode(t *testing.T) {
	// Without API key, service runs in mock mode
	service := NewLobService(&config.LobConfig{
		APIKey:  "",
		BaseURL: "https://api.lob.com/v1",
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

func TestLobService_ValidateAddress_WithAPI(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/us_verifications" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}

		// Verify Basic Auth header
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Basic ") {
			t.Error("Missing or incorrect Authorization header")
		}

		// Response matches Lob US Verification API format
		response := map[string]interface{}{
			"id":             "us_ver_abc123",
			"deliverability": "deliverable",
			"deliverability_analysis": map[string]interface{}{
				"dpv_cmra":         "N",
				"dpv_confirmation": "Y",
			},
			"components": map[string]interface{}{
				"primary_number":  "123",
				"street_name":     "MAIN",
				"street_suffix":   "ST",
				"city":            "SAN FRANCISCO",
				"state":           "CA",
				"zip_code":        "94102",
				"zip_code_plus_4": "1234",
				"zip_code_type":   "standard",
			},
			"primary_line":   "123 MAIN ST",
			"secondary_line": "",
			"last_line":      "SAN FRANCISCO CA 94102-1234",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewLobService(&config.LobConfig{
		APIKey:  "test_api_key",
		BaseURL: server.URL,
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
	// Check standardized address includes ZIP+4
	if result.StandardizedAddress.PostalCode != "94102-1234" {
		t.Errorf("Expected ZIP+4 format, got %s", result.StandardizedAddress.PostalCode)
	}
}

func TestLobService_ValidateAddress_POBox(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"id":             "us_ver_abc123",
			"deliverability": "deliverable",
			"deliverability_analysis": map[string]interface{}{
				"dpv_cmra":         "N",
				"dpv_confirmation": "Y",
			},
			"components": map[string]interface{}{
				"city":          "SAN FRANCISCO",
				"state":         "CA",
				"zip_code":      "94102",
				"zip_code_type": "po_box", // Lob indicates PO Box via zip_code_type
			},
			"primary_line": "PO BOX 123",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewLobService(&config.LobConfig{
		APIKey:  "test_api_key",
		BaseURL: server.URL,
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

func TestLobService_ValidateAddress_CMRA(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"id":             "us_ver_abc123",
			"deliverability": "deliverable",
			"deliverability_analysis": map[string]interface{}{
				"dpv_cmra":         "Y", // CMRA detected
				"dpv_confirmation": "Y",
			},
			"components": map[string]interface{}{
				"city":          "SAN FRANCISCO",
				"state":         "CA",
				"zip_code":      "94102",
				"zip_code_type": "standard",
			},
			"primary_line": "123 MAIN ST #PMB456",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewLobService(&config.LobConfig{
		APIKey:  "test_api_key",
		BaseURL: server.URL,
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

func TestLobService_ValidateAddress_Commercial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"id":             "us_ver_abc123",
			"deliverability": "deliverable",
			"deliverability_analysis": map[string]interface{}{
				"dpv_cmra":         "N",
				"dpv_confirmation": "Y",
			},
			"components": map[string]interface{}{
				"city":          "SAN FRANCISCO",
				"state":         "CA",
				"zip_code":      "94102",
				"zip_code_type": "standard",
				"address_type":  "commercial", // Commercial address
			},
			"primary_line": "456 CORPORATE BLVD",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewLobService(&config.LobConfig{
		APIKey:  "test_api_key",
		BaseURL: server.URL,
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
}

func TestLobService_ValidateAddress_Residential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"id":             "us_ver_abc123",
			"deliverability": "deliverable",
			"deliverability_analysis": map[string]interface{}{
				"dpv_cmra":         "N",
				"dpv_confirmation": "Y",
			},
			"components": map[string]interface{}{
				"city":          "SAN FRANCISCO",
				"state":         "CA",
				"zip_code":      "94102",
				"zip_code_type": "standard",
				"address_type":  "residential", // Residential address
			},
			"primary_line": "123 MAIN ST",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewLobService(&config.LobConfig{
		APIKey:  "test_api_key",
		BaseURL: server.URL,
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

func TestLobService_ValidateAddress_Undeliverable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"id":             "us_ver_abc123",
			"deliverability": "undeliverable",
			"deliverability_analysis": map[string]interface{}{
				"dpv_cmra":         "N",
				"dpv_confirmation": "N",
			},
			"components": map[string]interface{}{
				"zip_code_type": "standard",
			},
			"primary_line": "99999 NONEXISTENT ST",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewLobService(&config.LobConfig{
		APIKey:  "test_api_key",
		BaseURL: server.URL,
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
	if result.Reason == "" {
		t.Error("Expected reason for undeliverable address")
	}
}

func TestLobService_SendPostcard_MockMode(t *testing.T) {
	service := NewLobService(&config.LobConfig{
		APIKey:        "",
		BaseURL:       "https://api.lob.com/v1",
		ReturnName:    "Test Org",
		ReturnLine1:   "123 Test St",
		ReturnCity:    "Test City",
		ReturnState:   "CA",
		ReturnZip:     "94102",
		ReturnCountry: "US",
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
	if requestID != "mock_lob_ABC123" {
		t.Errorf("Expected mock request ID format, got '%s'", requestID)
	}
}

func TestLobService_SendPostcard_WithAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/postcards" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}

		// Verify Basic Auth header
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Basic ") {
			t.Error("Missing or incorrect Authorization header")
		}

		// Verify request body contains expected fields
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}

		// Check required fields
		if _, ok := reqBody["to"]; !ok {
			t.Error("Missing 'to' field in request")
		}
		if _, ok := reqBody["from"]; !ok {
			t.Error("Missing 'from' field in request")
		}
		if _, ok := reqBody["front"]; !ok {
			t.Error("Missing 'front' field in request")
		}
		if _, ok := reqBody["back"]; !ok {
			t.Error("Missing 'back' field in request")
		}

		// Verify the front template contains the ref and verification code
		front, _ := reqBody["front"].(string)
		if !strings.Contains(front, "REF: REF02") {
			t.Error("Front template missing postcard reference code")
		}
		if !strings.Contains(front, "VERIFY123") {
			t.Error("Front template missing verification code")
		}

		response := map[string]interface{}{
			"id": "psc_test123",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := NewLobService(&config.LobConfig{
		APIKey:        "test_api_key",
		BaseURL:       server.URL,
		ReturnName:    "Test Org",
		ReturnLine1:   "123 Test St",
		ReturnCity:    "Test City",
		ReturnState:   "CA",
		ReturnZip:     "94102",
		ReturnCountry: "US",
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

	if requestID != "psc_test123" {
		t.Errorf("Expected 'psc_test123', got '%s'", requestID)
	}
}

func TestLobService_SendPostcard_MissingReturnAddress(t *testing.T) {
	// Use a mock server so we don't trigger mock mode (test key + real Lob URL = mock mode)
	// We need to hit a non-Lob URL to test the return address validation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This should not be reached - the return address check happens first
		t.Error("Request should not have been made - return address check should have failed first")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	service := NewLobService(&config.LobConfig{
		APIKey:  "live_api_key", // Use live key prefix to avoid mock mode
		BaseURL: server.URL,
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

func TestLobService_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": {"message": "Internal server error"}}`))
	}))
	defer server.Close()

	service := NewLobService(&config.LobConfig{
		APIKey:  "test_api_key",
		BaseURL: server.URL,
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

func TestLobService_DeliverabilityVariants(t *testing.T) {
	tests := []struct {
		name              string
		deliverability    string
		expectDeliverable bool
	}{
		{"deliverable", "deliverable", true},
		{"deliverable_missing_unit", "deliverable_missing_unit", true},
		{"deliverable_unnecessary_unit", "deliverable_unnecessary_unit", true},
		{"deliverable_incorrect_unit", "deliverable_incorrect_unit", true},
		{"undeliverable", "undeliverable", false},
		{"no_match", "no_match", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"id":             "us_ver_abc123",
					"deliverability": tt.deliverability,
					"deliverability_analysis": map[string]interface{}{
						"dpv_cmra":         "N",
						"dpv_confirmation": "Y",
					},
					"components": map[string]interface{}{
						"city":          "TEST CITY",
						"state":         "CA",
						"zip_code":      "94102",
						"zip_code_type": "standard",
					},
					"primary_line": "123 TEST ST",
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			service := NewLobService(&config.LobConfig{
				APIKey:  "test_api_key",
				BaseURL: server.URL,
			})

			result, err := service.ValidateAddress(context.Background(), &models.Address{
				Line1:      "123 Test St",
				City:       "Test City",
				State:      "CA",
				PostalCode: "94102",
			})
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.IsDeliverable != tt.expectDeliverable {
				t.Errorf("Expected IsDeliverable=%v for %s, got %v",
					tt.expectDeliverable, tt.deliverability, result.IsDeliverable)
			}
		})
	}
}

// =============================================================================
// Fixture-based tests using real Lob API responses
// =============================================================================

// TestLobFixture_ResidentialAddress tests parsing of a real Lob API response
// for a residential address (100 Main St, Boston MA)
func TestLobFixture_ResidentialAddress(t *testing.T) {
	fixture := loadLobFixture(t, "residential_address.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	service := NewLobService(&config.LobConfig{
		APIKey:  "test_api_key",
		BaseURL: server.URL,
	})

	result, err := service.ValidateAddress(context.Background(), &models.Address{
		Line1:      "100 Main Street",
		City:       "Boston",
		State:      "MA",
		PostalCode: "02129",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify residential detection
	if result.IsCommercial {
		t.Error("Expected residential address, got commercial")
	}
	if result.AddressType != "residential" {
		t.Errorf("Expected AddressType 'residential', got '%s'", result.AddressType)
	}

	// Verify deliverability (deliverable_missing_unit is still deliverable)
	if !result.IsDeliverable {
		t.Error("Expected address to be deliverable")
	}

	// Verify not a PO Box or CMRA
	if result.IsPOBox {
		t.Error("Did not expect PO Box flag")
	}
	if result.IsCMRA {
		t.Error("Did not expect CMRA flag")
	}

	// Verify standardized address
	if result.StandardizedAddress.City != "BOSTON" {
		t.Errorf("Expected city 'BOSTON', got '%s'", result.StandardizedAddress.City)
	}
	if result.StandardizedAddress.State != "MA" {
		t.Errorf("Expected state 'MA', got '%s'", result.StandardizedAddress.State)
	}
}

// TestLobFixture_CommercialAddress tests parsing of a real Lob API response
// for a commercial address (1600 Pennsylvania Ave NW, Washington DC - The White House)
func TestLobFixture_CommercialAddress(t *testing.T) {
	fixture := loadLobFixture(t, "commercial_address.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	service := NewLobService(&config.LobConfig{
		APIKey:  "test_api_key",
		BaseURL: server.URL,
	})

	result, err := service.ValidateAddress(context.Background(), &models.Address{
		Line1:      "1600 Pennsylvania Avenue NW",
		City:       "Washington",
		State:      "DC",
		PostalCode: "20500",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify commercial detection
	if !result.IsCommercial {
		t.Error("Expected commercial address")
	}
	if result.AddressType != "commercial" {
		t.Errorf("Expected AddressType 'commercial', got '%s'", result.AddressType)
	}

	// Verify deliverability
	if !result.IsDeliverable {
		t.Error("Expected address to be deliverable")
	}

	// Verify not a PO Box or CMRA
	if result.IsPOBox {
		t.Error("Did not expect PO Box flag")
	}
	if result.IsCMRA {
		t.Error("Did not expect CMRA flag")
	}

	// Verify standardized address
	if result.StandardizedAddress.City != "WASHINGTON" {
		t.Errorf("Expected city 'WASHINGTON', got '%s'", result.StandardizedAddress.City)
	}
	if result.StandardizedAddress.PostalCode != "20500-0005" {
		t.Errorf("Expected postal code '20500-0005', got '%s'", result.StandardizedAddress.PostalCode)
	}
}

// TestLobFixture_CMRAAddress tests parsing of a real Lob API response
// for a CMRA address (5201 Great America Pkwy Suite 320, Santa Clara CA)
func TestLobFixture_CMRAAddress(t *testing.T) {
	fixture := loadLobFixture(t, "cmra_address.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	service := NewLobService(&config.LobConfig{
		APIKey:  "test_api_key",
		BaseURL: server.URL,
	})

	result, err := service.ValidateAddress(context.Background(), &models.Address{
		Line1:      "5201 Great America Pkwy",
		Line2:      "Suite 320",
		City:       "Santa Clara",
		State:      "CA",
		PostalCode: "95054",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify CMRA detection
	if !result.IsCMRA {
		t.Error("Expected CMRA flag to be set")
	}

	// CMRA addresses are typically commercial
	if !result.IsCommercial {
		t.Error("Expected CMRA address to be commercial")
	}

	// Verify deliverability
	if !result.IsDeliverable {
		t.Error("Expected address to be deliverable")
	}

	// Verify not a PO Box
	if result.IsPOBox {
		t.Error("Did not expect PO Box flag")
	}

	// Verify standardized address
	if result.StandardizedAddress.City != "SANTA CLARA" {
		t.Errorf("Expected city 'SANTA CLARA', got '%s'", result.StandardizedAddress.City)
	}
}

// TestLobFixture_POBoxAddress tests parsing of a real Lob API response
// for a PO Box address (PO Box 2500, Washington DC)
func TestLobFixture_POBoxAddress(t *testing.T) {
	fixture := loadLobFixture(t, "po_box_address.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	service := NewLobService(&config.LobConfig{
		APIKey:  "test_api_key",
		BaseURL: server.URL,
	})

	result, err := service.ValidateAddress(context.Background(), &models.Address{
		Line1:      "PO Box 2500",
		City:       "Washington",
		State:      "DC",
		PostalCode: "20013",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify PO Box detection (detected via zip_code_type and record_type)
	if !result.IsPOBox {
		t.Error("Expected PO Box flag to be set")
	}

	// PO Boxes are deliverable
	if !result.IsDeliverable {
		t.Error("Expected address to be deliverable")
	}

	// Verify not CMRA
	if result.IsCMRA {
		t.Error("Did not expect CMRA flag")
	}

	// Verify standardized address
	if result.StandardizedAddress.City != "WASHINGTON" {
		t.Errorf("Expected city 'WASHINGTON', got '%s'", result.StandardizedAddress.City)
	}
}

// TestLobFixture_AddressTypeVariants tests all address type variations using real fixtures
func TestLobFixture_AddressTypeVariants(t *testing.T) {
	tests := []struct {
		name              string
		fixture           string
		expectCommercial  bool
		expectPOBox       bool
		expectCMRA        bool
		expectAddressType string
	}{
		{
			name:              "residential_address",
			fixture:           "residential_address.json",
			expectCommercial:  false,
			expectPOBox:       false,
			expectCMRA:        false,
			expectAddressType: "residential",
		},
		{
			name:              "commercial_address",
			fixture:           "commercial_address.json",
			expectCommercial:  true,
			expectPOBox:       false,
			expectCMRA:        false,
			expectAddressType: "commercial",
		},
		{
			name:              "cmra_address",
			fixture:           "cmra_address.json",
			expectCommercial:  true,
			expectPOBox:       false,
			expectCMRA:        true,
			expectAddressType: "commercial",
		},
		{
			name:              "po_box_address",
			fixture:           "po_box_address.json",
			expectCommercial:  false,
			expectPOBox:       true,
			expectCMRA:        false,
			expectAddressType: "residential",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := loadLobFixture(t, tc.fixture)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(fixture)
			}))
			defer server.Close()

			service := NewLobService(&config.LobConfig{
				APIKey:  "test_api_key",
				BaseURL: server.URL,
			})

			result, err := service.ValidateAddress(context.Background(), &models.Address{
				Line1: "Test Address",
			})
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.IsCommercial != tc.expectCommercial {
				t.Errorf("Expected IsCommercial=%v, got %v", tc.expectCommercial, result.IsCommercial)
			}
			if result.IsPOBox != tc.expectPOBox {
				t.Errorf("Expected IsPOBox=%v, got %v", tc.expectPOBox, result.IsPOBox)
			}
			if result.IsCMRA != tc.expectCMRA {
				t.Errorf("Expected IsCMRA=%v, got %v", tc.expectCMRA, result.IsCMRA)
			}
			if result.AddressType != tc.expectAddressType {
				t.Errorf("Expected AddressType='%s', got '%s'", tc.expectAddressType, result.AddressType)
			}
		})
	}
}
