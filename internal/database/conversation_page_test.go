package database

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// TestConversationPagesAreStable verifies equal timestamps, concurrent appends,
// deleted/foreign cursors, limits and compatibility with full-history callers.
func TestConversationPagesAreStable(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "history.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conv, err := db.CreateConversation("fixture", ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 105; index++ {
		_, err := db.Exec("INSERT INTO messages (id, conversation_id, role, content, created_at, updated_at) VALUES (?, ?, 'user', ?, ?, ?)", fmt.Sprintf("m-%03d", index), conv.ID, fmt.Sprint(index), "2026-01-01 00:00:00", "2026-01-01 00:00:00")
		if err != nil {
			t.Fatal(err)
		}
	}
	first, err := db.GetConversationPage(context.Background(), conv.ID, 40, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Messages) != 40 || first.Messages[0].ID != "m-065" || !first.MessagePage.HasMore {
		t.Fatalf("unexpected first page: %+v", first.MessagePage)
	}
	_, err = db.AddMessage(conv.ID, "assistant", "new live reply", nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	page := first
	for {
		for _, msg := range page.Messages {
			if seen[msg.ID] {
				t.Fatalf("duplicate: %s", msg.ID)
			}
			seen[msg.ID] = true
		}
		if !page.MessagePage.HasMore {
			break
		}
		page, err = db.GetConversationPage(context.Background(), conv.ID, 40, page.MessagePage.BeforeID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 105 {
		t.Fatalf("lost history: %d", len(seen))
	}
	full, err := db.GetConversationLite(conv.ID)
	if err != nil || len(full.Messages) != 106 || full.MessagePage != nil {
		t.Fatalf("legacy history changed: %v", err)
	}
	other, _ := db.CreateConversation("other", ConversationCreateMeta{})
	if _, err := db.GetConversationPage(context.Background(), other.ID, 40, "m-065"); !errors.Is(err, ErrMessagePageCursor) {
		t.Fatalf("foreign cursor: %v", err)
	}
	if _, err := db.Exec("DELETE FROM messages WHERE id = 'm-065'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetConversationPage(context.Background(), conv.ID, 40, "m-065"); !errors.Is(err, ErrMessagePageCursor) {
		t.Fatalf("deleted cursor: %v", err)
	}
	for _, limit := range []int{-1, 0, 101} {
		if _, err := db.GetConversationPage(context.Background(), conv.ID, limit, ""); err == nil {
			t.Fatalf("accepted limit %d", limit)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := db.GetConversationPage(ctx, conv.ID, 40, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	empty, err := db.GetConversationPage(context.Background(), other.ID, 40, "")
	if err != nil || len(empty.Messages) != 0 || empty.MessagePage.HasMore {
		t.Fatalf("empty page: %v", err)
	}
}

// BenchmarkConversationHistory compares full reads with the interactive page on
// generated data only. Setup is excluded and no live conversation is inspected.
func BenchmarkConversationHistory(b *testing.B) {
	db, err := NewDB(filepath.Join(b.TempDir(), "benchmark.db"), zap.NewNop())
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	conv, err := db.CreateConversation("benchmark", ConversationCreateMeta{})
	if err != nil {
		b.Fatal(err)
	}
	transaction, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	for index := 0; index < 2000; index++ {
		_, err = transaction.Exec("INSERT INTO messages (id, conversation_id, role, content, created_at, updated_at) VALUES (?, ?, 'assistant', ?, ?, ?)", fmt.Sprintf("bench-%d", index), conv.ID, strings.Repeat("fixture ", 128), "2026-01-01 00:00:00", "2026-01-01 00:00:01")
		if err != nil {
			transaction.Rollback()
			b.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		b.Fatal(err)
	}
	b.Run("full_2000", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			conv, err := db.GetConversationLite(conv.ID)
			if err != nil || len(conv.Messages) != 2000 {
				b.Fatalf("full: %v", err)
			}
		}
	})
	b.Run("page_40", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			conv, err := db.GetConversationPage(context.Background(), conv.ID, 40, "")
			if err != nil || len(conv.Messages) != 40 {
				b.Fatalf("page: %v", err)
			}
		}
	})
}
