# SQLite Schema

Use two tables and start with a trigger-free approach.

## Schema

### `runs`

```sql
CREATE TABLE IF NOT EXISTS runs (
    run_id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    environment TEXT NOT NULL,
    task TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at DATETIME NULL
);
```

### `events`

```sql
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (run_id) REFERENCES runs(run_id) ON DELETE CASCADE,
    UNIQUE (run_id, sequence)
);
```

### Indexes

```sql
CREATE INDEX IF NOT EXISTS idx_events_run_id ON events(run_id);
CREATE INDEX IF NOT EXISTS idx_events_run_id_sequence ON events(run_id, sequence);
CREATE INDEX IF NOT EXISTS idx_runs_started_at ON runs(started_at);
```

## Why This Schema

A few deliberate choices:

### `run_id TEXT PRIMARY KEY`

Do not use an integer run ID externally. Use a stable string like:

```text
run_8f4e3d...
```

That is better for APIs, logs, and UI.

### `status CHECK (...)`

Keep the MVP status set tight:

- `running`
- `completed`
- `failed`

Add `cancelled` later if needed.

### `payload_json TEXT`

SQLite's JSON support is nice, but for an MVP a `TEXT` column containing JSON is enough. Marshal and unmarshal in Go.

### `UNIQUE (run_id, sequence)`

This is the important invariant. It guarantees deterministic per-run ordering.

## Event Types For MVP

Hardcode validation around a small set of event types:

```go
var ValidEventTypes = map[string]struct{}{
	"reasoning":         {},
	"tool_call":         {},
	"policy_decision":   {},
	"execution_blocked": {},
	"execution_result":  {},
	"run_finished":      {},
}
```

Do not let Codex invent 14 event types right now.

## Go Domain Models

Put these in [backend/internal/runs/model.go](/home/david/projects/stamper/backend/internal/runs/model.go).

### `Run`

```go
package runs

import "time"

type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

type Run struct {
	RunID       string     `json:"run_id"`
	AgentID     string     `json:"agent_id"`
	Environment string     `json:"environment"`
	Task        string     `json:"task"`
	Status      Status     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}
```

### `Event`

```go
package runs

import (
	"encoding/json"
	"time"
)

type Event struct {
	ID        int64           `json:"id"`
	RunID     string          `json:"run_id"`
	Sequence  int             `json:"sequence"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}
```

## Storage Interface

Keep it narrow and explicit.

Put it in [backend/internal/runs/store.go](/home/david/projects/stamper/backend/internal/runs/store.go).

```go
package runs

import "context"

type Store interface {
	CreateRun(ctx context.Context, agentID, environment, task string) (*Run, error)
	AppendEvent(ctx context.Context, runID, eventType string, payload []byte) (*Event, error)
	FinishRun(ctx context.Context, runID string, status Status, outputSummary string) error
	ListRuns(ctx context.Context) ([]Run, error)
	GetRun(ctx context.Context, runID string) (*Run, []Event, error)
}
```

## Why This Interface Is Right

### `CreateRun(...) (*Run, error)`

Return the created run, not just the ID. The API layer and mock agent will want the whole object.

### `AppendEvent(...) (*Event, error)`

Return the stored event with the assigned sequence. That is useful for debugging and tests.

### `FinishRun(...)`

This should:

- update the `runs` row
- append the `run_finished` event

One call, one invariant.

### `GetRun(...) (*Run, []Event, error)`

Simple and obvious.

## SQLite Implementation Shape

Put it in [backend/internal/runs/sqlite_store.go](/home/david/projects/stamper/backend/internal/runs/sqlite_store.go).

### Struct

```go
package runs

import "database/sql"

type SQLiteStore struct {
	db *sql.DB
}
```

### Constructor

```go
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}
```

## Exact Behavior For Each Method

### `CreateRun`

Responsibilities:

- generate a run ID
- insert the row
- return the run

```go
func (s *SQLiteStore) CreateRun(ctx context.Context, agentID, environment, task string) (*Run, error) {
	runID := newRunID()

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO runs (run_id, agent_id, environment, task, status)
		 VALUES (?, ?, ?, ?, ?)`,
		runID, agentID, environment, task, StatusRunning,
	)
	if err != nil {
		return nil, err
	}

	return s.getRunRow(ctx, runID)
}
```

### `AppendEvent`

This is the trickiest method because of sequencing.

The sequence should be:

- per-run
- monotonic
- safe under concurrent writes

For MVP, the simplest safe way is a transaction.

```go
func (s *SQLiteStore) AppendEvent(ctx context.Context, runID, eventType string, payload []byte) (*Event, error) {
	if _, ok := ValidEventTypes[eventType]; !ok {
		return nil, ErrInvalidEventType
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var nextSeq int
	err = tx.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(sequence), 0) + 1 FROM events WHERE run_id = ?`,
		runID,
	).Scan(&nextSeq)
	if err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO events (run_id, sequence, event_type, payload_json)
		 VALUES (?, ?, ?, ?)`,
		runID, nextSeq, eventType, string(payload),
	)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s.getEventByID(ctx, id)
}
```

