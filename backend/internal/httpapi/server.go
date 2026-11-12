package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/said-go/vibe-check/backend/internal/analysis"
	"github.com/said-go/vibe-check/backend/internal/scoring"
)

type Server struct {
	logger          *slog.Logger
	engine          *scoring.Engine
	maxRequestBytes int64
}

func New(logger *slog.Logger, maxRequestBytes int64) http.Handler {
	server := &Server{logger: logger, engine: scoring.NewEngine(), maxRequestBytes: maxRequestBytes}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /readyz", server.ready)
	mux.HandleFunc("POST /v1/scoring/evaluate", server.evaluate)
	return server.recoverPanic(server.requestID(server.accessLog(mux)))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (s *Server) ready(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) evaluate(w http.ResponseWriter, r *http.Request) {
	if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		s.writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json", false)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.maxRequestBytes)
	features, err := analysis.DecodeFeatures(r.Body)
	if err != nil {
		s.writeError(w, r, http.StatusUnprocessableEntity, "invalid_ai_contract", "AI feature contract is invalid", false)
		return
	}
	result, err := s.engine.Evaluate(features)
	if err != nil {
		s.writeError(w, r, http.StatusUnprocessableEntity, "scoring_failed", "Features cannot be evaluated", false)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Retryable bool   `json:"retryable"`
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, retryable bool) {
	writeJSONStatus(w, status, apiError{Code: code, Message: message, RequestID: requestIDFrom(r), Retryable: retryable})
}
func writeJSON(w http.ResponseWriter, status int, value any) { writeJSONStatus(w, status, value) }
func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type contextKey string

const requestIDKey contextKey = "request_id"

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(withRequestID(r.Context(), id)))
	})
}
func validRequestID(value string) bool {
	if len(value) < 8 || len(value) > 80 {
		return false
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}
func newRequestID() string {
	return strings.ReplaceAll(time.Now().UTC().Format("20060102T150405.000000000"), ".", "")
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("http_request", "method", r.Method, "path", r.URL.Path, "request_id", requestIDFrom(r), "duration_ms", time.Since(started).Milliseconds())
	})
}
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic", "request_id", requestIDFrom(r), "error", recovered, "stack", string(debug.Stack()))
				s.writeError(w, r, http.StatusInternalServerError, "internal_error", "Unexpected server error", true)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
