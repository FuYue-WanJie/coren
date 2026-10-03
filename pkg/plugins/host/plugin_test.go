package host

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"coren/pkg/coren"
	"coren/pkg/tools"
)

// toolsProvider mounts an empty tools registry so the host can register into it.
type toolsProvider struct{}

func (toolsProvider) ID() string       { return "tools.test" }
func (toolsProvider) Inject() []string { return nil }
func (toolsProvider) Apply(ctx coren.Context) error {
	ctx.Provide(tools.Key, tools.NewRegistry())
	return nil
}

// buildEchoPlugin compiles the example plugin and returns its path.
func buildEchoPlugin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "echoplugin")
	cmd := exec.Command("go", "build", "-o", bin, "coren/examples/echoplugin")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot build example plugin: %v", err)
	}
	return bin
}

func TestHostLaunchesPluginAndBridgesTool(t *testing.T) {
	bin := buildEchoPlugin(t)

	kernel := coren.NewKernel(context.Background())
	plugin := Plugin{
		Plugins: []PluginConfig{{Name: "echo", Command: bin}},
	}
	if err := kernel.Boot(toolsProvider{}, plugin); err != nil {
		t.Fatal(err)
	}
	defer kernel.Shutdown()
	if err := kernel.Start(); err != nil {
		t.Fatal(err)
	}

	registry, ok := coren.UnwrapKey[tools.Service](kernel.Context(), tools.Key)
	if !ok {
		t.Fatal("tools service missing")
	}
	if _, ok := registry.Get("echo"); !ok {
		t.Fatal("echo tool not registered by host")
	}
	out, err := registry.RunText(context.Background(), "echo", `{"text":"hi"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "echo: hi" {
		t.Fatalf("output = %q", out)
	}
}

func TestHostRejectsDuplicateTool(t *testing.T) {
	bin := buildEchoPlugin(t)

	kernel := coren.NewKernel(context.Background())
	plugin := Plugin{
		Plugins: []PluginConfig{
			{Name: "echo1", Command: bin},
			{Name: "echo2", Command: bin},
		},
	}
	if err := kernel.Boot(toolsProvider{}, plugin); err != nil {
		t.Fatal(err)
	}
	defer kernel.Shutdown()
	if err := kernel.Start(); err == nil {
		t.Fatal("expected duplicate tool to fail start")
	}
}
