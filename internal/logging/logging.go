package logging

import (
	"context"
	"log/slog"
	"os"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// Init configures the default slog logger.
// format: "json" for structured JSON output, anything else for human-readable text.
// level: "debug", "info", "warn", "error" (default "info").
func Init(format, level string) {
	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	slog.SetDefault(slog.New(handler))
}

// WithRequestID returns a context carrying the given request ID.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestID extracts the request ID from the context, or returns "".
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
