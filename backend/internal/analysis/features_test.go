package analysis

import (
	"strings"
	"testing"
)

const validJSON = `{
  "contract_version":"1.0.0","dominant_colors":["black","gray"],
  "color_harmony":82,"darkness":76,"formality":68,"sportiness":41,
  "originality":73,"outfit_coherence":88,"accessory_intensity":57,
  "layering":44,"traditional_elements":12,"image_quality":91,
  "outfit_visibility":90,"confidence":86,"person_count":1,
  "subculture_signals":{"goth":38,"alternative":22,"streetwear":46,"classic":75,"sport_chic":51}
}`

func TestDecodeFeatures(t *testing.T) {
	features, err := DecodeFeatures(strings.NewReader(validJSON))
	if err != nil {
		t.Fatal(err)
	}
	if features.ContractVersion != ContractVersion || features.Signals.Classic != 75 {
		t.Fatalf("unexpected features: %+v", features)
	}
}

func TestDecodeVisibleSkullcapAndBeard(t *testing.T) {
	input := strings.Replace(validJSON, `"person_count":1,`, `"person_count":1,"skullcap_visible":true,"beard_visible":true,`, 1)
	features, err := DecodeFeatures(strings.NewReader(input))
	if err != nil || !features.SkullcapVisible || !features.BeardVisible {
		t.Fatalf("features = %+v, error = %v", features, err)
	}
}

func TestDecodeAlternativeStyleCues(t *testing.T) {
	input := strings.Replace(validJSON, `"person_count":1,`, `"person_count":1,"unnatural_hair_color_visible":true,"piercing_visible":true,"hair_below_ears_visible":true,"hair_over_eyes_visible":true,`, 1)
	features, err := DecodeFeatures(strings.NewReader(input))
	if err != nil || !features.UnnaturalHairColor || !features.PiercingVisible || !features.HairBelowEars || !features.HairOverEyes {
		t.Fatalf("features = %+v, error = %v", features, err)
	}
}

func TestDecodeVisiblePoseAndExpression(t *testing.T) {
	input := strings.Replace(validJSON, `"person_count":1,`, `"person_count":1,"serious_index_pose_visible":true,"gentle_smile_visible":true,`, 1)
	features, err := DecodeFeatures(strings.NewReader(input))
	if err != nil || !features.SeriousIndexPose || !features.GentleSmile {
		t.Fatalf("features = %+v, error = %v", features, err)
	}
}

func TestDecodeWithoutRecognizableClothingColor(t *testing.T) {
	input := strings.Replace(validJSON, `["black","gray"]`, `[]`, 1)
	features, err := DecodeFeatures(strings.NewReader(input))
	if err != nil || len(features.DominantColors) != 0 {
		t.Fatalf("features = %+v, error = %v", features, err)
	}
}

func TestDecodeFeaturesRejectsUnknownAndOutOfRangeFields(t *testing.T) {
	unknown := strings.Replace(validJSON, `"contract_version":"1.0.0"`, `"contract_version":"1.0.0","ethnicity":"unknown"`, 1)
	if _, err := DecodeFeatures(strings.NewReader(unknown)); err == nil {
		t.Fatal("expected unknown field error")
	}
	outOfRange := strings.Replace(validJSON, `"confidence":86`, `"confidence":101`, 1)
	if _, err := DecodeFeatures(strings.NewReader(outOfRange)); err == nil {
		t.Fatal("expected range error")
	}
	missing := strings.Replace(validJSON, `"person_count":1,`, "", 1)
	if _, err := DecodeFeatures(strings.NewReader(missing)); err == nil {
		t.Fatal("expected missing person_count error")
	}
}

func TestLowConfidenceRules(t *testing.T) {
	features, _ := DecodeFeatures(strings.NewReader(validJSON))
	if reason := features.LowConfidenceReason(); reason != "" {
		t.Fatalf("unexpected reason %q", reason)
	}
	features.OutfitVisibility = 40
	features.ImageQuality = 30
	features.Confidence = 35
	features.PersonCount = 1
	if reason := features.LowConfidenceReason(); reason != "" {
		t.Fatalf("partial or lower-quality outfit should be accepted, got %q", reason)
	}
	features.PersonCount = 2
	if reason := features.LowConfidenceReason(); reason != "single_person_required" {
		t.Fatalf("multiple people must not produce a merged verdict, got %q", reason)
	}
	features.PersonCount = 0
	if reason := features.LowConfidenceReason(); reason != "single_person_required" {
		t.Fatalf("no person: %q", reason)
	}
	features.PersonCount = 1
	features.OutfitVisibility = 0
	if reason := features.LowConfidenceReason(); reason != "outfit_visibility_too_low" {
		t.Fatalf("invisible clothing must not produce a clothing score, got %q", reason)
	}
	features.OutfitVisibility = 40
	features.Confidence = 1
	if reason := features.LowConfidenceReason(); reason != "ai_confidence_too_low" {
		t.Fatalf("confidence 1: %q", reason)
	}
	features.OutfitVisibility = 0
	features.Confidence = 0
	features.OutfitCoherence = 0
	features.ColorHarmony = 0
	features.Originality = 0
	if reason := features.LowConfidenceReason(); reason != "outfit_visibility_too_low" {
		t.Fatalf("unexpected reason %q", reason)
	}
}

func TestPortraitCueAllowsOnlyRecognizableSinglePerson(t *testing.T) {
	for _, cue := range []string{"grimace", "finger", "smile", "skullcap_beard"} {
		t.Run(cue, func(t *testing.T) {
			f := VisualFeatures{PersonCount: 1, Confidence: 50}
			switch cue {
			case "grimace":
				f.PlayfulGrimace = true
			case "finger":
				f.SeriousIndexPose = true
			case "smile":
				f.GentleSmile = true
			case "skullcap_beard":
				f.SkullcapVisible, f.BeardVisible = true, true
			}
			if reason := f.LowConfidenceReason(); reason != "" {
				t.Fatalf("visible cue rejected: %s", reason)
			}
			f.Confidence = 19
			if reason := f.LowConfidenceReason(); reason != "ai_confidence_too_low" {
				t.Fatalf("uncertain cue accepted: %s", reason)
			}
			f.Confidence, f.PersonCount = 50, 2
			if reason := f.LowConfidenceReason(); reason != "single_person_required" {
				t.Fatalf("group accepted: %s", reason)
			}
		})
	}
	for _, f := range []VisualFeatures{
		{PersonCount: 1, Confidence: 90},
		{PersonCount: 1, Confidence: 90, SkullcapVisible: true},
		{PersonCount: 1, Confidence: 90, BeardVisible: true},
	} {
		if reason := f.LowConfidenceReason(); reason != "outfit_visibility_too_low" {
			t.Fatalf("no supported cue accepted: %s", reason)
		}
	}
}
