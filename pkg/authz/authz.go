// Package authz defines Coren's authorization levels.
//
// A level bounds what the agent may do without asking: readonly forbids
// mutation and execution, trusted allows them behind confirmation for risky
// actions, and full removes the guardrails. The level is the outer policy;
// individual approvals are decided by the approval service.
package authz

import "strings"

// Level is an authorization level.
type Level string

const (
	// Readonly permits observation only: reading files, listing, searching.
	Readonly Level = "readonly"
	// Trusted permits mutation and execution, but risky actions need approval.
	Trusted Level = "trusted"
	// Full permits everything without approval.
	Full Level = "full"
)

// Parse normalizes a level string, returning the level and whether it was valid.
// Empty input defaults to Trusted.
func Parse(s string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "trusted":
		return Trusted, true
	case "readonly", "read-only", "ro":
		return Readonly, true
	case "full", "unrestricted":
		return Full, true
	default:
		return Trusted, false
	}
}

// Rank orders levels by permissiveness for comparison.
func (l Level) Rank() int {
	switch l {
	case Readonly:
		return 0
	case Trusted:
		return 1
	case Full:
		return 2
	default:
		return 1
	}
}

// Allows reports whether this level is at least as permissive as other.
func (l Level) Allows(other Level) bool {
	return l.Rank() >= other.Rank()
}

// Action classifies what a tool does, so policy can reason about it without
// knowing tool names.
type Action string

const (
	// ActionRead observes without side effects.
	ActionRead Action = "read"
	// ActionWrite changes local state (files, git).
	ActionWrite Action = "write"
	// ActionExecute runs commands.
	ActionExecute Action = "execute"
	// ActionNetwork reaches outside the machine.
	ActionNetwork Action = "network"
)

// RequiredLevel returns the minimum level at which an action is permitted at all.
//
// Read-only is always permitted. Write and execute need Trusted or above (the
// approval service then decides whether confirmation is required). Network is
// allowed from Readonly because fetching a URL is observational for the caller.
func RequiredLevel(action Action) Level {
	switch action {
	case ActionWrite, ActionExecute:
		return Trusted
	default:
		return Readonly
	}
}

// Permits reports whether the level permits an action without immediate denial.
func (l Level) Permits(action Action) bool {
	return l.Allows(RequiredLevel(action))
}

// ActionForTool maps a tool name to an action classification.
//
// Extension tools register descriptors with their own action; this covers the
// built-ins.
func ActionForTool(name string) Action {
	switch name {
	case "read_file", "recall", "list_skills", "list_resources", "read_resource",
		"list_prompts", "get_prompt":
		return ActionRead
	case "write_file", "remember":
		return ActionWrite
	case "run_shell", "task":
		return ActionExecute
	case "http_request":
		return ActionNetwork
	default:
		return ActionExecute // conservative default for unknown/extension tools
	}
}
