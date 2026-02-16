package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	// Save original env vars
	origHost := os.Getenv("SERVER_HOST")
	origPort := os.Getenv("SERVER_PORT")
	origDBHost := os.Getenv("DB_HOST")
	origJWTSecret := os.Getenv("JWT_SECRET")

	// Cleanup
	defer func() {
		_ = os.Setenv("SERVER_HOST", origHost)
		_ = os.Setenv("SERVER_PORT", origPort)
		_ = os.Setenv("DB_HOST", origDBHost)
		_ = os.Setenv("JWT_SECRET", origJWTSecret)
	}()

	t.Run("loads default values", func(t *testing.T) {
		_ = os.Unsetenv("SERVER_HOST")
		_ = os.Unsetenv("SERVER_PORT")

		cfg := Load()

		if cfg.Server.Host != "0.0.0.0" {
			t.Errorf("Expected default host '0.0.0.0', got '%s'", cfg.Server.Host)
		}
		if cfg.Server.Port != 8080 {
			t.Errorf("Expected default port 8080, got %d", cfg.Server.Port)
		}
	})

	t.Run("loads from environment", func(t *testing.T) {
		_ = os.Setenv("SERVER_HOST", "127.0.0.1")
		_ = os.Setenv("SERVER_PORT", "9090")
		_ = os.Setenv("DB_HOST", "db.example.com")
		_ = os.Setenv("JWT_SECRET", "my_secret")

		cfg := Load()

		if cfg.Server.Host != "127.0.0.1" {
			t.Errorf("Expected host '127.0.0.1', got '%s'", cfg.Server.Host)
		}
		if cfg.Server.Port != 9090 {
			t.Errorf("Expected port 9090, got %d", cfg.Server.Port)
		}
		if cfg.Database.Host != "db.example.com" {
			t.Errorf("Expected DB host 'db.example.com', got '%s'", cfg.Database.Host)
		}
		if cfg.JWT.Secret != "my_secret" {
			t.Errorf("Expected JWT secret 'my_secret', got '%s'", cfg.JWT.Secret)
		}
	})

	t.Run("handles invalid port gracefully", func(t *testing.T) {
		_ = os.Setenv("SERVER_PORT", "invalid")

		cfg := Load()

		// Should use default value
		if cfg.Server.Port != 8080 {
			t.Errorf("Expected default port 8080 for invalid value, got %d", cfg.Server.Port)
		}
	})
}

func TestDatabaseConfig_DSN(t *testing.T) {
	cfg := &DatabaseConfig{
		Host:     "localhost",
		Port:     3306,
		User:     "testuser",
		Password: "testpass",
		Name:     "testdb",
		Charset:  "utf8mb4",
	}

	expected := "testuser:testpass@tcp(localhost:3306)/testdb?charset=utf8mb4&parseTime=true&loc=UTC"
	actual := cfg.DSN()

	if actual != expected {
		t.Errorf("Expected DSN '%s', got '%s'", expected, actual)
	}
}

func TestGetEnv(t *testing.T) {
	t.Run("returns env value when set", func(t *testing.T) {
		_ = os.Setenv("TEST_VAR", "test_value")
		defer func() { _ = os.Unsetenv("TEST_VAR") }()

		result := getEnv("TEST_VAR", "default")
		if result != "test_value" {
			t.Errorf("Expected 'test_value', got '%s'", result)
		}
	})

	t.Run("returns default when not set", func(t *testing.T) {
		_ = os.Unsetenv("TEST_VAR_NONEXISTENT")

		result := getEnv("TEST_VAR_NONEXISTENT", "default_value")
		if result != "default_value" {
			t.Errorf("Expected 'default_value', got '%s'", result)
		}
	})
}

func TestGetEnvInt(t *testing.T) {
	t.Run("returns env value as int", func(t *testing.T) {
		_ = os.Setenv("TEST_INT", "42")
		defer func() { _ = os.Unsetenv("TEST_INT") }()

		result := getEnvInt("TEST_INT", 0)
		if result != 42 {
			t.Errorf("Expected 42, got %d", result)
		}
	})

	t.Run("returns default for non-integer", func(t *testing.T) {
		_ = os.Setenv("TEST_INT", "not_a_number")
		defer func() { _ = os.Unsetenv("TEST_INT") }()

		result := getEnvInt("TEST_INT", 100)
		if result != 100 {
			t.Errorf("Expected default 100, got %d", result)
		}
	})

	t.Run("returns default when not set", func(t *testing.T) {
		_ = os.Unsetenv("TEST_INT_NONEXISTENT")

		result := getEnvInt("TEST_INT_NONEXISTENT", 50)
		if result != 50 {
			t.Errorf("Expected default 50, got %d", result)
		}
	})
}

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"single value", "https://example.com", []string{"https://example.com"}},
		{"multiple values", "https://a.com,https://b.com", []string{"https://a.com", "https://b.com"}},
		{"trims whitespace", " https://a.com , https://b.com ", []string{"https://a.com", "https://b.com"}},
		{"filters empty entries", "https://a.com,,https://b.com", []string{"https://a.com", "https://b.com"}},
		{"trailing comma", "https://a.com,", []string{"https://a.com"}},
		{"wildcard", "*", []string{"*"}},
		{"empty string", "", nil},
		{"whitespace only", "  ,  ,  ", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitCSV(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("splitCSV(%q) = %v (len %d), want %v (len %d)", tt.input, result, len(result), tt.expected, len(tt.expected))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("splitCSV(%q)[%d] = %q, want %q", tt.input, i, v, tt.expected[i])
				}
			}
		})
	}
}

