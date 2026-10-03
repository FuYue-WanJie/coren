// Package subagents defines the subagent capability seam.
//
// A subagent delegates a task to a child agent. Like the llm adapters, multiple
// providers coexist and are registered by name, so a caller can choose an
// in-process child, an isolated session, or (in future) an external product.
// The model reaches this seam through a delegation tool.
package subagents

import (
	"context"
	"sort"
)

// Key is the service key for the subagents service.
const Key = "subagents"

// Request describes one delegation.
type Request struct {
	// Prompt is the task given to the child as its user message.
	Prompt string
	// Label is an optional short description for diagnostics.
	Label string
	// System overrides the child's system prompt; empty inherits the parent's.
	System string
	// Model overrides the child's model; empty inherits the parent's.
	Model string
	// MaxDepth caps delegation depth when non-zero.
	MaxDepth int
	// Depth is the child's delegation depth (parent depth + 1).
	Depth int
}

// Result is the outcome of a delegation.
type Result struct {
	// Output is the child's final assistant text.
	Output string
	// Steps is the number of model turns the child used.
	Steps int
}

// Provider runs one delegation.
type Provider interface {
	// Name identifies the provider, e.g. "in-process".
	Name() string
	// Start runs a child agent to completion and returns its result.
	Start(ctx context.Context, req Request) (Result, error)
}

// Service is the pluggable registry of providers exposed as ctx.Service(subagents.Key).
type Service interface {
	// Register adds or replaces a provider by name.
	Register(provider Provider)
	// Get returns a provider by name.
	Get(name string) (Provider, bool)
	// Default returns the provider to use when none is named.
	Default() (Provider, bool)
	// Names lists registered provider names, sorted.
	Names() []string
}

// Registry is the default in-memory implementation of Service.
type Registry struct {
	providers map[string]Provider
	order     []string
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{providers: map[string]Provider{}}
}

func (r *Registry) Register(provider Provider) {
	if provider == nil {
		return
	}
	name := provider.Name()
	if _, exists := r.providers[name]; !exists {
		r.order = append(r.order, name)
	}
	r.providers[name] = provider
}

func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// Default returns the first registered provider.
func (r *Registry) Default() (Provider, bool) {
	if len(r.order) == 0 {
		return nil, false
	}
	p, ok := r.providers[r.order[0]]
	return p, ok
}

func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	sort.Strings(out)
	return out
}
