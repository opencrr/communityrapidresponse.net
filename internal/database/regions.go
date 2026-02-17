package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

var (
	ErrRegionNotFound      = errors.New("region not found")
	ErrRegionOutsideUS     = errors.New("region must be within the United States or its territories")
)

// RegionRepository handles region database operations
type RegionRepository struct {
	db *DB
}

// NewRegionRepository creates a new region repository
func NewRegionRepository(db *DB) *RegionRepository {
	return &RegionRepository{db: db}
}

// Create creates a new geographic region with required geometry
func (r *RegionRepository) Create(ctx context.Context, region *models.GeographicRegion, geoJSON string) error {
	region.ID = uuid.New().String()
	region.CreatedAt = time.Now().UTC()

	query := `
		INSERT INTO geographic_regions (id, name, region_type, parent_region_id, geometry, created_by, created_at)
		VALUES (?, ?, ?, ?, ST_GeomFromGeoJSON(?), ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		region.ID,
		region.Name,
		region.RegionType,
		region.ParentRegionID,
		geoJSON,
		region.CreatedBy,
		region.CreatedAt,
	)

	return err
}

// CreateWithOptionalGeometry creates a new geographic region with optional geometry
// If geoJSON is empty, the geometry column will be NULL (for localities/neighborhoods without boundaries)
func (r *RegionRepository) CreateWithOptionalGeometry(ctx context.Context, region *models.GeographicRegion, geoJSON string) error {
	region.ID = uuid.New().String()
	region.CreatedAt = time.Now().UTC()

	if geoJSON == "" {
		// Create region without geometry
		query := `
			INSERT INTO geographic_regions (id, name, region_type, parent_region_id, geometry, created_by, created_at)
			VALUES (?, ?, ?, ?, NULL, ?, ?)
		`
		_, err := r.db.ExecContext(ctx, query,
			region.ID,
			region.Name,
			region.RegionType,
			region.ParentRegionID,
			region.CreatedBy,
			region.CreatedAt,
		)
		return err
	}

	// Create region with geometry
	return r.Create(ctx, region, geoJSON)
}

// GetByID retrieves a region by ID
func (r *RegionRepository) GetByID(ctx context.Context, id string) (*models.GeographicRegion, error) {
	query := `
		SELECT id, name, region_type, parent_region_id, created_by, created_at
		FROM geographic_regions WHERE id = ?
	`

	region := &models.GeographicRegion{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&region.ID,
		&region.Name,
		&region.RegionType,
		&region.ParentRegionID,
		&region.CreatedBy,
		&region.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRegionNotFound
	}
	if err != nil {
		return nil, err
	}

	return region, nil
}

// GetByIDWithDetails retrieves a region with full details
// If userID is provided (non-empty), sub-regions are filtered to only those the user has access to
// If userID is empty, all sub-regions are returned (for superusers)
func (r *RegionRepository) GetByIDWithDetails(ctx context.Context, id string, userID string) (*models.RegionWithDetails, error) {
	region, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	result := &models.RegionWithDetails{
		GeographicRegion: *region,
	}

	// Get geometry as GeoJSON
	geoJSONQuery := `SELECT ST_AsGeoJSON(geometry) FROM geographic_regions WHERE id = ?`
	var geoJSONStr sql.NullString
	if err := r.db.QueryRowContext(ctx, geoJSONQuery, id).Scan(&geoJSONStr); err == nil && geoJSONStr.Valid {
		var geometry models.GeoJSONGeometry
		if err := json.Unmarshal([]byte(geoJSONStr.String), &geometry); err == nil {
			result.GeoJSON = &geometry
		}
	}

	// Get parent region
	if region.ParentRegionID != nil {
		parentQuery := `SELECT id, name, region_type FROM geographic_regions WHERE id = ?`
		parent := &models.RegionSummary{}
		if err := r.db.QueryRowContext(ctx, parentQuery, *region.ParentRegionID).Scan(
			&parent.ID, &parent.Name, &parent.RegionType,
		); err == nil {
			result.ParentRegion = parent
		}
	}

	// Get sub-regions - regions geographically contained within this region
	// If userID is provided, filter to only regions the user has access to
	var subQuery string
	var subArgs []interface{}

	if userID == "" {
		// Superuser: show all sub-regions
		// Includes both geometry-based containment and parent_id-based relationship
		// IMPORTANT: Exclude ancestor regions from ST_Contains to prevent parent regions
		// from appearing as sub-regions when their geometry is contained (e.g., NYC city
		// contains Kings County geographically, but Kings County is NYC's parent)
		subQuery = `
			WITH RECURSIVE ancestors AS (
				-- Get parent chain of the current region
				SELECT parent_region_id as id FROM geographic_regions WHERE id = ?
				UNION
				SELECT gr.parent_region_id FROM geographic_regions gr
				JOIN ancestors a ON gr.id = a.id
				WHERE gr.parent_region_id IS NOT NULL
			)
			SELECT sub.id, sub.name, sub.region_type,
				(SELECT COUNT(*) FROM user_regions WHERE region_id = sub.id AND is_admin = TRUE) as admin_count,
				(SELECT COUNT(DISTINCT ur.user_id) FROM user_regions ur
					JOIN geographic_regions child ON ur.region_id = child.id
					WHERE child.id = sub.id OR (child.geometry IS NOT NULL AND sub.geometry IS NOT NULL AND ST_Contains(sub.geometry, child.geometry))) as member_count,
				(SELECT COUNT(DISTINCT u.id) FROM users u
					JOIN user_regions ur ON u.id = ur.user_id
					JOIN geographic_regions child ON ur.region_id = child.id
					WHERE ur.is_admin = TRUE
						AND u.postcard_verified = TRUE
						AND u.vouch_verified = TRUE
						AND (child.id = sub.id OR (child.geometry IS NOT NULL AND sub.geometry IS NOT NULL AND ST_Contains(sub.geometry, child.geometry)))) as full_admin_count,
				ST_AsGeoJSON(sub.geometry) as geometry
			FROM geographic_regions sub
			WHERE sub.id != ?
				AND sub.id NOT IN (SELECT id FROM ancestors WHERE id IS NOT NULL)
				AND (
					sub.parent_region_id = ?
					OR (
						sub.geometry IS NOT NULL
						AND (SELECT geometry FROM geographic_regions WHERE id = ?) IS NOT NULL
						AND ST_Contains(
							(SELECT geometry FROM geographic_regions WHERE id = ?),
							sub.geometry
						)
					)
				)
			ORDER BY
				CASE sub.region_type
					WHEN 'county' THEN 1
					WHEN 'city' THEN 2
					WHEN 'locality' THEN 3
					WHEN 'neighborhood' THEN 4
					WHEN 'city_block' THEN 5
					ELSE 6
				END,
				sub.name
		`
		subArgs = []interface{}{id, id, id, id, id}
	} else {
		// Regular user: filter sub-regions to those they have access to
		// Uses the same recursive CTE logic as ListForUser
		// Includes both geometry-based containment and parent_id-based relationship
		// IMPORTANT: Exclude ancestor regions from ST_Contains to prevent parent regions
		// from appearing as sub-regions when their geometry is contained
		subQuery = `
			WITH RECURSIVE user_accessible_regions AS (
				-- Base case: regions the user is directly a member of
				SELECT gr.id, gr.parent_region_id
				FROM geographic_regions gr
				INNER JOIN user_regions ur ON gr.id = ur.region_id
				WHERE ur.user_id = ?

				UNION

				-- Recursive case: parent regions
				SELECT gr.id, gr.parent_region_id
				FROM geographic_regions gr
				INNER JOIN user_accessible_regions uar ON gr.id = uar.parent_region_id
			),
			ancestors AS (
				-- Get parent chain of the current region
				SELECT parent_region_id as id FROM geographic_regions WHERE id = ?
				UNION
				SELECT gr.parent_region_id FROM geographic_regions gr
				JOIN ancestors a ON gr.id = a.id
				WHERE gr.parent_region_id IS NOT NULL
			)
			SELECT sub.id, sub.name, sub.region_type,
				(SELECT COUNT(*) FROM user_regions WHERE region_id = sub.id AND is_admin = TRUE) as admin_count,
				(SELECT COUNT(DISTINCT ur.user_id) FROM user_regions ur
					JOIN geographic_regions child ON ur.region_id = child.id
					WHERE child.id = sub.id OR (child.geometry IS NOT NULL AND sub.geometry IS NOT NULL AND ST_Contains(sub.geometry, child.geometry))) as member_count,
				(SELECT COUNT(DISTINCT u.id) FROM users u
					JOIN user_regions ur ON u.id = ur.user_id
					JOIN geographic_regions child ON ur.region_id = child.id
					WHERE ur.is_admin = TRUE
						AND u.postcard_verified = TRUE
						AND u.vouch_verified = TRUE
						AND (child.id = sub.id OR (child.geometry IS NOT NULL AND sub.geometry IS NOT NULL AND ST_Contains(sub.geometry, child.geometry)))) as full_admin_count,
				ST_AsGeoJSON(sub.geometry) as geometry
			FROM geographic_regions sub
			INNER JOIN user_accessible_regions uar ON sub.id = uar.id
			WHERE sub.id != ?
				AND sub.id NOT IN (SELECT id FROM ancestors WHERE id IS NOT NULL)
				AND (
					sub.parent_region_id = ?
					OR (
						sub.geometry IS NOT NULL
						AND (SELECT geometry FROM geographic_regions WHERE id = ?) IS NOT NULL
						AND ST_Contains(
							(SELECT geometry FROM geographic_regions WHERE id = ?),
							sub.geometry
						)
					)
				)
			ORDER BY
				CASE sub.region_type
					WHEN 'county' THEN 1
					WHEN 'city' THEN 2
					WHEN 'locality' THEN 3
					WHEN 'neighborhood' THEN 4
					WHEN 'city_block' THEN 5
					ELSE 6
				END,
				sub.name
		`
		subArgs = []interface{}{userID, id, id, id, id, id}
	}

	subRows, err := r.db.QueryContext(ctx, subQuery, subArgs...)
	if err == nil {
		defer func() { _ = subRows.Close() }()
		for subRows.Next() {
			var sub models.RegionSummary
			var geometryJSON sql.NullString
			if err := subRows.Scan(&sub.ID, &sub.Name, &sub.RegionType, &sub.AdminCount, &sub.MemberCount, &sub.FullAdminCount, &geometryJSON); err != nil {
				continue
			}
			if geometryJSON.Valid {
				var geometry models.GeoJSONGeometry
				if err := json.Unmarshal([]byte(geometryJSON.String), &geometry); err == nil {
					sub.Geometry = &geometry
				}
			}
			// Bootstrap mode when fewer than 3 full admins
			sub.BootstrapMode = sub.FullAdminCount < 3
			result.SubRegions = append(result.SubRegions, sub)
		}
		if err := subRows.Err(); err != nil {
			return nil, err
		}
	}

	// Get admin count
	countQuery := `SELECT COUNT(*) FROM user_regions WHERE region_id = ? AND is_admin = TRUE`
	_ = r.db.QueryRowContext(ctx, countQuery, id).Scan(&result.AdminCount)

	// Get member count (includes members from all child regions)
	memberQuery := `
		WITH RECURSIVE region_tree AS (
			SELECT id FROM geographic_regions WHERE id = ?
			UNION ALL
			SELECT gr.id
			FROM geographic_regions gr
			INNER JOIN region_tree rt ON gr.parent_region_id = rt.id
		)
		SELECT COUNT(DISTINCT ur.user_id)
		FROM user_regions ur
		WHERE ur.region_id IN (SELECT id FROM region_tree)
	`
	_ = r.db.QueryRowContext(ctx, memberQuery, id).Scan(&result.MemberCount)

	// Get admins
	adminQuery := `
		SELECT u.id, u.username, u.verification_tier
		FROM users u
		JOIN user_regions ur ON u.id = ur.user_id
		WHERE ur.region_id = ? AND ur.is_admin = TRUE
	`
	adminRows, err := r.db.QueryContext(ctx, adminQuery, id)
	if err == nil {
		defer func() { _ = adminRows.Close() }()
		for adminRows.Next() {
			var admin models.PublicUser
			if err := adminRows.Scan(&admin.ID, &admin.Username, &admin.VerificationTier); err == nil {
				result.Admins = append(result.Admins, admin)
			}
		}
		if err := adminRows.Err(); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// List retrieves regions with optional filtering
func (r *RegionRepository) List(ctx context.Context, regionType *models.RegionType, parentID *string) ([]models.RegionSummary, error) {
	query := `
		SELECT gr.id, gr.name, gr.region_type,
			(SELECT COUNT(*) FROM user_regions WHERE region_id = gr.id AND is_admin = TRUE) as admin_count,
			(SELECT COUNT(DISTINCT ur.user_id) FROM user_regions ur
				JOIN geographic_regions child ON ur.region_id = child.id
				WHERE child.id = gr.id OR (child.geometry IS NOT NULL AND gr.geometry IS NOT NULL AND ST_Contains(gr.geometry, child.geometry))) as member_count,
			(SELECT COUNT(DISTINCT u.id) FROM users u
				JOIN user_regions ur ON u.id = ur.user_id
				JOIN geographic_regions child ON ur.region_id = child.id
				WHERE ur.is_admin = TRUE
					AND u.postcard_verified = TRUE
					AND u.vouch_verified = TRUE
					AND (child.id = gr.id OR (child.geometry IS NOT NULL AND gr.geometry IS NOT NULL AND ST_Contains(gr.geometry, child.geometry)))) as full_admin_count,
			ST_AsGeoJSON(gr.geometry) as geometry
		FROM geographic_regions gr
		WHERE 1=1
	`
	args := []interface{}{}

	if regionType != nil {
		query += " AND gr.region_type = ?"
		args = append(args, *regionType)
	}

	if parentID != nil {
		query += " AND gr.parent_region_id = ?"
		args = append(args, *parentID)
	}

	query += " ORDER BY gr.name"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	// Initialize as empty slice (not nil) so JSON marshals to [] instead of null
	regions := []models.RegionSummary{}
	for rows.Next() {
		var region models.RegionSummary
		var geometryJSON sql.NullString
		if err := rows.Scan(
			&region.ID,
			&region.Name,
			&region.RegionType,
			&region.AdminCount,
			&region.MemberCount,
			&region.FullAdminCount,
			&geometryJSON,
		); err != nil {
			return nil, err
		}
		if geometryJSON.Valid {
			var geometry models.GeoJSONGeometry
			if err := json.Unmarshal([]byte(geometryJSON.String), &geometry); err == nil {
				region.Geometry = &geometry
			}
		}
		// Bootstrap mode when fewer than 3 full admins
		region.BootstrapMode = region.FullAdminCount < 3
		regions = append(regions, region)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return regions, nil
}

// ListForUser retrieves regions that a user is a member of, including parent regions
// Parent region membership is derived dynamically from direct memberships
func (r *RegionRepository) ListForUser(ctx context.Context, userID string) ([]models.RegionSummary, error) {
	// Use a recursive CTE to get all regions the user has access to:
	// 1. Direct memberships (from user_regions)
	// 2. Parent regions of direct memberships (traversing up the hierarchy)
	query := `
		WITH RECURSIVE user_accessible_regions AS (
			-- Base case: regions the user is directly a member of
			SELECT gr.id, gr.parent_region_id
			FROM geographic_regions gr
			INNER JOIN user_regions ur ON gr.id = ur.region_id
			WHERE ur.user_id = ?

			UNION

			-- Recursive case: parent regions
			SELECT gr.id, gr.parent_region_id
			FROM geographic_regions gr
			INNER JOIN user_accessible_regions uar ON gr.id = uar.parent_region_id
		)
		SELECT DISTINCT gr.id, gr.name, gr.region_type,
			(SELECT COUNT(*) FROM user_regions WHERE region_id = gr.id AND is_admin = TRUE) as admin_count,
			(SELECT COUNT(DISTINCT ur.user_id) FROM user_regions ur
				JOIN geographic_regions child ON ur.region_id = child.id
				WHERE child.id = gr.id OR (child.geometry IS NOT NULL AND gr.geometry IS NOT NULL AND ST_Contains(gr.geometry, child.geometry))) as member_count,
			(SELECT COUNT(DISTINCT u.id) FROM users u
				JOIN user_regions ur ON u.id = ur.user_id
				JOIN geographic_regions child ON ur.region_id = child.id
				WHERE ur.is_admin = TRUE
					AND u.postcard_verified = TRUE
					AND u.vouch_verified = TRUE
					AND (child.id = gr.id OR (child.geometry IS NOT NULL AND gr.geometry IS NOT NULL AND ST_Contains(gr.geometry, child.geometry)))) as full_admin_count,
			ST_AsGeoJSON(gr.geometry) as geometry
		FROM geographic_regions gr
		INNER JOIN user_accessible_regions uar ON gr.id = uar.id
		ORDER BY
			CASE gr.region_type
				WHEN 'state' THEN 1
				WHEN 'county' THEN 2
				WHEN 'city' THEN 3
				WHEN 'locality' THEN 4
				WHEN 'neighborhood' THEN 5
				WHEN 'city_block' THEN 6
				ELSE 7
			END,
			gr.name
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	// Initialize as empty slice (not nil) so JSON marshals to [] instead of null
	regions := []models.RegionSummary{}
	for rows.Next() {
		var region models.RegionSummary
		var geometryJSON sql.NullString
		if err := rows.Scan(
			&region.ID,
			&region.Name,
			&region.RegionType,
			&region.AdminCount,
			&region.MemberCount,
			&region.FullAdminCount,
			&geometryJSON,
		); err != nil {
			return nil, err
		}
		if geometryJSON.Valid {
			var geometry models.GeoJSONGeometry
			if err := json.Unmarshal([]byte(geometryJSON.String), &geometry); err == nil {
				region.Geometry = &geometry
			}
		}
		// Bootstrap mode when fewer than 3 full admins
		region.BootstrapMode = region.FullAdminCount < 3
		regions = append(regions, region)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return regions, nil
}

// ListAdminRegions retrieves regions where the user is an admin
// Admin rights propagate up the hierarchy: if you're an admin for a child region,
// you're also an admin for all parent regions
func (r *RegionRepository) ListAdminRegions(ctx context.Context, userID string) ([]models.RegionSummary, error) {
	query := `
		WITH RECURSIVE user_admin_regions AS (
			-- Base case: regions the user is directly an admin of
			SELECT gr.id, gr.parent_region_id
			FROM geographic_regions gr
			INNER JOIN user_regions ur ON gr.id = ur.region_id
			WHERE ur.user_id = ? AND ur.is_admin = TRUE

			UNION

			-- Recursive case: parent regions (admin rights propagate up)
			SELECT gr.id, gr.parent_region_id
			FROM geographic_regions gr
			INNER JOIN user_admin_regions uar ON gr.id = uar.parent_region_id
		)
		SELECT DISTINCT gr.id, gr.name, gr.region_type,
			(SELECT COUNT(*) FROM user_regions WHERE region_id = gr.id AND is_admin = TRUE) as admin_count,
			(SELECT COUNT(DISTINCT ur.user_id) FROM user_regions ur
				JOIN geographic_regions child ON ur.region_id = child.id
				WHERE child.id = gr.id OR (child.geometry IS NOT NULL AND gr.geometry IS NOT NULL AND ST_Contains(gr.geometry, child.geometry))) as member_count,
			(SELECT COUNT(DISTINCT u.id) FROM users u
				JOIN user_regions ur ON u.id = ur.user_id
				JOIN geographic_regions child ON ur.region_id = child.id
				WHERE ur.is_admin = TRUE
					AND u.postcard_verified = TRUE
					AND u.vouch_verified = TRUE
					AND (child.id = gr.id OR (child.geometry IS NOT NULL AND gr.geometry IS NOT NULL AND ST_Contains(gr.geometry, child.geometry)))) as full_admin_count,
			ST_AsGeoJSON(gr.geometry) as geometry
		FROM geographic_regions gr
		INNER JOIN user_admin_regions uar ON gr.id = uar.id
		ORDER BY
			CASE gr.region_type
				WHEN 'state' THEN 1
				WHEN 'county' THEN 2
				WHEN 'city' THEN 3
				WHEN 'locality' THEN 4
				WHEN 'neighborhood' THEN 5
				WHEN 'city_block' THEN 6
				ELSE 7
			END,
			gr.name
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	// Initialize as empty slice (not nil) so JSON marshals to [] instead of null
	regions := []models.RegionSummary{}
	for rows.Next() {
		var region models.RegionSummary
		var geometryJSON sql.NullString
		if err := rows.Scan(
			&region.ID,
			&region.Name,
			&region.RegionType,
			&region.AdminCount,
			&region.MemberCount,
			&region.FullAdminCount,
			&geometryJSON,
		); err != nil {
			return nil, err
		}
		if geometryJSON.Valid {
			var geometry models.GeoJSONGeometry
			if err := json.Unmarshal([]byte(geometryJSON.String), &geometry); err == nil {
				region.Geometry = &geometry
			}
		}
		// Bootstrap mode when fewer than 3 full admins
		region.BootstrapMode = region.FullAdminCount < 3
		regions = append(regions, region)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return regions, nil
}

// GetRegionsContainingPoint finds regions that contain a given point
// Returns regions ordered from most specific (city_block) to least specific (state)
// Note: Only returns regions with geometry; regions without geometry are found via parent traversal
func (r *RegionRepository) GetRegionsContainingPoint(ctx context.Context, lat, lng float64) ([]models.RegionSummary, error) {
	query := `
		SELECT gr.id, gr.name, gr.region_type,
			(SELECT COUNT(*) FROM user_regions WHERE region_id = gr.id AND is_admin = TRUE) as admin_count,
			(SELECT COUNT(DISTINCT ur.user_id) FROM user_regions ur
				JOIN geographic_regions child ON ur.region_id = child.id
				WHERE child.id = gr.id OR (child.geometry IS NOT NULL AND gr.geometry IS NOT NULL AND ST_Contains(gr.geometry, child.geometry))) as member_count,
			(SELECT COUNT(DISTINCT u.id) FROM users u
				JOIN user_regions ur ON u.id = ur.user_id
				JOIN geographic_regions child ON ur.region_id = child.id
				WHERE ur.is_admin = TRUE
					AND u.postcard_verified = TRUE
					AND u.vouch_verified = TRUE
					AND (child.id = gr.id OR (child.geometry IS NOT NULL AND gr.geometry IS NOT NULL AND ST_Contains(gr.geometry, child.geometry)))) as full_admin_count,
			ST_AsGeoJSON(gr.geometry) as geometry
		FROM geographic_regions gr
		WHERE gr.geometry IS NOT NULL
			AND ST_Contains(gr.geometry, ST_GeomFromText(CONCAT('POINT(', ?, ' ', ?, ')'), 4326))
		ORDER BY CASE gr.region_type
			WHEN 'city_block' THEN 1
			WHEN 'neighborhood' THEN 2
			WHEN 'locality' THEN 3
			WHEN 'city' THEN 4
			WHEN 'county' THEN 5
			WHEN 'state' THEN 6
			ELSE 7
		END ASC
	`

	rows, err := r.db.QueryContext(ctx, query, lng, lat)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	// Initialize as empty slice (not nil) so JSON marshals to [] instead of null
	regions := []models.RegionSummary{}
	for rows.Next() {
		var region models.RegionSummary
		var geometryJSON sql.NullString
		if err := rows.Scan(
			&region.ID,
			&region.Name,
			&region.RegionType,
			&region.AdminCount,
			&region.MemberCount,
			&region.FullAdminCount,
			&geometryJSON,
		); err != nil {
			return nil, err
		}
		if geometryJSON.Valid {
			var geometry models.GeoJSONGeometry
			if err := json.Unmarshal([]byte(geometryJSON.String), &geometry); err == nil {
				region.Geometry = &geometry
			}
		}
		// Bootstrap mode when fewer than 3 full admins
		region.BootstrapMode = region.FullAdminCount < 3
		regions = append(regions, region)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return regions, nil
}

// IsPointInRegion checks if a point is within a region
// Returns false for regions without geometry
func (r *RegionRepository) IsPointInRegion(ctx context.Context, regionID string, lat, lng float64) (bool, error) {
	// First check if region exists and has geometry
	query := `
		SELECT CASE
			WHEN geometry IS NULL THEN FALSE
			ELSE ST_Contains(geometry, ST_GeomFromText(CONCAT('POINT(', ?, ' ', ?, ')'), 4326))
		END
		FROM geographic_regions
		WHERE id = ?
	`

	var contains bool
	err := r.db.QueryRowContext(ctx, query, lng, lat, regionID).Scan(&contains)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrRegionNotFound
	}
	if err != nil {
		return false, err
	}

	return contains, nil
}

// IsRegionWithinBoundary checks if a region is entirely within an admin boundary
func (r *RegionRepository) IsRegionWithinBoundary(ctx context.Context, regionGeoJSON string, boundaryID string) (bool, error) {
	query := `
		SELECT ST_Within(
			ST_GeomFromGeoJSON(?),
			boundary_geometry
		)
		FROM admin_boundaries
		WHERE id = ?
	`

	var within bool
	err := r.db.QueryRowContext(ctx, query, regionGeoJSON, boundaryID).Scan(&within)
	if err != nil {
		return false, err
	}

	return within, nil
}

// GetAdminCount returns the number of admins for a region
// Admin rights propagate up the hierarchy, so admins from child regions are counted
func (r *RegionRepository) GetAdminCount(ctx context.Context, regionID string) (int, error) {
	query := `
		WITH RECURSIVE region_tree AS (
			-- Base case: the specified region
			SELECT id FROM geographic_regions WHERE id = ?

			UNION ALL

			-- Recursive case: child regions
			SELECT gr.id
			FROM geographic_regions gr
			INNER JOIN region_tree rt ON gr.parent_region_id = rt.id
		)
		SELECT COUNT(DISTINCT ur.user_id)
		FROM user_regions ur
		WHERE ur.region_id IN (SELECT id FROM region_tree)
			AND ur.is_admin = TRUE
	`
	var count int
	err := r.db.QueryRowContext(ctx, query, regionID).Scan(&count)
	return count, err
}

// IsUserAdmin checks if a user is an admin for a region
// Admin rights propagate up the hierarchy: if you're an admin for a child region,
// you're also considered an admin for all parent regions
func (r *RegionRepository) IsUserAdmin(ctx context.Context, userID, regionID string) (bool, error) {
	query := `
		WITH RECURSIVE user_admin_regions AS (
			-- Base case: regions the user is directly an admin of
			SELECT gr.id, gr.parent_region_id
			FROM geographic_regions gr
			INNER JOIN user_regions ur ON gr.id = ur.region_id
			WHERE ur.user_id = ? AND ur.is_admin = TRUE

			UNION

			-- Recursive case: parent regions (admin rights propagate up)
			SELECT gr.id, gr.parent_region_id
			FROM geographic_regions gr
			INNER JOIN user_admin_regions uar ON gr.id = uar.parent_region_id
		)
		SELECT COUNT(*) > 0
		FROM user_admin_regions
		WHERE id = ?
	`
	var isAdmin bool
	err := r.db.QueryRowContext(ctx, query, userID, regionID).Scan(&isAdmin)
	return isAdmin, err
}

// IsUserInRegion checks if a user is a member of a region (directly or through hierarchy)
// Users have access to regions they're directly in, plus parent regions up the hierarchy
func (r *RegionRepository) IsUserInRegion(ctx context.Context, userID, regionID string) (bool, error) {
	// Check if the user has access to this region through:
	// 1. Direct membership in the region
	// 2. Membership in a child region (which grants access to parent regions)
	query := `
		WITH RECURSIVE user_accessible_regions AS (
			-- Base case: regions the user is directly a member of
			SELECT gr.id, gr.parent_region_id
			FROM geographic_regions gr
			INNER JOIN user_regions ur ON gr.id = ur.region_id
			WHERE ur.user_id = ?

			UNION

			-- Recursive case: parent regions
			SELECT gr.id, gr.parent_region_id
			FROM geographic_regions gr
			INNER JOIN user_accessible_regions uar ON gr.id = uar.parent_region_id
		)
		SELECT COUNT(*) > 0
		FROM user_accessible_regions
		WHERE id = ?
	`
	var isMember bool
	err := r.db.QueryRowContext(ctx, query, userID, regionID).Scan(&isMember)
	return isMember, err
}

// IsUserVerifiedInRegion checks if a user is a verified member of a region (directly or through hierarchy).
// Unlike IsUserInRegion, this excludes pending memberships.
func (r *RegionRepository) IsUserVerifiedInRegion(ctx context.Context, userID, regionID string) (bool, error) {
	query := `
		WITH RECURSIVE user_accessible_regions AS (
			-- Base case: regions the user is a verified member of
			SELECT gr.id, gr.parent_region_id
			FROM geographic_regions gr
			INNER JOIN user_regions ur ON gr.id = ur.region_id
			WHERE ur.user_id = ? AND ur.verification_status = 'verified'

			UNION

			-- Recursive case: parent regions
			SELECT gr.id, gr.parent_region_id
			FROM geographic_regions gr
			INNER JOIN user_accessible_regions uar ON gr.id = uar.parent_region_id
		)
		SELECT COUNT(*) > 0
		FROM user_accessible_regions
		WHERE id = ?
	`
	var isMember bool
	err := r.db.QueryRowContext(ctx, query, userID, regionID).Scan(&isMember)
	return isMember, err
}

// AddUserToRegion adds a user to a region with verified status
// Parent region membership is determined dynamically via ListForUser
func (r *RegionRepository) AddUserToRegion(ctx context.Context, userID, regionID string, isAdmin bool) error {
	id := uuid.New().String()
	query := `
		INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at)
		VALUES (?, ?, ?, ?, 'verified', ?)
		ON DUPLICATE KEY UPDATE is_admin = GREATEST(is_admin, VALUES(is_admin)), verification_status = 'verified'
	`

	_, err := r.db.ExecContext(ctx, query, id, userID, regionID, isAdmin, time.Now().UTC())
	return err
}

// RemoveUserFromRegion removes a user from a region
func (r *RegionRepository) RemoveUserFromRegion(ctx context.Context, userID, regionID string) error {
	query := `DELETE FROM user_regions WHERE user_id = ? AND region_id = ?`
	_, err := r.db.ExecContext(ctx, query, userID, regionID)
	return err
}

// IsGeometryWithinUS checks if a GeoJSON geometry is within US bounds
// This includes the continental US, Alaska, Hawaii, Puerto Rico, US Virgin Islands,
// Guam, American Samoa, and Northern Mariana Islands
func (r *RegionRepository) IsGeometryWithinUS(ctx context.Context, geoJSON string) (bool, error) {
	// Check if the centroid of the geometry falls within US territory bounds
	// We use multiple bounding boxes to cover all US territories:
	// - Continental US: lat 24-50, lng -125 to -66
	// - Alaska: lat 51-72, lng -180 to -130
	// - Hawaii: lat 18-29, lng -180 to -154
	// - Puerto Rico/USVI: lat 17-19, lng -68 to -64
	// - Guam/CNMI: lat 13-21, lng 144-146
	// - American Samoa: lat -15 to -11, lng -171 to -168
	query := `
		SELECT (
			-- Continental US
			(ST_Y(ST_Centroid(ST_GeomFromGeoJSON(?))) BETWEEN 24 AND 50
			 AND ST_X(ST_Centroid(ST_GeomFromGeoJSON(?))) BETWEEN -125 AND -66)
			OR
			-- Alaska
			(ST_Y(ST_Centroid(ST_GeomFromGeoJSON(?))) BETWEEN 51 AND 72
			 AND ST_X(ST_Centroid(ST_GeomFromGeoJSON(?))) BETWEEN -180 AND -130)
			OR
			-- Hawaii
			(ST_Y(ST_Centroid(ST_GeomFromGeoJSON(?))) BETWEEN 18 AND 29
			 AND ST_X(ST_Centroid(ST_GeomFromGeoJSON(?))) BETWEEN -180 AND -154)
			OR
			-- Puerto Rico and US Virgin Islands
			(ST_Y(ST_Centroid(ST_GeomFromGeoJSON(?))) BETWEEN 17 AND 19
			 AND ST_X(ST_Centroid(ST_GeomFromGeoJSON(?))) BETWEEN -68 AND -64)
			OR
			-- Guam and Northern Mariana Islands
			(ST_Y(ST_Centroid(ST_GeomFromGeoJSON(?))) BETWEEN 13 AND 21
			 AND ST_X(ST_Centroid(ST_GeomFromGeoJSON(?))) BETWEEN 144 AND 146)
			OR
			-- American Samoa
			(ST_Y(ST_Centroid(ST_GeomFromGeoJSON(?))) BETWEEN -15 AND -11
			 AND ST_X(ST_Centroid(ST_GeomFromGeoJSON(?))) BETWEEN -171 AND -168)
		) AS within_us
	`

	var withinUS bool
	err := r.db.QueryRowContext(ctx, query,
		geoJSON, geoJSON, // Continental US
		geoJSON, geoJSON, // Alaska
		geoJSON, geoJSON, // Hawaii
		geoJSON, geoJSON, // Puerto Rico/USVI
		geoJSON, geoJSON, // Guam/CNMI
		geoJSON, geoJSON, // American Samoa
	).Scan(&withinUS)

	if err != nil {
		return false, err
	}

	return withinUS, nil
}

// IsContainedInAdminRegion checks if a GeoJSON geometry is fully contained within
// at least one region where the user has admin access
// Only checks regions with geometry
func (r *RegionRepository) IsContainedInAdminRegion(ctx context.Context, userID string, geoJSON string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM user_regions ur
			JOIN geographic_regions gr ON ur.region_id = gr.id
			WHERE ur.user_id = ?
				AND ur.is_admin = TRUE
				AND gr.geometry IS NOT NULL
				AND ST_Contains(gr.geometry, ST_GeomFromGeoJSON(?))
		)
	`

	var contained bool
	err := r.db.QueryRowContext(ctx, query, userID, geoJSON).Scan(&contained)
	if err != nil {
		return false, err
	}

	return contained, nil
}

