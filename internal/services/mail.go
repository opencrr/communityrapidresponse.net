package services

import (
	"fmt"
	"log"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
)

// NewMailService creates the appropriate mail service based on configuration
// Returns a PostgridServiceInterface that can be either PostgridService or LobService
func NewMailService(cfg *config.Config) (PostgridServiceInterface, error) {
	switch cfg.MailProvider {
	case config.MailProviderPostgrid:
		log.Printf("INFO: Using Postgrid as mail provider")
		return NewPostgridService(&cfg.Postgrid), nil

	case config.MailProviderLob:
		log.Printf("INFO: Using Lob as mail provider (90-day data retention)")
		return NewLobService(&cfg.Lob), nil

	default:
		// Default to Lob for better privacy
		if cfg.MailProvider == "" {
			log.Printf("INFO: MAIL_PROVIDER not set, defaulting to Lob")
			return NewLobService(&cfg.Lob), nil
		}
		return nil, fmt.Errorf("unknown mail provider: %s (valid options: lob, postgrid)", cfg.MailProvider)
	}
}
