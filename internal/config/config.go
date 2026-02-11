package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// MailProvider represents the mailing service provider type
type MailProvider string

const (
	MailProviderPostgrid MailProvider = "postgrid" // Postgrid API (indefinite data retention)
	MailProviderLob      MailProvider = "lob"      // Lob API (90-day data retention, auto-delete)
)

// Config holds all application configuration
type Config struct {
	Server       ServerConfig
	Database     DatabaseConfig
	JWT          JWTConfig
	MFA          MFAConfig
	Mapbox       MapboxConfig
	MailProvider MailProvider // Which mailing service to use: "postgrid" or "lob"
	Postgrid     PostgridConfig
	Lob          LobConfig
	Email        EmailConfig
	RateLimit    RateLimitConfig
	Consensus    ConsensusConfig
	Bootstrap    BootstrapConfig
	Notification NotificationConfig
	Blocklist    BlocklistConfig
	CORS         CORSConfig
	Sentry       SentryConfig
}

// CORSConfig holds CORS configuration
type CORSConfig struct {
	AllowedOrigins []string // Comma-separated origins, default "*" for dev
}

// SentryConfig holds Sentry error tracking configuration
type SentryConfig struct {
	DSN              string  // Sentry DSN; empty string disables Sentry
	Environment      string  // Environment name sent to Sentry (e.g., "production", "staging")
	Release          string  // Release version (e.g., git SHA); empty omits release tag
	TracesSampleRate float64 // Fraction of requests to trace (0.0 to 1.0); 0 disables tracing
	Debug            bool    // Enable Sentry SDK debug logging
}

// BootstrapConfig holds settings for bootstrap mode verification
type BootstrapConfig struct {
	CooldownEnabled bool // Enable cooldown between vouches in bootstrap mode (default true for production)
	CooldownMinutes int  // Minutes between vouches from the same user (default 30)
}

// NotificationConfig holds settings for the email notification system
type NotificationConfig struct {
	WorkerInterval    time.Duration // How often the worker processes the queue (default: 1 minute)
	BatchSize         int           // Max notifications per worker run (default: 50)
	RateLimitDuration time.Duration // Rate limit window for duplicate notifications (default: 24 hours)
	RetryFailedAfter  time.Duration // Retry failed notifications after this duration (default: 1 hour)
}

// ConsensusConfig holds settings for admin consensus voting
type ConsensusConfig struct {
	VotePercent int // Percentage of admins required to approve (default 50)
	VoteFloor   int // Minimum number of votes required regardless of percentage (default 3)
}

// BlocklistConfig holds settings for the blocklist proposal system
type BlocklistConfig struct {
	AddressBlocklistDuration  time.Duration // How long address blocklist lasts (default: 2 years)
	ProposalRateLimitPerMonth int           // Max proposals per admin per month (default: 5)
}

// RequiredVotes calculates the number of votes needed for consensus given an admin count.
// Returns max(ceil(adminCount * VotePercent / 100), VoteFloor)
func (c *ConsensusConfig) RequiredVotes(adminCount int) int {
	// Calculate percentage-based requirement (ceiling division)
	percentBased := (adminCount*c.VotePercent + 99) / 100

	// Return the higher of percentage-based or floor
	if percentBased > c.VoteFloor {
		return percentBased
	}
	return c.VoteFloor
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	Enabled        bool     // Enable/disable rate limiting
	IPLimit        int      // Requests per IP per window
	IPWindowSecs   int      // Window size in seconds
	Backend        string   // Rate limit backend: "memory" (default) or "database"
	TrustedProxies []string // CIDR ranges of trusted reverse proxies (e.g. "127.0.0.1/32,10.0.0.0/8")
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Host          string
	Port          int
	ReadTimeout   time.Duration
	WriteTimeout  time.Duration
	SecureCookies bool // Set to false for development (HTTP)
}

// DatabaseConfig holds MariaDB connection configuration
type DatabaseConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Name            string
	Charset         string
	MaxOpenConns    int           // DB_MAX_OPEN_CONNS, default 25
	MaxIdleConns    int           // DB_MAX_IDLE_CONNS, default 5
	ConnMaxLifetime time.Duration // DB_CONN_MAX_LIFETIME, default 5m
}

// JWTConfig holds JWT authentication configuration
type JWTConfig struct {
	Secret          string
	ExpirationHours int
	Issuer          string
}

// MFAConfig holds MFA configuration
type MFAConfig struct {
	EncryptionKey string // 32-byte key for AES-256 encryption of TOTP secrets
	Issuer        string // Issuer name shown in authenticator apps
	Required      bool   // Whether MFA is required (set to false for development)
}

// MapboxConfig holds Mapbox API configuration
type MapboxConfig struct {
	PublicToken string
	SecretToken string
}

