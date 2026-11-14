package analysis

func StrictSchema() map[string]any {
	score := map[string]any{"type": "integer", "minimum": 0, "maximum": 100}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required": []string{
			"contract_version", "dominant_colors", "color_harmony", "darkness", "formality",
			"sportiness", "originality", "outfit_coherence", "accessory_intensity", "layering",
			"short_hair_or_bald_visible", "head_scar_visible",
			"traditional_elements", "image_quality", "outfit_visibility", "confidence", "person_count", "skullcap_visible", "beard_visible", "unnatural_hair_color_visible", "piercing_visible", "hair_below_ears_visible", "hair_over_eyes_visible", "playful_grimace_visible", "serious_index_pose_visible", "gentle_smile_visible", "relaxed_head_tilt_visible", "beard_absent_visible", "muscular_visible", "non_muscular_visible", "subculture_signals",
		},
		"properties": map[string]any{
			"short_hair_or_bald_visible": map[string]any{"type": "boolean"},
			"head_scar_visible":          map[string]any{"type": "boolean"},
			"beard_absent_visible":       map[string]any{"type": "boolean"},
			"muscular_visible":           map[string]any{"type": "boolean"},
			"non_muscular_visible":       map[string]any{"type": "boolean"},
			"contract_version":           map[string]any{"type": "string", "const": ContractVersion},
			"dominant_colors":            map[string]any{"type": "array", "maxItems": 5, "items": map[string]any{"type": "string"}},
			"color_harmony":              score, "darkness": score, "formality": score, "sportiness": score,
			"originality": score, "outfit_coherence": score, "accessory_intensity": score,
			"layering": score, "traditional_elements": score, "image_quality": score,
			"outfit_visibility": score, "confidence": score,
			"person_count":                 map[string]any{"type": "integer", "minimum": 0, "maximum": 10},
			"skullcap_visible":             map[string]any{"type": "boolean"},
			"beard_visible":                map[string]any{"type": "boolean"},
			"unnatural_hair_color_visible": map[string]any{"type": "boolean"},
			"piercing_visible":             map[string]any{"type": "boolean"},
			"hair_below_ears_visible":      map[string]any{"type": "boolean"},
			"hair_over_eyes_visible":       map[string]any{"type": "boolean"},
			"playful_grimace_visible":      map[string]any{"type": "boolean"},
			"serious_index_pose_visible":   map[string]any{"type": "boolean"},
			"gentle_smile_visible":         map[string]any{"type": "boolean"},
			"relaxed_head_tilt_visible":    map[string]any{"type": "boolean"},
			"subculture_signals": map[string]any{
				"type": "object", "additionalProperties": false,
				"required":   []string{"goth", "alternative", "streetwear", "classic", "sport_chic", "retro_90s"},
				"properties": map[string]any{"goth": score, "alternative": score, "streetwear": score, "classic": score, "sport_chic": score, "retro_90s": score},
			},
		},
	}
}
