// Package clockplugin is an example Coren plugin.
//
// It registers one model-facing tool that reports the current time, and shows
// the two things every plugin does: declare its dependencies with Inject, and
// contribute a reversible registration in Apply.
package clockplugin

import (
	"context"
	"fmt"
	"time"

	"coren/pkg/coren"
	"coren/pkg/llm"
	"coren/pkg/tools"
)

// Plugin registers the current_time tool on the tools service.
type Plugin struct {
	// Location is the time.Location name to report; empty uses local time.
	Location string
}

func (Plugin) ID() string { return "tools.clock" }

// Inject declares that this plugin needs the tools service before it can mount.
func (Plugin) Inject() []string { return []string{tools.Key} }

func (p Plugin) Apply(ctx coren.Context) error {
	registry, ok := coren.UnwrapKey[tools.Service](ctx, tools.Key)
	if !ok {
		return fmt.Errorf("clockplugin: tools service not available")
	}
	registry.Register(clockTool{location: p.Location})
	return nil
}

type clockTool struct {
	location string
}

func (clockTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "current_time",
		Description: "Return the current date and time in RFC3339 format.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t clockTool) Run(_ context.Context, _ string) (string, error) {
	now := time.Now()
	if t.location != "" {
		loc, err := time.LoadLocation(t.location)
		if err != nil {
			return "", fmt.Errorf("unknown location %q: %w", t.location, err)
		}
		now = now.In(loc)
	}
	return now.Format(time.RFC3339), nil
}
