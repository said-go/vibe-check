package imageproc

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"strings"
	"testing"
)

func jpegFixture(t *testing.T, width, height int, trailer string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 40, B: 50, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return append(buffer.Bytes(), []byte(trailer)...)
}

func TestSanitizeRemovesTrailingMetadata(t *testing.T) {
	input := jpegFixture(t, 640, 800, "GPS_SECRET_METADATA")
	result, err := New().Sanitize(bytes.NewReader(input), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if result.Width != 640 || result.Height != 800 {
		t.Fatalf("unexpected dimensions %dx%d", result.Width, result.Height)
	}
	if bytes.Contains(result.JPEG, []byte("GPS_SECRET_METADATA")) {
		t.Fatal("metadata was not removed")
	}
	if detected := httpContentType(result.JPEG); detected != "image/jpeg" {
		t.Fatalf("unexpected type %q", detected)
	}
}

func TestSanitizeRejectsSmallAndUnsupportedImages(t *testing.T) {
	if _, err := New().Sanitize(bytes.NewReader(jpegFixture(t, 128, 128, "")), "image/jpeg"); err != nil {
		t.Fatalf("128px image should be accepted: %v", err)
	}
	if _, err := New().Sanitize(bytes.NewReader(jpegFixture(t, 100, 100, "")), "image/jpeg"); !errorsIs(err, ErrImageTooSmall) {
		t.Fatalf("expected small image error, got %v", err)
	}
	if _, err := New().Sanitize(strings.NewReader("GIF89a"), "image/gif"); !errorsIs(err, ErrUnsupportedType) {
		t.Fatalf("expected type error, got %v", err)
	}
}

func httpContentType(data []byte) string { return http.DetectContentType(data) }
func errorsIs(err, target error) bool    { return errors.Is(err, target) }
