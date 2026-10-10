package mcp

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"
)

// FailureCooldownConfig bounds repeated failures of identical calls in a
// conversation. Zero values select 2 failures in 60 seconds, then 60 seconds of
// cooldown. A negative Threshold disables this guard. Changes apply on restart.
type FailureCooldownConfig = config.ToolFailureCooldownConfig

type failureState struct {
	failures     int
	firstFailure time.Time
	until        time.Time
	inFlight     bool
}

type failureCooldown struct {
	mu         sync.Mutex
	generation uint64
	config     FailureCooldownConfig
	states     map[[32]byte]*failureState
}

// ConfigureFailureCooldown updates limits and discards previous failure state.
// It is safe to call concurrently, although application settings use it at startup.
func (s *ExecutionService) ConfigureFailureCooldown(cfg FailureCooldownConfig) {
	s.failureGuard.mu.Lock()
	defer s.failureGuard.mu.Unlock()
	s.failureGuard.config = cfg
	s.failureGuard.states = nil
	s.failureGuard.generation++
}

// ConfigureFailureCooldown configures repeated-call protection for local MCP tools.
func (s *Server) ConfigureFailureCooldown(cfg FailureCooldownConfig) {
	s.executionService.ConfigureFailureCooldown(cfg)
}

// ConfigureFailureCooldown configures repeated-call protection for external MCP tools.
func (m *ExternalMCPManager) ConfigureFailureCooldown(cfg FailureCooldownConfig) {
	m.executionService.ConfigureFailureCooldown(cfg)
}

// admit scopes state by owner, conversation, tool and canonical JSON arguments.
// It retains only digests, limits memory to 4096 entries, and never retries work.
// The completion hook must receive the final execution status exactly once.
func (guard *failureCooldown) admit(exec *ToolExecution) (func(string), *ToolResult) {
	noop := func(string) {}
	if exec.ConversationID == "" {
		return noop, nil
	}
	raw, err := json.Marshal([]interface{}{exec.OwnerUserID, exec.ConversationID, exec.ToolName, exec.Arguments})
	if err != nil {
		return noop, nil
	}
	key := sha256.Sum256(raw)
	guard.mu.Lock()
	defer guard.mu.Unlock()
	generation := guard.generation
	cfg := guard.config
	if cfg.Threshold < 0 {
		return noop, nil
	}
	if cfg.Threshold == 0 {
		cfg.Threshold = 2
	}
	if cfg.WindowSeconds <= 0 {
		cfg.WindowSeconds = 60
	}
	if cfg.CooldownSeconds <= 0 {
		cfg.CooldownSeconds = 60
	}
	window := time.Duration(cfg.WindowSeconds) * time.Second
	cooldown := time.Duration(cfg.CooldownSeconds) * time.Second
	now := time.Now()
	if guard.states == nil {
		guard.states = make(map[[32]byte]*failureState)
	}
	for id, state := range guard.states {
		if !state.inFlight && !now.Before(state.until) && now.Sub(state.firstFailure) > window+cooldown {
			delete(guard.states, id)
		}
	}
	state := guard.states[key]
	if state != nil && (state.inFlight || now.Before(state.until)) {
		message := "tool_failure_cooldown: 相同调用正在恢复探测，本次未执行。不要重复提交；继续其他独立步骤，或汇报阻塞原因。"
		if now.Before(state.until) {
			message = fmt.Sprintf("tool_failure_cooldown: 相同工具及参数已连续失败，本次未执行；至少等待 %.0f 秒。不要立即重试或换执行器绕过；继续其他独立步骤，或汇报阻塞原因。", math.Ceil(state.until.Sub(now).Seconds()))
		}
		return nil, &ToolResult{IsError: true, Blocked: true, Content: []Content{{Type: "text", Text: message}}}
	}
	if state != nil {
		state.inFlight = true
	}
	return func(status string) {
		guard.mu.Lock()
		defer guard.mu.Unlock()
		if guard.generation != generation {
			return
		}
		current := guard.states[key]
		if state != nil && current != state {
			return
		}
		if state != nil {
			state.inFlight = false
		}
		failed := status == ToolExecutionStatusFailed || status == ToolExecutionStatusHardTimeout || status == ToolExecutionStatusOrphaned
		if !failed {
			if status == ToolExecutionStatusCompleted && current == state {
				delete(guard.states, key)
			}
			return
		}
		now := time.Now()
		// Ignore completions already in flight before a newer circuit opened.
		if current != nil && (now.Before(current.until) || (state == nil && current.inFlight)) {
			return
		}
		if current == nil {
			if len(guard.states) >= 4096 {
				return
			}
			current = &failureState{firstFailure: now}
			guard.states[key] = current
		}
		if current.until.IsZero() && now.Sub(current.firstFailure) > window {
			current.failures = 0
			current.firstFailure = now
		}
		current.failures++
		if current.failures >= cfg.Threshold {
			current.until = now.Add(cooldown)
		}
	}, nil
}
