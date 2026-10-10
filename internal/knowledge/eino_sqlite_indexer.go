package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

// SQLiteIndexer implements [indexer.Indexer] against knowledge_embeddings + existing schema.
type SQLiteIndexer struct {
	db             *sql.DB
	batchSize      int
	embeddingModel string
}

// NewSQLiteIndexer returns an indexer that writes chunk rows for one knowledge item per Store call.
// batchSize is the embedding batch size; if <= 0, default 64 is used.
// embeddingModel is persisted per row for retrieval-time consistency checks (may be empty).
func NewSQLiteIndexer(db *sql.DB, batchSize int, embeddingModel string) *SQLiteIndexer {
	return &SQLiteIndexer{db: db, batchSize: batchSize, embeddingModel: strings.TrimSpace(embeddingModel)}
}

// GetType implements eino callback run info.
func (s *SQLiteIndexer) GetType() string {
	return "SQLiteKnowledgeIndexer"
}

// Store embeds all documents before atomically replacing one item's rows.
// Embedding, validation, or transaction failures preserve the previous index. Each doc must carry MetaData:
// kb_item_id, kb_category, kb_title, kb_chunk_index (int). Content is chunk text only.
func (s *SQLiteIndexer) Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) (ids []string, err error) {
	options := indexer.GetCommonOptions(nil, opts...)
	if options.Embedding == nil {
		return nil, fmt.Errorf("sqlite indexer: embedding is required")
	}
	attempt, tracked := ctx.Value(indexAttemptKey{}).(indexAttempt)
	itemID := attempt.ItemID
	if len(docs) == 0 && !tracked {
		return nil, nil
	}
	for i, d := range docs {
		if d == nil {
			return nil, fmt.Errorf("sqlite indexer: nil document at %d", i)
		}
		docItem, e := RequireMetaString(d.MetaData, metaKBItemID)
		if e != nil {
			return nil, e
		}
		if i == 0 && !tracked {
			itemID = docItem
		}
		chunk, e := RequireMetaInt(d.MetaData, metaKBChunkIndex)
		if e != nil {
			return nil, e
		}
		if docItem != itemID || chunk != i {
			return nil, fmt.Errorf("sqlite indexer: mixed item or non-contiguous chunk at %d", i)
		}
	}
	if strings.TrimSpace(itemID) == "" {
		return nil, fmt.Errorf("sqlite indexer: item is required")
	}

	ctx = callbacks.EnsureRunInfo(ctx, s.GetType(), components.ComponentOfIndexer)
	ctx = callbacks.OnStart(ctx, &indexer.CallbackInput{Docs: docs})
	defer func() {
		if err != nil {
			_ = callbacks.OnError(ctx, err)
			return
		}
		_ = callbacks.OnEnd(ctx, &indexer.CallbackOutput{IDs: ids})
	}()

	subIdxStr := strings.Join(options.SubIndexes, ",")

	texts := make([]string, len(docs))
	for i, d := range docs {
		if d == nil {
			return nil, fmt.Errorf("sqlite indexer: nil document at %d", i)
		}
		cat := MetaLookupString(d.MetaData, metaKBCategory)
		title := MetaLookupString(d.MetaData, metaKBTitle)
		texts[i] = FormatEmbeddingInput(cat, title, d.Content)
	}

	bs := s.batchSize
	if bs <= 0 {
		bs = 64
	}

	var allVecs [][]float64
	for start := 0; start < len(texts); start += bs {
		end := start + bs
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[start:end]
		vecs, embedErr := options.Embedding.EmbedStrings(ctx, batch)
		if embedErr != nil {
			return nil, fmt.Errorf("sqlite indexer: embed batch %d-%d: %w", start, end, embedErr)
		}
		if len(vecs) != len(batch) {
			return nil, fmt.Errorf("sqlite indexer: embed count mismatch: got %d want %d", len(vecs), len(batch))
		}
		allVecs = append(allVecs, vecs...)
	}

	embedDim := 0
	if len(allVecs) > 0 {
		embedDim = len(allVecs[0])
		if embedDim == 0 {
			return nil, fmt.Errorf("sqlite indexer: empty embedding")
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("sqlite indexer: begin tx: %w", err)
	}
	defer tx.Rollback()
	if tracked {
		if err := verifyIndexSource(ctx, tx, attempt); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM knowledge_embeddings WHERE item_id=?", itemID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM knowledge_index_state WHERE item_id=?", itemID); err != nil {
		return nil, err
	}

	ids = make([]string, 0, len(docs))
	for i, d := range docs {
		chunkID := uuid.New().String()
		chunkIdx := i // Item identity and contiguous indices were validated before embedding.
		vec := allVecs[i]
		if embedDim > 0 && len(vec) != embedDim {
			return nil, fmt.Errorf("sqlite indexer: inconsistent embedding dim at doc %d: got %d want %d", i, len(vec), embedDim)
		}
		vec32 := make([]float32, len(vec))
		for j, v := range vec {
			vec32[j] = float32(v)
		}
		embeddingJSON, jsonErr := json.Marshal(vec32)
		if jsonErr != nil {
			return nil, fmt.Errorf("sqlite indexer: marshal embedding: %w", jsonErr)
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO knowledge_embeddings (id, item_id, chunk_index, chunk_text, embedding, sub_indexes, embedding_model, embedding_dim, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
			chunkID, itemID, chunkIdx, d.Content, string(embeddingJSON), subIdxStr, s.embeddingModel, embedDim,
		)
		if err != nil {
			return nil, fmt.Errorf("sqlite indexer: insert chunk %d: %w", i, err)
		}
		ids = append(ids, chunkID)
	}

	if tracked {
		if err := saveIndexManifest(ctx, tx, attempt, len(docs), s.embeddingModel, embedDim); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("sqlite indexer: commit: %w", err)
	}
	return ids, nil
}

var _ indexer.Indexer = (*SQLiteIndexer)(nil)
