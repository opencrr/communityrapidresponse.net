package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// --- Test helpers for encryption repo tests ---

// encCreateUser creates a test user and returns it. Caller must cleanup.
func encCreateUser(t *testing.T, db *DB, suffix string) *models.User {
	t.Helper()
	repo := NewUserRepository(db)
	ctx := context.Background()

	user := &models.User{
		Username:         "enctest_" + suffix,
		Email:            "enctest_" + suffix + "@example.com",
		PasswordHash:     "hash",
		VerificationTier: models.TierPostcard,
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	return user
}

// encCreateRegion creates a test region and returns it. Caller must cleanup.
func encCreateRegion(t *testing.T, db *DB, createdBy string, name string) *models.GeographicRegion {
	t.Helper()
	repo := NewRegionRepository(db)
	ctx := context.Background()

	region := &models.GeographicRegion{
		Name:       name,
		RegionType: models.RegionTypeCity,
		CreatedBy:  strPtr(createdBy),
	}
	geoJSON := `{"type":"Polygon","coordinates":[[[-122.5,37.7],[-122.4,37.7],[-122.4,37.8],[-122.5,37.8],[-122.5,37.7]]]}`
	if err := repo.Create(ctx, region, geoJSON); err != nil {
		t.Fatalf("Failed to create test region: %v", err)
	}
	return region
}

// encCreateSignalGroup creates a signal group scoped to a region. Caller must cleanup.
func encCreateSignalGroup(t *testing.T, db *DB, regionID, createdBy string) *models.SignalGroup {
	t.Helper()
	repo := NewSignalGroupRepository(db)
	ctx := context.Background()

	group := &models.SignalGroup{
		RegionID:  strPtr(regionID),
		GroupName: "Test Signal Group " + uuid.New().String()[:8],
		CreatedBy: strPtr(createdBy),
	}
	if err := repo.Create(ctx, group); err != nil {
		t.Fatalf("Failed to create test signal group: %v", err)
	}
	return group
}

// encCreateSecret creates an encrypted secret for a signal group with wrapped keys.
func encCreateSecret(t *testing.T, db *DB, groupID, userID string) *models.EncryptedSecret {
	t.Helper()
	repo := NewEncryptedSecretRepository(db)
	ctx := context.Background()

	secret := &models.EncryptedSecret{
		SecretType:       models.SecretTypeSignalInvite,
		SignalGroupID:    strPtr(groupID),
		EncryptedPayload: "encrypted_payload_test",
		EncryptionIV:     "test_iv_12345678",
		UpdatedBy:        userID,
	}
	wrappedKeys := []models.WrappedKeyEntry{
		{UserID: userID, WrappedDEK: "wrapped_dek_for_" + userID},
	}
	if err := repo.Create(ctx, secret, wrappedKeys); err != nil {
		t.Fatalf("Failed to create test encrypted secret: %v", err)
	}
	return secret
}

// encCreateGroup creates a test group and returns it. Caller must cleanup.
func encCreateGroup(t *testing.T, db *DB, createdBy string) *models.Group {
	t.Helper()
	repo := NewGroupRepository(db)
	ctx := context.Background()

	req := &models.CreateGroupRequest{
		Name: "Test Group " + uuid.New().String()[:8],
	}
	group, err := repo.Create(ctx, req, createdBy)
	if err != nil {
		t.Fatalf("Failed to create test group: %v", err)
	}
	return group
}

// encAddGroupMember adds a user to a group and returns the membership ID. Caller must cleanup.
func encAddGroupMember(t *testing.T, db *DB, groupID, userID string, isAdmin bool) string {
	t.Helper()
	ctx := context.Background()
	memberID := uuid.New().String()
	now := time.Now().UTC()

	query := `
		INSERT INTO group_members (id, group_id, user_id, is_admin, joined_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query, memberID, groupID, userID, isAdmin, now)
	if err != nil {
		t.Fatalf("Failed to add group member: %v", err)
	}
	return memberID
}

// --- EncryptionKeyRepository tests ---

func TestEncryptionKeyRepository_CreateAndGetByUserID(t *testing.T) {
	db := testDB(t)
	repo := NewEncryptionKeyRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "ek_create")
	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM user_encryption_keys WHERE user_id = ?", user.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	t.Run("creates and retrieves encryption key", func(t *testing.T) {
		key := &models.UserEncryptionKey{
			UserID:            user.ID,
			PublicKey:         "test_public_key",
			WrappedPrivateKey: "test_wrapped_private_key",
			KeySalt:           "test_salt_123456789012",
			KeyIV:             "test_iv_12345678",
		}

		if err := repo.Create(ctx, key); err != nil {
			t.Fatalf("Failed to create encryption key: %v", err)
		}
		if key.CreatedAt.IsZero() {
			t.Error("Expected CreatedAt to be set")
		}
		if key.RotatedAt.IsZero() {
			t.Error("Expected RotatedAt to be set")
		}

		retrieved, err := repo.GetByUserID(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to get encryption key: %v", err)
		}
		if retrieved.PublicKey != "test_public_key" {
			t.Errorf("Expected public key 'test_public_key', got '%s'", retrieved.PublicKey)
		}
		if retrieved.WrappedPrivateKey != "test_wrapped_private_key" {
			t.Errorf("Expected wrapped private key 'test_wrapped_private_key', got '%s'", retrieved.WrappedPrivateKey)
		}
		if retrieved.KeySalt != "test_salt_123456789012" {
			t.Errorf("Expected key salt 'test_salt_123456789012', got '%s'", retrieved.KeySalt)
		}
	})

	t.Run("upserts on duplicate user_id", func(t *testing.T) {
		key := &models.UserEncryptionKey{
			UserID:            user.ID,
			PublicKey:         "updated_public_key",
			WrappedPrivateKey: "updated_wrapped_key",
			KeySalt:           "updated_salt_12345678",
			KeyIV:             "updated_iv_1234",
		}

		if err := repo.Create(ctx, key); err != nil {
			t.Fatalf("Failed to upsert encryption key: %v", err)
		}

		retrieved, err := repo.GetByUserID(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to get upserted key: %v", err)
		}
		if retrieved.PublicKey != "updated_public_key" {
			t.Errorf("Expected upserted public key 'updated_public_key', got '%s'", retrieved.PublicKey)
		}
	})
}

func TestEncryptionKeyRepository_GetByUserID_NotFound(t *testing.T) {
	db := testDB(t)
	repo := NewEncryptionKeyRepository(db)
	ctx := context.Background()

	_, err := repo.GetByUserID(ctx, "nonexistent-user-id")
	if err != ErrEncryptionKeyNotFound {
		t.Errorf("Expected ErrEncryptionKeyNotFound, got %v", err)
	}
}

func TestEncryptionKeyRepository_GetPublicKeysByUserIDs(t *testing.T) {
	db := testDB(t)
	repo := NewEncryptionKeyRepository(db)
	ctx := context.Background()

	user1 := encCreateUser(t, db, "ek_pubkeys1")
	user2 := encCreateUser(t, db, "ek_pubkeys2")
	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM user_encryption_keys WHERE user_id IN (?, ?)", user1.ID, user2.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?)", user1.ID, user2.ID)
	}()

	_ = repo.Create(ctx, &models.UserEncryptionKey{
		UserID: user1.ID, PublicKey: "pub1", WrappedPrivateKey: "priv1", KeySalt: "salt1_12345678901234", KeyIV: "iv1_123456789012",
	})
	_ = repo.Create(ctx, &models.UserEncryptionKey{
		UserID: user2.ID, PublicKey: "pub2", WrappedPrivateKey: "priv2", KeySalt: "salt2_12345678901234", KeyIV: "iv2_123456789012",
	})

	t.Run("returns keys for multiple users", func(t *testing.T) {
		keys, err := repo.GetPublicKeysByUserIDs(ctx, []string{user1.ID, user2.ID})
		if err != nil {
			t.Fatalf("Failed to get public keys: %v", err)
		}
		if len(keys) != 2 {
			t.Fatalf("Expected 2 keys, got %d", len(keys))
		}
	})

	t.Run("returns empty slice for empty input", func(t *testing.T) {
		keys, err := repo.GetPublicKeysByUserIDs(ctx, []string{})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(keys) != 0 {
			t.Errorf("Expected 0 keys, got %d", len(keys))
		}
	})

	t.Run("returns empty slice for nonexistent users", func(t *testing.T) {
		keys, err := repo.GetPublicKeysByUserIDs(ctx, []string{"nonexistent1", "nonexistent2"})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(keys) != 0 {
			t.Errorf("Expected 0 keys, got %d", len(keys))
		}
	})
}

func TestEncryptionKeyRepository_Update(t *testing.T) {
	db := testDB(t)
	repo := NewEncryptionKeyRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "ek_update")
	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM user_encryption_keys WHERE user_id = ?", user.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	_ = repo.Create(ctx, &models.UserEncryptionKey{
		UserID: user.ID, PublicKey: "orig_pub", WrappedPrivateKey: "orig_priv",
		KeySalt: "orig_salt_1234567890", KeyIV: "orig_iv_12345678",
	})

	t.Run("updates wrapped private key", func(t *testing.T) {
		err := repo.Update(ctx, user.ID, "new_wrapped_key", "new_salt_12345678901", "new_iv_123456789")
		if err != nil {
			t.Fatalf("Failed to update: %v", err)
		}

		retrieved, err := repo.GetByUserID(ctx, user.ID)
		if err != nil {
			t.Fatalf("Failed to get updated key: %v", err)
		}
		if retrieved.WrappedPrivateKey != "new_wrapped_key" {
			t.Errorf("Expected 'new_wrapped_key', got '%s'", retrieved.WrappedPrivateKey)
		}
		// Public key should be unchanged
		if retrieved.PublicKey != "orig_pub" {
			t.Errorf("Public key should not change, got '%s'", retrieved.PublicKey)
		}
	})

	t.Run("returns error for nonexistent user", func(t *testing.T) {
		err := repo.Update(ctx, "nonexistent", "key", "salt", "iv")
		if err != ErrEncryptionKeyNotFound {
			t.Errorf("Expected ErrEncryptionKeyNotFound, got %v", err)
		}
	})
}

func TestEncryptionKeyRepository_GetPublicKeysForRegion(t *testing.T) {
	db := testDB(t)
	ekRepo := NewEncryptionKeyRepository(db)
	regionRepo := NewRegionRepository(db)
	ctx := context.Background()

	user1 := encCreateUser(t, db, "ek_region1")
	user2 := encCreateUser(t, db, "ek_region2")
	region := encCreateRegion(t, db, user1.ID, "Enc Key Region Test")

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM user_encryption_keys WHERE user_id IN (?, ?)", user1.ID, user2.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?)", user1.ID, user2.ID)
	}()

	// Add both users to region
	_ = regionRepo.AddUserToRegion(ctx, user1.ID, region.ID, true)
	_ = regionRepo.AddUserToRegion(ctx, user2.ID, region.ID, false)

	// Mark users as vouch-verified (required by GetPublicKeysForRegion filter)
	_, _ = db.ExecContext(ctx, "UPDATE users SET vouch_verified = TRUE WHERE id IN (?, ?)", user1.ID, user2.ID)

	// Create encryption keys for both
	_ = ekRepo.Create(ctx, &models.UserEncryptionKey{
		UserID: user1.ID, PublicKey: "pub1_region", WrappedPrivateKey: "priv1",
		KeySalt: "salt1_12345678901234", KeyIV: "iv1_123456789012",
	})
	_ = ekRepo.Create(ctx, &models.UserEncryptionKey{
		UserID: user2.ID, PublicKey: "pub2_region", WrappedPrivateKey: "priv2",
		KeySalt: "salt2_12345678901234", KeyIV: "iv2_123456789012",
	})

	keys, err := ekRepo.GetPublicKeysForRegion(ctx, region.ID)
	if err != nil {
		t.Fatalf("Failed to get keys for region: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(keys))
	}
}

// TestEncryptionKeyRepository_GetPublicKeysForRegion_ExcludesSuperuser verifies
// superusers are never included in wrapped-DEK recipient lists (issue #8).
func TestEncryptionKeyRepository_GetPublicKeysForRegion_ExcludesSuperuser(t *testing.T) {
	db := testDB(t)
	ekRepo := NewEncryptionKeyRepository(db)
	regionRepo := NewRegionRepository(db)
	ctx := context.Background()

	regular := encCreateUser(t, db, "ek_su_regular")
	super := encCreateUser(t, db, "ek_su_super")
	region := encCreateRegion(t, db, regular.ID, "Enc Key Superuser Region")

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM user_encryption_keys WHERE user_id IN (?, ?)", regular.ID, super.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?)", regular.ID, super.ID)
	}()

	_ = regionRepo.AddUserToRegion(ctx, regular.ID, region.ID, false)
	_ = regionRepo.AddUserToRegion(ctx, super.ID, region.ID, true)
	// Both vouch-verified; mark one as superuser.
	_, _ = db.ExecContext(ctx, "UPDATE users SET vouch_verified = TRUE WHERE id IN (?, ?)", regular.ID, super.ID)
	_, _ = db.ExecContext(ctx, "UPDATE users SET is_superuser = TRUE WHERE id = ?", super.ID)

	_ = ekRepo.Create(ctx, &models.UserEncryptionKey{
		UserID: regular.ID, PublicKey: "pub_regular", WrappedPrivateKey: "priv",
		KeySalt: "salt_123456789012345", KeyIV: "iv_1234567890123",
	})
	_ = ekRepo.Create(ctx, &models.UserEncryptionKey{
		UserID: super.ID, PublicKey: "pub_super", WrappedPrivateKey: "priv",
		KeySalt: "salt_123456789012346", KeyIV: "iv_1234567890124",
	})

	keys, err := ekRepo.GetPublicKeysForRegion(ctx, region.ID)
	if err != nil {
		t.Fatalf("GetPublicKeysForRegion failed: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key (superuser excluded), got %d", len(keys))
	}
	if keys[0].UserID == super.ID {
		t.Error("superuser must be excluded from wrapped-DEK recipient list")
	}
}

func TestEncryptionKeyRepository_GetPublicKeysForGroup(t *testing.T) {
	db := testDB(t)
	ekRepo := NewEncryptionKeyRepository(db)
	ctx := context.Background()

	creator := encCreateUser(t, db, "group_creator")
	member1 := encCreateUser(t, db, "group_member1")
	member2 := encCreateUser(t, db, "group_member2")
	group := encCreateGroup(t, db, creator.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM user_encryption_keys WHERE user_id IN (?, ?, ?)", creator.ID, member1.ID, member2.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM group_members WHERE group_id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM `groups` WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?, ?)", creator.ID, member1.ID, member2.ID)
	}()

	// Add members to group
	encAddGroupMember(t, db, group.ID, creator.ID, true)
	encAddGroupMember(t, db, group.ID, member1.ID, false)
	encAddGroupMember(t, db, group.ID, member2.ID, false)

	// Create encryption keys for all members
	_ = ekRepo.Create(ctx, &models.UserEncryptionKey{
		UserID: creator.ID, PublicKey: "pub_creator", WrappedPrivateKey: "priv_creator",
		KeySalt: "salt_creator_12345", KeyIV: "iv_creator_12345",
	})
	_ = ekRepo.Create(ctx, &models.UserEncryptionKey{
		UserID: member1.ID, PublicKey: "pub_member1", WrappedPrivateKey: "priv_member1",
		KeySalt: "salt_member1_12345", KeyIV: "iv_member1_12345",
	})
	_ = ekRepo.Create(ctx, &models.UserEncryptionKey{
		UserID: member2.ID, PublicKey: "pub_member2", WrappedPrivateKey: "priv_member2",
		KeySalt: "salt_member2_12345", KeyIV: "iv_member2_12345",
	})

	t.Run("returns keys for all members at tier=member", func(t *testing.T) {
		keys, err := ekRepo.GetPublicKeysForGroup(ctx, group.ID, models.AccessTierMember)
		if err != nil {
			t.Fatalf("GetPublicKeysForGroup failed: %v", err)
		}
		if len(keys) != 3 {
			t.Errorf("expected 3 keys, got %d", len(keys))
		}
		keysByID := make(map[string]string)
		for _, k := range keys {
			keysByID[k.UserID] = k.PublicKey
		}
		if keysByID[creator.ID] != "pub_creator" {
			t.Error("creator key not found")
		}
		if keysByID[member1.ID] != "pub_member1" {
			t.Error("member1 key not found")
		}
		if keysByID[member2.ID] != "pub_member2" {
			t.Error("member2 key not found")
		}
	})

	t.Run("excludes superusers", func(t *testing.T) {
		// Mark member2 as superuser
		_, _ = db.ExecContext(ctx, "UPDATE users SET is_superuser = TRUE WHERE id = ?", member2.ID)

		keys, err := ekRepo.GetPublicKeysForGroup(ctx, group.ID, models.AccessTierMember)
		if err != nil {
			t.Fatalf("GetPublicKeysForGroup failed: %v", err)
		}
		if len(keys) != 2 {
			t.Errorf("expected 2 keys (superuser excluded), got %d", len(keys))
		}
		for _, k := range keys {
			if k.UserID == member2.ID {
				t.Error("superuser must be excluded from wrapped-DEK recipient list")
			}
		}
	})
}

func TestEncryptionKeyRepository_GetPublicKeysForGroup_TrustedTier(t *testing.T) {
	db := testDB(t)
	ekRepo := NewEncryptionKeyRepository(db)
	ctx := context.Background()

	creator := encCreateUser(t, db, "trusted_creator")
	trusted := encCreateUser(t, db, "trusted_member")
	regular := encCreateUser(t, db, "regular_member")
	group := encCreateGroup(t, db, creator.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM user_encryption_keys WHERE user_id IN (?, ?, ?)", creator.ID, trusted.ID, regular.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM group_members WHERE group_id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM `groups` WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?, ?)", creator.ID, trusted.ID, regular.ID)
	}()

	// Add members with different trust levels
	encAddGroupMember(t, db, group.ID, creator.ID, true)  // admin
	_, _ = db.ExecContext(ctx, "INSERT INTO group_members (id, group_id, user_id, is_admin, trust_level, joined_at) VALUES (?, ?, ?, FALSE, 'trusted', ?)",
		uuid.New().String(), group.ID, trusted.ID, time.Now().UTC())
	encAddGroupMember(t, db, group.ID, regular.ID, false) // regular member

	// Create encryption keys
	_ = ekRepo.Create(ctx, &models.UserEncryptionKey{
		UserID: creator.ID, PublicKey: "pub_creator", WrappedPrivateKey: "priv",
		KeySalt: "salt_c", KeyIV: "iv_c",
	})
	_ = ekRepo.Create(ctx, &models.UserEncryptionKey{
		UserID: trusted.ID, PublicKey: "pub_trusted", WrappedPrivateKey: "priv",
		KeySalt: "salt_t", KeyIV: "iv_t",
	})
	_ = ekRepo.Create(ctx, &models.UserEncryptionKey{
		UserID: regular.ID, PublicKey: "pub_regular", WrappedPrivateKey: "priv",
		KeySalt: "salt_r", KeyIV: "iv_r",
	})

	t.Run("trusted tier includes admins and trusted members", func(t *testing.T) {
		keys, err := ekRepo.GetPublicKeysForGroup(ctx, group.ID, models.AccessTierTrusted)
		if err != nil {
			t.Fatalf("GetPublicKeysForGroup failed: %v", err)
		}
		if len(keys) != 2 {
			t.Errorf("expected 2 keys (creator + trusted), got %d", len(keys))
		}
		keysByID := make(map[string]string)
		for _, k := range keys {
			keysByID[k.UserID] = k.PublicKey
		}
		if keysByID[creator.ID] != "pub_creator" {
			t.Error("creator (admin) key not found")
		}
		if keysByID[trusted.ID] != "pub_trusted" {
			t.Error("trusted member key not found")
		}
		if _, exists := keysByID[regular.ID]; exists {
			t.Error("regular member should not be included at trusted tier")
		}
	})
}

// --- EncryptedSecretRepository tests ---

func TestEncryptedSecretRepository_CreateAndGetBySignalGroupID(t *testing.T) {
	db := testDB(t)
	secretRepo := NewEncryptedSecretRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "es_create")
	region := encCreateRegion(t, db, user.ID, "Encrypted Secret Region")
	group := encCreateSignalGroup(t, db, region.ID, user.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id IN (SELECT id FROM encrypted_secrets WHERE signal_group_id = ?)", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE signal_group_id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	t.Run("creates secret and retrieves by signal group ID", func(t *testing.T) {
		secret := &models.EncryptedSecret{
			SecretType:       models.SecretTypeSignalInvite,
			SignalGroupID:    strPtr(group.ID),
			EncryptedPayload: "payload_test",
			EncryptionIV:     "iv_test_12345678",
			UpdatedBy:        user.ID,
		}
		wrappedKeys := []models.WrappedKeyEntry{
			{UserID: user.ID, WrappedDEK: "wrapped_dek_1"},
		}

		err := secretRepo.Create(ctx, secret, wrappedKeys)
		if err != nil {
			t.Fatalf("Failed to create secret: %v", err)
		}
		if secret.ID == "" {
			t.Error("Expected secret ID to be set")
		}

		retrieved, err := secretRepo.GetBySignalGroupID(ctx, group.ID)
		if err != nil {
			t.Fatalf("Failed to get secret: %v", err)
		}
		if retrieved.EncryptedPayload != "payload_test" {
			t.Errorf("Expected payload 'payload_test', got '%s'", retrieved.EncryptedPayload)
		}
		if string(retrieved.SecretType) != string(models.SecretTypeSignalInvite) {
			t.Errorf("Expected secret type '%s', got '%s'", models.SecretTypeSignalInvite, retrieved.SecretType)
		}
	})
}

func TestEncryptedSecretRepository_GetBySignalGroupID_NotFound(t *testing.T) {
	db := testDB(t)
	repo := NewEncryptedSecretRepository(db)
	ctx := context.Background()

	_, err := repo.GetBySignalGroupID(ctx, "nonexistent-group")
	if err != ErrEncryptedSecretNotFound {
		t.Errorf("Expected ErrEncryptedSecretNotFound, got %v", err)
	}
}

func TestEncryptedSecretRepository_GetWrappedDEK(t *testing.T) {
	db := testDB(t)
	secretRepo := NewEncryptedSecretRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "es_dek")
	region := encCreateRegion(t, db, user.ID, "DEK Test Region")
	group := encCreateSignalGroup(t, db, region.ID, user.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id IN (SELECT id FROM encrypted_secrets WHERE signal_group_id = ?)", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE signal_group_id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	secret := encCreateSecret(t, db, group.ID, user.ID)

	t.Run("retrieves wrapped DEK for existing user", func(t *testing.T) {
		dek, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user.ID)
		if err != nil {
			t.Fatalf("Failed to get wrapped DEK: %v", err)
		}
		expectedDEK := "wrapped_dek_for_" + user.ID
		if dek != expectedDEK {
			t.Errorf("Expected DEK '%s', got '%s'", expectedDEK, dek)
		}
	})

	t.Run("returns error for nonexistent user", func(t *testing.T) {
		_, err := secretRepo.GetWrappedDEK(ctx, secret.ID, "nonexistent")
		if err != ErrEncryptedSecretNotFound {
			t.Errorf("Expected ErrEncryptedSecretNotFound, got %v", err)
		}
	})
}

func TestEncryptedSecretRepository_UpdatePayloadAndKeys(t *testing.T) {
	db := testDB(t)
	secretRepo := NewEncryptedSecretRepository(db)
	ctx := context.Background()

	user1 := encCreateUser(t, db, "es_update1")
	user2 := encCreateUser(t, db, "es_update2")
	region := encCreateRegion(t, db, user1.ID, "Update Payload Region")
	group := encCreateSignalGroup(t, db, region.ID, user1.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id IN (SELECT id FROM encrypted_secrets WHERE signal_group_id = ?)", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE signal_group_id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?)", user1.ID, user2.ID)
	}()

	secret := encCreateSecret(t, db, group.ID, user1.ID)

	t.Run("updates payload and replaces keys", func(t *testing.T) {
		newKeys := []models.WrappedKeyEntry{
			{UserID: user1.ID, WrappedDEK: "new_dek_user1"},
			{UserID: user2.ID, WrappedDEK: "new_dek_user2"},
		}

		err := secretRepo.UpdatePayloadAndKeys(ctx, secret.ID, "new_payload", "new_iv_12345678", user2.ID, newKeys)
		if err != nil {
			t.Fatalf("Failed to update: %v", err)
		}

		retrieved, err := secretRepo.GetBySignalGroupID(ctx, group.ID)
		if err != nil {
			t.Fatalf("Failed to get updated secret: %v", err)
		}
		if retrieved.EncryptedPayload != "new_payload" {
			t.Errorf("Expected 'new_payload', got '%s'", retrieved.EncryptedPayload)
		}
		if retrieved.UpdatedBy != user2.ID {
			t.Errorf("Expected updated_by '%s', got '%s'", user2.ID, retrieved.UpdatedBy)
		}

		// Verify new keys
		dek1, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user1.ID)
		if err != nil {
			t.Fatalf("Failed to get user1 DEK: %v", err)
		}
		if dek1 != "new_dek_user1" {
			t.Errorf("Expected 'new_dek_user1', got '%s'", dek1)
		}

		dek2, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user2.ID)
		if err != nil {
			t.Fatalf("Failed to get user2 DEK: %v", err)
		}
		if dek2 != "new_dek_user2" {
			t.Errorf("Expected 'new_dek_user2', got '%s'", dek2)
		}
	})
}

func TestEncryptedSecretRepository_FlagRekeyAndGetPendingRekeys(t *testing.T) {
	db := testDB(t)
	secretRepo := NewEncryptedSecretRepository(db)
	ekRepo := NewEncryptionKeyRepository(db)
	ctx := context.Background()

	user1 := encCreateUser(t, db, "es_rekey1")
	user2 := encCreateUser(t, db, "es_rekey2")
	region := encCreateRegion(t, db, user1.ID, "Rekey Region")
	group := encCreateSignalGroup(t, db, region.ID, user1.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM user_encryption_keys WHERE user_id IN (?, ?)", user1.ID, user2.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id IN (SELECT id FROM encrypted_secrets WHERE signal_group_id = ?)", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE signal_group_id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?)", user1.ID, user2.ID)
	}()

	// Create encryption keys for user2 (needed for GetPendingRekeys target_public_key JOIN)
	_ = ekRepo.Create(ctx, &models.UserEncryptionKey{
		UserID: user2.ID, PublicKey: "user2_pub_key", WrappedPrivateKey: "priv2",
		KeySalt: "salt2_12345678901234", KeyIV: "iv2_123456789012",
	})

	// Create secret with keys for both users
	secret := &models.EncryptedSecret{
		SecretType:       models.SecretTypeSignalInvite,
		SignalGroupID:    strPtr(group.ID),
		EncryptedPayload: "rekey_payload",
		EncryptionIV:     "rekey_iv_1234567",
		UpdatedBy:        user1.ID,
	}
	wrappedKeys := []models.WrappedKeyEntry{
		{UserID: user1.ID, WrappedDEK: "dek_user1"},
		{UserID: user2.ID, WrappedDEK: "dek_user2"},
	}
	if err := secretRepo.Create(ctx, secret, wrappedKeys); err != nil {
		t.Fatalf("Failed to create secret: %v", err)
	}

	t.Run("flags rekey and retrieves pending rekeys", func(t *testing.T) {
		// Flag user2 as needing rekey
		err := secretRepo.FlagRekeyForUser(ctx, user2.ID)
		if err != nil {
			t.Fatalf("Failed to flag rekey: %v", err)
		}

		// user1 should see pending rekeys for user2
		rekeys, err := secretRepo.GetPendingRekeys(ctx, user1.ID)
		if err != nil {
			t.Fatalf("Failed to get pending rekeys: %v", err)
		}
		if len(rekeys) != 1 {
			t.Fatalf("Expected 1 pending rekey, got %d", len(rekeys))
		}
		if rekeys[0].TargetUserID != user2.ID {
			t.Errorf("Expected target user '%s', got '%s'", user2.ID, rekeys[0].TargetUserID)
		}
		if rekeys[0].TargetPublicKey != "user2_pub_key" {
			t.Errorf("Expected target public key 'user2_pub_key', got '%s'", rekeys[0].TargetPublicKey)
		}
		if rekeys[0].CallerWrappedDEK != "dek_user1" {
			t.Errorf("Expected caller DEK 'dek_user1', got '%s'", rekeys[0].CallerWrappedDEK)
		}
	})
}

func TestEncryptedSecretRepository_GetPendingRekeys_GroupAndConnectionOwned(t *testing.T) {
	db := testDB(t)
	secretRepo := NewEncryptedSecretRepository(db)
	groupRepo := NewSignalGroupRepository(db)
	ekRepo := NewEncryptionKeyRepository(db)
	ctx := context.Background()

	user1 := encCreateUser(t, db, "es_rekey_multi1")
	user2 := encCreateUser(t, db, "es_rekey_multi2")
	region := encCreateRegion(t, db, user1.ID, "Rekey Multi Region")
	group := encCreateSignalGroup(t, db, region.ID, user1.ID)

	// Create a connection for the connection-owned signal group
	connID := uuid.New().String()
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, "INSERT INTO connections (id, name, created_at) VALUES (?, NULL, ?)", connID, now)
	if err != nil {
		t.Fatalf("Failed to create connection: %v", err)
	}

	// Create a signal group owned by the connection
	connGroup := &models.SignalGroup{
		ConnectionID: strPtr(connID),
		GroupName:    "Test Connection Signal Group",
		CreatedBy:    strPtr(user1.ID),
	}
	if err := groupRepo.Create(ctx, connGroup); err != nil {
		t.Fatalf("Failed to create connection-owned signal group: %v", err)
	}

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM user_encryption_keys WHERE user_id IN (?, ?)", user1.ID, user2.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id IN (SELECT id FROM encrypted_secrets WHERE signal_group_id = ?)", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE signal_group_id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id IN (SELECT id FROM encrypted_secrets WHERE signal_group_id = ?)", connGroup.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE signal_group_id = ?", connGroup.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id IN (?, ?)", group.ID, connGroup.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM connections WHERE id = ?", connID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?)", user1.ID, user2.ID)
	}()

	// Create encryption keys for user2 (needed for GetPendingRekeys target_public_key JOIN)
	_ = ekRepo.Create(ctx, &models.UserEncryptionKey{
		UserID: user2.ID, PublicKey: "user2_pub_key_multi", WrappedPrivateKey: "priv2",
		KeySalt: "salt2_12345678901234", KeyIV: "iv2_123456789012",
	})

	// Create group-owned secret with keys for both users
	groupSecret := &models.EncryptedSecret{
		SecretType:       models.SecretTypeSignalInvite,
		SignalGroupID:    strPtr(group.ID),
		EncryptedPayload: "group_payload",
		EncryptionIV:     "group_iv_1234567",
		UpdatedBy:        user1.ID,
	}
	groupWrappedKeys := []models.WrappedKeyEntry{
		{UserID: user1.ID, WrappedDEK: "group_dek_user1"},
		{UserID: user2.ID, WrappedDEK: "group_dek_user2"},
	}
	if err := secretRepo.Create(ctx, groupSecret, groupWrappedKeys); err != nil {
		t.Fatalf("Failed to create group secret: %v", err)
	}

	// Create connection-owned signal group secret with keys for both users
	connSecret := &models.EncryptedSecret{
		SecretType:       models.SecretTypeSignalInvite,
		SignalGroupID:    strPtr(connGroup.ID),
		EncryptedPayload: "conn_payload",
		EncryptionIV:     "conn_iv_1234567",
		UpdatedBy:        user1.ID,
	}
	connWrappedKeys := []models.WrappedKeyEntry{
		{UserID: user1.ID, WrappedDEK: "conn_dek_user1"},
		{UserID: user2.ID, WrappedDEK: "conn_dek_user2"},
	}
	if err := secretRepo.Create(ctx, connSecret, connWrappedKeys); err != nil {
		t.Fatalf("Failed to create connection secret: %v", err)
	}

	t.Run("GetPendingRekeys returns both group-owned and connection-owned secrets", func(t *testing.T) {
		// Flag user2 as needing rekey for both secrets
		_ = secretRepo.FlagRekeyForUser(ctx, user2.ID)

		// user1 should see pending rekeys for both group and connection secrets
		rekeys, err := secretRepo.GetPendingRekeys(ctx, user1.ID)
		if err != nil {
			t.Fatalf("Failed to get pending rekeys: %v", err)
		}
		if len(rekeys) != 2 {
			t.Fatalf("Expected 2 pending rekeys (1 group + 1 connection), got %d", len(rekeys))
		}

		// Verify we got both secrets
		secretIDs := make(map[string]bool)
		for _, rekey := range rekeys {
			secretIDs[rekey.SecretID] = true
			if rekey.TargetUserID != user2.ID {
				t.Errorf("Expected target user '%s', got '%s'", user2.ID, rekey.TargetUserID)
			}
			if rekey.TargetPublicKey != "user2_pub_key_multi" {
				t.Errorf("Expected target public key 'user2_pub_key_multi', got '%s'", rekey.TargetPublicKey)
			}
		}

		if !secretIDs[groupSecret.ID] {
			t.Error("Expected group-owned secret ID in pending rekeys")
		}
		if !secretIDs[connSecret.ID] {
			t.Error("Expected connection-owned secret ID in pending rekeys")
		}
	})
}

func TestEncryptedSecretRepository_GetPendingRekeys_MeshtasticSecrets(t *testing.T) {
	db := testDB(t)
	secretRepo := NewEncryptedSecretRepository(db)
	channelRepo := NewMeshtasticChannelRepository(db)
	ekRepo := NewEncryptionKeyRepository(db)
	ctx := context.Background()

	user1 := encCreateUser(t, db, "es_rekey_mesh1")
	user2 := encCreateUser(t, db, "es_rekey_mesh2")
	region := encCreateRegion(t, db, user1.ID, "Rekey Meshtastic Region")

	// Create a meshtastic channel
	channel := &models.MeshtasticChannel{
		RegionID:    strPtr(region.ID),
		ChannelName: "Test Mesh Channel",
		CreatedBy:   strPtr(user1.ID),
	}
	if err := channelRepo.Create(ctx, channel); err != nil {
		t.Fatalf("Failed to create meshtastic channel: %v", err)
	}

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM user_encryption_keys WHERE user_id IN (?, ?)", user1.ID, user2.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id IN (SELECT id FROM encrypted_secrets WHERE meshtastic_channel_id = ?)", channel.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE meshtastic_channel_id = ?", channel.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM meshtastic_channels WHERE id = ?", channel.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?)", user1.ID, user2.ID)
	}()

	// Create encryption keys for user2
	_ = ekRepo.Create(ctx, &models.UserEncryptionKey{
		UserID: user2.ID, PublicKey: "user2_pub_key_mesh", WrappedPrivateKey: "priv2",
		KeySalt: "salt2_12345678901234", KeyIV: "iv2_123456789012",
	})

	// Create meshtastic secret with keys for both users
	meshSecret := &models.EncryptedSecret{
		SecretType:          models.SecretTypeMeshtasticChannel,
		MeshtasticChannelID: strPtr(channel.ID),
		EncryptedPayload:    "mesh_payload",
		EncryptionIV:        "mesh_iv_1234567",
		UpdatedBy:           user1.ID,
	}
	meshWrappedKeys := []models.WrappedKeyEntry{
		{UserID: user1.ID, WrappedDEK: "mesh_dek_user1"},
		{UserID: user2.ID, WrappedDEK: "mesh_dek_user2"},
	}
	if err := secretRepo.Create(ctx, meshSecret, meshWrappedKeys); err != nil {
		t.Fatalf("Failed to create meshtastic secret: %v", err)
	}

	t.Run("GetPendingRekeys includes meshtastic-scoped secrets", func(t *testing.T) {
		// Flag user2 as needing rekey for the meshtastic secret
		_ = secretRepo.FlagRekeyForUser(ctx, user2.ID)

		// user1 should see the pending rekey for the meshtastic secret
		rekeys, err := secretRepo.GetPendingRekeys(ctx, user1.ID)
		if err != nil {
			t.Fatalf("Failed to get pending rekeys: %v", err)
		}
		if len(rekeys) != 1 {
			t.Fatalf("Expected 1 pending rekey (meshtastic), got %d", len(rekeys))
		}

		rekey := rekeys[0]
		if rekey.SecretID != meshSecret.ID {
			t.Errorf("Expected secret ID '%s', got '%s'", meshSecret.ID, rekey.SecretID)
		}
		if rekey.TargetUserID != user2.ID {
			t.Errorf("Expected target user '%s', got '%s'", user2.ID, rekey.TargetUserID)
		}
		if rekey.TargetPublicKey != "user2_pub_key_mesh" {
			t.Errorf("Expected target public key 'user2_pub_key_mesh', got '%s'", rekey.TargetPublicKey)
		}
		// For meshtastic secrets, group_id and group_name should be nil
		if rekey.GroupID != nil {
			t.Errorf("Expected group_id to be nil for meshtastic secret, got '%v'", rekey.GroupID)
		}
		if rekey.GroupName != nil {
			t.Errorf("Expected group_name to be nil for meshtastic secret, got '%v'", rekey.GroupName)
		}
	})
}

func TestEncryptedSecretRepository_SubmitRekey(t *testing.T) {
	db := testDB(t)
	secretRepo := NewEncryptedSecretRepository(db)
	ekRepo := NewEncryptionKeyRepository(db)
	ctx := context.Background()

	user1 := encCreateUser(t, db, "es_submit1")
	user2 := encCreateUser(t, db, "es_submit2")
	region := encCreateRegion(t, db, user1.ID, "Submit Rekey Region")
	group := encCreateSignalGroup(t, db, region.ID, user1.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM user_encryption_keys WHERE user_id IN (?, ?)", user1.ID, user2.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id IN (SELECT id FROM encrypted_secrets WHERE signal_group_id = ?)", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE signal_group_id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?)", user1.ID, user2.ID)
	}()

	_ = ekRepo.Create(ctx, &models.UserEncryptionKey{
		UserID: user2.ID, PublicKey: "pub2", WrappedPrivateKey: "priv2",
		KeySalt: "salt2_12345678901234", KeyIV: "iv2_123456789012",
	})

	secret := &models.EncryptedSecret{
		SecretType:       models.SecretTypeSignalInvite,
		SignalGroupID:    strPtr(group.ID),
		EncryptedPayload: "payload",
		EncryptionIV:     "iv_test_12345678",
		UpdatedBy:        user1.ID,
	}
	wrappedKeys := []models.WrappedKeyEntry{
		{UserID: user1.ID, WrappedDEK: "dek_u1"},
		{UserID: user2.ID, WrappedDEK: "old_dek_u2"},
	}
	_ = secretRepo.Create(ctx, secret, wrappedKeys)

	// Flag and submit rekey
	_ = secretRepo.FlagRekeyForUser(ctx, user2.ID)

	err := secretRepo.SubmitRekey(ctx, secret.ID, user2.ID, "rekeyed_dek_u2")
	if err != nil {
		t.Fatalf("Failed to submit rekey: %v", err)
	}

	// Verify rekey was applied
	dek, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user2.ID)
	if err != nil {
		t.Fatalf("Failed to get DEK after rekey: %v", err)
	}
	if dek != "rekeyed_dek_u2" {
		t.Errorf("Expected 'rekeyed_dek_u2', got '%s'", dek)
	}

	// Verify no more pending rekeys
	rekeys, err := secretRepo.GetPendingRekeys(ctx, user1.ID)
	if err != nil {
		t.Fatalf("Failed to get pending rekeys: %v", err)
	}
	if len(rekeys) != 0 {
		t.Errorf("Expected 0 pending rekeys after submit, got %d", len(rekeys))
	}
}

func TestEncryptedSecretRepository_GetUsersWithSharedSecrets(t *testing.T) {
	db := testDB(t)
	secretRepo := NewEncryptedSecretRepository(db)
	ctx := context.Background()

	user1 := encCreateUser(t, db, "es_shared1")
	user2 := encCreateUser(t, db, "es_shared2")
	user3 := encCreateUser(t, db, "es_shared3")
	region := encCreateRegion(t, db, user1.ID, "Shared Secrets Region")
	group := encCreateSignalGroup(t, db, region.ID, user1.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id IN (SELECT id FROM encrypted_secrets WHERE signal_group_id = ?)", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE signal_group_id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?, ?)", user1.ID, user2.ID, user3.ID)
	}()

	// Create secret shared between user1, user2, and user3
	secret := &models.EncryptedSecret{
		SecretType:       models.SecretTypeSignalInvite,
		SignalGroupID:    strPtr(group.ID),
		EncryptedPayload: "shared_payload",
		EncryptionIV:     "shared_iv_123456",
		UpdatedBy:        user1.ID,
	}
	wrappedKeys := []models.WrappedKeyEntry{
		{UserID: user1.ID, WrappedDEK: "dek1"},
		{UserID: user2.ID, WrappedDEK: "dek2"},
		{UserID: user3.ID, WrappedDEK: "dek3"},
	}
	_ = secretRepo.Create(ctx, secret, wrappedKeys)

	sharedUsers, err := secretRepo.GetUsersWithSharedSecrets(ctx, user1.ID)
	if err != nil {
		t.Fatalf("Failed to get shared users: %v", err)
	}
	if len(sharedUsers) != 2 {
		t.Errorf("Expected 2 shared users, got %d", len(sharedUsers))
	}

	// user1 should not be in the result
	for _, uid := range sharedUsers {
		if uid == user1.ID {
			t.Error("User should not be in their own shared users list")
		}
	}
}

func TestEncryptedSecretRepository_GetByMeshtasticChannelID(t *testing.T) {
	db := testDB(t)
	secretRepo := NewEncryptedSecretRepository(db)
	channelRepo := NewMeshtasticChannelRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "es_mesh")
	region := encCreateRegion(t, db, user.ID, "Meshtastic Secret Region")

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id IN (SELECT id FROM encrypted_secrets WHERE meshtastic_channel_id IS NOT NULL)")
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE meshtastic_channel_id IS NOT NULL")
		_, _ = db.ExecContext(ctx, "DELETE FROM meshtastic_channels WHERE region_id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	channel := &models.MeshtasticChannel{
		RegionID:    strPtr(region.ID),
		ChannelName: "Test Mesh Channel",
		CreatedBy:   strPtr(user.ID),
	}
	if err := channelRepo.Create(ctx, channel); err != nil {
		t.Fatalf("Failed to create channel: %v", err)
	}

	secret := &models.EncryptedSecret{
		SecretType:          models.SecretTypeMeshtasticChannel,
		MeshtasticChannelID: strPtr(channel.ID),
		EncryptedPayload:    "mesh_payload",
		EncryptionIV:        "mesh_iv_12345678",
		UpdatedBy:           user.ID,
	}
	_ = secretRepo.Create(ctx, secret, []models.WrappedKeyEntry{
		{UserID: user.ID, WrappedDEK: "mesh_dek"},
	})

	retrieved, err := secretRepo.GetByMeshtasticChannelID(ctx, channel.ID)
	if err != nil {
		t.Fatalf("Failed to get by meshtastic channel ID: %v", err)
	}
	if retrieved.EncryptedPayload != "mesh_payload" {
		t.Errorf("Expected 'mesh_payload', got '%s'", retrieved.EncryptedPayload)
	}
	if string(retrieved.SecretType) != string(models.SecretTypeMeshtasticChannel) {
		t.Errorf("Expected secret type '%s', got '%s'", models.SecretTypeMeshtasticChannel, retrieved.SecretType)
	}
}

// --- MeshtasticChannelRepository tests ---

func TestMeshtasticChannelRepository_CreateAndGetByID(t *testing.T) {
	db := testDB(t)
	repo := NewMeshtasticChannelRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "mc_create")
	region := encCreateRegion(t, db, user.ID, "Meshtastic Channel Region")

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM meshtastic_channels WHERE region_id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	t.Run("creates channel and retrieves by ID", func(t *testing.T) {
		desc := "Test description"
		channel := &models.MeshtasticChannel{
			RegionID:    strPtr(region.ID),
			ChannelName: "Test Channel",
			Description: &desc,
			CreatedBy:   strPtr(user.ID),
		}

		err := repo.Create(ctx, channel)
		if err != nil {
			t.Fatalf("Failed to create channel: %v", err)
		}
		if channel.ID == "" {
			t.Error("Expected channel ID to be set")
		}
		if !channel.IsActive {
			t.Error("Expected IsActive to be true")
		}

		retrieved, err := repo.GetByID(ctx, channel.ID)
		if err != nil {
			t.Fatalf("Failed to get channel: %v", err)
		}
		if retrieved.ChannelName != "Test Channel" {
			t.Errorf("Expected 'Test Channel', got '%s'", retrieved.ChannelName)
		}
		if retrieved.Description == nil || *retrieved.Description != "Test description" {
			t.Error("Expected description to be 'Test description'")
		}
		if *retrieved.RegionID != region.ID {
			t.Errorf("Expected region ID '%s', got '%s'", region.ID, *retrieved.RegionID)
		}
	})
}

func TestMeshtasticChannelRepository_GetByID_NotFound(t *testing.T) {
	db := testDB(t)
	repo := NewMeshtasticChannelRepository(db)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent-channel")
	if err != ErrMeshtasticChannelNotFound {
		t.Errorf("Expected ErrMeshtasticChannelNotFound, got %v", err)
	}
}

func TestMeshtasticChannelRepository_Update(t *testing.T) {
	db := testDB(t)
	repo := NewMeshtasticChannelRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "mc_update")
	region := encCreateRegion(t, db, user.ID, "Meshtastic Update Region")

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM meshtastic_channels WHERE region_id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	channel := &models.MeshtasticChannel{
		RegionID: strPtr(region.ID), ChannelName: "Original Name", CreatedBy: strPtr(user.ID),
	}
	_ = repo.Create(ctx, channel)

	t.Run("updates name only", func(t *testing.T) {
		newName := "Updated Name"
		err := repo.Update(ctx, channel.ID, &newName, nil)
		if err != nil {
			t.Fatalf("Failed to update name: %v", err)
		}

		retrieved, _ := repo.GetByID(ctx, channel.ID)
		if retrieved.ChannelName != "Updated Name" {
			t.Errorf("Expected 'Updated Name', got '%s'", retrieved.ChannelName)
		}
	})

	t.Run("updates description only", func(t *testing.T) {
		newDesc := "New description"
		err := repo.Update(ctx, channel.ID, nil, &newDesc)
		if err != nil {
			t.Fatalf("Failed to update description: %v", err)
		}

		retrieved, _ := repo.GetByID(ctx, channel.ID)
		if retrieved.Description == nil || *retrieved.Description != "New description" {
			t.Error("Expected description to be 'New description'")
		}
	})

	t.Run("no-op when no fields provided", func(t *testing.T) {
		err := repo.Update(ctx, channel.ID, nil, nil)
		if err != nil {
			t.Fatalf("No-op update should succeed: %v", err)
		}
	})
}

func TestMeshtasticChannelRepository_Deactivate(t *testing.T) {
	db := testDB(t)
	repo := NewMeshtasticChannelRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "mc_deact")
	region := encCreateRegion(t, db, user.ID, "Meshtastic Deactivate Region")

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM meshtastic_channels WHERE region_id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	channel := &models.MeshtasticChannel{
		RegionID: strPtr(region.ID), ChannelName: "To Deactivate", CreatedBy: strPtr(user.ID),
	}
	_ = repo.Create(ctx, channel)

	err := repo.Deactivate(ctx, channel.ID)
	if err != nil {
		t.Fatalf("Failed to deactivate: %v", err)
	}

	retrieved, err := repo.GetByID(ctx, channel.ID)
	if err != nil {
		t.Fatalf("Failed to get deactivated channel: %v", err)
	}
	if retrieved.IsActive {
		t.Error("Expected channel to be inactive")
	}
}

// --- SecretUpdateProposalRepository tests ---

func TestSecretUpdateProposalRepository_CreateAndGetByID(t *testing.T) {
	db := testDB(t)
	proposalRepo := NewSecretUpdateProposalRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "sup_create")
	region := encCreateRegion(t, db, user.ID, "Proposal Create Region")
	group := encCreateSignalGroup(t, db, region.ID, user.ID)
	secret := encCreateSecret(t, db, group.ID, user.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM proposal_wrapped_keys WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_votes WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_proposals WHERE encrypted_secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	t.Run("creates proposal within transaction and retrieves by ID", func(t *testing.T) {
		reason := "rotating keys"
		proposal := &models.SecretUpdateProposal{
			EncryptedSecretID: secret.ID,
			RegionID:          strPtr(region.ID),
			ProposedBy:        user.ID,
			EncryptedPayload:  "new_encrypted_payload",
			EncryptionIV:      "new_iv_12345678901",
			Reason:            &reason,
		}
		wrappedKeys := []models.WrappedKeyEntry{
			{UserID: user.ID, WrappedDEK: "proposal_dek"},
		}

		err := db.Transaction(ctx, func(tx *sql.Tx) error {
			return proposalRepo.CreateTx(ctx, tx, proposal, wrappedKeys)
		})
		if err != nil {
			t.Fatalf("Failed to create proposal: %v", err)
		}

		if proposal.ID == "" {
			t.Error("Expected proposal ID to be set")
		}
		if proposal.Status != models.ProposalStatusPending {
			t.Errorf("Expected status 'pending', got '%s'", proposal.Status)
		}
		if proposal.ExpiresAt.Before(time.Now()) {
			t.Error("Expected expires_at to be in the future")
		}

		retrieved, err := proposalRepo.GetByID(ctx, proposal.ID)
		if err != nil {
			t.Fatalf("Failed to get proposal: %v", err)
		}
		if retrieved.EncryptedSecretID != secret.ID {
			t.Errorf("Expected secret ID '%s', got '%s'", secret.ID, retrieved.EncryptedSecretID)
		}
		if retrieved.EncryptedPayload != "new_encrypted_payload" {
			t.Errorf("Expected 'new_encrypted_payload', got '%s'", retrieved.EncryptedPayload)
		}
	})
}

func TestSecretUpdateProposalRepository_VoteAndCount(t *testing.T) {
	db := testDB(t)
	proposalRepo := NewSecretUpdateProposalRepository(db)
	ctx := context.Background()

	user1 := encCreateUser(t, db, "sup_vote1")
	user2 := encCreateUser(t, db, "sup_vote2")
	region := encCreateRegion(t, db, user1.ID, "Proposal Vote Region")
	group := encCreateSignalGroup(t, db, region.ID, user1.ID)
	secret := encCreateSecret(t, db, group.ID, user1.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM proposal_wrapped_keys WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_votes WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_proposals WHERE encrypted_secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?)", user1.ID, user2.ID)
	}()

	// Create proposal
	proposal := &models.SecretUpdateProposal{
		EncryptedSecretID: secret.ID,
		RegionID:          strPtr(region.ID),
		ProposedBy:        user1.ID,
		EncryptedPayload:  "vote_payload",
		EncryptionIV:      "vote_iv_12345678",
	}
	_ = db.Transaction(ctx, func(tx *sql.Tx) error {
		return proposalRepo.CreateTx(ctx, tx, proposal, []models.WrappedKeyEntry{
			{UserID: user1.ID, WrappedDEK: "dek1"},
		})
	})

	t.Run("adds vote and counts correctly", func(t *testing.T) {
		err := db.Transaction(ctx, func(tx *sql.Tx) error {
			return proposalRepo.AddVoteTx(ctx, tx, proposal.ID, user1.ID, true)
		})
		if err != nil {
			t.Fatalf("Failed to add vote: %v", err)
		}

		var approvalCount int
		err = db.Transaction(ctx, func(tx *sql.Tx) error {
			var txErr error
			approvalCount, txErr = proposalRepo.CountApprovalVotesTx(ctx, tx, proposal.ID)
			return txErr
		})
		if err != nil {
			t.Fatalf("Failed to count votes: %v", err)
		}
		if approvalCount != 1 {
			t.Errorf("Expected 1 approval vote, got %d", approvalCount)
		}
	})

	t.Run("HasVotedTx returns true for voter", func(t *testing.T) {
		var hasVoted bool
		_ = db.Transaction(ctx, func(tx *sql.Tx) error {
			var txErr error
			hasVoted, txErr = proposalRepo.HasVotedTx(ctx, tx, proposal.ID, user1.ID)
			return txErr
		})
		if !hasVoted {
			t.Error("Expected HasVotedTx to return true for user who voted")
		}
	})

	t.Run("HasVotedTx returns false for non-voter", func(t *testing.T) {
		var hasVoted bool
		_ = db.Transaction(ctx, func(tx *sql.Tx) error {
			var txErr error
			hasVoted, txErr = proposalRepo.HasVotedTx(ctx, tx, proposal.ID, user2.ID)
			return txErr
		})
		if hasVoted {
			t.Error("Expected HasVotedTx to return false for user who has not voted")
		}
	})
}

func TestSecretUpdateProposalRepository_UpdateStatus(t *testing.T) {
	db := testDB(t)
	proposalRepo := NewSecretUpdateProposalRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "sup_status")
	region := encCreateRegion(t, db, user.ID, "Proposal Status Region")
	group := encCreateSignalGroup(t, db, region.ID, user.ID)
	secret := encCreateSecret(t, db, group.ID, user.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM proposal_wrapped_keys WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_proposals WHERE encrypted_secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	proposal := &models.SecretUpdateProposal{
		EncryptedSecretID: secret.ID,
		RegionID:          strPtr(region.ID),
		ProposedBy:        user.ID,
		EncryptedPayload:  "status_payload",
		EncryptionIV:      "status_iv_123456",
	}
	_ = db.Transaction(ctx, func(tx *sql.Tx) error {
		return proposalRepo.CreateTx(ctx, tx, proposal, []models.WrappedKeyEntry{
			{UserID: user.ID, WrappedDEK: "dek"},
		})
	})

	t.Run("updates status to approved_pending_finalization", func(t *testing.T) {
		err := proposalRepo.UpdateStatus(ctx, proposal.ID, models.ProposalStatusApprovedPendingFinalization)
		if err != nil {
			t.Fatalf("Failed to update status: %v", err)
		}

		retrieved, err := proposalRepo.GetByID(ctx, proposal.ID)
		if err != nil {
			t.Fatalf("Failed to get proposal: %v", err)
		}
		if retrieved.Status != models.ProposalStatusApprovedPendingFinalization {
			t.Errorf("Expected status '%s', got '%s'", models.ProposalStatusApprovedPendingFinalization, retrieved.Status)
		}
		if retrieved.ResolvedAt != nil {
			t.Error("Expected ResolvedAt to be nil for intermediate status")
		}
	})
}

func TestSecretUpdateProposalRepository_MarkFinalized(t *testing.T) {
	db := testDB(t)
	proposalRepo := NewSecretUpdateProposalRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "sup_final")
	region := encCreateRegion(t, db, user.ID, "Proposal Finalize Region")
	group := encCreateSignalGroup(t, db, region.ID, user.ID)
	secret := encCreateSecret(t, db, group.ID, user.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM proposal_wrapped_keys WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_proposals WHERE encrypted_secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	proposal := &models.SecretUpdateProposal{
		EncryptedSecretID: secret.ID,
		RegionID:          strPtr(region.ID),
		ProposedBy:        user.ID,
		EncryptedPayload:  "final_payload",
		EncryptionIV:      "final_iv_1234567",
	}
	_ = db.Transaction(ctx, func(tx *sql.Tx) error {
		return proposalRepo.CreateTx(ctx, tx, proposal, []models.WrappedKeyEntry{
			{UserID: user.ID, WrappedDEK: "dek"},
		})
	})

	err := proposalRepo.MarkFinalized(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Failed to mark finalized: %v", err)
	}

	retrieved, err := proposalRepo.GetByID(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Failed to get finalized proposal: %v", err)
	}
	if retrieved.FinalizedAt == nil {
		t.Error("Expected FinalizedAt to be set")
	}
	if string(retrieved.Status) != "approved" {
		t.Errorf("Expected status 'approved', got '%s'", retrieved.Status)
	}
}

func TestSecretUpdateProposalRepository_GetWrappedDEK(t *testing.T) {
	db := testDB(t)
	proposalRepo := NewSecretUpdateProposalRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "sup_dek")
	region := encCreateRegion(t, db, user.ID, "Proposal DEK Region")
	group := encCreateSignalGroup(t, db, region.ID, user.ID)
	secret := encCreateSecret(t, db, group.ID, user.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM proposal_wrapped_keys WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_proposals WHERE encrypted_secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	proposal := &models.SecretUpdateProposal{
		EncryptedSecretID: secret.ID,
		RegionID:          strPtr(region.ID),
		ProposedBy:        user.ID,
		EncryptedPayload:  "dek_payload",
		EncryptionIV:      "dek_iv_123456789",
	}
	_ = db.Transaction(ctx, func(tx *sql.Tx) error {
		return proposalRepo.CreateTx(ctx, tx, proposal, []models.WrappedKeyEntry{
			{UserID: user.ID, WrappedDEK: "admin_wrapped_dek"},
		})
	})

	t.Run("retrieves admin wrapped DEK", func(t *testing.T) {
		dek, err := proposalRepo.GetWrappedDEK(ctx, proposal.ID, user.ID)
		if err != nil {
			t.Fatalf("Failed to get wrapped DEK: %v", err)
		}
		if dek != "admin_wrapped_dek" {
			t.Errorf("Expected 'admin_wrapped_dek', got '%s'", dek)
		}
	})

	t.Run("returns error for nonexistent user", func(t *testing.T) {
		_, err := proposalRepo.GetWrappedDEK(ctx, proposal.ID, "nonexistent")
		if err == nil {
			t.Error("Expected error for nonexistent user")
		}
	})
}

func TestSecretUpdateProposalRepository_GetPendingBySecretForUpdate(t *testing.T) {
	db := testDB(t)
	proposalRepo := NewSecretUpdateProposalRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "sup_pending")
	region := encCreateRegion(t, db, user.ID, "Proposal Pending Region")
	group := encCreateSignalGroup(t, db, region.ID, user.ID)
	secret := encCreateSecret(t, db, group.ID, user.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM proposal_wrapped_keys WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_proposals WHERE encrypted_secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	t.Run("returns nil when no pending proposal", func(t *testing.T) {
		var pending *models.SecretUpdateProposal
		err := db.Transaction(ctx, func(tx *sql.Tx) error {
			var txErr error
			pending, txErr = proposalRepo.GetPendingBySecretForUpdate(ctx, tx, secret.ID)
			return txErr
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if pending != nil {
			t.Error("Expected nil when no pending proposals exist")
		}
	})

	t.Run("returns pending proposal", func(t *testing.T) {
		proposal := &models.SecretUpdateProposal{
			EncryptedSecretID: secret.ID,
			RegionID:          strPtr(region.ID),
			ProposedBy:        user.ID,
			EncryptedPayload:  "pending_payload",
			EncryptionIV:      "pending_iv_12345",
		}
		_ = db.Transaction(ctx, func(tx *sql.Tx) error {
			return proposalRepo.CreateTx(ctx, tx, proposal, []models.WrappedKeyEntry{
				{UserID: user.ID, WrappedDEK: "dek"},
			})
		})

		var pending *models.SecretUpdateProposal
		err := db.Transaction(ctx, func(tx *sql.Tx) error {
			var txErr error
			pending, txErr = proposalRepo.GetPendingBySecretForUpdate(ctx, tx, secret.ID)
			return txErr
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if pending == nil {
			t.Fatal("Expected to find pending proposal")
		}
		if pending.ID != proposal.ID {
			t.Errorf("Expected proposal ID '%s', got '%s'", proposal.ID, pending.ID)
		}
	})
}

func TestSecretUpdateProposalRepository_ExpirePendingProposals(t *testing.T) {
	db := testDB(t)
	proposalRepo := NewSecretUpdateProposalRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "sup_expire")
	region := encCreateRegion(t, db, user.ID, "Proposal Expire Region")
	group := encCreateSignalGroup(t, db, region.ID, user.ID)
	secret := encCreateSecret(t, db, group.ID, user.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM proposal_wrapped_keys WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_proposals WHERE encrypted_secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	// Create an already-expired proposal by inserting directly with past expires_at
	proposalID := uuid.New().String()
	now := time.Now().UTC()
	pastExpiry := now.Add(-24 * time.Hour)
	_, err := db.ExecContext(ctx,
		`INSERT INTO secret_update_proposals
		(id, encrypted_secret_id, region_id, proposed_by, encrypted_payload, encryption_iv, status, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
		proposalID, secret.ID, region.ID, user.ID, "payload", "iv_expire_12345", now, pastExpiry,
	)
	if err != nil {
		t.Fatalf("Failed to insert expired proposal: %v", err)
	}

	expired, err := proposalRepo.ExpirePendingProposals(ctx)
	if err != nil {
		t.Fatalf("Failed to expire proposals: %v", err)
	}
	if expired < 1 {
		t.Errorf("Expected at least 1 expired proposal, got %d", expired)
	}

	retrieved, err := proposalRepo.GetByID(ctx, proposalID)
	if err != nil {
		t.Fatalf("Failed to get expired proposal: %v", err)
	}
	if string(retrieved.Status) != "expired" {
		t.Errorf("Expected status 'expired', got '%s'", retrieved.Status)
	}
}

func TestSecretUpdateProposalRepository_GetByIDWithVotes(t *testing.T) {
	db := testDB(t)
	proposalRepo := NewSecretUpdateProposalRepository(db)
	ctx := context.Background()

	user1 := encCreateUser(t, db, "sup_detail1")
	user2 := encCreateUser(t, db, "sup_detail2")
	region := encCreateRegion(t, db, user1.ID, "Proposal Detail Region")
	group := encCreateSignalGroup(t, db, region.ID, user1.ID)
	secret := encCreateSecret(t, db, group.ID, user1.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM proposal_wrapped_keys WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_votes WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_proposals WHERE encrypted_secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?)", user1.ID, user2.ID)
	}()

	reason := "test reason"
	proposal := &models.SecretUpdateProposal{
		EncryptedSecretID: secret.ID,
		RegionID:          strPtr(region.ID),
		ProposedBy:        user1.ID,
		EncryptedPayload:  "detail_payload",
		EncryptionIV:      "detail_iv_12345",
		Reason:            &reason,
	}
	_ = db.Transaction(ctx, func(tx *sql.Tx) error {
		return proposalRepo.CreateTx(ctx, tx, proposal, []models.WrappedKeyEntry{
			{UserID: user1.ID, WrappedDEK: "dek1"},
		})
	})

	// Add votes
	_ = db.Transaction(ctx, func(tx *sql.Tx) error {
		return proposalRepo.AddVoteTx(ctx, tx, proposal.ID, user1.ID, true)
	})
	_ = db.Transaction(ctx, func(tx *sql.Tx) error {
		return proposalRepo.AddVoteTx(ctx, tx, proposal.ID, user2.ID, false)
	})

	detail, err := proposalRepo.GetByIDWithVotes(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Failed to get proposal with votes: %v", err)
	}

	if detail.ID != proposal.ID {
		t.Errorf("Expected ID '%s', got '%s'", proposal.ID, detail.ID)
	}
	if detail.Reason != "test reason" {
		t.Errorf("Expected reason 'test reason', got '%s'", detail.Reason)
	}
	if detail.ProposedBy.ID != user1.ID {
		t.Errorf("Expected proposer ID '%s', got '%s'", user1.ID, detail.ProposedBy.ID)
	}
	if len(detail.Votes) != 2 {
		t.Errorf("Expected 2 votes, got %d", len(detail.Votes))
	}
	if detail.CurrentVotes != 1 {
		t.Errorf("Expected 1 approval vote count, got %d", detail.CurrentVotes)
	}
	if string(detail.SecretType) != string(models.SecretTypeSignalInvite) {
		t.Errorf("Expected secret type '%s', got '%s'", models.SecretTypeSignalInvite, detail.SecretType)
	}
}

