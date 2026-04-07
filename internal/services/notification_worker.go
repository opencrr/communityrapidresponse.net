package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"time"

	gosentry "github.com/getsentry/sentry-go"
	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
)

// UserLookup provides user lookup for the notification worker
type UserLookup interface {
	GetByID(ctx context.Context, id string) (*models.User, error)
	GetVerifiedUsersInRegion(ctx context.Context, regionID string, offset, limit int) ([]*models.User, error)
}

// RegionLookup provides region lookup for the notification worker
type RegionLookup interface {
	GetByID(ctx context.Context, id string) (*models.GeographicRegion, error)
}

// SecretKeyLookup provides user lookup for re-keying notifications
type SecretKeyLookup interface {
	GetUsersWithSharedSecrets(ctx context.Context, userID string) ([]string, error)
}

// NotificationWorker processes queued notifications in the background
type NotificationWorker struct {
	queue           NotificationQueue
	emailService    EmailServiceInterface
	templates       *EmailTemplates
	userLookup      UserLookup
	regionLookup    RegionLookup
	secretKeyLookup SecretKeyLookup
	cfg             *config.NotificationConfig
}

// NewNotificationWorker creates a new NotificationWorker
func NewNotificationWorker(
	queue NotificationQueue,
	emailService EmailServiceInterface,
	templates *EmailTemplates,
	userLookup UserLookup,
	regionLookup RegionLookup,
	secretKeyLookup SecretKeyLookup,
	cfg *config.NotificationConfig,
) *NotificationWorker {
	return &NotificationWorker{
		queue:           queue,
		emailService:    emailService,
		templates:       templates,
		userLookup:      userLookup,
		regionLookup:    regionLookup,
		secretKeyLookup: secretKeyLookup,
		cfg:             cfg,
	}
}

// Start begins the background worker loop
func (w *NotificationWorker) Start(ctx context.Context) {
	monitorSlug := "crr-notification-worker"
	intervalMinutes := int(w.cfg.WorkerInterval.Minutes())
	if intervalMinutes < 1 {
		intervalMinutes = 1
	}
	monitorConfig := appSentry.IntervalSchedule(intervalMinutes, gosentry.MonitorScheduleUnitMinute)

	go func() {
		ticker := time.NewTicker(w.cfg.WorkerInterval)
		defer ticker.Stop()

		// Process immediately on startup
		w.runWithCheckIn(ctx, monitorSlug, monitorConfig)

		for {
			select {
			case <-ctx.Done():
				slog.Info("notification worker shutting down")
				return
			case <-ticker.C:
				w.runWithCheckIn(ctx, monitorSlug, monitorConfig)
			}
		}
	}()

	slog.Info("notification worker started", "interval", w.cfg.WorkerInterval, "batch_size", w.cfg.BatchSize)
}

// runWithCheckIn wraps processQueue with Sentry cron check-ins
func (w *NotificationWorker) runWithCheckIn(ctx context.Context, monitorSlug string, monitorConfig *gosentry.MonitorConfig) {
	checkInID := appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusInProgress, nil, monitorConfig)
	if checkInID != nil {
		slog.Debug("cron check-in started", "monitor", monitorSlug, "id", *checkInID)
	} else {
		slog.Debug("cron check-in skipped, sentry not initialized")
	}

	err := w.processQueue(ctx)

	if err != nil {
		slog.Error("notification worker queue processing failed", "error", err)
		appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusError, checkInID, monitorConfig)
	} else {
		appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusOK, checkInID, monitorConfig)
	}

	// Flush to ensure check-ins reach Sentry before the next interval
	gosentry.Flush(5 * time.Second)
}

// processQueue processes a batch of queued notifications
func (w *NotificationWorker) processQueue(ctx context.Context) error {
	// Emit queue depth gauge
	if depth, err := w.queue.QueueDepth(ctx); err == nil {
		appSentry.SetGauge("notification.queue_depth", float64(depth))
	}

	notifications, err := w.queue.Dequeue(ctx, w.cfg.BatchSize)
	if err != nil {
		slog.Error("failed to dequeue notifications", "error", err)
		_ = appSentry.CaptureError(err, "component", "notification_worker", "operation", "dequeue")
		return err
	}

	if len(notifications) == 0 {
		return nil
	}

	slog.Info("processing notifications", "count", len(notifications))

	for _, n := range notifications {
		// Check if this is a fan-out event (no user_id set)
		if n.UserID == "" {
			switch n.NotificationType {
			case models.NotificationTypeInviteLinkUpdated:
				w.fanOutInviteLinkNotification(ctx, n)
			case models.NotificationTypeRekeyingNeeded:
				w.fanOutRekeyingNotification(ctx, n)
			default:
				slog.Error("unknown fan-out event type", "notification_type", n.NotificationType)
				_ = w.queue.Nack(ctx, n.ID, "unknown fan-out type")
			}
			continue
		}

		// Process individual notification
		w.processNotification(ctx, n)
	}
	return nil
}

