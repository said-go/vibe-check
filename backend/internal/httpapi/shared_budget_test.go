package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/said-go/vibe-check/backend/internal/vision"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestSharedBudgetCachedSuccessIsFree(t *testing.T) {
	b := &fakeSharedBudget{remaining: 1}
	calls := 0
	content, _ := json.Marshal(validPayload)
	client := &http.Client{Transport: budgetTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":` + string(content) + `}}]}`))}, nil
	})}
	p := vision.NewYandex("key", "folder", "model", "https://example.invalid", client)
	h := NewWithLimits(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, p, Limits{SharedBudget: b, PerHour: 10, PerClientMinute: 10, Concurrent: 1})
	photo := patternedImage(t, false)
	for i := 0; i < 2; i++ {
		req := makeImageRequest(t, []string{"true"}, [][]byte{photo}, "image/jpeg")
		req.Header.Set("Idempotency-Key", "shared-budget-cache-key")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
	}
	if calls != 1 || b.calls != 1 {
		t.Fatalf("duplicate paid call %d budget %d", calls, b.calls)
	}
}

type fakeSharedBudget struct {
	calls     int
	remaining int
	broken    bool
}

func (b *fakeSharedBudget) Take(context.Context) (int, error) {
	b.calls++
	if b.broken {
		return 0, errors.New("secret redis password")
	}
	if b.remaining == 0 {
		return 17, nil
	}
	b.remaining--
	return 0, nil
}
func TestSharedBudgetHTTPAttempts(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		remaining                         int
		broken                            bool
		wantStatus, wantCalls, wantBudget int
	}{
		{"outage", 2, true, 503, 0, 1}, {"deny", 0, false, 429, 0, 1}, {"retry blocked", 1, false, 429, 1, 2}, {"two attempts", 2, false, 502, 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &fakeSharedBudget{remaining: tc.remaining, broken: tc.broken}
			calls := 0
			client := &http.Client{Transport: budgetTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[]}`))}, nil
			})}
			p := vision.NewYandex("key", "folder", "model", "https://example.invalid", client)
			h := NewWithLimits(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, p, Limits{SharedBudget: b, PerHour: 10, PerClientMinute: 10, Concurrent: 1})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, makeImageRequest(t, []string{"true"}, [][]byte{patternedImage(t, false)}, "image/jpeg"))
			if w.Code != tc.wantStatus || calls != tc.wantCalls || b.calls != tc.wantBudget {
				t.Fatalf("status=%d calls=%d budget=%d", w.Code, calls, b.calls)
			}
			if tc.wantStatus == 429 && w.Header().Get("Retry-After") != "17" {
				t.Fatal("no shared retry delay")
			}
			if strings.Contains(w.Body.String(), "password") {
				t.Fatal("redis details exposed")
			}
		})
	}
}

func concurrentBudget(t *testing.T, url, key string) {
	t.Helper()
	a, err := NewRedisBudget(url, key, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := NewRedisBudget(url, key, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client := a
			if i%2 == 0 {
				client = b
			}
			retry, e := client.Take(context.Background())
			if e != nil {
				t.Errorf("budget error: %v", e)
				return
			}
			if retry == 0 {
				allowed.Add(1)
			} else if retry < 1 || retry > 3600 {
				t.Errorf("invalid retry %d", retry)
			}
		}(i)
	}
	wg.Wait()
	if allowed.Load() != 7 {
		t.Fatalf("two clients admitted %d, want7", allowed.Load())
	}
}

func TestSharedBudgetConcurrentInstances(t *testing.T) {
	s := miniredis.RunT(t)
	concurrentBudget(t, "redis://"+s.Addr(), "test:shared")
	if s.TTL("test:shared") != time.Hour {
		t.Fatal("no atomic expiry")
	}
	s.FastForward(time.Hour)
	b, _ := NewRedisBudget("redis://"+s.Addr(), "test:shared", 7)
	defer b.Close()
	if n, e := b.Take(context.Background()); e != nil || n != 0 {
		t.Fatalf("expiry did not restore capacity: %d %v", n, e)
	}
	s.Close()
	if _, e := b.Take(context.Background()); e == nil {
		t.Fatal("outage allowed request")
	}
}
func TestSharedBudgetCancellation(t *testing.T) {
	s := miniredis.RunT(t)
	b, _ := NewRedisBudget("redis://"+s.Addr(), "test:cancel", 1)
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := b.Take(ctx); e == nil {
		t.Fatal("cancelled allowed")
	}
	if s.Exists("test:cancel") {
		t.Fatal("cancelled charged")
	}
}
func TestSharedBudgetRealRedis(t *testing.T) {
	url := os.Getenv("REDIS_TEST_URL")
	if url == "" {
		t.Skip("REDIS_TEST_URL unset")
	}
	concurrentBudget(t, url, fmt.Sprintf("vibe-check:test:%d", time.Now().UnixNano()))
}
