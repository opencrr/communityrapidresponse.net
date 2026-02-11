package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/handlers"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/mocks"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// DeletionE2ETestSuite holds dependencies for deletion proposal E2E tests
type DeletionE2ETestSuite struct {
	t               *testing.T
	db              *database.DB
	server          *httptest.Server
	userRepo        *database.UserRepository
	regionRepo      *database.RegionRepository
	groupRepo       *database.SignalGroupRepository
	proposalRepo    *database.DeletionProposalRepository
	jwtAuth         *middleware.JWTAuth
}

// SetupDeletionE2ETest creates a test suite with the deletion proposal handler wired in
func SetupDeletionE2ETest(t *testing.T) *DeletionE2ETestSuite {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST not set, skipping e2e tests")
	}

	port := 3306
	if p := os.Getenv("TEST_DB_PORT"); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &port)
	}

	cfg := &config.DatabaseConfig{
		Host:     host,
		Port:     port,
		User:     getEnvOrDefault("TEST_DB_USER", "test"),
		Password: getEnvOrDefault("TEST_DB_PASSWORD", "testpassword"),
		Name:     getEnvOrDefault("TEST_DB_NAME", "communityrapidresponse_test"),
		Charset:  "utf8mb4",
	}

	db, err := database.New(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Create repositories
	userRepo := database.NewUserRepository(db)
	regionRepo := database.NewRegionRepository(db)
	verifyRepo := database.NewVerificationRepository(db)
	vouchRepo := database.NewVouchRepository(db)
	groupRepo := database.NewSignalGroupRepository(db)
	inviteLinkProposalRepo := database.NewInviteLinkProposalRepository(db)
	membershipRepo := database.NewMembershipRepository(db)
	schoolRepo := database.NewSchoolRepository(db)
	districtRepo := database.NewSchoolDistrictRepository(db)
	schoolVouchRepo := database.NewSchoolVouchRepository(db)
	auditRepo := database.NewAuditRepository(db)
	deletionProposalRepo := database.NewDeletionProposalRepository(db)

	// Create JWT auth
	jwtConfig := &config.JWTConfig{
		Secret:          "test_secret_key_at_least_32_characters_long",
		ExpirationHours: 24,
		Issuer:          "test_issuer",
	}
	jwtAuth := middleware.NewJWTAuth(jwtConfig)

	// Create mock services
	mockPostgrid := mocks.NewMockPostgridService()
	mockMapbox := mocks.NewMockMapboxService()

	// Create handlers
	authHandler := handlers.NewAuthHandler(userRepo, jwtAuth)
	regionHandler := handlers.NewRegionHandler(regionRepo, mockMapbox, nil)
	verificationHandler := handlers.NewVerificationHandler(
		nil, verifyRepo, vouchRepo, userRepo, regionRepo,
		mockPostgrid, mockMapbox, nil,
		false, 30,
	)
	consensusConfig := &config.ConsensusConfig{VotePercent: 50, VoteFloor: 3}
	signalGroupHandler := handlers.NewSignalGroupHandler(
		nil, groupRepo, inviteLinkProposalRepo, regionRepo, nil, consensusConfig,
	)
	adminHandler := handlers.NewAdminHandler(userRepo, regionRepo, nil)
	mfaConfig := &config.MFAConfig{
		EncryptionKey: "01234567890123456789012345678901",
		Issuer:        "Test MFA",
	}
	mfaService, _ := services.NewMFAService(mfaConfig)
	mfaHandler := handlers.NewMFAHandler(nil, userRepo, mfaService, jwtAuth, false, nil)
	membershipHandler := handlers.NewMembershipHandler(nil, membershipRepo, regionRepo, userRepo, nil)

	blocklistConfig := &config.BlocklistConfig{
		AddressBlocklistDuration:  2 * 365 * 24 * time.Hour,
		ProposalRateLimitPerMonth: 5,
	}
	blocklistProposalRepo := database.NewBlocklistProposalRepository(db, blocklistConfig)
	blocklistProposalHandler := handlers.NewBlocklistProposalHandler(
		nil, blocklistProposalRepo, regionRepo, userRepo, nil, consensusConfig, blocklistConfig,
	)

	schoolHandler := handlers.NewSchoolHandler(
		db, schoolRepo, districtRepo, schoolVouchRepo, groupRepo, inviteLinkProposalRepo,
		userRepo, auditRepo, nil, consensusConfig, false, 0,
	)

	// Create deletion proposal handler — the key difference from SetupE2ETest
	deletionProposalHandler := handlers.NewDeletionProposalHandler(
		db, deletionProposalRepo, groupRepo, regionRepo, schoolRepo, userRepo, auditRepo, consensusConfig,
	)

	// Create router with deletion proposals wired in
	router := handlers.NewRouter(
		authHandler, mfaHandler, regionHandler, signalGroupHandler, verificationHandler, adminHandler,
		membershipHandler, blocklistProposalHandler, deletionProposalHandler, schoolHandler, nil, jwtAuth, nil, nil, nil,
		[]string{"*"}, nil,
	)
	handler := router.Setup()
	server := httptest.NewServer(handler)

	suite := &DeletionE2ETestSuite{
		t:            t,
		db:           db,
		server:       server,
		userRepo:     userRepo,
		regionRepo:   regionRepo,
		groupRepo:    groupRepo,
		proposalRepo: deletionProposalRepo,
		jwtAuth:      jwtAuth,
	}

	t.Cleanup(func() {
		server.Close()
		_ = db.Close()
	})

	return suite
}

