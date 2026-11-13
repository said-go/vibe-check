package scoring

import (
	"reflect"
	"strings"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

func baseFeatures() analysis.VisualFeatures {
	return analysis.VisualFeatures{
		ContractVersion: analysis.ContractVersion, DominantColors: []string{"black", "gray"}, ColorHarmony: 82,
		Darkness: 76, Formality: 68, Sportiness: 41, Originality: 73, OutfitCoherence: 88,
		AccessoryIntensity: 57, Layering: 44, TraditionalElements: 12, ImageQuality: 91,
		OutfitVisibility: 90, Confidence: 86, PersonCount: 1,
		Signals: analysis.SubcultureSignals{Goth: 38, Alternative: 22, Streetwear: 46, Classic: 75, SportChic: 51},
	}
}

func TestEvaluateIsDeterministic(t *testing.T) {
	engine := NewEngine()
	first, err := engine.Evaluate(baseFeatures())
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.Evaluate(baseFeatures())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("results differ: %#v %#v", first, second)
	}
	if first.PrimaryCategory.Code != "healthy_casual" {
		t.Fatalf("unexpected primary category %q", first.PrimaryCategory.Code)
	}
}

func TestGothIsAStyleNotALowLevel(t *testing.T) {
	features := baseFeatures()
	features.Signals.Goth = 98
	features.Darkness = 96
	features.Signals.Classic = 5
	features.OutfitCoherence = 94
	features.Originality = 90
	result, err := NewEngine().Evaluate(features)
	if err != nil {
		t.Fatal(err)
	}
	if result.PrimaryCategory.Code != "goth" {
		t.Fatalf("expected goth, got %q", result.PrimaryCategory.Code)
	}
	if result.Level == "experimenter" || result.Level == "base" {
		t.Fatalf("style incorrectly lowered the level: %q", result.Level)
	}
}

func TestLowConfidenceDoesNotReturnCategory(t *testing.T) {
	features := baseFeatures()
	features.OutfitVisibility = 0
	features.Confidence = 0
	features.OutfitCoherence = 0
	features.ColorHarmony = 0
	features.Originality = 0
	result, err := NewEngine().Evaluate(features)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "low_confidence" || result.PrimaryCategory.Code != "" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestPartialViewStillReturnsOutfitResult(t *testing.T) {
	features := baseFeatures()
	features.OutfitVisibility = 40
	features.ImageQuality = 30
	features.Confidence = 35
	result, err := NewEngine().Evaluate(features)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.PrimaryCategory.Code == "" {
		t.Fatalf("partial outfit should produce a result: %+v", result)
	}
	if result.Score != 82 {
		t.Fatalf("image quality must not lower outfit score: %d", result.Score)
	}
	if !strings.Contains(result.Summary, "видимой части образа") {
		t.Fatalf("limited view needs a visible qualifier: %q", result.Summary)
	}
}

func TestMulaRequiresVisibleSkullcapAndBeard(t *testing.T) {
	engine := NewEngine()
	for _, tc := range []struct {
		name, want      string
		skullcap, beard bool
	}{
		{"both", "mula", true, true},
		{"skullcap only", "healthy_casual", true, false},
		{"beard only", "healthy_casual", false, true},
		{"neither", "healthy_casual", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			features := baseFeatures()
			features.SkullcapVisible = tc.skullcap
			features.BeardVisible = tc.beard
			result, err := engine.Evaluate(features)
			if err != nil {
				t.Fatal(err)
			}
			if result.PrimaryCategory.Code != tc.want {
				t.Fatalf("category = %q, want %q", result.PrimaryCategory.Code, tc.want)
			}
			if tc.want == "mula" && (result.PrimaryCategory.Title != "Мула" || result.SecondaryCategory.Code != "healthy_casual") {
				t.Fatalf("unexpected category details: %+v", result)
			}
		})
	}
}

func TestHairAndPiercingAreSupportingCues(t *testing.T) {
	engine := NewEngine()
	features := baseFeatures()
	features.UnnaturalHairColor = true
	features.PiercingVisible = true
	features.HairBelowEars = true
	features.HairOverEyes = true
	result, err := engine.Evaluate(features)
	if err != nil {
		t.Fatal(err)
	}
	if result.PrimaryCategory.Code != "healthy_casual" {
		t.Fatalf("appearance cues alone changed category: %q", result.PrimaryCategory.Code)
	}

	features = baseFeatures()
	features.Signals.Alternative = 55
	features.Signals.Classic = 35
	features.Signals.Streetwear = 20
	features.Signals.Goth = 10
	features.Signals.SportChic = 15
	features.TraditionalElements = 0
	features.OutfitCoherence = 65
	features.ColorHarmony = 60
	features.Sportiness = 25
	features.Formality = 20
	features.Darkness = 35
	withoutCues, err := engine.Evaluate(features)
	if err != nil {
		t.Fatal(err)
	}
	features.HairBelowEars = true
	withLongHair, err := engine.Evaluate(features)
	if err != nil {
		t.Fatal(err)
	}
	features.UnnaturalHairColor = true
	features.PiercingVisible = true
	withSeveralCues, err := engine.Evaluate(features)
	if err != nil {
		t.Fatal(err)
	}
	if withoutCues.PrimaryCategory.Code == "alternative" || withLongHair.PrimaryCategory.Code == "alternative" || withSeveralCues.PrimaryCategory.Code != "alternative" {
		t.Fatalf("cues should support, not dictate: no=%q long=%q several=%q", withoutCues.PrimaryCategory.Code, withLongHair.PrimaryCategory.Code, withSeveralCues.PrimaryCategory.Code)
	}
}
