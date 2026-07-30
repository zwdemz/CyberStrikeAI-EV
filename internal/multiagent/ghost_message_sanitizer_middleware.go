package multiagent

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// ghostMessageSanitizerMiddleware drops assistant turns that carry neither content
// nor tool_calls before each ChatModel call.
//
// Background: glm-5.2 occasionally returns a reasoning-only turn (reasoning_content
// but no content and no tool_calls). The eino-ext client persists it as an
// assistant message whose `content` field is absent in the serialized request.
// Strict OpenAI-compatible gateways (the ARK coding-plan endpoint included)
// reject such messages with 400 "missing messages.content" and Eino wraps it as
// [NodeRunError], terminating the run. These ghost turns carry no actionable
// information, so dropping them keeps the request valid with no semantic loss.
//
// It is a defensive no-op when every message already has content or tool_calls.
// Position: after orphan-tool prune, close to the ChatModel call (so later
// rewriters see a clean trail).
type ghostMessageSanitizerMiddleware struct {
	adk.BaseChatModelAgentMiddleware
	logger *zap.Logger
	phase  string
}

func newGhostMessageSanitizerMiddleware(logger *zap.Logger, phase string) adk.ChatModelAgentMiddleware {
	return &ghostMessageSanitizerMiddleware{logger: logger, phase: phase}
}

// BeforeModelRewriteState scans for ghost assistant messages (role=assistant,
// no tool_calls, empty content) and rebuilds the slice without them.
//
// Dropping is safe for tool_call↔tool_result pairing: a ghost has no tool_calls,
// so it is never a tool-call assistant whose removal would orphan a tool result.
func (m *ghostMessageSanitizerMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.ChatModelAgentState,
	mc *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	_ = mc
	if m == nil || state == nil || len(state.Messages) == 0 {
		return ctx, state, nil
	}

	isGhost := func(msg adk.Message) bool {
		return msg != nil &&
			msg.Role == schema.Assistant &&
			len(msg.ToolCalls) == 0 &&
			strings.TrimSpace(msg.Content) == ""
	}

	hasGhost := false
	for _, msg := range state.Messages {
		if isGhost(msg) {
			hasGhost = true
			break
		}
	}
	if !hasGhost {
		return ctx, state, nil
	}

	pruned := make([]adk.Message, 0, len(state.Messages))
	dropped := 0
	for _, msg := range state.Messages {
		if isGhost(msg) {
			dropped++
			continue
		}
		pruned = append(pruned, msg)
	}

	if m.logger != nil {
		m.logger.Warn("eino ghost assistant messages dropped before model call",
			zap.String("phase", m.phase),
			zap.Int("dropped_count", dropped),
			zap.Int("messages_before", len(state.Messages)),
			zap.Int("messages_after", len(pruned)),
		)
	}

	ns := *state
	ns.Messages = pruned
	return ctx, &ns, nil
}