func (s *DeletionE2ETestSuite) request(method, path string, body interface{}, token string) *http.Response {
	var bodyReader *bytes.Reader
	if body != nil {
		bodyBytes, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(bodyBytes)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req, _ := http.NewRequest(method, s.server.URL+path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("Request failed: %v", err)
	}

	return resp
}

func (s *DeletionE2ETestSuite) createAdminUser(prefix string) (*models.User, string) {
	user := &models.User{
		Username:         fmt.Sprintf("dl-e2e-%s-%d", prefix, time.Now().UnixNano()%100000),
		Email:            fmt.Sprintf("dl-e2e-%s-%d@test.com", prefix, time.Now().UnixNano()%100000),
		PasswordHash:     "$2a$12$test.hash.only",
		VerificationTier: models.TierPostcard,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	if err := s.userRepo.Create(context.Background(), user); err != nil {
		s.t.Fatalf("Failed to create user: %v", err)
	}
	token, err := s.jwtAuth.GenerateToken(user)
	if err != nil {
		s.t.Fatalf("Failed to generate token: %v", err)
	}
	return user, token
}

func (s *DeletionE2ETestSuite) createSuperuser(prefix string) (*models.User, string) {
	user := &models.User{
		Username:         fmt.Sprintf("dl-e2e-%s-%d", prefix, time.Now().UnixNano()%100000),
		Email:            fmt.Sprintf("dl-e2e-%s-%d@test.com", prefix, time.Now().UnixNano()%100000),
		PasswordHash:     "$2a$12$test.hash.only",
		VerificationTier: models.TierPostcard,
		PostcardVerified: true,
		VouchVerified:    true,
		IsSuperuser:      true,
	}
	if err := s.userRepo.Create(context.Background(), user); err != nil {
		s.t.Fatalf("Failed to create superuser: %v", err)
	}
	token, err := s.jwtAuth.GenerateToken(user)
	if err != nil {
		s.t.Fatalf("Failed to generate token: %v", err)
	}
	return user, token
}

func (s *DeletionE2ETestSuite) createRegion(name, creatorID string) string {
	region := &models.GeographicRegion{
		Name:       name,
		RegionType: models.RegionTypeNeighborhood,
		CreatedBy:  &creatorID,
	}
	if err := s.regionRepo.CreateWithOptionalGeometry(context.Background(), region, ""); err != nil {
		s.t.Fatalf("Failed to create region: %v", err)
	}
	return region.ID
}

func (s *DeletionE2ETestSuite) createChildRegion(name, parentID, creatorID string) string {
	region := &models.GeographicRegion{
		Name:           name,
		RegionType:     models.RegionTypeCityBlock,
		ParentRegionID: &parentID,
		CreatedBy:      &creatorID,
	}
	if err := s.regionRepo.CreateWithOptionalGeometry(context.Background(), region, ""); err != nil {
		s.t.Fatalf("Failed to create child region: %v", err)
	}
	return region.ID
}

func (s *DeletionE2ETestSuite) addUserToRegion(userID, regionID string, isAdmin bool) {
	if err := s.regionRepo.AddUserToRegion(context.Background(), userID, regionID, isAdmin); err != nil {
		s.t.Fatalf("Failed to add user to region: %v", err)
	}
}

func (s *DeletionE2ETestSuite) createSignalGroup(regionID, creatorID, name string) string {
	group := &models.SignalGroup{
		RegionID:   &regionID,
		GroupName:  name,
		InviteLink: "https://signal.group/e2e-test",
		CreatedBy:  &creatorID,
		IsActive:   true,
	}
	if err := s.groupRepo.Create(context.Background(), group); err != nil {
		s.t.Fatalf("Failed to create signal group: %v", err)
	}
	return group.ID
}

func (s *DeletionE2ETestSuite) cleanup(userIDs []string, regionIDs []string, groupIDs []string) {
	ctx := context.Background()
	// Clean deletion votes and proposals first (FK constraints)
	_, _ = s.db.ExecContext(ctx, "DELETE FROM deletion_votes WHERE proposal_id IN (SELECT id FROM deletion_proposals WHERE reason LIKE '%dl-e2e%')")
	_, _ = s.db.ExecContext(ctx, "DELETE FROM deletion_proposals WHERE reason LIKE '%dl-e2e%'")

	for _, gid := range groupIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE id = ?", gid)
	}
	for _, uid := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE user_id = ?", uid)
	}
	// Delete child regions first (FK on parent_region_id)
	for i := len(regionIDs) - 1; i >= 0; i-- {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM signal_groups WHERE region_id = ?", regionIDs[i])
		_, _ = s.db.ExecContext(ctx, "DELETE FROM user_regions WHERE region_id = ?", regionIDs[i])
		_, _ = s.db.ExecContext(ctx, "DELETE FROM geographic_regions WHERE id = ?", regionIDs[i])
	}
	for _, uid := range userIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", uid)
	}
}

