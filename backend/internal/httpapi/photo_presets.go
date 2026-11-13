package httpapi

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math/bits"

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

var photoPresets = []photoPreset{
	{
		originalSHA256: "f1c8dc5f938b95675b54e169d8139c4166f61e2b1fe807114fcfd44ba64d7624",
		visualHash:     [4]uint64{0xba26cc62a5482852, 0x3a130c6bf1a1c822, 0xfd53ac8ebc3ece67, 0x55b56d34af42acb3},
		result: scoring.Result{
			Status: "completed", PrimaryCategory: scoring.CategoryScore{Code: "chushpan", Title: "Чушпан", Score: 100},
			Level: "base", LevelTitle: "Уже можно выходить", Score: 58, Confidence: 100,
			Metrics:         scoring.Metrics{Coherence: 55, Originality: 60, ColorHarmony: 62},
			Summary:         "Самому не стыдно было присылать такое фото!",
			Recommendations: []string{"На следующем фото дай одежде чуть больше места в кадре"},
		},
	},
	{
		originalSHA256: "4b4941958438cac8fb68040995e00f70172f527b6db0c75ecae818f7c6238173",
		visualHash:     [4]uint64{0xe00e8a4b9d7ee3d9, 0x8006a26ec31aec0e, 0xcb4c96eaa255a802, 0x6d4186b17520ce0d},
		result: scoring.Result{
			Status: "completed", PrimaryCategory: scoring.CategoryScore{Code: "pupsik", Title: "Пупсик", Score: 100},
			Level: "styler", LevelTitle: "Стиляга", Score: 78, Confidence: 100,
			Metrics:         scoring.Metrics{Coherence: 78, Originality: 72, ColorHarmony: 82},
			Summary:         "Мягкий человек с доброй душой, но очень наивный",
			Recommendations: []string{"Один яркий аксессуар — и образ заиграет ещё сильнее"},
		},
	},
}

func matchPhotoPreset(originalSHA256 string, sanitizedJPEG []byte) (scoring.Result, bool) {
	if len(originalSHA256) != 64 {
		return scoring.Result{}, false
	}
	for _, preset := range photoPresets {
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
