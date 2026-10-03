package openaillm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ModelInfo is one entry from the provider's model list.
type ModelInfo struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by,omitempty"`
	Created int64  `json:"created,omitempty"`
}

// ListModels queries an OpenAI-compatible /models endpoint.
func ListModels(ctx context.Context, baseURL, apiKey string) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return nil, fmt.Errorf("models request failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var payload struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}
	return payload.Data, nil
}

// FilterModels keeps ids containing any of the given substrings (case-insensitive).
// An empty filter returns all models.
func FilterModels(models []ModelInfo, filters []string) []ModelInfo {
	if len(filters) == 0 {
		return models
	}
	var out []ModelInfo
	for _, m := range models {
		lower := strings.ToLower(m.ID)
		for _, f := range filters {
			if f != "" && strings.Contains(lower, strings.ToLower(f)) {
				out = append(out, m)
				break
			}
		}
	}
	return out
}
