package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"

	fileloader "github.com/cloudwego/eino-ext/components/document/loader/file"
	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

// Indexer 使用 Eino Compose 索引链（Markdown/递归分块、Lambda  enrich、SQLite 索引）与嵌入写入。
type Indexer struct {
	indexMu     *semaphore.Weighted // cancellable indexing serialization
	indexOnce   sync.Once
	db          *sql.DB
	embedder    *Embedder
	logger      *zap.Logger
	chunkSize   int
	overlap     int
	indexingCfg *config.IndexingConfig

	indexChain compose.Runnable[[]*schema.Document, []string]
	fileLoader *fileloader.FileLoader

	mu            sync.RWMutex
	lastError     string
	lastErrorTime time.Time
	errorCount    int

	rebuildMu         sync.RWMutex
	isRebuilding      bool
	rebuildTotalItems int
	rebuildCurrent    int
	rebuildFailed     int
	rebuildStartTime  time.Time
	rebuildLastItemID string
	rebuildLastChunks int
}

// NewIndexer 创建索引器并编译 Eino 索引链；kcfg 为完整知识库配置（含 indexing 与路径相关行为）。
func NewIndexer(ctx context.Context, db *sql.DB, embedder *Embedder, logger *zap.Logger, kcfg *config.KnowledgeConfig) (*Indexer, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	if embedder == nil {
		return nil, fmt.Errorf("embedder is nil")
	}
	if err := EnsureKnowledgeEmbeddingsSchema(db); err != nil {
		return nil, fmt.Errorf("knowledge_embeddings 结构迁移: %w", err)
	}
	if kcfg == nil {
		kcfg = &config.KnowledgeConfig{}
	}
	indexingCfg := &kcfg.Indexing

	chunkSize, overlap := knowledgeChunkSettings(indexingCfg)

	embedModel := embedder.EmbeddingModelName()
	splitter, err := newKnowledgeSplitter(chunkSize, overlap, embedModel)
	if err != nil {
		return nil, fmt.Errorf("eino recursive splitter: %w", err)
	}

	chain, err := buildKnowledgeIndexChain(ctx, indexingCfg, db, splitter, embedModel)
	if err != nil {
		return nil, fmt.Errorf("knowledge index chain: %w", err)
	}

	var fl *fileloader.FileLoader
	fl, err = fileloader.NewFileLoader(ctx, nil)
	if err != nil {
		if logger != nil {
			logger.Warn("Eino FileLoader 初始化失败，prefer_source_file 将回退数据库正文", zap.Error(err))
		}
		fl = nil
		err = nil
	}

	return &Indexer{
		db:          db,
		embedder:    embedder,
		logger:      logger,
		chunkSize:   chunkSize,
		overlap:     overlap,
		indexingCfg: indexingCfg,
		indexChain:  chain,
		fileLoader:  fl,
	}, nil
}

// knowledgeChunkSettings applies the same defaults during creation and recompilation.
func knowledgeChunkSettings(cfg *config.IndexingConfig) (size, overlap int) {
	size, overlap = 512, 50
	if cfg != nil {
		if cfg.ChunkSize > 0 {
			size = cfg.ChunkSize
		}
		if cfg.ChunkOverlap >= 0 {
			overlap = cfg.ChunkOverlap
		}
	}
	return
}

// RecompileIndexChain 在配置或嵌入模型变更后重建 Eino 索引链（无需重启进程）。
func (idx *Indexer) RecompileIndexChain(ctx context.Context) error {
	if idx == nil {
		return fmt.Errorf("indexer is nil")
	}
	if err := idx.acquireIndex(ctx); err != nil {
		return err
	}
	defer idx.indexMu.Release(1)
	if idx.db == nil || idx.embedder == nil {
		return fmt.Errorf("indexer 未初始化")
	}
	if err := EnsureKnowledgeEmbeddingsSchema(idx.db); err != nil {
		return err
	}
	chunkSize, overlap := knowledgeChunkSettings(idx.indexingCfg)
	embedModel := idx.embedder.EmbeddingModelName()
	splitter, err := newKnowledgeSplitter(chunkSize, overlap, embedModel)
	if err != nil {
		return fmt.Errorf("eino recursive splitter: %w", err)
	}
	chain, err := buildKnowledgeIndexChain(ctx, idx.indexingCfg, idx.db, splitter, embedModel)
	if err != nil {
		return fmt.Errorf("knowledge index chain: %w", err)
	}
	idx.chunkSize, idx.overlap = chunkSize, overlap
	idx.indexChain = chain
	return nil
}

// IndexItem generates a complete replacement before atomically publishing vectors.
// Failed embedding or concurrent source updates preserve the previous index.
func (idx *Indexer) IndexItem(ctx context.Context, itemID string) error {
	return idx.indexItem(ctx, itemID, false)
}

