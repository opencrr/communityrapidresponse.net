package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

// RegionHandler handles region endpoints
type RegionHandler struct {
	regionRepo    *database.RegionRepository
	mapboxService services.MapboxServiceInterface
	auditRepo     *database.AuditRepository
}

// NewRegionHandler creates a new region handler
func NewRegionHandler(regionRepo *database.RegionRepository, mapboxService services.MapboxServiceInterface, auditRepo *database.AuditRepository) *RegionHandler {
	return &RegionHandler{
		regionRepo:    regionRepo,
		mapboxService: mapboxService,
		auditRepo:     auditRepo,
	}
}

// List handles GET /api/v1/communities
// Superusers see all regions; regular users only see regions they belong to
func (h *RegionHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())

	// Authentication required
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Superusers can see all regions and use filters
	if claims.IsSuperuser {
		// Parse query parameters
		var regionType *models.RegionType
		if t := r.URL.Query().Get("type"); t != "" {
			rt := models.RegionType(t)
			regionType = &rt
		}

		var parentID *string
		if p := r.URL.Query().Get("parent_id"); p != "" {
			parentID = &p
		}

		// Check for lat/lng to find regions containing a point
		latStr := r.URL.Query().Get("lat")
		lngStr := r.URL.Query().Get("lng")

		if latStr != "" && lngStr != "" {
			lat, err := strconv.ParseFloat(latStr, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_parameter", "Invalid latitude")
				return
			}
			lng, err := strconv.ParseFloat(lngStr, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_parameter", "Invalid longitude")
				return
			}

			regions, err := h.regionRepo.GetRegionsContainingPoint(r.Context(), lat, lng)
			if err != nil {
				writeServerError(w, r, err, "Failed to query regions", "region", "list_regions")
				return
			}

			writeJSON(w, http.StatusOK, models.RegionListResponse{Regions: regions})
			return
		}

		// List all regions with optional filters
		regions, err := h.regionRepo.List(r.Context(), regionType, parentID)
		if err != nil {
			writeServerError(w, r, err, "Failed to list regions", "region", "list_regions")
			return
		}

		writeJSON(w, http.StatusOK, models.RegionListResponse{Regions: regions})
		return
	}

	// Regular users only see regions they belong to
	regions, err := h.regionRepo.ListForUser(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list regions", "region", "list_regions")
		return
	}

	writeJSON(w, http.StatusOK, models.RegionListResponse{Regions: regions})
}

// ListAdmin handles GET /api/v1/communities/admin
// Returns only regions where the user is an admin
func (h *RegionHandler) ListAdmin(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())

	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Must have both postcard and vouch verification (or be superuser)
	if !claims.IsSuperuser && (!claims.PostcardVerified || !claims.VouchVerified) {
		writeError(w, http.StatusForbidden, "forbidden", "Admin access required (both postcard and vouch verification needed)")
		return
	}

	// Superusers see all regions
	if claims.IsSuperuser {
		regions, err := h.regionRepo.List(r.Context(), nil, nil)
		if err != nil {
			writeServerError(w, r, err, "Failed to list regions", "region", "list_regions")
			return
		}
		writeJSON(w, http.StatusOK, models.RegionListResponse{Regions: regions})
		return
	}

	// Regular admins see only regions they're admin of
	regions, err := h.regionRepo.ListAdminRegions(r.Context(), claims.UserID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list admin regions", "region", "list_regions")
		return
	}

	writeJSON(w, http.StatusOK, models.RegionListResponse{Regions: regions})
}

