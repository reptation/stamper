package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrNotFound           = errors.New("not found")
	ErrRunAlreadyFinished = errors.New("run already finished")
)

var validEventTypes = map[string]struct{}{
	"reasoning":         {},
	"tool_call":         {},
	"policy_decision":   {},
	"execution_blocked": {},
	"execution_result":  {},
	"run_finished":      {},
}

type Store struct {
	db *sql.DB
}

type Run struct {
	RunID       string     `json:"run_id"`
	AgentID     string     `json:"agent_id"`
	Environment string     `json:"environment"`
	Task        string     `json:"task"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

type Event struct {
	ID        int64           `json:"id"`
	RunID     string          `json:"run_id"`
	Sequence  int64           `json:"sequence"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	store := &Store{db: db}
	if err := store.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	return s.db.Close()
}

func (s *Store) init(ctx context.Context) error {
	statements := []string{
		`PRAGMA foreign_keys = ON;`,
		`PRAGMA busy_timeout = 5000;`,
		`CREATE TABLE IF NOT EXISTS runs (
			run_id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			environment TEXT NOT NULL,
			task TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
			started_at TEXT NOT NULL,
			finished_at TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES runs(run_id),
			UNIQUE(run_id, sequence)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_started_at ON runs(started_at);`,
		`CREATE INDEX IF NOT EXISTS idx_events_run_id ON events(run_id);`,
		`CREATE INDEX IF NOT EXISTS idx_events_run_id_sequence ON events(run_id, sequence);`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize sqlite schema: %w", err)
		}
	}

	return nil
}

