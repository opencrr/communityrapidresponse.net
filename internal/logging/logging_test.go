package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
		{"DEBUG", slog.LevelInfo}, // case-sensitive
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := parseLevel(tc.in); got != tc.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestWithRequestIDAndRequestID(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		ctx := WithRequestID(context.Background(), "abc-123")
		if got := RequestID(ctx); got != "abc-123" {
			t.Errorf("RequestID = %q, want %q", got, "abc-123")
		}
	})

	t.Run("missing key returns empty string", func(t *testing.T) {
		if got := RequestID(context.Background()); got != "" {
			t.Errorf("RequestID(empty ctx) = %q, want \"\"", got)
		}
	})

	t.Run("wrong-type value returns empty string", func(t *testing.T) {
		// Insert a non-string value under the same key.
		ctx := context.WithValue(context.Background(), requestIDKey, 42)
		if got := RequestID(ctx); got != "" {
			t.Errorf("RequestID(int-valued ctx) = %q, want \"\"", got)
		}
	})

	t.Run("overwrites prior request id", func(t *testing.T) {
		ctx := WithRequestID(context.Background(), "first")
		ctx = WithRequestID(ctx, "second")
		if got := RequestID(ctx); got != "second" {
			t.Errorf("RequestID = %q, want %q", got, "second")
		}
	})
}

// captureJSON runs h.Handle with the given context and returns the parsed JSON
// record emitted to buf.
func captureJSON(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("failed to parse json output %q: %v", buf.String(), err)
	}
	return out
}

func newJSONHandler(buf *bytes.Buffer) *contextHandler {
	base := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return &contextHandler{base: base}
}

func TestContextHandler_Handle_InjectsRequestID(t *testing.T) {
	buf := &bytes.Buffer{}
	h := newJSONHandler(buf)

	ctx := WithRequestID(context.Background(), "req-42")
	logger := slog.New(h)
	logger.InfoContext(ctx, "hello")

	rec := captureJSON(t, buf)
	if got, _ := rec["request_id"].(string); got != "req-42" {
		t.Errorf("request_id attr = %v, want %q", rec["request_id"], "req-42")
	}
	if got, _ := rec["msg"].(string); got != "hello" {
		t.Errorf("msg = %v, want %q", rec["msg"], "hello")
	}
}

func TestContextHandler_Handle_OmitsRequestIDWhenMissing(t *testing.T) {
	buf := &bytes.Buffer{}
	h := newJSONHandler(buf)

	logger := slog.New(h)
	logger.InfoContext(context.Background(), "no id")

	rec := captureJSON(t, buf)
	if _, present := rec["request_id"]; present {
		t.Errorf("expected no request_id key, got %v", rec["request_id"])
	}
}

func TestContextHandler_Enabled_DelegatesToBase(t *testing.T) {
	buf := &bytes.Buffer{}
	base := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := &contextHandler{base: base}

	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Enabled(Debug) should be false when base level is Warn")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled(Error) should be true when base level is Warn")
	}
}

func TestContextHandler_WithAttrs_StillInjectsRequestID(t *testing.T) {
	buf := &bytes.Buffer{}
	h := newJSONHandler(buf)

	wrapped := h.WithAttrs([]slog.Attr{slog.String("component", "auth")})
	if _, ok := wrapped.(*contextHandler); !ok {
		t.Fatalf("WithAttrs should return *contextHandler, got %T", wrapped)
	}

	logger := slog.New(wrapped)
	ctx := WithRequestID(context.Background(), "req-99")
	logger.InfoContext(ctx, "msg")

	rec := captureJSON(t, buf)
	if got, _ := rec["request_id"].(string); got != "req-99" {
		t.Errorf("request_id = %v, want %q", rec["request_id"], "req-99")
	}
	if got, _ := rec["component"].(string); got != "auth" {
		t.Errorf("component attr lost: got %v", rec["component"])
	}
}

func TestContextHandler_WithGroup_StillInjectsRequestID(t *testing.T) {
	buf := &bytes.Buffer{}
	h := newJSONHandler(buf)

	wrapped := h.WithGroup("svc")
	if _, ok := wrapped.(*contextHandler); !ok {
		t.Fatalf("WithGroup should return *contextHandler, got %T", wrapped)
	}

	logger := slog.New(wrapped)
	ctx := WithRequestID(context.Background(), "req-grp")
	logger.InfoContext(ctx, "msg", slog.String("k", "v"))

	rec := captureJSON(t, buf)
	// request_id is added at the top level (not inside the group) because
	// AddAttrs on the record runs before the group prefix is applied to
	// subsequent attributes. Either way, it should still be present and
	// resolve to the expected value somewhere in the record.
	if got, _ := rec["request_id"].(string); got != "req-grp" {
		// Fall back: it may have ended up inside the group.
		if grp, ok := rec["svc"].(map[string]any); ok {
			if got, _ := grp["request_id"].(string); got == "req-grp" {
				return
			}
		}
		t.Errorf("request_id not found after WithGroup; record = %v", rec)
	}
}

func TestInit_DoesNotPanic(t *testing.T) {
	// Init mutates slog's default logger. Save and restore.
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	Init("json", "debug")
	Init("text", "warn")
	Init("text", "") // exercises default level branch
}
