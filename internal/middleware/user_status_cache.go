package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

type cachedStatus struct {
	status    *models.UserStatus
	fetchedAt time.Time
}

// UserStatusCache wraps a models.UserStatusChecker with an in-memory TTL cache.
type UserStatusCache struct {
	checker models.UserStatusChecker
	ttl     time.Duration
	entries sync.Map
}

// NewUserStatusCache creates a new cache wrapping the given checker.
func NewUserStatusCache(checker models.UserStatusChecker, ttl time.Duration) *UserStatusCache {
	return &UserStatusCache{
		checker: checker,
		ttl:     ttl,
	}
}

// GetUserStatus returns the cached status if fresh, otherwise fetches from the underlying checker.
func (c *UserStatusCache) GetUserStatus(ctx context.Context, userID string) (*models.UserStatus, error) {
	if entry, ok := c.entries.Load(userID); ok {
		cached := entry.(*cachedStatus)
		if time.Since(cached.fetchedAt) < c.ttl {
			return cached.status, nil
		}
	}

	status, err := c.checker.GetUserStatus(ctx, userID)
	if err != nil {
		return nil, err
	}

	c.entries.Store(userID, &cachedStatus{
		status:    status,
		fetchedAt: time.Now(),
	})

	return status, nil
}

// Invalidate removes a user from the cache so the next check fetches fresh data.
func (c *UserStatusCache) Invalidate(userID string) {
	c.entries.Delete(userID)
}
