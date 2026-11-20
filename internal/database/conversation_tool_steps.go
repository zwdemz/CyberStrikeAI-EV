package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ConversationToolStep is a minimal, payload-free reference to a completed
// tool call. It does not claim the remote target's state has been verified.
type ConversationToolStep struct {
	ToolName  string
	DetailID  string
	CreatedAt time.Time
}

// RecentSuccessfulToolSteps returns recent durable tool completions for one
// conversation. Raw arguments and results never leave process_details through
// this API; failed, blocked, and background-running calls are excluded.
func (db *DB) RecentSuccessfulToolSteps(ctx context.Context, conversationID string, limit int) ([]ConversationToolStep, error) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || limit <= 0 {
		return nil, nil
	}
	if limit > 16 {
		limit = 16
	}
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(data, ''), created_at FROM process_details
		WHERE conversation_id = ? AND event_type = 'tool_result'
		ORDER BY created_at DESC, rowid DESC LIMIT 256`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("query recent tool steps: %w", err)
	}
	defer rows.Close()
	steps := make([]ConversationToolStep, 0, limit)
	seen := make(map[string]bool)
	for rows.Next() {
		var id, raw, created string
		if err := rows.Scan(&id, &raw, &created); err != nil {
			return nil, fmt.Errorf("scan recent tool step: %w", err)
		}
		var meta struct {
			ToolName string `json:"toolName"`
			Success  bool   `json:"success"`
			IsError  bool   `json:"isError"`
			Blocked  bool   `json:"blocked"`
			Status   string `json:"status"`
		}
		if json.Unmarshal([]byte(raw), &meta) != nil || !meta.Success || meta.IsError || meta.Blocked || meta.Status == "background_running" {
			continue
		}
		name := strings.TrimSpace(meta.ToolName)
		if !safeProgressToolName(name) || seen[name] {
			continue
		}
		stamp, err := parseProcessDetailTime(created)
		if err != nil {
			continue
		}
		seen[name] = true
		steps = append(steps, ConversationToolStep{ToolName: name, DetailID: id, CreatedAt: stamp})
		if len(steps) == limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent tool steps: %w", err)
	}
	return steps, nil
}

func safeProgressToolName(name string) bool {
	if name == "" || len(name) > 80 {
		return false
	}
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.' || char == ':' {
			continue
		}
		return false
	}
	return true
}

func parseProcessDetailTime(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"} {
		if stamp, err := time.Parse(layout, raw); err == nil {
			return stamp, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported process-detail timestamp")
}
