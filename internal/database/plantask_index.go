package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
)

const (
	planIndexDirectories = 64
	planIndexBatch       = 128
	planIndexPending     = 256
	planIndexInitialWait = 200 * time.Millisecond
	planIndexBatchBytes  = 1 << 20
)

// PlanTaskPage contains a storage-paginated board and whole-board counters.
// Indexing is explicit: callers must retain their previous complete snapshot.
type PlanTaskPage struct {
	PlanTaskSnapshot
	Total, Completed, ActiveStep int
	Indexing                     bool
}

type planIndexDirectory struct {
	id, path, generation                                    string
	lastUsed, synced, stamp                                 time.Time
	ready                                                   chan struct{}
	initialized, queued, working, rescan, fallback, retired bool
	files                                                   map[string]struct{}
	err                                                     error
	revision                                                uint64
}

type planTaskIndex struct {
	db       *DB
	ctx      context.Context
	cancel   context.CancelFunc
	watch    *fsnotify.Watcher
	mu       sync.Mutex
	dirs     map[string]*planIndexDirectory
	wake     chan struct{}
	wg       sync.WaitGroup
	eventWG  sync.WaitGroup
	readTask func(string, string, os.FileInfo) (ConversationPlanTask, error)
}

type planIndexExecutor interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}

