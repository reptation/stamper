package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/reptation/stamper/backend/internal/policy"
)

func TestHealth(t *testing.T) {
	server := NewServer()
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

func TestReadyReportsFalseUntilBundleIsLoaded(t *testing.T) {
	server := NewServer()
	req := httptest.NewRequest(http.MethodGet, "/v1/ready", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body struct {
		Ready bool `json:"ready"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if body.Ready {
		t.Fatal("expected ready=false before loading a bundle")
	}
}

func TestReadyReportsBundleVersionAfterLoad(t *testing.T) {
	server := NewServer()
	server.SetPolicyBundle(&policy.Bundle{Version: "v1"})

	req := httptest.NewRequest(http.MethodGet, "/v1/ready", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	var body struct {
		Ready               bool   `json:"ready"`
		PolicyBundleVersion string `json:"policy_bundle_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !body.Ready {
		t.Fatal("expected ready=true after loading a bundle")
	}
	if body.PolicyBundleVersion != "v1" {
		t.Fatalf("expected policy_bundle_version v1, got %q", body.PolicyBundleVersion)
	}
}
