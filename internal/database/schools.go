package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// SchoolRepository handles school database operations
type SchoolRepository struct {
	db *DB
}

// NewSchoolRepository creates a new school repository
func NewSchoolRepository(db *DB) *SchoolRepository {
	return &SchoolRepository{db: db}
}

// GetByID retrieves a school by ID
func (r *SchoolRepository) GetByID(ctx context.Context, id string) (*models.School, error) {
	query := `
		SELECT id, nces_id, district_id, name, street_address, city, state, zip,
			latitude, longitude, region_id, created_at
		FROM schools
		WHERE id = ?
	`

	school := &models.School{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&school.ID,
		&school.NCESID,
		&school.DistrictID,
		&school.Name,
		&school.StreetAddress,
		&school.City,
		&school.State,
		&school.Zip,
		&school.Latitude,
		&school.Longitude,
		&school.RegionID,
		&school.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSchoolNotFound
	}
	if err != nil {
		return nil, err
	}

	return school, nil
}

// GetByNCESID retrieves a school by NCES ID
func (r *SchoolRepository) GetByNCESID(ctx context.Context, ncesID string) (*models.School, error) {
	query := `
		SELECT id, nces_id, district_id, name, street_address, city, state, zip,
			latitude, longitude, region_id, created_at
		FROM schools
		WHERE nces_id = ?
	`

	school := &models.School{}
	err := r.db.QueryRowContext(ctx, query, ncesID).Scan(
		&school.ID,
		&school.NCESID,
		&school.DistrictID,
		&school.Name,
		&school.StreetAddress,
		&school.City,
		&school.State,
		&school.Zip,
		&school.Latitude,
		&school.Longitude,
		&school.RegionID,
		&school.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSchoolNotFound
	}
	if err != nil {
		return nil, err
	}

	return school, nil
}

// GetByIDWithDetails retrieves a school with joined details including district name,
// region name, member/admin/verified counts, and bootstrap mode status
func (r *SchoolRepository) GetByIDWithDetails(ctx context.Context, id string) (*models.SchoolWithDetails, error) {
	query := `
		SELECT s.id, s.nces_id, s.district_id, s.name, s.street_address, s.city, s.state, s.zip,
			s.latitude, s.longitude, s.region_id, s.created_at,
			COALESCE(sd.name, '') AS district_name,
			COALESCE(gr.name, '') AS region_name,
			(SELECT COUNT(*) FROM user_schools us WHERE us.school_id = s.id) AS member_count,
			(SELECT COUNT(*) FROM user_schools us WHERE us.school_id = s.id AND us.is_admin = TRUE AND us.verification_status = 'verified') AS admin_count,
			(SELECT COUNT(*) FROM user_schools us WHERE us.school_id = s.id AND us.verification_status = 'verified') AS verified_count
		FROM schools s
		LEFT JOIN school_districts sd ON s.district_id = sd.id
		LEFT JOIN geographic_regions gr ON s.region_id = gr.id
		WHERE s.id = ?
	`

	schoolWithDetails := &models.SchoolWithDetails{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&schoolWithDetails.ID,
		&schoolWithDetails.NCESID,
		&schoolWithDetails.DistrictID,
		&schoolWithDetails.Name,
		&schoolWithDetails.StreetAddress,
		&schoolWithDetails.City,
		&schoolWithDetails.State,
		&schoolWithDetails.Zip,
		&schoolWithDetails.Latitude,
		&schoolWithDetails.Longitude,
		&schoolWithDetails.RegionID,
		&schoolWithDetails.CreatedAt,
		&schoolWithDetails.DistrictName,
		&schoolWithDetails.RegionName,
		&schoolWithDetails.MemberCount,
		&schoolWithDetails.AdminCount,
		&schoolWithDetails.VerifiedCount,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSchoolNotFound
	}
	if err != nil {
		return nil, err
	}

	// Bootstrap mode: fewer than 3 verified admins
	schoolWithDetails.BootstrapMode = schoolWithDetails.AdminCount < 3

	return schoolWithDetails, nil
}

