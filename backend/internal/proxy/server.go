package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/reptation/stamper/backend/internal/logging"
)

const (
	DefaultRequestTimeout = 5 * time.Second
	MaxResponseBodyBytes  = 100 * 1024
)

var sensitiveHeaders = map[string]struct{}{
	"authorization":       {},
	"cookie":              {},
	"set-cookie":          {},
	"proxy-authorization": {},
}

type Server struct {
	mux            *http.ServeMux
	handler        http.Handler
	stamperBaseURL string
	client         *http.Client
	logger         *slog.Logger
}

func NewServer(stamperBaseURL string, client *http.Client, logger *slog.Logger) *Server {
	if client == nil {
		client = &http.Client{}
	}
	if logger == nil {
		logger = logging.NewFallback("stamper-proxy")
	}

	s := &Server{
		mux:            http.NewServeMux(),
		stamperBaseURL: strings.TrimRight(stamperBaseURL, "/"),
		client:         client,
		logger:         logger,
	}
	s.routes()
	s.handler = logging.Middleware(s.mux)
	return s
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/request", s.handleRequest)
}

type requestPayload struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	RunID     string            `json:"run_id,omitempty"`
	Headers   map[string]string `json:"headers"`
	Body      any               `json:"body"`
	TimeoutMS int               `json:"timeout_ms"`
}

type validateTokenRequest struct {
	ApprovalToken string `json:"approval_token"`
	Method        string `json:"method"`
	URL           string `json:"url"`
	RunID         string `json:"run_id,omitempty"`
}

