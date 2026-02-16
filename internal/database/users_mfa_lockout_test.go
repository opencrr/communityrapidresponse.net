package database

import (
	"context"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

func TestUserRepository_IncrementFailedMFAAttempts(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'mfa_lockout_incr@example.com'")

	user := &models.User{
		Username:     "mfa_lockout_incr",
		Email:        "mfa_lockout_incr@example.com",
		PasswordHash: "hash",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	t.Run("increments from zero", func(t *testing.T) {
		count, err := repo.IncrementFailedMFAAttempts(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to increment: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected count 1, got %d", count)
		}
	})

	t.Run("increments again", func(t *testing.T) {
		count, err := repo.IncrementFailedMFAAttempts(ctx, user.ID)
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
		if retrieved.FailedMFAAttempts != 2 {
			t.Errorf("Expected FailedMFAAttempts=2, got %d", retrieved.FailedMFAAttempts)
		}
	})

	t.Run("count visible via GetByEmail", func(t *testing.T) {
		retrieved, err := repo.GetByEmail(ctx, "mfa_lockout_incr@example.com")
		if err != nil {
			t.Fatalf("Failed to get user by email: %v", err)
		}
		if retrieved.FailedMFAAttempts != 2 {
			t.Errorf("Expected FailedMFAAttempts=2 via GetByEmail, got %d", retrieved.FailedMFAAttempts)
		}
	})

	t.Run("count visible via GetByEmailOrUsername", func(t *testing.T) {
		retrieved, err := repo.GetByEmailOrUsername(ctx, "mfa_lockout_incr@example.com")
		if err != nil {
			t.Fatalf("Failed to get user by email/username: %v", err)
		}
		if retrieved.FailedMFAAttempts != 2 {
			t.Errorf("Expected FailedMFAAttempts=2 via GetByEmailOrUsername, got %d", retrieved.FailedMFAAttempts)
		}
	})

	t.Run("count visible via GetByUsername", func(t *testing.T) {
		retrieved, err := repo.GetByUsername(ctx, "mfa_lockout_incr")
		if err != nil {
			t.Fatalf("Failed to get user by username: %v", err)
		}
		if retrieved.FailedMFAAttempts != 2 {
			t.Errorf("Expected FailedMFAAttempts=2 via GetByUsername, got %d", retrieved.FailedMFAAttempts)
		}
	})
}

func TestUserRepository_IncrementFailedMFAAttempts_ToThreshold(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'mfa_lockout_threshold@example.com'")

	user := &models.User{
		Username:     "mfa_lockout_threshold",
		Email:        "mfa_lockout_threshold@example.com",
		PasswordHash: "hash",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	// Increment exactly 5 times (the MFA lockout threshold)
	for i := 1; i <= 5; i++ {
		count, err := repo.IncrementFailedMFAAttempts(ctx, user.ID)
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
	if retrieved.FailedMFAAttempts != 5 {
		t.Errorf("Expected FailedMFAAttempts=5, got %d", retrieved.FailedMFAAttempts)
	}

	// Can increment beyond threshold (counter keeps going)
	count, err := repo.IncrementFailedMFAAttempts(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to increment beyond threshold: %v", err)
	}
	if count != 6 {
		t.Errorf("Expected count 6 beyond threshold, got %d", count)
	}
}

func TestUserRepository_ResetFailedMFAAttempts(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'mfa_lockout_reset@example.com'")

	user := &models.User{
		Username:     "mfa_lockout_reset",
		Email:        "mfa_lockout_reset@example.com",
		PasswordHash: "hash",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	t.Run("clears counter", func(t *testing.T) {
		// Increment a few times
		for i := 0; i < 3; i++ {
			_, _ = repo.IncrementFailedMFAAttempts(ctx, user.ID)
		}

		// Verify pre-reset state
		before, _ := repo.GetByID(ctx, user.ID)
		if before.FailedMFAAttempts != 3 {
			t.Fatalf("Pre-condition failed: expected 3 failed MFA attempts, got %d", before.FailedMFAAttempts)
		}

		// Reset
		if err := repo.ResetFailedMFAAttempts(ctx, user.ID); err != nil {
			t.Fatalf("Failed to reset: %v", err)
		}

		retrieved, err := repo.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if retrieved.FailedMFAAttempts != 0 {
			t.Errorf("Expected FailedMFAAttempts=0, got %d", retrieved.FailedMFAAttempts)
		}
	})

	t.Run("is idempotent on already-reset user", func(t *testing.T) {
		if err := repo.ResetFailedMFAAttempts(ctx, user.ID); err != nil {
			t.Fatalf("Failed to reset already-reset user: %v", err)
		}

		retrieved, err := repo.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if retrieved.FailedMFAAttempts != 0 {
			t.Errorf("Expected FailedMFAAttempts=0, got %d", retrieved.FailedMFAAttempts)
		}
	})

	t.Run("allows re-increment after reset", func(t *testing.T) {
		_ = repo.ResetFailedMFAAttempts(ctx, user.ID)

		count, err := repo.IncrementFailedMFAAttempts(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to increment after reset: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected count 1 after reset+increment, got %d", count)
		}
	})
}

func TestUserRepository_NewUserDefaultsMFAAttempts(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'mfa_lockout_defaults@example.com'")

	user := &models.User{
		Username:     "mfa_lockout_defaults",
		Email:        "mfa_lockout_defaults@example.com",
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

	if retrieved.FailedMFAAttempts != 0 {
		t.Errorf("New user should have FailedMFAAttempts=0, got %d", retrieved.FailedMFAAttempts)
	}
}

func TestUserRepository_MFALockoutFields_InSearch(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'mfa_lockout_search@example.com'")

	user := &models.User{
		Username:     "mfa_lockout_search",
		Email:        "mfa_lockout_search@example.com",
		PasswordHash: "hash",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	// Set some MFA lockout state
	for i := 0; i < 3; i++ {
		_, _ = repo.IncrementFailedMFAAttempts(ctx, user.ID)
	}

	// Search should scan MFA lockout fields without error
	users, total, err := repo.Search(ctx, "mfa_lockout_search", 1, 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if total != 1 {
		t.Fatalf("Expected 1 search result, got %d", total)
	}
	if len(users) != 1 {
		t.Fatalf("Expected 1 user, got %d", len(users))
	}
	if users[0].FailedMFAAttempts != 3 {
		t.Errorf("Search result: expected FailedMFAAttempts=3, got %d", users[0].FailedMFAAttempts)
	}
}

func TestUserRepository_MFALockoutFields_InGetByNormalizedEmail(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'mfa_lockout_norm@example.com'")

	normalizedEmail := "mfalockoutnorm@example.com"
	user := &models.User{
		Username:        "mfa_lockout_norm",
		Email:           "mfa_lockout_norm@example.com",
		PasswordHash:    "hash",
		EmailNormalized: &normalizedEmail,
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	// Set MFA lockout state
	_, _ = repo.IncrementFailedMFAAttempts(ctx, user.ID)

	retrieved, err := repo.GetByNormalizedEmail(ctx, normalizedEmail)
	if err != nil {
		t.Fatalf("GetByNormalizedEmail failed: %v", err)
	}
	if retrieved.FailedMFAAttempts != 1 {
		t.Errorf("Expected FailedMFAAttempts=1, got %d", retrieved.FailedMFAAttempts)
	}
}

func TestUserRepository_MFAAndLoginLockoutIndependent(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE email = 'mfa_login_independent@example.com'")

	user := &models.User{
		Username:     "mfa_login_independent",
		Email:        "mfa_login_independent@example.com",
		PasswordHash: "hash",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	})

	// Increment both counters
	_, _ = repo.IncrementFailedLoginAttempts(ctx, user.ID)
	_, _ = repo.IncrementFailedLoginAttempts(ctx, user.ID)
	_, _ = repo.IncrementFailedMFAAttempts(ctx, user.ID)
	_, _ = repo.IncrementFailedMFAAttempts(ctx, user.ID)
	_, _ = repo.IncrementFailedMFAAttempts(ctx, user.ID)

	retrieved, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}
	if retrieved.FailedLoginAttempts != 2 {
		t.Errorf("Expected FailedLoginAttempts=2, got %d", retrieved.FailedLoginAttempts)
	}
	if retrieved.FailedMFAAttempts != 3 {
		t.Errorf("Expected FailedMFAAttempts=3, got %d", retrieved.FailedMFAAttempts)
	}

	// Reset MFA only
	_ = repo.ResetFailedMFAAttempts(ctx, user.ID)
	retrieved, _ = repo.GetByID(ctx, user.ID)
	if retrieved.FailedLoginAttempts != 2 {
		t.Errorf("ResetFailedMFAAttempts should not affect login counter, got %d", retrieved.FailedLoginAttempts)
	}
	if retrieved.FailedMFAAttempts != 0 {
		t.Errorf("Expected FailedMFAAttempts=0 after reset, got %d", retrieved.FailedMFAAttempts)
	}

	// Reset login only
	_ = repo.ResetFailedLoginAttempts(ctx, user.ID)
	retrieved, _ = repo.GetByID(ctx, user.ID)
	if retrieved.FailedLoginAttempts != 0 {
		t.Errorf("Expected FailedLoginAttempts=0 after reset, got %d", retrieved.FailedLoginAttempts)
	}
	if retrieved.FailedMFAAttempts != 0 {
		t.Errorf("ResetFailedLoginAttempts should not affect MFA counter, got %d", retrieved.FailedMFAAttempts)
	}
}
