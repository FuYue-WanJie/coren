package pluginproto

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// JSON-RPC 2.0 message shapes. A request has ID and Method; a response has ID
// and exactly one of Result or Error; a notification omits ID.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("plugin rpc error %d: %s", e.Code, e.Message)
}

// CodeError is the JSON-RPC code for a plugin-side handler failure.
const CodeError = -32000

// Conn is an NDJSON JSON-RPC connection over a pair of streams. One Conn owns
// the write side; reads are serialized internally so a single reader goroutine
// can dispatch responses to pending calls.
type Conn struct {
	r  *bufio.Scanner
	w  io.Writer
	mu sync.Mutex

	nextID  int64
	pending map[int64]chan rpcResponse
	writeMu sync.Mutex
}

// NewConn wraps a reader (responses/requests) and writer (requests/responses).
func NewConn(r io.Reader, w io.Writer) *Conn {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	return &Conn{
		r:       scanner,
		w:       w,
		pending: map[int64]chan rpcResponse{},
	}
}

// nextRequestID returns a monotonic id.
func (c *Conn) nextRequestID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	return c.nextID
}

// writeInline writes one JSON message followed by a newline.
func (c *Conn) writeInline(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.w.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// readMessage reads one line and decodes it, returning false at EOF.
func (c *Conn) readMessage() ([]byte, bool) {
	if !c.r.Scan() {
		return nil, false
	}
	line := c.r.Bytes()
	out := make([]byte, len(line))
	copy(out, line)
	return out, true
}

// Err returns any read error after the stream ended.
func (c *Conn) Err() error { return c.r.Err() }
