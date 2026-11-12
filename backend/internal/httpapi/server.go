package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/said-go/vibe-check/backend/internal/imageproc"
	"github.com/said-go/vibe-check/backend/internal/scoring"
	"github.com/said-go/vibe-check/backend/internal/vision"
)

type Server struct {
	logger          *slog.Logger
	engine          *scoring.Engine
	vision          vision.Provider
	images          imageproc.Processor
	maxRequestBytes int64
	configured      bool
	limiter         *requestLimiter
	idempotency     *idempotencyCache
}

func New(logger *slog.Logger, maxRequestBytes int64, provider vision.Provider) http.Handler {
	return NewWithLimits(logger, maxRequestBytes, provider, DefaultLimits)
}

func NewWithLimits(logger *slog.Logger, maxRequestBytes int64, provider vision.Provider, limits Limits) http.Handler {
	if provider == nil {
		provider = vision.Unconfigured{}
	}
	configured := true
	switch provider.(type) {
	case vision.Unconfigured, *vision.Unconfigured:
		configured = false
	}
	server := &Server{logger: logger, engine: scoring.NewEngine(), vision: provider, images: imageproc.New(), maxRequestBytes: maxRequestBytes, configured: configured, limiter: newRequestLimiter(limits), idempotency: newIdempotencyCache()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /readyz", server.ready)
	mux.HandleFunc("POST /v1/analyses", server.analyzeImage)
	return server.recoverPanic(server.requestID(server.accessLog(mux)))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (s *Server) ready(w http.ResponseWriter, _ *http.Request) {
	if !s.configured {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "ai_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

type analysisResponse struct {
	AnalysisID string         `json:"analysis_id"`
	Status     string         `json:"status"`
	Result     scoring.Result `json:"result"`
	Privacy    struct {
		ServerImageStored bool `json:"server_image_stored"`
	} `json:"privacy"`
}

func (s *Server) analyzeImage(w http.ResponseWriter, r *http.Request) {
	mediaType, _, typeErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if typeErr != nil || mediaType != "multipart/form-data" {
		s.writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be multipart/form-data", false)
		return
	}
	idempotencyValues := r.Header.Values("Idempotency-Key")
	if len(idempotencyValues) > 1 || len(idempotencyValues) == 1 && !validIdempotencyKey(idempotencyValues[0]) {
		s.writeError(w, r, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key must be 16 to 80 letters, digits, hyphens or underscores", false)
		return
	}
	knownRetry := len(idempotencyValues) == 1 && s.idempotency.contains(idempotencyValues[0])
	reservation, retryAfter := s.limiter.acquire(r, knownRetry)
	if reservation == nil {
		w.Header().Set("Retry-After", retryAfterHeader(retryAfter))
		s.writeError(w, r, http.StatusTooManyRequests, "analysis_rate_limited", "Too many analysis requests", true)
		return
	}
	defer reservation.release()
	r.Body = http.MaxBytesReader(w, r.Body, s.maxRequestBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_multipart", "Multipart body is invalid", false)
		return
	}
	var sanitized imageproc.SanitizedImage
	found := false
	consented := false
	consentSeen := false
	for {
		part, partErr := reader.NextPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		if isBodyTooLarge(partErr) {
			s.writeError(w, r, http.StatusRequestEntityTooLarge, "image_too_large", "Image is too large", false)
			return
		}
		if partErr != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_multipart", "Multipart body is invalid", false)
			return
		}
		if part.FormName() == "privacy_consent" {
			if consentSeen {
				_ = part.Close()
				s.writeError(w, r, http.StatusBadRequest, "duplicate_consent", "Only one privacy_consent field is allowed", false)
				return
			}
			consentSeen = true
			value, readErr := io.ReadAll(io.LimitReader(part, 17))
			_ = part.Close()
			if isBodyTooLarge(readErr) {
				s.writeError(w, r, http.StatusRequestEntityTooLarge, "image_too_large", "Image is too large", false)
				return
			}
			if readErr != nil || len(value) > 16 {
				s.writeError(w, r, http.StatusBadRequest, "invalid_consent", "Privacy consent is invalid", false)
				return
			}
			consented = strings.EqualFold(strings.TrimSpace(string(value)), "true")
			continue
		}
		if part.FormName() != "image" {
			_ = part.Close()
			continue
		}
		if found {
			_ = part.Close()
			s.writeError(w, r, http.StatusBadRequest, "multiple_images", "Only one image is allowed", false)
			return
		}
		found = true
		sanitized, err = s.images.Sanitize(part, part.Header.Get("Content-Type"))
		_ = part.Close()
		if err != nil {
			if isBodyTooLarge(err) {
				s.writeError(w, r, http.StatusRequestEntityTooLarge, "image_too_large", "Image is too large", false)
				return
			}
			s.writeImageError(w, r, err)
			return
		}
	}
	if !found {
		s.writeError(w, r, http.StatusBadRequest, "image_required", "Multipart field image is required", false)
		return
	}
	if !consented {
		s.writeError(w, r, http.StatusUnprocessableEntity, "privacy_consent_required", "Consent is required before sending the image to the AI processor", false)
		return
	}
	var idempotencyKey string
	var idempotencyEntry *idempotencyEntry
	if len(idempotencyValues) == 1 {
		idempotencyKey = idempotencyValues[0]
		var state idempotencyState
		idempotencyEntry, state = s.idempotency.begin(idempotencyKey, sanitized.JPEG)
		switch state {
		case idempotencyConflict:
			s.writeError(w, r, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was used with a different image", false)
			return
		case idempotencyFull:
			s.writeError(w, r, http.StatusServiceUnavailable, "idempotency_unavailable", "Analysis is temporarily busy", true)
			return
		case idempotencyExisting:
			select {
			case <-idempotencyEntry.done:
				s.writeAnalysisOutcome(w, r, idempotencyEntry.outcome)
			case <-r.Context().Done():
			}
			return
		case idempotencyLeader:
			defer s.idempotency.finish(idempotencyKey, idempotencyEntry, analysisOutcome{internalError: true})
		}
	}
	complete := func(outcome analysisOutcome) {
		if idempotencyEntry != nil {
			s.idempotency.finish(idempotencyKey, idempotencyEntry, outcome)
		}
		s.writeAnalysisOutcome(w, r, outcome)
	}
	if !s.configured {
		complete(analysisOutcome{visionErr: vision.ErrUnavailable})
		return
	}
	if retryAfter := reservation.commit(); retryAfter > 0 {
		complete(analysisOutcome{rateLimited: true, retryAfter: retryAfter})
		return
	}
	features, err := s.vision.Analyze(r.Context(), sanitized.JPEG)
	if err != nil {
		complete(analysisOutcome{visionErr: err})
		return
	}
	result, err := s.engine.Evaluate(features)
	if err != nil {
		complete(analysisOutcome{scoringFailed: true})
		return
	}
	complete(analysisOutcome{result: result, analysisID: requestIDFrom(r)})
}

func (s *Server) writeAnalysisOutcome(w http.ResponseWriter, r *http.Request, outcome analysisOutcome) {
	switch {
	case outcome.rateLimited:
		w.Header().Set("Retry-After", retryAfterHeader(outcome.retryAfter))
		s.writeError(w, r, http.StatusTooManyRequests, "analysis_rate_limited", "Too many analysis requests", true)
		return
	case outcome.internalError:
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", "Unexpected server error", true)
		return
	case outcome.visionErr != nil:
		s.writeVisionError(w, r, outcome.visionErr)
		return
	case outcome.scoringFailed:
		s.writeError(w, r, http.StatusBadGateway, "scoring_failed", "AI result could not be scored", true)
		return
	}
	analysisID := outcome.analysisID
	if analysisID == "" {
		analysisID = requestIDFrom(r)
	}
	response := analysisResponse{AnalysisID: analysisID, Status: outcome.result.Status, Result: outcome.result}
	response.Privacy.ServerImageStored = false
	writeJSON(w, http.StatusOK, response)
}

func isBodyTooLarge(err error) bool {
	var limitErr *http.MaxBytesError
	return errors.Is(err, multipart.ErrMessageTooLarge) || errors.As(err, &limitErr)
}

func (s *Server) writeImageError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, imageproc.ErrUnsupportedType):
		s.writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_image_type", "Only JPEG and PNG images are supported", false)
	case errors.Is(err, imageproc.ErrImageTooLarge):
		s.writeError(w, r, http.StatusUnprocessableEntity, "image_dimensions_too_large", "Image dimensions are too large", false)
	case errors.Is(err, imageproc.ErrImageTooSmall):
		s.writeError(w, r, http.StatusUnprocessableEntity, "image_dimensions_too_small", "Image must be at least 320×320 pixels", false)
	default:
		s.writeError(w, r, http.StatusUnprocessableEntity, "invalid_image", "Image could not be decoded", false)
	}
}

func (s *Server) writeVisionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, vision.ErrUnavailable):
		s.writeError(w, r, http.StatusServiceUnavailable, "ai_unavailable", "AI analysis is temporarily unavailable", true)
	case errors.Is(err, vision.ErrRejected):
		s.writeError(w, r, http.StatusUnprocessableEntity, "ai_rejected_image", "AI could not analyze this image", false)
	default:
		s.writeError(w, r, http.StatusBadGateway, "invalid_ai_response", "AI returned an invalid response", true)
	}
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
				s.logger.Error("panic", "request_id", requestIDFrom(r), "panic_type", fmt.Sprintf("%T", recovered), "stack", string(debug.Stack()))
				s.writeError(w, r, http.StatusInternalServerError, "internal_error", "Unexpected server error", true)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
