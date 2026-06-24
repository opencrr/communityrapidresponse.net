package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// encryptionTestSuite holds shared test dependencies for encryption handler tests.
type encryptionTestSuite struct {
	handler    *EncryptionHandler
	keyMock    sqlmock.Sqlmock
	secretMock sqlmock.Sqlmock
	keyRepo    *database.EncryptionKeyRepository
	secretRepo *database.EncryptedSecretRepository
}

// setupEncryptionTestSuite creates an EncryptionHandler backed by sqlmock databases.
func setupEncryptionTestSuite(t *testing.T) *encryptionTestSuite {
	t.Helper()

	keyDB, keyMock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock for encryption keys: %v", err)
	}
	t.Cleanup(func() { _ = keyDB.Close() })

	secretDB, secretMock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock for encrypted secrets: %v", err)
	}
	t.Cleanup(func() { _ = secretDB.Close() })

	keyRepo := database.NewEncryptionKeyRepository(&database.DB{DB: keyDB})
	secretRepo := database.NewEncryptedSecretRepository(&database.DB{DB: secretDB})

	regionRepo := database.NewRegionRepository(&database.DB{DB: keyDB})
	schoolRepo := database.NewSchoolRepository(&database.DB{DB: keyDB})
	groupRepo := database.NewGroupRepository(&database.DB{DB: keyDB})
	signalGroupRepo := database.NewSignalGroupRepository(&database.DB{DB: keyDB})

	userRepo := database.NewUserRepository(&database.DB{DB: keyDB})
	handler := NewEncryptionHandler(keyRepo, secretRepo, regionRepo, schoolRepo, userRepo, groupRepo, signalGroupRepo)

	return &encryptionTestSuite{
		handler:    handler,
		keyMock:    keyMock,
		secretMock: secretMock,
		keyRepo:    keyRepo,
		secretRepo: secretRepo,
	}
}

// setupEncryptionTestSuiteNoSecretRepo creates an EncryptionHandler with a nil
// encryptedSecretRepo, simulating deployments where the secret repo is unavailable.
func setupEncryptionTestSuiteNoSecretRepo(t *testing.T) *encryptionTestSuite {
	t.Helper()

	keyDB, keyMock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock for encryption keys: %v", err)
	}
	t.Cleanup(func() { _ = keyDB.Close() })

	keyRepo := database.NewEncryptionKeyRepository(&database.DB{DB: keyDB})
	handler := NewEncryptionHandler(keyRepo, nil, nil, nil, nil, nil, nil)

	return &encryptionTestSuite{
		handler: handler,
		keyMock: keyMock,
		keyRepo: keyRepo,
	}
}

// authenticatedRequest creates an HTTP request with user claims injected into the context.
func authenticatedRequest(method, target string, body []byte, claims *middleware.Claims) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	req.Header.Set("Content-Type", "application/json")

	if claims != nil {
		ctx := middleware.ContextWithUser(req.Context(), claims)
		req = req.WithContext(ctx)
	}
	return req
}

// testClaims returns standard test user claims.
func testClaims() *middleware.Claims {
	return &middleware.Claims{
		UserID:           "user-123",
		Username:         "testuser",
		Email:            "test@example.com",
		VerificationTier: models.TierPostcard,
		IsSuperuser:      false,
		TokenType:        middleware.TokenTypeFull,
	}
}

// userGetByIDColumns is the list of columns returned by UserRepository.GetByID.
var userGetByIDColumns = []string{
	"id", "username", "email", "password_hash", "verification_tier",
	"postcard_verified", "vouch_verified", "is_superuser",
	"mfa_secret", "mfa_enabled", "mfa_backup_codes", "mfa_setup_required",
	"email_verified", "email_normalized",
	"is_blocked", "blocked_at", "blocked_by", "block_reason",
	"address_hash", "created_at", "last_login", "deleted_at",
	"failed_login_attempts", "locked_until", "failed_mfa_attempts",
}

// expectUserGetByID sets up a sqlmock expectation for UserRepository.GetByID
// returning a non-superuser.
func expectUserGetByID(mock sqlmock.Sqlmock, userID string, isSuperuser bool) {
	mock.ExpectQuery("SELECT id, username, email, password_hash").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows(userGetByIDColumns).AddRow(
			userID, "testuser", "test@example.com", "hash",
			int(models.TierPostcard), false, false, isSuperuser,
			nil, false, nil, false,
			true, nil,
			false, nil, nil, nil,
			nil, time.Now(), nil, nil,
			0, nil, 0,
		))
}

// parseResponseBody decodes the JSON response body into a map.
func parseResponseBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return result
}

// =============================================================================
// UploadKeys Tests
// =============================================================================

