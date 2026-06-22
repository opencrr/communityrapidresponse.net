package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
)

// SendGridEmailService handles email sending via SendGrid API
type SendGridEmailService struct {
	apiKey          string
	fromAddress     string
	fromName        string
	verificationURL string
	enabled         bool
	httpClient      *http.Client
}

// NewSendGridEmailService creates a new SendGrid email service
func NewSendGridEmailService(cfg *config.EmailConfig) *SendGridEmailService {
	return &SendGridEmailService{
		apiKey:          cfg.SendGridAPIKey,
		fromAddress:     cfg.FromAddress,
		fromName:        cfg.FromName,
		verificationURL: cfg.VerificationURL,
		enabled:         cfg.Enabled,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// IsEnabled returns whether email sending is enabled
func (s *SendGridEmailService) IsEnabled() bool {
	return s.enabled
}

// Backend returns the backend name
func (s *SendGridEmailService) Backend() string {
	return "sendgrid"
}

// Send sends a generic email message via SendGrid
func (s *SendGridEmailService) Send(ctx context.Context, msg *EmailMessage) error {
	if span := appSentry.StartSpan(ctx, "http.client", "POST sendgrid /v3/mail/send"); span != nil {
		defer span.Finish()
		ctx = span.Context()
	}

	if !s.enabled {
		slog.Info("sendgrid disabled, would send email", "to", redactEmail(msg.To), "subject", msg.Subject)
		return nil
	}

	payload := map[string]interface{}{
		"personalizations": []map[string]interface{}{
			{
				"to": []map[string]string{
					{"email": msg.To},
				},
			},
		},
		"from": map[string]string{
			"email": s.fromAddress,
			"name":  s.fromName,
		},
		"subject": msg.Subject,
		"content": []map[string]string{
			{"type": "text/plain", "value": msg.TextContent},
			{"type": "text/html", "value": msg.HTMLContent},
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal email payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.sendgrid.com/v3/mail/send", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("SendGrid API error: status %d", resp.StatusCode)
	}

	slog.Info("email sent via sendgrid", "to", redactEmail(msg.To))
	return nil
}

// SendVerificationEmail sends an email verification link to the user
func (s *SendGridEmailService) SendVerificationEmail(ctx context.Context, toEmail, token string) error {
	verifyURL := fmt.Sprintf("%s?token=%s", s.verificationURL, token)

	msg := &EmailMessage{
		To:      toEmail,
		Subject: "Verify your email address - Community Rapid Response",
		HTMLContent: fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
</head>
<body style="font-family: Arial, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
    <h1 style="color: #2c5282;">Community Rapid Response</h1>
    <h2>Verify your email address</h2>
    <p>Thank you for registering with Community Rapid Response. Please click the button below to verify your email address:</p>
    <p style="text-align: center; margin: 30px 0;">
        <a href="%s" style="background-color: #2c5282; color: white; padding: 12px 24px; text-decoration: none; border-radius: 4px; display: inline-block;">Verify Email</a>
    </p>
    <p>Or copy and paste this link into your browser:</p>
    <p style="word-break: break-all; color: #4a5568;">%s</p>
    <p style="margin-top: 30px; font-size: 14px; color: #718096;">This link will expire in 24 hours.</p>
    <p style="font-size: 14px; color: #718096;">If you didn't create an account with Community Rapid Response, you can safely ignore this email.</p>
</body>
</html>
`, verifyURL, verifyURL),
		TextContent: fmt.Sprintf(`Community Rapid Response - Verify your email address

Thank you for registering with Community Rapid Response. Please click the link below to verify your email address:

%s

This link will expire in 24 hours.

If you didn't create an account with Community Rapid Response, you can safely ignore this email.
`, verifyURL),
	}

	return s.Send(ctx, msg)
}

// Ensure SendGridEmailService implements EmailServiceInterface
var _ EmailServiceInterface = (*SendGridEmailService)(nil)
