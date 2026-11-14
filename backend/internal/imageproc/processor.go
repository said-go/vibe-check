package imageproc

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
)

var (
	ErrUnsupportedType = errors.New("unsupported image type")
	ErrInvalidImage    = errors.New("invalid image")
	ErrImageTooSmall   = errors.New("image dimensions are too small")
	ErrImageTooLarge   = errors.New("image dimensions are too large")
)

type SanitizedImage struct {
	JPEG   []byte
	Width  int
	Height int
}

type Processor struct {
	MinDimension int
	MaxPixels    int64
	JPEGQuality  int
}

func New() Processor {
	return Processor{MinDimension: 128, MaxPixels: 20_000_000, JPEGQuality: 88}
}

func (p Processor) Sanitize(reader io.Reader, declaredType string) (SanitizedImage, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return SanitizedImage{}, fmt.Errorf("read image: %w", err)
	}
	if len(data) == 0 {
		return SanitizedImage{}, ErrInvalidImage
	}
	detected := http.DetectContentType(data)
	if detected != "image/jpeg" && detected != "image/png" {
		return SanitizedImage{}, ErrUnsupportedType
	}
	if declaredType != "" {
		mediaType, _, err := mime.ParseMediaType(declaredType)
		if err != nil || mediaType != detected && mediaType != "application/octet-stream" && !(detected == "image/jpeg" && mediaType == "image/jpg") {
			return SanitizedImage{}, ErrUnsupportedType
		}
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return SanitizedImage{}, ErrInvalidImage
	}
	if config.Width < p.MinDimension || config.Height < p.MinDimension {
		return SanitizedImage{}, ErrImageTooSmall
	}
	if int64(config.Width)*int64(config.Height) > p.MaxPixels {
		return SanitizedImage{}, ErrImageTooLarge
	}

	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return SanitizedImage{}, ErrInvalidImage
	}
	if detected == "image/jpeg" {
		if orientation := jpegOrientation(data); orientation != 1 {
			decoded = orientedImage{decoded, orientation}
		}
	}
	bounds := decoded.Bounds()
	clean := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(clean, clean.Bounds(), &image.Uniform{C: color.RGBA{R: 245, G: 241, B: 234, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(clean, clean.Bounds(), decoded, bounds.Min, draw.Over)

	var output bytes.Buffer
	if err := jpeg.Encode(&output, clean, &jpeg.Options{Quality: p.JPEGQuality}); err != nil {
		return SanitizedImage{}, fmt.Errorf("encode sanitized image: %w", err)
	}
	return SanitizedImage{JPEG: output.Bytes(), Width: bounds.Dx(), Height: bounds.Dy()}, nil
}
