package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type planEvidenceView struct {
	database.PlanTaskEvidence
	Label     string `json:"label"`
	MessageID string `json:"messageId,omitempty"`
	ProjectID string `json:"projectId,omitempty"`
	FactKey   string `json:"factKey,omitempty"`
}

// latestConversationPlanState shares the actual run boundary between board and
// evidence operations. Legacy sessions retain a zero boundary; storage failures
// fail the request instead of silently exposing an unscoped historical plan.
func (h *ConversationHandler) latestConversationPlanState(id string) (bool, time.Time, error) {
	running, startedAt := false, time.Time{}
	if h.taskState != nil {
		running, startedAt = h.taskState.ConversationTaskRuntimeState(id)
	}
	if running && !startedAt.IsZero() {
		return running, startedAt, nil
	}
	boundary, err := h.db.GetConversationPlanRunStart(id)
	return running, boundary, err
}

// GetPlanTaskEvidenceSources returns up to 25 safe source labels in the task's
// project/conversation. Search is bounded; every candidate passes source read RBAC.
func (h *ConversationHandler) GetPlanTaskEvidenceSources(c *gin.Context) {
	conv, _, session, ok := h.planEvidenceTask(c)
	if !ok {
		return
	}
	kind, search := c.Query("kind"), strings.TrimSpace(c.Query("search"))
	if len(search) > 400 || (kind != "project_fact" && kind != "vulnerability") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid source search"})
		return
	}
	refs := []database.PlanTaskEvidence{}
	if kind == "project_fact" && conv.ProjectID != "" && session.Permissions["project:read"] && h.db.UserCanAccessResource(session.UserID, session.ScopeFor("project:read"), "project", conv.ProjectID) {
		access := database.ProjectFactEvidenceAccess{Project: database.RBACListAccess{UserID: session.UserID, Scope: session.ScopeFor("project:read")}}
		if session.Permissions["chat:read"] {
			access.Conversation = &database.RBACListAccess{UserID: session.UserID, Scope: session.ScopeFor("chat:read")}
		}
		if session.Permissions["vulnerability:read"] {
			access.Vulnerability = &database.RBACListAccess{UserID: session.UserID, Scope: session.ScopeFor("vulnerability:read")}
		}
		facts, err := h.db.ListProjectFactsForEvidence(conv.ProjectID, search, access)
		if err != nil {
			h.planEvidenceFailure(c, err)
			return
		}
		for _, fact := range facts {
			refs = append(refs, database.PlanTaskEvidence{Kind: kind, SourceID: fact.ID})
		}
	}
	if kind == "vulnerability" && session.Permissions["vulnerability:read"] {
		items, err := h.db.ListVulnerabilitiesForEvidence(conv.ProjectID, conv.ID, search, database.RBACListAccess{UserID: session.UserID, Scope: session.ScopeFor("vulnerability:read")})
		if err != nil {
			h.planEvidenceFailure(c, err)
			return
		}
		for _, item := range items {
			refs = append(refs, database.PlanTaskEvidence{Kind: kind, SourceID: item.ID})
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": h.visiblePlanEvidence(session, conv, refs)})
}

// planEvidenceTask authorizes the exact conversation and reads its current task.
// A partial concurrent file write returns 409, never an empty successful task.
func (h *ConversationHandler) planEvidenceTask(c *gin.Context) (*database.Conversation, *database.ConversationPlanTask, security.Session, bool) {
	session, ok := security.CurrentSession(c)
	permission := "chat:read"
	if c.Request.Method == http.MethodPost {
		permission = "chat:write"
	}
	if c.Request.Method == http.MethodDelete {
		permission = "chat:delete"
	}
	id := strings.TrimSpace(c.Param("id"))
	if !ok || !session.Permissions[permission] || !h.db.UserCanAccessResource(session.UserID, session.ScopeFor(permission), "conversation", id) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Conversation access denied"})
		return nil, nil, session, false
	}
	taskID := c.Param("taskId")
	number, err := strconv.Atoi(taskID)
	if err != nil || number < 1 || strconv.Itoa(number) != taskID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return nil, nil, session, false
	}
	conversation, err := h.db.GetConversationLite(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Conversation unavailable"})
		return nil, nil, session, false
	}
	_, startedAt, err := h.latestConversationPlanState(id)
	if err != nil {
		h.planEvidenceFailure(c, err)
		return nil, nil, session, false
	}
	snapshot, err := h.db.ReadConversationPlanTasksSince(id, startedAt)
	if err != nil {
		h.planEvidenceFailure(c, err)
		return nil, nil, session, false
	}
	for _, task := range snapshot.Tasks {
		if task.ID == taskID {
			return conversation, &task, session, true
		}
	}
	code := http.StatusNotFound
	if snapshot.Incomplete {
		code = http.StatusConflict
	}
	c.JSON(code, gin.H{"error": "Task snapshot unavailable; refresh the board"})
	return nil, nil, session, false
}

