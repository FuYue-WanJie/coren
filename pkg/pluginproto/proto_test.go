package pluginproto

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// newDuplexPair creates matched reader/writer pairs using two pipes.
func newDuplexPair(t *testing.T) (hostR io.Reader, hostW io.Writer, plugR io.Reader, plugW io.Writer, cleanup func()) {
	t.Helper()
	// host->plugin
	h2pR, h2pW := io.Pipe()
	// plugin->host
	p2hR, p2hW := io.Pipe()
	return p2hR, h2pW, h2pR, p2hW, func() {
		_ = h2pR.Close()
		_ = h2pW.Close()
		_ = p2hR.Close()
		_ = p2hW.Close()
	}
}

func TestClientServerHandshakeAndCall(t *testing.T) {
	hostR, hostW, plugR, plugW, cleanup := newDuplexPair(t)
	defer cleanup()

	srv := &Server{
		Name:    "echo",
		Version: "0.1.0",
		Tools: map[string]ServerTool{
			"echo": {
				Spec: ToolSpec{Name: "echo", Description: "echo back"},
				Run: func(args string) (string, error) {
					return "got:" + args, nil
				},
			},
		},
	}
	go func() { _ = srv.Serve(plugR, plugW) }()

	client := NewClient(hostR, hostW)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	hs, err := client.Initialize(ctx, InitializeParams{})
	if err != nil {
		t.Fatal(err)
	}
	if hs.Name != "echo" || hs.ProtocolVersion != Version {
		t.Fatalf("handshake = %+v", hs)
	}
	if len(hs.Tools) != 1 || hs.Tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", hs.Tools)
	}

	res, err := client.CallTool(ctx, "echo", `{"x":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "got:"+`{"x":1}` || res.IsError {
		t.Fatalf("call result = %+v", res)
	}
}

func TestServerToolErrorIsReported(t *testing.T) {
	hostR, hostW, plugR, plugW, cleanup := newDuplexPair(t)
	defer cleanup()

	srv := &Server{
		Name: "boom",
		Tools: map[string]ServerTool{
			"fail": {
				Spec: ToolSpec{Name: "fail"},
				Run: func(string) (string, error) {
					return "", errString("kaboom")
				},
			},
		},
	}
	go func() { _ = srv.Serve(plugR, plugW) }()

	client := NewClient(hostR, hostW)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := client.Initialize(ctx, InitializeParams{}); err != nil {
		t.Fatal(err)
	}
	res, err := client.CallTool(ctx, "fail", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Output, "kaboom") {
		t.Fatalf("expected error result, got %+v", res)
	}
}

func TestClientRejectsVersionMismatch(t *testing.T) {
	hostR, hostW, plugR, plugW, cleanup := newDuplexPair(t)
	defer cleanup()

	// A server that always advertises a bogus protocol version.
	go func() {
		dec := NewConn(plugR, plugW)
		_ = dec
		// Manually reply with a wrong version.
		for {
			data, ok := dec.readMessage()
			if !ok {
				return
			}
			var req rpcRequest
			if err := json.Unmarshal(data, &req); err != nil {
				continue
			}
			_ = writeLine(plugW, rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  mustJSON(Handshake{ProtocolVersion: Version + 99, Name: "old"}),
			})
		}
	}()

	client := NewClient(hostR, hostW)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := client.Initialize(ctx, InitializeParams{}); err == nil {
		t.Fatal("expected version mismatch error")
	}
}

// errString is a tiny error type for tests.
type errString string

func (e errString) Error() string { return string(e) }
