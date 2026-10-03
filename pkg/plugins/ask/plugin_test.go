package askplugin

import (
	"context"
	"strings"
	"testing"
	"time"

	"coren/pkg/ask"
	"coren/pkg/coren"
	"coren/pkg/tools"
)

// fakeAsker answers with a fixed value or an error.
type fakeAsker struct {
	answer string
	err    error
}

func (f fakeAsker) Ask(_ context.Context, _ string) (string, error) {
	return f.answer, f.err
}

func mount(t *testing.T, asker ask.Asker, timeout time.Duration) *coren.Kernel {
	t.Helper()
	k := coren.NewKernel(context.Background())
	plugins := []coren.Plugin{
		coren.PluginFunc{Name: "tools", Mount: func(ctx coren.Context) error {
			ctx.Provide(tools.Key, tools.NewRegistry())
			return nil
		}},
		Plugin{Config: Config{Timeout: timeout}},
	}
	if asker != nil {
		plugins = append(plugins, coren.PluginFunc{Name: "asker", Mount: func(ctx coren.Context) error {
			ctx.Provide(ask.Key, asker)
			return nil
		}})
	}
	if err := k.Boot(plugins...); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestAskToolRegistered(t *testing.T) {
	k := mount(t, fakeAsker{answer: "yes"}, time.Second)
	defer k.Shutdown()

	svc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	if _, ok := svc.Get("ask_user"); !ok {
		t.Error("ask_user tool missing")
	}
}

func TestAskToolReturnsAnswer(t *testing.T) {
	k := mount(t, fakeAsker{answer: "42"}, time.Second)
	defer k.Shutdown()

	svc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	out, err := svc.RunText(context.Background(), "ask_user", `{"question":"what is the answer?"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "42" {
		t.Errorf("answer = %q", out)
	}
}

func TestAskToolWithoutAskerFailsClearly(t *testing.T) {
	k := mount(t, nil, time.Second)
	defer k.Shutdown()

	svc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	_, err := svc.RunText(context.Background(), "ask_user", `{"question":"hi"}`)
	if err == nil {
		t.Fatal("expected an error when no asker is mounted")
	}
	if !strings.Contains(err.Error(), "no interactive user") {
		t.Errorf("error = %v, want a clear 'no interactive user' message", err)
	}
}

func TestAskToolRejectsEmptyQuestion(t *testing.T) {
	k := mount(t, fakeAsker{answer: "x"}, time.Second)
	defer k.Shutdown()

	svc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	if _, err := svc.RunText(context.Background(), "ask_user", `{"question":"  "}`); err == nil {
		t.Fatal("expected error for empty question")
	}
}

func TestAskToolHonorsContextCancellation(t *testing.T) {
	// Asker that blocks until its context is done.
	blocking := blockingAsker{}
	k := mount(t, blocking, 5*time.Second)
	defer k.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	svc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	if _, err := svc.RunText(ctx, "ask_user", `{"question":"hi"}`); err == nil {
		t.Fatal("expected cancellation error")
	}
}

type blockingAsker struct{}

func (blockingAsker) Ask(ctx context.Context, _ string) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}
