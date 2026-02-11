package services

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
)

func setupRateLimitTestDB(t *testing.T) *sql.DB {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping database tests")
	}

	port := 3306
	if portStr := os.Getenv("TEST_DB_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	cfg := &config.DatabaseConfig{
		Host:     host,
		Port:     port,
		User:     os.Getenv("TEST_DB_USER"),
		Password: os.Getenv("TEST_DB_PASSWORD"),
		Name:     os.Getenv("TEST_DB_NAME"),
		Charset:  "utf8mb4",
	}

	if cfg.User == "" {
		cfg.User = "test"
	}
	if cfg.Password == "" {
		cfg.Password = "testpassword"
	}
	if cfg.Name == "" {
		cfg.Name = "communityrapidresponse_test"
	}

	db, err := sql.Open("mysql", cfg.DSN())
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Clean up any existing rate limit entries
	_, _ = db.Exec("DELETE FROM rate_limits")

	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM rate_limits")
		_ = db.Close()
	})

	return db
}

func TestDBRateLimiter_Allow(t *testing.T) {
	db := setupRateLimitTestDB(t)
	limiter := NewDBRateLimiter(db)
	ctx := context.Background()

	t.Run("allows requests under limit", func(t *testing.T) {
		key := "test-ip-1"
		defer func() { _ = limiter.Reset(ctx, key) }()

		for i := 0; i < 5; i++ {
			allowed, remaining, _, err := limiter.Allow(ctx, key, 10, time.Minute)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if !allowed {
				t.Errorf("Request %d should be allowed", i+1)
			}
			expectedRemaining := 10 - (i + 1)
			if remaining != expectedRemaining {
				t.Errorf("Expected remaining %d, got %d", expectedRemaining, remaining)
			}
		}
	})

	t.Run("blocks requests over limit", func(t *testing.T) {
		key := "test-ip-2"
		defer func() { _ = limiter.Reset(ctx, key) }()

		// Use up the limit
		for i := 0; i < 3; i++ {
			allowed, _, _, err := limiter.Allow(ctx, key, 3, time.Minute)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if !allowed {
				t.Errorf("Request %d should be allowed (under limit)", i+1)
			}
		}

		// Next request should be blocked
		allowed, remaining, _, err := limiter.Allow(ctx, key, 3, time.Minute)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if allowed {
			t.Error("Request should be blocked (over limit)")
		}
		if remaining != 0 {
			t.Errorf("Expected remaining 0, got %d", remaining)
		}
	})

	t.Run("returns correct reset time", func(t *testing.T) {
		key := "test-ip-3"
		defer func() { _ = limiter.Reset(ctx, key) }()

		window := time.Minute
		before := time.Now().UTC().Truncate(window).Add(window)

		_, _, resetTime, err := limiter.Allow(ctx, key, 10, window)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		after := time.Now().UTC().Truncate(window).Add(window)

		if resetTime.Before(before) || resetTime.After(after) {
			t.Errorf("Reset time %v should be between %v and %v", resetTime, before, after)
		}
	})

	t.Run("different keys are independent", func(t *testing.T) {
		key1 := "test-ip-4a"
		key2 := "test-ip-4b"
		defer func() {
			_ = limiter.Reset(ctx, key1)
			_ = limiter.Reset(ctx, key2)
		}()

		// Use up limit for key1
		for i := 0; i < 3; i++ {
			_, _, _, _ = limiter.Allow(ctx, key1, 3, time.Minute)
		}

		// key2 should still be allowed
		allowed, remaining, _, err := limiter.Allow(ctx, key2, 3, time.Minute)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !allowed {
			t.Error("key2 should be allowed (independent from key1)")
		}
		if remaining != 2 {
			t.Errorf("Expected remaining 2, got %d", remaining)
		}
	})
}