func (idx *Indexer) indexItem(ctx context.Context, itemID string, skipComplete bool) error {
	if err := idx.acquireIndex(ctx); err != nil {
		return err
	}
	defer idx.indexMu.Release(1)
	if idx.indexChain == nil {
		return fmt.Errorf("索引链未初始化")
	}
	if idx.embedder == nil {
		return fmt.Errorf("嵌入器未初始化")
	}

	var content, category, title, filePath string
	err := idx.db.QueryRowContext(ctx, "SELECT content, category, title, file_path FROM knowledge_base_items WHERE id = ?", itemID).Scan(&content, &category, &title, &filePath)
	if err != nil {
		return fmt.Errorf("获取知识项失败：%w", err)
	}

	body := strings.TrimSpace(content)
	if idx.indexingCfg != nil && idx.indexingCfg.PreferSourceFile && strings.TrimSpace(filePath) != "" && idx.fileLoader != nil {
		docs, lerr := idx.fileLoader.Load(ctx, document.Source{URI: strings.TrimSpace(filePath)})
		if lerr == nil && len(docs) > 0 {
			var b strings.Builder
			for i, d := range docs {
				if d == nil {
					continue
				}
				if i > 0 {
					b.WriteString("\n\n")
				}
				b.WriteString(d.Content)
			}
			if s := strings.TrimSpace(b.String()); s != "" {
				body = s
			}
		} else if idx.logger != nil {
			idx.logger.Warn("优先源文件读取失败，使用数据库正文",
				zap.String("itemId", itemID),
				zap.String("path", filePath),
				zap.Error(lerr))
		}
	}

	attempt := indexAttempt{ItemID: itemID, Content: content, Category: category, Title: title, FilePath: filePath,
		SourceHash: indexDigest([]string{content, category, title, filePath, body}), ConfigHash: idx.indexConfigHash()}
	if skipComplete {
		complete, err := idx.indexStateComplete(ctx, attempt)
		if err != nil {
			return err
		}
		if complete {
			return nil
		}
	}
	ctx = context.WithValue(ctx, indexAttemptKey{}, attempt)

	root := &schema.Document{
		ID:      itemID,
		Content: body,
		MetaData: map[string]any{
			metaKBCategory: category,
			metaKBTitle:    title,
			metaKBItemID:   itemID,
		},
	}

	idxOpts := []indexer.Option{indexer.WithEmbedding(idx.embedder.EinoEmbeddingComponent())}
	if idx.indexingCfg != nil && len(idx.indexingCfg.SubIndexes) > 0 {
		idxOpts = append(idxOpts, indexer.WithSubIndexes(idx.indexingCfg.SubIndexes))
	}

	ids, err := idx.indexChain.Invoke(ctx, []*schema.Document{root}, compose.WithIndexerOption(idxOpts...))
	if err != nil {
		msg := fmt.Sprintf("索引写入失败 (知识项：%s): %v", itemID, err)
		idx.mu.Lock()
		idx.lastError = msg
		idx.lastErrorTime = time.Now()
		idx.mu.Unlock()
		return err
	}

	if idx.logger != nil {
		idx.logger.Info("知识项索引完成", zap.String("itemId", itemID), zap.Int("chunks", len(ids)))
	}
	idx.rebuildMu.Lock()
	idx.rebuildLastItemID = itemID
	idx.rebuildLastChunks = len(ids)
	idx.rebuildMu.Unlock()
	return nil
}

// HasIndex 检查是否存在索引
func (idx *Indexer) HasIndex() (bool, error) {
	var count int
	err := idx.db.QueryRow("SELECT COUNT(*) FROM knowledge_embeddings").Scan(&count)
	if err != nil {
		return false, fmt.Errorf("检查索引失败：%w", err)
	}
	return count > 0, nil
}

func (idx *Indexer) beginIndexRun() error {
	idx.rebuildMu.Lock()
	defer idx.rebuildMu.Unlock()

	if idx.isRebuilding {
		return fmt.Errorf("索引任务已在进行中")
	}
	idx.isRebuilding = true
	idx.rebuildTotalItems = 0
	idx.rebuildCurrent = 0
	idx.rebuildFailed = 0
	idx.rebuildStartTime = time.Now()
	idx.rebuildLastItemID = ""
	idx.rebuildLastChunks = 0
	return nil
}

// TryBeginIndexRun 同步占用索引任务槽位；调用方必须在后台任务结束时调用 FinishIndexRun。
func (idx *Indexer) TryBeginIndexRun() error {
	return idx.beginIndexRun()
}

