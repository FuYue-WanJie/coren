// Package memory exposes a project memory file to the model through tools.
//
// Two tools are registered:
//
//	remember — append a durable note to MEMORY.md
//	recall   — read MEMORY.md back
//
// The same file is injected into the system prompt at boot, so the model starts
// each session already knowing what it recorded before.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"coren/pkg/coren"
	"coren/pkg/llm"
	"coren/pkg/memory"
	"coren/pkg/tools"
)

// Config configures the memory plugin.
type Config struct {
	// Path is the memory file; empty disables the plugin.
	Path string
}

// Plugin registers the memory tools.
type Plugin struct {
	Config Config
}

func (Plugin) ID() string       { return "memory" }
func (Plugin) Inject() []string { return []string{tools.Key} }

func (p Plugin) Apply(ctx coren.Context) error {
	if p.Config.Path == "" {
		return nil
	}
	registry, ok := coren.UnwrapKey[tools.Service](ctx, tools.Key)
	if !ok {
		return fmt.Errorf("memory: tools service missing")
	}
	store := memory.New(p.Config.Path)
	registry.Register(rememberTool{store: store})
	registry.Register(recallTool{store: store})
	return nil
}

// Load returns the memory file contents for prompt injection. Missing files
// yield an empty string.
func Load(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	return memory.New(path).Read()
}

// DefaultPath returns the memory file path for a working directory.
func DefaultPath(workDir string) string {
	if workDir == "" {
		return ""
	}
	return filepath.Join(workDir, memory.DefaultFileName)
}

type rememberTool struct {
	store *memory.Store
}

func (rememberTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "remember",
		Description: "Append a durable note to project memory (MEMORY.md) so it is available in future " +
			"sessions. Use for stable facts: project conventions, decisions, user preferences. " +
			"Not for transient task state.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"note": map[string]any{"type": "string", "description": "The fact to remember."},
				"title": map[string]any{
					"type":        "string",
					"description": "Optional short heading grouping related notes.",
				},
			},
			"required": []string{"note"},
		},
	}
}

func (t rememberTool) Run(_ context.Context, arguments string) (string, error) {
	var args struct {
		Note  string `json:"note"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Note) == "" {
		return "", fmt.Errorf("note is required")
	}
	entry := memory.Section(args.Title, args.Note)
	if err := t.store.Append(entry); err != nil {
		return "", err
	}
	return fmt.Sprintf("remembered in %s", t.store.Path()), nil
}

type recallTool struct {
	store *memory.Store
}

func (recallTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "recall",
		Description: "Read the full project memory (MEMORY.md).",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (t recallTool) Run(_ context.Context, _ string) (string, error) {
	content, err := t.store.Read()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(content) == "" {
		return "memory is empty", nil
	}
	return content, nil
}
