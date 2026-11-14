package stability

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/evaluation"
	"github.com/said-go/vibe-check/backend/internal/imageproc"
)

func syntheticImage(t *testing.T, width, height int) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			im.SetRGBA(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 100, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestPrepareDeterministicExclusiveCollectorCompatible(t *testing.T) {
	raw := syntheticImage(t, 512, 256)
	expected := evaluation.Expected{Status: "completed", Category: "goth"}
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, dir := range []string{a, b} {
		if err := Prepare(bytes.NewReader(raw), expected, dir); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(a, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := evaluation.DecodeImages(bytes.NewReader(manifest))
	if err != nil || len(m.Cases) != 5 {
		t.Fatalf("manifest: %v", err)
	}
	for i, c := range m.Cases {
		if c.Path != names[i]+".jpg" || !reflect.DeepEqual(c.Expected, expected) {
			t.Fatal("unexpected manifest")
		}
		one, _ := os.ReadFile(filepath.Join(a, c.Path))
		two, _ := os.ReadFile(filepath.Join(b, c.Path))
		if !bytes.Equal(one, two) {
			t.Fatal("non-deterministic")
		}
		clean, err := imageproc.New().Sanitize(bytes.NewReader(one), "")
		if err != nil {
			t.Fatal(err)
		}
		if i == 2 && (clean.Width != 256 || clean.Height != 128) {
			t.Fatal("wrong half dimensions")
		}
	}
	if err := Prepare(bytes.NewReader(raw), expected, a); err == nil {
		t.Fatal("overwrote directory")
	}
	after, _ := os.ReadFile(filepath.Join(a, "manifest.json"))
	if !bytes.Equal(manifest, after) {
		t.Fatal("modified existing directory")
	}
}
func TestPrepareValidationAndMinimumSize(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "new")
	if err := Prepare(bytes.NewReader([]byte("not an image")), evaluation.Expected{Status: "completed", Category: "goth"}, target); err == nil {
		t.Fatal("accepted bad image")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("created directory before validation")
	}
	if err := Prepare(bytes.NewReader(syntheticImage(t, 128, 128)), evaluation.Expected{Status: "completed", Category: "goth"}, target); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(target, "half-size.jpg"))
	config, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width != 128 || config.Height != 128 {
		t.Fatal("minimum edge not preserved")
	}
}

func TestBoundaryAspectRatios(t *testing.T) {
	for _, size := range [][2]int{{128, 256}, {129, 1000}, {128, 4000}, {512, 2048}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "variants")
			if err := Prepare(bytes.NewReader(syntheticImage(t, size[0], size[1])), evaluation.Expected{Status: "completed", Category: "goth"}, target); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"baseline", "half-size"} {
				raw, _ := os.ReadFile(filepath.Join(target, name+".jpg"))
				cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				// Up to one output pixel of integer rounding is expected.
				if cfg.Width < 128 || cfg.Height < 128 || abs(cfg.Width*size[1]-cfg.Height*size[0]) > max(size[0], size[1]) {
					t.Fatalf("distorted %v to %dx%d", size, cfg.Width, cfg.Height)
				}
			}
		})
	}
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestColorDifferencesAreRedacted(t *testing.T) {
	d := saved(t)
	var f map[string]any
	_ = json.Unmarshal(d.Cases[1].Features, &f)
	f["dominant_colors"] = []string{"private-person"}
	d.Cases[1].Features, _ = json.Marshal(f)
	r, err := Compare(d)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	if bytes.Contains(b, []byte("private-person")) || len(r.Variants[0].Differences) != 1 {
		t.Fatal("free text leaked or difference omitted")
	}
}
func saved(t *testing.T) evaluation.Dataset {
	t.Helper()
	f, err := os.Open("../evaluation/testdata/synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := evaluation.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	out := evaluation.Dataset{Kind: "synthetic"}
	for i := range names {
		c := d.Cases[0]
		c.ID = fmt.Sprintf("case-%04d", i+1)
		c.Features = append([]byte(nil), c.Features...)
		out.Cases = append(out.Cases, c)
	}
	return out
}
func TestCompareSavedFeatures(t *testing.T) {
	d := saved(t)
	r, err := Compare(d)
	if err != nil || !r.Stable || len(r.Variants) != 4 {
		t.Fatalf("stable comparison: %+v %v", r, err)
	}
	var f map[string]any
	_ = json.Unmarshal(d.Cases[1].Features, &f)
	f["person_count"] = 0
	d.Cases[1].Features, _ = json.Marshal(f)
	r, err = Compare(d)
	if err != nil || r.Stable || !r.Variants[0].StatusChanged {
		t.Fatalf("missed verdict change: %+v %v", r, err)
	}
	if len(r.Variants[0].Differences) != 1 || r.Variants[0].Differences[0].Field != "person_count" {
		t.Fatal("wrong feature diff")
	}
	// File ordering does not alter the baseline mapping.
	d.Cases[0], d.Cases[4] = d.Cases[4], d.Cases[0]
	s, err := Compare(d)
	if err != nil || !reflect.DeepEqual(r, s) {
		t.Fatal("order-dependent mapping")
	}
}
func TestCompareRejectsPartialForeignAndInvalidInputs(t *testing.T) {
	for _, mutation := range []func(*evaluation.Dataset){
		func(d *evaluation.Dataset) { d.Cases = d.Cases[:4] },
		func(d *evaluation.Dataset) { d.Cases[4].ID = "foreign" },
		func(d *evaluation.Dataset) { d.Cases[4].ID = d.Cases[0].ID },
		func(d *evaluation.Dataset) { d.Kind = "unknown" },
		func(d *evaluation.Dataset) { d.Cases[1].Expected.Category = "goth" },
		func(d *evaluation.Dataset) { d.Cases[1].Features = json.RawMessage(`{}`) },
	} {
		d := saved(t)
		mutation(&d)
		if _, err := Compare(d); err == nil {
			t.Fatal("accepted invalid comparison")
		}
	}
}

func TestCompareFeaturesCanChangeWithoutVerdictChange(t *testing.T) {
	d := saved(t)
	var f map[string]any
	if err := json.Unmarshal(d.Cases[1].Features, &f); err != nil {
		t.Fatal(err)
	}
	f["image_quality"] = 79
	d.Cases[1].Features, _ = json.Marshal(f)
	r, err := Compare(d)
	if err != nil || !r.Stable {
		t.Fatalf("feature change should preserve verdict: %+v %v", r, err)
	}
	v := r.Variants[0]
	if v.StatusChanged || v.PrimaryChanged || v.ComponentsChanged || v.ReasonChanged || len(v.Differences) != 1 || v.Differences[0].Field != "image_quality" || v.Differences[0].Baseline != float64(80) || v.Differences[0].Variant != float64(79) {
		t.Fatalf("wrong diff: %+v", v)
	}
}

func TestCompareDetectsSecondaryComponentChange(t *testing.T) {
	f, err := os.Open("../evaluation/testdata/synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fixture, err := evaluation.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	var single, mixed evaluation.Case
	for _, c := range fixture.Cases {
		switch c.ID {
		case "synthetic-action-hero":
			single = c
		case "synthetic-action-hero-muscular":
			mixed = c
		}
	}
	if single.ID == "" || mixed.ID == "" {
		t.Fatal("missing synthetic fixtures")
	}
	d := saved(t)
	for i := range d.Cases {
		d.Cases[i].Features = single.Features
		d.Cases[i].Expected = single.Expected
	}
	d.Cases[1].Features = mixed.Features
	r, err := Compare(d)
	if err != nil {
		t.Fatal(err)
	}
	v := r.Variants[0]
	if r.Stable || v.StatusChanged || v.PrimaryChanged || !v.ComponentsChanged || r.Baseline.Category != "action_hero" || !reflect.DeepEqual(v.Actual.Components, []string{"action_hero", "muscular"}) {
		t.Fatalf("missed secondary component: %+v", r)
	}
}