// validDevConfig returns a minimal Config that passes validation in development mode.
func validDevConfig() *Config {
	return &Config{
		Env: EnvDevelopment,
		JWT: JWTConfig{
			Secret: "development_secret_key_at_least_32_characters_long",
		},
		MFA: MFAConfig{
			Required: false,
		},
		Email: EmailConfig{
			Backend: EmailBackendMock,
		},
		MailProvider: MailProviderLob,
		Log: LogConfig{
			Format: "text",
			Level:  "info",
		},
	}
}

// validProdConfig returns a Config that passes validation in production mode.
func validProdConfig() *Config {
	return &Config{
		Env: EnvProduction,
		Server: ServerConfig{
			SecureCookies: true,
		},
		Database: DatabaseConfig{
			Password: "strong-db-password",
		},
		JWT: JWTConfig{
			Secret: "production_secret_key_at_least_32_characters_long",
		},
		MFA: MFAConfig{
			Required:      true,
			EncryptionKey: "01234567890123456789012345678901",
		},
		Mapbox: MapboxConfig{
			SecretToken: "sk.mapbox-secret-token",
		},
		MailProvider: MailProviderLob,
		Lob: LobConfig{
			APIKey: "lob-api-key",
		},
		Email: EmailConfig{
			Backend:        EmailBackendSendGrid,
			Enabled:        true,
			SendGridAPIKey: "sg-api-key",
		},
		Log: LogConfig{
			Format: "json",
			Level:  "info",
		},
	}
}

