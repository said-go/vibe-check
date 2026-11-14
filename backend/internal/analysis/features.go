package analysis

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const ContractVersion = "1.0.0"

type SubcultureSignals struct {
	Goth        int `json:"goth"`
	Alternative int `json:"alternative"`
	Streetwear  int `json:"streetwear"`
	Classic     int `json:"classic"`
	SportChic   int `json:"sport_chic"`
	Retro90s    int `json:"retro_90s,omitempty"`
}

type VisualFeatures struct {
	ContractVersion     string            `json:"contract_version"`
	DominantColors      []string          `json:"dominant_colors"`
	ColorHarmony        int               `json:"color_harmony"`
	Darkness            int               `json:"darkness"`
	Formality           int               `json:"formality"`
	Sportiness          int               `json:"sportiness"`
	Originality         int               `json:"originality"`
	OutfitCoherence     int               `json:"outfit_coherence"`
	AccessoryIntensity  int               `json:"accessory_intensity"`
	Layering            int               `json:"layering"`
	TraditionalElements int               `json:"traditional_elements"`
	ImageQuality        int               `json:"image_quality"`
	OutfitVisibility    int               `json:"outfit_visibility"`
	Confidence          int               `json:"confidence"`
	PersonCount         int               `json:"person_count"`
	SkullcapVisible     bool              `json:"skullcap_visible"`
	BeardVisible        bool              `json:"beard_visible"`
	UnnaturalHairColor  bool              `json:"unnatural_hair_color_visible"`
	PiercingVisible     bool              `json:"piercing_visible"`
	HairBelowEars       bool              `json:"hair_below_ears_visible"`
	HairOverEyes        bool              `json:"hair_over_eyes_visible"`
	PlayfulGrimace      bool              `json:"playful_grimace_visible"`
	SeriousIndexPose    bool              `json:"serious_index_pose_visible"`
	GentleSmile         bool              `json:"gentle_smile_visible"`
	RelaxedHeadTilt     bool              `json:"relaxed_head_tilt_visible"`
	BeardAbsentVisible  bool              `json:"beard_absent_visible"`
	MuscularVisible     bool              `json:"muscular_visible"`
	NonMuscularVisible  bool              `json:"non_muscular_visible"`
	Signals             SubcultureSignals `json:"subculture_signals"`
}

func DecodeFeatures(reader io.Reader) (VisualFeatures, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return VisualFeatures{}, fmt.Errorf("read AI response: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return VisualFeatures{}, fmt.Errorf("decode AI response: %w", err)
	}
	for _, name := range []string{"contract_version", "dominant_colors", "color_harmony", "darkness", "formality", "sportiness", "originality", "outfit_coherence", "accessory_intensity", "layering", "traditional_elements", "image_quality", "outfit_visibility", "confidence", "person_count", "subculture_signals"} {
		if value, ok := fields[name]; !ok || string(value) == "null" {
			return VisualFeatures{}, fmt.Errorf("decode AI response: missing %s", name)
		}
	}
	var signals map[string]json.RawMessage
	if err := json.Unmarshal(fields["subculture_signals"], &signals); err != nil {
		return VisualFeatures{}, fmt.Errorf("decode AI response: subculture_signals: %w", err)
	}
	for _, name := range []string{"goth", "alternative", "streetwear", "classic", "sport_chic"} {
		if value, ok := signals[name]; !ok || string(value) == "null" {
			return VisualFeatures{}, fmt.Errorf("decode AI response: missing subculture_signals.%s", name)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var features VisualFeatures
	if err := decoder.Decode(&features); err != nil {
		return VisualFeatures{}, fmt.Errorf("decode AI response: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return VisualFeatures{}, errors.New("decode AI response: trailing JSON value")
	}
	if err := features.Validate(); err != nil {
		return VisualFeatures{}, err
	}
	return features, nil
}

func (f VisualFeatures) Validate() error {
	if f.BeardVisible && f.BeardAbsentVisible {
		return errors.New("contradictory beard evidence")
	}
	if f.MuscularVisible && f.NonMuscularVisible {
		return errors.New("contradictory muscularity evidence")
	}
	if f.ContractVersion != ContractVersion {
		return fmt.Errorf("unsupported contract_version %q", f.ContractVersion)
	}
	if len(f.DominantColors) > 5 {
		return errors.New("dominant_colors must contain at most 5 values")
	}
	if f.PersonCount < 0 || f.PersonCount > 10 {
		return errors.New("person_count must be between 0 and 10")
	}
	values := map[string]int{
		"color_harmony": f.ColorHarmony, "darkness": f.Darkness, "formality": f.Formality,
		"sportiness": f.Sportiness, "originality": f.Originality, "outfit_coherence": f.OutfitCoherence,
		"accessory_intensity": f.AccessoryIntensity, "layering": f.Layering,
		"traditional_elements": f.TraditionalElements, "image_quality": f.ImageQuality,
		"outfit_visibility": f.OutfitVisibility, "confidence": f.Confidence,
		"subculture_signals.goth": f.Signals.Goth, "subculture_signals.alternative": f.Signals.Alternative,
		"subculture_signals.streetwear": f.Signals.Streetwear, "subculture_signals.classic": f.Signals.Classic,
		"subculture_signals.sport_chic": f.Signals.SportChic,
		"subculture_signals.retro_90s":  f.Signals.Retro90s,
	}
	for name, value := range values {
		if value < 0 || value > 100 {
			return fmt.Errorf("%s must be between 0 and 100", name)
		}
	}
	return nil
}

func (f VisualFeatures) LowConfidenceReason() string {
	switch {
	case f.PersonCount != 1:
		return "single_person_required"
	case f.OutfitVisibility == 0 && !f.HasPortraitCue():
		return "outfit_visibility_too_low"
	case f.Confidence < 20:
		return "ai_confidence_too_low"
	default:
		return ""
	}
}

// HasPortraitCue reports only explicit cues that can support a playful portrait
// category without guessing unseen clothing.
func (f VisualFeatures) HasPortraitCue() bool {
	return f.PlayfulGrimace || f.SeriousIndexPose || f.HasGentlePose() || (f.SkullcapVisible && f.BeardVisible) || f.MuscularVisible || f.HasAlternativeCombination()
}

func (f VisualFeatures) HasAlternativeCombination() bool {
	return f.HairBelowEars && f.BeardAbsentVisible && f.NonMuscularVisible && !f.MuscularVisible
}

func (f VisualFeatures) HasGentlePose() bool {
	return f.GentleSmile && f.RelaxedHeadTilt
}
