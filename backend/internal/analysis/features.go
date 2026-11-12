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
	Signals             SubcultureSignals `json:"subculture_signals"`
}

func DecodeFeatures(reader io.Reader) (VisualFeatures, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return VisualFeatures{}, fmt.Errorf("read AI response: %w", err)
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
	if f.ContractVersion != ContractVersion {
		return fmt.Errorf("unsupported contract_version %q", f.ContractVersion)
	}
	if len(f.DominantColors) == 0 || len(f.DominantColors) > 5 {
		return errors.New("dominant_colors must contain 1 to 5 values")
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
	case f.ImageQuality < 55:
		return "image_quality_too_low"
	case f.OutfitVisibility < 60:
		return "outfit_visibility_too_low"
	case f.Confidence < 70:
		return "ai_confidence_too_low"
	default:
		return ""
	}
}
