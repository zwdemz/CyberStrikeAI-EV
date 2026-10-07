package handler

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"cyberstrike-ai/internal/database"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TestConversationPageAPI verifies bounds, cursor errors and the legacy response.
// Route authentication/ownership remains covered by the existing RBAC suite.
func TestConversationPageAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.NewDB(filepath.Join(t.TempDir(), "pages.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conv, err := db.CreateConversation("fixture", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		if _, err := db.AddMessage(conv.ID, "user", "fixture", nil); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewConversationHandler(db, zap.NewNop())
	for _, entry := range []struct {
		query         string
		status, count int
	}{
		{"?message_limit=2", 200, 2}, {"", 200, 3},
		{"?message_limit=", 400, 0}, {"?message_limit=0", 400, 0}, {"?message_limit=101", 400, 0},
		{"?message_limit=invalid", 400, 0},
		{"?message_limit=2&include_process_details=1", 400, 0},
		{"?message_limit=2&before_message_id=missing", 400, 0},
	} {
		t.Run(entry.query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest("GET", "/api/conversations/"+conv.ID+entry.query, nil)
			ctx.Params = gin.Params{{Key: "id", Value: conv.ID}}
			handler.GetConversation(ctx)
			if recorder.Code != entry.status {
				t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
			}
			if entry.status == 200 {
				var data database.Conversation
				if err := json.Unmarshal(recorder.Body.Bytes(), &data); err != nil {
					t.Fatal(err)
				}
				if len(data.Messages) != entry.count {
					t.Fatalf("messages: %d", len(data.Messages))
				}
				if entry.query != "" && (data.MessagePage == nil || !data.MessagePage.HasMore) {
					t.Fatal("missing cursor metadata")
				}
			}
		})
	}
}
