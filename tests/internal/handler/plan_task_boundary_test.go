package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cyberstrike-ai/internal/database"
	handlerimpl "cyberstrike-ai/internal/handler"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TestPlanTaskBoundarySurvivesRestart ensures old files cannot fill the first
// page of a later run, and a completed board retains that exact boundary.
func TestPlanTaskBoundarySurvivesRestart(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "boundary.db")
	db, err := database.NewDB(file, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	conv, err := db.CreateConversation("boundary", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	user, err := db.CreateRBACUser("boundary-user", "Boundary", "hash", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(root, "plans")
	dir := filepath.Join(base, conv.ID)
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(-time.Minute)
	old := filepath.Join(dir, "1.json")
	newer := filepath.Join(dir, "2.json")
	for path, body := range map[string]string{old: `{"id":"1","subject":"Old run","status":"completed"}`, newer: `{"id":"2","subject":"Latest run","status":"completed"}`} {
		if err = os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.Chtimes(old, start.Add(-time.Hour), start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = db.RecordConversationPlanRun(conv.ID, "latest", start); err != nil {
		t.Fatal(err)
	}
	if err = db.RecordConversationPlanRun(conv.ID, "old", start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = database.NewDB(file, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	db.SetEinoConversationDirs(base, "", "", "")
	h := handlerimpl.NewConversationHandler(db, zap.NewNop())
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?snapshot=latest&limit=1", nil)
	c.Params = gin.Params{{Key: "id", Value: conv.ID}}
	c.Set(security.ContextSessionKey, security.Session{UserID: user.ID, Scope: database.RBACScopeAll})
	h.GetConversationPlanTasks(c)
	var response struct {
		Tasks         []database.ConversationPlanTask `json:"tasks"`
		Total         int                             `json:"total"`
		BoundaryKnown bool                            `json:"boundaryKnown"`
		Running       bool                            `json:"running"`
	}
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 || len(response.Tasks) != 1 || response.Tasks[0].ID != "2" || !response.BoundaryKnown || response.Running {
		t.Fatal(w.Body.String())
	}
}
