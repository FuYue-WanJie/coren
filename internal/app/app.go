// Package app assembles a Coren kernel from a profile and configuration.
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"coren/internal/config"
	"coren/internal/profile"
	"coren/pkg/agent"
	"coren/pkg/agents"
	"coren/pkg/coren"
	"coren/pkg/ctxfiles"
	"coren/pkg/llm"
	"coren/pkg/modelinfo"
	memoryplugin "coren/pkg/plugins/memory"
	todoplugin "coren/pkg/plugins/todo"
	"coren/pkg/prompt"
	"coren/pkg/session"
	"coren/pkg/shell"
)

// Options selects the profile and shell-specific settings.
type Options struct {
	// Profile names the composition to boot; empty defaults to "web".
	Profile string
	// Prompt is an optional one-shot prompt for the CLI shell.
	Prompt string
	// Registry, when set, resolves plugin ids the framework does not ship.
	Registry *profile.Registry
	// HostVersion is reported to external plugins during their handshake.
	HostVersion string
	// NoServe makes the http-server plugin provide its service without
	// listening, so another owner can run the process lifetime.
	NoServe bool
}

// App owns a booted kernel and the mounts it assembled.
type App struct {
	Kernel    *coren.Kernel
	Profile   profile.Profile
	Agent     *agent.Agent
	Loop      agents.Loop
	Session   session.Service
	Shell     shell.Shell
	ModelInfo modelinfo.Info
}

// New boots the profile named in opts for cfg.
func New(ctx context.Context, cfg config.Config, opts Options) (*App, error) {
	profileName := opts.Profile
	if profileName == "" {
		profileName = cfg.Profile
	}
	prof, err := profile.Resolve(profileName)
	if err != nil {
		return nil, err
	}
	prof, err = applyPluginOverrides(prof, cfg)
	if err != nil {
		return nil, err
	}
	if err := validatePlugins(prof.Plugins, opts); err != nil {
		return nil, err
	}
	opts.Profile = prof.Name

	info := resolveModelInfo(ctx, cfg)

	system, err := assembleSystemPrompt(cfg, info)
	if err != nil {
		return nil, err
	}

	compactAfter := cfg.CompactAfter
	if compactAfter == 0 && info.ContextWindow() > 0 {
		// Derive a message-count trigger from the context window when the user
		// did not set one explicitly. Roughly 1 message per 500 tokens keeps the
		// projection well under the window.
		compactAfter = info.ContextWindow() / 500
	}

	agentConfig := agent.Agent{
		Model:          cfg.Model,
		System:         system,
		Temperature:    cfg.Temperature,
		MaxTokens:      cfg.MaxTokens,
		MaxSteps:       cfg.MaxSteps,
		CacheRetention: cfg.CacheRetention,
		CompactAfter:   compactAfter,
		ModelInfo:      info,
		ReasoningLevel: reasoningLevel(cfg.Reasoning),
		ToolTimeout:    toolTimeout(cfg),
	}

	bc := buildContext{Config: cfg, AgentConfig: agentConfig, Options: opts}
	plugins := make([]coren.Plugin, 0, len(prof.Plugins))
	for _, id := range prof.Plugins {
		plugin, err := buildPlugin(id, bc)
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, plugin)
	}

	kernel := coren.NewKernel(ctx)
	if err := kernel.Boot(plugins...); err != nil {
		return nil, err
	}
	if err := kernel.Start(); err != nil {
		_ = kernel.Shutdown()
		return nil, err
	}

	sessions, ok := coren.UnwrapKey[session.Service](kernel.Context(), session.Key)
	if !ok {
		_ = kernel.Shutdown()
		return nil, fmt.Errorf("app: sessions service missing after boot")
	}
	loop, hasLoop := coren.UnwrapKey[agents.Loop](kernel.Context(), agents.LoopKey)
	if !hasLoop {
		_ = kernel.Shutdown()
		return nil, fmt.Errorf("app: agent-loop service missing after boot")
	}
	mounted, _ := coren.UnwrapKey[shell.Shell](kernel.Context(), shell.Key)

	ag := agentConfig
	ag.Context = kernel.Context()

	return &App{
		Kernel:  kernel,
		Profile: prof,
		Agent:   &ag,
		Loop:    loop,
		Session: sessions,
		Shell:   mounted,
	}, nil
}

// reasoningLevel parses the configured reasoning level, warning on invalid input.
func reasoningLevel(value string) llm.ReasoningLevel {
	level, ok := llm.ParseReasoningLevel(value)
	if !ok {
		fmt.Fprintf(os.Stderr, "warning: unknown reasoning level %q; using auto\n", value)
	}
	return level
}

// assembleSystemPrompt builds the system prompt from configuration: the base
// identity, project instruction files, memory, and any caller customizations.
func assembleSystemPrompt(cfg config.Config, info modelinfo.Info) (string, error) {
	base := cfg.System
	if base == "" {
		base = defaultSystem
	}

	home, _ := os.UserHomeDir()
	files, err := ctxfiles.Load(ctxfiles.Options{
		WorkDir:    cfg.WorkDir,
		HomeDir:    home,
		FixedPaths: append([]string{ctxfiles.FixedProjectPath}, cfg.ContextFiles.FixedPaths...),
		Disabled:   cfg.ContextFiles.Disabled,
	})
	if err != nil {
		return "", err
	}

	opts := prompt.Options{
		Base:         base,
		Custom:       cfg.CustomSystemPrompt,
		Guidelines:   append(defaultGuidelines, cfg.Guidelines...),
		Append:       cfg.AppendSystemPrompt,
		ContextFiles: files,
	}

	// Inject memory as an appendix so it lands after project context.
	if memoryPath(cfg) != "" {
		if content, err := memoryplugin.Load(memoryPath(cfg)); err == nil && strings.TrimSpace(content) != "" {
			opts.Append = append(opts.Append, "<project_memory>\n"+strings.TrimSpace(content)+"\n</project_memory>")
		}
	}
	// Inject the open task list so the model resumes with awareness of pending work.
	if todoPath(cfg) != "" {
		if content := todoplugin.Load(todoPath(cfg)); strings.TrimSpace(content) != "" {
			opts.Append = append(opts.Append, "<project_todos>\n"+strings.TrimSpace(content)+"\n</project_todos>")
		}
	}
	// Tell the model what it is running as, so it can answer identity questions
	// and reason about its own capabilities (tools, modalities, context, cost).
	if block := formatModelBlock(cfg, info); block != "" {
		opts.Append = append(opts.Append, block)
	}
	return prompt.Build(opts), nil
}

