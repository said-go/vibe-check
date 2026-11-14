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

func TestSmileDoesNotEraseStrongClothingStyle(t *testing.T) {
	for _, style := range []string{"goth", "alternative", "retro_90s", "sport_chic", "streetwear", "urban_classic"} {
		t.Run(style, func(t *testing.T) {
			f := baseFeatures()
			f.Signals = analysis.SubcultureSignals{}
			f.Formality = 0
			f.Sportiness = 0
			f.Darkness = 0
			switch style {
			case "goth":
				f.Signals.Goth = 100
				f.Darkness = 100
			case "alternative":
				f.Signals.Alternative = 100
				f.Originality = 100
				f.AccessoryIntensity = 100
			case "retro_90s":
				f.Signals.Retro90s = 100
			case "sport_chic":
				f.Signals.SportChic = 100
				f.Sportiness = 100
			case "streetwear":
				f.Signals.Streetwear = 100
				f.Originality = 100
			case "urban_classic":
				f.Signals.Classic = 100
				f.Formality = 100
			}
			f.GentleSmile = true
			r, err := NewEngine().Evaluate(f)
			if err != nil || r.PrimaryCategory.Code != style {
				t.Fatalf("smile erased %s: %+v %v", style, r, err)
			}
		})
	}
}

func TestUnreliableFeaturesDoNotExposeNormalVerdict(t *testing.T) {
	for _, kind := range []string{"no person", "multiple people", "no clothing", "very low confidence"} {
		f := baseFeatures()
		switch kind {
		case "no person":
			f.PersonCount = 0
		case "multiple people":
			f.PersonCount = 3
		case "no clothing":
			f.OutfitVisibility = 0
		case "very low confidence":
			f.Confidence = 1
		}
		r, err := NewEngine().Evaluate(f)
		if err != nil || r.Status != "low_confidence" || r.PrimaryCategory.Code != "" || r.Score != 0 || r.LevelTitle != "" || r.Metrics != (Metrics{}) {
			t.Fatalf("%s: %+v %v", kind, r, err)
		}
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
	if !result.Provisional || !strings.Contains(result.AssessmentNote, "Видна только часть образа") {
		t.Fatalf("limited view needs a visible qualifier: %q", result.AssessmentNote)
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
	if len(withSeveralCues.Explanations) == 0 {
		t.Fatalf("visible supporting cues missing from result: %+v", withSeveralCues)
	}
	if strings.Contains(withoutCues.Summary, "пирсинг") {
		t.Fatalf("invisible cue included in result: %q", withoutCues.Summary)
	}
}

func TestPlayfulGrimaceWinsOnlyWhenVisible(t *testing.T) {
	features := baseFeatures()
	engine := NewEngine()
	without, err := engine.Evaluate(features)
	if err != nil {
		t.Fatal(err)
	}
	if without.PrimaryCategory.Code == "clown" {
		t.Fatal("ordinary expression selected clown")
	}
	features.PlayfulGrimace = true
	with, err := engine.Evaluate(features)
	if err != nil {
		t.Fatal(err)
	}
	if with.PrimaryCategory.Code != "clown" || with.SecondaryCategory.Code != without.PrimaryCategory.Code {
		t.Fatalf("grimace priority is wrong: %+v", with)
	}
	if with.Score != without.Score {
		t.Fatal("grimace should not change the outfit score")
	}
}

func TestVisiblePoseAndSmileChoosePlayfulCategories(t *testing.T) {
	engine := NewEngine()
	baseline, err := engine.Evaluate(baseFeatures())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, wantCode, wantTitle, summaryCue string
		pose, smile                           bool
	}{
		{"raised index and serious face", "cool_pose", "Типо крутой", "палец", true, false},
		{"small relaxed smile", "pupsik", "Пупсик", "улыбка", false, true},
		{"both cues favor pose", "cool_pose", "Типо крутой", "палец", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			features := baseFeatures()
			features.SeriousIndexPose = tc.pose
			features.GentleSmile = tc.smile
			result, err := engine.Evaluate(features)
			if err != nil {
				t.Fatal(err)
			}
			if result.PrimaryCategory.Code != tc.wantCode || result.PrimaryCategory.Title != tc.wantTitle || !strings.Contains(strings.ToLower(result.Summary), tc.summaryCue) {
				t.Fatalf("unexpected verdict: %+v", result)
			}
			if result.Score != baseline.Score || result.Level != baseline.Level {
				t.Fatalf("pose or smile changed clothing score: %+v, baseline %+v", result, baseline)
			}
		})
	}
}

func TestClownAndMulaKeepPriorityOverPoseAndSmile(t *testing.T) {
	features := baseFeatures()
	features.GentleSmile = true
	features.SeriousIndexPose = true
	features.SkullcapVisible = true
	features.BeardVisible = true
	result, err := NewEngine().Evaluate(features)
	if err != nil {
		t.Fatal(err)
	}
	if result.PrimaryCategory.Code != "mula" || result.SecondaryCategory.Code != "cool_pose" {
		t.Fatalf("special category priority changed: %+v", result)
	}
	features.PlayfulGrimace = true
	result, err = NewEngine().Evaluate(features)
	if err != nil {
		t.Fatal(err)
	}
	if result.PrimaryCategory.Code != "clown" || result.SecondaryCategory.Code != "mula" {
		t.Fatalf("grimace priority changed: %+v", result)
	}
}

func TestRetro90sStyleDoesNotDependOnTraditionalSignal(t *testing.T) {
	features := baseFeatures()
	features.TraditionalElements = 100
	features.Signals.Retro90s = 100
	result, err := NewEngine().Evaluate(features)
	if err != nil {
		t.Fatal(err)
	}
	if result.PrimaryCategory.Code != "retro_90s" || result.PrimaryCategory.Title != "Бандит 90-х" {
		t.Fatalf("retro outfit should win: %+v", result.PrimaryCategory)
	}
	if result.LevelTitle != "Стиляга" {
		t.Fatalf("styler level was renamed: %q", result.LevelTitle)
	}
	features.Signals.Retro90s = 0
	result, err = NewEngine().Evaluate(features)
	if err != nil {
		t.Fatal(err)
	}
	if result.PrimaryCategory.Code == "traditional_accent" || result.PrimaryCategory.Code == "retro_90s" {
		t.Fatalf("traditional or retro category selected without retro outfit: %+v", result.PrimaryCategory)
	}
}

func TestHealthyLevelTitle(t *testing.T) {
	if got := level(60).title; got != "Здравый пацан" {
		t.Fatalf("level title = %q", got)
	}
	if got := level(75).title; got != "Стиляга" {
		t.Fatalf("styler title = %q", got)
	}
}
