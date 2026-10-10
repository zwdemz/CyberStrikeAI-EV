package testproxy

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/mcp"
)

// Store isolates persistence. Implementations must parameterize queries and enforce project foreign keys.
type Store interface {
	SaveTestProxyPool(context.Context, string, []byte) error
	SaveTestProxyPreference(context.Context, string, string) error
	TestProxyPreference(context.Context, string) (string, bool, error)
	TestProxyPools(context.Context) ([][]byte, error)
	BindTestProxy(context.Context, string, string) error
	TestProxyBinding(context.Context, string) (string, error)
	GetConversationProjectID(string) (string, error)
	DeleteTestProxyPool(context.Context, string) error
}

// Pool defines bounded, sticky test routing. Credentials are encrypted in node.Secret.
type Pool struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	MaxConcurrent    int    `json:"max_concurrent"`
	PerNode          int    `json:"per_node"`
	FailureThreshold int    `json:"failure_threshold"`
	CooldownSeconds  int    `json:"cooldown_seconds"`
	Nodes            []Node `json:"nodes"`
}
type nodeState struct {
	active, failures int
	until            time.Time
}

// Service owns process-wide admission and health state; persisted bindings survive restarts.
type Service struct {
	store       Store
	key         []byte
	mu          sync.Mutex
	states      map[string]*nodeState
	active      int
	globalLimit int
	targetLimit int
	waitSeconds int
	targets     map[string]int
	probeURLs   []string
}

var installed atomic.Pointer[Service]

// Install publishes the app service before accepting requests. Nil removes it for isolated tests.
func Install(s *Service) { installed.Store(s) }

// Current returns the installed service, or nil before initialization.
func Current() *Service { return installed.Load() }

// Uninstall removes only this application's service when shutdown completes.
func Uninstall(s *Service) { installed.CompareAndSwap(s, nil) }

// New validates an optional base64 AES-256 key; imports with credentials fail if no key is configured.
func New(store Store, key string) (*Service, error) {
	var decoded []byte
	var err error
	if key != "" {
		decoded, err = base64.StdEncoding.DecodeString(key)
		if err != nil || len(decoded) != 32 {
			return nil, errors.New("TEST_PROXY_KEY must be base64 encoding of 32 bytes")
		}
	}
	return &Service{store: store, key: decoded, states: map[string]*nodeState{}, globalLimit: 16, targetLimit: 2, waitSeconds: 30, targets: map[string]int{}}, nil
}
func (s *Service) crypt(data string, decrypt bool) (string, error) {
	if len(s.key) != 32 {
		return "", errors.New("proxy credentials require TEST_PROXY_KEY")
	}
	block, _ := aes.NewCipher(s.key)
	gcm, _ := cipher.NewGCM(block)
	if decrypt {
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil || len(raw) < gcm.NonceSize() {
			return "", errors.New("invalid proxy credential record")
		}
		plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], []byte("test-proxy-v1"))
		if err != nil {
			return "", errors.New("cannot decrypt proxy credentials")
		}
		return string(plain), nil
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(data), []byte("test-proxy-v1"))), nil
}

