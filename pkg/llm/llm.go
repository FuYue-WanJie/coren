// Package llm defines the model-backend contract and the llm service.
//
// Plugins register adapters on the service; the agent loop resolves an adapter
// by name and streams a request through it. Nothing here imports a concrete
// backend, so swapping providers is a plugin swap.
package llm

import (
	"context"
	"strings"
)

// Role identifies who produced a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one item in a conversation.
type Message struct {
	Role Role `json:"role"`
	// Text is the natural-language content.
	Text string `json:"text,omitempty"`
	// Parts carries multimodal content (images, audio, video) alongside or
	// instead of Text. Used for tool results that return media.
	Parts []ContentPart `json:"parts,omitempty"`
	// ToolCalls requested by the assistant.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID links a tool result back to the call that produced it.
	ToolCallID string `json:"tool_call_id,omitempty"`
	// Name is the tool name for tool-result messages.
	Name string `json:"name,omitempty"`
}

// ContentPart is one piece of multimodal message content.
type ContentPart struct {
	// Type is "text", "image", "audio", or "video".
	Type string `json:"type"`
	// Text is set for text parts.
	Text string `json:"text,omitempty"`
	// MimeType is the media MIME type, e.g. image/png.
	MimeType string `json:"mime_type,omitempty"`
	// Data is base64-encoded media bytes.
	Data string `json:"data,omitempty"`
	// URI references remote media instead of inline Data.
	URI string `json:"uri,omitempty"`
}

// Part type constants.
const (
	PartText  = "text"
	PartImage = "image"
	PartAudio = "audio"
	PartVideo = "video"
)

// ToolCall is a model request to invoke a tool.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // raw JSON object
}

// ToolSpec describes a tool the model may call.
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema
}

// Request is a single model invocation.
type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolSpec
	Temperature *float64
	MaxTokens   *int
	// SessionID enables provider prompt caching. Providers that support a cache
	// key (OpenAI) reuse it across requests with the same prefix; others ignore
	// it. Keeping the request prefix stable is what makes caching effective.
	SessionID string
	// CacheRetention is "short" (default), "long", or "none" to disable.
	CacheRetention string
	// CacheSystem marks the system prompt and tool list as cacheable for
	// providers that need explicit breakpoints (Anthropic-style).
	CacheSystem bool
	// Reasoning requests a reasoning/thinking level. Empty and ReasoningAuto
	// both mean "let the model decide"; ReasoningOff disables reasoning.
	Reasoning ReasoningLevel
}

// ReasoningLevel controls how much the model should think before answering.
type ReasoningLevel string

const (
	// ReasoningAuto lets the provider/model decide (the default).
	ReasoningAuto ReasoningLevel = "auto"
	// ReasoningOff disables reasoning entirely.
	ReasoningOff ReasoningLevel = "off"
	// ReasoningMinimal, Low, Medium, High request increasing effort.
	ReasoningMinimal ReasoningLevel = "minimal"
	ReasoningLow     ReasoningLevel = "low"
	ReasoningMedium  ReasoningLevel = "medium"
	ReasoningHigh    ReasoningLevel = "high"
)

// ParseReasoningLevel normalizes user input, returning the level and whether it
// was recognized. Empty input maps to ReasoningAuto.
func ParseReasoningLevel(s string) (ReasoningLevel, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return ReasoningAuto, true
	case "off", "none", "disabled":
		return ReasoningOff, true
	case "minimal", "low", "medium", "high":
		return ReasoningLevel(strings.ToLower(strings.TrimSpace(s))), true
	default:
		return ReasoningAuto, false
	}
}

// WantsReasoning reports whether the level asks for reasoning output.
func (l ReasoningLevel) WantsReasoning() bool {
	return l != ReasoningOff && l != ""
}

// EffortValue returns the OpenAI-style effort string for this level, or "" when
// the level should not send an effort (auto/off).
func (l ReasoningLevel) EffortValue() string {
	switch l {
	case ReasoningMinimal, ReasoningLow, ReasoningMedium, ReasoningHigh:
		return string(l)
	default:
		return ""
	}
}

// Chunk is one streamed piece of a model response.
type Chunk struct {
	// TextDelta carries incremental assistant text.
	TextDelta string
	// ReasoningDelta carries incremental model reasoning/thinking text, when the
	// provider exposes it. It is display-only and never becomes assistant content.
	ReasoningDelta string
	// ToolCall is set when the model finishes a tool call.
	ToolCall *ToolCall
	// Done marks the end of the stream; Usage is set when available.
	Done  bool
	Usage *Usage
	// Err is set if the stream failed.
	Err error
}

// Usage reports token accounting for a request.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Adapter streams model responses from a specific backend.
type Adapter interface {
	// Name identifies the backend, e.g. "openai-chat".
	Name() string
	// Stream runs a request and delivers chunks until Done or an error.
	Stream(ctx context.Context, req Request) (<-chan Chunk, error)
}

// Service is the pluggable registry of adapters exposed as ctx.Service(llm.Key).
type Service interface {
	// Register adds or replaces an adapter by name and returns a disposer.
	Register(adapter Adapter)
	// Get returns an adapter by name.
	Get(name string) (Adapter, bool)
	// Names lists registered adapter names.
	Names() []string
}

// ModelInfoKey is the context key under which the active model's capabilities
// are stored, so tools can adapt to what the model supports.
type modelInfoCtxKey struct{}

// WithModelInfo attaches model capabilities to a context.
func WithModelInfo(ctx context.Context, info any) context.Context {
	return context.WithValue(ctx, modelInfoCtxKey{}, info)
}

// ModelInfoFrom retrieves model capabilities previously attached with WithModelInfo.
func ModelInfoFrom(ctx context.Context) (any, bool) {
	v := ctx.Value(modelInfoCtxKey{})
	return v, v != nil
}
