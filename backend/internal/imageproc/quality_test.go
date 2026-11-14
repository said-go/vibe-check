package imageproc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// A deterministic, nonuniform synthetic frame exercises DCT blocks and edges;
// no user photos or external fixtures are needed for these tests.
func texturedJPEG(t *testing.T, width, height, quality int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x*3 + y), uint8(y*5 + x/8), uint8((x/11 + y/7) * 23), 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestSanitizeResolutionAndCompressionMatrix(t *testing.T) {
	for _, size := range []image.Point{{128, 128}, {129, 257}, {640, 480}, {480, 640}, {1600, 1200}} {
		// Include all client compression qualities and an extreme valid JPEG.
		for _, quality := range []int{1, 40, 54, 70, 86, 100} {
			t.Run(fmt.Sprintf("%dx%d/q%d", size.X, size.Y, quality), func(t *testing.T) {
				input := texturedJPEG(t, size.X, size.Y, quality)
				got, err := New().Sanitize(bytes.NewReader(input), "image/jpeg")
				if err != nil {
					t.Fatalf("valid compressed photo rejected: %v", err)
				}
				decoded, err := jpeg.Decode(bytes.NewReader(got.JPEG))
				if err != nil {
					t.Fatalf("output cannot be decoded: %v", err)
				}
				if got.Width != size.X || got.Height != size.Y || decoded.Bounds().Size() != size {
					t.Fatalf("dimensions changed: result %dx%d, pixels %v", got.Width, got.Height, decoded.Bounds())
				}
				if len(got.JPEG) == 0 || len(got.JPEG) > size.X*size.Y*3+1024 {
					t.Fatalf("unexpected encoded size: %d", len(got.JPEG))
				}
			})
		}
	}
}

func TestSanitizeDimensionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		want error
	}{{127, 256, ErrImageTooSmall}, {256, 127, ErrImageTooSmall}, {128, 256, nil}, {256, 128, nil}, {129, 256, ErrImageTooLarge}} {
		t.Run(fmt.Sprintf("%dx%d", tc.w, tc.h), func(t *testing.T) {
			p := New()
			// Use a small pixel cap to test the exact boundary without allocating
			// a 20 MP image; the separate huge-header test covers the default cap.
			p.MaxPixels = 128 * 256
			got, err := p.Sanitize(bytes.NewReader(texturedJPEG(t, tc.w, tc.h, 40)), "image/jpeg")
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if err != nil && (len(got.JPEG) != 0 || got.Width != 0 || got.Height != 0) {
				t.Fatal("rejected image returned usable output")
			}
		})
	}
}

func TestSanitizeRejectsTruncatedPixelStream(t *testing.T) {
	input := texturedJPEG(t, 256, 256, 86)
	input = input[:len(input)/2]
	if _, err := jpeg.DecodeConfig(bytes.NewReader(input)); err != nil {
		t.Fatalf("fixture must have a valid dimension header: %v", err)
	}
	if _, err := New().Sanitize(bytes.NewReader(input), "image/jpeg"); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("corrupt pixel stream should be invalid_image, got %v", err)
	}
}

func TestSanitizeReencodesQualityAndRemovesJPEGMetadata(t *testing.T) {
	input := texturedJPEG(t, 256, 256, 100)
	// APP1 and COM are inside the JPEG, not merely data after the end marker.
	decorated := append([]byte(nil), input[:2]...)
	for _, marker := range []byte{0xe1, 0xfe} {
		payload := []byte("SYNTHETIC_PRIVATE_METADATA")
		segment := []byte{0xff, marker, 0, 0}
		binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2))
		decorated = append(decorated, segment...)
		decorated = append(decorated, payload...)
	}
	decorated = append(decorated, input[2:]...)
	var lengths []int
	for _, quality := range []int{40, 88} {
		p := New()
		p.JPEGQuality = quality
		got, err := p.Sanitize(bytes.NewReader(decorated), "image/jpeg")
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(got.JPEG, []byte("SYNTHETIC_PRIVATE_METADATA")) {
			t.Fatal("embedded metadata survived sanitization")
		}
		if _, err := jpeg.Decode(bytes.NewReader(got.JPEG)); err != nil {
			t.Fatal(err)
		}
		lengths = append(lengths, len(got.JPEG))
	}
	if lengths[0] >= lengths[1] {
		t.Fatalf("lower configured quality should reduce textured fixture size: %v", lengths)
	}
}

func TestOrientationMapsEveryPixelWithNonzeroOrigin(t *testing.T) {
	// A nonsquare image with a translated origin exposes edge/off-by-one
	// errors which checking just the output dimensions cannot detect.
	source := image.NewRGBA(image.Rect(7, 11, 10, 13))
	for i := 0; i < 6; i++ {
		source.SetRGBA(7+i%3, 11+i/3, color.RGBA{R: uint8(i + 1), A: 255})
	}
	for index, want := range [][]uint8{
		{1, 2, 3, 4, 5, 6}, {3, 2, 1, 6, 5, 4},
		{6, 5, 4, 3, 2, 1}, {4, 5, 6, 1, 2, 3},
		{1, 4, 2, 5, 3, 6}, {4, 1, 5, 2, 6, 3},
		{6, 3, 5, 2, 4, 1}, {3, 6, 2, 5, 1, 4},
	} {
		t.Run(fmt.Sprintf("orientation%d", index+1), func(t *testing.T) {
			got := orientedImage{source, index + 1}
			width, height := 3, 2
			if index+1 >= 5 {
				width, height = height, width
			}
			if got.Bounds() != image.Rect(0, 0, width, height) {
				t.Fatalf("unexpected bounds %v", got.Bounds())
			}
			for i, red := range want {
				pixel := color.RGBAModel.Convert(got.At(i%width, i/width)).(color.RGBA)
				if pixel != (color.RGBA{R: red, A: 255}) {
					t.Fatalf("pixel %d: got %v, want red=%d", i, pixel, red)
				}
			}
		})
	}
}
