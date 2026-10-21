package handler

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/database"
	"go.uber.org/zap"
)

// TestTaskPlanRunRecorderDoesNotHoldManagerLock verifies that a slow store does
// not stall reads or cancellation for the manager's other conversations.
func TestTaskPlanRunRecorderDoesNotHoldManagerLock(t *testing.T) {
	manager := NewAgentTaskManager()
	defer manager.Shutdown()
	entered, release := make(chan struct{}), make(chan struct{})
	manager.runStartRecorder = func(string, string, time.Time) error {
		close(entered)
		<-release
		return errors.New("store unavailable")
	}
	done := make(chan error, 1)
	go func() { _, err := manager.StartTask("recorded", "fixture", func(error) {}); done <- err }()
	<-entered
	read := make(chan struct{}, 1)
	go func() { _ = manager.GetActiveTasks(); read <- struct{}{} }()
	select {
	case <-read:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("database recorder held global task lock")
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("failed recorder allowed execution")
	}
	if task := manager.GetTaskSnapshot("recorded"); task != nil {
		t.Fatal("failed reservation not removed")
	}
	manager.runStartRecorder = nil
	_, cancel := context.WithCancelCause(context.Background())
	if _, err := manager.StartTask("recorded", "retry", cancel); err != nil {
		t.Fatal(err)
	}
}

// TestTaskPlanRunClockRollback rejects execution when a newer saved timestamp
// makes SQLite skip the new run boundary, and removes the failed reservation.
func TestTaskPlanRunClockRollback(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "clock.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conv, err := db.CreateConversation("clock fixture", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(time.Hour)
	if err := db.RecordConversationPlanRun(conv.ID, "future-run", future); err != nil {
		t.Fatal(err)
	}
	manager := NewAgentTaskManager()
	defer manager.Shutdown()
	var boundaryErr error
	manager.runStartRecorder = func(conversationID, runID string, startedAt time.Time) error {
		boundaryErr = db.RecordConversationPlanRun(conversationID, runID, startedAt)
		return boundaryErr
	}
	// StartTask intentionally returns a generic storage error; verify the
	// underlying rejected write separately without exposing it to clients.
	if _, err := manager.StartTask(conv.ID, "fixture", func(error) {}); err == nil || boundaryErr == nil || !strings.Contains(boundaryErr.Error(), "stale start time") {
		t.Fatal("run did not reject the unrecorded boundary", err, boundaryErr)
	}
	if manager.GetTaskSnapshot(conv.ID) != nil {
		t.Fatal("clock-regressed run left an active reservation")
	}
	if saved, err := db.GetConversationPlanRunStart(conv.ID); err != nil || !saved.Equal(future) {
		t.Fatal("previous boundary was overwritten", saved, err)
	}
}
