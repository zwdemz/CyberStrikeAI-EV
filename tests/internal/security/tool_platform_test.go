package security_test

import (
	"context"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/security"
	"go.uber.org/zap"
	"testing"
)

func TestUnsupportedPlatformCannotRegisterOrExecute(t *testing.T) {
	cfg := &config.SecurityConfig{Tools: []config.ToolConfig{{Name: "exec", Command: "sh", Enabled: true, Platforms: []string{"fixture-os"}}, {Name: "builtin", Command: "internal:fixture", Enabled: true, Platforms: []string{"fixture-os"}}}}
	server := mcp.NewServer(zap.NewNop())
	executor := security.NewExecutor(cfg, server, zap.NewNop())
	executor.RegisterTools(server)
	if len(server.GetAllTools()) != 0 {
		t.Fatal("unsupported tools advertised")
	}
	for _, name := range []string{"exec", "builtin"} {
		if _, err := executor.ExecuteTool(context.Background(), name, map[string]interface{}{}); err == nil {
			t.Fatal("unsupported tool admitted")
		}
	}
}
