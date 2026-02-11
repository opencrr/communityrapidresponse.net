package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// reportTestSuite holds all dependencies for user report handler tests
type reportTestSuite struct {
	db          *database.DB
	handler     *UserReportHandler
	reportRepo  *database.UserReportRepository
	regionRepo  *database.RegionRepository
	schoolRepo  *database.SchoolRepository
	userRepo    *database.UserRepository
	auditRepo   *database.AuditRepository
	regionID    string
	adminID     string
	admin2ID    string
	admin3ID    string
	memberID    string
	member2ID   string
	superuserID string
}

func setupReportTestSuite(t *testing.T) *reportTestSuite {
	t.Helper()

	dsn := "root:@tcp(127.0.0.1:3306)/communityrapidresponse_test?charset=utf8mb4&parseTime=true&loc=UTC"
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skip("Test database not available")
	}

	if err := sqlDB.Ping(); err != nil {
		t.Skip("Test database not available")
	}

	db := &database.DB{DB: sqlDB}

	reportRepo := database.NewUserReportRepository(db)
	regionRepo := database.NewRegionRepository(db)
	schoolRepo := database.NewSchoolRepository(db)
	userRepo := database.NewUserRepository(db)
	auditRepo := database.NewAuditRepository(db)

	handler := NewUserReportHandler(db, reportRepo, regionRepo, schoolRepo, userRepo, auditRepo)

	// Create test users
	adminID := createReportTestUser(db, "rpt-admin1")
	admin2ID := createReportTestUser(db, "rpt-admin2")
	admin3ID := createReportTestUser(db, "rpt-admin3")
	memberID := createReportTestUser(db, "rpt-member1")
	member2ID := createReportTestUser(db, "rpt-member2")
	superuserID := createReportTestSuperuser(db, "rpt-super")

	// Create test region
	regionID := createReportTestRegion(db, adminID, "Test Report Region")

	// Add users to region
	addReportTestUserToRegion(db, adminID, regionID, true)
	addReportTestUserToRegion(db, admin2ID, regionID, true)
	addReportTestUserToRegion(db, admin3ID, regionID, true)
	addReportTestUserToRegion(db, memberID, regionID, false)
	addReportTestUserToRegion(db, member2ID, regionID, false)

	suite := &reportTestSuite{
		db:          db,
		handler:     handler,
		reportRepo:  reportRepo,
		regionRepo:  regionRepo,
		schoolRepo:  schoolRepo,
		userRepo:    userRepo,
		auditRepo:   auditRepo,
		regionID:    regionID,
		adminID:     adminID,
		admin2ID:    admin2ID,
		admin3ID:    admin3ID,
		memberID:    memberID,
		member2ID:   member2ID,
		superuserID: superuserID,
	}

	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM user_reports WHERE reporter_id IN (SELECT id FROM users WHERE username LIKE 'rpt-%')")
		_, _ = db.Exec("DELETE FROM audit_log WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'rpt-%')")
		_, _ = db.Exec("DELETE FROM user_regions WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'rpt-%')")
		_, _ = db.Exec("DELETE FROM geographic_regions WHERE name LIKE 'Test Report%'")
		_, _ = db.Exec("DELETE FROM users WHERE username LIKE 'rpt-%'")
		_ = db.Close()
	})

	return suite
}

func createReportTestUser(db *database.DB, username string) string {
	userID := uuid.New().String()
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO users (id, username, email, password_hash, verification_tier, postcard_verified, vouch_verified, is_superuser, email_verified, created_at)
		VALUES (?, ?, ?, 'hash', 1, TRUE, TRUE, FALSE, TRUE, NOW())
	`, userID, username, username+"@test.com")
	return userID
}

func createReportTestSuperuser(db *database.DB, username string) string {
	userID := uuid.New().String()
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO users (id, username, email, password_hash, verification_tier, postcard_verified, vouch_verified, is_superuser, email_verified, created_at)
		VALUES (?, ?, ?, 'hash', 1, TRUE, TRUE, TRUE, TRUE, NOW())
	`, userID, username, username+"@test.com")
	return userID
}

func createReportTestRegion(db *database.DB, creatorID, name string) string {
	regionID := uuid.New().String()
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO geographic_regions (id, name, region_type, created_by, created_at)
		VALUES (?, ?, 'neighborhood', ?, NOW())
	`, regionID, name, creatorID)
	return regionID
}

func addReportTestUserToRegion(db *database.DB, userID, regionID string, isAdmin bool) {
	id := uuid.New().String()
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO user_regions (id, user_id, region_id, is_admin, joined_at)
		VALUES (?, ?, ?, ?, NOW())
	`, id, userID, regionID, isAdmin)
}

// =============================================================================
// CreateReport Tests
// =============================================================================

