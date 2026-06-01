package services

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"strings"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
)

var (
	ErrInvalidMFACode    = errors.New("invalid MFA code")
	ErrMFANotEnabled     = errors.New("MFA is not enabled for this user")
	ErrInvalidBackupCode = errors.New("invalid backup code")
	ErrNoBackupCodes     = errors.New("no backup codes remaining")
	ErrEncryptionKey     = errors.New("invalid encryption key length (must be 32 bytes)")
)

// MFAService handles TOTP generation, validation, and backup codes
type MFAService struct {
	encryptionKey []byte
	issuer        string
}

// NewMFAService creates a new MFA service
func NewMFAService(cfg *config.MFAConfig) (*MFAService, error) {
	keyBytes := []byte(cfg.EncryptionKey)
	if len(keyBytes) != 32 {
		return nil, ErrEncryptionKey
	}

	return &MFAService{
		encryptionKey: keyBytes,
		issuer:        cfg.Issuer,
	}, nil
}

// GenerateSecret generates a new TOTP secret for a user
func (s *MFAService) GenerateSecret(email string) (*otp.Key, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      s.issuer,
		AccountName: email,
		SecretSize:  32,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate TOTP secret: %w", err)
	}
	return key, nil
}

// GenerateQRCode generates a QR code image as base64 PNG for the TOTP key
func (s *MFAService) GenerateQRCode(key *otp.Key) (string, error) {
	var buf bytes.Buffer
	img, err := key.Image(256, 256)
	if err != nil {
		return "", fmt.Errorf("failed to generate QR code image: %w", err)
	}

	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("failed to encode QR code as PNG: %w", err)
	}

	base64Image := base64.StdEncoding.EncodeToString(buf.Bytes())
	return "data:image/png;base64," + base64Image, nil
}

// EncryptSecret encrypts a TOTP secret for storage
func (s *MFAService) EncryptSecret(secret string) (string, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(secret), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptSecret decrypts a stored TOTP secret
func (s *MFAService) DecryptSecret(encryptedSecret string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedSecret)
	if err != nil {
		return "", fmt.Errorf("failed to decode secret: %w", err)
	}

	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt secret: %w", err)
	}

	return string(plaintext), nil
}

// ValidateCode validates a TOTP code against a secret
func (s *MFAService) ValidateCode(encryptedSecret string, code string) (bool, error) {
	secret, err := s.DecryptSecret(encryptedSecret)
	if err != nil {
		return false, err
	}

	valid := totp.Validate(code, secret)
	return valid, nil
}

// GenerateBackupCodes generates a set of backup codes
func (s *MFAService) GenerateBackupCodes(count int) ([]string, error) {
	codes := make([]string, count)
	for i := 0; i < count; i++ {
		codeBytes := make([]byte, 4) // 4 bytes = 8 hex chars
		if _, err := rand.Read(codeBytes); err != nil {
			return nil, fmt.Errorf("failed to generate backup code: %w", err)
		}
		// Format as XXXX-XXXX for readability
		code := fmt.Sprintf("%02x%02x-%02x%02x", codeBytes[0], codeBytes[1], codeBytes[2], codeBytes[3])
		codes[i] = strings.ToUpper(code)
	}
	return codes, nil
}

// HashBackupCodes hashes backup codes for storage
func (s *MFAService) HashBackupCodes(codes []string) (string, error) {
	hashedCodes := make([]string, len(codes))
	for i, code := range codes {
		// Normalize code: remove dashes and uppercase
		normalizedCode := strings.ToUpper(strings.ReplaceAll(code, "-", ""))
		hash, err := bcrypt.GenerateFromPassword([]byte(normalizedCode), 12)
		if err != nil {
			return "", fmt.Errorf("failed to hash backup code: %w", err)
		}
		hashedCodes[i] = string(hash)
	}

	jsonBytes, err := json.Marshal(hashedCodes)
	if err != nil {
		return "", fmt.Errorf("failed to marshal backup codes: %w", err)
	}

	return string(jsonBytes), nil
}

// ValidateBackupCode validates and consumes a backup code
// Returns the updated backup codes JSON (with the used code removed) and any error
func (s *MFAService) ValidateBackupCode(backupCodesJSON string, code string) (string, error) {
	if backupCodesJSON == "" {
		return "", ErrNoBackupCodes
	}

	var hashedCodes []string
	if err := json.Unmarshal([]byte(backupCodesJSON), &hashedCodes); err != nil {
		return "", fmt.Errorf("failed to parse backup codes: %w", err)
	}

	if len(hashedCodes) == 0 {
		return "", ErrNoBackupCodes
	}

	// Normalize input code: remove dashes and uppercase
	normalizedCode := strings.ToUpper(strings.ReplaceAll(code, "-", ""))

	// Find and remove the matching code
	matchIndex := -1
	for i, hashedCode := range hashedCodes {
		if err := bcrypt.CompareHashAndPassword([]byte(hashedCode), []byte(normalizedCode)); err == nil {
			matchIndex = i
			break
		}
	}

	if matchIndex == -1 {
		return "", ErrInvalidBackupCode
	}

	// Remove the used code
	hashedCodes = append(hashedCodes[:matchIndex], hashedCodes[matchIndex+1:]...)

	// Return updated JSON
	updatedJSON, err := json.Marshal(hashedCodes)
	if err != nil {
		return "", fmt.Errorf("failed to marshal updated backup codes: %w", err)
	}

	return string(updatedJSON), nil
}

// GetBackupCodeCount returns the number of remaining backup codes
func (s *MFAService) GetBackupCodeCount(backupCodesJSON string) int {
	if backupCodesJSON == "" {
		return 0
	}

	var hashedCodes []string
	if err := json.Unmarshal([]byte(backupCodesJSON), &hashedCodes); err != nil {
		return 0
	}

	return len(hashedCodes)
}