func (db *DB) initPlanTaskIndexTable() error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS plan_task_file_index (
		conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
		generation TEXT NOT NULL, task_id TEXT NOT NULL, task_number INTEGER NOT NULL,
		modified_at TEXT NOT NULL, file_size INTEGER NOT NULL, valid INTEGER NOT NULL,
		status TEXT NOT NULL, payload TEXT NOT NULL,
		PRIMARY KEY(conversation_id,generation,task_id));
		CREATE INDEX IF NOT EXISTS idx_plan_task_file_page ON plan_task_file_index
		(conversation_id,generation,valid,task_number,task_id,status,modified_at)`)
	return err
}

func (db *DB) planTaskIndexer() (*planTaskIndex, error) {
	db.planIndexMu.Lock()
	defer db.planIndexMu.Unlock()
	if db.planIndexClosed {
		return nil, fmt.Errorf("task index is closed")
	}
	if db.planIndex != nil {
		return db.planIndex, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	watch, _ := fsnotify.NewWatcher() // Failed/unsupported notifications use reconciliation.
	index := &planTaskIndex{db: db, ctx: ctx, cancel: cancel, watch: watch, dirs: map[string]*planIndexDirectory{}, wake: make(chan struct{}, 1), readTask: readPlanTaskFile}
	index.wg.Add(1)
	go index.work()
	if watch != nil {
		index.eventWG.Add(1)
		go index.events()
	}
	db.planIndex = index
	return index, nil
}

func (index *planTaskIndex) close() {
	index.cancel()
	// Windows Add/Remove wait for replies from the watcher loop. Finish those
	// operations while events are still drained, then close the watcher itself.
	index.wg.Wait()
	if index.watch != nil {
		_ = index.watch.Close()
	}
	index.eventWG.Wait()
}

func (index *planTaskIndex) notify() {
	select {
	case index.wake <- struct{}{}:
	default:
	}
}

// directory retains at most 64 directory watches and tiny pending-name sets;
// task payloads are stored in SQLite, never cached as a whole board in memory.
func (index *planTaskIndex) directory(id, path string) *planIndexDirectory {
	index.mu.Lock()
	defer index.mu.Unlock()
	state := index.dirs[id]
	if state != nil && state.path != path {
		state.retired = true
		delete(index.dirs, id)
		state = nil
	}
	if state == nil {
		if len(index.dirs) >= planIndexDirectories {
			var oldest *planIndexDirectory
			for _, candidate := range index.dirs {
				if oldest == nil || candidate.lastUsed.Before(oldest.lastUsed) {
					oldest = candidate
				}
			}
			oldest.retired = true
			delete(index.dirs, oldest.id)
		}
		state = &planIndexDirectory{id: id, path: path, ready: make(chan struct{}), rescan: true, queued: true, files: map[string]struct{}{}}
		state.fallback = index.watch == nil
		index.dirs[id] = state
		index.notify()
	}
	state.lastUsed = time.Now()
	if info, err := os.Stat(path); err == nil && state.initialized && !state.working && !state.queued && !info.ModTime().Equal(state.stamp) {
		state.rescan = true
		state.queued = true
		index.notify()
	}
	if state.err != nil && time.Since(state.synced) > time.Second {
		state.rescan = true
		state.queued = true
		index.notify()
	}
	return state
}

func planTaskFileName(name string) (string, int, bool) {
	if filepath.Ext(name) != ".json" {
		return "", 0, false
	}
	id := strings.TrimSuffix(name, ".json")
	number, err := strconv.Atoi(id)
	return id, number, err == nil && number > 0
}

func (index *planTaskIndex) events() {
	defer index.eventWG.Done()
	for {
		select {
		case event, ok := <-index.watch.Events:
			if !ok {
				return
			}
			if index.ctx.Err() != nil {
				continue
			}
			parent := filepath.Clean(filepath.Dir(event.Name))
			index.mu.Lock()
			for _, state := range index.dirs {
				if filepath.Clean(state.path) == filepath.Clean(event.Name) && event.Has(fsnotify.Remove|fsnotify.Rename) {
					state.rescan = true
					state.queued = true
					state.fallback = true
				}
				if filepath.Clean(state.path) != parent {
					continue
				}
				name := filepath.Base(event.Name)
				if _, _, valid := planTaskFileName(name); !valid {
					continue
				}
				if len(state.files) < planIndexPending {
					state.files[name] = struct{}{}
				} else {
					state.rescan = true
				}
				state.queued = true
			}
			index.mu.Unlock()
			index.notify()
		case _, ok := <-index.watch.Errors:
			if !ok {
				return
			}
			if index.ctx.Err() != nil {
				continue
			}
			index.mu.Lock()
			for _, state := range index.dirs {
				state.rescan = true
				state.queued = true
			}
			index.mu.Unlock()
			index.notify()
		}
	}
}

func (index *planTaskIndex) work() {
	defer index.wg.Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-index.ctx.Done():
			return
		case <-ticker.C:
			index.mu.Lock()
			for _, state := range index.dirs {
				interval := 5 * time.Minute
				if state.fallback {
					interval = 15 * time.Second
				}
				if time.Since(state.synced) >= interval {
					state.rescan = true
					state.queued = true
				}
			}
			index.mu.Unlock()
		case <-index.wake:
		}
		for {
			if index.ctx.Err() != nil {
				return
			}
			index.mu.Lock()
			var state *planIndexDirectory
			for _, candidate := range index.dirs {
				if candidate.queued && !candidate.working {
					state = candidate
					break
				}
			}
			if state == nil {
				index.mu.Unlock()
				break
			}
			state.working = true
			state.queued = false
			rescan := state.rescan || !state.initialized
			state.rescan = false
			files := state.files
			state.files = map[string]struct{}{}
			generation := state.generation
			index.mu.Unlock()
			var err error
			if rescan {
				generation = uuid.NewString()
				err = index.scan(state, generation)
			} else {
				for name := range files {
					if err = index.writeRow(index.db, state, generation, index.readRow(state, name)); err != nil {
						break
					}
				}
			}
			if err == nil && rescan {
				_, err = index.db.ExecContext(index.ctx, `DELETE FROM plan_task_file_index WHERE conversation_id=? AND generation<>?`, state.id, generation)
			}
			if err != nil && rescan {
				_, _ = index.db.ExecContext(index.ctx, `DELETE FROM plan_task_file_index WHERE conversation_id=? AND generation=?`, state.id, generation)
			}
			index.mu.Lock()
			state.working = false
			state.synced = time.Now()
			state.err = err
			state.revision++
			if info, statErr := os.Stat(state.path); statErr == nil {
				state.stamp = info.ModTime()
			}
			if err == nil && !state.retired {
				state.generation = generation
				if !state.initialized {
					state.initialized = true
					close(state.ready)
				}
			}
			index.mu.Unlock()
			if index.ctx.Err() != nil {
				return
			}
		}
	}
}

// scan uses bounded directory batches and a single worker. A cold large board
// never makes an HTTP request enumerate or JSON-decode the whole directory.
func (index *planTaskIndex) scan(state *planIndexDirectory, generation string) error {
	index.syncWatches()
	directory, err := os.Open(state.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer directory.Close()
	for {
		if index.ctx.Err() != nil {
			return index.ctx.Err()
		}
		index.mu.Lock()
		retired := state.retired
		index.mu.Unlock()
		if retired {
			return context.Canceled
		}
		entries, readErr := directory.ReadDir(planIndexBatch)
		batch := make([]planIndexRow, 0, planIndexBatch)
		payloadBytes := 0
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if _, _, valid := planTaskFileName(entry.Name()); !valid {
				continue
			}
			if index.ctx.Err() != nil {
				return index.ctx.Err()
			}
			row := index.readRow(state, entry.Name())
			batch = append(batch, row)
			payloadBytes += len(row.payload)
			if payloadBytes >= planIndexBatchBytes {
				if err := index.writeBatch(state, generation, batch); err != nil {
					return err
				}
				batch = batch[:0]
				payloadBytes = 0
			}
		}
		if err := index.writeBatch(state, generation, batch); err != nil {
			return err
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// syncWatches runs only on the worker, outside the state mutex. Notifications
// must continue draining while Windows waits for watch-operation replies. LRU
// removals precede additions, and directory recreation reinstalls lost watches.
func (index *planTaskIndex) syncWatches() {
	if index.watch == nil || index.ctx.Err() != nil {
		return
	}
	index.mu.Lock()
	desired := make(map[string]*planIndexDirectory, len(index.dirs))
	for _, state := range index.dirs {
		desired[state.path] = state
	}
	index.mu.Unlock()
	installed := map[string]bool{}
	for _, path := range index.watch.WatchList() {
		if _, keep := desired[path]; !keep {
			_ = index.watch.Remove(path)
		} else {
			installed[path] = true
		}
	}
	for path, state := range desired {
		if index.ctx.Err() != nil {
			return
		}
		failed := false
		if !installed[path] {
			failed = index.watch.Add(path) != nil
		}
		index.mu.Lock()
		state.fallback = failed
		index.mu.Unlock()
	}
}

type planIndexRow struct {
	id, stamp, status, payload string
	number, valid              int
	size                       int64
	missing                    bool
}

// readRow performs filesystem work before any SQLite write transaction. A slow
// disk must not hold the shared writer lock and stall unrelated application writes.
func (index *planTaskIndex) readRow(state *planIndexDirectory, name string) planIndexRow {
	id, number, valid := planTaskFileName(name)
	row := planIndexRow{id: id, number: number}
	if !valid {
		return planIndexRow{}
	}
	path := filepath.Join(state.path, name)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		row.missing = true
		return row
	}
	stamp := "9999-12-31T23:59:59.999999999Z"
	size := int64(0)
	if info != nil {
		stamp = info.ModTime().UTC().Format("2006-01-02T15:04:05.000000000Z")
		size = info.Size()
	}
	payload, status := "", ""
	complete := 0
	if err == nil && info.Mode().IsRegular() {
		task, readErr := index.readTask(path, id, info)
		if readErr == nil {
			data, _ := json.Marshal(task)
			payload = string(data)
			status = strings.ToLower(strings.TrimSpace(task.Status))
			complete = 1
		}
	}
	row.stamp, row.size, row.valid, row.status, row.payload = stamp, size, complete, status, payload
	return row
}

func (index *planTaskIndex) writeRow(executor planIndexExecutor, state *planIndexDirectory, generation string, row planIndexRow) error {
	if row.id == "" {
		return nil
	}
	if row.missing {
		_, err := executor.ExecContext(index.ctx, `DELETE FROM plan_task_file_index WHERE conversation_id=? AND generation=? AND task_id=?`, state.id, generation, row.id)
		return err
	}
	_, err := executor.ExecContext(index.ctx, `INSERT INTO plan_task_file_index(conversation_id,generation,task_id,task_number,modified_at,file_size,valid,status,payload)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(conversation_id,generation,task_id) DO UPDATE SET task_number=excluded.task_number,
		modified_at=excluded.modified_at,file_size=excluded.file_size,valid=excluded.valid,status=excluded.status,payload=excluded.payload`, state.id, generation, row.id, row.number, row.stamp, row.size, row.valid, row.status, row.payload)
	return err
}

// writeBatch holds the single SQLite writer only while persisting prepared rows.
// Failed batches roll back; a generation is visible only after a complete scan.
func (index *planTaskIndex) writeBatch(state *planIndexDirectory, generation string, rows []planIndexRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := index.db.BeginTx(index.ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, row := range rows {
		if err := index.writeRow(tx, state, generation, row); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReadConversationPlanTaskPage hydrates at most limit task payloads from SQLite.
// File notifications update the projection; initial indexing is bounded from the
// caller's perspective and cancellation ends the initial wait immediately.
func (db *DB) ReadConversationPlanTaskPage(ctx context.Context, id string, since time.Time, limit, offset int) (PlanTaskPage, error) {
	page := PlanTaskPage{PlanTaskSnapshot: PlanTaskSnapshot{Tasks: []ConversationPlanTask{}}}
	if limit < 1 || limit > 100 || offset < 0 {
		return page, fmt.Errorf("invalid task page")
	}
	base := strings.TrimSpace(db.einoPlantaskBaseDir)
	if base == "" {
		return page, nil
	}
	index, err := db.planTaskIndexer()
	if err != nil {
		return page, err
	}
	state := index.directory(id, filepath.Join(base, sanitizeConversationPathSegment(id)))
	select {
	case <-state.ready:
	case <-time.After(planIndexInitialWait):
	case <-ctx.Done():
		return page, ctx.Err()
	}
	index.mu.Lock()
	generation := state.generation
	ready := state.initialized
	page.Indexing = state.working || state.queued || !ready
	indexErr := state.err
	revision := state.revision
	index.mu.Unlock()
	if indexErr != nil {
		return page, indexErr
	}
	if !ready {
		page.Incomplete = true
		return page, nil
	}
	boundary := ""
	if !since.IsZero() {
		boundary = since.UTC().Format("2006-01-02T15:04:05.000000000Z")
	}
	where := `conversation_id=? AND generation=? AND modified_at>=? AND valid=1 AND status<>'deleted'`
	args := []interface{}{id, generation, boundary}
	if err := db.QueryRowContext(ctx, `WITH ranked AS (SELECT status,ROW_NUMBER() OVER (ORDER BY task_number,task_id) position FROM plan_task_file_index WHERE `+where+`)
		SELECT COUNT(*),COALESCE(SUM(status='completed'),0),COALESCE(MIN(CASE WHEN status='in_progress' THEN position END),MIN(CASE WHEN status<>'completed' THEN position END),COUNT(*)) FROM ranked`, args...).Scan(&page.Total, &page.Completed, &page.ActiveStep); err != nil {
		return page, err
	}
	var invalid int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plan_task_file_index WHERE conversation_id=? AND generation=? AND modified_at>=? AND valid=0`, args...).Scan(&invalid); err != nil {
		return page, err
	}
	rows, err := db.QueryContext(ctx, `SELECT task_id,payload,modified_at,file_size FROM plan_task_file_index WHERE `+where+` ORDER BY task_number,task_id LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var taskID, payload, stamp string
		var size int64
		if err := rows.Scan(&taskID, &payload, &stamp, &size); err != nil {
			return page, err
		}
		var task ConversationPlanTask
		if err := json.Unmarshal([]byte(payload), &task); err != nil {
			return page, err
		}
		page.Tasks = append(page.Tasks, task)
		// Verify only this page, so an in-flight notification cannot present a
		// newly partial/deleted visible file as a complete current snapshot.
		info, statErr := os.Lstat(filepath.Join(state.path, taskID+".json"))
		if statErr != nil || info.Size() != size || info.ModTime().UTC().Format("2006-01-02T15:04:05.000000000Z") != stamp {
			index.mu.Lock()
			if len(state.files) < planIndexPending {
				state.files[taskID+".json"] = struct{}{}
			} else {
				state.rescan = true
			}
			state.queued = true
			index.mu.Unlock()
			index.notify()
			page.Indexing = true
		}
	}
	index.mu.Lock()
	page.Indexing = page.Indexing || state.retired || state.working || state.queued || state.revision != revision
	index.mu.Unlock()
	page.Incomplete = invalid > 0 || page.Indexing
	return page, rows.Err()
}
