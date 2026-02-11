package services

import (
	"context"
	"fmt"
	"log"

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
	log.Printf("[MOCK EMAIL] ============================================")
	log.Printf("[MOCK EMAIL] To: %s", msg.To)
	log.Printf("[MOCK EMAIL] From: %s <%s>", s.fromName, s.fromAddress)
	log.Printf("[MOCK EMAIL] Subject: %s", msg.Subject)
	log.Printf("[MOCK EMAIL] Text Content:")
	log.Printf("%s", msg.TextContent)
	log.Printf("[MOCK EMAIL] ============================================")
	return nil
}

// SendVerificationEmail logs the verification email details
func (s *MockEmailService) SendVerificationEmail(ctx context.Context, toEmail, token string) error {
	verifyURL := fmt.Sprintf("%s?token=%s", s.verificationURL, token)

	log.Printf("[MOCK EMAIL] ============================================")
	log.Printf("[MOCK EMAIL] VERIFICATION EMAIL")
	log.Printf("[MOCK EMAIL] To: %s", toEmail)
	log.Printf("[MOCK EMAIL] From: %s <%s>", s.fromName, s.fromAddress)
	log.Printf("[MOCK EMAIL] Subject: Verify your email address - Community Rapid Response")
	log.Printf("[MOCK EMAIL] Verification URL: %s", verifyURL)
	log.Printf("[MOCK EMAIL] Token: %s", token)
	log.Printf("[MOCK EMAIL] ============================================")

	return nil
}

// Ensure MockEmailService implements EmailServiceInterface
var _ EmailServiceInterface = (*MockEmailService)(nil)
