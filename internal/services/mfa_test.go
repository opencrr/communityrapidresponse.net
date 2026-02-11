package services

import (
	"strings"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
)

func testMFAConfig() *config.MFAConfig {
	return &config.MFAConfig{
		EncryptionKey: "01234567890123456789012345678901", // Exactly 32 chars
		Issuer:        "Test Issuer",
	}
}

func TestNewMFAService(t *testing.T) {
	t.Run("creates service with valid config", func(t *testing.T) {
		service, err := NewMFAService(testMFAConfig())
		if err != nil {
			t.Fatalf("Failed to create MFA service: %v", err)
		}
		if service == nil {
			t.Error("Expected non-nil service")
		}
	})

	t.Run("rejects invalid encryption key length", func(t *testing.T) {
		cfg := &config.MFAConfig{
			EncryptionKey: "too_short",
			Issuer:        "Test",
		}
		_, err := NewMFAService(cfg)
		if err != ErrEncryptionKey {
			t.Errorf("Expected ErrEncryptionKey, got %v", err)
		}
	})

	t.Run("rejects empty encryption key", func(t *testing.T) {
		cfg := &config.MFAConfig{
			EncryptionKey: "",
			Issuer:        "Test",
		}
		_, err := NewMFAService(cfg)
		if err != ErrEncryptionKey {
			t.Errorf("Expected ErrEncryptionKey, got %v", err)
		}
	})
}

func TestMFAService_GenerateSecret(t *testing.T) {
	service, _ := NewMFAService(testMFAConfig())

	t.Run("generates valid TOTP key", func(t *testing.T) {
		key, err := service.GenerateSecret("test@example.com")
		if err != nil {
			t.Fatalf("Failed to generate secret: %v", err)
		}

		if key == nil {
			t.Fatal("Expected non-nil key")
		}

		if key.Secret() == "" {
			t.Error("Expected non-empty secret")
		}

		if key.AccountName() != "test@example.com" {
			t.Errorf("Expected account 'test@example.com', got '%s'", key.AccountName())
		}

		if key.Issuer() != "Test Issuer" {
			t.Errorf("Expected issuer 'Test Issuer', got '%s'", key.Issuer())
		}
	})

	t.Run("generates unique secrets", func(t *testing.T) {
		key1, _ := service.GenerateSecret("user1@example.com")
		key2, _ := service.GenerateSecret("user2@example.com")

		if key1.Secret() == key2.Secret() {
			t.Error("Expected unique secrets for different users")
		}
	})
}

func TestMFAService_EncryptDecrypt(t *testing.T) {
	service, _ := NewMFAService(testMFAConfig())

	t.Run("encrypts and decrypts secret", func(t *testing.T) {
		originalSecret := "JBSWY3DPEHPK3PXP"

		encrypted, err := service.EncryptSecret(originalSecret)
		if err != nil {
			t.Fatalf("Failed to encrypt: %v", err)
		}

		if encrypted == "" {
			t.Error("Expected non-empty encrypted value")
		}

		if encrypted == originalSecret {
			t.Error("Encrypted value should not equal original")
		}

		decrypted, err := service.DecryptSecret(encrypted)
		if err != nil {
			t.Fatalf("Failed to decrypt: %v", err)
		}

		if decrypted != originalSecret {
			t.Errorf("Expected '%s', got '%s'", originalSecret, decrypted)
		}
	})

	t.Run("produces different ciphertext for same plaintext", func(t *testing.T) {
		secret := "JBSWY3DPEHPK3PXP"

		encrypted1, _ := service.EncryptSecret(secret)
		encrypted2, _ := service.EncryptSecret(secret)

		// Due to random nonce, ciphertext should differ
		if encrypted1 == encrypted2 {
			t.Error("Expected different ciphertext due to random nonce")
		}

		// But both should decrypt to same value
		decrypted1, _ := service.DecryptSecret(encrypted1)
		decrypted2, _ := service.DecryptSecret(encrypted2)

		if decrypted1 != decrypted2 {
			t.Error("Both should decrypt to same value")
		}
	})

	t.Run("fails to decrypt with wrong key", func(t *testing.T) {
		secret := "JBSWY3DPEHPK3PXP"
		encrypted, _ := service.EncryptSecret(secret)

		// Create service with different key
		otherService, _ := NewMFAService(&config.MFAConfig{
			EncryptionKey: "zyxwvutsrqponmlkjihgfedcba123456",
			Issuer:        "Test",
		})

		_, err := otherService.DecryptSecret(encrypted)
		if err == nil {
			t.Error("Expected error when decrypting with wrong key")
		}
	})

	t.Run("fails to decrypt invalid base64", func(t *testing.T) {
		_, err := service.DecryptSecret("not-valid-base64!!!")
		if err == nil {
			t.Error("Expected error for invalid base64")
		}
	})
}

