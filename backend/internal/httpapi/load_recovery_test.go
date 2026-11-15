package httpapi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/said-go/vibe-check/backend/internal/analysis"
	"github.com/said-go/vibe-check/backend/internal/vision"
)

// This fixture uses real loopback HTTP but never calls an external AI service.
type loadProvider struct {
	features analysis.VisualFeatures
	started  chan struct{}
	ended    chan struct{}
	gate     chan struct{}
	active   atomic.Int32
	peak     atomic.Int32
	calls    atomic.Int32
	fail     atomic.Bool
}

func (p *loadProvider) Analyze(ctx context.Context, _ []byte) (analysis.VisualFeatures, error) {
	p.calls.Add(1)
	active := p.active.Add(1)
	for peak := p.peak.Load(); active > peak; peak = p.peak.Load() {
		if p.peak.CompareAndSwap(peak, active) {
			break
		}
	}
	p.started <- struct{}{}
	defer func() { p.active.Add(-1); p.ended <- struct{}{} }()
	if p.fail.Load() {
		return analysis.VisualFeatures{}, vision.ErrUnavailable
	}
	select {
	case <-ctx.Done():
		return analysis.VisualFeatures{}, ctx.Err()
	case <-p.gate:
		return p.features, nil
	}
}

func loadSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for %s", label)
	}
}

func TestHTTPLoadRejectsOverflowAndRecoversAfterCancellation(t *testing.T) {
	features, err := analysis.DecodeFeatures(strings.NewReader(validPayload))
	if err != nil {
		t.Fatal(err)
	}
	p := &loadProvider{features: features, started: make(chan struct{}, 64), ended: make(chan struct{}, 64), gate: make(chan struct{})}
	handler := NewWithLimits(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, p, Limits{PerHour: 6, PerClientMinute: 100, Concurrent: 2})
	// Observe handler completion separately from provider return: release is a
	// handler defer and must have run before testing the recovered capacity.
	completed := make(chan struct{}, 128)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r); completed <- struct{}{} }))
	defer srv.Close()
	client := srv.Client()
	client.Timeout = 5 * time.Second
	template := makeImageRequest(t, []string{"true"}, [][]byte{imageFixture(t)}, "image/jpeg")
	body, err := io.ReadAll(template.Body)
	if err != nil {
		t.Fatal(err)
	}
	post := func(ctx context.Context) (int, string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/v1/analyses", bytes.NewReader(body))
		if err != nil {
			return 0, "", err
		}
		req.Header.Set("Content-Type", template.Header.Get("Content-Type"))
		resp, err := client.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer resp.Body.Close()
		_, err = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, resp.Header.Get("Retry-After"), err
	}
	var leaders sync.WaitGroup
	var cancels []context.CancelFunc
	for range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		defer cancel()
		leaders.Add(1)
		go func() { defer leaders.Done(); _, _, _ = post(ctx) }()
	}
	loadSignal(t, p.started, "first provider")
	loadSignal(t, p.started, "second provider")
	const burst = 48
	var overflow sync.WaitGroup
	var denied atomic.Int32
	started := time.Now()
	for range burst {
		overflow.Add(1)
		go func() {
			defer overflow.Done()
			status, retry, err := post(context.Background())
			if err != nil || status != http.StatusTooManyRequests || retry != "1" {
				t.Errorf("overflow status=%d retry=%q err=%v", status, retry, err)
				return
			}
			denied.Add(1)
		}()
	}
	overflow.Wait()
	for range burst {
		loadSignal(t, completed, "overflow completion")
	}
	if p.calls.Load() != 2 || p.peak.Load() != 2 || denied.Load() != burst {
		t.Fatalf("calls=%d peak=%d denied=%d", p.calls.Load(), p.peak.Load(), denied.Load())
	}
	for _, cancel := range cancels {
		cancel()
	}
	leaders.Wait()
	for range 2 {
		loadSignal(t, p.ended, "cancelled provider")
		loadSignal(t, completed, "cancelled handler")
	}
	if p.active.Load() != 0 {
		t.Fatal("cancelled providers still active")
	}
	// Cancellation after entering AI spends its slot; an unavailable provider
	// also spends a slot, but neither failure may leak concurrency capacity.
	p.fail.Store(true)
	if status, _, err := post(context.Background()); err != nil || status != 503 {
		t.Fatalf("failure recovery status=%d err=%v", status, err)
	}
	loadSignal(t, completed, "failed handler")
	p.fail.Store(false)
	close(p.gate)
	for range 3 {
		if status, _, err := post(context.Background()); err != nil || status != 200 {
			t.Fatalf("success after failure status=%d err=%v", status, err)
		}
		loadSignal(t, completed, "successful handler")
	}
	if status, retry, err := post(context.Background()); err != nil || status != 429 || retry == "" {
		t.Fatalf("hour cap status=%d retry=%q err=%v", status, retry, err)
	}
	if p.calls.Load() != 6 || p.active.Load() != 0 || p.peak.Load() != 2 {
		t.Fatalf("final calls=%d active=%d peak=%d", p.calls.Load(), p.active.Load(), p.peak.Load())
	}
	t.Logf("loopback burst=%d denied=%d peak_provider=2; cancelled=2 failure=1 recovered=3; provider_calls=6, next=429; elapsed=%s", burst, denied.Load(), time.Since(started).Round(time.Millisecond))
}
