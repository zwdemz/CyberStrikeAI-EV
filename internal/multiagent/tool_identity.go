package multiagent

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

type originalToolNamesKey struct{}

func originalToolName(ctx context.Context, name string) string {
	names, _ := ctx.Value(originalToolNamesKey{}).(map[string]string)
	if original, ok := names[name]; ok {
		return original
	}
	return name
}

// toolIdentityMiddleware carries registered identities to authorization and
// review without changing the provider-facing dispatch name. The immutable map
// is local to this agent's tool set; no global alias registry can cross roles.
func toolIdentityMiddleware(tools []tool.BaseTool) compose.ToolMiddleware {
	names := make(map[string]string)
	for _, item := range tools {
		if original, ok := item.(interface{ OriginalName() string }); ok {
			info, err := item.Info(context.Background())
			if err == nil && info != nil {
				names[info.Name] = original.OriginalName()
			}
		}
	}
	return compose.ToolMiddleware{
		Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
				return next(context.WithValue(ctx, originalToolNamesKey{}, names), input)
			}
		},
		Streamable: func(next compose.StreamableToolEndpoint) compose.StreamableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.StreamToolOutput, error) {
				return next(context.WithValue(ctx, originalToolNamesKey{}, names), input)
			}
		},
	}
}