// =============================================================================
// Signal Group Deletion E2E
// =============================================================================

func TestE2E_DeletionProposal_SignalGroupDeletion(t *testing.T) {
	suite := SetupDeletionE2ETest(t)

	// Create 3 admin users
	admin1, token1 := suite.createAdminUser("sgdel-a1")
	admin2, token2 := suite.createAdminUser("sgdel-a2")
	admin3, token3 := suite.createAdminUser("sgdel-a3")

	// Create region and add all as admins
	regionID := suite.createRegion("DL E2E SG Del Region", admin1.ID)
	suite.addUserToRegion(admin1.ID, regionID, true)
	suite.addUserToRegion(admin2.ID, regionID, true)
	suite.addUserToRegion(admin3.ID, regionID, true)

	// Create signal group
	groupID := suite.createSignalGroup(regionID, admin1.ID, "dl-e2e-sg-delete")

	defer suite.cleanup(
		[]string{admin1.ID, admin2.ID, admin3.ID},
		[]string{regionID},
		[]string{groupID},
	)

	// Step 1: Create proposal
	var proposalID string
	t.Run("create proposal", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/deletion-proposals", map[string]string{
			"asset_type": "signal_group",
			"asset_id":   groupID,
			"reason":     "dl-e2e signal group no longer needed",
		}, token1)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("Expected 201, got %d", resp.StatusCode)
		}

		var body models.DeletionProposalResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.ProposalID == "" {
			t.Fatal("Expected proposal ID")
		}
		proposalID = body.ProposalID

		if body.Status != "pending" {
			t.Errorf("Expected status pending, got %s", body.Status)
		}
		if body.CurrentVotes != 1 {
			t.Errorf("Expected 1 auto-vote, got %d", body.CurrentVotes)
		}
	})

	// Step 2: Second admin votes
	t.Run("second admin votes", func(t *testing.T) {
		resp := suite.request("POST", fmt.Sprintf("/api/v1/deletion-proposals/%s/vote", proposalID), map[string]bool{
			"vote": true,
		}, token2)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body models.DeletionProposalResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.Status != "pending" {
			t.Errorf("Expected still pending with 2 votes, got %s", body.Status)
		}
		if body.CurrentVotes != 2 {
			t.Errorf("Expected 2 votes, got %d", body.CurrentVotes)
		}
	})

	// Step 3: Third admin votes — triggers consensus
	t.Run("third admin votes triggers consensus", func(t *testing.T) {
		resp := suite.request("POST", fmt.Sprintf("/api/v1/deletion-proposals/%s/vote", proposalID), map[string]bool{
			"vote": true,
		}, token3)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body models.DeletionProposalResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.Status != "approved" {
			t.Errorf("Expected approved after 3 votes, got %s", body.Status)
		}
		if body.CurrentVotes != 3 {
			t.Errorf("Expected 3 votes, got %d", body.CurrentVotes)
		}
	})

	// Step 4: Verify signal group is deactivated
	t.Run("signal group is deactivated", func(t *testing.T) {
		var isActive bool
		err := suite.db.QueryRowContext(context.Background(),
			`SELECT is_active FROM signal_groups WHERE id = ?`, groupID).Scan(&isActive)
		if err != nil {
			t.Fatalf("Failed to check signal group: %v", err)
		}
		if isActive {
			t.Error("Expected signal group to be deactivated")
		}
	})

	// Step 5: Verify proposal details via GET
	t.Run("get proposal shows approved", func(t *testing.T) {
		resp := suite.request("GET", fmt.Sprintf("/api/v1/deletion-proposals/%s", proposalID), nil, token1)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body models.DeletionProposalDetailResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.Status != "approved" {
			t.Errorf("Expected status approved, got %s", body.Status)
		}
		if body.CurrentVotes != 3 {
			t.Errorf("Expected 3 votes, got %d", body.CurrentVotes)
		}
		if len(body.Votes) != 3 {
			t.Errorf("Expected 3 vote details, got %d", len(body.Votes))
		}
	})
}