// PostgridConfig holds Postgrid API configuration
// Postgrid has two separate APIs with different base URLs and API keys:
// - Address Verification: https://api.postgrid.com/v1/addver/...
// - Print & Mail: https://api.postgrid.com/print-mail/v1/...
// NOTE: Postgrid retains address data indefinitely for compliance purposes
type PostgridConfig struct {
	// Address Verification API
	AddressVerificationAPIKey  string
	AddressVerificationBaseURL string
	// Print & Mail API
	PrintMailAPIKey  string
	PrintMailBaseURL string
	// Return address for postcards
	ReturnName    string
	ReturnLine1   string
	ReturnCity    string
	ReturnState   string
	ReturnZip     string
	ReturnCountry string
}

// LobConfig holds Lob API configuration
// Lob uses a single API with Basic Auth for both address verification and postcards
// PRIVACY ADVANTAGE: Lob auto-deletes address data after 90 days
type LobConfig struct {
	APIKey  string
	BaseURL string
	// Return address for postcards
	ReturnName    string
	ReturnLine1   string
	ReturnCity    string
	ReturnState   string
	ReturnZip     string
	ReturnCountry string
}

// EmailBackend represents the email service backend type
type EmailBackend string

const (
	EmailBackendMock     EmailBackend = "mock"     // Log emails instead of sending (development)
	EmailBackendSendGrid EmailBackend = "sendgrid" // SendGrid API
	EmailBackendSMTP     EmailBackend = "smtp"     // Standard SMTP
)

// EmailConfig holds email service configuration
type EmailConfig struct {
	// Backend selection
	Backend         EmailBackend // Email backend: mock, sendgrid, smtp
	Enabled         bool         // Enable email verification
	VerificationURL string       // Frontend URL for email verification

	// Common settings
	FromAddress string
	FromName    string

	// SMTP settings (used when Backend = smtp)
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string

	// SendGrid settings (used when Backend = sendgrid)
	SendGridAPIKey string
}

