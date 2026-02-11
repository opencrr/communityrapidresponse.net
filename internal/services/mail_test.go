package services

import (
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
)

func TestNewMailService_Postgrid(t *testing.T) {
	cfg := &config.Config{
		MailProvider: config.MailProviderPostgrid,
		Postgrid: config.PostgridConfig{
			AddressVerificationAPIKey: "",
			PrintMailAPIKey:           "",
		},
	}

	service, err := NewMailService(cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	_, ok := service.(*PostgridService)
	if !ok {
		t.Error("Expected PostgridService when MAIL_PROVIDER=postgrid")
	}
}

func TestNewMailService_Lob(t *testing.T) {
	cfg := &config.Config{
		MailProvider: config.MailProviderLob,
		Lob: config.LobConfig{
			APIKey: "",
		},
	}

	service, err := NewMailService(cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	_, ok := service.(*LobService)
	if !ok {
		t.Error("Expected LobService when MAIL_PROVIDER=lob")
	}
}

func TestNewMailService_DefaultsToLob(t *testing.T) {
	cfg := &config.Config{
		MailProvider: "", // Empty - should default to Lob
		Lob: config.LobConfig{
			APIKey: "",
		},
	}

	service, err := NewMailService(cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	_, ok := service.(*LobService)
	if !ok {
		t.Error("Expected LobService when MAIL_PROVIDER is empty")
	}
}

func TestNewMailService_InvalidProvider(t *testing.T) {
	cfg := &config.Config{
		MailProvider: "invalid_provider",
	}

	_, err := NewMailService(cfg)
	if err == nil {
		t.Error("Expected error for invalid mail provider")
	}
}