// Get handles GET /api/v1/communities/:id
// Superusers can see any region; regular users can only see regions they belong to
func (h *RegionHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())

	// Authentication required
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	id := getPathParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Community ID required")
		return
	}

	// Determine userID for filtering sub-regions
	// Superusers see all sub-regions (empty userID), regular users see only their accessible sub-regions
	filterUserID := ""
	if !claims.IsSuperuser {
		// Check membership first for regular users
		isMember, err := h.regionRepo.IsUserInRegion(r.Context(), claims.UserID, id)
		if err != nil {
			writeServerError(w, r, err, "Failed to check region access", "region", "get_region")
			return
		}
		if !isMember {
			writeError(w, http.StatusForbidden, "forbidden", "You do not have access to this region")
			return
		}
		filterUserID = claims.UserID
	}

	region, err := h.regionRepo.GetByIDWithDetails(r.Context(), id, filterUserID)
	if errors.Is(err, database.ErrRegionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Community not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get region", "region", "get_region")
		return
	}

	// Add bootstrap mode info
	bootstrapMode, fullAdminCount, err := h.regionRepo.IsRegionInBootstrapMode(r.Context(), id)
	if err != nil {
		slog.Warn("failed to check bootstrap mode", "error", err)
		// Non-fatal: continue with default values
	} else {
		region.BootstrapMode = bootstrapMode
		region.FullAdminCount = fullAdminCount
	}

	// Set membership flag — regular users already passed the membership check above;
	// for superusers, check explicitly
	if claims.IsSuperuser {
		isMember, err := h.regionRepo.IsUserInRegion(r.Context(), claims.UserID, id)
		if err != nil {
			slog.Warn("failed to check superuser membership", "error", err)
		} else {
			region.IsMember = isMember
		}
	} else {
		region.IsMember = true
	}

	writeJSON(w, http.StatusOK, region)
}

// Create handles POST /api/v1/communities (admin only)
func (h *RegionHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Debug logging for region creation authorization
	slog.Debug("region create authorization", "user_id", claims.UserID, "email", claims.Email, "is_superuser", claims.IsSuperuser, "postcard_verified", claims.PostcardVerified, "vouch_verified", claims.VouchVerified, "tier", claims.VerificationTier)

	// Require at least Tier 1 (postcard verified)
	if claims.VerificationTier < models.TierPostcard {
		writeError(w, http.StatusForbidden, "forbidden", "Postcard verification required to create communities")
		return
	}

	var req models.CreateRegionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate request
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Community name is required")
		return
	}

	if req.Geometry == nil {
		writeError(w, http.StatusBadRequest, "validation_error", "Community geometry is required")
		return
	}

	// Convert geometry to GeoJSON string
	geoJSON, err := json.Marshal(req.Geometry)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", "Invalid geometry format")
		return
	}

	// Validate that the region is within US bounds (superusers can create regions anywhere)
	if !claims.IsSuperuser {
		withinUS, err := h.regionRepo.IsGeometryWithinUS(r.Context(), string(geoJSON))
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", "Invalid geometry - could not validate location")
			return
		}
		if !withinUS {
			writeError(w, http.StatusBadRequest, "validation_error", "Community must be within the United States or its territories")
			return
		}
	}

	// Validate that the region is within a region the user has admin access to
	// Superusers bypass this check and can create regions anywhere
	if !claims.IsSuperuser {
		contained, err := h.regionRepo.IsContainedInAdminRegion(r.Context(), claims.UserID, string(geoJSON))
		if err != nil {
			writeServerError(w, r, err, "Failed to validate region boundary", "region", "admin_containment_check")
			return
		}
		if !contained {
			// Get user's admin regions to include in error response
			adminRegions, err := h.regionRepo.GetUserAdminRegions(r.Context(), claims.UserID)
			if err != nil {
				slog.Warn("failed to get admin regions for error response", "error", err)
				adminRegions = nil
			}

			regionList := make([]map[string]string, 0, len(adminRegions))
			for _, region := range adminRegions {
				regionList = append(regionList, map[string]string{
					"id":   region.ID,
					"name": region.Name,
					"type": string(region.RegionType),
				})
			}

			writeJSON(w, http.StatusForbidden, map[string]interface{}{
				"error":         "outside_admin_regions",
				"message":       "Community must be within a community you have admin access to",
				"admin_regions": regionList,
			})
			return
		}
	}

	// Auto-create parent regions if needed (for county and city types)
	parentRegionID := req.ParentRegionID
	if parentRegionID == nil || *parentRegionID == "" {
		autoParentID, err := h.autoCreateParentRegions(r.Context(), req, claims.UserID)
		if err != nil {
			slog.Error("failed to auto-create parent regions", "error", err)
			_ = appSentry.CaptureErrorWithContext(r.Context(), err, "component", "region", "operation", "auto_create_parents")
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		parentRegionID = autoParentID
	}

	// Validate type/parent hierarchy
	if err := h.validateTypeParentHierarchy(r, req.Type, parentRegionID); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}

	// Create region
	region := &models.GeographicRegion{
		Name:           req.Name,
		RegionType:     req.Type,
		ParentRegionID: parentRegionID,
		CreatedBy:      &claims.UserID,
	}

	if err := h.regionRepo.Create(r.Context(), region, string(geoJSON)); err != nil {
		writeServerError(w, r, err, "Failed to create region", "region", "create_region")
		return
	}

	// Add creator as admin of the region
	if err := h.regionRepo.AddUserToRegion(r.Context(), claims.UserID, region.ID, true); err != nil {
		slog.Warn("failed to add creator as admin", "error", err)
	}

	// Audit log: region created
	if h.auditRepo != nil {
		resourceType := "region"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionRegionCreated, &resourceType, &region.ID, map[string]interface{}{
			"name":        region.Name,
			"region_type": string(region.RegionType),
			"parent_id":   parentRegionID,
		}), "region_created")
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"region_id":  region.ID,
		"name":       region.Name,
		"created_by": claims.UserID,
	})
}

