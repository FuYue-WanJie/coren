// Package ask provides the model-facing ask_user tool.
//
// The tool delegates to whichever Asker a shell mounted, so the same tool works
// in any interactive shell without the model or loop knowing which one.
package askplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"coren/pkg/ask"
	"coren/pkg/coren"
	"coren/pkg/llm"
	"coren/pkg/tools"
)

// Config configures the ask_user tool.
type Config struct {
	// Timeout bounds how long to wait for an answer; zero uses a 120s default.
	Timeout time.Duration
}

// Plugin registers the ask_user tool.
type Plugin struct {
	Config Config
}

func (Plugin) ID() string       { return "ask" }
func (Plugin) Inject() []string { return []string{tools.Key} }

func (p Plugin) Apply(ctx coren.Context) error {
	registry, ok := coren.UnwrapKey[tools.Service](ctx, tools.Key)
	if !ok {
		return fmt.Errorf("ask: tools service missing")
	}
	registry.Register(askTool{ctx: ctx, timeout: p.timeout()})
	return nil
}

func (p Plugin) timeout() time.Duration {
	if p.Config.Timeout > 0 {
		return p.Config.Timeout
	}
	return 120 * time.Second
}

// askTool asks the user a question and waits for the answer.
type askTool struct {
	ctx     coren.Context
	timeout time.Duration
}

func (askTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "ask_user",
		Description: "Ask the user a question when you need information or a decision you cannot infer. " +
			"Use sparingly; prefer inferring from context. Returns the user's answer, or an error if " +
			"no interactive user is available or the user does not reply in time.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{"type": "string", "description": "The question to ask."},
			},
			"required": []string{"question"},
		},
	}
}

func (t askTool) Run(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	question := strings.TrimSpace(args.Question)
	if question == "" {
		return "", fmt.Errorf("question is required")
	}

	asker, ok := coren.UnwrapKey[ask.Asker](t.ctx, ask.Key)
	if !ok {
		return "", fmt.Errorf("no interactive user is available to answer questions")
	}

	cctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	answer, err := asker.Ask(cctx, question)
	if err != nil {
		if cctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("the user did not answer within %s", t.timeout)
		}
		return "", err
	}
	return answer, nil
}
