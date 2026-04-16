package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func testPasswordResetDB(t *testing.T) *DB {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping database tests")
		return nil
	}

	port := 3306
	if p := os.Getenv("TEST_DB_PORT"); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &port)
	}

	cfg := &config.DatabaseConfig{
		Host:     host,
		Port:     port,
		User:     getEnvOrDefaultPR("TEST_DB_USER", "root"),
		Password: getEnvOrDefaultPR("TEST_DB_PASSWORD", ""),
		Name:     getEnvOrDefaultPR("TEST_DB_NAME", "communityrapidresponse_test"),
		Charset:  "utf8mb4",
	}

	db, err := New(cfg)
	if err != nil {
		t.Skipf("Failed to connect to test database: %v", err)
		return nil
	}

	t.Cleanup(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	return db
}

func getEnvOrDefaultPR(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// createTestUserForReset creates a test user and returns the user ID
func createTestUserForReset(t *testing.T, db *DB, email string) string {
	userID := uuid.New().String()
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("testpassword123"), 12)
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO users (id, username, email, password_hash, verification_tier,
		                   postcard_verified, vouch_verified, is_superuser,
		                   mfa_enabled, mfa_setup_required, email_verified, email_normalized, created_at)
		VALUES (?, ?, ?, ?, 0, FALSE, FALSE, FALSE, FALSE, TRUE, FALSE, ?, NOW())
	`, userID, "resetuser_"+userID[:8], email, string(hashedPassword), email)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", userID)
	})
	return userID
}

func hashToken(rawToken string) string {
	hash := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(hash[:])
}

func TestPasswordResetRepository_Create(t *testing.T) {
	db := testPasswordResetDB(t)
	repo := NewPasswordResetRepository(db)
	userID := createTestUserForReset(t, db, "create@resettest.com")

	t.Run("creates token successfully", func(t *testing.T) {
		tokenHash := hashToken("test_token_create")
		expiresAt := time.Now().UTC().Add(1 * time.Hour)

		token, err := repo.Create(context.Background(), userID, tokenHash, expiresAt)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
		if token.ID == "" {
			t.Error("Expected token ID to be set")
		}
		if token.UserID != userID {
			t.Errorf("Expected user ID %s, got %s", userID, token.UserID)
		}

		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), "DELETE FROM password_reset_tokens WHERE id = ?", token.ID)
		})
	})
}

func TestPasswordResetRepository_GetByTokenHash(t *testing.T) {
	db := testPasswordResetDB(t)
	repo := NewPasswordResetRepository(db)
	userID := createTestUserForReset(t, db, "getbyhash@resettest.com")

	t.Run("retrieves valid token", func(t *testing.T) {
		tokenHash := hashToken("test_token_get")
		expiresAt := time.Now().UTC().Add(1 * time.Hour)

		created, err := repo.Create(context.Background(), userID, tokenHash, expiresAt)
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), "DELETE FROM password_reset_tokens WHERE id = ?", created.ID)
		})

		found, err := repo.GetByTokenHash(context.Background(), tokenHash)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
		if found.ID != created.ID {
			t.Errorf("Expected token ID %s, got %s", created.ID, found.ID)
		}
	})

	t.Run("rejects non-existent token", func(t *testing.T) {
		_, err := repo.GetByTokenHash(context.Background(), hashToken("nonexistent"))
		if err != ErrResetTokenNotFound {
			t.Errorf("Expected ErrResetTokenNotFound, got: %v", err)
		}
	})

	t.Run("rejects expired token", func(t *testing.T) {
		tokenHash := hashToken("test_token_expired")
		expiresAt := time.Now().UTC().Add(-1 * time.Hour) // Already expired

		created, err := repo.Create(context.Background(), userID, tokenHash, expiresAt)
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), "DELETE FROM password_reset_tokens WHERE id = ?", created.ID)
		})

		_, err = repo.GetByTokenHash(context.Background(), tokenHash)
		if err != ErrResetTokenExpired {
			t.Errorf("Expected ErrResetTokenExpired, got: %v", err)
		}
	})

	t.Run("rejects used token", func(t *testing.T) {
		tokenHash := hashToken("test_token_used")
		expiresAt := time.Now().UTC().Add(1 * time.Hour)

		created, err := repo.Create(context.Background(), userID, tokenHash, expiresAt)
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), "DELETE FROM password_reset_tokens WHERE id = ?", created.ID)
		})

		// Mark as used
		err = repo.MarkUsed(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("Failed to mark token as used: %v", err)
		}

		_, err = repo.GetByTokenHash(context.Background(), tokenHash)
		if err != ErrResetTokenUsed {
			t.Errorf("Expected ErrResetTokenUsed, got: %v", err)
		}
	})
}

func TestPasswordResetRepository_InvalidateForUser(t *testing.T) {
	db := testPasswordResetDB(t)
	repo := NewPasswordResetRepository(db)
	userID := createTestUserForReset(t, db, "invalidate@resettest.com")

	// Create multiple tokens
	token1Hash := hashToken("invalidate_token_1")
	token2Hash := hashToken("invalidate_token_2")
	expiresAt := time.Now().UTC().Add(1 * time.Hour)

	created1, _ := repo.Create(context.Background(), userID, token1Hash, expiresAt)
	created2, _ := repo.Create(context.Background(), userID, token2Hash, expiresAt)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM password_reset_tokens WHERE id IN (?, ?)", created1.ID, created2.ID)
	})

	// Invalidate all tokens
	err := repo.InvalidateForUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Both tokens should now be rejected
	_, err = repo.GetByTokenHash(context.Background(), token1Hash)
	if err != ErrResetTokenUsed {
		t.Errorf("Expected ErrResetTokenUsed for token1, got: %v", err)
	}

	_, err = repo.GetByTokenHash(context.Background(), token2Hash)
	if err != ErrResetTokenUsed {
		t.Errorf("Expected ErrResetTokenUsed for token2, got: %v", err)
	}
}

func TestPasswordResetRepository_CountActiveForUser(t *testing.T) {
	db := testPasswordResetDB(t)
	repo := NewPasswordResetRepository(db)
	userID := createTestUserForReset(t, db, "count@resettest.com")

	// Start with 0
	count, err := repo.CountActiveForUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected count 0, got %d", count)
	}

	// Create active token
	tokenHash := hashToken("count_token_1")
	created, _ := repo.Create(context.Background(), userID, tokenHash, time.Now().UTC().Add(1*time.Hour))
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM password_reset_tokens WHERE id = ?", created.ID)
	})

	count, err = repo.CountActiveForUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// Create expired token - should not count
	expiredHash := hashToken("count_token_expired")
	expiredToken, _ := repo.Create(context.Background(), userID, expiredHash, time.Now().UTC().Add(-1*time.Hour))
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM password_reset_tokens WHERE id = ?", expiredToken.ID)
	})

	count, err = repo.CountActiveForUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1 (expired shouldn't count), got %d", count)
	}
}

func TestPasswordResetRepository_CleanupExpired(t *testing.T) {
	db := testPasswordResetDB(t)
	repo := NewPasswordResetRepository(db)
	userID := createTestUserForReset(t, db, "cleanup@resettest.com")

	// Create an old expired token (expired more than 24h ago)
	tokenHash := hashToken("cleanup_token")
	oldExpiry := time.Now().UTC().Add(-48 * time.Hour) // Expired 48h ago
	created, err := repo.Create(context.Background(), userID, tokenHash, oldExpiry)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	// Run cleanup
	deleted, err := repo.CleanupExpired(context.Background())
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if deleted < 1 {
		t.Errorf("Expected at least 1 deleted, got %d", deleted)
	}

	// Token should be gone
	var count int
	_ = db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM password_reset_tokens WHERE id = ?", created.ID).Scan(&count)
	if count != 0 {
		t.Error("Expected token to be deleted by cleanup")
	}
}