func TestMFAService_ValidateCode(t *testing.T) {
	service, _ := NewMFAService(testMFAConfig())

	// Generate a secret and encrypt it
	key, _ := service.GenerateSecret("test@example.com")
	encryptedSecret, _ := service.EncryptSecret(key.Secret())

	t.Run("rejects invalid code format", func(t *testing.T) {
		valid, err := service.ValidateCode(encryptedSecret, "12345") // 5 digits
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if valid {
			t.Error("Expected invalid code to be rejected")
		}
	})

	t.Run("rejects random code", func(t *testing.T) {
		valid, err := service.ValidateCode(encryptedSecret, "000000")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// Very unlikely to be valid
		if valid {
			t.Log("Warning: Random code was valid (very unlikely)")
		}
	})

	t.Run("fails with invalid encrypted secret", func(t *testing.T) {
		_, err := service.ValidateCode("invalid-encrypted", "123456")
		if err == nil {
			t.Error("Expected error for invalid encrypted secret")
		}
	})
}

func TestMFAService_GenerateQRCode(t *testing.T) {
	service, _ := NewMFAService(testMFAConfig())

	t.Run("generates valid QR code data URL", func(t *testing.T) {
		key, _ := service.GenerateSecret("test@example.com")

		qrCode, err := service.GenerateQRCode(key)
		if err != nil {
			t.Fatalf("Failed to generate QR code: %v", err)
		}

		if !strings.HasPrefix(qrCode, "data:image/png;base64,") {
			t.Error("Expected data URL with PNG prefix")
		}

		// Should have substantial base64 data
		base64Data := strings.TrimPrefix(qrCode, "data:image/png;base64,")
		if len(base64Data) < 100 {
			t.Error("Expected substantial base64 data for QR code image")
		}
	})
}

func TestMFAService_GenerateBackupCodes(t *testing.T) {
	service, _ := NewMFAService(testMFAConfig())

	t.Run("generates correct number of codes", func(t *testing.T) {
		codes, err := service.GenerateBackupCodes(10)
		if err != nil {
			t.Fatalf("Failed to generate backup codes: %v", err)
		}

		if len(codes) != 10 {
			t.Errorf("Expected 10 codes, got %d", len(codes))
		}
	})

	t.Run("generates codes in correct format", func(t *testing.T) {
		codes, _ := service.GenerateBackupCodes(5)

		for i, code := range codes {
			// Format should be XXXX-XXXX (uppercase hex)
			if len(code) != 9 {
				t.Errorf("Code %d: expected length 9, got %d", i, len(code))
			}
			if code[4] != '-' {
				t.Errorf("Code %d: expected dash at position 4", i)
			}
			// Check uppercase
			if code != strings.ToUpper(code) {
				t.Errorf("Code %d: expected uppercase, got '%s'", i, code)
			}
		}
	})

	t.Run("generates unique codes", func(t *testing.T) {
		codes, _ := service.GenerateBackupCodes(10)

		seen := make(map[string]bool)
		for _, code := range codes {
			if seen[code] {
				t.Errorf("Duplicate code generated: %s", code)
			}
			seen[code] = true
		}
	})
}

func TestMFAService_HashBackupCodes(t *testing.T) {
	service, _ := NewMFAService(testMFAConfig())

	t.Run("hashes codes to JSON array", func(t *testing.T) {
		codes := []string{"ABCD-1234", "EFGH-5678"}

		hashedJSON, err := service.HashBackupCodes(codes)
		if err != nil {
			t.Fatalf("Failed to hash codes: %v", err)
		}

		if !strings.HasPrefix(hashedJSON, "[") || !strings.HasSuffix(hashedJSON, "]") {
			t.Error("Expected JSON array format")
		}

		// Should contain bcrypt hashes (start with $2)
		if !strings.Contains(hashedJSON, "$2") {
			t.Error("Expected bcrypt hash format in JSON")
		}
	})
}

