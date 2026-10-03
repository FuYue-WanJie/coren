package todo

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"coren/pkg/coren"
	"coren/pkg/tools"
)

func mount(t *testing.T, path string) (*coren.Kernel, tools.Service) {
	t.Helper()
	k := coren.NewKernel(context.Background())
	if err := k.Boot(
		coren.PluginFunc{Name: "tools", Mount: func(ctx coren.Context) error {
			ctx.Provide(tools.Key, tools.NewRegistry())
			return nil
		}},
		Plugin{Config: Config{Path: path}},
	); err != nil {
		t.Fatal(err)
	}
	svc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	return k, svc
}

func TestToolRegistered(t *testing.T) {
	k, svc := mount(t, filepath.Join(t.TempDir(), "TODO.md"))
	defer k.Shutdown()
	if _, ok := svc.Get("todo"); !ok {
		t.Fatal("todo tool missing")
	}
}

func TestAddListDoneRemoveFlow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "TODO.md")
	k, svc := mount(t, path)
	defer k.Shutdown()
	ctx := context.Background()

	out, err := svc.RunText(ctx, "todo", `{"action":"add","text":"first task"}`)
	if err != nil || !strings.Contains(out, "first task") {
		t.Fatalf("add: %q, %v", out, err)
	}
	_, _ = svc.RunText(ctx, "todo", `{"action":"add","text":"second task"}`)

	out, err = svc.RunText(ctx, "todo", `{"action":"list"}`)
	if err != nil || !strings.Contains(out, "2 open") {
		t.Fatalf("list: %q, %v", out, err)
	}

	out, err = svc.RunText(ctx, "todo", `{"action":"done","id":1}`)
	if err != nil || !strings.Contains(out, "completed [1] first task") {
		t.Fatalf("done: %q, %v", out, err)
	}

	out, err = svc.RunText(ctx, "todo", `{"action":"list"}`)
	if err != nil || !strings.Contains(out, "1 open, 1 done") {
		t.Fatalf("list after done: %q, %v", out, err)
	}

	out, err = svc.RunText(ctx, "todo", `{"action":"remove","text":"second"}`)
	if err != nil || strings.Contains(out, "second task") {
		t.Fatalf("remove: %q, %v", out, err)
	}
}

func TestClearAction(t *testing.T) {
	k, svc := mount(t, filepath.Join(t.TempDir(), "TODO.md"))
	defer k.Shutdown()
	ctx := context.Background()

	_, _ = svc.RunText(ctx, "todo", `{"action":"add","text":"x"}`)
	out, err := svc.RunText(ctx, "todo", `{"action":"clear"}`)
	if err != nil || !strings.Contains(out, "cleared") {
		t.Fatalf("clear: %q, %v", out, err)
	}
	out, _ = svc.RunText(ctx, "todo", `{"action":"list"}`)
	if !strings.Contains(out, "no tasks") {
		t.Errorf("list after clear = %q", out)
	}
}

func TestUnknownActionFails(t *testing.T) {
	k, svc := mount(t, filepath.Join(t.TempDir(), "TODO.md"))
	defer k.Shutdown()
	if _, err := svc.RunText(context.Background(), "todo", `{"action":"bogus"}`); err == nil {
		t.Fatal("unknown action should error")
	}
}

func TestDisabledWhenNoPath(t *testing.T) {
	k, svc := mount(t, "")
	defer k.Shutdown()
	if _, ok := svc.Get("todo"); ok {
		t.Error("todo tool should not register without a path")
	}
}

func TestLoadFormatsTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "TODO.md")
	k, svc := mount(t, path)
	defer k.Shutdown()
	_, _ = svc.RunText(context.Background(), "todo", `{"action":"add","text":"pending item"}`)

	got := Load(path)
	if !strings.Contains(got, "pending item") {
		t.Errorf("Load = %q", got)
	}
}
