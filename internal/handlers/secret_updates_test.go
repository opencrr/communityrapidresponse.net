package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// secretUpdateTestSuite holds shared test dependencies for secret update handler tests.
type secretUpdateTestSuite struct {
	handler *SecretUpdateHandler
	mock    sqlmock.Sqlmock
}

// setupSecretUpdateTestSuite creates a SecretUpdateHandler backed by a single shared sqlmock.
func setupSecretUpdateTestSuite(t *testing.T) *secretUpdateTestSuite {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db := &database.DB{DB: sqlDB}
	proposalRepo := database.NewSecretUpdateProposalRepository(db)
	secretRepo := database.NewEncryptedSecretRepository(db)
	encryptionKeyRepo := database.NewEncryptionKeyRepository(db)
	regionRepo := database.NewRegionRepository(db)
	schoolRepo := database.NewSchoolRepository(db)
	auditRepo := database.NewAuditRepository(db)

	consensusConfig := &config.ConsensusConfig{
		VotePercent: 50,
		VoteFloor:   3,
	}

	signalGroupRepo := database.NewSignalGroupRepository(db)
	meshtasticRepo := database.NewMeshtasticChannelRepository(db)

	handler := NewSecretUpdateHandler(db, proposalRepo, secretRepo, encryptionKeyRepo, regionRepo, schoolRepo, signalGroupRepo, meshtasticRepo, auditRepo, consensusConfig)

	return &secretUpdateTestSuite{handler: handler, mock: mock}
}

// setupSecretUpdateTestSuiteNoAudit creates a handler with nil auditRepo.
func setupSecretUpdateTestSuiteNoAudit(t *testing.T) *secretUpdateTestSuite {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db := &database.DB{DB: sqlDB}
	proposalRepo := database.NewSecretUpdateProposalRepository(db)
	secretRepo := database.NewEncryptedSecretRepository(db)
	encryptionKeyRepo := database.NewEncryptionKeyRepository(db)
	regionRepo := database.NewRegionRepository(db)
	schoolRepo := database.NewSchoolRepository(db)
	signalGroupRepo := database.NewSignalGroupRepository(db)
	meshtasticRepo := database.NewMeshtasticChannelRepository(db)

	consensusConfig := &config.ConsensusConfig{
		VotePercent: 50,
		VoteFloor:   3,
	}

	handler := NewSecretUpdateHandler(db, proposalRepo, secretRepo, encryptionKeyRepo, regionRepo, schoolRepo, signalGroupRepo, meshtasticRepo, nil, consensusConfig)

	return &secretUpdateTestSuite{handler: handler, mock: mock}
}

