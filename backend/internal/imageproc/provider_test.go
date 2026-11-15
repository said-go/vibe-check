package imageproc

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestProviderKeepsSmallJPEGBytes(t *testing.T) {
	for _, size := range []image.Point{{960, 1280}, {1600, 128}, {128, 1600}} {
		clean := SanitizedImage{JPEG: jpegFixture(t, size.X, size.Y, ""), Width: size.X, Height: size.Y}
		got, err := ForProvider(clean)
		if err != nil || !bytes.Equal(got, clean.JPEG) {
			t.Fatalf("small image changed: %v, %v", size, err)
		}
	}
}

func TestProviderBoundsDimensionsAndPreservesAspect(t *testing.T) {
	for _, size := range []image.Point{{3200, 2400}, {2400, 3200}, {1601, 128}, {10000, 128}} {
		clean := SanitizedImage{JPEG: jpegFixture(t, size.X, size.Y, ""), Width: size.X, Height: size.Y}
		got, err := ForProvider(clean)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(got))
		if err != nil || max(cfg.Width, cfg.Height) != ProviderMaxDimension || min(cfg.Width, cfg.Height) < 1 {
			t.Fatalf("unbounded output for %v: %+v, %v", size, cfg, err)
		}
		// Rounding may change the short edge by less than one output pixel.
		if delta := cfg.Width*size.Y - cfg.Height*size.X; delta <= -max(size.X, size.Y) || delta >= max(size.X, size.Y) {
			t.Fatalf("aspect ratio changed: %v -> %+v", size, cfg)
		}
	}
}

func TestAreaResizeAveragesFineDetail(t *testing.T) {
	src := image.NewRGBA(image.Rect(3, 7, 7, 11))
	for y := 7; y < 11; y++ {
		for x := 3; x < 7; x++ {
			value := uint8(((x + y) % 2) * 255)
			src.SetRGBA(x, y, color.RGBA{value, value, value, 255})
		}
	}
	dst := areaResize(src, 2, 2)
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			if got := dst.RGBAAt(x, y); got != (color.RGBA{128, 128, 128, 255}) {
				t.Fatalf("fine detail aliased rather than averaged: %+v", got)
			}
		}
	}
}

func TestProviderRejectsInvalidLargeJPEG(t *testing.T) {
	if _, err := ForProvider(SanitizedImage{JPEG: []byte("invalid"), Width: 2000, Height: 2000}); err == nil {
		t.Fatal("invalid JPEG accepted")
	}
}
