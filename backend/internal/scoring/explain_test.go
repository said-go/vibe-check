package scoring

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

func TestExplanationsUseStrongestActualContributions(t *testing.T) {
	f := analysis.VisualFeatures{UnnaturalHairColor: true, PiercingVisible: true, HairBelowEars: true}
	e := NewEngine()
	want := []string{"Волосы окрашены в ненатуральный цвет.", "В кадре заметен пирсинг.", "Волосы опускаются ниже ушей."}
	if got := e.explanations("alternative", f); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	f.UnnaturalHairColor = false
	if got := strings.Join(e.explanations("alternative", f), " "); strings.Contains(got, "окрашены") || strings.Contains(got, "глаза") {
		t.Fatalf("invented cue: %s", got)
	}
}

func TestPortraitHasNoOutfitAssessment(t *testing.T) {
	f := baseFeatures()
	f.OutfitVisibility, f.GentleSmile = 0, true
	f.RelaxedHeadTilt = true
	f.Signals.Goth, f.Darkness = 100, 100 // stale outfit values cannot override portrait evidence
	r, err := NewEngine().Evaluate(f)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "completed" || r.PrimaryCategory.Code != "pupsik" || r.AssessmentScope != "portrait" || !r.Provisional {
		t.Fatalf("unexpected portrait: %+v", r)
	}
	if r.Score != 0 || r.Metrics != (Metrics{}) || r.Level != "" || r.SecondaryCategory.Code != "" {
		t.Fatalf("invented outfit assessment: %+v", r)
	}
	if len(r.Explanations) != 2 || !strings.Contains(r.Explanations[0], "улыбка") {
		t.Fatalf("wrong evidence: %v", r.Explanations)
	}
}

func TestNoEvidenceDoesNotChooseArbitraryCategory(t *testing.T) {
	f := analysis.VisualFeatures{ContractVersion: analysis.ContractVersion, PersonCount: 1, OutfitVisibility: 90, Confidence: 90, ImageQuality: 90}
	r, err := NewEngine().Evaluate(f)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "low_confidence" || r.LowConfidenceReason != "insufficient_style_evidence" || r.PrimaryCategory.Code != "" || len(r.Explanations) != 0 {
		t.Fatalf("unexpected evidence-free result: %+v", r)
	}
}

func TestManualResultDoesNotInventExplanation(t *testing.T) {
	data, err := json.Marshal(Result{Summary: "Manual text"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "explanations") {
		t.Fatalf("unexpected explanation: %s", data)
	}
}

func TestProvisionalThresholds(t *testing.T) {
	for _, change := range []func(*analysis.VisualFeatures){
		func(f *analysis.VisualFeatures) { f.OutfitVisibility = 59 },
		func(f *analysis.VisualFeatures) { f.ImageQuality = 54 },
		func(f *analysis.VisualFeatures) { f.Confidence = 69 },
	} {
		f := baseFeatures()
		change(&f)
		r, err := NewEngine().Evaluate(f)
		if err != nil || !r.Provisional || r.AssessmentNote == "" {
			t.Fatalf("missing qualifier: %+v, %v", r, err)
		}
	}
}
