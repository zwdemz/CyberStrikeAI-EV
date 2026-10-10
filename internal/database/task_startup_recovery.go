package database

import (
	"fmt"
	"time"
)

// TaskRecoverySummary counts committed startup repairs, not attempted updates.
type TaskRecoverySummary struct{ Tasks, Queues, Messages int64 }

// RecoverInterruptedTasks closes process-owned task state before any workers
// start. The database must have one application owner. Running children become
// failed; unfinished queues pause without replaying work. Completed messages and
// pending children are preserved. All updates and error events commit together;
// on any database error they roll back and a zero summary is returned.
func (db *DB) RecoverInterruptedTasks() (TaskRecoverySummary, error) {
	var summary TaskRecoverySummary
	tx, err := db.Begin()
	if err != nil {
		return summary, err
	}
	defer tx.Rollback()
	now := time.Now()
	reason := "Service restart interrupted this task; review its results before resuming."
	steps := []struct {
		query string
		args  []any
		count *int64
	}{
		{`UPDATE batch_task_queues SET status = CASE
		 WHEN EXISTS (SELECT 1 FROM batch_tasks WHERE queue_id = batch_task_queues.id AND status = 'pending') THEN 'paused'
		 ELSE 'completed' END,
		 completed_at = CASE WHEN EXISTS (SELECT 1 FROM batch_tasks WHERE queue_id = batch_task_queues.id AND status = 'pending') THEN NULL ELSE COALESCE(completed_at, ?) END,
		 last_run_error = ? WHERE status IN ('running', 'pausing')`, []any{now, reason}, &summary.Queues},
		{`UPDATE batch_tasks SET status = 'failed', completed_at = COALESCE(completed_at, ?),
		 error = CASE WHEN COALESCE(error, '') = '' THEN ? ELSE error END WHERE status = 'running'`, []any{now, reason}, &summary.Tasks},
		{`INSERT INTO process_details (id, message_id, conversation_id, event_type, message, data, created_at)
		 SELECT lower(hex(randomblob(16))), id, conversation_id, 'error', ?, '', ? FROM messages
		 WHERE role = 'assistant' AND TRIM(content) = '处理中...'`, []any{reason, now}, nil},
		{`UPDATE messages SET content = ?, updated_at = ? WHERE role = 'assistant' AND TRIM(content) = '处理中...'`, []any{reason, now}, &summary.Messages},
	}
	for _, step := range steps {
		result, err := tx.Exec(step.query, step.args...)
		if err != nil {
			return TaskRecoverySummary{}, fmt.Errorf("recover interrupted tasks: %w", err)
		}
		if step.count != nil {
			*step.count, err = result.RowsAffected()
			if err != nil {
				return TaskRecoverySummary{}, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return TaskRecoverySummary{}, err
	}
	return summary, nil
}