func TestUserReportHandler_CreateReport_RegionScoped(t *testing.T) {
	suite := setupReportTestSuite(t)

	body := map[string]interface{}{
		"reported_user_id": suite.member2ID,
		"reason":           "harassment",
		"details":          "Test report details",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/communities/"+suite.regionID+"/reports", bytes.NewReader(jsonBody))
	q := req.URL.Query()
	q.Set("id", suite.regionID)
	q.Set("scope", "region")
	req.URL.RawQuery = q.Encode()

	claims := &middleware.Claims{
		UserID:           suite.memberID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.CreateReport(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp models.CreateReportResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if resp.ReportID == "" {
		t.Fatal("Expected non-empty report ID")
	}
	if resp.Status != "pending" {
		t.Fatalf("Expected status 'pending', got '%s'", resp.Status)
	}
}

func TestUserReportHandler_CreateReport_SelfReport(t *testing.T) {
	suite := setupReportTestSuite(t)

	body := map[string]interface{}{
		"reported_user_id": suite.memberID,
		"reason":           "harassment",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/communities/"+suite.regionID+"/reports", bytes.NewReader(jsonBody))
	q := req.URL.Query()
	q.Set("id", suite.regionID)
	q.Set("scope", "region")
	req.URL.RawQuery = q.Encode()

	claims := &middleware.Claims{
		UserID:           suite.memberID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.CreateReport(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for self-report, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUserReportHandler_CreateReport_InvalidReason(t *testing.T) {
	suite := setupReportTestSuite(t)

	body := map[string]interface{}{
		"reported_user_id": suite.member2ID,
		"reason":           "invalid_reason",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/communities/"+suite.regionID+"/reports", bytes.NewReader(jsonBody))
	q := req.URL.Query()
	q.Set("id", suite.regionID)
	q.Set("scope", "region")
	req.URL.RawQuery = q.Encode()

	claims := &middleware.Claims{
		UserID:           suite.memberID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.CreateReport(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for invalid reason, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUserReportHandler_CreateReport_RateLimit(t *testing.T) {
	suite := setupReportTestSuite(t)

	// Create 5 reports to hit the rate limit
	for i := 0; i < 5; i++ {
		report := &models.UserReport{
			ReporterID:     suite.memberID,
			ReportedUserID: suite.member2ID,
			RegionID:       &suite.regionID,
			Reason:         "spam",
		}
		if err := suite.reportRepo.Create(context.Background(), report); err != nil {
			t.Fatalf("Failed to create rate limit test report: %v", err)
		}
		// Dismiss each one so duplicate check doesn't fire
		_ = suite.reportRepo.UpdateStatus(context.Background(), report.ID, models.ReportStatusDismissed, &suite.adminID, nil, nil)
	}

	// 6th report should be rate limited
	body := map[string]interface{}{
		"reported_user_id": suite.member2ID,
		"reason":           "harassment",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/communities/"+suite.regionID+"/reports", bytes.NewReader(jsonBody))
	q := req.URL.Query()
	q.Set("id", suite.regionID)
	q.Set("scope", "region")
	req.URL.RawQuery = q.Encode()

	claims := &middleware.Claims{
		UserID:           suite.memberID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.CreateReport(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("Expected 429 for rate limit, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUserReportHandler_CreateReport_DuplicatePending(t *testing.T) {
	suite := setupReportTestSuite(t)

	// Create a pending report first
	report := &models.UserReport{
		ReporterID:     suite.adminID,
		ReportedUserID: suite.member2ID,
		RegionID:       &suite.regionID,
		Reason:         "spam",
	}
	if err := suite.reportRepo.Create(context.Background(), report); err != nil {
		t.Fatalf("Failed to create initial report: %v", err)
	}

	// Try to create another pending report from same reporter to same target in same scope
	body := map[string]interface{}{
		"reported_user_id": suite.member2ID,
		"reason":           "harassment",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/communities/"+suite.regionID+"/reports", bytes.NewReader(jsonBody))
	q := req.URL.Query()
	q.Set("id", suite.regionID)
	q.Set("scope", "region")
	req.URL.RawQuery = q.Encode()

	claims := &middleware.Claims{
		UserID:           suite.adminID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.CreateReport(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("Expected 409 for duplicate report, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUserReportHandler_CreateReport_ReporterNotInRegion(t *testing.T) {
	suite := setupReportTestSuite(t)

	// Create a user not in the region
	outsiderID := createReportTestUser(suite.db, "rpt-outsider")

	body := map[string]interface{}{
		"reported_user_id": suite.member2ID,
		"reason":           "harassment",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/communities/"+suite.regionID+"/reports", bytes.NewReader(jsonBody))
	q := req.URL.Query()
	q.Set("id", suite.regionID)
	q.Set("scope", "region")
	req.URL.RawQuery = q.Encode()

	claims := &middleware.Claims{
		UserID:           outsiderID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.CreateReport(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 for outsider, got %d: %s", rr.Code, rr.Body.String())
	}
}

// =============================================================================
// ListReports Tests
// =============================================================================

func TestUserReportHandler_ListReports_Admin(t *testing.T) {
	suite := setupReportTestSuite(t)

	// Create a report for the admin to see
	report := &models.UserReport{
		ReporterID:     suite.memberID,
		ReportedUserID: suite.member2ID,
		RegionID:       &suite.regionID,
		Reason:         "spam",
	}
	if err := suite.reportRepo.Create(context.Background(), report); err != nil {
		t.Fatalf("Failed to create report: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports", nil)
	claims := &middleware.Claims{
		UserID:           suite.adminID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.ListReports(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	reports, ok := resp["reports"].([]interface{})
	if !ok {
		t.Fatal("Expected reports array in response")
	}
	if len(reports) == 0 {
		t.Fatal("Expected at least one report for admin")
	}
}

func TestUserReportHandler_ListReports_Superuser(t *testing.T) {
	suite := setupReportTestSuite(t)

	// Create a report
	report := &models.UserReport{
		ReporterID:     suite.memberID,
		ReportedUserID: suite.member2ID,
		RegionID:       &suite.regionID,
		Reason:         "harassment",
	}
	if err := suite.reportRepo.Create(context.Background(), report); err != nil {
		t.Fatalf("Failed to create report: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports", nil)
	claims := &middleware.Claims{
		UserID:      suite.superuserID,
		IsSuperuser: true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.ListReports(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUserReportHandler_ListReports_NonAdmin(t *testing.T) {
	suite := setupReportTestSuite(t)

	// Create a user who is not admin of anything
	nonAdminID := createReportTestUser(suite.db, "rpt-nonadmin")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports", nil)
	claims := &middleware.Claims{
		UserID:           nonAdminID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.ListReports(rr, req)

	// Non-admins get 200 with empty reports (frontend route gate handles access control)
	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	reports, ok := resp["reports"].([]interface{})
	if !ok {
		t.Fatal("Expected reports array in response")
	}
	if len(reports) != 0 {
		t.Fatalf("Expected empty reports for non-admin, got %d", len(reports))
	}
}

// =============================================================================
// ResolveReport Tests
// =============================================================================

func TestUserReportHandler_ResolveReport_Dismiss(t *testing.T) {
	suite := setupReportTestSuite(t)

	// Create a pending report
	report := &models.UserReport{
		ReporterID:     suite.memberID,
		ReportedUserID: suite.member2ID,
		RegionID:       &suite.regionID,
		Reason:         "spam",
	}
	if err := suite.reportRepo.Create(context.Background(), report); err != nil {
		t.Fatalf("Failed to create report: %v", err)
	}

	body := map[string]interface{}{
		"action": "dismiss",
		"note":   "Not a valid concern",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/"+report.ID+"/resolve", bytes.NewReader(jsonBody))
	q := req.URL.Query()
	q.Set("id", report.ID)
	req.URL.RawQuery = q.Encode()

	claims := &middleware.Claims{
		UserID:           suite.adminID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.ResolveReport(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify the report was dismissed
	updatedReport, err := suite.reportRepo.GetByID(context.Background(), report.ID)
	if err != nil {
		t.Fatalf("Failed to get updated report: %v", err)
	}
	if updatedReport.Status != models.ReportStatusDismissed {
		t.Fatalf("Expected status '%s', got '%s'", models.ReportStatusDismissed, updatedReport.Status)
	}
}

func TestUserReportHandler_ResolveReport_InitiateBlocklist(t *testing.T) {
	suite := setupReportTestSuite(t)

	// Create a pending report
	report := &models.UserReport{
		ReporterID:     suite.memberID,
		ReportedUserID: suite.member2ID,
		RegionID:       &suite.regionID,
		Reason:         "harassment",
	}
	if err := suite.reportRepo.Create(context.Background(), report); err != nil {
		t.Fatalf("Failed to create report: %v", err)
	}

	body := map[string]interface{}{
		"action": "initiate_blocklist",
		"note":   "Escalating to blocklist",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/"+report.ID+"/resolve", bytes.NewReader(jsonBody))
	q := req.URL.Query()
	q.Set("id", report.ID)
	req.URL.RawQuery = q.Encode()

	claims := &middleware.Claims{
		UserID:           suite.adminID,
		PostcardVerified: true,
		VouchVerified:    true,
	}
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	suite.handler.ResolveReport(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify the report was resolved
	updatedReport, err := suite.reportRepo.GetByID(context.Background(), report.ID)
	if err != nil {
		t.Fatalf("Failed to get updated report: %v", err)
	}
	if updatedReport.Status != models.ReportStatusResolvedBlocklist {
		t.Fatalf("Expected status '%s', got '%s'", models.ReportStatusResolvedBlocklist, updatedReport.Status)
	}

	// Verify response includes region_id for redirect
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if resp["reported_user_id"] != suite.member2ID {
		t.Fatalf("Expected reported_user_id '%s', got '%v'", suite.member2ID, resp["reported_user_id"])
	}
	if resp["region_id"] != suite.regionID {
		t.Fatalf("Expected region_id '%s', got '%v'", suite.regionID, resp["region_id"])
	}
}
