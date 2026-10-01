package security_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/security"
	"go.uber.org/zap"
)

func TestDigPMIsRegisteredAndCalledThroughMCP(t *testing.T) {
	t.Setenv("DIG_PM_BASE_URL", "https://provider.example.test")
	tool, err := config.LoadToolFromFile(filepath.Join("..", "..", "..", "tools", "dig-pm-dnslog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if missing := config.CheckToolAvailability([]config.ToolConfig{*tool}); len(missing) != 0 {
		t.Fatal("internal MCP tool requires an external executable")
	}
	server := mcp.NewServer(zap.NewNop())
	server.SetToolAuthorizer(func(context.Context, string, map[string]interface{}) error { return nil })
	executor := security.NewExecutor(&config.SecurityConfig{Tools: []config.ToolConfig{*tool}}, server, zap.NewNop())
	executor.RegisterTools(server)
	definitions := server.GetAllTools()
	if len(definitions) != 1 || definitions[0].Name != "dig-pm-dnslog" {
		t.Fatalf("MCP definitions: %+v", definitions)
	}
	properties := definitions[0].InputSchema["properties"].(map[string]interface{})
	if _, ok := properties["token"]; ok {
		t.Fatal("provider token exposed in MCP schema")
	}
	if len(properties) != 4 {
		t.Fatalf("MCP parameters: %+v", properties)
	}
	for _, ctx := range []context.Context{context.Background(), mcp.WithMCPConversationID(context.Background(), "fixture-conversation"), authctx.WithPrincipal(context.Background(), authctx.NewPrincipal("fixture-user", "fixture", "self", nil))} {
		args := map[string]interface{}{"operation": "unknown"}
		if ctx == context.Background() {
			args["operation"] = "get_domain"
		}
		result, _, err := server.CallTool(ctx, "dig-pm-dnslog", args)
		if err != nil || result == nil || !result.IsError {
			t.Fatalf("MCP boundary result: %v %v", result, err)
		}
		body := mcp.ToolResultPlainText(result)
		if !strings.Contains(body, `"status":"error"`) || strings.Contains(body, "未知的内部工具") {
			t.Fatalf("not routed to DNSLog client: %s", body)
		}
	}
	tool.Enabled = false
	disabled := security.NewExecutor(&config.SecurityConfig{Tools: []config.ToolConfig{*tool}}, nil, zap.NewNop())
	if _, err := disabled.ExecuteTool(context.Background(), "dig-pm-dnslog", map[string]interface{}{"operation": "list_domains"}); err == nil {
		t.Fatal("disabled tool accepted")
	}
}

func TestDigPMRejectsUnsafeEndpointWithoutEchoingCredentials(t *testing.T) {
	t.Setenv("DIG_PM_BASE_URL", "https://fixture:private-value@example.test")
	executor := security.NewExecutor(&config.SecurityConfig{Tools: []config.ToolConfig{{Name: "dig-pm-dnslog", Command: "internal:dig_pm_dnslog", Enabled: true}}}, nil, zap.NewNop())
	result, err := executor.ExecuteTool(context.Background(), "dig-pm-dnslog", map[string]interface{}{"operation": "list_domains"})
	if err != nil || result == nil || !result.IsError || strings.Contains(mcp.ToolResultPlainText(result), "private-value") {
		t.Fatalf("unsafe endpoint result: %+v %v", result, err)
	}
}
