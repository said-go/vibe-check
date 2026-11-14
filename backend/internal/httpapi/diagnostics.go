package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

const diagnosticHeader = "X-Vibe-Diagnostics"

// Actual connection and authority must both be local. Forwarding headers are
// rejected even for trusted proxies: this endpoint is not a proxy capability.
func (s *Server) diagnosticsAllowed(r *http.Request) bool {
	if !s.localDiagnostics {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Unmap().IsLoopback() {
		return false
	}
	if !localAuthority(r.Host) {
		return false
	}
	for _, name := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP"} {
		if len(r.Header.Values(name)) > 0 {
			return false
		}
	}
	if origins := r.Header.Values("Origin"); len(origins) > 0 {
		if len(origins) != 1 {
			return false
		}
		u, err := url.Parse(origins[0])
		if err != nil || u.User != nil || u.Scheme != "http" && u.Scheme != "https" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || !strings.EqualFold(u.Host, r.Host) {
			return false
		}
	}
	return true
}

func localAuthority(authority string) bool {
	u, err := url.Parse("http://" + authority)
	if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}

func requestedDiagnostics(r *http.Request) bool {
	values := r.Header.Values(diagnosticHeader)
	return len(values) == 1 && values[0] == "1"
}
