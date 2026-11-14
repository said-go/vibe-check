package scoring

import (
	"strings"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

// A blend needs independent, visible evidence for both parts. A runner-up
// score alone does not justify adding a second label.
func strongCategory(f analysis.VisualFeatures, c CategoryScore) bool {
	switch c.Code {
	case "action_hero":
		return f.HasActionHeroCombination()
	case "muscular":
		return f.MuscularVisible
	case "clown":
		return f.PlayfulGrimace
	case "mula":
		return f.SkullcapVisible && f.BeardVisible
	case "cool_pose":
		return f.SeriousIndexPose
	case "pupsik":
		return f.HasGentlePose()
	case "alternative":
		if f.HasAlternativeCombination() {
			return true
		}
	}
	return f.OutfitVisibility > 0 && strongOutfitStyle(f, c)
}

func clothingCategory(code string) bool {
	switch code {
	case "urban_classic", "healthy_casual", "sport_chic", "streetwear", "alternative", "goth", "retro_90s":
		return true
	}
	return false
}

func (e *Engine) mix(result *Result, ranked []CategoryScore, f analysis.VisualFeatures, trace *decisionTrace) {
	first := ranked[0]
	if !strongCategory(f, first) {
		trace.add("mix_first_weak", first, ranked[1], "Вторая часть не добавлена: у лидера недостаточно самостоятельных признаков для смешанного вердикта. Одних баллов недостаточно.")
		return
	}
	for _, second := range ranked[1:] {
		// Contradictory facial expressions do not make a meaningful blend.
		if expressionCategory(first.Code) && expressionCategory(second.Code) {
			trace.add("mix_expression_conflict", first, second, "Кандидат не добавлен: два выражения лица не объединяются в один вердикт.")
			continue
		}
		if !strongCategory(f, second) {
			trace.add("mix_second_weak", first, second, "Кандидат не добавлен: его самостоятельные признаки недостаточно выражены. Одних баллов недостаточно.")
			continue
		}
		// Two clothing styles must be close enough to be a genuine blend.
		if clothingCategory(first.Code) && clothingCategory(second.Code) && first.Score-second.Score > 15 {
			trace.add("mix_score_gap", first, second, "Кандидат не добавлен: разница между двумя стилями одежды больше допустимых 15 баллов.")
			continue
		}
		if first.Code == "muscular" && clothingCategory(second.Code) {
			trace.add("mix_clothing_first", second, first, "Обе части подтверждены; в паре с мускулатурой стиль одежды ставится первым, даже если у него меньше баллов.")
			first, second = second, first
			result.PrimaryCategory = first
		}
		trace.add("mix_included", first, second, "Вторая часть добавлена: у обеих категорий есть самостоятельные признаки, конфликт выражений лица отсутствует. Для двух стилей одежды разница не превышает 15 баллов. Это первый подходящий кандидат в порядке приоритета. После этого поиск останавливается: в результате максимум две категории.")
		result.Components = []CategoryScore{first, second}
		result.PrimaryCategory.Title = first.Title + "-" + strings.ToLower(second.Title)
		result.SecondaryCategory = second
		result.Summary = "Два вайба в одном кадре. Одному названию тут явно тесно."
		if second.Code == "muscular" || first.Code == "muscular" {
			result.Summary = "Два вайба в одном кадре. Мышцы тоже решили попасть в титры."
		}
		// Give both parts a reason, rather than filling all three lines with one.
		a, b := e.explanations(first.Code, f), e.explanations(second.Code, f)
		result.Explanations = nil
		if len(a) > 0 {
			result.Explanations = append(result.Explanations, a[0])
		}
		if len(b) > 0 {
			result.Explanations = append(result.Explanations, b[0])
		}
		return
	}
}

func expressionCategory(code string) bool {
	return code == "clown" || code == "cool_pose" || code == "pupsik"
}
