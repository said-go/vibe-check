package analysis

import (
	"io"
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

func TestNewVisibleBodyCuesSurviveYandexDecoder(t *testing.T) {
	input := strings.Replace(validJSON, `"person_count":1,`, `"person_count":1,"beard_absent_visible":true,"muscular_visible":true,`, 1)
	f, err := DecodeYandexFeatures(strings.NewReader(input))
	if err != nil || !f.BeardAbsentVisible || !f.MuscularVisible || f.NonMuscularVisible {
		t.Fatalf("lost evidence: %+v %v", f, err)
	}
	for _, fields := range []string{`"beard_visible":true,"beard_absent_visible":true,`, `"muscular_visible":true,"non_muscular_visible":true,`} {
		input = strings.Replace(validJSON, `"person_count":1,`, `"person_count":1,`+fields, 1)
		if _, err := DecodeYandexFeatures(strings.NewReader(input)); err == nil {
			t.Fatal("contradictory evidence accepted")
		}
	}
}

func TestCinematicCuesDecodeAndDefaultToUnknown(t *testing.T) {
	for _, decode := range []func(io.Reader) (VisualFeatures, error){DecodeFeatures, DecodeYandexFeatures} {
		old, err := decode(strings.NewReader(validJSON))
		if err != nil {
			t.Fatal(err)
		}
		if old.ShortHairOrBaldVisible || old.HeadScarVisible {
			t.Fatal("old response invented cues")
		}
		input := strings.Replace(validJSON, `"person_count":1,`, `"person_count":1,"short_hair_or_bald_visible":true,"head_scar_visible":true,"beard_visible":true,`, 1)
		f, err := decode(strings.NewReader(input))
		if err != nil || !f.HasActionHeroCombination() {
			t.Fatalf("lost cinematic cues: %+v %v", f, err)
		}
	}
}
