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

// ResponsesAdapter talks to the OpenAI Responses API (`/v1/responses`).
type ResponsesAdapter struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
	// MaxRetries overrides the transient-failure retry count (0 uses the default).
	MaxRetries int
}

func (p *ResponsesAdapter) Name() string { return "openai-responses" }

func (p *ResponsesAdapter) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	payload, err := json.Marshal(p.buildRequest(req))
	if err != nil {
		return nil, err
	}
	headers := map[string]string{}
	if p.APIKey != "" {
		headers["Authorization"] = "Bearer " + p.APIKey
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	policy := defaultRetryPolicy
	if p.MaxRetries > 0 {
		policy.MaxAttempts = p.MaxRetries + 1
	}
	resp, err := postWithRetry(ctx, client,
		strings.TrimRight(p.BaseURL, "/")+"/responses", headers, payload, policy)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return nil, fmt.Errorf("responses request failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	out := make(chan llm.Chunk)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		parseResponsesSSE(ctx, resp.Body, out)
	}()
	return out, nil
}

func (p *ResponsesAdapter) buildRequest(req llm.Request) map[string]any {
	input := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		input = append(input, toResponsesInput(m)...)
	}
	body := map[string]any{
		"model":  req.Model,
		"input":  input,
		"stream": true,
	}
	if req.System != "" {
		body["instructions"] = req.System
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, s := range req.Tools {
			tools = append(tools, map[string]any{
				"type":        "function",
				"name":        s.Name,
				"description": s.Description,
				"parameters":  s.Parameters,
			})
		}
		body["tools"] = tools
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		body["max_output_tokens"] = *req.MaxTokens
	}
	if req.SessionID != "" && req.CacheRetention != "none" {
		body["prompt_cache_key"] = clampCacheKey(req.SessionID)
		if req.CacheRetention == "long" {
			body["prompt_cache_retention"] = "24h"
		}
	}
	if effort := req.Reasoning.EffortValue(); effort != "" {
		// Responses API nests reasoning effort under a "reasoning" object.
		body["reasoning"] = map[string]any{"effort": effort}
	}
	return body
}

// toResponsesInput converts a Coren message into one or more Responses input items.
func toResponsesInput(m llm.Message) []map[string]any {
	switch m.Role {
	case llm.RoleTool:
		items := []map[string]any{{
			"type":    "function_call_output",
			"call_id": m.ToolCallID,
			"output":  m.Text,
		}}
		// Media from a tool result is delivered as a following user message.
		if content := responsesContent(m.Parts); len(content) > 0 {
			items = append(items, map[string]any{
				"type":    "message",
				"role":    "user",
				"content": content,
			})
		}
		return items
	case llm.RoleAssistant:
		var items []map[string]any
		if content := responsesContent(m.Parts); len(content) > 0 {
			items = append(items, map[string]any{
				"type":    "message",
				"role":    "assistant",
				"content": content,
			})
		} else if m.Text != "" {
			items = append(items, map[string]any{
				"type":    "message",
				"role":    "assistant",
				"content": m.Text,
			})
		}
		for _, c := range m.ToolCalls {
			items = append(items, map[string]any{
				"type":      "function_call",
				"call_id":   c.ID,
				"name":      c.Name,
				"arguments": c.Arguments,
			})
		}
		return items
	case llm.RoleSystem:
		return nil // handled via instructions
	default:
		if content := responsesContent(m.Parts); len(content) > 0 {
			return []map[string]any{{
				"type":    "message",
				"role":    string(m.Role),
				"content": content,
			}}
		}
		return []map[string]any{{
			"type":    "message",
			"role":    string(m.Role),
			"content": m.Text,
		}}
	}
}

// responsesContent builds Responses API content parts from media. It returns nil
// when there is no media to send.
func responsesContent(parts []llm.ContentPart) []map[string]any {
	var media []map[string]any
	for _, p := range parts {
		switch p.Type {
		case llm.PartImage:
			if p.URI != "" {
				media = append(media, map[string]any{"type": "input_image", "image_url": p.URI})
			} else if p.Data != "" {
				url := "data:" + mimeOr(p.MimeType, "image/png") + ";base64," + p.Data
				media = append(media, map[string]any{"type": "input_image", "image_url": url})
			}
		case llm.PartAudio:
			if p.Data != "" {
				media = append(media, map[string]any{
					"type": "input_audio",
					"input_audio": map[string]any{
						"data":   p.Data,
						"format": mimeSubtype(p.MimeType),
					},
				})
			}
		}
	}
	if len(media) == 0 {
		return nil
	}
	return media
}

// responsesEvent is the envelope shared by Responses SSE events.
type responsesEvent struct {
	Type     string `json:"type"`
	Delta    string `json:"delta"`
	CallID   string `json:"call_id"`
	Name     string `json:"name"`
	ItemID   string `json:"item_id"`
	Response *struct {
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"response"`
}

func parseResponsesSSE(ctx context.Context, body io.Reader, out chan<- llm.Chunk) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	type pending struct {
		id   string
		name string
		args strings.Builder
	}
	pendingCalls := map[string]*pending{}
	var order []string

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
		var ev responsesEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "response.output_text.delta":
			out <- llm.Chunk{TextDelta: ev.Delta}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			out <- llm.Chunk{ReasoningDelta: ev.Delta}
		case "response.output_item.added":
			// A function_call item begins; capture its metadata.
			if ev.ItemID != "" && ev.Name != "" {
				if _, ok := pendingCalls[ev.ItemID]; !ok {
					pendingCalls[ev.ItemID] = &pending{id: ev.CallID, name: ev.Name}
					order = append(order, ev.ItemID)
				}
			}
		case "response.function_call_arguments.delta":
			if p := pendingCalls[ev.ItemID]; p != nil {
				p.args.WriteString(ev.Delta)
			}
		case "response.completed":
			if ev.Response != nil && ev.Response.Usage != nil {
				out <- llm.Chunk{Usage: &llm.Usage{
					InputTokens:  ev.Response.Usage.InputTokens,
					OutputTokens: ev.Response.Usage.OutputTokens,
				}}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		out <- llm.Chunk{Err: err}
		return
	}
	for _, id := range order {
		p := pendingCalls[id]
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
