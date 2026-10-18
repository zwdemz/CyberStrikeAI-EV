package handler

import (
	"bytes"
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
	message, err := db.AddMessage(conv.ID, "assistant", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	source, err := db.AddProcessDetailWithID(message.ID, conv.ID, "tool_result", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	identity := database.PlanTaskIdentity(database.ConversationPlanTask{ID: "1", Subject: "Old run"})
	link, err := db.AddPlanTaskEvidence(conv.ID, "1", identity, "tool_result", source)
	if err != nil {
		t.Fatal(err)
	}
	readEvidence := func(method, taskID, endpoint string) int {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body, _ := json.Marshal(map[string]string{"kind": "tool_result", "sourceId": source, "taskIdentity": identity})
		c.Request = httptest.NewRequest(method, "/?kind=project_fact", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: conv.ID}, {Key: "taskId", Value: taskID}, {Key: "linkId", Value: link}}
		c.Set(security.ContextSessionKey, security.Session{UserID: user.ID, Scope: database.RBACScopeAll, Permissions: map[string]bool{"chat:read": true, "chat:write": true, "chat:delete": true}})
		switch endpoint {
		case "search":
			h.GetPlanTaskEvidenceSources(c)
		case "add":
			h.AddPlanTaskEvidence(c)
		case "delete":
			h.DeletePlanTaskEvidence(c)
		default:
			h.GetPlanTaskEvidence(c)
		}
		return w.Code
	}
	for _, endpoint := range []struct{ method, name string }{{http.MethodGet, "list"}, {http.MethodGet, "search"}, {http.MethodPost, "add"}, {http.MethodDelete, "delete"}} {
		if got := readEvidence(endpoint.method, "1", endpoint.name); got != http.StatusNotFound {
			t.Fatalf("archived task %s returned %d", endpoint.name, got)
		}
	}
	if refs, err := db.ListPlanTaskEvidence(conv.ID, "1", identity); err != nil || len(refs) != 1 {
		t.Fatal("archived evidence was mutated", refs, err)
	}
	if got := readEvidence(http.MethodGet, "2", "list"); got != http.StatusOK {
		t.Fatalf("latest saved task unavailable: %d", got)
	}
	h.SetTaskStateProvider(&boardRuntime{running: true, started: time.Now().Add(time.Minute)})
	if got := readEvidence(http.MethodGet, "2", "list"); got != http.StatusNotFound {
		t.Fatalf("evidence lookup ignored active run boundary: %d", got)
	}
}
