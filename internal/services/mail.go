package services

import (
	"fmt"
	"log/slog"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
)

// NewMailService creates the appropriate mail service based on configuration
// Returns a PostgridServiceInterface that can be either PostgridService or LobService
func NewMailService(cfg *config.Config) (PostgridServiceInterface, error) {
	switch cfg.MailProvider {
	case config.MailProviderPostgrid:
		slog.Info("using postgrid as mail provider")
		return NewPostgridService(&cfg.Postgrid), nil

	case config.MailProviderLob:
		slog.Info("using lob as mail provider", "data_retention", "90 days")
		return NewLobService(&cfg.Lob), nil

	default:
		// Default to Lob for better privacy
		if cfg.MailProvider == "" {
			slog.Info("mail provider not set, defaulting to lob")
			return NewLobService(&cfg.Lob), nil
		}
		return nil, fmt.Errorf("unknown mail provider: %s (valid options: lob, postgrid)", cfg.MailProvider)
	}
}
