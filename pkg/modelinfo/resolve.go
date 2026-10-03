package modelinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Override is a configuration-provided capability entry. Pointer fields let a
// caller set one value without clobbering the rest.
type Override struct {
	Reasoning  *bool       `json:"reasoning,omitempty"`
	ToolCall   *bool       `json:"tool_call,omitempty"`
	Attachment *bool       `json:"attachment,omitempty"`
	Modalities *Modalities `json:"modalities,omitempty"`
	Limit      *Limits     `json:"limit,omitempty"`
	Cost       *Cost       `json:"cost,omitempty"`
}

// Resolver composes model info from a catalog, an endpoint probe, and overrides.
type Resolver struct {
	Catalog   *Catalog
	Overrides map[string]Override
	// ProviderHint prefers a catalog provider when ids are ambiguous.
	ProviderHint string
}

// Probe is the endpoint's contribution, with presence flags so an explicit
// value from the API overrides the catalog while absent fields do not.
type Probe struct {
	Info Info
	// Present marks which fields the endpoint actually reported.
	HasReasoning  bool
	HasToolCall   bool
	HasAttachment bool
	HasModalities bool
	HasContext    bool
	HasMaxOutput  bool
}

// Any reports whether the probe carried at least one field.
func (p Probe) Any() bool {
	return p.HasReasoning || p.HasToolCall || p.HasAttachment ||
		p.HasModalities || p.HasContext || p.HasMaxOutput
}

// Resolve returns the best-known Info for a model id.
//
// Precedence, highest first: configuration override, then the provider's own
// API fields, then the catalog. The API is authoritative because it describes
// the exact deployment being called; the catalog fills what the API omits.
func (r *Resolver) Resolve(ctx context.Context, baseURL, apiKey, modelID string) Info {
	var info Info
	fromCatalog := false

	// 1. Catalog as the baseline.
	if c, ok := r.Catalog.LookupIn(r.ProviderHint, modelID); ok {
		info = c
		fromCatalog = true
	}
	info.ID = modelID

	// 2. Endpoint probe overrides the catalog for the fields it reports.
	if probe, ok := ProbeEndpoint(ctx, baseURL, apiKey, modelID); ok {
		info = probe.overlayOnto(info)
		if fromCatalog {
			info.Source = "endpoint+catalog"
		} else {
			info.Source = "endpoint"
		}
	}
	if info.Source == "" && fromCatalog {
		info.Source = "catalog:" + info.Provider
	}

	// 3. Config override wins over everything.
	if ov, ok := r.Overrides[modelID]; ok {
		info = applyOverride(info, ov)
	}
	return info
}

// overlayOnto returns base with the probe's present fields applied on top.
func (p Probe) overlayOnto(base Info) Info {
	out := base
	if p.HasReasoning {
		out.Reasoning = p.Info.Reasoning
	}
	if p.HasToolCall {
		out.ToolCall = p.Info.ToolCall
	}
	if p.HasAttachment {
		out.Attachment = p.Info.Attachment
	}
	if p.HasModalities {
		out.Modalities = p.Info.Modalities
	}
	if p.HasContext {
		out.Limit.Context = p.Info.Limit.Context
	}
	if p.HasMaxOutput {
		out.Limit.Output = p.Info.Limit.Output
	}
	if out.Provider == "" {
		out.Provider = p.Info.Provider
	}
	if out.Name == "" {
		out.Name = p.Info.Name
	}
	return out
}

func applyOverride(info Info, ov Override) Info {
	if ov.Reasoning != nil {
		info.Reasoning = *ov.Reasoning
	}
	if ov.ToolCall != nil {
		info.ToolCall = *ov.ToolCall
	}
	if ov.Attachment != nil {
		info.Attachment = *ov.Attachment
	}
	if ov.Modalities != nil {
		info.Modalities = *ov.Modalities
	}
	if ov.Limit != nil {
		info.Limit = *ov.Limit
	}
	if ov.Cost != nil {
		info.Cost = *ov.Cost
	}
	info.Source = "config"
	return info
}

