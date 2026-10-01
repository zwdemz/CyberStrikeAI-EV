package rolepolicy_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/handler"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/workflow"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestRestrictedAgentWaitsForWorkerBeyondAsyncTimeout(t *testing.T) {
	server := mcp.NewServer(zap.NewNop())
	server.ConfigureToolWaitTimeoutSeconds(1)
	server.RegisterTool(mcp.Tool{Name: "nmap"}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		select {
		case <-time.After(1200 * time.Millisecond):
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "completed-fixture"}}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	ag := agent.NewAgent(&config.OpenAIConfig{}, &config.AgentConfig{}, server, nil, zap.NewNop(), 5)
	result, err := ag.ExecuteMCPToolForConversation(restricted(t, 1), "", "nmap", map[string]interface{}{"target": "127.0.0.1", "ports": "80"})
	if err != nil || result == nil || result.IsError || !strings.Contains(result.Result, "completed-fixture") {
		t.Fatalf("returned before worker completed: %#v %v", result, err)
	}
}

func TestAgentAppliesPolicyBeforeMCPAndPreservesDependencyFailures(t *testing.T) {
	for _, transportError := range []bool{false, true} {
		server := mcp.NewServer(zap.NewNop())
		calls := 0
		server.RegisterTool(mcp.Tool{Name: "nmap"}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
			calls++
			if args["scan_type"] != "-sT -Pn -n --max-rate 1 --max-retries 0 --host-timeout 30s" {
				t.Errorf("unsafe arguments reached MCP: %v", args)
			}
			if transportError {
				return nil, errors.New("executable file not found")
			}
			return &mcp.ToolResult{IsError: true, Content: []mcp.Content{{Type: "text", Text: "nmap: command not found"}}}, nil
		})
		ag := agent.NewAgent(&config.OpenAIConfig{}, &config.AgentConfig{}, server, nil, zap.NewNop(), 5)
		ctx := restricted(t, 1)
		blocked, err := ag.ExecuteMCPToolForConversation(ctx, "", "nmap", map[string]interface{}{"target": "127.0.0.1/24", "ports": "80"})
		if err != nil || !blocked.Blocked || calls != 0 {
			t.Fatalf("blocked call reached MCP: %v %v, calls=%d", blocked, err, calls)
		}
		result, err := ag.ExecuteMCPToolForConversation(ctx, "", "nmap", map[string]interface{}{"target": "127.0.0.1", "ports": "80"})
		if err != nil || !result.IsError || result.Blocked || calls != 1 || !strings.Contains(result.Result, "tools/runtime") || strings.Contains(result.Result, "execute-python-script") {
			t.Fatalf("unexpected dependency handling: %#v %v, calls=%d", result, err, calls)
		}
	}
}

func TestRoleUpdatePreservesPolicyForOldClients(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	cfg := &config.Config{RolesDir: "roles", Roles: map[string]config.RoleConfig{
		"src": {Name: "src", Enabled: true, Tools: []string{"nmap"}, ToolPolicy: config.RoleToolPolicy{Profile: "src-low-impact"}},
	}}
	h := handler.NewRoleHandler(cfg, filepath.Join(root, "config.yaml"), zap.NewNop())
	router := gin.New()
	router.PUT("/roles/:name", h.UpdateRole)
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"name":"src","enabled":true,"tools":["nmap"]}`, http.StatusOK},
		{`{"name":"src","enabled":true,"tools":["exec"]}`, http.StatusBadRequest},
		{`{"name":"src","enabled":true,"tools":[]}`, http.StatusBadRequest},
	} {
		request := httptest.NewRequest(http.MethodPut, "/roles/src", strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("status=%d: %s", response.Code, response.Body)
		}
	}
	saved, err := config.LoadRoleFromFile(filepath.Join(root, "roles", "src.yaml"))
	if err != nil || saved.ToolPolicy.Profile != "src-low-impact" || len(saved.Tools) != 1 || saved.Tools[0] != "nmap" {
		t.Fatalf("policy was lost or rejected data was saved: %+v %v", saved, err)
	}
}

func TestWorkflowRejectsInvalidRoleBeforeExecution(t *testing.T) {
	_, err := workflow.RunRoleBoundWorkflow(context.Background(), workflow.RunArgs{Role: config.RoleConfig{
		Tools: []string{"exec"}, ToolPolicy: config.RoleToolPolicy{Profile: "src-low-impact"},
	}})
	if err == nil || !strings.Contains(err.Error(), "outside the SRC policy") {
		t.Fatalf("unexpected result: %v", err)
	}
}
