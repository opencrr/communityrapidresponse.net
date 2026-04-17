package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// UserReportHandler handles user report endpoints
type UserReportHandler struct {
	db         *database.DB
	reportRepo *database.UserReportRepository
	regionRepo *database.RegionRepository
	schoolRepo *database.SchoolRepository
	userRepo   *database.UserRepository
	auditRepo  *database.AuditRepository
}

// NewUserReportHandler creates a new user report handler
func NewUserReportHandler(
	db *database.DB,
	reportRepo *database.UserReportRepository,
	regionRepo *database.RegionRepository,
	schoolRepo *database.SchoolRepository,
	userRepo *database.UserRepository,
	auditRepo *database.AuditRepository,
) *UserReportHandler {
	return &UserReportHandler{
		db:         db,
		reportRepo: reportRepo,
		regionRepo: regionRepo,
		schoolRepo: schoolRepo,
		userRepo:   userRepo,
		auditRepo:  auditRepo,
	}
}

// CreateReport handles POST requests to create a user report
func (h *UserReportHandler) CreateReport(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Parse scope from query params (set by router)
	scopeType := r.URL.Query().Get("scope")
	scopeID := r.URL.Query().Get("id")

	if scopeType == "" || scopeID == "" {
		writeError(w, http.StatusBadRequest, "missing_scope", "Scope type and ID are required")
		return
	}

	// Decode request body
	var req models.CreateReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate reason
	if !models.ValidReportReasons[req.Reason] {
		writeError(w, http.StatusBadRequest, "invalid_reason", "Invalid report reason")
		return
	}

	// Validate not self-report
	if req.ReportedUserID == claims.UserID {
		writeError(w, http.StatusBadRequest, "self_report", "You cannot report yourself")
		return
	}

	// Validate target user exists
	targetUser, err := h.userRepo.GetByID(r.Context(), req.ReportedUserID)
	if err != nil || targetUser == nil {
		writeError(w, http.StatusBadRequest, "user_not_found", "Reported user not found")
		return
	}

	// Build the report with scope
	report := &models.UserReport{
		ReporterID:     claims.UserID,
		ReportedUserID: req.ReportedUserID,
		Reason:         req.Reason,
		Details:        req.Details,
	}

	// Validate scope-specific membership for both reporter and target
	switch scopeType {
	case "region":
		report.RegionID = &scopeID

		// Verify reporter is in region
		reporterInRegion, err := h.regionRepo.IsUserInRegion(r.Context(), claims.UserID, scopeID)
		if err != nil {
			writeServerError(w, r, err, "Community membership could not be verified. Please try again.", "reports", "check_reporter_region")
			return
		}
		if !reporterInRegion {
			writeError(w, http.StatusForbidden, "not_in_region", "You are not a member of this community")
			return
		}

		// Verify target is in region
		targetInRegion, err := h.regionRepo.IsUserInRegion(r.Context(), req.ReportedUserID, scopeID)
		if err != nil {
			writeServerError(w, r, err, "Community membership could not be verified. Please try again.", "reports", "check_target_region")
			return
		}
		if !targetInRegion {
			writeError(w, http.StatusBadRequest, "target_not_in_region", "Reported user is not a member of this community")
			return
		}

	case "school":
		report.SchoolID = &scopeID

		// Verify reporter is a member of the school
		reporterSchool, err := h.schoolRepo.GetUserSchool(r.Context(), claims.UserID, scopeID)
		if err != nil {
			writeServerError(w, r, err, "School membership could not be verified. Please try again.", "reports", "check_reporter_school")
			return
		}
		if reporterSchool == nil {
			writeError(w, http.StatusForbidden, "not_in_school", "You are not a member of this school")
			return
		}

		// Verify target is a member of the school
		targetSchool, err := h.schoolRepo.GetUserSchool(r.Context(), req.ReportedUserID, scopeID)
		if err != nil {
			writeServerError(w, r, err, "School membership could not be verified. Please try again.", "reports", "check_target_school")
			return
		}
		if targetSchool == nil {
			writeError(w, http.StatusBadRequest, "target_not_in_school", "Reported user is not a member of this school")
			return
		}

	case "district":
		report.DistrictID = &scopeID

		// Verify reporter is a member of a school in the district
		reporterInDistrict, err := h.isUserInDistrict(r, claims.UserID, scopeID)
		if err != nil {
			writeServerError(w, r, err, "District membership could not be verified. Please try again.", "reports", "check_reporter_district")
			return
		}
		if !reporterInDistrict {
			writeError(w, http.StatusForbidden, "not_in_district", "You are not a member of any school in this district")
			return
		}

		// Verify target is a member of a school in the district
		targetInDistrict, err := h.isUserInDistrict(r, req.ReportedUserID, scopeID)
		if err != nil {
			writeServerError(w, r, err, "District membership could not be verified. Please try again.", "reports", "check_target_district")
			return
		}
		if !targetInDistrict {
			writeError(w, http.StatusBadRequest, "target_not_in_district", "Reported user is not a member of any school in this district")
			return
		}

	default:
		writeError(w, http.StatusBadRequest, "invalid_scope", "Scope must be region, school, or district")
		return
	}

	// Rate limit: 5 reports per week
	weekCount, err := h.reportRepo.CountReportsThisWeek(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Report limit could not be verified. Please try again.", "reports", "count_reports")
		return
	}
	if weekCount >= 5 {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "You can submit a maximum of 5 reports per week")
		return
	}

	// Duplicate check
	existing, err := h.reportRepo.GetPendingByReporterAndTarget(r.Context(), claims.UserID, req.ReportedUserID, report.RegionID, report.SchoolID, report.DistrictID)
	if err != nil {
		writeServerError(w, r, err, "Existing reports could not be checked. Please try again.", "reports", "duplicate_check")
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, "duplicate_report", "You already have a pending report against this user in this scope")
		return
	}

	// Create the report
	if err := h.reportRepo.Create(r.Context(), report); err != nil {
		writeServerError(w, r, err, "Report could not be submitted. Please try again.", "reports", "create_report")
		return
	}

	// Audit log
	resourceType := "user_report"
	logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionReportCreated, &resourceType, &report.ID, map[string]interface{}{
		"reported_user_id": req.ReportedUserID,
		"scope_type":       scopeType,
		"scope_id":         scopeID,
		"reason":           req.Reason,
	}), "report_created")

	writeJSON(w, http.StatusCreated, models.CreateReportResponse{
		ReportID: report.ID,
		Status:   report.Status,
		Message:  "Report submitted successfully",
	})
}

