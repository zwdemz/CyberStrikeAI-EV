// Package dnslog implements the dig.pm DNS-only session protocol.
package dnslog

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const maxResponseBytes = 1 << 20

// Options configures the trusted provider endpoint, TLS transport and local
// session lifetime/capacity. Zero values select production defaults.
type Options struct {
	BaseURL     string
	Transport   *http.Transport
	SessionTTL  time.Duration
	MaxSessions int
}

// Client holds private provider tokens in memory, scoped to a trusted caller.
// Use New to initialize it. It is safe for concurrent tool calls.
type Client struct {
	base     string
	http     *http.Client
	dialer   websocket.Dialer
	ttl      time.Duration
	capacity int
	mu       sync.Mutex
	sessions map[string]session
	pending  int
}

type session struct {
	FullDomain string `json:"fullDomain"`
	MainDomain string `json:"mainDomain"`
	SubDomain  string `json:"subDomain"`
	Token      string `json:"token"`
	owner      string
	expires    time.Time
}

// New creates a client using HTTPS/WSS and certificate verification. Invalid
// endpoints, nonpositive limits and insecure TLS settings return an error.
// An optional transport supplies proxy settings or a private certificate pool.
func New(options Options) (*Client, error) {
	if options.BaseURL == "" {
		options.BaseURL = "https://dig.pm"
	}
	endpoint, err := url.Parse(options.BaseURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.ForceQuery || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
		return nil, errors.New("dig.pm endpoint must be an HTTPS origin without credentials, query or path")
	}
	if options.SessionTTL == 0 {
		options.SessionTTL = time.Hour
	}
	if options.MaxSessions == 0 {
		options.MaxSessions = 256
	}
	if options.SessionTTL < 0 || options.MaxSessions < 1 {
		return nil, errors.New("invalid dig.pm session limits")
	}
	transport := options.Transport
	if transport == nil {
		transport = http.DefaultTransport.(*http.Transport)
	}
	transport = transport.Clone()
	if transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify {
		return nil, errors.New("dig.pm requires TLS certificate verification")
	}
	return &Client{
		base: strings.TrimSuffix(endpoint.String(), "/"), ttl: options.SessionTTL,
		capacity: options.MaxSessions, sessions: make(map[string]session),
		http: &http.Client{Transport: transport, Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		dialer: websocket.Dialer{Proxy: transport.Proxy, TLSClientConfig: transport.TLSClientConfig, HandshakeTimeout: 10 * time.Second},
	}, nil
}

// Execute validates tool arguments and executes list_domains, get_domain or
// get_records. owner must come from trusted authentication/conversation context,
// never tool arguments. Returned values and errors never include provider tokens.
func (c *Client) Execute(ctx context.Context, owner string, args map[string]interface{}) (interface{}, error) {
	request, err := parseRequest(args)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(owner) == "" {
		return nil, errors.New("dig.pm requires an authenticated caller or conversation context")
	}
	switch request.operation {
	case "list_domains":
		domains, err := c.domains(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"status": "success", "domains": domains}, nil
	case "get_domain":
		return c.allocate(ctx, owner, request.mainDomain)
	default:
		return c.records(ctx, owner, request.sessionID, request.wait)
	}
}

// requestJSON bounds provider responses and suppresses raw transport/body errors:
// redirects and provider-controlled error text must never expose credentials.
func (c *Client) requestJSON(ctx context.Context, method, path string, form url.Values, destination interface{}) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New("could not prepare dig.pm request")
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return errors.New("dig.pm request failed or was cancelled")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("dig.pm returned an unsuccessful HTTP status")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes || json.Unmarshal(body, destination) != nil {
		return errors.New("dig.pm returned an invalid or oversized JSON response")
	}
	return nil
}

// domains fetches the current allowlist and normalizes/deduplicates its labels.
// Empty, malformed and excessive lists fail closed before session allocation.
func (c *Client) domains(ctx context.Context) ([]string, error) {
	var raw []string
	if err := c.requestJSON(ctx, http.MethodGet, "/get_domain", nil, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > 128 {
		return nil, errors.New("dig.pm returned an empty or oversized domain list")
	}
	domains := make([]string, 0, len(raw))
	seen := make(map[string]bool)
	for _, value := range raw {
		domain, err := normalizeDomain(value)
		if err != nil {
			return nil, errors.New("dig.pm returned an invalid main domain")
		}
		if !seen[domain] {
			seen[domain] = true
			domains = append(domains, domain)
		}
	}
	return domains, nil
}

// reserve removes expired tokens and reserves capacity before any allocation
// request, so concurrent callers cannot exceed the in-memory session bound.
func (c *Client) reserve() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, entry := range c.sessions {
		if !time.Now().Before(entry.expires) {
			delete(c.sessions, id)
		}
	}
	if len(c.sessions)+c.pending >= c.capacity {
		return false
	}
	c.pending++
	return true
}

// allocate validates a selected live main domain and the returned field
// relationships before storing its token under an opaque, caller-scoped ID.
// Capacity, protocol and network failures release the pending reservation.
func (c *Client) allocate(ctx context.Context, owner, requested string) (interface{}, error) {
	if !c.reserve() {
		return nil, errors.New("dig.pm local session capacity reached; retry after existing sessions expire")
	}
	defer func() { c.mu.Lock(); c.pending--; c.mu.Unlock() }()
	domains, err := c.domains(ctx)
	if err != nil {
		return nil, err
	}
	selected := domains[0]
	if requested != "" {
		found := false
		for _, domain := range domains {
			if requested == domain {
				selected, found = domain, true
				break
			}
		}
		if !found {
			return nil, errors.New("main_domain is not in the provider's current domain list")
		}
	}
	var entry session
	if err := c.requestJSON(ctx, http.MethodPost, "/get_sub_domain", url.Values{"mainDomain": {selected + "."}}, &entry); err != nil {
		return nil, err
	}
	mainDomain, mainErr := normalizeDomain(entry.MainDomain)
	fullDomain, fullErr := normalizeDomain(entry.FullDomain)
	subDomain, subErr := normalizeDomain(entry.SubDomain)
	if mainErr != nil || fullErr != nil || subErr != nil || strings.Contains(subDomain, ".") || mainDomain != selected || fullDomain != subDomain+"."+mainDomain || len(entry.Token) < 1 || len(entry.Token) > 512 || strings.ContainsAny(entry.Token, "\r\n\x00") {
		return nil, errors.New("dig.pm returned inconsistent session fields")
	}
	entry.MainDomain, entry.FullDomain, entry.SubDomain = mainDomain+".", fullDomain+".", subDomain
	entry.owner, entry.expires = owner, time.Now().Add(c.ttl)
	id := uuid.NewString()
	c.mu.Lock()
	c.sessions[id] = entry
	c.mu.Unlock()
	return map[string]interface{}{
		"status": "success", "session_id": id, "domain": fullDomain,
		"main_domain": mainDomain, "expires_at": entry.expires.UTC().Format(time.RFC3339),
		"note": "Local session expiry; the provider's expiry is unknown. DNS only; records are not proof of a vulnerability.",
	}, nil
}
