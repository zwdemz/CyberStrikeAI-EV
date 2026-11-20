package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

// TestConcurrentAssetUpserts exercises the Deep tool's many independent writes
// against one real WAL file and verifies every committed asset is present.
func TestConcurrentAssetUpserts(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "assets.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const writers = 48
	start := make(chan struct{})
	errorsByWorker := make(chan error, writers)
	var workers sync.WaitGroup
	for index := 0; index < writers; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			result, err := db.UpsertAssetsContext(context.Background(), []*Asset{{IP: fmt.Sprintf("198.51.100.%d", index+1), Port: 443, Protocol: "https"}}, "")
			if err != nil || result.Created != 1 {
				errorsByWorker <- fmt.Errorf("worker %d: result=%+v err=%v", index, result, err)
			}
		}(index)
	}
	close(start)
	workers.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		t.Error(err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM assets WHERE ip LIKE '198.51.100.%'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != writers {
		t.Fatalf("persisted %d of %d concurrent assets", count, writers)
	}
}

// TestAssetUpsertWaitsForWriter ensures BEGIN IMMEDIATE waits for another WAL
// writer instead of upgrading a stale read transaction and failing immediately.
func TestAssetUpsertWaitsForWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assets.db")
	db, err := NewDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	blocker, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_txlock=immediate&_busy_timeout=1000")
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	tx, err := blocker.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	type outcome struct {
		result AssetImportResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := db.UpsertAssetsContext(context.Background(), []*Asset{{IP: "192.0.2.50", Port: 443, Protocol: "https"}}, "")
		done <- outcome{result, err}
	}()
	select {
	case early := <-done:
		t.Fatalf("write did not wait for blocker: %+v", early)
	case <-time.After(120 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case finished := <-done:
		if finished.err != nil || finished.result.Created != 1 {
			t.Fatalf("waiting writer failed: %+v", finished)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting writer did not finish after lock release")
	}
}

// TestAssetUpsertBusyRetry verifies the bounded fallback and cancellation on a
// private connection with a deliberately short busy timeout.
func TestAssetUpsertBusyRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assets.db")
	db, err := NewDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	shortPool, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=1&_busy_timeout=20&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	defer shortPool.Close()
	shortPool.SetMaxOpenConns(1)
	short := &DB{DB: shortPool, logger: zap.NewNop()}
	blocker, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_txlock=immediate&_busy_timeout=1000")
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	tx, err := blocker.Begin()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Millisecond)
	defer cancel()
	if _, err := short.UpsertAssetsContext(ctx, []*Asset{{IP: "192.0.2.60", Port: 443, Protocol: "https"}}, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled retry returned %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = blocker.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	released := make(chan error, 1)
	time.AfterFunc(130*time.Millisecond, func() { released <- tx.Commit() })
	candidate := &Asset{IP: "192.0.2.61", Port: 443, Protocol: "https"}
	result, err := short.UpsertAssetsContext(context.Background(), []*Asset{candidate}, "")
	if releaseErr := <-released; releaseErr != nil {
		t.Fatal(releaseErr)
	}
	if err != nil || result.Created != 1 {
		t.Fatalf("bounded retry failed: result=%+v err=%v", result, err)
	}
	asset, err := short.GetAsset(candidate.ID, RBACListAccess{Scope: RBACScopeAll})
	if err != nil {
		t.Fatal(err)
	}
	asset.Title = "updated after lock"
	tx, err = blocker.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	released = make(chan error, 1)
	time.AfterFunc(130*time.Millisecond, func() { released <- tx.Commit() })
	err = short.UpdateAssetContext(context.Background(), asset.ID, asset, RBACListAccess{Scope: RBACScopeAll})
	if releaseErr := <-released; releaseErr != nil {
		t.Fatal(releaseErr)
	}
	if err != nil {
		t.Fatalf("asset update did not recover from transient writer: %v", err)
	}
}
