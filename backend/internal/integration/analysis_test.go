package integration_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/httpapi"
	"github.com/said-go/vibe-check/backend/internal/vision"
)

const visualFeatures = `{"contract_version":"1.0.0","dominant_colors":["black","gray"],"color_harmony":82,"darkness":76,"formality":68,"sportiness":41,"originality":73,"outfit_coherence":88,"accessory_intensity":57,"layering":44,"traditional_elements":12,"image_quality":91,"outfit_visibility":90,"confidence":86,"person_count":1,"subculture_signals":{"goth":38,"alternative":22,"streetwear":46,"classic":75,"sport_chic":51}}`

func TestPhotoToYandexToResult(t *testing.T) {
	for _, tc := range []struct {
		name       string
		features   string
		wantStatus string
	}{
		{"completed", visualFeatures, "completed"},
		{"no person", strings.Replace(visualFeatures, `"person_count":1`, `"person_count":0`, 1), "low_confidence"},
		{"multiple people", strings.Replace(visualFeatures, `"person_count":1`, `"person_count":3`, 1), "low_confidence"},
		{"uncertain model", strings.Replace(visualFeatures, `"confidence":86`, `"confidence":1`, 1), "low_confidence"},
		{"mula", strings.Replace(visualFeatures, `"person_count":1,`, `"person_count":1,"skullcap_visible":true,"beard_visible":true,`, 1), "completed"},
		{"partial outfit", strings.NewReplacer(`"outfit_visibility":90`, `"outfit_visibility":40`, `"image_quality":91`, `"image_quality":30`, `"confidence":86`, `"confidence":35`).Replace(visualFeatures), "completed"},
		{"no visible clothing", strings.NewReplacer(`"outfit_visibility":90`, `"outfit_visibility":0`, `"confidence":86`, `"confidence":0`, `"outfit_coherence":88`, `"outfit_coherence":0`, `"color_harmony":82`, `"color_harmony":0`, `"originality":73`, `"originality":0`).Replace(visualFeatures), "low_confidence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var providerCalls atomic.Int32
			mockYandex := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				providerCalls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
					t.Errorf("Yandex request: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Api-Key test-key" || r.Header.Get("OpenAI-Project") != "test-folder" {
					t.Error("Yandex authentication headers are missing")
				}
				var sent struct {
					Model    string `json:"model"`
					Messages []struct {
						Content []struct {
							Type     string `json:"type"`
							ImageURL struct {
								URL string `json:"url"`
							} `json:"image_url"`
						} `json:"content"`
					} `json:"messages"`
					ResponseFormat struct {
						Type string `json:"type"`
					} `json:"response_format"`
				}
				if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
					t.Errorf("decode provider request: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if sent.Model != "gpt://test-folder/qwen3.6-35b-a3b" || sent.ResponseFormat.Type != "json_object" {
					t.Errorf("model or response format is invalid: %+v", sent)
				}
				if len(sent.Messages) != 1 || len(sent.Messages[0].Content) != 2 || sent.Messages[0].Content[1].Type != "image_url" {
					t.Errorf("image content is missing: %+v", sent.Messages)
				} else {
					const prefix = "data:image/jpeg;base64,"
					url := sent.Messages[0].Content[1].ImageURL.URL
					if !strings.HasPrefix(url, prefix) {
						t.Errorf("image is not a JPEG data URL")
					} else if data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(url, prefix)); err != nil {
						t.Errorf("decode image: %v", err)
					} else if size, err := jpeg.DecodeConfig(bytes.NewReader(data)); err != nil || size.Width != 360 || size.Height != 480 {
						t.Errorf("sanitized JPEG dimensions: %+v, %v", size, err)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": tc.features}}}})
			}))
			defer mockYandex.Close()

			provider := vision.NewYandex("test-key", "test-folder", "", mockYandex.URL, mockYandex.Client())
			api := httpapi.New(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, provider)
			request := uploadRequest(t, true)
			response := httptest.NewRecorder()
			api.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			var result struct {
				Status string `json:"status"`
				Result struct {
					Score           int    `json:"score"`
					Summary         string `json:"summary"`
					PrimaryCategory struct {
						Code string `json:"code"`
					} `json:"primary_category"`
				} `json:"result"`
				Privacy struct {
					ServerImageStored bool `json:"server_image_stored"`
				} `json:"privacy"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Status != tc.wantStatus || result.Privacy.ServerImageStored || providerCalls.Load() != 1 {
				t.Fatalf("unexpected result %+v, provider calls %d", result, providerCalls.Load())
			}
			if tc.wantStatus == "completed" && (result.Result.Score != 82 || result.Result.PrimaryCategory.Code == "") {
				t.Fatalf("completed score or category invalid: %+v", result.Result)
			}
			if tc.name == "partial outfit" && !strings.Contains(result.Result.Summary, "Одежду видно не целиком") {
				t.Fatalf("partial view qualifier missing: %+v", result.Result)
			}
			if tc.name == "mula" && result.Result.PrimaryCategory.Code != "mula" {
				t.Fatalf("visible skullcap and beard should select mula: %+v", result.Result)
			}
			if tc.wantStatus == "low_confidence" && result.Result.PrimaryCategory.Code != "" {
				t.Fatalf("low-confidence response exposed category: %+v", result.Result)
			}
		})
	}
}

func TestMissingConsentNeverCallsYandex(t *testing.T) {
	var called atomic.Bool
	mockYandex := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer mockYandex.Close()
	provider := vision.NewYandex("test-key", "test-folder", "", mockYandex.URL, mockYandex.Client())
	api := httpapi.New(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, provider)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, uploadRequest(t, false))
	if response.Code != http.StatusUnprocessableEntity || called.Load() {
		t.Fatalf("status %d, provider called %t", response.Code, called.Load())
	}
}

func uploadRequest(t *testing.T, consent bool) *http.Request {
	t.Helper()
	var data bytes.Buffer
	writer := multipart.NewWriter(&data)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="image"; filename="outfit.png"`)
	header.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 360, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 360; x++ {
			img.Set(x, y, color.RGBA{R: 25, G: 40, B: 65, A: 255})
		}
	}
	if err := png.Encode(part, img); err != nil {
		t.Fatal(err)
	}
	if consent {
		if err := writer.WriteField("privacy_consent", "true"); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/analyses", &data)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}
