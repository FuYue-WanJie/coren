package modelinfo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeCatalog(t *testing.T, dir string) string {
	t.Helper()
	catalog := map[string]any{
		"anthropic": map[string]any{
			"id": "anthropic",
			"models": map[string]any{
				"claude-sonnet-4-5": map[string]any{
					"id":        "claude-sonnet-4-5",
					"name":      "Claude Sonnet 4.5",
					"reasoning": true,
					"tool_call": true,
					"modalities": map[string]any{
						"input":  []string{"text", "image"},
						"output": []string{"text"},
					},
					"limit": map[string]any{"context": 200000, "output": 64000},
					"cost":  map[string]any{"input": 3.0, "output": 15.0, "cache_read": 0.3},
				},
			},
		},
	}
	data, _ := json.Marshal(catalog)
	path := filepath.Join(dir, "models.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCatalogAndLookup(t *testing.T) {
	path := writeCatalog(t, t.TempDir())
	catalog, err := LoadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	info, ok := catalog.Lookup("claude-sonnet-4-5")
	if !ok {
		t.Fatal("model not found")
	}
	if !info.Reasoning || !info.ToolCall {
		t.Errorf("capabilities = %+v", info)
	}
	if info.ContextWindow() != 200000 {
		t.Errorf("context = %d", info.ContextWindow())
	}
	if !info.SupportsInput("image") {
		t.Error("image input should be supported")
	}
	if info.SupportsOutput("image") {
		t.Error("image output should not be supported")
	}
}

func TestResolverConfigOverrideWins(t *testing.T) {
	path := writeCatalog(t, t.TempDir())
	catalog, _ := LoadCatalog(path)

	toolCall := false
	windowSize := 8000
	resolver := &Resolver{
		Catalog: catalog,
		Overrides: map[string]Override{
			"claude-sonnet-4-5": {
				ToolCall: &toolCall,
				Limit:    &Limits{Context: windowSize},
			},
		},
	}
	info := resolver.Resolve(context.Background(), "", "", "claude-sonnet-4-5")
	if info.ToolCall {
		t.Error("override should disable tool_call")
	}
	if info.ContextWindow() != 8000 {
		t.Errorf("context = %d, want override 8000", info.ContextWindow())
	}
	if info.Source != "config" {
		t.Errorf("source = %q", info.Source)
	}
}

func TestProbeEndpointExtractsFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             "some-model",
			"context_length": 32000,
			"top_provider":   map[string]any{"max_completion_tokens": 4096},
			"architecture": map[string]any{
				"input_modalities":  []string{"text", "image"},
				"output_modalities": []string{"text"},
			},
			"supported_parameters": []string{"tools", "reasoning"},
		})
	}))
	defer srv.Close()

	probe, ok := ProbeEndpoint(context.Background(), srv.URL, "", "some-model")
	if !ok {
		t.Fatal("probe should report useful data")
	}
	info := probe.Info
	if info.Limit.Context != 32000 || info.Limit.Output != 4096 {
		t.Errorf("limits = %+v", info.Limit)
	}
	if !info.ToolCall || !info.Reasoning {
		t.Errorf("capabilities = %+v", info)
	}
	if !info.SupportsInput("image") {
		t.Error("image input missing")
	}
	if !probe.HasContext || !probe.HasMaxOutput || !probe.HasModalities {
		t.Errorf("presence flags = %+v", probe)
	}
}

func TestProbeEndpointReportsNothingWhenSparse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Only id/owned_by, like most OpenAI-compatible servers.
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "m", "owned_by": "x"})
	}))
	defer srv.Close()

	if _, ok := ProbeEndpoint(context.Background(), srv.URL, "", "m"); ok {
		t.Error("sparse response should report no useful data")
	}
}

func TestEstimateCost(t *testing.T) {
	info := Info{Cost: Cost{Input: 3, Output: 15, CacheRead: 0.3}}
	// 1M input + 1M output = 3 + 15 = 18
	if got := info.EstimateCost(1_000_000, 1_000_000, 0); got != 18 {
		t.Errorf("cost = %v, want 18", got)
	}
}

func TestLookupPrefersProviderHint(t *testing.T) {
	catalog := &Catalog{Providers: map[string]CatalogProvider{
		"a": {Models: map[string]CatalogModel{"m": {ID: "m", ToolCall: true}}},
		"b": {Models: map[string]CatalogModel{"m": {ID: "m", Reasoning: true}}},
	}}
	info, ok := catalog.LookupIn("b", "m")
	if !ok || info.Provider != "b" {
		t.Errorf("provider = %q, want b", info.Provider)
	}
}

func TestEndpointOverridesCatalogWhenPresent(t *testing.T) {
	// Catalog says tools=true, context=200000. The endpoint explicitly says
	// tools=false and a different context, and omits reasoning.
	catalog := &Catalog{Providers: map[string]CatalogProvider{
		"p": {Models: map[string]CatalogModel{
			"m": {ID: "m", ToolCall: true, Reasoning: true, Limit: Limits{Context: 200000}},
		}},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             "m",
			"context_length": 8000,
			"tool_call":      false,
		})
	}))
	defer srv.Close()

	resolver := &Resolver{Catalog: catalog}
	info := resolver.Resolve(context.Background(), srv.URL, "", "m")

	if info.ToolCall {
		t.Error("API explicitly said tool_call=false; it must win over the catalog")
	}
	if info.ContextWindow() != 8000 {
		t.Errorf("context = %d, want API value 8000", info.ContextWindow())
	}
	// Reasoning is absent from the API, so the catalog value stands.
	if !info.Reasoning {
		t.Error("reasoning should fall back to the catalog value")
	}
	if info.Source != "endpoint+catalog" {
		t.Errorf("source = %q", info.Source)
	}
}

func TestCatalogFillsWhenEndpointSparse(t *testing.T) {
	catalog := &Catalog{Providers: map[string]CatalogProvider{
		"p": {Models: map[string]CatalogModel{
			"m": {ID: "m", ToolCall: true, Limit: Limits{Context: 12345}},
		}},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "m", "owned_by": "x"})
	}))
	defer srv.Close()

	resolver := &Resolver{Catalog: catalog}
	info := resolver.Resolve(context.Background(), srv.URL, "", "m")
	if info.ContextWindow() != 12345 || !info.ToolCall {
		t.Errorf("catalog values should survive a sparse endpoint: %+v", info)
	}
	if info.Source != "catalog:p" {
		t.Errorf("source = %q", info.Source)
	}
}
