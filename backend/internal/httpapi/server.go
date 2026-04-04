package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/reptation/stamper/backend/internal/approval"
	"github.com/reptation/stamper/backend/internal/logging"
	"github.com/reptation/stamper/backend/internal/policy"
	"github.com/reptation/stamper/backend/internal/storage"
)

type Server struct {
	mux     *http.ServeMux
	handler http.Handler

	mu         sync.RWMutex
	bundle     *policy.Bundle
	evaluator  *policy.Evaluator
	tokenStore *approval.Store
	store      RunStore
	logger     *slog.Logger
}

type RunStore interface {
	CreateRun(ctx context.Context, agentID, environment, task string) (string, error)
	AppendEvent(ctx context.Context, runID, eventType string, payload json.RawMessage) (storage.Event, error)
	FinishRun(ctx context.Context, runID, status, outputSummary string) error
	ListRuns(ctx context.Context) ([]storage.Run, error)
	GetRun(ctx context.Context, runID string) (storage.Run, []storage.Event, error)
}

func NewServer(store RunStore, logger *slog.Logger) *Server {
	if logger == nil {
		logger = logging.NewFallback("stamperd")
	}

	s := &Server{
		mux:        http.NewServeMux(),
		store:      store,
		tokenStore: approval.NewStore(60 * time.Second),
		logger:     logger,
	}

	s.routes()
	s.handler = logging.Middleware(s.mux)

	return s
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) SetPolicyBundle(bundle *policy.Bundle) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bundle = bundle
	s.evaluator = nil
	if bundle == nil {
		return
	}

	evaluator, err := policy.NewEvaluator(bundle)
	if err != nil {
		s.logger.With("component", "policy_engine").Error("failed to build policy evaluator",
			"policy_bundle_version", bundle.Version,
			"error", err,
		)
		return
	}

	s.evaluator = evaluator
}

func (s *Server) SetApprovalTokenStore(tokenStore *approval.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if tokenStore == nil {
		s.tokenStore = approval.NewStore(60 * time.Second)
		return
	}

	s.tokenStore = tokenStore
}