func TestValidate(t *testing.T) {
	t.Run("valid dev config passes", func(t *testing.T) {
		cfg := validDevConfig()
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("valid production config passes", func(t *testing.T) {
		cfg := validProdConfig()
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("invalid ENVIRONMENT fails", func(t *testing.T) {
		cfg := validDevConfig()
		cfg.Env = "bogus"
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for invalid ENVIRONMENT")
		}
		if !strings.Contains(err.Error(), "ENVIRONMENT") {
			t.Errorf("expected error to mention ENVIRONMENT, got: %v", err)
		}
	})

	t.Run("empty JWT_SECRET fails", func(t *testing.T) {
		cfg := validDevConfig()
		cfg.JWT.Secret = ""
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for empty JWT_SECRET")
		}
		if !strings.Contains(err.Error(), "JWT_SECRET is required") {
			t.Errorf("expected error to mention JWT_SECRET, got: %v", err)
		}
	})

	t.Run("short JWT_SECRET fails", func(t *testing.T) {
		cfg := validDevConfig()
		cfg.JWT.Secret = "too-short"
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for short JWT_SECRET")
		}
		if !strings.Contains(err.Error(), "at least 32 characters") {
			t.Errorf("expected error about min length, got: %v", err)
		}
	})

	t.Run("MFA_ENCRYPTION_KEY required when MFA enabled", func(t *testing.T) {
		cfg := validDevConfig()
		cfg.MFA.Required = true
		cfg.MFA.EncryptionKey = ""
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for missing MFA_ENCRYPTION_KEY")
		}
		if !strings.Contains(err.Error(), "MFA_ENCRYPTION_KEY is required") {
			t.Errorf("expected error about MFA_ENCRYPTION_KEY, got: %v", err)
		}
	})

	t.Run("MFA_ENCRYPTION_KEY wrong length fails", func(t *testing.T) {
		cfg := validDevConfig()
		cfg.MFA.Required = true
		cfg.MFA.EncryptionKey = "too-short"
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for wrong-length MFA_ENCRYPTION_KEY")
		}
		if !strings.Contains(err.Error(), "exactly 32 characters") {
			t.Errorf("expected error about 32 characters, got: %v", err)
		}
	})

	t.Run("invalid email backend fails", func(t *testing.T) {
		cfg := validDevConfig()
		cfg.Email.Backend = "carrier_pigeon"
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for invalid EMAIL_BACKEND")
		}
		if !strings.Contains(err.Error(), "EMAIL_BACKEND") {
			t.Errorf("expected error about EMAIL_BACKEND, got: %v", err)
		}
	})

	t.Run("invalid mail provider fails", func(t *testing.T) {
		cfg := validDevConfig()
		cfg.MailProvider = "usps"
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for invalid MAIL_PROVIDER")
		}
		if !strings.Contains(err.Error(), "MAIL_PROVIDER") {
			t.Errorf("expected error about MAIL_PROVIDER, got: %v", err)
		}
	})

	t.Run("production with SECURE_COOKIES=false fails", func(t *testing.T) {
		cfg := validProdConfig()
		cfg.Server.SecureCookies = false
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for SECURE_COOKIES=false in production")
		}
		if !strings.Contains(err.Error(), "SECURE_COOKIES") {
			t.Errorf("expected error about SECURE_COOKIES, got: %v", err)
		}
	})

	t.Run("production with mock email backend fails", func(t *testing.T) {
		cfg := validProdConfig()
		cfg.Email.Backend = EmailBackendMock
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for mock email in production")
		}
		if !strings.Contains(err.Error(), "EMAIL_BACKEND") {
			t.Errorf("expected error about EMAIL_BACKEND, got: %v", err)
		}
	})

	t.Run("production with EMAIL_ENABLED=false fails", func(t *testing.T) {
		cfg := validProdConfig()
		cfg.Email.Enabled = false
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for EMAIL_ENABLED=false in production")
		}
		if !strings.Contains(err.Error(), "EMAIL_ENABLED") {
			t.Errorf("expected error about EMAIL_ENABLED, got: %v", err)
		}
	})

	t.Run("production with empty DB password fails", func(t *testing.T) {
		cfg := validProdConfig()
		cfg.Database.Password = ""
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for empty DB_PASSWORD in production")
		}
		if !strings.Contains(err.Error(), "DB_PASSWORD") {
			t.Errorf("expected error about DB_PASSWORD, got: %v", err)
		}
	})

	t.Run("production with empty MAPBOX_SECRET_TOKEN fails", func(t *testing.T) {
		cfg := validProdConfig()
		cfg.Mapbox.SecretToken = ""
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for empty MAPBOX_SECRET_TOKEN in production")
		}
		if !strings.Contains(err.Error(), "MAPBOX_SECRET_TOKEN") {
			t.Errorf("expected error about MAPBOX_SECRET_TOKEN, got: %v", err)
		}
	})

	t.Run("production with lob and empty LOB_API_KEY fails", func(t *testing.T) {
		cfg := validProdConfig()
		cfg.Lob.APIKey = ""
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for empty LOB_API_KEY in production")
		}
		if !strings.Contains(err.Error(), "LOB_API_KEY") {
			t.Errorf("expected error about LOB_API_KEY, got: %v", err)
		}
	})

	t.Run("production with postgrid and empty POSTGRID_PRINT_API_KEY fails", func(t *testing.T) {
		cfg := validProdConfig()
		cfg.MailProvider = MailProviderPostgrid
		cfg.Postgrid.PrintMailAPIKey = ""
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for empty POSTGRID_PRINT_API_KEY in production")
		}
		if !strings.Contains(err.Error(), "POSTGRID_PRINT_API_KEY") {
			t.Errorf("expected error about POSTGRID_PRINT_API_KEY, got: %v", err)
		}
	})

	t.Run("production with sendgrid and empty SENDGRID_API_KEY fails", func(t *testing.T) {
		cfg := validProdConfig()
		cfg.Email.SendGridAPIKey = ""
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for empty SENDGRID_API_KEY in production")
		}
		if !strings.Contains(err.Error(), "SENDGRID_API_KEY") {
			t.Errorf("expected error about SENDGRID_API_KEY, got: %v", err)
		}
	})

	t.Run("production with smtp and missing credentials fails", func(t *testing.T) {
		cfg := validProdConfig()
		cfg.Email.Backend = EmailBackendSMTP
		cfg.Email.SMTPHost = ""
		cfg.Email.SMTPUser = ""
		cfg.Email.SMTPPassword = ""
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for missing SMTP credentials in production")
		}
		if !strings.Contains(err.Error(), "SMTP_HOST") {
			t.Errorf("expected error about SMTP_HOST, got: %v", err)
		}
		if !strings.Contains(err.Error(), "SMTP_USER") {
			t.Errorf("expected error about SMTP_USER, got: %v", err)
		}
		if !strings.Contains(err.Error(), "SMTP_PASSWORD") {
			t.Errorf("expected error about SMTP_PASSWORD, got: %v", err)
		}
	})

	t.Run("staging treated as production", func(t *testing.T) {
		cfg := validDevConfig()
		cfg.Env = EnvStaging
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for staging env with dev defaults")
		}
		if !strings.Contains(err.Error(), "SECURE_COOKIES") {
			t.Errorf("expected staging to enforce SECURE_COOKIES, got: %v", err)
		}
	})

	t.Run("collects multiple errors at once", func(t *testing.T) {
		cfg := &Config{
			Env:          EnvProduction,
			JWT:          JWTConfig{Secret: ""},
			Email:        EmailConfig{Backend: EmailBackendMock},
			MailProvider: MailProviderLob,
		}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected multiple errors")
		}
		errorText := err.Error()
		// Should contain at least JWT_SECRET, SECURE_COOKIES, EMAIL_BACKEND, DB_PASSWORD
		for _, keyword := range []string{"JWT_SECRET", "SECURE_COOKIES", "EMAIL_BACKEND", "DB_PASSWORD"} {
			if !strings.Contains(errorText, keyword) {
				t.Errorf("expected error to mention %s, got: %v", keyword, err)
			}
		}
	})
}

