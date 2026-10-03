package pluginproto

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// ToolFunc handles one tool call: it receives raw JSON arguments and returns
// output text.
type ToolFunc func(arguments string) (string, error)

// Server is the plugin side of the protocol. A Go plugin implements tool
// handlers and calls Serve, which blocks until the host sends shutdown or
// closes stdin.
type Server struct {
	// Name identifies the plugin in the handshake.
	Name string
	// Version is the plugin's version string.
	Version string
	// Tools maps tool name to its declaration and handler.
	Tools map[string]ServerTool
}

// ServerTool declares a tool and its handler.
type ServerTool struct {
	Spec ToolSpec
	Run  ToolFunc
}

// Serve reads requests from in and writes responses to out until shutdown.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		if err := s.handle(req, out); err != nil {
			return err
		}
		if req.Method == MethodShutdown {
			return nil
		}
	}
	return scanner.Err()
}

// ServeStdio runs the server over the process's stdin/stdout.
func (s *Server) ServeStdio() error {
	return s.Serve(os.Stdin, os.Stdout)
}

// handle dispatches one request and writes the response.
func (s *Server) handle(req rpcRequest, out io.Writer) error {
	if req.Method == MethodShutdown {
		return nil
	}
	if req.ID == 0 {
		return nil // notification, no reply
	}
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case MethodInitialize:
		resp.Result = mustJSON(s.handshake())
	case MethodToolsCall:
		var params CallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &RPCError{Code: CodeError, Message: err.Error()}
			break
		}
		tool, ok := s.Tools[params.Name]
		if !ok {
			resp.Error = &RPCError{Code: CodeError, Message: "unknown tool " + params.Name}
			break
		}
		text, err := tool.Run(params.Arguments)
		if err != nil {
			resp.Result = mustJSON(CallResult{Output: err.Error(), IsError: true})
			break
		}
		resp.Result = mustJSON(CallResult{Output: text})
	default:
		resp.Error = &RPCError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return writeLine(out, resp)
}

// handshake builds the initialize reply from the declared tools.
func (s *Server) handshake() Handshake {
	specs := make([]ToolSpec, 0, len(s.Tools))
	for name, t := range s.Tools {
		spec := t.Spec
		if spec.Name == "" {
			spec.Name = name
		}
		specs = append(specs, spec)
	}
	return Handshake{
		ProtocolVersion: Version,
		Name:            s.Name,
		Version:         s.Version,
		Tools:           specs,
	}
}

// mustJSON marshals v, panicking only on impossible errors for our own types.
func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("pluginproto: marshal: %v", err))
	}
	return data
}

// writeLine writes one JSON message followed by a newline.
func writeLine(out io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := out.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}
