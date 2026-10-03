// Package modelinfo resolves a model's capability parameters.
//
// Providers rarely expose context window, tool support, reasoning, modalities,
// or pricing through their API. Coren composes the answer from three sources,
// each overriding the previous:
//
//  1. a cached snapshot of the models.dev catalog (broad coverage, offline),
//  2. whatever extra fields the provider's /models endpoint happens to return,
//  3. explicit configuration overrides.
//
// The resolved Info drives behaviour: whether to send tools, whether to request
// reasoning, how full the context is (for auto-compaction), and cost estimates.
package modelinfo

import (
	"encoding/json"
	"os"
	"strings"
)

// Info describes a model's capabilities and limits.
type Info struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Provider string `json:"provider,omitempty"`
	// Reasoning reports whether the model supports a reasoning/thinking mode.
	Reasoning bool `json:"reasoning"`
	// ToolCall reports whether the model supports function/tool calling.
	ToolCall bool `json:"tool_call"`
	// Attachment reports whether the model accepts file attachments.
	Attachment bool `json:"attachment,omitempty"`
	// Modalities lists supported input/output modalities (text, image, audio, video, pdf).
	Modalities Modalities `json:"modalities"`
	// Limit holds token limits.
	Limit Limits `json:"limit"`
	// Cost holds per-million-token pricing.
	Cost Cost `json:"cost"`
	// Source records which layer produced the authoritative values.
	Source string `json:"-"`
}

// Modalities lists input and output modalities.
type Modalities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

// Limits holds context and output token limits.
type Limits struct {
	Context int `json:"context,omitempty"`
	Input   int `json:"input,omitempty"`
	Output  int `json:"output,omitempty"`
}

// Cost holds per-million-token prices in USD.
type Cost struct {
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

// SupportsInput reports whether the model accepts a modality.
func (i Info) SupportsInput(modality string) bool {
	return containsFold(i.Modalities.Input, modality)
}

// SupportsOutput reports whether the model produces a modality.
func (i Info) SupportsOutput(modality string) bool {
	return containsFold(i.Modalities.Output, modality)
}

// ContextWindow returns the usable context size, preferring the explicit context
// limit and falling back to the input limit.
func (i Info) ContextWindow() int {
	if i.Limit.Context > 0 {
		return i.Limit.Context
	}
	return i.Limit.Input
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

// EstimateCost returns the USD cost for the given token counts.
func (i Info) EstimateCost(inputTokens, outputTokens, cacheReadTokens int) float64 {
	const perMillion = 1_000_000
	cost := float64(inputTokens) / perMillion * i.Cost.Input
	cost += float64(outputTokens) / perMillion * i.Cost.Output
	cost += float64(cacheReadTokens) / perMillion * i.Cost.CacheRead
	return cost
}

// Catalog is a parsed models.dev snapshot: provider -> model id -> Info.
type Catalog struct {
	Providers map[string]CatalogProvider `json:"providers"`
}

// CatalogProvider is one provider entry in the catalog.
type CatalogProvider struct {
	ID     string                  `json:"id"`
	Name   string                  `json:"name,omitempty"`
	Models map[string]CatalogModel `json:"models"`
}

// CatalogModel is one model entry in the catalog.
type CatalogModel struct {
	ID         string     `json:"id"`
	Name       string     `json:"name,omitempty"`
	Reasoning  bool       `json:"reasoning"`
	ToolCall   bool       `json:"tool_call"`
	Attachment bool       `json:"attachment,omitempty"`
	Modalities Modalities `json:"modalities"`
	Limit      Limits     `json:"limit"`
	Cost       Cost       `json:"cost"`
}

// LoadCatalog reads a models.dev-shaped JSON file.
func LoadCatalog(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseCatalog(data)
}

// parseCatalog accepts the raw models.dev object: {providerID: {models: {modelID: {...}}}}.
func parseCatalog(data []byte) (*Catalog, error) {
	var raw map[string]struct {
		ID     string                  `json:"id"`
		Name   string                  `json:"name"`
		Models map[string]CatalogModel `json:"models"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	catalog := &Catalog{Providers: map[string]CatalogProvider{}}
	for id, p := range raw {
		catalog.Providers[id] = CatalogProvider{
			ID:     firstNonEmpty(p.ID, id),
			Name:   p.Name,
			Models: p.Models,
		}
	}
	return catalog, nil
}

// Lookup finds a model across providers. It tries an exact id match, then a
// suffix match (providers often prefix ids), returning the first hit.
func (c *Catalog) Lookup(modelID string) (Info, bool) {
	return c.lookup(modelID, "")
}

// LookupIn prefers a specific provider, falling back to a global search.
func (c *Catalog) LookupIn(provider, modelID string) (Info, bool) {
	return c.lookup(modelID, provider)
}

func (c *Catalog) lookup(modelID, preferredProvider string) (Info, bool) {
	if c == nil || modelID == "" {
		return Info{}, false
	}
	// Preferred provider first.
	if preferredProvider != "" {
		if prov, ok := c.Providers[preferredProvider]; ok {
			if m, ok := prov.Models[modelID]; ok {
				return infoFromCatalog(preferredProvider, m, modelID), true
			}
		}
	}
	// Exact match across providers.
	for provID, prov := range c.Providers {
		if m, ok := prov.Models[modelID]; ok {
			return infoFromCatalog(provID, m, modelID), true
		}
	}
	// Suffix match (e.g. "gpt-4o-mini" inside "openai/gpt-4o-mini").
	for provID, prov := range c.Providers {
		for id, m := range prov.Models {
			if strings.HasSuffix(modelID, id) || strings.HasSuffix(id, modelID) {
				return infoFromCatalog(provID, m, modelID), true
			}
		}
	}
	return Info{}, false
}

func infoFromCatalog(providerID string, m CatalogModel, requestedID string) Info {
	return Info{
		ID:         firstNonEmpty(requestedID, m.ID),
		Name:       firstNonEmpty(m.Name, m.ID),
		Provider:   providerID,
		Reasoning:  m.Reasoning,
		ToolCall:   m.ToolCall,
		Attachment: m.Attachment,
		Modalities: m.Modalities,
		Limit:      m.Limit,
		Cost:       m.Cost,
		Source:     "catalog:" + providerID,
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