func (s *Store) CreateRun(ctx context.Context, agentID, environment, task string) (string, error) {
	runID, err := generateRunID()
	if err != nil {
		return "", err
	}

	now := nowString(time.Now().UTC())
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO runs (run_id, agent_id, environment, task, status, started_at) VALUES (?, ?, ?, ?, ?, ?)`,
		runID,
		agentID,
		environment,
		task,
		"running",
		now,
	); err != nil {
		return "", fmt.Errorf("create run: %w", err)
	}

	return runID, nil
}

func (s *Store) AppendEvent(ctx context.Context, runID, eventType string, payload json.RawMessage) (Event, error) {
	if _, ok := validEventTypes[eventType]; !ok {
		return Event{}, fmt.Errorf("%w: unsupported event type %q", ErrInvalidInput, eventType)
	}

	payload, err := normalizePayload(payload)
	if err != nil {
		return Event{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return Event{}, fmt.Errorf("append event: begin transaction: %w", err)
	}
	defer tx.Rollback()

	event, err := appendEventTx(ctx, tx, runID, eventType, payload)
	if err != nil {
		return Event{}, err
	}

	if err := tx.Commit(); err != nil {
		return Event{}, fmt.Errorf("append event: commit transaction: %w", err)
	}

	return event, nil
}

func (s *Store) FinishRun(ctx context.Context, runID, status, outputSummary string) error {
	if status != "completed" && status != "failed" {
		return fmt.Errorf("%w: unsupported status %q", ErrInvalidInput, status)
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("finish run: begin transaction: %w", err)
	}
	defer tx.Rollback()

	finishedAt := nowString(time.Now().UTC())
	result, err := tx.ExecContext(
		ctx,
		`UPDATE runs SET status = ?, finished_at = ? WHERE run_id = ? AND finished_at IS NULL`,
		status,
		finishedAt,
		runID,
	)
	if err != nil {
		return fmt.Errorf("finish run: update run: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("finish run: read update result: %w", err)
	}
	if rowsAffected == 0 {
		var existingStatus string
		err := tx.QueryRowContext(ctx, `SELECT status FROM runs WHERE run_id = ?`, runID).Scan(&existingStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("finish run: check existing run: %w", err)
		}

		return ErrRunAlreadyFinished
	}

	payload, err := normalizePayload(json.RawMessage(fmt.Sprintf(`{"status":%q,"output_summary":%q}`, status, outputSummary)))
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	if _, err := appendEventTx(ctx, tx, runID, "run_finished", payload); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("finish run: commit transaction: %w", err)
	}

	return nil
}

func (s *Store) ListRuns(ctx context.Context) ([]Run, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT run_id, agent_id, environment, task, status, started_at, finished_at FROM runs ORDER BY started_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("list runs: %w", err)
		}
		runs = append(runs, run)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}

	return runs, nil
}

func (s *Store) GetRun(ctx context.Context, runID string) (Run, []Event, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT run_id, agent_id, environment, task, status, started_at, finished_at FROM runs WHERE run_id = ?`,
		runID,
	)

	run, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, nil, ErrNotFound
	}
	if err != nil {
		return Run{}, nil, fmt.Errorf("get run: %w", err)
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, run_id, sequence, event_type, payload_json, created_at FROM events WHERE run_id = ? ORDER BY sequence ASC`,
		runID,
	)
	if err != nil {
		return Run{}, nil, fmt.Errorf("get run events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return Run{}, nil, fmt.Errorf("get run events: %w", err)
		}
		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return Run{}, nil, fmt.Errorf("get run events: %w", err)
	}

	return run, events, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRun(s scanner) (Run, error) {
	var run Run
	var startedAt string
	var finishedAt sql.NullString

	if err := s.Scan(
		&run.RunID,
		&run.AgentID,
		&run.Environment,
		&run.Task,
		&run.Status,
		&startedAt,
		&finishedAt,
	); err != nil {
		return Run{}, err
	}

	parsedStartedAt, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return Run{}, fmt.Errorf("parse started_at: %w", err)
	}
	run.StartedAt = parsedStartedAt

	if finishedAt.Valid {
		parsedFinishedAt, err := time.Parse(time.RFC3339Nano, finishedAt.String)
		if err != nil {
			return Run{}, fmt.Errorf("parse finished_at: %w", err)
		}
		run.FinishedAt = &parsedFinishedAt
	}

	return run, nil
}

func scanEvent(s scanner) (Event, error) {
	var event Event
	var payload string
	var createdAt string

	if err := s.Scan(
		&event.ID,
		&event.RunID,
		&event.Sequence,
		&event.EventType,
		&payload,
		&createdAt,
	); err != nil {
		return Event{}, err
	}

	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Event{}, fmt.Errorf("parse created_at: %w", err)
	}
	event.CreatedAt = parsedCreatedAt
	event.Payload = json.RawMessage(payload)

	return event, nil
}

func appendEventTx(ctx context.Context, tx *sql.Tx, runID, eventType string, payload json.RawMessage) (Event, error) {
	if err := ensureRunExists(ctx, tx, runID); err != nil {
		return Event{}, err
	}

	var sequence int64
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(sequence), 0) + 1 FROM events WHERE run_id = ?`,
		runID,
	).Scan(&sequence); err != nil {
		return Event{}, fmt.Errorf("append event: get next sequence: %w", err)
	}

	createdAt := nowString(time.Now().UTC())
	result, err := tx.ExecContext(
		ctx,
		`INSERT INTO events (run_id, sequence, event_type, payload_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		runID,
		sequence,
		eventType,
		string(payload),
		createdAt,
	)
	if err != nil {
		return Event{}, fmt.Errorf("append event: insert event: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Event{}, fmt.Errorf("append event: read inserted id: %w", err)
	}

	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Event{}, fmt.Errorf("append event: parse created_at: %w", err)
	}

	return Event{
		ID:        id,
		RunID:     runID,
		Sequence:  sequence,
		EventType: eventType,
		Payload:   payload,
		CreatedAt: parsedCreatedAt,
	}, nil
}

func ensureRunExists(ctx context.Context, tx *sql.Tx, runID string) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM runs WHERE run_id = ?`, runID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("append event: check run: %w", err)
	}

	return nil
}

func normalizePayload(payload json.RawMessage) (json.RawMessage, error) {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if !json.Valid(payload) {
		return nil, fmt.Errorf("payload must be valid JSON")
	}

	return payload, nil
}

func generateRunID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate run id: %w", err)
	}

	return "run_" + hex.EncodeToString(b[:]), nil
}

func nowString(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
