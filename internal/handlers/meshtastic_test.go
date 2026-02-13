package handlers

import (
	"database/sql"
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

// meshtasticTestSuite holds shared test dependencies for meshtastic handler tests.
type meshtasticTestSuite struct {
	handler *MeshtasticHandler
	mock    sqlmock.Sqlmock
}

// setupMeshtasticTestSuite creates a MeshtasticHandler backed by a single shared sqlmock.
func setupMeshtasticTestSuite(t *testing.T) *meshtasticTestSuite {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db := &database.DB{DB: sqlDB}
	channelRepo := database.NewMeshtasticChannelRepository(db)
	encryptedSecretRepo := database.NewEncryptedSecretRepository(db)
	regionRepo := database.NewRegionRepository(db)
	schoolRepo := database.NewSchoolRepository(db)
	auditRepo := database.NewAuditRepository(db)

	handler := NewMeshtasticHandler(db, channelRepo, encryptedSecretRepo, regionRepo, schoolRepo, auditRepo)

	return &meshtasticTestSuite{handler: handler, mock: mock}
}

// setupMeshtasticTestSuiteNoAudit creates a handler with nil auditRepo for testing without audit.
func setupMeshtasticTestSuiteNoAudit(t *testing.T) *meshtasticTestSuite {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db := &database.DB{DB: sqlDB}
	channelRepo := database.NewMeshtasticChannelRepository(db)
	encryptedSecretRepo := database.NewEncryptedSecretRepository(db)
	regionRepo := database.NewRegionRepository(db)
	schoolRepo := database.NewSchoolRepository(db)

	handler := NewMeshtasticHandler(db, channelRepo, encryptedSecretRepo, regionRepo, schoolRepo, nil)

	return &meshtasticTestSuite{handler: handler, mock: mock}
}

// adminClaims returns claims for a fully verified admin user.
func adminClaims() *middleware.Claims {
	return &middleware.Claims{
		UserID:           "admin-user-123",
		Username:         "adminuser",
		Email:            "admin@example.com",
		VerificationTier: models.TierPostcard,
		PostcardVerified: true,
		VouchVerified:    true,
		IsSuperuser:      false,
		TokenType:        middleware.TokenTypeFull,
	}
}

// superuserClaims returns claims for a superuser.
func superuserClaims() *middleware.Claims {
	return &middleware.Claims{
		UserID:           "superuser-456",
		Username:         "superuser",
		Email:            "super@example.com",
		VerificationTier: models.TierPostcard,
		PostcardVerified: true,
		VouchVerified:    true,
		IsSuperuser:      true,
		TokenType:        middleware.TokenTypeFull,
	}
}

// vouchedClaims returns claims for a vouch-verified user (read access but not full admin).
func vouchedClaims() *middleware.Claims {
	return &middleware.Claims{
		UserID:           "vouched-user-789",
		Username:         "voucheduser",
		Email:            "vouched@example.com",
		VerificationTier: models.TierVouched,
		PostcardVerified: false,
		VouchVerified:    true,
		IsSuperuser:      false,
		TokenType:        middleware.TokenTypeFull,
	}
}

// unverifiedClaims returns claims for an unverified user.
func unverifiedClaims() *middleware.Claims {
	return &middleware.Claims{
		UserID:           "unverified-user-000",
		Username:         "unverifieduser",
		Email:            "unverified@example.com",
		VerificationTier: models.TierUnverified,
		PostcardVerified: false,
		VouchVerified:    false,
		IsSuperuser:      false,
		TokenType:        middleware.TokenTypeFull,
	}
}

// postcardOnlyClaims returns claims for a postcard-verified-only user (no vouch).
func postcardOnlyClaims() *middleware.Claims {
	return &middleware.Claims{
		UserID:           "postcard-user-111",
		Username:         "postcardonly",
		Email:            "postcard@example.com",
		VerificationTier: models.TierPostcard,
		PostcardVerified: true,
		VouchVerified:    false,
		IsSuperuser:      false,
		TokenType:        middleware.TokenTypeFull,
	}
}

func strPtr(s string) *string {
	return &s
}

// =============================================================================
// Create Tests - Validation
// =============================================================================

func TestMeshtasticHandler_Create_NoAuth(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	body, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		RegionID:         strPtr("region-1"),
		Name:             "Test Channel",
		EncryptedPayload: "encrypted-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", body, nil)
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestMeshtasticHandler_Create_InvalidJSON(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", []byte("{bad json}"), adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	body := parseResponseBody(t, recorder)
	if body["error"] != "invalid_request" {
		t.Errorf("expected error 'invalid_request', got %v", body["error"])
	}
}

func TestMeshtasticHandler_Create_NoScope(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	body, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		Name:             "Test Channel",
		EncryptedPayload: "encrypted-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "validation_error" {
		t.Errorf("expected error 'validation_error', got %v", respBody["error"])
	}
}

func TestMeshtasticHandler_Create_MultipleScopes(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	body, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		RegionID:         strPtr("region-1"),
		SchoolID:         strPtr("school-1"),
		Name:             "Test Channel",
		EncryptedPayload: "encrypted-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "validation_error" {
		t.Errorf("expected error 'validation_error', got %v", respBody["error"])
	}
}

