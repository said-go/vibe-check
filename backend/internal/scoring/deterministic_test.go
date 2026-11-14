package scoring

import (
	"reflect"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

func TestCategoryRoundingDoesNotChangeVerdictOnIdenticalEvidence(t *testing.T) {
	// This exact case alternated between retro_90s (47) and streetwear
	// (47 or 48) when fractional weights were summed in random map order.
	f := baseFeatures()
	f.ColorHarmony, f.Darkness, f.Formality, f.Sportiness = 3, 36, 3, 6
	f.Originality, f.OutfitCoherence, f.AccessoryIntensity, f.Layering = 58, 38, 37, 65
	f.Signals = analysis.SubcultureSignals{Goth: 35, Alternative: 22, Streetwear: 59, Classic: 94, SportChic: 78, Retro90s: 56}
	e := NewEngine()
	first, err := e.Evaluate(f)
	if err != nil {
		t.Fatal(err)
	}
	if first.PrimaryCategory.Code != "streetwear" || first.PrimaryCategory.Score != 48 || first.SecondaryCategory.Code != "retro_90s" || first.SecondaryCategory.Score != 47 {
		t.Fatalf("incorrect exact rounding: %+v", first)
	}
	for i := 0; i < 1000; i++ {
		next, err := e.Evaluate(f)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(first, next) {
			t.Fatalf("same evidence changed result at iteration %d: %+v", i, next)
		}
	}
}

func TestCategoryWeightsStayAtOneHundredPercent(t *testing.T) {
	for _, r := range NewEngine().rules {
		total := 0
		for _, weight := range r.Weights {
			if weight <= 0 {
				t.Fatalf("non-positive weight for %s", r.Code)
			}
			total += weight
		}
		if total != 100 {
			t.Fatalf("%s weights total %d, want 100", r.Code, total)
		}
	}
}

func TestExactRoundingAtStrongStyleThreshold(t *testing.T) {
	e := NewEngine()
	f := baseFeatures()
	f.Signals.Goth, f.Darkness, f.OutfitCoherence = 100, 0, 97
	// 45 + 14.55 + 4.90 = 64.45; just below the strong-style cutoff.
	f.Layering = 49
	score := categoryScore(f, e.ruleByCode("goth"))
	if score != 64 || strongOutfitStyle(f, CategoryScore{Code: "goth", Score: score}) {
		t.Fatal("64.45 must remain below strong-style cutoff")
	}
	// 45 + 14.55 + 5 = 64.55 rounds up to 65 and becomes strong.
	f.Layering = 50
	score = categoryScore(f, e.ruleByCode("goth"))
	if score != 65 || !strongOutfitStyle(f, CategoryScore{Code: "goth", Score: score}) {
		t.Fatal("64.55 must qualify at 65")
	}
	for _, tc := range []struct{ raw, want int }{{0, 0}, {4749, 47}, {4750, 48}, {4751, 48}, {6449, 64}, {6450, 65}, {10000, 100}, {14000, 100}} {
		if got := roundWeightedScore(tc.raw); got != tc.want {
			t.Fatalf("%d hundredths: got %d want %d", tc.raw, got, tc.want)
		}
	}
}

func TestOverallRatingUsesExactHalfUpRounding(t *testing.T) {
	f := baseFeatures()
	f.OutfitCoherence, f.ColorHarmony, f.Originality = 1, 1, 3 // exactly 1.50
	r, err := NewEngine().Evaluate(f)
	if err != nil {
		t.Fatal(err)
	}
	if r.Score != 2 {
		t.Fatalf("overall score 1.50 must round to 2, got %d", r.Score)
	}
}
