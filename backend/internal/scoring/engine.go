package scoring

import (
	"errors"
	"math"
	"sort"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

type CategoryScore struct {
	Code  string `json:"code"`
	Title string `json:"title"`
	Score int    `json:"score"`
}

type Metrics struct {
	Coherence    int `json:"coherence"`
	Originality  int `json:"originality"`
	ColorHarmony int `json:"color_harmony"`
}

type Result struct {
	Status              string          `json:"status"`
	PrimaryCategory     CategoryScore   `json:"primary_category"`
	SecondaryCategory   CategoryScore   `json:"secondary_category"`
	Level               string          `json:"level"`
	LevelTitle          string          `json:"level_title"`
	Score               int             `json:"score"`
	Confidence          int             `json:"confidence"`
	Metrics             Metrics         `json:"metrics"`
	Summary             string          `json:"summary"`
	Recommendations     []string        `json:"recommendations"`
	Explanations        []string        `json:"explanations,omitempty"`
	Provisional         bool            `json:"provisional,omitempty"`
	AssessmentNote      string          `json:"assessment_note,omitempty"`
	AssessmentScope     string          `json:"assessment_scope,omitempty"`
	Components          []CategoryScore `json:"components,omitempty"`
	LowConfidenceReason string          `json:"low_confidence_reason,omitempty"`
}

type featureName string
type rule struct {
	Code, Title string
	Weights     map[featureName]float64
}

type Engine struct{ rules []rule }

func NewEngine() *Engine {
	return &Engine{rules: []rule{
		{Code: "urban_classic", Title: "Бизнесмен", Weights: map[featureName]float64{"formality": .30, "coherence": .25, "classic": .25, "harmony": .15, "accessories": .05}},
		{Code: "healthy_casual", Title: "Стиляга", Weights: map[featureName]float64{"coherence": .35, "harmony": .25, "sportiness": .15, "classic": .15, "originality": .10}},
		{Code: "sport_chic", Title: "Спортик", Weights: map[featureName]float64{"sport_chic": .35, "sportiness": .30, "coherence": .20, "originality": .15}},
		{Code: "streetwear", Title: "Абушка", Weights: map[featureName]float64{"streetwear": .45, "originality": .25, "sportiness": .15, "accessories": .15}},
		{Code: "alternative", Title: "Нефор", Weights: map[featureName]float64{"alternative": .45, "originality": .20, "accessories": .10, "layering": .05, "unnatural_hair_color": .10, "piercing": .05, "hair_below_ears": .03, "hair_over_eyes": .02}},
		{Code: "goth", Title: "Гот", Weights: map[featureName]float64{"goth": .45, "darkness": .30, "coherence": .15, "layering": .10}},
		{Code: "retro_90s", Title: "Бандит 90-х", Weights: map[featureName]float64{"retro_90s": .70, "sportiness": .15, "accessories": .10, "originality": .05}},
	}}
}

func (e *Engine) Evaluate(features analysis.VisualFeatures) (Result, error) {
	if err := features.Validate(); err != nil {
		return Result{}, err
	}
	if len(e.rules) < 2 {
		return Result{}, errors.New("at least two category rules are required")
	}
	ranked := make([]CategoryScore, 0, len(e.rules))
	for _, category := range e.rules {
		score := 0.0
		for name, weight := range category.Weights {
			score += float64(value(features, name)) * weight
		}
		if category.Code == "alternative" && features.HasAlternativeCombination() {
			score += 40
		}
		ranked = append(ranked, CategoryScore{Code: category.Code, Title: category.Title, Score: clamp(int(math.Round(score)))})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].Code < ranked[j].Code
		}
		return ranked[i].Score > ranked[j].Score
	})
	if features.OutfitVisibility == 0 && features.HasAlternativeCombination() {
		for i, c := range ranked {
			if c.Code == "alternative" {
				ranked = append([]CategoryScore{c}, append(ranked[:i], ranked[i+1:]...)...)
				break
			}
		}
	}
	if features.MuscularVisible {
		muscle := CategoryScore{Code: "muscular", Title: "Качок", Score: 100}
		if features.OutfitVisibility > 0 && strongOutfitStyle(features, ranked[0]) {
			ranked = append(ranked[:1], append([]CategoryScore{muscle}, ranked[1:]...)...)
		} else {
			ranked = append([]CategoryScore{muscle}, ranked...)
		}
	}
	if features.HasGentlePose() && (features.OutfitVisibility == 0 || !strongOutfitStyle(features, ranked[0])) {
		ranked = append([]CategoryScore{{Code: "pupsik", Title: "Пупсик", Score: 100}}, ranked...)
	}
	if features.HasActionHeroCombination() {
		ranked = append([]CategoryScore{{Code: "action_hero", Title: "Герой боевика", Score: 100}}, ranked...)
	}
	if features.SeriousIndexPose {
		ranked = append([]CategoryScore{{Code: "cool_pose", Title: "Типо крутой", Score: 100}}, ranked...)
	}
	if features.SkullcapVisible && features.BeardVisible {
		ranked = append([]CategoryScore{{Code: "mula", Title: "Мула", Score: 100}}, ranked...)
	}
	if features.PlayfulGrimace {
		ranked = append([]CategoryScore{{Code: "clown", Title: "Клоун", Score: 100}}, ranked...)
	}
	overall := clamp(int(math.Round(float64(features.OutfitCoherence)*.45 + float64(features.ColorHarmony)*.30 + float64(features.Originality)*.25)))
	result := Result{
		Status: "completed", PrimaryCategory: ranked[0], SecondaryCategory: ranked[1],
		Level: level(overall).code, LevelTitle: level(overall).title, Score: overall, Confidence: features.Confidence,
		Metrics: Metrics{Coherence: features.OutfitCoherence, Originality: features.Originality, ColorHarmony: features.ColorHarmony},
		Summary: summaryForCategory(ranked[0].Code, features), Recommendations: recommendationsForFeatures(features),
		Explanations: e.explanations(ranked[0].Code, features),
	}
	e.mix(&result, ranked, features)
	if features.OutfitVisibility < 60 || features.ImageQuality < 55 || features.Confidence < 70 {
		result.Provisional = true
		result.AssessmentNote = "Примерный вердикт: некоторые детали трудно разобрать."
		if features.OutfitVisibility < 60 {
			result.AssessmentNote = "Видна только часть образа — вердикт по тому, что попало в кадр."
		}
	}
	if features.OutfitVisibility == 0 && features.HasPortraitCue() {
		result.AssessmentScope = "portrait"
		result.AssessmentNote = "Вердикт по позе или деталям портрета. Одежду не видно, поэтому баллы за образ не считаем."
		result.Score, result.Metrics = 0, Metrics{}
		result.Level, result.LevelTitle = "", ""
		result.SecondaryCategory = CategoryScore{}
	}
	if len(result.Explanations) == 0 {
		result.Provisional = true
		result.AssessmentNote = "Ярких признаков стиля мало — это только примерный вариант."
	}
	reason := features.LowConfidenceReason()
	if reason == "" && ranked[0].Score == 0 {
		reason = "insufficient_style_evidence"
	}
	if reason != "" {
		result.Status = "low_confidence"
		result.Explanations = nil
		result.Components = nil
		result.Provisional, result.AssessmentNote, result.AssessmentScope = false, "", ""
		result.LowConfidenceReason = reason
		result.PrimaryCategory = CategoryScore{}
		result.SecondaryCategory = CategoryScore{}
		result.Score, result.Confidence = 0, features.Confidence
		result.Level, result.LevelTitle = "", ""
		result.Metrics = Metrics{}
		switch reason {
		case "insufficient_style_evidence":
			result.Summary = "На этом кадре пока не нашли деталей, по которым можно выбрать стиль."
			result.Recommendations = []string{"Попробуй кадр, где лучше видны одежда или поза"}
		case "single_person_required":
			result.Summary = "Нужен кадр с одним человеком, чтобы не смешать разные образы."
			result.Recommendations = []string{"Выбери фото с одним человеком или обрежь кадр"}
		case "ai_confidence_too_low":
			result.Summary = "Детали пока не удалось уверенно разобрать. Попробуй другой кадр."
			result.Recommendations = []string{"Покажи видимые детали образа на другом фото"}
		default:
			result.Summary = "Не разглядели одежду. Покажи её на другом фото."
			result.Recommendations = []string{"Покажи хотя бы одну вещь одежды"}
		}
	}
	return result, nil
}

