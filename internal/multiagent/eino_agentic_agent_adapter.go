package multiagent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type einoAgenticMessageAgentAdapter struct {
	inner adk.TypedAgent[*schema.AgenticMessage]
}

// The outer supervisor runs classic agents, while the inner AgenticMessage
// ChatModelAgent uses typed agents only to describe transfer destinations.
// Eino's flow runner remains responsible for executing the classic agents.
type einoAgenticTransferReference struct {
	agent adk.Agent
}

type einoAgenticSubAgentSetter interface {
	OnSetSubAgents(context.Context, []adk.TypedAgent[*schema.AgenticMessage]) error
	OnSetAsSubAgent(context.Context, adk.TypedAgent[*schema.AgenticMessage]) error
	OnDisallowTransferToParent(context.Context) error
}

var _ adk.OnSubAgents = (*einoAgenticMessageAgentAdapter)(nil)

func (r *einoAgenticTransferReference) Name(ctx context.Context) string {
	return r.agent.Name(ctx)
}

func (r *einoAgenticTransferReference) Description(ctx context.Context) string {
	return r.agent.Description(ctx)
}

func (r *einoAgenticTransferReference) Run(context.Context, *adk.TypedAgentInput[*schema.AgenticMessage], ...adk.AgentRunOption) *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	go func() {
		defer gen.Close()
		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{Err: fmt.Errorf("agentic transfer reference cannot run directly")})
	}()
	return iter
}

func newEinoAgenticMessageAgentAdapter(inner adk.TypedAgent[*schema.AgenticMessage]) adk.Agent {
	if inner == nil {
		return nil
	}
	return &einoAgenticMessageAgentAdapter{inner: inner}
}

func (a *einoAgenticMessageAgentAdapter) Name(ctx context.Context) string {
	if a == nil || a.inner == nil {
		return ""
	}
	return a.inner.Name(ctx)
}

func (a *einoAgenticMessageAgentAdapter) Description(ctx context.Context) string {
	if a == nil || a.inner == nil {
		return ""
	}
	return a.inner.Description(ctx)
}

// OnSetSubAgents forwards the supervisor's transfer destinations to the typed
// model agent. The classic flow runner executes the real destination agents.
func (a *einoAgenticMessageAgentAdapter) OnSetSubAgents(ctx context.Context, subAgents []adk.Agent) error {
	setter, err := a.subAgentSetter()
	if err != nil {
		return err
	}
	typed := make([]adk.TypedAgent[*schema.AgenticMessage], 0, len(subAgents))
	for _, subAgent := range subAgents {
		if subAgent == nil {
			return fmt.Errorf("agentic adapter: sub-agent is nil")
		}
		typed = append(typed, &einoAgenticTransferReference{agent: subAgent})
	}
	return setter.OnSetSubAgents(ctx, typed)
}

// OnSetAsSubAgent forwards parent registration so a typed agent can expose a
// return transfer when it is directly used in a classic flow.
func (a *einoAgenticMessageAgentAdapter) OnSetAsSubAgent(ctx context.Context, parent adk.Agent) error {
	setter, err := a.subAgentSetter()
	if err != nil {
		return err
	}
	if parent == nil {
		return fmt.Errorf("agentic adapter: parent agent is nil")
	}
	return setter.OnSetAsSubAgent(ctx, &einoAgenticTransferReference{agent: parent})
}

// OnDisallowTransferToParent preserves the outer flow's parent-transfer rule
// inside the typed model agent.
func (a *einoAgenticMessageAgentAdapter) OnDisallowTransferToParent(ctx context.Context) error {
	setter, err := a.subAgentSetter()
	if err != nil {
		return err
	}
	return setter.OnDisallowTransferToParent(ctx)
}

func (a *einoAgenticMessageAgentAdapter) subAgentSetter() (einoAgenticSubAgentSetter, error) {
	if a == nil || a.inner == nil {
		return nil, fmt.Errorf("agentic adapter: inner agent is nil")
	}
	setter, ok := a.inner.(einoAgenticSubAgentSetter)
	if !ok {
		return nil, fmt.Errorf("agentic adapter: inner agent does not support sub-agent transfer")
	}
	return setter, nil
}

func (a *einoAgenticMessageAgentAdapter) Run(ctx context.Context, input *adk.AgentInput, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	return a.runTyped(ctx, input, nil, opts...)
}

func (a *einoAgenticMessageAgentAdapter) Resume(ctx context.Context, info *adk.ResumeInfo, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	return a.runTyped(ctx, nil, info, opts...)
}

func (a *einoAgenticMessageAgentAdapter) runTyped(ctx context.Context, input *adk.AgentInput, resumeInfo *adk.ResumeInfo, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		if a == nil || a.inner == nil {
			gen.Send(&adk.AgentEvent{Err: fmt.Errorf("agentic adapter: inner agent is nil")})
			return
		}
		var agenticIter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]
		if resumeInfo != nil {
			resumable, ok := a.inner.(adk.TypedResumableAgent[*schema.AgenticMessage])
			if !ok {
				gen.Send(&adk.AgentEvent{Err: fmt.Errorf("agentic adapter: inner agent does not support resume")})
				return
			}
			agenticIter = resumable.Resume(ctx, resumeInfo, opts...)
		} else {
			agenticInput := &adk.TypedAgentInput[*schema.AgenticMessage]{}
			if input != nil {
				agenticInput.EnableStreaming = input.EnableStreaming
				agenticInput.Messages = EinoMessagesToAgentic(input.Messages)
			}
			agenticIter = a.inner.Run(ctx, agenticInput, opts...)
		}
		for {
			ev, ok := agenticIter.Next()
			if !ok {
				return
			}
			for _, adapted := range adaptAgenticEventToEinoEvents(ev) {
				if adapted != nil {
					gen.Send(adapted)
				}
			}
		}
	}()
	return iter
}
