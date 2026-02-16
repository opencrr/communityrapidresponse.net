package sentry

import (
	"context"
	"fmt"
	"log/slog"

	gosentry "github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
)

// meter holds the global Sentry meter for custom metrics.
// It is initialized by initMeter() after Sentry SDK init.
var meter gosentry.Meter

// Init initializes the Sentry SDK. If cfg.DSN is empty, Sentry is disabled (no-op).
func Init(cfg *config.SentryConfig) error {
	if cfg.DSN == "" {
		slog.Info("sentry disabled", "reason", "SENTRY_DSN not set")
		return nil
	}

	opts := gosentry.ClientOptions{
		Dsn:              cfg.DSN,
		Environment:      cfg.Environment,
		Release:          cfg.Release,
		TracesSampleRate: cfg.TracesSampleRate,
		EnableTracing:    cfg.TracesSampleRate > 0,
		Debug:            cfg.Debug,
	}

	err := gosentry.Init(opts)
	if err != nil {
		return fmt.Errorf("sentry init: %w", err)
	}

	initMeter()

	slog.Info("sentry initialized", "environment", cfg.Environment, "release", cfg.Release, "traces_sample_rate", cfg.TracesSampleRate)
	return nil
}

// initMeter creates the global Sentry meter for custom metrics.
func initMeter() {
	meter = gosentry.NewMeter(context.Background())
}

// Initialized returns true if the Sentry SDK has an active client.
func Initialized() bool {
	return gosentry.CurrentHub().Client() != nil
}

// CaptureError sends an error to Sentry with optional key-value tag pairs.
// Returns the original error for inline use. Safe no-op when Sentry is not initialized.
func CaptureError(err error, tags ...string) error {
	if err == nil || !Initialized() {
		return err
	}

	hub := gosentry.CurrentHub().Clone()
	applyTags(hub.Scope(), tags)
	hub.CaptureException(err)
	return err
}

// CaptureErrorWithContext sends an error to Sentry using the per-request hub
// from sentryhttp middleware context. Falls back to CaptureError if no hub in context.
// Safe no-op when Sentry is not initialized.
func CaptureErrorWithContext(ctx context.Context, err error, tags ...string) error {
	if err == nil || !Initialized() {
		return err
	}

	hub := gosentry.GetHubFromContext(ctx)
	if hub == nil {
		return CaptureError(err, tags...)
	}

	hub.WithScope(func(scope *gosentry.Scope) {
		applyTags(scope, tags)
		hub.CaptureException(err)
	})
	return err
}

// RecoverAndReport reports a recovered panic value to Sentry.
// Safe no-op when Sentry is not initialized or panicValue is nil.
func RecoverAndReport(panicValue interface{}) {
	if panicValue == nil || !Initialized() {
		return
	}

	hub := gosentry.CurrentHub().Clone()

	switch v := panicValue.(type) {
	case error:
		hub.CaptureException(v)
	default:
		hub.CaptureMessage(fmt.Sprintf("panic: %v", v))
	}
}

// SetUser sets the authenticated user on the per-request Sentry hub.
// Safe no-op when Sentry is not initialized or no hub in context.
func SetUser(ctx context.Context, userID string) {
	if !Initialized() {
		return
	}

	hub := gosentry.GetHubFromContext(ctx)
	if hub == nil {
		return
	}

	hub.Scope().SetUser(gosentry.User{ID: userID})
}

// StartSpan creates a child span on the current transaction from context.
// Returns the span (call span.Finish() when done) or nil if tracing is inactive.
// Safe no-op when Sentry is not initialized.
//
// When called from a background worker context (no parent transaction),
// WithTransactionName ensures the root transaction is labeled with the
// operation description instead of appearing as "<unlabeled transaction>".
func StartSpan(ctx context.Context, operation, description string) *gosentry.Span {
	if !Initialized() {
		return nil
	}

	span := gosentry.StartSpan(ctx, operation, gosentry.WithTransactionName(description))
	span.Description = description
	return span
}

// CheckIn sends a cron monitor check-in to Sentry.
// status should be gosentry.CheckInStatusInProgress, CheckInStatusOK, or CheckInStatusError.
// For a start check-in, pass nil for checkInID.
// For a finish check-in, pass the ID returned by the start call.
// Returns the check-in event ID (use for the finish call). Safe no-op when uninitialized.
func CheckIn(monitorSlug string, status gosentry.CheckInStatus, checkInID *gosentry.EventID, monitorConfig *gosentry.MonitorConfig) *gosentry.EventID {
	if !Initialized() {
		return nil
	}

	checkIn := &gosentry.CheckIn{
		MonitorSlug: monitorSlug,
		Status:      status,
	}
	if checkInID != nil {
		checkIn.ID = *checkInID
	}

	return gosentry.CaptureCheckIn(checkIn, monitorConfig)
}

// IntervalSchedule is a convenience to build a MonitorConfig for interval-based cron monitors.
// CheckInMargin gives a grace period (in minutes) before a missed check-in triggers an alert.
// MaxRuntime caps how long a single run can take (in minutes) before it's marked timed-out.
func IntervalSchedule(value int, unit gosentry.MonitorScheduleUnit) *gosentry.MonitorConfig {
	return &gosentry.MonitorConfig{
		Schedule:      gosentry.IntervalSchedule(int64(value), unit),
		CheckInMargin: 2,
		MaxRuntime:    5,
	}
}

// IncrementCounter emits a counter metric. Tags are alternating key, value strings.
// Safe no-op when Sentry is not initialized.
func IncrementCounter(name string, value int64, tags ...string) {
	if meter == nil {
		return
	}
	meter.Count(name, value, buildAttributeOpts(tags)...)
}

// SetGauge emits a gauge metric. Tags are alternating key, value strings.
// Safe no-op when Sentry is not initialized.
func SetGauge(name string, value float64, tags ...string) {
	if meter == nil {
		return
	}
	meter.Gauge(name, value, buildAttributeOpts(tags)...)
}

// RecordDistribution emits a distribution metric. Tags are alternating key, value strings.
// Safe no-op when Sentry is not initialized.
func RecordDistribution(name string, value float64, tags ...string) {
	if meter == nil {
		return
	}
	meter.Distribution(name, value, buildAttributeOpts(tags)...)
}

// buildAttributeOpts converts alternating key, value string pairs into MeterOption slice.
func buildAttributeOpts(tags []string) []gosentry.MeterOption {
	if len(tags) < 2 {
		return nil
	}

	builders := make([]attribute.Builder, 0, len(tags)/2)
	for i := 0; i+1 < len(tags); i += 2 {
		builders = append(builders, attribute.String(tags[i], tags[i+1]))
	}
	return []gosentry.MeterOption{gosentry.WithAttributes(builders...)}
}

// applyTags sets key-value pairs on a Sentry scope.
// Tags are provided as alternating key, value strings.
func applyTags(scope *gosentry.Scope, tags []string) {
	for i := 0; i+1 < len(tags); i += 2 {
		scope.SetTag(tags[i], tags[i+1])
	}
}
