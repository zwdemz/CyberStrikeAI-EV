package multiagent

import (
	"context"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/database"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

const (
	progressSectionStart = "<!-- completed-tool-progress-start -->"
	progressSectionEnd   = "<!-- completed-tool-progress-end -->"
)

// conversationProgressRecoveryMiddleware refreshes a bounded reference to
// durable tool completions before every model call, including after Eino
// summarization. No arguments, output, or credentials enter the system prompt.
type conversationProgressRecoveryMiddleware struct {
	adk.BaseChatModelAgentMiddleware
	db             *database.DB
	conversationID string
	logger         *zap.Logger
}

func newConversationProgressRecoveryMiddleware(db *database.DB, conversationID string, logger *zap.Logger) adk.ChatModelAgentMiddleware {
	if db == nil || strings.TrimSpace(conversationID) == "" {
		return nil
	}
	return &conversationProgressRecoveryMiddleware{db: db, conversationID: conversationID, logger: logger}
}

func (m *conversationProgressRecoveryMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if state == nil {
		return ctx, state, nil
	}
	block, err := loadConversationProgressBlock(ctx, m.db, m.conversationID)
	if err != nil {
		if m.logger != nil {
			m.logger.Warn("无法读取会话工具进展", zap.Error(err))
		}
		return ctx, state, nil
	}
	out := *state
	out.Messages = withClassicProgressBlock(state.Messages, block)
	return ctx, &out, nil
}

type agenticConversationProgressRecoveryMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	db             *database.DB
	conversationID string
	logger         *zap.Logger
}

func newAgenticConversationProgressRecoveryMiddleware(db *database.DB, conversationID string, logger *zap.Logger) adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] {
	if db == nil || strings.TrimSpace(conversationID) == "" {
		return nil
	}
	return &agenticConversationProgressRecoveryMiddleware{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
		db:                                db, conversationID: conversationID, logger: logger,
	}
}

func (m *agenticConversationProgressRecoveryMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.TypedChatModelAgentState[*schema.AgenticMessage], _ *adk.TypedModelContext[*schema.AgenticMessage]) (context.Context, *adk.TypedChatModelAgentState[*schema.AgenticMessage], error) {
	if state == nil {
		return ctx, state, nil
	}
	block, err := loadConversationProgressBlock(ctx, m.db, m.conversationID)
	if err != nil {
		if m.logger != nil {
			m.logger.Warn("无法读取会话工具进展", zap.Error(err))
		}
		return ctx, state, nil
	}
	out := *state
	out.Messages = withAgenticProgressBlock(state.Messages, block)
	return ctx, &out, nil
}

func loadConversationProgressBlock(ctx context.Context, db *database.DB, conversationID string) (string, error) {
	steps, err := db.RecentSuccessfulToolSteps(ctx, conversationID, 12)
	if err != nil || len(steps) == 0 {
		return "", err
	}
	var text strings.Builder
	text.WriteString(progressSectionStart + "\n## 已落库的近期工具进展\n")
	text.WriteString("以下仅是本会话工具报告成功的索引，不代表已独立验证目标状态；如需细节，请查看原工具结果。继续任何可能重复的写入或变更前，先核对现有状态。重大进展应先调用 upsert_project_fact 落库（未绑定项目时在本轮明确记录），再继续下一步。\n")
	for _, step := range steps {
		text.WriteString(fmt.Sprintf("- %s: %s (过程详情 %s)\n", step.CreatedAt.Format("2006-01-02 15:04:05"), step.ToolName, step.DetailID))
	}
	text.WriteString(progressSectionEnd)
	return text.String(), nil
}

func replaceProgressBlock(content, block string) string {
	start := strings.Index(content, progressSectionStart)
	if start >= 0 {
		if endOffset := strings.Index(content[start:], progressSectionEnd); endOffset >= 0 {
			end := start + endOffset + len(progressSectionEnd)
			content = strings.TrimSpace(content[:start] + content[end:])
		}
	}
	if block == "" {
		return content
	}
	if content == "" {
		return block
	}
	return strings.TrimSpace(content) + "\n\n" + block
}

func withClassicProgressBlock(messages []adk.Message, block string) []adk.Message {
	out := append([]adk.Message(nil), messages...)
	for index, msg := range out {
		if msg != nil && msg.Role == schema.System {
			cloned := *msg
			cloned.Content = replaceProgressBlock(msg.Content, block)
			out[index] = &cloned
			return out
		}
	}
	if block != "" {
		return append([]adk.Message{schema.SystemMessage(block)}, out...)
	}
	return out
}

func withAgenticProgressBlock(messages []*schema.AgenticMessage, block string) []*schema.AgenticMessage {
	out := append([]*schema.AgenticMessage(nil), messages...)
	for index, msg := range out {
		if msg != nil && msg.Role == schema.AgenticRoleTypeSystem {
			updated := *msg
			updated.ContentBlocks = append([]*schema.ContentBlock(nil), msg.ContentBlocks...)
			replaced := false
			for blockIndex, content := range updated.ContentBlocks {
				if content == nil || content.UserInputText == nil {
					continue
				}
				clonedBlock := *content
				clonedText := *content.UserInputText
				clonedText.Text = replaceProgressBlock(clonedText.Text, block)
				clonedBlock.UserInputText = &clonedText
				updated.ContentBlocks[blockIndex] = &clonedBlock
				replaced = true
				break
			}
			if !replaced && block != "" {
				updated.ContentBlocks = append(updated.ContentBlocks, schema.NewContentBlock(&schema.UserInputText{Text: block}))
			}
			out[index] = &updated
			return out
		}
	}
	if block != "" {
		return append([]*schema.AgenticMessage{EinoMessageToAgentic(schema.SystemMessage(block))}, out...)
	}
	return out
}
