package multiagent

import (
	"context"
	"testing"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/einomcp"
	"github.com/cloudwego/eino/compose"
)

func TestToolIdentityAndArgumentsReachReviewer(t *testing.T) {
	tools, err := einomcp.ToolsFromDefinitions(nil, nil, []agent.Tool{{Type: "function", Function: agent.FunctionDefinition{Name: "fs.read"}}}, nil, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	info, err := tools[0].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	reviewed := false
	ctx := WithHITLToolInterceptor(context.Background(), func(_ context.Context, name, arguments string) (string, error) {
		reviewed = true
		if name != "fs.read" || arguments != `{"path":"test"}` {
			t.Fatalf("review saw %q %q", name, arguments)
		}
		return "", nil
	})
	endpoint := compose.InvokableToolEndpoint(func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
		if !reviewed || input.Name != info.Name || originalToolName(ctx, input.Name) != "fs.read" {
			t.Fatal("identity or review ordering lost")
		}
		return &compose.ToolOutput{Result: "ok"}, nil
	})
	endpoint = toolIdentityMiddleware(tools).Invokable(modelOutputExecutionGuardMiddleware().Invokable(hitlToolCallMiddleware().Invokable(endpoint)))
	_, err = endpoint(ctx, &compose.ToolInput{Name: info.Name, Arguments: "```json\n{\"path\":\"test\"}\n```"})
	if err != nil {
		t.Fatal(err)
	}
}