func TestSecretUpdateProposalRepository_List(t *testing.T) {
	db := testDB(t)
	proposalRepo := NewSecretUpdateProposalRepository(db)
	regionRepo := NewRegionRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "sup_list")
	region := encCreateRegion(t, db, user.ID, "Proposal List Region")
	group := encCreateSignalGroup(t, db, region.ID, user.ID)
	secret := encCreateSecret(t, db, group.ID, user.ID)

	// Make user admin of region
	_ = regionRepo.AddUserToRegion(ctx, user.ID, region.ID, true)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM proposal_wrapped_keys WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_votes WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_proposals WHERE encrypted_secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	// Create 2 proposals
	for i := 0; i < 2; i++ {
		p := &models.SecretUpdateProposal{
			EncryptedSecretID: secret.ID,
			RegionID:          strPtr(region.ID),
			ProposedBy:        user.ID,
			EncryptedPayload:  "list_payload",
			EncryptionIV:      "list_iv_12345678",
		}
		_ = db.Transaction(ctx, func(tx *sql.Tx) error {
			return proposalRepo.CreateTx(ctx, tx, p, []models.WrappedKeyEntry{
				{UserID: user.ID, WrappedDEK: "dek"},
			})
		})
		// Expire the first one so only 1 is pending
		if i == 0 {
			_ = proposalRepo.UpdateStatus(ctx, p.ID, "expired")
		}
	}

	t.Run("superuser sees all proposals", func(t *testing.T) {
		proposals, err := proposalRepo.List(ctx, user.ID, true, models.SecretProposalListFilter{})
		if err != nil {
			t.Fatalf("Failed to list: %v", err)
		}
		if len(proposals) < 2 {
			t.Errorf("Expected at least 2 proposals, got %d", len(proposals))
		}
	})

	t.Run("superuser filters by status", func(t *testing.T) {
		proposals, err := proposalRepo.List(ctx, user.ID, true, models.SecretProposalListFilter{
			Status: "pending",
		})
		if err != nil {
			t.Fatalf("Failed to list with filter: %v", err)
		}
		for _, p := range proposals {
			if p.Status != "pending" {
				t.Errorf("Expected status 'pending', got '%s'", p.Status)
			}
		}
	})

	t.Run("non-superuser admin sees scoped proposals", func(t *testing.T) {
		proposals, err := proposalRepo.List(ctx, user.ID, false, models.SecretProposalListFilter{})
		if err != nil {
			t.Fatalf("Failed to list as admin: %v", err)
		}
		if len(proposals) < 2 {
			t.Errorf("Expected at least 2 proposals for admin, got %d", len(proposals))
		}
	})
}

