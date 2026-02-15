package database

import (
	"context"
	"testing"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

func TestUserRepository_IncrementFailedLoginAttempts(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	// Cleanup before test
	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'lockout_incr@example.com'")

	user := &models.User{
		Username:     "lockout_incr",
		Email:        "lockout_incr@example.com",
		PasswordHash: "hash",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	t.Run("increments from zero", func(t *testing.T) {
		count, err := repo.IncrementFailedLoginAttempts(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to increment: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected count 1, got %d", count)
		}
	})

	t.Run("increments again", func(t *testing.T) {
		count, err := repo.IncrementFailedLoginAttempts(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to increment: %v", err)
		}
		if count != 2 {
			t.Errorf("Expected count 2, got %d", count)
		}
	})

	t.Run("user reflects new count via GetByID", func(t *testing.T) {
		retrieved, err := repo.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if retrieved.FailedLoginAttempts != 2 {
			t.Errorf("Expected FailedLoginAttempts=2, got %d", retrieved.FailedLoginAttempts)
		}
	})

	t.Run("count visible via GetByEmail", func(t *testing.T) {
		retrieved, err := repo.GetByEmail(ctx, "lockout_incr@example.com")
		if err != nil {
			t.Fatalf("Failed to get user by email: %v", err)
		}
		if retrieved.FailedLoginAttempts != 2 {
			t.Errorf("Expected FailedLoginAttempts=2 via GetByEmail, got %d", retrieved.FailedLoginAttempts)
		}
	})

	t.Run("count visible via GetByEmailOrUsername", func(t *testing.T) {
		retrieved, err := repo.GetByEmailOrUsername(ctx, "lockout_incr@example.com")
		if err != nil {
			t.Fatalf("Failed to get user by email/username: %v", err)
		}
		if retrieved.FailedLoginAttempts != 2 {
			t.Errorf("Expected FailedLoginAttempts=2 via GetByEmailOrUsername, got %d", retrieved.FailedLoginAttempts)
		}
	})

	t.Run("count visible via GetByUsername", func(t *testing.T) {
		retrieved, err := repo.GetByUsername(ctx, "lockout_incr")
		if err != nil {
			t.Fatalf("Failed to get user by username: %v", err)
		}
		if retrieved.FailedLoginAttempts != 2 {
			t.Errorf("Expected FailedLoginAttempts=2 via GetByUsername, got %d", retrieved.FailedLoginAttempts)
		}
	})
}

func TestUserRepository_IncrementFailedLoginAttempts_ToThreshold(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'lockout_threshold@example.com'")

	user := &models.User{
		Username:     "lockout_threshold",
		Email:        "lockout_threshold@example.com",
		PasswordHash: "hash",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	// Increment exactly 10 times (the lockout threshold)
	for i := 1; i <= 10; i++ {
		count, err := repo.IncrementFailedLoginAttempts(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to increment at step %d: %v", i, err)
		}
		if count != i {
			t.Errorf("Step %d: expected count %d, got %d", i, i, count)
		}
	}

	// Verify final state
	retrieved, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}
	if retrieved.FailedLoginAttempts != 10 {
		t.Errorf("Expected FailedLoginAttempts=10, got %d", retrieved.FailedLoginAttempts)
	}

	// Can increment beyond threshold (counter keeps going)
	count, err := repo.IncrementFailedLoginAttempts(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to increment beyond threshold: %v", err)
	}
	if count != 11 {
		t.Errorf("Expected count 11 beyond threshold, got %d", count)
	}
}

func TestUserRepository_NewUserDefaults(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'lockout_defaults@example.com'")

	user := &models.User{
		Username:     "lockout_defaults",
		Email:        "lockout_defaults@example.com",
		PasswordHash: "hash",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	retrieved, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}

	if retrieved.FailedLoginAttempts != 0 {
		t.Errorf("New user should have FailedLoginAttempts=0, got %d", retrieved.FailedLoginAttempts)
	}
	if retrieved.LockedUntil != nil {
		t.Errorf("New user should have LockedUntil=nil, got %v", retrieved.LockedUntil)
	}
}

func TestUserRepository_LockAccount(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'lockout_lock@example.com'")

	user := &models.User{
		Username:     "lockout_lock",
		Email:        "lockout_lock@example.com",
		PasswordHash: "hash",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	t.Run("sets locked_until timestamp", func(t *testing.T) {
		lockUntil := time.Now().Add(15 * time.Minute).UTC()
		if err := repo.LockAccount(ctx, user.ID, lockUntil); err != nil {
			t.Fatalf("Failed to lock account: %v", err)
		}

		retrieved, err := repo.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if retrieved.LockedUntil == nil {
			t.Fatal("Expected LockedUntil to be set")
		}
		// Allow 2-second tolerance for time comparison
		diff := retrieved.LockedUntil.Sub(lockUntil)
		if diff < -2*time.Second || diff > 2*time.Second {
			t.Errorf("LockedUntil mismatch: expected ~%v, got %v", lockUntil, *retrieved.LockedUntil)
		}
	})

	t.Run("overwrites existing lock with new time", func(t *testing.T) {
		// First lock
		firstLock := time.Now().Add(5 * time.Minute).UTC()
		_ = repo.LockAccount(ctx, user.ID, firstLock)

		// Second lock with a longer duration
		secondLock := time.Now().Add(30 * time.Minute).UTC()
		if err := repo.LockAccount(ctx, user.ID, secondLock); err != nil {
			t.Fatalf("Failed to overwrite lock: %v", err)
		}

		retrieved, err := repo.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if retrieved.LockedUntil == nil {
			t.Fatal("Expected LockedUntil to be set")
		}
		diff := retrieved.LockedUntil.Sub(secondLock)
		if diff < -2*time.Second || diff > 2*time.Second {
			t.Errorf("Expected lock to be overwritten to ~%v, got %v", secondLock, *retrieved.LockedUntil)
		}
	})

	t.Run("locked_until visible via GetByEmailOrUsername", func(t *testing.T) {
		lockUntil := time.Now().Add(10 * time.Minute).UTC()
		_ = repo.LockAccount(ctx, user.ID, lockUntil)

		retrieved, err := repo.GetByEmailOrUsername(ctx, "lockout_lock@example.com")
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if retrieved.LockedUntil == nil {
			t.Error("Expected LockedUntil to be visible via GetByEmailOrUsername")
		}
	})
}

func TestUserRepository_ResetFailedLoginAttempts(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'lockout_reset@example.com'")

	user := &models.User{
		Username:     "lockout_reset",
		Email:        "lockout_reset@example.com",
		PasswordHash: "hash",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	t.Run("clears both counter and lock", func(t *testing.T) {
		// Increment a few times and lock
		for i := 0; i < 5; i++ {
			_, _ = repo.IncrementFailedLoginAttempts(ctx, user.ID)
		}
		_ = repo.LockAccount(ctx, user.ID, time.Now().Add(15*time.Minute))

		// Verify pre-reset state
		before, _ := repo.GetByID(ctx, user.ID)
		if before.FailedLoginAttempts != 5 {
			t.Fatalf("Pre-condition failed: expected 5 failed attempts, got %d", before.FailedLoginAttempts)
		}
		if before.LockedUntil == nil {
			t.Fatal("Pre-condition failed: expected LockedUntil to be set")
		}

		// Reset
		if err := repo.ResetFailedLoginAttempts(ctx, user.ID); err != nil {
			t.Fatalf("Failed to reset: %v", err)
		}

		retrieved, err := repo.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if retrieved.FailedLoginAttempts != 0 {
			t.Errorf("Expected FailedLoginAttempts=0, got %d", retrieved.FailedLoginAttempts)
		}
		if retrieved.LockedUntil != nil {
			t.Errorf("Expected LockedUntil to be nil, got %v", retrieved.LockedUntil)
		}
	})

	t.Run("is idempotent on already-reset user", func(t *testing.T) {
		// Reset on a user that's already at zero
		if err := repo.ResetFailedLoginAttempts(ctx, user.ID); err != nil {
			t.Fatalf("Failed to reset already-reset user: %v", err)
		}

		retrieved, err := repo.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if retrieved.FailedLoginAttempts != 0 {
			t.Errorf("Expected FailedLoginAttempts=0, got %d", retrieved.FailedLoginAttempts)
		}
	})

	t.Run("allows re-increment after reset", func(t *testing.T) {
		// Reset first
		_ = repo.ResetFailedLoginAttempts(ctx, user.ID)

		// Increment again
		count, err := repo.IncrementFailedLoginAttempts(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to increment after reset: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected count 1 after reset+increment, got %d", count)
		}
	})
}

func TestUserRepository_LockoutFields_InSearch(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'lockout_search@example.com'")

	user := &models.User{
		Username:     "lockout_search",
		Email:        "lockout_search@example.com",
		PasswordHash: "hash",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	// Set some lockout state
	for i := 0; i < 3; i++ {
		_, _ = repo.IncrementFailedLoginAttempts(ctx, user.ID)
	}
	lockUntil := time.Now().Add(10 * time.Minute).UTC()
	_ = repo.LockAccount(ctx, user.ID, lockUntil)

	// Search should scan lockout fields without error
	users, total, err := repo.Search(ctx, "lockout_search", 1, 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if total != 1 {
		t.Fatalf("Expected 1 search result, got %d", total)
	}
	if len(users) != 1 {
		t.Fatalf("Expected 1 user, got %d", len(users))
	}
	if users[0].FailedLoginAttempts != 3 {
		t.Errorf("Search result: expected FailedLoginAttempts=3, got %d", users[0].FailedLoginAttempts)
	}
	if users[0].LockedUntil == nil {
		t.Error("Search result: expected LockedUntil to be set")
	}
}

func TestUserRepository_LockoutFields_InGetByNormalizedEmail(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'lockout_norm@example.com'")

	normalizedEmail := "lockoutnorm@example.com"
	user := &models.User{
		Username:        "lockout_norm",
		Email:           "lockout_norm@example.com",
		PasswordHash:    "hash",
		EmailNormalized: &normalizedEmail,
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	// Set lockout state
	_, _ = repo.IncrementFailedLoginAttempts(ctx, user.ID)
	_ = repo.LockAccount(ctx, user.ID, time.Now().Add(5*time.Minute))

	retrieved, err := repo.GetByNormalizedEmail(ctx, normalizedEmail)
	if err != nil {
		t.Fatalf("GetByNormalizedEmail failed: %v", err)
	}
	if retrieved.FailedLoginAttempts != 1 {
		t.Errorf("Expected FailedLoginAttempts=1, got %d", retrieved.FailedLoginAttempts)
	}
	if retrieved.LockedUntil == nil {
		t.Error("Expected LockedUntil to be set")
	}
}