// Update handles PUT /api/v1/communities/:id (admin only)
func (h *RegionHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	id := getPathParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Community ID required")
		return
	}

	// Check if region exists
	_, err := h.regionRepo.GetByID(r.Context(), id)
	if errors.Is(err, database.ErrRegionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Community not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get region", "region", "update_region")
		return
	}

	// Check if user is admin of this region (or superuser)
	if !claims.IsSuperuser {
		isAdmin, err := h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, id)
		if err != nil {
			writeServerError(w, r, err, "Failed to check permissions", "region", "update_region")
			return
		}
		if !isAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this community to update it")
			return
		}
	}

	var req models.UpdateRegionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Community name is required")
		return
	}

	if err := h.regionRepo.Update(r.Context(), id, req.Name); err != nil {
		writeServerError(w, r, err, "Failed to update region", "region", "update_region")
		return
	}

	// Audit log: region updated
	if h.auditRepo != nil {
		resourceType := "region"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionRegionUpdated, &resourceType, &id, map[string]interface{}{
			"new_name": req.Name,
		}), "region_updated")
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Community updated successfully",
		"id":      id,
		"name":    req.Name,
	})
}

// Delete handles DELETE /api/v1/communities/:id (superuser only)
func (h *RegionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Only superusers can delete communities
	if !claims.IsSuperuser {
		writeError(w, http.StatusForbidden, "forbidden", "Only superusers can delete communities")
		return
	}

	id := getPathParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Community ID required")
		return
	}

	// Check if region exists
	region, err := h.regionRepo.GetByID(r.Context(), id)
	if errors.Is(err, database.ErrRegionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Community not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get region", "region", "delete_region")
		return
	}

	if err := h.regionRepo.Delete(r.Context(), id); err != nil {
		writeServerError(w, r, err, "Failed to delete region", "region", "delete_region")
		return
	}

	// Audit log: region deleted
	if h.auditRepo != nil {
		resourceType := "region"
		logAuditError(r, h.auditRepo.Log(r.Context(), &claims.UserID, models.AuditActionRegionDeleted, &resourceType, &id, map[string]interface{}{
			"name":        region.Name,
			"region_type": string(region.RegionType),
		}), "region_deleted")
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Community deleted successfully",
		"id":      id,
		"name":    region.Name,
	})
}

// GetAdminCount returns the number of admins for a region (used by other handlers)
func (h *RegionHandler) GetAdminCount(r *http.Request, regionID string) (int, error) {
	return h.regionRepo.GetAdminCount(r.Context(), regionID)
}

// IsUserAdmin checks if a user is an admin (exported for middleware)
func (h *RegionHandler) IsUserAdmin(r *http.Request, userID, regionID string) (bool, error) {
	return h.regionRepo.IsUserAdmin(r.Context(), userID, regionID)
}

