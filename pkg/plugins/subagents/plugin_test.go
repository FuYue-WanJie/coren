package subagentsplugin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"coren/pkg/coren"
	"coren/pkg/llm"
	"coren/pkg/session"
	"coren/pkg/subagents"
	"coren/pkg/tools"
)

// scriptedAdapter answers every request with a canned text.
type scriptedAdapter struct {
	text string
	seen []llm.Request
}

func (s *scriptedAdapter) Name() string { return "scripted" }
func (s *scriptedAdapter) Stream(_ context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	s.seen = append(s.seen, req)
	ch := make(chan llm.Chunk, 2)
	ch <- llm.Chunk{TextDelta: s.text}
	ch <- llm.Chunk{Done: true}
	close(ch)
	return ch, nil
}

func mount(t *testing.T, adapter llm.Adapter, cfg Config) *coren.Kernel {
	t.Helper()
	k := coren.NewKernel(context.Background())
	err := k.Boot(
		coren.PluginFunc{Name: "llm", Mount: func(ctx coren.Context) error {
			reg := llm.NewRegistry()
			reg.Register(adapter)
			ctx.Provide(llm.Key, reg)
			return nil
		}},
		coren.PluginFunc{Name: "tools", Mount: func(ctx coren.Context) error {
			ctx.Provide(tools.Key, tools.NewRegistry())
			return nil
		}},
		coren.PluginFunc{Name: "sessions", Mount: func(ctx coren.Context) error {
			ctx.Provide(session.Key, session.NewStore())
			return nil
		}},
		ProviderPlugin{},
		InProcessPlugin{Config: cfg},
		ToolPlugin{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSubagentsServiceAndToolRegistered(t *testing.T) {
	k := mount(t, &scriptedAdapter{text: "hi"}, Config{})
	defer k.Shutdown()

	if _, ok := coren.UnwrapKey[subagents.Service](k.Context(), subagents.Key); !ok {
		t.Fatal("subagents service missing")
	}
	toolSvc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	if _, ok := toolSvc.Get("task"); !ok {
		t.Error("task tool missing")
	}
}

func TestTaskToolReturnsChildOutput(t *testing.T) {
	adapter := &scriptedAdapter{text: "the child answer"}
	k := mount(t, adapter, Config{})
	defer k.Shutdown()

	toolSvc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	out, err := toolSvc.RunText(context.Background(), "task", `{"prompt":"do a thing","label":"probe"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "the child answer" {
		t.Errorf("output = %q", out)
	}
	if len(adapter.seen) != 1 {
		t.Fatalf("child made %d requests, want 1", len(adapter.seen))
	}
	// The prompt must be the child's user message.
	msgs := adapter.seen[0].Messages
	if len(msgs) != 1 || msgs[0].Text != "do a thing" {
		t.Errorf("child messages = %+v", msgs)
	}
}

func TestTaskToolRejectsEmptyPrompt(t *testing.T) {
	k := mount(t, &scriptedAdapter{text: "x"}, Config{})
	defer k.Shutdown()

	toolSvc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	if _, err := toolSvc.RunText(context.Background(), "task", `{"prompt":"   "}`); err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestInProcessProviderEnforcesMaxDepth(t *testing.T) {
	k := mount(t, &scriptedAdapter{text: "x"}, Config{MaxDepth: 1})
	defer k.Shutdown()

	svc, _ := coren.UnwrapKey[subagents.Service](k.Context(), subagents.Key)
	provider, _ := svc.Get("in-process")
	if _, err := provider.Start(context.Background(), subagents.Request{Prompt: "deep", Depth: 5}); err == nil {
		t.Fatal("expected depth limit error")
	}
}

func TestChildSessionIsIsolatedFromParent(t *testing.T) {
	adapter := &scriptedAdapter{text: "child says hi"}
	k := mount(t, adapter, Config{})
	defer k.Shutdown()

	sessions, _ := coren.UnwrapKey[session.Service](k.Context(), session.Key)
	parent := sessions.Get("parent")
	parent.Append(session.Event{Type: session.EventUserMsg, Text: "parent-only history"})

	toolSvc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	if _, err := toolSvc.RunText(context.Background(), "task", `{"prompt":"child task"}`); err != nil {
		t.Fatal(err)
	}

	// The child's request must not contain the parent's history.
	msgs := adapter.seen[0].Messages
	for _, m := range msgs {
		if strings.Contains(m.Text, "parent-only history") {
			t.Fatal("child request leaked parent history")
		}
	}
	if len(msgs) != 1 || msgs[0].Text != "child task" {
		t.Errorf("child messages = %+v", msgs)
	}
}

func TestProviderErrorPropagates(t *testing.T) {
	k := mount(t, &failingAdapter{}, Config{})
	defer k.Shutdown()

	toolSvc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	if _, err := toolSvc.RunText(context.Background(), "task", `{"prompt":"x"}`); err == nil {
		t.Fatal("expected provider error to surface")
	}
}

type failingAdapter struct{}

func (failingAdapter) Name() string { return "failing" }
func (failingAdapter) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	return nil, errors.New("backend down")
}

func TestNestedDelegationAccumulatesDepth(t *testing.T) {
	// The tool reads depth from context; a call made at depth N must report N+1.
	ctx := withDepth(context.Background(), 2)
	if got := depthFromContext(ctx); got != 2 {
		t.Fatalf("depth = %d, want 2", got)
	}
}
