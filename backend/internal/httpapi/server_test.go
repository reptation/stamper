package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/reptation/stamper/backend/internal/policy"
	"github.com/reptation/stamper/backend/internal/storage"
)

func TestHealth(t *testing.T) {
	server := NewServer(nil)
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
	server := NewServer(nil)
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
	server := NewServer(nil)
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

func TestRunLifecycleAPI(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "stamper.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	server := NewServer(store)

	createReq := httptest.NewRequest(http.MethodPost, "/v1/runs", bytes.NewBufferString(`{
		"agent_id":"hermes-ops-agent",
		"environment":"prod",
		"task":"Fetch customer data"
	}`))
	createRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create status 201, got %d", createRec.Code)
	}

	var createBody struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	if createBody.RunID == "" {
		t.Fatal("expected run_id in create response")
	}

	appendEvent := func(body string) {
		t.Helper()
		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/runs/"+createBody.RunID+"/events",
			bytes.NewBufferString(body),
		)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected append status 201, got %d body=%s", rec.Code, rec.Body.String())
		}
	}

	appendEvent(`{"event_type":"reasoning","payload":{"step":"plan"}}`)
	appendEvent(`{"event_type":"tool_call","payload":{"tool_name":"http_request"}}`)
	appendEvent(`{"event_type":"policy_decision","payload":{"decision":"deny"}}`)
	appendEvent(`{"event_type":"execution_blocked","payload":{"reason":"policy"}}`)

	finishReq := httptest.NewRequest(
		http.MethodPost,
		"/v1/runs/"+createBody.RunID+"/finish",
		bytes.NewBufferString(`{"status":"failed","output_summary":"Blocked by policy"}`),
	)
	finishRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(finishRec, finishReq)
	if finishRec.Code != http.StatusOK {
		t.Fatalf("expected finish status 200, got %d body=%s", finishRec.Code, finishRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/runs/"+createBody.RunID, nil)
	getRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected get status 200, got %d body=%s", getRec.Code, getRec.Body.String())
	}

	var getBody struct {
		Run    storage.Run     `json:"run"`
		Events []storage.Event `json:"events"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("unmarshal get response: %v", err)
	}

	if getBody.Run.Status != "failed" {
		t.Fatalf("expected failed status, got %q", getBody.Run.Status)
	}
	if len(getBody.Events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(getBody.Events))
	}

	wantTypes := []string{"reasoning", "tool_call", "policy_decision", "execution_blocked", "run_finished"}
	for i, event := range getBody.Events {
		if event.Sequence != int64(i+1) {
			t.Fatalf("expected sequence %d, got %d", i+1, event.Sequence)
		}
		if event.EventType != wantTypes[i] {
			t.Fatalf("expected event type %q, got %q", wantTypes[i], event.EventType)
		}
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/runs", nil)
	listRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d body=%s", listRec.Code, listRec.Body.String())
	}
}

func TestAppendEventRejectsInvalidEventType(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "stamper.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	server := NewServer(store)

	createReq := httptest.NewRequest(http.MethodPost, "/v1/runs", bytes.NewBufferString(`{
		"agent_id":"hermes-ops-agent",
		"environment":"prod",
		"task":"Fetch customer data"
	}`))
	createRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(createRec, createReq)

	var createBody struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	appendReq := httptest.NewRequest(
		http.MethodPost,
		"/v1/runs/"+createBody.RunID+"/events",
		bytes.NewBufferString(`{"event_type":"not_real","payload":{"step":"plan"}}`),
	)
	appendRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(appendRec, appendReq)

	if appendRec.Code != http.StatusBadRequest {
		t.Fatalf("expected append status 400, got %d body=%s", appendRec.Code, appendRec.Body.String())
	}
}
