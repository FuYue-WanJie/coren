package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"coren/internal/config"
	"coren/pkg/mcp"
)

const mcpUsage = `Usage: coren mcp <action>

Actions:
  list                          List configured MCP servers.
  add <name> <url> [--sse] [--header K=V]   Add or update a remote server.
  remove <name>                 Remove a server.
  test <name>                   Connect and list the server's primitives.

The server list is stored in the project coren.json under "mcp".
`

func runMCP(args []string) error {
	if len(args) == 0 {
		fmt.Print(mcpUsage)
		return nil
	}
	switch args[0] {
	case "list":
		return mcpList()
	case "add":
		if len(args) < 3 {
			return fmt.Errorf("usage: coren mcp add <name> <url> [--sse] [--header K=V]")
		}
		return mcpAdd(args[1], args[2], args[3:])
	case "remove", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: coren mcp remove <name>")
		}
		return mcpRemove(args[1])
	case "test":
		if len(args) < 2 {
			return fmt.Errorf("usage: coren mcp test <name>")
		}
		return mcpTest(args[1])
	case "help", "-h", "--help":
		fmt.Print(mcpUsage)
		return nil
	default:
		return fmt.Errorf("unknown mcp action %q", args[0])
	}
}

// projectConfigPath is the project-level coren.json.
func projectConfigPath() string {
	wd, _ := os.Getwd()
	return config.ProjectConfigPath(wd)
}

// loadRaw reads the project config as a generic map so unknown keys survive.
func loadRaw() (map[string]any, error) {
	data, err := os.ReadFile(projectConfigPath())
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	raw := map[string]any{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", projectConfigPath(), err)
	}
	return raw, nil
}

func saveRaw(raw map[string]any) error {
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := projectConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func mcpList() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if len(cfg.MCP) == 0 {
		fmt.Println("no MCP servers configured")
		return nil
	}
	for _, s := range cfg.MCP {
		transport := s.Transport
		if transport == "" {
			transport = "streamable"
		}
		status := "enabled"
		if s.Enabled != nil && !*s.Enabled {
			status = "disabled"
		}
		fmt.Printf("%-16s %-12s %-10s %s\n", s.Name, transport, status, s.URL)
	}
	return nil
}

func mcpAdd(name, url string, rest []string) error {
	server := map[string]any{"name": name, "url": url}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--sse":
			server["transport"] = "sse"
		case "--header":
			if i+1 >= len(rest) {
				return fmt.Errorf("--header needs K=V")
			}
			k, v, ok := strings.Cut(rest[i+1], "=")
			if !ok {
				return fmt.Errorf("--header expects K=V, got %q", rest[i+1])
			}
			headers, _ := server["headers"].(map[string]any)
			if headers == nil {
				headers = map[string]any{}
				server["headers"] = headers
			}
			headers[k] = v
			i++
		default:
			return fmt.Errorf("unknown flag %q", rest[i])
		}
	}

	raw, err := loadRaw()
	if err != nil {
		return err
	}
	list, _ := raw["mcp"].([]any)
	replaced := false
	for i, entry := range list {
		if m, ok := entry.(map[string]any); ok && m["name"] == name {
			list[i] = server
			replaced = true
			break
		}
	}
	if !replaced {
		list = append(list, server)
	}
	raw["mcp"] = list
	if err := saveRaw(raw); err != nil {
		return err
	}
	if replaced {
		fmt.Printf("updated MCP server %q\n", name)
	} else {
		fmt.Printf("added MCP server %q\n", name)
	}
	return nil
}

func mcpRemove(name string) error {
	raw, err := loadRaw()
	if err != nil {
		return err
	}
	list, _ := raw["mcp"].([]any)
	filtered := make([]any, 0, len(list))
	removed := false
	for _, entry := range list {
		if m, ok := entry.(map[string]any); ok && m["name"] == name {
			removed = true
			continue
		}
		filtered = append(filtered, entry)
	}
	if !removed {
		return fmt.Errorf("no MCP server named %q", name)
	}
	if len(filtered) == 0 {
		delete(raw, "mcp")
	} else {
		raw["mcp"] = filtered
	}
	if err := saveRaw(raw); err != nil {
		return err
	}
	fmt.Printf("removed MCP server %q\n", name)
	return nil
}

func mcpTest(name string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var target *config.MCPServer
	for i := range cfg.MCP {
		if cfg.MCP[i].Name == name {
			target = &cfg.MCP[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("no MCP server named %q", name)
	}

	client := &mcp.Client{
		Name:      target.Name,
		URL:       target.URL,
		Transport: target.Transport,
		Headers:   target.Headers,
	}
	ctx := context.Background()
	if err := client.Initialize(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	info := client.ServerInfo()
	fmt.Printf("connected to %s %s\n", info.Name, info.Version)

	caps := client.Capabilities()
	if caps.Tools != nil {
		tools, err := client.ListTools(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("tools (%d):\n", len(tools))
		for _, tool := range tools {
			fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
		}
	}
	if caps.Resources != nil {
		resources, err := client.ListResources(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("resources (%d):\n", len(resources))
		for _, r := range resources {
			fmt.Printf("  - %s\n", r.URI)
		}
	}
	if caps.Prompts != nil {
		prompts, err := client.ListPrompts(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("prompts (%d):\n", len(prompts))
		for _, p := range prompts {
			fmt.Printf("  - %s\n", p.Name)
		}
	}
	return nil
}
