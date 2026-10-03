// Package risk flags tool calls that need confirmation or are outright denied.
//
// Rules are patterns over a tool call's arguments (and, for shell commands, over
// the command text). A rule either requires approval or denies the call. The
// built-in rules cover destructive commands and writes outside a sandbox root;
// users can add, disable, or override them through configuration.
package risk

import (
	"encoding/json"
	"regexp"
)

// Severity is what a matching rule does.
type Severity string

const (
	// Confirm requires user approval before running.
	Confirm Severity = "confirm"
	// Deny blocks the call outright.
	Deny Severity = "deny"
)

// Rule matches a tool call and decides its severity.
type Rule struct {
	// Name identifies the rule in messages and config.
	Name string `json:"name"`
	// Severity is "confirm" or "deny".
	Severity Severity `json:"severity"`
	// Tools lists tool names the rule applies to; empty means all tools.
	Tools []string `json:"tools,omitempty"`
	// Pattern is a regular expression matched against the tool arguments.
	Pattern string `json:"pattern"`
	// Reason explains the match to the user.
	Reason string `json:"reason,omitempty"`

	re *regexp.Regexp
}

// Findings is the result of evaluating rules against a call.
type Findings struct {
	// Severity is the most severe finding, or empty if none matched.
	Severity Severity
	// Matched names the rules that matched, in order.
	Matched []string
	// Reason is the first matched rule's explanation.
	Reason string
}

// NeedsConfirmation reports whether the call requires approval.
func (f Findings) NeedsConfirmation() bool { return f.Severity == Confirm || f.Severity == Deny }

// Denied reports whether the call is blocked outright.
func (f Findings) Denied() bool { return f.Severity == Deny }

// Engine evaluates rules.
type Engine struct {
	rules []Rule
}

// NewEngine compiles rules. Invalid patterns are returned as errors so bad
// configuration fails loudly at boot.
func NewEngine(rules []Rule) (*Engine, error) {
	compiled := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if r.Pattern == "" {
			continue
		}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, err
		}
		r.re = re
		compiled = append(compiled, r)
	}
	return &Engine{rules: compiled}, nil
}

// Rules returns the compiled rules.
func (e *Engine) Rules() []Rule { return e.rules }

// Evaluate returns the findings for a tool call.
func (e *Engine) Evaluate(toolName, arguments string) Findings {
	// Shell commands are matched against the decoded command text as well as the
	// raw JSON, so patterns read naturally.
	haystacks := []string{arguments}
	if command := shellCommand(arguments); command != "" {
		haystacks = append(haystacks, command)
	}

	var findings Findings
	for _, r := range e.rules {
		if !appliesToTool(r, toolName) {
			continue
		}
		if !matchesAny(r.re, haystacks) {
			continue
		}
		findings.Matched = append(findings.Matched, r.Name)
		if findings.Reason == "" {
			findings.Reason = r.Reason
		}
		if r.Severity == Deny {
			findings.Severity = Deny
			break // deny dominates
		}
		findings.Severity = Confirm
	}
	return findings
}

func appliesToTool(r Rule, tool string) bool {
	if len(r.Tools) == 0 {
		return true
	}
	for _, t := range r.Tools {
		if t == tool {
			return true
		}
	}
	return false
}

func matchesAny(re *regexp.Regexp, values []string) bool {
	for _, v := range values {
		if re.MatchString(v) {
			return true
		}
	}
	return false
}

// shellCommand decodes a run_shell command argument, if present.
func shellCommand(arguments string) string {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return ""
	}
	return args.Command
}

// DefaultRules are the built-in risk rules.
func DefaultRules() []Rule {
	return []Rule{
		{Name: "rm-recursive", Severity: Deny, Tools: []string{"run_shell"},
			Pattern: `\brm\s+(-[a-zA-Z]*[rR][a-zA-Z]*\b[^\n|]*)`,
			Reason:  "recursive delete can destroy data"},
		{Name: "destructive-rm", Severity: Confirm, Tools: []string{"run_shell"},
			Pattern: `\brm\b`, Reason: "file deletion requires confirmation"},
		{Name: "privilege-escalation", Severity: Confirm, Tools: []string{"run_shell"},
			Pattern: `\b(sudo|su|doas)\b`, Reason: "privilege escalation"},
		{Name: "pipe-to-shell", Severity: Confirm, Tools: []string{"run_shell"},
			Pattern: `(curl|wget)[^|]*\|\s*(ba)?sh`, Reason: "piping remote content to a shell"},
		{Name: "force-push", Severity: Confirm, Tools: []string{"run_shell"},
			Pattern: `git\s+push[^\n]*--force`, Reason: "force push rewrites remote history"},
		{Name: "disk-write", Severity: Deny, Tools: []string{"run_shell"},
			Pattern: `\b(dd|mkfs|fdisk)\b`, Reason: "raw disk operations are blocked"},
		{Name: "system-control", Severity: Deny, Tools: []string{"run_shell"},
			Pattern: `\b(shutdown|reboot|poweroff|halt)\b`, Reason: "system power control is blocked"},
		{Name: "history-rewrite", Severity: Confirm, Tools: []string{"run_shell"},
			Pattern: `git\s+(reset\s+--hard|clean\s+-[a-zA-Z]*f)`, Reason: "discards uncommitted work"},
	}
}
