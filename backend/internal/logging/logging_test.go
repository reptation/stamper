package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewWithConfigEmitsStructuredJSON(t *testing.T) {
	var buffer bytes.Buffer

	logger, err := NewWithConfig("stamperd", Config{
		Level:  "info",
		Format: "json",
	}, &buffer)
	if err != nil {
		t.Fatalf("build logger: %v", err)
	}

	logger.With("component", "startup").Info("configuration loaded", "run_id", "run_123")

	var entry map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log entry: %v", err)
	}

	if entry["timestamp"] == "" {
		t.Fatal("expected timestamp field")
	}
	if entry["level"] != "info" {
		t.Fatalf("expected level info, got %#v", entry["level"])
	}
	if entry["service"] != "stamperd" {
		t.Fatalf("expected service stamperd, got %#v", entry["service"])
	}
	if entry["component"] != "startup" {
		t.Fatalf("expected component startup, got %#v", entry["component"])
	}
	if entry["message"] != "configuration loaded" {
		t.Fatalf("expected message field, got %#v", entry["message"])
	}
	if entry["run_id"] != "run_123" {
		t.Fatalf("expected run_id run_123, got %#v", entry["run_id"])
	}
}

func TestMiddlewareAssignsRequestIDToContextAndResponse(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestID(r.Context()); got == "" {
			t.Fatal("expected request id in context")
		}

		_, _ = w.Write([]byte(RequestID(r.Context())))
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get(RequestIDHeader) == "" {
		t.Fatal("expected request id response header")
	}
	if rec.Body.String() != rec.Header().Get(RequestIDHeader) {
		t.Fatalf("expected request id body to match header, got body=%q header=%q", rec.Body.String(), rec.Header().Get(RequestIDHeader))
	}
}

func TestWithContextAddsRequestIDField(t *testing.T) {
	var buffer bytes.Buffer

	logger, err := NewWithConfig("stamperd", Config{
		Level:  "info",
		Format: "json",
	}, &buffer)
	if err != nil {
		t.Fatalf("build logger: %v", err)
	}

	ctx := context.WithValue(context.Background(), requestIDContextKey{}, "req_123")
	WithContext(logger.With("component", "http_handler"), ctx).Info("request received")

	var entry map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log entry: %v", err)
	}

	if entry["request_id"] != "req_123" {
		t.Fatalf("expected request_id req_123, got %#v", entry["request_id"])
	}
}
