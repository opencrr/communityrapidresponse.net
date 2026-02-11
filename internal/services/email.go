package services

import (
	"errors"
	"fmt"
	"strings"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
)

var (
	ErrAliasedEmailNotAllowed  = errors.New("email aliases with '+' are not allowed")
	ErrUnsupportedEmailBackend = errors.New("unsupported email backend")
)

// NewEmailService creates an email service based on the configuration.
// It returns the appropriate backend implementation based on cfg.Backend.
//
// Supported backends:
//   - "mock": Logs emails instead of sending (for development)
//   - "sendgrid": Uses SendGrid API
//   - "smtp": Uses standard SMTP
func NewEmailService(cfg *config.EmailConfig) (EmailServiceInterface, error) {
	switch cfg.Backend {
	case config.EmailBackendMock, "":
		// Default to mock if not specified
		return NewMockEmailService(cfg), nil
	case config.EmailBackendSendGrid:
		if cfg.SendGridAPIKey == "" {
			return nil, fmt.Errorf("SendGrid API key is required when using sendgrid backend")
		}
		return NewSendGridEmailService(cfg), nil
	case config.EmailBackendSMTP:
		if cfg.SMTPHost == "" {
			return nil, fmt.Errorf("SMTP host is required when using smtp backend")
		}
		return NewSMTPEmailService(cfg), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedEmailBackend, cfg.Backend)
	}
}

// NormalizeEmail normalizes an email address for duplicate detection.
// It rejects '+' aliases, normalizes googlemail.com to gmail.com,
// and removes dots from Gmail local parts.
// Returns the normalized email in lowercase.
func NormalizeEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", errors.New("email is required")
	}

	// Check for '+' alias (reject outright)
	if strings.Contains(email, "+") {
		return "", ErrAliasedEmailNotAllowed
	}

	// Split into local and domain parts
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "", errors.New("invalid email format")
	}

	localPart := parts[0]
	domain := strings.ToLower(parts[1])

	// Validate local part and domain are not empty
	if localPart == "" || domain == "" {
		return "", errors.New("invalid email format")
	}

	// Normalize googlemail.com to gmail.com
	if domain == "googlemail.com" {
		domain = "gmail.com"
	}

	// For Gmail addresses, remove dots from the local part
	if domain == "gmail.com" {
		localPart = strings.ReplaceAll(localPart, ".", "")
	}

	// Return lowercase normalized email
	return strings.ToLower(localPart) + "@" + domain, nil
}

// ContainsEmailAlias checks if an email contains a '+' alias character.
// This is a lightweight check used during validation before full normalization.
func ContainsEmailAlias(email string) bool {
	return strings.Contains(email, "+")
}
