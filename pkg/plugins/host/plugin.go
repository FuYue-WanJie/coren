// Package host mounts out-of-process plugins onto the tools service.
//
// A host plugin launches each configured child process, performs the
// pluginproto handshake, and wraps every tool the plugin declares as a
// tools.Tool backed by the child. Plugins run in their own process, so they may
// be written in any language and crash without taking the agent down.
package host

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"coren/pkg/coren"
	"coren/pkg/llm"
	"coren/pkg/pluginproto"
	"coren/pkg/tools"
)

// Key is the service key for the plugin host.
const Key = "plugin-host"

// PluginConfig declares one external plugin process.
type PluginConfig struct {
	// Name identifies the plugin in diagnostics and errors.
	Name string
	// Command is the executable; Args are its arguments.
	Command string
	Args    []string
	// Env adds environment variables for the child, as "KEY=VALUE".
	Env []string
	// Dir sets the child's working directory; empty inherits the host's.
	Dir string
	// Disabled skips launching this plugin.
	Disabled bool
}

// Plugin mounts the host and launches every enabled plugin at Start.
type Plugin struct {
	// Plugins lists the external plugin processes to launch.
	Plugins []PluginConfig
	// WorkDir is passed to plugins in the handshake.
	WorkDir string
	// HostVersion is passed to plugins in the handshake.
	HostVersion string
}

func (Plugin) ID() string       { return "plugin-host" }
func (Plugin) Inject() []string { return []string{tools.Key} }

// Apply records the configuration; launching happens in Start so the tools
// registry already exists and every plugin has been applied.
func (p Plugin) Apply(ctx coren.Context) error {
	if _, ok := coren.UnwrapKey[tools.Service](ctx, tools.Key); !ok {
		return fmt.Errorf("host: tools service not available")
	}
	return nil
}

// Start launches each configured plugin and registers its tools.
func (p Plugin) Start(ctx coren.Context) error {
	registry, ok := coren.UnwrapKey[tools.Service](ctx, tools.Key)
	if !ok {
		return fmt.Errorf("host: tools service not available")
	}
	for _, pc := range p.Plugins {
		if pc.Disabled || pc.Command == "" {
			continue
		}
		proc, err := p.launch(ctx, pc)
		if err != nil {
			return fmt.Errorf("host: plugin %q: %w", pc.Name, err)
		}
		for _, spec := range proc.tools {
			if _, exists := registry.Get(spec.Name); exists {
				proc.stop()
				return fmt.Errorf("host: plugin %q tool %q collides with an existing tool", pc.Name, spec.Name)
			}
			registry.Register(bridgedTool{client: proc.client, spec: spec, plugin: pc.Name})
		}
		// Tear down the child when the plugin unloads.
		ctx.EffectFunc("plugin-host:"+pc.Name, func() error {
			proc.stop()
			return nil
		})
	}
	return nil
}

// Stop is a no-op: children are reaped through registered effects.
func (p Plugin) Stop(coren.Context) error { return nil }

// running is a launched plugin process and its live client.
type running struct {
	cmd    *exec.Cmd
	client *pluginproto.Client
	tools  []pluginproto.ToolSpec
	closer io.Closer
}

// launch starts a child process and completes the handshake.
func (p Plugin) launch(ctx coren.Context, pc PluginConfig) (*running, error) {
	cmd := exec.Command(pc.Command, pc.Args...)
	if pc.Dir != "" {
		cmd.Dir = pc.Dir
	}
	if len(pc.Env) > 0 {
		cmd.Env = append(cmd.Environ(), pc.Env...)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// Plugin logs go to the host's stderr, never into the protocol stream.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	client := pluginproto.NewClient(stdout, stdin)
	hs, err := client.Initialize(ctx.GoContext(), pluginproto.InitializeParams{
		WorkDir:     p.WorkDir,
		HostVersion: p.HostVersion,
	})
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	if len(hs.Tools) == 0 {
		// A plugin with no tools still holds a process; keep it alive so it can
		// later be extended, but nothing is registered.
	}
	return &running{cmd: cmd, client: client, tools: hs.Tools, closer: stdin}, nil
}

// stop shuts the plugin down and waits for the process to exit.
func (r *running) stop() {
	r.client.Shutdown()
	if r.closer != nil {
		_ = r.closer.Close()
	}
	if r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
	}
	_ = r.cmd.Wait()
}

// bridgedTool adapts a remote tool to the tools.Tool interface.
type bridgedTool struct {
	client *pluginproto.Client
	spec   pluginproto.ToolSpec
	plugin string
}

func (t bridgedTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        t.spec.Name,
		Description: t.spec.Description,
		Parameters:  t.spec.Parameters,
	}
}

func (t bridgedTool) Run(ctx context.Context, arguments string) (string, error) {
	res, err := t.client.CallTool(ctx, t.spec.Name, arguments)
	if err != nil {
		return "", fmt.Errorf("plugin %q tool %q: %w", t.plugin, t.spec.Name, err)
	}
	if res.IsError {
		return "", fmt.Errorf("plugin %q tool %q: %s", t.plugin, t.spec.Name, res.Output)
	}
	return res.Output, nil
}
