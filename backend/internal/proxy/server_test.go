package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/reptation/stamper/backend/internal/logging"
)

func TestRequestForwardsWhenTokenIsValid(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Set-Cookie", "session=secret")
		_, _ = w.Write([]byte("hello world"))
	}))
	defer upstream.Close()

	stamper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/validate-token" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"valid": true})
	}))
	defer stamper.Close()

	server := NewServer(stamper.URL, upstream.Client(), testLogger(t))

	body := `{"method":"GET","url":"` + upstream.URL + `","headers":{"Accept":"text/plain"},"timeout_ms":5000}`
	req := httptest.NewRequest(http.MethodPost, "/request", bytes.NewBufferString(body))
	req.Header.Set("X-Stamper-Token", "tok_123")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var response proxyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if response.Status != "success" {
		t.Fatalf("expected success, got %q", response.Status)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected upstream status 200, got %d", response.StatusCode)
	}
	if response.Headers["Set-Cookie"] != "[REDACTED]" {
		t.Fatalf("expected redacted cookie header, got %q", response.Headers["Set-Cookie"])
	}
	if response.Body != "hello world" {
		t.Fatalf("expected body hello world, got %q", response.Body)
	}
}

func TestRequestRejectsMissingToken(t *testing.T) {
	server := NewServer("http://localhost:8080", http.DefaultClient, testLogger(t))

	req := httptest.NewRequest(http.MethodPost, "/request", bytes.NewBufferString(`{"method":"GET","url":"https://example.com"}`))
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRequestReturnsForbiddenWhenTokenValidationFails(t *testing.T) {
	stamper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusForbidden, "approval token method mismatch")
	}))
	defer stamper.Close()

	server := NewServer(stamper.URL, stamper.Client(), testLogger(t))

	req := httptest.NewRequest(
		http.MethodPost,
		"/request",
		bytes.NewBufferString(`{"method":"GET","url":"https://example.com"}`),
	)
	req.Header.Set("X-Stamper-Token", "tok_123")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRequestPropagatesRequestIDAndRunIDToStamper(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	var capturedRequestID string
	var capturedRunID string

	stamper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequestID = r.Header.Get(logging.RequestIDHeader)

		var body validateTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode validate request: %v", err)
		}
		capturedRunID = body.RunID

		writeJSON(w, http.StatusOK, map[string]any{"valid": true})
	}))
	defer stamper.Close()

	server := NewServer(stamper.URL, upstream.Client(), testLogger(t))

	req := httptest.NewRequest(
		http.MethodPost,
		"/request",
		bytes.NewBufferString(`{"method":"GET","url":"`+upstream.URL+`","run_id":"run_123"}`),
	)
	req.Header.Set("X-Stamper-Token", "tok_123")
	req.Header.Set(logging.RequestIDHeader, "req_test_123")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if capturedRequestID != "req_test_123" {
		t.Fatalf("expected propagated request id, got %q", capturedRequestID)
	}
	if capturedRunID != "run_123" {
		t.Fatalf("expected propagated run id, got %q", capturedRunID)
	}
	if rec.Header().Get(logging.RequestIDHeader) != "req_test_123" {
		t.Fatalf("expected response request id header, got %q", rec.Header().Get(logging.RequestIDHeader))
	}
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()

	logger, err := logging.NewWithConfig("stamper-proxy", logging.Config{
		Level:  logging.DefaultLevel,
		Format: logging.FormatJSON,
	}, io.Discard)
	if err != nil {
		t.Fatalf("build test logger: %v", err)
	}

	return logger
}