// GetUserAdminRegions returns all regions where the user has admin access
func (r *RegionRepository) GetUserAdminRegions(ctx context.Context, userID string) ([]models.GeographicRegion, error) {
	query := `
		SELECT gr.id, gr.name, gr.region_type, gr.parent_region_id, gr.created_by, gr.created_at
		FROM geographic_regions gr
		JOIN user_regions ur ON gr.id = ur.region_id
		WHERE ur.user_id = ? AND ur.is_admin = TRUE
		ORDER BY gr.name
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var regions []models.GeographicRegion
	for rows.Next() {
		var region models.GeographicRegion
		if err := rows.Scan(
			&region.ID,
			&region.Name,
			&region.RegionType,
			&region.ParentRegionID,
			&region.CreatedBy,
			&region.CreatedAt,
		); err != nil {
			return nil, err
		}
		regions = append(regions, region)
	}

	return regions, rows.Err()
}

// Update updates a region's name
func (r *RegionRepository) Update(ctx context.Context, regionID string, name string) error {
	query := `UPDATE geographic_regions SET name = ? WHERE id = ?`
	result, err := r.db.ExecContext(ctx, query, name, regionID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrRegionNotFound
	}

	return nil
}

// PendingVouchUser represents a user with a pending vouch request
type PendingVouchUser struct {
	UserID           string    `json:"user_id"`
	Username         string    `json:"username"`
	Email            string    `json:"email"`
	UserCreatedAt    time.Time `json:"user_created_at"`
	RegionID         string    `json:"region_id"`
	RegionName       string    `json:"region_name"`
	RequestCreatedAt time.Time `json:"request_created_at"`
	VouchCount       int       `json:"vouch_count"`
}

// GetPendingVouchUsersForAdmin returns users waiting for vouches
// in regions where the given user is an admin (excludes users already vouched for)
// Includes both:
// 1. Users with explicit pending vouch requests (user_regions.verification_status = 'pending')
// 2. Postcard-verified users who aren't yet vouch-verified (automatically eligible for vouches)
func (r *RegionRepository) GetPendingVouchUsersForAdmin(ctx context.Context, adminUserID string) ([]PendingVouchUser, error) {
	query := `
		SELECT
			u.id, u.username, u.email, u.created_at as user_created_at,
			gr.id as region_id, gr.name as region_name,
			ur.verified_at as request_created_at,
			-- Display-only vouch count: counts at the region + direct children only.
			-- MariaDB cannot use recursive CTEs inside scalar subqueries, so this
			-- is limited to one level of descendants. The actual verification
			-- threshold check in the Vouch handler uses the full recursive
			-- CountVouchesForUserWithDescendants which is correct.
			COALESCE((
				SELECT COUNT(*) FROM vouches v
				WHERE v.vouched_user_id = u.id
				AND (v.region_id = gr.id OR v.region_id IN (
					SELECT g2.id FROM geographic_regions g2
					WHERE g2.parent_region_id = gr.id
				))
			), 0) as vouch_count
		FROM user_regions ur
		JOIN users u ON ur.user_id = u.id
		JOIN geographic_regions gr ON ur.region_id = gr.id
		WHERE (
			ur.verification_status = 'pending'
			OR (ur.verification_status = 'verified' AND u.postcard_verified = TRUE AND u.vouch_verified = FALSE)
		)
		AND ur.region_id IN (
			SELECT region_id FROM user_regions
			WHERE user_id = ? AND is_admin = TRUE AND verification_status = 'verified'
		)
		AND u.id != ?
		AND NOT EXISTS (
			SELECT 1 FROM vouches v
			WHERE v.voucher_user_id = ?
			AND v.vouched_user_id = u.id
		)
		AND NOT EXISTS (
			SELECT 1 FROM vouches v
			WHERE v.voucher_user_id = u.id
			AND v.vouched_user_id = ?
		)
		ORDER BY ur.verified_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, adminUserID, adminUserID, adminUserID, adminUserID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	// Initialize as empty slice (not nil) so JSON marshals to [] instead of null
	users := []PendingVouchUser{}
	for rows.Next() {
		var user PendingVouchUser
		if err := rows.Scan(
			&user.UserID,
			&user.Username,
			&user.Email,
			&user.UserCreatedAt,
			&user.RegionID,
			&user.RegionName,
			&user.RequestCreatedAt,
			&user.VouchCount,
		); err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	return users, rows.Err()
}

// GetPendingVouchUsersForBootstrapMode returns users waiting for vouches
// in bootstrap mode regions where the given user is a verified member (not necessarily admin)
// Used when a postcard-only user can vouch because the region is in bootstrap mode
// Includes both:
// 1. Users with explicit pending vouch requests (user_regions.verification_status = 'pending')
// 2. Postcard-verified users who aren't yet vouch-verified (automatically eligible for vouches)
func (r *RegionRepository) GetPendingVouchUsersForBootstrapMode(ctx context.Context, userID string) ([]PendingVouchUser, error) {
	// This query finds users waiting for vouches in regions where:
	// 1. The caller is a verified member
	// 2. The region is in bootstrap mode (< 3 full admins)
	// 3. The caller hasn't already vouched for them
	query := `
		SELECT
			u.id, u.username, u.email, u.created_at as user_created_at,
			gr.id as region_id, gr.name as region_name,
			ur.verified_at as request_created_at,
			-- Display-only vouch count: counts at the region + direct children only.
			-- See GetPendingVouchUsersForAdmin for rationale on the one-level limit.
			COALESCE((
				SELECT COUNT(*) FROM vouches v
				WHERE v.vouched_user_id = u.id
				AND (v.region_id = gr.id OR v.region_id IN (
					SELECT g2.id FROM geographic_regions g2
					WHERE g2.parent_region_id = gr.id
				))
			), 0) as vouch_count
		FROM user_regions ur
		JOIN users u ON ur.user_id = u.id
		JOIN geographic_regions gr ON ur.region_id = gr.id
		WHERE (
			ur.verification_status = 'pending'
			OR (ur.verification_status = 'verified' AND u.postcard_verified = TRUE AND u.vouch_verified = FALSE)
		)
		AND ur.region_id IN (
			SELECT region_id FROM user_regions
			WHERE user_id = ? AND verification_status = 'verified'
		)
		AND u.id != ?
		AND NOT EXISTS (
			SELECT 1 FROM vouches v
			WHERE v.voucher_user_id = ?
			AND v.vouched_user_id = u.id
		)
		AND NOT EXISTS (
			SELECT 1 FROM vouches v
			WHERE v.voucher_user_id = u.id
			AND v.vouched_user_id = ?
		)
		AND (
			SELECT COUNT(DISTINCT usr.id)
			FROM users usr
			JOIN user_regions ureg ON usr.id = ureg.user_id
			WHERE ureg.region_id = gr.id
				AND ureg.is_admin = TRUE
				AND usr.postcard_verified = TRUE
				AND usr.vouch_verified = TRUE
		) < 3
		ORDER BY ur.verified_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	// Initialize as empty slice (not nil) so JSON marshals to [] instead of null
	users := []PendingVouchUser{}
	for rows.Next() {
		var user PendingVouchUser
		if err := rows.Scan(
			&user.UserID,
			&user.Username,
			&user.Email,
			&user.UserCreatedAt,
			&user.RegionID,
			&user.RegionName,
			&user.RequestCreatedAt,
			&user.VouchCount,
		); err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	return users, rows.Err()
}

// GetByNameAndType retrieves a region by name and type (for finding existing state/county/city regions)
func (r *RegionRepository) GetByNameAndType(ctx context.Context, name string, regionType models.RegionType) (*models.GeographicRegion, error) {
	query := `
		SELECT id, name, region_type, parent_region_id, created_by, created_at
		FROM geographic_regions WHERE name = ? AND region_type = ?
	`

	region := &models.GeographicRegion{}
	err := r.db.QueryRowContext(ctx, query, name, regionType).Scan(
		&region.ID,
		&region.Name,
		&region.RegionType,
		&region.ParentRegionID,
		&region.CreatedBy,
		&region.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRegionNotFound
	}
	if err != nil {
		return nil, err
	}

	return region, nil
}

// GetByNameTypeAndParent retrieves a region by name, type, and parent (for finding existing locality/neighborhood regions)
func (r *RegionRepository) GetByNameTypeAndParent(ctx context.Context, name string, regionType models.RegionType, parentID string) (*models.GeographicRegion, error) {
	query := `
		SELECT id, name, region_type, parent_region_id, created_by, created_at
		FROM geographic_regions WHERE name = ? AND region_type = ? AND parent_region_id = ?
	`

	region := &models.GeographicRegion{}
	err := r.db.QueryRowContext(ctx, query, name, regionType, parentID).Scan(
		&region.ID,
		&region.Name,
		&region.RegionType,
		&region.ParentRegionID,
		&region.CreatedBy,
		&region.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRegionNotFound
	}
	if err != nil {
		return nil, err
	}

	return region, nil
}

// Delete deletes a region and all its related data (cascades to sub-regions, user_regions, signal_groups)
func (r *RegionRepository) Delete(ctx context.Context, regionID string) error {
	// Start a transaction to ensure atomicity
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Delete signal groups in this region
	if _, err := tx.ExecContext(ctx, `DELETE FROM signal_groups WHERE region_id = ?`, regionID); err != nil {
		return err
	}

	// Delete user_regions associations
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_regions WHERE region_id = ?`, regionID); err != nil {
		return err
	}

	// Recursively delete child regions (find all children first)
	var childIDs []string
	rows, err := tx.QueryContext(ctx, `SELECT id FROM geographic_regions WHERE parent_region_id = ?`, regionID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var childID string
		if err := rows.Scan(&childID); err != nil {
			_ = rows.Close()
			return err
		}
		childIDs = append(childIDs, childID)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Delete each child region recursively (outside transaction, each will have its own)
	// For simplicity, we'll delete children in the same transaction
	for _, childID := range childIDs {
		// Delete child's signal groups
		if _, err := tx.ExecContext(ctx, `DELETE FROM signal_groups WHERE region_id = ?`, childID); err != nil {
			return err
		}
		// Delete child's user_regions
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_regions WHERE region_id = ?`, childID); err != nil {
			return err
		}
		// Delete the child region itself
		if _, err := tx.ExecContext(ctx, `DELETE FROM geographic_regions WHERE id = ?`, childID); err != nil {
			return err
		}
	}

	// Finally, delete the region itself
	result, err := tx.ExecContext(ctx, `DELETE FROM geographic_regions WHERE id = ?`, regionID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrRegionNotFound
	}

	return tx.Commit()
}

// GetCityByNameAndState retrieves a city region by name and state (case-insensitive)
// Joins through the hierarchy: city -> county -> state to verify the city is in the correct state
func (r *RegionRepository) GetCityByNameAndState(ctx context.Context, cityName, stateName string) (*models.GeographicRegion, error) {
	// Join city -> county -> state to find city in the correct state
	query := `
		SELECT city.id, city.name, city.region_type, city.parent_region_id, city.created_by, city.created_at
		FROM geographic_regions city
		JOIN geographic_regions county ON city.parent_region_id = county.id
		JOIN geographic_regions state ON county.parent_region_id = state.id
		WHERE LOWER(city.name) = LOWER(?)
		  AND city.region_type = 'city'
		  AND county.region_type = 'county'
		  AND state.region_type = 'state'
		  AND LOWER(state.name) = LOWER(?)
	`

	region := &models.GeographicRegion{}
	err := r.db.QueryRowContext(ctx, query, cityName, stateName).Scan(
		&region.ID,
		&region.Name,
		&region.RegionType,
		&region.ParentRegionID,
		&region.CreatedBy,
		&region.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRegionNotFound
	}
	if err != nil {
		return nil, err
	}

	return region, nil
}

// AddUserToRegionPending adds a user to a region with pending verification status
// Used when a user requests vouch verification for a city
func (r *RegionRepository) AddUserToRegionPending(ctx context.Context, userID, regionID string) (string, error) {
	id := uuid.New().String()
	query := `
		INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at)
		VALUES (?, ?, ?, FALSE, 'pending', ?)
	`

	_, err := r.db.ExecContext(ctx, query, id, userID, regionID, time.Now().UTC())
	if err != nil {
		// Check for duplicate key error
		if strings.Contains(err.Error(), "Duplicate entry") {
			return "", errors.New("user already has a membership request for this region")
		}
		return "", err
	}
	return id, nil
}

// GetUserRegionByStatus retrieves a user's region membership by status
func (r *RegionRepository) GetUserRegionByStatus(ctx context.Context, userID, regionID, status string) (*models.UserRegion, error) {
	query := `
		SELECT id, user_id, region_id, is_admin, verification_status, verified_at
		FROM user_regions
		WHERE user_id = ? AND region_id = ? AND verification_status = ?
	`

	ur := &models.UserRegion{}
	err := r.db.QueryRowContext(ctx, query, userID, regionID, status).Scan(
		&ur.ID,
		&ur.UserID,
		&ur.RegionID,
		&ur.IsAdmin,
		&ur.VerificationStatus,
		&ur.VerifiedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // Not found is not an error - return nil
	}
	if err != nil {
		return nil, err
	}

	return ur, nil
}

// GetUserRegion retrieves a user's region membership (any status)
func (r *RegionRepository) GetUserRegion(ctx context.Context, userID, regionID string) (*models.UserRegion, error) {
	query := `
		SELECT id, user_id, region_id, is_admin, verification_status, verified_at
		FROM user_regions
		WHERE user_id = ? AND region_id = ?
	`

	ur := &models.UserRegion{}
	err := r.db.QueryRowContext(ctx, query, userID, regionID).Scan(
		&ur.ID,
		&ur.UserID,
		&ur.RegionID,
		&ur.IsAdmin,
		&ur.VerificationStatus,
		&ur.VerifiedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // Not found is not an error - return nil
	}
	if err != nil {
		return nil, err
	}

	return ur, nil
}

// UpgradeUserRegionToVerified upgrades a pending user_region to verified status
// and sets is_admin based on whether user has both postcard and vouch verification
func (r *RegionRepository) UpgradeUserRegionToVerified(ctx context.Context, userID, regionID string, isAdmin bool) error {
	query := `
		UPDATE user_regions
		SET verification_status = 'verified', is_admin = ?, verified_at = ?
		WHERE user_id = ? AND region_id = ? AND verification_status = 'pending'
	`

	result, err := r.db.ExecContext(ctx, query, isAdmin, time.Now().UTC(), userID, regionID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return errors.New("no pending user_region found to upgrade")
	}

	return nil
}

// SetUserRegionAdmin updates the is_admin flag for an already-verified user_region
// Used when a postcard-verified user receives enough vouches to become vouch-verified
func (r *RegionRepository) SetUserRegionAdmin(ctx context.Context, userID, regionID string, isAdmin bool) error {
	query := `
		UPDATE user_regions
		SET is_admin = ?
		WHERE user_id = ? AND region_id = ? AND verification_status = 'verified'
	`

	result, err := r.db.ExecContext(ctx, query, isAdmin, userID, regionID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return errors.New("no verified user_region found to update")
	}

	return nil
}

// UpgradeAllPendingUserRegions upgrades all pending user_region entries for a user to verified status
// Used when a superuser grants vouch verification to a user
func (r *RegionRepository) UpgradeAllPendingUserRegions(ctx context.Context, userID string, isAdmin bool) (int64, error) {
	query := `
		UPDATE user_regions
		SET verification_status = 'verified', is_admin = ?, verified_at = ?
		WHERE user_id = ? AND verification_status = 'pending'
	`

	result, err := r.db.ExecContext(ctx, query, isAdmin, time.Now().UTC(), userID)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// UpgradeVerifiedUserRegionsToAdmin upgrades is_admin to true for all verified user_region entries
// Used when a user becomes a full admin (has both postcard and vouch verification)
func (r *RegionRepository) UpgradeVerifiedUserRegionsToAdmin(ctx context.Context, userID string) (int64, error) {
	query := `
		UPDATE user_regions
		SET is_admin = TRUE
		WHERE user_id = ? AND verification_status = 'verified' AND is_admin = FALSE
	`

	result, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// GetFullAdminCount counts users with both postcard_verified=true AND vouch_verified=true AND is_admin=true
// These are "full admins" who have completed both verifications
// Admin count propagates UP the hierarchy: admins in child regions count towards parent regions
func (r *RegionRepository) GetFullAdminCount(ctx context.Context, regionID string) (int, error) {
	// Count full admins in this region AND all regions geographically contained within it
	// or linked via parent_region_id (for regions without geometry)
	// This allows admin count to propagate up the hierarchy (city admins count for county/state)
	query := `
		SELECT COUNT(DISTINCT u.id)
		FROM users u
		JOIN user_regions ur ON u.id = ur.user_id
		JOIN geographic_regions child ON ur.region_id = child.id
		WHERE ur.is_admin = TRUE
			AND u.postcard_verified = TRUE
			AND u.vouch_verified = TRUE
			AND (
				child.id = ?
				OR child.parent_region_id = ?
				OR (
					child.geometry IS NOT NULL
					AND (SELECT geometry FROM geographic_regions WHERE id = ?) IS NOT NULL
					AND ST_Contains(
						(SELECT geometry FROM geographic_regions WHERE id = ?),
						child.geometry
					)
				)
			)
	`
	var count int
	err := r.db.QueryRowContext(ctx, query, regionID, regionID, regionID, regionID).Scan(&count)
	return count, err
}

// IsRegionInBootstrapMode checks if a region is in bootstrap mode (< 3 full admins)
// Returns (isBootstrap, fullAdminCount, error)
func (r *RegionRepository) IsRegionInBootstrapMode(ctx context.Context, regionID string) (bool, int, error) {
	count, err := r.GetFullAdminCount(ctx, regionID)
	if err != nil {
		return false, 0, err
	}
	// Bootstrap mode when fewer than 3 full admins
	const minAdminsToEndBootstrap = 3
	return count < minAdminsToEndBootstrap, count, nil
}

// GetUserPendingVouchRegion retrieves the most specific region where a user has a pending vouch request.
// Orders by specificity so the most granular region (city_block > neighborhood > locality > city > county > state) is returned first.
func (r *RegionRepository) GetUserPendingVouchRegion(ctx context.Context, userID string) (*models.GeographicRegion, error) {
	query := `
		SELECT gr.id, gr.name, gr.region_type, gr.parent_region_id, gr.created_by, gr.created_at
		FROM user_regions ur
		JOIN geographic_regions gr ON ur.region_id = gr.id
		WHERE ur.user_id = ? AND ur.verification_status = 'pending'
		ORDER BY CASE gr.region_type
			WHEN 'city_block' THEN 1
			WHEN 'neighborhood' THEN 2
			WHEN 'locality' THEN 3
			WHEN 'city' THEN 4
			WHEN 'county' THEN 5
			WHEN 'state' THEN 6
			ELSE 7
		END ASC
		LIMIT 1
	`

	region := &models.GeographicRegion{}
	err := r.db.QueryRowContext(ctx, query, userID).Scan(
		&region.ID,
		&region.Name,
		&region.RegionType,
		&region.ParentRegionID,
		&region.CreatedBy,
		&region.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // Not found is not an error
	}
	if err != nil {
		return nil, err
	}

	return region, nil
}

// GetUserVerifiedRegion retrieves a region where a user has verified status
// Used for bootstrap mode where postcard-verified users can receive vouches
func (r *RegionRepository) GetUserVerifiedRegion(ctx context.Context, userID string) (*models.GeographicRegion, error) {
	query := `
		SELECT gr.id, gr.name, gr.region_type, gr.parent_region_id, gr.created_by, gr.created_at
		FROM user_regions ur
		JOIN geographic_regions gr ON ur.region_id = gr.id
		WHERE ur.user_id = ? AND ur.verification_status = 'verified'
		LIMIT 1
	`

	verifiedRegion := &models.GeographicRegion{}
	err := r.db.QueryRowContext(ctx, query, userID).Scan(
		&verifiedRegion.ID,
		&verifiedRegion.Name,
		&verifiedRegion.RegionType,
		&verifiedRegion.ParentRegionID,
		&verifiedRegion.CreatedBy,
		&verifiedRegion.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // Not found is not an error
	}
	if err != nil {
		return nil, err
	}

	return verifiedRegion, nil
}

// --- Transactional methods for race condition prevention ---

// GetByNameAndTypeForUpdate locks a region by name and type within a transaction
func (r *RegionRepository) GetByNameAndTypeForUpdate(ctx context.Context, tx *sql.Tx, name string, regionType models.RegionType) (*models.GeographicRegion, error) {
	query := `
		SELECT id, name, region_type, parent_region_id, created_by, created_at
		FROM geographic_regions WHERE name = ? AND region_type = ?
		FOR UPDATE
	`

	region := &models.GeographicRegion{}
	err := tx.QueryRowContext(ctx, query, name, regionType).Scan(
		&region.ID,
		&region.Name,
		&region.RegionType,
		&region.ParentRegionID,
		&region.CreatedBy,
		&region.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRegionNotFound
	}
	if err != nil {
		return nil, err
	}

	return region, nil
}

// GetByNameTypeAndParentForUpdate locks a region by name, type, and parent within a transaction
func (r *RegionRepository) GetByNameTypeAndParentForUpdate(ctx context.Context, tx *sql.Tx, name string, regionType models.RegionType, parentID string) (*models.GeographicRegion, error) {
	query := `
		SELECT id, name, region_type, parent_region_id, created_by, created_at
		FROM geographic_regions WHERE name = ? AND region_type = ? AND parent_region_id = ?
		FOR UPDATE
	`

	region := &models.GeographicRegion{}
	err := tx.QueryRowContext(ctx, query, name, regionType, parentID).Scan(
		&region.ID,
		&region.Name,
		&region.RegionType,
		&region.ParentRegionID,
		&region.CreatedBy,
		&region.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRegionNotFound
	}
	if err != nil {
		return nil, err
	}

	return region, nil
}

// CreateTx creates a new region within a transaction (with optional geometry)
func (r *RegionRepository) CreateTx(ctx context.Context, tx *sql.Tx, region *models.GeographicRegion, geoJSON string) error {
	region.ID = uuid.New().String()
	region.CreatedAt = time.Now().UTC()

	if geoJSON == "" {
		query := `
			INSERT INTO geographic_regions (id, name, region_type, parent_region_id, geometry, created_by, created_at)
			VALUES (?, ?, ?, ?, NULL, ?, ?)
		`
		_, err := tx.ExecContext(ctx, query,
			region.ID,
			region.Name,
			region.RegionType,
			region.ParentRegionID,
			region.CreatedBy,
			region.CreatedAt,
		)
		return err
	}

	query := `
		INSERT INTO geographic_regions (id, name, region_type, parent_region_id, geometry, created_by, created_at)
		VALUES (?, ?, ?, ?, ST_GeomFromGeoJSON(?), ?, ?)
	`
	_, err := tx.ExecContext(ctx, query,
		region.ID,
		region.Name,
		region.RegionType,
		region.ParentRegionID,
		geoJSON,
		region.CreatedBy,
		region.CreatedAt,
	)
	return err
}

// AddUserToRegionTx adds a user to a region within a transaction
func (r *RegionRepository) AddUserToRegionTx(ctx context.Context, tx *sql.Tx, userID, regionID string, isAdmin bool) error {
	id := uuid.New().String()
	query := `
		INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at)
		VALUES (?, ?, ?, ?, 'verified', ?)
		ON DUPLICATE KEY UPDATE is_admin = GREATEST(is_admin, VALUES(is_admin)), verification_status = 'verified'
	`

	_, err := tx.ExecContext(ctx, query, id, userID, regionID, isAdmin, time.Now().UTC())
	return err
}

// AddUserToRegionPendingTx adds a user to a region with pending status within a transaction
func (r *RegionRepository) AddUserToRegionPendingTx(ctx context.Context, tx *sql.Tx, userID, regionID string) (string, error) {
	id := uuid.New().String()
	query := `
		INSERT INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at)
		VALUES (?, ?, ?, FALSE, 'pending', ?)
	`

	_, err := tx.ExecContext(ctx, query, id, userID, regionID, time.Now().UTC())
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") {
			return "", errors.New("user already has a membership request for this region")
		}
		return "", err
	}
	return id, nil
}

// GetUserRegionForUpdate locks a user_region row within a transaction
func (r *RegionRepository) GetUserRegionForUpdate(ctx context.Context, tx *sql.Tx, userID, regionID string) (*models.UserRegion, error) {
	query := `
		SELECT id, user_id, region_id, is_admin, verification_status, verified_at
		FROM user_regions
		WHERE user_id = ? AND region_id = ?
		FOR UPDATE
	`

	ur := &models.UserRegion{}
	err := tx.QueryRowContext(ctx, query, userID, regionID).Scan(
		&ur.ID,
		&ur.UserID,
		&ur.RegionID,
		&ur.IsAdmin,
		&ur.VerificationStatus,
		&ur.VerifiedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return ur, nil
}

// IsRegionInBootstrapModeTx checks bootstrap mode within a transaction
func (r *RegionRepository) IsRegionInBootstrapModeTx(ctx context.Context, tx *sql.Tx, regionID string) (bool, int, error) {
	query := `
		SELECT COUNT(DISTINCT u.id)
		FROM users u
		JOIN user_regions ur ON u.id = ur.user_id
		JOIN geographic_regions child ON ur.region_id = child.id
		WHERE ur.is_admin = TRUE
			AND u.postcard_verified = TRUE
			AND u.vouch_verified = TRUE
			AND (
				child.id = ?
				OR child.parent_region_id = ?
				OR (
					child.geometry IS NOT NULL
					AND (SELECT geometry FROM geographic_regions WHERE id = ?) IS NOT NULL
					AND ST_Contains(
						(SELECT geometry FROM geographic_regions WHERE id = ?),
						child.geometry
					)
				)
			)
	`
	var count int
	err := tx.QueryRowContext(ctx, query, regionID, regionID, regionID, regionID).Scan(&count)
	if err != nil {
		return false, 0, err
	}
	const minAdminsToEndBootstrap = 3
	return count < minAdminsToEndBootstrap, count, nil
}

// UpgradeUserRegionToVerifiedTx upgrades a pending user_region within a transaction
func (r *RegionRepository) UpgradeUserRegionToVerifiedTx(ctx context.Context, tx *sql.Tx, userID, regionID string, isAdmin bool) error {
	query := `
		UPDATE user_regions
		SET verification_status = 'verified', is_admin = ?, verified_at = ?
		WHERE user_id = ? AND region_id = ? AND verification_status = 'pending'
	`

	result, err := tx.ExecContext(ctx, query, isAdmin, time.Now().UTC(), userID, regionID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return errors.New("no pending user_region found to upgrade")
	}

	return nil
}

// SetUserRegionAdminTx updates is_admin for a verified user_region within a transaction
func (r *RegionRepository) SetUserRegionAdminTx(ctx context.Context, tx *sql.Tx, userID, regionID string, isAdmin bool) error {
	query := `
		UPDATE user_regions
		SET is_admin = ?
		WHERE user_id = ? AND region_id = ? AND verification_status = 'verified'
	`

	result, err := tx.ExecContext(ctx, query, isAdmin, userID, regionID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return errors.New("no verified user_region found to update")
	}

	return nil
}

// UpgradeVerifiedUserRegionsToAdminTx upgrades all verified user_region entries to admin within a transaction
func (r *RegionRepository) UpgradeVerifiedUserRegionsToAdminTx(ctx context.Context, tx *sql.Tx, userID string) (int64, error) {
	query := `
		UPDATE user_regions
		SET is_admin = TRUE
		WHERE user_id = ? AND verification_status = 'verified' AND is_admin = FALSE
	`

	result, err := tx.ExecContext(ctx, query, userID)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// RegionUser represents a user in a region for listing purposes
type RegionUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"is_admin"`
}