func TestMeshtasticHandler_Create_AllThreeScopes(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	body, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		RegionID:         strPtr("region-1"),
		SchoolID:         strPtr("school-1"),
		DistrictID:       strPtr("district-1"),
		Name:             "Test Channel",
		EncryptedPayload: "encrypted-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestMeshtasticHandler_Create_EmptyScopeStrings(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// Empty string IDs should not count as a valid scope
	body, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		RegionID:         strPtr(""),
		SchoolID:         strPtr(""),
		Name:             "Test Channel",
		EncryptedPayload: "encrypted-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestMeshtasticHandler_Create_MissingRequiredFields(t *testing.T) {
	testCases := []struct {
		name    string
		request models.CreateMeshtasticChannelRequest
	}{
		{
			name: "missing name",
			request: models.CreateMeshtasticChannelRequest{
				RegionID:         strPtr("region-1"),
				Name:             "",
				EncryptedPayload: "encrypted-data",
				EncryptionIV:     "iv-data",
				WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
			},
		},
		{
			name: "missing encrypted payload",
			request: models.CreateMeshtasticChannelRequest{
				RegionID:         strPtr("region-1"),
				Name:             "Test Channel",
				EncryptedPayload: "",
				EncryptionIV:     "iv-data",
				WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
			},
		},
		{
			name: "missing encryption IV",
			request: models.CreateMeshtasticChannelRequest{
				RegionID:         strPtr("region-1"),
				Name:             "Test Channel",
				EncryptedPayload: "encrypted-data",
				EncryptionIV:     "",
				WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
			},
		},
		{
			name: "missing wrapped keys",
			request: models.CreateMeshtasticChannelRequest{
				RegionID:         strPtr("region-1"),
				Name:             "Test Channel",
				EncryptedPayload: "encrypted-data",
				EncryptionIV:     "iv-data",
				WrappedKeys:      []models.WrappedKeyEntry{},
			},
		},
		{
			name: "nil wrapped keys",
			request: models.CreateMeshtasticChannelRequest{
				RegionID:         strPtr("region-1"),
				Name:             "Test Channel",
				EncryptedPayload: "encrypted-data",
				EncryptionIV:     "iv-data",
				WrappedKeys:      nil,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			suite := setupMeshtasticTestSuite(t)

			body, _ := json.Marshal(testCase.request)
			req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", body, adminClaims())
			recorder := httptest.NewRecorder()

			suite.handler.Create(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
			}

			respBody := parseResponseBody(t, recorder)
			if respBody["error"] != "validation_error" {
				t.Errorf("expected error 'validation_error', got %v", respBody["error"])
			}
		})
	}
}

// =============================================================================
// Create Tests - Region Scope
// =============================================================================

