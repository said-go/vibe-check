package httpapi

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/imageproc"
	"github.com/said-go/vibe-check/backend/internal/scoring"
	"github.com/said-go/vibe-check/backend/internal/vision"
)

func patternedImage(t *testing.T, reverse bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			v := uint8((x*5 + y*3 + (x*y)%37) % 256)
			if reverse {
				v = 255 - v
			}
			img.Set(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 86}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestPhotoPresetRequiresBothDigestAndVisualMatch(t *testing.T) {
	photo := patternedImage(t, false)
	sanitized, err := imageproc.New().Sanitize(bytes.NewReader(photo), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := visualHash(sanitized.JPEG)
	if err != nil {
		t.Fatal(err)
	}
	presets := PhotoPresets{{originalSHA256: strings.Repeat("a", 64), visualHash: hash, result: scoring.Result{Status: "completed", PrimaryCategory: scoring.CategoryScore{Title: "Test preset"}}}}

	if got, ok := presets.match(strings.Repeat("a", 64), sanitized.JPEG); !ok || got.PrimaryCategory.Title != "Test preset" {
		t.Fatalf("expected preset result, got %#v, matched=%v", got, ok)
	}
	if _, ok := presets.match(strings.Repeat("b", 64), sanitized.JPEG); ok {
		t.Fatal("wrong digest matched")
	}
	other, err := imageproc.New().Sanitize(bytes.NewReader(patternedImage(t, true)), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := presets.match(strings.Repeat("a", 64), other.JPEG); ok {
		t.Fatal("unrelated image with claimed digest matched")
	}
}

func TestPhotoPresetSkipsProviderAndSpoofedDigestDoesNot(t *testing.T) {
	photo := patternedImage(t, false)
	sanitized, err := imageproc.New().Sanitize(bytes.NewReader(photo), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := visualHash(sanitized.JPEG)
	if err != nil {
		t.Fatal(err)
	}
	presets := PhotoPresets{{originalSHA256: strings.Repeat("a", 64), visualHash: hash, result: scoring.Result{Status: "completed", PrimaryCategory: scoring.CategoryScore{Code: "preset", Title: "Test preset"}}}}
	shared := &fakeSharedBudget{broken: true}
	limits := DefaultLimits
	limits.SharedBudget = shared
	handler := NewWithPresets(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, vision.Unconfigured{}, limits, presets)
	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != 200 || !strings.Contains(ready.Body.String(), `"ai_available":false`) || !strings.Contains(ready.Body.String(), `"presets_available":true`) {
		t.Fatalf("presets-only readiness: %d %s", ready.Code, ready.Body.String())
	}
	request := makeImageRequest(t, []string{"true"}, [][]byte{photo}, "image/jpeg")
	request.Header.Set("X-Original-Image-SHA256", strings.Repeat("a", 64))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"title":"Test preset"`) {
		t.Fatalf("preset response: status %d, body %s", response.Code, response.Body.String())
	}
	if shared.calls != 0 {
		t.Fatal("preset consumed shared budget")
	}
	request = makeImageRequest(t, []string{"true"}, [][]byte{patternedImage(t, true)}, "image/jpeg")
	request.Header.Set("X-Original-Image-SHA256", strings.Repeat("a", 64))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("spoofed digest should use normal provider path: status %d, body %s", response.Code, response.Body.String())
	}
}

func TestLoadPhotoPresetsValidation(t *testing.T) {
	valid := map[string]any{"original_sha256": strings.Repeat("a", 64), "visual_hash": []string{"0000000000000000", "0000000000000001", "0000000000000002", "0000000000000003"}, "result": scoring.Result{Status: "completed", Score: 50, Confidence: 80, PrimaryCategory: scoring.CategoryScore{Code: "synthetic", Title: "Synthetic", Score: 50}, LevelTitle: "Synthetic", Summary: "Synthetic fixture"}}
	encode := func(rows any) []byte {
		data, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	validBytes := encode([]any{valid})
	for _, tc := range []struct {
		name string
		data []byte
		ok   bool
	}{
		{"valid", validBytes, true}, {"empty", []byte("[]"), false}, {"null", []byte("null"), false}, {"malformed", []byte("{"), false},
		{"unknown field", []byte(strings.Replace(string(validBytes), `"original_sha256"`, `"unknown"`, 1)), false},
		{"bad digest", []byte(strings.Replace(string(validBytes), strings.Repeat("a", 64), "invalid", 1)), false},
		{"bad hash", []byte(strings.Replace(string(validBytes), "0000000000000003", "invalid", 1)), false},
		{"bad result", []byte(strings.Replace(string(validBytes), `"score":50`, `"score":101`, 1)), false},
		{"duplicate", encode([]any{valid, valid}), false}, {"trailing", append(append([]byte{}, validBytes...), []byte(" {}")...), false},
		{"oversized", bytes.Repeat([]byte(" "), (256<<10)+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "presets.json")
			if err := os.WriteFile(path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := LoadPhotoPresets(path)
			if (err == nil) != tc.ok {
				t.Fatalf("load: %v", err)
			}
			if tc.name == "valid" && len(got) != 1 {
				t.Fatal("expected synthetic preset")
			}
		})
	}
	if got, err := LoadPhotoPresets(""); err != nil || len(got) != 0 {
		t.Fatal("empty path must disable presets")
	}
	if _, err := LoadPhotoPresets(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestPresetWorksAfterPaidBudgetExhausted(t *testing.T) {
	photo := patternedImage(t, false)
	clean, err := imageproc.New().Sanitize(bytes.NewReader(photo), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := visualHash(clean.JPEG)
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	presets := PhotoPresets{{originalSHA256: digest, visualHash: hash, result: scoring.Result{Status: "completed", PrimaryCategory: scoring.CategoryScore{Code: "preset", Title: "Synthetic"}}}}
	calls := 0
	handler := NewWithPresets(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, fakeVision{err: vision.ErrUnavailable, calls: &calls}, Limits{PerHour: 1, PerClientMinute: 1, Concurrent: 1}, presets)
	run := func(image []byte, claim string) int {
		r := makeImageRequest(t, []string{"true"}, [][]byte{image}, "image/jpeg")
		r.Header.Set("X-Original-Image-SHA256", claim)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	if got := run(photo, ""); got != 503 || calls != 1 {
		t.Fatalf("initial status=%d calls=%d", got, calls)
	}
	if got := run(photo, digest); got != 200 || calls != 1 {
		t.Fatalf("preset status=%d calls=%d", got, calls)
	}
	if got := run(patternedImage(t, true), digest); got != 429 || calls != 1 {
		t.Fatalf("spoof status=%d calls=%d", got, calls)
	}
}
