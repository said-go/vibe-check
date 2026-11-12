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
	calls    *int
}

func (f fakeVision) Analyze(context.Context, []byte) (analysis.VisualFeatures, error) {
	if f.calls != nil {
		*f.calls++
	}
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

func TestReadinessRequiresConfiguredProvider(t *testing.T) {
	response := httptest.NewRecorder()
	testHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured readiness status %d", response.Code)
	}
	response = httptest.NewRecorder()
	New(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, fakeVision{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("configured readiness status %d", response.Code)
	}
}

func TestInternalScoringRouteIsNotPublic(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/scoring/evaluate", strings.NewReader(validPayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	testHandler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
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
	if !strings.Contains(response.Body.String(), `"server_image_stored":false`) {
		t.Fatalf("privacy flag missing: %s", response.Body.String())
	}
}

func makeImageRequest(t *testing.T, consent []string, images [][]byte, contentType string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, value := range consent {
		if err := writer.WriteField("privacy_consent", value); err != nil {
			t.Fatal(err)
		}
	}
	for _, imageBytes := range images {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", `form-data; name="image"; filename="look.jpg"`)
		header.Set("Content-Type", contentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(imageBytes); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/analyses", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func imageFixture(t *testing.T) []byte {
	t.Helper()
	var body bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 320, 320))
	if err := jpeg.Encode(&body, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func TestAnalyzeRejectsMissingOrDuplicateConsentBeforeProviderCall(t *testing.T) {
	features, _ := analysis.DecodeFeatures(strings.NewReader(validPayload))
	for _, tc := range []struct {
		name      string
		consent   []string
		wantCode  int
		wantError string
	}{
		{"missing", nil, http.StatusUnprocessableEntity, "privacy_consent_required"},
		{"duplicate", []string{"true", "true"}, http.StatusBadRequest, "duplicate_consent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, fakeVision{features: features, calls: &calls})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, makeImageRequest(t, tc.consent, [][]byte{imageFixture(t)}, "image/jpeg"))
			if response.Code != tc.wantCode || !strings.Contains(response.Body.String(), tc.wantError) {
				t.Fatalf("status %d, body %s", response.Code, response.Body.String())
			}
			if calls != 0 {
				t.Fatalf("provider was called %d times", calls)
			}
		})
	}
}

func TestAnalyzeRejectsDuplicateAndMismatchedImages(t *testing.T) {
	for _, tc := range []struct {
		name       string
		images     [][]byte
		typeHeader string
		wantCode   int
		wantError  string
	}{
		{"duplicate", [][]byte{imageFixture(t), imageFixture(t)}, "image/jpeg", http.StatusBadRequest, "multiple_images"},
		{"type mismatch", [][]byte{imageFixture(t)}, "image/png", http.StatusUnsupportedMediaType, "unsupported_image_type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			testHandler().ServeHTTP(response, makeImageRequest(t, []string{"true"}, tc.images, tc.typeHeader))
			if response.Code != tc.wantCode || !strings.Contains(response.Body.String(), tc.wantError) {
				t.Fatalf("status %d, body %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAnalyzeEnforcesBodyLimitWhileReadingImage(t *testing.T) {
	imageBytes := append(imageFixture(t), bytes.Repeat([]byte("x"), 4096)...)
	request := makeImageRequest(t, []string{"true"}, [][]byte{imageBytes}, "image/jpeg")
	response := httptest.NewRecorder()
	New(slog.New(slog.NewTextHandler(io.Discard, nil)), 1024, vision.Unconfigured{}).ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "image_too_large") {
		t.Fatalf("status %d, body %s", response.Code, response.Body.String())
	}
}

func TestAnalyzeRateLimitRunsBeforeImageDecode(t *testing.T) {
	features, _ := analysis.DecodeFeatures(strings.NewReader(validPayload))
	calls := 0
	handler := NewWithLimits(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, fakeVision{features: features, calls: &calls}, Limits{PerHour: 1, PerClientMinute: 1, Concurrent: 1})
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, makeImageRequest(t, []string{"true"}, [][]byte{imageFixture(t)}, "image/jpeg"))
	if first.Code != http.StatusOK || calls != 1 {
		t.Fatalf("first analysis: status=%d calls=%d", first.Code, calls)
	}
	second := httptest.NewRecorder()
	request := makeImageRequest(t, []string{"true"}, [][]byte{[]byte("invalid image")}, "image/jpeg")
	request.Header.Set("X-Forwarded-For", "198.51.100.10")
	handler.ServeHTTP(second, request)
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" || calls != 1 {
		t.Fatalf("rate limit must precede image decoding and AI call: status=%d body=%s calls=%d", second.Code, second.Body.String(), calls)
	}
}
