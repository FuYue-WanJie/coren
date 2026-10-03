// Package builtintools registers Coren's built-in tools as a plugin.
package builtintools

import (
	"fmt"

	"coren/pkg/coren"
	"coren/pkg/tools"
)

// ProviderPlugin provides the tools service backed by an in-memory registry.
type ProviderPlugin struct{}

func (ProviderPlugin) ID() string       { return "tools" }
func (ProviderPlugin) Inject() []string { return nil }

func (ProviderPlugin) Apply(ctx coren.Context) error {
	ctx.Provide(tools.Key, tools.NewRegistry())
	return nil
}

// Plugin registers the built-in file, shell, and HTTP tools.
type Plugin struct {
	// WorkDir scopes file tools and sets the shell working directory.
	WorkDir string
}

func (p Plugin) ID() string       { return "tools.builtin" }
func (p Plugin) Inject() []string { return []string{tools.Key} }

func (p Plugin) Apply(ctx coren.Context) error {
	registry, ok := coren.UnwrapKey[tools.Service](ctx, tools.Key)
	if !ok {
		return fmt.Errorf("builtintools: tools service not available")
	}
	registry.Register(ReadFile{Root: p.WorkDir})
	registry.Register(WriteFile{Root: p.WorkDir})
	registry.Register(Shell{Dir: p.WorkDir})
	registry.Register(HTTPRequest{})
	return nil
}