func TestDBRateLimiter_Reset(t *testing.T) {
	db := setupRateLimitTestDB(t)
	limiter := NewDBRateLimiter(db)
	ctx := context.Background()

	key := "test-ip-reset"

	// Use up limit
	for i := 0; i < 5; i++ {
		_, _, _, _ = limiter.Allow(ctx, key, 5, time.Minute)
	}

	// Verify blocked
	allowed, _, _, _ := limiter.Allow(ctx, key, 5, time.Minute)
	if allowed {
		t.Error("Should be blocked before reset")
	}

	// Reset
	err := limiter.Reset(ctx, key)
	if err != nil {
		t.Fatalf("Failed to reset: %v", err)
	}

	// Should be allowed again
	allowed, remaining, _, err := limiter.Allow(ctx, key, 5, time.Minute)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("Should be allowed after reset")
	}
	if remaining != 4 {
		t.Errorf("Expected remaining 4, got %d", remaining)
	}

	// Cleanup
	_ = limiter.Reset(ctx, key)
}

func TestDBRateLimiter_Cleanup(t *testing.T) {
	db := setupRateLimitTestDB(t)
	limiter := NewDBRateLimiter(db)
	ctx := context.Background()

	key := "test-ip-cleanup"

	// Create an entry
	_, _, _, err := limiter.Allow(ctx, key, 10, time.Minute)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify entry exists
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM rate_limits WHERE limit_key = ?", key).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 entry, got %d", count)
	}

	// Cleanup entries older than 0 (i.e., all entries)
	// First, manually backdate the entry
	_, err = db.Exec("UPDATE rate_limits SET window_start = DATE_SUB(NOW(), INTERVAL 2 HOUR) WHERE limit_key = ?", key)
	if err != nil {
		t.Fatalf("Failed to backdate: %v", err)
	}

	// Cleanup entries older than 1 hour
	err = limiter.Cleanup(ctx, time.Hour)
	if err != nil {
		t.Fatalf("Failed to cleanup: %v", err)
	}

	// Verify entry removed
	err = db.QueryRow("SELECT COUNT(*) FROM rate_limits WHERE limit_key = ?", key).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 entries after cleanup, got %d", count)
	}
}

func TestNoOpRateLimiter(t *testing.T) {
	limiter := NewNoOpRateLimiter()
	ctx := context.Background()

	t.Run("always allows requests", func(t *testing.T) {
		for i := 0; i < 1000; i++ {
			allowed, remaining, _, err := limiter.Allow(ctx, "any-key", 1, time.Second)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if !allowed {
				t.Error("NoOp limiter should always allow")
			}
			if remaining != 1 {
				t.Errorf("Expected remaining 1, got %d", remaining)
			}
		}
	})

	t.Run("reset does nothing", func(t *testing.T) {
		err := limiter.Reset(ctx, "any-key")
		if err != nil {
			t.Errorf("Reset should not error: %v", err)
		}
	})

	t.Run("cleanup does nothing", func(t *testing.T) {
		err := limiter.Cleanup(ctx, time.Hour)
		if err != nil {
			t.Errorf("Cleanup should not error: %v", err)
		}
	})
}

func TestInMemoryRateLimiter_Allow(t *testing.T) {
	limiter := NewInMemoryRateLimiter()
	ctx := context.Background()

	t.Run("allows requests under limit", func(t *testing.T) {
		key := "test-ip-1"
		defer func() { _ = limiter.Reset(ctx, key) }()

		for i := 0; i < 5; i++ {
			allowed, remaining, _, err := limiter.Allow(ctx, key, 10, time.Minute)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if !allowed {
				t.Errorf("Request %d should be allowed", i+1)
			}
			expectedRemaining := 10 - (i + 1)
			if remaining != expectedRemaining {
				t.Errorf("Expected remaining %d, got %d", expectedRemaining, remaining)
			}
		}
	})

	t.Run("blocks requests over limit", func(t *testing.T) {
		key := "test-ip-2"
		defer func() { _ = limiter.Reset(ctx, key) }()

		// Use up the limit
		for i := 0; i < 3; i++ {
			allowed, _, _, err := limiter.Allow(ctx, key, 3, time.Minute)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if !allowed {
				t.Errorf("Request %d should be allowed (under limit)", i+1)
			}
		}

		// Next request should be blocked
		allowed, remaining, _, err := limiter.Allow(ctx, key, 3, time.Minute)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if allowed {
			t.Error("Request should be blocked (over limit)")
		}
		if remaining != 0 {
			t.Errorf("Expected remaining 0, got %d", remaining)
		}
	})

	t.Run("returns correct reset time", func(t *testing.T) {
		key := "test-ip-3"
		defer func() { _ = limiter.Reset(ctx, key) }()

		window := time.Minute
		before := time.Now().UTC().Truncate(window).Add(window)

		_, _, resetTime, err := limiter.Allow(ctx, key, 10, window)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		after := time.Now().UTC().Truncate(window).Add(window)

		if resetTime.Before(before) || resetTime.After(after) {
			t.Errorf("Reset time %v should be between %v and %v", resetTime, before, after)
		}
	})

	t.Run("different keys are independent", func(t *testing.T) {
		key1 := "test-ip-4a"
		key2 := "test-ip-4b"
		defer func() {
			_ = limiter.Reset(ctx, key1)
			_ = limiter.Reset(ctx, key2)
		}()

		// Use up limit for key1
		for i := 0; i < 3; i++ {
			_, _, _, _ = limiter.Allow(ctx, key1, 3, time.Minute)
		}

		// key2 should still be allowed
		allowed, remaining, _, err := limiter.Allow(ctx, key2, 3, time.Minute)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !allowed {
			t.Error("key2 should be allowed (independent from key1)")
		}
		if remaining != 2 {
			t.Errorf("Expected remaining 2, got %d", remaining)
		}
	})
}

