// Package mcp implements a Model Context Protocol client.
//
// It speaks JSON-RPC 2.0 over the remote transports MCP defines: Streamable
// HTTP (a single endpoint that answers with JSON or an SSE stream) and the older
// HTTP+SSE transport. A client exposes a server's tools, resources and prompts
// so a Coren plugin can surface them to the model.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ProtocolVersion is the MCP revision this client advertises.
const ProtocolVersion = "2025-06-18"

// request is a JSON-RPC 2.0 request.
type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// response is a JSON-RPC 2.0 response.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("mcp: rpc error %d: %s", e.Code, e.Message)
}

// Client is a connection to one MCP server.
type Client struct {
	// Name labels the server for diagnostics and tool namespacing.
	Name string
	// URL is the server endpoint.
	URL string
	// Headers are sent on every request (e.g. Authorization).
	Headers map[string]string
	// Transport is "streamable" (default) or "sse".
	Transport string
	// HTTPClient overrides the default client when set.
	HTTPClient *http.Client

	http      *http.Client
	nextID    atomic.Int64
	sessionID string
	mu        sync.Mutex

	// capabilities and serverInfo come from initialize.
	capabilities ServerCapabilities
	serverInfo   ServerInfo
	initialized  bool

	// sse transport state
	sseEndpoint string
	sseMessages chan []byte
	sseErr      error
	sseOnce     sync.Once
}

// ServerInfo describes the server from initialize.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerCapabilities lists which primitives the server supports.
type ServerCapabilities struct {
	Tools     *struct{} `json:"tools,omitempty"`
	Resources *struct{} `json:"resources,omitempty"`
	Prompts   *struct{} `json:"prompts,omitempty"`
}

func (c *Client) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 60 * time.Second}
	}
	return c.http
}

func (c *Client) transport() string {
	if c.Transport == "" {
		return "streamable"
	}
	return c.Transport
}

// Initialize performs the MCP handshake and records server capabilities.
func (c *Client) Initialize(ctx context.Context) error {
	c.mu.Lock()
	if c.initialized {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	params := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "coren",
			"version": "0.1.0",
		},
	}
	var result struct {
		ProtocolVersion string             `json:"protocolVersion"`
		Capabilities    ServerCapabilities `json:"capabilities"`
		ServerInfo      ServerInfo         `json:"serverInfo"`
	}
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		return err
	}
	c.mu.Lock()
	c.capabilities = result.Capabilities
	c.serverInfo = result.ServerInfo
	c.initialized = true
	c.mu.Unlock()

	// Best-effort notification; servers may ignore it.
	_ = c.notify(ctx, "notifications/initialized", nil)
	return nil
}

// ServerInfo returns the info reported during initialize.
func (c *Client) ServerInfo() ServerInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.serverInfo
}

// Capabilities returns the server capabilities reported during initialize.
func (c *Client) Capabilities() ServerCapabilities {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.capabilities
}

// call sends a request and decodes the result.
func (c *Client) call(ctx context.Context, method string, params, out any) error {
	id := c.nextID.Add(1)
	req := request{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	raw, err := c.roundTrip(ctx, body)
	if err != nil {
		return err
	}
	var resp response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("mcp: decode response: %w", err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("mcp: decode result for %s: %w", method, err)
		}
	}
	return nil
}

// notify sends a JSON-RPC notification (no response expected).
func (c *Client) notify(ctx context.Context, method string, params any) error {
	req := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		req["params"] = params
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = c.roundTrip(ctx, body)
	return err
}

// roundTrip POSTs a JSON-RPC message and returns the response body bytes.
func (c *Client) roundTrip(ctx context.Context, body []byte) ([]byte, error) {
	if c.transport() == "sse" {
		return c.sseRoundTrip(ctx, body)
	}
	return c.streamableRoundTrip(ctx, body)
}

