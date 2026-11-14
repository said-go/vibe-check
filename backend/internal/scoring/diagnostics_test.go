package scoring

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

func TestDecisionTraceMatchesEvaluation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*analysis.VisualFeatures)
		want   []string
		absent string
	}{
		{"ordinary", func(f *analysis.VisualFeatures) {}, []string{"score_order", "ranked_leader", "mix_first_weak", "final_primary"}, "mix_included"},
		{"special priority and expression conflict", func(f *analysis.VisualFeatures) {
			f.PlayfulGrimace = true
			f.SeriousIndexPose = true
			f.MuscularVisible = true
		}, []string{"special_priority", "mix_expression_conflict", "mix_included"}, ""},
		{"suppressed gentle pose", func(f *analysis.VisualFeatures) {
			f.GentleSmile = true
			f.RelaxedHeadTilt = true
			f.Signals.Goth = 100
			f.Darkness = 100
		}, []string{"pupsik_suppressed"}, "pupsik_priority"},
		{"gentle pose after muscles", func(f *analysis.VisualFeatures) {
			f.GentleSmile = true
			f.RelaxedHeadTilt = true
			f.MuscularVisible = true
		}, []string{"muscular_priority", "pupsik_priority", "mix_included"}, "pupsik_suppressed"},
		{"low confidence", func(f *analysis.VisualFeatures) { f.Confidence = 1; f.PlayfulGrimace = true }, []string{"quality_rejected"}, "final_primary"},
		{"portrait alternative", func(f *analysis.VisualFeatures) {
			f.OutfitVisibility = 0
			f.HairBelowEars = true
			f.BeardAbsentVisible = true
			f.NonMuscularVisible = true
		}, []string{"portrait_alternative"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := baseFeatures()
			tt.change(&f)
			e := NewEngine()
			plain, err := e.Evaluate(f)
			if err != nil {
				t.Fatal(err)
			}
			trace := &decisionTrace{}
			observed, err := e.evaluate(f, trace)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(plain, observed) {
				t.Fatal("observer changed result")
			}
			d := e.Diagnose(f, plain)
			if !reflect.DeepEqual(trace.reasons, d.DecisionReasons) {
				t.Fatal("diagnostic did not replay actual branches")
			}
			found := map[string]bool{}
			for _, step := range d.DecisionReasons {
				found[step.Code] = true
				if step.Code == "final_primary" && !reflect.DeepEqual(*step.Category, plain.PrimaryCategory) {
					t.Fatal("wrong final primary")
				}
				if step.Code == "mix_included" && plain.Status == "completed" && !reflect.DeepEqual([]CategoryScore{*step.Category, *step.Other}, plain.Components) {
					t.Fatal("wrong mixed components")
				}
			}
			for _, code := range tt.want {
				if !found[code] {
					t.Errorf("missing %s: %#v", code, d.DecisionReasons)
				}
			}
			if tt.absent != "" && found[tt.absent] {
				t.Errorf("unexpected %s", tt.absent)
			}
			encoded, _ := json.Marshal(plain)
			if strings.Contains(string(encoded), "decision_reasons") {
				t.Fatal("trace leaked into public result")
			}
			if plain.Status == "low_confidence" && len(d.Selected) != 0 {
				t.Fatal("rejected choice remained selected")
			}
		})
	}
}

func TestDecisionTraceTieAndMuscularSwap(t *testing.T) {
	f := baseFeatures()
	// Custom rules expose the existing exact tie and muscular-first swap paths.
	e := &Engine{rules: []rule{{Code: "healthy_casual", Title: "Стиляга", Weights: map[featureName]int{"coherence": 100}}, {Code: "alternative", Title: "Нефор", Weights: map[featureName]int{"coherence": 100}}}}
	result, _ := e.Evaluate(f)
	d := e.Diagnose(f, result)
	if d.DecisionReasons[0].Category.Code != "alternative" || d.DecisionReasons[0].Category.Score != d.DecisionReasons[0].Other.Score {
		t.Fatal("tie order not traced")
	}
	// Use a lower-ranked strong style behind a weak clothing leader.
	f.MuscularVisible = true
	f.OutfitCoherence = 100
	f.Signals.Goth = 100
	f.Darkness = 80
	e = &Engine{rules: []rule{{Code: "healthy_casual", Title: "Стиляга", Weights: map[featureName]int{"coherence": 100}}, {Code: "goth", Title: "Гот", Weights: map[featureName]int{"darkness": 100}}}}
	result, err := e.Evaluate(f)
	if err != nil {
		t.Fatal(err)
	}
	d = e.Diagnose(f, result)
	found := false
	for _, step := range d.DecisionReasons {
		if step.Code == "mix_clothing_first" {
			found = true
			if step.Category.Code != "goth" || step.Other.Code != "muscular" {
				t.Fatal("wrong swap")
			}
		}
	}
	if !found || result.PrimaryCategory.Code != "goth" || len(result.Components) != 2 {
		t.Fatalf("swap not exercised: %#v", result)
	}
}

func TestDecisionTraceClothingGapBoundary(t *testing.T) {
	for _, secondScore := range []int{84, 85} {
		f := baseFeatures()
		f.Darkness, f.OutfitCoherence = 100, secondScore
		f.Signals.Goth, f.Signals.Alternative = 100, 100
		e := &Engine{rules: []rule{
			{Code: "goth", Title: "Гот", Weights: map[featureName]int{"darkness": 100}},
			{Code: "alternative", Title: "Нефор", Weights: map[featureName]int{"coherence": 100}},
		}}
		result, err := e.Evaluate(f)
		if err != nil {
			t.Fatal(err)
		}
		d := e.Diagnose(f, result)
		want := "mix_score_gap"
		if secondScore == 85 {
			want = "mix_included"
		}
		found := false
		for _, step := range d.DecisionReasons {
			if step.Code == want {
				found = true
				if step.Other.Score != secondScore {
					t.Fatal("wrong compared score")
				}
			}
		}
		if !found || (len(result.Components) == 2) != (secondScore == 85) {
			t.Fatalf("wrong gap decision at %d", secondScore)
		}
	}
}
