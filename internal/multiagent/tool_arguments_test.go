package multiagent

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/compose"
)

func TestFixToolArgumentsConservative(t *testing.T) {
	for _, raw := range []string{`{"path":"a}"}`, "```json\n{}\n```", "```\r\n{\"path\":\"a}\"}\r\n```"} {
		fixed := fixToolCallArguments(raw)
		if raw[0] == '`' && fixed == "" {
			t.Fatalf("not repaired: %q", raw)
		}
	}
	for _, raw := range []string{"```json\nnull\n```", "```json\n[]\n```", "```json\n{} {}\n```", "```json\n{}\n``` prose", "{} trailing", "{broken}"} {
		if fixed := fixToolCallArguments(raw); fixed != "" {
			t.Fatalf("unsafe repair %q -> %q", raw, fixed)
		}
	}
	wrapped := modelOutputExecutionGuardMiddleware().Invokable(func(_ context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
		if input.Arguments != "{}" {
			t.Fatalf("downstream sees %q", input.Arguments)
		}
		return &compose.ToolOutput{}, nil
	})
	if _, err := wrapped(context.Background(), &compose.ToolInput{Arguments: "```json\n{}\n```"}); err != nil {
		t.Fatal(err)
	}
}
