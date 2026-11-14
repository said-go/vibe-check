package scoring

import (
	"reflect"
	"strings"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

func TestReportCopyVariesWithVisualEvidenceAndStaysStable(t *testing.T) {
	f := baseFeatures()
	f.RelaxedHeadTilt = true
	f.ShortHairOrBaldVisible, f.HeadScarVisible = true, true
	f.MuscularVisible = true
	f.SkullcapVisible, f.BeardVisible, f.PlayfulGrimace, f.SeriousIndexPose, f.GentleSmile = true, true, true, true, true
	for code := range categorySummaries {
		t.Run(code, func(t *testing.T) {
			seen := map[string]bool{}
			for value := 0; value <= 100; value++ {
				f.Originality = value
				got := summaryForCategory(code, f)
				if got == "" || got != summaryForCategory(code, f) {
					t.Fatal("summary must be nonempty and deterministic")
				}
				seen[got] = true
			}
			if len(seen) < 3 {
				t.Fatalf("expected varied captions, got %d", len(seen))
			}
		})
	}
}

func TestReportJokeDoesNotDependOnPhotoQuality(t *testing.T) {
	f := baseFeatures()
	want := summaryForCategory("alternative", f)
	f.ImageQuality, f.OutfitVisibility, f.Confidence = 30, 25, 25
	if got := summaryForCategory("alternative", f); got != want {
		t.Fatalf("quality changed the joke: %q != %q", got, want)
	}
}

func TestReportDoesNotInventSpecificAppearanceDetails(t *testing.T) {
	f := analysis.VisualFeatures{}
	for value := 0; value <= 100; value++ {
		f.Originality = value
		got := strings.ToLower(summaryForCategory("alternative", f))
		for _, absent := range []string{"пирсинг", "волос", "бород", "тюбетей", "маск"} {
			if strings.Contains(got, absent) {
				t.Fatalf("caption invented %q: %q", absent, got)
			}
		}
	}
	for _, code := range []string{"mula", "clown", "cool_pose", "pupsik", "unknown"} {
		if got := summaryForCategory(code, f); got != "Смотрим по деталям, которые попали в кадр." {
			t.Fatalf("unsupported category %s described missing evidence: %q", code, got)
		}
	}
}

func TestRecommendationsDoNotJudgeUnseenClothing(t *testing.T) {
	for _, visibility := range []int{0, 1, 30, 59} {
		f := baseFeatures()
		f.OutfitVisibility = visibility
		f.ColorHarmony, f.OutfitCoherence, f.AccessoryIntensity = 0, 0, 0
		got := recommendationsForFeatures(f)
		want := []string{"Хочешь оценить весь образ — поставь камеру чуть дальше"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("visibility %d prompted outfit changes: %v", visibility, got)
		}
		f.ImageQuality = 30
		if got = recommendationsForFeatures(f); len(got) != 2 || !strings.Contains(got[1], "свету") {
			t.Fatalf("unclear crop should get a photo tip: %v", got)
		}
	}
}

func TestRecommendationsKeepUncertainOutfitAdviceToPhotoTips(t *testing.T) {
	for _, f := range []analysis.VisualFeatures{
		{OutfitVisibility: 100, ImageQuality: 30, Confidence: 80},
		{OutfitVisibility: 100, ImageQuality: 90, Confidence: 25},
		{OutfitVisibility: 100, ImageQuality: 90, Confidence: 69},
	} {
		got := recommendationsForFeatures(f)
		if len(got) != 1 || !strings.Contains(got[0], "ракурс") {
			t.Fatalf("uncertain photo should not prompt clothing changes: %v", got)
		}
	}
}

func TestRecommendationsAreShortAndOptional(t *testing.T) {
	f := baseFeatures()
	f.ColorHarmony, f.OutfitCoherence, f.AccessoryIntensity = 10, 10, 10
	got := recommendationsForFeatures(f)
	if len(got) != 2 {
		t.Fatalf("expected no more than two tips, got %v", got)
	}
	for _, tip := range got {
		if !strings.HasPrefix(tip, "Попробуй") {
			t.Fatalf("expected optional suggestion: %q", tip)
		}
	}
	f = baseFeatures()
	got = recommendationsForFeatures(f)
	if len(got) != 1 || !strings.Contains(got[0], "оставить как есть") {
		t.Fatalf("coherent outfit should not need invented fixes: %v", got)
	}
}
