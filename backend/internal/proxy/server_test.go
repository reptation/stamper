package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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

	server := NewServer(stamper.URL, upstream.Client())

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
	server := NewServer("http://localhost:8080", http.DefaultClient)

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

	server := NewServer(stamper.URL, stamper.Client())

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