// GetUsersInRegion returns all verified users in a specific region and its child regions
// Excludes superusers and blocked users
func (r *RegionRepository) GetUsersInRegion(ctx context.Context, regionID string) ([]RegionUser, error) {
	// Use recursive CTE to get the region and all its child regions
	query := `
		WITH RECURSIVE region_tree AS (
			-- Base case: the specified region
			SELECT id FROM geographic_regions WHERE id = ?

			UNION ALL

			-- Recursive case: child regions
			SELECT gr.id
			FROM geographic_regions gr
			INNER JOIN region_tree rt ON gr.parent_region_id = rt.id
		)
		SELECT DISTINCT u.id, u.username, u.email,
			COALESCE(MAX(ur.is_admin), FALSE) as is_admin
		FROM users u
		JOIN user_regions ur ON u.id = ur.user_id
		WHERE ur.region_id IN (SELECT id FROM region_tree)
			AND ur.verification_status = 'verified'
			AND u.is_blocked = FALSE
			AND u.is_superuser = FALSE
		GROUP BY u.id, u.username, u.email
		ORDER BY u.username
	`

	rows, err := r.db.QueryContext(ctx, query, regionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var users []RegionUser
	for rows.Next() {
		var user RegionUser
		if err := rows.Scan(&user.ID, &user.Username, &user.Email, &user.IsAdmin); err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

// GetAncestorRegions returns the ancestor chain for a region (parent → grandparent → ... → state)
// ordered from most specific to least specific
func (r *RegionRepository) GetAncestorRegions(ctx context.Context, regionID string) ([]models.GeographicRegion, error) {
	query := `
		WITH RECURSIVE ancestors AS (
			SELECT gr.id, gr.name, gr.region_type, gr.parent_region_id, gr.created_by, gr.created_at
			FROM geographic_regions gr
			JOIN geographic_regions child ON gr.id = child.parent_region_id
			WHERE child.id = ?
			UNION ALL
			SELECT gr.id, gr.name, gr.region_type, gr.parent_region_id, gr.created_by, gr.created_at
			FROM geographic_regions gr
			JOIN ancestors a ON gr.id = a.parent_region_id
		)
		SELECT id, name, region_type, parent_region_id, created_by, created_at
		FROM ancestors
	`

	rows, err := r.db.QueryContext(ctx, query, regionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var regions []models.GeographicRegion
	for rows.Next() {
		var region models.GeographicRegion
		if err := rows.Scan(
			&region.ID,
			&region.Name,
			&region.RegionType,
			&region.ParentRegionID,
			&region.CreatedBy,
			&region.CreatedAt,
		); err != nil {
			return nil, err
		}
		regions = append(regions, region)
	}

	return regions, rows.Err()
}

// GetAncestorRegionsTx returns the ancestor chain for a region within a transaction
func (r *RegionRepository) GetAncestorRegionsTx(ctx context.Context, tx *sql.Tx, regionID string) ([]models.GeographicRegion, error) {
	query := `
		WITH RECURSIVE ancestors AS (
			SELECT gr.id, gr.name, gr.region_type, gr.parent_region_id, gr.created_by, gr.created_at
			FROM geographic_regions gr
			JOIN geographic_regions child ON gr.id = child.parent_region_id
			WHERE child.id = ?
			UNION ALL
			SELECT gr.id, gr.name, gr.region_type, gr.parent_region_id, gr.created_by, gr.created_at
			FROM geographic_regions gr
			JOIN ancestors a ON gr.id = a.parent_region_id
		)
		SELECT id, name, region_type, parent_region_id, created_by, created_at
		FROM ancestors
	`

	rows, err := tx.QueryContext(ctx, query, regionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var regions []models.GeographicRegion
	for rows.Next() {
		var region models.GeographicRegion
		if err := rows.Scan(
			&region.ID,
			&region.Name,
			&region.RegionType,
			&region.ParentRegionID,
			&region.CreatedBy,
			&region.CreatedAt,
		); err != nil {
			return nil, err
		}
		regions = append(regions, region)
	}

	return regions, rows.Err()
}

// AddUserToAncestorRegionsPendingTx adds pending user_region entries for all ancestors of a region.
// Ignores duplicate key errors (user may already be in that region).
func (r *RegionRepository) AddUserToAncestorRegionsPendingTx(ctx context.Context, tx *sql.Tx, userID, regionID string) error {
	ancestors, err := r.GetAncestorRegionsTx(ctx, tx, regionID)
	if err != nil {
		return err
	}

	for _, ancestor := range ancestors {
		_, err := r.AddUserToRegionPendingTx(ctx, tx, userID, ancestor.ID)
		if err != nil {
			// Ignore duplicate key errors — user may already be in this region
			if strings.Contains(err.Error(), "Duplicate entry") ||
				strings.Contains(err.Error(), "already has a membership") {
				continue
			}
			return err
		}
	}

	return nil
}

// AddUserToAncestorRegionsPending adds pending user_region entries for all ancestors of a region (non-transactional).
// Ignores duplicate key errors (user may already be in that region).
func (r *RegionRepository) AddUserToAncestorRegionsPending(ctx context.Context, userID, regionID string) error {
	ancestors, err := r.GetAncestorRegions(ctx, regionID)
	if err != nil {
		return err
	}

	for _, ancestor := range ancestors {
		_, err := r.AddUserToRegionPending(ctx, userID, ancestor.ID)
		if err != nil {
			// Ignore duplicate key errors — user may already be in this region
			if strings.Contains(err.Error(), "Duplicate entry") ||
				strings.Contains(err.Error(), "already has a membership") {
				continue
			}
			return err
		}
	}

	return nil
}

// GetUserPendingVouchRegions retrieves ALL regions where a user has a pending vouch request,
// ordered most-specific first
func (r *RegionRepository) GetUserPendingVouchRegions(ctx context.Context, userID string) ([]models.GeographicRegion, error) {
	query := `
		SELECT gr.id, gr.name, gr.region_type, gr.parent_region_id, gr.created_by, gr.created_at
		FROM user_regions ur
		JOIN geographic_regions gr ON ur.region_id = gr.id
		WHERE ur.user_id = ? AND ur.verification_status = 'pending'
		ORDER BY CASE gr.region_type
			WHEN 'city_block' THEN 1 WHEN 'neighborhood' THEN 2 WHEN 'locality' THEN 3
			WHEN 'city' THEN 4 WHEN 'county' THEN 5 WHEN 'state' THEN 6
			ELSE 7
		END ASC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var regions []models.GeographicRegion
	for rows.Next() {
		var region models.GeographicRegion
		if err := rows.Scan(
			&region.ID,
			&region.Name,
			&region.RegionType,
			&region.ParentRegionID,
			&region.CreatedBy,
			&region.CreatedAt,
		); err != nil {
			return nil, err
		}
		regions = append(regions, region)
	}

	return regions, rows.Err()
}

// upgradeUserRegionAndAncestorsToVerified is the shared implementation for upgrading
// pending user_region entries for the target region AND all its ancestors.
// Uses a two-step approach (get ancestors then UPDATE with explicit IDs) to avoid
// MariaDB compatibility issues with CTEs inside IN() subqueries.
func (r *RegionRepository) upgradeUserRegionAndAncestorsToVerified(ctx context.Context, execer interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}, getAncestors func(ctx context.Context, regionID string) ([]models.GeographicRegion, error), userID, regionID string, isAdmin bool) error {
	ancestors, err := getAncestors(ctx, regionID)
	if err != nil {
		return err
	}

	regionIDs := make([]interface{}, 0, len(ancestors)+1)
	regionIDs = append(regionIDs, regionID)
	for _, a := range ancestors {
		regionIDs = append(regionIDs, a.ID)
	}

	placeholders := strings.Repeat("?,", len(regionIDs))
	placeholders = placeholders[:len(placeholders)-1] // trim trailing comma

	query := `
		UPDATE user_regions
		SET verification_status = 'verified', is_admin = ?, verified_at = ?
		WHERE user_id = ?
			AND verification_status = 'pending'
			AND region_id IN (` + placeholders + `)
	`

	now := time.Now().UTC()
	args := make([]interface{}, 0, 3+len(regionIDs))
	args = append(args, isAdmin, now, userID)
	args = append(args, regionIDs...)

	_, err = execer.ExecContext(ctx, query, args...)
	return err
}

// UpgradeUserRegionAndAncestorsToVerifiedTx upgrades pending user_region entries
// for the target region AND all its ancestors within a transaction.
func (r *RegionRepository) UpgradeUserRegionAndAncestorsToVerifiedTx(ctx context.Context, tx *sql.Tx, userID, regionID string, isAdmin bool) error {
	return r.upgradeUserRegionAndAncestorsToVerified(ctx, tx, func(ctx context.Context, regionID string) ([]models.GeographicRegion, error) {
		return r.GetAncestorRegionsTx(ctx, tx, regionID)
	}, userID, regionID, isAdmin)
}

// UpgradeUserRegionAndAncestorsToVerified upgrades pending user_region entries
// for the target region AND all its ancestors (non-transactional).
func (r *RegionRepository) UpgradeUserRegionAndAncestorsToVerified(ctx context.Context, userID, regionID string, isAdmin bool) error {
	return r.upgradeUserRegionAndAncestorsToVerified(ctx, r.db, func(ctx context.Context, regionID string) ([]models.GeographicRegion, error) {
		return r.GetAncestorRegions(ctx, regionID)
	}, userID, regionID, isAdmin)
}

// GetMostSpecificSharedPendingRegion finds the most specific region where the voucher
// is verified AND the vouchee has a pending membership, excluding state-level regions.
func (r *RegionRepository) GetMostSpecificSharedPendingRegion(ctx context.Context, voucherID, voucheeID string) (*models.GeographicRegion, error) {
	query := `
		SELECT gr.id, gr.name, gr.region_type, gr.parent_region_id, gr.created_by, gr.created_at
		FROM user_regions ur_voucher
		JOIN user_regions ur_vouchee ON ur_voucher.region_id = ur_vouchee.region_id
		JOIN geographic_regions gr ON ur_voucher.region_id = gr.id
		WHERE ur_voucher.user_id = ? AND ur_voucher.verification_status = 'verified'
			AND ur_vouchee.user_id = ? AND ur_vouchee.verification_status = 'pending'
			AND gr.region_type != 'state'
		ORDER BY CASE gr.region_type
			WHEN 'city_block' THEN 1 WHEN 'neighborhood' THEN 2 WHEN 'locality' THEN 3
			WHEN 'city' THEN 4 WHEN 'county' THEN 5 ELSE 6
		END ASC
		LIMIT 1
	`

	region := &models.GeographicRegion{}
	err := r.db.QueryRowContext(ctx, query, voucherID, voucheeID).Scan(
		&region.ID,
		&region.Name,
		&region.RegionType,
		&region.ParentRegionID,
		&region.CreatedBy,
		&region.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return region, nil
}