// Import validates all rows before one atomic save. Preview returns sanitized nodes without writing.
func (s *Service) Import(ctx context.Context, p Pool, raw string, preview bool) (Pool, error) {
	if !preview {
		pools, err := s.pools(ctx)
		if err != nil {
			return Pool{}, errors.New("proxy storage unavailable")
		}
		if len(pools) >= 50 {
			return Pool{}, errors.New("maximum 50 pools; remove unused pools first")
		}
	}
	if len(strings.TrimSpace(p.Name)) == 0 || len(p.Name) > 100 {
		return Pool{}, errors.New("pool name must contain 1–100 bytes")
	}
	if p.MaxConcurrent == 0 {
		p.MaxConcurrent = 4
	}
	if p.PerNode == 0 {
		p.PerNode = 1
	}
	if p.FailureThreshold == 0 {
		p.FailureThreshold = 3
	}
	if p.CooldownSeconds == 0 {
		p.CooldownSeconds = 60
	}
	if p.MaxConcurrent < 1 || p.MaxConcurrent > 16 || p.PerNode < 1 || p.PerNode > 4 || p.FailureThreshold < 1 || p.FailureThreshold > 10 || p.CooldownSeconds < 10 || p.CooldownSeconds > 3600 {
		return Pool{}, errors.New("pool limits out of range")
	}
	nodes, err := ParseImport(raw)
	if err != nil {
		return Pool{}, err
	}
	for i := range nodes {
		u, _ := url.Parse(nodes[i].Address)
		if u.User != nil {
			if !preview {
				nodes[i].Secret, err = s.crypt(u.User.String(), false)
				if err != nil {
					return Pool{}, err
				}
			}
			u.User = nil
			nodes[i].Address = u.String()
		}
	}
	p.Nodes = nodes
	if preview {
		return p, nil
	}
	// Each import creates an immutable pool, preventing in-flight credentials or routing from changing.
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return Pool{}, err
	}
	p.ID = fmt.Sprintf("%x", id)
	data, err := json.Marshal(p)
	if err != nil {
		return Pool{}, err
	}
	if err = s.store.SaveTestProxyPool(ctx, p.ID, data); err != nil {
		return Pool{}, errors.New("could not save proxy pool")
	}
	for i := range p.Nodes {
		p.Nodes[i].Secret = ""
	}
	return p, nil
}

