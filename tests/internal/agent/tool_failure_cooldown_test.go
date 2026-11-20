package agent_test

import (
	"context"
	"errors"
	"testing"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"go.uber.org/zap"
)

func TestAgentContinuesAfterFailureCooldown(t *testing.T) {
	logger := zap.NewNop()
	server := mcp.NewServer(logger)
	server.RegisterTool(mcp.Tool{Name: "failing_fixture"}, func(context.Context, map[string]interface{}) (*mcp.ToolResult, error) {
		return nil, errors.New("fixture unavailable")
	})
	server.RegisterTool(mcp.Tool{Name: "healthy_fixture"}, func(context.Context, map[string]interface{}) (*mcp.ToolResult, error) {
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "ok"}}}, nil
	})
	runner := agent.NewAgent(&config.OpenAIConfig{}, &config.AgentConfig{}, server, nil, logger, 10)
	for attempt := 0; attempt < 3; attempt++ {
		result, err := runner.ExecuteMCPToolForConversation(context.Background(), "conversation", "failing_fixture", nil)
		if err != nil || result == nil || !result.IsError {
			t.Fatalf("failure escaped as a run error: %#v, %v", result, err)
		}
		if attempt == 2 && !result.Blocked {
			t.Fatal("third identical call was not cooled down")
		}
	}
	result, err := runner.ExecuteMCPToolForConversation(context.Background(), "conversation", "healthy_fixture", nil)
	if err != nil || result == nil || result.IsError {
		t.Fatalf("independent step could not continue: %#v, %v", result, err)
	}
}
