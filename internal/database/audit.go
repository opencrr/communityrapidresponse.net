package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	gosentry "github.com/getsentry/sentry-go"
	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/middleware"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
)

// AuditRepository handles audit log database operations
type AuditRepository struct {
	db *DB
}

// NewAuditRepository creates a new audit repository
func NewAuditRepository(db *DB) *AuditRepository {
	return &AuditRepository{db: db}
}

// Log creates an audit log entry, extracting IP and user agent from context
func (r *AuditRepository) Log(ctx context.Context, userID *string, action string, resourceType *string, resourceID *string, details interface{}) error {
	id := uuid.New().String()
	createdAt := time.Now().UTC()

	// Extract IP and user agent from context
	ip := middleware.GetIPFromContext(ctx)
	userAgent := middleware.GetUserAgentFromContext(ctx)

	var ipPtr, uaPtr *string
	if ip != "" {
		ipPtr = &ip
	}
	if userAgent != "" {
		// Truncate user agent if too long
		if len(userAgent) > 512 {
			userAgent = userAgent[:512]
		}
		uaPtr = &userAgent
	}

	// Marshal details to JSON
	var detailsJSON []byte
	var err error
	if details != nil {
		detailsJSON, err = json.Marshal(details)
		if err != nil {
			slog.Warn("failed to marshal audit log details", "error", err)
			detailsJSON = nil
		}
	}

	query := `
		INSERT INTO audit_log (id, user_id, action, resource_type, resource_id, details, ip_address, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = r.db.ExecContext(ctx, query,
		id,
		userID,
		action,
		resourceType,
		resourceID,
		detailsJSON,
		ipPtr,
		uaPtr,
		createdAt,
	)

	if err != nil {
		slog.Error("failed to create audit log entry", "error", err)
		return err
	}

	return nil
}

// Query retrieves audit logs with filters and pagination
func (r *AuditRepository) Query(ctx context.Context, filter models.AuditLogFilter) (*models.AuditLogResponse, error) {
	// Build the WHERE clause dynamically
	var conditions []string
	var args []interface{}

	if filter.UserID != nil && *filter.UserID != "" {
		conditions = append(conditions, "user_id = ?")
		args = append(args, *filter.UserID)
	}

	if len(filter.Actions) > 0 {
		placeholders := make([]string, len(filter.Actions))
		for i, action := range filter.Actions {
			placeholders[i] = "?"
			args = append(args, action)
		}
		conditions = append(conditions, fmt.Sprintf("action IN (%s)", strings.Join(placeholders, ",")))
	}

	if filter.ResourceType != nil && *filter.ResourceType != "" {
		conditions = append(conditions, "resource_type = ?")
		args = append(args, *filter.ResourceType)
	}

	if filter.ResourceID != nil && *filter.ResourceID != "" {
		conditions = append(conditions, "resource_id = ?")
		args = append(args, *filter.ResourceID)
	}

	if filter.DateFrom != nil {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, *filter.DateFrom)
	}

	if filter.DateTo != nil {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, *filter.DateTo)
	}

	if filter.IPAddress != nil && *filter.IPAddress != "" {
		conditions = append(conditions, "ip_address = ?")
		args = append(args, *filter.IPAddress)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Count total matching records
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM audit_log %s", whereClause)
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("failed to count audit logs: %w", err)
	}

	// Apply pagination defaults
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 {
		filter.Limit = 50
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}

	offset := (filter.Page - 1) * filter.Limit

	// Fetch the logs
	selectQuery := fmt.Sprintf(`
		SELECT id, user_id, action, resource_type, resource_id, details, ip_address, user_agent, created_at
		FROM audit_log
		%s
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	queryArgs := append(args, filter.Limit, offset)
	rows, err := r.db.QueryContext(ctx, selectQuery, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var logs []models.AuditLog
	for rows.Next() {
		var log models.AuditLog
		var detailsJSON sql.NullString
		if err := rows.Scan(
			&log.ID,
			&log.UserID,
			&log.Action,
			&log.ResourceType,
			&log.ResourceID,
			&detailsJSON,
			&log.IPAddress,
			&log.UserAgent,
			&log.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}

		if detailsJSON.Valid && detailsJSON.String != "" {
			log.Details = json.RawMessage(detailsJSON.String)
		}

		logs = append(logs, log)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate audit logs: %w", err)
	}

	// Initialize empty slice if nil
	if logs == nil {
		logs = []models.AuditLog{}
	}

	return &models.AuditLogResponse{
		Logs:    logs,
		Total:   total,
		Page:    filter.Page,
		Limit:   filter.Limit,
		HasMore: (filter.Page * filter.Limit) < total,
	}, nil
}

// DeleteOlderThan removes audit log entries older than the specified duration
// Returns the number of deleted rows
func (r *AuditRepository) DeleteOlderThan(ctx context.Context, retention time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-retention)

	// Delete in batches to avoid locking issues
	const batchSize = 1000
	var totalDeleted int64

	for {
		query := `DELETE FROM audit_log WHERE created_at < ? LIMIT ?`
		result, err := r.db.ExecContext(ctx, query, cutoff, batchSize)
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to delete old audit logs: %w", err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to get rows affected: %w", err)
		}

		totalDeleted += rowsAffected

		// If we deleted fewer than batchSize, we're done
		if rowsAffected < batchSize {
			break
		}
	}

	return totalDeleted, nil
}

// StartCleanupWorker starts a background goroutine that periodically cleans up old audit logs
func (r *AuditRepository) StartCleanupWorker(ctx context.Context, interval, retention time.Duration) {
	monitorSlug := "crr-audit-cleanup"
	intervalHours := int(interval.Hours())
	if intervalHours < 1 {
		intervalHours = 1
	}
	monitorConfig := appSentry.IntervalSchedule(intervalHours, gosentry.MonitorScheduleUnitHour)

	runCleanup := func() {
		checkInID := appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusInProgress, nil, monitorConfig)
		deleted, err := r.DeleteOlderThan(ctx, retention)
		if err != nil {
			slog.Error("audit log cleanup failed", "error", err)
			_ = appSentry.CaptureError(err, "component", "audit_worker", "operation", "cleanup")
			appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusError, checkInID, monitorConfig)
		} else {
			if deleted > 0 {
				slog.Info("audit log cleanup deleted old entries", "count", deleted)
			}
			appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusOK, checkInID, monitorConfig)
		}
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Run once immediately at startup
		runCleanup()

		for {
			select {
			case <-ctx.Done():
				slog.Info("audit log cleanup worker stopped")
				return
			case <-ticker.C:
				runCleanup()
			}
		}
	}()

	slog.Info("audit log cleanup worker started", "interval", interval, "retention", retention)
}
