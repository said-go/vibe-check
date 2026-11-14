package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/analysis"
	"github.com/said-go/vibe-check/backend/internal/imageproc"
	"github.com/said-go/vibe-check/backend/internal/scoring"
)

func diagnosticRequest(t *testing.T, key string, debug bool) *http.Request {
	r := makeImageRequest(t, []string{"true"}, [][]byte{imageFixture(t)}, "image/jpeg")
	r.RemoteAddr = "127.0.0.1:12345"
	r.Host = "localhost:8080"
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if debug {
		r.Header.Set(diagnosticHeader, "1")
	}
	return r
}

func TestDiagnosticsDenyNonlocalDisabledAndForwardedRequests(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		mutate  func(*http.Request)
	}{
		{"disabled", false, func(r *http.Request) {}},
		{"remote peer", true, func(r *http.Request) { r.RemoteAddr = "192.0.2.1:1234" }},
		{"rebound host", true, func(r *http.Request) { r.Host = "attacker.example:8080" }},
		{"forwarded for", true, func(r *http.Request) { r.Header.Set("X-Forwarded-For", "127.0.0.1") }},
		{"forwarded", true, func(r *http.Request) { r.Header.Set("Forwarded", "for=127.0.0.1") }},
		{"cross origin", true, func(r *http.Request) { r.Header.Set("Origin", "https://attacker.example") }},
		{"invalid option", true, func(r *http.Request) { r.Header.Set(diagnosticHeader, "true") }},
		{"duplicate option", true, func(r *http.Request) { r.Header.Add(diagnosticHeader, "1") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h := NewWithPresetsAndDiagnostics(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, fakeVision{calls: &calls}, DefaultLimits, nil, tc.enabled)
			r := diagnosticRequest(t, "", true)
			tc.mutate(r)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden || calls != 0 || !strings.Contains(w.Body.String(), "diagnostics_forbidden") {
				t.Fatalf("denial failed status=%d calls=%d", w.Code, calls)
			}
		})
	}
}

func TestDiagnosticSnapshotDoesNotLeakViaIdempotencyOrLogs(t *testing.T) {
	f, err := analysis.DecodeFeatures(strings.NewReader(validPayload))
	if err != nil {
		t.Fatal(err)
	}
	f.BeardVisible, f.ShortHairOrBaldVisible = true, true
	f.DominantColors = []string{"RAW_PROVIDER_TEXT_MUST_NOT_BE_ECHOED"}
	calls := 0
	var logs bytes.Buffer
	h := NewWithPresetsAndDiagnostics(slog.New(slog.NewJSONHandler(&logs, nil)), 10<<20, fakeVision{features: f, calls: &calls}, DefaultLimits, nil, true)
	key := "private-diagnostics-retry-123"
	w := httptest.NewRecorder()
	r := diagnosticRequest(t, key, true)
	r.Header.Set("Origin", "http://localhost:8080")
	h.ServeHTTP(w, r)
	var response analysisResponse
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || response.Diagnostics == nil || !response.Diagnostics.Available || response.Diagnostics.Features == nil || len(response.Diagnostics.Candidates) != 7 {
		t.Fatalf("snapshot missing: %s", w.Body.String())
	}
	check := response.Diagnostics.SpecialChecks[0]
	if check.Code != "action_hero" || check.Eligible || !check.Requirements[0].Met || !check.Requirements[1].Met || check.Requirements[2].Met {
		t.Fatalf("wrong missing-cue trace: %+v", check)
	}
	if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "RAW_PROVIDER_TEXT") {
		t.Fatal("diagnostic cache/text leak")
	}
	for _, remote := range []bool{false, true} {
		r = diagnosticRequest(t, key, false)
		if remote {
			r.RemoteAddr = "192.0.2.2:1234"
			r.Host = "public.example"
		}
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || strings.Contains(w.Body.String(), `"diagnostics"`) || strings.Contains(w.Body.String(), `"head_scar_visible"`) {
			t.Fatalf("cached trace leaked: %s", w.Body.String())
		}
	}
	if calls != 1 {
		t.Fatalf("retry billed again: %d", calls)
	}
	if strings.Contains(logs.String(), "head_scar_visible") || strings.Contains(logs.String(), "RAW_PROVIDER_TEXT") {
		t.Fatal("feature data in logs")
	}
}

func TestNormalCachedResultDoesNotTriggerNewAIForDiagnostics(t *testing.T) {
	f, _ := analysis.DecodeFeatures(strings.NewReader(validPayload))
	calls := 0
	h := NewWithPresetsAndDiagnostics(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, fakeVision{features: f, calls: &calls}, DefaultLimits, nil, true)
	for _, debug := range []bool{false, true} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, diagnosticRequest(t, "normal-then-debug-12345", debug))
		if w.Code != 200 {
			t.Fatalf("status %d", w.Code)
		}
		if debug {
			var response analysisResponse
			_ = json.Unmarshal(w.Body.Bytes(), &response)
			if response.Diagnostics == nil || response.Diagnostics.Available || response.Diagnostics.Source != "not_recorded" {
				t.Fatal("normal result invented diagnostic snapshot")
			}
		}
	}
	if calls != 1 {
		t.Fatal("diagnostics repeated a paid call")
	}
}

func TestPresetDiagnosticsDoNotInventAIFeatures(t *testing.T) {
	photo := imageFixture(t)
	clean, err := imageproc.New().Sanitize(bytes.NewReader(photo), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := visualHash(clean.JPEG)
	if err != nil {
		t.Fatal(err)
	}
	presets := PhotoPresets{{originalSHA256: strings.Repeat("a", 64), visualHash: hash, result: scoring.Result{Status: "completed", PrimaryCategory: scoring.CategoryScore{Code: "manual", Title: "Manual"}}}}
	calls := 0
	h := NewWithPresetsAndDiagnostics(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, fakeVision{calls: &calls}, DefaultLimits, presets, true)
	r := diagnosticRequest(t, "", true)
	r.Header.Set("X-Original-Image-SHA256", strings.Repeat("a", 64))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var response analysisResponse
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	if w.Code != 200 || calls != 0 || response.Diagnostics == nil || response.Diagnostics.Available || response.Diagnostics.Source != "preset" || response.Diagnostics.Features != nil {
		t.Fatalf("preset invented AI: %s", w.Body.String())
	}
}

func TestDiagnosticsReadinessUsesSameLocalBoundary(t *testing.T) {
	h := NewWithPresetsAndDiagnostics(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, fakeVision{}, DefaultLimits, nil, true)
	for _, tc := range []struct {
		remote, host string
		want         bool
	}{{"127.0.0.1:1234", "localhost:8080", true}, {"[::1]:1234", "[::1]:8080", true}, {"192.0.2.1:1234", "localhost:8080", false}, {"127.0.0.1:1234", "evil.example", false}} {
		r := httptest.NewRequest("GET", "/readyz", nil)
		r.RemoteAddr = tc.remote
		r.Host = tc.host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var body struct {
			Available bool `json:"diagnostics_available"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.Available != tc.want {
			t.Fatalf("readiness local boundary mismatch: %+v", tc)
		}
	}
}
