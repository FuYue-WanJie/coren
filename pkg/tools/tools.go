// Package tools defines the tool contract and the tools service.
//
// A tool is a capability the model may invoke. Plugins register tools on the
// service; the agent loop lists their specs for prompt assembly and runs them
// through the service so policy plugins can intercept execution.
package tools

import (
	"context"
	"fmt"
	"sort"

	"coren/pkg/llm"
)

// Key is the service key for the tools service.
const Key = "tools"

// Tool is a capability the model may invoke.
type Tool interface {
	// Spec describes the tool to the model.
	Spec() llm.ToolSpec
	// Run executes the tool with raw JSON arguments and returns text output.
	Run(ctx context.Context, arguments string) (string, error)
}

// Result is a tool outcome that may carry multimodal content.
type Result struct {
	// Text is the textual output.
	Text string
	// Parts carries media (images, audio, video) to place in the tool result.
	Parts []llm.ContentPart
}

// ResultTool is implemented by tools that can return media alongside text.
// The agent prefers it over Tool.Run when available.
type ResultTool interface {
	Tool
	RunResult(ctx context.Context, arguments string) (Result, error)
}

// runTool executes a tool, using RunResult when the tool supports media.
func runTool(ctx context.Context, t Tool, arguments string) (Result, error) {
	if rt, ok := t.(ResultTool); ok {
		return rt.RunResult(ctx, arguments)
	}
	text, err := t.Run(ctx, arguments)
	return Result{Text: text}, err
}

// Service is the pluggable tool registry exposed as ctx.Service(tools.Key).
type Service interface {
	// Register adds or replaces a tool by name.
	Register(t Tool)
	// Get returns a tool by name.
	Get(name string) (Tool, bool)
	// Specs lists tool specs sorted by name for stable prompts.
	Specs() []llm.ToolSpec
	// Run looks up a tool and executes it, returning text and any media parts.
	Run(ctx context.Context, name, arguments string) (Result, error)
	// RunText runs a tool and returns only its text output.
	RunText(ctx context.Context, name, arguments string) (string, error)
}

// Registry is the default in-memory implementation of Service.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

func (r *Registry) Register(t Tool) {
	if t == nil {
		return
	}
	r.tools[t.Spec().Name] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) Specs() []llm.ToolSpec {
	specs := make([]llm.ToolSpec, 0, len(r.tools))
	for _, t := range r.tools {
		specs = append(specs, t.Spec())
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

func (r *Registry) Run(ctx context.Context, name, arguments string) (Result, error) {
	t, ok := r.tools[name]
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", name)
	}
	return runTool(ctx, t, arguments)
}

func (r *Registry) RunText(ctx context.Context, name, arguments string) (string, error) {
	result, err := r.Run(ctx, name, arguments)
	return result.Text, err
}