// probeShape captures the extra fields some OpenAI-compatible servers add to a
// model entry beyond id/owned_by. Only the fields present are applied.
type probeShape struct {
	ContextLength int `json:"context_length"`
	MaxTokens     int `json:"max_tokens"`
	TopProvider   struct {
		ContextLength int `json:"context_length"`
		MaxCompletion int `json:"max_completion_tokens"`
	} `json:"top_provider"`
	Architecture struct {
		Modality         string   `json:"modality"`
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	SupportedParams []string `json:"supported_parameters"`
}

// ProbeEndpoint asks the provider for a model's metadata and extracts any
// capability fields it exposes. It reports presence per field, so the caller
// can let the API override the catalog where it speaks and fall back where it
// stays silent. Most OpenAI-compatible servers expose little; ok is then false.
func ProbeEndpoint(ctx context.Context, baseURL, apiKey, modelID string) (Probe, bool) {
	if baseURL == "" || modelID == "" {
		return Probe{}, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(baseURL, "/")+"/models/"+modelID, nil)
	if err != nil {
		return Probe{}, false
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Probe{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Probe{}, false
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Probe{}, false
	}

	// Decode into a map so field presence is observable even when a value is
	// false or zero.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Probe{}, false
	}
	var shape probeShape
	if err := json.Unmarshal(data, &shape); err != nil {
		return Probe{}, false
	}

	probe := Probe{Info: Info{ID: modelID, Source: "endpoint"}}

	// Context window: context_length, or top_provider.context_length.
	if _, ok := raw["context_length"]; ok && shape.ContextLength > 0 {
		probe.Info.Limit.Context = shape.ContextLength
		probe.HasContext = true
	}
	if shape.TopProvider.ContextLength > 0 {
		probe.Info.Limit.Context = shape.TopProvider.ContextLength
		probe.HasContext = true
	}
	if _, ok := raw["max_tokens"]; ok && shape.MaxTokens > 0 {
		probe.Info.Limit.Output = shape.MaxTokens
		probe.HasMaxOutput = true
	}
	if shape.TopProvider.MaxCompletion > 0 {
		probe.Info.Limit.Output = shape.TopProvider.MaxCompletion
		probe.HasMaxOutput = true
	}

	// Modalities.
	if len(shape.Architecture.InputModalities) > 0 {
		probe.Info.Modalities.Input = shape.Architecture.InputModalities
		probe.HasModalities = true
	} else if shape.Architecture.Modality != "" {
		probe.Info.Modalities.Input = strings.Split(shape.Architecture.Modality, "->")
		probe.HasModalities = true
	}
	if len(shape.Architecture.OutputModalities) > 0 {
		probe.Info.Modalities.Output = shape.Architecture.OutputModalities
		probe.HasModalities = true
	}

	// supported_parameters names capabilities explicitly.
	if _, ok := raw["supported_parameters"]; ok {
		probe.HasToolCall = true
		probe.HasReasoning = true
		for _, p := range shape.SupportedParams {
			switch p {
			case "tools", "tool_choice":
				probe.Info.ToolCall = true
			case "reasoning", "include_reasoning":
				probe.Info.Reasoning = true
			}
		}
	}
	// Explicit boolean capability fields some servers add.
	if v, ok := raw["tool_call"]; ok {
		probe.HasToolCall = true
		probe.Info.ToolCall = parseBool(v)
	}
	if v, ok := raw["reasoning"]; ok {
		probe.HasReasoning = true
		probe.Info.Reasoning = parseBool(v)
	}

	if !probe.Any() {
		return Probe{}, false
	}
	return probe, true
}

func parseBool(raw json.RawMessage) bool {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b
	}
	return false
}

// CatalogURL is the default source for the capability catalog.
const CatalogURL = "https://models.dev/api.json"

// CachePath returns the cached catalog path for a config directory.
func CachePath(configDir string) string {
	return filepath.Join(configDir, "models.json")
}

// FetchCatalog downloads a fresh catalog snapshot to dest, creating parent dirs.
func FetchCatalog(ctx context.Context, url, dest string) error {
	if url == "" {
		url = CatalogURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("catalog fetch: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if _, err := parseCatalog(data); err != nil {
		return fmt.Errorf("catalog invalid: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o644)
}
