package app

import (
	"fmt"
	"sort"
	"sync"

	"coren/examples/clockplugin"
	"coren/internal/config"
	"coren/internal/profile"
	"coren/pkg/agent"
	"coren/pkg/authz"
	"coren/pkg/coren"
	askplugin "coren/pkg/plugins/ask"
	"coren/pkg/plugins/builtintools"
	"coren/pkg/plugins/coreagent"
	deliverplugin "coren/pkg/plugins/deliver"
	"coren/pkg/plugins/guard"
	hostplugin "coren/pkg/plugins/host"
	"coren/pkg/plugins/httpserver"
	"coren/pkg/plugins/logging"
	mcpPlugin "coren/pkg/plugins/mcp"
	memoryplugin "coren/pkg/plugins/memory"
	"coren/pkg/plugins/memsession"
	"coren/pkg/plugins/openaillm"
	"coren/pkg/plugins/shellcli"
	"coren/pkg/plugins/shellweb"
	skillsplugin "coren/pkg/plugins/skills"
	subagentsplugin "coren/pkg/plugins/subagents"
	todoplugin "coren/pkg/plugins/todo"
	"coren/pkg/risk"
)

// buildContext is everything a factory may need to instantiate a plugin. It
// bundles the runtime configuration, the assembled agent config, and the launch
// options so factories share one stable signature.
type buildContext struct {
	Config      config.Config
	AgentConfig agent.Agent
	Options     Options
}

// Factory instantiates a plugin from the build context.
type Factory func(bc buildContext) (coren.Plugin, error)

// catalog maps built-in plugin ids to their factories. It replaces the old
// compile-time switch: any plugin can be enabled purely by naming its id in a
// profile or in configuration, without touching assembly code.
type catalog struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

func newCatalog() *catalog {
	return &catalog{factories: map[string]Factory{}}
}

// register adds or replaces a factory for an id.
func (c *catalog) register(id string, f Factory) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.factories[id] = f
}

// build instantiates the plugin registered under id.
func (c *catalog) build(id string, bc buildContext) (coren.Plugin, error) {
	c.mu.RLock()
	f, ok := c.factories[id]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("app: unknown plugin %q in profile %q", id, bc.Options.Profile)
	}
	return f(bc)
}

// has reports whether a factory is registered for id.
func (c *catalog) has(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.factories[id]
	return ok
}

