package imageproc

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
)

const ProviderMaxDimension = 1600

// ForProvider bounds an already sanitized JPEG without changing preset matching
// or upload validation. Small images retain their exact bytes. Area averaging
// preserves fine detail when reducing large images, without external packages.
func ForProvider(clean SanitizedImage) ([]byte, error) {
	if clean.Width <= ProviderMaxDimension && clean.Height <= ProviderMaxDimension {
		return clean.JPEG, nil
	}
	src, err := jpeg.Decode(bytes.NewReader(clean.JPEG))
	if err != nil {
		return nil, ErrInvalidImage
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w >= h {
		w, h = ProviderMaxDimension, max(1, h*ProviderMaxDimension/w)
	} else {
		w, h = max(1, w*ProviderMaxDimension/h), ProviderMaxDimension
	}
	dst := areaResize(src, w, h)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func areaResize(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sx, sy := float64(b.Dx())/float64(w), float64(b.Dy())/float64(h)
	for y := 0; y < h; y++ {
		y0, y1 := float64(y)*sy, float64(y+1)*sy
		for x := 0; x < w; x++ {
			x0, x1 := float64(x)*sx, float64(x+1)*sx
			var red, green, blue, total float64
			for iy := int(y0); iy < min(b.Dy(), int(math.Ceil(y1))); iy++ {
				wy := math.Min(y1, float64(iy+1)) - math.Max(y0, float64(iy))
				for ix := int(x0); ix < min(b.Dx(), int(math.Ceil(x1))); ix++ {
					weight := wy * (math.Min(x1, float64(ix+1)) - math.Max(x0, float64(ix)))
					r, g, blueValue, _ := src.At(b.Min.X+ix, b.Min.Y+iy).RGBA()
					red += float64(r>>8) * weight
					green += float64(g>>8) * weight
					blue += float64(blueValue>>8) * weight
					total += weight
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(math.Round(red / total)), uint8(math.Round(green / total)), uint8(math.Round(blue / total)), 255})
		}
	}
	return dst
}
