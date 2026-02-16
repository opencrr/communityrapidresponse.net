package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// NotificationService handles queuing email notifications
type NotificationService struct {
	queue NotificationQueue
}

// NewNotificationService creates a new NotificationService
func NewNotificationService(queue NotificationQueue) *NotificationService {
	return &NotificationService{
		queue: queue,
	}
}

// QueueInviteLinkUpdatedEvent queues a fan-out event for invite link updates.
// This creates a single event that will be expanded by the worker to notify all verified users in the region.
func (s *NotificationService) QueueInviteLinkUpdatedEvent(ctx context.Context, groupID, regionID string) error {
	resourceType := "signal_group"
	notification := &models.EmailNotification{
		ID:               uuid.New().String(),
		UserID:           "", // Empty user_id indicates fan-out event
		NotificationType: models.NotificationTypeInviteLinkUpdated,
		ResourceType:     &resourceType,
		ResourceID:       &regionID, // Store region ID for fan-out lookup
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}

	if err := s.queue.Enqueue(ctx, notification); err != nil {
		slog.Error("failed to queue invite link update event", "group_id", groupID, "error", err)
		return err
	}

	slog.Info("queued invite link update event", "group_id", groupID, "region_id", regionID)
	return nil
}

// QueueVerificationComplete queues a notification for postcard verification completion
func (s *NotificationService) QueueVerificationComplete(ctx context.Context, userID, regionID string) error {
	resourceType := "region"
	notification := &models.EmailNotification{
		ID:               uuid.New().String(),
		UserID:           userID,
		NotificationType: models.NotificationTypeVerificationComplete,
		ResourceType:     &resourceType,
		ResourceID:       &regionID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}

	if err := s.queue.Enqueue(ctx, notification); err != nil {
		slog.Error("failed to queue verification complete notification", "user_id", userID, "error", err)
		return err
	}

	slog.Info("queued verification complete notification", "user_id", userID)
	return nil
}

// QueueVouchReceived queues a notification when a user receives a vouch
func (s *NotificationService) QueueVouchReceived(ctx context.Context, userID, voucherID, regionID string) error {
	resourceType := "vouch"
	notification := &models.EmailNotification{
		ID:               uuid.New().String(),
		UserID:           userID,
		NotificationType: models.NotificationTypeVouchReceived,
		ResourceType:     &resourceType,
		ResourceID:       &voucherID, // Store voucher ID so we can look up their name
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}

	if err := s.queue.Enqueue(ctx, notification); err != nil {
		slog.Error("failed to queue vouch received notification", "user_id", userID, "error", err)
		return err
	}

	slog.Info("queued vouch received notification", "user_id", userID, "voucher_id", voucherID)
	return nil
}

// QueueVouchComplete queues a notification when a user achieves vouch verification
func (s *NotificationService) QueueVouchComplete(ctx context.Context, userID, regionID string) error {
	resourceType := "region"
	notification := &models.EmailNotification{
		ID:               uuid.New().String(),
		UserID:           userID,
		NotificationType: models.NotificationTypeVouchComplete,
		ResourceType:     &resourceType,
		ResourceID:       &regionID,
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}

	if err := s.queue.Enqueue(ctx, notification); err != nil {
		slog.Error("failed to queue vouch complete notification", "user_id", userID, "error", err)
		return err
	}

	slog.Info("queued vouch complete notification", "user_id", userID)
	return nil
}

// QueueSubRegionInvitation queues a notification when a user is invited to a sub-region
func (s *NotificationService) QueueSubRegionInvitation(ctx context.Context, userID, inviterID, regionID string) error {
	resourceType := "membership_invitation"
	notification := &models.EmailNotification{
		ID:               uuid.New().String(),
		UserID:           userID,
		NotificationType: models.NotificationTypeSubRegionInvitation,
		ResourceType:     &resourceType,
		ResourceID:       &inviterID, // Store inviter ID so we can look up their name
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}

	if err := s.queue.Enqueue(ctx, notification); err != nil {
		slog.Error("failed to queue sub-region invitation notification", "user_id", userID, "error", err)
		return err
	}

	slog.Info("queued sub-region invitation notification", "user_id", userID, "inviter_id", inviterID)
	return nil
}

// QueueRekeyingNeededEvent queues a fan-out event to notify members sharing secrets with a user
// who has rotated their encryption keys. The worker expands this to per-user notifications.
func (s *NotificationService) QueueRekeyingNeededEvent(ctx context.Context, userID string) error {
	resourceType := "encryption_key"
	notification := &models.EmailNotification{
		ID:               uuid.New().String(),
		UserID:           "", // Empty user_id indicates fan-out event
		NotificationType: models.NotificationTypeRekeyingNeeded,
		ResourceType:     &resourceType,
		ResourceID:       &userID, // Store the user who rotated keys
		Status:           models.NotificationStatusQueued,
		QueuedAt:         time.Now().UTC(),
	}

	if err := s.queue.Enqueue(ctx, notification); err != nil {
		slog.Error("failed to queue re-keying needed event", "user_id", userID, "error", err)
		return err
	}

	slog.Info("queued re-keying needed event", "user_id", userID)
	return nil
}