// ListReports handles GET /api/v1/reports
func (h *UserReportHandler) ListReports(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	filter := models.ReportListFilter{
		Status:     r.URL.Query().Get("status"),
		RegionID:   r.URL.Query().Get("region_id"),
		SchoolID:   r.URL.Query().Get("school_id"),
		DistrictID: r.URL.Query().Get("district_id"),
	}

	var allReports []*models.ReportSummary

	if claims.IsSuperuser {
		reports, err := h.reportRepo.ListAll(r.Context(), filter)
		if err != nil {
			writeServerError(w, r, err, "Reports could not be loaded. Please try again.", "reports", "list_all")
			return
		}
		allReports = reports
	} else {
		// Check if user is admin of any regions
		regionReports, err := h.reportRepo.ListByRegionAdmin(r.Context(), claims.UserID, filter)
		if err != nil {
			writeServerError(w, r, err, "Reports could not be loaded. Please try again.", "reports", "list_region_reports")
			return
		}

		// Check if user is admin of any schools/districts
		schoolReports, err := h.reportRepo.ListBySchoolAdmin(r.Context(), claims.UserID, filter)
		if err != nil {
			writeServerError(w, r, err, "Reports could not be loaded. Please try again.", "reports", "list_school_reports")
			return
		}

		// Merge results, dedup by ID
		seen := make(map[string]bool)
		for _, report := range regionReports {
			if !seen[report.ID] {
				allReports = append(allReports, report)
				seen[report.ID] = true
			}
		}
		for _, report := range schoolReports {
			if !seen[report.ID] {
				allReports = append(allReports, report)
				seen[report.ID] = true
			}
		}

	}

	if allReports == nil {
		allReports = []*models.ReportSummary{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"reports": allReports,
	})
}

