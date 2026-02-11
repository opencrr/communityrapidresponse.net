package services

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// RateLimiter defines the interface for rate limiting implementations.
// This interface allows swapping between database, Redis, or in-memory backends.
type RateLimiter interface {
	// Allow checks if a request should be allowed and increments the counter.
	// Returns (allowed bool, remaining int, resetTime time.Time, err error)
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Time, error)

	// Reset clears the rate limit for a specific key (useful for testing)
	Reset(ctx context.Context, key string) error

	// Cleanup removes expired entries (should be called periodically)
	Cleanup(ctx context.Context, olderThan time.Duration) error
}

// RateLimitConfig holds configuration for rate limiting
type RateLimitConfig struct {
	// IPRateLimit is requests per minute per IP
	IPRateLimit int
	// IPRateWindow is the time window for IP rate limiting
	IPRateWindow time.Duration
	// Enabled allows disabling rate limiting (e.g., for testing)
	Enabled bool
}

// DefaultRateLimitConfig returns sensible defaults
func DefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		IPRateLimit:  100,
		IPRateWindow: time.Minute,
		Enabled:      true,
	}
}

// DBRateLimiter implements RateLimiter using MariaDB/MySQL
type DBRateLimiter struct {
	db *sql.DB
}

// NewDBRateLimiter creates a new database-backed rate limiter
func NewDBRateLimiter(db *sql.DB) *DBRateLimiter {
	return &DBRateLimiter{db: db}
}

// Allow checks if a request should be allowed and increments the counter
func (r *DBRateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
	windowStart := time.Now().UTC().Truncate(window)
	windowEnd := windowStart.Add(window)

	// Use a transaction to ensure atomicity
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, time.Time{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Try to increment existing record or insert new one
	// Using INSERT ... ON DUPLICATE KEY UPDATE for atomic upsert
	query := `
		INSERT INTO rate_limits (limit_type, limit_key, request_count, window_start)
		VALUES ('ip', ?, 1, ?)
		ON DUPLICATE KEY UPDATE
			request_count = IF(window_start = VALUES(window_start), request_count + 1, 1),
			window_start = VALUES(window_start)
	`
	_, err = tx.ExecContext(ctx, query, key, windowStart)
	if err != nil {
		return false, 0, time.Time{}, fmt.Errorf("failed to update rate limit: %w", err)
	}

	// Get current count
	var count int
	countQuery := `
		SELECT request_count FROM rate_limits
		WHERE limit_type = 'ip' AND limit_key = ? AND window_start = ?
	`
	err = tx.QueryRowContext(ctx, countQuery, key, windowStart).Scan(&count)
	if err != nil {
		return false, 0, time.Time{}, fmt.Errorf("failed to get rate limit count: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return false, 0, time.Time{}, fmt.Errorf("failed to commit transaction: %w", err)
	}

	remaining := limit - count
	if remaining < 0 {
		remaining = 0
	}

	allowed := count <= limit
	return allowed, remaining, windowEnd, nil
}

// Reset clears the rate limit for a specific key
func (r *DBRateLimiter) Reset(ctx context.Context, key string) error {
	query := `DELETE FROM rate_limits WHERE limit_key = ?`
	_, err := r.db.ExecContext(ctx, query, key)
	return err
}

// Cleanup removes expired entries
func (r *DBRateLimiter) Cleanup(ctx context.Context, olderThan time.Duration) error {
	cutoff := time.Now().UTC().Add(-olderThan)
	query := `DELETE FROM rate_limits WHERE window_start < ?`
	_, err := r.db.ExecContext(ctx, query, cutoff)
	return err
}

// StartCleanupWorker starts a background goroutine that periodically cleans up old entries
func (r *DBRateLimiter) StartCleanupWorker(ctx context.Context, interval time.Duration, maxAge time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := r.Cleanup(ctx, maxAge); err != nil {
					// Log error but continue
					fmt.Printf("rate limit cleanup error: %v\n", err)
				}
			}
		}
	}()
}

// NoOpRateLimiter is a rate limiter that always allows requests (for testing/disabled mode)
type NoOpRateLimiter struct{}

// NewNoOpRateLimiter creates a rate limiter that always allows requests
func NewNoOpRateLimiter() *NoOpRateLimiter {
	return &NoOpRateLimiter{}
}

// Allow always returns true
func (r *NoOpRateLimiter) Allow(_ context.Context, _ string, limit int, window time.Duration) (bool, int, time.Time, error) {
	return true, limit, time.Now().Add(window), nil
}

// Reset does nothing
func (r *NoOpRateLimiter) Reset(_ context.Context, _ string) error {
	return nil
}

// Cleanup does nothing
func (r *NoOpRateLimiter) Cleanup(_ context.Context, _ time.Duration) error {
	return nil
}

// CleanableRateLimiter extends RateLimiter with a background cleanup worker.
// DB and in-memory backends need periodic cleanup; Redis handles TTL natively.
type CleanableRateLimiter interface {
	RateLimiter
	StartCleanupWorker(ctx context.Context, interval time.Duration, maxAge time.Duration)
}

// rateLimitEntry tracks request count within a time window
type rateLimitEntry struct {
	count       int
	windowStart time.Time
}

// InMemoryRateLimiter implements RateLimiter using an in-memory map.
// Eliminates ~4 DB round-trips per rate-limited request.
// Trade-off: state is lost on restart (acceptable with Nginx rate limiting as first line of defense).
type InMemoryRateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateLimitEntry
}

// NewInMemoryRateLimiter creates a new in-memory rate limiter
func NewInMemoryRateLimiter() *InMemoryRateLimiter {
	return &InMemoryRateLimiter{
		entries: make(map[string]*rateLimitEntry),
	}
}

// Allow checks if a request should be allowed and increments the counter
func (r *InMemoryRateLimiter) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
	windowStart := time.Now().UTC().Truncate(window)
	windowEnd := windowStart.Add(window)

	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[key]
	if !exists || entry.windowStart != windowStart {
		// New key or new window — start fresh
		r.entries[key] = &rateLimitEntry{
			count:       1,
			windowStart: windowStart,
		}
		return true, limit - 1, windowEnd, nil
	}

	entry.count++
	remaining := limit - entry.count
	if remaining < 0 {
		remaining = 0
	}

	allowed := entry.count <= limit
	return allowed, remaining, windowEnd, nil
}

// Reset clears the rate limit for a specific key
func (r *InMemoryRateLimiter) Reset(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, key)
	return nil
}

// Cleanup removes entries older than the given threshold
func (r *InMemoryRateLimiter) Cleanup(_ context.Context, olderThan time.Duration) error {
	cutoff := time.Now().UTC().Add(-olderThan)

	r.mu.Lock()
	defer r.mu.Unlock()

	for key, entry := range r.entries {
		if entry.windowStart.Before(cutoff) {
			delete(r.entries, key)
		}
	}
	return nil
}

// StartCleanupWorker starts a background goroutine that periodically cleans up old entries
func (r *InMemoryRateLimiter) StartCleanupWorker(ctx context.Context, interval time.Duration, maxAge time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := r.Cleanup(ctx, maxAge); err != nil {
					fmt.Printf("rate limit cleanup error: %v\n", err)
				}
			}
		}
	}()
}