// autoCreateParentRegions auto-creates parent regions for county and city types
// Returns the parent region ID to use, or nil if no parent is needed
// State and county names are auto-detected from the geometry location using reverse geocoding
func (h *RegionHandler) autoCreateParentRegions(ctx context.Context, req models.CreateRegionRequest, userID string) (*string, error) {
	// Calculate centroid of the geometry for boundary lookups
	centroid := calculateGeometryCentroid(req.Geometry)
	lat := centroid[1]
	lng := centroid[0]

	switch req.Type {
	case models.RegionTypeState:
		// State has no parent
		return nil, nil

	case models.RegionTypeCounty:
		// County needs a State parent - detect state from location
		stateName := req.StateName
		if stateName == "" {
			// Auto-detect state from coordinates using reverse geocoding
			detectedState, err := h.detectStateFromCoordinates(ctx, lat, lng)
			if err != nil {
				return nil, errors.New("could not determine state from location: " + err.Error())
			}
			stateName = detectedState
			slog.Info("auto-detected state", "state", stateName)
		}

		stateID, err := h.getOrCreateStateRegion(ctx, stateName, lat, lng, userID)
		if err != nil {
			return nil, err
		}
		return &stateID, nil

	case models.RegionTypeCity:
		// City needs County and State parents - detect both from location
		stateName := req.StateName
		countyName := req.CountyName

		if stateName == "" || countyName == "" {
			// Auto-detect county and state from coordinates
			countyBoundary, err := h.mapboxService.GetCountyForCoordinates(ctx, lat, lng, "")
			if err != nil {
				return nil, errors.New("could not determine county from location: " + err.Error())
			}
			if countyName == "" {
				countyName = countyBoundary.Name
			}
			if stateName == "" {
				stateName = countyBoundary.State
			}
			slog.Info("auto-detected county and state", "county", countyName, "state", stateName)
		}

		// First, get or create the state
		stateID, err := h.getOrCreateStateRegion(ctx, stateName, lat, lng, userID)
		if err != nil {
			return nil, err
		}

		// Then, get or create the county
		countyID, err := h.getOrCreateCountyRegion(ctx, countyName, stateName, lat, lng, stateID, userID)
		if err != nil {
			return nil, err
		}
		return &countyID, nil

	case models.RegionTypeNeighborhood:
		// Neighborhood requires an existing City parent
		return nil, errors.New("neighborhood regions must have an existing city parent")

	case models.RegionTypeCityBlock:
		// CityBlock requires an existing Neighborhood parent
		return nil, errors.New("city block regions must have an existing neighborhood parent")
	}

	return nil, nil
}

// detectStateFromCoordinates uses reverse geocoding to determine the state for given coordinates
func (h *RegionHandler) detectStateFromCoordinates(ctx context.Context, lat, lng float64) (string, error) {
	// Use the county lookup which also returns state information
	countyBoundary, err := h.mapboxService.GetCountyForCoordinates(ctx, lat, lng, "")
	if err != nil {
		return "", err
	}
	if countyBoundary.State == "" {
		return "", errors.New("could not determine state from coordinates")
	}
	return countyBoundary.State, nil
}

// getOrCreateStateRegion finds an existing state region or creates a new one
func (h *RegionHandler) getOrCreateStateRegion(ctx context.Context, stateName string, lat, lng float64, userID string) (string, error) {
	// Check if state region already exists
	existingState, err := h.regionRepo.GetByNameAndType(ctx, stateName, models.RegionTypeState)
	if err == nil && existingState != nil {
		slog.Info("found existing state region", "name", stateName, "region_id", existingState.ID)
		return existingState.ID, nil
	}

	// State doesn't exist - fetch boundary and create it
	slog.Info("creating new state region", "name", stateName)
	stateBoundary, err := h.mapboxService.GetStateBoundary(ctx, stateName, lat, lng)
	if err != nil {
		return "", err
	}

	newState := &models.GeographicRegion{
		Name:       stateBoundary.Name,
		RegionType: models.RegionTypeState,
		CreatedBy:  &userID,
	}

	if err := h.regionRepo.Create(ctx, newState, stateBoundary.GeoJSON); err != nil {
		return "", err
	}

	slog.Info("created state region", "name", newState.Name, "region_id", newState.ID)
	return newState.ID, nil
}

// getOrCreateCountyRegion finds an existing county region or creates a new one
func (h *RegionHandler) getOrCreateCountyRegion(ctx context.Context, countyName, stateName string, lat, lng float64, stateRegionID string, userID string) (string, error) {
	// Check if county region already exists
	existingCounty, err := h.regionRepo.GetByNameAndType(ctx, countyName, models.RegionTypeCounty)
	if err == nil && existingCounty != nil {
		slog.Info("found existing county region", "name", countyName, "region_id", existingCounty.ID)
		return existingCounty.ID, nil
	}

	// County doesn't exist - fetch boundary and create it
	slog.Info("creating new county region", "name", countyName)
	countyBoundary, err := h.mapboxService.GetCountyBoundary(ctx, countyName, stateName, lat, lng)
	if err != nil {
		return "", err
	}

	newCounty := &models.GeographicRegion{
		Name:           countyBoundary.Name,
		RegionType:     models.RegionTypeCounty,
		ParentRegionID: &stateRegionID,
		CreatedBy:      &userID,
	}

	if err := h.regionRepo.Create(ctx, newCounty, countyBoundary.GeoJSON); err != nil {
		return "", err
	}

	slog.Info("created county region", "name", newCounty.Name, "region_id", newCounty.ID)
	return newCounty.ID, nil
}