func TestEnvironment_IsProduction(t *testing.T) {
	tests := []struct {
		env      Environment
		expected bool
	}{
		{EnvProduction, true},
		{EnvStaging, true},
		{EnvDevelopment, false},
		{EnvTest, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.env), func(t *testing.T) {
			if got := tt.env.IsProduction(); got != tt.expected {
				t.Errorf("Environment(%q).IsProduction() = %v, want %v", tt.env, got, tt.expected)
			}
		})
	}
}

func TestLoad_Environment(t *testing.T) {
	orig := os.Getenv("ENVIRONMENT")
	defer func() { _ = os.Setenv("ENVIRONMENT", orig) }()

	t.Run("defaults to development", func(t *testing.T) {
		_ = os.Unsetenv("ENVIRONMENT")
		cfg := Load()
		if cfg.Env != EnvDevelopment {
			t.Errorf("expected default env %q, got %q", EnvDevelopment, cfg.Env)
		}
	})

	t.Run("loads from ENVIRONMENT", func(t *testing.T) {
		_ = os.Setenv("ENVIRONMENT", "production")
		cfg := Load()
		if cfg.Env != EnvProduction {
			t.Errorf("expected env %q, got %q", EnvProduction, cfg.Env)
		}
	})
}

func TestConsensusConfig_RequiredVotes(t *testing.T) {
	tests := []struct {
		name        string
		votePercent int
		voteFloor   int
		adminCount  int
		expected    int
	}{
		// Default config (50%, floor 3)
		{"3 admins, 50%, floor 3 -> 3 (floor)", 50, 3, 3, 3},
		{"4 admins, 50%, floor 3 -> 3 (floor wins over 2)", 50, 3, 4, 3},
		{"5 admins, 50%, floor 3 -> 3 (ceil(2.5)=3)", 50, 3, 5, 3},
		{"6 admins, 50%, floor 3 -> 3 (50%=3)", 50, 3, 6, 3},
		{"7 admins, 50%, floor 3 -> 4 (ceil(3.5)=4)", 50, 3, 7, 4},
		{"10 admins, 50%, floor 3 -> 5", 50, 3, 10, 5},
		{"20 admins, 50%, floor 3 -> 10", 50, 3, 20, 10},

		// Higher percentage (67%)
		{"3 admins, 67%, floor 3 -> 3 (floor)", 67, 3, 3, 3},
		{"6 admins, 67%, floor 3 -> 5 (ceil(4.02)=5)", 67, 3, 6, 5},
		{"10 admins, 67%, floor 3 -> 7", 67, 3, 10, 7},

		// Lower floor (2)
		{"3 admins, 50%, floor 2 -> 2 (ceil(1.5)=2)", 50, 2, 3, 2},
		{"4 admins, 50%, floor 2 -> 2", 50, 2, 4, 2},
		{"5 admins, 50%, floor 2 -> 3 (ceil(2.5)=3)", 50, 2, 5, 3},

		// Edge cases
		{"0 admins, 50%, floor 3 -> 3 (floor)", 50, 3, 0, 3},
		{"1 admin, 50%, floor 3 -> 3 (floor)", 50, 3, 1, 3},
		{"100 admins, 50%, floor 3 -> 50", 50, 3, 100, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ConsensusConfig{
				VotePercent: tt.votePercent,
				VoteFloor:   tt.voteFloor,
			}
			result := cfg.RequiredVotes(tt.adminCount)
			if result != tt.expected {
				t.Errorf("RequiredVotes(%d) = %d, want %d", tt.adminCount, result, tt.expected)
			}
		})
	}
}
