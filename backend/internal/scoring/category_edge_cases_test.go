package scoring

import (
	"strings"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

// Missing evidence must not leak through a secondary label or mixed explanation.
func TestIncompleteCombinationsNeverAppearInResult(t *testing.T) {
	for _, tc := range []struct {
		name, excluded, claim string
		set                   func(*analysis.VisualFeatures)
	}{
		{"smile without tilt", "pupsik", "наклонена", func(f *analysis.VisualFeatures) { f.GentleSmile = true }},
		{"scar without beard", "action_hero", "шрам", func(f *analysis.VisualFeatures) {
			f.ShortHairOrBaldVisible, f.HeadScarVisible = true, true
		}},
		{"long hair and unknown body", "alternative", "сработало сочетание", func(f *analysis.VisualFeatures) {
			f.HairBelowEars, f.BeardAbsentVisible = true, true
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := baseFeatures()
			// A separately evidenced primary category lets incomplete combinations
			// compete for the second slot without becoming the primary themselves.
			f.MuscularVisible = true
			if tc.excluded == "alternative" {
				f.MuscularVisible = false
				f.PlayfulGrimace = true
			}
			tc.set(&f)
			for _, visibility := range []int{90, 0} {
				f.OutfitVisibility = visibility
				r, err := NewEngine().Evaluate(f)
				if err != nil {
					t.Fatal(err)
				}
				if r.Status != "completed" {
					t.Fatalf("independent visible cue rejected: %+v", r)
				}
				categories := append([]CategoryScore{r.PrimaryCategory, r.SecondaryCategory}, r.Components...)
				for _, c := range categories {
					if c.Code == tc.excluded {
						t.Fatalf("visibility %d: incomplete category leaked: %+v", visibility, r)
					}
				}
				if strings.Contains(strings.Join(r.Explanations, " "), tc.claim) {
					t.Fatalf("unsupported explanation: %v", r.Explanations)
				}
			}
		})
	}
}

func TestAlternativePortraitRequiresExplicitNonMuscularEvidence(t *testing.T) {
	f := baseFeatures()
	f.OutfitVisibility = 0
	f.HairBelowEars, f.BeardAbsentVisible = true, true
	r, err := NewEngine().Evaluate(f)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "low_confidence" || r.PrimaryCategory.Code != "" || len(r.Components) != 0 {
		t.Fatalf("unknown body treated as visible non-muscular body: %+v", r)
	}
	f.NonMuscularVisible = true
	r, err = NewEngine().Evaluate(f)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "completed" || r.PrimaryCategory.Code != "alternative" || r.AssessmentScope != "portrait" || r.Score != 0 || len(r.Components) != 0 {
		t.Fatalf("explicit combination did not yield portrait-only verdict: %+v", r)
	}
	if len(r.Explanations) != 1 || !strings.Contains(r.Explanations[0], "бороды явно нет") || !strings.Contains(r.Explanations[0], "хорошо различимом теле") {
		t.Fatalf("explanation omits required visible evidence: %v", r.Explanations)
	}
}

func TestThreeIndependentCategoriesKeepTwoPrioritizedAndExplained(t *testing.T) {
	for _, tc := range []struct {
		name          string
		set           func(*analysis.VisualFeatures)
		codes, claims [2]string
		omittedClaim  string
	}{
		{"grimace skullcap muscles", func(f *analysis.VisualFeatures) {
			f.PlayfulGrimace, f.SkullcapVisible, f.BeardVisible, f.MuscularVisible = true, true, true, true
		}, [2]string{"clown", "mula"}, [2]string{"гримаса", "тюбетейка"}, "мускулатура"},
		{"pose action hero muscles", func(f *analysis.VisualFeatures) {
			f.SeriousIndexPose, f.ShortHairOrBaldVisible, f.BeardVisible, f.HeadScarVisible, f.MuscularVisible = true, true, true, true, true
		}, [2]string{"cool_pose", "action_hero"}, [2]string{"палец", "шрам"}, "мускулатура"},
		{"expression conflict skips to muscles", func(f *analysis.VisualFeatures) {
			f.PlayfulGrimace, f.SeriousIndexPose, f.GentleSmile, f.RelaxedHeadTilt, f.MuscularVisible = true, true, true, true, true
		}, [2]string{"clown", "muscular"}, [2]string{"гримаса", "мускулатура"}, "палец"},
		{"clothing beats muscles with third clothing signal", func(f *analysis.VisualFeatures) {
			f.Signals.Goth, f.Darkness, f.Signals.Alternative, f.Originality, f.AccessoryIntensity, f.Layering = 100, 100, 100, 100, 100, 100
			f.MuscularVisible = true
		}, [2]string{"goth", "muscular"}, [2]string{"готического", "мускулатура"}, "альтернативного"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := baseFeatures()
			tc.set(&f)
			r, err := NewEngine().Evaluate(f)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Components) != 2 || len(r.Explanations) != 2 {
				t.Fatalf("expected exactly two explained components: %+v", r)
			}
			for i, code := range tc.codes {
				if r.Components[i].Code != code || !strings.Contains(r.Explanations[i], tc.claims[i]) {
					t.Fatalf("component %d priority or evidence mismatch: %+v", i, r)
				}
			}
			if r.PrimaryCategory.Code != tc.codes[0] || r.SecondaryCategory.Code != tc.codes[1] {
				t.Fatalf("labels disagree with components: %+v", r)
			}
			if strings.Contains(strings.Join(r.Explanations, " "), tc.omittedClaim) {
				t.Fatalf("explanations include discarded third category: %v", r.Explanations)
			}
		})
	}
}
