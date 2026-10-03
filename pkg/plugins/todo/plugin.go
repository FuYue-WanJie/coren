// Package todo exposes the project task list to the model through one tool.
//
// A single `todo` tool takes an action, keeping the model's toolset small:
//
//	action=add    text=...   append a task
//	action=list              show open and done tasks
//	action=done   id=... or text=...   mark a task complete
//	action=remove id=... or text=...   delete a task
//	action=clear             remove every task
package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"coren/pkg/coren"
	"coren/pkg/llm"
	"coren/pkg/todo"
	"coren/pkg/tools"
)

// Config configures the todo plugin.
type Config struct {
	// Path is the task file; empty disables the plugin.
	Path string
}

// Plugin registers the todo tool.
type Plugin struct {
	Config Config
}

func (Plugin) ID() string       { return "todo" }
func (Plugin) Inject() []string { return []string{tools.Key} }

func (p Plugin) Apply(ctx coren.Context) error {
	if p.Config.Path == "" {
		return nil
	}
	registry, ok := coren.UnwrapKey[tools.Service](ctx, tools.Key)
	if !ok {
		return fmt.Errorf("todo: tools service missing")
	}
	registry.Register(todoTool{store: todo.New(p.Config.Path)})
	return nil
}

// DefaultPath returns the task file path for a working directory.
func DefaultPath(workDir string) string {
	if workDir == "" {
		return ""
	}
	return filepath.Join(workDir, todo.DefaultFileName)
}

// Load returns the task list for prompt injection, or "" when empty.
func Load(path string) string {
	if path == "" {
		return ""
	}
	tasks, err := todo.New(path).List()
	if err != nil || len(tasks) == 0 {
		return ""
	}
	return todo.Format(tasks)
}

type todoTool struct {
	store *todo.Store
}

func (todoTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "todo",
		Description: "Manage the project task list (TODO.md). Actions: " +
			"add (text), list, done (id or text), remove (id or text), clear. " +
			"Use it to track multi-step work and keep the user informed of progress.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"add", "list", "done", "remove", "clear"},
					"description": "Operation to perform.",
				},
				"text": map[string]any{"type": "string", "description": "Task text (add) or a substring to match (done/remove)."},
				"id":   map[string]any{"type": "integer", "description": "Task id (done/remove)."},
			},
			"required": []string{"action"},
		},
	}
}

func (t todoTool) Run(_ context.Context, arguments string) (string, error) {
	var args struct {
		Action string `json:"action"`
		Text   string `json:"text"`
		ID     int    `json:"id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(args.Action)) {
	case "add":
		tasks, err := t.store.Add(args.Text)
		if err != nil {
			return "", err
		}
		return "added. " + todo.Format(tasks), nil

	case "list", "":
		tasks, err := t.store.List()
		if err != nil {
			return "", err
		}
		return todo.Format(tasks), nil

	case "done":
		tasks, task, err := t.store.Done(args.ID, args.Text)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("completed [%d] %s. %s", task.ID, task.Text, todo.Format(tasks)), nil

	case "remove":
		tasks, err := t.store.Remove(args.ID, args.Text)
		if err != nil {
			return "", err
		}
		return "removed. " + todo.Format(tasks), nil

	case "clear":
		if err := t.store.Clear(); err != nil {
			return "", err
		}
		return "cleared all tasks", nil

	default:
		return "", fmt.Errorf("unknown action %q (want add, list, done, remove, or clear)", args.Action)
	}
}
