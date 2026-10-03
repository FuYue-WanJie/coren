// Package openai implements OpenAI-compatible Chat Completions and Responses providers.
package openaillm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"coren/pkg/llm"
)

// ChatAdapter talks to the OpenAI Chat Completions API (`/v1/chat/completions`).
// Any OpenAI-compatible endpoint can be used by setting BaseURL.
type ChatAdapter struct {
	BaseURL string // e.g. https://api.openai.com/v1
	APIKey  string
	Client  *http.Client
	// MaxRetries overrides the transient-failure retry count (0 uses the default).
	MaxRetries int
}

func (p *ChatAdapter) Name() string { return "openai-chat" }

func (p *ChatAdapter) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	body := p.buildRequest(req)
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{}
	if p.APIKey != "" {
		headers["Authorization"] = "Bearer " + p.APIKey
	}
	resp, err := postWithRetry(ctx, p.client(),
		strings.TrimRight(p.BaseURL, "/")+"/chat/completions", headers, payload, p.retry())
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return nil, fmt.Errorf("chat request failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	out := make(chan llm.Chunk)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		parseChatSSE(ctx, resp.Body, out)
	}()
	return out, nil
}

func (p *ChatAdapter) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
}

// retry returns the effective retry policy.
func (p *ChatAdapter) retry() retryPolicy {
	policy := defaultRetryPolicy
	if p.MaxRetries > 0 {
		policy.MaxAttempts = p.MaxRetries + 1
	}
	return policy
}

func (p *ChatAdapter) buildRequest(req llm.Request) map[string]any {
	messages := make([]map[string]any, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		messages = append(messages, toChatMessages(m)...)
	}
	body := map[string]any{
		"model":    req.Model,
		"messages": messages,
		"stream":   true,
	}
	if len(req.Tools) > 0 {
		body["tools"] = toChatTools(req.Tools)
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		body["max_tokens"] = *req.MaxTokens
	}
	// Prompt caching: an OpenAI-compatible server reuses the cached prefix keyed
	// by prompt_cache_key, so a stable system prompt and tool list cost less.
	if req.SessionID != "" && req.CacheRetention != "none" {
		body["prompt_cache_key"] = clampCacheKey(req.SessionID)
		if req.CacheRetention == "long" {
			body["prompt_cache_retention"] = "24h"
		}
	}
	if effort := req.Reasoning.EffortValue(); effort != "" {
		body["reasoning_effort"] = effort
	}
	return body
}

// clampCacheKey keeps a cache key within the provider's accepted length.
func clampCacheKey(key string) string {
	const max = 64
	if len(key) <= max {
		return key
	}
	return key[:max]
}

// toChatMessages expands a Coren message into one or more OpenAI chat messages.
// A tool result carrying media becomes a tool message plus a following user
// message with image/audio parts, since the tool role accepts text only.
func toChatMessages(m llm.Message) []map[string]any {
	switch m.Role {
	case llm.RoleTool:
		out := []map[string]any{{
			"role":         "tool",
			"tool_call_id": m.ToolCallID,
			"content":      firstNonEmptyStr(m.Text, "(no output)"),
		}}
		if media := mediaParts(m.Parts); len(media) > 0 {
			out = append(out, map[string]any{"role": "user", "content": media})
		}
		return out
	case llm.RoleAssistant:
		msg := map[string]any{"role": "assistant", "content": m.Text}
		if len(m.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(m.ToolCalls))
			for _, c := range m.ToolCalls {
				calls = append(calls, map[string]any{
					"id":   c.ID,
					"type": "function",
					"function": map[string]any{
						"name":      c.Name,
						"arguments": c.Arguments,
					},
				})
			}
			msg["tool_calls"] = calls
		}
		return []map[string]any{msg}
	default:
		if media := mediaParts(m.Parts); len(media) > 0 {
			content := make([]map[string]any, 0, len(media)+1)
			if m.Text != "" {
				content = append(content, map[string]any{"type": "text", "text": m.Text})
			}
			content = append(content, media...)
			return []map[string]any{{"role": string(m.Role), "content": content}}
		}
		return []map[string]any{{"role": string(m.Role), "content": m.Text}}
	}
}

// mediaParts converts image/audio ContentParts to OpenAI content parts.
func mediaParts(parts []llm.ContentPart) []map[string]any {
	var out []map[string]any
	for _, p := range parts {
		switch p.Type {
		case llm.PartImage:
			url := p.URI
			if url == "" && p.Data != "" {
				url = "data:" + mimeOr(p.MimeType, "image/png") + ";base64," + p.Data
			}
			if url != "" {
				out = append(out, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
			}
		case llm.PartAudio:
			// OpenAI-compatible audio input uses an input_audio part with a
			// format field derived from the MIME subtype.
			if p.Data != "" {
				out = append(out, map[string]any{
					"type":        "input_audio",
					"input_audio": map[string]any{"data": p.Data, "format": mimeSubtype(p.MimeType)},
				})
			}
		case llm.PartVideo:
			// No standard inline video part; reference by URL when available.
			if p.URI != "" {
				out = append(out, map[string]any{"type": "video_url", "video_url": map[string]any{"url": p.URI}})
			}
		}
	}
	return out
}

func mimeOr(mime, fallback string) string {
	if mime == "" {
		return fallback
	}
	return mime
}

func mimeSubtype(mime string) string {
	if i := strings.LastIndex(mime, "/"); i >= 0 && i+1 < len(mime) {
		return mime[i+1:]
	}
	return mime
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func toChatTools(specs []llm.ToolSpec) []map[string]any {
	tools := make([]map[string]any, 0, len(specs))
	for _, s := range specs {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        s.Name,
				"description": s.Description,
				"parameters":  s.Parameters,
			},
		})
	}
	return tools
}

// chatStreamFrame is one SSE data object from the chat completions stream.
type chatStreamFrame struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func parseChatSSE(ctx context.Context, body io.Reader, out chan<- llm.Chunk) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	// Tool-call arguments stream in fragments keyed by index; accumulate then emit.
	type pending struct {
		id   string
		name string
		args strings.Builder
	}
	pendingCalls := map[int]*pending{}

	for scanner.Scan() {
		if ctx.Err() != nil {
			out <- llm.Chunk{Err: ctx.Err()}
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var frame chatStreamFrame
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			continue
		}
		if frame.Usage != nil {
			out <- llm.Chunk{Usage: &llm.Usage{
				InputTokens:  frame.Usage.PromptTokens,
				OutputTokens: frame.Usage.CompletionTokens,
			}}
		}
		if len(frame.Choices) == 0 {
			continue
		}
		delta := frame.Choices[0].Delta
		if delta.Content != "" {
			out <- llm.Chunk{TextDelta: delta.Content}
		}
		for _, tc := range delta.ToolCalls {
			p := pendingCalls[tc.Index]
			if p == nil {
				p = &pending{}
				pendingCalls[tc.Index] = p
			}
			if tc.ID != "" {
				p.id = tc.ID
			}
			if tc.Function.Name != "" {
				p.name = tc.Function.Name
			}
			p.args.WriteString(tc.Function.Arguments)
		}
	}
	if err := scanner.Err(); err != nil {
		out <- llm.Chunk{Err: err}
		return
	}
	for _, p := range pendingCalls {
		if p.name == "" {
			continue
		}
		args := p.args.String()
		if args == "" {
			args = "{}"
		}
		out <- llm.Chunk{ToolCall: &llm.ToolCall{ID: p.id, Name: p.name, Arguments: args}}
	}
	out <- llm.Chunk{Done: true}
}