// formatModelBlock renders a concise self-description of the active model. It
// reports only what is known; unknown fields are omitted rather than guessed.
func formatModelBlock(cfg config.Config, info modelinfo.Info) string {
	if cfg.Model == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("<model>\n")
	fmt.Fprintf(&b, "id: %s\n", cfg.Model)
	if cfg.API != "" {
		fmt.Fprintf(&b, "api: %s\n", cfg.API)
	}
	if info.Name != "" && info.Name != cfg.Model {
		fmt.Fprintf(&b, "name: %s\n", info.Name)
	}
	if info.Provider != "" {
		fmt.Fprintf(&b, "provider: %s\n", info.Provider)
	}
	fmt.Fprintf(&b, "reasoning: %t\n", info.Reasoning)
	fmt.Fprintf(&b, "tool_call: %t\n", info.ToolCall)
	if info.Attachment {
		b.WriteString("attachment: true\n")
	}
	if len(info.Modalities.Input) > 0 {
		fmt.Fprintf(&b, "input_modalities: %s\n", strings.Join(info.Modalities.Input, ", "))
	}
	if len(info.Modalities.Output) > 0 {
		fmt.Fprintf(&b, "output_modalities: %s\n", strings.Join(info.Modalities.Output, ", "))
	}
	if cw := info.ContextWindow(); cw > 0 {
		fmt.Fprintf(&b, "context_window: %d\n", cw)
	}
	if info.Limit.Output > 0 {
		fmt.Fprintf(&b, "max_output_tokens: %d\n", info.Limit.Output)
	}
	b.WriteString("</model>")
	return b.String()
}

// memoryPath resolves the memory file, honoring the disabled sentinel "-".
func memoryPath(cfg config.Config) string {
	if cfg.MemoryPath == "-" {
		return ""
	}
	if cfg.MemoryPath != "" {
		return cfg.MemoryPath
	}
	return memoryplugin.DefaultPath(cfg.WorkDir)
}

// todoPath resolves the task list file, honoring the disabled sentinel "-".
func todoPath(cfg config.Config) string {
	if cfg.TodoPath == "-" {
		return ""
	}
	if cfg.TodoPath != "" {
		return cfg.TodoPath
	}
	return todoplugin.DefaultPath(cfg.WorkDir)
}

const defaultSystem = "You are Coren, a concise and capable assistant. " +
	"Use the available tools when they help answer the user's request, and explain your results briefly."

// defaultGuidelines are token-lean rules that reduce wasted turns.
var defaultGuidelines = []string{
	"Prefer acting over asking when the intent is clear; ask only when a wrong guess would be costly.",
	"Read a file before editing it; make the smallest change that satisfies the request.",
	"Keep responses short and factual; skip restating context the user already has.",
	"Call tools in parallel when they are independent.",
}

// resolveModelInfo composes the model's capabilities from the cached catalog,
// an endpoint probe, and configuration. Failures degrade to an empty Info so
// boot never blocks on the network.
func resolveModelInfo(ctx context.Context, cfg config.Config) modelinfo.Info {
	resolver := &modelinfo.Resolver{Overrides: map[string]modelinfo.Override{}}
	for id, ov := range cfg.Models {
		resolver.Overrides[id] = modelinfo.Override{
			Reasoning:  ov.Reasoning,
			ToolCall:   ov.ToolCall,
			Attachment: ov.Attachment,
			Modalities: ov.Modalities,
			Limit:      ov.Limit,
			Cost:       ov.Cost,
		}
	}

	if path := catalogPath(cfg); path != "" {
		if catalog, err := modelinfo.LoadCatalog(path); err == nil {
			resolver.Catalog = catalog
		}
	}
	return resolver.Resolve(ctx, cfg.BaseURL, cfg.APIKey, cfg.Model)
}

// catalogPath resolves the cached catalog location.
func catalogPath(cfg config.Config) string {
	if cfg.CatalogPath != "" {
		return cfg.CatalogPath
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return modelinfo.CachePath(filepath.Join(dir, "coren"))
}

// Close shuts the kernel down, unwinding every plugin's registrations.
func (a *App) Close() error {
	if a == nil || a.Kernel == nil {
		return nil
	}
	return a.Kernel.Shutdown()
}

// AdapterNames lists the llm adapters registered in the kernel.
func (a *App) AdapterNames() []string {
	service, ok := coren.UnwrapKey[llm.Service](a.Kernel.Context(), llm.Key)
	if !ok {
		return nil
	}
	return service.Names()
}

// toolTimeout resolves the per-tool timeout from seconds in config.
func toolTimeout(cfg config.Config) time.Duration {
	if cfg.ToolTimeout <= 0 {
		return 0 // agent applies its default
	}
	return time.Duration(cfg.ToolTimeout) * time.Second
}