// fanOutInviteLinkNotification expands a fan-out event to individual user notifications
func (w *NotificationWorker) fanOutInviteLinkNotification(ctx context.Context, event *models.EmailNotification) {
	if event.ResourceID == nil {
		slog.Error("fan-out event missing region id", "event_id", event.ID)
		_ = w.queue.Nack(ctx, event.ID, "missing region ID")
		return
	}

	regionID := *event.ResourceID
	offset := 0
	batchSize := 100
	totalQueued := 0

	for {
		users, err := w.userLookup.GetVerifiedUsersInRegion(ctx, regionID, offset, batchSize)
		if err != nil {
			slog.Error("failed to get users for fan-out", "region_id", regionID, "error", err)
			_ = appSentry.CaptureError(err, "component", "notification_worker", "operation", "fan_out_users")
			_ = w.queue.Nack(ctx, event.ID, err.Error())
			return
		}

		if len(users) == 0 {
			break
		}

		// Queue per-user notification for each user
		for _, user := range users {
			resourceType := "signal_group"
			notification := &models.EmailNotification{
				ID:               uuid.New().String(),
				UserID:           user.ID,
				NotificationType: models.NotificationTypeInviteLinkUpdated,
				ResourceType:     &resourceType,
				ResourceID:       event.ResourceID,
				Status:           models.NotificationStatusQueued,
				QueuedAt:         time.Now().UTC(),
			}
			if err := w.queue.Enqueue(ctx, notification); err != nil {
				slog.Error("failed to queue notification for user", "user_id", user.ID, "error", err)
			} else {
				totalQueued++
			}
		}

		offset += len(users)
	}

	// Ack the fan-out event
	_ = w.queue.Ack(ctx, event.ID, "")
	slog.Info("fan-out complete", "region_id", regionID, "queued_count", totalQueued)
}

// fanOutRekeyingNotification expands a re-keying event to individual user notifications
// for all users sharing encrypted secrets with the user who rotated their keys
func (w *NotificationWorker) fanOutRekeyingNotification(ctx context.Context, event *models.EmailNotification) {
	if event.ResourceID == nil {
		slog.Error("re-keying fan-out event missing user id", "event_id", event.ID)
		_ = w.queue.Nack(ctx, event.ID, "missing user ID")
		return
	}

	rotatedUserID := *event.ResourceID

	if w.secretKeyLookup == nil {
		slog.Error("secret key lookup not configured, cannot fan out re-keying notification")
		_ = w.queue.Nack(ctx, event.ID, "secret key lookup not configured")
		return
	}

	userIDs, err := w.secretKeyLookup.GetUsersWithSharedSecrets(ctx, rotatedUserID)
	if err != nil {
		slog.Error("failed to get users sharing secrets", "rotated_user_id", rotatedUserID, "error", err)
		_ = appSentry.CaptureError(err, "component", "notification_worker", "operation", "fan_out_rekey_users")
		_ = w.queue.Nack(ctx, event.ID, err.Error())
		return
	}

	totalQueued := 0
	resourceType := "encryption_key"
	for _, userID := range userIDs {
		notification := &models.EmailNotification{
			ID:               uuid.New().String(),
			UserID:           userID,
			NotificationType: models.NotificationTypeRekeyingNeeded,
			ResourceType:     &resourceType,
			ResourceID:       event.ResourceID,
			Status:           models.NotificationStatusQueued,
			QueuedAt:         time.Now().UTC(),
		}
		if err := w.queue.Enqueue(ctx, notification); err != nil {
			slog.Error("failed to queue re-keying notification", "user_id", userID, "error", err)
		} else {
			totalQueued++
		}
	}

	_ = w.queue.Ack(ctx, event.ID, "")
	slog.Info("re-keying fan-out complete", "rotated_user_id", rotatedUserID, "queued_count", totalQueued)
}

