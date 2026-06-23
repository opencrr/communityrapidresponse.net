package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// SchoolHandler handles school NCES search and school-to-group linking.
type SchoolHandler struct {
	schoolRepo   *database.SchoolRepository
	districtRepo *database.SchoolDistrictRepository
	groupRepo    *database.GroupRepository
	userRepo     *database.UserRepository
	auditRepo    *database.AuditRepository
	ncesService  services.NCESServiceInterface
}

// NewSchoolHandler creates a new school handler.
func NewSchoolHandler(
	schoolRepo *database.SchoolRepository,
	districtRepo *database.SchoolDistrictRepository,
	groupRepo *database.GroupRepository,
	userRepo *database.UserRepository,
	auditRepo *database.AuditRepository,
	ncesService services.NCESServiceInterface,
) *SchoolHandler {
	return &SchoolHandler{
		schoolRepo:   schoolRepo,
		districtRepo: districtRepo,
		groupRepo:    groupRepo,
		userRepo:     userRepo,
		auditRepo:    auditRepo,
		ncesService:  ncesService,
	}
}

// SchoolSearchResult extends SchoolSummary with linked group info.
type SchoolSearchResult struct {
	models.SchoolSummary
	GroupID *string `json:"group_id,omitempty"`
}

// SchoolSearchResultResponse wraps search results with pagination.
type SchoolSearchResultResponse struct {
	Schools []SchoolSearchResult `json:"schools"`
	Total   int                  `json:"total"`
	Page    int                  `json:"page"`
	Limit   int                  `json:"limit"`
	HasMore bool                 `json:"has_more"`
}

// Search handles GET /api/v1/schools?query=...&state=... (auth required)
func (h *SchoolHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	state := r.URL.Query().Get("state")

	page := 1
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if parsed, err := strconv.Atoi(pageStr); err == nil && parsed > 0 {
			page = parsed
		}
	}

	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			if parsed > 100 {
				parsed = 100
			}
			limit = parsed
		}
	}

	schools, totalCount, err := h.schoolRepo.Search(r.Context(), query, state, "", page, limit)
	if err != nil {
		writeServerError(w, r, err, "Failed to search schools", "school", "search_schools")
		return
	}

	if schools == nil {
		schools = []models.SchoolSummary{}
	}

	// Enrich with linked group IDs
	results := make([]SchoolSearchResult, len(schools))
	for i, school := range schools {
		results[i] = SchoolSearchResult{SchoolSummary: school}
		linkedGroup, groupErr := h.groupRepo.GetBySchoolID(r.Context(), school.ID)
		if groupErr != nil {
			slog.WarnContext(r.Context(), "failed to look up school group", "school_id", school.ID, "error", groupErr)
			continue
		}
		if linkedGroup != nil {
			results[i].GroupID = &linkedGroup.ID
		}
	}

	writeJSON(w, http.StatusOK, SchoolSearchResultResponse{
		Schools: results,
		Total:   totalCount,
		Page:    page,
		Limit:   limit,
		HasMore: (page * limit) < totalCount,
	})
}

// Get handles GET /api/v1/schools/:id (auth required)
// Returns school details with linked group info.
func (h *SchoolHandler) Get(w http.ResponseWriter, r *http.Request) {
	schoolID := getPathParam(r, "id")
	if schoolID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "School ID required")
		return
	}

	school, err := h.schoolRepo.GetByID(r.Context(), schoolID)
	if err != nil {
		if errors.Is(err, database.ErrSchoolNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "School not found")
			return
		}
		writeServerError(w, r, err, "Failed to get school", "school", "get_school")
		return
	}

	// Look up linked group
	var groupID *string
	linkedGroup, groupErr := h.groupRepo.GetBySchoolID(r.Context(), schoolID)
	if groupErr != nil {
		slog.WarnContext(r.Context(), "failed to look up school group", "school_id", schoolID, "error", groupErr)
	}
	if linkedGroup != nil {
		groupID = &linkedGroup.ID
	}

	// Build district name
	var districtName string
	if school.DistrictID != nil {
		district, dErr := h.districtRepo.GetByID(r.Context(), *school.DistrictID)
		if dErr == nil && district != nil {
			districtName = district.Name
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":            school.ID,
		"nces_id":       school.NCESID,
		"district_id":   school.DistrictID,
		"name":          school.Name,
		"street_address": school.StreetAddress,
		"city":          school.City,
		"state":         school.State,
		"zip":           school.Zip,
		"latitude":      school.Latitude,
		"longitude":     school.Longitude,
		"region_id":     school.RegionID,
		"district_name": districtName,
		"group_id":      groupID,
	})
}

