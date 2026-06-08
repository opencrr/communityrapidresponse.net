package services

import (
	"bytes"
	"context"
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

// PostgridService handles Postgrid API interactions
// Postgrid has two separate APIs:
// - Address Verification API for validating addresses
// - Print & Mail API for sending postcards
type PostgridService struct {
	addverAPIKey  string
	addverBaseURL string
	printAPIKey   string
	printBaseURL  string
	returnAddress *returnAddress
	client        *http.Client
}

// returnAddress holds the configured return address for postcards
type returnAddress struct {
	Name    string
	Line1   string
	City    string
	State   string
	Zip     string
	Country string
}

// NewPostgridService creates a new Postgrid service
func NewPostgridService(cfg *config.PostgridConfig) *PostgridService {
	return &PostgridService{
		addverAPIKey:  cfg.AddressVerificationAPIKey,
		addverBaseURL: cfg.AddressVerificationBaseURL,
		printAPIKey:   cfg.PrintMailAPIKey,
		printBaseURL:  cfg.PrintMailBaseURL,
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

// AddressValidationResult represents the result of address validation
type AddressValidationResult struct {
	IsDeliverable       bool
	Deliverability      string
	IsPOBox             bool
	IsCMRA              bool
	IsCommercial        bool   // true if address is a business/commercial address
	AddressType         string // "residential", "commercial", or empty if unknown
	Reason              string
	StandardizedAddress *models.Address
}

// isPOBoxAddress checks if an address line contains a PO Box pattern
func isPOBoxAddress(line string) bool {
	normalized := strings.ToLower(line)
	// Remove periods and extra spaces for normalization
	normalized = strings.ReplaceAll(normalized, ".", "")
	normalized = strings.ReplaceAll(normalized, "  ", " ")
	// Check various patterns: "po box", "pobox", "p o box"
	return strings.Contains(normalized, "po box") ||
		strings.Contains(normalized, "pobox") ||
		strings.Contains(normalized, "p o box")
}

// ValidateAddress validates an address using Postgrid Address Verification API
// NOTE: Address is processed in memory only and NEVER stored
func (s *PostgridService) ValidateAddress(ctx context.Context, address *models.Address) (*AddressValidationResult, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "POST postgrid /addver/verifications"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	if s.addverAPIKey == "" {
		// Return mock result for development
		return &AddressValidationResult{
			IsDeliverable:       true,
			Deliverability:      "deliverable",
			IsPOBox:             isPOBoxAddress(address.Line1),
			IsCMRA:              false,
			StandardizedAddress: address,
		}, nil
	}

	// Prepare request - Postgrid expects address wrapped in "address" object
	reqBody := map[string]interface{}{
		"address": map[string]interface{}{
			"line1":           address.Line1,
			"line2":           address.Line2,
			"city":            address.City,
			"provinceOrState": address.State,
			"postalOrZip":     address.PostalCode,
			"country":         "us",
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.addverBaseURL+"/addver/verifications", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.addverAPIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := readLimitedBody(resp.Body, maxSmallResponseSize)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("API error: %s", string(body))
	}

	// Parse response
	var apiResp struct {
		Status string `json:"status"`
		Data   struct {
			Status          string              `json:"status"` // "verified", "corrected", "failed"
			Line1           string              `json:"line1"`
			Line2           string              `json:"line2"`
			City            string              `json:"city"`
			ProvinceOrState string              `json:"provinceOrState"`
			PostalOrZip     string              `json:"postalOrZip"`
			Country         string              `json:"country"`
			AddressType     string              `json:"addressType"`
			CMRA            bool                `json:"cmra"`
			Errors          map[string][]string `json:"errors"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Status can be "verified", "corrected", or "failed"
	// Both "verified" and "corrected" are considered deliverable
	isDeliverable := apiResp.Data.Status == "verified" || apiResp.Data.Status == "corrected"

	// Detect commercial addresses - Postgrid returns "commercial" or "residential" in addressType
	isCommercial := strings.ToLower(apiResp.Data.AddressType) == "commercial"

	result := &AddressValidationResult{
		IsDeliverable:  isDeliverable,
		Deliverability: apiResp.Data.Status,
		IsPOBox:        apiResp.Data.AddressType == "po_box" || isPOBoxAddress(apiResp.Data.Line1),
		IsCMRA:         apiResp.Data.CMRA,
		IsCommercial:   isCommercial,
		AddressType:    apiResp.Data.AddressType,
		StandardizedAddress: &models.Address{
			Line1:      apiResp.Data.Line1,
			Line2:      apiResp.Data.Line2,
			City:       apiResp.Data.City,
			State:      apiResp.Data.ProvinceOrState,
			PostalCode: apiResp.Data.PostalOrZip,
			Country:    apiResp.Data.Country,
		},
	}

	if !result.IsDeliverable {
		result.Reason = "Address validation failed"
		if len(apiResp.Data.Errors) > 0 {
			for field, errs := range apiResp.Data.Errors {
				if len(errs) > 0 {
					result.Reason = fmt.Sprintf("%s: %s", field, errs[0])
					break
				}
			}
		}
	}

	return result, nil
}

// SendPostcard sends a verification postcard via Postgrid Print & Mail API
// NOTE: Address is sent directly to Postgrid and NEVER stored by our system
func (s *PostgridService) SendPostcard(ctx context.Context, address *models.Address, verificationCode, postcardRef string) (string, error) {
	if span := appSentry.StartSpan(ctx, "http.client", "POST postgrid /postcards"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	if s.printAPIKey == "" {
		// Return mock request ID for development
		slog.Info("postgrid print and mail api key not configured, using mock mode")
		return "mock_postgrid_" + verificationCode, nil
	}

	// Verify return address is configured
	if s.returnAddress == nil || s.returnAddress.Line1 == "" {
		return "", fmt.Errorf("return address not configured: POSTGRID_RETURN_ADDRESS environment variable required")
	}

	// Normalize country code to ISO 3166-1 Alpha-2 format (uppercase)
	// Default to "US" if not specified since this is a US-focused application
	country := strings.ToUpper(strings.TrimSpace(address.Country))
	if country == "" {
		country = "US"
	}

	returnCountry := strings.ToUpper(strings.TrimSpace(s.returnAddress.Country))
	if returnCountry == "" {
		returnCountry = "US"
	}

	// Prepare postcard request
	reqBody := map[string]interface{}{
		"to": map[string]interface{}{
			"firstName":       "Resident",
			"addressLine1":    address.Line1,
			"addressLine2":    address.Line2,
			"city":            address.City,
			"provinceOrState": address.State,
			"postalOrZip":     address.PostalCode,
			"country":         country,
		},
		"from": map[string]interface{}{
			"companyName":     s.returnAddress.Name,
			"addressLine1":    s.returnAddress.Line1,
			"city":            s.returnAddress.City,
			"provinceOrState": s.returnAddress.State,
			"postalOrZip":     s.returnAddress.Zip,
			"country":         returnCountry,
		},
		"size": "6x4",
		"frontHTML": fmt.Sprintf(`
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
		"backHTML": `
			<html>
			<body style="font-family: Arial, sans-serif; padding: 20px;">
				<p><strong>What is Community Rapid Response?</strong></p>
				<p>A community platform that connects verified neighbors through secure Signal group chats.</p>
				<p>Your address is not stored in our database. It was shared with our mailing partner, Lob, who handles delivery and automatically deletes address data after 90 days.</p>
				<p>Questions? Visit communityrapidresponse.net/help</p>
			</body>
			</html>
		`,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.printBaseURL+"/postcards", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.printAPIKey)

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
		if strings.Contains(string(body), "invalid_api_key") {
			slog.Warn("postgrid print/mail api key is invalid, using mock mode")
			return "mock_postgrid_" + verificationCode, nil
		}
		return "", fmt.Errorf("API error: %s", string(body))
	}

	// Parse response to get request ID
	var apiResp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return apiResp.Data.ID, nil
}