// Delete refuses active or bound pools. It removes health state only after persistence succeeds.
func (s *Service) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, state := range s.states {
		if strings.HasPrefix(key, id+":") && state.active > 0 {
			return errors.New("proxy pool is busy")
		}
	}
	if err := s.store.DeleteTestProxyPool(ctx, id); err != nil {
		return errors.New("could not delete pool; clear account preferences and legacy bindings first")
	}
	for key := range s.states {
		if strings.HasPrefix(key, id+":") {
			delete(s.states, key)
		}
	}
	return nil
}
func (s *Service) pools(ctx context.Context) ([]Pool, error) {
	docs, err := s.store.TestProxyPools(ctx)
	if err != nil {
		return nil, err
	}
	out := []Pool{}
	for _, doc := range docs {
		var p Pool
		if err := json.Unmarshal(doc, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// List returns public metadata only; it never reveals stored credential ciphertext.
func (s *Service) List(ctx context.Context) ([]Pool, error) {
	pools, err := s.pools(ctx)
	for i := range pools {
		for j := range pools[i].Nodes {
			pools[i].Nodes[j].Secret = ""
		}
	}
	return pools, err
}

// Bind selects a validated pool or disables routing with an empty ID; caller must authorize project access.
func (s *Service) Bind(ctx context.Context, projectID, poolID string) error {
	if projectID == "" {
		return errors.New("project is required")
	}
	if err := s.validatePool(ctx, poolID); err != nil {
		return err
	}
	return s.store.BindTestProxy(ctx, projectID, poolID)
}

func (s *Service) validatePool(ctx context.Context, poolID string) error {
	if poolID != "" {
		pools, err := s.pools(ctx)
		if err != nil {
			return err
		}
		found := false
		for _, p := range pools {
			if p.ID == poolID {
				for _, n := range p.Nodes {
					found = found || n.Enabled
				}
			}
		}
		if !found {
			return errors.New("pool has no enabled nodes")
		}
	}
	return nil
}

// Preference reads only the trusted authenticated account; missing identity fails closed.
func (s *Service) Preference(ctx context.Context) (string, bool, error) {
	p, ok := authctx.PrincipalFromContext(ctx)
	if !ok {
		return "", false, errors.New("authenticated account required")
	}
	return s.store.TestProxyPreference(ctx, p.UserID)
}

// SetPreference remembers an explicit choice across sessions; callers require configuration permission.
// Invalid or disabled pools and persistence failures leave the previous selection unchanged.
func (s *Service) SetPreference(ctx context.Context, poolID string) error {
	p, ok := authctx.PrincipalFromContext(ctx)
	if !ok {
		return errors.New("authenticated account required")
	}
	if err := s.validatePool(ctx, poolID); err != nil {
		return err
	}
	return s.store.SaveTestProxyPreference(ctx, p.UserID, poolID)
}

// Binding resolves the authenticated account preference, then legacy project constraints.
// Database failures are never treated as direct-connect permission.
func (s *Service) Binding(ctx context.Context) (string, error) {
	if _, ok := authctx.PrincipalFromContext(ctx); ok {
		id, exists, err := s.Preference(ctx)
		if err != nil || exists {
			return id, err
		}
	}
	id := mcp.MCPProjectIDFromContext(ctx)
	if id == "" && mcp.MCPConversationIDFromContext(ctx) != "" {
		var err error
		id, err = s.store.GetConversationProjectID(mcp.MCPConversationIDFromContext(ctx))
		if err != nil {
			return "", err
		}
	}
	if id == "" {
		return "", nil
	}
	return s.store.TestProxyBinding(ctx, id)
}

// CheckTool blocks unsupported network and execution capabilities for proxy-bound projects.
// This is an application guard, not OS-level isolation against privileged users or arbitrary programs.
func CheckTool(ctx context.Context, name string) error {
	s := Current()
	if s == nil {
		return nil
	}
	id, err := s.Binding(ctx)
	if err != nil {
		return errors.New("TEST_PROXY_POLICY_UNAVAILABLE")
	}
	if id == "" {
		return nil
	}
	switch name {
	case "http-framework-test", "read_file", "ls", "list_dir", "glob", "grep", "write_todos", "task", "transfer_to_agent", "exit", "tool_search", "TaskCreate", "TaskGet", "TaskUpdate", "TaskList",
		"list_knowledge_risk_types", "search_knowledge_base", "record_vulnerability", "list_vulnerabilities", "get_vulnerability", "create_asset", "get_asset", "query_assets", "update_asset", "delete_asset", "complete_asset_scan",
		"upsert_project_fact", "get_project_fact", "list_project_facts", "search_project_facts", "deprecate_project_fact", "restore_project_fact", "get_tool_execution", "wait_tool_execution", "cancel_tool_execution", "query_execution_result":
		return nil
	default:
		return errors.New("TEST_PROXY_UNSUPPORTED_TOOL: this project requires a test proxy; use http-framework-test; direct fallback is blocked")
	}
}

// Lease reserves a sticky node. Finish must be called once; only proxy/connection failures trip its circuit.
type Lease struct {
	ID, URL string
	finish  func(bool)
	once    sync.Once
}

// Finish releases admission after execution; repeated calls are harmless.
func (l *Lease) Finish(proxyFailure bool) {
	if l != nil {
		l.once.Do(func() { l.finish(proxyFailure) })
	}
}

// Acquire waits cancellably for global/pool/node capacity. The same session always chooses the same node.
// Open circuits return immediately; requests are not replayed and proxies are not rotated to bypass target limits.
func (s *Service) Acquire(ctx context.Context, targetURLs ...string) (*Lease, error) {
	id, err := s.Binding(ctx)
	if err != nil {
		return nil, errors.New("TEST_PROXY_POLICY_UNAVAILABLE")
	}
	if id == "" {
		return nil, nil
	}
	pools, err := s.pools(ctx)
	if err != nil {
		return nil, errors.New("TEST_PROXY_STORE_UNAVAILABLE")
	}
	var selected *Pool
	for i := range pools {
		if pools[i].ID == id {
			selected = &pools[i]
		}
	}
	if selected == nil {
		return nil, errors.New("TEST_PROXY_POOL_MISSING")
	}
	p := *selected
	nodes := []Node{}
	for _, n := range p.Nodes {
		if n.Enabled {
			nodes = append(nodes, n)
		}
	}
	if len(nodes) == 0 {
		return nil, errors.New("TEST_PROXY_POOL_EMPTY")
	}
	key := mcp.MCPConversationIDFromContext(ctx)
	if key == "" {
		key = mcp.MCPProjectIDFromContext(ctx)
	}
	h := sha256.Sum256([]byte(id + ":" + key))
	n := nodes[int(binary.BigEndian.Uint32(h[:4]))%len(nodes)]
	stateKey := id + ":" + n.ID
	u, err := url.Parse(n.Address)
	if err != nil {
		return nil, errors.New("TEST_PROXY_INVALID")
	}
	if n.Secret != "" {
		secret, e := s.crypt(n.Secret, true)
		if e != nil {
			return nil, e
		}
		parsed, e := url.Parse(u.Scheme + "://" + secret + "@" + u.Host)
		if e != nil {
			return nil, errors.New("TEST_PROXY_CREDENTIALS_INVALID")
		}
		u = parsed
	}
	targetKey := ""
	if len(targetURLs) > 0 {
		parsed, e := url.Parse(targetURLs[0])
		if e != nil || parsed.Hostname() == "" {
			return nil, errors.New("invalid test target URL")
		}
		targetKey = strings.ToLower(parsed.Hostname())
	}
	timer := time.NewTicker(50 * time.Millisecond)
	defer timer.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.mu.Lock()
		st := s.states[stateKey]
		if st == nil {
			st = &nodeState{}
			s.states[stateKey] = st
		}
		if time.Now().Before(st.until) {
			s.mu.Unlock()
			return nil, errors.New("TEST_PROXY_CIRCUIT_OPEN: sticky proxy cooling down; no direct fallback")
		}
		poolActive := 0
		for k, v := range s.states {
			if strings.HasPrefix(k, id+":") {
				poolActive += v.active
			}
		}
		if s.active < s.globalLimit && poolActive < p.MaxConcurrent && st.active < p.PerNode && (st.failures < p.FailureThreshold || st.active == 0) && (targetKey == "" || s.targets[targetKey] < s.targetLimit) {
			s.active++
			if targetKey != "" {
				s.targets[targetKey]++
			}
			st.active++
			s.mu.Unlock()
			return &Lease{ID: n.ID, URL: u.String(), finish: func(failed bool) {
				s.mu.Lock()
				defer s.mu.Unlock()
				s.active--
				if targetKey != "" {
					s.targets[targetKey]--
					if s.targets[targetKey] == 0 {
						delete(s.targets, targetKey)
					}
				}
				st.active--
				if failed {
					st.failures++
					if st.failures >= p.FailureThreshold {
						st.until = time.Now().Add(time.Duration(p.CooldownSeconds) * time.Second)
					}
				} else {
					st.failures = 0
					st.until = time.Time{}
				}
			}}, nil
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// Status returns bounded node health counters for administrators, without credentials or target URLs.
func (s *Service) Status() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	nodes := map[string]interface{}{}
	for k, v := range s.states {
		nodes[k] = map[string]interface{}{"active": v.active, "failures": v.failures, "circuit_open": time.Now().Before(v.until), "cooldown_until": v.until}
	}
	return map[string]interface{}{"active": s.active, "global_limit": s.globalLimit, "nodes": nodes}
}

// Configure validates service-wide limits before startup; zero selects conservative defaults.
func (s *Service) Configure(global, target, wait int) error {
	if global == 0 {
		global = 16
	}
	if target == 0 {
		target = 2
	}
	if wait == 0 {
		wait = 30
	}
	if global < 1 || global > 128 || target < 1 || target > 16 || wait < 1 || wait > 120 {
		return errors.New("test_proxy limits out of range")
	}
	s.globalLimit = global
	s.targetLimit = target
	s.waitSeconds = wait
	return nil
}

// QueueTimeout returns the configured admission deadline, independently of tool execution timeout.
func (s *Service) QueueTimeout() time.Duration { return time.Duration(s.waitSeconds) * time.Second }
