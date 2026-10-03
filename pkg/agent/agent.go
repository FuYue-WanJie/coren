// Package agent implements the agent loop that drives a provider and tools.
//
// The loop resolves the llm and tools services from the kernel context at send
// time, so a plugin swap changes behaviour without touching this package.
package agent

import (
	"context"
	"coren/pkg/modelinfo"
	"fmt"
	"strings"
	"time"

	"coren/pkg/coren"
	"coren/pkg/llm"
	"coren/pkg/session"
	"coren/pkg/tools"
)

// Agent runs a conversation against services resolved from a kernel context.
type Agent struct {
	// Context is the kernel context used to resolve services.
	Context coren.Context
	// Model names the llm adapter to use; empty uses the adapter name.
	Model       string
	AdapterName string
	System      string
	Temperature *float64
	MaxTokens   *int
	// MaxSteps bounds tool-call round trips per turn; zero means a 12-step default.
	MaxSteps int
	// CacheRetention enables prompt caching: "" or "short" (default), "long", or "none".
	CacheRetention string
	// CompactAfter, when > 0, summarizes history once it exceeds this many
	// projected messages, bounding token use on long sessions.
	CompactAfter int
	// ModelInfo carries the resolved model capabilities. When zero-valued, the
	// loop assumes tools and text are supported.
	ModelInfo modelinfo.Info
	// ReasoningLevel controls how much the model thinks. Empty means auto.
	ReasoningLevel llm.ReasoningLevel
	// ToolTimeout bounds each tool call; zero uses a 120s default.
	ToolTimeout time.Duration
}

// Event is emitted as a turn progresses.
type Event struct {
	TextDelta string
	// ReasoningDelta carries streamed model thinking, when exposed by the
	// provider. It is display-only and not appended to the assistant message.
	ReasoningDelta string
	ToolCallStart  *ToolCallEvent
	ToolCallResult *ToolResultEvent
	// Rejected is set when the agent/pre-step waterfall declined the input.
	Rejected *Rejection
	Done     bool
	// Paused reports that the turn stopped to await user approval of a
	// deliverable, rather than completing normally.
	Paused bool
	Usage  *llm.Usage
	Err    error
}

// ToolCallEvent describes a tool invocation that is starting.
type ToolCallEvent struct {
	ID        string
	Name      string
	Arguments string
}

// ToolResultEvent describes a completed tool invocation.
type ToolResultEvent struct {
	ID     string
	Name   string
	Output string
	Err    error
}

// Send appends the user message to the session and runs the loop, streaming events.
// The input passes through the agent/pre-step waterfall first: a listener may
// rewrite or reject it. A rejected input closes the turn without any step.
func (a *Agent) Send(ctx context.Context, sess session.Session, input string) <-chan Event {
	events := make(chan Event)
	go func() {
		defer close(events)
		accepted, err := a.preStep(ctx, sess, input)
		if err != nil {
			events <- Event{Err: err}
			return
		}
		if !accepted.Accepted {
			if accepted.Reason != "" {
				events <- Event{Rejected: &Rejection{Reason: accepted.Reason}}
			}
			events <- Event{Done: true}
			return
		}
		sess.Append(session.Event{Type: session.EventTurnStart})
		sess.SetStatus(session.Active)
		sess.Append(session.Event{Type: session.EventUserMsg, Text: accepted.Input})
		if a.CompactAfter > 0 {
			if err := a.compact(ctx, sess); err != nil {
				events <- Event{Err: err}
				return
			}
		}
		if err := a.run(ctx, sess, events); err != nil {
			events <- Event{Err: err}
		}
	}()
	return events
}

// compact summarizes older history when the projection exceeds CompactAfter
// messages, appending a session/compaction event that supersedes it.
func (a *Agent) compact(ctx context.Context, sess session.Session) error {
	messages := sess.Messages()
	if len(messages) <= a.CompactAfter {
		return nil
	}
	adapter, err := a.adapter()
	if err != nil {
		return err
	}
	summary, err := a.summarize(ctx, adapter, messages)
	if err != nil {
		return err
	}
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	sess.Append(session.Event{Type: session.EventCompaction, Text: summary})
	return nil
}

