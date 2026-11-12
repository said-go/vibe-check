package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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