// resolvePlanEvidence rechecks each source's current permission and scope. Only
// safe identifying metadata is returned; supporting references never prove impact.
func (h *ConversationHandler) resolvePlanEvidence(session security.Session, conv *database.Conversation, ref database.PlanTaskEvidence) (planEvidenceView, bool) {
	view := planEvidenceView{PlanTaskEvidence: ref}
	allowed := func(permission, kind, id string) bool {
		return session.Permissions[permission] && h.db.UserCanAccessResource(session.UserID, session.ScopeFor(permission), kind, id)
	}
	switch ref.Kind {
	case "tool_result":
		if !allowed("chat:read", "conversation", conv.ID) {
			return view, false
		}
		detail, err := h.db.GetProcessDetailByID(ref.SourceID)
		if err != nil || detail.ConversationID != conv.ID || detail.EventType != "tool_result" {
			return view, false
		}
		var data struct {
			ToolName string `json:"toolName"`
		}
		_ = json.Unmarshal([]byte(detail.Data), &data)
		view.Label = data.ToolName
		view.MessageID = detail.MessageID
	case "project_fact":
		if conv.ProjectID == "" || !allowed("project:read", "project", conv.ProjectID) {
			return view, false
		}
		fact, err := h.db.GetProjectFact(ref.SourceID)
		if err != nil || fact.ProjectID != conv.ProjectID {
			return view, false
		}
		if fact.SourceConversationID != "" && !allowed("chat:read", "conversation", fact.SourceConversationID) {
			return view, false
		}
		if fact.RelatedVulnerabilityID != "" && !allowed("vulnerability:read", "vulnerability", fact.RelatedVulnerabilityID) {
			return view, false
		}
		view.Label = fact.Summary
		view.ProjectID = fact.ProjectID
		view.FactKey = fact.FactKey
	case "vulnerability":
		if !allowed("vulnerability:read", "vulnerability", ref.SourceID) {
			return view, false
		}
		vuln, err := h.db.GetVulnerability(ref.SourceID)
		if err != nil || (conv.ProjectID != "" && vuln.ProjectID != conv.ProjectID) || (conv.ProjectID == "" && vuln.ConversationID != conv.ID) {
			return view, false
		}
		view.Label = vuln.Title
		view.ProjectID = vuln.ProjectID
	default:
		return view, false
	}
	if len([]rune(view.Label)) > 160 {
		view.Label = string([]rune(view.Label)[:160])
	}
	return view, true
}

// GetPlanTaskEvidence lists currently readable explicit references for one task.
// Hidden or deleted sources disclose neither their labels nor their IDs/counts.
func (h *ConversationHandler) GetPlanTaskEvidence(c *gin.Context) {
	conv, task, session, ok := h.planEvidenceTask(c)
	if !ok {
		return
	}
	refs, err := h.db.ListPlanTaskEvidence(conv.ID, task.ID, task.Identity)
	if err != nil {
		h.planEvidenceFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": h.visiblePlanEvidence(session, conv, refs), "taskIdentity": task.Identity})
}

// visiblePlanEvidence shares the current-permission projection between saved
// links and search candidates, without retaining inaccessible source metadata.
func (h *ConversationHandler) visiblePlanEvidence(session security.Session, conv *database.Conversation, refs []database.PlanTaskEvidence) []planEvidenceView {
	items := []planEvidenceView{}
	for _, ref := range refs {
		if view, visible := h.resolvePlanEvidence(session, conv, ref); visible {
			items = append(items, view)
		}
	}
	return items
}

// AddPlanTaskEvidence validates a bounded request and records an explicit link.
// It requires chat write plus source read access and rejects stale task identities.
func (h *ConversationHandler) AddPlanTaskEvidence(c *gin.Context) {
	conv, task, session, ok := h.planEvidenceTask(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var req struct {
		Kind         string `json:"kind"`
		SourceID     string `json:"sourceId"`
		TaskIdentity string `json:"taskIdentity"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.SourceID) == "" || len(req.SourceID) > 128 || (req.Kind != "tool_result" && req.Kind != "project_fact" && req.Kind != "vulnerability") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid evidence reference"})
		return
	}
	if req.TaskIdentity != task.Identity {
		c.JSON(http.StatusConflict, gin.H{"error": "Task changed; refresh the board"})
		return
	}
	ref := database.PlanTaskEvidence{Kind: req.Kind, SourceID: strings.TrimSpace(req.SourceID)}
	if _, visible := h.resolvePlanEvidence(session, conv, ref); !visible {
		c.JSON(http.StatusNotFound, gin.H{"error": "Evidence source unavailable"})
		return
	}
	id, err := h.db.AddPlanTaskEvidence(conv.ID, task.ID, task.Identity, ref.Kind, ref.SourceID)
	if errors.Is(err, database.ErrPlanTaskEvidenceLimit) {
		c.JSON(http.StatusConflict, gin.H{"error": "Task evidence limit reached"})
		return
	}
	if err != nil {
		h.planEvidenceFailure(c, err)
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "conversation", "link_task_evidence", "Link task evidence", "conversation", conv.ID, map[string]interface{}{"task_id": task.ID, "reference_id": id, "kind": ref.Kind})
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// DeletePlanTaskEvidence unlinks a reference within the authorized conversation
// and task. Source evidence is retained and no source payload enters the audit log.
func (h *ConversationHandler) DeletePlanTaskEvidence(c *gin.Context) {
	conv, task, _, ok := h.planEvidenceTask(c)
	if !ok {
		return
	}
	removed, err := h.db.DeletePlanTaskEvidence(conv.ID, task.ID, task.Identity, c.Param("linkId"))
	if err != nil {
		h.planEvidenceFailure(c, err)
		return
	}
	if !removed {
		c.JSON(http.StatusNotFound, gin.H{"error": "Reference unavailable"})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "conversation", "unlink_task_evidence", "Unlink task evidence", "conversation", conv.ID, map[string]interface{}{"task_id": task.ID, "reference_id": c.Param("linkId")})
	}
	c.JSON(http.StatusOK, gin.H{"removed": true})
}

func (h *ConversationHandler) planEvidenceFailure(c *gin.Context, err error) {
	h.logger.Error("Task evidence storage failed", zap.Error(err))
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Unable to load or save task evidence"})
}
