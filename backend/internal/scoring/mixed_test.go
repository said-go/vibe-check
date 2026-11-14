package scoring

import (
	"github.com/said-go/vibe-check/backend/internal/analysis"
	"strings"
	"testing"
)

func TestAlternativeCombinationNeedsVisibleAbsenceAndNonMuscularBuild(t *testing.T) {
	base := analysis.VisualFeatures{ContractVersion: analysis.ContractVersion, PersonCount: 1, Confidence: 90, ImageQuality: 90, OutfitVisibility: 80, HairBelowEars: true}
	for _, tc := range []struct {
		name                          string
		absent, nonmuscular, muscular bool
		score                         int
	}{
		{"unknown face and body", false, false, false, 3},
		{"face only", true, false, false, 3},
		{"body only", false, true, false, 3},
		{"full combination", true, true, false, 43},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			f.BeardAbsentVisible = tc.absent
			f.NonMuscularVisible = tc.nonmuscular
			f.MuscularVisible = tc.muscular
			r, err := NewEngine().Evaluate(f)
			if err != nil {
				t.Fatal(err)
			}
			if r.PrimaryCategory.Score != tc.score {
				t.Fatalf("got %+v", r)
			}
		})
	}
	f := base
	f.BeardAbsentVisible = true
	f.MuscularVisible = true
	r, err := NewEngine().Evaluate(f)
	if err != nil {
		t.Fatal(err)
	}
	if r.PrimaryCategory.Code != "muscular" || len(r.Components) != 0 {
		t.Fatalf("long hair alone blended with muscles: %+v", r)
	}
}

func TestIndependentAlternativeAndMusclesBlend(t *testing.T) {
	f := baseFeatures()
	f.Signals.Alternative = 100
	f.Originality = 100
	f.AccessoryIntensity = 100
	f.MuscularVisible = true
	e := NewEngine()
	r, err := e.Evaluate(f)
	if err != nil {
		t.Fatal(err)
	}
	if r.PrimaryCategory.Title != "Нефор-качок" || len(r.Components) != 2 || len(r.Explanations) != 2 {
		t.Fatalf("unexpected blend %+v", r)
	}
	if !strings.Contains(strings.Join(r.Explanations, " "), "мускулатура") {
		t.Fatal("missing muscle explanation")
	}
	f.MuscularVisible = false
	plain, _ := e.Evaluate(f)
	if plain.Score != r.Score {
		t.Fatal("muscles changed clothing score")
	}
}

func TestMusclesWithoutClothesHaveNoOutfitRating(t *testing.T) {
	f := baseFeatures()
	f.OutfitVisibility = 0
	f.MuscularVisible = true
	r, err := NewEngine().Evaluate(f)
	if err != nil {
		t.Fatal(err)
	}
	if r.PrimaryCategory.Code != "muscular" || r.AssessmentScope != "portrait" || r.Score != 0 || r.Metrics != (Metrics{}) {
		t.Fatalf("unexpected body-only result %+v", r)
	}
	if len(r.Components) != 0 {
		t.Fatal("mixed unseen clothing")
	}
	f.PersonCount = 2
	r, _ = NewEngine().Evaluate(f)
	if r.Status != "low_confidence" || len(r.Components) != 0 {
		t.Fatal("group got a blend")
	}
}

func TestBlendsKeepPriorityAndAtMostTwoParts(t *testing.T) {
	f := baseFeatures()
	f.PlayfulGrimace = true
	f.MuscularVisible = true
	f.SkullcapVisible = true
	f.BeardVisible = true
	r, err := NewEngine().Evaluate(f)
	if err != nil {
		t.Fatal(err)
	}
	if r.PrimaryCategory.Code != "clown" || len(r.Components) != 2 || r.Components[1].Code != "mula" {
		t.Fatalf("priority changed %+v", r)
	}
}

func TestRenamedCategories(t *testing.T) {
	e := NewEngine()
	for code, want := range map[string]string{"urban_classic": "Бизнесмен", "healthy_casual": "Стиляга", "streetwear": "Абушка"} {
		if e.ruleByCode(code).Title != want {
			t.Fatalf("old title for %s", code)
		}
	}
}

func TestTwoClothingStylesNeedStrongAndCloseEvidence(t *testing.T) {
	f := baseFeatures()
	f.Signals.Goth = 90
	f.Darkness = 90
	f.Signals.Alternative = 100
	f.Originality = 100
	f.AccessoryIntensity = 100
	f.Layering = 100
	r, err := NewEngine().Evaluate(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Components) != 2 || r.Components[0].Code != "goth" || r.Components[1].Code != "alternative" {
		t.Fatalf("expected two evidenced styles: %+v", r)
	}
	f.Signals.Alternative = 50
	r, err = NewEngine().Evaluate(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Components) != 0 {
		t.Fatalf("weak runner up blended: %+v", r)
	}
}
