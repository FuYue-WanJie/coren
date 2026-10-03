package pluginproto

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// Client is the host side of a plugin connection. It sends requests and matches
// responses by id on a background read loop.
type Client struct {
	conn *Conn
	done chan struct{}
}

// NewClient starts a client over the given streams and begins reading.
func NewClient(r io.Reader, w io.Writer) *Client {
	c := &Client{
		conn: NewConn(r, w),
		done: make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// readLoop dispatches responses to their pending callers until the stream ends.
func (c *Client) readLoop() {
	defer close(c.done)
	for {
		data, ok := c.conn.readMessage()
		if !ok {
			return
		}
		var resp rpcResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			continue
		}
		if resp.ID == 0 {
			continue // notification or stray
		}
		c.conn.mu.Lock()
		ch := c.conn.pending[resp.ID]
		delete(c.conn.pending, resp.ID)
		c.conn.mu.Unlock()
		if ch != nil {
			ch <- resp
			close(ch)
		}
	}
}

// call sends a request and waits for its response or ctx cancellation.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.conn.nextRequestID()
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		raw = data
	}
	ch := make(chan rpcResponse, 1)
	c.conn.mu.Lock()
	c.conn.pending[id] = ch
	c.conn.mu.Unlock()
	defer func() {
		c.conn.mu.Lock()
		delete(c.conn.pending, id)
		c.conn.mu.Unlock()
	}()

	if err := c.conn.writeInline(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: raw}); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("plugin connection closed")
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// notify sends a fire-and-forget notification (no id, no reply).
func (c *Client) notify(method string, params any) error {
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = data
	}
	return c.conn.writeInline(rpcRequest{JSONRPC: "2.0", Method: method, Params: raw})
}

// Initialize performs the handshake and returns the plugin's declared tools.
func (c *Client) Initialize(ctx context.Context, params InitializeParams) (Handshake, error) {
	if params.ProtocolVersion == 0 {
		params.ProtocolVersion = Version
	}
	result, err := c.call(ctx, MethodInitialize, params)
	if err != nil {
		return Handshake{}, err
	}
	var hs Handshake
	if err := json.Unmarshal(result, &hs); err != nil {
		return Handshake{}, fmt.Errorf("invalid handshake: %w", err)
	}
	if hs.ProtocolVersion != Version {
		return Handshake{}, fmt.Errorf("plugin protocol version %d, host expects %d", hs.ProtocolVersion, Version)
	}
	return hs, nil
}

// CallTool runs a tool and returns its result.
func (c *Client) CallTool(ctx context.Context, name, arguments string) (CallResult, error) {
	result, err := c.call(ctx, MethodToolsCall, CallParams{Name: name, Arguments: arguments})
	if err != nil {
		return CallResult{}, err
	}
	var out CallResult
	if err := json.Unmarshal(result, &out); err != nil {
		return CallResult{}, fmt.Errorf("invalid tool result: %w", err)
	}
	return out, nil
}

// Shutdown sends a best-effort shutdown notification.
func (c *Client) Shutdown() {
	_ = c.notify(MethodShutdown, nil)
}
