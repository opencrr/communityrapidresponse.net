package testutil

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// TestConfig returns a test configuration
func TestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Host:         "localhost",
			Port:         8080,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		},
		Database: config.DatabaseConfig{
			Host:     getEnvOrDefault("TEST_DB_HOST", "localhost"),
			Port:     3306,
			User:     getEnvOrDefault("TEST_DB_USER", "communityrapidresponse_test"),
			Password: getEnvOrDefault("TEST_DB_PASSWORD", "test_password"),
			Name:     getEnvOrDefault("TEST_DB_NAME", "communityrapidresponse_test"),
			Charset:  "utf8mb4",
		},
		JWT: config.JWTConfig{
			Secret:          "test_secret_key_for_testing_only_32chars",
			ExpirationHours: 24,
			Issuer:          "communityrapidresponse_test",
		},
		Mapbox: config.MapboxConfig{
			PublicToken: "test_public_token",
			SecretToken: "test_secret_token",
		},
		Postgrid: config.PostgridConfig{
			AddressVerificationAPIKey:  "", // Empty for mock mode
			AddressVerificationBaseURL: "https://api.postgrid.com/v1",
			PrintMailAPIKey:            "", // Empty for mock mode
			PrintMailBaseURL:           "https://api.postgrid.com/print-mail/v1",
		},
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// TestDB creates a test database connection
// Returns nil if database is not available (for unit tests)
func TestDB(t *testing.T) *database.DB {
	t.Helper()

	cfg := TestConfig()
	db, err := database.New(&cfg.Database)
	if err != nil {
		t.Skipf("Skipping test: database not available: %v", err)
		return nil
	}

	return db
}

// CleanupDB cleans all tables for a fresh test
func CleanupDB(t *testing.T, db *database.DB) {
	t.Helper()

	tables := []string{
		"email_notifications",
		"audit_log",
		"blocked_users",
		"blocklist_votes",
		"blocklist_proposals",
		"proposal_wrapped_keys",
		"secret_update_votes",
		"secret_update_proposals",
		"encrypted_secret_keys",
		"encrypted_secrets",
		"meshtastic_channels",
		"deletion_votes",
		"deletion_proposals",
		"signal_groups",
		"vouches",
		"verification_requests",
		"admin_boundaries",
		"user_regions",
		"geographic_regions",
		"user_encryption_keys",
		"users",
	}

	for _, table := range tables {
		_, err := db.ExecContext(context.Background(), "DELETE FROM "+table)
		if err != nil {
			t.Logf("Warning: failed to clean table %s: %v", table, err)
		}
	}
}

// CreateTestUser creates a user for testing
func CreateTestUser(t *testing.T, db *database.DB, tier models.VerificationTier) *models.User {
	t.Helper()

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("testpassword123"), 12)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	user := &models.User{
		ID:               uuid.New().String(),
		Username:         fmt.Sprintf("testuser_%s", uuid.New().String()[:8]),
		Email:            fmt.Sprintf("test_%s@example.com", uuid.New().String()[:8]),
		PasswordHash:     string(hashedPassword),
		VerificationTier: tier,
		CreatedAt:        time.Now().UTC(),
	}

	query := `
		INSERT INTO users (id, username, email, password_hash, verification_tier, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	_, err = db.ExecContext(context.Background(), query,
		user.ID, user.Username, user.Email, user.PasswordHash, user.VerificationTier, user.CreatedAt)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	return user
}

// CreateTestRegion creates a region for testing
func CreateTestRegion(t *testing.T, db *database.DB, name string, regionType models.RegionType, createdBy string) *models.GeographicRegion {
	t.Helper()

	region := &models.GeographicRegion{
		ID:         uuid.New().String(),
		Name:       name,
		RegionType: regionType,
		CreatedBy:  &createdBy,
		CreatedAt:  time.Now().UTC(),
	}

	// Create a simple polygon for San Francisco area
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`

	query := `
		INSERT INTO geographic_regions (id, name, region_type, parent_region_id, geometry, created_by, created_at)
		VALUES (?, ?, ?, NULL, ST_GeomFromGeoJSON(?), ?, ?)
	`
	_, err := db.ExecContext(context.Background(), query,
		region.ID, region.Name, region.RegionType, geoJSON, region.CreatedBy, region.CreatedAt)
	if err != nil {
		t.Fatalf("Failed to create test region: %v", err)
	}

	return region
}

