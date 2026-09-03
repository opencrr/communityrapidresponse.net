package services

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
)

// SMTPEmailService handles email sending via SMTP
type SMTPEmailService struct {
	host            string
	port            int
	username        string
	password        string
	fromAddress     string
	fromName        string
	verificationURL string
	enabled         bool
}

// NewSMTPEmailService creates a new SMTP email service
func NewSMTPEmailService(cfg *config.EmailConfig) *SMTPEmailService {
	return &SMTPEmailService{
		host:            cfg.SMTPHost,
		port:            cfg.SMTPPort,
		username:        cfg.SMTPUser,
		password:        cfg.SMTPPassword,
		fromAddress:     cfg.FromAddress,
		fromName:        cfg.FromName,
		verificationURL: cfg.VerificationURL,
		enabled:         cfg.Enabled,
	}
}

// IsEnabled returns whether email sending is enabled
func (s *SMTPEmailService) IsEnabled() bool {
	return s.enabled
}

// Backend returns the backend name
func (s *SMTPEmailService) Backend() string {
	return "smtp"
}

// sanitizeHeaderValue strips CR and LF characters to prevent SMTP header injection.
func sanitizeHeaderValue(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	return value
}

// generateBoundary creates a random MIME boundary string.
func generateBoundary() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("==boundary-%x==", b)
}

// Send sends a generic email message via SMTP
func (s *SMTPEmailService) Send(ctx context.Context, msg *EmailMessage) error {
	if !s.enabled {
		// Subject is intentionally omitted: it can correlate users with
		// password-reset / email-verification flows in log aggregation.
		slog.Info("smtp disabled, would send email", "to", RedactEmail(msg.To))
		return nil
	}

	// Build the email message
	from := s.fromAddress
	if s.fromName != "" {
		from = fmt.Sprintf("%s <%s>", s.fromName, s.fromAddress)
	}

	// Build MIME message with both text and HTML parts
	boundary := generateBoundary()
	headers := fmt.Sprintf("From: %s\r\n", sanitizeHeaderValue(from))
	headers += fmt.Sprintf("To: %s\r\n", sanitizeHeaderValue(msg.To))
	headers += fmt.Sprintf("Subject: %s\r\n", sanitizeHeaderValue(msg.Subject))
	headers += "MIME-Version: 1.0\r\n"
	headers += fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary)
	headers += "\r\n"

	body := fmt.Sprintf("--%s\r\n", boundary)
	body += "Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n"
	body += msg.TextContent + "\r\n"
	body += fmt.Sprintf("--%s\r\n", boundary)
	body += "Content-Type: text/html; charset=\"utf-8\"\r\n\r\n"
	body += msg.HTMLContent + "\r\n"
	body += fmt.Sprintf("--%s--\r\n", boundary)

	message := []byte(headers + body)

	// Connect to SMTP server
	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	var auth smtp.Auth
	if s.username != "" && s.password != "" {
		auth = smtp.PlainAuth("", s.username, s.password, s.host)
	}

	// For port 465, use TLS from the start
	if s.port == 465 {
		return s.sendWithTLS(addr, auth, s.fromAddress, msg.To, message)
	}

	// For other ports (587, 25), use STARTTLS
	err := smtp.SendMail(addr, auth, s.fromAddress, []string{msg.To}, message)
	if err != nil {
		return fmt.Errorf("failed to send email via SMTP: %w", err)
	}

	slog.Info("email sent via smtp", "to", RedactEmail(msg.To))
	return nil
}

// sendWithTLS sends email using implicit TLS (port 465)
func (s *SMTPEmailService) sendWithTLS(addr string, auth smtp.Auth, from, to string, message []byte) error {
	tlsConfig := &tls.Config{
		ServerName: s.host,
		MinVersion: tls.VersionTLS12,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer func() { _ = conn.Close() }()

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer func() { _ = client.Close() }()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL command failed: %w", err)
	}

	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("SMTP RCPT command failed: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA command failed: %w", err)
	}

	if _, err := w.Write(message); err != nil {
		return fmt.Errorf("failed to write email body: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close email body: %w", err)
	}

	slog.Info("email sent via smtp with tls", "to", RedactEmail(to))
	return nil
}

// SendVerificationEmail sends an email verification link to the user
func (s *SMTPEmailService) SendVerificationEmail(ctx context.Context, toEmail, token string) error {
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

// Ensure SMTPEmailService implements EmailServiceInterface
var _ EmailServiceInterface = (*SMTPEmailService)(nil)
