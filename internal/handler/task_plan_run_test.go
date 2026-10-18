package handler

import (
	"context"
	"errors"
	"testing"
	"time"
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
