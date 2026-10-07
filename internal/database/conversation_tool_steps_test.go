package database

import (
	"context"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestRecentSuccessfulToolStepsFiltersAndScopes(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "progress.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first, err := db.CreateConversation("first", ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateConversation("other", ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	firstMessage, err := db.AddMessage(first.ID, "assistant", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	otherMessage, err := db.AddMessage(other.ID, "assistant", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		messageID, conversationID, toolName string
		success, blocked                    bool
		status                              string
	}{
		{firstMessage.ID, first.ID, "create_asset", true, false, ""},
		{firstMessage.ID, first.ID, "failed_tool", false, false, ""},
		{firstMessage.ID, first.ID, "blocked_tool", true, true, "blocked"},
		{firstMessage.ID, first.ID, "background_tool", true, false, "background_running"},
		{firstMessage.ID, first.ID, "unsafe\nname", true, false, ""},
		{firstMessage.ID, first.ID, "create_asset", true, false, ""},
		{otherMessage.ID, other.ID, "other_conversation", true, false, ""},
	} {
		if err := db.AddProcessDetail(sample.messageID, sample.conversationID, "tool_result", "private result", map[string]interface{}{
			"toolName": sample.toolName, "success": sample.success,
			"blocked": sample.blocked, "status": sample.status,
			"result": "secret output must not be copied into the progress index",
		}); err != nil {
			t.Fatal(err)
		}
	}
	steps, err := db.RecentSuccessfulToolSteps(context.Background(), first.ID, 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].ToolName != "create_asset" || steps[0].DetailID == "" || steps[0].CreatedAt.IsZero() {
		t.Fatalf("unexpected durable steps: %+v", steps)
	}
}