// summarize asks the model to condense a conversation into durable facts.
func (a *Agent) summarize(ctx context.Context, adapter llm.Adapter, messages []llm.Message) (string, error) {
	var transcript strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&transcript, "%s: %s\n", m.Role, m.Text)
	}
	req := llm.Request{
		Model: a.Model,
		System: "You compress conversations. Return only a terse summary that preserves decisions, " +
			"facts, file paths, and open tasks; omit pleasantries.",
		Messages: []llm.Message{{
			Role: llm.RoleUser,
			Text: "Summarize this conversation:\n\n" + transcript.String(),
		}},
	}
	stream, err := adapter.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for chunk := range stream {
		if chunk.Err != nil {
			return "", chunk.Err
		}
		out.WriteString(chunk.TextDelta)
	}
	return strings.TrimSpace(out.String()), nil
}

// PreStepResult is the decision produced by the agent/pre-step waterfall.
type PreStepResult struct {
	// Accepted reports whether the input should proceed to a step.
	Accepted bool
	// Reason explains a rejection; surfaced to the caller as a Rejection event.
	Reason string
	// Input is the possibly-rewritten user input.
	Input string
}

// preStep dispatches agent/pre-step. With no listeners, input is accepted as-is.
func (a *Agent) preStep(ctx context.Context, sess session.Session, input string) (PreStepResult, error) {
	result := PreStepResult{Accepted: true, Input: input}
	if a.Context == nil {
		return result, nil
	}
	out, err := a.Context.Waterfall(coren.EventAgentPreStep, &result)
	if err != nil {
		return result, err
	}
	if decision, ok := out.(*PreStepResult); ok {
		return *decision, nil
	}
	return result, nil
}

func (a *Agent) maxSteps() int {
	if a.MaxSteps > 0 {
		return a.MaxSteps
	}
	return 12
}

// effectiveReasoning resolves the reasoning level to send.
//
// Explicit user choices win. On auto, reasoning is requested only when the model
// is known to support it, using "medium" as a balanced default; a model without
// reasoning support gets none. Unknown models (no info) stay on auto without a
// level, letting the provider decide.
func (a *Agent) effectiveReasoning() llm.ReasoningLevel {
	switch a.ReasoningLevel {
	case llm.ReasoningAuto, "":
		if a.ModelInfo.ID != "" && a.ModelInfo.Reasoning {
			return llm.ReasoningMedium
		}
		return llm.ReasoningAuto
	default:
		return a.ReasoningLevel
	}
}

