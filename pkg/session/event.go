// Package session holds conversation state as an append-only event log.
//
// The log is the source of truth for the context the model sees. Model history
// is projected from it via DeriveMessages, so fork, resume, transcripts and
// audit all derive from the same durable facts.
package session

import (
	"time"

	"coren/pkg/llm"
)

// EventType identifies a durable session fact.
type EventType string

const (
	EventTurnStart  EventType = "turn/start"
	EventUserMsg    EventType = "user/message"
	EventAssistant  EventType = "assistant/message"
	EventToolCall   EventType = "tool/call"
	EventToolResult EventType = "tool/result"
	EventStepEnd    EventType = "step/end"
	EventTurnEnd    EventType = "turn/end"
	// EventCompaction records a summary that replaces prior history in the model
	// projection, bounding token use on long conversations.
	EventCompaction EventType = "session/compaction"
)

// Event is one durable fact appended to the session log.
type Event struct {
	// Type classifies the event.
	Type EventType `json:"type"`
	// Seq is the monotonic sequence number within the session.
	Seq int64 `json:"seq"`
	// Time is when the event was appended.
	Time time.Time `json:"time"`
	// Turn groups events that belong to the same user turn.
	Turn int64 `json:"turn,omitempty"`
	// Text carries user/assistant text or tool output.
	Text string `json:"text,omitempty"`
	// Parts carries multimodal content for tool results (images, audio, video).
	Parts []llm.ContentPart `json:"parts,omitempty"`
	// ToolCalls carries assistant tool requests (assistant/message).
	ToolCalls []llm.ToolCall `json:"tool_calls,omitempty"`
	// CallID links tool/call and tool/result to a specific invocation.
	CallID string `json:"call_id,omitempty"`
	// Name is the tool name for tool events.
	Name string `json:"name,omitempty"`
	// Arguments is the raw JSON for tool/call.
	Arguments string `json:"arguments,omitempty"`
	// Error records a tool failure.
	Error string `json:"error,omitempty"`
	// Usage is set on assistant/message when the provider reports it.
	Usage *llm.Usage `json:"usage,omitempty"`
}