// regionAdminClaims returns claims for a region admin user.
func regionAdminClaims() *middleware.Claims {
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

// secondAdminClaims returns claims for a second admin user (for voting tests).
func secondAdminClaims() *middleware.Claims {
	return &middleware.Claims{
		UserID:           "admin-user-456",
		Username:         "adminuser2",
		Email:            "admin2@example.com",
		VerificationTier: models.TierPostcard,
		PostcardVerified: true,
		VouchVerified:    true,
		IsSuperuser:      false,
		TokenType:        middleware.TokenTypeFull,
	}
}

// =============================================================================
// CreateProposal Tests
// =============================================================================

func TestSecretUpdateHandler_CreateProposal_NoAuth(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	body, _ := json.Marshal(models.CreateSecretProposalRequest{
		EncryptedPayload: "enc-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/signal-groups/secret-proposals?id=group-1", body, nil)
	recorder := httptest.NewRecorder()

	suite.handler.CreateProposal(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestSecretUpdateHandler_CreateProposal_MissingGroupID(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	body, _ := json.Marshal(models.CreateSecretProposalRequest{
		EncryptedPayload: "enc-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/signal-groups/secret-proposals", body, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.CreateProposal(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "missing_parameter" {
		t.Errorf("expected error 'missing_parameter', got %v", respBody["error"])
	}
}

func TestSecretUpdateHandler_CreateProposal_InvalidJSON(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	req := authenticatedRequest(http.MethodPost, "/api/v1/signal-groups/secret-proposals?id=group-1", []byte("{bad}"), regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.CreateProposal(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "invalid_request" {
		t.Errorf("expected error 'invalid_request', got %v", respBody["error"])
	}
}

func TestSecretUpdateHandler_CreateProposal_MissingFields(t *testing.T) {
	testCases := []struct {
		name    string
		request models.CreateSecretProposalRequest
	}{
		{
			name: "missing encrypted payload",
			request: models.CreateSecretProposalRequest{
				EncryptedPayload: "",
				EncryptionIV:     "iv-data",
				WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
			},
		},
		{
			name: "missing encryption IV",
			request: models.CreateSecretProposalRequest{
				EncryptedPayload: "enc-data",
				EncryptionIV:     "",
				WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
			},
		},
		{
			name: "empty wrapped keys",
			request: models.CreateSecretProposalRequest{
				EncryptedPayload: "enc-data",
				EncryptionIV:     "iv-data",
				WrappedKeys:      []models.WrappedKeyEntry{},
			},
		},
		{
			name: "nil wrapped keys",
			request: models.CreateSecretProposalRequest{
				EncryptedPayload: "enc-data",
				EncryptionIV:     "iv-data",
				WrappedKeys:      nil,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			suite := setupSecretUpdateTestSuite(t)

			body, _ := json.Marshal(testCase.request)
			req := authenticatedRequest(http.MethodPost, "/api/v1/signal-groups/secret-proposals?id=group-1", body, regionAdminClaims())
			recorder := httptest.NewRecorder()

			suite.handler.CreateProposal(recorder, req)

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

func TestSecretUpdateHandler_CreateProposal_SecretNotFound(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	// GetBySignalGroupID returns no rows
	suite.mock.ExpectQuery("SELECT.*FROM encrypted_secrets.*WHERE signal_group_id").
		WithArgs("group-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "secret_type", "signal_group_id", "meshtastic_channel_id",
			"encrypted_payload", "encryption_iv", "updated_by", "updated_at",
		}))

	body, _ := json.Marshal(models.CreateSecretProposalRequest{
		EncryptedPayload: "enc-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/signal-groups/secret-proposals?id=group-1", body, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.CreateProposal(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d: %s", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "not_found" {
		t.Errorf("expected error 'not_found', got %v", respBody["error"])
	}
}

func TestSecretUpdateHandler_CreateProposal_Region_NotAdmin(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	groupID := "group-1"

	// GetBySignalGroupID returns a secret
	suite.mock.ExpectQuery("SELECT.*FROM encrypted_secrets.*WHERE signal_group_id").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "secret_type", "signal_group_id", "meshtastic_channel_id",
			"encrypted_payload", "encryption_iv", "updated_by", "updated_at",
		}).AddRow("secret-1", "signal_invite", &groupID, nil, "old-enc", "old-iv", "user-1", createdAt))

	// resolveGroupScope: GetByID on signal_groups
	regionID := "region-1"
	suite.mock.ExpectQuery("SELECT id, region_id, school_id, district_id, group_name.*FROM signal_groups").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "region_id", "school_id", "district_id", "group_name", "description", "created_by", "created_at", "is_active"}).
			AddRow(groupID, &regionID, nil, nil, "Test Group", nil, "user-1", createdAt, true))

	// verifyRegionAdmin: IsUserAdmin returns false
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs("admin-user-123", regionID).
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(false))

	body, _ := json.Marshal(models.CreateSecretProposalRequest{
		EncryptedPayload: "enc-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/signal-groups/secret-proposals?id="+groupID, body, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.CreateProposal(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}
}

func TestSecretUpdateHandler_CreateProposal_InsufficientAdmins(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	groupID := "group-1"
	regionID := "region-1"

	// GetBySignalGroupID
	suite.mock.ExpectQuery("SELECT.*FROM encrypted_secrets.*WHERE signal_group_id").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "secret_type", "signal_group_id", "meshtastic_channel_id",
			"encrypted_payload", "encryption_iv", "updated_by", "updated_at",
		}).AddRow("secret-1", "signal_invite", &groupID, nil, "old-enc", "old-iv", "user-1", createdAt))

	// resolveGroupScope: GetByID on signal_groups
	suite.mock.ExpectQuery("SELECT id, region_id, school_id, district_id, group_name.*FROM signal_groups").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "region_id", "school_id", "district_id", "group_name", "description", "created_by", "created_at", "is_active"}).
			AddRow(groupID, &regionID, nil, nil, "Test Group", nil, "user-1", createdAt, true))

	// IsUserAdmin returns true
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs("admin-user-123", regionID).
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(true))

	// GetAdminCount returns 2 (below VoteFloor of 3)
	suite.mock.ExpectQuery("COUNT").
		WithArgs(regionID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	body, _ := json.Marshal(models.CreateSecretProposalRequest{
		EncryptedPayload: "enc-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/signal-groups/secret-proposals?id="+groupID, body, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.CreateProposal(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "insufficient_admins" {
		t.Errorf("expected error 'insufficient_admins', got %v", respBody["error"])
	}
}

func TestSecretUpdateHandler_CreateProposal_PendingProposalExists(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	groupID := "group-1"
	regionID := "region-1"

	// GetBySignalGroupID
	suite.mock.ExpectQuery("SELECT.*FROM encrypted_secrets.*WHERE signal_group_id").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "secret_type", "signal_group_id", "meshtastic_channel_id",
			"encrypted_payload", "encryption_iv", "updated_by", "updated_at",
		}).AddRow("secret-1", "signal_invite", &groupID, nil, "old-enc", "old-iv", "user-1", createdAt))

	// resolveGroupScope: GetByID on signal_groups
	suite.mock.ExpectQuery("SELECT id, region_id, school_id, district_id, group_name.*FROM signal_groups").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "region_id", "school_id", "district_id", "group_name", "description", "created_by", "created_at", "is_active"}).
			AddRow(groupID, &regionID, nil, nil, "Test Group", nil, "user-1", createdAt, true))

	// IsUserAdmin
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs("admin-user-123", regionID).
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(true))

	// GetAdminCount returns 3 (meets VoteFloor)
	suite.mock.ExpectQuery("COUNT").
		WithArgs(regionID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	// Transaction begins
	suite.mock.ExpectBegin()

	// GetPendingBySecretForUpdate returns an existing pending proposal
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE encrypted_secret_id.*FOR UPDATE").
		WithArgs("secret-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("existing-prop", "secret-1", &regionID, nil, nil,
			"other-user", "other-enc", "other-iv", nil,
			"pending", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	// Transaction rolls back due to ErrPendingExists
	suite.mock.ExpectRollback()

	body, _ := json.Marshal(models.CreateSecretProposalRequest{
		EncryptedPayload: "enc-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/signal-groups/secret-proposals?id="+groupID, body, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.CreateProposal(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d: %s", http.StatusConflict, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "pending_proposal_exists" {
		t.Errorf("expected error 'pending_proposal_exists', got %v", respBody["error"])
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestSecretUpdateHandler_CreateProposal_Region_Success(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	groupID := "group-1"
	regionID := "region-1"

	// GetBySignalGroupID
	suite.mock.ExpectQuery("SELECT.*FROM encrypted_secrets.*WHERE signal_group_id").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "secret_type", "signal_group_id", "meshtastic_channel_id",
			"encrypted_payload", "encryption_iv", "updated_by", "updated_at",
		}).AddRow("secret-1", "signal_invite", &groupID, nil, "old-enc", "old-iv", "user-1", createdAt))

	// resolveGroupScope: GetByID on signal_groups
	suite.mock.ExpectQuery("SELECT id, region_id, school_id, district_id, group_name.*FROM signal_groups").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "region_id", "school_id", "district_id", "group_name", "description", "created_by", "created_at", "is_active"}).
			AddRow(groupID, &regionID, nil, nil, "Test Group", nil, "user-1", createdAt, true))

	// IsUserAdmin
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs("admin-user-123", regionID).
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(true))

	// GetAdminCount returns 3
	suite.mock.ExpectQuery("COUNT").
		WithArgs(regionID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	// Transaction
	suite.mock.ExpectBegin()

	// GetPendingBySecretForUpdate — no existing pending proposal
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE encrypted_secret_id.*FOR UPDATE").
		WithArgs("secret-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}))

	// CreateTx — INSERT proposal
	suite.mock.ExpectExec("INSERT INTO secret_update_proposals").
		WithArgs(
			sqlmock.AnyArg(), // id
			"secret-1",       // encrypted_secret_id
			sqlmock.AnyArg(), // region_id
			sqlmock.AnyArg(), // school_id
			sqlmock.AnyArg(), // district_id
			"admin-user-123", // proposed_by
			"enc-data",       // encrypted_payload
			"enc-iv",         // encryption_iv
			sqlmock.AnyArg(), // reason
			"pending",        // status
			sqlmock.AnyArg(), // created_at
			sqlmock.AnyArg(), // expires_at
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// CreateTx — INSERT proposal_wrapped_keys
	suite.mock.ExpectExec("INSERT INTO proposal_wrapped_keys").
		WithArgs(sqlmock.AnyArg(), "user-1", "dek-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// AddVoteTx — auto-vote for proposer
	suite.mock.ExpectExec("INSERT INTO secret_update_votes").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "admin-user-123", true, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectCommit()

	// Audit log
	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), models.AuditActionProposalCreated,
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body, _ := json.Marshal(models.CreateSecretProposalRequest{
		EncryptedPayload: "enc-data",
		EncryptionIV:     "enc-iv",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/signal-groups/secret-proposals?id="+groupID, body, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.CreateProposal(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response models.SecretProposalResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.EncryptedSecretID != "secret-1" {
		t.Errorf("expected encrypted_secret_id 'secret-1', got %q", response.EncryptedSecretID)
	}
	if response.Status != "pending" {
		t.Errorf("expected status 'pending', got %q", response.Status)
	}
	if response.CurrentVotes != 1 {
		t.Errorf("expected current_votes=1 (auto-vote), got %d", response.CurrentVotes)
	}
	if response.VotesNeeded != 3 {
		t.Errorf("expected votes_needed=3, got %d", response.VotesNeeded)
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestSecretUpdateHandler_CreateProposal_Superuser_Success(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	groupID := "group-1"
	regionID := "region-1"
	suClaims := superuserClaims()

	// GetBySignalGroupID
	suite.mock.ExpectQuery("SELECT.*FROM encrypted_secrets.*WHERE signal_group_id").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "secret_type", "signal_group_id", "meshtastic_channel_id",
			"encrypted_payload", "encryption_iv", "updated_by", "updated_at",
		}).AddRow("secret-1", "signal_invite", &groupID, nil, "old-enc", "old-iv", "user-1", createdAt))

	// resolveGroupScope: GetByID on signal_groups
	suite.mock.ExpectQuery("SELECT id, region_id, school_id, district_id, group_name.*FROM signal_groups").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "region_id", "school_id", "district_id", "group_name", "description", "created_by", "created_at", "is_active"}).
			AddRow(groupID, &regionID, nil, nil, "Test Group", nil, "user-1", createdAt, true))

	// Superuser skips IsUserAdmin — goes directly to GetAdminCount
	suite.mock.ExpectQuery("COUNT").
		WithArgs(regionID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	// Transaction
	suite.mock.ExpectBegin()

	// No pending
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE encrypted_secret_id.*FOR UPDATE").
		WithArgs("secret-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}))

	// CreateTx
	suite.mock.ExpectExec("INSERT INTO secret_update_proposals").
		WithArgs(
			sqlmock.AnyArg(), "secret-1",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			suClaims.UserID, "enc-data", "enc-iv",
			sqlmock.AnyArg(), "pending",
			sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectExec("INSERT INTO proposal_wrapped_keys").
		WithArgs(sqlmock.AnyArg(), "user-1", "dek-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Auto-vote
	suite.mock.ExpectExec("INSERT INTO secret_update_votes").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), suClaims.UserID, true, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectCommit()

	// Audit
	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), models.AuditActionProposalCreated,
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body, _ := json.Marshal(models.CreateSecretProposalRequest{
		EncryptedPayload: "enc-data",
		EncryptionIV:     "enc-iv",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/signal-groups/secret-proposals?id="+groupID, body, suClaims)
	recorder := httptest.NewRecorder()

	suite.handler.CreateProposal(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestSecretUpdateHandler_CreateProposal_GroupNotFound(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	groupID := "group-1"

	// GetBySignalGroupID returns a secret
	suite.mock.ExpectQuery("SELECT.*FROM encrypted_secrets.*WHERE signal_group_id").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "secret_type", "signal_group_id", "meshtastic_channel_id",
			"encrypted_payload", "encryption_iv", "updated_by", "updated_at",
		}).AddRow("secret-1", "signal_invite", &groupID, nil, "old-enc", "old-iv", "user-1", createdAt))

	// resolveGroupScope — signal group not found
	suite.mock.ExpectQuery("SELECT id, region_id, school_id, district_id, group_name.*FROM signal_groups").
		WithArgs(groupID).
		WillReturnError(sql.ErrNoRows)

	body, _ := json.Marshal(models.CreateSecretProposalRequest{
		EncryptedPayload: "enc-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/signal-groups/secret-proposals?id="+groupID, body, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.CreateProposal(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d: %s", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestSecretUpdateHandler_CreateProposal_NoAuditRepo(t *testing.T) {
	suite := setupSecretUpdateTestSuiteNoAudit(t)

	createdAt := time.Now().UTC()
	groupID := "group-1"
	regionID := "region-1"
	suClaims := superuserClaims()

	// GetBySignalGroupID
	suite.mock.ExpectQuery("SELECT.*FROM encrypted_secrets.*WHERE signal_group_id").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "secret_type", "signal_group_id", "meshtastic_channel_id",
			"encrypted_payload", "encryption_iv", "updated_by", "updated_at",
		}).AddRow("secret-1", "signal_invite", &groupID, nil, "old-enc", "old-iv", "user-1", createdAt))

	// resolveGroupScope: GetByID on signal_groups
	suite.mock.ExpectQuery("SELECT id, region_id, school_id, district_id, group_name.*FROM signal_groups").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "region_id", "school_id", "district_id", "group_name", "description", "created_by", "created_at", "is_active"}).
			AddRow(groupID, &regionID, nil, nil, "Test Group", nil, "user-1", createdAt, true))

	// Superuser skips admin check
	suite.mock.ExpectQuery("COUNT").
		WithArgs(regionID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	suite.mock.ExpectBegin()

	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE encrypted_secret_id.*FOR UPDATE").
		WithArgs("secret-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}))

	suite.mock.ExpectExec("INSERT INTO secret_update_proposals").
		WithArgs(
			sqlmock.AnyArg(), "secret-1",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			suClaims.UserID, "enc-data", "enc-iv",
			sqlmock.AnyArg(), "pending",
			sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectExec("INSERT INTO proposal_wrapped_keys").
		WithArgs(sqlmock.AnyArg(), "user-1", "dek-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectExec("INSERT INTO secret_update_votes").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), suClaims.UserID, true, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	suite.mock.ExpectCommit()
	// No audit expectation since auditRepo is nil

	body, _ := json.Marshal(models.CreateSecretProposalRequest{
		EncryptedPayload: "enc-data",
		EncryptionIV:     "enc-iv",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/signal-groups/secret-proposals?id="+groupID, body, suClaims)
	recorder := httptest.NewRecorder()

	suite.handler.CreateProposal(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

// =============================================================================
// Vote Tests
// =============================================================================

func TestSecretUpdateHandler_Vote_NoAuth(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	body, _ := json.Marshal(models.VoteRequest{Vote: true})
	req := authenticatedRequest(http.MethodPost, "/api/v1/secret-proposals/vote?id=prop-1", body, nil)
	recorder := httptest.NewRecorder()

	suite.handler.Vote(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestSecretUpdateHandler_Vote_MissingProposalID(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	body, _ := json.Marshal(models.VoteRequest{Vote: true})
	req := authenticatedRequest(http.MethodPost, "/api/v1/secret-proposals/vote", body, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Vote(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestSecretUpdateHandler_Vote_ProposalNotFound(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	// GetByID returns no rows
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id").
		WithArgs("prop-nonexistent").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}))

	body, _ := json.Marshal(models.VoteRequest{Vote: true})
	req := authenticatedRequest(http.MethodPost, "/api/v1/secret-proposals/vote?id=prop-nonexistent", body, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Vote(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d: %s", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestSecretUpdateHandler_Vote_AlreadyVoted(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	regionID := "region-1"
	claims := secondAdminClaims()

	// GetByID returns a pending proposal
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			"admin-user-123", "enc-data", "enc-iv", nil,
			"pending", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	// verifyAdminAccess: IsUserAdmin
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs(claims.UserID, regionID).
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(true))

	// GetAdminCount
	suite.mock.ExpectQuery("COUNT").
		WithArgs(regionID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	// Transaction
	suite.mock.ExpectBegin()

	// GetByIDForUpdate
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id.*FOR UPDATE").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			"admin-user-123", "enc-data", "enc-iv", nil,
			"pending", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	// HasVotedTx — already voted
	suite.mock.ExpectQuery("SELECT COUNT.*FROM secret_update_votes.*WHERE proposal_id.*AND voter_id").
		WithArgs("prop-1", claims.UserID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	suite.mock.ExpectRollback()

	body, _ := json.Marshal(models.VoteRequest{Vote: true})
	req := authenticatedRequest(http.MethodPost, "/api/v1/secret-proposals/vote?id=prop-1", body, claims)
	recorder := httptest.NewRecorder()

	suite.handler.Vote(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d: %s", http.StatusConflict, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "already_voted" {
		t.Errorf("expected error 'already_voted', got %v", respBody["error"])
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestSecretUpdateHandler_Vote_ProposalClosed(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	regionID := "region-1"
	claims := secondAdminClaims()

	// GetByID — pending proposal (initial check passes)
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			"admin-user-123", "enc-data", "enc-iv", nil,
			"pending", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	// verifyAdminAccess
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs(claims.UserID, regionID).
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(true))

	suite.mock.ExpectQuery("COUNT").
		WithArgs(regionID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	// Transaction
	suite.mock.ExpectBegin()

	// GetByIDForUpdate — proposal is now expired (race condition)
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id.*FOR UPDATE").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			"admin-user-123", "enc-data", "enc-iv", nil,
			"expired", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	suite.mock.ExpectRollback()

	body, _ := json.Marshal(models.VoteRequest{Vote: true})
	req := authenticatedRequest(http.MethodPost, "/api/v1/secret-proposals/vote?id=prop-1", body, claims)
	recorder := httptest.NewRecorder()

	suite.handler.Vote(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d: %s", http.StatusConflict, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "proposal_closed" {
		t.Errorf("expected error 'proposal_closed', got %v", respBody["error"])
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestSecretUpdateHandler_Vote_Success_NoConsensus(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	regionID := "region-1"
	claims := secondAdminClaims()

	// GetByID
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			"admin-user-123", "enc-data", "enc-iv", nil,
			"pending", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	// verifyAdminAccess
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs(claims.UserID, regionID).
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(true))

	suite.mock.ExpectQuery("COUNT").
		WithArgs(regionID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	// Transaction
	suite.mock.ExpectBegin()

	// GetByIDForUpdate
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id.*FOR UPDATE").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			"admin-user-123", "enc-data", "enc-iv", nil,
			"pending", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	// HasVotedTx — not voted yet
	suite.mock.ExpectQuery("SELECT COUNT.*FROM secret_update_votes.*WHERE proposal_id.*AND voter_id").
		WithArgs("prop-1", claims.UserID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	// AddVoteTx
	suite.mock.ExpectExec("INSERT INTO secret_update_votes").
		WithArgs(sqlmock.AnyArg(), "prop-1", claims.UserID, true, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// CountApprovalVotesTx — returns 2 (below threshold of 3)
	suite.mock.ExpectQuery("SELECT COUNT.*FROM secret_update_votes.*WHERE proposal_id.*AND vote = TRUE").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	suite.mock.ExpectCommit()

	// Audit
	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), models.AuditActionProposalVoted,
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body, _ := json.Marshal(models.VoteRequest{Vote: true})
	req := authenticatedRequest(http.MethodPost, "/api/v1/secret-proposals/vote?id=prop-1", body, claims)
	recorder := httptest.NewRecorder()

	suite.handler.Vote(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response models.SecretProposalResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.ConsensusReached {
		t.Error("expected consensus_reached=false")
	}
	if response.CurrentVotes != 2 {
		t.Errorf("expected current_votes=2, got %d", response.CurrentVotes)
	}
	if response.Status != string(models.ProposalStatusPending) {
		t.Errorf("expected status 'pending', got %q", response.Status)
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestSecretUpdateHandler_Vote_Success_ConsensusReached(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	regionID := "region-1"
	claims := secondAdminClaims()

	// GetByID
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			"admin-user-123", "enc-data", "enc-iv", nil,
			"pending", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	// verifyAdminAccess
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs(claims.UserID, regionID).
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(true))

	suite.mock.ExpectQuery("COUNT").
		WithArgs(regionID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	// Transaction
	suite.mock.ExpectBegin()

	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id.*FOR UPDATE").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			"admin-user-123", "enc-data", "enc-iv", nil,
			"pending", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	suite.mock.ExpectQuery("SELECT COUNT.*FROM secret_update_votes.*WHERE proposal_id.*AND voter_id").
		WithArgs("prop-1", claims.UserID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	suite.mock.ExpectExec("INSERT INTO secret_update_votes").
		WithArgs(sqlmock.AnyArg(), "prop-1", claims.UserID, true, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// CountApprovalVotesTx — returns 3 (meets threshold)
	suite.mock.ExpectQuery("SELECT COUNT.*FROM secret_update_votes.*WHERE proposal_id.*AND vote = TRUE").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	// UpdateStatusTx — set to approved_pending_finalization (no resolved_at for intermediate status)
	suite.mock.ExpectExec("UPDATE secret_update_proposals SET status").
		WithArgs(string(models.ProposalStatusApprovedPendingFinalization), "prop-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	suite.mock.ExpectCommit()

	// Audit: voted
	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), models.AuditActionProposalVoted,
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Audit: approved
	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), models.AuditActionProposalApproved,
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body, _ := json.Marshal(models.VoteRequest{Vote: true})
	req := authenticatedRequest(http.MethodPost, "/api/v1/secret-proposals/vote?id=prop-1", body, claims)
	recorder := httptest.NewRecorder()

	suite.handler.Vote(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response models.SecretProposalResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.ConsensusReached {
		t.Error("expected consensus_reached=true")
	}
	if response.Status != string(models.ProposalStatusApprovedPendingFinalization) {
		t.Errorf("expected status 'approved_pending_finalization', got %q", response.Status)
	}
	if response.CurrentVotes != 3 {
		t.Errorf("expected current_votes=3, got %d", response.CurrentVotes)
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

// =============================================================================
// Finalize Tests
// =============================================================================

func TestSecretUpdateHandler_Finalize_NoAuth(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	body, _ := json.Marshal(models.FinalizeSecretUpdateRequest{
		EncryptedPayload: "enc-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encrypted-secrets/finalize?id=secret-1", body, nil)
	recorder := httptest.NewRecorder()

	suite.handler.Finalize(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestSecretUpdateHandler_Finalize_MissingSecretID(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	body, _ := json.Marshal(models.FinalizeSecretUpdateRequest{
		EncryptedPayload: "enc-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encrypted-secrets/finalize", body, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Finalize(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestSecretUpdateHandler_Finalize_InvalidJSON(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	req := authenticatedRequest(http.MethodPost, "/api/v1/encrypted-secrets/finalize?id=secret-1", []byte("{bad}"), regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.Finalize(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestSecretUpdateHandler_Finalize_MissingFields(t *testing.T) {
	testCases := []struct {
		name    string
		request models.FinalizeSecretUpdateRequest
	}{
		{
			name: "missing encrypted payload",
			request: models.FinalizeSecretUpdateRequest{
				EncryptedPayload: "",
				EncryptionIV:     "iv-data",
				WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
			},
		},
		{
			name: "missing encryption IV",
			request: models.FinalizeSecretUpdateRequest{
				EncryptedPayload: "enc-data",
				EncryptionIV:     "",
				WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
			},
		},
		{
			name: "empty wrapped keys",
			request: models.FinalizeSecretUpdateRequest{
				EncryptedPayload: "enc-data",
				EncryptionIV:     "iv-data",
				WrappedKeys:      []models.WrappedKeyEntry{},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			suite := setupSecretUpdateTestSuite(t)

			body, _ := json.Marshal(testCase.request)
			req := authenticatedRequest(http.MethodPost, "/api/v1/encrypted-secrets/finalize?id=secret-1", body, regionAdminClaims())
			recorder := httptest.NewRecorder()

			suite.handler.Finalize(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestSecretUpdateHandler_Finalize_Success(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	claims := regionAdminClaims()
	createdAt := time.Now().UTC()
	groupID := "group-1"
	regionID := "region-1"

	// 1. GetByID on encrypted_secrets
	suite.mock.ExpectQuery("SELECT id, secret_type, signal_group_id, meshtastic_channel_id.*FROM encrypted_secrets.*WHERE id").
		WithArgs("secret-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "secret_type", "signal_group_id", "meshtastic_channel_id",
			"encrypted_payload", "encryption_iv", "updated_by", "updated_at",
		}).AddRow("secret-1", "signal_invite", &groupID, nil, "old-enc", "old-iv", "user-1", createdAt))

	// 2. resolveGroupScope: GetByID on signal_groups
	suite.mock.ExpectQuery("SELECT id, region_id, school_id, district_id, group_name.*FROM signal_groups").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "region_id", "school_id", "district_id", "group_name", "description", "created_by", "created_at", "is_active"}).
			AddRow(groupID, &regionID, nil, nil, "Test Group", nil, "user-1", createdAt, true))

	// IsUserAdmin
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs(claims.UserID, regionID).
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(true))

	// GetAdminCount
	suite.mock.ExpectQuery("COUNT").
		WithArgs(regionID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	// 3. Transaction to verify approved_pending_finalization proposal exists
	suite.mock.ExpectBegin()
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE encrypted_secret_id.*FOR UPDATE").
		WithArgs("secret-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			claims.UserID, "enc-data", "enc-iv", nil,
			string(models.ProposalStatusApprovedPendingFinalization), createdAt, createdAt.Add(72*time.Hour), nil, nil))
	suite.mock.ExpectCommit()

	// 4. UpdatePayloadAndKeys starts an internal transaction
	suite.mock.ExpectBegin()
	suite.mock.ExpectExec("UPDATE encrypted_secrets").
		WithArgs("new-enc-data", "new-iv", claims.UserID, sqlmock.AnyArg(), "secret-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	suite.mock.ExpectExec("DELETE FROM encrypted_secret_keys WHERE secret_id").
		WithArgs("secret-1").
		WillReturnResult(sqlmock.NewResult(0, 3))
	suite.mock.ExpectExec("INSERT INTO encrypted_secret_keys").
		WithArgs("secret-1", "user-1", "dek-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	suite.mock.ExpectExec("INSERT INTO encrypted_secret_keys").
		WithArgs("secret-1", "user-2", "dek-2", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	suite.mock.ExpectCommit()

	// 5. MarkFinalized
	suite.mock.ExpectExec("UPDATE secret_update_proposals SET finalized_at").
		WithArgs(sqlmock.AnyArg(), "prop-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Audit
	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), models.AuditActionInviteLinkUpdated,
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body, _ := json.Marshal(models.FinalizeSecretUpdateRequest{
		EncryptedPayload: "new-enc-data",
		EncryptionIV:     "new-iv",
		WrappedKeys: []models.WrappedKeyEntry{
			{UserID: "user-1", WrappedDEK: "dek-1"},
			{UserID: "user-2", WrappedDEK: "dek-2"},
		},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/encrypted-secrets/finalize?id=secret-1", body, claims)
	recorder := httptest.NewRecorder()

	suite.handler.Finalize(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["status"] != "finalized" {
		t.Errorf("expected status 'finalized', got %v", respBody["status"])
	}
	if respBody["secret_id"] != "secret-1" {
		t.Errorf("expected secret_id 'secret-1', got %v", respBody["secret_id"])
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

// =============================================================================
// GetProposal Tests
// =============================================================================

func TestSecretUpdateHandler_GetProposal_NoAuth(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	req := authenticatedRequest(http.MethodGet, "/api/v1/secret-proposals?id=prop-1", nil, nil)
	recorder := httptest.NewRecorder()

	suite.handler.GetProposal(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestSecretUpdateHandler_GetProposal_MissingID(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	req := authenticatedRequest(http.MethodGet, "/api/v1/secret-proposals", nil, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetProposal(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestSecretUpdateHandler_GetProposal_NotFound(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	// GetByID — return empty rows to trigger "proposal not found"
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id").
		WithArgs("prop-nonexistent").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}))

	req := authenticatedRequest(http.MethodGet, "/api/v1/secret-proposals?id=prop-nonexistent", nil, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.GetProposal(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d: %s", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestSecretUpdateHandler_GetProposal_NonAdminForbidden(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	regionID := "region-1"
	claims := regionAdminClaims()

	// GetByID — returns proposal with region scope
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			"admin-user-123", "enc-data", "enc-iv", nil,
			"pending", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	// verifyAdminAccess: IsUserAdmin returns false
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs(claims.UserID, regionID).
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(false))

	req := authenticatedRequest(http.MethodGet, "/api/v1/secret-proposals?id=prop-1", nil, claims)
	recorder := httptest.NewRecorder()

	suite.handler.GetProposal(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestSecretUpdateHandler_GetProposal_SuperuserBypass(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	regionID := "region-1"
	suClaims := superuserClaims()

	// GetByID — returns proposal with region scope
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			"admin-user-123", "enc-data", "enc-iv", nil,
			"pending", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	// Superuser skips IsUserAdmin — goes directly to GetAdminCount
	suite.mock.ExpectQuery("COUNT").
		WithArgs(regionID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	// GetByIDWithVotes
	suite.mock.ExpectQuery("SELECT p.id.*FROM secret_update_proposals p").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "secret_type", "scope_name", "group_name",
			"proposed_by", "username",
			"encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", "signal_invite", "Region One", "Group One",
			"admin-user-123", "adminuser",
			"enc-data", "enc-iv", nil,
			"pending", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	// Votes subquery
	suite.mock.ExpectQuery("SELECT v.voter_id.*FROM secret_update_votes v").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{"voter_id", "username", "vote", "voted_at"}))

	// GetWrappedDEK for caller
	suite.mock.ExpectQuery("SELECT wrapped_dek FROM proposal_wrapped_keys").
		WithArgs("prop-1", suClaims.UserID).
		WillReturnRows(sqlmock.NewRows([]string{"wrapped_dek"}))

	req := authenticatedRequest(http.MethodGet, "/api/v1/secret-proposals?id=prop-1", nil, suClaims)
	recorder := httptest.NewRecorder()

	suite.handler.GetProposal(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestSecretUpdateHandler_GetProposal_Success(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	regionID := "region-1"
	claims := regionAdminClaims()

	// GetByID — returns proposal with region scope
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			"admin-user-123", "enc-data", "enc-iv", nil,
			"pending", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	// verifyAdminAccess: IsUserAdmin
	suite.mock.ExpectQuery("user_admin_regions").
		WithArgs(claims.UserID, regionID).
		WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(true))

	// GetAdminCount
	suite.mock.ExpectQuery("COUNT").
		WithArgs(regionID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	// GetByIDWithVotes — complex JOIN query
	suite.mock.ExpectQuery("SELECT p.id.*FROM secret_update_proposals p").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "secret_type", "scope_name", "group_name",
			"proposed_by", "username",
			"encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", "signal_invite", "Region One", "Group One",
			"admin-user-123", "adminuser",
			"enc-data", "enc-iv", nil,
			"pending", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	// Votes subquery
	suite.mock.ExpectQuery("SELECT v.voter_id.*FROM secret_update_votes v").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{"voter_id", "username", "vote", "voted_at"}).
			AddRow("admin-user-123", "adminuser", true, createdAt))

	// GetWrappedDEK for caller
	suite.mock.ExpectQuery("SELECT wrapped_dek FROM proposal_wrapped_keys").
		WithArgs("prop-1", claims.UserID).
		WillReturnRows(sqlmock.NewRows([]string{"wrapped_dek"}).AddRow("my-dek"))

	req := authenticatedRequest(http.MethodGet, "/api/v1/secret-proposals?id=prop-1", nil, claims)
	recorder := httptest.NewRecorder()

	suite.handler.GetProposal(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response models.SecretProposalDetailResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.ID != "prop-1" {
		t.Errorf("expected id 'prop-1', got %q", response.ID)
	}
	if response.Status != "pending" {
		t.Errorf("expected status 'pending', got %q", response.Status)
	}
	if response.WrappedDEK != "my-dek" {
		t.Errorf("expected wrapped_dek 'my-dek', got %q", response.WrappedDEK)
	}
	if !response.HasVoted {
		t.Error("expected has_voted=true since user voted")
	}
	if response.CanVote {
		t.Error("expected can_vote=false since user already voted")
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

// =============================================================================
// ListProposals Tests
// =============================================================================

func TestSecretUpdateHandler_ListProposals_NoAuth(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	req := authenticatedRequest(http.MethodGet, "/api/v1/secret-proposals", nil, nil)
	recorder := httptest.NewRecorder()

	suite.handler.ListProposals(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestSecretUpdateHandler_ListProposals_Success(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	claims := regionAdminClaims()

	// List query (non-superuser path)
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals").
		WithArgs(claims.UserID, claims.UserID, claims.UserID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "secret_type", "scope_name", "group_name",
			"proposed_by", "reason", "votes", "status", "created_at", "expires_at",
		}).
			AddRow("prop-1", "secret-1", "signal_invite", "Region One", "Group One",
				"admin-user-123", nil, 2, "pending", createdAt, createdAt.Add(72*time.Hour)).
			AddRow("prop-2", "secret-2", "meshtastic_channel", "School One", "Channel One",
				"admin-user-456", nil, 1, "pending", createdAt, createdAt.Add(72*time.Hour)))

	req := authenticatedRequest(http.MethodGet, "/api/v1/secret-proposals", nil, claims)
	recorder := httptest.NewRecorder()

	suite.handler.ListProposals(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	proposals, ok := respBody["proposals"].([]interface{})
	if !ok {
		t.Fatalf("expected proposals to be an array, got %T", respBody["proposals"])
	}
	if len(proposals) != 2 {
		t.Errorf("expected 2 proposals, got %d", len(proposals))
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestSecretUpdateHandler_ListProposals_WithFilters(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	claims := regionAdminClaims()

	// List query with status and region filters — appends additional WHERE clauses with args
	// Base args: userID x3, then filter args: status, region_id
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals").
		WithArgs(claims.UserID, claims.UserID, claims.UserID, "pending", "region-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "secret_type", "scope_name", "group_name",
			"proposed_by", "reason", "votes", "status", "created_at", "expires_at",
		}))

	req := authenticatedRequest(http.MethodGet, "/api/v1/secret-proposals?status=pending&region_id=region-1", nil, claims)
	recorder := httptest.NewRecorder()

	suite.handler.ListProposals(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	proposals, ok := respBody["proposals"].([]interface{})
	if !ok {
		t.Fatalf("expected proposals to be an array, got %T", respBody["proposals"])
	}
	if len(proposals) != 0 {
		t.Errorf("expected 0 proposals, got %d", len(proposals))
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestSecretUpdateHandler_ListProposals_DBError(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	claims := regionAdminClaims()

	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals").
		WithArgs(claims.UserID, claims.UserID, claims.UserID).
		WillReturnError(sql.ErrConnDone)

	req := authenticatedRequest(http.MethodGet, "/api/v1/secret-proposals", nil, claims)
	recorder := httptest.NewRecorder()

	suite.handler.ListProposals(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}
}

// =============================================================================
// ExpireProposal Tests
// =============================================================================

func TestSecretUpdateHandler_ExpireProposal_NoAuth(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	req := authenticatedRequest(http.MethodPost, "/api/v1/secret-proposals/expire?id=prop-1", nil, nil)
	recorder := httptest.NewRecorder()

	suite.handler.ExpireProposal(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestSecretUpdateHandler_ExpireProposal_NotSuperuser(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	req := authenticatedRequest(http.MethodPost, "/api/v1/secret-proposals/expire?id=prop-1", nil, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.ExpireProposal(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "forbidden" {
		t.Errorf("expected error 'forbidden', got %v", respBody["error"])
	}
}

func TestSecretUpdateHandler_ExpireProposal_MissingID(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	req := authenticatedRequest(http.MethodPost, "/api/v1/secret-proposals/expire", nil, superuserClaims())
	recorder := httptest.NewRecorder()

	suite.handler.ExpireProposal(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestSecretUpdateHandler_ExpireProposal_NotFound(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	// GetByID returns no rows
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id").
		WithArgs("prop-nonexistent").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}))

	req := authenticatedRequest(http.MethodPost, "/api/v1/secret-proposals/expire?id=prop-nonexistent", nil, superuserClaims())
	recorder := httptest.NewRecorder()

	suite.handler.ExpireProposal(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d: %s", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestSecretUpdateHandler_ExpireProposal_AlreadyClosed(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	regionID := "region-1"

	// GetByID returns an already-expired proposal
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			"admin-user-123", "enc-data", "enc-iv", nil,
			"rejected", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	req := authenticatedRequest(http.MethodPost, "/api/v1/secret-proposals/expire?id=prop-1", nil, superuserClaims())
	recorder := httptest.NewRecorder()

	suite.handler.ExpireProposal(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d: %s", http.StatusConflict, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["error"] != "proposal_closed" {
		t.Errorf("expected error 'proposal_closed', got %v", respBody["error"])
	}
}

func TestSecretUpdateHandler_ExpireProposal_Pending_Success(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	regionID := "region-1"
	claims := superuserClaims()

	// GetByID — pending proposal
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			"admin-user-123", "enc-data", "enc-iv", nil,
			"pending", createdAt, createdAt.Add(72*time.Hour), nil, nil))

	// UpdateStatus
	suite.mock.ExpectExec("UPDATE secret_update_proposals SET status").
		WithArgs(string(models.ProposalStatusExpired), sqlmock.AnyArg(), "prop-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Audit
	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), models.AuditActionProposalExpired,
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := authenticatedRequest(http.MethodPost, "/api/v1/secret-proposals/expire?id=prop-1", nil, claims)
	recorder := httptest.NewRecorder()

	suite.handler.ExpireProposal(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	respBody := parseResponseBody(t, recorder)
	if respBody["status"] != "expired" {
		t.Errorf("expected status 'expired', got %v", respBody["status"])
	}
	if respBody["proposal_id"] != "prop-1" {
		t.Errorf("expected proposal_id 'prop-1', got %v", respBody["proposal_id"])
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

func TestSecretUpdateHandler_ExpireProposal_ApprovedPendingFinalization_Success(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	regionID := "region-1"
	claims := superuserClaims()

	// GetByID — approved_pending_finalization proposal
	suite.mock.ExpectQuery("SELECT.*FROM secret_update_proposals.*WHERE id").
		WithArgs("prop-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "encrypted_secret_id", "region_id", "school_id", "district_id",
			"proposed_by", "encrypted_payload", "encryption_iv", "reason",
			"status", "created_at", "expires_at", "resolved_at", "finalized_at",
		}).AddRow("prop-1", "secret-1", &regionID, nil, nil,
			"admin-user-123", "enc-data", "enc-iv", nil,
			string(models.ProposalStatusApprovedPendingFinalization), createdAt, createdAt.Add(72*time.Hour), nil, nil))

	// UpdateStatus
	suite.mock.ExpectExec("UPDATE secret_update_proposals SET status").
		WithArgs(string(models.ProposalStatusExpired), sqlmock.AnyArg(), "prop-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Audit
	suite.mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), models.AuditActionProposalExpired,
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := authenticatedRequest(http.MethodPost, "/api/v1/secret-proposals/expire?id=prop-1", nil, claims)
	recorder := httptest.NewRecorder()

	suite.handler.ExpireProposal(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	if err := suite.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet mock expectations: %v", err)
	}
}

// =============================================================================
// School Scope Tests (for CreateProposal/Vote scope variations)
// =============================================================================

func TestSecretUpdateHandler_CreateProposal_SchoolScope_NotAdmin(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	groupID := "group-1"
	schoolID := "school-1"

	// GetBySignalGroupID
	suite.mock.ExpectQuery("SELECT.*FROM encrypted_secrets.*WHERE signal_group_id").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "secret_type", "signal_group_id", "meshtastic_channel_id",
			"encrypted_payload", "encryption_iv", "updated_by", "updated_at",
		}).AddRow("secret-1", "signal_invite", &groupID, nil, "old-enc", "old-iv", "user-1", createdAt))

	// resolveGroupScope — school-scoped group
	suite.mock.ExpectQuery("SELECT id, region_id, school_id, district_id, group_name.*FROM signal_groups").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "region_id", "school_id", "district_id", "group_name", "description", "created_by", "created_at", "is_active"}).
			AddRow(groupID, nil, &schoolID, nil, "Test Group", nil, "user-1", createdAt, true))

	// verifySchoolAdmin: GetUserSchool — not a member
	suite.mock.ExpectQuery("SELECT.*FROM user_schools.*WHERE user_id").
		WithArgs("admin-user-123", schoolID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}))

	body, _ := json.Marshal(models.CreateSecretProposalRequest{
		EncryptedPayload: "enc-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/signal-groups/secret-proposals?id="+groupID, body, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.CreateProposal(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}
}

func TestSecretUpdateHandler_CreateProposal_DistrictScope_NotAdmin(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	createdAt := time.Now().UTC()
	groupID := "group-1"
	districtID := "district-1"

	// GetBySignalGroupID
	suite.mock.ExpectQuery("SELECT.*FROM encrypted_secrets.*WHERE signal_group_id").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "secret_type", "signal_group_id", "meshtastic_channel_id",
			"encrypted_payload", "encryption_iv", "updated_by", "updated_at",
		}).AddRow("secret-1", "signal_invite", &groupID, nil, "old-enc", "old-iv", "user-1", createdAt))

	// resolveGroupScope — district-scoped group
	suite.mock.ExpectQuery("SELECT id, region_id, school_id, district_id, group_name.*FROM signal_groups").
		WithArgs(groupID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "region_id", "school_id", "district_id", "group_name", "description", "created_by", "created_at", "is_active"}).
			AddRow(groupID, nil, nil, &districtID, "Test Group", nil, "user-1", createdAt, true))

	// verifyDistrictAdmin: ListByDistrict returns one school
	suite.mock.ExpectQuery("SELECT.*FROM schools.*WHERE.*district_id").
		WithArgs(districtID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "nces_id", "name", "city", "state",
			"district_id", "district_name", "latitude", "longitude",
			"member_count", "verified_count", "admin_count",
		}).AddRow("school-1", "NCES001", "School One", "City", "ST",
			districtID, "District One", 40.0, -74.0, 10, 5, 3))

	// GetUserSchool — not a member
	suite.mock.ExpectQuery("SELECT.*FROM user_schools.*WHERE user_id").
		WithArgs("admin-user-123", "school-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "school_id", "is_admin", "verification_status", "verified_at"}))

	body, _ := json.Marshal(models.CreateSecretProposalRequest{
		EncryptedPayload: "enc-data",
		EncryptionIV:     "iv-data",
		WrappedKeys:      []models.WrappedKeyEntry{{UserID: "user-1", WrappedDEK: "dek-1"}},
	})

	req := authenticatedRequest(http.MethodPost, "/api/v1/signal-groups/secret-proposals?id="+groupID, body, regionAdminClaims())
	recorder := httptest.NewRecorder()

	suite.handler.CreateProposal(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}
}

// =============================================================================
// SetNotificationService Test
// =============================================================================

func TestSecretUpdateHandler_SetNotificationService(t *testing.T) {
	suite := setupSecretUpdateTestSuite(t)

	if suite.handler.notificationService != nil {
		t.Error("expected notification service to be nil initially")
	}

	// SetNotificationService should not panic with nil
	suite.handler.SetNotificationService(nil)

	if suite.handler.notificationService != nil {
		t.Error("expected notification service to still be nil")
	}
}
