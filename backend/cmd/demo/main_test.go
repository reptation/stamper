package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/reptation/stamper/backend/internal/httpapi"
	"github.com/reptation/stamper/backend/internal/policy"
	"github.com/reptation/stamper/backend/internal/storage"
)

func TestRunDemoCreatesDenyPathTimeline(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "stamper.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	bundle, err := policy.LoadBundle(filepath.Join("..", "..", "policies", "dev.json"))
	if err != nil {
		t.Fatalf("load bundle: %v", err)
	}

	apiServer := httpapi.NewServer(store)
	apiServer.SetPolicyBundle(bundle)

	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := runDemo(ctx, server.Client(), server.URL)
	if err != nil {
		t.Fatalf("run demo: %v", err)
	}

	if result.RunID == "" {
		t.Fatal("expected run_id to be set")
	}
	if result.Decision.Decision != "deny" {
		t.Fatalf("expected deny decision, got %q", result.Decision.Decision)
	}
	if result.Decision.PolicyID != "POL-NET-001" {
		t.Fatalf("expected policy id POL-NET-001, got %q", result.Decision.PolicyID)
	}

	run, events, err := store.GetRun(ctx, result.RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if run.Status != "failed" {
		t.Fatalf("expected failed status, got %q", run.Status)
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}

	wantTypes := []string{
		"reasoning",
		"tool_requested",
		"policy_decision",
		"run_finished",
	}
	for i, event := range events {
		if event.Sequence != int64(i+1) {
			t.Fatalf("expected sequence %d, got %d", i+1, event.Sequence)
		}
		if event.EventType != wantTypes[i] {
			t.Fatalf("expected event type %q, got %q", wantTypes[i], event.EventType)
		}
	}

	var policyDecisionPayload struct {
		Decision  string `json:"decision"`
		PolicyID  string `json:"policy_id"`
		Rationale string `json:"rationale"`
	}
	if err := json.Unmarshal(events[2].Payload, &policyDecisionPayload); err != nil {
		t.Fatalf("unmarshal policy decision payload: %v", err)
	}
	if policyDecisionPayload.Decision != "deny" {
		t.Fatalf("expected policy decision deny, got %q", policyDecisionPayload.Decision)
	}
	if policyDecisionPayload.PolicyID != "POL-NET-001" {
		t.Fatalf("expected policy id POL-NET-001, got %q", policyDecisionPayload.PolicyID)
	}
}

func TestAPIBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		httpAddr string
		want     string
	}{
		{name: "host and port", httpAddr: "127.0.0.1:18081", want: "http://127.0.0.1:18081"},
		{name: "port only", httpAddr: ":8080", want: "http://127.0.0.1:8080"},
		{name: "http url", httpAddr: "http://localhost:8080", want: "http://localhost:8080"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := apiBaseURL(tc.httpAddr); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
