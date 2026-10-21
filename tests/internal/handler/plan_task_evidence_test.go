package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	handlerimpl "cyberstrike-ai/internal/handler"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type boardRuntime struct {
	running bool
	started time.Time
}

func (state *boardRuntime) ConversationTaskRuntimeState(string) (bool, time.Time) {
	return state.running, state.started
}

// TestPlanTaskEvidence verifies persistence, source permissions, cross-project
// isolation, identity conflicts, idempotent writes, unlinking and safe projections.
func TestPlanTaskEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	dbPath := filepath.Join(root, "board.db")
	db, err := database.NewDB(dbPath, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	project, err := db.CreateProject(&database.Project{Name: "allowed"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateProject(&database.Project{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := db.CreateConversation("board", database.ConversationCreateMeta{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := db.CreateConversation("foreign", database.ConversationCreateMeta{ProjectID: other.ID})
	if err != nil {
		t.Fatal(err)
	}
	message, err := db.AddMessage(conv.ID, "assistant", "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	foreignMessage, err := db.AddMessage(foreign.ID, "assistant", "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	source, err := db.AddProcessDetailWithID(message.ID, conv.ID, "tool_result", "PRIVATE OUTPUT", map[string]interface{}{"toolName": "query_assets", "result": "PRIVATE PAYLOAD"})
	if err != nil {
		t.Fatal(err)
	}
	foreignSource, err := db.AddProcessDetailWithID(foreignMessage.ID, foreign.ID, "tool_result", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	vuln, err := db.CreateVulnerability(&database.Vulnerability{ConversationID: conv.ID, ProjectID: project.ID, Title: "PRIVATE VULN", Severity: "low"})
	if err != nil {
		t.Fatal(err)
	}
	fact, err := db.UpsertProjectFact(&database.ProjectFact{ProjectID: project.ID, FactKey: "evidence/one", Summary: "Fact", Body: "PRIVATE BODY"})
	if err != nil {
		t.Fatal(err)
	}
	foreignFact, err := db.UpsertProjectFact(&database.ProjectFact{ProjectID: other.ID, FactKey: "evidence/foreign", Summary: "PRIVATE FOREIGN"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := db.CreateRBACUser("board-user", "Board", "hash", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(root, "plans")
	dir := filepath.Join(base, conv.ID)
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	task := database.ConversationPlanTask{ID: "1", Subject: "Collect evidence", Status: "in_progress"}
	writeTask := func() {
		t.Helper()
		data, _ := json.Marshal(task)
		if err := os.WriteFile(filepath.Join(dir, "1.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeTask()
	db.SetEinoConversationDirs(base, "", "", "")
	h := handlerimpl.NewConversationHandler(db, zap.NewNop())
	h.SetAudit(audit.NewService(db, &config.Config{}, zap.NewNop()))
	permissions := map[string]bool{"chat:read": true, "chat:write": true, "chat:delete": true, "project:read": true, "vulnerability:read": true}
	session := security.Session{UserID: user.ID, Scope: database.RBACScopeAll, Permissions: permissions}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(security.ContextSessionKey, session) }, security.RBACMiddleware(db))
	router.GET("/api/conversations/:id/plan-tasks/:taskId/evidence", h.GetPlanTaskEvidence)
	router.GET("/api/conversations/:id/plan-tasks/:taskId/evidence-sources", h.GetPlanTaskEvidenceSources)
	router.POST("/api/conversations/:id/plan-tasks/:taskId/evidence", h.AddPlanTaskEvidence)
	router.DELETE("/api/conversations/:id/plan-tasks/:taskId/evidence/:linkId", h.DeletePlanTaskEvidence)
	url := "/api/conversations/" + conv.ID + "/plan-tasks/1/evidence"
	request := func(method, path string, body interface{}) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	add := func(kind, id, identity string) *httptest.ResponseRecorder {
		return request(http.MethodPost, url, map[string]string{"kind": kind, "sourceId": id, "taskIdentity": identity})
	}
	identity := database.PlanTaskIdentity(task)
	if got := request(http.MethodGet, strings.TrimSuffix(url, "/evidence")+"/evidence-sources?kind=project_fact", nil); got.Code != 200 || !strings.Contains(got.Body.String(), fact.ID) || strings.Contains(got.Body.String(), "PRIVATE BODY") || strings.Contains(got.Body.String(), foreignFact.ID) {
		t.Fatal("source picker scope/payload failure", got.Body.String())
	}
	t.Run("fact visibility precedes result cap", func(t *testing.T) {
		if err := db.SetResourceOwner("conversation", conv.ID, user.ID); err != nil {
			t.Fatal(err)
		}
		hiddenVuln, err := db.CreateVulnerability(&database.Vulnerability{ProjectID: other.ID, ConversationID: foreign.ID, Title: "Hidden source", Severity: "low"})
		if err != nil {
			t.Fatal(err)
		}
		candidates := []*database.ProjectFact{fact}
		for index := 0; index < 30; index++ {
			hidden := &database.ProjectFact{ProjectID: project.ID, FactKey: fmt.Sprintf("evidence/hidden-%d", index), Summary: "PRIVATE HIDDEN", Pinned: true}
			if index%2 == 0 {
				hidden.SourceConversationID = foreign.ID
			} else {
				hidden.RelatedVulnerabilityID = hiddenVuln.ID
			}
			saved, err := db.UpsertProjectFact(hidden)
			if err != nil {
				t.Fatal(err)
			}
			if index < 2 {
				candidates = append(candidates, saved)
			}
		}
		session.PermissionScopes = map[string]string{"chat:read": database.RBACScopeOwn, "project:read": database.RBACScopeAll, "vulnerability:read": database.RBACScopeAssigned}
		defer func() { session.PermissionScopes = nil }()
		for _, search := range []string{"", "&search=evidence"} {
			got := request(http.MethodGet, strings.TrimSuffix(url, "/evidence")+"/evidence-sources?kind=project_fact"+search, nil)
			if got.Code != 200 || !strings.Contains(got.Body.String(), fact.ID) || strings.Contains(got.Body.String(), "PRIVATE HIDDEN") {
				t.Fatal("hidden sources consumed the readable page", got.Body.String())
			}
		}
		if err := db.SetResourceOwner("project", project.ID, user.ID); err != nil {
			t.Fatal(err)
		}
		if err := db.AssignResourceToUser(user.ID, "vulnerability", hiddenVuln.ID); err != nil {
			t.Fatal(err)
		}
		if err := db.SetResourceOwner("vulnerability", vuln.ID, user.ID); err != nil {
			t.Fatal(err)
		}
		inherited, err := db.UpsertProjectFact(&database.ProjectFact{ProjectID: project.ID, FactKey: "evidence/inherited", Summary: "Inherited", SourceConversationID: conv.ID, RelatedVulnerabilityID: vuln.ID})
		if err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, inherited)
		// SQL paging predicates must retain the exact independent RBAC scopes,
		// including direct assignment and project/conversation inheritance.
		for _, scope := range []string{database.RBACScopeAll, database.RBACScopeOwn, database.RBACScopeAssigned} {
			read := database.RBACListAccess{UserID: user.ID, Scope: scope}
			access := database.ProjectFactEvidenceAccess{Project: database.RBACListAccess{UserID: user.ID, Scope: database.RBACScopeAll}, Conversation: &read, Vulnerability: &read}
			for _, candidate := range candidates {
				rows, err := db.ListProjectFactsForEvidence(project.ID, candidate.FactKey, access)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, row := range rows {
					found = found || row.ID == candidate.ID
				}
				want := (candidate.SourceConversationID == "" || db.UserCanAccessResource(user.ID, scope, "conversation", candidate.SourceConversationID)) &&
					(candidate.RelatedVulnerabilityID == "" || db.UserCanAccessResource(user.ID, scope, "vulnerability", candidate.RelatedVulnerabilityID))
				if found != want {
					t.Fatalf("scope %s fact %s visibility=%v want=%v", scope, candidate.FactKey, found, want)
				}
			}
			for _, candidate := range []*database.Vulnerability{vuln, hiddenVuln} {
				rows, err := db.ListVulnerabilitiesForEvidence(candidate.ProjectID, conv.ID, candidate.Title, read)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, row := range rows {
					found = found || row.ID == candidate.ID
				}
				if want := db.UserCanAccessResource(user.ID, scope, "vulnerability", candidate.ID); found != want {
					t.Fatalf("vulnerability scope %s visibility=%v want=%v", scope, found, want)
				}
			}
		}
		rows, err := db.ListProjectFactsForEvidence(project.ID, "evidence/inherited", database.ProjectFactEvidenceAccess{Project: database.RBACListAccess{UserID: user.ID, Scope: database.RBACScopeAll}})
		if err != nil || len(rows) != 0 {
			t.Fatal("missing provenance read permissions exposed a source", rows, err)
		}
	})
	for _, tc := range []struct {
		kind, id string
		code     int
	}{{"tool_result", source, 200}, {"tool_result", foreignSource, 404}, {"project_fact", fact.ID, 200}, {"project_fact", foreignFact.ID, 404}, {"vulnerability", vuln.ID, 200}, {"bad", source, 400}} {
		if got := add(tc.kind, tc.id, identity); got.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.kind, got.Code, got.Body.String())
		}
	}
	first := add("tool_result", source, identity)
	second := add("tool_result", source, identity)
	if first.Body.String() != second.Body.String() {
		t.Fatal("duplicate link was not idempotent")
	}
	if count, err := db.CountAuditLogs(database.ListAuditLogsFilter{Action: "link_task_evidence"}); err != nil || count == 0 {
		t.Fatal("link audit missing", count, err)
	}
	var linked struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(first.Body.Bytes(), &linked)
	if got := request(http.MethodGet, url, nil); strings.Contains(got.Body.String(), "PRIVATE PAYLOAD") || strings.Contains(got.Body.String(), "PRIVATE BODY") {
		t.Fatal("raw evidence payload leaked")
	}
	if got := request(http.MethodDelete, url+"/"+linked.ID, nil); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	if _, err := db.GetProcessDetailByID(source); err != nil {
		t.Fatal("unlink deleted source")
	}
	if got := add("tool_result", source, identity); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	if got := add("tool_result", source, "stale"); got.Code != 409 {
		t.Fatal("stale task identity accepted")
	}
	permissions["chat:write"] = false
	if got := add("tool_result", source, identity); got.Code != 403 {
		t.Fatal("read-only user wrote a reference")
	}
	permissions["chat:write"] = true
	session.PermissionScopes = map[string]string{"chat:read": database.RBACScopeAssigned, "chat:write": database.RBACScopeAll}
	if got := add("tool_result", source, identity); got.Code != 404 {
		t.Fatal("write scope widened source read access")
	}
	session.PermissionScopes = nil
	permissions["vulnerability:read"] = false
	permissions["project:read"] = false
	if got := request(http.MethodGet, strings.TrimSuffix(url, "/evidence")+"/evidence-sources?kind=vulnerability", nil); got.Code != 200 || strings.Contains(got.Body.String(), vuln.ID) {
		t.Fatal("picker bypassed source read permission")
	}
	got := request(http.MethodGet, url, nil)
	if got.Code != 200 || strings.Contains(got.Body.String(), "PRIVATE") || strings.Contains(got.Body.String(), vuln.ID) || strings.Contains(got.Body.String(), fact.ID) {
		t.Fatalf("source permission leak: %s", got.Body.String())
	}
	if got := add("vulnerability", vuln.ID, identity); got.Code != 404 {
		t.Fatal("missing source read permission accepted")
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = database.NewDB(dbPath, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	refs, err := db.ListPlanTaskEvidence(conv.ID, "1", identity)
	if err != nil || len(refs) != 3 {
		t.Fatalf("references not durable: %#v %v", refs, err)
	}
	db.SetEinoConversationDirs(base, "", "", "")
	h = handlerimpl.NewConversationHandler(db, zap.NewNop())
	// Existing router methods hold the old handler; a fresh handler checks renamed tasks directly.
	task.Subject = "Different task"
	writeTask()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, url, nil)
	c.Params = gin.Params{{Key: "id", Value: conv.ID}, {Key: "taskId", Value: "1"}}
	c.Set(security.ContextSessionKey, session)
	h.GetPlanTaskEvidence(c)
	if w.Code != 200 || strings.Contains(w.Body.String(), source) {
		t.Fatal("reused task ID inherited old evidence")
	}
	removed, err := db.DeletePlanTaskEvidence(foreign.ID, "1", identity, refs[0].ID)
	if err != nil || removed {
		t.Fatal("cross-conversation unlink succeeded")
	}
	removed, err = db.DeletePlanTaskEvidence(conv.ID, "1", identity, refs[0].ID)
	if err != nil || !removed {
		t.Fatal("unlink failed")
	}
	if _, err = db.GetProcessDetailByID(source); err != nil {
		t.Fatal("unlink removed source evidence")
	}
}

// TestPlanTaskEvidenceCapacity ensures concurrent clients cannot grow one task's
// references without bound, while duplicate submissions remain idempotent.
func TestPlanTaskEvidenceCapacity(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "capacity.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conv, err := db.CreateConversation("capacity", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	message, err := db.AddMessage(conv.ID, "assistant", "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	sources := make([]string, 40)
	for index := range sources {
		sources[index], err = db.AddProcessDetailWithID(message.ID, conv.ID, "tool_result", "", nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	var workers sync.WaitGroup
	results := make(chan error, 40)
	for index := 0; index < 40; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			_, err := db.AddPlanTaskEvidence(conv.ID, "1", "identity", "tool_result", sources[index])
			results <- err
		}(index)
	}
	workers.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else if !errors.Is(err, database.ErrPlanTaskEvidenceLimit) {
			t.Fatal(err)
		}
	}
	if accepted != 20 {
		t.Fatal("concurrent limit not enforced", accepted)
	}
	refs, err := db.ListPlanTaskEvidence(conv.ID, "1", "identity")
	if err != nil || len(refs) != 20 {
		t.Fatal(refs, err)
	}
	if _, err := db.AddPlanTaskEvidence(conv.ID, "1", "identity", "tool_result", refs[0].SourceID); err != nil {
		t.Fatal("duplicate should not consume capacity", err)
	}
	if _, err := db.AddPlanTaskEvidence(conv.ID, "1", "identity", "tool_result", "overflow"); !errors.Is(err, database.ErrPlanTaskEvidenceLimit) {
		t.Fatal("unbounded references", err)
	}
	if _, err = db.Exec(`DELETE FROM process_details WHERE id=?`, refs[0].SourceID); err != nil {
		t.Fatal(err)
	}
	extra, err := db.AddProcessDetailWithID(message.ID, conv.ID, "tool_result", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AddPlanTaskEvidence(conv.ID, "1", "identity", "tool_result", extra); err != nil {
		t.Fatal("deleted source kept consuming capacity", err)
	}
	refs, err = db.ListPlanTaskEvidence(conv.ID, "1", "identity")
	if err != nil || len(refs) != 20 {
		t.Fatal(refs, err)
	}
}

// TestPlanTaskEvidencePrunesInvalidSources covers deletion and source moves,
// while retaining in-scope sources regardless of the requesting user's permissions.
func TestPlanTaskEvidencePrunesInvalidSources(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "prune.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.CreateProject(&database.Project{Name: "prune"})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := db.CreateConversation("prune", database.ConversationCreateMeta{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	fact, err := db.UpsertProjectFact(&database.ProjectFact{ProjectID: project.ID, FactKey: "evidence/live", Summary: "Live"})
	if err != nil {
		t.Fatal(err)
	}
	removed, err := db.UpsertProjectFact(&database.ProjectFact{ProjectID: project.ID, FactKey: "evidence/deleted", Summary: "Deleted"})
	if err != nil {
		t.Fatal(err)
	}
	vuln, err := db.CreateVulnerability(&database.Vulnerability{ProjectID: project.ID, ConversationID: conv.ID, Title: "Deleted", Severity: "low"})
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []database.PlanTaskEvidence{{Kind: "project_fact", SourceID: fact.ID}, {Kind: "project_fact", SourceID: removed.ID}, {Kind: "vulnerability", SourceID: vuln.ID}} {
		if _, err = db.AddPlanTaskEvidence(conv.ID, "1", "identity", ref.Kind, ref.SourceID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`DELETE FROM project_facts WHERE id=?`, removed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DELETE FROM vulnerabilities WHERE id=?`, vuln.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AddPlanTaskEvidence(conv.ID, "1", "identity", "project_fact", fact.ID); err != nil {
		t.Fatal(err)
	}
	refs, err := db.ListPlanTaskEvidence(conv.ID, "1", "identity")
	if err != nil || len(refs) != 1 || refs[0].SourceID != fact.ID {
		t.Fatalf("pruned a live source or retained a missing source: %#v %v", refs, err)
	}
	other, err := db.CreateProject(&database.Project{Name: "moved sources"})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 20; index++ {
		moved, err := db.CreateVulnerability(&database.Vulnerability{ProjectID: project.ID, ConversationID: conv.ID, Title: fmt.Sprintf("Moved %d", index), Severity: "low"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.AddPlanTaskEvidence(conv.ID, "2", "moved", "vulnerability", moved.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE vulnerabilities SET project_id=? WHERE project_id=?`, other.ID, project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddPlanTaskEvidence(conv.ID, "2", "moved", "project_fact", fact.ID); err != nil {
		t.Fatal("moved findings kept consuming all capacity", err)
	}
	if refs, err := db.ListPlanTaskEvidence(conv.ID, "2", "moved"); err != nil || len(refs) != 1 || refs[0].SourceID != fact.ID {
		t.Fatal("structurally out-of-scope references were retained", refs, err)
	}
	var preserved int
	if err := db.QueryRow(`SELECT count(*) FROM vulnerabilities WHERE project_id=?`, other.ID).Scan(&preserved); err != nil || preserved != 20 {
		t.Fatal("cleanup modified source findings", preserved, err)
	}
}

// TestPlanTaskSnapshotSync checks partial writes and the runtime transition from
// active to stopped without inventing task completion or hiding saved work.
func TestPlanTaskSnapshotSync(t *testing.T) {
	root := t.TempDir()
	db, err := database.NewDB(filepath.Join(root, "sync.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conv, err := db.CreateConversation("sync", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	user, err := db.CreateRBACUser("sync-user", "Sync", "hash", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(root, "plans")
	dir := filepath.Join(base, conv.ID)
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "1.json")
	if err = os.WriteFile(file, []byte(`{"id":"1","subject":"Saved","status":"in_progress"}`), 0600); err != nil {
		t.Fatal(err)
	}
	db.SetEinoConversationDirs(base, "", "", "")
	h := handlerimpl.NewConversationHandler(db, zap.NewNop())
	runtime := &boardRuntime{running: true, started: time.Now().Add(time.Minute)}
	h.SetTaskStateProvider(runtime)
	read := func() string {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/?snapshot=latest", nil)
		c.Params = gin.Params{{Key: "id", Value: conv.ID}}
		c.Set(security.ContextSessionKey, security.Session{UserID: user.ID, Scope: database.RBACScopeAll})
		h.GetConversationPlanTasks(c)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		return w.Body.String()
	}
	if got := read(); !strings.Contains(got, `"tasks":[]`) {
		t.Fatal(got)
	}
	if err := db.RecordConversationPlanRun(conv.ID, "run-current", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	runtime.running = false
	if got := read(); !strings.Contains(got, `"runtimeStatus":"paused"`) || strings.Contains(got, `"status":"completed"`) {
		t.Fatal(got)
	}
	if err = os.WriteFile(file, []byte(`{"id":`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := read(); !strings.Contains(got, `"incomplete":true`) {
		t.Fatal(got)
	}
}
