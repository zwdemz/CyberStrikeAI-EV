package mcp

import (
	"fmt"
	"math"
	"time"

	"go.uber.org/zap"
)

// externalMCPCircuitError distinguishes admission refusal from a provider call
// failure. It carries no provider output, arguments or credentials.
type externalMCPCircuitError struct {
	retryAfter time.Duration
	probe      bool
}

func (e *externalMCPCircuitError) Error() string {
	if e.probe {
		return "mcp_circuit_open: 外部 MCP 正在进行单次恢复探测，本次调用未发送。不要重复提交，请继续不依赖该服务的步骤。"
	}
	return fmt.Sprintf("mcp_circuit_open: 外部 MCP 已临时熔断，本次调用未发送；至少等待 %.0f 秒。不要立即重试或通过其他执行器绕过，请继续不依赖该服务的步骤；若无可继续步骤，汇报阻塞原因。", math.Ceil(e.retryAfter.Seconds()))
}

func externalMCPCircuitResult(err error) *ToolResult {
	return &ToolResult{Content: []Content{{Type: "text", Text: err.Error()}}, IsError: true, Blocked: true}
}

// checkExternalMCPCircuit is a non-reserving fast check before queueing. Admission
// must be checked again after obtaining a slot, using admitExternalMCPCall.
func (m *ExternalMCPManager) checkExternalMCPCircuit(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.resilience.CircuitFailureThreshold < 0 {
		return nil
	}
	runtime := m.externalMCPRuntimeLocked(name)
	if runtime == nil {
		return nil
	}
	return externalCircuitRefusal(runtime, time.Now())
}

func externalCircuitRefusal(runtime *externalMCPServerRuntime, now time.Time) error {
	if runtime.probeInFlight {
		return &externalMCPCircuitError{probe: true}
	}
	if now.Before(runtime.circuitOpenUntil) {
		return &externalMCPCircuitError{retryAfter: runtime.circuitOpenUntil.Sub(now)}
	}
	return nil
}

// admitExternalMCPCall reserves one recovery probe after cooldown. Its completion
// callback accepts failed/cancelled outcomes and is called exactly once by Run.
// Results admitted before an opening cannot close or extend that newer circuit.
func (m *ExternalMCPManager) admitExternalMCPCall(name string) (func(bool, bool), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.resilience.CircuitFailureThreshold < 0 {
		return func(bool, bool) {}, nil
	}
	runtime := m.externalMCPRuntimeLocked(name)
	if err := externalCircuitRefusal(runtime, time.Now()); err != nil {
		return nil, err
	}
	probe := !runtime.circuitOpenUntil.IsZero()
	if probe {
		runtime.probeInFlight = true
	}
	generation := runtime.circuitGeneration
	return func(failed, cancelled bool) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.serverRuntimes[name] != runtime || runtime.circuitGeneration != generation {
			return
		}
		if cancelled {
			if probe {
				runtime.probeInFlight = false
			}
			return
		}
		runtime.probeInFlight = false
		if !failed {
			runtime.consecutiveFailures = 0
			runtime.circuitOpenUntil = time.Time{}
			return
		}
		runtime.consecutiveFailures++
		if probe || runtime.consecutiveFailures >= m.resilience.CircuitFailureThreshold {
			runtime.circuitOpenUntil = time.Now().Add(m.resilience.CircuitCooldown)
			runtime.circuitGeneration++
			m.logger.Warn("外部MCP服务触发熔断", zap.String("name", name), zap.Int("consecutiveFailures", runtime.consecutiveFailures), zap.Duration("cooldown", m.resilience.CircuitCooldown))
		}
	}, nil
}
