package sentry

import (
	"context"
	"errors"
	"testing"

	gosentry "github.com/getsentry/sentry-go"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
)

func TestInitEmptyDSN(t *testing.T) {
	cfg := &config.SentryConfig{DSN: "", Environment: "test"}
	if err := Init(cfg); err != nil {
		t.Fatalf("Init with empty DSN should not error, got: %v", err)
	}
	if Initialized() {
		t.Fatal("Initialized() should return false when DSN is empty")
	}
}

func TestInitWithTracing(t *testing.T) {
	cfg := &config.SentryConfig{
		DSN:              "",
		Environment:      "test",
		Release:          "abc123",
		TracesSampleRate: 0.5,
	}
	if err := Init(cfg); err != nil {
		t.Fatalf("Init should not error with empty DSN even with tracing config, got: %v", err)
	}
}

// --- CaptureError ---

func TestCaptureErrorNilError(t *testing.T) {
	result := CaptureError(nil, "key", "value")
	if result != nil {
		t.Fatal("CaptureError(nil) should return nil")
	}
}

func TestCaptureErrorWithoutInit(t *testing.T) {
	testErr := errors.New("test error")
	result := CaptureError(testErr, "key", "value")
	if result != testErr {
		t.Fatal("CaptureError should return the original error")
	}
}

// --- CaptureErrorWithContext ---

func TestCaptureErrorWithContextNilError(t *testing.T) {
	result := CaptureErrorWithContext(context.Background(), nil)
	if result != nil {
		t.Fatal("CaptureErrorWithContext(nil) should return nil")
	}
}

func TestCaptureErrorWithContextWithoutInit(t *testing.T) {
	testErr := errors.New("test error")
	result := CaptureErrorWithContext(context.Background(), testErr, "key", "value")
	if result != testErr {
		t.Fatal("CaptureErrorWithContext should return the original error")
	}
}

// --- RecoverAndReport ---

func TestRecoverAndReportNil(t *testing.T) {
	RecoverAndReport(nil)
}

func TestRecoverAndReportWithoutInit(t *testing.T) {
	RecoverAndReport("test panic")
	RecoverAndReport(errors.New("test panic error"))
}

// --- SetUser ---

func TestSetUserWithoutInit(t *testing.T) {
	// Should not panic when Sentry is not initialized
	SetUser(context.Background(), "user-123")
}

func TestSetUserNilHub(t *testing.T) {
	// Context without sentryhttp hub — should be a safe no-op
	SetUser(context.Background(), "user-456")
}

// --- StartSpan ---

func TestStartSpanWithoutInit(t *testing.T) {
	span := StartSpan(context.Background(), "http.client", "GET /test")
	if span != nil {
		t.Fatal("StartSpan should return nil when Sentry is not initialized")
	}
}

// --- CheckIn ---

func TestCheckInWithoutInit(t *testing.T) {
	result := CheckIn("test-monitor", gosentry.CheckInStatusInProgress, nil, nil)
	if result != nil {
		t.Fatal("CheckIn should return nil when Sentry is not initialized")
	}
}

func TestCheckInFinishWithoutInit(t *testing.T) {
	eventID := gosentry.EventID("fake-id")
	result := CheckIn("test-monitor", gosentry.CheckInStatusOK, &eventID, nil)
	if result != nil {
		t.Fatal("CheckIn finish should return nil when Sentry is not initialized")
	}
}

// --- Metrics Helpers ---

func TestIncrementCounterWithoutInit(t *testing.T) {
	// Should not panic when Sentry is not initialized (meter is nil)
	IncrementCounter("test.counter", 1, "key", "value")
}

func TestSetGaugeWithoutInit(t *testing.T) {
	// Should not panic when Sentry is not initialized (meter is nil)
	SetGauge("test.gauge", 42.0, "key", "value")
}

func TestRecordDistributionWithoutInit(t *testing.T) {
	// Should not panic when Sentry is not initialized (meter is nil)
	RecordDistribution("test.distribution", 3.14, "key", "value")
}

func TestMetricsNoTagsWithoutInit(t *testing.T) {
	// Should not panic with no tags
	IncrementCounter("test.counter", 1)
	SetGauge("test.gauge", 1.0)
	RecordDistribution("test.distribution", 1.0)
}

func TestBuildAttributeOpts(t *testing.T) {
	// Empty tags should return nil
	result := buildAttributeOpts(nil)
	if result != nil {
		t.Fatal("buildAttributeOpts(nil) should return nil")
	}

	// Single tag (odd number) should return nil
	result = buildAttributeOpts([]string{"key"})
	if result != nil {
		t.Fatal("buildAttributeOpts with single element should return nil")
	}

	// Valid pairs should return a single MeterOption
	result = buildAttributeOpts([]string{"key1", "val1", "key2", "val2"})
	if len(result) != 1 {
		t.Fatalf("expected 1 MeterOption, got %d", len(result))
	}
}

// --- IntervalSchedule ---

func TestIntervalSchedule(t *testing.T) {
	cfg := IntervalSchedule(60, gosentry.MonitorScheduleUnitMinute)
	if cfg == nil {
		t.Fatal("IntervalSchedule should return a non-nil MonitorConfig")
	}
}