func TestEncryptionHandler_UploadKeys_Success(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	suite.keyMock.ExpectExec("INSERT INTO user_encryption_keys").
		WithArgs(
			"user-123",
			"test-public-key",
			"test-wrapped-private-key",
			"test-salt",
			"test-iv",
			sqlmock.AnyArg(), // created_at
			sqlmock.AnyArg(), // rotated_at
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	requestBody, _ := json.Marshal(models.CreateEncryptionKeyRequest{
		PublicKey:         "test-public-key",
		WrappedPrivateKey: "test-wrapped-private-key",
		KeySalt:           "test-salt",
		KeyIV:             "test-iv",
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/keys", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.UploadKeys(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if stored, ok := body["stored"].(bool); !ok || !stored {
		t.Errorf("expected stored=true, got %v", body["stored"])
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_UploadKeys_MissingAuth(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	requestBody, _ := json.Marshal(models.CreateEncryptionKeyRequest{
		PublicKey:         "test-public-key",
		WrappedPrivateKey: "test-wrapped-private-key",
		KeySalt:           "test-salt",
		KeyIV:             "test-iv",
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/keys", requestBody, nil)
	recorder := httptest.NewRecorder()

	suite.handler.UploadKeys(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "unauthorized" {
		t.Errorf("expected error 'unauthorized', got %v", body["error"])
	}
}

func TestEncryptionHandler_UploadKeys_InvalidJSON(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	invalidBody := []byte("{not valid json}")
	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/keys", invalidBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.UploadKeys(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "invalid_request" {
		t.Errorf("expected error 'invalid_request', got %v", body["error"])
	}
}

func TestEncryptionHandler_UploadKeys_MissingFields(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	testCases := []struct {
		name    string
		request models.CreateEncryptionKeyRequest
	}{
		{
			name: "missing public key",
			request: models.CreateEncryptionKeyRequest{
				PublicKey:         "",
				WrappedPrivateKey: "wrapped-key",
				KeySalt:           "salt",
				KeyIV:             "iv",
			},
		},
		{
			name: "missing wrapped private key",
			request: models.CreateEncryptionKeyRequest{
				PublicKey:         "pub-key",
				WrappedPrivateKey: "",
				KeySalt:           "salt",
				KeyIV:             "iv",
			},
		},
		{
			name: "missing key salt",
			request: models.CreateEncryptionKeyRequest{
				PublicKey:         "pub-key",
				WrappedPrivateKey: "wrapped-key",
				KeySalt:           "",
				KeyIV:             "iv",
			},
		},
		{
			name: "missing key iv",
			request: models.CreateEncryptionKeyRequest{
				PublicKey:         "pub-key",
				WrappedPrivateKey: "wrapped-key",
				KeySalt:           "salt",
				KeyIV:             "",
			},
		},
		{
			name: "all fields empty",
			request: models.CreateEncryptionKeyRequest{
				PublicKey:         "",
				WrappedPrivateKey: "",
				KeySalt:           "",
				KeyIV:             "",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			requestBody, _ := json.Marshal(testCase.request)
			req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/keys", requestBody, testClaims())
			recorder := httptest.NewRecorder()

			suite.handler.UploadKeys(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
			}

			body := parseResponseBody(t, recorder)
			if body["error"] != "validation_error" {
				t.Errorf("expected error 'validation_error', got %v", body["error"])
			}
		})
	}
}

// =============================================================================
// GetKeys Tests
// =============================================================================

func TestEncryptionHandler_GetKeys_Success(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	rotatedAt := time.Now().UTC().Truncate(time.Second)
	createdAt := rotatedAt.Add(-24 * time.Hour)

	columns := []string{"user_id", "public_key", "wrapped_private_key", "key_salt", "key_iv", "created_at", "rotated_at"}
	suite.keyMock.ExpectQuery("SELECT user_id, public_key, wrapped_private_key, key_salt, key_iv, created_at, rotated_at FROM user_encryption_keys").
		WithArgs("user-123").
		WillReturnRows(
			sqlmock.NewRows(columns).
				AddRow("user-123", "pub-key-data", "wrapped-priv-data", "salt-data", "iv-data", createdAt, rotatedAt),
		)

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/keys", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetKeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var responsePayload models.EncryptionKeyResponse
	if err := json.NewDecoder(recorder.Body).Decode(&responsePayload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if responsePayload.PublicKey != "pub-key-data" {
		t.Errorf("expected public_key 'pub-key-data', got %q", responsePayload.PublicKey)
	}
	if responsePayload.WrappedPrivateKey != "wrapped-priv-data" {
		t.Errorf("expected wrapped_private_key 'wrapped-priv-data', got %q", responsePayload.WrappedPrivateKey)
	}
	if responsePayload.KeySalt != "salt-data" {
		t.Errorf("expected key_salt 'salt-data', got %q", responsePayload.KeySalt)
	}
	if responsePayload.KeyIV != "iv-data" {
		t.Errorf("expected key_iv 'iv-data', got %q", responsePayload.KeyIV)
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_GetKeys_NotFound(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	columns := []string{"user_id", "public_key", "wrapped_private_key", "key_salt", "key_iv", "created_at", "rotated_at"}
	suite.keyMock.ExpectQuery("SELECT user_id, public_key, wrapped_private_key, key_salt, key_iv, created_at, rotated_at FROM user_encryption_keys").
		WithArgs("user-123").
		WillReturnRows(sqlmock.NewRows(columns)) // empty result set triggers sql.ErrNoRows

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/keys", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetKeys(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "not_found" {
		t.Errorf("expected error 'not_found', got %v", body["error"])
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_GetKeys_MissingAuth(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/keys", nil, nil)
	recorder := httptest.NewRecorder()

	suite.handler.GetKeys(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "unauthorized" {
		t.Errorf("expected error 'unauthorized', got %v", body["error"])
	}
}

// =============================================================================
// UpdateKeys Tests
// =============================================================================

func TestEncryptionHandler_UpdateKeys_Success(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	suite.keyMock.ExpectExec("UPDATE user_encryption_keys").
		WithArgs("new-wrapped-key", "new-salt", "new-iv", "user-123").
		WillReturnResult(sqlmock.NewResult(0, 1))

	requestBody, _ := json.Marshal(models.UpdateEncryptionKeyRequest{
		WrappedPrivateKey: "new-wrapped-key",
		KeySalt:           "new-salt",
		KeyIV:             "new-iv",
	})

	req := authenticatedRequest(http.MethodPut, "/api/v1/encryption/keys", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.UpdateKeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if updated, ok := body["updated"].(bool); !ok || !updated {
		t.Errorf("expected updated=true, got %v", body["updated"])
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_UpdateKeys_NotFound(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	suite.keyMock.ExpectExec("UPDATE user_encryption_keys").
		WithArgs("new-wrapped-key", "new-salt", "new-iv", "user-123").
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected

	requestBody, _ := json.Marshal(models.UpdateEncryptionKeyRequest{
		WrappedPrivateKey: "new-wrapped-key",
		KeySalt:           "new-salt",
		KeyIV:             "new-iv",
	})

	req := authenticatedRequest(http.MethodPut, "/api/v1/encryption/keys", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.UpdateKeys(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "not_found" {
		t.Errorf("expected error 'not_found', got %v", body["error"])
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_UpdateKeys_MissingFields(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	testCases := []struct {
		name    string
		request models.UpdateEncryptionKeyRequest
	}{
		{
			name: "missing wrapped private key",
			request: models.UpdateEncryptionKeyRequest{
				WrappedPrivateKey: "",
				KeySalt:           "salt",
				KeyIV:             "iv",
			},
		},
		{
			name: "missing key salt",
			request: models.UpdateEncryptionKeyRequest{
				WrappedPrivateKey: "wrapped-key",
				KeySalt:           "",
				KeyIV:             "iv",
			},
		},
		{
			name: "missing key iv",
			request: models.UpdateEncryptionKeyRequest{
				WrappedPrivateKey: "wrapped-key",
				KeySalt:           "salt",
				KeyIV:             "",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			requestBody, _ := json.Marshal(testCase.request)
			req := authenticatedRequest(http.MethodPut, "/api/v1/encryption/keys", requestBody, testClaims())
			recorder := httptest.NewRecorder()

			suite.handler.UpdateKeys(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
			}

			body := parseResponseBody(t, recorder)
			if body["error"] != "validation_error" {
				t.Errorf("expected error 'validation_error', got %v", body["error"])
			}
		})
	}
}

func TestEncryptionHandler_UpdateKeys_MissingAuth(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	requestBody, _ := json.Marshal(models.UpdateEncryptionKeyRequest{
		WrappedPrivateKey: "wrapped-key",
		KeySalt:           "salt",
		KeyIV:             "iv",
	})

	req := authenticatedRequest(http.MethodPut, "/api/v1/encryption/keys", requestBody, nil)
	recorder := httptest.NewRecorder()

	suite.handler.UpdateKeys(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

// =============================================================================
// RotateKeys Tests
// =============================================================================

func TestEncryptionHandler_RotateKeys_Success(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	// Expect the key creation (upsert)
	suite.keyMock.ExpectExec("INSERT INTO user_encryption_keys").
		WithArgs(
			"user-123",
			"rotated-public-key",
			"rotated-wrapped-private-key",
			"rotated-salt",
			"rotated-iv",
			sqlmock.AnyArg(), // created_at
			sqlmock.AnyArg(), // rotated_at
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Expect the FlagRekeyForUser call
	suite.secretMock.ExpectExec("UPDATE encrypted_secret_keys SET rekey_needed = TRUE").
		WithArgs("user-123").
		WillReturnResult(sqlmock.NewResult(0, 5))

	requestBody, _ := json.Marshal(models.RotateEncryptionKeyRequest{
		PublicKey:         "rotated-public-key",
		WrappedPrivateKey: "rotated-wrapped-private-key",
		KeySalt:           "rotated-salt",
		KeyIV:             "rotated-iv",
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/keys/rotate", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.RotateKeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if rotated, ok := body["rotated"].(bool); !ok || !rotated {
		t.Errorf("expected rotated=true, got %v", body["rotated"])
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet key mock expectations: %v", err)
	}
	if err := suite.secretMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet secret mock expectations: %v", err)
	}
}

func TestEncryptionHandler_RotateKeys_MissingFields(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	testCases := []struct {
		name    string
		request models.RotateEncryptionKeyRequest
	}{
		{
			name: "missing public key",
			request: models.RotateEncryptionKeyRequest{
				PublicKey:         "",
				WrappedPrivateKey: "wrapped-key",
				KeySalt:           "salt",
				KeyIV:             "iv",
			},
		},
		{
			name: "missing wrapped private key",
			request: models.RotateEncryptionKeyRequest{
				PublicKey:         "pub-key",
				WrappedPrivateKey: "",
				KeySalt:           "salt",
				KeyIV:             "iv",
			},
		},
		{
			name: "missing key salt",
			request: models.RotateEncryptionKeyRequest{
				PublicKey:         "pub-key",
				WrappedPrivateKey: "wrapped-key",
				KeySalt:           "",
				KeyIV:             "iv",
			},
		},
		{
			name: "missing key iv",
			request: models.RotateEncryptionKeyRequest{
				PublicKey:         "pub-key",
				WrappedPrivateKey: "wrapped-key",
				KeySalt:           "salt",
				KeyIV:             "",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			requestBody, _ := json.Marshal(testCase.request)
			req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/keys/rotate", requestBody, testClaims())
			recorder := httptest.NewRecorder()

			suite.handler.RotateKeys(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
			}

			body := parseResponseBody(t, recorder)
			if body["error"] != "validation_error" {
				t.Errorf("expected error 'validation_error', got %v", body["error"])
			}
		})
	}
}

func TestEncryptionHandler_RotateKeys_MissingAuth(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	requestBody, _ := json.Marshal(models.RotateEncryptionKeyRequest{
		PublicKey:         "pub-key",
		WrappedPrivateKey: "wrapped-key",
		KeySalt:           "salt",
		KeyIV:             "iv",
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/keys/rotate", requestBody, nil)
	recorder := httptest.NewRecorder()

	suite.handler.RotateKeys(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestEncryptionHandler_RotateKeys_FlagRekeyError_StillSucceeds(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	// Key creation succeeds
	suite.keyMock.ExpectExec("INSERT INTO user_encryption_keys").
		WithArgs(
			"user-123",
			"rotated-pub",
			"rotated-wrapped",
			"rotated-salt",
			"rotated-iv",
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// FlagRekeyForUser returns an error
	suite.secretMock.ExpectExec("UPDATE encrypted_secret_keys SET rekey_needed = TRUE").
		WithArgs("user-123").
		WillReturnError(context.DeadlineExceeded)

	requestBody, _ := json.Marshal(models.RotateEncryptionKeyRequest{
		PublicKey:         "rotated-pub",
		WrappedPrivateKey: "rotated-wrapped",
		KeySalt:           "rotated-salt",
		KeyIV:             "rotated-iv",
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/keys/rotate", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.RotateKeys(recorder, req)

	// Should still return 200 because the rotation itself succeeded
	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d (FlagRekeyForUser error should not fail the response)", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if rotated, ok := body["rotated"].(bool); !ok || !rotated {
		t.Errorf("expected rotated=true, got %v", body["rotated"])
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet key mock expectations: %v", err)
	}
	if err := suite.secretMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet secret mock expectations: %v", err)
	}
}

func TestEncryptionHandler_RotateKeys_NilSecretRepo_StillSucceeds(t *testing.T) {
	suite := setupEncryptionTestSuiteNoSecretRepo(t)

	suite.keyMock.ExpectExec("INSERT INTO user_encryption_keys").
		WithArgs(
			"user-123",
			"rotated-pub",
			"rotated-wrapped",
			"rotated-salt",
			"rotated-iv",
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	requestBody, _ := json.Marshal(models.RotateEncryptionKeyRequest{
		PublicKey:         "rotated-pub",
		WrappedPrivateKey: "rotated-wrapped",
		KeySalt:           "rotated-salt",
		KeyIV:             "rotated-iv",
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/keys/rotate", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.RotateKeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if rotated, ok := body["rotated"].(bool); !ok || !rotated {
		t.Errorf("expected rotated=true, got %v", body["rotated"])
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

// =============================================================================
// GetPendingRekeys Tests
// =============================================================================

func TestEncryptionHandler_GetPendingRekeys_Success(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	columns := []string{"secret_id", "target_user_id", "target_public_key", "caller_wrapped_dek", "group_id", "group_name", "connection_id"}
	suite.secretMock.ExpectQuery("SELECT DISTINCT esk_need.secret_id").
		WithArgs("user-123").
		WillReturnRows(
			sqlmock.NewRows(columns).
				AddRow("secret-1", "target-user-1", "target-pub-key-1", "caller-dek-1", "group-1", "Test Group 1", nil).
				AddRow("secret-2", "target-user-2", "target-pub-key-2", "caller-dek-2", "group-2", "Test Group 2", nil),
		)

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/pending-rekeys", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetPendingRekeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	pendingRekeys, ok := body["pending_rekeys"].([]interface{})
	if !ok {
		t.Fatalf("expected pending_rekeys to be an array, got %T", body["pending_rekeys"])
	}
	if len(pendingRekeys) != 2 {
		t.Errorf("expected 2 pending rekeys, got %d", len(pendingRekeys))
	}

	// Verify first entry
	firstEntry, ok := pendingRekeys[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first entry to be a map, got %T", pendingRekeys[0])
	}
	if firstEntry["secret_id"] != "secret-1" {
		t.Errorf("expected secret_id 'secret-1', got %v", firstEntry["secret_id"])
	}
	if firstEntry["target_user_id"] != "target-user-1" {
		t.Errorf("expected target_user_id 'target-user-1', got %v", firstEntry["target_user_id"])
	}

	if err := suite.secretMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_GetPendingRekeys_NoPending(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	columns := []string{"secret_id", "target_user_id", "target_public_key", "caller_wrapped_dek", "group_id", "group_name", "connection_id"}
	suite.secretMock.ExpectQuery("SELECT DISTINCT esk_need.secret_id").
		WithArgs("user-123").
		WillReturnRows(sqlmock.NewRows(columns)) // empty result

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/pending-rekeys", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetPendingRekeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	pendingRekeys, ok := body["pending_rekeys"].([]interface{})
	if !ok {
		t.Fatalf("expected pending_rekeys to be an array, got %T", body["pending_rekeys"])
	}
	if len(pendingRekeys) != 0 {
		t.Errorf("expected 0 pending rekeys, got %d", len(pendingRekeys))
	}

	if err := suite.secretMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_GetPendingRekeys_MissingAuth(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/pending-rekeys", nil, nil)
	recorder := httptest.NewRecorder()

	suite.handler.GetPendingRekeys(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestEncryptionHandler_GetPendingRekeys_NilSecretRepo(t *testing.T) {
	suite := setupEncryptionTestSuiteNoSecretRepo(t)

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/pending-rekeys", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetPendingRekeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	pendingRekeys, ok := body["pending_rekeys"].([]interface{})
	if !ok {
		t.Fatalf("expected pending_rekeys to be an array, got %T", body["pending_rekeys"])
	}
	if len(pendingRekeys) != 0 {
		t.Errorf("expected 0 pending rekeys for nil repo, got %d", len(pendingRekeys))
	}
}

// =============================================================================
// SubmitRekeys Tests
// =============================================================================

func TestEncryptionHandler_SubmitRekeys_Success(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	// The handler authorizes each entry against the caller's advertised
	// pending-rekey set (GetPendingRekeys), loaded once up front.
	suite.secretMock.ExpectQuery("SELECT DISTINCT esk_need.secret_id").
		WithArgs("user-123").
		WillReturnRows(sqlmock.NewRows([]string{"secret_id", "target_user_id", "target_public_key", "caller_wrapped_dek", "group_id", "group_name", "connection_id"}).
			AddRow("secret-1", "target-user-1", "pub-1", "caller-dek-1", "group-1", "Group 1", nil).
			AddRow("secret-2", "target-user-2", "pub-2", "caller-dek-2", "group-2", "Group 2", nil))

	suite.secretMock.ExpectExec("UPDATE encrypted_secret_keys").
		WithArgs("new-wrapped-dek-1", sqlmock.AnyArg(), "secret-1", "target-user-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	suite.secretMock.ExpectExec("UPDATE encrypted_secret_keys").
		WithArgs("new-wrapped-dek-2", sqlmock.AnyArg(), "secret-2", "target-user-2").
		WillReturnResult(sqlmock.NewResult(0, 1))

	requestBody, _ := json.Marshal(models.SubmitRekeysRequest{
		Rekeys: []models.RekeyEntry{
			{
				SecretID:     "secret-1",
				TargetUserID: "target-user-1",
				WrappedDEK:   "new-wrapped-dek-1",
			},
			{
				SecretID:     "secret-2",
				TargetUserID: "target-user-2",
				WrappedDEK:   "new-wrapped-dek-2",
			},
		},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/rekey", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.SubmitRekeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	rekeyedCount, ok := body["rekeyed"].(float64) // JSON numbers are float64
	if !ok {
		t.Fatalf("expected rekeyed to be a number, got %T", body["rekeyed"])
	}
	if int(rekeyedCount) != 2 {
		t.Errorf("expected rekeyed=2, got %v", rekeyedCount)
	}

	if err := suite.secretMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_SubmitRekeys_RejectsForgedTarget(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	// Caller is legitimately offered a rekey for (secret-1, victim) only.
	suite.secretMock.ExpectQuery("SELECT DISTINCT esk_need.secret_id").
		WithArgs("user-123").
		WillReturnRows(sqlmock.NewRows([]string{"secret_id", "target_user_id", "target_public_key", "caller_wrapped_dek", "group_id", "group_name", "connection_id"}).
			AddRow("secret-1", "victim", "pub-victim", "caller-dek-1", "group-1", "Group 1", nil))

	// Attacker submits a rekey for a DIFFERENT, unadvertised target for the same
	// secret. No UPDATE must be issued — the (secret, target) pair is not in the
	// caller's pending set, so the forged blob is rejected (key-injection guard).
	requestBody, _ := json.Marshal(models.SubmitRekeysRequest{
		Rekeys: []models.RekeyEntry{
			{SecretID: "secret-1", TargetUserID: "other-user", WrappedDEK: "forged-blob"},
		},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/rekey", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.SubmitRekeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	body := parseResponseBody(t, recorder)
	if c, _ := body["rekeyed"].(float64); int(c) != 0 {
		t.Errorf("expected rekeyed=0 (forged target rejected), got %v", c)
	}
	if err := suite.secretMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_SubmitRekeys_EmptyArray(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	requestBody, _ := json.Marshal(models.SubmitRekeysRequest{
		Rekeys: []models.RekeyEntry{},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/rekey", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.SubmitRekeys(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "validation_error" {
		t.Errorf("expected error 'validation_error', got %v", body["error"])
	}
}

func TestEncryptionHandler_SubmitRekeys_PartialFailures(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	// Pending set advertised to the caller: secret-1/target-1 and secret-3/target-3
	suite.secretMock.ExpectQuery("SELECT DISTINCT esk_need.secret_id").
		WithArgs("user-123").
		WillReturnRows(sqlmock.NewRows([]string{"secret_id", "target_user_id", "target_public_key", "caller_wrapped_dek", "group_id", "group_name", "connection_id"}).
			AddRow("secret-1", "target-1", "pub-1", "caller-dek-1", "group-1", "Group 1", nil).
			AddRow("secret-3", "target-3", "pub-3", "caller-dek-3", "group-3", "Group 3", nil))

	// First entry: in pending set, succeeds
	suite.secretMock.ExpectExec("UPDATE encrypted_secret_keys").
		WithArgs("dek-1", sqlmock.AnyArg(), "secret-1", "target-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Second entry: skipped (empty SecretID) — no UPDATE

	// Third entry: in pending set, but DB returns error on update
	suite.secretMock.ExpectExec("UPDATE encrypted_secret_keys").
		WithArgs("dek-3", sqlmock.AnyArg(), "secret-3", "target-3").
		WillReturnError(context.DeadlineExceeded)

	requestBody, _ := json.Marshal(models.SubmitRekeysRequest{
		Rekeys: []models.RekeyEntry{
			{
				SecretID:     "secret-1",
				TargetUserID: "target-1",
				WrappedDEK:   "dek-1",
			},
			{
				// Invalid entry: missing required fields — skipped without DB call
				SecretID:     "",
				TargetUserID: "target-2",
				WrappedDEK:   "dek-2",
			},
			{
				SecretID:     "secret-3",
				TargetUserID: "target-3",
				WrappedDEK:   "dek-3",
			},
		},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/rekey", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.SubmitRekeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	rekeyedCount, ok := body["rekeyed"].(float64)
	if !ok {
		t.Fatalf("expected rekeyed to be a number, got %T", body["rekeyed"])
	}
	// Only the first entry succeeded; second was skipped (missing field), third had a DB error
	if int(rekeyedCount) != 1 {
		t.Errorf("expected rekeyed=1 (1 success, 1 skipped, 1 error), got %v", rekeyedCount)
	}

	if err := suite.secretMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_SubmitRekeys_MissingAuth(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	requestBody, _ := json.Marshal(models.SubmitRekeysRequest{
		Rekeys: []models.RekeyEntry{
			{SecretID: "s1", TargetUserID: "u1", WrappedDEK: "d1"},
		},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/rekey", requestBody, nil)
	recorder := httptest.NewRecorder()

	suite.handler.SubmitRekeys(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestEncryptionHandler_SubmitRekeys_NilSecretRepo(t *testing.T) {
	suite := setupEncryptionTestSuiteNoSecretRepo(t)

	requestBody, _ := json.Marshal(models.SubmitRekeysRequest{
		Rekeys: []models.RekeyEntry{
			{SecretID: "s1", TargetUserID: "u1", WrappedDEK: "d1"},
		},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/rekey", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.SubmitRekeys(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "server_error" {
		t.Errorf("expected error 'server_error', got %v", body["error"])
	}
}

// =============================================================================
// GetPublicKeys Tests
// =============================================================================

func TestEncryptionHandler_GetPublicKeys_ByRegion(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	// Superuser check via isSuperuserFromDB
	expectUserGetByID(suite.keyMock, "user-123", false)

	// Membership check: IsUserInRegion (uses recursive CTE on keyMock since regionRepo shares keyDB)
	suite.keyMock.ExpectQuery("WITH RECURSIVE user_accessible_regions").
		WithArgs("user-123", "region-abc").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(true))

	columns := []string{"user_id", "public_key"}
	suite.keyMock.ExpectQuery("SELECT ek.user_id, ek.public_key FROM user_encryption_keys").
		WithArgs("region-abc").
		WillReturnRows(
			sqlmock.NewRows(columns).
				AddRow("user-1", "pub-key-1").
				AddRow("user-2", "pub-key-2"),
		)

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/public-keys?region_id=region-abc", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetPublicKeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	keys, ok := body["keys"].([]interface{})
	if !ok {
		t.Fatalf("expected keys to be an array, got %T", body["keys"])
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_GetPublicKeys_BySchool(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	// Superuser check via isSuperuserFromDB
	expectUserGetByID(suite.keyMock, "user-123", false)

	// Membership check: GetUserSchool (schoolRepo shares keyDB)
	suite.keyMock.ExpectQuery("SELECT id, user_id, school_id, is_admin, verification_status, verified_at FROM user_schools").
		WithArgs("user-123", "school-xyz").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}).
			AddRow("us-1", "user-123", "school-xyz", false, "verified", time.Now()))

	columns := []string{"user_id", "public_key"}
	suite.keyMock.ExpectQuery("SELECT ek.user_id, ek.public_key FROM user_encryption_keys").
		WithArgs("school-xyz").
		WillReturnRows(
			sqlmock.NewRows(columns).
				AddRow("user-3", "pub-key-3"),
		)

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/public-keys?school_id=school-xyz", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetPublicKeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	keys, ok := body["keys"].([]interface{})
	if !ok {
		t.Fatalf("expected keys to be an array, got %T", body["keys"])
	}
	if len(keys) != 1 {
		t.Errorf("expected 1 key, got %d", len(keys))
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_GetPublicKeys_ByDistrict(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	// Superuser check via isSuperuserFromDB
	expectUserGetByID(suite.keyMock, "user-123", false)

	// Membership check: ListByDistrict returns one school, then GetUserSchool confirms membership
	schoolColumns := []string{"id", "nces_id", "name", "city", "state", "district_id", "district_name", "latitude", "longitude", "member_count", "verified_count", "admin_count"}
	suite.keyMock.ExpectQuery("SELECT s.id, s.nces_id, s.name").
		WithArgs("district-99").
		WillReturnRows(sqlmock.NewRows(schoolColumns).
			AddRow("school-in-district", "123456", "Test School", "Anytown", "CA", "district-99", "Test District", 37.0, -122.0, 10, 5, 2))

	suite.keyMock.ExpectQuery("SELECT id, user_id, school_id, is_admin, verification_status, verified_at FROM user_schools").
		WithArgs("user-123", "school-in-district").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}).
			AddRow("us-2", "user-123", "school-in-district", false, "verified", time.Now()))

	columns := []string{"user_id", "public_key"}
	suite.keyMock.ExpectQuery("SELECT DISTINCT ek.user_id, ek.public_key FROM user_encryption_keys").
		WithArgs("district-99").
		WillReturnRows(
			sqlmock.NewRows(columns).
				AddRow("user-4", "pub-key-4").
				AddRow("user-5", "pub-key-5").
				AddRow("user-6", "pub-key-6"),
		)

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/public-keys?district_id=district-99", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetPublicKeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	keys, ok := body["keys"].([]interface{})
	if !ok {
		t.Fatalf("expected keys to be an array, got %T", body["keys"])
	}
	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_GetPublicKeys_NoScopeProvided(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/public-keys", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetPublicKeys(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "validation_error" {
		t.Errorf("expected error 'validation_error', got %v", body["error"])
	}
}

func TestEncryptionHandler_GetPublicKeys_MultipleScopes(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/public-keys?region_id=r1&school_id=s1", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetPublicKeys(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "validation_error" {
		t.Errorf("expected error 'validation_error', got %v", body["error"])
	}
}

func TestEncryptionHandler_GetPublicKeys_MissingAuth(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/public-keys?region_id=r1", nil, nil)
	recorder := httptest.NewRecorder()

	suite.handler.GetPublicKeys(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestEncryptionHandler_GetPublicKeys_RegionNotMember(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	// Superuser check via isSuperuserFromDB
	expectUserGetByID(suite.keyMock, "user-123", false)

	// IsUserInRegion returns false
	suite.keyMock.ExpectQuery("WITH RECURSIVE user_accessible_regions").
		WithArgs("user-123", "region-nope").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(false))

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/public-keys?region_id=region-nope", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetPublicKeys(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "forbidden" {
		t.Errorf("expected error 'forbidden', got %v", body["error"])
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_GetPublicKeys_SchoolNotMember(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	// Superuser check via isSuperuserFromDB
	expectUserGetByID(suite.keyMock, "user-123", false)

	// GetUserSchool returns no rows (not a member)
	suite.keyMock.ExpectQuery("SELECT id, user_id, school_id, is_admin, verification_status, verified_at FROM user_schools").
		WithArgs("user-123", "school-nope").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}))

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/public-keys?school_id=school-nope", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetPublicKeys(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "forbidden" {
		t.Errorf("expected error 'forbidden', got %v", body["error"])
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_GetPublicKeys_DistrictNotMember(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	// Superuser check via isSuperuserFromDB
	expectUserGetByID(suite.keyMock, "user-123", false)

	// ListByDistrict returns one school, but user is not a member of it
	schoolColumns := []string{"id", "nces_id", "name", "city", "state", "district_id", "district_name", "latitude", "longitude", "member_count", "verified_count", "admin_count"}
	suite.keyMock.ExpectQuery("SELECT s.id, s.nces_id, s.name").
		WithArgs("district-nope").
		WillReturnRows(sqlmock.NewRows(schoolColumns).
			AddRow("school-in-district", "654321", "Other School", "Nowhere", "TX", "district-nope", "Other District", 30.0, -97.0, 5, 2, 1))

	// GetUserSchool returns no rows for the school in the district
	suite.keyMock.ExpectQuery("SELECT id, user_id, school_id, is_admin, verification_status, verified_at FROM user_schools").
		WithArgs("user-123", "school-in-district").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}))

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/public-keys?district_id=district-nope", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetPublicKeys(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "forbidden" {
		t.Errorf("expected error 'forbidden', got %v", body["error"])
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_GetPublicKeys_SuperuserBypassesMembership(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	superuserClaims := &middleware.Claims{
		UserID:           "superuser-1",
		Username:         "superuser",
		Email:            "super@example.com",
		VerificationTier: models.TierPostcard,
		IsSuperuser:      true,
		TokenType:        middleware.TokenTypeFull,
	}

	// Superuser check via isSuperuserFromDB — returns true, so membership check is skipped
	expectUserGetByID(suite.keyMock, "superuser-1", true)

	columns := []string{"user_id", "public_key"}
	suite.keyMock.ExpectQuery("SELECT ek.user_id, ek.public_key FROM user_encryption_keys").
		WithArgs("region-abc").
		WillReturnRows(
			sqlmock.NewRows(columns).
				AddRow("user-1", "pub-key-1"),
		)

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/public-keys?region_id=region-abc", nil, superuserClaims)
	recorder := httptest.NewRecorder()

	suite.handler.GetPublicKeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	keys, ok := body["keys"].([]interface{})
	if !ok {
		t.Fatalf("expected keys to be an array, got %T", body["keys"])
	}
	if len(keys) != 1 {
		t.Errorf("expected 1 key, got %d", len(keys))
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

// =============================================================================
// Additional edge-case tests
// =============================================================================

func TestEncryptionHandler_UploadKeys_DBError(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	suite.keyMock.ExpectExec("INSERT INTO user_encryption_keys").
		WithArgs(
			"user-123",
			"pub-key",
			"wrapped-key",
			"salt",
			"iv",
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnError(context.DeadlineExceeded)

	requestBody, _ := json.Marshal(models.CreateEncryptionKeyRequest{
		PublicKey:         "pub-key",
		WrappedPrivateKey: "wrapped-key",
		KeySalt:           "salt",
		KeyIV:             "iv",
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/keys", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.UploadKeys(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_UpdateKeys_InvalidJSON(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	invalidBody := []byte("{not valid json}")
	req := authenticatedRequest(http.MethodPut, "/api/v1/encryption/keys", invalidBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.UpdateKeys(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "invalid_request" {
		t.Errorf("expected error 'invalid_request', got %v", body["error"])
	}
}

func TestEncryptionHandler_RotateKeys_InvalidJSON(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	invalidBody := []byte("{not valid json}")
	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/keys/rotate", invalidBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.RotateKeys(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "invalid_request" {
		t.Errorf("expected error 'invalid_request', got %v", body["error"])
	}
}

func TestEncryptionHandler_RotateKeys_DBError(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	suite.keyMock.ExpectExec("INSERT INTO user_encryption_keys").
		WithArgs(
			"user-123",
			"pub-key",
			"wrapped-key",
			"salt",
			"iv",
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnError(context.DeadlineExceeded)

	requestBody, _ := json.Marshal(models.RotateEncryptionKeyRequest{
		PublicKey:         "pub-key",
		WrappedPrivateKey: "wrapped-key",
		KeySalt:           "salt",
		KeyIV:             "iv",
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/keys/rotate", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.RotateKeys(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_GetKeys_DBError(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	suite.keyMock.ExpectQuery("SELECT user_id, public_key, wrapped_private_key, key_salt, key_iv, created_at, rotated_at FROM user_encryption_keys").
		WithArgs("user-123").
		WillReturnError(context.DeadlineExceeded)

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/keys", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetKeys(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_GetPendingRekeys_DBError(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	suite.secretMock.ExpectQuery("SELECT DISTINCT esk_need.secret_id").
		WithArgs("user-123").
		WillReturnError(context.DeadlineExceeded)

	req := authenticatedRequest(http.MethodGet, "/api/v1/encryption/pending-rekeys", nil, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetPendingRekeys(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}

	if err := suite.secretMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_SubmitRekeys_InvalidJSON(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	invalidBody := []byte("{not valid json}")
	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/rekey", invalidBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.SubmitRekeys(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "invalid_request" {
		t.Errorf("expected error 'invalid_request', got %v", body["error"])
	}
}

func TestEncryptionHandler_SubmitRekeys_RejectsPairNotInPendingSet(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	// GetPendingRekeys returns nothing — the (secret, target) pair was never
	// advertised to this caller, so it must be rejected (key-injection guard).
	suite.secretMock.ExpectQuery("SELECT DISTINCT esk_need.secret_id").
		WithArgs("user-123").
		WillReturnRows(sqlmock.NewRows([]string{"secret_id", "target_user_id", "target_public_key", "caller_wrapped_dek", "group_id", "group_name", "connection_id"})) // empty result

	requestBody, _ := json.Marshal(models.SubmitRekeysRequest{
		Rekeys: []models.RekeyEntry{
			{
				SecretID:     "secret-1",
				TargetUserID: "target-1",
				WrappedDEK:   "new-dek-1",
			},
		},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/rekey", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.SubmitRekeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	rekeyedCount, ok := body["rekeyed"].(float64)
	if !ok {
		t.Fatalf("expected rekeyed to be a number, got %T", body["rekeyed"])
	}
	if int(rekeyedCount) != 0 {
		t.Errorf("expected rekeyed=0 (pair not in pending set), got %v", rekeyedCount)
	}

	if err := suite.secretMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_UpdateKeys_DBError(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	suite.keyMock.ExpectExec("UPDATE user_encryption_keys").
		WithArgs("wrapped-key", "salt", "iv", "user-123").
		WillReturnError(context.DeadlineExceeded)

	requestBody, _ := json.Marshal(models.UpdateEncryptionKeyRequest{
		WrappedPrivateKey: "wrapped-key",
		KeySalt:           "salt",
		KeyIV:             "iv",
	})

	req := authenticatedRequest(http.MethodPut, "/api/v1/encryption/keys", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.UpdateKeys(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}

	if err := suite.keyMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestEncryptionHandler_SubmitRekeys_SkipsEntriesWithMissingFields(t *testing.T) {
	suite := setupEncryptionTestSuite(t)

	// The pending set is loaded once up front; every entry is then skipped for
	// missing required fields before any UPDATE.
	suite.secretMock.ExpectQuery("SELECT DISTINCT esk_need.secret_id").
		WithArgs("user-123").
		WillReturnRows(sqlmock.NewRows([]string{"secret_id", "target_user_id", "target_public_key", "caller_wrapped_dek", "group_id", "group_name", "connection_id"}))

	// All entries have missing required fields — none should hit an UPDATE
	requestBody, _ := json.Marshal(models.SubmitRekeysRequest{
		Rekeys: []models.RekeyEntry{
			{SecretID: "", TargetUserID: "u1", WrappedDEK: "d1"},
			{SecretID: "s2", TargetUserID: "", WrappedDEK: "d2"},
			{SecretID: "s3", TargetUserID: "u3", WrappedDEK: ""},
		},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encryption/rekey", requestBody, testClaims())
	recorder := httptest.NewRecorder()

	suite.handler.SubmitRekeys(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	rekeyedCount, ok := body["rekeyed"].(float64)
	if !ok {
		t.Fatalf("expected rekeyed to be a number, got %T", body["rekeyed"])
	}
	if int(rekeyedCount) != 0 {
		t.Errorf("expected rekeyed=0 (all entries had missing fields), got %v", rekeyedCount)
	}

	// No DB calls should have been made for the secret mock
	if err := suite.secretMock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}