// Join handles POST /api/v1/schools/:id/join (auth required)
// If a group exists for this school, join it. Otherwise create one.
func (h *SchoolHandler) Join(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	schoolID := getPathParam(r, "id")
	if schoolID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "School ID required")
		return
	}

	// Verify school exists
	school, err := h.schoolRepo.GetByID(r.Context(), schoolID)
	if err != nil {
		if errors.Is(err, database.ErrSchoolNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "School not found")
			return
		}
		writeServerError(w, r, err, "Failed to get school", "school", "join_school")
		return
	}

	// Check if a group already exists for this school
	existingGroup, err := h.groupRepo.GetBySchoolID(r.Context(), schoolID)
	if err != nil {
		writeServerError(w, r, err, "Failed to check school group", "school", "join_school")
		return
	}

	if existingGroup != nil {
		// Group exists: check membership first, then join
		isMember, memberErr := h.groupRepo.IsUserMember(r.Context(), existingGroup.ID, claims.UserID)
		if memberErr != nil {
			writeServerError(w, r, memberErr, "Failed to check membership", "school", "join_school")
			return
		}
		if isMember {
			writeError(w, http.StatusConflict, "already_member", "You are already a member of this school's group")
			return
		}
		if addErr := h.groupRepo.AddMember(r.Context(), existingGroup.ID, claims.UserID, false, false); addErr != nil {
			writeServerError(w, r, addErr, "Failed to join school group", "school", "join_school")
			return
		}

		// Audit log
		if h.auditRepo != nil {
			resourceType := "group"
			logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSchoolJoined, &resourceType, &existingGroup.ID, map[string]interface{}{
				"school_id": schoolID,
				"group_id":  existingGroup.ID,
			}), "school_joined")
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"group_id":  existingGroup.ID,
			"school_id": schoolID,
			"status":    "joined",
		})
		return
	}

	// No group exists: create one linked to this school
	description := ""
	if school.City != nil {
		description = *school.City
	}
	if school.State != "" {
		if description != "" {
			description += ", "
		}
		description += school.State
	}

	createReq := &models.CreateGroupRequest{
		Name:        school.Name,
		Description: description,
		Visibility:  "unlisted",
		SchoolID:    &schoolID,
	}

	newGroup, err := h.groupRepo.Create(r.Context(), createReq, claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to create school group", "school", "create_school_group")
		return
	}

	// Audit log
	if h.auditRepo != nil {
		resourceType := "group"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionSchoolJoined, &resourceType, &newGroup.ID, map[string]interface{}{
			"school_id":     schoolID,
			"group_id":      newGroup.ID,
			"group_created": true,
		}), "school_group_created")
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"group_id":      newGroup.ID,
		"school_id":     schoolID,
		"status":        "created",
		"group_created": true,
	})
}

// ListMySchools handles GET /api/v1/schools/my (auth required)
// Returns groups where school_id IS NOT NULL for the current user.
func (h *SchoolHandler) ListMySchools(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	groups, err := h.groupRepo.ListByUser(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list school groups", "school", "list_my_schools")
		return
	}

	// Filter to only school-linked groups and enrich with school data
	type MySchoolGroup struct {
		GroupID     string                `json:"group_id"`
		GroupName   string                `json:"group_name"`
		SchoolID   string                `json:"school_id"`
		SchoolName string                `json:"school_name"`
		City       string                `json:"city,omitempty"`
		State      string                `json:"state"`
		IsAdmin    bool                  `json:"is_admin"`
		MemberCount int                  `json:"member_count"`
		Status     models.GroupStatus    `json:"status"`
	}

	var schoolGroups []MySchoolGroup
	for _, g := range groups {
		if g.SchoolID == nil {
			continue
		}
		school, schoolErr := h.schoolRepo.GetByID(r.Context(), *g.SchoolID)
		if schoolErr != nil {
			slog.WarnContext(r.Context(), "failed to get school for group", "school_id", *g.SchoolID, "error", schoolErr)
			continue
		}
		city := ""
		if school.City != nil {
			city = *school.City
		}
		schoolGroups = append(schoolGroups, MySchoolGroup{
			GroupID:     g.ID,
			GroupName:   g.Name,
			SchoolID:    school.ID,
			SchoolName:  school.Name,
			City:        city,
			State:       school.State,
			IsAdmin:     g.IsUserAdmin,
			MemberCount: g.MemberCount,
			Status:      g.Status,
		})
	}

	if schoolGroups == nil {
		schoolGroups = []MySchoolGroup{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"schools": schoolGroups,
	})
}

// SearchDistricts handles GET /api/v1/school-districts (auth required)
func (h *SchoolHandler) SearchDistricts(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	state := r.URL.Query().Get("state")

	districts, err := h.districtRepo.Search(r.Context(), query, state)
	if err != nil {
		writeServerError(w, r, err, "Failed to search districts", "school", "search_districts")
		return
	}

	if districts == nil {
		districts = []models.SchoolDistrict{}
	}

	writeJSON(w, http.StatusOK, models.DistrictSearchResponse{
		Districts: districts,
	})
}

// GetDistrict handles GET /api/v1/school-districts/:id (auth required)
func (h *SchoolHandler) GetDistrict(w http.ResponseWriter, r *http.Request) {
	districtID := getPathParam(r, "id")
	if districtID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "District ID required")
		return
	}

	district, err := h.districtRepo.GetByID(r.Context(), districtID)
	if err != nil {
		if errors.Is(err, database.ErrDistrictNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "District not found")
			return
		}
		writeServerError(w, r, err, "Failed to get district", "school", "get_district")
		return
	}

	// Lazy-load district boundary from NCES if not yet cached
	if district.Geometry == nil && district.NCESID != "" && h.ncesService != nil {
		geoJSONStr, fetchErr := h.ncesService.FetchDistrictBoundary(r.Context(), district.NCESID)
		if fetchErr != nil {
			slog.WarnContext(r.Context(), "failed to fetch district boundary", "nces_id", district.NCESID, "error", fetchErr)
		} else if geoJSONStr != "" {
			if storeErr := h.districtRepo.UpdateGeometry(r.Context(), district.NCESID, geoJSONStr); storeErr != nil {
				slog.WarnContext(r.Context(), "failed to store district boundary", "nces_id", district.NCESID, "error", storeErr)
			}
			var geom models.GeoJSONGeometry
			if jsonErr := json.Unmarshal([]byte(geoJSONStr), &geom); jsonErr == nil {
				district.Geometry = &geom
			}
		}
	}

	schools, err := h.schoolRepo.ListByDistrict(r.Context(), districtID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list schools in district", "school", "list_schools_in_district")
		return
	}

	if schools == nil {
		schools = []models.SchoolSummary{}
	}

	writeJSON(w, http.StatusOK, models.DistrictWithDetails{
		SchoolDistrict: *district,
		Schools:        schools,
	})
}
