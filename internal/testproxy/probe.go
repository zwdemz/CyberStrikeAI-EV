package testproxy

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"time"
)

// ConfigureProbeURLs validates the explicit startup allowlist. Empty disables probes.
// URLs are exact matches; no suffix, redirect or arbitrary URL fallback is allowed.
func (s *Service) ConfigureProbeURLs(addresses []string) error {
	if len(addresses) > 16 {
		return errors.New("at most 16 probe URLs are allowed")
	}
	for _, address := range addresses {
		u, err := url.Parse(address)
		if err != nil || len(address) > 2048 || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("probe_urls must contain controlled HTTP(S) URLs without credentials or fragments")
		}
	}
	s.probeURLs = append([]string(nil), addresses...)
	return nil
}

// Probe makes one bounded HEAD request through a selected proxy to a URL in the startup
// allowlist. It never follows redirects, returns bodies or falls back to direct access.
// A target HTTP error proves reachability and does not count as a broken proxy.
func (s *Service) Probe(ctx context.Context, poolID, nodeID, target string) (map[string]interface{}, error) {
	controlledURL := ""
	for _, configured := range s.probeURLs {
		if configured == target {
			controlledURL = configured
			break
		}
	}
	if controlledURL == "" {
		return nil, errors.New("probe URL not allowed; configure test_proxy.probe_urls and restart first")
	}
	pools, err := s.pools(ctx)
	if err != nil {
		return nil, errors.New("proxy storage unavailable")
	}
	var node *Node
	var pool Pool
	for _, p := range pools {
		if p.ID == poolID {
			pool = p
			for i := range p.Nodes {
				if p.Nodes[i].ID == nodeID && p.Nodes[i].Enabled {
					n := p.Nodes[i]
					node = &n
				}
			}
		}
	}
	if node == nil {
		return nil, errors.New("enabled proxy node not found")
	}
	proxyURL, _ := url.Parse(node.Address)
	if node.Secret != "" {
		secret, e := s.crypt(node.Secret, true)
		if e != nil {
			return nil, e
		}
		proxyURL, err = url.Parse(proxyURL.Scheme + "://" + secret + "@" + proxyURL.Host)
		if err != nil {
			return nil, errors.New("invalid proxy credentials")
		}
	}
	stateKey := poolID + ":" + nodeID
	s.mu.Lock()
	state := s.states[stateKey]
	if state == nil {
		state = &nodeState{}
		s.states[stateKey] = state
	}
	poolActive := 0
	for key, v := range s.states {
		if len(key) > len(poolID) && key[:len(poolID)+1] == poolID+":" {
			poolActive += v.active
		}
	}
	if s.active >= s.globalLimit || poolActive >= pool.MaxConcurrent || state.active >= pool.PerNode {
		s.mu.Unlock()
		return nil, errors.New("proxy busy; retry after current request")
	}
	s.active++
	state.active++
	s.mu.Unlock()
	failed := true
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.active--
		state.active--
		if failed {
			state.failures++
			if state.failures >= pool.FailureThreshold {
				state.until = time.Now().Add(time.Duration(pool.CooldownSeconds) * time.Second)
			}
		} else {
			state.failures = 0
			state.until = time.Time{}
		}
	}()
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 32 * 1024}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, controlledURL, nil)
	if err != nil {
		return nil, errors.New("invalid probe URL")
	}
	start := time.Now()
	response, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, errors.New("proxy probe canceled")
		}
		if os.IsTimeout(err) {
			return nil, errors.New("proxy probe timed out; no direct fallback")
		}
		return nil, errors.New("proxy connection, TLS or tunnel authentication failed; no direct fallback")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusProxyAuthRequired {
		return nil, errors.New("proxy authentication failed (HTTP 407)")
	}
	failed = false
	return map[string]interface{}{"node_id": nodeID, "http_status": response.StatusCode, "latency_ms": time.Since(start).Milliseconds(), "reachable": true, "usable": response.StatusCode >= 200 && response.StatusCode < 300}, nil
}
