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

func TestEvaluateActionReturnsDenyDecision(t *testing.T) {
	server := newTestServer(t)
	server.SetPolicyBundle(newTestPolicyBundle())

	rec := performRequest(t, server, http.MethodPost, "/v1/evaluate-action", `{
		"run_id":"run_123",
		"agent":{"id":"hermes-ops-agent","team":"platform"},
		"environment":{"name":"prod"},
		"action":{
			"type":"tool_call",
			"tool_name":"governed_http_request",
			"arguments":{"url":"https://example.com/resource","method":"GET"}
		}
	}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Decision         string `json:"decision"`
		PolicyID         string `json:"policy_id"`
		Rationale        string `json:"rationale"`
		Reason           string `json:"reason"`
		ApprovalRequired bool   `json:"approval_required"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if body.Decision != "deny" {
		t.Fatalf("expected deny decision, got %q", body.Decision)
	}
	if body.PolicyID != "POL-NET-001" {
		t.Fatalf("expected policy id POL-NET-001, got %q", body.PolicyID)
	}
	if body.Rationale == "" || body.Reason == "" {
		t.Fatal("expected rationale and reason in response")
	}
	if body.ApprovalRequired {
		t.Fatal("expected approval_required=false")
	}
}

func TestEvaluateActionReturnsAllowWhenNoPolicyMatches(t *testing.T) {
	server := newTestServer(t)
	server.SetPolicyBundle(newTestPolicyBundle())

	rec := performRequest(t, server, http.MethodPost, "/v1/evaluate-action", `{
		"run_id":"run_123",
		"agent":{"id":"hermes-ops-agent","team":"platform"},
		"environment":{"name":"dev"},
		"action":{
			"type":"tool_call",
			"tool_name":"governed_http_request",
			"arguments":{"url":"https://example.com/resource","method":"GET"}
		}
	}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Decision  string `json:"decision"`
		Rationale string `json:"rationale"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if body.Decision != "allow" {
		t.Fatalf("expected allow decision, got %q", body.Decision)
	}
	if body.Rationale == "" {
		t.Fatal("expected rationale in response")
	}
}

func TestEvaluateActionRequiresReadyPolicyEvaluator(t *testing.T) {
	server := newTestServer(t)

	rec := performRequest(t, server, http.MethodPost, "/v1/evaluate-action", `{
		"run_id":"run_123",
		"agent":{"id":"hermes-ops-agent"},
		"environment":{"name":"prod"},
		"action":{"type":"tool_call","tool_name":"governed_http_request"}
	}`)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateRunSuccess(t *testing.T) {
	server := newTestServer(t)

	rec := performRequest(t, server, http.MethodPost, "/v1/runs", `{
		"agent_id":"hermes-ops-agent",
		"environment":"prod",
		"task":"Fetch customer data"
	}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if body.RunID == "" {
		t.Fatal("expected run_id in response")
	}
}

func TestAppendEventSuccess(t *testing.T) {
	server := newTestServer(t)
	runID := createRun(t, server)

	rec := performRequest(t, server, http.MethodPost, "/v1/runs/"+runID+"/events", `{
		"event_type":"tool_call",
		"payload":{"tool_name":"http_request"}
	}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body storage.Event
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if body.RunID != runID {
		t.Fatalf("expected run_id %q, got %q", runID, body.RunID)
	}
	if body.Sequence != 1 {
		t.Fatalf("expected sequence 1, got %d", body.Sequence)
	}
	if body.EventType != "tool_call" {
		t.Fatalf("expected event_type tool_call, got %q", body.EventType)
	}
}

func TestFinishRunSuccess(t *testing.T) {
	server := newTestServer(t)
	runID := createRun(t, server)

	rec := performRequest(t, server, http.MethodPost, "/v1/runs/"+runID+"/finish", `{
		"status":"failed",
		"output_summary":"Blocked by policy"
	}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !body["ok"] {
		t.Fatal("expected ok=true in response")
	}
}

func TestGetRunDetailReturnsOrderedEvents(t *testing.T) {
	server := newTestServer(t)
	runID := createRun(t, server)

	performRequest(t, server, http.MethodPost, "/v1/runs/"+runID+"/events", `{
		"event_type":"tool_call",
		"payload":{"tool_name":"http_request"}
	}`)
	performRequest(t, server, http.MethodPost, "/v1/runs/"+runID+"/events", `{
		"event_type":"execution_result",
		"payload":{"ok":true}
	}`)
	performRequest(t, server, http.MethodPost, "/v1/runs/"+runID+"/finish", `{
		"status":"completed",
		"output_summary":"Finished successfully"
	}`)

	rec := performRequest(t, server, http.MethodGet, "/v1/runs/"+runID, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Run    storage.Run     `json:"run"`
		Events []storage.Event `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if body.Run.RunID != runID {
		t.Fatalf("expected run_id %q, got %q", runID, body.Run.RunID)
	}
	if len(body.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(body.Events))
	}

	wantTypes := []string{"tool_call", "execution_result", "run_finished"}
	for i, event := range body.Events {
		if event.Sequence != int64(i+1) {
			t.Fatalf("expected sequence %d, got %d", i+1, event.Sequence)
		}
		if event.EventType != wantTypes[i] {
			t.Fatalf("expected event type %q, got %q", wantTypes[i], event.EventType)
		}
	}
}

func TestInvalidRequestsReturnBadRequest(t *testing.T) {
	server := newTestServer(t)
	runID := createRun(t, server)
	server.SetPolicyBundle(newTestPolicyBundle())

	tests := []struct {
		name string
		path string
		body string
	}{
		{
			name: "create run missing required fields",
			path: "/v1/runs",
			body: `{"agent_id":"hermes-ops-agent","environment":"prod"}`,
		},
		{
			name: "append event missing payload",
			path: "/v1/runs/" + runID + "/events",
			body: `{"event_type":"tool_call"}`,
		},
		{
			name: "append event invalid event type",
			path: "/v1/runs/" + runID + "/events",
			body: `{"event_type":"not_real","payload":{"step":"plan"}}`,
		},
		{
			name: "finish run missing output summary",
			path: "/v1/runs/" + runID + "/finish",
			body: `{"status":"failed"}`,
		},
		{
			name: "finish run invalid status",
			path: "/v1/runs/" + runID + "/finish",
			body: `{"status":"running","output_summary":"still going"}`,
		},
		{
			name: "append event invalid json",
			path: "/v1/runs/" + runID + "/events",
			body: `{"event_type":"tool_call","payload":{`,
		},
		{
			name: "evaluate action missing action tool name",
			path: "/v1/evaluate-action",
			body: `{
				"run_id":"run_123",
				"agent":{"id":"hermes-ops-agent"},
				"environment":{"name":"prod"},
				"action":{"type":"tool_call"}
			}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := performRequest(t, server, http.MethodPost, tc.path, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d body=%s", rec.Code, rec.Body.String())
			}

			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal error response: %v", err)
			}
			if body["error"] == "" {
				t.Fatal("expected error message in response")
			}
		})
	}
}

func TestUnknownRunReturnsNotFound(t *testing.T) {
	server := newTestServer(t)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "append event",
			method: http.MethodPost,
			path:   "/v1/runs/run_missing/events",
			body:   `{"event_type":"tool_call","payload":{"tool_name":"http_request"}}`,
		},
		{
			name:   "finish run",
			method: http.MethodPost,
			path:   "/v1/runs/run_missing/finish",
			body:   `{"status":"failed","output_summary":"Blocked by policy"}`,
		},
		{
			name:   "get run",
			method: http.MethodGet,
			path:   "/v1/runs/run_missing",
			body:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := performRequest(t, server, tc.method, tc.path, tc.body)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected status 404, got %d body=%s", rec.Code, rec.Body.String())
			}

			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal error response: %v", err)
			}
			if body["error"] != "run not found" {
				t.Fatalf("expected run not found error, got %q", body["error"])
			}
		})
	}
}

func TestListRunsSuccess(t *testing.T) {
	server := newTestServer(t)
	runID := createRun(t, server)

	rec := performRequest(t, server, http.MethodGet, "/v1/runs", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Runs []storage.Run `json:"runs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(body.Runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(body.Runs))
	}
	if body.Runs[0].RunID != runID {
		t.Fatalf("expected run_id %q, got %q", runID, body.Runs[0].RunID)
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()

	store, err := storage.Open(filepath.Join(t.TempDir(), "stamper.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	return NewServer(store)
}

func createRun(t *testing.T, server *Server) string {
	t.Helper()

	rec := performRequest(t, server, http.MethodPost, "/v1/runs", `{
		"agent_id":"hermes-ops-agent",
		"environment":"prod",
		"task":"Fetch customer data"
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	return body.RunID
}

func performRequest(t *testing.T, server *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	var requestBody *bytes.Buffer
	if body == "" {
		requestBody = bytes.NewBuffer(nil)
	} else {
		requestBody = bytes.NewBufferString(body)
	}

	req := httptest.NewRequest(method, path, requestBody)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	return rec
}

func newTestPolicyBundle() *policy.Bundle {
	return &policy.Bundle{
		Version: "v1",
		Policies: []policy.Policy{
			{
				ID:        "POL-NET-001",
				Name:      "Block governed HTTP in prod",
				Enabled:   true,
				Priority:  200,
				Effect:    "deny",
				Rationale: "Outbound HTTP requests are not allowed in prod.",
				Scope: policy.Scope{
					Agents:       []string{"*"},
					Environments: []string{"prod"},
					Teams:        []string{"*"},
				},
				Match: policy.Match{
					ActionTypes: []string{"tool_call"},
					ToolNames:   []string{"governed_http_request"},
				},
			},
		},
	}
}
