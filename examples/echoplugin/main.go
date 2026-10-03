// Command echoplugin is an example out-of-process Coren plugin written in Go.
//
// It speaks the pluginproto NDJSON JSON-RPC protocol over stdio and exposes a
// single `echo` tool. Build it and point the host at the binary:
//
//	go build -o /tmp/echoplugin ./examples/echoplugin
//
// then configure:
//
//	{"plugins": ["...", "plugin-host"], "external_plugins": [
//	  {"name": "echo", "command": "/tmp/echoplugin"}
//	]}
package main

import (
	"encoding/json"
	"fmt"

	"coren/pkg/pluginproto"
)

func main() {
	server := &pluginproto.Server{
		Name:    "echo",
		Version: "0.1.0",
		Tools: map[string]pluginproto.ServerTool{
			"echo": {
				Spec: pluginproto.ToolSpec{
					Name:        "echo",
					Description: "Echo the provided text back to the caller.",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"text": map[string]any{"type": "string", "description": "text to echo"},
						},
						"required": []string{"text"},
					},
				},
				Run: echo,
			},
		},
	}
	if err := server.ServeStdio(); err != nil {
		fmt.Println(err)
	}
}

// echo returns the "text" argument, or an error when it is missing.
func echo(arguments string) (string, error) {
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Text == "" {
		return "", fmt.Errorf("missing required argument %q", "text")
	}
	return "echo: " + args.Text, nil
}
