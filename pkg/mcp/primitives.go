package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Tool is a tool advertised by an MCP server.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolResult is the outcome of tools/call.
type ToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Content is one piece of an MCP result.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// Data and MimeType appear on image/audio content.
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// Resource is a resource advertised by an MCP server.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceContent is the outcome of resources/read.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// Prompt is a prompt template advertised by an MCP server.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument describes one prompt argument.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptMessage is one message returned by prompts/get.
type PromptMessage struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

// ListTools returns the server's tools.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool invokes a tool and returns its result.
func (c *Client) CallTool(ctx context.Context, name string, arguments string) (ToolResult, error) {
	var args any
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return ToolResult{}, fmt.Errorf("mcp: tool arguments must be JSON: %w", err)
		}
	}
	params := map[string]any{"name": name, "arguments": args}
	var result ToolResult
	if err := c.call(ctx, "tools/call", params, &result); err != nil {
		return ToolResult{}, err
	}
	return result, nil
}

// ListResources returns the server's resources.
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	var result struct {
		Resources []Resource `json:"resources"`
	}
	if err := c.call(ctx, "resources/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Resources, nil
}

// ReadResource reads one resource by URI.
func (c *Client) ReadResource(ctx context.Context, uri string) ([]ResourceContent, error) {
	var result struct {
		Contents []ResourceContent `json:"contents"`
	}
	if err := c.call(ctx, "resources/read", map[string]any{"uri": uri}, &result); err != nil {
		return nil, err
	}
	return result.Contents, nil
}

// ListPrompts returns the server's prompts.
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	var result struct {
		Prompts []Prompt `json:"prompts"`
	}
	if err := c.call(ctx, "prompts/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Prompts, nil
}

// GetPrompt renders a prompt by name with the given arguments.
func (c *Client) GetPrompt(ctx context.Context, name string, arguments map[string]string) ([]PromptMessage, error) {
	params := map[string]any{"name": name}
	if len(arguments) > 0 {
		params["arguments"] = arguments
	}
	var result struct {
		Messages []PromptMessage `json:"messages"`
	}
	if err := c.call(ctx, "prompts/get", params, &result); err != nil {
		return nil, err
	}
	return result.Messages, nil
}

// Text returns a tool result's concatenated text content.
func (r ToolResult) Text() string {
	var b strings.Builder
	for _, c := range r.Content {
		switch c.Type {
		case "text":
			b.WriteString(c.Text)
		case "image", "audio":
			fmt.Fprintf(&b, "[%s content: %s]", c.Type, c.MimeType)
		}
	}
	return b.String()
}