// =============================================================================
// Sub-Region Deletion E2E
// =============================================================================

func TestE2E_DeletionProposal_SubRegionDeletion(t *testing.T) {
	suite := SetupDeletionE2ETest(t)

	// Create 3 admin users
	admin1, token1 := suite.createAdminUser("srdel-a1")
	admin2, token2 := suite.createAdminUser("srdel-a2")
	admin3, token3 := suite.createAdminUser("srdel-a3")

	// Create parent region and child region
	parentRegionID := suite.createRegion("DL E2E SubRegion Parent", admin1.ID)
	suite.addUserToRegion(admin1.ID, parentRegionID, true)
	suite.addUserToRegion(admin2.ID, parentRegionID, true)
	suite.addUserToRegion(admin3.ID, parentRegionID, true)

	childRegionID := suite.createChildRegion("DL E2E SubRegion Child", parentRegionID, admin1.ID)
	suite.addUserToRegion(admin1.ID, childRegionID, true)

	// Add a signal group to the child region
	childGroupID := suite.createSignalGroup(childRegionID, admin1.ID, "dl-e2e-child-sg")

	defer suite.cleanup(
		[]string{admin1.ID, admin2.ID, admin3.ID},
		[]string{parentRegionID, childRegionID},
		[]string{childGroupID},
	)

	// Step 1: Create sub-region deletion proposal
	var proposalID string
	t.Run("create sub-region proposal", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/deletion-proposals", map[string]string{
			"asset_type": "sub_region",
			"asset_id":   childRegionID,
			"reason":     "dl-e2e sub-region no longer needed",
		}, token1)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("Expected 201, got %d", resp.StatusCode)
		}

		var body models.DeletionProposalResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)
		proposalID = body.ProposalID
	})

	// Step 2 & 3: Two more admins vote
	t.Run("second admin votes", func(t *testing.T) {
		resp := suite.request("POST", fmt.Sprintf("/api/v1/deletion-proposals/%s/vote", proposalID), map[string]bool{
			"vote": true,
		}, token2)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("third admin votes triggers cascade deletion", func(t *testing.T) {
		resp := suite.request("POST", fmt.Sprintf("/api/v1/deletion-proposals/%s/vote", proposalID), map[string]bool{
			"vote": true,
		}, token3)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body models.DeletionProposalResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.Status != "approved" {
			t.Errorf("Expected approved, got %s", body.Status)
		}
	})

	// Step 4: Verify cascade deletion
	t.Run("child region is deleted", func(t *testing.T) {
		var count int
		err := suite.db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM geographic_regions WHERE id = ?`, childRegionID).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check child region: %v", err)
		}
		if count != 0 {
			t.Error("Expected child region to be deleted")
		}
	})

	t.Run("child signal groups are deleted", func(t *testing.T) {
		var count int
		err := suite.db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM signal_groups WHERE region_id = ?`, childRegionID).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check signal groups: %v", err)
		}
		if count != 0 {
			t.Error("Expected signal groups in child region to be deleted")
		}
	})

	t.Run("child user_regions are deleted", func(t *testing.T) {
		var count int
		err := suite.db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM user_regions WHERE region_id = ?`, childRegionID).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check user_regions: %v", err)
		}
		if count != 0 {
			t.Error("Expected user_regions for child to be deleted")
		}
	})

	t.Run("parent region still exists", func(t *testing.T) {
		var count int
		err := suite.db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM geographic_regions WHERE id = ?`, parentRegionID).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check parent region: %v", err)
		}
		if count != 1 {
			t.Error("Expected parent region to still exist")
		}
	})
}

