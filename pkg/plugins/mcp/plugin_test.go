package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"coren/pkg/coren"
	"coren/pkg/tools"
)

func mockServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ID == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities": map[string]any{
					"tools": map[string]any{}, "resources": map[string]any{}, "prompts": map[string]any{},
				},
				"serverInfo": map[string]any{"name": "mock"},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name": "echo", "description": "Echo.", "inputSchema": map[string]any{"type": "object"},
			}}}
		case "tools/call":
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "pong"}}}
		case "resources/list":
			result = map[string]any{"resources": []map[string]any{{"uri": "mock://x", "name": "x"}}}
		case "resources/read":
			result = map[string]any{"contents": []map[string]any{{"uri": "mock://x", "text": "resource body"}}}
		case "prompts/list":
			result = map[string]any{"prompts": []map[string]any{{"name": "greet"}}}
		case "prompts/get":
			result = map[string]any{"messages": []map[string]any{{"role": "user", "content": map[string]any{"type": "text", "text": "hi"}}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
}

func mount(t *testing.T, cfg Config) *coren.Kernel {
	t.Helper()
	k := coren.NewKernel(context.Background())
	err := k.Boot(
		coren.PluginFunc{Name: "tools", Mount: func(ctx coren.Context) error {
			ctx.Provide(tools.Key, tools.NewRegistry())
			return nil
		}},
		Plugin{Config: cfg},
	)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestPluginRegistersNamespacedTools(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	k := mount(t, Config{Servers: []ServerConfig{{Name: "mock", URL: srv.URL}}})
	defer k.Shutdown()

	registry, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	if _, ok := registry.Get("mock__echo"); !ok {
		t.Errorf("namespaced tool missing; have %v", specNames(registry))
	}

	out, err := registry.RunText(context.Background(), "mock__echo", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "pong" {
		t.Errorf("tool output = %q", out)
	}
}

func TestPluginRegistersResourceAndPromptTools(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	k := mount(t, Config{Servers: []ServerConfig{{Name: "s", URL: srv.URL}}})
	defer k.Shutdown()

	registry, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	for _, name := range []string{"s__list_resources", "s__read_resource", "s__list_prompts", "s__get_prompt"} {
		if _, ok := registry.Get(name); !ok {
			t.Errorf("missing %s; have %v", name, specNames(registry))
		}
	}

	out, err := registry.RunText(context.Background(), "s__read_resource", `{"uri":"mock://x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "resource body" {
		t.Errorf("resource = %q", out)
	}
}

func TestPluginNamespaceCanBeDisabled(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	disabled := false
	k := mount(t, Config{
		Servers:   []ServerConfig{{Name: "mock", URL: srv.URL}},
		Namespace: &disabled,
	})
	defer k.Shutdown()

	registry, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	if _, ok := registry.Get("echo"); !ok {
		t.Errorf("unnamespaced tool missing; have %v", specNames(registry))
	}
}

func TestPluginSkipsDisabledServer(t *testing.T) {
	disabled := false
	k := mount(t, Config{Servers: []ServerConfig{{Name: "off", URL: "http://127.0.0.1:1", Enabled: &disabled}}})
	defer k.Shutdown()

	registry, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	if len(registry.Specs()) != 0 {
		t.Errorf("disabled server registered tools: %v", specNames(registry))
	}
}

func TestPluginSkipsUnreachableServer(t *testing.T) {
	// A server that fails to initialize must be skipped, not abort boot.
	k := mount(t, Config{Servers: []ServerConfig{{Name: "dead", URL: "http://127.0.0.1:1"}}})
	defer k.Shutdown()

	registry, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	if len(registry.Specs()) != 0 {
		t.Errorf("unreachable server registered tools: %v", specNames(registry))
	}
}

func specNames(registry tools.Service) string {
	var names []string
	for _, s := range registry.Specs() {
		names = append(names, s.Name)
	}
	return strings.Join(names, ",")
}
