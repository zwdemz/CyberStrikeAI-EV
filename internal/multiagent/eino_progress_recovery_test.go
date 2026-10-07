package multiagent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/database"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

func TestConversationProgressRecoveryRefreshesAfterToolResult(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "progress.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conversation, err := db.CreateConversation("progress", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	message, err := db.AddMessage(conversation.ID, "assistant", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	middleware := newAgenticConversationProgressRecoveryMiddleware(db, conversation.ID, zap.NewNop())
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{Messages: []*schema.AgenticMessage{
		EinoMessageToAgentic(schema.SystemMessage("baseline instruction")),
		EinoMessageToAgentic(schema.UserMessage("user request")),
	}}
	_, first, err := middleware.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Messages) != 2 || strings.Contains(AgenticMessageToEino(first.Messages[0])[0].Content, progressSectionStart) {
		t.Fatal("empty conversation should not add a progress index")
	}
	if err := db.AddProcessDetail(message.ID, conversation.ID, "tool_result", "result", map[string]interface{}{
		"toolName": "create_asset", "success": true,
		"result": "private output and credentials are not prompt material",
	}); err != nil {
		t.Fatal(err)
	}
	_, second, err := middleware.BeforeModelRewriteState(context.Background(), first, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := AgenticMessageToEino(second.Messages[0])[0].Content
	if !strings.Contains(text, "create_asset") || !strings.Contains(text, "baseline instruction") || strings.Contains(text, "private output") {
		t.Fatalf("unexpected refreshed progress: %q", text)
	}
	_, third, err := middleware.BeforeModelRewriteState(context.Background(), second, nil)
	if err != nil {
		t.Fatal(err)
	}
	text = AgenticMessageToEino(third.Messages[0])[0].Content
	if strings.Count(text, progressSectionStart) != 1 || strings.Count(text, "create_asset") != 1 {
		t.Fatalf("progress index duplicated on repeated model call: %q", text)
	}
}

func TestClassicConversationProgressRecoveryKeepsMessages(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "progress.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conversation, err := db.CreateConversation("classic", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	message, err := db.AddMessage(conversation.ID, "assistant", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddProcessDetail(message.ID, conversation.ID, "tool_result", "result", map[string]interface{}{
		"toolName": "record_vulnerability", "success": true,
	}); err != nil {
		t.Fatal(err)
	}
	middleware := newConversationProgressRecoveryMiddleware(db, conversation.ID, zap.NewNop())
	state := &adk.ChatModelAgentState{Messages: []adk.Message{
		schema.SystemMessage("system"), schema.UserMessage("question"),
	}}
	_, updated, err := middleware.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Messages) != 2 || !strings.Contains(updated.Messages[0].Content, "record_vulnerability") || updated.Messages[1].Content != "question" {
		t.Fatalf("classic progress changed conversation structure: %+v", updated.Messages)
	}
	if strings.Contains(state.Messages[0].Content, progressSectionStart) {
		t.Fatal("progress injection mutated input state")
	}
}
