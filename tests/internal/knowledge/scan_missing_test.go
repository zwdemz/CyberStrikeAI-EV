package knowledge_test

import (
	"cyberstrike-ai/internal/knowledge"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"testing"
)

func TestScanUnchangedMissingVectors(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE knowledge_base_items (id TEXT PRIMARY KEY,category TEXT,title TEXT,file_path TEXT,content TEXT,created_at DATETIME,updated_at DATETIME); CREATE TABLE knowledge_embeddings(item_id TEXT);`)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := filepath.Join(root, "sample")
	if err = os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "note.md"), []byte("fixture body"), 0600); err != nil {
		t.Fatal(err)
	}
	manager := knowledge.NewManager(db, root, zap.NewNop())
	first, err := manager.ScanKnowledgeBase()
	if err != nil || len(first) != 1 {
		t.Fatalf("first: %v %v", first, err)
	}
	again, err := manager.ScanKnowledgeBase()
	if err != nil || len(again) != 1 || again[0] != first[0] {
		t.Fatalf("missing retry: %v %v", again, err)
	}
	if _, err = db.Exec("INSERT INTO knowledge_embeddings(item_id) VALUES (?)", first[0]); err != nil {
		t.Fatal(err)
	}
	complete, err := manager.ScanKnowledgeBase()
	if err != nil || len(complete) != 0 {
		t.Fatalf("complete requeued: %v %v", complete, err)
	}
	if err = os.WriteFile(filepath.Join(dir, "note.md"), []byte("updated fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	updated, err := manager.ScanKnowledgeBase()
	if err != nil || len(updated) != 1 {
		t.Fatalf("updated: %v %v", updated, err)
	}
}
