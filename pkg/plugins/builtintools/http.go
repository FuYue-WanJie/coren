package builtintools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"coren/pkg/llm"
)

// HTTPRequest performs an outbound HTTP request.
type HTTPRequest struct {
	Client *http.Client
}

func (t HTTPRequest) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "http_request",
		Description: "Perform an HTTP request and return the response body.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"method":  map[string]any{"type": "string", "description": "HTTP method, default GET."},
				"url":     map[string]any{"type": "string", "description": "Request URL."},
				"headers": map[string]any{"type": "object", "description": "Optional request headers."},
				"body":    map[string]any{"type": "string", "description": "Optional request body."},
			},
			"required": []string{"url"},
		},
	}
}

func (t HTTPRequest) Run(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if args.URL == "" {
		return "", fmt.Errorf("url is required")
	}
	method := strings.ToUpper(strings.TrimSpace(args.Method))
	if method == "" {
		method = http.MethodGet
	}

	var body io.Reader
	if args.Body != "" {
		body = strings.NewReader(args.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, args.URL, body)
	if err != nil {
		return "", err
	}
	for k, v := range args.Headers {
		req.Header.Set(k, v)
	}

	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, string(data)), nil
}
