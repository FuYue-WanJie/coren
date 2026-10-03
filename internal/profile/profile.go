// Package profile composes a Coren kernel from a named, ordered list of plugins.
//
// A profile is data, not code: it names the capabilities to mount so the set of
// running features is configuration rather than a hard-coded sequence.
package profile

import (
	"fmt"
	"sort"
)

// Profile is a named composition of plugins.
type Profile struct {
	// Name identifies the profile, e.g. "web" or "cli".
	Name string `json:"name"`
	// Plugins lists plugin identifiers in intended mount order. Dependency
	// resolution still runs, so the order only breaks ties.
	Plugins []string `json:"plugins"`
	// Description is shown by "coren profile list".
	Description string `json:"description,omitempty"`
}

// Known plugin identifiers. Keep them centralized so profiles cannot drift.
const (
	PluginLLM           = "llm"
	PluginLLMOpenAI     = "llm.openai"
	PluginTools         = "tools"
	PluginToolsBuiltin  = "tools.builtin"
	PluginSessions      = "sessions"
	PluginAgents        = "agents"
	PluginAgentLoop     = "agent-loop"
	PluginShellCLI      = "shell.cli"
	PluginShellWeb      = "shell.web"
	PluginToolsClock    = "tools.clock"
	PluginLogging       = "logging"
	PluginSkills        = "skills"
	PluginSkillsTools   = "skills.tools"
	PluginSubagents     = "subagents"
	PluginSubagentsIP   = "subagents.in-process"
	PluginSubagentsTool = "subagents.tool"
	PluginAsk           = "ask"
	PluginMCP           = "mcp"
	PluginMemory        = "memory"
	PluginTodo          = "todo"
	PluginDelivery      = "delivery"
	PluginDeliver       = "deliver"
	PluginApproval      = "approval"
	PluginGuard         = "guard"
	PluginHost          = "plugin-host"
)

// base is the shared plugin set most profiles build on: model, tools, sessions,
// agents, the agent loop, skills, subagent delegation, and user questioning.
var base = []string{
	PluginLLM, PluginSessions, PluginTools, PluginAgents,
	PluginLLMOpenAI, PluginToolsBuiltin, PluginAgentLoop,
	PluginSkills, PluginSkillsTools,
	PluginSubagents, PluginSubagentsIP, PluginSubagentsTool,
	PluginAsk, PluginMCP, PluginMemory, PluginTodo, PluginDelivery, PluginDeliver,
	PluginApproval, PluginGuard,
}

// withBase returns a copy of base with extra plugins appended.
func withBase(extra ...string) []string {
	out := make([]string, 0, len(base)+len(extra))
	out = append(out, base...)
	return append(out, extra...)
}

// builtin holds the profiles that ship with Coren.
var builtin = map[string]Profile{
	"core": {
		Name:        "core",
		Description: "Model, tools, sessions, skills, and the agent loop; no user-facing shell.",
		Plugins:     withBase(),
	},
	"web": {
		Name:        "web",
		Description: "HTTP API and browser WebUI.",
		Plugins:     withBase(PluginShellWeb),
	},
	"cli": {
		Name:        "cli",
		Description: "Interactive terminal session.",
		Plugins:     withBase(PluginShellCLI),
	},
	"headless": {
		Name:        "headless",
		Description: "No shell; for embedding or one-shot runs driven by the caller.",
		Plugins:     withBase(),
	},
	"cli-clock": {
		Name:        "cli-clock",
		Description: "Interactive terminal session with the example clock tool and tracing.",
		Plugins:     withBase(PluginToolsClock, PluginLogging, PluginShellCLI),
	},
	"web-verbose": {
		Name:        "web-verbose",
		Description: "Web profile plus event tracing to stderr.",
		Plugins:     withBase(PluginLogging, PluginShellWeb),
	},
}

// Resolve returns the built-in profile with the given name.
func Resolve(name string) (Profile, error) {
	if name == "" {
		name = "web"
	}
	p, ok := builtin[name]
	if !ok {
		return Profile{}, fmt.Errorf("unknown profile %q (available: %v)", name, Names())
	}
	return p, nil
}

// Names lists the built-in profile names, sorted.
func Names() []string {
	names := make([]string, 0, len(builtin))
	for name := range builtin {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// All returns every built-in profile, sorted by name.
func All() []Profile {
	names := Names()
	out := make([]Profile, 0, len(names))
	for _, name := range names {
		out = append(out, builtin[name])
	}
	return out
}

// Has reports whether a plugin id is part of the profile.
func (p Profile) Has(plugin string) bool {
	for _, id := range p.Plugins {
		if id == plugin {
			return true
		}
	}
	return false
}