// processNotification processes a single notification
func (w *NotificationWorker) processNotification(ctx context.Context, n *models.EmailNotification) {
	// Get user for email address
	user, err := w.userLookup.GetByID(ctx, n.UserID)
	if err != nil {
		slog.Error("failed to get user for notification", "user_id", n.UserID, "error", err)
		_ = appSentry.CaptureError(err, "component", "notification_worker", "operation", "get_user")
		_ = w.queue.Nack(ctx, n.ID, err.Error())
		return
	}

	// Skip if user is blocked
	if user.IsBlocked {
		slog.Info("skipping notification for blocked user", "user_id", n.UserID)
		_ = w.queue.Ack(ctx, n.ID, "")
		return
	}

	// Check rate limit at send time
	resourceID := ""
	if n.ResourceID != nil {
		resourceID = *n.ResourceID
	}
	recentlySent, err := w.queue.WasSentRecently(ctx, n.UserID, string(n.NotificationType), resourceID, w.cfg.RateLimitDuration)
	if err != nil {
		slog.Error("failed to check rate limit", "user_id", n.UserID, "error", err)
		_ = w.queue.Nack(ctx, n.ID, err.Error())
		return
	}
	if recentlySent {
		slog.Info("rate limited notification", "user_id", n.UserID, "notification_type", n.NotificationType)
		_ = w.queue.Ack(ctx, n.ID, "")
		return
	}

	// Build template data
	data := &TemplateData{
		UserEmail: user.Email,
	}

	// Enrich data based on notification type
	if err := w.enrichTemplateData(ctx, n, data); err != nil {
		slog.Error("failed to enrich template data", "notification_id", n.ID, "error", err)
		// Continue with partial data - don't fail the notification
	}

	// Build email from template
	msg := w.templates.Build(n, data)

	// Content-based deduplication
	contentHash := hashEmailContent(n.UserID, msg.Subject, msg.TextContent)
	isDuplicate, err := w.queue.WasContentSentRecently(ctx, contentHash, w.cfg.RateLimitDuration)
	if err != nil {
		slog.Error("failed to check content deduplication", "notification_id", n.ID, "error", err)
		// Continue anyway - better to potentially send a duplicate than to fail
	}
	if isDuplicate {
		slog.Info("skipping duplicate content", "user_id", n.UserID, "notification_id", n.ID)
		_ = w.queue.Ack(ctx, n.ID, contentHash)
		return
	}

	// Send email
	if err := w.emailService.Send(ctx, msg); err != nil {
		slog.Error("failed to send notification", "notification_id", n.ID, "user_id", n.UserID, "error", err)
		_ = appSentry.CaptureError(err, "component", "notification_worker", "operation", "send_email")
		_ = w.queue.Nack(ctx, n.ID, err.Error())
		return
	}

	// Mark as sent with content hash
	_ = w.queue.Ack(ctx, n.ID, contentHash)
	slog.Info("sent notification", "notification_id", n.ID, "notification_type", n.NotificationType, "user_id", n.UserID)
}

// enrichTemplateData adds context-specific data to the template
func (w *NotificationWorker) enrichTemplateData(ctx context.Context, n *models.EmailNotification, data *TemplateData) error {
	if n.ResourceID == nil {
		return nil
	}

	switch n.NotificationType {
	case models.NotificationTypeVerificationComplete, models.NotificationTypeVouchComplete:
		// Resource ID is region ID
		region, err := w.regionLookup.GetByID(ctx, *n.ResourceID)
		if err != nil {
			return err
		}
		data.RegionName = region.Name

	case models.NotificationTypeVouchReceived:
		// Resource ID is voucher ID
		voucher, err := w.userLookup.GetByID(ctx, *n.ResourceID)
		if err != nil {
			return err
		}
		data.VoucherName = voucher.Username

	case models.NotificationTypeInviteLinkUpdated:
		// Resource ID is region ID (for fan-out events) or group ID
		// We don't include group names in emails for security

	case models.NotificationTypeRekeyingNeeded:
		// Resource ID is the user who rotated keys
		// No sensitive data to enrich — email just tells user to log in

	default:
		// No enrichment needed
	}

	return nil
}

// hashEmailContent creates a SHA-256 hash of the email content for deduplication
func hashEmailContent(userID, subject, body string) string {
	h := sha256.Sum256([]byte(userID + subject + body))
	return hex.EncodeToString(h[:])
}
