package mcp_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/mcp"
	"go.uber.org/zap"
)

func submitFailureTest(t *testing.T, service *mcp.ExecutionService, request mcp.ExecutionRequest) *mcp.ToolExecution {
	t.Helper()
	handle, err := service.Submit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Wait(context.Background(), handle.ID, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.Execution
}

func TestFailureCooldownBoundsRepeatedCallsAndRecovers(t *testing.T) {
	service := mcp.NewExecutionService(nil, zap.NewNop())
	service.ConfigureFailureCooldown(mcp.FailureCooldownConfig{Threshold: 2, WindowSeconds: 60, CooldownSeconds: 1})
	var calls atomic.Int32
	request := mcp.ExecutionRequest{ConversationID: "conversation", OwnerUserID: "owner", ToolName: "fixture", Arguments: map[string]interface{}{"key": "value"}, Run: func(context.Context) (*mcp.ToolResult, error) { calls.Add(1); return nil, errors.New("unavailable") }}
	for i := 0; i < 2; i++ {
		if got := submitFailureTest(t, service, request).Status; got != mcp.ToolExecutionStatusFailed {
			t.Fatal(got)
		}
	}
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			execution := submitFailureTest(t, service, request)
			if execution.Status != mcp.ToolExecutionStatusBlocked || !strings.Contains(execution.Error, "tool_failure_cooldown") {
				t.Errorf("unexpected execution: %#v", execution)
			}
		}()
	}
	workers.Wait()
	if calls.Load() != 2 {
		t.Fatalf("provider calls=%d, want 2", calls.Load())
	}
	// A different conversation, owner or argument set has independent state.
	for _, variant := range []int{0, 1, 2} {
		other := request
		switch variant {
		case 0:
			other.ConversationID = "other"
		case 1:
			other.OwnerUserID = "other"
		case 2:
			other.Arguments = map[string]interface{}{"key": "other"}
		}
		if submitFailureTest(t, service, other).Status != mcp.ToolExecutionStatusFailed {
			t.Fatal("unrelated request blocked")
		}
	}
	time.Sleep(1100 * time.Millisecond)
	request.Run = func(context.Context) (*mcp.ToolResult, error) { return &mcp.ToolResult{}, nil }
	for i := 0; i < 2; i++ {
		if submitFailureTest(t, service, request).Status != mcp.ToolExecutionStatusCompleted {
			t.Fatal("recovery failed")
		}
	}
}

func TestFailureCooldownIgnoresCancellationAndPolicyRefusal(t *testing.T) {
	service := mcp.NewExecutionService(nil, zap.NewNop())
	request := mcp.ExecutionRequest{ConversationID: "conversation", ToolName: "fixture", Run: func(context.Context) (*mcp.ToolResult, error) { return nil, errors.New("unavailable") }}
	if submitFailureTest(t, service, request).Status != mcp.ToolExecutionStatusFailed {
		t.Fatal("expected failure")
	}
	failedRun := request.Run
	request.Run = func(context.Context) (*mcp.ToolResult, error) { return nil, context.Canceled }
	if submitFailureTest(t, service, request).Status != mcp.ToolExecutionStatusCancelled {
		t.Fatal("expected cancellation")
	}
	request.Run = func(context.Context) (*mcp.ToolResult, error) {
		return &mcp.ToolResult{IsError: true, Blocked: true}, nil
	}
	if submitFailureTest(t, service, request).Status != mcp.ToolExecutionStatusBlocked {
		t.Fatal("expected policy refusal")
	}
	request.Run = failedRun
	if submitFailureTest(t, service, request).Status != mcp.ToolExecutionStatusFailed {
		t.Fatal("cancellation or policy refusal consumed budget")
	}
	if submitFailureTest(t, service, request).Status != mcp.ToolExecutionStatusBlocked {
		t.Fatal("expected cooldown")
	}
	service.ConfigureFailureCooldown(mcp.FailureCooldownConfig{Threshold: -1})
	for i := 0; i < 3; i++ {
		if submitFailureTest(t, service, request).Status != mcp.ToolExecutionStatusFailed {
			t.Fatal("disable ignored")
		}
	}
}
