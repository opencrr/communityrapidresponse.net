// Package sentry wraps the Sentry SDK for error tracking, performance
// tracing, and metrics.
//
// [Init] configures the SDK from [config.SentryConfig]. After
// initialization, use [CaptureError] or [CaptureErrorWithContext] to
// report errors, [StartSpan] for distributed tracing, [CheckIn] for
// cron monitor heartbeats, and the metric helpers ([IncrementCounter],
// [SetGauge], [RecordDistribution]) for custom telemetry.
// [RecoverAndReport] is intended for deferred panic recovery.
package sentry