func (idx *Indexer) FinishIndexRun() {
	idx.rebuildMu.Lock()
	idx.isRebuilding = false
	idx.rebuildMu.Unlock()
}

func (idx *Indexer) resetLastError() {
	idx.mu.Lock()
	idx.lastError = ""
	idx.lastErrorTime = time.Time{}
	idx.errorCount = 0
	idx.mu.Unlock()
}

func (idx *Indexer) setIndexRunTotal(total int) {
	idx.rebuildMu.Lock()
	idx.rebuildTotalItems = total
	idx.rebuildMu.Unlock()
}

// IndexMissing repairs missing, incomplete, legacy, or outdated indexes.
// Unchanged complete items are skipped; failed replacements retain old vectors.
func (idx *Indexer) IndexMissing(ctx context.Context) error {
	if err := idx.beginIndexRun(); err != nil {
		return err
	}
	defer idx.FinishIndexRun()
	return idx.runIndexMissing(ctx)
}

// RebuildIndex 全量重建所有知识项索引（显式 opt-in，成本更高）。
func (idx *Indexer) RebuildIndex(ctx context.Context) error {
	if err := idx.beginIndexRun(); err != nil {
		return err
	}
	defer idx.FinishIndexRun()
	return idx.runRebuildIndex(ctx)
}

// RunRebuildIndex 在已占用索引任务槽位后执行全量重建（供 HTTP handler 后台任务使用）。
func (idx *Indexer) RunRebuildIndex(ctx context.Context) error {
	return idx.runRebuildIndex(ctx)
}

// RunIndexItems indexes supplied IDs after the caller acquires the run slot.
// It reports progress and uses the shared failure cutoff; callers must release the slot.
func (idx *Indexer) RunIndexItems(ctx context.Context, itemIDs []string) error {
	idx.resetLastError()
	idx.setIndexRunTotal(len(itemIDs))
	return idx.indexItemIDsWithLimit(ctx, itemIDs, "增量索引完成", 2)
}

// RunIndexMissing 在已占用索引任务槽位后执行缺失索引补齐（供 HTTP handler 后台任务使用）。
func (idx *Indexer) RunIndexMissing(ctx context.Context) error {
	return idx.runIndexMissing(ctx)
}

func (idx *Indexer) runRebuildIndex(ctx context.Context) error {
	idx.resetLastError()

	rows, err := idx.db.QueryContext(ctx, "SELECT id FROM knowledge_base_items ORDER BY updated_at ASC, id ASC")
	if err != nil {
		return fmt.Errorf("查询知识项失败：%w", err)
	}
	defer rows.Close()

	itemIDs, err := scanKnowledgeItemIDs(rows)
	if err != nil {
		return err
	}

	idx.setIndexRunTotal(len(itemIDs))
	idx.logger.Info("开始重建索引", zap.Int("totalItems", len(itemIDs)))

	return idx.indexItemIDs(ctx, itemIDs, "索引重建完成")
}

func (idx *Indexer) runIndexMissing(ctx context.Context) error {
	idx.resetLastError()
	if err := idx.acquireIndex(ctx); err != nil {
		return err
	}
	configHash, preferSource := idx.indexConfigHash(), idx.indexingCfg.PreferSourceFile
	idx.indexMu.Release(1)

	rows, err := idx.db.QueryContext(ctx, `
        SELECT i.id FROM knowledge_base_items i LEFT JOIN knowledge_index_state s ON s.item_id=i.id
        WHERE s.item_id IS NULL OR s.config_hash != ? OR (? AND i.file_path != '')
        OR NOT (`+indexRowsCompletePredicate+`)
        ORDER BY i.updated_at ASC, i.id ASC`, configHash, preferSource)
	if err != nil {
		return fmt.Errorf("查询未索引知识项失败：%w", err)
	}
	defer rows.Close()

	itemIDs, err := scanKnowledgeItemIDs(rows)
	if err != nil {
		return fmt.Errorf("扫描未索引知识项 ID 失败：%w", err)
	}

	idx.setIndexRunTotal(len(itemIDs))
	idx.logger.Info("开始补齐缺失索引", zap.Int("totalItems", len(itemIDs)))

	return idx.indexItemIDs(ctx, itemIDs, "索引构建完成", true)
}

func scanKnowledgeItemIDs(rows *sql.Rows) ([]string, error) {
	var itemIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("扫描知识项 ID 失败：%w", err)
		}
		itemIDs = append(itemIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("扫描知识项 ID 失败：%w", err)
	}
	return itemIDs, nil
}

func (idx *Indexer) indexItemIDs(ctx context.Context, itemIDs []string, doneMessage string, skipComplete ...bool) error {
	return idx.indexItemIDsWithLimit(ctx, itemIDs, doneMessage, 5, skipComplete...)
}

