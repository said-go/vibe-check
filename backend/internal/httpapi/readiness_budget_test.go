package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestRedisReadinessDoesNotConsumeBudget(t *testing.T) {
	r := miniredis.RunT(t)
	b, err := NewRedisBudget("redis://"+r.Addr(), "readiness:test", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for i := 0; i < 3; i++ {
		if err := b.Check(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if r.Exists("readiness:test") {
		t.Fatal("readiness created budget key")
	}
	if n, err := b.Take(context.Background()); err != nil || n != 0 {
		t.Fatal("probe consumed capacity")
	}
	ttl := r.TTL("readiness:test")
	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	value, _ := r.Get("readiness:test")
	if value != "1" || r.TTL("readiness:test") != ttl {
		t.Fatal("probe modified budget")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if b.Check(ctx) == nil {
		t.Fatal("cancelled probe succeeded")
	}
}

func TestReadinessRedisOutagePreservesPresetPath(t *testing.T) {
	r := miniredis.RunT(t)
	b, err := NewRedisBudget("redis://"+r.Addr(), "readiness:test", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewWithLimits(logger, 10<<20, fakeVision{}, Limits{SharedBudget: b})
	probe := func(handler http.Handler, wantStatus int, wantAI, wantBudget bool) {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		var body struct {
			AI         bool `json:"ai_available"`
			Budget     bool `json:"shared_budget_available"`
			Configured bool `json:"shared_budget_configured"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != wantStatus || body.AI != wantAI || body.Budget != wantBudget || !body.Configured {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("readiness may be cached")
		}
	}
	probe(h, 200, true, true)
	r.Close()
	probe(h, 503, false, false)
	presetHandler := NewWithPresets(logger, 10<<20, fakeVision{}, Limits{SharedBudget: b}, PhotoPresets{{originalSHA256: "synthetic"}})
	probe(presetHandler, 200, false, false)
}
