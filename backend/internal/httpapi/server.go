package httpapi

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/reptation/stamper/backend/internal/policy"
)

type Server struct {
	mux *http.ServeMux

	mu     sync.RWMutex
	bundle *policy.Bundle
}

func NewServer() *Server {
	s := &Server{
		mux: http.NewServeMux(),
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
