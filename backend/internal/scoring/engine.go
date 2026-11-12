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
	Status              string        `json:"status"`
	PrimaryCategory     CategoryScore `json:"primary_category"`
	SecondaryCategory   CategoryScore `json:"secondary_category"`
	Level               string        `json:"level"`
	LevelTitle          string        `json:"level_title"`
	Score               int           `json:"score"`
	Confidence          int           `json:"confidence"`
	Metrics             Metrics       `json:"metrics"`
	Summary             string        `json:"summary"`
	Recommendations     []string      `json:"recommendations"`
	LowConfidenceReason string        `json:"low_confidence_reason,omitempty"`
}

type featureName string
type rule struct {
	Code, Title, Summary string
	Weights              map[featureName]float64
}

type Engine struct{ rules []rule }

func NewEngine() *Engine {
	return &Engine{rules: []rule{
		{Code: "urban_classic", Title: "Строгий городской", Summary: "Собранный городской образ: спокойно, уверенно и без лишнего шума.", Weights: map[featureName]float64{"formality": .30, "coherence": .25, "classic": .25, "harmony": .15, "accessories": .05}},
		{Code: "healthy_casual", Title: "Здравый casual", Summary: "Удобный и цельный образ, который выглядит уверенно без лишней демонстративности.", Weights: map[featureName]float64{"coherence": .35, "harmony": .25, "sportiness": .15, "classic": .15, "originality": .10}},
		{Code: "sport_chic", Title: "Спорт-шик", Summary: "Динамичный образ, в котором комфорт работает вместе с заметными деталями.", Weights: map[featureName]float64{"sport_chic": .35, "sportiness": .30, "coherence": .20, "originality": .15}},
		{Code: "streetwear", Title: "Streetwear", Summary: "Городская энергия, свободные сочетания и выразительные детали.", Weights: map[featureName]float64{"streetwear": .45, "originality": .25, "sportiness": .15, "accessories": .15}},
		{Code: "alternative", Title: "Нефор", Summary: "Нестандартный образ с выразительными деталями и собственным визуальным языком.", Weights: map[featureName]float64{"alternative": .50, "originality": .25, "accessories": .15, "layering": .10}},
		{Code: "goth", Title: "Гот", Summary: "Тёмная палитра, драматичный контраст и собранная альтернативная эстетика.", Weights: map[featureName]float64{"goth": .45, "darkness": .30, "coherence": .15, "layering": .10}},
		{Code: "traditional_accent", Title: "Традиционный акцент", Summary: "Современный образ дополнен заметными традиционными элементами.", Weights: map[featureName]float64{"traditional": .55, "coherence": .25, "harmony": .20}},
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
		ranked = append(ranked, CategoryScore{Code: category.Code, Title: category.Title, Score: clamp(int(math.Round(score)))})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].Code < ranked[j].Code
		}
		return ranked[i].Score > ranked[j].Score
	})
	overall := clamp(int(math.Round(float64(features.OutfitCoherence)*.35 + float64(features.ColorHarmony)*.20 + float64(features.Originality)*.20 + float64(features.ImageQuality)*.15 + float64(features.Confidence)*.10)))
	primaryRule := e.ruleByCode(ranked[0].Code)
	result := Result{
		Status: "completed", PrimaryCategory: ranked[0], SecondaryCategory: ranked[1],
		Level: level(overall).code, LevelTitle: level(overall).title, Score: overall, Confidence: features.Confidence,
		Metrics: Metrics{Coherence: features.OutfitCoherence, Originality: features.Originality, ColorHarmony: features.ColorHarmony},
		Summary: primaryRule.Summary, Recommendations: recommendations(features),
	}
	if reason := features.LowConfidenceReason(); reason != "" {
		result.Status = "low_confidence"
		result.LowConfidenceReason = reason
		result.PrimaryCategory = CategoryScore{}
		result.SecondaryCategory = CategoryScore{}
		result.Summary = "Не удалось уверенно определить стиль образа."
		result.Recommendations = []string{"Сделай фото при хорошем освещении", "Покажи одежду целиком и оставь в кадре одного человека"}
	}
	return result, nil
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
	case "layering":
		return f.Layering
	case "goth":
		return f.Signals.Goth
	case "darkness":
		return f.Darkness
	case "traditional":
		return f.TraditionalElements
	default:
		return 0
	}
}

type levelDefinition struct {
	min         int
	code, title string
}

func level(score int) levelDefinition {
	levels := []levelDefinition{{0, "experimenter", "Экспериментатор"}, {50, "base", "Базовый вайб"}, {65, "healthy", "Здравый образ"}, {80, "styler", "Стиляга"}, {92, "icon", "Икона вайба"}}
	selected := levels[0]
	for _, item := range levels {
		if score >= item.min {
			selected = item
		}
	}
	return selected
}
func recommendations(f analysis.VisualFeatures) []string {
	items := make([]string, 0, 2)
	if f.ColorHarmony < 65 {
		items = append(items, "Сократи палитру до двух-трёх основных цветов")
	}
	if f.AccessoryIntensity < 35 {
		items = append(items, "Добавь один выразительный аксессуар")
	}
	if f.OutfitCoherence < 70 {
		items = append(items, "Повтори один цвет или материал в нескольких деталях")
	}
	if len(items) == 0 {
		items = append(items, "Образ уже собран — сохрани баланс деталей")
	}
	if len(items) > 2 {
		return items[:2]
	}
	return items
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
