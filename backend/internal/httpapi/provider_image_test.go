package httpapi

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

type dimensionVision struct{ received []byte }

func (p *dimensionVision) Analyze(_ context.Context, data []byte) (analysis.VisualFeatures, error) {
	p.received = data
	return analysis.DecodeFeatures(strings.NewReader(validPayload))
}

func TestDirectUploadBoundsProviderImage(t *testing.T) {
	var input bytes.Buffer
	if err := jpeg.Encode(&input, image.NewRGBA(image.Rect(0, 0, 2400, 1800)), nil); err != nil {
		t.Fatal(err)
	}
	p := &dimensionVision{}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, p)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, makeImageRequest(t, []string{"true"}, [][]byte{input.Bytes()}, "image/jpeg"))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(p.received))
	if err != nil || cfg.Width != 1600 || cfg.Height != 1200 {
		t.Fatalf("provider image not bounded: %+v, %v", cfg, err)
	}
}
