package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
)

// LobService handles Lob API interactions for address verification and postcards
// Lob uses a single API with Basic Authentication
// Key privacy advantage: Lob auto-deletes address data after 90 days
type LobService struct {
	apiKey        string
	baseURL       string
	returnAddress *returnAddress
	client        *http.Client
}

// NewLobService creates a new Lob service
func NewLobService(cfg *config.LobConfig) *LobService {
	return &LobService{
		apiKey:  cfg.APIKey,
		baseURL: cfg.BaseURL,
		returnAddress: &returnAddress{
			Name:    cfg.ReturnName,
			Line1:   cfg.ReturnLine1,
			City:    cfg.ReturnCity,
			State:   cfg.ReturnState,
			Zip:     cfg.ReturnZip,
			Country: cfg.ReturnCountry,
		},
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// basicAuth returns the Basic Auth header value for Lob API
// Lob uses the API key as the username with an empty password
func (s *LobService) basicAuth() string {
	auth := s.apiKey + ":"
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
}

// isTestKey returns true if the API key is a Lob test key
func (s *LobService) isTestKey() bool {
	return strings.HasPrefix(s.apiKey, "test_")
}

// isRealLobAPI returns true if the base URL is the real Lob API
// Used to distinguish between real Lob API and mock test servers
func (s *LobService) isRealLobAPI() bool {
	return strings.Contains(s.baseURL, "api.lob.com")
}

// shouldUseMockMode returns true if we should use mock mode instead of calling the API
// Mock mode is used when:
// - No API key is configured
// - A test key is used with the real Lob API (test keys return "undeliverable" for real addresses)
// We DON'T use mock mode when a test key is used with a mock server (for unit tests)
func (s *LobService) shouldUseMockMode() bool {
	if s.apiKey == "" {
		return true
	}
	// Only use mock mode for test keys when hitting the real Lob API
	// This allows unit tests with mock servers to still exercise the API code paths
	return s.isTestKey() && s.isRealLobAPI()
}

// ValidateAddress validates an address using Lob US Verification API
// NOTE: Address is processed in memory only and NEVER stored by our system
// Lob retains data for 90 days then auto-deletes (better than Postgrid's indefinite retention)
func (s *LobService) ValidateAddress(ctx context.Context, address *models.Address) (*AddressValidationResult, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "POST lob /us_verifications"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	// Use mock mode for development (no key or test key hitting real Lob API)
	// Lob test keys don't validate real addresses - they return "undeliverable" for everything
	// except special test addresses. For development convenience, we use mock mode.
	if s.shouldUseMockMode() {
		if s.isTestKey() {
			slog.Info("lob test api key detected, using mock address validation")
		}
		// Return mock result for development
		return &AddressValidationResult{
			IsDeliverable:       true,
			Deliverability:      "deliverable",
			IsPOBox:             isPOBoxAddress(address.Line1),
			IsCMRA:              false,
			StandardizedAddress: address,
		}, nil
	}

	// Prepare request body for Lob US Verification
	reqBody := map[string]interface{}{
		"primary_line": address.Line1,
		"city":         address.City,
		"state":        address.State,
		"zip_code":     address.PostalCode,
	}

	// Add secondary line if present
	if address.Line2 != "" {
		reqBody["secondary_line"] = address.Line2
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/us_verifications", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", s.basicAuth())

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := readLimitedBody(resp.Body, maxSmallResponseSize)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s", string(body))
	}

	// Parse Lob response
	var apiResp struct {
		ID                     string `json:"id"`
		Deliverability         string `json:"deliverability"` // "deliverable", "deliverable_missing_unit", "undeliverable", etc.
		DeliverabilityAnalysis struct {
			DPV_CMRA         string `json:"dpv_cmra"`         // "Y" = CMRA, "N" = not CMRA
			DPV_Confirmation string `json:"dpv_confirmation"` // "Y", "S", "D", "N"
		} `json:"deliverability_analysis"`
		Components struct {
			PrimaryNumber       string `json:"primary_number"`
			StreetName          string `json:"street_name"`
			StreetSuffix        string `json:"street_suffix"`
			SecondaryDesignator string `json:"secondary_designator"`
			SecondaryNumber     string `json:"secondary_number"`
			City                string `json:"city"`
			State               string `json:"state"`
			ZipCode             string `json:"zip_code"`
			ZipCodePlus4        string `json:"zip_code_plus_4"`
			ZipCodeType         string `json:"zip_code_type"` // "standard", "po_box", "unique", "military"
			AddressType         string `json:"address_type"`  // "residential", "commercial", or empty
			RecordType          string `json:"record_type"`   // "street", "highrise", "po_box", etc.
		} `json:"components"`
		PrimaryLine   string `json:"primary_line"`
		SecondaryLine string `json:"secondary_line"`
		LastLine      string `json:"last_line"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Determine deliverability - Lob uses specific statuses
	// "deliverable" and "deliverable_missing_unit" are considered deliverable
	isDeliverable := apiResp.Deliverability == "deliverable" ||
		apiResp.Deliverability == "deliverable_missing_unit" ||
		apiResp.Deliverability == "deliverable_unnecessary_unit" ||
		apiResp.Deliverability == "deliverable_incorrect_unit"

	// Check for PO Box - Lob indicates this in zip_code_type or record_type
	isPOBox := apiResp.Components.ZipCodeType == "po_box" ||
		apiResp.Components.RecordType == "po_box" ||
		isPOBoxAddress(apiResp.PrimaryLine)

	// Check for CMRA (Commercial Mail Receiving Agency) - e.g., UPS Store, Mailboxes Etc.
	isCMRA := apiResp.DeliverabilityAnalysis.DPV_CMRA == "Y"

	// Check for commercial address - Lob returns address_type in components
	// Values: "residential", "commercial", or empty string if unknown
	addressType := strings.ToLower(apiResp.Components.AddressType)
	isCommercial := addressType == "commercial"

	// Build standardized address from Lob's normalized components
	standardized := &models.Address{
		Line1:      apiResp.PrimaryLine,
		Line2:      apiResp.SecondaryLine,
		City:       apiResp.Components.City,
		State:      apiResp.Components.State,
		PostalCode: apiResp.Components.ZipCode,
		Country:    "US",
	}
	if apiResp.Components.ZipCodePlus4 != "" {
		standardized.PostalCode = apiResp.Components.ZipCode + "-" + apiResp.Components.ZipCodePlus4
	}

	result := &AddressValidationResult{
		IsDeliverable:       isDeliverable,
		Deliverability:      apiResp.Deliverability,
		IsPOBox:             isPOBox,
		IsCMRA:              isCMRA,
		IsCommercial:        isCommercial,
		AddressType:         addressType,
		StandardizedAddress: standardized,
	}

	if !result.IsDeliverable {
		result.Reason = fmt.Sprintf("Address verification failed: %s", apiResp.Deliverability)
	}

	return result, nil
}

// SendPostcard sends a verification postcard via Lob Postcards API
// NOTE: Address is sent to Lob but NEVER stored by our system
// Lob retains data for 90 days then auto-deletes
func (s *LobService) SendPostcard(ctx context.Context, address *models.Address, verificationCode, postcardRef string) (string, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "POST lob /postcards"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	// Use mock mode for development (no key or test key hitting real Lob API)
	// Test keys create test postcards that aren't mailed, but for simplicity
	// in development we use mock mode to avoid Lob API calls entirely.
	if s.shouldUseMockMode() {
		if s.isTestKey() {
			slog.Info("lob test api key detected, using mock postcard")
		} else {
			slog.Info("lob api key not configured, using mock mode")
		}
		return "mock_lob_" + verificationCode, nil
	}

	// Verify return address is configured
	if s.returnAddress == nil || s.returnAddress.Line1 == "" {
		return "", fmt.Errorf("return address not configured: LOB_RETURN_ADDRESS environment variable required")
	}

	// Normalize country code
	country := strings.ToUpper(strings.TrimSpace(address.Country))
	if country == "" {
		country = "US"
	}

	returnCountry := strings.ToUpper(strings.TrimSpace(s.returnAddress.Country))
	if returnCountry == "" {
		returnCountry = "US"
	}

	// Build line2 for secondary address component
	line2 := address.Line2
	if line2 == "" {
		line2 = ""
	}

	// Prepare postcard request - Lob API format
	reqBody := map[string]interface{}{
		"description": "Address verification postcard",
		"to": map[string]interface{}{
			"name":            "Resident",
			"address_line1":   address.Line1,
			"address_line2":   line2,
			"address_city":    address.City,
			"address_state":   address.State,
			"address_zip":     address.PostalCode,
			"address_country": country,
		},
		"from": map[string]interface{}{
			"name":            s.returnAddress.Name,
			"address_line1":   s.returnAddress.Line1,
			"address_city":    s.returnAddress.City,
			"address_state":   s.returnAddress.State,
			"address_zip":     s.returnAddress.Zip,
			"address_country": returnCountry,
		},
		"size": "4x6",
		"front": fmt.Sprintf(`
			<html>
			<body style="font-family: Arial, sans-serif; padding: 20px;">
				<h2>Community Rapid Response</h2>
				<h3>Address Verification</h3>
				<p style="font-size: 12px; color: #666;">REF: %s</p>
				<p>Your verification code:</p>
				<div style="font-size: 24px; font-weight: bold; letter-spacing: 2px; padding: 10px; background: #f0f0f0; text-align: center;">
					%s
				</div>
				<p style="margin-top: 20px;">Enter this code at communityrapidresponse.net to complete verification.</p>
			</body>
			</html>
		`, postcardRef, verificationCode),
		"back": `
			<html>
			<body style="font-family: Arial, sans-serif; margin: 0; padding: 20px; font-size: 13px; line-height: 1.5;">
				<p style="margin: 0 0 10px 0; font-size: 16px;"><strong>What is Community Rapid Response?</strong></p>
				<p style="margin: 0 0 10px 0;">A community platform that connects verified neighbors through secure Signal group chats.</p>
				<p style="margin: 0 0 10px 0;">Your address is not stored in our database. It was shared with our mailing partner, Lob, who handles delivery and automatically deletes address data after 90 days.</p>
				<p style="margin: 0;">Questions? Visit communityrapidresponse.net</p>
			</body>
			</html>
		`,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/postcards", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", s.basicAuth())

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := readLimitedBody(resp.Body, maxSmallResponseSize)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		// Check if this is an API key error - fall back to mock mode for development
		if strings.Contains(string(body), "invalid_api_key") || strings.Contains(string(body), "unauthorized") {
			slog.Warn("lob api key is invalid, using mock mode")
			return "mock_lob_" + verificationCode, nil
		}
		return "", fmt.Errorf("API error: %s", string(body))
	}

	// Parse response to get postcard ID
	var apiResp struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return apiResp.ID, nil
}
