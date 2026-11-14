package scoring

import (
	"sort"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

// Explanations describe the inputs that contributed most to this verdict.
// They deliberately avoid inventing specific garments from broad style signals.
func (e *Engine) explanations(code string, f analysis.VisualFeatures) []string {
	switch code {
	case "clown":
		if f.PlayfulGrimace {
			return []string{"В кадре заметна нарочитая гримаса — она и задала вердикт."}
		}
	case "cool_pose":
		if f.SeriousIndexPose {
			return []string{"Поднят указательный палец.", "Серьёзное выражение лица дополняет позу."}
		}
	case "pupsik":
		if f.GentleSmile {
			return []string{"В кадре лёгкая улыбка — по ней выбран этот игровой вердикт."}
		}
	case "mula":
		if f.SkullcapVisible && f.BeardVisible {
			return []string{"На фото видна тюбетейка.", "Также видна борода — сработало сочетание этих двух деталей."}
		}
	}
	labels := map[featureName]string{
		"formality":            "Одежда выглядит сдержанно и ближе к деловому стилю.",
		"coherence":            "Видимые вещи сочетаются между собой.",
		"classic":              "В одежде заметны классические черты.",
		"harmony":              "Цвета в кадре сочетаются между собой.",
		"accessories":          "Заметные аксессуары добавили баллов этому стилю.",
		"sport_chic":           "В одежде сочетаются спортивные и повседневные черты.",
		"sportiness":           "Видны спортивные черты одежды.",
		"originality":          "Необычное сочетание деталей добавило баллов.",
		"streetwear":           "В одежде заметны черты уличного стиля.",
		"alternative":          "В одежде заметны черты альтернативного стиля.",
		"unnatural_hair_color": "Волосы окрашены в ненатуральный цвет.",
		"piercing":             "В кадре заметен пирсинг.",
		"hair_below_ears":      "Волосы опускаются ниже ушей.",
		"hair_over_eyes":       "Волосы частично закрывают глаза.",
		"layering":             "Видно несколько слоёв одежды.",
		"goth":                 "В одежде заметны черты готического стиля.",
		"darkness":             "В одежде преобладают тёмные оттенки.",
		"retro_90s":            "В одежде заметны черты стиля 90-х.",
	}
	type evidence struct {
		name         featureName
		contribution float64
	}
	items := make([]evidence, 0)
	for name, weight := range e.ruleByCode(code).Weights {
		// Weak model signals are insufficient to make a positive claim.
		if v := value(f, name); v >= 50 && weight > 0 {
			items = append(items, evidence{name, float64(v) * weight})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].contribution == items[j].contribution {
			return items[i].name < items[j].name
		}
		return items[i].contribution > items[j].contribution
	})
	var result []string
	for _, item := range items {
		if label := labels[item.name]; label != "" {
			result = append(result, label)
		}
		if len(result) == 3 {
			break
		}
	}
	return result
}
