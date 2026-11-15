package scoring

import (
	"github.com/said-go/vibe-check/backend/internal/analysis"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMixedReportsKeepEveryRequiredPortraitCue(t *testing.T) {
	for _, tc := range []struct {
		code         string
		setup        func(*analysis.VisualFeatures)
		promoteOther func(*analysis.VisualFeatures)
		cues         []string
	}{
		{"mula", func(f *analysis.VisualFeatures) { f.SkullcapVisible, f.BeardVisible = true, true },
			func(f *analysis.VisualFeatures) { f.PlayfulGrimace = true }, []string{"тюбетейка", "борода"}},
		{"cool_pose", func(f *analysis.VisualFeatures) { f.SeriousIndexPose = true },
			func(f *analysis.VisualFeatures) { f.SkullcapVisible, f.BeardVisible = true, true }, []string{"палец", "Серьёзное"}},
		{"pupsik", func(f *analysis.VisualFeatures) { f.GentleSmile, f.RelaxedHeadTilt = true, true },
			func(f *analysis.VisualFeatures) {
				f.ShortHairOrBaldVisible, f.BeardVisible, f.HeadScarVisible = true, true, true
			}, []string{"улыбка", "наклонена", "брови"}},
	} {
		for _, secondary := range []bool{false, true} {
			t.Run(tc.code+map[bool]string{false: "/primary", true: "/secondary"}[secondary], func(t *testing.T) {
				f := baseFeatures()
				f.OutfitVisibility, f.MuscularVisible = 0, true
				tc.setup(&f)
				if secondary {
					tc.promoteOther(&f)
				}
				r, err := NewEngine().Evaluate(f)
				if err != nil {
					t.Fatal(err)
				}
				index := 0
				if secondary {
					index = 1
				}
				if len(r.Components) != 2 || r.Components[index].Code != tc.code || len(r.Explanations) != 2 {
					t.Fatalf("unexpected blend: %+v", r)
				}
				for _, cue := range tc.cues {
					if !strings.Contains(r.Explanations[index], cue) {
						t.Fatalf("missing %q in %q", cue, r.Explanations[index])
					}
				}
				if utf8.RuneCountInString(r.Explanations[index]) > 300 {
					t.Fatal("explanation will be truncated by history normalizer")
				}
			})
		}
	}
}

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