That is good enough for MVP.

### `FinishRun`

This should be transactional too.

Responsibilities:

- validate status
- set run status and `finished_at`
- append a `run_finished` event with a summary

Payload shape:

```json
{
  "status": "failed",
  "output_summary": "Blocked by policy"
}
```

```go
func (s *SQLiteStore) FinishRun(ctx context.Context, runID string, status Status, outputSummary string) error {
	if status != StatusCompleted && status != StatusFailed {
		return ErrInvalidStatus
	}

	payload := map[string]any{
		"status":         status,
		"output_summary": outputSummary,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(
		ctx,
		`UPDATE runs
		 SET status = ?, finished_at = CURRENT_TIMESTAMP
		 WHERE run_id = ?`,
		status, runID,
	)
	if err != nil {
		return err
	}

	var nextSeq int
	err = tx.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(sequence), 0) + 1 FROM events WHERE run_id = ?`,
		runID,
	).Scan(&nextSeq)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO events (run_id, sequence, event_type, payload_json)
		 VALUES (?, ?, ?, ?)`,
		runID, nextSeq, "run_finished", string(raw),
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}
```

### `ListRuns`

Keep it simple.

```go
func (s *SQLiteStore) ListRuns(ctx context.Context) ([]Run, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT run_id, agent_id, environment, task, status, started_at, finished_at
		 FROM runs
		 ORDER BY started_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(
			&r.RunID, &r.AgentID, &r.Environment, &r.Task, &r.Status, &r.StartedAt, &r.FinishedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

### `GetRun`

```go
func (s *SQLiteStore) GetRun(ctx context.Context, runID string) (*Run, []Event, error) {
	run, err := s.getRunRow(ctx, runID)
	if err != nil {
		return nil, nil, err
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, run_id, sequence, event_type, payload_json, created_at
		 FROM events
		 WHERE run_id = ?
		 ORDER BY sequence ASC`,
		runID,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(
			&e.ID, &e.RunID, &e.Sequence, &e.EventType, &e.Payload, &e.CreatedAt,
		); err != nil {
			return nil, nil, err
		}
		events = append(events, e)
	}

	return run, events, rows.Err()
}
```

## Helper Methods

Add these explicitly:

```go
func (s *SQLiteStore) getRunRow(ctx context.Context, runID string) (*Run, error)
func (s *SQLiteStore) getEventByID(ctx context.Context, id int64) (*Event, error)
```

Do not duplicate row-scanning logic.

## Error Values

Define these once:

```go
package runs

import "errors"

var (
	ErrRunNotFound      = errors.New("run not found")
	ErrInvalidEventType = errors.New("invalid event type")
	ErrInvalidStatus    = errors.New("invalid run status")
)
```

That gives API handlers something clean to map to HTTP codes later.

## Run ID Generation

Do not let Codex pick UUID-without-prefix or random nonsense inconsistently.

Use one helper:

```go
func newRunID() string {
	return "run_" + hexToken(16)
}
```

Implement `hexToken` with `crypto/rand`.

Use the same pattern later for approvals:

```text
apr_...
```

## Initialization Function

Lock down schema initialization too.

Put it in [backend/internal/runs/schema.go](/home/david/projects/stamper/backend/internal/runs/schema.go).

```go
func InitSchema(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS runs (
			run_id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			environment TEXT NOT NULL,
			task TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
			started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			finished_at DATETIME NULL
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (run_id) REFERENCES runs(run_id) ON DELETE CASCADE,
			UNIQUE (run_id, sequence)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_run_id ON events(run_id)`,
		`CREATE INDEX IF NOT EXISTS idx_events_run_id_sequence ON events(run_id, sequence)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_started_at ON runs(started_at)`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
```
