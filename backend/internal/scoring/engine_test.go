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