func (a *Agent) run(ctx context.Context, sess session.Session, events chan<- Event) error {
	adapter, err := a.adapter()
	if err != nil {
		return err
	}
	toolService, err := a.toolService()
	if err != nil {
		return err
	}

	// A rejected or pending deliverable pauses the turn: the scan flag is set by
	// the delivery plugin's event and checked after each tool call.
	var deliveryPending bool
	if a.Context != nil {
		disposer := a.Context.On(coren.DeliveryPending, func(context.Context, any) {
			deliveryPending = true
		})
		defer disposer()
	}

	for step := 0; step < a.maxSteps(); step++ {
		assistant, usage, err := a.streamOnce(ctx, adapter, toolService, sess, events)
		if err != nil {
			return err
		}
		sess.Append(session.Event{
			Type:      session.EventAssistant,
			Text:      assistant.Text,
			ToolCalls: assistant.ToolCalls,
			Usage:     usage,
		})

		if len(assistant.ToolCalls) == 0 {
			sess.Append(session.Event{Type: session.EventStepEnd})
			sess.Append(session.Event{Type: session.EventTurnEnd})
			if usage != nil && a.Context != nil {
				a.Context.Emit(coren.EventModelUsage, usage)
			}
			events <- Event{Done: true, Usage: usage}
			return nil
		}

		for _, call := range assistant.ToolCalls {
			sess.Append(session.Event{
				Type:      session.EventToolCall,
				CallID:    call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
			})
			events <- Event{ToolCallStart: &ToolCallEvent{ID: call.ID, Name: call.Name, Arguments: call.Arguments}}

			args, err := a.interceptExecute(ctx, call.Name, call.Arguments)
			if err != nil {
				events <- Event{ToolCallResult: &ToolResultEvent{ID: call.ID, Name: call.Name, Err: err}}
				sess.Append(session.Event{
					Type:   session.EventToolResult,
					CallID: call.ID,
					Name:   call.Name,
					Error:  err.Error(),
				})
				continue
			}

			result, runErr := a.runTool(ctx, toolService, call.Name, args)
			result.Text = a.processResult(ctx, call.Name, result.Text, runErr)
			events <- Event{ToolCallResult: &ToolResultEvent{ID: call.ID, Name: call.Name, Output: result.Text, Err: runErr}}
			resultEvent := session.Event{
				Type:   session.EventToolResult,
				CallID: call.ID,
				Name:   call.Name,
				Text:   result.Text,
				Parts:  result.Parts,
			}
			if runErr != nil {
				resultEvent.Error = runErr.Error()
			}
			sess.Append(resultEvent)
		}
		sess.Append(session.Event{Type: session.EventStepEnd})

		// A pending deliverable pauses the turn: stop now and wait for the user,
		// rather than continuing to act on an unapproved artifact.
		if deliveryPending {
			sess.SetStatus(session.AwaitingApproval)
			sess.Append(session.Event{Type: session.EventTurnEnd})
			events <- Event{Done: true, Paused: true}
			return nil
		}
	}

	// Step budget exhausted: give turn-stopping listeners a say before failing.
	if a.Context != nil {
		if _, err := a.Context.Serial(coren.EventAgentTurnStopping, &TurnStoppingPayload{
			Reason: TurnStoppingMaxSteps,
			Steps:  a.maxSteps(),
		}); err != nil {
			return err
		}
	}
	sess.Append(session.Event{Type: session.EventTurnEnd})
	return fmt.Errorf("exceeded %d steps without finishing", a.maxSteps())
}

// TurnStoppingReason explains why a turn is about to stop abnormally.
type TurnStoppingReason string

const (
	TurnStoppingMaxSteps TurnStoppingReason = "max_steps"
)

// TurnStoppingPayload is the payload for agent/turn-stopping.
type TurnStoppingPayload struct {
	Reason TurnStoppingReason
	Steps  int
}

// Rejection reports that the agent/pre-step waterfall declined the input.
type Rejection struct {
	Reason string
}

// runTool executes a tool with the model info attached and a timeout applied.
func (a *Agent) runTool(ctx context.Context, toolService tools.Service, name, args string) (tools.Result, error) {
	runCtx := llm.WithModelInfo(ctx, a.ModelInfo)
	timeout := a.ToolTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	runCtx, cancel := context.WithTimeout(runCtx, timeout)
	defer cancel()

	result, err := toolService.Run(runCtx, name, args)
	if runCtx.Err() == context.DeadlineExceeded {
		return result, fmt.Errorf("tool %q timed out after %s", name, timeout)
	}
	return result, err
}

