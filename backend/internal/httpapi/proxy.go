package httpapi

import (
	"net/http"
	"net/netip"
	"strings"
)

// Only explicitly trusted peers may supply forwarding metadata. On malformed
// metadata use the socket peer, so arbitrary text cannot create fresh buckets.
func proxyClientKey(r *http.Request, trusted []netip.Prefix) string {
	fallback := clientKey(r)
	peer, err := netip.ParseAddr(fallback)
	if err != nil || len(trusted) == 0 {
		return fallback
	}
	peer = peer.Unmap()
	isTrusted := func(ip netip.Addr) bool {
		for _, p := range trusted {
			if p.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !isTrusted(peer) {
		return fallback
	}
	values := r.Header.Values("X-Forwarded-For")
	// Reject duplicate headers and excessively long chains rather than guessing.
	if len(values) != 1 || len(values[0]) > 2048 {
		return fallback
	}
	parts := strings.Split(values[0], ",")
	if len(parts) > 32 {
		return fallback
	}
	chain := make([]netip.Addr, len(parts))
	for i, s := range parts {
		ip, e := netip.ParseAddr(strings.TrimSpace(s))
		if e != nil || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
			return fallback
		}
		chain[i] = ip.Unmap()
	}
	current := peer
	for i := len(chain) - 1; i >= 0; i-- {
		if !isTrusted(current) {
			break
		}
		current = chain[i]
	}
	return current.String()
}
