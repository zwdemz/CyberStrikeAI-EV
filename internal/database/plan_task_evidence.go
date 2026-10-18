package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PlanTaskEvidence stores an explicit supporting reference, never a verification
// verdict or raw evidence payload. Source access is rechecked by the HTTP layer.
type PlanTaskEvidence struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	SourceID  string    `json:"sourceId"`
	CreatedAt time.Time `json:"createdAt"`
}

// ErrPlanTaskEvidenceLimit bounds the number of explicit references per task.
var ErrPlanTaskEvidenceLimit = errors.New("task evidence limit reached")

func (db *DB) initPlanTaskEvidenceTable() error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS plan_task_evidence (
		id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL,
		task_id TEXT NOT NULL, task_identity TEXT NOT NULL,
		kind TEXT NOT NULL CHECK(kind IN ('tool_result','project_fact','vulnerability')),
		source_id TEXT NOT NULL, created_at TEXT NOT NULL,
		UNIQUE(conversation_id,task_id,task_identity,kind,source_id),
		FOREIGN KEY(conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
	)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS conversation_plan_runs (
		conversation_id TEXT PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
		run_id TEXT NOT NULL, started_at TEXT NOT NULL
	)`)
	return err
}

// RecordConversationPlanRun persists the authoritative task-manager boundary
// before Agent execution. Monotonic updates prevent an older run replacing it.
func (db *DB) RecordConversationPlanRun(conversationID, runID string, startedAt time.Time) error {
	if conversationID == "" || runID == "" || startedAt.IsZero() {
		return fmt.Errorf("invalid plan run boundary")
	}
	_, err := db.Exec(`INSERT INTO conversation_plan_runs(conversation_id,run_id,started_at) VALUES(?,?,?)
		ON CONFLICT(conversation_id) DO UPDATE SET run_id=excluded.run_id,started_at=excluded.started_at
		WHERE excluded.started_at>=conversation_plan_runs.started_at`, conversationID, runID, startedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"))
	return err
}

// GetConversationPlanRunStart recovers the last recorded boundary after stop or
// restart. Legacy conversations without a boundary return zero, never a guess.
func (db *DB) GetConversationPlanRunStart(conversationID string) (time.Time, error) {
	var value string
	err := db.QueryRow(`SELECT started_at FROM conversation_plan_runs WHERE conversation_id=?`, conversationID).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, value)
}

// ListPlanTaskEvidence returns at most 20 references for the exact task identity.
// It does not resolve source labels or grant permission to the source resource.
func (db *DB) ListPlanTaskEvidence(conversationID, taskID, identity string) ([]PlanTaskEvidence, error) {
	rows, err := db.Query(`SELECT id,kind,source_id,created_at FROM plan_task_evidence
		WHERE conversation_id=? AND task_id=? AND task_identity=? ORDER BY created_at,id LIMIT 20`, conversationID, taskID, identity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PlanTaskEvidence{}
	for rows.Next() {
		var item PlanTaskEvidence
		var timestamp string
		if err := rows.Scan(&item.ID, &item.Kind, &item.SourceID, &timestamp); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339Nano, timestamp)
		items = append(items, item)
	}
	return items, rows.Err()
}

// AddPlanTaskEvidence atomically deduplicates a validated reference and enforces
// the per-task limit. The SQLite immediate transaction serializes concurrent adds.
func (db *DB) AddPlanTaskEvidence(conversationID, taskID, identity, kind, sourceID string) (string, error) {
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// Polymorphic sources cannot use one foreign key. Reclaim only references
	// whose source no longer exists, never a source hidden by the caller's RBAC.
	if _, err = tx.Exec(`DELETE FROM plan_task_evidence
		WHERE conversation_id=? AND task_id=? AND task_identity=? AND (
			(kind='tool_result' AND NOT EXISTS (SELECT 1 FROM process_details WHERE id=plan_task_evidence.source_id)) OR
			(kind='project_fact' AND NOT EXISTS (SELECT 1 FROM project_facts WHERE id=plan_task_evidence.source_id)) OR
			(kind='vulnerability' AND NOT EXISTS (SELECT 1 FROM vulnerabilities WHERE id=plan_task_evidence.source_id))
		)`, conversationID, taskID, identity); err != nil {
		return "", err
	}
	var existing string
	err = tx.QueryRow(`SELECT id FROM plan_task_evidence WHERE conversation_id=? AND task_id=? AND task_identity=? AND kind=? AND source_id=?`, conversationID, taskID, identity, kind, sourceID).Scan(&existing)
	if err == nil {
		return existing, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var total int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM plan_task_evidence WHERE conversation_id=? AND task_id=? AND task_identity=?`, conversationID, taskID, identity).Scan(&total); err != nil {
		return "", err
	}
	if total >= 20 {
		return "", ErrPlanTaskEvidenceLimit
	}
	id := uuid.NewString()
	_, err = tx.Exec(`INSERT INTO plan_task_evidence(id,conversation_id,task_id,task_identity,kind,source_id,created_at) VALUES(?,?,?,?,?,?,?)`, id, conversationID, taskID, identity, kind, sourceID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return "", fmt.Errorf("save task reference: %w", err)
	}
	return id, tx.Commit()
}

// DeletePlanTaskEvidence removes only the specified task's reference. It never
// deletes source records; nonexistent references report false without mutation.
func (db *DB) DeletePlanTaskEvidence(conversationID, taskID, identity, id string) (bool, error) {
	result, err := db.Exec(`DELETE FROM plan_task_evidence WHERE conversation_id=? AND task_id=? AND task_identity=? AND id=?`, conversationID, taskID, identity, id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}
