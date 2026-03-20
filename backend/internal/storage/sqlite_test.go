package storage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestStoreRunLifecycleAndPersistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "stamper.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	ctx := context.Background()
	runID, err := store.CreateRun(ctx, "hermes-ops-agent", "prod", "Fetch customer data")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	firstEvent, err := store.AppendEvent(ctx, runID, "reasoning", json.RawMessage(`{"step":"plan"}`))
	if err != nil {
		t.Fatalf("append first event: %v", err)
	}
	secondEvent, err := store.AppendEvent(ctx, runID, "tool_call", json.RawMessage(`{"tool_name":"http_request"}`))
	if err != nil {
		t.Fatalf("append second event: %v", err)
	}

	if firstEvent.Sequence != 1 || secondEvent.Sequence != 2 {
		t.Fatalf("expected event sequences 1 and 2, got %d and %d", firstEvent.Sequence, secondEvent.Sequence)
	}

	if err := store.FinishRun(ctx, runID, "failed", "Blocked by policy"); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopenedStore, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopenedStore.Close()

	run, events, err := reopenedStore.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if run.Status != "failed" {
		t.Fatalf("expected failed status, got %q", run.Status)
	}
	if run.FinishedAt == nil {
		t.Fatal("expected finished_at to be set")
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].EventType != "reasoning" || events[1].EventType != "tool_call" || events[2].EventType != "run_finished" {
		t.Fatalf("unexpected event order: %#v", events)
	}
	if events[2].Sequence != 3 {
		t.Fatalf("expected final event sequence 3, got %d", events[2].Sequence)
	}
}

func TestListRunsReturnsMetadataOnly(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "stamper.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runID, err := store.CreateRun(ctx, "agent-1", "dev", "Test task")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	runs, err := store.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}

	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].RunID != runID {
		t.Fatalf("expected run id %q, got %q", runID, runs[0].RunID)
	}
}

func TestAppendEventRejectsInvalidJSON(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "stamper.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runID, err := store.CreateRun(ctx, "agent-1", "dev", "Test task")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	if _, err := store.AppendEvent(ctx, runID, "reasoning", json.RawMessage("{")); err == nil {
		t.Fatal("expected invalid json error")
	}
}