// isNotification reports whether a JSON-RPC body has no id (a notification).
func isNotification(body []byte) bool {
	var probe struct {
		ID *int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	return probe.ID == nil
}

// idOf extracts the JSON-RPC id from a request body.
func idOf(body []byte) int64 {
	var probe struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(body, &probe)
	return probe.ID
}

// responseID extracts the id from a response message.
func responseID(msg []byte) (int64, bool) {
	var probe struct {
		ID *int64 `json:"id"`
	}
	if err := json.Unmarshal(msg, &probe); err != nil || probe.ID == nil {
		return 0, false
	}
	return *probe.ID, true
}

// streamableRoundTrip posts to the single MCP endpoint. The response is either
// application/json or text/event-stream carrying the reply.
func (c *Client) streamableRoundTrip(ctx context.Context, body []byte) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range c.Headers {
		httpReq.Header.Set(k, v)
	}
	c.mu.Lock()
	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	c.mu.Unlock()

	resp, err := c.client().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.mu.Lock()
		c.sessionID = sid
		c.mu.Unlock()
	}

	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
		return nil, nil // notification acknowledged
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return nil, fmt.Errorf("mcp: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "text/event-stream") {
		return readSSEMessage(resp.Body)
	}
	return io.ReadAll(resp.Body)
}

// sseRoundTrip uses the legacy HTTP+SSE transport: a GET opens the event stream
// (carrying the endpoint), and requests are POSTed to that endpoint; the reply
// arrives on the stream. Responses are matched by JSON-RPC id, so unrelated
// messages (such as notifications or server-initiated requests) are skipped.
func (c *Client) sseRoundTrip(ctx context.Context, body []byte) ([]byte, error) {
	endpoint, err := c.openSSE(ctx)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range c.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := c.client().Do(httpReq)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()

	// Notifications have no reply; the server acknowledges over HTTP only.
	if isNotification(body) {
		return nil, nil
	}

	want := idOf(body)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case msg, ok := <-c.sseMessages:
			if !ok {
				return nil, fmt.Errorf("mcp: SSE stream closed")
			}
			if id, ok := responseID(msg); ok && id == want {
				return msg, nil
			}
			// Not our response: ignore and keep reading.
		}
	}
}

// openSSE lazily opens the SSE stream and returns its POST endpoint.
func (c *Client) openSSE(ctx context.Context) (string, error) {
	c.sseOnce.Do(func() {
		c.sseMessages = make(chan []byte, 32)
		endpoint, err := c.startSSE(ctx)
		if err != nil {
			c.sseErr = err
			close(c.sseMessages)
			return
		}
		c.sseEndpoint = endpoint
	})
	if c.sseErr != nil {
		return "", c.sseErr
	}
	return c.sseEndpoint, nil
}

// startSSE opens the event stream, reads the "endpoint" event, and pumps
// subsequent "message" events into c.sseMessages.
func (c *Client) startSSE(ctx context.Context) (string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	for k, v := range c.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := c.client().Do(httpReq)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return "", fmt.Errorf("mcp: SSE connect HTTP %d", resp.StatusCode)
	}

	scanner := newSSEScanner(resp.Body)
	// First event carries the endpoint.
	event, data, err := scanner.next()
	if err != nil {
		resp.Body.Close()
		return "", fmt.Errorf("mcp: reading SSE endpoint: %w", err)
	}
	endpoint := strings.TrimSpace(data)
	if event != "endpoint" || endpoint == "" {
		resp.Body.Close()
		return "", fmt.Errorf("mcp: SSE stream did not announce an endpoint")
	}
	if !strings.HasPrefix(endpoint, "http") {
		endpoint = resolveEndpoint(c.URL, endpoint)
	}

	// Pump the rest in the background.
	go func() {
		defer resp.Body.Close()
		for {
			_, data, err := scanner.next()
			if err != nil {
				close(c.sseMessages)
				return
			}
			c.sseMessages <- []byte(data)
		}
	}()
	return endpoint, nil
}
