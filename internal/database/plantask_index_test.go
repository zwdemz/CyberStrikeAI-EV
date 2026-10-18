package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func planIndexFixture(t *testing.T) (*DB, string) {
	t.Helper()
	db, err := NewDB(filepath.Join(t.TempDir(), "index.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base := t.TempDir()
	db.SetEinoConversationDirs(base, "", "", "")
	if _, err := db.Exec(`INSERT INTO conversations(id,title,created_at,updated_at) VALUES('board','fixture',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "board")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return db, dir
}

func writeIndexTask(t *testing.T, dir, id, status string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(fmt.Sprintf(`{"id":%q,"subject":"fixture","status":%q}`, id, status)), 0600); err != nil {
		t.Fatal(err)
	}
}

func awaitIndexPage(t *testing.T, db *DB, since time.Time, limit, offset int, accept func(PlanTaskPage) bool) PlanTaskPage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var page PlanTaskPage
	var err error
	for ctx.Err() == nil {
		page, err = db.ReadConversationPlanTaskPage(ctx, "board", since, limit, offset)
		if err == nil && !page.Indexing && accept(page) {
			return page
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("index did not converge: page=%+v err=%v", page, err)
	return page
}

func TestPlanTaskIndexPaginationAndNotifications(t *testing.T) {
	db, dir := planIndexFixture(t)
	for number := 1; number <= 300; number++ {
		status := "pending"
		if number <= 10 {
			status = "completed"
		}
		if number == 250 {
			status = "in_progress"
		}
		writeIndexTask(t, dir, fmt.Sprint(number), status)
	}
	index, err := db.planTaskIndexer()
	if err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int64
	index.readTask = func(path, id string, info os.FileInfo) (ConversationPlanTask, error) {
		reads.Add(1)
		return readPlanTaskFile(path, id, info)
	}
	page := awaitIndexPage(t, db, time.Time{}, 20, 240, func(p PlanTaskPage) bool { return p.Total == 300 })
	if page.Incomplete || page.Completed != 10 || page.ActiveStep != 250 || len(page.Tasks) != 20 || page.Tasks[0].ID != "241" {
		t.Fatalf("bad pagination: %+v", page)
	}
	before := reads.Load()
	page = awaitIndexPage(t, db, time.Time{}, 20, 0, func(p PlanTaskPage) bool { return p.Total == 300 })
	if reads.Load() != before {
		t.Fatalf("warm paging reread task files: %d -> %d", before, reads.Load())
	}
	writeIndexTask(t, dir, "250", "completed")
	awaitIndexPage(t, db, time.Time{}, 20, 240, func(p PlanTaskPage) bool { return p.Completed == 11 && p.ActiveStep == 11 })
	if err := os.Remove(filepath.Join(dir, "1.json")); err != nil {
		t.Fatal(err)
	}
	writeIndexTask(t, dir, "301", "pending")
	awaitIndexPage(t, db, time.Time{}, 20, 280, func(p PlanTaskPage) bool {
		return p.Total == 300 && p.Completed == 10 && p.Tasks[len(p.Tasks)-1].ID == "301"
	})
	if err := os.WriteFile(filepath.Join(dir, "301.json"), []byte(`{"id":`), 0600); err != nil {
		t.Fatal(err)
	}
	awaitIndexPage(t, db, time.Time{}, 20, 280, func(p PlanTaskPage) bool { return p.Incomplete && p.Total == 299 })
	writeIndexTask(t, dir, "301", "pending")
	awaitIndexPage(t, db, time.Time{}, 20, 280, func(p PlanTaskPage) bool { return !p.Incomplete && p.Total == 300 })
	writeIndexTask(t, dir, "301", "deleted")
	awaitIndexPage(t, db, time.Time{}, 20, 280, func(p PlanTaskPage) bool { return !p.Incomplete && p.Total == 299 })
	boundary := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "2.json"), boundary, boundary); err != nil {
		t.Fatal(err)
	}
	awaitIndexPage(t, db, boundary, 20, 0, func(p PlanTaskPage) bool { return p.Total == 1 && p.Tasks[0].ID == "2" })
}

func TestPlanTaskIndexColdWaitCancellationDoesNotHoldWriter(t *testing.T) {
	db, dir := planIndexFixture(t)
	writeIndexTask(t, dir, "1", "pending")
	index, err := db.planTaskIndexer()
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	index.readTask = func(path, id string, info os.FileInfo) (ConversationPlanTask, error) {
		close(entered)
		<-release
		return readPlanTaskFile(path, id, info)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := db.ReadConversationPlanTaskPage(ctx, "board", time.Time{}, 20, 0); result <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	cancel()
	select {
	case err := <-result:
		if err != context.Canceled {
			t.Fatalf("cancel=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request blocked behind indexing")
	}
	page, err := db.ReadConversationPlanTaskPage(context.Background(), "board", time.Time{}, 20, 0)
	if err != nil || !page.Indexing || !page.Incomplete {
		t.Fatalf("cold index status=%+v err=%v", page, err)
	}
	writerCtx, writerCancel := context.WithTimeout(context.Background(), time.Second)
	defer writerCancel()
	if _, err := db.ExecContext(writerCtx, `UPDATE conversations SET title='unblocked' WHERE id='board'`); err != nil {
		t.Fatalf("filesystem read held SQLite writer: %v", err)
	}
	once.Do(func() { close(release) })
	awaitIndexPage(t, db, time.Time{}, 20, 0, func(p PlanTaskPage) bool { return p.Total == 1 })
}

func TestPlanTaskIndexRecreatesDirectoryAndBoundsWatches(t *testing.T) {
	db, dir := planIndexFixture(t)
	writeIndexTask(t, dir, "1", "pending")
	awaitIndexPage(t, db, time.Time{}, 20, 0, func(p PlanTaskPage) bool { return p.Total == 1 })
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	awaitIndexPage(t, db, time.Time{}, 20, 0, func(p PlanTaskPage) bool { return p.Total == 0 })
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	writeIndexTask(t, dir, "2", "pending")
	awaitIndexPage(t, db, time.Time{}, 20, 0, func(p PlanTaskPage) bool { return p.Total == 1 && p.Tasks[0].ID == "2" })
	writeIndexTask(t, dir, "2", "completed")
	awaitIndexPage(t, db, time.Time{}, 20, 0, func(p PlanTaskPage) bool { return p.Completed == 1 })
	index, err := db.planTaskIndexer()
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 70; n++ {
		index.directory(fmt.Sprint(n), filepath.Join(filepath.Dir(dir), fmt.Sprint(n)))
	}
	index.mu.Lock()
	count := len(index.dirs)
	index.mu.Unlock()
	if count != planIndexDirectories {
		t.Fatalf("watch state unbounded: %d", count)
	}
	if index.watch != nil && len(index.watch.WatchList()) > planIndexDirectories {
		t.Fatal("watch resources unbounded")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.planTaskIndexer(); err == nil {
		t.Fatal("closed DB restarted index")
	}
}

func TestPlanTaskDirectReadValidatesIdentityAndBoundary(t *testing.T) {
	db, dir := planIndexFixture(t)
	writeIndexTask(t, dir, "1", "pending")
	// A damaged unrelated file must not affect single-task evidence lookup.
	if err := os.WriteFile(filepath.Join(dir, "2.json"), []byte(`{`), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := db.ReadConversationPlanTaskSince("board", "1", time.Time{})
	if err != nil || page.Incomplete || len(page.Tasks) != 1 {
		t.Fatalf("direct lookup=%+v %v", page, err)
	}
	page, err = db.ReadConversationPlanTaskSince("board", "1", time.Now().Add(time.Hour))
	if err != nil || len(page.Tasks) != 0 {
		t.Fatal("boundary ignored")
	}
	for _, id := range []string{"../1", "01", "0", "-1"} {
		if _, err := db.ReadConversationPlanTaskSince("board", id, time.Time{}); err == nil {
			t.Fatalf("invalid id accepted: %s", id)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "1.json"), []byte(`{"id":"99"}`), 0600); err != nil {
		t.Fatal(err)
	}
	page, err = db.ReadConversationPlanTaskSince("board", "1", time.Time{})
	if err != nil || !page.Incomplete || len(page.Tasks) != 0 {
		t.Fatal("mismatched file identity accepted")
	}
}

func TestPlanTaskIndexRebuildsAfterRestart(t *testing.T) {
	db, dir := planIndexFixture(t)
	writeIndexTask(t, dir, "1", "pending")
	awaitIndexPage(t, db, time.Time{}, 20, 0, func(p PlanTaskPage) bool { return p.Total == 1 })
	var sequence int
	var name, path string
	if err := db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	writeIndexTask(t, dir, "1", "completed")
	reopened, err := NewDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopened.SetEinoConversationDirs(filepath.Dir(dir), "", "", "")
	page := awaitIndexPage(t, reopened, time.Time{}, 20, 0, func(p PlanTaskPage) bool { return p.Completed == 1 })
	if len(page.Tasks) != 1 || page.Tasks[0].Status != "completed" {
		t.Fatal("restart reused stale projection")
	}
	var generations int
	if err := reopened.QueryRow(`SELECT COUNT(DISTINCT generation) FROM plan_task_file_index WHERE conversation_id='board'`).Scan(&generations); err != nil {
		t.Fatal(err)
	}
	// A complete published generation must have removed obsolete payloads.
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	verify, err := NewDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer verify.Close()
	if err := verify.QueryRow(`SELECT COUNT(DISTINCT generation) FROM plan_task_file_index WHERE conversation_id='board'`).Scan(&generations); err != nil {
		t.Fatal(err)
	}
	if generations != 1 {
		t.Fatalf("abandoned projection retained: %d", generations)
	}
}
