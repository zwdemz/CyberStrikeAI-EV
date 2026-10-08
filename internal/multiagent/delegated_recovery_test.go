package multiagent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/projectprompt"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

func TestDelegatedAgentTailRestoresPersistedToolProgress(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "delegated.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conversation, err := db.CreateConversation("delegated", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	message, err := db.AddMessage(conversation.ID, "assistant", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddProcessDetail(message.ID, conversation.ID, "tool_result", "done", map[string]interface{}{
		"toolName": "record_vulnerability", "success": true,
	}); err != nil {
		t.Fatal(err)
	}
	handlers := appendDelegatedAgentTailMiddlewares(nil, einoChatModelTailConfig{
		phase: "sub_agent:specialist", conversationID: conversation.ID, logger: zap.NewNop(),
	}, db)
	var recovery *agenticConversationProgressRecoveryMiddleware
	for _, middleware := range handlers {
		if typed, ok := middleware.(*agenticConversationProgressRecoveryMiddleware); ok {
			recovery = typed
		}
	}
	if recovery == nil {
		t.Fatal("delegated tail omitted durable progress recovery")
	}
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{Messages: []*schema.AgenticMessage{
		schema.SystemAgenticMessage("specialist instruction"), schema.UserAgenticMessage("continue"),
	}}
	_, updated, err := recovery.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil || !strings.Contains(agenticMessageText(updated.Messages[0]), "record_vulnerability") {
		t.Fatalf("delegated progress was not restored: %#v, err=%v", updated, err)
	}
}

func TestSpecialistInstructionIncludesSeverityPolicyOnce(t *testing.T) {
	instruction := specialistInstructionWithSeverityPolicy("specialist")
	if !strings.Contains(instruction, projectprompt.VulnerabilitySeverityGuidance) {
		t.Fatal("specialist lacks severity policy")
	}
	if repeated := specialistInstructionWithSeverityPolicy(instruction); strings.Count(repeated, projectprompt.VulnerabilitySeverityGuidance) != 1 {
		t.Fatal("severity policy duplicated")
	}
}
