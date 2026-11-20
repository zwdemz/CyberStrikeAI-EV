package database_test

import (
	"path/filepath"
	"testing"

	"cyberstrike-ai/internal/database"
	"go.uber.org/zap"
)

func TestStartupRecoveryIsAtomicAndIdempotent(t *testing.T) {
	for _, injectFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "rollback"}[injectFailure], func(t *testing.T) {
			db, err := database.NewDB(filepath.Join(t.TempDir(), "tasks.db"), zap.NewNop())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			conversation, err := db.CreateConversation("recovery", database.ConversationCreateMeta{})
			if err != nil {
				t.Fatal(err)
			}
			placeholder, err := db.AddMessage(conversation.ID, "assistant", "处理中...", nil)
			if err != nil {
				t.Fatal(err)
			}
			normal, err := db.AddMessage(conversation.ID, "assistant", "completed evidence", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.CreateBatchQueue("queue", "recovery", "", "eino_single", "manual", "", nil, "", 1,
				[]map[string]interface{}{{"id": "running", "message": "first"}, {"id": "pending", "message": "second"}}); err != nil {
				t.Fatal(err)
			}
			for _, statement := range []string{
				"UPDATE batch_task_queues SET status='running' WHERE id='queue'",
				"UPDATE batch_tasks SET status='running' WHERE id='running'",
			} {
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			if injectFailure {
				if _, err := db.Exec(`CREATE TRIGGER fail_recovery BEFORE UPDATE ON messages BEGIN SELECT RAISE(ABORT, 'fixture'); END`); err != nil {
					t.Fatal(err)
				}
			}
			summary, err := db.RecoverInterruptedTasks()
			if injectFailure {
				if err == nil || summary != (database.TaskRecoverySummary{}) {
					t.Fatalf("expected atomic failure: %+v %v", summary, err)
				}
				var status string
				if err := db.QueryRow("SELECT status FROM batch_tasks WHERE id='running'").Scan(&status); err != nil || status != "running" {
					t.Fatalf("rollback failed: %s %v", status, err)
				}
				return
			}
			if err != nil || summary.Tasks != 1 || summary.Queues != 1 || summary.Messages != 1 {
				t.Fatalf("summary=%+v err=%v", summary, err)
			}
			for id, want := range map[string]string{"running": "failed", "pending": "pending"} {
				var status string
				if err := db.QueryRow("SELECT status FROM batch_tasks WHERE id=?", id).Scan(&status); err != nil || status != want {
					t.Fatalf("task %s: %s %v", id, status, err)
				}
			}
			var status, content string
			if err := db.QueryRow("SELECT status FROM batch_task_queues WHERE id='queue'").Scan(&status); err != nil || status != "paused" {
				t.Fatalf("queue: %s %v", status, err)
			}
			if err := db.QueryRow("SELECT content FROM messages WHERE id=?", normal.ID).Scan(&content); err != nil || content != "completed evidence" {
				t.Fatal("modified completed message", err)
			}
			again, err := db.RecoverInterruptedTasks()
			if err != nil || again != (database.TaskRecoverySummary{}) {
				t.Fatalf("not idempotent: %+v %v", again, err)
			}
			var events int
			if err := db.QueryRow("SELECT count(*) FROM process_details WHERE message_id=?", placeholder.ID).Scan(&events); err != nil || events != 1 {
				t.Fatalf("events=%d %v", events, err)
			}
		})
	}
}