func (s *Server) routes() {
	s.mux.HandleFunc("/v1/health", s.handleHealth)
	s.mux.HandleFunc("/v1/ready", s.handleReady)
	s.mux.HandleFunc("/v1/evaluate-action", s.handleEvaluateAction)
	s.mux.HandleFunc("/v1/validate-token", s.handleValidateToken)
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

func (s *Server) handleEvaluateAction(w http.ResponseWriter, r *http.Request) {
	logger := s.requestLogger(r, "policy_engine")
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	evaluator, ok := s.currentEvaluator()
	if !ok {
		logger.Error("policy evaluator unavailable")
		writeError(w, http.StatusServiceUnavailable, "policy evaluator unavailable")
		return
	}

	var request policy.ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logger.Warn("invalid evaluate-action JSON body",
			"error", err,
		)
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := validateActionRequest(request); err != nil {
		logger.Warn("invalid action request",
			"run_id", request.RunID,
			"error", err,
		)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	logger.Debug("evaluating policy",
		"run_id", request.RunID,
		"agent_id", request.Agent.ID,
		"team", request.Agent.Team,
		"environment", request.Environment.Name,
		"action_type", request.Action.Type,
		"tool_name", request.Action.ToolName,
		"action_arguments", request.Action.Arguments,
		"resource", request.Resource,
		"context_fields", request.Context,
	)

	decision, err := evaluator.Evaluate(request)
	if err != nil {
		logger.Error("policy evaluation failed",
			"run_id", request.RunID,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "policy evaluation failed")
		return
	}

	logPolicyDecision(logger, request, decision)

	response := map[string]any{
		"decision":          decision.Decision,
		"policy_id":         decision.PolicyID,
		"policy_name":       decision.PolicyName,
		"rationale":         decision.Rationale,
		"reason":            decision.Rationale,
		"approval_required": decision.ApprovalRequired,
	}

	if decision.Decision == "allow" && request.Action.ToolName == "governed_http_request" {
		tokenStore, ok := s.currentTokenStore()
		if !ok {
			logger.Error("approval token store unavailable",
				"run_id", request.RunID,
			)
			writeError(w, http.StatusServiceUnavailable, "approval token store unavailable")
			return
		}

		method, _ := request.Action.Arguments["method"].(string)
		rawURL, _ := request.Action.Arguments["url"].(string)
		token, err := tokenStore.Issue(method, rawURL)
		if err != nil {
			logger.Warn("approval token issuance rejected",
				"run_id", request.RunID,
				"method", strings.ToUpper(strings.TrimSpace(method)),
				"url", rawURL,
				"error", err,
			)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		response["approval_token"] = token.Value
		response["approval_expires_at"] = token.ExpiresAt.UTC().Format(time.RFC3339)
		logger.Debug("approval token issued",
			"run_id", request.RunID,
			"method", token.Method,
			"host", token.Host,
			"expires_at", token.ExpiresAt.UTC().Format(time.RFC3339),
		)
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleValidateToken(w http.ResponseWriter, r *http.Request) {
	logger := s.requestLogger(r, "http_handler")
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	tokenStore, ok := s.currentTokenStore()
	if !ok {
		logger.Error("approval token store unavailable")
		writeError(w, http.StatusServiceUnavailable, "approval token store unavailable")
		return
	}

	var request struct {
		ApprovalToken string `json:"approval_token"`
		Method        string `json:"method"`
		URL           string `json:"url"`
		RunID         string `json:"run_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logger.Warn("invalid validate-token JSON body",
			"error", err,
		)
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.ApprovalToken == "" || request.Method == "" || request.URL == "" {
		logger.Warn("invalid validate-token request",
			"run_id", request.RunID,
		)
		writeError(w, http.StatusBadRequest, "approval_token, method, and url are required")
		return
	}

	token, err := tokenStore.Validate(request.ApprovalToken, request.Method, request.URL)
	if err != nil {
		method := strings.ToUpper(strings.TrimSpace(request.Method))
		switch {
		case errors.Is(err, approval.ErrInvalidRequest):
			logger.Warn("approval token validation request rejected",
				"run_id", request.RunID,
				"method", method,
				"url", request.URL,
				"error", err,
			)
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, approval.ErrInvalidToken),
			errors.Is(err, approval.ErrExpiredToken),
			errors.Is(err, approval.ErrMethodMismatch),
			errors.Is(err, approval.ErrHostMismatch):
			logger.Error("approval token rejected",
				"run_id", request.RunID,
				"method", method,
				"url", request.URL,
				"decision", "deny",
				"error", err,
			)
			writeError(w, http.StatusForbidden, err.Error())
		default:
			logger.Error("approval token validation failed",
				"run_id", request.RunID,
				"method", method,
				"url", request.URL,
				"error", err,
			)
			writeError(w, http.StatusInternalServerError, "token validation failed")
		}
		return
	}

	logger.Info("approval token validated",
		"run_id", request.RunID,
		"method", token.Method,
		"host", token.Host,
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"valid":      true,
		"method":     token.Method,
		"host":       token.Host,
		"expires_at": token.ExpiresAt.UTC().Format(time.RFC3339),
	})
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
	logger := s.requestLogger(r, "http_handler")
	if s.store == nil {
		logger.Error("storage unavailable")
		writeError(w, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	var request struct {
		AgentID     string `json:"agent_id"`
		Environment string `json:"environment"`
		Task        string `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logger.Warn("invalid create-run JSON body",
			"error", err,
		)
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.AgentID == "" || request.Environment == "" || request.Task == "" {
		logger.Warn("invalid create-run request")
		writeError(w, http.StatusBadRequest, "agent_id, environment, and task are required")
		return
	}

	runID, err := s.store.CreateRun(r.Context(), request.AgentID, request.Environment, request.Task)
	if err != nil {
		logger.Error("failed to create run",
			"agent_id", request.AgentID,
			"environment", request.Environment,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Info("run created",
		"run_id", runID,
		"agent_id", request.AgentID,
		"environment", request.Environment,
		"task", request.Task,
	)

	writeJSON(w, http.StatusCreated, map[string]string{
		"run_id": runID,
	})
}

func (s *Server) handleAppendEvent(w http.ResponseWriter, r *http.Request, runID string) {
	logger := s.requestLogger(r, "http_handler")
	if s.store == nil {
		logger.Error("storage unavailable",
			"run_id", runID,
		)
		writeError(w, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	var request struct {
		EventType string          `json:"event_type"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logger.Warn("invalid append-event JSON body",
			"run_id", runID,
			"error", err,
		)
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.EventType == "" || len(request.Payload) == 0 {
		logger.Warn("invalid append-event request",
			"run_id", runID,
		)
		writeError(w, http.StatusBadRequest, "event_type and payload are required")
		return
	}

	event, err := s.store.AppendEvent(r.Context(), runID, request.EventType, request.Payload)
	if err != nil {
		s.writeStoreError(w, logger, err, "run_id", runID, "event_type", request.EventType)
		return
	}

	logger.Info("event appended",
		"run_id", event.RunID,
		"event_id", event.ID,
		"event_type", event.EventType,
		"sequence", event.Sequence,
	)
	logger.Debug("event payload appended",
		"run_id", event.RunID,
		"event_id", event.ID,
		"payload", json.RawMessage(event.Payload),
	)

	writeJSON(w, http.StatusCreated, event)
}

func (s *Server) handleFinishRun(w http.ResponseWriter, r *http.Request, runID string) {
	logger := s.requestLogger(r, "http_handler")
	if s.store == nil {
		logger.Error("storage unavailable",
			"run_id", runID,
		)
		writeError(w, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	var request struct {
		Status        string `json:"status"`
		OutputSummary string `json:"output_summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logger.Warn("invalid finish-run JSON body",
			"run_id", runID,
			"error", err,
		)
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.Status == "" || request.OutputSummary == "" {
		logger.Warn("invalid finish-run request",
			"run_id", runID,
		)
		writeError(w, http.StatusBadRequest, "status and output_summary are required")
		return
	}

	if err := s.store.FinishRun(r.Context(), runID, request.Status, request.OutputSummary); err != nil {
		s.writeStoreError(w, logger, err, "run_id", runID, "status", request.Status)
		return
	}

	logger.Info("run finished",
		"run_id", runID,
		"status", request.Status,
		"output_summary", request.OutputSummary,
	)

	writeJSON(w, http.StatusOK, map[string]bool{
		"ok": true,
	})
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	logger := s.requestLogger(r, "http_handler")
	if s.store == nil {
		logger.Error("storage unavailable")
		writeError(w, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	runs, err := s.store.ListRuns(r.Context())
	if err != nil {
		logger.Error("failed to list runs",
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Debug("runs listed",
		"count", len(runs),
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"runs": runs,
	})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request, runID string) {
	logger := s.requestLogger(r, "http_handler")
	if s.store == nil {
		logger.Error("storage unavailable",
			"run_id", runID,
		)
		writeError(w, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	run, events, err := s.store.GetRun(r.Context(), runID)
	if err != nil {
		s.writeStoreError(w, logger, err, "run_id", runID)
		return
	}

	logger.Debug("run loaded",
		"run_id", runID,
		"event_count", len(events),
		"status", run.Status,
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"run":    run,
		"events": events,
	})
}

func (s *Server) currentEvaluator() (*policy.Evaluator, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.evaluator == nil {
		return nil, false
	}

	return s.evaluator, true
}

func (s *Server) currentTokenStore() (*approval.Store, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.tokenStore == nil {
		return nil, false
	}

	return s.tokenStore, true
}

func validateActionRequest(request policy.ActionRequest) error {
	switch {
	case request.RunID == "":
		return errors.New("run_id is required")
	case request.Agent.ID == "":
		return errors.New("agent.id is required")
	case request.Environment.Name == "":
		return errors.New("environment.name is required")
	case request.Action.Type == "":
		return errors.New("action.type is required")
	case request.Action.ToolName == "":
		return errors.New("action.tool_name is required")
	default:
		if request.Action.ToolName != "governed_http_request" {
			return nil
		}

		method, ok := request.Action.Arguments["method"].(string)
		if !ok || strings.TrimSpace(method) == "" {
			return errors.New("action.arguments.method is required for governed_http_request")
		}
		rawURL, ok := request.Action.Arguments["url"].(string)
		if !ok || strings.TrimSpace(rawURL) == "" {
			return errors.New("action.arguments.url is required for governed_http_request")
		}
		if _, _, err := approval.NormalizeMethodAndHost(method, rawURL); err != nil {
			return fmt.Errorf("action.arguments invalid: %w", err)
		}
		return nil
	}
}

func (s *Server) writeStoreError(w http.ResponseWriter, logger *slog.Logger, err error, attrs ...any) {
	switch {
	case errors.Is(err, storage.ErrInvalidInput):
		logger.Warn("storage rejected request", append(attrs, "error", err)...)
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, storage.ErrNotFound):
		logger.Warn("run not found", attrs...)
		writeError(w, http.StatusNotFound, "run not found")
	case errors.Is(err, storage.ErrRunAlreadyFinished):
		logger.Warn("run already finished", attrs...)
		writeError(w, http.StatusConflict, "run already finished")
	default:
		logger.Error("storage operation failed", append(attrs, "error", err)...)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (s *Server) requestLogger(r *http.Request, component string) *slog.Logger {
	if s.logger == nil {
		return nil
	}

	return logging.WithContext(s.logger.With("component", component), r.Context())
}

func logPolicyDecision(logger *slog.Logger, request policy.ActionRequest, decision policy.Decision) {
	attrs := []any{
		"run_id", request.RunID,
		"agent_id", request.Agent.ID,
		"team", request.Agent.Team,
		"environment", request.Environment.Name,
		"action_type", request.Action.Type,
		"tool_name", request.Action.ToolName,
		"decision", decision.Decision,
		"policy_id", decision.PolicyID,
		"policy_name", decision.PolicyName,
		"approval_required", decision.ApprovalRequired,
	}

	switch decision.Decision {
	case "deny":
		logger.Error("action denied by policy", attrs...)
	case "require_approval":
		logger.Warn("action requires approval", attrs...)
	default:
		logger.Info("policy evaluated", attrs...)
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
