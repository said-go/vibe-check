package analysis

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// DecodeYandexFeatures accepts harmless schema drift from Yandex's json_object
// mode. The model is not asked to choose a contract version, and stray fields
// do not affect scoring. All required visual values still pass DecodeFeatures.
func DecodeYandexFeatures(reader io.Reader) (VisualFeatures, error) {
	data, err := io.ReadAll(io.LimitReader(reader, 1<<20))
	if err != nil {
		return VisualFeatures{}, fmt.Errorf("read Yandex features: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return VisualFeatures{}, fmt.Errorf("decode Yandex features: %w", err)
	}
	if fields == nil {
		return VisualFeatures{}, errors.New("decode Yandex features: expected object")
	}
	allowed := map[string]bool{
		"short_hair_or_bald_visible": true, "head_scar_visible": true,
		"beard_absent_visible": true, "muscular_visible": true, "non_muscular_visible": true,
		"contract_version": true, "dominant_colors": true, "color_harmony": true,
		"darkness": true, "formality": true, "sportiness": true, "originality": true,
		"outfit_coherence": true, "accessory_intensity": true, "layering": true,
		"traditional_elements": true, "image_quality": true, "outfit_visibility": true,
		"confidence": true, "person_count": true, "skullcap_visible": true,
		"beard_visible": true, "unnatural_hair_color_visible": true,
		"piercing_visible": true, "hair_below_ears_visible": true,
		"hair_over_eyes_visible": true, "playful_grimace_visible": true,
		"serious_index_pose_visible": true, "gentle_smile_visible": true,
		"relaxed_head_tilt_visible": true,
		"subculture_signals":        true,
	}
	for name := range fields {
		if !allowed[name] {
			delete(fields, name)
		}
	}
	fields["contract_version"] = json.RawMessage(`"` + ContractVersion + `"`)
	if raw, ok := fields["subculture_signals"]; ok {
		var signals map[string]json.RawMessage
		if err := json.Unmarshal(raw, &signals); err != nil || signals == nil {
			return VisualFeatures{}, errors.New("decode Yandex features: invalid subculture_signals")
		}
		for name := range signals {
			switch name {
			case "goth", "alternative", "streetwear", "classic", "sport_chic", "retro_90s":
			default:
				delete(signals, name)
			}
		}
		fields["subculture_signals"], err = json.Marshal(signals)
		if err != nil {
			return VisualFeatures{}, fmt.Errorf("encode Yandex signals: %w", err)
		}
	}
	normalized, err := json.Marshal(fields)
	if err != nil {
		return VisualFeatures{}, fmt.Errorf("encode Yandex features: %w", err)
	}
	return DecodeFeatures(bytes.NewReader(normalized))
}
