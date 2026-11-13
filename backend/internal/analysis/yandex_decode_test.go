package analysis

import (
	"strings"
	"testing"
)

func TestDecodeYandexFeaturesRecoversHarmlessSchemaDrift(t *testing.T) {
	input := strings.Replace(validJSON, `"contract_version":"1.0.0",`, `"unexpected_key":"ignored",`, 1)
	input = strings.Replace(input, `"sport_chic":51`, `"sport_chic":51,"extra_signal":44`, 1)
	features, err := DecodeYandexFeatures(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if features.ContractVersion != ContractVersion || features.OutfitCoherence != 88 || features.Signals.SportChic != 51 {
		t.Fatalf("visual features changed: %+v", features)
	}
}

func TestDecodeYandexFeaturesStillRejectsMissingVisualValues(t *testing.T) {
	input := strings.Replace(validJSON, `"outfit_coherence":88,`, ``, 1)
	if _, err := DecodeYandexFeatures(strings.NewReader(input)); err == nil {
		t.Fatal("missing scoring value was accepted")
	}
}