// calculateGeometryCentroid calculates the centroid of a GeoJSON polygon or multipolygon
// Returns [lng, lat]
func calculateGeometryCentroid(geom *models.GeoJSONGeometry) [2]float64 {
	if geom == nil || geom.Coordinates == nil {
		return [2]float64{0, 0}
	}

	// Get the outer ring based on geometry type
	var ring [][]float64

	switch geom.Type {
	case "Polygon":
		// Polygon: coordinates is [][][]float64 - array of rings
		// But JSON unmarshal produces []interface{}
		coords, ok := geom.Coordinates.([][][]float64)
		if ok && len(coords) > 0 && len(coords[0]) > 0 {
			ring = coords[0]
		} else {
			// Try interface{} slice unmarshaling (from JSON)
			coordsIface, ok := geom.Coordinates.([]interface{})
			if !ok || len(coordsIface) == 0 {
				return [2]float64{0, 0}
			}
			// First element is the outer ring
			outerRing, ok := coordsIface[0].([]interface{})
			if !ok || len(outerRing) == 0 {
				return [2]float64{0, 0}
			}
			// Extract coords from interface slice
			for _, point := range outerRing {
				pointArr, ok := point.([]interface{})
				if ok && len(pointArr) >= 2 {
					lng, _ := pointArr[0].(float64)
					lat, _ := pointArr[1].(float64)
					ring = append(ring, []float64{lng, lat})
				}
			}
		}

	case "MultiPolygon":
		// MultiPolygon: coordinates is [][][][]float64 - array of polygons
		coords, ok := geom.Coordinates.([][][][]float64)
		if !ok || len(coords) == 0 || len(coords[0]) == 0 || len(coords[0][0]) == 0 {
			// Try interface{} slice unmarshaling
			coordsIface, ok := geom.Coordinates.([]interface{})
			if !ok || len(coordsIface) == 0 {
				return [2]float64{0, 0}
			}
			// Use first polygon's outer ring from interface
			polygon, ok := coordsIface[0].([]interface{})
			if !ok || len(polygon) == 0 {
				return [2]float64{0, 0}
			}
			outerRing, ok := polygon[0].([]interface{})
			if !ok || len(outerRing) == 0 {
				return [2]float64{0, 0}
			}
			// Extract coords from interface slice
			for _, point := range outerRing {
				pointArr, ok := point.([]interface{})
				if ok && len(pointArr) >= 2 {
					lng, _ := pointArr[0].(float64)
					lat, _ := pointArr[1].(float64)
					ring = append(ring, []float64{lng, lat})
				}
			}
		} else {
			ring = coords[0][0] // First polygon's outer ring
		}

	default:
		// Try generic interface slice handling for JSON unmarshaled data
		coords, ok := geom.Coordinates.([]interface{})
		if !ok || len(coords) == 0 {
			return [2]float64{0, 0}
		}
		// Assume Polygon-like structure
		outerRing, ok := coords[0].([]interface{})
		if !ok || len(outerRing) == 0 {
			return [2]float64{0, 0}
		}
		for _, point := range outerRing {
			pointArr, ok := point.([]interface{})
			if ok && len(pointArr) >= 2 {
				lng, _ := pointArr[0].(float64)
				lat, _ := pointArr[1].(float64)
				ring = append(ring, []float64{lng, lat})
			}
		}
	}

	if len(ring) == 0 {
		return [2]float64{0, 0}
	}

	// Calculate centroid from the outer ring
	var sumLng, sumLat float64
	for _, coord := range ring {
		if len(coord) >= 2 {
			sumLng += coord[0]
			sumLat += coord[1]
		}
	}

	n := float64(len(ring))
	if n == 0 {
		return [2]float64{0, 0}
	}

	return [2]float64{sumLng / n, sumLat / n}
}

