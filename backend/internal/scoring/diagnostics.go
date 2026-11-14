package scoring

import (
	"sort"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

// Diagnostic is only served by the explicitly enabled, loopback-only development
// path. It contains normalized evidence, never photos or raw provider responses.
type Diagnostic struct {
	Available           bool                     `json:"available"`
	Source              string                   `json:"source"`
	Features            *analysis.VisualFeatures `json:"features,omitempty"`
	Candidates          []CategoryScore          `json:"candidates,omitempty"`
	SpecialChecks       []DiagnosticCheck        `json:"special_checks,omitempty"`
	Selected            []CategoryScore          `json:"selected,omitempty"`
	DecisionReasons     []DecisionReason         `json:"decision_reasons,omitempty"`
	Provisional         bool                     `json:"provisional"`
	AssessmentNote      string                   `json:"assessment_note,omitempty"`
	LowConfidenceReason string                   `json:"low_confidence_reason,omitempty"`
}

// DecisionReason records an executed decision branch, not an inferred explanation.
// It is private diagnostic data and never part of Result or saved history.
type DecisionReason struct {
	Code     string         `json:"code"`
	Category *CategoryScore `json:"category,omitempty"`
	Other    *CategoryScore `json:"other,omitempty"`
	Reason   string         `json:"reason"`
}

type decisionTrace struct{ reasons []DecisionReason }

func (t *decisionTrace) add(code string, category, other CategoryScore, reason string) {
	if t == nil {
		return
	}
	r := DecisionReason{Code: code, Reason: reason}
	if category.Code != "" {
		r.Category = &category
	}
	if other.Code != "" {
		r.Other = &other
	}
	t.reasons = append(t.reasons, r)
}

type DiagnosticRequirement struct {
	Feature string `json:"feature"`
	Label   string `json:"label"`
	Met     bool   `json:"met"`
}

type DiagnosticCheck struct {
	Code         string                  `json:"code"`
	Title        string                  `json:"title"`
	Eligible     bool                    `json:"eligible"`
	Requirements []DiagnosticRequirement `json:"requirements"`
	Note         string                  `json:"note"`
}

func (e *Engine) Diagnose(f analysis.VisualFeatures, result Result) *Diagnostic {
	// Replay the same deterministic path with an optional observer. No separate
	// implementation of priorities or mixing is used for diagnostic explanations.
	trace := &decisionTrace{}
	if _, err := e.evaluate(f, trace); err != nil {
		trace.reasons = nil
	}
	// The only unconstrained model strings are color names; omit them rather
	// than echoing arbitrary provider text in diagnostics.
	f.DominantColors = nil
	d := &Diagnostic{Available: true, Source: "ai", Features: &f, Provisional: result.Provisional, AssessmentNote: result.AssessmentNote, LowConfidenceReason: result.LowConfidenceReason}
	d.DecisionReasons = trace.reasons
	for _, rule := range e.rules {
		d.Candidates = append(d.Candidates, CategoryScore{Code: rule.Code, Title: rule.Title, Score: categoryScore(f, rule)})
	}
	sort.Slice(d.Candidates, func(i, j int) bool {
		if d.Candidates[i].Score == d.Candidates[j].Score {
			return d.Candidates[i].Code < d.Candidates[j].Code
		}
		return d.Candidates[i].Score > d.Candidates[j].Score
	})
	add := func(code, title string, requirements ...DiagnosticRequirement) {
		eligible := true
		for _, r := range requirements {
			eligible = eligible && r.Met
		}
		d.SpecialChecks = append(d.SpecialChecks, DiagnosticCheck{Code: code, Title: title, Eligible: eligible, Requirements: requirements, Note: "Это проверка признаков, а не гарантия выбора. Действуют приоритеты, правила смешивания и ограничения качества. Неподтверждённый признак может быть неразличим."})
	}
	add("action_hero", "Герой боевика", DiagnosticRequirement{"short_hair_or_bald_visible", "Короткая стрижка или лысина подтверждена", f.ShortHairOrBaldVisible}, DiagnosticRequirement{"beard_visible", "Борода подтверждена", f.BeardVisible}, DiagnosticRequirement{"head_scar_visible", "Шрам на голове или лице подтверждён", f.HeadScarVisible})
	add("clown", "Клоун", DiagnosticRequirement{"playful_grimace_visible", "Намеренная гримаса подтверждена", f.PlayfulGrimace})
	add("mula", "Мула", DiagnosticRequirement{"skullcap_visible", "Тюбетейка подтверждена", f.SkullcapVisible}, DiagnosticRequirement{"beard_visible", "Борода подтверждена", f.BeardVisible})
	add("cool_pose", "Типо крутой", DiagnosticRequirement{"serious_index_pose_visible", "Указательный палец у лица и серьёзное выражение подтверждены", f.SeriousIndexPose})
	add("pupsik", "Пупсик", DiagnosticRequirement{"gentle_smile_visible", "Небольшая расслабленная улыбка подтверждена", f.GentleSmile}, DiagnosticRequirement{"relaxed_head_tilt_visible", "Наклон головы и расслабленные брови подтверждены", f.RelaxedHeadTilt})
	add("muscular", "Качок", DiagnosticRequirement{"muscular_visible", "Выраженная мускулатура подтверждена", f.MuscularVisible})
	add("alternative_combination", "Дополнительные 40 баллов Нефору", DiagnosticRequirement{"hair_below_ears_visible", "Волосы ниже ушей подтверждены", f.HairBelowEars}, DiagnosticRequirement{"beard_absent_visible", "Видимое отсутствие бороды подтверждено", f.BeardAbsentVisible}, DiagnosticRequirement{"non_muscular_visible", "Телосложение без выраженной мускулатуры видно", f.NonMuscularVisible}, DiagnosticRequirement{"muscular_visible_false", "Признак выраженной мускулатуры выключен", !f.MuscularVisible})
	if len(result.Components) > 0 {
		d.Selected = append([]CategoryScore(nil), result.Components...)
	} else if result.PrimaryCategory.Code != "" {
		d.Selected = []CategoryScore{result.PrimaryCategory}
	}
	return d
}
