package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"cyberstrike-ai/internal/database"
	handlerimpl "cyberstrike-ai/internal/handler"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TestConversationBoardSnapshot verifies explicit, scoped access to a saved board
// after execution stops, while preserving the live chat's empty stopped state.
func TestConversationBoardSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmp := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmp, "board.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conversation, err := db.CreateConversation("board fixture", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	user, err := db.CreateRBACUser("board-fixture", "Fixture", "hash", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(tmp, "plans")
	db.SetEinoConversationDirs(base, "", "", "")
	dir := filepath.Join(base, conversation.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for id, body := range map[string]string{
		"1": `{"id":"1","subject":"Plan","status":"pending"}`,
		"2": `{"id":"2","subject":"Execute","status":"in_progress"}`,
		"3": `{"id":"3","subject":"Review evidence","status":"completed"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	handler := handlerimpl.NewConversationHandler(db, zap.NewNop())
	request := func(query string, authenticated bool) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/conversations/"+conversation.ID+"/plan-tasks"+query, nil)
		c.Params = gin.Params{{Key: "id", Value: conversation.ID}}
		if authenticated {
			c.Set(security.ContextSessionKey, security.Session{UserID: user.ID, Scope: database.RBACScopeAssigned})
		}
		handler.GetConversationPlanTasks(c)
		return w
	}
	for _, authenticated := range []bool{false, true} {
		if got := request("?snapshot=latest", authenticated); got.Code != http.StatusForbidden {
			t.Fatalf("unassigned access: %d", got.Code)
		}
	}
	if err := db.AssignResourceToUser(user.ID, "conversation", conversation.ID); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"?snapshot=wrong", "?snapshot=latest&limit=0", "?snapshot=latest&limit=101", "?snapshot=latest&offset=-1", "?snapshot=latest&offset=bad"} {
		if got := request(query, true); got.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", query, got.Code)
		}
	}
	for _, tc := range []struct {
		query         string
		total, length int
		first         string
	}{
		{"", 0, 0, ""},
		{"?snapshot=latest&limit=2&offset=1", 3, 2, "2"},
		{"?snapshot=latest&offset=999", 3, 0, ""},
	} {
		got := request(tc.query, true)
		if got.Code != http.StatusOK {
			t.Fatalf("%s: %d", tc.query, got.Code)
		}
		var body struct {
			Tasks   []database.ConversationPlanTask `json:"tasks"`
			Total   int                             `json:"total"`
			Running bool                            `json:"running"`
		}
		if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Total != tc.total || len(body.Tasks) != tc.length || body.Running {
			t.Fatalf("%s: %#v", tc.query, body)
		}
		if tc.first != "" && body.Tasks[0].ID != tc.first {
			t.Fatalf("unexpected task page: %#v", body.Tasks)
		}
	}
}