// AddUserToRegion adds a user to a region for testing
func AddUserToRegion(t *testing.T, db *database.DB, userID, regionID string, isAdmin bool) {
	t.Helper()

	query := `
		INSERT INTO user_regions (id, user_id, region_id, is_admin, verified_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(context.Background(), query,
		uuid.New().String(), userID, regionID, isAdmin, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to add user to region: %v", err)
	}
}

// CreateTestSignalGroup creates a signal group for testing
func CreateTestSignalGroup(t *testing.T, db *database.DB, regionID, createdBy string) *models.SignalGroup {
	t.Helper()

	group := &models.SignalGroup{
		ID:        uuid.New().String(),
		RegionID:  &regionID,
		GroupName: fmt.Sprintf("Test Group %s", uuid.New().String()[:8]),
		CreatedBy: &createdBy,
		CreatedAt: time.Now().UTC(),
		IsActive:  true,
	}

	query := `
		INSERT INTO signal_groups (id, region_id, group_name, created_by, created_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(context.Background(), query,
		group.ID, group.RegionID, group.GroupName, group.CreatedBy, group.CreatedAt, group.IsActive)
	if err != nil {
		t.Fatalf("Failed to create test signal group: %v", err)
	}

	return group
}

// TestJWTAuth returns a JWT auth middleware for testing
func TestJWTAuth() *middleware.JWTAuth {
	cfg := TestConfig()
	return middleware.NewJWTAuth(&cfg.JWT)
}

// GenerateTestToken generates a JWT token for a user
func GenerateTestToken(t *testing.T, user *models.User) string {
	t.Helper()

	jwtAuth := TestJWTAuth()
	token, err := jwtAuth.GenerateToken(user)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}
	return token
}

// HTTPTestCase represents a test case for HTTP handlers
type HTTPTestCase struct {
	Name           string
	Method         string
	Path           string
	Body           interface{}
	Token          string
	ExpectedStatus int
	ExpectedBody   map[string]interface{}
	CheckResponse  func(t *testing.T, resp *http.Response, body []byte)
}

// RunHTTPTest runs a single HTTP test case
func RunHTTPTest(t *testing.T, handler http.Handler, tc HTTPTestCase) {
	t.Helper()

	var bodyReader io.Reader
	if tc.Body != nil {
		bodyBytes, err := json.Marshal(tc.Body)
		if err != nil {
			t.Fatalf("Failed to marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req := httptest.NewRequest(tc.Method, tc.Path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if tc.Token != "" {
		req.Header.Set("Authorization", "Bearer "+tc.Token)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if resp.StatusCode != tc.ExpectedStatus {
		t.Errorf("Expected status %d, got %d. Body: %s", tc.ExpectedStatus, resp.StatusCode, string(body))
	}

	if tc.ExpectedBody != nil {
		var respBody map[string]interface{}
		if err := json.Unmarshal(body, &respBody); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		for key, expectedValue := range tc.ExpectedBody {
			if actualValue, ok := respBody[key]; !ok {
				t.Errorf("Expected key %q not found in response", key)
			} else if fmt.Sprintf("%v", actualValue) != fmt.Sprintf("%v", expectedValue) {
				t.Errorf("Expected %q = %v, got %v", key, expectedValue, actualValue)
			}
		}
	}

	if tc.CheckResponse != nil {
		tc.CheckResponse(t, resp, body)
	}
}

// MockDB is a mock database for unit tests
type MockDB struct {
	ExecFunc     func(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryFunc    func(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowFunc func(ctx context.Context, query string, args ...interface{}) *sql.Row
}