func TestMeshtasticHandler_Create_Region_Success(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// 1. IsUserAdmin check — returns true (userID, regionID)
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs("admin-user-123", "region-1").
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(true))

	// 2. GetFullAdminCount (bootstrap mode check) — 3 admins = not bootstrap
	suite.mock.ExpectQuery("COUNT.DISTINCT u.id").
		WithArgs("region-1", "region-1", "region-1", "region-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	// 3. Transaction: BEGIN
	suite.mock.ExpectBegin()

	// 4. CountByRegionForUpdate
	suite.mock.ExpectQuery("COUNT.*meshtastic_channels.*FOR UPDATE").
		WithArgs("region-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	// 5. CreateChannelTx — INSERT channel
	suite.mock.ExpectExec("INSERT INTO meshtastic_channels").
		WithArgs(
			sqlmock.AnyArg(), // id (uuid)
			strPtr("region-1"), sqlmock.AnyArg(), sqlmock.AnyArg(), // region_id, school_id, district_id
			"Test Channel",
			sqlmock.AnyArg(), // description
			sqlmock.AnyArg(), // created_by
			sqlmock.AnyArg(), // created_at
			true,             // is_active
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// 6. CreateTx — INSERT encrypted_secret
	suite.mock.ExpectExec("INSERT INTO encrypted_secrets").
		WithArgs(
			sqlmock.AnyArg(), // id
			"meshtastic_channel",
			sqlmock.AnyArg(), // signal_group_id
			sqlmock.AnyArg(), // meshtastic_channel_id
			"encrypted-data",
			"iv-data",
			"admin-user-123",
			sqlmock.AnyArg(), // updated_at
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// 7. CreateTx — INSERT encrypted_secret_keys
	suite.mock.ExpectExec("INSERT INTO encrypted_secret_keys").
		WithArgs(sqlmock.AnyArg(), "user-1", "dek-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// 8. COMMIT
	suite.mock.ExpectCommit()

	// 9. Audit log
	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(
			sqlmock.AnyArg(), // id
			sqlmock.AnyArg(), // user_id
			"meshtastic_channel_created",
			sqlmock.AnyArg(), // resource_type
			sqlmock.AnyArg(), // resource_id
			sqlmock.AnyArg(), // details
			sqlmock.AnyArg(), // ip_address
			sqlmock.AnyArg(), // user_agent
			sqlmock.AnyArg(), // created_at
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		RegionID:         strPtr("region-1"),
		Name:             "Test Channel",
		EncryptedPayload: "encrypted-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["region_id"] != "region-1" {
		t.Errorf("expected region_id 'region-1', got %v", respBody["region_id"])
	}
	if respBody["channel_name"] != "Test Channel" {
		t.Errorf("expected channel_name 'Test Channel', got %v", respBody["channel_name"])
	}
	if respBody["channel_id"] == nil || respBody["channel_id"] == "" {
		t.Error("expected non-empty channel_id")
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestMeshtasticHandler_Create_Region_Superuser(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// Superuser skips IsUserAdmin and bootstrap mode checks — goes straight to transaction
	suite.mock.ExpectBegin()

	suite.mock.ExpectQuery("COUNT.*meshtastic_channels.*FOR UPDATE").
		WithArgs("region-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	suite.mock.ExpectExec("INSERT INTO meshtastic_channels").
		WithArgs(
			sqlmock.AnyArg(), strPtr("region-1"), sqlmock.AnyArg(), sqlmock.AnyArg(),
			"Test Channel", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), true,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectExec("INSERT INTO encrypted_secrets").
		WithArgs(
			sqlmock.AnyArg(), "meshtastic_channel", sqlmock.AnyArg(), sqlmock.AnyArg(),
			"encrypted-data", "iv-data", "superuser-456", sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectExec("INSERT INTO encrypted_secret_keys").
		WithArgs(sqlmock.AnyArg(), "user-1", "dek-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectCommit()

	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "meshtastic_channel_created",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		RegionID:         strPtr("region-1"),
		Name:             "Test Channel",
		EncryptedPayload: "encrypted-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", body, superuserClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestMeshtasticHandler_Create_Region_NotAdmin(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// IsUserAdmin returns false (userID, regionID)
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs("admin-user-123", "region-1").
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(false))

	body, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		RegionID:         strPtr("region-1"),
		Name:             "Test Channel",
		EncryptedPayload: "encrypted-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "forbidden" {
		t.Errorf("expected error 'forbidden', got %v", respBody["error"])
	}
}

func TestMeshtasticHandler_Create_Region_BootstrapMode(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// IsUserAdmin returns true (userID, regionID)
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs("admin-user-123", "region-1").
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(true))

	// GetFullAdminCount → 2 admins = bootstrap mode
	suite.mock.ExpectQuery("COUNT.DISTINCT u.id").
		WithArgs("region-1", "region-1", "region-1", "region-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	body, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		RegionID:         strPtr("region-1"),
		Name:             "Test Channel",
		EncryptedPayload: "encrypted-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "region_in_bootstrap" {
		t.Errorf("expected error 'region_in_bootstrap', got %v", respBody["error"])
	}
	if respBody["bootstrap_mode"] != true {
		t.Errorf("expected bootstrap_mode=true, got %v", respBody["bootstrap_mode"])
	}
}

func TestMeshtasticHandler_Create_Region_LimitReached(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// Admin check passes (userID, regionID)
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs("admin-user-123", "region-1").
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(true))

	// Not in bootstrap
	suite.mock.ExpectQuery("COUNT.DISTINCT u.id").
		WithArgs("region-1", "region-1", "region-1", "region-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	// Transaction begins
	suite.mock.ExpectBegin()

	// CountByRegionForUpdate returns max (5)
	suite.mock.ExpectQuery("COUNT.*meshtastic_channels.*FOR UPDATE").
		WithArgs("region-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	// Transaction rolls back because of ErrLimitReached
	suite.mock.ExpectRollback()

	body, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		RegionID:         strPtr("region-1"),
		Name:             "Test Channel",
		EncryptedPayload: "encrypted-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d: %s", http.StatusConflict, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "limit_reached" {
		t.Errorf("expected error 'limit_reached', got %v", respBody["error"])
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

// =============================================================================
// Create Tests - School Scope
// =============================================================================

func TestMeshtasticHandler_Create_School_Success(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// GetUserSchool — user is admin of school
	verifiedAt := time.Now().UTC()
	suite.mock.ExpectQuery("SELECT.*FROM user_schools.*WHERE user_id").
		WithArgs("admin-user-123", "school-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}).
			AddRow("us-1", "admin-user-123", "school-1", true, "verified", verifiedAt))

	// Transaction
	suite.mock.ExpectBegin()

	suite.mock.ExpectQuery("COUNT.*meshtastic_channels.*school_id.*FOR UPDATE").
		WithArgs("school-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	suite.mock.ExpectExec("INSERT INTO meshtastic_channels").
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), strPtr("school-1"), sqlmock.AnyArg(),
			"School Channel", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), true,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectExec("INSERT INTO encrypted_secrets").
		WithArgs(sqlmock.AnyArg(), "meshtastic_channel", sqlmock.AnyArg(), sqlmock.AnyArg(),
			"enc-payload", "enc-iv", "admin-user-123", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectExec("INSERT INTO encrypted_secret_keys").
		WithArgs(sqlmock.AnyArg(), "user-1", "dek-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectCommit()

	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "meshtastic_channel_created",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	reqBody, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		SchoolID:         strPtr("school-1"),
		Name:             "School Channel",
		EncryptedPayload: "enc-payload",
		EncryptionIV:     "enc-iv",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", reqBody, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["school_id"] != "school-1" {
		t.Errorf("expected school_id 'school-1', got %v", respBody["school_id"])
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestMeshtasticHandler_Create_School_NotMember(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// GetUserSchool — user is not a member (returns no rows)
	suite.mock.ExpectQuery("SELECT.*FROM user_schools.*WHERE user_id").
		WithArgs("admin-user-123", "school-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}))

	reqBody, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		SchoolID:         strPtr("school-1"),
		Name:             "School Channel",
		EncryptedPayload: "enc-payload",
		EncryptionIV:     "enc-iv",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", reqBody, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}
}

func TestMeshtasticHandler_Create_School_MemberNotAdmin(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// GetUserSchool — user is member but not admin
	verifiedAt := time.Now().UTC()
	suite.mock.ExpectQuery("SELECT.*FROM user_schools.*WHERE user_id").
		WithArgs("admin-user-123", "school-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}).
			AddRow("us-1", "admin-user-123", "school-1", false, "verified", verifiedAt))

	reqBody, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		SchoolID:         strPtr("school-1"),
		Name:             "School Channel",
		EncryptedPayload: "enc-payload",
		EncryptionIV:     "enc-iv",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", reqBody, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "forbidden" {
		t.Errorf("expected error 'forbidden', got %v", respBody["error"])
	}
}

func TestMeshtasticHandler_Create_School_LimitReached(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// Admin check passes
	verifiedAt := time.Now().UTC()
	suite.mock.ExpectQuery("SELECT.*FROM user_schools.*WHERE user_id").
		WithArgs("admin-user-123", "school-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}).
			AddRow("us-1", "admin-user-123", "school-1", true, "verified", verifiedAt))

	suite.mock.ExpectBegin()

	suite.mock.ExpectQuery("COUNT.*meshtastic_channels.*school_id.*FOR UPDATE").
		WithArgs("school-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	suite.mock.ExpectRollback()

	reqBody, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		SchoolID:         strPtr("school-1"),
		Name:             "School Channel",
		EncryptedPayload: "enc-payload",
		EncryptionIV:     "enc-iv",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", reqBody, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d: %s", http.StatusConflict, recorder.Code, recorder.Body.String())
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

// =============================================================================
// Create Tests - District Scope
// =============================================================================

func TestMeshtasticHandler_Create_District_Success(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// ListByDistrict — returns one school in the district
	suite.mock.ExpectQuery("SELECT.*FROM schools.*WHERE.*district_id").
		WithArgs("district-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "nces_id", "name", "city", "state",
			"district_id", "district_name", "latitude", "longitude",
			"member_count", "verified_count", "admin_count",
		}).AddRow("school-1", "NCES001", "School One", "City", "ST",
			"district-1", "District One", 40.0, -74.0, 10, 5, 3))

	// GetUserSchool for that school — admin
	verifiedAt := time.Now().UTC()
	suite.mock.ExpectQuery("SELECT.*FROM user_schools.*WHERE user_id").
		WithArgs("admin-user-123", "school-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}).
			AddRow("us-1", "admin-user-123", "school-1", true, "verified", verifiedAt))

	// Transaction
	suite.mock.ExpectBegin()

	suite.mock.ExpectQuery("COUNT.*meshtastic_channels.*district_id.*FOR UPDATE").
		WithArgs("district-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	suite.mock.ExpectExec("INSERT INTO meshtastic_channels").
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), strPtr("district-1"),
			"District Channel", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), true,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectExec("INSERT INTO encrypted_secrets").
		WithArgs(sqlmock.AnyArg(), "meshtastic_channel", sqlmock.AnyArg(), sqlmock.AnyArg(),
			"enc-payload", "enc-iv", "admin-user-123", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectExec("INSERT INTO encrypted_secret_keys").
		WithArgs(sqlmock.AnyArg(), "user-1", "dek-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectCommit()

	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "meshtastic_channel_created",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	reqBody, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		DistrictID:       strPtr("district-1"),
		Name:             "District Channel",
		EncryptedPayload: "enc-payload",
		EncryptionIV:     "enc-iv",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", reqBody, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["district_id"] != "district-1" {
		t.Errorf("expected district_id 'district-1', got %v", respBody["district_id"])
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestMeshtasticHandler_Create_District_NotAdmin(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// ListByDistrict returns one school
	suite.mock.ExpectQuery("SELECT.*FROM schools.*WHERE.*district_id").
		WithArgs("district-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "nces_id", "name", "city", "state",
			"district_id", "district_name", "latitude", "longitude",
			"member_count", "verified_count", "admin_count",
		}).AddRow("school-1", "NCES001", "School One", "City", "ST",
			"district-1", "District One", 40.0, -74.0, 10, 5, 3))

	// GetUserSchool — not a member of any school in district
	suite.mock.ExpectQuery("SELECT.*FROM user_schools.*WHERE user_id").
		WithArgs("admin-user-123", "school-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}))

	reqBody, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		DistrictID:       strPtr("district-1"),
		Name:             "District Channel",
		EncryptedPayload: "enc-payload",
		EncryptionIV:     "enc-iv",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", reqBody, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}
}

func TestMeshtasticHandler_Create_District_EmptyDistrict(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// ListByDistrict returns empty (no schools in district)
	suite.mock.ExpectQuery("SELECT.*FROM schools.*WHERE.*district_id").
		WithArgs("district-empty").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "nces_id", "name", "city", "state",
			"district_id", "district_name", "latitude", "longitude",
			"member_count", "verified_count", "admin_count",
		}))

	reqBody, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		DistrictID:       strPtr("district-empty"),
		Name:             "District Channel",
		EncryptedPayload: "enc-payload",
		EncryptionIV:     "enc-iv",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", reqBody, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}
}

// =============================================================================
// Create Tests - No Audit Repo
// =============================================================================

func TestMeshtasticHandler_Create_NoAuditRepo(t *testing.T) {
	suite := setupMeshtasticTestSuiteNoAudit(t)

	// Superuser, no audit repo → should still succeed without audit log
	suite.mock.ExpectBegin()

	suite.mock.ExpectQuery("COUNT.*meshtastic_channels.*FOR UPDATE").
		WithArgs("region-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	suite.mock.ExpectExec("INSERT INTO meshtastic_channels").
		WithArgs(sqlmock.AnyArg(), strPtr("region-1"), sqlmock.AnyArg(), sqlmock.AnyArg(),
			"Test Channel", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), true).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectExec("INSERT INTO encrypted_secrets").
		WithArgs(sqlmock.AnyArg(), "meshtastic_channel", sqlmock.AnyArg(), sqlmock.AnyArg(),
			"enc-data", "iv-data", "superuser-456", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectExec("INSERT INTO encrypted_secret_keys").
		WithArgs(sqlmock.AnyArg(), "user-1", "dek-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectCommit()
	// No audit expectation since auditRepo is nil

	reqBody, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		RegionID:         strPtr("region-1"),
		Name:             "Test Channel",
		EncryptedPayload: "enc-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", reqBody, superuserClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

// =============================================================================
// List Tests
// =============================================================================

func TestMeshtasticHandler_List_NoAuth(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	req := authenticatedRequest(http.MethodGet, "/api/v1/meshtastic-channels", nil, nil)
	recorder := httptest.NewRecorder()

	suite.handler.List(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestMeshtasticHandler_List_Unverified(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	req := authenticatedRequest(http.MethodGet, "/api/v1/meshtastic-channels", nil, unverifiedClaims())
	recorder := httptest.NewRecorder()

	suite.handler.List(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "forbidden" {
		t.Errorf("expected error 'forbidden', got %v", respBody["error"])
	}
}

func TestMeshtasticHandler_List_ByRegion(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	createdAt := time.Now().UTC()
	regionID := "region-1"

	// ListByRegion query
	suite.mock.ExpectQuery("SELECT.*meshtastic_channels.*WHERE.*region_id").
		WithArgs("region-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "region_id", "school_id", "district_id",
			"channel_name", "description", "created_by",
			"created_at", "is_active", "has_pending_deletion", "region_name",
		}).AddRow("ch-1", &regionID, nil, nil,
			"Channel One", nil, "user-1",
			createdAt, true, false, "Region One"))

	// GetByMeshtasticChannelID
	suite.mock.ExpectQuery("SELECT.*FROM encrypted_secrets.*WHERE meshtastic_channel_id").
		WithArgs("ch-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "secret_type", "signal_group_id", "meshtastic_channel_id",
			"encrypted_payload", "encryption_iv", "updated_by", "updated_at",
		}).AddRow("secret-1", "meshtastic_channel", nil, "ch-1",
			"enc-payload", "iv-data", "user-1", createdAt))

	// GetWrappedDEK
	suite.mock.ExpectQuery("SELECT wrapped_dek FROM encrypted_secret_keys").
		WithArgs("secret-1", "vouched-user-789").
		WillReturnRows(sqlmock.NewRows([]string{"wrapped_dek"}).AddRow("user-dek-1"))

	req := authenticatedRequest(http.MethodGet, "/api/v1/meshtastic-channels?region_id=region-1", nil, vouchedClaims())
	recorder := httptest.NewRecorder()

	suite.handler.List(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var listResp models.MeshtasticChannelListResponse
	if err := json.NewDecoder(recorder.Body).Decode(&listResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(listResp.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(listResp.Channels))
	}

	ch := listResp.Channels[0]
	if ch.Name != "Channel One" {
		t.Errorf("expected name 'Channel One', got %q", ch.Name)
	}
	if ch.EncryptedSecret == nil {
		t.Error("expected encrypted_secret to be present")
	} else {
		if ch.EncryptedSecret.WrappedDEK != "user-dek-1" {
			t.Errorf("expected wrapped_dek 'user-dek-1', got %q", ch.EncryptedSecret.WrappedDEK)
		}
		if ch.EncryptedSecret.EncryptedPayload != "enc-payload" {
			t.Errorf("expected encrypted_payload 'enc-payload', got %q", ch.EncryptedSecret.EncryptedPayload)
		}
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestMeshtasticHandler_List_BySchool(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	createdAt := time.Now().UTC()
	schoolID := "school-1"

	// ListBySchool query
	suite.mock.ExpectQuery("SELECT.*meshtastic_channels.*WHERE.*school_id").
		WithArgs("school-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "region_id", "school_id", "district_id",
			"channel_name", "description", "created_by",
			"created_at", "is_active", "has_pending_deletion",
		}).AddRow("ch-2", nil, &schoolID, nil,
			"School Channel", nil, "user-1",
			createdAt, true, false))

	// GetByMeshtasticChannelID — not found (no secret yet)
	suite.mock.ExpectQuery("SELECT.*FROM encrypted_secrets.*WHERE meshtastic_channel_id").
		WithArgs("ch-2").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "secret_type", "signal_group_id", "meshtastic_channel_id",
			"encrypted_payload", "encryption_iv", "updated_by", "updated_at",
		}))

	req := authenticatedRequest(http.MethodGet, "/api/v1/meshtastic-channels?school_id=school-1", nil, vouchedClaims())
	recorder := httptest.NewRecorder()

	suite.handler.List(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var listResp models.MeshtasticChannelListResponse
	if err := json.NewDecoder(recorder.Body).Decode(&listResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(listResp.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(listResp.Channels))
	}

	ch := listResp.Channels[0]
	if ch.EncryptedSecret != nil {
		t.Error("expected encrypted_secret to be nil when no secret exists")
	}
}

func TestMeshtasticHandler_List_AllUserChannels(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// ListByUser (3-way UNION query) — returns empty
	suite.mock.ExpectQuery("SELECT.*meshtastic_channels.*UNION").
		WithArgs("vouched-user-789", "vouched-user-789", "vouched-user-789").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "region_id", "school_id", "district_id",
			"channel_name", "description", "created_by",
			"created_at", "is_active", "has_pending_deletion",
			"region_name", "school_name", "district_name",
		}))

	req := authenticatedRequest(http.MethodGet, "/api/v1/meshtastic-channels", nil, vouchedClaims())
	recorder := httptest.NewRecorder()

	suite.handler.List(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var listResp models.MeshtasticChannelListResponse
	if err := json.NewDecoder(recorder.Body).Decode(&listResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(listResp.Channels) != 0 {
		t.Errorf("expected 0 channels, got %d", len(listResp.Channels))
	}
}

func TestMeshtasticHandler_List_DBError(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// ListByRegion fails
	suite.mock.ExpectQuery("SELECT.*meshtastic_channels.*WHERE.*region_id").
		WithArgs("region-1").
		WillReturnError(sql.ErrConnDone)

	req := authenticatedRequest(http.MethodGet, "/api/v1/meshtastic-channels?region_id=region-1", nil, vouchedClaims())
	recorder := httptest.NewRecorder()

	suite.handler.List(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d: %s", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}
}

// =============================================================================
// ListAdmin Tests
// =============================================================================

func TestMeshtasticHandler_ListAdmin_NoAuth(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	req := authenticatedRequest(http.MethodGet, "/api/v1/meshtastic-channels/admin", nil, nil)
	recorder := httptest.NewRecorder()

	suite.handler.ListAdmin(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestMeshtasticHandler_ListAdmin_PostcardOnly(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	req := authenticatedRequest(http.MethodGet, "/api/v1/meshtastic-channels/admin", nil, postcardOnlyClaims())
	recorder := httptest.NewRecorder()

	suite.handler.ListAdmin(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}
}

func TestMeshtasticHandler_ListAdmin_VouchOnly(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	req := authenticatedRequest(http.MethodGet, "/api/v1/meshtastic-channels/admin", nil, vouchedClaims())
	recorder := httptest.NewRecorder()

	suite.handler.ListAdmin(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}
}

func TestMeshtasticHandler_ListAdmin_Success(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	createdAt := time.Now().UTC()
	regionID := "region-1"

	// ListByAdminUser (recursive CTE + 3-way UNION)
	suite.mock.ExpectQuery("user_admin_regions.*UNION").
		WithArgs("admin-user-123", "admin-user-123", "admin-user-123").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "region_id", "school_id", "district_id",
			"region_name", "school_name", "district_name",
			"channel_name", "description",
			"created_by", "created_at", "is_active", "has_pending_deletion",
		}).AddRow("ch-1", &regionID, nil, nil,
			"Region One", "", "",
			"Admin Channel", nil,
			"admin-user-123", createdAt, true, false))

	// GetByMeshtasticChannelID
	suite.mock.ExpectQuery("SELECT.*FROM encrypted_secrets.*WHERE meshtastic_channel_id").
		WithArgs("ch-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "secret_type", "signal_group_id", "meshtastic_channel_id",
			"encrypted_payload", "encryption_iv", "updated_by", "updated_at",
		}).AddRow("secret-1", "meshtastic_channel", nil, "ch-1",
			"enc-payload", "iv-data", "admin-user-123", createdAt))

	// GetWrappedDEK
	suite.mock.ExpectQuery("SELECT wrapped_dek FROM encrypted_secret_keys").
		WithArgs("secret-1", "admin-user-123").
		WillReturnRows(sqlmock.NewRows([]string{"wrapped_dek"}).AddRow("admin-dek-1"))

	req := authenticatedRequest(http.MethodGet, "/api/v1/meshtastic-channels/admin", nil, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.ListAdmin(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var listResp models.MeshtasticChannelListResponse
	if err := json.NewDecoder(recorder.Body).Decode(&listResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(listResp.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(listResp.Channels))
	}

	if listResp.Channels[0].RegionName != "Region One" {
		t.Errorf("expected region_name 'Region One', got %q", listResp.Channels[0].RegionName)
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestMeshtasticHandler_ListAdmin_DBError(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	suite.mock.ExpectQuery("user_admin_regions.*UNION").
		WithArgs("admin-user-123", "admin-user-123", "admin-user-123").
		WillReturnError(sql.ErrConnDone)

	req := authenticatedRequest(http.MethodGet, "/api/v1/meshtastic-channels/admin", nil, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.ListAdmin(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}
}

// =============================================================================
// Update Tests
// =============================================================================

func TestMeshtasticHandler_Update_NoAuth(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	body, _ := json.Marshal(models.UpdateMeshtasticChannelRequest{Name: strPtr("New Name")})
	req := authenticatedRequest(http.MethodPut, "/api/v1/meshtastic-channels?id=ch-1", body, nil)
	recorder := httptest.NewRecorder()

	suite.handler.Update(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestMeshtasticHandler_Update_MissingID(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	body, _ := json.Marshal(models.UpdateMeshtasticChannelRequest{Name: strPtr("New Name")})
	// No id query param
	req := authenticatedRequest(http.MethodPut, "/api/v1/meshtastic-channels", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Update(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestMeshtasticHandler_Update_InvalidJSON(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	req := authenticatedRequest(http.MethodPut, "/api/v1/meshtastic-channels?id=ch-1", []byte("{bad}"), adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Update(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestMeshtasticHandler_Update_ChannelNotFound(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// GetByID returns no rows
	suite.mock.ExpectQuery("SELECT.*FROM meshtastic_channels.*WHERE id").
		WithArgs("nonexistent").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "region_id", "school_id", "district_id",
			"channel_name", "description", "created_by", "created_at", "is_active",
		}))

	body, _ := json.Marshal(models.UpdateMeshtasticChannelRequest{Name: strPtr("New Name")})
	req := authenticatedRequest(http.MethodPut, "/api/v1/meshtastic-channels?id=nonexistent", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Update(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d: %s", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestMeshtasticHandler_Update_RegionChannel_Success(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	regionID := "region-1"
	createdAt := time.Now().UTC()

	// GetByID returns channel
	suite.mock.ExpectQuery("SELECT.*FROM meshtastic_channels.*WHERE id").
		WithArgs("ch-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "region_id", "school_id", "district_id",
			"channel_name", "description", "created_by", "created_at", "is_active",
		}).AddRow("ch-1", &regionID, nil, nil, "Old Name", nil, "user-1", createdAt, true))

	// IsUserAdmin (userID, regionID)
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs("admin-user-123", "region-1").
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(true))

	// Update query
	newName := "New Name"
	suite.mock.ExpectExec("UPDATE meshtastic_channels SET").
		WithArgs(newName, "ch-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Audit log
	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "meshtastic_channel_updated",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body, _ := json.Marshal(models.UpdateMeshtasticChannelRequest{Name: &newName})
	req := authenticatedRequest(http.MethodPut, "/api/v1/meshtastic-channels?id=ch-1", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Update(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["channel_name"] != "New Name" {
		t.Errorf("expected channel_name 'New Name', got %v", respBody["channel_name"])
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestMeshtasticHandler_Update_RegionChannel_NotAdmin(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	regionID := "region-1"
	createdAt := time.Now().UTC()

	// GetByID returns channel
	suite.mock.ExpectQuery("SELECT.*FROM meshtastic_channels.*WHERE id").
		WithArgs("ch-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "region_id", "school_id", "district_id",
			"channel_name", "description", "created_by", "created_at", "is_active",
		}).AddRow("ch-1", &regionID, nil, nil, "Old Name", nil, "user-1", createdAt, true))

	// IsUserAdmin returns false (userID, regionID)
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs("admin-user-123", "region-1").
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(false))

	body, _ := json.Marshal(models.UpdateMeshtasticChannelRequest{Name: strPtr("New Name")})
	req := authenticatedRequest(http.MethodPut, "/api/v1/meshtastic-channels?id=ch-1", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Update(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}
}

func TestMeshtasticHandler_Update_SchoolChannel_Success(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	schoolID := "school-1"
	createdAt := time.Now().UTC()
	verifiedAt := time.Now().UTC()

	// GetByID returns school-scoped channel
	suite.mock.ExpectQuery("SELECT.*FROM meshtastic_channels.*WHERE id").
		WithArgs("ch-2").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "region_id", "school_id", "district_id",
			"channel_name", "description", "created_by", "created_at", "is_active",
		}).AddRow("ch-2", nil, &schoolID, nil, "School Channel", nil, "user-1", createdAt, true))

	// GetUserSchool — admin
	suite.mock.ExpectQuery("SELECT.*FROM user_schools.*WHERE user_id").
		WithArgs("admin-user-123", "school-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}).
			AddRow("us-1", "admin-user-123", "school-1", true, "verified", verifiedAt))

	// Update
	newName := "Updated School Channel"
	suite.mock.ExpectExec("UPDATE meshtastic_channels SET").
		WithArgs(newName, "ch-2").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Audit
	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "meshtastic_channel_updated",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body, _ := json.Marshal(models.UpdateMeshtasticChannelRequest{Name: &newName})
	req := authenticatedRequest(http.MethodPut, "/api/v1/meshtastic-channels?id=ch-2", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Update(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestMeshtasticHandler_Update_SchoolChannel_NotAdmin(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	schoolID := "school-1"
	createdAt := time.Now().UTC()
	verifiedAt := time.Now().UTC()

	// GetByID
	suite.mock.ExpectQuery("SELECT.*FROM meshtastic_channels.*WHERE id").
		WithArgs("ch-2").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "region_id", "school_id", "district_id",
			"channel_name", "description", "created_by", "created_at", "is_active",
		}).AddRow("ch-2", nil, &schoolID, nil, "School Channel", nil, "user-1", createdAt, true))

	// GetUserSchool — member but not admin
	suite.mock.ExpectQuery("SELECT.*FROM user_schools.*WHERE user_id").
		WithArgs("admin-user-123", "school-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}).
			AddRow("us-1", "admin-user-123", "school-1", false, "verified", verifiedAt))

	body, _ := json.Marshal(models.UpdateMeshtasticChannelRequest{Name: strPtr("New Name")})
	req := authenticatedRequest(http.MethodPut, "/api/v1/meshtastic-channels?id=ch-2", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Update(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}
}

func TestMeshtasticHandler_Update_SchoolChannel_NotMember(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	schoolID := "school-1"
	createdAt := time.Now().UTC()

	// GetByID
	suite.mock.ExpectQuery("SELECT.*FROM meshtastic_channels.*WHERE id").
		WithArgs("ch-2").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "region_id", "school_id", "district_id",
			"channel_name", "description", "created_by", "created_at", "is_active",
		}).AddRow("ch-2", nil, &schoolID, nil, "School Channel", nil, "user-1", createdAt, true))

	// GetUserSchool — no rows (not a member)
	suite.mock.ExpectQuery("SELECT.*FROM user_schools.*WHERE user_id").
		WithArgs("admin-user-123", "school-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}))

	body, _ := json.Marshal(models.UpdateMeshtasticChannelRequest{Name: strPtr("New Name")})
	req := authenticatedRequest(http.MethodPut, "/api/v1/meshtastic-channels?id=ch-2", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Update(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}
}

func TestMeshtasticHandler_Update_Superuser(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	regionID := "region-1"
	createdAt := time.Now().UTC()

	// GetByID
	suite.mock.ExpectQuery("SELECT.*FROM meshtastic_channels.*WHERE id").
		WithArgs("ch-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "region_id", "school_id", "district_id",
			"channel_name", "description", "created_by", "created_at", "is_active",
		}).AddRow("ch-1", &regionID, nil, nil, "Old Name", nil, "user-1", createdAt, true))

	// Superuser skips admin checks — goes straight to Update
	newName := "Superuser Rename"
	suite.mock.ExpectExec("UPDATE meshtastic_channels SET").
		WithArgs(newName, "ch-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Audit
	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "meshtastic_channel_updated",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body, _ := json.Marshal(models.UpdateMeshtasticChannelRequest{Name: &newName})
	req := authenticatedRequest(http.MethodPut, "/api/v1/meshtastic-channels?id=ch-1", body, superuserClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Update(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestMeshtasticHandler_Update_DistrictChannel_NotAdmin(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	districtID := "district-1"
	createdAt := time.Now().UTC()

	// GetByID — district-scoped channel
	suite.mock.ExpectQuery("SELECT.*FROM meshtastic_channels.*WHERE id").
		WithArgs("ch-3").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "region_id", "school_id", "district_id",
			"channel_name", "description", "created_by", "created_at", "is_active",
		}).AddRow("ch-3", nil, nil, &districtID, "District Channel", nil, "user-1", createdAt, true))

	// ListByDistrict
	suite.mock.ExpectQuery("SELECT.*FROM schools.*WHERE.*district_id").
		WithArgs("district-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "nces_id", "name", "city", "state",
			"district_id", "district_name", "latitude", "longitude",
			"member_count", "verified_count", "admin_count",
		}).AddRow("school-1", "NCES001", "School One", "City", "ST",
			"district-1", "District One", 40.0, -74.0, 10, 5, 3))

	// GetUserSchool — not a member
	suite.mock.ExpectQuery("SELECT.*FROM user_schools.*WHERE user_id").
		WithArgs("admin-user-123", "school-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}))

	body, _ := json.Marshal(models.UpdateMeshtasticChannelRequest{Name: strPtr("New Name")})
	req := authenticatedRequest(http.MethodPut, "/api/v1/meshtastic-channels?id=ch-3", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Update(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}
}

func TestMeshtasticHandler_Update_GetByID_DBError(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// GetByID returns a DB error (not ErrNoRows)
	suite.mock.ExpectQuery("SELECT.*FROM meshtastic_channels.*WHERE id").
		WithArgs("ch-1").
		WillReturnError(sql.ErrConnDone)

	body, _ := json.Marshal(models.UpdateMeshtasticChannelRequest{Name: strPtr("New Name")})
	req := authenticatedRequest(http.MethodPut, "/api/v1/meshtastic-channels?id=ch-1", body, adminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Update(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d: %s", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}
}

// =============================================================================
// Create Tests - Multiple Wrapped Keys
// =============================================================================

func TestMeshtasticHandler_Create_MultipleWrappedKeys(t *testing.T) {
	suite := setupMeshtasticTestSuite(t)

	// Superuser shortcut
	suite.mock.ExpectBegin()

	suite.mock.ExpectQuery("COUNT.*meshtastic_channels.*FOR UPDATE").
		WithArgs("region-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	suite.mock.ExpectExec("INSERT INTO meshtastic_channels").
		WithArgs(sqlmock.AnyArg(), strPtr("region-1"), sqlmock.AnyArg(), sqlmock.AnyArg(),
			"Multi-Key Channel", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), true).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectExec("INSERT INTO encrypted_secrets").
		WithArgs(sqlmock.AnyArg(), "meshtastic_channel", sqlmock.AnyArg(), sqlmock.AnyArg(),
			"enc-data", "iv-data", "superuser-456", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Three wrapped keys
	suite.mock.ExpectExec("INSERT INTO encrypted_secret_keys").
		WithArgs(sqlmock.AnyArg(), "user-1", "dek-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	suite.mock.ExpectExec("INSERT INTO encrypted_secret_keys").
		WithArgs(sqlmock.AnyArg(), "user-2", "dek-2", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	suite.mock.ExpectExec("INSERT INTO encrypted_secret_keys").
		WithArgs(sqlmock.AnyArg(), "user-3", "dek-3", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectCommit()

	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "meshtastic_channel_created",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	reqBody, _ := json.Marshal(models.CreateMeshtasticChannelRequest{
		RegionID:         strPtr("region-1"),
		Name:             "Multi-Key Channel",
		EncryptedPayload: "enc-data",
		EncryptionIV:     "iv-data",
		WrappedKeys: []models.WrappedKeyEntry{
			{UserID: "user-1", WrappedDEK: "dek-1"},
			{UserID: "user-2", WrappedDEK: "dek-2"},
			{UserID: "user-3", WrappedDEK: "dek-3"},
		},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/meshtastic-channels", reqBody, superuserClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Create(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}
