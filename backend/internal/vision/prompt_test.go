package vision

import (
	"strings"
	"testing"
)

// This guards prompt policy only; it does not test real image recognition.
func TestScarPromptRequiresVisibleEvidenceWithoutBlanketStripeExclusion(t *testing.T) {
	var scarRule string
	for _, line := range strings.Split(analysisPrompt, "\n") {
		if strings.HasPrefix(line, "head_scar_visible =") {
			scarRule = line
			break
		}
	}
	for _, required := range []string{
		"отчётливо виден след, похожий на шрам",
		"Пробор, тень, морщина, блик и дефект изображения не подходят",
		"Одной светлой полоски между волосами недостаточно",
		"при сомнении false",
		"Не определяй причину, травму или медицинскую историю",
	} {
		if !strings.Contains(scarRule, required) {
			t.Errorf("scar rule is missing evidence or uncertainty guard: %q", required)
		}
	}
	if strings.Contains(scarRule, "Пробор, светлая полоска между волосами,") {
		t.Error("scar rule must not classify every light stripe as a non-scar")
	}
}