// interceptExecute runs the tools/pre-execute waterfall, which may rewrite
// arguments or reject the call.
func (a *Agent) interceptExecute(ctx context.Context, name, arguments string) (string, error) {
	if a.Context == nil {
		return arguments, nil
	}
	result, err := a.Context.Waterfall(coren.EventToolsPreExecute, &PreExecutePayload{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		return "", err
	}
	if payload, ok := result.(*PreExecutePayload); ok {
		return payload.Arguments, nil
	}
	return arguments, nil
}

// processResult runs the tools/post-execute waterfall, which may rewrite output.
func (a *Agent) processResult(ctx context.Context, name, output string, runErr error) string {
	if a.Context == nil {
		return output
	}
	result, err := a.Context.Waterfall(coren.EventToolsPostExecute, &PostExecutePayload{
		Name:   name,
		Output: output,
		Err:    runErr,
	})
	if err != nil {
		return output
	}
	if payload, ok := result.(*PostExecutePayload); ok {
		return payload.Output
	}
	return output
}

// streamOnce runs one provider turn, forwarding text deltas and collecting tool calls.
func (a *Agent) streamOnce(
	ctx context.Context,
	adapter llm.Adapter,
	toolService tools.Service,
	sess session.Session,
	events chan<- Event,
) (llm.Message, *llm.Usage, error) {
	req := llm.Request{
		Model:          a.Model,
		System:         a.System,
		Messages:       sess.Messages(),
		Temperature:    a.Temperature,
		MaxTokens:      a.MaxTokens,
		SessionID:      sess.ID(),
		CacheRetention: a.CacheRetention,
		Reasoning:      a.effectiveReasoning(),
	}
	// Only advertise tools when the model is known to support them. An unset
	// capability (zero Info) is treated as "assume supported" for safety.
	if a.ModelInfo.ID == "" || a.ModelInfo.ToolCall {
		req.Tools = toolService.Specs()
	}
	// Respect the model's output limit when the caller did not set one.
	if req.MaxTokens == nil && a.ModelInfo.Limit.Output > 0 {
		limit := a.ModelInfo.Limit.Output
		req.MaxTokens = &limit
	}

	req = a.interceptRequest(ctx, req)

	stream, err := adapter.Stream(ctx, req)
	if err != nil {
		return llm.Message{}, nil, err
	}

	assistant := llm.Message{Role: llm.RoleAssistant}
	var usage *llm.Usage
	for chunk := range stream {
		switch {
		case chunk.Err != nil:
			return llm.Message{}, nil, chunk.Err
		case chunk.TextDelta != "":
			assistant.Text += chunk.TextDelta
			events <- Event{TextDelta: chunk.TextDelta}
		case chunk.ReasoningDelta != "":
			events <- Event{ReasoningDelta: chunk.ReasoningDelta}
		case chunk.ToolCall != nil:
			assistant.ToolCalls = append(assistant.ToolCalls, *chunk.ToolCall)
		case chunk.Done:
			usage = chunk.Usage
		}
	}
	return assistant, usage, nil
}

// interceptRequest runs the agent/request waterfall, which may rewrite the request.
func (a *Agent) interceptRequest(ctx context.Context, req llm.Request) llm.Request {
	if a.Context == nil {
		return req
	}
	result, err := a.Context.Waterfall(coren.EventAgentRequest, &RequestPayload{Request: req})
	if err != nil {
		return req
	}
	if payload, ok := result.(*RequestPayload); ok {
		return payload.Request
	}
	return req
}

func (a *Agent) adapter() (llm.Adapter, error) {
	if a.Context == nil {
		return nil, fmt.Errorf("agent: no kernel context")
	}
	service, ok := coren.UnwrapKey[llm.Service](a.Context, llm.Key)
	if !ok {
		return nil, fmt.Errorf("agent: llm service not available")
	}
	name := a.AdapterName
	if name == "" {
		names := service.Names()
		if len(names) == 0 {
			return nil, fmt.Errorf("agent: no llm adapter registered")
		}
		name = names[0]
	}
	adapter, ok := service.Get(name)
	if !ok {
		return nil, fmt.Errorf("agent: adapter %q not found", name)
	}
	return adapter, nil
}

func (a *Agent) toolService() (tools.Service, error) {
	if a.Context == nil {
		return nil, fmt.Errorf("agent: no kernel context")
	}
	service, ok := coren.UnwrapKey[tools.Service](a.Context, tools.Key)
	if !ok {
		return nil, fmt.Errorf("agent: tools service not available")
	}
	return service, nil
}

// PreExecutePayload is the payload for the tools/pre-execute waterfall.
type PreExecutePayload struct {
	Name      string
	Arguments string
}

// PostExecutePayload is the payload for the tools/post-execute waterfall.
type PostExecutePayload struct {
	Name   string
	Output string
	Err    error
}

// RequestPayload is the payload for the agent/request waterfall.
type RequestPayload struct {
	Request llm.Request
}
