// Package rolepolicy enforces bounded tool use for explicitly restricted roles.
package rolepolicy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"
	"gopkg.in/yaml.v3"
)

type contextKey struct{}
type state struct {
	policy        config.RoleToolPolicy
	tools         []string
	allowed       map[string]bool
	documentRoots []string
	gate          chan struct{}
	mu            sync.Mutex
	host          string
	calls         int
	next          time.Time
}

var safeTools = map[string]bool{
	"record_vulnerability": true, "list_vulnerabilities": true, "get_vulnerability": true,
	"upsert_project_fact": true, "get_project_fact": true, "list_project_facts": true, "search_project_facts": true,
	"list_knowledge_risk_types": true, "search_knowledge_base": true,
	"get_tool_execution": true, "wait_tool_execution": true, "cancel_tool_execution": true,
	"http-framework-test": true, "nmap": true, "jwt-analyzer": true, "api-schema-analyzer": true, "waybackurls": true, "skill": true,
}

// With binds a validated policy and explicit tool list to a run. Nested callers
// inherit the original bounds, preventing a child agent from widening its scope.
// Document roots must come from trusted session metadata, never tool arguments.
// Empty profiles leave the context unchanged; invalid tool lists fail closed.
func With(ctx context.Context, policy config.RoleToolPolicy, tools []string, documentRoots ...string) (context.Context, error) {
	if current(ctx) != nil {
		return ctx, nil
	}
	effective, err := policy.Effective()
	if err != nil {
		return ctx, err
	}
	if effective.Profile == "" {
		return ctx, nil
	}
	if len(tools) == 0 {
		return ctx, fmt.Errorf("restricted role must define tools")
	}
	allowed := make(map[string]bool, len(tools))
	for _, name := range tools {
		if !safeTools[name] {
			return ctx, fmt.Errorf("tool %q is outside the SRC policy", name)
		}
		allowed[name] = true
	}
	roots := make([]string, 0, len(documentRoots))
	for _, root := range documentRoots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return ctx, fmt.Errorf("cannot resolve trusted document directory")
		}
		roots = append(roots, absolute)
	}
	return context.WithValue(ctx, contextKey{}, &state{policy: effective, tools: append([]string(nil), tools...), allowed: allowed, documentRoots: roots, gate: make(chan struct{}, 1)}), nil
}

func current(ctx context.Context) *state {
	if ctx == nil {
		return nil
	}
	value, _ := ctx.Value(contextKey{}).(*state)
	return value
}

// Active reports whether server-enforced role bounds exist on this run context.
func Active(ctx context.Context) bool { return current(ctx) != nil }

// CheckTool rejects tools outside the role, including shell and installer aliases.
// Planning/discovery helpers remain usable but cannot enlarge the shared policy.
func CheckTool(ctx context.Context, name string) error {
	value := current(ctx)
	if value == nil {
		return nil
	}
	if value.allowed[name] {
		return nil
	}
	switch name {
	case "task", "transfer_to_agent", "exit", "write_todos", "TaskCreate", "TaskGet", "TaskUpdate", "TaskList", "tool_search":
		return nil
	}
	return fmt.Errorf("role policy denied tool %q; do not replace it with a shell, script or another agent", name)
}

// Filter returns the requested tool list intersected with the parent role. An
// empty child list inherits the parent list; an empty intersection never means all.
func Filter(ctx context.Context, requested []string) []string {
	value := current(ctx)
	if value == nil {
		return requested
	}
	if len(requested) == 0 {
		return append([]string(nil), value.tools...)
	}
	filtered := make([]string, 0, len(requested))
	for _, name := range requested {
		if value.allowed[name] {
			filtered = append(filtered, name)
		}
	}
	if len(filtered) == 0 {
		return []string{"__role_policy_no_tools__"}
	}
	return filtered
}