func TestSecretUpdateProposalRepository_UpdateStatusTx(t *testing.T) {
	db := testDB(t)
	proposalRepo := NewSecretUpdateProposalRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "sup_statustx")
	region := encCreateRegion(t, db, user.ID, "StatusTx Region")
	group := encCreateSignalGroup(t, db, region.ID, user.ID)
	secret := encCreateSecret(t, db, group.ID, user.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM proposal_wrapped_keys WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_proposals WHERE encrypted_secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	proposal := &models.SecretUpdateProposal{
		EncryptedSecretID: secret.ID,
		RegionID:          strPtr(region.ID),
		ProposedBy:        user.ID,
		EncryptedPayload:  "tx_payload",
		EncryptionIV:      "tx_iv_1234567890",
	}
	_ = db.Transaction(ctx, func(tx *sql.Tx) error {
		return proposalRepo.CreateTx(ctx, tx, proposal, []models.WrappedKeyEntry{
			{UserID: user.ID, WrappedDEK: "dek"},
		})
	})

	err := db.Transaction(ctx, func(tx *sql.Tx) error {
		return proposalRepo.UpdateStatusTx(ctx, tx, proposal.ID, "rejected")
	})
	if err != nil {
		t.Fatalf("Failed to update status in tx: %v", err)
	}

	retrieved, err := proposalRepo.GetByID(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Failed to get: %v", err)
	}
	if string(retrieved.Status) != "rejected" {
		t.Errorf("Expected status 'rejected', got '%s'", retrieved.Status)
	}
}

