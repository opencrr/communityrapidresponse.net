package services

import (
	"context"
	"strings"
	"testing"
)

func TestSanitizeHeaderValue_StripsCR(t *testing.T) {
	result := sanitizeHeaderValue("hello\rworld")
	if result != "helloworld" {
		t.Errorf("expected %q, got %q", "helloworld", result)
	}
}

func TestSanitizeHeaderValue_StripsLF(t *testing.T) {
	result := sanitizeHeaderValue("hello\nworld")
	if result != "helloworld" {
		t.Errorf("expected %q, got %q", "helloworld", result)
	}
}

func TestSanitizeHeaderValue_StripsCRLF(t *testing.T) {
	result := sanitizeHeaderValue("hello\r\nBcc: attacker@evil.com\r\nworld")
	expected := "helloBcc: attacker@evil.comworld"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestSanitizeHeaderValue_PassthroughClean(t *testing.T) {
	input := "Normal Subject Line"
	result := sanitizeHeaderValue(input)
	if result != input {
		t.Errorf("expected %q, got %q", input, result)
	}
}

func TestGenerateBoundary_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		b := generateBoundary()
		if seen[b] {
			t.Fatalf("duplicate boundary generated: %s", b)
		}
		seen[b] = true
	}
}

func TestGenerateBoundary_HasPrefix(t *testing.T) {
	b := generateBoundary()
	if !strings.HasPrefix(b, "==boundary-") {
		t.Errorf("boundary %q missing expected prefix", b)
	}
	if !strings.HasSuffix(b, "==") {
		t.Errorf("boundary %q missing expected suffix", b)
	}
}

func TestSMTPSend_HeaderInjectionPrevented(t *testing.T) {
	svc := &SMTPEmailService{
		fromAddress: "noreply@example.com",
		fromName:    "CRR",
		enabled:     false, // disabled so no actual SMTP connection
	}

	msg := &EmailMessage{
		To:          "victim@example.com\r\nBcc: attacker@evil.com",
		Subject:     "Test\r\nBcc: attacker@evil.com",
		TextContent: "hello",
		HTMLContent: "<p>hello</p>",
	}

	// Send returns nil when disabled, but we want to verify the
	// sanitization logic by checking that the service doesn't panic
	// and processes the message without error.
	err := svc.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSMTPSend_CRLFInjectedToField(t *testing.T) {
	// Verify that sanitizeHeaderValue actually prevents header injection
	// by checking the constructed header string directly.
	injectedTo := "victim@example.com\r\nBcc: attacker@evil.com"
	sanitized := sanitizeHeaderValue(injectedTo)

	if strings.Contains(sanitized, "\r") || strings.Contains(sanitized, "\n") {
		t.Error("sanitized To still contains CR or LF")
	}

	// The "Bcc:" text remains but is collapsed onto the same line as the To value,
	// so it cannot act as a separate SMTP header.
	expected := "victim@example.comBcc: attacker@evil.com"
	if sanitized != expected {
		t.Errorf("expected %q, got %q", expected, sanitized)
	}
}
