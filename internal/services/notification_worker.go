package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
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

// NotificationWorker processes queued notifications in the background
type NotificationWorker struct {
	queue        NotificationQueue
	emailService EmailServiceInterface
	templates    *EmailTemplates
	userLookup   UserLookup
	regionLookup RegionLookup
	cfg          *config.NotificationConfig
}

// NewNotificationWorker creates a new NotificationWorker
func NewNotificationWorker(
	queue NotificationQueue,
	emailService EmailServiceInterface,
	templates *EmailTemplates,
	userLookup UserLookup,
	regionLookup RegionLookup,
	cfg *config.NotificationConfig,
) *NotificationWorker {
	return &NotificationWorker{
		queue:        queue,
		emailService: emailService,
		templates:    templates,
		userLookup:   userLookup,
		regionLookup: regionLookup,
		cfg:          cfg,
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
				log.Println("INFO: Notification worker shutting down")
				return
			case <-ticker.C:
				w.runWithCheckIn(ctx, monitorSlug, monitorConfig)
			}
		}
	}()

	log.Printf("INFO: Notification worker started (interval: %s, batch: %d)",
		w.cfg.WorkerInterval, w.cfg.BatchSize)
}

// runWithCheckIn wraps processQueue with Sentry cron check-ins
func (w *NotificationWorker) runWithCheckIn(ctx context.Context, monitorSlug string, monitorConfig *gosentry.MonitorConfig) {
	checkInID := appSentry.CheckIn(monitorSlug, gosentry.CheckInStatusInProgress, nil, monitorConfig)
	if checkInID != nil {
		log.Printf("DEBUG: Cron check-in started (monitor=%s, id=%s)", monitorSlug, *checkInID)
	} else {
		log.Printf("DEBUG: Cron check-in skipped (Sentry not initialized)")
	}

	err := w.processQueue(ctx)

	if err != nil {
		log.Printf("ERROR: Notification worker queue processing failed: %v", err)
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
		log.Printf("ERROR: Failed to dequeue notifications: %v", err)
		_ = appSentry.CaptureError(err, "component", "notification_worker", "operation", "dequeue")
		return err
	}

	if len(notifications) == 0 {
		return nil
	}

	log.Printf("INFO: Processing %d notifications", len(notifications))

	for _, n := range notifications {
		// Check if this is a fan-out event (no user_id set)
		if n.UserID == "" && n.NotificationType == models.NotificationTypeInviteLinkUpdated {
			w.fanOutInviteLinkNotification(ctx, n)
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
		log.Printf("ERROR: Fan-out event %s missing region ID", event.ID)
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
			log.Printf("ERROR: Failed to get users for fan-out in region %s: %v", regionID, err)
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
				log.Printf("ERROR: Failed to queue notification for user %s: %v", user.ID, err)
			} else {
				totalQueued++
			}
		}

		offset += len(users)
	}

	// Ack the fan-out event
	_ = w.queue.Ack(ctx, event.ID, "")
	log.Printf("INFO: Fan-out complete for region %s: queued %d notifications", regionID, totalQueued)
}

// processNotification processes a single notification
func (w *NotificationWorker) processNotification(ctx context.Context, n *models.EmailNotification) {
	// Get user for email address
	user, err := w.userLookup.GetByID(ctx, n.UserID)
	if err != nil {
		log.Printf("ERROR: Failed to get user %s for notification: %v", n.UserID, err)
		_ = appSentry.CaptureError(err, "component", "notification_worker", "operation", "get_user")
		_ = w.queue.Nack(ctx, n.ID, err.Error())
		return
	}

	// Skip if user is blocked
	if user.IsBlocked {
		log.Printf("INFO: Skipping notification for blocked user %s", n.UserID)
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
		log.Printf("ERROR: Failed to check rate limit for user %s: %v", n.UserID, err)
		_ = w.queue.Nack(ctx, n.ID, err.Error())
		return
	}
	if recentlySent {
		log.Printf("INFO: Rate limited notification for user %s (type: %s)", n.UserID, n.NotificationType)
		_ = w.queue.Ack(ctx, n.ID, "")
		return
	}

	// Build template data
	data := &TemplateData{
		UserEmail: user.Email,
	}

	// Enrich data based on notification type
	if err := w.enrichTemplateData(ctx, n, data); err != nil {
		log.Printf("ERROR: Failed to enrich template data for notification %s: %v", n.ID, err)
		// Continue with partial data - don't fail the notification
	}

	// Build email from template
	msg := w.templates.Build(n, data)

	// Content-based deduplication
	contentHash := hashEmailContent(n.UserID, msg.Subject, msg.TextContent)
	isDuplicate, err := w.queue.WasContentSentRecently(ctx, contentHash, w.cfg.RateLimitDuration)
	if err != nil {
		log.Printf("ERROR: Failed to check content deduplication for notification %s: %v", n.ID, err)
		// Continue anyway - better to potentially send a duplicate than to fail
	}
	if isDuplicate {
		log.Printf("INFO: Skipping duplicate content for user %s (notification %s)", n.UserID, n.ID)
		_ = w.queue.Ack(ctx, n.ID, contentHash)
		return
	}

	// Send email
	if err := w.emailService.Send(ctx, msg); err != nil {
		log.Printf("ERROR: Failed to send notification %s to user %s: %v", n.ID, n.UserID, err)
		_ = appSentry.CaptureError(err, "component", "notification_worker", "operation", "send_email")
		_ = w.queue.Nack(ctx, n.ID, err.Error())
		return
	}

	// Mark as sent with content hash
	_ = w.queue.Ack(ctx, n.ID, contentHash)
	log.Printf("INFO: Sent notification %s (type: %s) to user %s", n.ID, n.NotificationType, n.UserID)
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

	case models.NotificationTypeSubRegionInvitation:
		// Resource ID is inviter ID
		inviter, err := w.userLookup.GetByID(ctx, *n.ResourceID)
		if err != nil {
			return err
		}
		data.InviterName = inviter.Username

	case models.NotificationTypeInviteLinkUpdated:
		// Resource ID is region ID (for fan-out events) or group ID
		// We don't include group names in emails for security

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