// UpsertByNCESID inserts or updates a school by NCES ID
func (r *SchoolRepository) UpsertByNCESID(ctx context.Context, school *models.School) error {
	if school.ID == "" {
		school.ID = uuid.New().String()
	}
	school.CreatedAt = time.Now().UTC()

	query := `
		INSERT INTO schools (id, nces_id, district_id, name, street_address, city, state, zip,
			latitude, longitude, region_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			district_id = VALUES(district_id),
			name = VALUES(name),
			street_address = VALUES(street_address),
			city = VALUES(city),
			state = VALUES(state),
			zip = VALUES(zip),
			latitude = VALUES(latitude),
			longitude = VALUES(longitude),
			region_id = VALUES(region_id)
	`

	_, err := r.db.ExecContext(ctx, query,
		school.ID,
		school.NCESID,
		school.DistrictID,
		school.Name,
		school.StreetAddress,
		school.City,
		school.State,
		school.Zip,
		school.Latitude,
		school.Longitude,
		school.RegionID,
		school.CreatedAt,
	)

	return err
}

// BatchUpsertByNCESID inserts or updates multiple schools in a single multi-row INSERT
func (r *SchoolRepository) BatchUpsertByNCESID(ctx context.Context, schools []*models.School) error {
	if len(schools) == 0 {
		return nil
	}

	now := time.Now().UTC()
	var queryBuilder strings.Builder
	queryBuilder.WriteString(`
		INSERT INTO schools (id, nces_id, district_id, name, street_address, city, state, zip,
			latitude, longitude, region_id, created_at)
		VALUES `)

	args := make([]interface{}, 0, len(schools)*12)
	for i, school := range schools {
		if school.ID == "" {
			school.ID = uuid.New().String()
		}
		school.CreatedAt = now

		if i > 0 {
			queryBuilder.WriteString(", ")
		}
		queryBuilder.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")

		args = append(args, school.ID, school.NCESID, school.DistrictID, school.Name,
			school.StreetAddress, school.City, school.State, school.Zip,
			school.Latitude, school.Longitude, school.RegionID, school.CreatedAt)
	}

	queryBuilder.WriteString(`
		ON DUPLICATE KEY UPDATE
			district_id = VALUES(district_id),
			name = VALUES(name),
			street_address = VALUES(street_address),
			city = VALUES(city),
			state = VALUES(state),
			zip = VALUES(zip),
			latitude = VALUES(latitude),
			longitude = VALUES(longitude),
			region_id = VALUES(region_id)`)

	_, err := r.db.ExecContext(ctx, queryBuilder.String(), args...)
	return err
}

