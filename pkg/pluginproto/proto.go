// Package pluginproto defines the wire protocol for out-of-process plugins.
//
// A plugin is a child process the host launches. They exchange newline-delimited
// JSON-RPC 2.0 messages over the child's stdin/stdout; stderr carries logs and is
// never parsed. The protocol is language-agnostic: a plugin may be written in any
// language that can read lines and print JSON.
//
// Handshake:
//
//	host -> plugin  initialize {protocol_version, ...}
//	plugin -> host  result {protocol_version, name, version, tools: [...]}
//
// Then the host may call tools:
//
//	host -> plugin  tools/call {name, arguments}
//	plugin -> host  result {output, is_error?}
//
// The host sends shutdown and waits for the process to exit.
package pluginproto

// Version is the protocol version both sides must agree on.
const Version = 1

// Method names exchanged over the wire.
const (
	// MethodInitialize is the first request; the plugin replies with Handshake.
	MethodInitialize = "initialize"
	// MethodToolsCall runs a tool declared in the handshake.
	MethodToolsCall = "tools/call"
	// MethodShutdown asks the plugin to exit cleanly.
	MethodShutdown = "shutdown"
)

// InitializeParams accompanies MethodInitialize.
type InitializeParams struct {
	// ProtocolVersion is the host's Version; the plugin must echo a compatible one.
	ProtocolVersion int `json:"protocol_version"`
	// WorkDir is the working directory the host wants tools scoped to.
	WorkDir string `json:"work_dir,omitempty"`
	// HostVersion is informational.
	HostVersion string `json:"host_version,omitempty"`
}

// Handshake is the plugin's reply to initialize.
type Handshake struct {
	// ProtocolVersion must equal Version for the host to accept the plugin.
	ProtocolVersion int `json:"protocol_version"`
	// Name identifies the plugin for diagnostics.
	Name string `json:"name"`
	// Version is the plugin's own version string.
	Version string `json:"version,omitempty"`
	// Tools declares the tools the plugin exposes.
	Tools []ToolSpec `json:"tools"`
}

// ToolSpec declares one tool.
type ToolSpec struct {
	// Name is the tool name the model sees; must be unique across plugins.
	Name string `json:"name"`
	// Description tells the model when to use the tool.
	Description string `json:"description,omitempty"`
	// Parameters is a JSON Schema object for the tool arguments.
	Parameters map[string]any `json:"parameters,omitempty"`
}

// CallParams accompanies MethodToolsCall.
type CallParams struct {
	// Name is the tool name from the handshake.
	Name string `json:"name"`
	// Arguments is the raw JSON string the model produced.
	Arguments string `json:"arguments,omitempty"`
}

// CallResult is the plugin's reply to a tool call.
type CallResult struct {
	// Output is the textual result shown to the model.
	Output string `json:"output"`
	// IsError marks the output as an error rather than a normal result.
	IsError bool `json:"is_error,omitempty"`
}
