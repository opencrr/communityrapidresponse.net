package config

import (
	"os"
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
