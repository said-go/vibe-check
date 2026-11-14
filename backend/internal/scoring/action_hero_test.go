package scoring

import (
	"strings"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

func actionHeroFeatures() analysis.VisualFeatures {
	f := baseFeatures()
	f.ShortHairOrBaldVisible, f.BeardVisible, f.HeadScarVisible = true, true, true
	return f
}

func TestActionHeroRequiresAllThreeVisibleCues(t *testing.T) {
	for mask := 0; mask < 8; mask++ {
		f := baseFeatures()
		f.ShortHairOrBaldVisible, f.BeardVisible, f.HeadScarVisible = mask&1 != 0, mask&2 != 0, mask&4 != 0
		r, err := NewEngine().Evaluate(f)
		if err != nil {
			t.Fatal(err)
		}
		if (r.PrimaryCategory.Code == "action_hero") != (mask == 7) {
			t.Fatalf("mask %d: %+v", mask, r)
		}
		if mask == 7 && (len(r.Explanations) != 1 || !strings.Contains(r.Explanations[0], "кинообраза")) {
			t.Fatal("missing cinematic explanation")
		}
	}
}

func TestActionHeroBlendsWithIndependentCategories(t *testing.T) {
	for _, tc := range []struct {
		name, title string
		change      func(*analysis.VisualFeatures)
	}{
		{"muscular", "Герой боевика-качок", func(f *analysis.VisualFeatures) { f.MuscularVisible = true }},
		{"alternative", "Герой боевика-нефор", func(f *analysis.VisualFeatures) {
			f.Signals.Alternative = 100
			f.Originality = 100
			f.AccessoryIntensity = 100
		}},
		{"grimace", "Клоун-герой боевика", func(f *analysis.VisualFeatures) { f.PlayfulGrimace = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := actionHeroFeatures()
			tc.change(&f)
			r, err := NewEngine().Evaluate(f)
			if err != nil {
				t.Fatal(err)
			}
			if r.PrimaryCategory.Title != tc.title || len(r.Components) != 2 || len(r.Explanations) != 2 {
				t.Fatalf("wrong blend: %+v", r)
			}
			f.ShortHairOrBaldVisible, f.HeadScarVisible = false, false
			without, err := NewEngine().Evaluate(f)
			if err != nil {
				t.Fatal(err)
			}
			if r.Score != without.Score {
				t.Fatal("cinematic cue changed clothing rating")
			}
		})
	}
}

func TestActionHeroPortraitAndSafetyGates(t *testing.T) {
	f := actionHeroFeatures()
	f.OutfitVisibility = 0
	r, err := NewEngine().Evaluate(f)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "completed" || r.AssessmentScope != "portrait" || r.Score != 0 || r.Metrics != (Metrics{}) || r.PrimaryCategory.Code != "action_hero" {
		t.Fatalf("wrong portrait result: %+v", r)
	}
	for _, change := range []func(*analysis.VisualFeatures){func(f *analysis.VisualFeatures) { f.PersonCount = 0 }, func(f *analysis.VisualFeatures) { f.PersonCount = 2 }, func(f *analysis.VisualFeatures) { f.Confidence = 19 }} {
		f := actionHeroFeatures()
		f.MuscularVisible = true
		change(&f)
		r, err := NewEngine().Evaluate(f)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != "low_confidence" || r.PrimaryCategory.Code != "" || len(r.Components) != 0 {
			t.Fatalf("gate bypassed: %+v", r)
		}
	}
}
