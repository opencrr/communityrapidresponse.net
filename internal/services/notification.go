package services

import (
	"context"
	"log"
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
		log.Printf("ERROR: Failed to queue invite link update event for group %s: %v", groupID, err)
		return err
	}

	log.Printf("INFO: Queued invite link update event for group %s in region %s", groupID, regionID)
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
		log.Printf("ERROR: Failed to queue verification complete notification for user %s: %v", userID, err)
		return err
	}

	log.Printf("INFO: Queued verification complete notification for user %s", userID)
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
		log.Printf("ERROR: Failed to queue vouch received notification for user %s: %v", userID, err)
		return err
	}

	log.Printf("INFO: Queued vouch received notification for user %s from voucher %s", userID, voucherID)
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
		log.Printf("ERROR: Failed to queue vouch complete notification for user %s: %v", userID, err)
		return err
	}

	log.Printf("INFO: Queued vouch complete notification for user %s", userID)
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
		log.Printf("ERROR: Failed to queue sub-region invitation notification for user %s: %v", userID, err)
		return err
	}

	log.Printf("INFO: Queued sub-region invitation notification for user %s from inviter %s", userID, inviterID)
	return nil
}