type proxyResponse struct {
	Status        string            `json:"status"`
	StatusCode    int               `json:"status_code"`
	Headers       map[string]string `json:"headers"`
	Body          string            `json:"body"`
	BodyTruncated bool              `json:"body_truncated"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	logger := s.requestLogger(r, "proxy")
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	approvalToken := strings.TrimSpace(r.Header.Get("X-Stamper-Token"))
	if approvalToken == "" {
		logger.Error("request blocked: missing approval token",
			"decision", "deny",
		)
		writeError(w, http.StatusForbidden, "missing X-Stamper-Token header")
		return
	}

	var payload requestPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		logger.Warn("invalid proxy request JSON body",
			"error", err,
		)
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	parsedURL, err := validateRequestPayload(payload)
	if err != nil {
		logger.Warn("invalid proxy request",
			"run_id", payload.RunID,
			"error", err,
		)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	method := strings.ToUpper(strings.TrimSpace(payload.Method))
	host := loggedHost(parsedURL)
	path := loggedPath(parsedURL)
	sanitizedHeaders := redactStringHeaders(payload.Headers)

	logger.Info("incoming governed request",
		"run_id", payload.RunID,
		"method", method,
		"host", host,
		"path", path,
		"headers", sanitizedHeaders,
	)
	logger.Debug("proxy request payload",
		"run_id", payload.RunID,
		"method", method,
		"host", host,
		"path", path,
		"timeout_ms", payload.TimeoutMS,
	)

	if err := s.validateApprovalToken(r.Context(), approvalToken, method, payload.URL, payload.RunID); err != nil {
		switch err.(type) {
		case errTokenRejected:
			logger.Error("request blocked by policy",
				"run_id", payload.RunID,
				"method", method,
				"host", host,
				"path", path,
				"decision", "deny",
				"error", err,
			)
			writeError(w, http.StatusForbidden, err.Error())
		case errBadValidationRequest:
			logger.Warn("proxy validation request rejected",
				"run_id", payload.RunID,
				"method", method,
				"host", host,
				"path", path,
				"error", err,
			)
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			logger.Error("token validation failed",
				"run_id", payload.RunID,
				"method", method,
				"host", host,
				"path", path,
				"error", err,
			)
			writeError(w, http.StatusBadGateway, err.Error())
		}
		return
	}

	logger.Info("approval token validated",
		"run_id", payload.RunID,
		"method", method,
		"host", host,
	)
	logger.Info("request forwarded upstream",
		"run_id", payload.RunID,
		"method", method,
		"host", host,
		"path", path,
		"decision", "allow",
	)

	started := time.Now()
	result, err := s.forwardRequest(r.Context(), payload)
	if err != nil {
		logger.Error("upstream request failed",
			"run_id", payload.RunID,
			"method", method,
			"host", host,
			"path", path,
			"latency_ms", time.Since(started).Milliseconds(),
			"error", err,
		)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	logger.Info("upstream response received",
		"run_id", payload.RunID,
		"method", method,
		"host", host,
		"path", path,
		"status_code", result.StatusCode,
		"latency_ms", time.Since(started).Milliseconds(),
	)

	writeJSON(w, http.StatusOK, result)
}

func validateRequestPayload(payload requestPayload) (*url.URL, error) {
	method := strings.ToUpper(strings.TrimSpace(payload.Method))
	if method == "" {
		return nil, fmt.Errorf("method is required")
	}

	parsed, err := url.Parse(strings.TrimSpace(payload.URL))
	if err != nil {
		return nil, fmt.Errorf("url is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("url must use http or https")
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("url host is required")
	}

	if payload.TimeoutMS < 0 {
		return nil, fmt.Errorf("timeout_ms must be greater than or equal to 0")
	}

	return parsed, nil
}

type errTokenRejected struct{ message string }

func (e errTokenRejected) Error() string { return e.message }

type errBadValidationRequest struct{ message string }

func (e errBadValidationRequest) Error() string { return e.message }

func (s *Server) validateApprovalToken(ctx context.Context, approvalToken, method, rawURL, runID string) error {
	requestBody, err := json.Marshal(validateTokenRequest{
		ApprovalToken: approvalToken,
		Method:        method,
		URL:           rawURL,
		RunID:         runID,
	})
	if err != nil {
		return fmt.Errorf("marshal validation request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.stamperBaseURL+"/v1/validate-token",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		return fmt.Errorf("build validation request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	logging.PropagateRequestID(req, ctx)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("validate token with stamper: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	message := extractErrorMessage(resp.Body)
	if message == "" {
		message = fmt.Sprintf("stamper validation returned HTTP %d", resp.StatusCode)
	}

	switch resp.StatusCode {
	case http.StatusForbidden:
		return errTokenRejected{message: message}
	case http.StatusBadRequest:
		return errBadValidationRequest{message: message}
	default:
		return fmt.Errorf("stamper validation failed: %s", message)
	}
}

func (s *Server) forwardRequest(ctx context.Context, payload requestPayload) (proxyResponse, error) {
	timeout := DefaultRequestTimeout
	if payload.TimeoutMS > 0 {
		timeout = time.Duration(payload.TimeoutMS) * time.Millisecond
	}

	requestBody, err := buildOutboundBody(payload.Body)
	if err != nil {
		return proxyResponse{}, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(
		reqCtx,
		strings.ToUpper(strings.TrimSpace(payload.Method)),
		payload.URL,
		requestBody,
	)
	if err != nil {
		return proxyResponse{}, fmt.Errorf("build outbound request: %w", err)
	}

	for key, value := range payload.Headers {
		req.Header.Set(key, value)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return proxyResponse{}, fmt.Errorf("forward request: %w", err)
	}
	defer resp.Body.Close()

	body, truncated, err := readResponseBody(resp.Body)
	if err != nil {
		return proxyResponse{}, fmt.Errorf("read upstream response: %w", err)
	}

	return proxyResponse{
		Status:        "success",
		StatusCode:    resp.StatusCode,
		Headers:       redactHeaders(resp.Header),
		Body:          strings.ToValidUTF8(string(body), "\uFFFD"),
		BodyTruncated: truncated,
	}, nil
}

func buildOutboundBody(body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}
	if text, ok := body.(string); ok {
		return strings.NewReader(text), nil
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request body: %w", err)
	}
	return bytes.NewReader(encoded), nil
}

func readResponseBody(body io.Reader) ([]byte, bool, error) {
	limited, err := io.ReadAll(io.LimitReader(body, MaxResponseBodyBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(limited) > MaxResponseBodyBytes {
		return limited[:MaxResponseBodyBytes], true, nil
	}
	return limited, false, nil
}

func redactHeaders(headers http.Header) map[string]string {
	result := make(map[string]string, len(headers))
	for key, values := range headers {
		if _, ok := sensitiveHeaders[strings.ToLower(key)]; ok {
			result[key] = "[REDACTED]"
			continue
		}
		result[key] = strings.Join(values, ", ")
	}
	return result
}

func redactStringHeaders(headers map[string]string) map[string]string {
	result := make(map[string]string, len(headers))
	for key, value := range headers {
		if _, ok := sensitiveHeaders[strings.ToLower(key)]; ok {
			result[key] = "[REDACTED]"
			continue
		}
		result[key] = value
	}
	return result
}

func loggedHost(parsedURL *url.URL) string {
	if parsedURL == nil {
		return ""
	}
	if hostname := parsedURL.Hostname(); hostname != "" {
		return hostname
	}
	return parsedURL.Host
}

func loggedPath(parsedURL *url.URL) string {
	if parsedURL == nil {
		return ""
	}
	if path := parsedURL.EscapedPath(); path != "" {
		return path
	}
	return "/"
}

func (s *Server) requestLogger(r *http.Request, component string) *slog.Logger {
	if s.logger == nil {
		return nil
	}

	return logging.WithContext(s.logger.With("component", component), r.Context())
}

func extractErrorMessage(body io.Reader) string {
	var payload map[string]string
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload["error"])
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeMethodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}