// Search searches schools by name with optional state and district filters, paginated
func (r *SchoolRepository) Search(ctx context.Context, query string, state string, districtID string, page int, limit int) ([]models.SchoolSummary, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	// Count query for pagination
	countQuery := `
		SELECT COUNT(*)
		FROM schools s
		WHERE s.name LIKE ?
	`
	countArgs := []interface{}{"%" + query + "%"}

	if state != "" {
		countQuery += " AND s.state = ?"
		countArgs = append(countArgs, state)
	}
	if districtID != "" {
		countQuery += " AND s.district_id = ?"
		countArgs = append(countArgs, districtID)
	}

	var totalCount int
	if err := r.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount); err != nil {
		return nil, 0, err
	}

	// Data query
	dataQuery := `
		SELECT s.id, s.nces_id, s.name, COALESCE(s.city, '') AS city, s.state,
			s.district_id, COALESCE(sd.name, '') AS district_name,
			s.latitude, s.longitude,
			(SELECT COUNT(*) FROM user_schools us WHERE us.school_id = s.id) AS member_count,
			(SELECT COUNT(*) FROM user_schools us WHERE us.school_id = s.id AND us.verification_status = 'verified') AS verified_count,
			(SELECT COUNT(*) FROM user_schools us WHERE us.school_id = s.id AND us.is_admin = TRUE AND us.verification_status = 'verified') AS admin_count
		FROM schools s
		LEFT JOIN school_districts sd ON s.district_id = sd.id
		WHERE s.name LIKE ?
	`
	dataArgs := []interface{}{"%" + query + "%"}

	if state != "" {
		dataQuery += " AND s.state = ?"
		dataArgs = append(dataArgs, state)
	}
	if districtID != "" {
		dataQuery += " AND s.district_id = ?"
		dataArgs = append(dataArgs, districtID)
	}

	dataQuery += " ORDER BY s.name LIMIT ? OFFSET ?"
	dataArgs = append(dataArgs, limit, offset)

	rows, err := r.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var schoolList []models.SchoolSummary
	for rows.Next() {
		var schoolSummary models.SchoolSummary
		var adminCount int
		if err := rows.Scan(
			&schoolSummary.ID,
			&schoolSummary.NCESID,
			&schoolSummary.Name,
			&schoolSummary.City,
			&schoolSummary.State,
			&schoolSummary.DistrictID,
			&schoolSummary.DistrictName,
			&schoolSummary.Latitude,
			&schoolSummary.Longitude,
			&schoolSummary.MemberCount,
			&schoolSummary.VerifiedCount,
			&adminCount,
		); err != nil {
			return nil, 0, err
		}
		schoolSummary.BootstrapMode = adminCount < 3
		schoolList = append(schoolList, schoolSummary)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return schoolList, totalCount, nil
}

// ListByDistrict lists all schools in a district as summaries
func (r *SchoolRepository) ListByDistrict(ctx context.Context, districtID string) ([]models.SchoolSummary, error) {
	query := `
		SELECT s.id, s.nces_id, s.name, COALESCE(s.city, '') AS city, s.state,
			s.district_id, COALESCE(sd.name, '') AS district_name,
			s.latitude, s.longitude,
			(SELECT COUNT(*) FROM user_schools us WHERE us.school_id = s.id) AS member_count,
			(SELECT COUNT(*) FROM user_schools us WHERE us.school_id = s.id AND us.verification_status = 'verified') AS verified_count,
			(SELECT COUNT(*) FROM user_schools us WHERE us.school_id = s.id AND us.is_admin = TRUE AND us.verification_status = 'verified') AS admin_count
		FROM schools s
		LEFT JOIN school_districts sd ON s.district_id = sd.id
		WHERE s.district_id = ?
		ORDER BY s.name
	`

	rows, err := r.db.QueryContext(ctx, query, districtID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var schoolList []models.SchoolSummary
	for rows.Next() {
		var schoolSummary models.SchoolSummary
		var adminCount int
		if err := rows.Scan(
			&schoolSummary.ID,
			&schoolSummary.NCESID,
			&schoolSummary.Name,
			&schoolSummary.City,
			&schoolSummary.State,
			&schoolSummary.DistrictID,
			&schoolSummary.DistrictName,
			&schoolSummary.Latitude,
			&schoolSummary.Longitude,
			&schoolSummary.MemberCount,
			&schoolSummary.VerifiedCount,
			&adminCount,
		); err != nil {
			return nil, err
		}
		schoolSummary.BootstrapMode = adminCount < 3
		schoolList = append(schoolList, schoolSummary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return schoolList, nil
}

// GetUserSchool retrieves a user's school membership, returns nil, nil if not found
func (r *SchoolRepository) GetUserSchool(ctx context.Context, userID, schoolID string) (*models.UserSchool, error) {
	query := `
		SELECT id, user_id, school_id, is_admin, verification_status, verified_at
		FROM user_schools
		WHERE user_id = ? AND school_id = ?
	`

	userSchool := &models.UserSchool{}
	err := r.db.QueryRowContext(ctx, query, userID, schoolID).Scan(
		&userSchool.ID,
		&userSchool.UserID,
		&userSchool.SchoolID,
		&userSchool.IsAdmin,
		&userSchool.VerificationStatus,
		&userSchool.VerifiedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return userSchool, nil
}
