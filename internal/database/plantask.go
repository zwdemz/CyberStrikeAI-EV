package database

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ConversationPlanTask mirrors the public fields persisted by Eino plantask.
// Keeping the transport model here avoids coupling the HTTP layer to Eino's
// private task type.
type ConversationPlanTask struct {
	ID            string    `json:"id"`
	Subject       string    `json:"subject"`
	Description   string    `json:"description,omitempty"`
	Status        string    `json:"status"`
	Blocks        []string  `json:"blocks,omitempty"`
	BlockedBy     []string  `json:"blockedBy,omitempty"`
	ActiveForm    string    `json:"activeForm,omitempty"`
	Owner         string    `json:"owner,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt,omitempty"`
	Identity      string    `json:"identity,omitempty"`
	RuntimeStatus string    `json:"runtimeStatus,omitempty"`
}

// PlanTaskIdentity binds evidence to a conversation task's ID and subject. A reused
// ID with a different subject cannot inherit old evidence; status changes retain it.
func PlanTaskIdentity(task ConversationPlanTask) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(task.ID+"\x00"+task.Subject)))
}

// PlanTaskSnapshot reports incomplete concurrent reads so clients keep their last
// successful task cards rather than treating a partial write as task deletion.
type PlanTaskSnapshot struct {
	Tasks      []ConversationPlanTask
	Incomplete bool
}

// ListConversationPlanTasks returns the live Eino task board for one
// conversation. A missing task directory is the normal state for short or
// legacy conversations and therefore returns an empty list.
func (db *DB) ListConversationPlanTasks(conversationID string) ([]ConversationPlanTask, error) {
	return db.ListConversationPlanTasksSince(conversationID, time.Time{})
}

// ListConversationPlanTasksSince limits the board to files written during the
// current agent run. The Eino backend intentionally keeps older task files for
// model continuity, but the conversation UI must not surface those files before
// the new run has called TaskCreate.
func (db *DB) ListConversationPlanTasksSince(conversationID string, since time.Time) ([]ConversationPlanTask, error) {
	snapshot, err := db.ReadConversationPlanTasksSince(conversationID, since)
	return snapshot.Tasks, err
}

// ReadConversationPlanTasksSince reads a saved board with file timestamps and
// partial-read status. Missing directories are empty; storage failures return errors.
func (db *DB) ReadConversationPlanTasksSince(conversationID string, since time.Time) (PlanTaskSnapshot, error) {
	result := PlanTaskSnapshot{Tasks: []ConversationPlanTask{}}
	if db == nil {
		return result, nil
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return result, fmt.Errorf("conversation id is required")
	}
	base := strings.TrimSpace(db.einoPlantaskBaseDir)
	if base == "" {
		return result, nil
	}

	dir := filepath.Join(base, sanitizeConversationPathSegment(conversationID))
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read conversation plan tasks: %w", err)
	}

	type numberedTask struct {
		number int
		task   ConversationPlanTask
	}
	numbered := make([]numberedTask, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		idText := strings.TrimSuffix(entry.Name(), ".json")
		number, parseErr := strconv.Atoi(idText)
		if parseErr != nil || number < 1 {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			result.Incomplete = true
			continue
		}
		if !since.IsZero() {
			if info.ModTime().Before(since) {
				continue
			}
		}
		content, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			result.Incomplete = true
			if db.logger != nil {
				db.logger.Debug("读取 Eino 任务文件失败",
					zap.String("conversationId", conversationID),
					zap.String("file", entry.Name()),
					zap.Error(readErr))
			}
			continue
		}
		var task ConversationPlanTask
		if decodeErr := json.Unmarshal(content, &task); decodeErr != nil {
			result.Incomplete = true
			// TaskUpdate writes files concurrently with this read. A partial read
			// is transient, so skip it and let the next poll recover.
			if db.logger != nil {
				db.logger.Debug("解析 Eino 任务文件失败",
					zap.String("conversationId", conversationID),
					zap.String("file", entry.Name()),
					zap.Error(decodeErr))
			}
			continue
		}
		if strings.TrimSpace(task.ID) == "" {
			task.ID = idText
		}
		if task.ID != idText {
			result.Incomplete = true
			continue
		}
		task.UpdatedAt = info.ModTime().UTC()
		task.Identity = PlanTaskIdentity(task)
		if strings.EqualFold(strings.TrimSpace(task.Status), "deleted") {
			continue
		}
		numbered = append(numbered, numberedTask{number: number, task: task})
	}

	sort.SliceStable(numbered, func(i, j int) bool {
		return numbered[i].number < numbered[j].number
	})
	tasks := make([]ConversationPlanTask, 0, len(numbered))
	for _, item := range numbered {
		tasks = append(tasks, item.task)
	}
	result.Tasks = tasks
	return result, nil
}
