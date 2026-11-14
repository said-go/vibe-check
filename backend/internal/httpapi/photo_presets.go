package httpapi

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math/bits"
	"os"
	"strconv"

	"github.com/said-go/vibe-check/backend/internal/scoring"
)

// The original file digest identifies an owner-approved photo. A visual check
// against the uploaded, sanitized image prevents a caller from claiming that
// digest for an unrelated photo. Neither the original photos nor their EXIF
// data are stored on the server.
type photoPreset struct {
	originalSHA256 string
	visualHash     [4]uint64
	result         scoring.Result
}

// PhotoPresets holds private owner-defined responses loaded once at startup.
type PhotoPresets []photoPreset

func LoadPhotoPresets(path string) (PhotoPresets, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open photo preset configuration")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (256<<10)+1))
	if err != nil || len(data) > 256<<10 {
		return nil, errors.New("photo preset configuration exceeds 256 KiB or cannot be read")
	}
	var rows []struct {
		OriginalSHA256 string         `json:"original_sha256"`
		VisualHash     []string       `json:"visual_hash"`
		Result         scoring.Result `json:"result"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rows); err != nil {
		return nil, errors.New("invalid photo preset JSON")
	}
	if decoder.Decode(&struct{}{}) != io.EOF || len(rows) == 0 || len(rows) > 100 {
		return nil, errors.New("invalid photo preset configuration")
	}
	result := make(PhotoPresets, 0, len(rows))
	seen := map[string]bool{}
	for i, row := range rows {
		bad := func() (PhotoPresets, error) { return nil, fmt.Errorf("invalid photo preset at index %d", i) }
		digest, err := hex.DecodeString(row.OriginalSHA256)
		if err != nil || len(digest) != 32 || row.OriginalSHA256 != fmt.Sprintf("%x", digest) || seen[row.OriginalSHA256] || len(row.VisualHash) != 4 {
			return bad()
		}
		seen[row.OriginalSHA256] = true
		var h [4]uint64
		for j, v := range row.VisualHash {
			if len(v) != 16 {
				return bad()
			}
			n, e := strconv.ParseUint(v, 16, 64)
			if e != nil {
				return bad()
			}
			h[j] = n
		}
		r := row.Result
		if r.Status != "completed" || r.PrimaryCategory.Code == "" || r.PrimaryCategory.Title == "" || r.LevelTitle == "" || r.Summary == "" || len(r.Summary) > 2000 || len(r.PrimaryCategory.Title) > 160 || len(r.LevelTitle) > 160 || len(r.Recommendations) > 5 {
			return bad()
		}
		for _, v := range []int{r.Score, r.Confidence, r.PrimaryCategory.Score, r.Metrics.Coherence, r.Metrics.Originality, r.Metrics.ColorHarmony} {
			if v < 0 || v > 100 {
				return bad()
			}
		}
		for _, v := range r.Recommendations {
			if len(v) > 500 {
				return bad()
			}
		}
		result = append(result, photoPreset{row.OriginalSHA256, h, r})
	}
	return result, nil
}

func (presets PhotoPresets) match(originalSHA256 string, sanitizedJPEG []byte) (scoring.Result, bool) {
	if len(originalSHA256) != 64 {
		return scoring.Result{}, false
	}
	for _, preset := range presets {
		if originalSHA256 != preset.originalSHA256 {
			continue
		}
		actual, err := visualHash(sanitizedJPEG)
		if err != nil || visualDistance(actual, preset.visualHash) > 24 {
			return scoring.Result{}, false
		}
		return preset.result, true
	}
	return scoring.Result{}, false
}

// 256-bit difference hash tolerates JPEG re-encoding in the browser while
// retaining enough detail to reject unrelated photos with a claimed digest.
func visualHash(jpegBytes []byte) ([4]uint64, error) {
	img, _, err := image.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		return [4]uint64{}, err
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	var hash [4]uint64
	for y := 0; y < 16; y++ {
		py := bounds.Min.Y + (2*y+1)*h/(2*16)
		for x := 0; x < 16; x++ {
			left := bounds.Min.X + (2*x+1)*w/(2*17)
			right := bounds.Min.X + (2*(x+1)+1)*w/(2*17)
			if gray(img.At(left, py)) > gray(img.At(right, py)) {
				index := y*16 + x
				hash[index/64] |= 1 << (index % 64)
			}
		}
	}
	return hash, nil
}

func gray(c interface{ RGBA() (r, g, b, a uint32) }) uint64 {
	r, g, b, _ := c.RGBA()
	return 299*uint64(r) + 587*uint64(g) + 114*uint64(b)
}

func visualDistance(a, b [4]uint64) int {
	return bits.OnesCount64(a[0]^b[0]) + bits.OnesCount64(a[1]^b[1]) + bits.OnesCount64(a[2]^b[2]) + bits.OnesCount64(a[3]^b[3])
}
