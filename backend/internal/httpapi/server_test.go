package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const validPayload = `{"contract_version":"1.0.0","dominant_colors":["black","gray"],"color_harmony":82,"darkness":76,"formality":68,"sportiness":41,"originality":73,"outfit_coherence":88,"accessory_intensity":57,"layering":44,"traditional_elements":12,"image_quality":91,"outfit_visibility":90,"confidence":86,"person_count":1,"subculture_signals":{"goth":38,"alternative":22,"streetwear":46,"classic":75,"sport_chic":51}}`

func testHandler() http.Handler { return New(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20) }

func TestHealthAndRequestID(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Request-ID", "test-request-123")
	response := httptest.NewRecorder()
	testHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d", response.Code)
	}
	if got := response.Header().Get("X-Request-ID"); got != "test-request-123" {
		t.Fatalf("request id %q", got)
	}
}

func TestEvaluate(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/scoring/evaluate", strings.NewReader(validPayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	testHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"primary_category"`) {
		t.Fatalf("missing result: %s", response.Body.String())
	}
}

func TestEvaluateRejectsInvalidContract(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/scoring/evaluate", strings.NewReader(`{"ethnicity":"x"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	testHandler().ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"invalid_ai_contract"`) {
		t.Fatalf("unexpected error: %s", response.Body.String())
	}
}
