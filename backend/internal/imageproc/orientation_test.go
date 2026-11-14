package imageproc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func exifJPEG(t *testing.T, orientation int, order binary.ByteOrder) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 160, 128))
	colors := []color.RGBA{{240, 20, 20, 255}, {20, 240, 20, 255}, {20, 20, 240, 255}, {240, 240, 20, 255}}
	for y := 0; y < 128; y++ {
		for x := 0; x < 160; x++ {
			img.Set(x, y, colors[(y/64)*2+x/80])
		}
	}
	var raw bytes.Buffer
	jpeg.Encode(&raw, img, &jpeg.Options{Quality: 95})
	exif := make([]byte, 32)
	copy(exif, "Exif\x00\x00")
	h := exif[6:]
	copy(h, "II")
	if order == binary.BigEndian {
		copy(h, "MM")
	}
	order.PutUint16(h[2:4], 42)
	order.PutUint32(h[4:8], 8)
	order.PutUint16(h[8:10], 1)
	order.PutUint16(h[10:12], 0x112)
	order.PutUint16(h[12:14], 3)
	order.PutUint32(h[14:18], 1)
	order.PutUint16(h[18:20], uint16(orientation))
	out := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, 34}, exif...)
	return append(out, raw.Bytes()[2:]...)
}

func TestSanitizeJPEGOrientationAndStripEXIF(t *testing.T) {
	expected := [][]int{{0, 1, 2, 3}, {1, 0, 3, 2}, {3, 2, 1, 0}, {2, 3, 0, 1}, {0, 2, 1, 3}, {2, 0, 3, 1}, {3, 1, 2, 0}, {1, 3, 0, 2}}
	colors := []color.RGBA{{240, 20, 20, 255}, {20, 240, 20, 255}, {20, 20, 240, 255}, {240, 240, 20, 255}}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for orientation := 1; orientation <= 8; orientation++ {
			got, err := New().Sanitize(bytes.NewReader(exifJPEG(t, orientation, order)), "image/jpeg")
			if err != nil {
				t.Fatal(err)
			}
			w, h := 160, 128
			if orientation >= 5 {
				w, h = h, w
			}
			if got.Width != w || got.Height != h {
				t.Fatalf("orientation%d dimensions %dx%d", orientation, got.Width, got.Height)
			}
			if bytes.Contains(got.JPEG, []byte("Exif")) {
				t.Fatal("EXIF retained")
			}
			img, err := jpeg.Decode(bytes.NewReader(got.JPEG))
			if err != nil {
				t.Fatal(err)
			}
			for i, p := range []image.Point{{20, 20}, {w - 20, 20}, {20, h - 20}, {w - 20, h - 20}} {
				c := color.RGBAModel.Convert(img.At(p.X, p.Y)).(color.RGBA)
				want := colors[expected[orientation-1][i]]
				for _, pair := range [][2]uint8{{c.R, want.R}, {c.G, want.G}, {c.B, want.B}} {
					d := int(pair[0]) - int(pair[1])
					if d < -20 || d > 20 {
						t.Fatalf("orientation%d corner%d got%v want%v", orientation, i, c, want)
					}
				}
			}
		}
	}
}

func TestMalformedOrientationIgnored(t *testing.T) {
	for _, data := range [][]byte{nil, {0xff, 0xd8, 0xff, 0xe1, 0xff, 0xff}, []byte("Exif"), {0xff, 0xd8, 0xff, 0xff}} {
		if jpegOrientation(data) != 1 {
			t.Fatal("invalid metadata changed orientation")
		}
	}
	data := exifJPEG(t, 6, binary.LittleEndian)
	// Corrupt TIFF IFD offset while keeping the JPEG decodable.
	for i := 16; i < 20; i++ {
		data[i] = 0xff
	}
	got, err := New().Sanitize(bytes.NewReader(data), "image/jpeg")
	if err != nil || got.Width != 160 || got.Height != 128 {
		t.Fatalf("invalid EXIF should not reject valid photo: %v", err)
	}
}

func TestTransparentPNGAndTruncatedImages(t *testing.T) {
	var raw bytes.Buffer
	png.Encode(&raw, image.NewNRGBA(image.Rect(0, 0, 128, 128)))
	got, err := New().Sanitize(bytes.NewReader(raw.Bytes()), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	img, _ := jpeg.Decode(bytes.NewReader(got.JPEG))
	r, g, b, _ := img.At(64, 64).RGBA()
	if r>>8 < 240 || g>>8 < 235 || b>>8 < 228 {
		t.Fatal("transparent background became dark")
	}
	for _, data := range [][]byte{raw.Bytes()[:raw.Len()/2], jpegFixture(t, 128, 128, "")[:100]} {
		if _, err := New().Sanitize(bytes.NewReader(data), ""); !errors.Is(err, ErrInvalidImage) {
			t.Fatalf("truncated image: %v", err)
		}
	}
}

func TestHugePNGRejectedBeforePixelDecode(t *testing.T) {
	var raw bytes.Buffer
	png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 128, 128)))
	data := append([]byte(nil), raw.Bytes()...)
	binary.BigEndian.PutUint32(data[16:20], 30000)
	binary.BigEndian.PutUint32(data[20:24], 30000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if _, err := New().Sanitize(bytes.NewReader(data), "image/png"); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("huge image header should be rejected before decoding pixels: %v", err)
	}
}
