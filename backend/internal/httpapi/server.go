package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/reptation/stamper/backend/internal/policy"
	"github.com/reptation/stamper/backend/internal/storage"
)

type Server struct {
	mux *http.ServeMux

	mu     sync.RWMutex
	bundle *policy.Bundle
	store  RunStore
}

type RunStore interface {
	CreateRun(ctx context.Context, agentID, environment, task string) (string, error)
	AppendEvent(ctx context.Context, runID, eventType string, payload json.RawMessage) (storage.Event, error)
	FinishRun(ctx context.Context, runID, status, outputSummary string) error
	ListRuns(ctx context.Context) ([]storage.Run, error)
	GetRun(ctx context.Context, runID string) (storage.Run, []storage.Event, error)
}

func NewServer(store RunStore) *Server {
	s := &Server{
		mux:   http.NewServeMux(),
		store: store,
	}

	s.routes()

	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) SetPolicyBundle(bundle *policy.Bundle) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bundle = bundle
}

func (s *Server) routes() {
	s.mux.HandleFunc("/v1/health", s.handleHealth)
	s.mux.HandleFunc("/v1/ready", s.handleReady)
	s.mux.HandleFunc("/v1/runs", s.handleRuns)
	s.mux.HandleFunc("/v1/runs/", s.handleRunByID)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	bundle := s.bundle
	s.mu.RUnlock()

	response := map[string]any{
		"ready": bundle != nil,
	}
	if bundle != nil {
		response["policy_bundle_version"] = bundle.Version
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleCreateRun(w, r)
	case http.MethodGet:
		s.handleListRuns(w, r)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleRunByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/runs/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	runID := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		s.handleGetRun(w, r, runID)
		return
	}

	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}

	switch parts[1] {
	case "events":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		s.handleAppendEvent(w, r, runID)
	case "finish":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		s.handleFinishRun(w, r, runID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	var request struct {
		AgentID     string `json:"agent_id"`
		Environment string `json:"environment"`
		Task        string `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.AgentID == "" || request.Environment == "" || request.Task == "" {
		writeError(w, http.StatusBadRequest, "agent_id, environment, and task are required")
		return
	}

	runID, err := s.store.CreateRun(r.Context(), request.AgentID, request.Environment, request.Task)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"run_id": runID,
	})
}

func (s *Server) handleAppendEvent(w http.ResponseWriter, r *http.Request, runID string) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	var request struct {
		EventType string          `json:"event_type"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.EventType == "" || len(request.Payload) == 0 {
		writeError(w, http.StatusBadRequest, "event_type and payload are required")
		return
	}

	event, err := s.store.AppendEvent(r.Context(), runID, request.EventType, request.Payload)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, event)
}

func (s *Server) handleFinishRun(w http.ResponseWriter, r *http.Request, runID string) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	var request struct {
		Status        string `json:"status"`
		OutputSummary string `json:"output_summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.Status == "" || request.OutputSummary == "" {
		writeError(w, http.StatusBadRequest, "status and output_summary are required")
		return
	}

	if err := s.store.FinishRun(r.Context(), runID, request.Status, request.OutputSummary); err != nil {
		s.writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{
		"ok": true,
	})
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	runs, err := s.store.ListRuns(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"runs": runs,
	})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request, runID string) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	run, events, err := s.store.GetRun(r.Context(), runID)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"run":    run,
		"events": events,
	})
}

func (s *Server) writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "run not found")
	case errors.Is(err, storage.ErrRunAlreadyFinished):
		writeError(w, http.StatusConflict, "run already finished")
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}

func writeMethodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}