// ids lists registered plugin ids, sorted.
func (c *catalog) ids() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.factories))
	for id := range c.factories {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// builtins is the shared catalog of plugins the framework ships. It is built
// once and read concurrently; external registration goes through the profile
// Registry on Options, not here.
var builtins = func() *catalog {
	c := newCatalog()
	c.register(profile.PluginLLM, func(bc buildContext) (coren.Plugin, error) {
		return openaillm.ProviderPlugin{}, nil
	})
	c.register(profile.PluginSessions, func(bc buildContext) (coren.Plugin, error) {
		return memsession.Plugin{Dir: bc.Config.SessionDir}, nil
	})
	c.register(profile.PluginTools, func(bc buildContext) (coren.Plugin, error) {
		return builtintools.ProviderPlugin{}, nil
	})
	c.register(profile.PluginAgents, func(bc buildContext) (coren.Plugin, error) {
		return coreagent.ProviderPlugin{}, nil
	})
	c.register(profile.PluginLLMOpenAI, func(bc buildContext) (coren.Plugin, error) {
		cfg := bc.Config
		return openaillm.Plugin{Config: openaillm.Config{
			API:        cfg.API,
			BaseURL:    cfg.BaseURL,
			APIKey:     cfg.APIKey,
			MaxRetries: cfg.MaxRetries,
		}}, nil
	})
	c.register(profile.PluginToolsBuiltin, func(bc buildContext) (coren.Plugin, error) {
		return builtintools.Plugin{WorkDir: bc.Config.WorkDir}, nil
	})
	c.register(profile.PluginAgentLoop, func(bc buildContext) (coren.Plugin, error) {
		ac := bc.AgentConfig
		return coreagent.LoopPlugin{Config: coreagent.Config{
			Model:       ac.Model,
			System:      ac.System,
			Temperature: ac.Temperature,
			MaxTokens:   ac.MaxTokens,
			MaxSteps:    ac.MaxSteps,
		}}, nil
	})
	c.register(profile.PluginShellWeb, func(bc buildContext) (coren.Plugin, error) {
		return shellweb.Plugin{Addr: bc.Config.Addr}, nil
	})
	c.register(profile.PluginHTTPServer, func(bc buildContext) (coren.Plugin, error) {
		return httpserver.Plugin{
			Addr:        bc.Config.Addr,
			AgentConfig: bc.AgentConfig,
			Auth:        webAuth(bc.Config),
			NoServe:     bc.Options.NoServe,
		}, nil
	})
	c.register(profile.PluginShellCLI, func(bc buildContext) (coren.Plugin, error) {
		return shellcli.Plugin{AgentConfig: bc.AgentConfig}, nil
	})
	c.register(profile.PluginToolsClock, func(bc buildContext) (coren.Plugin, error) {
		return clockplugin.Plugin{}, nil
	})
	c.register(profile.PluginLogging, func(bc buildContext) (coren.Plugin, error) {
		return logging.Plugin{}, nil
	})
	c.register(profile.PluginSkills, func(bc buildContext) (coren.Plugin, error) {
		return skillsplugin.ProviderPlugin{Dir: bc.Config.SkillsDir}, nil
	})
	c.register(profile.PluginSkillsTools, func(bc buildContext) (coren.Plugin, error) {
		return skillsplugin.ToolsPlugin{}, nil
	})
	c.register(profile.PluginSubagents, func(bc buildContext) (coren.Plugin, error) {
		return subagentsplugin.ProviderPlugin{}, nil
	})
	c.register(profile.PluginSubagentsIP, func(bc buildContext) (coren.Plugin, error) {
		cfg := bc.Config
		return subagentsplugin.InProcessPlugin{Config: subagentsplugin.Config{
			Model:    cfg.Model,
			MaxSteps: cfg.MaxSteps,
		}}, nil
	})
	c.register(profile.PluginSubagentsTool, func(bc buildContext) (coren.Plugin, error) {
		return subagentsplugin.ToolPlugin{}, nil
	})
	c.register(profile.PluginAsk, func(bc buildContext) (coren.Plugin, error) {
		return askplugin.Plugin{}, nil
	})
	c.register(profile.PluginMCP, func(bc buildContext) (coren.Plugin, error) {
		return mcpPlugin.Plugin{Config: mcpPlugin.Config{Servers: mcpServers(bc.Config.MCP)}}, nil
	})
	c.register(profile.PluginMemory, func(bc buildContext) (coren.Plugin, error) {
		return memoryplugin.Plugin{Config: memoryplugin.Config{Path: memoryPath(bc.Config)}}, nil
	})
	c.register(profile.PluginTodo, func(bc buildContext) (coren.Plugin, error) {
		return todoplugin.Plugin{Config: todoplugin.Config{Path: todoPath(bc.Config)}}, nil
	})
	c.register(profile.PluginDelivery, func(bc buildContext) (coren.Plugin, error) {
		return deliverplugin.ProviderPlugin{AutoApprove: bc.Config.DeliverAutoApprove}, nil
	})
	c.register(profile.PluginDeliver, func(bc buildContext) (coren.Plugin, error) {
		return deliverplugin.Plugin{Config: deliverplugin.Config{WorkDir: bc.Config.WorkDir}}, nil
	})
	c.register(profile.PluginApproval, func(bc buildContext) (coren.Plugin, error) {
		level, _ := authz.Parse(bc.Config.Authz)
		return guard.ProviderPlugin{Authz: level}, nil
	})
	c.register(profile.PluginGuard, func(bc buildContext) (coren.Plugin, error) {
		return guard.Plugin{Config: guardConfig(bc.Config)}, nil
	})
	c.register(profile.PluginHost, func(bc buildContext) (coren.Plugin, error) {
		return hostplugin.Plugin{
			Plugins:     externalPlugins(bc.Config.ExternalPlugins),
			WorkDir:     bc.Config.WorkDir,
			HostVersion: bc.Options.HostVersion,
		}, nil
	})
	return c
}()

// buildPlugin resolves one plugin id, checking the built-in catalog first and
// falling back to any externally registered factory in opts.Registry.
func buildPlugin(id string, bc buildContext) (coren.Plugin, error) {
	if builtins.has(id) {
		return builtins.build(id, bc)
	}
	if reg := bc.Options.Registry; reg != nil && reg.Has(id) {
		return reg.Build(id, bc.Config)
	}
	return nil, fmt.Errorf("app: unknown plugin %q in profile %q", id, bc.Options.Profile)
}

// mcpServers converts configuration entries to the MCP plugin's server type.
func mcpServers(in []config.MCPServer) []mcpPlugin.ServerConfig {
	out := make([]mcpPlugin.ServerConfig, 0, len(in))
	for _, s := range in {
		out = append(out, mcpPlugin.ServerConfig{
			Name:      s.Name,
			URL:       s.URL,
			Transport: s.Transport,
			Headers:   s.Headers,
			Enabled:   s.Enabled,
		})
	}
	return out
}

// guardConfig builds the guard configuration from settings.
func guardConfig(cfg config.Config) guard.Config {
	rules := make([]risk.Rule, 0, len(cfg.RiskRules))
	for _, r := range cfg.RiskRules {
		rules = append(rules, risk.Rule{
			Name:     r.Name,
			Severity: risk.Severity(r.Severity),
			Tools:    r.Tools,
			Pattern:  r.Pattern,
			Reason:   r.Reason,
		})
	}
	level, _ := authz.Parse(cfg.Authz)
	return guard.Config{
		Authz:           level,
		Rules:           rules,
		DisableDefaults: cfg.DisableDefaultRiskRules,
	}
}
