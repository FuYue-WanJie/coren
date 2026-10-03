package builtintools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"coren/pkg/llm"
)

// WriteFile writes a UTF-8 text file, creating parent directories.
type WriteFile struct {
	Root string
}

func (t WriteFile) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "write_file",
		Description: "Write text to a file, creating parent directories as needed.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "File path to write."},
				"content": map[string]any{"type": "string", "description": "Full file content."},
			},
			"required": []string{"path", "content"},
		},
	}
}

func (t WriteFile) Run(_ context.Context, arguments string) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	path, err := resolvePath(t.Root, args.Path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(args.Content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), path), nil
}

// resolvePath joins an optional root with a relative path and rejects escapes.
func resolvePath(root, path string) (string, error) {
	if root == "" {
		return path, nil
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute paths are not allowed when a root is set")
	}
	joined := filepath.Join(root, path)
	cleanRoot := filepath.Clean(root)
	if joined != cleanRoot && !hasPrefixDir(joined, cleanRoot) {
		return "", fmt.Errorf("path %q escapes root", path)
	}
	return joined, nil
}

func hasPrefixDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && len(rel) > 0 && rel[0] != '.'
}
