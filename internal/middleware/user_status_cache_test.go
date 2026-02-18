package middleware

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// mockStatusChecker is a test implementation of UserStatusChecker.
type mockStatusChecker struct {
	status    *UserStatus
	err       error
	callCount atomic.Int32
}

func (m *mockStatusChecker) GetUserStatus(_ context.Context, _ string) (*UserStatus, error) {
	m.callCount.Add(1)
	return m.status, m.err
}

func TestUserStatusCache_FirstCallFetchesFromChecker(t *testing.T) {
	checker := &mockStatusChecker{
		status: &UserStatus{IsBlocked: false},
	}
	cache := NewUserStatusCache(checker, time.Minute)

	status, err := cache.GetUserStatus(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.IsBlocked {
		t.Error("expected IsBlocked to be false")
	}
	if checker.callCount.Load() != 1 {
		t.Errorf("expected 1 call to checker, got %d", checker.callCount.Load())
	}
}

func TestUserStatusCache_ReturnsCachedWithinTTL(t *testing.T) {
	checker := &mockStatusChecker{
		status: &UserStatus{IsBlocked: false},
	}
	cache := NewUserStatusCache(checker, time.Minute)

	// First call
	_, _ = cache.GetUserStatus(context.Background(), "user1")
	// Second call - should use cache
	_, _ = cache.GetUserStatus(context.Background(), "user1")

	if checker.callCount.Load() != 1 {
		t.Errorf("expected 1 call (cached), got %d", checker.callCount.Load())
	}
}

func TestUserStatusCache_RefetchesAfterTTL(t *testing.T) {
	checker := &mockStatusChecker{
		status: &UserStatus{IsBlocked: false},
	}
	cache := NewUserStatusCache(checker, 10*time.Millisecond)

	// First call
	_, _ = cache.GetUserStatus(context.Background(), "user1")

	// Wait for TTL to expire
	time.Sleep(20 * time.Millisecond)

	// Second call - should re-fetch
	_, _ = cache.GetUserStatus(context.Background(), "user1")

	if checker.callCount.Load() != 2 {
		t.Errorf("expected 2 calls after TTL, got %d", checker.callCount.Load())
	}
}

func TestUserStatusCache_BlockedUser(t *testing.T) {
	checker := &mockStatusChecker{
		status: &UserStatus{IsBlocked: true},
	}
	cache := NewUserStatusCache(checker, time.Minute)

	status, err := cache.GetUserStatus(context.Background(), "blocked-user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.IsBlocked {
		t.Error("expected IsBlocked to be true")
	}
}

func TestUserStatusCache_DeletedUser(t *testing.T) {
	deletedAt := time.Now()
	checker := &mockStatusChecker{
		status: &UserStatus{DeletedAt: &deletedAt},
	}
	cache := NewUserStatusCache(checker, time.Minute)

	status, err := cache.GetUserStatus(context.Background(), "deleted-user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.DeletedAt == nil {
		t.Error("expected DeletedAt to be set")
	}
}

func TestUserStatusCache_TokenInvalidated(t *testing.T) {
	invalidatedAt := time.Now()
	checker := &mockStatusChecker{
		status: &UserStatus{TokenInvalidatedAt: &invalidatedAt},
	}
	cache := NewUserStatusCache(checker, time.Minute)

	status, err := cache.GetUserStatus(context.Background(), "invalidated-user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.TokenInvalidatedAt == nil {
		t.Error("expected TokenInvalidatedAt to be set")
	}
}

func TestUserStatusCache_InvalidateClearsEntry(t *testing.T) {
	checker := &mockStatusChecker{
		status: &UserStatus{IsBlocked: false},
	}
	cache := NewUserStatusCache(checker, time.Minute)

	// Fill cache
	_, _ = cache.GetUserStatus(context.Background(), "user1")

	// Invalidate
	cache.Invalidate("user1")

	// Next call should re-fetch
	_, _ = cache.GetUserStatus(context.Background(), "user1")

	if checker.callCount.Load() != 2 {
		t.Errorf("expected 2 calls after invalidation, got %d", checker.callCount.Load())
	}
}

func TestUserStatusCache_PropagatesErrors(t *testing.T) {
	expectedErr := errors.New("database connection failed")
	checker := &mockStatusChecker{
		err: expectedErr,
	}
	cache := NewUserStatusCache(checker, time.Minute)

	_, err := cache.GetUserStatus(context.Background(), "user1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != expectedErr {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

func TestUserStatusCache_ErrorNotCached(t *testing.T) {
	expectedErr := errors.New("transient error")
	checker := &mockStatusChecker{
		err: expectedErr,
	}
	cache := NewUserStatusCache(checker, time.Minute)

	// First call - error
	_, _ = cache.GetUserStatus(context.Background(), "user1")

	// Fix the checker
	checker.err = nil
	checker.status = &UserStatus{IsBlocked: false}

	// Second call - should retry (error wasn't cached)
	status, err := cache.GetUserStatus(context.Background(), "user1")
	if err != nil {
		t.Fatalf("expected success after fix, got error: %v", err)
	}
	if status.IsBlocked {
		t.Error("expected IsBlocked to be false")
	}
	if checker.callCount.Load() != 2 {
		t.Errorf("expected 2 calls (error not cached), got %d", checker.callCount.Load())
	}
}

func TestUserStatusCache_DifferentUsersIndependent(t *testing.T) {
	checker := &mockStatusChecker{
		status: &UserStatus{IsBlocked: false},
	}
	cache := NewUserStatusCache(checker, time.Minute)

	_, _ = cache.GetUserStatus(context.Background(), "user1")
	_, _ = cache.GetUserStatus(context.Background(), "user2")

	// Each user should result in a separate fetch
	if checker.callCount.Load() != 2 {
		t.Errorf("expected 2 calls for different users, got %d", checker.callCount.Load())
	}
}
