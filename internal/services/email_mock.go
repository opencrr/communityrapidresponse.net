package services

import (
	"context"
	"log/slog"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
)

// MockEmailService logs emails instead of sending them (for development/testing)
type MockEmailService struct {
	fromAddress     string
	fromName        string
	verificationURL string
	enabled         bool
}

// NewMockEmailService creates a new mock email service
func NewMockEmailService(cfg *config.EmailConfig) *MockEmailService {
	return &MockEmailService{
		fromAddress:     cfg.FromAddress,
		fromName:        cfg.FromName,
		verificationURL: cfg.VerificationURL,
		enabled:         cfg.Enabled,
	}
}

// IsEnabled returns whether email sending is enabled
func (s *MockEmailService) IsEnabled() bool {
	return s.enabled
}

// Backend returns the backend name
func (s *MockEmailService) Backend() string {
	return "mock"
}

// Send logs email details instead of sending
func (s *MockEmailService) Send(ctx context.Context, msg *EmailMessage) error {
	slog.Info("mock email sent", "to", redactEmail(msg.To), "subject", msg.Subject)
	return nil
}

// SendVerificationEmail logs the verification email details
func (s *MockEmailService) SendVerificationEmail(ctx context.Context, toEmail, token string) error {
	slog.Info("mock verification email sent", "to", redactEmail(toEmail), "subject", "Verify your email address - Community Rapid Response")
	return nil
}

// Ensure MockEmailService implements EmailServiceInterface
var _ EmailServiceInterface = (*MockEmailService)(nil)
