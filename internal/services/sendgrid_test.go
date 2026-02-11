package services

import (
	"context"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
)

func TestSendGridEmailService_DisabledMode(t *testing.T) {
	cfg := &config.EmailConfig{
		Backend:         config.EmailBackendSendGrid,
		Enabled:         false,
		SendGridAPIKey:  "test-key",
		FromAddress:     "test@example.com",
		FromName:        "Test",
		VerificationURL: "http://localhost:3000/verify-email",
	}

	service := NewSendGridEmailService(cfg)

	// Verify disabled mode is detected
	if service.IsEnabled() {
		t.Error("Expected IsEnabled() to be false when Enabled is false")
	}

	// Sending email should not fail when disabled (just logs)
	err := service.SendVerificationEmail(context.Background(), "user@example.com", "test-token")
	if err != nil {
		t.Errorf("SendVerificationEmail when disabled should not return error, got: %v", err)
	}
}

func TestSendGridEmailService_IsEnabled(t *testing.T) {
	testCases := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{"enabled", true, true},
		{"disabled", false, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.EmailConfig{
				Backend:         config.EmailBackendSendGrid,
				Enabled:         tc.enabled,
				SendGridAPIKey:  "test-key",
				FromAddress:     "test@example.com",
				FromName:        "Test",
				VerificationURL: "http://localhost:3000/verify-email",
			}

			service := NewSendGridEmailService(cfg)
			if service.IsEnabled() != tc.expected {
				t.Errorf("IsEnabled() = %v, want %v", service.IsEnabled(), tc.expected)
			}
		})
	}
}

func TestSendGridEmailService_Backend(t *testing.T) {
	cfg := &config.EmailConfig{
		Backend:         config.EmailBackendSendGrid,
		Enabled:         true,
		SendGridAPIKey:  "test-key",
		FromAddress:     "test@example.com",
		FromName:        "Test",
		VerificationURL: "http://localhost:3000/verify-email",
	}

	service := NewSendGridEmailService(cfg)
	if service.Backend() != "sendgrid" {
		t.Errorf("Backend() = %s, want sendgrid", service.Backend())
	}
}

func TestSendGridEmailService_ImplementsInterface(t *testing.T) {
	cfg := &config.EmailConfig{
		Backend:         config.EmailBackendSendGrid,
		Enabled:         true,
		SendGridAPIKey:  "test-key",
		FromAddress:     "test@example.com",
		FromName:        "Test",
		VerificationURL: "http://localhost:3000/verify-email",
	}

	service := NewSendGridEmailService(cfg)

	// Verify it implements EmailServiceInterface
	var _ EmailServiceInterface = service
}

func TestSMTPEmailService_ImplementsInterface(t *testing.T) {
	cfg := &config.EmailConfig{
		Backend:         config.EmailBackendSMTP,
		Enabled:         true,
		SMTPHost:        "localhost",
		SMTPPort:        587,
		FromAddress:     "test@example.com",
		FromName:        "Test",
		VerificationURL: "http://localhost:3000/verify-email",
	}

	service := NewSMTPEmailService(cfg)

	// Verify it implements EmailServiceInterface
	var _ EmailServiceInterface = service
}

func TestMockEmailService_ImplementsInterface(t *testing.T) {
	cfg := &config.EmailConfig{
		Backend:         config.EmailBackendMock,
		Enabled:         true,
		FromAddress:     "test@example.com",
		FromName:        "Test",
		VerificationURL: "http://localhost:3000/verify-email",
	}

	service := NewMockEmailService(cfg)

	// Verify it implements EmailServiceInterface
	var _ EmailServiceInterface = service
}

func TestNewEmailService_Factory(t *testing.T) {
	testCases := []struct {
		name            string
		backend         config.EmailBackend
		expectedBackend string
		expectError     bool
	}{
		{"mock backend", config.EmailBackendMock, "mock", false},
		{"empty backend defaults to mock", "", "mock", false},
		{"sendgrid backend", config.EmailBackendSendGrid, "sendgrid", false},
		{"smtp backend", config.EmailBackendSMTP, "smtp", false},
		{"invalid backend", config.EmailBackend("invalid"), "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.EmailConfig{
				Backend:         tc.backend,
				Enabled:         true,
				SendGridAPIKey:  "test-key",
				SMTPHost:        "localhost",
				FromAddress:     "test@example.com",
				FromName:        "Test",
				VerificationURL: "http://localhost:3000/verify-email",
			}

			service, err := NewEmailService(cfg)
			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if service.Backend() != tc.expectedBackend {
				t.Errorf("Backend() = %s, want %s", service.Backend(), tc.expectedBackend)
			}
		})
	}
}

func TestNewEmailService_RequiresAPIKey(t *testing.T) {
	cfg := &config.EmailConfig{
		Backend:         config.EmailBackendSendGrid,
		Enabled:         true,
		SendGridAPIKey:  "", // Missing API key
		FromAddress:     "test@example.com",
		FromName:        "Test",
		VerificationURL: "http://localhost:3000/verify-email",
	}

	_, err := NewEmailService(cfg)
	if err == nil {
		t.Error("Expected error when SendGrid API key is missing")
	}
}

func TestNewEmailService_RequiresSMTPHost(t *testing.T) {
	cfg := &config.EmailConfig{
		Backend:         config.EmailBackendSMTP,
		Enabled:         true,
		SMTPHost:        "", // Missing host
		FromAddress:     "test@example.com",
		FromName:        "Test",
		VerificationURL: "http://localhost:3000/verify-email",
	}

	_, err := NewEmailService(cfg)
	if err == nil {
		t.Error("Expected error when SMTP host is missing")
	}
}
