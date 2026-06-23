package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/opencrr/communityrapidresponse.net/internal/database"
	appSentry "github.com/opencrr/communityrapidresponse.net/internal/sentry"
)

// isSuperuserFromDB re-checks superuser status from the database.
// Use this instead of claims.IsSuperuser for authorization decisions
// to prevent privilege escalation via stale JWT claims.
func isSuperuserFromDB(ctx context.Context, userRepo *database.UserRepository, userID string) (bool, error) {
	user, err := userRepo.GetByID(ctx, userID)
	if err != nil {
		return false, err
	}
	return user.IsSuperuser, nil
}

// validate is the shared struct validator instance used by all handlers.
var validate = validator.New()

// validateStruct validates a struct using go-playground/validator tags.
// Returns a human-readable error message or empty string if valid.
func validateStruct(s interface{}) string {
	err := validate.Struct(s)
	if err == nil {
		return ""
	}

	var validationErrors validator.ValidationErrors
	if errors.As(err, &validationErrors) {
		var messages []string
		for _, fieldErr := range validationErrors {
			messages = append(messages, formatFieldError(fieldErr))
		}
		return strings.Join(messages, "; ")
	}

	return "Invalid request"
}

// isUsernameChar reports whether c is allowed in a username: ASCII letters,
// digits, or underscore.
func isUsernameChar(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// isValidHTTPURL reports whether rawURL is a well-formed absolute http(s) URL.
// User-supplied resource/channel URLs are rendered into href attributes on the
// frontend, so non-http(s) schemes (notably javascript:) must be rejected at the
// source to prevent stored XSS.
func isValidHTTPURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// formatFieldError converts a single field validation error into a readable message.
func formatFieldError(fe validator.FieldError) string {
	field := fe.Field()
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "min":
		return fmt.Sprintf("%s must be at least %s", field, fe.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s", field, fe.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, fe.Param())
	case "email":
		return fmt.Sprintf("%s must be a valid email address", field)
	case "alphanum":
		return fmt.Sprintf("%s must contain only letters and numbers", field)
	default:
		return fmt.Sprintf("%s failed validation (%s)", field, fe.Tag())
	}
}

// errPasswordResetNotConfigured is a sentinel for the config-level case where
// password reset has no runtime error value available.
var errPasswordResetNotConfigured = errors.New("password reset not configured")

// uuidRegex matches standard UUID format (8-4-4-4-12 hex digits).
var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// isValidUUID returns true if s is a valid UUID v4 format string.
func isValidUUID(s string) bool {
	return uuidRegex.MatchString(s)
}

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
