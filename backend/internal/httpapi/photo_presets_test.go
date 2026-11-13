package httpapi

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	previous := photoPresets
	photoPresets = []photoPreset{{originalSHA256: strings.Repeat("a", 64), visualHash: hash, result: scoring.Result{Status: "completed", PrimaryCategory: scoring.CategoryScore{Title: "Test preset"}}}}
	t.Cleanup(func() { photoPresets = previous })

	if got, ok := matchPhotoPreset(strings.Repeat("a", 64), sanitized.JPEG); !ok || got.PrimaryCategory.Title != "Test preset" {
		t.Fatalf("expected preset result, got %#v, matched=%v", got, ok)
	}
	if _, ok := matchPhotoPreset(strings.Repeat("b", 64), sanitized.JPEG); ok {
		t.Fatal("wrong digest matched")
	}
	other, err := imageproc.New().Sanitize(bytes.NewReader(patternedImage(t, true)), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := matchPhotoPreset(strings.Repeat("a", 64), other.JPEG); ok {
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
	previous := photoPresets
	photoPresets = []photoPreset{{originalSHA256: strings.Repeat("a", 64), visualHash: hash, result: scoring.Result{Status: "completed", PrimaryCategory: scoring.CategoryScore{Code: "preset", Title: "Test preset"}}}}
	t.Cleanup(func() { photoPresets = previous })
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, vision.Unconfigured{})
	request := makeImageRequest(t, []string{"true"}, [][]byte{photo}, "image/jpeg")
	request.Header.Set("X-Original-Image-SHA256", strings.Repeat("a", 64))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"title":"Test preset"`) {
		t.Fatalf("preset response: status %d, body %s", response.Code, response.Body.String())
	}
	request = makeImageRequest(t, []string{"true"}, [][]byte{patternedImage(t, true)}, "image/jpeg")
	request.Header.Set("X-Original-Image-SHA256", strings.Repeat("a", 64))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("spoofed digest should use normal provider path: status %d, body %s", response.Code, response.Body.String())
	}
}