// A smile can provide a playful verdict for an otherwise ordinary outfit,
// but must not erase a clearly evidenced clothing category.
func strongOutfitStyle(f analysis.VisualFeatures, category CategoryScore) bool {
	if category.Score < 65 {
		return false
	}
	switch category.Code {
	case "goth":
		return f.Signals.Goth >= 75
	case "alternative":
		return f.Signals.Alternative >= 75
	case "retro_90s":
		return f.Signals.Retro90s >= 75
	case "sport_chic":
		return f.Signals.SportChic >= 75
	case "streetwear":
		return f.Signals.Streetwear >= 75
	case "urban_classic":
		return f.Signals.Classic >= 75 && f.Formality >= 75
	default:
		return false
	}
}

func (e *Engine) ruleByCode(code string) rule {
	for _, item := range e.rules {
		if item.Code == code {
			return item
		}
	}
	return rule{}
}

func value(f analysis.VisualFeatures, name featureName) int {
	switch name {
	case "formality":
		return f.Formality
	case "coherence":
		return f.OutfitCoherence
	case "classic":
		return f.Signals.Classic
	case "harmony":
		return f.ColorHarmony
	case "accessories":
		return f.AccessoryIntensity
	case "sport_chic":
		return f.Signals.SportChic
	case "sportiness":
		return f.Sportiness
	case "originality":
		return f.Originality
	case "streetwear":
		return f.Signals.Streetwear
	case "alternative":
		return f.Signals.Alternative
	case "unnatural_hair_color":
		if f.UnnaturalHairColor {
			return 100
		}
		return 0
	case "piercing":
		if f.PiercingVisible {
			return 100
		}
		return 0
	case "hair_below_ears":
		if f.HairBelowEars {
			return 100
		}
		return 0
	case "hair_over_eyes":
		if f.HairOverEyes {
			return 100
		}
		return 0
	case "layering":
		return f.Layering
	case "goth":
		return f.Signals.Goth
	case "darkness":
		return f.Darkness
	case "retro_90s":
		return f.Signals.Retro90s
	default:
		return 0
	}
}

type levelDefinition struct {
	min         int
	code, title string
}

func level(score int) levelDefinition {
	levels := []levelDefinition{{0, "experimenter", "Сегодня так задумано"}, {40, "base", "Уже можно выходить"}, {60, "healthy", "Здравый пацан"}, {75, "styler", "Стиляга"}, {90, "icon", "Остановите фотографа"}}
	selected := levels[0]
	for _, item := range levels {
		if score >= item.min {
			selected = item
		}
	}
	return selected
}
func clamp(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
