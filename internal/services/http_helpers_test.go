package services

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadLimitedBody(t *testing.T) {
	t.Run("within limit returns data", func(t *testing.T) {
		input := "hello world"
		data, err := readLimitedBody(strings.NewReader(input), 100)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(data) != input {
			t.Fatalf("expected %q, got %q", input, string(data))
		}
	})

	t.Run("exactly at limit returns data", func(t *testing.T) {
		input := strings.Repeat("x", 50)
		data, err := readLimitedBody(strings.NewReader(input), 50)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(data) != 50 {
			t.Fatalf("expected 50 bytes, got %d", len(data))
		}
	})

	t.Run("exceeds limit returns error", func(t *testing.T) {
		input := strings.Repeat("x", 101)
		_, err := readLimitedBody(strings.NewReader(input), 100)
		if err == nil {
			t.Fatal("expected error for oversized body")
		}
		if !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("expected 'exceeds' in error message, got: %v", err)
		}
	})

	t.Run("empty body returns empty slice", func(t *testing.T) {
		data, err := readLimitedBody(strings.NewReader(""), 100)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(data) != 0 {
			t.Fatalf("expected empty slice, got %d bytes", len(data))
		}
	})

	t.Run("read error propagated", func(t *testing.T) {
		expectedErr := errors.New("read failure")
		reader := &errorReader{err: expectedErr}
		_, err := readLimitedBody(reader, 100)
		if err == nil {
			t.Fatal("expected error to be propagated")
		}
		if !strings.Contains(err.Error(), "read failure") {
			t.Fatalf("expected 'read failure' in error, got: %v", err)
		}
	})

	t.Run("one byte over limit returns error", func(t *testing.T) {
		input := strings.Repeat("a", 51)
		_, err := readLimitedBody(strings.NewReader(input), 50)
		if err == nil {
			t.Fatal("expected error for body 1 byte over limit")
		}
	})

	t.Run("works with bytes reader", func(t *testing.T) {
		input := []byte("binary data \x00\x01\x02")
		data, err := readLimitedBody(bytes.NewReader(input), 100)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Equal(data, input) {
			t.Fatalf("expected %v, got %v", input, data)
		}
	})
}

// errorReader is a test helper that always returns an error on Read.
type errorReader struct {
	err error
}

func (r *errorReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

// Compile-time check that errorReader implements io.Reader.
var _ io.Reader = (*errorReader)(nil)
