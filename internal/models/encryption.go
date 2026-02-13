package models

import "time"

// UserEncryptionKey stores a user's public key and wrapped (encrypted) private key backup
type UserEncryptionKey struct {
	UserID            string    `json:"user_id" db:"user_id"`
	PublicKey         string    `json:"public_key" db:"public_key"`
	WrappedPrivateKey string    `json:"wrapped_private_key" db:"wrapped_private_key"`
	KeySalt           string    `json:"key_salt" db:"key_salt"`
	KeyIV             string    `json:"key_iv" db:"key_iv"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
	RotatedAt         time.Time `json:"rotated_at" db:"rotated_at"`
}

// CreateEncryptionKeyRequest is the request body for uploading encryption keys
type CreateEncryptionKeyRequest struct {
	PublicKey         string `json:"public_key" validate:"required"`
	WrappedPrivateKey string `json:"wrapped_private_key" validate:"required"`
	KeySalt           string `json:"key_salt" validate:"required"`
	KeyIV             string `json:"key_iv" validate:"required"`
}

// UpdateEncryptionKeyRequest is the request body for re-wrapping the private key (e.g. password change)
type UpdateEncryptionKeyRequest struct {
	WrappedPrivateKey string `json:"wrapped_private_key" validate:"required"`
	KeySalt           string `json:"key_salt" validate:"required"`
	KeyIV             string `json:"key_iv" validate:"required"`
}

// RotateEncryptionKeyRequest is the request body for uploading a new keypair (e.g. password reset).
// Identical fields to CreateEncryptionKeyRequest but represents a distinct operation (rotation vs initial creation).
type RotateEncryptionKeyRequest struct {
	PublicKey         string `json:"public_key" validate:"required"`
	WrappedPrivateKey string `json:"wrapped_private_key" validate:"required"`
	KeySalt           string `json:"key_salt" validate:"required"`
	KeyIV             string `json:"key_iv" validate:"required"`
}

// EncryptionKeyResponse is returned when fetching own wrapped backup
type EncryptionKeyResponse struct {
	PublicKey         string    `json:"public_key"`
	WrappedPrivateKey string    `json:"wrapped_private_key"`
	KeySalt           string    `json:"key_salt"`
	KeyIV             string    `json:"key_iv"`
	RotatedAt         time.Time `json:"rotated_at"`
}

// PublicKeyEntry represents a user's public key for encryption
type PublicKeyEntry struct {
	UserID    string `json:"user_id"`
	PublicKey string `json:"public_key"`
}

// RekeyEntry represents a single re-keyed wrapped DEK
type RekeyEntry struct {
	SecretID     string `json:"secret_id"`
	TargetUserID string `json:"target_user_id"`
	WrappedDEK   string `json:"wrapped_dek"`
}

// SubmitRekeysRequest is the request body for submitting re-keyed DEKs
type SubmitRekeysRequest struct {
	Rekeys []RekeyEntry `json:"rekeys"`
}
