package evaluation

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

func TestCollectionBoundsProviderImage(t *testing.T) {
	dir := t.TempDir()
	var input bytes.Buffer
	if err := png.Encode(&input, image.NewRGBA(image.Rect(0, 0, 1800, 2400))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "large.png"), input.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Decode(bytes.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	f, err := analysis.DecodeFeatures(bytes.NewReader(d.Cases[0].Features))
	if err != nil {
		t.Fatal(err)
	}
	p := &collectProvider{features: f}
	m := ImageManifest{Cases: []ImageCase{{Path: "large.png", Expected: d.Cases[0].Expected}}}
	if _, err := Collect(context.Background(), m, dir, p, 1); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(p.received))
	if err != nil || cfg.Width != 1200 || cfg.Height != 1600 {
		t.Fatalf("provider image not bounded: %+v, %v", cfg, err)
	}
}
