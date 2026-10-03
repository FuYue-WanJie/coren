// Package subagents provides the subagents service and the delegation tool.
//
// The built-in provider runs a child agent in the same process using an isolated
// session, so a parent can hand off a focused task without polluting its own
// history. The service is a registry, so other providers (external products,
// isolated runtimes) can be added beside it.
package subagentsplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coren/pkg/agent"
	"coren/pkg/coren"
	"coren/pkg/llm"
	"coren/pkg/session"
	"coren/pkg/subagents"
	"coren/pkg/tools"
)

// ProviderPlugin provides the subagents service.
type ProviderPlugin struct{}

func (ProviderPlugin) ID() string       { return "subagents" }
func (ProviderPlugin) Inject() []string { return nil }

func (ProviderPlugin) Apply(ctx coren.Context) error {
	ctx.Provide(subagents.Key, subagents.NewRegistry())
	return nil
}

// Config configures the in-process provider.
type Config struct {
	// Model overrides the child model; empty inherits the parent loop's model.
	Model string
	// System sets the child's system prompt; empty inherits the framework default.
	System string
	// MaxSteps bounds the child's tool-call steps.
	MaxSteps int
	// MaxDepth caps delegation depth; zero means a 2-level default.
	MaxDepth int
}

// InProcessPlugin registers the in-process subagent provider.
type InProcessPlugin struct {
	Config Config
}

func (InProcessPlugin) ID() string { return "subagents.in-process" }
func (InProcessPlugin) Inject() []string {
	return []string{subagents.Key, session.Key, llm.Key, tools.Key}
}

func (p InProcessPlugin) Apply(ctx coren.Context) error {
	registry, ok := coren.UnwrapKey[subagents.Service](ctx, subagents.Key)
	if !ok {
		return fmt.Errorf("subagents.in-process: subagents service missing")
	}
	registry.Register(&InProcessProvider{ctx: ctx, config: p.Config})
	return nil
}

// InProcessProvider runs the child with a fresh session in this process.
type InProcessProvider struct {
	ctx    coren.Context
	config Config
}

func (p *InProcessProvider) Name() string { return "in-process" }

func (p *InProcessProvider) Start(ctx context.Context, req subagents.Request) (subagents.Result, error) {
	sessions, ok := coren.UnwrapKey[session.Service](p.ctx, session.Key)
	if !ok {
		return subagents.Result{}, fmt.Errorf("in-process subagent: sessions service missing")
	}

	maxDepth := p.config.MaxDepth
	if maxDepth == 0 {
		maxDepth = 2
	}
	if req.Depth > maxDepth {
		return subagents.Result{}, fmt.Errorf("in-process subagent: delegation depth %d exceeds limit %d", req.Depth, maxDepth)
	}

	system := req.System
	if system == "" {
		system = p.config.System
	}
	model := req.Model
	if model == "" {
		model = p.config.Model
	}

	child := &agent.Agent{
		Context:     p.ctx,
		Model:       model,
		System:      system,
		MaxSteps:    p.config.MaxSteps,
		AdapterName: adapterNameFromParent(p.ctx),
	}

	// A unique session id per delegation keeps the child isolated from the parent.
	label := req.Label
	if label == "" {
		label = "task"
	}
	sess := sessions.Get("subagent:" + label)
	sess.Reset()

	var (
		output strings.Builder
		steps  int
	)
	// Propagate depth so a child's own delegations accumulate against the limit.
	childCtx := withDepth(ctx, req.Depth)
	for ev := range child.Send(childCtx, sess, req.Prompt) {
		switch {
		case ev.Err != nil:
			return subagents.Result{Output: output.String(), Steps: steps}, ev.Err
		case ev.TextDelta != "":
			output.WriteString(ev.TextDelta)
		case ev.ToolCallStart != nil:
			steps++
		}
	}
	return subagents.Result{Output: strings.TrimSpace(output.String()), Steps: steps}, nil
}

// adapterNameFromParent returns the parent's first registered adapter so the
// child uses the same backend unless overridden.
func adapterNameFromParent(ctx coren.Context) string {
	service, ok := coren.UnwrapKey[llm.Service](ctx, llm.Key)
	if !ok {
		return ""
	}
	names := service.Names()
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// ToolPlugin registers the model-facing `task` delegation tool.
type ToolPlugin struct {
	// MaxDepth caps delegation depth; zero means the provider default.
	MaxDepth int
}

func (ToolPlugin) ID() string       { return "subagents.tool" }
func (ToolPlugin) Inject() []string { return []string{subagents.Key, tools.Key} }

func (ToolPlugin) Apply(ctx coren.Context) error {
	service, ok := coren.UnwrapKey[subagents.Service](ctx, subagents.Key)
	if !ok {
		return fmt.Errorf("subagents.tool: subagents service missing")
	}
	registry, ok := coren.UnwrapKey[tools.Service](ctx, tools.Key)
	if !ok {
		return fmt.Errorf("subagents.tool: tools service missing")
	}
	registry.Register(taskTool{service: service})
	return nil
}

// taskTool delegates a focused task to a child agent.
type taskTool struct {
	service subagents.Service
}

func (taskTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "task",
		Description: "Delegate a focused, self-contained task to a child agent with its own context. " +
			"Use it to explore or work on a subproblem without cluttering this conversation. " +
			"The child returns only its final answer.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{"type": "string", "description": "The task for the child agent."},
				"label":  map[string]any{"type": "string", "description": "Optional short label for the subagent."},
			},
			"required": []string{"prompt"},
		},
	}
}

func (t taskTool) Run(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Prompt string `json:"prompt"`
		Label  string `json:"label"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}
	provider, ok := t.service.Default()
	if !ok {
		return "", fmt.Errorf("no subagent provider registered")
	}
	result, err := provider.Start(ctx, subagents.Request{
		Prompt: args.Prompt,
		Label:  args.Label,
		Depth:  depthFromContext(ctx) + 1,
	})
	if err != nil {
		return result.Output, err
	}
	return result.Output, nil
}

// depthKey carries the current delegation depth through context so nested task
// calls accumulate depth instead of always starting at one.
type depthKey struct{}

func withDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, depthKey{}, depth)
}

func depthFromContext(ctx context.Context) int {
	if v, ok := ctx.Value(depthKey{}).(int); ok {
		return v
	}
	return 0
}
