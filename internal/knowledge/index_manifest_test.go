package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"cyberstrike-ai/internal/config"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

type manifestEmbedder struct {
	calls  int
	failAt int
	before func()
}

func (e *manifestEmbedder) EmbedStrings(ctx context.Context, texts []string, _ ...embedding.Option) ([][]float64, error) {
	e.calls++
	if e.before != nil {
		e.before()
		e.before = nil
	}
	if e.calls == e.failAt {
		return nil, errors.New("fixture embedding failure")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([][]float64, len(texts))
	for i := range out {
		out[i] = []float64{1, 0.5}
	}
	return out, nil
}

func manifestFixture(t *testing.T) (*sql.DB, indexAttempt) {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "knowledge.db")+"?_busy_timeout=5000&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE knowledge_base_items(id TEXT PRIMARY KEY,content TEXT NOT NULL, category TEXT NOT NULL,title TEXT NOT NULL,file_path TEXT NOT NULL,updated_at TEXT);
 CREATE TABLE knowledge_embeddings(id TEXT PRIMARY KEY,item_id TEXT,chunk_index INTEGER,chunk_text TEXT,embedding TEXT,sub_indexes TEXT,embedding_model TEXT,embedding_dim INTEGER,created_at TEXT);
 CREATE INDEX embeddings_item ON knowledge_embeddings(item_id);
 INSERT INTO knowledge_base_items VALUES('item','body','category','title','','2026-01-01');
 INSERT INTO knowledge_embeddings VALUES('old','item',0,'old','[1,0]','','model',2,'');`)
	if err != nil {
		t.Fatal(err)
	}
	if err = EnsureKnowledgeEmbeddingsSchema(db); err != nil {
		t.Fatal(err)
	}
	return db, indexAttempt{ItemID: "item", Content: "body", Category: "category", Title: "title", SourceHash: "source", ConfigHash: "config"}
}

func manifestDocs() []*schema.Document {
	return []*schema.Document{
		{Content: "first", MetaData: map[string]any{metaKBItemID: "item", metaKBChunkIndex: 0}},
		{Content: "second", MetaData: map[string]any{metaKBItemID: "item", metaKBChunkIndex: 1}},
	}
}

func TestIndexReplacementPreservesPreviousOnFailure(t *testing.T) {
	for _, kind := range []string{"embedding", "insert", "source", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			db, attempt := manifestFixture(t)
			mock := &manifestEmbedder{}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), indexAttemptKey{}, attempt))
			defer cancel()
			switch kind {
			case "embedding":
				mock.failAt = 2
			case "insert":
				_, err := db.Exec(`CREATE TRIGGER fail_insert BEFORE INSERT ON knowledge_embeddings WHEN NEW.chunk_index=1 BEGIN SELECT RAISE(ABORT,'fixture insert failure'); END;`)
				if err != nil {
					t.Fatal(err)
				}
			case "source":
				mock.before = func() {
					if _, err := db.Exec(`UPDATE knowledge_base_items SET content='changed' WHERE id='item'`); err != nil {
						t.Fatal(err)
					}
				}
			case "cancel":
				mock.before = cancel
			}
			_, err := NewSQLiteIndexer(db, 1, "model").Store(ctx, manifestDocs(), indexer.WithEmbedding(mock))
			if err == nil {
				t.Fatal("expected failure")
			}
			var count int
			if err = db.QueryRow(`SELECT count(*) FROM knowledge_embeddings WHERE id='old'`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("previous index lost: %d %v", count, err)
			}
			if err = db.QueryRow(`SELECT count(*) FROM knowledge_embeddings`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("partial replacement: %d %v", count, err)
			}
		})
	}
}

func TestIndexManifestDetectsIncompleteAndChangedState(t *testing.T) {
	for _, mutation := range []string{
		`DELETE FROM knowledge_embeddings WHERE chunk_index=1`,
		`UPDATE knowledge_embeddings SET chunk_index=0`,
		`UPDATE knowledge_embeddings SET chunk_index=9 WHERE chunk_index=1`,
		`UPDATE knowledge_embeddings SET embedding_model='other' WHERE chunk_index=1`,
		`UPDATE knowledge_embeddings SET embedding_dim=0 WHERE chunk_index=1`,
		`UPDATE knowledge_base_items SET content='changed'`,
		`DELETE FROM knowledge_base_items`,
	} {
		t.Run(mutation, func(t *testing.T) {
			db, a := manifestFixture(t)
			ctx := context.WithValue(context.Background(), indexAttemptKey{}, a)
			store := NewSQLiteIndexer(db, 1, "model")
			for i := 0; i < 2; i++ {
				if _, err := store.Store(ctx, manifestDocs(), indexer.WithEmbedding(&manifestEmbedder{})); err != nil {
					t.Fatal(err)
				}
			}
			idx := &Indexer{db: db}
			complete, err := idx.indexStateComplete(ctx, a)
			if err != nil || !complete {
				t.Fatalf("replacement not complete: %v %v", complete, err)
			}
			changed := a
			changed.ConfigHash = "changed"
			complete, err = idx.indexStateComplete(ctx, changed)
			if err != nil || complete {
				t.Fatal("config change ignored")
			}
			if _, err = db.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			complete, err = idx.indexStateComplete(ctx, a)
			if err != nil || complete {
				t.Fatalf("corrupt/source-changed index skipped: %v %v", complete, err)
			}
		})
	}
}

func TestIndexMissingRepairsLegacyAndSkipsComplete(t *testing.T) {
	db, _ := manifestFixture(t)
	mock := &manifestEmbedder{}
	cfg := &config.KnowledgeConfig{}
	cfg.Indexing.ChunkStrategy = "recursive"
	cfg.Indexing.ChunkSize = 512
	embed := &Embedder{config: cfg, eino: mock, maxRetries: 1, logger: zap.NewNop()}
	idx, err := NewIndexer(context.Background(), db, embed, zap.NewNop(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	run := func() {
		t.Helper()
		if err := idx.IndexMissing(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	run()
	if mock.calls == 0 {
		t.Fatal("legacy rows skipped")
	}
	before := mock.calls
	run()
	if mock.calls != before {
		t.Fatal("complete item embedded again")
	}
	if _, err = db.Exec(`DELETE FROM knowledge_embeddings`); err != nil {
		t.Fatal(err)
	}
	run()
	if mock.calls == before {
		t.Fatal("missing chunk not repaired")
	}
	before = mock.calls
	cfg.Embedding.Model = "different-model"
	if err = idx.RecompileIndexChain(context.Background()); err != nil {
		t.Fatal(err)
	}
	run()
	if mock.calls == before {
		t.Fatal("changed model not rebuilt")
	}
	before = mock.calls
	cfg.Embedding.BaseURL = "https://embedding.example.invalid/v1"
	run()
	if mock.calls == before {
		t.Fatal("changed endpoint not rebuilt")
	}
	if _, err = db.Exec(`UPDATE knowledge_base_items SET content=''`); err != nil {
		t.Fatal(err)
	}
	run()
	before = mock.calls
	run()
	if mock.calls != before {
		t.Fatal("empty source repeatedly embedded")
	}
	var count int
	if err = db.QueryRow(`SELECT chunk_count FROM knowledge_index_state WHERE item_id='item'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("empty index manifest: %d %v", count, err)
	}
}

