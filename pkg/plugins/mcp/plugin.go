// Package mcp connects Coren to remote MCP servers.
//
// Each configured server's tools are registered on the tools service (so the
// model can call them like any local tool), its resources are exposed through
// dedicated tools, and its prompts through a prompt tool. Connections are made
// at mount time; a failing server is reported and skipped rather than aborting
// the whole boot.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"coren/pkg/coren"
	"coren/pkg/llm"
	"coren/pkg/mcp"
	"coren/pkg/tools"
)

// ServerConfig describes one remote MCP server.
type ServerConfig struct {
	// Name labels the server and prefixes its tool names.
	Name string `json:"name"`
	// URL is the server endpoint.
	URL string `json:"url"`
	// Transport is "streamable" (default) or "sse".
	Transport string `json:"transport,omitempty"`
	// Headers are sent on every request (e.g. Authorization).
	Headers map[string]string `json:"headers,omitempty"`
	// Enabled defaults to true; set false to skip without removing the entry.
	Enabled *bool `json:"enabled,omitempty"`
}

func (s ServerConfig) isEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

// Config configures the MCP plugin.
type Config struct {
	// Servers lists remote MCP servers to connect at boot.
	Servers []ServerConfig
	// Namespace prefixes tool names with the server name to avoid collisions.
	// Defaults to true.
	Namespace *bool
}

// Plugin connects to configured MCP servers and registers their primitives.
type Plugin struct {
	Config Config
}

func (Plugin) ID() string       { return "mcp" }
func (Plugin) Inject() []string { return []string{tools.Key} }

func (p Plugin) Apply(ctx coren.Context) error {
	registry, ok := coren.UnwrapKey[tools.Service](ctx, tools.Key)
	if !ok {
		return fmt.Errorf("mcp: tools service missing")
	}
	ns := pNamespace(p.Config.Namespace)
	for _, server := range pServers(p.Config.Servers) {
		if !server.isEnabled() {
			continue
		}
		client := &mcp.Client{
			Name:      server.Name,
			URL:       server.URL,
			Headers:   server.Headers,
			Transport: server.Transport,
		}
		if err := client.Initialize(ctx.GoContext()); err != nil {
			fmt.Fprintf(os.Stderr, "warning: mcp server %q: %v\n", server.Name, err)
			continue
		}
		if err := registerServer(ctx.GoContext(), registry, client, ns); err != nil {
			fmt.Fprintf(os.Stderr, "warning: mcp server %q: %v\n", server.Name, err)
		}
	}
	return nil
}

// registerServer lists and registers a connected server's primitives.
func registerServer(ctx context.Context, registry tools.Service, client *mcp.Client, namespace bool) error {
	caps := client.Capabilities()

	if caps.Tools != nil {
		list, err := client.ListTools(ctx)
		if err != nil {
			return fmt.Errorf("listing tools: %w", err)
		}
		for _, tool := range list {
			registry.Register(remoteTool{client: client, tool: tool, name: toolName(client.Name, tool.Name, namespace)})
		}
	}

	if caps.Resources != nil {
		list, err := client.ListResources(ctx)
		if err != nil {
			return fmt.Errorf("listing resources: %w", err)
		}
		if len(list) > 0 {
			registry.Register(resourceListTool{client: client, name: toolName(client.Name, "list_resources", namespace)})
			registry.Register(resourceReadTool{client: client, name: toolName(client.Name, "read_resource", namespace)})
		}
	}

	if caps.Prompts != nil {
		list, err := client.ListPrompts(ctx)
		if err != nil {
			return fmt.Errorf("listing prompts: %w", err)
		}
		if len(list) > 0 {
			registry.Register(promptListTool{client: client, name: toolName(client.Name, "list_prompts", namespace)})
			registry.Register(promptGetTool{client: client, name: toolName(client.Name, "get_prompt", namespace)})
		}
	}
	return nil
}

// toolName namespaces a remote tool name by server to avoid collisions.
func toolName(server, tool string, namespace bool) string {
	if !namespace {
		return tool
	}
	return sanitize(server) + "__" + tool
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, s)
}

func pNamespace(v *bool) bool {
	return v == nil || *v
}

func pServers(v []ServerConfig) []ServerConfig {
	out := append([]ServerConfig(nil), v...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// remoteTool is an MCP tool presented as a local tool.
type remoteTool struct {
	client *mcp.Client
	tool   mcp.Tool
	name   string
}

func (t remoteTool) Spec() llm.ToolSpec {
	schema := t.tool.InputSchema
	if schema == nil {
		schema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return llm.ToolSpec{
		Name:        t.name,
		Description: t.tool.Description,
		Parameters:  schema,
	}
}

func (t remoteTool) Run(ctx context.Context, arguments string) (string, error) {
	result, err := t.client.CallTool(ctx, t.tool.Name, arguments)
	if err != nil {
		return "", err
	}
	text := result.Text()
	if result.IsError {
		return text, fmt.Errorf("mcp tool %s reported an error", t.tool.Name)
	}
	return text, nil
}

// resourceListTool lists resources on an MCP server.
type resourceListTool struct {
	client *mcp.Client
	name   string
}

func (t resourceListTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        t.name,
		Description: "List resources available from the MCP server " + t.client.Name + ".",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (t resourceListTool) Run(ctx context.Context, _ string) (string, error) {
	list, err := t.client.ListResources(ctx)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "no resources", nil
	}
	var b strings.Builder
	for _, r := range list {
		fmt.Fprintf(&b, "- %s (%s): %s\n", r.URI, r.Name, r.Description)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// resourceReadTool reads one resource by URI.
type resourceReadTool struct {
	client *mcp.Client
	name   string
}

func (t resourceReadTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        t.name,
		Description: "Read a resource by URI from the MCP server " + t.client.Name + ".",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"uri": map[string]any{"type": "string", "description": "Resource URI."},
			},
			"required": []string{"uri"},
		},
	}
}

func (t resourceReadTool) Run(ctx context.Context, arguments string) (string, error) {
	var args struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	contents, err := t.client.ReadResource(ctx, args.URI)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, c := range contents {
		switch {
		case c.Text != "":
			b.WriteString(c.Text)
		case c.Blob != "":
			fmt.Fprintf(&b, "[binary resource %s, %s]", c.URI, c.MimeType)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// promptListTool lists prompts on an MCP server.
type promptListTool struct {
	client *mcp.Client
	name   string
}

func (t promptListTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        t.name,
		Description: "List prompt templates available from the MCP server " + t.client.Name + ".",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (t promptListTool) Run(ctx context.Context, _ string) (string, error) {
	list, err := t.client.ListPrompts(ctx)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "no prompts", nil
	}
	var b strings.Builder
	for _, p := range list {
		fmt.Fprintf(&b, "- %s: %s\n", p.Name, p.Description)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// promptGetTool renders a prompt by name.
type promptGetTool struct {
	client *mcp.Client
	name   string
}

func (t promptGetTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        t.name,
		Description: "Render a prompt template from the MCP server " + t.client.Name + ".",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":      map[string]any{"type": "string", "description": "Prompt name."},
				"arguments": map[string]any{"type": "object", "description": "Optional string arguments."},
			},
			"required": []string{"name"},
		},
	}
}

func (t promptGetTool) Run(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	messages, err := t.client.GetPrompt(ctx, args.Name, args.Arguments)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&b, "[%s] %s\n", m.Role, m.Content.Text)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
