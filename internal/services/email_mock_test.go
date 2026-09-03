package services

import (
	"context"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
)

func newMockEmailServiceForTest(enabled bool) *MockEmailService {
	return NewMockEmailService(&config.EmailConfig{
		Backend:         config.EmailBackendMock,
		Enabled:         enabled,
		VerificationURL: "https://example.test/verify",
		FromAddress:     "noreply@example.test",
		FromName:        "Community Rapid Response",
	})
}

func TestMockEmailService_BackendName(t *testing.T) {
	svc := newMockEmailServiceForTest(true)
	if got := svc.Backend(); got != "mock" {
		t.Errorf("Backend() = %q, want %q", got, "mock")
	}
}

func TestMockEmailService_IsEnabledReflectsConfig(t *testing.T) {
	if !newMockEmailServiceForTest(true).IsEnabled() {
		t.Error("IsEnabled() = false for enabled=true config")
	}
	if newMockEmailServiceForTest(false).IsEnabled() {
		t.Error("IsEnabled() = true for enabled=false config")
	}
}

func TestMockEmailService_SendNeverErrors(t *testing.T) {
	svc := newMockEmailServiceForTest(true)
	msg := &EmailMessage{
		To:          "user@example.test",
		Subject:     "hi",
		TextContent: "body",
	}
	if err := svc.Send(context.Background(), msg); err != nil {
		t.Errorf("Send returned error: %v", err)
	}
}

func TestMockEmailService_SendVerificationEmailNeverErrors(t *testing.T) {
	svc := newMockEmailServiceForTest(true)
	if err := svc.SendVerificationEmail(context.Background(), "user@example.test", "tok"); err != nil {
		t.Errorf("SendVerificationEmail returned error: %v", err)
	}
}

func TestMockEmailService_SendStillSucceedsWhenDisabled(t *testing.T) {
	// MockEmailService.Send is a no-op log line and does not gate on IsEnabled.
	// This test pins that behavior so a future change that adds gating doesn't
	// silently break the development setup that relies on the mock.
	svc := newMockEmailServiceForTest(false)
	if err := svc.Send(context.Background(), &EmailMessage{To: "x@y", Subject: "s"}); err != nil {
		t.Errorf("Send returned error when disabled: %v", err)
	}
}