// indexItemIDsWithLimit preserves the caller-specific consecutive failure budget.
func (idx *Indexer) indexItemIDsWithLimit(ctx context.Context, itemIDs []string, doneMessage string, maxConsecutiveFailures int, skipComplete ...bool) error {
	failedCount := 0
	consecutiveFailures := 0
	firstFailureItemID := ""
	var firstFailureError error

	for i, itemID := range itemIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := idx.indexItem(ctx, itemID, len(skipComplete) > 0 && skipComplete[0]); err != nil {
			failedCount++
			consecutiveFailures++
			idx.rebuildMu.Lock()
			idx.rebuildCurrent, idx.rebuildFailed = i+1, failedCount
			idx.rebuildMu.Unlock()

			if consecutiveFailures == 1 {
				firstFailureItemID = itemID
				firstFailureError = err
				idx.logger.Warn("索引知识项失败",
					zap.String("itemId", itemID),
					zap.Int("totalItems", len(itemIDs)),
					zap.Error(err),
				)
			}

			if consecutiveFailures >= maxConsecutiveFailures {
				errorMsg := fmt.Sprintf("连续 %d 个知识项索引失败，可能存在配置问题（如嵌入模型配置错误、API 密钥无效、余额不足等）。第一个失败项：%s, 错误：%v", consecutiveFailures, firstFailureItemID, firstFailureError)
				idx.mu.Lock()
				idx.lastError = errorMsg
				idx.lastErrorTime = time.Now()
				idx.mu.Unlock()

				idx.logger.Error("连续索引失败次数过多，立即停止索引",
					zap.Int("consecutiveFailures", consecutiveFailures),
					zap.Int("totalItems", len(itemIDs)),
					zap.Int("processedItems", i+1),
					zap.String("firstFailureItemId", firstFailureItemID),
					zap.Error(firstFailureError),
				)
				return fmt.Errorf("连续索引失败次数过多：%v", firstFailureError)
			}

			if failedCount > len(itemIDs)*3/10 && failedCount == len(itemIDs)*3/10+1 {
				errorMsg := fmt.Sprintf("索引失败的知识项过多 (%d/%d)，可能存在配置问题。第一个失败项：%s, 错误：%v", failedCount, len(itemIDs), firstFailureItemID, firstFailureError)
				idx.mu.Lock()
				idx.lastError = errorMsg
				idx.lastErrorTime = time.Now()
				idx.mu.Unlock()

				idx.logger.Error("索引失败的知识项过多，可能存在配置问题",
					zap.Int("failedCount", failedCount),
					zap.Int("totalItems", len(itemIDs)),
					zap.String("firstFailureItemId", firstFailureItemID),
					zap.Error(firstFailureError),
				)
			}
			continue
		}

		if consecutiveFailures > 0 {
			consecutiveFailures = 0
			firstFailureItemID = ""
			firstFailureError = nil
		}

		idx.rebuildMu.Lock()
		idx.rebuildCurrent = i + 1
		idx.rebuildFailed = failedCount
		idx.rebuildMu.Unlock()

		if (i+1)%10 == 0 || (len(itemIDs) > 0 && (i+1)*100/len(itemIDs)%10 == 0 && (i+1)*100/len(itemIDs) > 0) {
			idx.logger.Info("索引进度", zap.Int("current", i+1), zap.Int("total", len(itemIDs)), zap.Int("failed", failedCount))
		}
	}

	idx.logger.Info(doneMessage, zap.Int("totalItems", len(itemIDs)), zap.Int("failedCount", failedCount))
	if failedCount > 0 {
		return fmt.Errorf("knowledge indexing failed for %d of %d items; retry incomplete items", failedCount, len(itemIDs))
	}
	return nil
}

// GetLastError 获取最近一次错误信息
func (idx *Indexer) GetLastError() (string, time.Time) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.lastError, idx.lastErrorTime
}

// GetRebuildStatus 获取重建索引状态
func (idx *Indexer) GetRebuildStatus() (isRebuilding bool, totalItems int, current int, failed int, lastItemID string, lastChunks int, startTime time.Time) {
	idx.rebuildMu.RLock()
	defer idx.rebuildMu.RUnlock()
	return idx.isRebuilding, idx.rebuildTotalItems, idx.rebuildCurrent, idx.rebuildFailed, idx.rebuildLastItemID, idx.rebuildLastChunks, idx.rebuildStartTime
}

// acquireIndex waits for exclusive indexing access, or returns caller cancellation.
func (idx *Indexer) acquireIndex(ctx context.Context) error {
	idx.indexOnce.Do(func() { idx.indexMu = semaphore.NewWeighted(1) })
	return idx.indexMu.Acquire(ctx, 1)
}
