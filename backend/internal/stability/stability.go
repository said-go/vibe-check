// Package stability prepares local image variants and compares saved features.
// Neither operation calls an AI provider or reads credentials.
package stability

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"

	"github.com/said-go/vibe-check/backend/internal/analysis"
	"github.com/said-go/vibe-check/backend/internal/evaluation"
	"github.com/said-go/vibe-check/backend/internal/imageproc"
)

var names = [...]string{"baseline", "jpeg-45", "half-size", "brightness-80", "brightness-120"}

// Prepare validates and encodes everything before exclusively creating output.
// Failures after creation leave a partial directory; never reuse it for collection.
func Prepare(r io.Reader, expected evaluation.Expected, output string) error {
	manifest := evaluation.ImageManifest{Cases: []evaluation.ImageCase{{Path: "baseline.jpg", Expected: expected}}}
	check, _ := json.Marshal(manifest)
	if _, err := evaluation.DecodeImages(bytes.NewReader(check)); err != nil {
		return errors.New("invalid expectation")
	}
	raw, err := io.ReadAll(io.LimitReader(r, (10<<20)+1))
	if err != nil || len(raw) > 10<<20 {
		return errors.New("cannot read image within 10 MiB limit")
	}
	clean, err := imageproc.New().Sanitize(bytes.NewReader(raw), "")
	if err != nil {
		return errors.New("invalid JPEG/PNG or dimensions")
	}
	src, err := jpeg.Decode(bytes.NewReader(clean.JPEG))
	if err != nil {
		return errors.New("cannot decode sanitized image")
	}
	// Bound generated artifacts while preserving aspect ratio. Very narrow inputs
	// retain the minimum valid short edge instead of producing invalid variants.
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if max(w, h) > 1600 {
		scale := max(1600.0/float64(max(w, h)), 128.0/float64(min(w, h)))
		w, h = max(128, int(float64(w)*scale)), max(128, int(float64(h)*scale))
	}
	src = transform(src, w, h, 100)
	files := make([][]byte, len(names))
	manifest.Cases = nil
	for i, name := range names {
		img, quality := src, 88
		switch i {
		case 1:
			quality = 45
		case 2:
			scale := max(0.5, 128.0/float64(min(w, h)))
			img = transform(src, max(128, int(float64(w)*scale)), max(128, int(float64(h)*scale)), 100)
		case 3:
			img = transform(src, w, h, 80)
		case 4:
			img = transform(src, w, h, 120)
		}
		var b bytes.Buffer
		if jpeg.Encode(&b, img, &jpeg.Options{Quality: quality}) != nil {
			return errors.New("cannot encode variant")
		}
		files[i] = b.Bytes()
		if len(files[i]) > 10<<20 {
			return errors.New("generated variant exceeds collector 10 MiB limit")
		}
		manifest.Cases = append(manifest.Cases, evaluation.ImageCase{Path: name + ".jpg", Expected: expected})
	}
	encoded, _ := json.MarshalIndent(manifest, "", "  ")
	if os.Mkdir(output, 0700) != nil {
		return errors.New("cannot create new output directory; parent must exist and target must not exist")
	}
	for i, name := range names {
		if write(filepath.Join(output, name+".jpg"), files[i]) != nil {
			return errors.New("cannot write variants; discard partial output directory")
		}
	}
	if write(filepath.Join(output, "manifest.json"), encoded) != nil {
		return errors.New("cannot write manifest; discard partial output directory")
	}
	return nil
}

func write(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// Deterministic nearest-neighbor reduction and integer RGB brightness, no crop.
func transform(src image.Image, w, h, percent int) image.Image {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	b := src.Bounds()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, blue, _ := src.At(b.Min.X+x*b.Dx()/w, b.Min.Y+y*b.Dy()/h).RGBA()
			channel := func(v uint32) uint8 { return uint8(min(255, (int(v>>8)*percent+50)/100)) }
			out.SetRGBA(x, y, color.RGBA{channel(r), channel(g), channel(blue), 255})
		}
	}
	return out
}

