// Package openaillm provides OpenAI-compatible llm adapters as a Coren plugin.
package openaillm

import (
	"fmt"
	"strings"

	"coren/pkg/coren"
	"coren/pkg/llm"
)

// Config selects the adapter flavour and endpoint.
type Config struct {
	// API is "chat" or "responses".
	API string
	// BaseURL is the OpenAI-compatible endpoint root.
	BaseURL string
	// APIKey is the bearer token.
	APIKey string
	// MaxRetries overrides the transient-failure retry count; 0 uses the default.
	MaxRetries int
}

// Plugin registers the configured adapter on the llm service.
type Plugin struct {
	Config Config
}

func (p Plugin) ID() string       { return "llm.openai" }
func (p Plugin) Inject() []string { return nil }

func (p Plugin) Apply(ctx coren.Context) error {
	if p.Config.BaseURL == "" {
		return fmt.Errorf("openaillm: base_url is required")
	}
	registry, ok := coren.UnwrapKey[llm.Service](ctx, llm.Key)
	if !ok {
		return fmt.Errorf("openaillm: llm service not available")
	}

	switch strings.ToLower(p.Config.API) {
	case "chat", "":
		registry.Register(&ChatAdapter{BaseURL: p.Config.BaseURL, APIKey: p.Config.APIKey, MaxRetries: p.Config.MaxRetries})
	case "responses":
		registry.Register(&ResponsesAdapter{BaseURL: p.Config.BaseURL, APIKey: p.Config.APIKey, MaxRetries: p.Config.MaxRetries})
	default:
		return fmt.Errorf("openaillm: unknown api %q", p.Config.API)
	}
	return nil
}

// ProviderPlugin registers the llm service itself backed by a registry.
type ProviderPlugin struct{}

func (ProviderPlugin) ID() string       { return "llm" }
func (ProviderPlugin) Inject() []string { return nil }

func (ProviderPlugin) Apply(ctx coren.Context) error {
	ctx.Provide(llm.Key, llm.NewRegistry())
	return nil
}
