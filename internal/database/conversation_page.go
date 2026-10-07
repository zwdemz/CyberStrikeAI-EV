package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// MaxConversationMessagePage bounds the work of an interactive history request.
const MaxConversationMessagePage = 100

// ErrMessagePageCursor indicates an invalid, deleted, or foreign conversation cursor.
var ErrMessagePageCursor = errors.New("invalid message page cursor")

// ConversationMessagePage describes older history without counting or loading it.
type ConversationMessagePage struct {
	HasMore  bool   `json:"hasMore"`
	BeforeID string `json:"beforeMessageId,omitempty"`
}

// GetConversationPage returns the latest lightweight messages before beforeID,
// in chronological order. An empty cursor starts at the latest message. The
// limit must be 1..MaxConversationMessagePage; invalid/foreign/deleted cursors
// return ErrMessagePageCursor. Context cancellation stops the message query.
// Existing full-history APIs remain unchanged for exports and model context.
func (db *DB) GetConversationPage(ctx context.Context, id string, limit int, beforeID string) (*Conversation, error) {
	if limit < 1 || limit > MaxConversationMessagePage {
		return nil, fmt.Errorf("invalid message page size")
	}
	conv, err := db.getConversationMetadata(id)
	if err != nil {
		return nil, err
	}
	query := "SELECT id, conversation_id, role, content, mcp_execution_ids, created_at, updated_at FROM messages WHERE conversation_id = ?"
	args := []interface{}{id}
	if beforeID != "" {
		// Preserve SQLite storage text; timestamp driver conversion changes cursor ordering.
		var createdAt string
		var rowID int64
		if err := db.QueryRowContext(ctx, "SELECT CAST(created_at AS TEXT), rowid FROM messages WHERE conversation_id = ? AND id = ?", id, beforeID).Scan(&createdAt, &rowID); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrMessagePageCursor
			}
			return nil, err
		}
		// Match the full-history ordering, including messages sharing a timestamp.
		query += " AND (created_at, rowid) < (?, ?)"
		args = append(args, createdAt, rowID)
	}
	query += " ORDER BY created_at DESC, rowid DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages, err := db.scanMessagesLite(rows)
	if err != nil {
		return nil, err
	}
	page := &ConversationMessagePage{HasMore: len(messages) > limit}
	if page.HasMore {
		messages = messages[:limit]
	}
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	if len(messages) > 0 {
		page.BeforeID = messages[0].ID
	}
	conv.Messages, conv.MessagePage = messages, page
	return conv, nil
}
