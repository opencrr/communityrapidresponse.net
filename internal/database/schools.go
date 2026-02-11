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

	var schools []models.SchoolSummary
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
		schools = append(schools, schoolSummary)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return schools, totalCount, nil
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

	var schools []models.SchoolSummary
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
		schools = append(schools, schoolSummary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return schools, nil
}

// --- Membership methods ---

// AddUserToSchool creates a pending membership entry for a user in a school
func (r *SchoolRepository) AddUserToSchool(ctx context.Context, userID, schoolID string) (string, error) {
	membershipID := uuid.New().String()

	query := `
		INSERT INTO user_schools (id, user_id, school_id, is_admin, verification_status)
		VALUES (?, ?, ?, FALSE, 'pending')
	`

	_, err := r.db.ExecContext(ctx, query, membershipID, userID, schoolID)
	if err != nil {
		return "", err
	}

	return membershipID, nil
}

// AddUserToSchoolTx creates a pending membership entry within a transaction
func (r *SchoolRepository) AddUserToSchoolTx(ctx context.Context, tx *sql.Tx, userID, schoolID string) (string, error) {
	membershipID := uuid.New().String()

	query := `
		INSERT INTO user_schools (id, user_id, school_id, is_admin, verification_status)
		VALUES (?, ?, ?, FALSE, 'pending')
	`

	_, err := tx.ExecContext(ctx, query, membershipID, userID, schoolID)
	if err != nil {
		return "", err
	}

	return membershipID, nil
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

// GetUserSchoolForUpdate retrieves a user's school membership with a row lock
func (r *SchoolRepository) GetUserSchoolForUpdate(ctx context.Context, tx *sql.Tx, userID, schoolID string) (*models.UserSchool, error) {
	query := `
		SELECT id, user_id, school_id, is_admin, verification_status, verified_at
		FROM user_schools
		WHERE user_id = ? AND school_id = ?
		FOR UPDATE
	`

	userSchool := &models.UserSchool{}
	err := tx.QueryRowContext(ctx, query, userID, schoolID).Scan(
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

// UpgradeUserSchoolToVerifiedTx upgrades a user's school membership to verified status
func (r *SchoolRepository) UpgradeUserSchoolToVerifiedTx(ctx context.Context, tx *sql.Tx, userID, schoolID string, isAdmin bool) error {
	query := `
		UPDATE user_schools
		SET verification_status = 'verified', verified_at = NOW(), is_admin = ?
		WHERE user_id = ? AND school_id = ?
	`

	_, err := tx.ExecContext(ctx, query, isAdmin, userID, schoolID)
	return err
}

// SetUserSchoolAdminTx sets the admin flag for a user's school membership
func (r *SchoolRepository) SetUserSchoolAdminTx(ctx context.Context, tx *sql.Tx, userID, schoolID string, isAdmin bool) error {
	query := `
		UPDATE user_schools
		SET is_admin = ?
		WHERE user_id = ? AND school_id = ?
	`

	_, err := tx.ExecContext(ctx, query, isAdmin, userID, schoolID)
	return err
}

// ListUserSchools lists all schools a user belongs to as summaries
func (r *SchoolRepository) ListUserSchools(ctx context.Context, userID string) ([]models.SchoolSummary, error) {
	query := `
		SELECT s.id, s.nces_id, s.name, COALESCE(s.city, '') AS city, s.state,
			s.district_id, COALESCE(sd.name, '') AS district_name,
			s.latitude, s.longitude,
			(SELECT COUNT(*) FROM user_schools us2 WHERE us2.school_id = s.id) AS member_count,
			(SELECT COUNT(*) FROM user_schools us2 WHERE us2.school_id = s.id AND us2.verification_status = 'verified') AS verified_count,
			(SELECT COUNT(*) FROM user_schools us2 WHERE us2.school_id = s.id AND us2.is_admin = TRUE AND us2.verification_status = 'verified') AS admin_count
		FROM user_schools us
		JOIN schools s ON us.school_id = s.id
		LEFT JOIN school_districts sd ON s.district_id = sd.id
		WHERE us.user_id = ?
		ORDER BY s.name
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var schools []models.SchoolSummary
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
		schools = append(schools, schoolSummary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return schools, nil
}

// ListUserSchoolsWithStatus lists all schools a user belongs to, enriched with membership status
func (r *SchoolRepository) ListUserSchoolsWithStatus(ctx context.Context, userID string) ([]models.MySchoolSummary, error) {
	query := `
		SELECT s.id, s.nces_id, s.name, COALESCE(s.city, '') AS city, s.state,
			s.district_id, COALESCE(sd.name, '') AS district_name,
			s.latitude, s.longitude,
			(SELECT COUNT(*) FROM user_schools us2 WHERE us2.school_id = s.id) AS member_count,
			(SELECT COUNT(*) FROM user_schools us2 WHERE us2.school_id = s.id AND us2.verification_status = 'verified') AS verified_count,
			(SELECT COUNT(*) FROM user_schools us2 WHERE us2.school_id = s.id AND us2.is_admin = TRUE AND us2.verification_status = 'verified') AS admin_count,
			us.verification_status,
			us.is_admin,
			(SELECT COUNT(*) FROM school_vouches sv WHERE sv.vouched_user_id = us.user_id AND sv.school_id = s.id) AS vouch_count
		FROM user_schools us
		JOIN schools s ON us.school_id = s.id
		LEFT JOIN school_districts sd ON s.district_id = sd.id
		WHERE us.user_id = ?
		ORDER BY s.name
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var schools []models.MySchoolSummary
	for rows.Next() {
		var schoolSummary models.MySchoolSummary
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
			&schoolSummary.VerificationStatus,
			&schoolSummary.IsAdmin,
			&schoolSummary.VouchCount,
		); err != nil {
			return nil, err
		}
		schoolSummary.BootstrapMode = adminCount < 3
		// In bootstrap mode: 3 vouches required; otherwise 2
		vouchesRequired := 2
		if adminCount < 3 {
			vouchesRequired = 3
		}
		vouchesNeeded := vouchesRequired - schoolSummary.VouchCount
		if vouchesNeeded < 0 {
			vouchesNeeded = 0
		}
		schoolSummary.VouchesNeeded = vouchesNeeded
		schools = append(schools, schoolSummary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return schools, nil
}

// GetSchoolMembers retrieves all members of a school with user details and vouch count
func (r *SchoolRepository) GetSchoolMembers(ctx context.Context, schoolID string) ([]models.SchoolMember, error) {
	query := `
		SELECT u.id, u.username, u.email, us.is_admin, us.verification_status, us.verified_at,
			(SELECT COUNT(*) FROM school_vouches sv WHERE sv.vouched_user_id = u.id AND sv.school_id = us.school_id) AS vouch_count
		FROM user_schools us
		JOIN users u ON us.user_id = u.id
		WHERE us.school_id = ?
		ORDER BY us.verification_status DESC, u.username
	`

	rows, err := r.db.QueryContext(ctx, query, schoolID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var members []models.SchoolMember
	for rows.Next() {
		var member models.SchoolMember
		if err := rows.Scan(
			&member.UserID,
			&member.Username,
			&member.Email,
			&member.IsAdmin,
			&member.VerificationStatus,
			&member.VerifiedAt,
			&member.VouchCount,
		); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return members, nil
}

// RemoveUserFromSchool removes a user from a school
func (r *SchoolRepository) RemoveUserFromSchool(ctx context.Context, userID, schoolID string) error {
	query := `DELETE FROM user_schools WHERE user_id = ? AND school_id = ?`
	_, err := r.db.ExecContext(ctx, query, userID, schoolID)
	return err
}

// RemoveUserFromSchoolTx removes a user from a school within a transaction
func (r *SchoolRepository) RemoveUserFromSchoolTx(ctx context.Context, tx *sql.Tx, userID, schoolID string) error {
	query := `DELETE FROM user_schools WHERE user_id = ? AND school_id = ?`
	_, err := tx.ExecContext(ctx, query, userID, schoolID)
	return err
}

// IsUserInSchool checks if a user is a member of a school
func (r *SchoolRepository) IsUserInSchool(ctx context.Context, userID, schoolID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM user_schools WHERE user_id = ? AND school_id = ?)`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, userID, schoolID).Scan(&exists)
	return exists, err
}

// IsUserVerifiedInDistrictTx checks if a user is verified in any school within a district
func (r *SchoolRepository) IsUserVerifiedInDistrictTx(ctx context.Context, tx *sql.Tx, userID, districtID string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM user_schools us
			JOIN schools s ON us.school_id = s.id
			WHERE us.user_id = ? AND s.district_id = ? AND us.verification_status = 'verified'
		)
	`
	var exists bool
	err := tx.QueryRowContext(ctx, query, userID, districtID).Scan(&exists)
	return exists, err
}

// GetDistrictMembers retrieves all members across schools in a district.
// A user in multiple schools appears once (first school alphabetically).
func (r *SchoolRepository) GetDistrictMembers(ctx context.Context, districtID string) ([]models.DistrictMember, error) {
	query := `
		SELECT u.id, u.username, s.name AS school_name, us.is_admin, us.verification_status
		FROM user_schools us
		JOIN users u ON us.user_id = u.id
		JOIN schools s ON us.school_id = s.id
		WHERE s.district_id = ?
		AND s.name = (
			SELECT MIN(s2.name) FROM user_schools us2
			JOIN schools s2 ON us2.school_id = s2.id
			WHERE us2.user_id = us.user_id AND s2.district_id = ?
		)
		ORDER BY us.verification_status DESC, u.username
	`

	rows, err := r.db.QueryContext(ctx, query, districtID, districtID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var members []models.DistrictMember
	for rows.Next() {
		var member models.DistrictMember
		if err := rows.Scan(
			&member.UserID,
			&member.Username,
			&member.SchoolName,
			&member.IsAdmin,
			&member.VerificationStatus,
		); err != nil {
			return nil, err
		}
		members = append(members, member)
	}

	return members, rows.Err()
}

// --- Bootstrap mode methods ---

// IsSchoolInBootstrapMode checks if a school has fewer than 3 verified admins
func (r *SchoolRepository) IsSchoolInBootstrapMode(ctx context.Context, schoolID string) (bool, int, error) {
	verifiedAdminCount, err := r.GetVerifiedAdminCount(ctx, schoolID)
	if err != nil {
		return false, 0, err
	}
	return verifiedAdminCount < 3, verifiedAdminCount, nil
}

// IsSchoolInBootstrapModeTx checks bootstrap mode within a transaction
func (r *SchoolRepository) IsSchoolInBootstrapModeTx(ctx context.Context, tx *sql.Tx, schoolID string) (bool, int, error) {
	query := `
		SELECT COUNT(*) FROM user_schools
		WHERE school_id = ? AND is_admin = TRUE AND verification_status = 'verified'
	`
	var verifiedAdminCount int
	err := tx.QueryRowContext(ctx, query, schoolID).Scan(&verifiedAdminCount)
	if err != nil {
		return false, 0, err
	}
	return verifiedAdminCount < 3, verifiedAdminCount, nil
}

// GetVerifiedAdminCount returns the number of verified admins in a school
func (r *SchoolRepository) GetVerifiedAdminCount(ctx context.Context, schoolID string) (int, error) {
	query := `
		SELECT COUNT(*) FROM user_schools
		WHERE school_id = ? AND is_admin = TRUE AND verification_status = 'verified'
	`
	var count int
	err := r.db.QueryRowContext(ctx, query, schoolID).Scan(&count)
	return count, err
}

// --- Blocklist methods ---

// IsUserBlockedInSchool checks if a user is blocked from a school
func (r *SchoolRepository) IsUserBlockedInSchool(ctx context.Context, userID, schoolID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM school_blocked_users WHERE user_id = ? AND school_id = ?)`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, userID, schoolID).Scan(&exists)
	return exists, err
}

// IsUserBlockedInSchoolTx checks if a user is blocked from a school within a transaction
func (r *SchoolRepository) IsUserBlockedInSchoolTx(ctx context.Context, tx *sql.Tx, userID, schoolID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM school_blocked_users WHERE user_id = ? AND school_id = ?)`
	var exists bool
	err := tx.QueryRowContext(ctx, query, userID, schoolID).Scan(&exists)
	return exists, err
}

// BlockUserInSchoolTx blocks a user from a school within a transaction
func (r *SchoolRepository) BlockUserInSchoolTx(ctx context.Context, tx *sql.Tx, userID, schoolID, blockedBy, reason string) error {
	blockID := uuid.New().String()

	query := `
		INSERT INTO school_blocked_users (id, user_id, school_id, blocked_by, reason, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := tx.ExecContext(ctx, query, blockID, userID, schoolID, blockedBy, reason, time.Now().UTC())
	return err
}

// UnblockUserInSchool removes a block on a user in a school
func (r *SchoolRepository) UnblockUserInSchool(ctx context.Context, userID, schoolID string) error {
	query := `DELETE FROM school_blocked_users WHERE user_id = ? AND school_id = ?`
	_, err := r.db.ExecContext(ctx, query, userID, schoolID)
	return err
}

// ListBlockedUsers lists all blocked users for a school
func (r *SchoolRepository) ListBlockedUsers(ctx context.Context, schoolID string) ([]models.SchoolBlockedUser, error) {
	query := `
		SELECT id, user_id, school_id, blocked_by, reason, created_at
		FROM school_blocked_users
		WHERE school_id = ?
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, schoolID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var blockedUsers []models.SchoolBlockedUser
	for rows.Next() {
		var blockedUser models.SchoolBlockedUser
		if err := rows.Scan(
			&blockedUser.ID,
			&blockedUser.UserID,
			&blockedUser.SchoolID,
			&blockedUser.BlockedBy,
			&blockedUser.Reason,
			&blockedUser.CreatedAt,
		); err != nil {
			return nil, err
		}
		blockedUsers = append(blockedUsers, blockedUser)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return blockedUsers, nil
}