// validateTypeParentHierarchy validates the region type and parent combination
// New hierarchy: State -> County -> City -> Neighborhood -> CityBlock
// Rules:
// - State cannot have a parent
// - County must have a State parent
// - City must have a County parent
// - Neighborhood must have a City parent
// - City Block must have a Neighborhood parent
func (h *RegionHandler) validateTypeParentHierarchy(r *http.Request, regionType models.RegionType, parentID *string) error {
	switch regionType {
	case models.RegionTypeState:
		// State cannot have a parent
		if parentID != nil && *parentID != "" {
			return errors.New("state regions cannot have a parent region")
		}

	case models.RegionTypeCounty:
		// County must have a State parent
		if parentID == nil || *parentID == "" {
			return errors.New("county regions must have a state parent")
		}
		parent, err := h.regionRepo.GetByID(r.Context(), *parentID)
		if err != nil {
			return errors.New("parent region not found")
		}
		if parent.RegionType != models.RegionTypeState {
			return errors.New("county regions must have a state parent")
		}

	case models.RegionTypeCity:
		// City must have a County parent
		if parentID == nil || *parentID == "" {
			return errors.New("city regions must have a county parent")
		}
		parent, err := h.regionRepo.GetByID(r.Context(), *parentID)
		if err != nil {
			return errors.New("parent region not found")
		}
		if parent.RegionType != models.RegionTypeCounty {
			return errors.New("city regions must have a county parent")
		}

	case models.RegionTypeNeighborhood:
		// Neighborhood must have a City parent
		if parentID == nil || *parentID == "" {
			return errors.New("neighborhood regions must have a city parent")
		}
		parent, err := h.regionRepo.GetByID(r.Context(), *parentID)
		if err != nil {
			return errors.New("parent region not found")
		}
		if parent.RegionType != models.RegionTypeCity {
			return errors.New("neighborhood regions must have a city parent")
		}

	case models.RegionTypeCityBlock:
		// City Block must have a Neighborhood parent
		if parentID == nil || *parentID == "" {
			return errors.New("city block regions must have a neighborhood parent")
		}
		parent, err := h.regionRepo.GetByID(r.Context(), *parentID)
		if err != nil {
			return errors.New("parent region not found")
		}
		if parent.RegionType != models.RegionTypeNeighborhood {
			return errors.New("city block regions must have a neighborhood parent")
		}
	}

	return nil
}

// ListMembers handles GET /api/v1/communities/:id/members
// Returns region members with email stripped (member-visible, not admin-only)
func (h *RegionHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	regionID := getPathParam(r, "id")
	if regionID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Community ID required")
		return
	}

	// Check caller is a member of the region (or superuser)
	if !claims.IsSuperuser {
		isMember, err := h.regionRepo.IsUserInRegion(r.Context(), claims.UserID, regionID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check membership", "region", "list_members")
			return
		}
		if !isMember {
			writeError(w, http.StatusForbidden, "not_member", "You must be a member of this community")
			return
		}
	}

	users, err := h.regionRepo.GetUsersInRegion(r.Context(), regionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to list members", "region", "list_members")
		return
	}

	// Strip email from response — only expose id, username, is_admin
	type memberResponse struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		IsAdmin  bool   `json:"is_admin"`
	}

	members := make([]memberResponse, len(users))
	for i, u := range users {
		members[i] = memberResponse{
			ID:       u.ID,
			Username: u.Username,
			IsAdmin:  u.IsAdmin,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"members": members,
	})
}

// ListUsersInRegion handles GET /api/v1/communities/:id/users
// Returns all verified users in the specified region
// Only admins of the region (or superusers) can access this
func (h *RegionHandler) ListUsersInRegion(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	regionID := getPathParam(r, "id")
	if regionID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "Community ID required")
		return
	}

	// Check if region exists
	_, err := h.regionRepo.GetByID(r.Context(), regionID)
	if errors.Is(err, database.ErrRegionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Community not found")
		return
	}
	if err != nil {
		writeServerError(w, r, err, "Failed to get region", "region", "list_users")
		return
	}

	// Check if user is admin of this region (or superuser)
	if !claims.IsSuperuser {
		isAdmin, err := h.regionRepo.IsUserAdmin(r.Context(), claims.UserID, regionID)
		if err != nil {
			writeServerError(w, r, err, "Failed to check permissions", "region", "list_users")
			return
		}
		if !isAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "You must be an admin of this community to view users")
			return
		}
	}

	users, err := h.regionRepo.GetUsersInRegion(r.Context(), regionID)
	if err != nil {
		writeServerError(w, r, err, "Failed to get users", "region", "list_users")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"users": users,
	})
}
