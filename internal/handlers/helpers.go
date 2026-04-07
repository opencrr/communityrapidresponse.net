package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
)

// Sentinel errors for configuration-level issues (no runtime err variable available).
var (
	errPasswordResetNotConfigured = errors.New("password reset not configured")
	errProposalMissingRegion      = errors.New("proposal has no associated region")
)

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError writes an error response
func writeError(w http.ResponseWriter, status int, errorCode, message string) {
	writeJSON(w, status, ErrorResponse{
		Error:   errorCode,
		Message: message,
	})
}

// writeServerError captures the error in Sentry and writes a 500 response.
func writeServerError(w http.ResponseWriter, r *http.Request, err error, message, component, operation string) {
	_ = appSentry.CaptureErrorWithContext(r.Context(), err, "component", component, "operation", operation)
	writeError(w, http.StatusInternalServerError, "internal_error", message)
}

// logAuditError captures a non-nil audit log error in Sentry.
func logAuditError(r *http.Request, err error, action string) {
	if err != nil {
		_ = appSentry.CaptureErrorWithContext(r.Context(), err, "component", "audit", "operation", action)
	}
}

// getPathParam extracts a path parameter from the URL
// This is a simple implementation - in production, use a router like chi
func getPathParam(r *http.Request, name string) string {
	// This will be replaced with proper router parameter extraction
	// For now, we'll use query params as a fallback for path params
	return r.URL.Query().Get(name)
}

// NotificationServiceInterface defines the interface for notification operations
// Used by handlers to queue notifications without depending on the concrete implementation
type NotificationServiceInterface interface {
	// QueueInviteLinkUpdatedEvent queues a fan-out notification for invite link updates
	QueueInviteLinkUpdatedEvent(ctx context.Context, groupID, regionID string) error

	// QueueVerificationComplete queues a notification for postcard verification completion
	QueueVerificationComplete(ctx context.Context, userID, regionID string) error

	// QueueVouchReceived queues a notification when a user receives a vouch
	QueueVouchReceived(ctx context.Context, userID, voucherID, regionID string) error

	// QueueVouchComplete queues a notification when a user achieves vouch verification
	QueueVouchComplete(ctx context.Context, userID, regionID string) error

	// QueueRekeyingNeededEvent queues a fan-out notification for key rotation rekey
	QueueRekeyingNeededEvent(ctx context.Context, userID string) error
}
