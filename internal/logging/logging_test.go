package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
		{"INFO", slog.LevelInfo}, // case-sensitive: not matched, falls through to default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLevel(tt.input)
			if got != tt.expected {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestWithRequestIDAndRequestID(t *testing.T) {
	ctx := context.Background()

	if got := RequestID(ctx); got != "" {
		t.Errorf("RequestID on empty ctx = %q, want \"\"", got)
	}

	ctx = WithRequestID(ctx, "abc-123")
	if got := RequestID(ctx); got != "abc-123" {
		t.Errorf("RequestID after WithRequestID = %q, want %q", got, "abc-123")
	}
}

func TestRequestID_WrongType(t *testing.T) {
	// Manually inject a non-string value at the request ID key by going through
	// WithRequestID with a context that overrides it via direct context.WithValue.
	// Since requestIDKey is unexported, we use a parallel scenario: the lookup
	// must return "" when the key is absent or value type mismatches.
	ctx := context.WithValue(context.Background(), contextKey("request_id"), 12345)
	// Note: contextKey("request_id") is a distinct key from the package's
	// internal requestIDKey constant value because Go context keys compare by
	// identity of the typed value. However since contextKey is defined in this
	// same package (test file is package logging), and requestIDKey == contextKey("request_id"),
	// they compare equal.
	if got := RequestID(ctx); got != "" {
		t.Errorf("RequestID with non-string value = %q, want \"\"", got)
	}
}

func TestRequestID_EmptyString(t *testing.T) {
	ctx := WithRequestID(context.Background(), "")
	if got := RequestID(ctx); got != "" {
		t.Errorf("RequestID with empty string = %q, want \"\"", got)
	}
}

func TestContextHandler_Enabled(t *testing.T) {
	base := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := &contextHandler{base: base}

	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Enabled(Debug) = true, want false")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled(Error) = false, want true")
	}
}

func TestContextHandler_Handle_AddsRequestID(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(&contextHandler{base: base})

	ctx := WithRequestID(context.Background(), "req-42")
	logger.InfoContext(ctx, "hello")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; output: %s", err, buf.String())
	}
	if got["request_id"] != "req-42" {
		t.Errorf("request_id = %v, want %q", got["request_id"], "req-42")
	}
	if got["msg"] != "hello" {
		t.Errorf("msg = %v, want %q", got["msg"], "hello")
	}
}

func TestContextHandler_Handle_OmitsRequestIDWhenAbsent(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(&contextHandler{base: base})

	logger.InfoContext(context.Background(), "hello")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; output: %s", err, buf.String())
	}
	if _, ok := got["request_id"]; ok {
		t.Errorf("request_id present but should be absent: %v", got["request_id"])
	}
}

func TestContextHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := &contextHandler{base: base}

	wrapped := h.WithAttrs([]slog.Attr{slog.String("service", "api")})
	if _, ok := wrapped.(*contextHandler); !ok {
		t.Fatalf("WithAttrs did not return *contextHandler, got %T", wrapped)
	}

	logger := slog.New(wrapped)
	ctx := WithRequestID(context.Background(), "r1")
	logger.InfoContext(ctx, "msg")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["service"] != "api" {
		t.Errorf("service = %v, want \"api\"", got["service"])
	}
	if got["request_id"] != "r1" {
		t.Errorf("request_id = %v, want \"r1\"", got["request_id"])
	}
}

func TestContextHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := &contextHandler{base: base}

	wrapped := h.WithGroup("g1")
	if _, ok := wrapped.(*contextHandler); !ok {
		t.Fatalf("WithGroup did not return *contextHandler, got %T", wrapped)
	}

	logger := slog.New(wrapped)
	logger.InfoContext(context.Background(), "msg", "k", "v")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; output: %s", err, buf.String())
	}
	g, ok := got["g1"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested group g1, got: %v", got)
	}
	if g["k"] != "v" {
		t.Errorf("g1.k = %v, want \"v\"", g["k"])
	}
}

// captureStdout swaps os.Stdout for the duration of fn and returns whatever
// fn wrote. Init writes to os.Stdout directly, so capturing it requires this.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stdout = orig
	return <-done
}

func TestInit_JSONFormat(t *testing.T) {
	origLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(origLogger) })

	out := captureStdout(t, func() {
		Init("json", "debug")
		ctx := WithRequestID(context.Background(), "init-json")
		slog.InfoContext(ctx, "from-init-json")
	})

	if !strings.Contains(out, "\"msg\":\"from-init-json\"") {
		t.Errorf("expected JSON output containing msg, got: %s", out)
	}
	if !strings.Contains(out, "\"request_id\":\"init-json\"") {
		t.Errorf("expected JSON output containing request_id, got: %s", out)
	}
}

func TestInit_TextFormat(t *testing.T) {
	origLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(origLogger) })

	out := captureStdout(t, func() {
		Init("text", "warn")
		ctx := WithRequestID(context.Background(), "init-text")
		// Debug should be suppressed by warn level
		slog.DebugContext(ctx, "suppressed")
		slog.WarnContext(ctx, "from-init-text")
	})

	if strings.Contains(out, "suppressed") {
		t.Errorf("debug message leaked at warn level: %s", out)
	}
	if !strings.Contains(out, "from-init-text") {
		t.Errorf("expected text output containing warn message, got: %s", out)
	}
	if !strings.Contains(out, "request_id=init-text") {
		t.Errorf("expected text output containing request_id, got: %s", out)
	}
}
