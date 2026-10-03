package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newMockServer builds an httptest server implementing the Streamable HTTP
// transport for a tiny MCP surface: initialize, tools/list, tools/call,
// resources/list, resources/read, prompts/list, prompts/get.
func newMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Notifications (no id) are acknowledged with 202.
		if req.ID == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		result := mockResult(t, req.Method, req.Params)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	})
	return httptest.NewServer(handler)
}

func mockResult(t *testing.T, method string, params json.RawMessage) any {
	t.Helper()
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{},
				"prompts":   map[string]any{},
			},
			"serverInfo": map[string]any{"name": "mock", "version": "1.0"},
		}
	case "tools/list":
		return map[string]any{
			"tools": []map[string]any{{
				"name":        "echo",
				"description": "Echo text back.",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"text": map[string]any{"type": "string"}},
					"required":   []string{"text"},
				},
			}},
		}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(params, &p)
		text, _ := p.Arguments["text"].(string)
		if p.Name != "echo" {
			return map[string]any{
				"content": []map[string]any{{"type": "text", "text": "unknown tool"}},
				"isError": true,
			}
		}
		return map[string]any{"content": []map[string]any{{"type": "text", "text": "echo: " + text}}}
	case "resources/list":
		return map[string]any{
			"resources": []map[string]any{{
				"uri":         "mock://readme",
				"name":        "readme",
				"description": "The readme.",
			}},
		}
	case "resources/read":
		return map[string]any{
			"contents": []map[string]any{{
				"uri":      "mock://readme",
				"mimeType": "text/plain",
				"text":     "hello from resource",
			}},
		}
	case "prompts/list":
		return map[string]any{
			"prompts": []map[string]any{{
				"name":        "greet",
				"description": "A greeting prompt.",
				"arguments":   []map[string]any{{"name": "who", "required": true}},
			}},
		}
	case "prompts/get":
		return map[string]any{
			"messages": []map[string]any{{
				"role":    "user",
				"content": map[string]any{"type": "text", "text": "hello, world"},
			}},
		}
	default:
		return map[string]any{}
	}
}

func TestClientInitialize(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()

	client := &Client{Name: "mock", URL: srv.URL}
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.ServerInfo().Name != "mock" {
		t.Errorf("server info = %+v", client.ServerInfo())
	}
	caps := client.Capabilities()
	if caps.Tools == nil || caps.Resources == nil || caps.Prompts == nil {
		t.Errorf("capabilities = %+v", caps)
	}
}

func TestClientListAndCallTool(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()

	client := &Client{Name: "mock", URL: srv.URL}
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", tools)
	}

	result, err := client.CallTool(context.Background(), "echo", `{"text":"hi"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text() != "echo: hi" {
		t.Errorf("text = %q", result.Text())
	}
}

func TestClientResourcesAndPrompts(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()

	client := &Client{Name: "mock", URL: srv.URL}
	_ = client.Initialize(context.Background())

	res, err := client.ListResources(context.Background())
	if err != nil || len(res) != 1 || res[0].URI != "mock://readme" {
		t.Fatalf("resources = %+v, err = %v", res, err)
	}
	contents, err := client.ReadResource(context.Background(), "mock://readme")
	if err != nil || len(contents) != 1 || contents[0].Text != "hello from resource" {
		t.Fatalf("contents = %+v, err = %v", contents, err)
	}

	prompts, err := client.ListPrompts(context.Background())
	if err != nil || len(prompts) != 1 || prompts[0].Name != "greet" {
		t.Fatalf("prompts = %+v, err = %v", prompts, err)
	}
	messages, err := client.GetPrompt(context.Background(), "greet", map[string]string{"who": "world"})
	if err != nil || len(messages) != 1 || messages[0].Content.Text != "hello, world" {
		t.Fatalf("messages = %+v, err = %v", messages, err)
	}
}

func TestClientStreamableSSEResponse(t *testing.T) {
	// A Streamable HTTP server that answers with an SSE stream instead of JSON.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ID == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		result := mockResult(t, req.Method, nil)
		payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
	}))
	defer srv.Close()

	client := &Client{Name: "sse", URL: srv.URL}
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Errorf("tools = %+v", tools)
	}
}

func TestClientLegacySSETransport(t *testing.T) {
	// Legacy transport: GET /sse announces the POST endpoint; replies arrive on
	// the event stream.
	messageCh := make(chan string, 8)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", srv.URL+"/messages")
			flusher.Flush()
			for msg := range messageCh {
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
				flusher.Flush()
			}
		case "/messages":
			var req struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.ID == 0 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			result := mockResult(t, req.Method, nil)
			payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
			messageCh <- string(payload)
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer srv.Close()
	defer close(messageCh)

	client := &Client{Name: "legacy", URL: srv.URL + "/sse", Transport: "sse"}
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Errorf("tools = %+v", tools)
	}
}

func TestToolResultText(t *testing.T) {
	r := ToolResult{Content: []Content{
		{Type: "text", Text: "a"},
		{Type: "image", MimeType: "image/png"},
		{Type: "text", Text: "b"},
	}}
	got := r.Text()
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") || !strings.Contains(got, "image/png") {
		t.Errorf("text = %q", got)
	}
}
