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
	features.PersonCount = 0
	if reason := features.LowConfidenceReason(); reason != "" {
		t.Fatalf("partial or lower-quality outfit should be accepted, got %q", reason)
	}
	features.PersonCount = 2
	if reason := features.LowConfidenceReason(); reason != "" {
		t.Fatalf("model person count should not reject visible clothing, got %q", reason)
	}
	features.OutfitVisibility = 0
	if reason := features.LowConfidenceReason(); reason != "" {
		t.Fatalf("a single low visibility estimate should not reject clothing, got %q", reason)
	}
	features.Confidence = 0
	features.OutfitCoherence = 0
	features.ColorHarmony = 0
	features.Originality = 0
	if reason := features.LowConfidenceReason(); reason != "outfit_visibility_too_low" {
		t.Fatalf("unexpected reason %q", reason)
	}
}