// Load reads configuration from environment variables
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Host:          getEnv("SERVER_HOST", "0.0.0.0"),
			Port:          getEnvInt("SERVER_PORT", 8080),
			ReadTimeout:   time.Duration(getEnvInt("SERVER_READ_TIMEOUT", 30)) * time.Second,
			WriteTimeout:  time.Duration(getEnvInt("SERVER_WRITE_TIMEOUT", 30)) * time.Second,
			SecureCookies: getEnvBool("SECURE_COOKIES", false), // Default false for development
		},
		Database: DatabaseConfig{
			Host:            getEnv("DB_HOST", "localhost"),
			Port:            getEnvInt("DB_PORT", 3306),
			User:            getEnv("DB_USER", "communityrapidresponse"),
			Password:        getEnv("DB_PASSWORD", ""),
			Name:            getEnv("DB_NAME", "communityrapidresponse"),
			Charset:         "utf8mb4",
			MaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: getEnvDuration("DB_CONN_MAX_LIFETIME", 5*time.Minute),
		},
		JWT: JWTConfig{
			Secret:          getEnv("JWT_SECRET", ""),
			ExpirationHours: getEnvInt("JWT_EXPIRATION_HOURS", 24),
			Issuer:          getEnv("JWT_ISSUER", "communityrapidresponse"),
		},
		MFA: MFAConfig{
			EncryptionKey: getEnv("MFA_ENCRYPTION_KEY", ""),
			Issuer:        getEnv("MFA_ISSUER", "Community Rapid Response"),
			Required:      getEnvBool("MFA_REQUIRED", true), // Default true for production security
		},
		Mapbox: MapboxConfig{
			PublicToken: getEnv("MAPBOX_PUBLIC_TOKEN", ""),
			SecretToken: getEnv("MAPBOX_SECRET_TOKEN", ""),
		},
		MailProvider: MailProvider(getEnv("MAIL_PROVIDER", "lob")), // Default to Lob for better privacy (90-day data retention)
		Postgrid: PostgridConfig{
			AddressVerificationAPIKey:  getEnv("POSTGRID_ADDVER_API_KEY", ""),
			AddressVerificationBaseURL: getEnv("POSTGRID_ADDVER_BASE_URL", "https://api.postgrid.com/v1"),
			PrintMailAPIKey:            getEnv("POSTGRID_PRINT_API_KEY", ""),
			PrintMailBaseURL:           getEnv("POSTGRID_PRINT_BASE_URL", "https://api.postgrid.com/print-mail/v1"),
			ReturnName:                 getEnv("POSTGRID_RETURN_NAME", "Community Rapid Response"),
			ReturnLine1:                getEnv("POSTGRID_RETURN_ADDRESS", ""),
			ReturnCity:                 getEnv("POSTGRID_RETURN_CITY", ""),
			ReturnState:                getEnv("POSTGRID_RETURN_STATE", ""),
			ReturnZip:                  getEnv("POSTGRID_RETURN_ZIP", ""),
			ReturnCountry:              getEnv("POSTGRID_RETURN_COUNTRY", "US"),
		},
		Lob: LobConfig{
			APIKey:        getEnv("LOB_API_KEY", ""),
			BaseURL:       getEnv("LOB_BASE_URL", "https://api.lob.com/v1"),
			ReturnName:    getEnv("LOB_RETURN_NAME", "Community Rapid Response"),
			ReturnLine1:   getEnv("LOB_RETURN_ADDRESS", ""),
			ReturnCity:    getEnv("LOB_RETURN_CITY", ""),
			ReturnState:   getEnv("LOB_RETURN_STATE", ""),
			ReturnZip:     getEnv("LOB_RETURN_ZIP", ""),
			ReturnCountry: getEnv("LOB_RETURN_COUNTRY", "US"),
		},
		Email: EmailConfig{
			Backend:         EmailBackend(getEnv("EMAIL_BACKEND", "mock")),
			Enabled:         getEnvBool("EMAIL_ENABLED", false),
			VerificationURL: getEnv("EMAIL_VERIFICATION_URL", "http://localhost:3000/verify-email"),
			FromAddress:     getEnv("EMAIL_FROM_ADDRESS", "noreply@communityrapidresponse.net"),
			FromName:        getEnv("EMAIL_FROM_NAME", "Community Rapid Response"),
			SMTPHost:        getEnv("SMTP_HOST", ""),
			SMTPPort:        getEnvInt("SMTP_PORT", 587),
			SMTPUser:        getEnv("SMTP_USER", ""),
			SMTPPassword:    getEnv("SMTP_PASSWORD", ""),
			SendGridAPIKey:  getEnv("SENDGRID_API_KEY", ""),
		},
		RateLimit: RateLimitConfig{
			Enabled:        getEnvBool("RATE_LIMIT_ENABLED", true),
			IPLimit:        getEnvInt("RATE_LIMIT_IP_LIMIT", 100),
			IPWindowSecs:   getEnvInt("RATE_LIMIT_IP_WINDOW_SECS", 60),
			Backend:        getEnv("RATE_LIMIT_BACKEND", "memory"),
			TrustedProxies: splitCSV(getEnv("TRUSTED_PROXIES", "")),
		},
		Consensus: ConsensusConfig{
			VotePercent: getEnvInt("CONSENSUS_VOTE_PERCENT", 50),
			VoteFloor:   getEnvInt("CONSENSUS_VOTE_FLOOR", 3),
		},
		Bootstrap: BootstrapConfig{
			CooldownEnabled: getEnvBool("BOOTSTRAP_COOLDOWN_ENABLED", true), // Default true for production
			CooldownMinutes: getEnvInt("BOOTSTRAP_COOLDOWN_MINUTES", 30),    // Default 30 minutes
		},
		Notification: NotificationConfig{
			WorkerInterval:    getEnvDuration("NOTIFICATION_WORKER_INTERVAL", time.Minute),
			BatchSize:         getEnvInt("NOTIFICATION_BATCH_SIZE", 200),
			RateLimitDuration: getEnvDuration("NOTIFICATION_RATE_LIMIT_DURATION", 24*time.Hour),
			RetryFailedAfter:  getEnvDuration("NOTIFICATION_RETRY_FAILED_AFTER", time.Hour),
		},
		Blocklist: BlocklistConfig{
			AddressBlocklistDuration:  getEnvDuration("BLOCKLIST_ADDRESS_DURATION", 2*365*24*time.Hour), // Default: 2 years
			ProposalRateLimitPerMonth: getEnvInt("BLOCKLIST_PROPOSAL_RATE_LIMIT", 5),                    // Default: 5 per month
		},
		CORS: CORSConfig{
			AllowedOrigins: splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "*")),
		},
		Sentry: SentryConfig{
			DSN:              getEnv("SENTRY_DSN", ""),
			Environment:      getEnv("SENTRY_ENVIRONMENT", "development"),
			Release:          getEnv("SENTRY_RELEASE", ""),
			TracesSampleRate: getEnvFloat("SENTRY_TRACES_SAMPLE_RATE", 0),
			Debug:            getEnvBool("SENTRY_DEBUG", false),
		},
	}
}

// DSN returns the MariaDB data source name
func (c *DatabaseConfig) DSN() string {
	return c.User + ":" + c.Password + "@tcp(" + c.Host + ":" + strconv.Itoa(c.Port) + ")/" + c.Name + "?charset=" + c.Charset + "&parseTime=true&loc=UTC"
}

// IsValid returns whether the email backend is a valid option
func (b EmailBackend) IsValid() bool {
	switch b {
	case EmailBackendMock, EmailBackendSendGrid, EmailBackendSMTP:
		return true
	default:
		return false
	}
}

// String returns the string representation of the email backend
func (b EmailBackend) String() string {
	return string(b)
}

// IsValid returns whether the mail provider is a valid option
func (p MailProvider) IsValid() bool {
	switch p {
	case MailProviderPostgrid, MailProviderLob:
		return true
	default:
		return false
	}
}

// String returns the string representation of the mail provider
func (p MailProvider) String() string {
	return string(p)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			return floatValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}

// splitCSV splits a comma-separated string, trims whitespace, and filters empty strings.
func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	var result []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