// GetReport handles GET /api/v1/reports/:id
func (h *UserReportHandler) GetReport(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	reportID := r.URL.Query().Get("id")
	if reportID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Report ID required")
		return
	}

	// Get report to check access
	report, err := h.reportRepo.GetByID(r.Context(), reportID)
	if err != nil {
		if errors.Is(err, database.ErrReportNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Report not found")
			return
		}
		writeServerError(w, r, err, "Report could not be retrieved. Please try again.", "reports", "get_report")
		return
	}

	// Verify access: must be admin of report's scope or superuser
	if !claims.IsSuperuser {
		hasAccess, err := h.isAdminForReport(r, claims.UserID, report)
		if err != nil {
			writeServerError(w, r, err, "Your permissions could not be verified. Please try again.", "reports", "check_admin_access")
			return
		}
		if !hasAccess {
			writeError(w, http.StatusForbidden, "not_admin", "You must be an admin of this scope to view reports")
			return
		}
	}

	// Get full details
	detail, err := h.reportRepo.GetByIDWithDetails(r.Context(), reportID)
	if err != nil {
		writeServerError(w, r, err, "Report details could not be retrieved. Please try again.", "reports", "get_report_details")
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// ResolveReport handles POST /api/v1/reports/:id/resolve
func (h *UserReportHandler) ResolveReport(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	reportID := r.URL.Query().Get("id")
	if reportID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Report ID required")
		return
	}

	// Get report
	report, err := h.reportRepo.GetByID(r.Context(), reportID)
	if err != nil {
		if errors.Is(err, database.ErrReportNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Report not found")
			return
		}
		writeServerError(w, r, err, "Report could not be retrieved. Please try again.", "reports", "get_report")
		return
	}

	// Check report is pending
	if report.Status != models.ReportStatusPending {
		writeError(w, http.StatusBadRequest, "already_resolved", "This report has already been resolved")
		return
	}

	// Verify access: must be admin of report's scope or superuser
	if !claims.IsSuperuser {
		hasAccess, err := h.isAdminForReport(r, claims.UserID, report)
		if err != nil {
			writeServerError(w, r, err, "Your permissions could not be verified. Please try again.", "reports", "check_admin_access")
			return
		}
		if !hasAccess {
			writeError(w, http.StatusForbidden, "not_admin", "You must be an admin of this scope to resolve reports")
			return
		}
	}

	// Decode request
	var req models.ResolveReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	switch req.Action {
	case "dismiss":
		err = h.reportRepo.UpdateStatus(r.Context(), reportID, models.ReportStatusDismissed, &claims.UserID, req.Note, nil)
		if err != nil {
			writeServerError(w, r, err, "Report could not be dismissed. Please try again.", "reports", "dismiss_report")
			return
		}

		resolveResourceType := "user_report"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionReportDismissed, &resolveResourceType, &reportID, map[string]interface{}{
			"reported_user_id": report.ReportedUserID,
		}), "report_dismissed")

		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "dismissed",
			"message": "Report has been dismissed",
		})

	case "initiate_blocklist":
		err = h.reportRepo.UpdateStatus(r.Context(), reportID, models.ReportStatusResolvedBlocklist, &claims.UserID, req.Note, nil)
		if err != nil {
			writeServerError(w, r, err, "Report could not be resolved. Please try again.", "reports", "resolve_blocklist")
			return
		}

		blocklistResourceType := "user_report"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionReportResolvedBlocklist, &blocklistResourceType, &reportID, map[string]interface{}{
			"reported_user_id": report.ReportedUserID,
		}), "report_resolved_blocklist")

		// Return info for frontend to redirect to blocklist proposal creation
		response := map[string]interface{}{
			"status":           "resolved_blocklist",
			"message":          "Report resolved. Redirecting to create blocklist proposal.",
			"reported_user_id": report.ReportedUserID,
		}
		if report.RegionID != nil {
			response["region_id"] = *report.RegionID
		}
		if report.SchoolID != nil {
			response["school_id"] = *report.SchoolID
		}
		if report.DistrictID != nil {
			response["district_id"] = *report.DistrictID
		}

		writeJSON(w, http.StatusOK, response)

	default:
		writeError(w, http.StatusBadRequest, "invalid_action", "Action must be 'dismiss' or 'initiate_blocklist'")
	}
}

// isAdminForReport checks if a user is an admin in the scope of the report
func (h *UserReportHandler) isAdminForReport(r *http.Request, userID string, report *models.UserReport) (bool, error) {
	if report.RegionID != nil {
		return h.regionRepo.IsUserAdmin(r.Context(), userID, *report.RegionID)
	}
	if report.SchoolID != nil {
		userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), userID, *report.SchoolID)
		if err != nil {
			return false, err
		}
		return userSchool != nil && userSchool.IsAdmin && userSchool.VerificationStatus == models.SchoolVerificationStatusVerified, nil
	}
	if report.DistrictID != nil {
		return h.isDistrictAdmin(r, userID, *report.DistrictID)
	}
	return false, nil
}

// isDistrictAdmin checks if a user is a verified admin of any school in the district
func (h *UserReportHandler) isDistrictAdmin(r *http.Request, userID, districtID string) (bool, error) {
	districtSchools, err := h.schoolRepo.ListByDistrict(r.Context(), districtID)
	if err != nil {
		return false, err
	}
	for _, schoolSummary := range districtSchools {
		userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), userID, schoolSummary.ID)
		if err == nil && userSchool != nil && userSchool.IsAdmin && userSchool.VerificationStatus == models.SchoolVerificationStatusVerified {
			return true, nil
		}
	}
	return false, nil
}

// isUserInDistrict checks if a user is a member of any school in the district
func (h *UserReportHandler) isUserInDistrict(r *http.Request, userID, districtID string) (bool, error) {
	districtSchools, err := h.schoolRepo.ListByDistrict(r.Context(), districtID)
	if err != nil {
		return false, err
	}
	for _, schoolSummary := range districtSchools {
		userSchool, err := h.schoolRepo.GetUserSchool(r.Context(), userID, schoolSummary.ID)
		if err == nil && userSchool != nil {
			return true, nil
		}
	}
	return false, nil
}
