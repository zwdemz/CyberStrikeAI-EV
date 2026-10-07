package multiagent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/supervisor"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type capturingAgenticChatModel struct {
	mu      sync.Mutex
	inputs  [][]*schema.AgenticMessage
	output  *schema.AgenticMessage
	outputs []*schema.AgenticMessage
}

func (m *capturingAgenticChatModel) Generate(_ context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	m.mu.Lock()
	m.inputs = append(m.inputs, input)
	callNo := len(m.inputs)
	m.mu.Unlock()
	if len(m.outputs) > 0 {
		idx := callNo - 1
		if idx >= len(m.outputs) {
			idx = len(m.outputs) - 1
		}
		return m.outputs[idx], nil
	}
	if m.output != nil {
		return m.output, nil
	}
	return &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "agentic answer"})},
	}, nil
}

func (m *capturingAgenticChatModel) Stream(_ context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(context.Background(), input)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.AgenticMessage{msg}), nil
}

func (m *capturingAgenticChatModel) snapshotInputs() [][]*schema.AgenticMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]*schema.AgenticMessage, len(m.inputs))
	copy(out, m.inputs)
	return out
}

func TestNewEinoAgenticChatModelAgentAdapterRunsThroughClassicAgentBoundary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	trace := newModelFacingTraceHolder()
	fakeModel := &capturingAgenticChatModel{}
	agent, err := newEinoAgenticChatModelAgentAdapter(ctx, einoAgenticChatModelAgentConfig{
		Name:        "agentic",
		Description: "agentic adapter test",
		Instruction: "system instruction",
		Model:       fakeModel,
		Handlers: appendEinoAgenticChatModelTailMiddlewares(nil, einoChatModelTailConfig{
			phase: "agentic",
			trace: trace,
		}),
	})
	if err != nil {
		t.Fatalf("newEinoAgenticChatModelAgentAdapter: %v", err)
	}

	iter := agent.Run(ctx, &adk.AgentInput{
		Messages: []*schema.Message{schema.UserMessage("classic input")},
	})
	var last *adk.AgentEvent
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			t.Fatalf("agent event error: %v", ev.Err)
		}
		last = ev
	}
	if last == nil || last.Output == nil || last.Output.MessageOutput == nil {
		t.Fatalf("last event = %#v, want message output", last)
	}
	if got := last.Output.MessageOutput.Message.Content; got != "agentic answer" {
		t.Fatalf("classic output content = %q, want agentic answer", got)
	}

	inputs := fakeModel.snapshotInputs()
	if len(inputs) != 1 {
		t.Fatalf("model calls = %d, want 1", len(inputs))
	}
	if len(inputs[0]) != 2 {
		t.Fatalf("model input messages = %d, want instruction + user", len(inputs[0]))
	}
	if inputs[0][0].Role != schema.AgenticRoleTypeSystem || agenticMessageText(inputs[0][0]) != "system instruction" {
		t.Fatalf("first agentic input = %#v", inputs[0][0])
	}
	if inputs[0][1].Role != schema.AgenticRoleTypeUser || agenticMessageText(inputs[0][1]) != "classic input" {
		t.Fatalf("second agentic input = %#v", inputs[0][1])
	}

	snapshot := trace.Snapshot()
	if len(snapshot) != 2 || snapshot[0].Role != schema.System || snapshot[1].Role != schema.User {
		t.Fatalf("trace snapshot = %#v, want classic system + user trace", snapshot)
	}
}

func TestAgenticSupervisorTransfersToConfiguredSubAgent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	worker := &fakeAgenticMessageAgent{
		name: "worker", description: "Handles a delegated task",
		events: []*adk.TypedAgentEvent[*schema.AgenticMessage]{{
			AgentName: "worker",
			Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
				Message: &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant,
					ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "worker completed"})}},
			}},
		}},
	}
	model := &capturingAgenticChatModel{outputs: []*schema.AgenticMessage{
		{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.FunctionToolCall{
				CallID: "transfer-1", Name: adk.TransferToAgentToolName, Arguments: `{"agent_name":"worker"}`,
			}),
		}},
		{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "delegation completed"}),
		}},
	}}
	root, err := newEinoAgenticChatModelAgentAdapter(ctx, einoAgenticChatModelAgentConfig{
		Name: "supervisor", Description: "Delegates tasks", Model: model, MaxIterations: 4,
	})
	if err != nil {
		t.Fatalf("create supervisor model agent: %v", err)
	}
	flow, err := supervisor.New(ctx, &supervisor.Config{
		Supervisor: root,
		SubAgents:  []adk.Agent{newEinoAgenticMessageAgentAdapter(worker)},
	})
	if err != nil {
		t.Fatalf("create supervisor flow: %v", err)
	}
	iter := flow.Run(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("delegate a task")}})
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatalf("supervisor event: %v", event.Err)
		}
	}
	if worker.captured == nil {
		t.Fatal("transfer_to_agent did not invoke the configured sub-agent")
	}
	inputs := model.snapshotInputs()
	if len(inputs) < 2 || len(inputs[0]) == 0 ||
		!strings.Contains(agenticMessageText(inputs[0][0]), adk.TransferToAgentToolName) {
		t.Fatalf("supervisor model input lacks transfer instruction: %#v", inputs)
	}
}

func TestNewEinoAgenticChatModelAgentAdapterPreservesTypedToolCallsForToolLayerRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fakeModel := &capturingAgenticChatModel{
		output: &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.FunctionToolCall{
				CallID:    "call-1",
				Name:      "exec",
				Arguments: `{"command":"` + strings.Repeat("x", 20000) + `"}`,
			})},
		},
	}
	agent, err := newEinoAgenticChatModelAgentAdapter(ctx, einoAgenticChatModelAgentConfig{
		Name:        "agentic",
		Description: "agentic adapter test",
		Model:       fakeModel,
		Handlers: appendEinoAgenticChatModelTailMiddlewares(nil, einoChatModelTailConfig{
			phase: "agentic",
		}),
	})
	if err != nil {
		t.Fatalf("newEinoAgenticChatModelAgentAdapter: %v", err)
	}
	iter := agent.Run(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("run")}})
	var last *adk.AgentEvent
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			t.Fatalf("agent event error: %v", ev.Err)
		}
		last = ev
	}
	if last == nil || last.Output == nil || last.Output.MessageOutput == nil {
		t.Fatalf("last event = %#v, want message output", last)
	}
	msg := last.Output.MessageOutput.Message
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v, want one tool call", msg.ToolCalls)
	}
	args := msg.ToolCalls[0].Function.Arguments
	if !strings.Contains(args, strings.Repeat("x", 32)) || strings.Contains(args, modelOutputRecoveryKey) {
		t.Fatalf("agentic tool args were unexpectedly rewritten: %q", args)
	}
}

func TestNewEinoAgenticChatModelAgentAdapterRequiresModel(t *testing.T) {
	t.Parallel()
	if _, err := newEinoAgenticChatModelAgentAdapter(context.Background(), einoAgenticChatModelAgentConfig{}); err == nil {
		t.Fatal("expected missing model error")
	}
}
