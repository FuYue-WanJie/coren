package builtintools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"coren/pkg/llm"
)

// Shell runs a command through the system shell.
type Shell struct {
	// Dir is the working directory; empty means the process default.
	Dir string
	// Timeout bounds execution; zero means a 60s default.
	Timeout time.Duration
}

func (t Shell) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "run_shell",
		Description: "Run a shell command and return its combined stdout/stderr.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "Command line to execute."},
			},
			"required": []string{"command"},
		},
	}
}

func (t Shell) Run(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", args.Command)
	cmd.Dir = t.Dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	output := out.String()
	if runCtx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("command timed out after %s", timeout)
	}
	if err != nil {
		return output, fmt.Errorf("command failed: %w", err)
	}
	return output, nil
}
