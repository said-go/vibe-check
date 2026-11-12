package httpapi

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/analysis"
	"github.com/said-go/vibe-check/backend/internal/vision"
)

const validPayload = `{"contract_version":"1.0.0","dominant_colors":["black","gray"],"color_harmony":82,"darkness":76,"formality":68,"sportiness":41,"originality":73,"outfit_coherence":88,"accessory_intensity":57,"layering":44,"traditional_elements":12,"image_quality":91,"outfit_visibility":90,"confidence":86,"person_count":1,"subculture_signals":{"goth":38,"alternative":22,"streetwear":46,"classic":75,"sport_chic":51}}`

type fakeVision struct {
	features analysis.VisualFeatures
	err      error
}

func (f fakeVision) Analyze(context.Context, []byte) (analysis.VisualFeatures, error) {
	return f.features, f.err
}

func testHandler() http.Handler {
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, vision.Unconfigured{})
}

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

func TestAnalyzeImageEndToEnd(t *testing.T) {
	features, err := analysis.DecodeFeatures(strings.NewReader(validPayload))
	if err != nil {
		t.Fatal(err)
	}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, fakeVision{features: features})
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="image"; filename="look.jpg"`)
	header.Set("Content-Type", "image/jpeg")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 640, 800))
	for y := 0; y < 800; y++ {
		for x := 0; x < 640; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 30, B: 40, A: 255})
		}
	}
	if err := jpeg.Encode(part, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("privacy_consent", "true"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/analyses", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"server_image_stored":false`) || !strings.Contains(response.Body.String(), `"external_processor":"openai"`) {
		t.Fatalf("privacy flag missing: %s", response.Body.String())
	}
}