// =============================================================================
// Unauthorized Access E2E
// =============================================================================

func TestE2E_DeletionProposal_Unauthorized(t *testing.T) {
	suite := SetupDeletionE2ETest(t)

	// Create admin and non-admin users
	admin1, _ := suite.createAdminUser("unauth-a1")
	admin2, _ := suite.createAdminUser("unauth-a2")
	admin3, _ := suite.createAdminUser("unauth-a3")
	nonAdmin, nonAdminToken := suite.createAdminUser("unauth-member")

	// Create region with 3 admins
	regionID := suite.createRegion("DL E2E Unauthorized Region", admin1.ID)
	suite.addUserToRegion(admin1.ID, regionID, true)
	suite.addUserToRegion(admin2.ID, regionID, true)
	suite.addUserToRegion(admin3.ID, regionID, true)
	suite.addUserToRegion(nonAdmin.ID, regionID, false) // non-admin member

	groupID := suite.createSignalGroup(regionID, admin1.ID, "dl-e2e-unauth-sg")

	defer suite.cleanup(
		[]string{admin1.ID, admin2.ID, admin3.ID, nonAdmin.ID},
		[]string{regionID},
		[]string{groupID},
	)

	t.Run("unauthenticated request returns 401", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/deletion-proposals", map[string]string{
			"asset_type": "signal_group",
			"asset_id":   groupID,
			"reason":     "dl-e2e unauthorized",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("non-admin cannot create proposal", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/deletion-proposals", map[string]string{
			"asset_type": "signal_group",
			"asset_id":   groupID,
			"reason":     "dl-e2e non-admin create",
		}, nonAdminToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("list requires authentication", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/deletion-proposals", nil, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected 401, got %d", resp.StatusCode)
		}
	})
}

// =============================================================================
// Superuser Expire E2E
// =============================================================================

func TestE2E_DeletionProposal_SuperuserExpire(t *testing.T) {
	suite := SetupDeletionE2ETest(t)

	admin1, token1 := suite.createAdminUser("expire-a1")
	admin2, _ := suite.createAdminUser("expire-a2")
	admin3, _ := suite.createAdminUser("expire-a3")
	_, superToken := suite.createSuperuser("expire-su")

	regionID := suite.createRegion("DL E2E Expire Region", admin1.ID)
	suite.addUserToRegion(admin1.ID, regionID, true)
	suite.addUserToRegion(admin2.ID, regionID, true)
	suite.addUserToRegion(admin3.ID, regionID, true)

	groupID := suite.createSignalGroup(regionID, admin1.ID, "dl-e2e-expire-sg")

	defer suite.cleanup(
		[]string{admin1.ID, admin2.ID, admin3.ID},
		[]string{regionID},
		[]string{groupID},
	)

	// Create proposal
	var proposalID string
	t.Run("create proposal for expiration", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/deletion-proposals", map[string]string{
			"asset_type": "signal_group",
			"asset_id":   groupID,
			"reason":     "dl-e2e expire test",
		}, token1)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("Expected 201, got %d", resp.StatusCode)
		}

		var body models.DeletionProposalResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)
		proposalID = body.ProposalID
	})

	// Admin cannot expire
	t.Run("admin cannot expire", func(t *testing.T) {
		resp := suite.request("POST", fmt.Sprintf("/api/v1/deletion-proposals/%s/expire", proposalID), nil, token1)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403, got %d", resp.StatusCode)
		}
	})

	// Superuser can expire
	t.Run("superuser can expire", func(t *testing.T) {
		resp := suite.request("POST", fmt.Sprintf("/api/v1/deletion-proposals/%s/expire", proposalID), nil, superToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["status"] != "expired" {
			t.Errorf("Expected status expired, got %v", body["status"])
		}
	})

	// Verify signal group is still active (proposal was expired, not approved)
	t.Run("signal group still active after expiry", func(t *testing.T) {
		var isActive bool
		err := suite.db.QueryRowContext(context.Background(),
			`SELECT is_active FROM signal_groups WHERE id = ?`, groupID).Scan(&isActive)
		if err != nil {
			t.Fatalf("Failed to check signal group: %v", err)
		}
		if !isActive {
			t.Error("Expected signal group to still be active after proposal expiry")
		}
	})
}

