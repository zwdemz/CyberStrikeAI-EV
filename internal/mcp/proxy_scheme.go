package mcp

import (
	"net"
	"net/http"
	"strings"

	"cyberstrike-ai/internal/config"
)

// ConfigureTrustedProxies snapshots validated proxy IPs/CIDRs for SSE endpoint
// construction. An empty list trusts no forwarded headers; invalid lists leave
// the previous configuration unchanged. Call during startup or reconfiguration.
func (s *Server) ConfigureTrustedProxies(proxies []string) error {
	if err := (config.ServerConfig{TrustedProxies: proxies}).ValidateHTTPSecurity(); err != nil {
		return err
	}
	s.mu.Lock()
	s.trustedProxies = append([]string(nil), proxies...)
	s.mu.Unlock()
	return nil
}

func (s *Server) endpointScheme(request *http.Request) string {
	if request.TLS != nil {
		return "https"
	}
	if request.URL.Scheme == "http" || request.URL.Scheme == "https" {
		return request.URL.Scheme
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return "http"
	}
	peer := net.ParseIP(host)
	if peer == nil {
		return "http"
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, proxy := range s.trustedProxies {
		trusted := peer.Equal(net.ParseIP(proxy))
		if _, network, err := net.ParseCIDR(proxy); err == nil {
			trusted = network.Contains(peer)
		}
		if !trusted {
			continue
		}
		// Require one canonical value. The trusted edge must overwrite, not append,
		// this header; chains and arbitrary URI schemes are deliberately ignored.
		values := request.Header.Values("X-Forwarded-Proto")
		if len(values) == 1 {
			proto := strings.TrimSpace(values[0])
			if proto == "http" || proto == "https" {
				return proto
			}
		}
		break
	}
	return "http"
}