func TestSecretUpdateProposalRepository_GetByIDForUpdate(t *testing.T) {
	db := testDB(t)
	proposalRepo := NewSecretUpdateProposalRepository(db)
	ctx := context.Background()

	user := encCreateUser(t, db, "sup_forupd")
	region := encCreateRegion(t, db, user.ID, "ForUpdate Region")
	group := encCreateSignalGroup(t, db, region.ID, user.ID)
	secret := encCreateSecret(t, db, group.ID, user.ID)

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM proposal_wrapped_keys WHERE proposal_id IN (SELECT id FROM secret_update_proposals WHERE encrypted_secret_id = ?)", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM secret_update_proposals WHERE encrypted_secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secret_keys WHERE secret_id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM encrypted_secrets WHERE id = ?", secret.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", group.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", region.ID)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", user.ID)
	}()

	proposal := &models.SecretUpdateProposal{
		EncryptedSecretID: secret.ID,
		RegionID:          strPtr(region.ID),
		ProposedBy:        user.ID,
		EncryptedPayload:  "forupd_payload",
		EncryptionIV:      "forupd_iv_12345",
	}
	_ = db.Transaction(ctx, func(tx *sql.Tx) error {
		return proposalRepo.CreateTx(ctx, tx, proposal, []models.WrappedKeyEntry{
			{UserID: user.ID, WrappedDEK: "dek"},
		})
	})

	var retrieved *models.SecretUpdateProposal
	err := db.Transaction(ctx, func(tx *sql.Tx) error {
		var txErr error
		retrieved, txErr = proposalRepo.GetByIDForUpdate(ctx, tx, proposal.ID)
		return txErr
	})
	if err != nil {
		t.Fatalf("Failed to get for update: %v", err)
	}
	if retrieved.ID != proposal.ID {
		t.Errorf("Expected ID '%s', got '%s'", proposal.ID, retrieved.ID)
	}
}

