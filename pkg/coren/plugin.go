package coren

// Plugin is a mountable unit. Everything in Coren is a plugin.
//
// A plugin contributes services, listeners, and other reversible effects to a
// Context during Apply. The kernel guarantees Apply runs only after the services
// named by Inject are available.
type Plugin interface {
	// ID identifies the plugin for diagnostics and unloading.
	ID() string
	// Inject lists service keys this plugin requires. The kernel waits for them.
	Inject() []string
	// Apply mounts the plugin onto ctx. Registrations made through ctx are
	// unwound automatically when the plugin is unloaded.
	Apply(ctx Context) error
}

// Startable is an optional lifecycle a plugin may implement. Start runs after
// every plugin has been applied; Stop runs during teardown, before effects unwind.
type Startable interface {
	Start(ctx Context) error
	Stop(ctx Context) error
}

// PluginFunc adapts a function to the Plugin interface.
type PluginFunc struct {
	Name    string
	Needs   []string
	Mount   func(ctx Context) error
	OnStart func(ctx Context) error
	OnStop  func(ctx Context) error
}

func (p PluginFunc) ID() string       { return p.Name }
func (p PluginFunc) Inject() []string { return p.Needs }
func (p PluginFunc) Apply(ctx Context) error {
	if p.Mount == nil {
		return nil
	}
	return p.Mount(ctx)
}
func (p PluginFunc) Start(ctx Context) error {
	if p.OnStart == nil {
		return nil
	}
	return p.OnStart(ctx)
}
func (p PluginFunc) Stop(ctx Context) error {
	if p.OnStop == nil {
		return nil
	}
	return p.OnStop(ctx)
}

// Service keys for the built-in capabilities. Centralized so plugins cannot
// drift on spelling (Go has no declaration merging to catch typos).
const (
	ServiceLLM       = "llm"
	ServiceTools     = "tools"
	ServiceSessions  = "sessions"
	ServiceAgents    = "agents"
	ServiceAgentLoop = "agent-loop"
	ServiceCommands  = "commands"
	ServiceFS        = "fs"
	ServiceShell     = "shell"
	ServiceHTTP      = "http"
	ServiceServer    = "server"
)