func TestIndexResumeReportsFailureAndDetectsFileAndChunkChanges(t *testing.T) {
	db, _ := manifestFixture(t)
	mock := &manifestEmbedder{failAt: 1}
	cfg := &config.KnowledgeConfig{}
	cfg.Indexing.ChunkStrategy = "recursive"
	cfg.Indexing.PreferSourceFile = true
	embed := &Embedder{config: cfg, eino: mock, maxRetries: 1, logger: zap.NewNop()}
	idx, err := NewIndexer(context.Background(), db, embed, zap.NewNop(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = idx.IndexMissing(context.Background()); err == nil {
		t.Fatal("failed batch reported success")
	}
	_, total, current, failed, _, _, _ := idx.GetRebuildStatus()
	if total != 1 || current != 1 || failed != 1 {
		t.Fatalf("failure progress: %d %d %d", total, current, failed)
	}
	source := filepath.Join(t.TempDir(), "source.txt")
	if err = os.WriteFile(source, []byte("initial source"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE knowledge_base_items SET file_path=?`, source); err != nil {
		t.Fatal(err)
	}
	run := func() {
		t.Helper()
		if err := idx.IndexMissing(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	run()
	before := mock.calls
	run()
	if mock.calls != before {
		t.Fatal("unchanged source file re-embedded")
	}
	if err = os.WriteFile(source, []byte("changed source"), 0600); err != nil {
		t.Fatal(err)
	}
	run()
	if mock.calls == before {
		t.Fatal("changed source file ignored")
	}
	before = mock.calls
	cfg.Indexing.ChunkSize = 128
	if err = idx.RecompileIndexChain(context.Background()); err != nil {
		t.Fatal(err)
	}
	run()
	if mock.calls == before || idx.chunkSize != 128 {
		t.Fatal("chunk config was not applied")
	}
}

func TestKnowledgeEditRetainsVectorsUntilReplacementSucceeds(t *testing.T) {
	db, a := manifestFixture(t)
	base := t.TempDir()
	source := filepath.Join(base, "category", "title.md")
	if err := os.MkdirAll(filepath.Dir(source), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("body"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE knowledge_base_items ADD COLUMN created_at TEXT NOT NULL DEFAULT '2026-01-01 00:00:00'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE knowledge_base_items SET file_path=?`, source); err != nil {
		t.Fatal(err)
	}
	a.FilePath = source
	ctx := context.WithValue(context.Background(), indexAttemptKey{}, a)
	if _, err := NewSQLiteIndexer(db, 1, "model").Store(ctx, manifestDocs(), indexer.WithEmbedding(&manifestEmbedder{})); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(db, base, zap.NewNop())
	if _, err := manager.UpdateItem("item", "category", "title", "edited body"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.KnowledgeConfig{}
	embed := &Embedder{config: cfg, eino: &manifestEmbedder{failAt: 1}, maxRetries: 1, logger: zap.NewNop()}
	idx, err := NewIndexer(context.Background(), db, embed, zap.NewNop(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = idx.IndexItem(context.Background(), "item"); err == nil {
		t.Fatal("expected provider failure after edit")
	}
	var rows int
	if err = db.QueryRow(`SELECT count(*) FROM knowledge_embeddings WHERE item_id='item'`).Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("edit erased old vectors: %d %v", rows, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM knowledge_index_state WHERE item_id='item'`).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("edited item retained stale completion marker")
	}
}
