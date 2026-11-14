package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/vision"
)

type budgetTransport func(*http.Request) (*http.Response, error)

func (f budgetTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProviderRetrySpendsIndependentBudget(t *testing.T) {
	for _, budget := range []int{1, 2} {
		t.Run(string(rune('0'+budget)), func(t *testing.T) {
			calls := 0
			var logs strings.Builder
			client := &http.Client{Transport: budgetTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`))}, nil
			})}
			provider := vision.NewYandex("synthetic-key", "synthetic-folder", "model", "https://example.invalid", client)
			handler := NewWithLimits(slog.New(slog.NewTextHandler(&logs, nil)), 10<<20, provider, Limits{PerHour: budget, PerClientMinute: 10, Concurrent: 1})
			req := makeImageRequest(t, []string{"true"}, [][]byte{patternedImage(t, false)}, "image/jpeg")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			want := http.StatusBadGateway
			if budget == 1 {
				want = http.StatusTooManyRequests
			}
			if calls != budget || response.Code != want {
				t.Fatalf("calls=%d status=%d body=%s", calls, response.Code, response.Body.String())
			}
			if strings.Count(logs.String(), "msg=provider_attempt") != budget || !strings.Contains(logs.String(), "total_tokens=10") {
				t.Fatalf("missing safe attempt metrics: %s", logs.String())
			}
			if strings.Contains(logs.String(), "synthetic-key") {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestAdditionalAttemptRespectsPendingReservations(t *testing.T) {
	limiter := newRequestLimiter(Limits{PerHour: 2, PerClientMinute: 10, Concurrent: 2})
	r := httptest.NewRequest(http.MethodPost, "/v1/analyses", nil)
	first, _ := limiter.acquire(r)
	defer first.release()
	if first.commit() != 0 {
		t.Fatal("first attempt denied")
	}
	second, _ := limiter.acquire(r)
	if second == nil {
		t.Fatal("reservation denied")
	}
	defer second.release()
	if first.chargeAdditional() == 0 {
		t.Fatal("retry consumed reserved capacity")
	}
	if second.commit() != 0 {
		t.Fatal("reserved attempt denied")
	}
}

func TestRequestLimiterPerClientAndGlobalBudget(t *testing.T) {
	limiter := newRequestLimiter(Limits{PerHour: 2, PerClientMinute: 1, Concurrent: 2})
	first := httptest.NewRequest(http.MethodPost, "/v1/analyses", nil)
	first.RemoteAddr = "192.0.2.1:1111"
	reservation, retry := limiter.acquire(first)
	if reservation == nil || retry != 0 {
		t.Fatalf("first request: admitted=%v retry=%d", reservation != nil, retry)
	}
	if retry = reservation.commit(); retry != 0 {
		t.Fatalf("first commit retry=%d", retry)
	}
	reservation.release()
	first.Header.Set("X-Forwarded-For", "198.51.100.8")
	if reservation, retry := limiter.acquire(first); reservation != nil || retry < 1 {
		t.Fatalf("spoofed forwarded address bypassed per-client limit: admitted=%v retry=%d", reservation != nil, retry)
	}
	second := httptest.NewRequest(http.MethodPost, "/v1/analyses", nil)
	second.RemoteAddr = "192.0.2.2:2222"
	reservation, retry = limiter.acquire(second)
	if reservation == nil || retry != 0 {
		t.Fatalf("second client: admitted=%v retry=%d", reservation != nil, retry)
	}
	if retry = reservation.commit(); retry != 0 {
		t.Fatalf("second commit retry=%d", retry)
	}
	reservation.release()
	third := httptest.NewRequest(http.MethodPost, "/v1/analyses", nil)
	third.RemoteAddr = "192.0.2.3:3333"
	if reservation, retry := limiter.acquire(third); reservation != nil || retry < 1 {
		t.Fatalf("global budget not enforced: admitted=%v retry=%d", reservation != nil, retry)
	}
}

func TestRequestLimiterConcurrentBudget(t *testing.T) {
	limiter := newRequestLimiter(Limits{PerHour: 10, PerClientMinute: 10, Concurrent: 1})
	r := httptest.NewRequest(http.MethodPost, "/v1/analyses", nil)
	reservation, _ := limiter.acquire(r)
	if reservation == nil {
		t.Fatal("first request denied")
	}
	if other, retry := limiter.acquire(r); other != nil || retry != 1 {
		t.Fatalf("concurrent request should be denied: admitted=%v retry=%d", other != nil, retry)
	}
	reservation.release()
	if other, retry := limiter.acquire(r); other == nil || retry != 0 {
		t.Fatalf("capacity was not released: admitted=%v retry=%d", other != nil, retry)
	} else {
		other.release()
	}
}

func TestInvalidReservationDoesNotSpendBudget(t *testing.T) {
	limiter := newRequestLimiter(Limits{PerHour: 1, PerClientMinute: 1, Concurrent: 1})
	r := httptest.NewRequest(http.MethodPost, "/v1/analyses", nil)
	first, _ := limiter.acquire(r)
	first.release()
	second, retry := limiter.acquire(r)
	if second == nil || retry != 0 {
		t.Fatalf("invalid request spent budget: admitted=%v retry=%d", second != nil, retry)
	}
	if retry := second.commit(); retry != 0 {
		t.Fatalf("valid request denied: retry=%d", retry)
	}
	second.release()
}
