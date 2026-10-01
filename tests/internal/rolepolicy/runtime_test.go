package rolepolicy_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/security"
	"go.uber.org/zap"
)

// TestInstalledSRCTools exercises real managed binaries against local fixtures.
// It is opt-in because managed tool runtimes are deliberately excluded from Git.
func TestInstalledSRCTools(t *testing.T) {
	toolsDir := os.Getenv("CSAI_SRC_TOOLS_DIR")
	if toolsDir == "" {
		t.Skip("set CSAI_SRC_TOOLS_DIR to an installed tools directory")
	}
	var requests atomic.Int32
	httpFixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, "src-fixture-response")
	}))
	defer httpFixture.Close()
	_, port, err := net.SplitHostPort(httpFixture.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(zap.NewNop())
	var installed []config.ToolConfig
	for _, name := range []string{"http-framework-test", "nmap", "jwt-analyzer", "api-schema-analyzer", "waybackurls"} {
		tool, err := config.LoadToolFromFile(filepath.Join("..", "..", "..", "tools", name+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		tool.RuntimeToolsDir = toolsDir
		if _, err := config.ResolveToolCommand(*tool); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		installed = append(installed, *tool)
	}
	executor := security.NewExecutor(&config.SecurityConfig{Tools: installed}, server, zap.NewNop())
	executor.RegisterTools(server)
	ag := agent.NewAgent(&config.OpenAIConfig{}, &config.AgentConfig{}, server, nil, zap.NewNop(), 5)
	ctx, cancel := context.WithTimeout(restricted(t, 3), 90*time.Second)
	defer cancel()
	document := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := os.WriteFile(document, []byte("openapi: 3.0.3\ninfo:\n  title: Fixture\n  version: 1.0.0\npaths: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		args     map[string]interface{}
		contains string
	}{
		{"http-framework-test", map[string]interface{}{"url": httpFixture.URL}, "src-fixture-response"},
		{"nmap", map[string]interface{}{"target": "127.0.0.1", "ports": port}, port + "/tcp"},
		{"jwt-analyzer", map[string]interface{}{"jwt_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJmaXh0dXJlIn0.c2lnbmF0dXJl"}, "fixture"},
		{"api-schema-analyzer", map[string]interface{}{"schema_url": document}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := ag.ExecuteMCPToolForConversation(ctx, "", test.name, test.args)
			if err != nil || result == nil {
				t.Fatalf("tool failed: %v", err)
			}
			if result.IsError || !strings.Contains(result.Result, test.contains) {
				t.Fatalf("tool result: %s", result.Result)
			}
		})
	}
	if requests.Load() != 1 {
		t.Fatalf("HTTP sent %d requests; expected one", requests.Load())
	}
}
