package httpapi

import (
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestProxyClientKey(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24"), netip.MustParsePrefix("2001:db8::/64")}
	for _, tt := range []struct{ name, peer, xff, want string }{
		{"untrusted spoof", "192.0.2.9:80", "198.51.100.1", "192.0.2.9"},
		{"trusted", "10.0.0.1:80", "198.51.100.1", "198.51.100.1"},
		{"multi-hop", "10.0.0.1:80", "198.51.100.1, 10.0.0.2", "198.51.100.1"},
		{"left spoof", "10.0.0.1:80", "192.0.2.99, 198.51.100.1", "198.51.100.1"},
		{"ipv6", "[2001:db8::1]:80", "2001:db8:1::4, 2001:db8::2", "2001:db8:1::4"},
		{"mapped", "[::ffff:10.0.0.1]:80", "::ffff:198.51.100.1", "198.51.100.1"},
		{"empty", "10.0.0.1:80", "", "10.0.0.1"},
		{"invalid", "10.0.0.1:80", "bad, 198.51.100.1", "10.0.0.1"},
		{"zone", "10.0.0.1:80", "fe80::1%zone", "10.0.0.1"},
		{"long", "10.0.0.1:80", strings.Repeat("1", 2049), "10.0.0.1"},
		{"too many hops", "10.0.0.1:80", strings.Repeat("10.0.0.2,", 32) + "10.0.0.2", "10.0.0.1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.peer
			r.Header.Set("X-Forwarded-For", tt.xff)
			if got := proxyClientKey(r, trusted); got != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
			if got := proxyClientKey(r, nil); got != clientKey(r) {
				t.Fatal("default changed")
			}
		})
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:80"
	r.Header.Add("X-Forwarded-For", "192.0.2.1")
	r.Header.Add("X-Forwarded-For", "192.0.2.2")
	if proxyClientKey(r, trusted) != "10.0.0.1" {
		t.Fatal("duplicate accepted")
	}
}

func TestTrustedClientsHaveSeparateLimiterBuckets(t *testing.T) {
	l := newRequestLimiter(Limits{PerHour: 10, PerClientMinute: 1, Concurrent: 2, TrustedProxyCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.1/32")}})
	for _, ip := range []string{"192.0.2.1", "192.0.2.2"} {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = "10.0.0.1:80"
		r.Header.Set("X-Forwarded-For", ip)
		a, retry := l.acquire(r)
		if retry != 0 {
			t.Fatal("clients share bucket")
		}
		if a.commit() != 0 {
			t.Fatal("commit rejected")
		}
		a.release()
		if a, retry = l.acquire(r); retry == 0 {
			a.release()
			t.Fatal("same client evaded limit")
		}
	}
}