type Difference struct {
	Field    string `json:"field"`
	Baseline any    `json:"baseline"`
	Variant  any    `json:"variant"`
}
type Variant struct {
	ID                string              `json:"id"`
	Name              string              `json:"variant"`
	Actual            evaluation.Expected `json:"actual"`
	StatusChanged     bool                `json:"status_changed"`
	PrimaryChanged    bool                `json:"primary_changed"`
	ComponentsChanged bool                `json:"ordered_components_changed"`
	ReasonChanged     bool                `json:"reason_changed"`
	Differences       []Difference        `json:"feature_differences"`
}
type Report struct {
	Kind     string              `json:"kind"`
	Stable   bool                `json:"verdict_stable"`
	Baseline evaluation.Expected `json:"baseline"`
	Variants []Variant           `json:"variants"`
}

// Compare requires the complete unmodified five-case collector ordering.
// IDs/counts cannot prove provenance: the operator must select the correct run.
func Compare(d evaluation.Dataset) (Report, error) {
	out := Report{Kind: d.Kind, Stable: true, Variants: []Variant{}}
	if (d.Kind != "synthetic" && d.Kind != "reviewed_features") || len(d.Cases) != len(names) {
		return out, errors.New("expected one complete five-variant dataset")
	}
	ordered := evaluation.Dataset{Kind: d.Kind, Cases: make([]evaluation.Case, len(names))}
	seen := map[string]bool{}
	for _, c := range d.Cases {
		index := -1
		for i := range names {
			if c.ID == fmt.Sprintf("case-%04d", i+1) {
				index = i
			}
		}
		if index < 0 || seen[c.ID] {
			return out, errors.New("expected exactly case-0001 through case-0005")
		}
		seen[c.ID] = true
		ordered.Cases[index] = c
	}
	canonical := make([]map[string]any, len(names))
	for i, c := range ordered.Cases {
		if !reflect.DeepEqual(c.Expected, ordered.Cases[0].Expected) {
			return out, errors.New("variant expectations must match baseline")
		}
		f, err := analysis.DecodeFeatures(bytes.NewReader(c.Features))
		if err != nil {
			return out, errors.New("invalid saved feature contract")
		}
		encoded, _ := json.Marshal(f)
		var fields map[string]any
		_ = json.Unmarshal(encoded, &fields)
		canonical[i] = flatten(fields)
	}
	scored := evaluation.Run(ordered)
	if scored.Invalid != 0 {
		return out, errors.New("cannot score saved feature contracts")
	}
	out.Baseline = scored.Outcomes[0].Actual
	for i := 1; i < len(names); i++ {
		actual := scored.Outcomes[i].Actual
		v := Variant{ID: ordered.Cases[i].ID, Name: names[i], Actual: actual, Differences: []Difference{}, StatusChanged: actual.Status != out.Baseline.Status, PrimaryChanged: actual.Category != out.Baseline.Category, ComponentsChanged: !slices.Equal(actual.Components, out.Baseline.Components), ReasonChanged: actual.Reason != out.Baseline.Reason}
		keys := map[string]bool{}
		for k := range canonical[0] {
			keys[k] = true
		}
		for k := range canonical[i] {
			keys[k] = true
		}
		var sorted []string
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			if !reflect.DeepEqual(canonical[0][k], canonical[i][k]) {
				before, after := canonical[0][k], canonical[i][k]
				// Color labels are provider-controlled strings; disclose only that
				// they differ, never echo free text into reports.
				if k == "dominant_colors" {
					before, after = "[redacted]", "[redacted]"
				}
				v.Differences = append(v.Differences, Difference{k, before, after})
			}
		}
		if v.StatusChanged || v.PrimaryChanged || v.ComponentsChanged || v.ReasonChanged {
			out.Stable = false
		}
		out.Variants = append(out.Variants, v)
	}
	return out, nil
}

func flatten(fields map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range fields {
		if nested, ok := v.(map[string]any); ok {
			for child, value := range nested {
				out[k+"."+child] = value
			}
		} else {
			out[k] = v
		}
	}
	return out
}
