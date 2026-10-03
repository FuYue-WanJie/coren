// Package coreagent provides the agents service and the default agent-loop.
package coreagent

import (
	"context"
	"fmt"

	"coren/pkg/agent"
	"coren/pkg/agents"
	"coren/pkg/coren"
	"coren/pkg/llm"
	"coren/pkg/session"
	"coren/pkg/tools"
)

// ProviderPlugin provides the agents service.
type ProviderPlugin struct{}

func (ProviderPlugin) ID() string       { return "agents" }
func (ProviderPlugin) Inject() []string { return nil }

func (ProviderPlugin) Apply(ctx coren.Context) error {
	ctx.Provide(agents.Key, agents.NewStore())
	return nil
}

// Config configures the default loop.
type Config struct {
	Model       string
	System      string
	Temperature *float64
	MaxTokens   *int
	MaxSteps    int
}

// LoopPlugin registers the default agent-loop implementation.
//
// Injects the llm/tools/agents services so it cannot mount before they exist.
type LoopPlugin struct {
	Config Config
}

func (LoopPlugin) ID() string { return "agent-loop" }
func (LoopPlugin) Inject() []string {
	return []string{llm.Key, tools.Key, agents.Key}
}

func (p LoopPlugin) Apply(ctx coren.Context) error {
	for _, key := range []string{llm.Key, tools.Key} {
		if _, ok := coren.UnwrapKey[any](ctx, key); !ok {
			return fmt.Errorf("agent-loop: %s service missing", key)
		}
	}
	ctx.Provide(agents.LoopKey, &DefaultLoop{Config: p.Config, base: ctx.GoContext()})
	return nil
}

// DefaultLoop is the built-in Loop implementation wrapping agent.Agent.
type DefaultLoop struct {
	Config Config
	base   context.Context
}

// Send runs one turn: drives agent.Agent with the loop's base context.
func (l *DefaultLoop) Send(ctx coren.Context, sess session.Session, input string) <-chan agent.Event {
	ag := &agent.Agent{
		Context:     ctx,
		Model:       l.Config.Model,
		System:      l.Config.System,
		Temperature: l.Config.Temperature,
		MaxTokens:   l.Config.MaxTokens,
		MaxSteps:    l.Config.MaxSteps,
	}
	return ag.Send(l.base, sess, input)
}