func TestMFAService_ValidateBackupCode(t *testing.T) {
	service, _ := NewMFAService(testMFAConfig())

	// Generate and hash codes
	codes := []string{"ABCD-1234", "EFGH-5678", "IJKL-9012"}
	hashedJSON, _ := service.HashBackupCodes(codes)

	t.Run("validates correct backup code", func(t *testing.T) {
		updatedJSON, err := service.ValidateBackupCode(hashedJSON, "ABCD-1234")
		if err != nil {
			t.Fatalf("Failed to validate correct code: %v", err)
		}

		// Updated JSON should have one fewer code
		if updatedJSON == hashedJSON {
			t.Error("Expected updated JSON to be different")
		}
	})

	t.Run("validates code without dash", func(t *testing.T) {
		_, err := service.ValidateBackupCode(hashedJSON, "EFGH5678")
		if err != nil {
			t.Fatalf("Failed to validate code without dash: %v", err)
		}
	})

	t.Run("validates lowercase code", func(t *testing.T) {
		_, err := service.ValidateBackupCode(hashedJSON, "ijkl-9012")
		if err != nil {
			t.Fatalf("Failed to validate lowercase code: %v", err)
		}
	})

	t.Run("rejects invalid backup code", func(t *testing.T) {
		_, err := service.ValidateBackupCode(hashedJSON, "XXXX-YYYY")
		if err != ErrInvalidBackupCode {
			t.Errorf("Expected ErrInvalidBackupCode, got %v", err)
		}
	})

	t.Run("rejects already used code", func(t *testing.T) {
		// Use the code once
		updatedJSON, _ := service.ValidateBackupCode(hashedJSON, "ABCD-1234")

		// Try to use it again
		_, err := service.ValidateBackupCode(updatedJSON, "ABCD-1234")
		if err != ErrInvalidBackupCode {
			t.Errorf("Expected ErrInvalidBackupCode for used code, got %v", err)
		}
	})

	t.Run("returns error for empty codes JSON", func(t *testing.T) {
		_, err := service.ValidateBackupCode("", "ABCD-1234")
		if err != ErrNoBackupCodes {
			t.Errorf("Expected ErrNoBackupCodes, got %v", err)
		}
	})

	t.Run("returns error for empty array", func(t *testing.T) {
		_, err := service.ValidateBackupCode("[]", "ABCD-1234")
		if err != ErrNoBackupCodes {
			t.Errorf("Expected ErrNoBackupCodes, got %v", err)
		}
	})
}

func TestMFAService_GetBackupCodeCount(t *testing.T) {
	service, _ := NewMFAService(testMFAConfig())

	t.Run("counts backup codes correctly", func(t *testing.T) {
		codes := []string{"A", "B", "C", "D", "E"}
		hashedJSON, _ := service.HashBackupCodes(codes)

		count := service.GetBackupCodeCount(hashedJSON)
		if count != 5 {
			t.Errorf("Expected 5, got %d", count)
		}
	})

	t.Run("returns 0 for empty JSON", func(t *testing.T) {
		count := service.GetBackupCodeCount("")
		if count != 0 {
			t.Errorf("Expected 0, got %d", count)
		}
	})

	t.Run("returns 0 for empty array", func(t *testing.T) {
		count := service.GetBackupCodeCount("[]")
		if count != 0 {
			t.Errorf("Expected 0, got %d", count)
		}
	})

	t.Run("returns 0 for invalid JSON", func(t *testing.T) {
		count := service.GetBackupCodeCount("not json")
		if count != 0 {
			t.Errorf("Expected 0, got %d", count)
		}
	})
}

func TestMFAService_Integration(t *testing.T) {
	service, _ := NewMFAService(testMFAConfig())

	t.Run("full MFA setup flow", func(t *testing.T) {
		// 1. Generate secret
		key, err := service.GenerateSecret("user@example.com")
		if err != nil {
			t.Fatalf("Generate secret failed: %v", err)
		}

		// 2. Encrypt secret for storage
		encryptedSecret, err := service.EncryptSecret(key.Secret())
		if err != nil {
			t.Fatalf("Encrypt secret failed: %v", err)
		}

		// 3. Generate QR code
		qrCode, err := service.GenerateQRCode(key)
		if err != nil {
			t.Fatalf("Generate QR code failed: %v", err)
		}
		if qrCode == "" {
			t.Error("QR code should not be empty")
		}

		// 4. Generate backup codes
		backupCodes, err := service.GenerateBackupCodes(10)
		if err != nil {
			t.Fatalf("Generate backup codes failed: %v", err)
		}

		// 5. Hash backup codes for storage
		hashedCodes, err := service.HashBackupCodes(backupCodes)
		if err != nil {
			t.Fatalf("Hash backup codes failed: %v", err)
		}

		// 6. Verify we can decrypt the secret
		decrypted, err := service.DecryptSecret(encryptedSecret)
		if err != nil {
			t.Fatalf("Decrypt secret failed: %v", err)
		}
		if decrypted != key.Secret() {
			t.Error("Decrypted secret doesn't match original")
		}

		// 7. Verify backup codes work
		updatedCodes, err := service.ValidateBackupCode(hashedCodes, backupCodes[0])
		if err != nil {
			t.Fatalf("Validate backup code failed: %v", err)
		}

		// 8. Verify code count decreased
		count := service.GetBackupCodeCount(updatedCodes)
		if count != 9 {
			t.Errorf("Expected 9 remaining codes, got %d", count)
		}
	})
}