func TestInMemoryRateLimiter_Reset(t *testing.T) {
	limiter := NewInMemoryRateLimiter()
	ctx := context.Background()

	key := "test-ip-reset"

	// Use up limit
	for i := 0; i < 5; i++ {
		_, _, _, _ = limiter.Allow(ctx, key, 5, time.Minute)
	}

	// Verify blocked
	allowed, _, _, _ := limiter.Allow(ctx, key, 5, time.Minute)
	if allowed {
		t.Error("Should be blocked before reset")
	}

	// Reset
	err := limiter.Reset(ctx, key)
	if err != nil {
		t.Fatalf("Failed to reset: %v", err)
	}

	// Should be allowed again
	allowed, remaining, _, err := limiter.Allow(ctx, key, 5, time.Minute)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("Should be allowed after reset")
	}
	if remaining != 4 {
		t.Errorf("Expected remaining 4, got %d", remaining)
	}
}

func TestInMemoryRateLimiter_Cleanup(t *testing.T) {
	limiter := NewInMemoryRateLimiter()
	ctx := context.Background()

	// Create entries with different ages
	recentKey := "test-recent"
	oldKey := "test-old"

	_, _, _, _ = limiter.Allow(ctx, recentKey, 10, time.Minute)
	_, _, _, _ = limiter.Allow(ctx, oldKey, 10, time.Minute)

	// Manually backdate the old entry
	limiter.mu.Lock()
	limiter.entries[oldKey].windowStart = time.Now().UTC().Add(-2 * time.Hour)
	limiter.mu.Unlock()

	// Cleanup entries older than 1 hour
	err := limiter.Cleanup(ctx, time.Hour)
	if err != nil {
		t.Fatalf("Failed to cleanup: %v", err)
	}

	// Old entry should be removed
	limiter.mu.Lock()
	_, oldExists := limiter.entries[oldKey]
	_, recentExists := limiter.entries[recentKey]
	limiter.mu.Unlock()

	if oldExists {
		t.Error("Old entry should have been removed by cleanup")
	}
	if !recentExists {
		t.Error("Recent entry should have been preserved by cleanup")
	}
}

func TestInMemoryRateLimiter_Concurrent(t *testing.T) {
	limiter := NewInMemoryRateLimiter()
	ctx := context.Background()
	key := "test-concurrent"
	limit := 100
	goroutines := 50
	requestsPerGoroutine := 10

	var wg sync.WaitGroup
	var allowedCount int64
	var mu sync.Mutex

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				allowed, _, _, err := limiter.Allow(ctx, key, limit, time.Minute)
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
					return
				}
				if allowed {
					mu.Lock()
					allowedCount++
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	// Total requests = 500, limit = 100, so exactly 100 should be allowed
	if allowedCount != int64(limit) {
		t.Errorf("Expected exactly %d allowed requests, got %d (total requests: %d)", limit, allowedCount, goroutines*requestsPerGoroutine)
	}
}
