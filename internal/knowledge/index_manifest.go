package knowledge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type indexAttemptKey struct{}

// indexAttempt binds generated vectors to the exact database snapshot and input
// used for embedding. It travels with the invocation, never in provider payloads.
type indexAttempt struct {
	ItemID, Content, Category, Title, FilePath string
	SourceHash, ConfigHash                     string
}

func indexDigest(value any) string {
	data, _ := json.Marshal(value)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (idx *Indexer) indexConfigHash() string {
	baseURL := strings.TrimSuffix(strings.TrimSpace(idx.embedder.config.Embedding.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return indexDigest([]any{"ev-index-v1", idx.embedder.EmbeddingModelName(), baseURL,
		idx.chunkSize, idx.overlap, idx.indexingCfg.ChunkStrategy,
		idx.indexingCfg.MaxChunksPerItem, idx.indexingCfg.SubIndexes, idx.indexingCfg.PreferSourceFile})
}

func ensureIndexManifest(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS knowledge_index_state (
	 item_id TEXT PRIMARY KEY REFERENCES knowledge_base_items(id) ON DELETE CASCADE,
	 source_hash TEXT NOT NULL, config_hash TEXT NOT NULL,
	 chunk_count INTEGER NOT NULL CHECK(chunk_count >= 0),
	 embedding_model TEXT NOT NULL, embedding_dim INTEGER NOT NULL);
	 CREATE TRIGGER IF NOT EXISTS knowledge_index_source_changed
	 AFTER UPDATE OF content, category, title, file_path ON knowledge_base_items
	 BEGIN DELETE FROM knowledge_index_state WHERE item_id = NEW.id; END;
	 CREATE TRIGGER IF NOT EXISTS knowledge_index_source_deleted
	 AFTER DELETE ON knowledge_base_items
	 BEGIN DELETE FROM knowledge_index_state WHERE item_id = OLD.id; END;`)
	return err
}

// indexStateComplete checks the manifest and actual rows, including missing,
// duplicate or non-contiguous chunk indices and mixed model/dimension metadata.
const indexRowsCompletePredicate = `s.chunk_count=(SELECT count(*) FROM knowledge_embeddings e WHERE e.item_id=s.item_id)
 AND s.chunk_count=(SELECT count(DISTINCT chunk_index) FROM knowledge_embeddings e WHERE e.item_id=s.item_id)
 AND NOT EXISTS(SELECT 1 FROM knowledge_embeddings e WHERE e.item_id=s.item_id
 AND (chunk_index < 0 OR chunk_index >= s.chunk_count OR embedding_model != s.embedding_model
 OR embedding_dim != s.embedding_dim OR embedding_dim <= 0))`

func (idx *Indexer) indexStateComplete(ctx context.Context, attempt indexAttempt) (bool, error) {
	var complete bool
	err := idx.db.QueryRowContext(ctx, `SELECT EXISTS (
 SELECT 1 FROM knowledge_index_state s WHERE item_id=? AND source_hash=? AND config_hash=?
 AND `+indexRowsCompletePredicate+`)`, attempt.ItemID, attempt.SourceHash, attempt.ConfigHash).Scan(&complete)
	return complete, err
}

func verifyIndexSource(ctx context.Context, tx *sql.Tx, attempt indexAttempt) error {
	var matches bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_base_items
	 WHERE id=? AND content=? AND category=? AND title=? AND file_path=?)`,
		attempt.ItemID, attempt.Content, attempt.Category, attempt.Title, attempt.FilePath).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("knowledge source changed during embedding; retry indexing")
	}
	return nil
}

func saveIndexManifest(ctx context.Context, tx *sql.Tx, attempt indexAttempt, count int, model string, dimension int) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO knowledge_index_state
	 (item_id, source_hash, config_hash, chunk_count, embedding_model, embedding_dim)
	 VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(item_id) DO UPDATE SET
	 source_hash=excluded.source_hash, config_hash=excluded.config_hash,
	 chunk_count=excluded.chunk_count, embedding_model=excluded.embedding_model, embedding_dim=excluded.embedding_dim`,
		attempt.ItemID, attempt.SourceHash, attempt.ConfigHash, count, model, dimension)
	return err
}
