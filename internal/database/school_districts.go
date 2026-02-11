package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// SchoolDistrictRepository handles school district database operations
type SchoolDistrictRepository struct {
	db *DB
}

// NewSchoolDistrictRepository creates a new school district repository
func NewSchoolDistrictRepository(db *DB) *SchoolDistrictRepository {
	return &SchoolDistrictRepository{db: db}
}

// GetByID retrieves a school district by ID, converting spatial geometry to GeoJSON
func (r *SchoolDistrictRepository) GetByID(ctx context.Context, id string) (*models.SchoolDistrict, error) {
	query := `
		SELECT id, nces_id, name, state, district_type,
			CASE WHEN geometry IS NOT NULL THEN ST_AsGeoJSON(geometry) ELSE NULL END,
			created_at
		FROM school_districts
		WHERE id = ?
	`

	district := &models.SchoolDistrict{}
	var geoJSONStr *string
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&district.ID,
		&district.NCESID,
		&district.Name,
		&district.State,
		&district.DistrictType,
		&geoJSONStr,
		&district.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDistrictNotFound
	}
	if err != nil {
		return nil, err
	}

	if geoJSONStr != nil {
		var geom models.GeoJSONGeometry
		if jsonErr := json.Unmarshal([]byte(*geoJSONStr), &geom); jsonErr == nil {
			district.Geometry = &geom
		}
	}

	return district, nil
}

// GetByNCESID retrieves a school district by NCES ID, converting spatial geometry to GeoJSON
func (r *SchoolDistrictRepository) GetByNCESID(ctx context.Context, ncesID string) (*models.SchoolDistrict, error) {
	query := `
		SELECT id, nces_id, name, state, district_type,
			CASE WHEN geometry IS NOT NULL THEN ST_AsGeoJSON(geometry) ELSE NULL END,
			created_at
		FROM school_districts
		WHERE nces_id = ?
	`

	district := &models.SchoolDistrict{}
	var geoJSONStr *string
	err := r.db.QueryRowContext(ctx, query, ncesID).Scan(
		&district.ID,
		&district.NCESID,
		&district.Name,
		&district.State,
		&district.DistrictType,
		&geoJSONStr,
		&district.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDistrictNotFound
	}
	if err != nil {
		return nil, err
	}

	if geoJSONStr != nil {
		var geom models.GeoJSONGeometry
		if jsonErr := json.Unmarshal([]byte(*geoJSONStr), &geom); jsonErr == nil {
			district.Geometry = &geom
		}
	}

	return district, nil
}

// UpsertByNCESID inserts or updates a school district by NCES ID
func (r *SchoolDistrictRepository) UpsertByNCESID(ctx context.Context, district *models.SchoolDistrict) error {
	if district.ID == "" {
		district.ID = uuid.New().String()
	}
	district.CreatedAt = time.Now().UTC()

	// Serialize geometry to GeoJSON string for ST_GeomFromGeoJSON, or nil
	var geometryParam interface{}
	if district.Geometry != nil {
		geoBytes, err := json.Marshal(district.Geometry)
		if err != nil {
			return fmt.Errorf("failed to marshal geometry: %w", err)
		}
		geometryParam = string(geoBytes)
	}

	query := `
		INSERT INTO school_districts (id, nces_id, name, state, district_type, geometry, created_at)
		VALUES (?, ?, ?, ?, ?, CASE WHEN ? IS NOT NULL THEN ST_GeomFromGeoJSON(?) ELSE NULL END, ?)
		ON DUPLICATE KEY UPDATE
			name = VALUES(name),
			state = VALUES(state),
			district_type = VALUES(district_type),
			geometry = VALUES(geometry)
	`

	_, err := r.db.ExecContext(ctx, query,
		district.ID,
		district.NCESID,
		district.Name,
		district.State,
		district.DistrictType,
		geometryParam,
		geometryParam,
		district.CreatedAt,
	)

	return err
}

// GetNCESIDMap returns a map of NCES ID -> UUID for all districts, used for bulk lookups
func (r *SchoolDistrictRepository) GetNCESIDMap(ctx context.Context) (map[string]string, error) {
	query := `SELECT nces_id, id FROM school_districts`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]string)
	for rows.Next() {
		var ncesID, id string
		if err := rows.Scan(&ncesID, &id); err != nil {
			return nil, err
		}
		result[ncesID] = id
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// BatchUpsertByNCESID inserts or updates multiple school districts in a single multi-row INSERT
func (r *SchoolDistrictRepository) BatchUpsertByNCESID(ctx context.Context, districts []*models.SchoolDistrict) error {
	if len(districts) == 0 {
		return nil
	}

	now := time.Now().UTC()
	var queryBuilder strings.Builder
	queryBuilder.WriteString(`
		INSERT INTO school_districts (id, nces_id, name, state, district_type, geometry, created_at)
		VALUES `)

	args := make([]interface{}, 0, len(districts)*8)
	for i, district := range districts {
		if district.ID == "" {
			district.ID = uuid.New().String()
		}
		district.CreatedAt = now

		if i > 0 {
			queryBuilder.WriteString(", ")
		}
		queryBuilder.WriteString("(?, ?, ?, ?, ?, CASE WHEN ? IS NOT NULL THEN ST_GeomFromGeoJSON(?) ELSE NULL END, ?)")

		var geometryParam interface{}
		if district.Geometry != nil {
			geoBytes, err := json.Marshal(district.Geometry)
			if err != nil {
				return fmt.Errorf("failed to marshal geometry for district %s: %w", district.NCESID, err)
			}
			geometryParam = string(geoBytes)
		}

		args = append(args, district.ID, district.NCESID, district.Name, district.State, district.DistrictType, geometryParam, geometryParam, district.CreatedAt)
	}

	queryBuilder.WriteString(`
		ON DUPLICATE KEY UPDATE
			name = VALUES(name),
			state = VALUES(state),
			district_type = VALUES(district_type),
			geometry = VALUES(geometry)`)

	_, err := r.db.ExecContext(ctx, queryBuilder.String(), args...)
	return err
}

// Search searches school districts by name with optional state filter.
// Does not return geometry (would be too much data for a listing).
func (r *SchoolDistrictRepository) Search(ctx context.Context, query string, state string) ([]models.SchoolDistrict, error) {
	sqlQuery := `
		SELECT id, nces_id, name, state, district_type, created_at
		FROM school_districts
		WHERE name LIKE ?
	`
	args := []interface{}{"%" + query + "%"}

	if state != "" {
		sqlQuery += " AND state = ?"
		args = append(args, state)
	}

	sqlQuery += " ORDER BY name LIMIT 50"

	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var districts []models.SchoolDistrict
	for rows.Next() {
		var district models.SchoolDistrict
		if err := rows.Scan(
			&district.ID,
			&district.NCESID,
			&district.Name,
			&district.State,
			&district.DistrictType,
			&district.CreatedAt,
		); err != nil {
			return nil, err
		}
		districts = append(districts, district)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return districts, nil
}

// UpdateGeometry stores a GeoJSON geometry for a district identified by NCES ID
func (r *SchoolDistrictRepository) UpdateGeometry(ctx context.Context, ncesID, geoJSON string) error {
	query := `
		UPDATE school_districts SET geometry = ST_GeomFromGeoJSON(?) WHERE nces_id = ?
	`
	_, err := r.db.ExecContext(ctx, query, geoJSON, ncesID)
	return err
}