// Prepare validates and copies MCP arguments before execution. Network calls share
// a per-run host, quota and serial gate across child agents. The caller must defer
// release after execution; rejected calls never acquire a network slot.
func Prepare(ctx context.Context, name string, arguments map[string]interface{}) (map[string]interface{}, func(), error) {
	noop := func() {}
	if err := CheckTool(ctx, name); err != nil {
		return nil, noop, err
	}
	value := current(ctx)
	if value == nil {
		return arguments, noop, nil
	}
	args := make(map[string]interface{}, len(arguments))
	for key, argument := range arguments {
		args[key] = argument
	}
	var host string
	var err error
	switch name {
	case "http-framework-test":
		host, err = prepareHTTP(args)
	case "nmap":
		host, err = prepareNmap(args, value.policy.MaxPorts)
	case "waybackurls":
		if err = onlyKeys(args, "domain", "no_subs"); err == nil {
			host, err = singleHost(textArg(args, "domain"), false)
			args["domain"], args["no_subs"] = host, true
		}
	case "jwt-analyzer":
		err = onlyKeys(args, "jwt_token")
		token := textArg(args, "jwt_token")
		if token == "" || strings.HasPrefix(token, "-") || len(token) > 32768 || strings.ContainsAny(token, " \t\r\n") {
			err = fmt.Errorf("provide a single bounded test JWT for offline inspection")
		}
	case "api-schema-analyzer":
		cleanup, err := prepareSchema(args, value.documentRoots)
		return args, cleanup, err
	}
	if err != nil {
		return nil, noop, err
	}
	if host == "" {
		return args, noop, nil
	}
	select {
	case value.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, noop, ctx.Err()
	}
	var once sync.Once
	release := func() { once.Do(func() { <-value.gate }) }
	value.mu.Lock()
	if value.host != "" && value.host != host {
		value.mu.Unlock()
		release()
		return nil, noop, fmt.Errorf("SRC run is limited to one target host")
	}
	if value.calls >= value.policy.MaxNetworkCalls {
		value.mu.Unlock()
		release()
		return nil, noop, fmt.Errorf("SRC network call budget exhausted")
	}
	wait := time.Until(value.next)
	value.mu.Unlock()
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			release()
			return nil, noop, ctx.Err()
		}
	}
	value.mu.Lock()
	value.host = host
	value.calls++
	value.next = time.Now().Add(time.Duration(value.policy.MinIntervalMS) * time.Millisecond)
	value.mu.Unlock()
	return args, release, nil
}

func textArg(args map[string]interface{}, key string) string {
	text, _ := args[key].(string)
	return strings.TrimSpace(text)
}

func onlyKeys(args map[string]interface{}, keys ...string) error {
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		allowed[key] = true
	}
	for key := range args {
		if !allowed[key] {
			return fmt.Errorf("argument %q is not available in the SRC policy", key)
		}
	}
	return nil
}

// singleHost rejects target lists, CIDRs, URL credentials and ambiguous host syntax.
// The selected target must still be explicitly authorized by the user; first-host
// pinning limits later expansion and does not infer authorization from a URL.
func singleHost(raw string, requireURL bool) (string, error) {
	if raw == "" || strings.ContainsAny(raw, " \t\r\n,*\\") || strings.HasPrefix(raw, "-") {
		return "", fmt.Errorf("provide one explicit target")
	}
	host := raw
	if strings.Contains(raw, "://") || requireURL {
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.Hostname() == "" || parsed.Fragment != "" {
			return "", fmt.Errorf("provide one HTTP(S) target without URL credentials or fragments")
		}
		host = parsed.Hostname()
	} else if strings.ContainsAny(raw, "/?#@") {
		return "", fmt.Errorf("target lists and CIDRs are not allowed")
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if net.ParseIP(host) == nil {
		for _, label := range strings.Split(host, ".") {
			if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return "", fmt.Errorf("invalid target hostname")
			}
			for _, char := range label {
				if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
					return "", fmt.Errorf("invalid target hostname")
				}
			}
		}
	}
	return host, nil
}

func prepareHTTP(args map[string]interface{}) (string, error) {
	if err := onlyKeys(args, "url", "method", "headers", "cookies", "user_agent", "timeout", "repeat", "include_headers", "response_filter", "response_max_bytes", "response_max_lines", "show_command", "show_summary"); err != nil {
		return "", err
	}
	method := strings.ToUpper(textArg(args, "method"))
	if method == "" {
		method = "GET"
	}
	if method != "GET" && method != "HEAD" && method != "OPTIONS" {
		return "", fmt.Errorf("SRC HTTP verification only permits GET, HEAD and OPTIONS")
	}
	if repeat, exists := args["repeat"]; exists && fmt.Sprint(repeat) != "1" {
		return "", fmt.Errorf("SRC HTTP verification permits one request per call")
	}
	args["method"], args["repeat"], args["timeout"] = method, 1, "10"
	args["follow_redirects"], args["show_command"], args["debug"] = false, false, false
	args["response_max_bytes"], args["response_max_lines"] = 8192, 100
	args["url"] = textArg(args, "url")
	return singleHost(textArg(args, "url"), true)
}