func TestEncryptedSecretRepository_RevokeConnectionSecretKeysForGroup(t *testing.T) {
	db := testDB(t)
	secretRepo := NewEncryptedSecretRepository(db)
	ctx := context.Background()

	user1 := encCreateUser(t, db, "revoke_u1")
	user2 := encCreateUser(t, db, "revoke_u2")
	user3 := encCreateUser(t, db, "revoke_u3")

	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE connection_id IS NOT NULL")
		_, _ = db.ExecContext(ctx, "DELETE FROM group_members WHERE group_id IN (SELECT id FROM `groups` WHERE name LIKE 'test_group_%')")
		_, _ = db.ExecContext(ctx, "DELETE FROM `groups` WHERE name LIKE 'test_group_%'")
		_, _ = db.ExecContext(ctx, "DELETE FROM connection_members")
		_, _ = db.ExecContext(ctx, "DELETE FROM connections")
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?, ?)", user1.ID, user2.ID, user3.ID)
	}()

	connID := uuid.New().String()
	_, _ = db.ExecContext(ctx, "INSERT INTO connections (id, name) VALUES (?, ?)", connID, "test_connection")

	group1ID := uuid.New().String()
	group2ID := uuid.New().String()
	_, _ = db.ExecContext(ctx, `INSERT INTO `+"`groups`"+` (id, name, status) VALUES (?, ?, 'active')`, group1ID, "test_group_1")
	_, _ = db.ExecContext(ctx, `INSERT INTO `+"`groups`"+` (id, name, status) VALUES (?, ?, 'active')`, group2ID, "test_group_2")

	_, _ = db.ExecContext(ctx, "INSERT INTO connection_members (id, connection_id, group_id) VALUES (?, ?, ?)", uuid.New().String(), connID, group1ID)
	_, _ = db.ExecContext(ctx, "INSERT INTO connection_members (id, connection_id, group_id) VALUES (?, ?, ?)", uuid.New().String(), connID, group2ID)

	_, _ = db.ExecContext(ctx, "INSERT INTO group_members (id, group_id, user_id) VALUES (?, ?, ?)", uuid.New().String(), group1ID, user1.ID)
	_, _ = db.ExecContext(ctx, "INSERT INTO group_members (id, group_id, user_id) VALUES (?, ?, ?)", uuid.New().String(), group2ID, user2.ID)
	_, _ = db.ExecContext(ctx, "INSERT INTO group_members (id, group_id, user_id) VALUES (?, ?, ?)", uuid.New().String(), group2ID, user3.ID)

	sigGroupID := uuid.New().String()
	_, _ = db.ExecContext(ctx, `INSERT INTO signal_groups (id, connection_id, group_name, created_by, created_at, is_active)
		VALUES (?, ?, 'test_sig_group', ?, NOW(), TRUE)`, sigGroupID, connID, user1.ID)

	secret := &models.EncryptedSecret{
		SecretType:       models.SecretTypeSignalInvite,
		SignalGroupID:    strPtr(sigGroupID),
		EncryptedPayload: "revoke_payload",
		EncryptionIV:     "revoke_iv_123456",
		UpdatedBy:        user1.ID,
	}
	wrappedKeys := []models.WrappedKeyEntry{
		{UserID: user1.ID, WrappedDEK: "dek_u1"},
		{UserID: user2.ID, WrappedDEK: "dek_u2"},
		{UserID: user3.ID, WrappedDEK: "dek_u3"},
	}
	if err := secretRepo.Create(ctx, secret, wrappedKeys); err != nil {
		t.Fatalf("Failed to create secret: %v", err)
	}

	err := db.Transaction(ctx, func(tx *sql.Tx) error {
		return secretRepo.RevokeConnectionSecretKeysForGroup(ctx, tx, connID, group2ID)
	})
	if err != nil {
		t.Fatalf("Failed to revoke keys: %v", err)
	}

	dek1, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user1.ID)
	if err != nil {
		t.Fatalf("Failed to get user1 DEK: %v", err)
	}
	if dek1 != "dek_u1" {
		t.Errorf("user1 (survivor) key should be unchanged, got '%s'", dek1)
	}

	_, err = secretRepo.GetWrappedDEK(ctx, secret.ID, user2.ID)
	if err == nil {
		t.Error("user2 (leaving) key should be deleted")
	}

	_, err = secretRepo.GetWrappedDEK(ctx, secret.ID, user3.ID)
	if err == nil {
		t.Error("user3 (leaving) key should be deleted")
	}

	var rekeyNeeded bool
	err = db.QueryRowContext(ctx, `
		SELECT rekey_needed FROM encrypted_secret_keys
		WHERE secret_id = ? AND user_id = ?
	`, secret.ID, user1.ID).Scan(&rekeyNeeded)
	if err != nil {
		t.Fatalf("Failed to query rekey flag for user1: %v", err)
	}
	if !rekeyNeeded {
		t.Error("user1 (survivor) should have rekey_needed=true")
	}
}