// =============================================================================
// Proposal Listing E2E
// =============================================================================

func TestE2E_DeletionProposal_ListAndFilter(t *testing.T) {
	suite := SetupDeletionE2ETest(t)

	admin1, token1 := suite.createAdminUser("list-a1")
	admin2, _ := suite.createAdminUser("list-a2")
	admin3, _ := suite.createAdminUser("list-a3")
	_, superToken := suite.createSuperuser("list-su")

	regionID := suite.createRegion("DL E2E List Region", admin1.ID)
	suite.addUserToRegion(admin1.ID, regionID, true)
	suite.addUserToRegion(admin2.ID, regionID, true)
	suite.addUserToRegion(admin3.ID, regionID, true)

	// Create multiple signal groups and proposals
	var groupIDs []string
	for i := 0; i < 3; i++ {
		gid := suite.createSignalGroup(regionID, admin1.ID, fmt.Sprintf("dl-e2e-list-sg-%d", i))
		groupIDs = append(groupIDs, gid)

		reason := "dl-e2e list test reason"
		proposal := &models.DeletionProposal{
			AssetType:  models.AssetTypeSignalGroup,
			AssetID:    gid,
			RegionID:   &regionID,
			ProposedBy: &admin1.ID,
			Reason:     &reason,
		}
		if err := suite.proposalRepo.Create(context.Background(), proposal); err != nil {
			t.Fatalf("Failed to create proposal: %v", err)
		}
	}

	defer suite.cleanup(
		[]string{admin1.ID, admin2.ID, admin3.ID},
		[]string{regionID},
		groupIDs,
	)

	t.Run("admin can list proposals", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/deletion-proposals", nil, token1)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body models.DeletionProposalListResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if len(body.Proposals) < 3 {
			t.Errorf("Expected at least 3 proposals, got %d", len(body.Proposals))
		}
	})

	t.Run("superuser can list all proposals", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/deletion-proposals", nil, superToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body models.DeletionProposalListResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if len(body.Proposals) < 3 {
			t.Errorf("Expected at least 3 proposals, got %d", len(body.Proposals))
		}
	})

	t.Run("filter by pending status", func(t *testing.T) {
		resp := suite.request("GET", "/api/v1/deletion-proposals?status=pending", nil, superToken)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var body models.DeletionProposalListResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		for _, p := range body.Proposals {
			if p.Status != "pending" {
				t.Errorf("Expected all proposals to be pending, got %s", p.Status)
			}
		}
	})
}

// =============================================================================
// Duplicate Proposal Prevention E2E
// =============================================================================

func TestE2E_DeletionProposal_DuplicatePrevention(t *testing.T) {
	suite := SetupDeletionE2ETest(t)

	admin1, token1 := suite.createAdminUser("dup-a1")
	admin2, _ := suite.createAdminUser("dup-a2")
	admin3, _ := suite.createAdminUser("dup-a3")

	regionID := suite.createRegion("DL E2E Dup Region", admin1.ID)
	suite.addUserToRegion(admin1.ID, regionID, true)
	suite.addUserToRegion(admin2.ID, regionID, true)
	suite.addUserToRegion(admin3.ID, regionID, true)

	groupID := suite.createSignalGroup(regionID, admin1.ID, "dl-e2e-dup-sg")

	defer suite.cleanup(
		[]string{admin1.ID, admin2.ID, admin3.ID},
		[]string{regionID},
		[]string{groupID},
	)

	// Create first proposal
	t.Run("first proposal succeeds", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/deletion-proposals", map[string]string{
			"asset_type": "signal_group",
			"asset_id":   groupID,
			"reason":     "dl-e2e first proposal",
		}, token1)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("Expected 201, got %d", resp.StatusCode)
		}
	})

	// Duplicate proposal should be rejected
	t.Run("duplicate proposal rejected", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/deletion-proposals", map[string]string{
			"asset_type": "signal_group",
			"asset_id":   groupID,
			"reason":     "dl-e2e duplicate attempt",
		}, token1)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusConflict {
			t.Errorf("Expected 409, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["error"] != "pending_proposal_exists" {
			t.Errorf("Expected error pending_proposal_exists, got %v", body["error"])
		}
	})
}