func prepareNmap(args map[string]interface{}, maxPorts int) (string, error) {
	if err := onlyKeys(args, "target", "ports"); err != nil {
		return "", err
	}
	host, err := singleHost(textArg(args, "target"), false)
	if err != nil {
		return "", err
	}
	ports := strings.Split(textArg(args, "ports"), ",")
	if len(ports) > maxPorts {
		return "", fmt.Errorf("SRC port limit exceeded")
	}
	for index, port := range ports {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", fmt.Errorf("provide explicit comma-separated ports; ranges are not allowed")
		}
		ports[index] = strconv.Itoa(number)
	}
	args["target"] = host
	args["ports"] = strings.Join(ports, ",")
	args["scan_type"] = "-sT -Pn -n --max-rate 1 --max-retries 0 --host-timeout 30s"
	return host, nil
}

// prepareSchema limits Spectral to a root-confined OpenAPI document and internal
// references, returning snapshot cleanup to run after tool completion.
func prepareSchema(args map[string]interface{}, roots []string) (func(), error) {
	noop := func() {}
	if err := onlyKeys(args, "schema_url"); err != nil {
		return noop, err
	}
	file, err := openSchemaInRoots(textArg(args, "schema_url"), roots)
	if err != nil {
		return noop, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2*1024*1024 {
		return noop, fmt.Errorf("provide a local OpenAPI document no larger than 2 MiB")
	}
	body, err := io.ReadAll(io.LimitReader(file, 2*1024*1024+1))
	if err != nil || len(body) > 2*1024*1024 {
		return noop, fmt.Errorf("cannot read bounded local OpenAPI document")
	}
	var document map[string]interface{}
	decoder := yaml.NewDecoder(bytes.NewReader(body))
	if decoder.Decode(&document) != nil || document == nil || document["openapi"] == nil && document["swagger"] == nil {
		return noop, fmt.Errorf("provide a valid local OpenAPI document")
	}
	var extra interface{}
	if decoder.Decode(&extra) != io.EOF {
		return noop, fmt.Errorf("provide exactly one OpenAPI document")
	}
	var check func(interface{}) bool
	check = func(node interface{}) bool {
		switch typed := node.(type) {
		case map[string]interface{}:
			for key, value := range typed {
				if key == "$ref" {
					ref, ok := value.(string)
					if !ok || !strings.HasPrefix(ref, "#") {
						return false
					}
				}
				if !check(value) {
					return false
				}
			}
		case []interface{}:
			for _, value := range typed {
				if !check(value) {
					return false
				}
			}
		case map[interface{}]interface{}:
			// Non-string YAML keys have inconsistent meanings across parsers.
			return false
		}
		return true
	}
	if !check(document) {
		return noop, fmt.Errorf("SRC schema inspection only permits document-local references")
	}
	// Spectral consumes a private snapshot of the validated bytes. It never reopens
	// the user-selected pathname, which could change after the root-confined read.
	snapshot, err := os.CreateTemp("", "cyberstrike-src-schema-*.yaml")
	if err != nil {
		return noop, fmt.Errorf("cannot prepare OpenAPI snapshot")
	}
	cleanup := func() { _ = os.Remove(snapshot.Name()) }
	_, writeErr := snapshot.Write(body)
	closeErr := snapshot.Close()
	if writeErr != nil || closeErr != nil {
		cleanup()
		return noop, fmt.Errorf("cannot write OpenAPI snapshot")
	}
	args["schema_url"] = snapshot.Name()
	return cleanup, nil
}

// openSchemaInRoots confines the read at the OS layer, including symlink races.
// The roots come from server session metadata; relative names are rooted there.
func openSchemaInRoots(path string, roots []string) (*os.File, error) {
	for _, directory := range roots {
		relative := path
		if filepath.IsAbs(path) {
			var err error
			relative, err = filepath.Rel(directory, path)
			if err != nil {
				continue
			}
		}
		if !filepath.IsLocal(relative) {
			continue
		}
		root, err := os.OpenRoot(directory)
		if err != nil {
			continue
		}
		file, openErr := root.Open(relative)
		_ = root.Close()
		if openErr == nil {
			return file, nil
		}
	}
	return nil, fmt.Errorf("OpenAPI document must be inside this session's workspace or conversation uploads")
}