func TestEncryptedSecretRepository_RevokeGroupSecretKeysForUser(t *testing.T) {
	db := testDB(t)
	secretRepo := NewEncryptedSecretRepository(db)
	sgRepo := NewSignalGroupRepository(db)
	ctx := context.Background()

	user1ID := createConnectionTestUser(t, db, "rg_u1")
	user2ID := createConnectionTestUser(t, db, "rg_u2")
	user3ID := createConnectionTestUser(t, db, "rg_u3")

	regionID := createConnectionTestRegion(t, db, "RevokeGroupKeys Region")
	groupID := createConnectionTestGroup(t, db, "RevokeGroupKeys Group", user1ID, regionID)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM signal_groups WHERE owner_group_id = ?", groupID)
		cleanupConnectionTest(t, db, []string{groupID}, []string{user1ID, user2ID, user3ID}, []string{regionID}, nil)
	})

	sg := &models.SignalGroup{
		OwnerGroupID: &groupID,
		GroupName:    "Revoke Group Keys Signal Group",
		AccessTier:   models.AccessTierMember,
		CreatedBy:    &user1ID,
	}
	if err := sgRepo.CreateForOwnerGroup(ctx, sg); err != nil {
		t.Fatalf("Failed to create signal group: %v", err)
	}

	secret := &models.EncryptedSecret{
		SecretType:       models.SecretTypeSignalInvite,
		SignalGroupID:    &sg.ID,
		EncryptedPayload: "rg_revoke_payload",
		EncryptionIV:     "rg_revoke_iv_1234",
		UpdatedBy:        user1ID,
	}
	wrappedKeys := []models.WrappedKeyEntry{
		{UserID: user1ID, WrappedDEK: "dek_rg_u1"},
		{UserID: user2ID, WrappedDEK: "dek_rg_u2"},
		{UserID: user3ID, WrappedDEK: "dek_rg_u3"},
	}
	if err := secretRepo.Create(ctx, secret, wrappedKeys); err != nil {
		t.Fatalf("Failed to create secret: %v", err)
	}

	err := db.Transaction(ctx, func(tx *sql.Tx) error {
		return secretRepo.RevokeGroupSecretKeysForUser(ctx, tx, groupID, user1ID)
	})
	if err != nil {
		t.Fatalf("RevokeGroupSecretKeysForUser failed: %v", err)
	}

	// Revoked user's row must be gone.
	_, err = secretRepo.GetWrappedDEK(ctx, secret.ID, user1ID)
	if err == nil {
		t.Error("user1 (revoked) key should be deleted")
	}

	// Survivors must still have their keys.
	dek2, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user2ID)
	if err != nil {
		t.Fatalf("Failed to get user2 DEK: %v", err)
	}
	if dek2 != "dek_rg_u2" {
		t.Errorf("user2 (survivor) key should be unchanged, got '%s'", dek2)
	}
	dek3, err := secretRepo.GetWrappedDEK(ctx, secret.ID, user3ID)
	if err != nil {
		t.Fatalf("Failed to get user3 DEK: %v", err)
	}
	if dek3 != "dek_rg_u3" {
		t.Errorf("user3 (survivor) key should be unchanged, got '%s'", dek3)
	}

	// Survivors must have rekey_needed=TRUE.
	for _, uid := range []string{user2ID, user3ID} {
		var rekeyNeeded bool
		if err := db.QueryRowContext(ctx, `
			SELECT rekey_needed FROM encrypted_secret_keys
			WHERE secret_id = ? AND user_id = ?
		`, secret.ID, uid).Scan(&rekeyNeeded); err != nil {
			t.Fatalf("Failed to query rekey flag for %s: %v", uid, err)
		}
		if !rekeyNeeded {
			t.Errorf("survivor %s should have rekey_needed=true", uid)
		}
	}
}
